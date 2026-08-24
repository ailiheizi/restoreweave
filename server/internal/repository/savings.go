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
	"crypto/sha256"
	"encoding/hex"
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

// SavingsCategoryStatus states whether a physical-overhead category has a
// measured value. UNMEASURED is intentionally distinct from zero: the
// in-tree repository profiles do not own external indexes/models, and their
// transient temp area is not a stable accounting surface.
type SavingsCategoryStatus string

const (
	SavingsCategoryMeasured   SavingsCategoryStatus = "MEASURED"
	SavingsCategoryUnmeasured SavingsCategoryStatus = "UNMEASURED"
)

// SavingsOverheadCategory is one independently reported physical-overhead
// category. Bytes is meaningful only when Status is MEASURED.
type SavingsOverheadCategory struct {
	Bytes  int64                 `json:"bytes"`
	Status SavingsCategoryStatus `json:"status"`
}

// SavingsOverhead separates repository-owned metadata from categories that
// are outside the in-tree repository measurement boundary. It prevents an
// unavailable index/model/temp measurement from being presented as zero.
type SavingsOverhead struct {
	RepositoryMetadata SavingsOverheadCategory `json:"repository_metadata"`
	RecoveryRecords    SavingsOverheadCategory `json:"recovery_records"`
	Index              SavingsOverheadCategory `json:"index"`
	Model              SavingsOverheadCategory `json:"model"`
	Temporary          SavingsOverheadCategory `json:"temporary"`
}

func (overhead SavingsOverhead) measuredBytes() int64 {
	var total int64
	for _, category := range []SavingsOverheadCategory{
		overhead.RepositoryMetadata,
		overhead.RecoveryRecords,
		overhead.Index,
		overhead.Model,
		overhead.Temporary,
	} {
		if category.Status == SavingsCategoryMeasured {
			total += category.Bytes
		}
	}
	return total
}

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
	// RepositoryGrowthBytes is the measured repository footprint attributable
	// to this repository root: payload objects plus measured repository-owned
	// overhead. It is not a filesystem block-allocation or time-series delta.
	RepositoryGrowthBytes int64
	// OverheadBytes is the repository metadata and catalog bytes measurable on
	// a local filesystem: the profile marker, the repository identity, and the
	// portable recovery-record tree. The in-tree profiles keep no separately
	// stored index or model on the repository path, so index/model overhead is
	// not fabricated; it is simply not measurable for these profiles.
	OverheadBytes int64
	// Overhead provides the typed category/status view behind OverheadBytes.
	// Categories marked UNMEASURED are excluded from both OverheadBytes and
	// RepositoryGrowthBytes; they must not be interpreted as zero bytes.
	Overhead SavingsOverhead
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
	expectedLengths := make(map[string]int64, len(placements))
	for _, receipt := range placements {
		if _, err := parseContentID(receipt.ContentID); err != nil {
			return SavingsReport{}, fmt.Errorf("invalid placement receipt: %w", err)
		}
		if receipt.Bytes < 0 {
			return SavingsReport{}, fmt.Errorf("invalid placement receipt %q with negative logical bytes %d", receipt.ContentID, receipt.Bytes)
		}
		if previous, ok := expectedLengths[receipt.ContentID]; ok && previous != receipt.Bytes {
			return SavingsReport{}, fmt.Errorf("placement receipt %q has conflicting logical lengths %d and %d", receipt.ContentID, previous, receipt.Bytes)
		}
		expectedLengths[receipt.ContentID] = receipt.Bytes
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
	observed := make(map[string]struct{}, len(expectedLengths))
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
		if !entry.Type().IsRegular() {
			return fmt.Errorf("savings measurement found non-regular repository object %q", path)
		}
		if len(name) != 64 {
			return fmt.Errorf("savings measurement found repository object with invalid name %q", name)
		}
		if _, err := parseContentID(AlgorithmSHA256 + ":" + name); err != nil {
			return err
		}
		if prefix := filepath.Base(filepath.Dir(path)); prefix != name[:hexPrefixLen] {
			return fmt.Errorf("savings measurement found repository object %q in prefix directory %q", name, prefix)
		}
		contentID := AlgorithmSHA256 + ":" + name
		if _, ok := expectedLengths[contentID]; !ok {
			return fmt.Errorf("savings measurement found untracked repository object %s", contentID)
		}
		observed[contentID] = struct{}{}
		physical, err := fileSize(path)
		if err != nil {
			return err
		}
		if err := driver.Verify(ctx, contentID); err != nil {
			return fmt.Errorf("savings measurement verify %s: %w", name, err)
		}
		logical, err := readbackLogicalObject(ctx, driver, contentID)
		if err != nil {
			return err
		}
		if expected, ok := expectedLengths[contentID]; ok && expected != logical {
			return fmt.Errorf("savings measurement logical length mismatch for %s: read back %d want %d", contentID, logical, expected)
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
	for contentID := range expectedLengths {
		if _, ok := observed[contentID]; !ok {
			return SavingsReport{}, fmt.Errorf("savings measurement receipt %s has no repository object", contentID)
		}
	}

	if storedObjects > 0 && len(placements) == 0 {
		return SavingsReport{}, errors.New("cannot measure savings honestly without the placement receipts observed during ingest")
	}

	// Overhead is measured from the repository layout. Index/model overhead is
	// not measurable for the in-tree profiles and is deliberately not fabricated
	// as zero.
	report.Overhead, err = measureOverhead(dir)
	if err != nil {
		return SavingsReport{}, fmt.Errorf("measure repository overhead: %w", err)
	}
	report.OverheadBytes = report.Overhead.measuredBytes()
	report.RepositoryGrowthBytes = report.PhysicalStoredBytes + report.OverheadBytes
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
	case savingsRootProvider:
		return d.savingsRoot(), true
	default:
		return nil, false
	}
}

