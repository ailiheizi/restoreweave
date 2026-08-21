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

// PreflightRestore reads the portable snapshot and reports file/byte totals
// without creating a destination or writing CAS bytes.
func (s *Service) PreflightRestore(ctx context.Context, snapshotRef string) (RestoreResult, error) {
	var result RestoreResult
	if err := s.requireRepository(); err != nil {
		return result, err
	}
	if strings.TrimSpace(snapshotRef) == "" {
		return result, fmt.Errorf("%w: snapshot ref is required", ErrBlocked)
	}
	manifest, err := s.loadManifest(ctx, snapshotRef)
	if err != nil {
		return result, err
	}
	if err := requireLocallyRestorable(manifest); err != nil {
		return result, err
	}
	if _, err := prepareRestoreEntries(manifest); err != nil {
		return result, err
	}
	files, bytes := manifestFileTotals(manifest)
	return RestoreResult{SnapshotRef: snapshotRef, Files: files, Bytes: bytes}, nil
}

// Restore reconstructs a published snapshot into destination without reading
// the operational catalog. The repository snapshot JSON and CAS blobs are
// the recovery authority.
func (s *Service) Restore(ctx context.Context, snapshotRef, destination string) (RestoreResult, error) {
	var result RestoreResult
	if err := s.requireRepository(); err != nil {
		return result, err
	}
	if strings.TrimSpace(destination) == "" {
		return result, fmt.Errorf("%w: restore destination is required", ErrBlocked)
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	manifest, err := s.loadManifest(ctx, snapshotRef)
	if err != nil {
		return result, err
	}
	if err := requireLocallyRestorable(manifest); err != nil {
		return result, err
	}
	entries, err := prepareRestoreEntries(manifest)
	if err != nil {
		return result, err
	}
	if err := prepareRestoreDir(absolute); err != nil {
		return result, err
	}
	var files int
	var bytes int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		rel := entry.relativePath
		if rel == "." || rel == "" {
			continue
		}
		dest := filepath.Join(absolute, filepath.FromSlash(rel))
		switch sqlite.NamespaceEntryType(entry.ManifestEntry.EntryType) {
		case sqlite.EntryDirectory:
			if err := os.MkdirAll(dest, restoreMode(entry.ManifestEntry, 0o755)); err != nil {
				return result, err
			}
		case sqlite.EntrySymlink:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return result, err
			}
			if err := os.Symlink(string(entry.ManifestEntry.SymlinkTarget), dest); err != nil {
				return result, err
			}
		case sqlite.EntryFile:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return result, err
			}
			n, err := s.restoreFile(ctx, dest, entry.ManifestEntry)
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

// restoreEntry carries the path after raw/display path fallback and validation.
// Keeping this resolved path with the entry prevents sorting and writing from
// applying different interpretations of a manifest path.
type restoreEntry struct {
	ManifestEntry
	relativePath string
}

// prepareRestoreEntries validates the complete namespace before writing any
// destination entries. In particular, a symlink (or file) cannot be used as
// a parent of a later entry, and duplicate paths cannot change type based on
// manifest ordering.
func prepareRestoreEntries(manifest Manifest) ([]restoreEntry, error) {
	entries := make([]restoreEntry, 0, len(manifest.Entries))
	byPath := make(map[string]sqlite.NamespaceEntryType, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		rel, err := restoreRelative(entry)
		if err != nil {
			return nil, err
		}
		key := rel
		entryType := sqlite.NamespaceEntryType(entry.EntryType)
		if previous, ok := byPath[key]; ok {
			return nil, fmt.Errorf("%w: snapshot has conflicting entries at %q (%s and %s)",
				ErrBlocked, rel, previous, entryType)
		}
		byPath[key] = entryType
		entries = append(entries, restoreEntry{ManifestEntry: entry, relativePath: rel})
	}

	for _, entry := range entries {
		if entry.relativePath == "." || entry.relativePath == "" {
			continue
		}
		parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(entry.relativePath)))
		for parent != "." && parent != "" {
			if parentType, ok := byPath[parent]; ok && parentType != sqlite.EntryDirectory {
				return nil, fmt.Errorf("%w: snapshot entry %q has non-directory parent %q (%s)",
					ErrBlocked, entry.relativePath, parent, parentType)
			}
			parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(parent)))
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i].relativePath, entries[j].relativePath
		leftDepth, rightDepth := pathDepth(left), pathDepth(right)
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return left < right
	})
	return entries, nil
}

func (s *Service) restoreFile(ctx context.Context, dest string, entry ManifestEntry) (int64, error) {
	body, err := s.Repo.Open(ctx, entry.ContentID)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", entry.RelativePath, err)
	}
	defer body.Close()
	file, err := createRestoreFile(dest, restoreMode(entry, 0o644))
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
	return s.VerifyMode(ctx, snapshotRef, VerifyFullBytes, "")
}

// ListSnapshots returns portable snapshots from the repository, not SQLite.
func (s *Service) ListSnapshots(ctx context.Context) ([]Manifest, error) {
	if err := s.requireRepository(); err != nil {
		return nil, err
	}
	if s.signedPublicationEnabled() {
		publications, err := s.committedPublications(ctx)
		if err != nil {
			return nil, err
		}
		manifests := make([]Manifest, 0, len(publications))
		for _, publication := range publications {
			manifests = append(manifests, publication.Manifest)
		}
		return manifests, nil
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
	// RawPath is the recovery identity. RelativePath is only the safe display
	// fallback used by older manifests that did not retain raw bytes.
	rel := string(entry.RawPath)
	if len(entry.RawPath) == 0 {
		rel = entry.RelativePath
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

func requireLocallyRestorable(manifest Manifest) error {
	var unavailable []string
	for _, entry := range manifest.Entries {
		if sqlite.NamespaceEntryType(entry.EntryType) != sqlite.EntryFile {
			continue
		}
		// Empty mode is the compatibility shape for snapshots created before
		// protection records were embedded. Their ContentID still names CAS.
		if entry.Protection.Mode == "" {
			continue
		}
		outcome := sqlite.ProtectionOutcome(entry.Protection.Outcome)
		if entry.Protection.LocalRepresentationID == "" ||
			(outcome != sqlite.ProtectionExactProtected && outcome != sqlite.ProtectionExactFallback) {
			unavailable = append(unavailable, entry.RelativePath)
		}
	}
	if len(unavailable) == 0 {
		return nil
	}
	preview := strings.Join(unavailable, ", ")
	if len(unavailable) > 3 {
		preview = strings.Join(unavailable[:3], ", ") + fmt.Sprintf(" and %d more", len(unavailable)-3)
	}
	return fmt.Errorf("%w: snapshot has %d file(s) without local exact recovery: %s", ErrBlocked, len(unavailable), preview)
}
