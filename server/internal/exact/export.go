package exact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const RecoveryExportBundleSchemaV1 = "org.restoreweave.recovery-export.v1"

type recoveryExportBundle struct {
	Schema                    string                  `json:"schema"`
	SnapshotRef               string                  `json:"snapshot_ref"`
	PublicationCommitDigest   string                  `json:"publication_commit_digest"`
	PublicationCommit         PublicationCommitRecord `json:"publication_commit"`
	PreparedClosureDigest     string                  `json:"prepared_closure_digest"`
	PreparedClosure           PreparedClosureEnvelope `json:"prepared_closure"`
	RequiredTrustAnchorKeyID  string                  `json:"required_trust_anchor_key_id"`
	RequiredTrustAnchorDigest string                  `json:"required_trust_anchor_digest"`
}

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

// ExportRecoveryArtifact selects the current authenticated v2 reference for a
// signed publication and retains the legacy snapshot export only for an
// explicitly unsigned development service.
func (s *Service) ExportRecoveryArtifact(ctx context.Context, snapshotRef, destination string) (ExportResult, error) {
	if s.signedPublicationEnabled() {
		return s.ExportRecoveryReference(ctx, snapshotRef, destination)
	}
	return s.ExportRecovery(ctx, snapshotRef, destination)
}

// ExportRecovery copies the portable snapshot JSON to destination. It never
// overwrites an existing file and never writes credentials or keys.
func (s *Service) ExportRecovery(ctx context.Context, snapshotRef, destination string) (ExportResult, error) {
	var result ExportResult
	if err := s.requireRepository(); err != nil {
		return result, err
	}
	if strings.TrimSpace(destination) == "" {
		return result, errors.New("export destination is required")
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	manifest, err := s.loadManifest(ctx, snapshotRef)
	if err != nil {
		return result, err
	}
	var payload []byte
	schema := manifest.Schema
	if s.signedPublicationEnabled() {
		publications, err := s.committedPublications(ctx)
		if err != nil {
			return result, err
		}
		var selected *committedPublication
		for i := range publications {
			if publications[i].Manifest.SnapshotRef == snapshotRef {
				copy := publications[i]
				selected = &copy
				break
			}
		}
		if selected == nil || s.TrustAnchor == nil {
			return result, fmt.Errorf("committed snapshot %s is unavailable", snapshotRef)
		}
		anchorDigest, err := DigestCanonicalJSON(*s.TrustAnchor)
		if err != nil {
			return result, err
		}
		payload, err = json.MarshalIndent(recoveryExportBundle{
			Schema: RecoveryExportBundleSchemaV1, SnapshotRef: snapshotRef,
			PublicationCommitDigest: selected.CommitDigest, PublicationCommit: selected.Commit,
			PreparedClosureDigest: selected.Commit.PreparedObjectDigest, PreparedClosure: selected.Prepared,
			RequiredTrustAnchorKeyID: s.TrustAnchor.KeyID, RequiredTrustAnchorDigest: anchorDigest,
		}, "", "  ")
		if err != nil {
			return result, err
		}
		schema = RecoveryExportBundleSchemaV1
	} else {
		payload, err = os.ReadFile(snapshotPath(s.Repo.Root(), snapshotRef))
		if err != nil {
			return result, fmt.Errorf("read snapshot %s: %w", snapshotRef, err)
		}
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
		Schema:              schema,
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
