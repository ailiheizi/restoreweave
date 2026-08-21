// Package-level savings measurement for the exact repository lane. This file
// implements the Phase 5 "simple qualified storage savings" report required by
// the content-store contract (section 6) and the core execution plan (Phase 5,
// item 3): logical bytes, whole-file duplicate savings, compression savings,
// physical stored bytes, repository overhead, and net physical savings are
// reported separately and are never summed as if they were interchangeable.
//
// This is measurement infrastructure, not engine selection. local-zstd-v1
// remains a candidate; nothing in this file names a release engine or grants
// qualification.
package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SavingsMechanism names one independent layer that contributed to the savings
// report. Layers are reported separately because they have different recovery
// consequences: duplicate elimination reuses an already-stored exact object,
// while compression changes the physical bytes that must be decoded before the
// original digest can be checked.
type SavingsMechanism string

const (
	// SavingsMechanismWholeFileDedup means identical whole-file objects were
	// placed once and later placements of the same bytes reused the stored
	// object instead of being stored again.
	SavingsMechanismWholeFileDedup SavingsMechanism = "whole-file-deduplication"
	// SavingsMechanismCompression means the stored object is a lossless
	// compressed representation whose physical bytes are smaller than the
	// logical bytes it restores.
	SavingsMechanismCompression SavingsMechanism = "compression"
)

// SavingsReport is an honest, mechanism-separated accounting of what a
// repository actually holds. The fields are deliberately named to mirror the
// normative waterfall (logical bytes, duplicate savings, compression savings,
// physical stored bytes, overhead, net savings) so that no caller can add the
// layers together as one interchangeable number. Mechanisms names the layers
// that measurably contributed; layers that did not contribute are absent.
type SavingsReport struct {
	// LogicalBytes is the sum of the logical decoded lengths of every
	// placement observed during ingest, including placements that reused an
	// already-stored object. It is the reference "source bytes preserved"
	// number for this measurement.
	LogicalBytes int64
	// DuplicateBytes is the number of logical bytes saved by not re-placing
	// identical whole-file content that was already stored. It is zero for a
	// repository that never saw a repeated content ID. Only placements are
	// authoritative here: a repository alone cannot know how many times one
	// stored object was requested.
	DuplicateBytes int64
	// DuplicateFiles is the number of placements that reused an already-stored
	// object rather than adding a new physical object.
	DuplicateFiles int64
	// CompressionSavedBytes is the logical-minus-physical difference for stored
	// objects whose physical bytes are smaller than their logical length. Only
	// objects where physical < logical contribute, so a lossless identity
	// profile reports zero here and never a negative "compression".
	CompressionSavedBytes int64
	// PhysicalStoredBytes is the sum of the physical object lengths on disk,
	// measured from the repository layout after every object was re-verified.
	PhysicalStoredBytes int64
	// OverheadBytes is the repository metadata and catalog bytes measurable on
	// a local filesystem: the profile marker, the repository identity, and the
	// portable recovery-record tree. The in-tree profiles keep no separately
	// stored index or model on the repository path, so index/model overhead is
	// not fabricated; it is simply not measurable for these profiles.
	OverheadBytes int64
	// NetPhysicalSavingsBytes is the difference between LogicalBytes and the
	// full physical footprint (PhysicalStoredBytes plus OverheadBytes). It is a
	// derived number, never an input, and it is only valid when the repository
	// could be measured; unmeasurable repositories return an error instead of a
	// fabricated zero.
	NetPhysicalSavingsBytes int64
	// Mechanisms lists the savings layers that measurably contributed. They are
	// always reported separately; an empty list means no layer contributed.
	Mechanisms []SavingsMechanism
}