// savingsRootProvider is an internal seam for directory-backed qualification
// wrappers. It keeps savings measurement limited to a host-readable layout
// while allowing tests to model a dishonest backend implementation.
type savingsRootProvider interface {
	savingsRoot() *Dir
}

// readbackLogicalObject opens the object through the driver and independently
// hashes the logical stream. Backend Verify is an additional signal, never the
// authority for a savings claim.
func readbackLogicalObject(ctx context.Context, driver Driver, contentID string) (int64, error) {
	body, err := driver.Open(ctx, contentID)
	if err != nil {
		return 0, err
	}
	hash := sha256.New()
	logical, copyErr := io.Copy(hash, body)
	closeErr := body.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	got := AlgorithmSHA256 + ":" + hex.EncodeToString(hash.Sum(nil))
	if got != contentID {
		return 0, fmt.Errorf("%w: readback got %s want %s", ErrDigestMismatch, got, contentID)
	}
	return logical, nil
}

// measureOverhead returns the bytes the repository layout itself consumes on
// disk beyond the payload objects: the profile marker, the repository identity,
// and the portable recovery-record tree. It never invents an index/model
// overhead figure; the in-tree profiles keep no separate index on the
// repository path. Blob and temporary placement state are payload or transient
// and are not counted here.
func measureOverhead(dir *Dir) (SavingsOverhead, error) {
	overhead := SavingsOverhead{
		RepositoryMetadata: SavingsOverheadCategory{Status: SavingsCategoryMeasured},
		RecoveryRecords:    SavingsOverheadCategory{Status: SavingsCategoryMeasured},
		Index:              SavingsOverheadCategory{Status: SavingsCategoryUnmeasured},
		Model:              SavingsOverheadCategory{Status: SavingsCategoryUnmeasured},
		Temporary:          SavingsOverheadCategory{Status: SavingsCategoryUnmeasured},
	}
	err := filepath.WalkDir(dir.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir.root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(relative, string(filepath.Separator))
		switch {
		case len(parts) == 1 && entry.Name() == repositoryProfileFile:
			size, err := entrySize(entry, path)
			if err != nil {
				return err
			}
			overhead.RepositoryMetadata.Bytes += size
		case len(parts) == 1 && entry.Name() == repositoryIdentityFile:
			size, err := entrySize(entry, path)
			if err != nil {
				return err
			}
			overhead.RepositoryMetadata.Bytes += size
		case len(parts) >= 2 && parts[0] == recoveryDirName:
			size, err := entrySize(entry, path)
			if err != nil {
				return err
			}
			overhead.RecoveryRecords.Bytes += size
		case len(parts) >= 3 && parts[0] == blobDirName && parts[1] == AlgorithmSHA256:
			// Payload bytes are measured and verified by the object walk.
			// Any other blob namespace entry is rejected there.
			return nil
		default:
			return fmt.Errorf("unaccounted repository file %q", path)
		}
		return nil
	})
	if err != nil {
		return SavingsOverhead{}, err
	}
	return overhead, nil
}

// entrySize returns the size of a directory entry via its DirEntry info,
// falling back to a stat when the entry info is incomplete.
func entrySize(entry fs.DirEntry, path string) (int64, error) {
	info, err := entry.Info()
	if err == nil {
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("repository overhead entry is not a regular file: %s", path)
		}
		return info.Size(), nil
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return 0, fmt.Errorf("stat repository overhead entry %q: %w (directory entry info: %v)", path, statErr, err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("repository overhead entry is not a regular file: %s", path)
	}
	return info.Size(), nil
}
