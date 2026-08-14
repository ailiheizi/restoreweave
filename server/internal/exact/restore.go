package exact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// Restore reconstructs a published snapshot into destination without reading
// the operational catalog. The repository snapshot JSON and CAS blobs are
// the recovery authority.
func (s *Service) Restore(ctx context.Context, snapshotRef, destination string) (RestoreResult, error) {
	var result RestoreResult
	if err := s.require(); err != nil {
		return result, err
	}
	if strings.TrimSpace(destination) == "" {
		return result, fmt.Errorf("%w: restore destination is required", ErrBlocked)
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	if err := prepareRestoreDir(absolute); err != nil {
		return result, err
	}
	manifest, err := readManifest(s.Repo.Root(), snapshotRef)
	if err != nil {
		return result, err
	}
	entries := append([]ManifestEntry(nil), manifest.Entries...)
	sort.SliceStable(entries, func(i, j int) bool {
		return pathDepth(entries[i].RelativePath) < pathDepth(entries[j].RelativePath)
	})
	var files int
	var bytes int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		rel, err := restoreRelative(entry)
		if err != nil {
			return result, err
		}
		if rel == "." || rel == "" {
			continue
		}
		dest := filepath.Join(absolute, filepath.FromSlash(rel))
		switch sqlite.NamespaceEntryType(entry.EntryType) {
		case sqlite.EntryDirectory:
			if err := os.MkdirAll(dest, restoreMode(entry, 0o755)); err != nil {
				return result, err
			}
		case sqlite.EntrySymlink:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return result, err
			}
			if err := os.Symlink(string(entry.SymlinkTarget), dest); err != nil {
				return result, err
			}
		case sqlite.EntryFile:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return result, err
			}
			n, err := s.restoreFile(ctx, dest, entry)
			if err != nil {
				return result, err
			}
			files++
			bytes += n
		default:
			continue
		}
	}
	result = RestoreResult{SnapshotRef: snapshotRef, Destination: absolute, Files: files, Bytes: bytes}
	return result, nil
}

func (s *Service) restoreFile(ctx context.Context, dest string, entry ManifestEntry) (int64, error) {
	body, err := s.Repo.Open(ctx, entry.ContentID)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", entry.RelativePath, err)
	}
	defer body.Close()
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, restoreMode(entry, 0o644))
	if err != nil {
		return 0, err
	}
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest), body)
	closeErr := file.Close()
	if err != nil {
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}
	got := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if got != entry.ContentID {
		return 0, fmt.Errorf("restored %s digest mismatch: got %s", entry.RelativePath, got)
	}
	return written, nil
}

// Verify independently hashes every file blob named by the snapshot.
func (s *Service) Verify(ctx context.Context, snapshotRef string) (VerifyResult, error) {
	var result VerifyResult
	if err := s.require(); err != nil {
		return result, err
	}
	manifest, err := readManifest(s.Repo.Root(), snapshotRef)
	if err != nil {
		return result, err
	}
	result.SnapshotRef = snapshotRef
	result.Entries = len(manifest.Entries)
	for _, entry := range manifest.Entries {
		if sqlite.NamespaceEntryType(entry.EntryType) != sqlite.EntryFile || entry.ContentID == "" {
			continue
		}
		if err := s.Repo.Verify(ctx, entry.ContentID); err != nil {
			return result, fmt.Errorf("verify %s: %w", entry.RelativePath, err)
		}
		result.Files++
		if entry.LogicalSize != nil {
			result.Bytes += *entry.LogicalSize
		}
	}
	return result, nil
}

// ListSnapshots returns portable snapshots from the repository, not SQLite.
func (s *Service) ListSnapshots(_ context.Context) ([]Manifest, error) {
	if err := s.require(); err != nil {
		return nil, err
	}
	return listManifests(s.Repo.Root())
}

func prepareRestoreDir(path string) error {
	info, err := os.Lstat(path)
	if errorsIsNotExist(err) {
		return os.MkdirAll(path, 0o755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: restore destination exists and is not a directory", ErrBlocked)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: restore destination is not empty", ErrBlocked)
	}
	return nil
}

func restoreRelative(entry ManifestEntry) (string, error) {
	rel := entry.RelativePath
	if rel == "" {
		rel = string(entry.RawPath)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return ".", nil
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%w: snapshot path %q is absolute", ErrBlocked, rel)
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("%w: snapshot path %q is unsafe", ErrBlocked, rel)
		}
	}
	return rel, nil
}

func restoreMode(entry ManifestEntry, fallback os.FileMode) os.FileMode {
	if entry.Mode == 0 {
		return fallback
	}
	return os.FileMode(entry.Mode) & 0o777
}

func pathDepth(rel string) int {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" || rel == "." {
		return 0
	}
	return strings.Count(rel, "/") + 1
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
