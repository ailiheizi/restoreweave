package exact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const (
	VerifyAuthenticatedMetadata = "authenticated-metadata"
	VerifySampledContent        = "sampled-content"
	VerifyFullBytes             = "full-bytes"
	VerifyRestoreDrill          = "restore-drill"
	VerifyCleanRecovery         = "clean-recovery"
)

// VerifyMode runs one declared verification level against the portable
// snapshot. Sampled work is never reported as full-bytes. Only a completed
// restore with post-restore digest comparison may set RestoreVerified.
func (s *Service) VerifyMode(ctx context.Context, snapshotRef, mode, destination string) (VerifyResult, error) {
	var result VerifyResult
	if err := s.requireRepository(); err != nil {
		return result, err
	}
	normalized, err := normalizeVerifyMode(mode)
	if err != nil {
		return result, err
	}
	if normalized == VerifyRestoreDrill && strings.TrimSpace(destination) == "" {
		return result, fmt.Errorf("%w: restore-drill requires a destination", ErrBlocked)
	}

	manifest, err := s.loadManifest(ctx, snapshotRef)
	if err != nil {
		return result, err
	}
	if err := authenticateManifest(manifest); err != nil {
		return result, err
	}

	files := fileEntries(manifest)
	result.SnapshotRef = snapshotRef
	result.Mode = normalized
	result.AcceptedLevel = normalized
	result.Entries = len(manifest.Entries)
	result.Files = len(files)
	result.Bytes = fileBytes(files)
	result.CatalogUsed = false

	switch normalized {
	case VerifyAuthenticatedMetadata:
		err = s.verifyEntries(ctx, &result, files, false)
	case VerifySampledContent:
		if err = requireLocallyRestorable(manifest); err != nil {
			return result, err
		}
		err = s.verifyEntries(ctx, &result, sampleFileEntries(snapshotRef, files), true)
	case VerifyFullBytes, VerifyCleanRecovery:
		if err = requireLocallyRestorable(manifest); err != nil {
			return result, err
		}
		err = s.verifyEntries(ctx, &result, files, true)
	case VerifyRestoreDrill:
		restored, restoreErr := s.Restore(ctx, snapshotRef, destination)
		if restoreErr != nil {
			return result, restoreErr
		}
		result.AttemptedFiles = restored.Files
		result.AttemptedBytes = restored.Bytes
		result.PassedFiles = restored.Files
		result.PassedBytes = restored.Bytes
		result.OK = true
		result.RestoreVerified = true
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.OK = true
	return result, nil
}

func normalizeVerifyMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", VerifyFullBytes, "full_bytes":
		return VerifyFullBytes, nil
	case VerifyAuthenticatedMetadata, "authenticated_metadata":
		return VerifyAuthenticatedMetadata, nil
	case VerifySampledContent, "sampled_content":
		return VerifySampledContent, nil
	case VerifyRestoreDrill, "restore_drill":
		return VerifyRestoreDrill, nil
	case VerifyCleanRecovery, "clean_recovery":
		return VerifyCleanRecovery, nil
	default:
		return "", fmt.Errorf("%w: unknown verify mode %q", ErrBlocked, mode)
	}
}

func authenticateManifest(manifest Manifest) error {
	if manifest.Schema != "" && manifest.Schema != SnapshotSchemaV1 && manifest.Schema != SnapshotSchemaV2 {
		return fmt.Errorf("%w: unsupported snapshot schema %q", ErrBlocked, manifest.Schema)
	}
	if err := validateManifestFacts(manifest); err != nil {
		return fmt.Errorf("%w: invalid portable facts: %v", ErrBlocked, err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		return err
	}
	if manifest.ManifestDigest != "" && manifest.ManifestDigest != digest {
		return fmt.Errorf("snapshot manifest digest mismatch: got %s want %s", digest, manifest.ManifestDigest)
	}
	return nil
}

func (s *Service) verifyEntries(ctx context.Context, result *VerifyResult, entries []ManifestEntry, hashBytes bool) error {
	for _, entry := range entries {
		size := entrySize(entry)
		result.AttemptedFiles++
		result.AttemptedBytes += size
		if hashBytes {
			if err := s.Repo.Verify(ctx, entry.ContentID); err != nil {
				return fmt.Errorf("verify %s: %w", entry.RelativePath, err)
			}
		} else {
			if strings.TrimSpace(entry.ContentID) == "" || strings.TrimSpace(entry.Protection.Outcome) == "" {
				return fmt.Errorf("authenticate %s: content identity or protection outcome is missing", entry.RelativePath)
			}
		}
		result.PassedFiles++
		result.PassedBytes += size
	}
	return nil
}

func fileEntries(manifest Manifest) []ManifestEntry {
	var files []ManifestEntry
	for _, entry := range manifest.Entries {
		if sqlite.NamespaceEntryType(entry.EntryType) != sqlite.EntryFile {
			continue
		}
		files = append(files, entry)
	}
	return files
}

func fileBytes(files []ManifestEntry) int64 {
	var total int64
	for _, entry := range files {
		total += entrySize(entry)
	}
	return total
}

func entrySize(entry ManifestEntry) int64 {
	if entry.LogicalSize != nil {
		return *entry.LogicalSize
	}
	return 0
}

func sampleFileEntries(snapshotRef string, files []ManifestEntry) []ManifestEntry {
	if len(files) == 0 {
		return nil
	}
	sorted := append([]ManifestEntry(nil), files...)
	sort.Slice(sorted, func(i, j int) bool {
		return sampleKey(snapshotRef, sorted[i]) < sampleKey(snapshotRef, sorted[j])
	})
	limit := (len(sorted) + 9) / 10
	if limit < 1 {
		limit = 1
	}
	return sorted[:limit]
}

func sampleKey(snapshotRef string, entry ManifestEntry) string {
	sum := sha256.Sum256([]byte(snapshotRef + "\n" + entry.RelativePath + "\n" + entry.ContentID))
	return hex.EncodeToString(sum[:])
}
