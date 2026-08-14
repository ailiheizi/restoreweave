package exact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// ExportResult is returned after writing an independently retainable snapshot.
type ExportResult struct {
	SnapshotRef         string
	Schema              string
	ManifestDigest      string
	ArtifactPath        string
	Length              int64
	Files               int
	Bytes               int64
	IndependentlyStored bool
}

// ExportRecovery copies the portable snapshot JSON to destination. It never
// overwrites an existing file and never writes credentials or keys.
func (s *Service) ExportRecovery(_ context.Context, snapshotRef, destination string) (ExportResult, error) {
	var result ExportResult
	if err := s.require(); err != nil {
		return result, err
	}
	if strings.TrimSpace(destination) == "" {
		return result, errors.New("export destination is required")
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	manifest, err := readManifest(s.Repo.Root(), snapshotRef)
	if err != nil {
		return result, err
	}
	payload, err := os.ReadFile(snapshotPath(s.Repo.Root(), snapshotRef))
	if err != nil {
		return result, fmt.Errorf("read snapshot %s: %w", snapshotRef, err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return result, err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return result, fmt.Errorf("%w: export destination already exists", ErrBlocked)
		}
		return result, err
	}
	written, writeErr := file.Write(payload)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(absolute)
		return result, writeErr
	}
	if syncErr != nil {
		_ = os.Remove(absolute)
		return result, syncErr
	}
	if closeErr != nil {
		_ = os.Remove(absolute)
		return result, closeErr
	}
	files, bytes := manifestFileTotals(manifest)
	return ExportResult{
		SnapshotRef:         manifest.SnapshotRef,
		Schema:              manifest.Schema,
		ManifestDigest:      manifest.ManifestDigest,
		ArtifactPath:        absolute,
		Length:              int64(written),
		Files:               files,
		Bytes:               bytes,
		IndependentlyStored: true,
	}, nil
}

func manifestFileTotals(manifest Manifest) (int, int64) {
	var files int
	var bytes int64
	for _, entry := range manifest.Entries {
		if sqlite.NamespaceEntryType(entry.EntryType) != sqlite.EntryFile {
			continue
		}
		files++
		if entry.LogicalSize != nil {
			bytes += *entry.LogicalSize
		}
	}
	return files, bytes
}