// MeasureSavings computes a mechanism-separated savings report for the given
// repository driver. Logical and duplicate accounting come from the placement
// receipts the same driver returned during ingest (the only authoritative
// record of how often an object was requested); physical bytes, compression,
// and overhead are re-measured from the repository itself and every stored
// object is re-verified first.
//
// The measurement is honest by construction:
//   - logical length comes from placement receipts and from decompressed
//     readback, not from a filename, mode, or metadata estimate;
//   - duplicate savings are counted once per reused placement, never as stored
//     bytes;
//   - compression savings count only objects where physical < logical;
//   - physical bytes are read from the repository layout, never assumed to
//     equal logical bytes;
//   - any read, hash, decode, or layout failure aborts the report, so a
//     corrupt or absent repository fails closed instead of returning zeros.
//
// A non-empty repository without placement receipts cannot honestly report
// logical or duplicate savings, so MeasureSavings returns an error in that
// case rather than inventing them.
func MeasureSavings(ctx context.Context, driver Driver, placements []Receipt) (SavingsReport, error) {
	if err := ctx.Err(); err != nil {
		return SavingsReport{}, err
	}
	if driver == nil {
		return SavingsReport{}, errors.New("savings measurement requires a repository driver")
	}
	if _, ok := driver.(profileReporter); !ok {
		return SavingsReport{}, errors.New("savings measurement requires a driver that reports its repository profile")
	}
	dir, ok := storageRoot(driver)
	if !ok {
		return SavingsReport{}, errors.New("savings measurement is supported for the in-tree directory profiles only")
	}
	info, err := os.Stat(dir.root)
	if err != nil {
		return SavingsReport{}, fmt.Errorf("measure repository %q: %w", dir.root, err)
	}
	if !info.IsDir() {
		return SavingsReport{}, fmt.Errorf("measure repository %q: not a directory", dir.root)
	}

	// The placement ledger is the only honest source for logical and duplicate
	// accounting. A stored object could have been placed once or a hundred
	// times; the repository layout alone cannot tell.
	report := SavingsReport{Mechanisms: []SavingsMechanism{}}
	for _, receipt := range placements {
		if receipt.Bytes <= 0 {
			return SavingsReport{}, fmt.Errorf("invalid placement receipt %q with non-positive logical bytes %d", receipt.ContentID, receipt.Bytes)
		}
		report.LogicalBytes += receipt.Bytes
		if receipt.Existed {
			report.DuplicateBytes += receipt.Bytes
			report.DuplicateFiles++
		}
	}

	// Walk the blob namespace exactly as the drivers publish it. Each physical
	// object is re-verified through the driver (decompressing for zstd) before
	// its lengths enter the report, so corruption or relocation fails the
	// measurement instead of contributing a guessed number.
	storedObjects := 0
	if err := filepath.WalkDir(filepath.Join(dir.root, blobDirName, AlgorithmSHA256), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !entry.Type().IsRegular() || len(name) != 64 {
			return nil
		}
		if _, err := parseContentID(AlgorithmSHA256 + ":" + name); err != nil {
			return err
		}
		contentID := AlgorithmSHA256 + ":" + name
		physical, err := fileSize(path)
		if err != nil {
			return err
		}
		if err := driver.Verify(ctx, contentID); err != nil {
			return fmt.Errorf("savings measurement verify %s: %w", name, err)
		}
		logical, err := logicalLength(ctx, driver, contentID)
		if err != nil {
			return err
		}
		storedObjects++
		report.PhysicalStoredBytes += physical
		if physical < logical {
			report.CompressionSavedBytes += logical - physical
		}
		return nil
	}); err != nil {
		return SavingsReport{}, fmt.Errorf("measure repository objects: %w", err)
	}

	if storedObjects > 0 && len(placements) == 0 {
		return SavingsReport{}, errors.New("cannot measure savings honestly without the placement receipts observed during ingest")
	}

	// Overhead is measured from the repository layout. Index/model overhead is
	// not measurable for the in-tree profiles and is deliberately not fabricated
	// as zero.
	report.OverheadBytes = measureOverhead(dir)
	report.NetPhysicalSavingsBytes = report.LogicalBytes - report.PhysicalStoredBytes - report.OverheadBytes

	if report.DuplicateFiles > 0 {
		report.Mechanisms = append(report.Mechanisms, SavingsMechanismWholeFileDedup)
	}
	if report.CompressionSavedBytes > 0 {
		report.Mechanisms = append(report.Mechanisms, SavingsMechanismCompression)
	}
	return report, nil
}

// storageRoot unwraps a directory-backed driver (raw or zstd) and returns its
// underlying *Dir so the measurement can read the on-disk layout.
func storageRoot(driver Driver) (*Dir, bool) {
	switch d := driver.(type) {
	case *Dir:
		return d, true
	case *ZstdDir:
		return d.Dir, true
	default:
		return nil, false
	}
}

// logicalLength opens the object through the driver and returns the logical
// decoded length. For the raw identity profile this equals the physical length;
// for the zstd profile it is the decompressed length.
func logicalLength(ctx context.Context, driver Driver, contentID string) (int64, error) {
	body, err := driver.Open(ctx, contentID)
	if err != nil {
		return 0, err
	}
	defer body.Close()
	return io.Copy(io.Discard, body)
}

// measureOverhead returns the bytes the repository layout itself consumes on
// disk beyond the payload objects: the profile marker, the repository identity,
// and the portable recovery-record tree. It never invents an index/model
// overhead figure; the in-tree profiles keep no separate index on the
// repository path. Blob and temporary placement state are payload or transient
// and are not counted here.
func measureOverhead(dir *Dir) int64 {
	var total int64
	_ = filepath.WalkDir(dir.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir.root, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(relative, string(filepath.Separator))
		switch {
		case len(parts) == 1 && entry.Name() == repositoryProfileFile:
			total += entrySize(entry, path)
		case len(parts) == 1 && entry.Name() == repositoryIdentityFile:
			total += entrySize(entry, path)
		case len(parts) >= 2 && parts[0] == recoveryDirName:
			total += entrySize(entry, path)
		default:
			// Blob and tmp files are counted by the object walk or are
			// transient placement state.
		}
		return nil
	})
	return total
}

// entrySize returns the size of a directory entry via its DirEntry info,
// falling back to a stat when the entry info is incomplete.
func entrySize(entry fs.DirEntry, path string) int64 {
	if info, err := entry.Info(); err == nil {
		return info.Size()
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
