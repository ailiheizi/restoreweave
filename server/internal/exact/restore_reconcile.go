package exact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// ErrRestoreNotExecuted means that the planned destination is still absent or
// empty. It is the only reconcile result that permits a caller to run the
// restore writer. Any non-empty destination is treated as evidence and must
// validate completely before it can be claimed for a plan.
var ErrRestoreNotExecuted = errors.New("restore destination has not been executed")

// ReconcileRestorePlan verifies an already populated restore destination
// against the immutable snapshot bound by plan. It is read-only and never
// creates, removes, or changes anything below the destination.
//
// An absent or empty destination returns ErrRestoreNotExecuted. A non-empty
// destination is successful only when its complete directory tree is exactly
// the manifest tree, including entry types, file lengths and SHA-256 digests,
// and symlink targets.
func (s *Service) ReconcileRestorePlan(ctx context.Context, plan RestorePlan) (RestoreResult, error) {
	var result RestoreResult
	if err := s.requireRepository(); err != nil {
		return result, err
	}
	if strings.TrimSpace(plan.SnapshotRef) == "" || strings.TrimSpace(plan.ManifestDigest) == "" ||
		strings.TrimSpace(plan.Destination) == "" {
		return result, ErrInvalidRestorePlan
	}

	destination, err := filepath.Abs(plan.Destination)
	if err != nil {
		return result, err
	}
	if filepath.Clean(destination) != filepath.Clean(plan.Destination) {
		return result, fmt.Errorf("%w: destination changed", ErrRestorePlanStale)
	}
	manifest, err := s.loadManifest(ctx, plan.SnapshotRef)
	if err != nil {
		return result, err
	}
	if manifest.ManifestDigest != plan.ManifestDigest {
		return result, fmt.Errorf("%w: manifest digest changed", ErrRestorePlanStale)
	}
	if s.signedPublicationEnabled() {
		publication, err := s.committedPublicationForSnapshot(ctx, plan.SnapshotRef)
		if err != nil {
			return result, err
		}
		if plan.PublicationCommitDigest == "" || publication.CommitDigest != plan.PublicationCommitDigest {
			return result, fmt.Errorf("%w: publication commit changed", ErrRestorePlanStale)
		}
	}
	if err := requireLocallyRestorable(manifest); err != nil {
		return result, err
	}
	entries, err := prepareRestoreEntries(manifest)
	if err != nil {
		return result, err
	}

	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return result, ErrRestoreNotExecuted
	}
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, fmt.Errorf("%w: restore destination is not a directory", ErrBlocked)
	}
	directoryEntries, err := os.ReadDir(destination)
	if err != nil {
		return result, err
	}
	if len(directoryEntries) == 0 {
		return result, ErrRestoreNotExecuted
	}

	expected := make(map[string]restoreEntry, len(entries))
	for _, entry := range entries {
		if entry.relativePath == "." || entry.relativePath == "" {
			continue
		}
		expected[entry.relativePath] = entry
	}

	seen := make(map[string]struct{}, len(expected))
	walkErr := filepath.WalkDir(destination, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == destination {
			return nil
		}
		rel, err := filepath.Rel(destination, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		entry, ok := expected[rel]
		if !ok {
			return fmt.Errorf("%w: destination contains unexpected path %q", ErrBlocked, rel)
		}
		seen[rel] = struct{}{}
		if err := reconcileRestoreEntry(path, entry.ManifestEntry); err != nil {
			return err
		}
		return nil
	})
	if walkErr != nil {
		return result, walkErr
	}
	for rel := range expected {
		if _, ok := seen[rel]; !ok {
			return result, fmt.Errorf("%w: destination is missing %q", ErrBlocked, rel)
		}
	}
	files, bytes := manifestFileTotals(manifest)
	return RestoreResult{
		SnapshotRef: plan.SnapshotRef, Destination: destination,
		Files: files, Bytes: bytes,
	}, nil
}

func reconcileRestoreEntry(path string, expected ManifestEntry) error {
	expectedType := sqlite.NamespaceEntryType(expected.EntryType)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	switch expectedType {
	case sqlite.EntryDirectory:
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %q is not the expected directory", ErrBlocked, path)
		}
	case sqlite.EntrySymlink:
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%w: %q is not the expected symlink", ErrBlocked, path)
		}
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if target != string(expected.SymlinkTarget) {
			return fmt.Errorf("%w: symlink target mismatch for %q", ErrBlocked, path)
		}
	case sqlite.EntryFile:
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %q is not the expected regular file", ErrBlocked, path)
		}
		if expected.LogicalSize != nil && info.Size() != *expected.LogicalSize {
			return fmt.Errorf("%w: file length mismatch for %q: got %d want %d", ErrBlocked, path, info.Size(), *expected.LogicalSize)
		}
		got, size, err := hashRestoreFile(path)
		if err != nil {
			return err
		}
		if got != expected.ContentID {
			return fmt.Errorf("%w: file digest mismatch for %q: got %s want %s", ErrBlocked, path, got, expected.ContentID)
		}
		if expected.LogicalSize == nil && size != info.Size() {
			return fmt.Errorf("%w: file length changed while reading %q", ErrBlocked, path)
		}
	default:
		return fmt.Errorf("%w: unsupported manifest entry type %q", ErrBlocked, expected.EntryType)
	}
	return nil
}

func hashRestoreFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	count, err := io.Copy(io.MultiWriter(digest), file)
	if err != nil {
		return "", count, err
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), count, nil
}
