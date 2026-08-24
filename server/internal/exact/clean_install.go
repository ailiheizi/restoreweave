package exact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// RecoveryImportResult is the admission summary returned after an
// independently authenticated recovery bundle is verified against a supplied
// trust anchor. It mirrors the portable recovery facts an operator needs; it
// is not a substitute for the repository records themselves.
type RecoveryImportResult struct {
	Schema                string
	SnapshotRef           string
	ManifestDigest        string
	CommitDigest          string
	PreparedClosureDigest string
	Generation            uint64
	TrustAnchorDigest     string
	FactHealth            string
	Files                 int
	Bytes                 int64
	CatalogCreated        bool
}

// ImportRecoveryArtifact admits either the current typed v2 recovery
// reference or the legacy signed v1 bundle. New public exports use v2; v1 is
// retained as an explicit migration input so an existing independently stored
// recovery artifact does not become unreadable after an upgrade.
func (s *Service) ImportRecoveryArtifact(ctx context.Context, artifactPath, trustAnchorPath, publicationDomain string) (RecoveryImportResult, error) {
	var result RecoveryImportResult
	if err := s.requireRepository(); err != nil {
		return result, err
	}
	anchor, err := s.loadRecoveryImportAnchor(trustAnchorPath, publicationDomain)
	if err != nil {
		return result, err
	}
	payload, err := readRecoveryArtifact(artifactPath)
	if err != nil {
		return result, err
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(payload, &header); err != nil {
		return result, fmt.Errorf("decode recovery artifact header: %w", err)
	}
	switch header.Schema {
	case RecoveryReferenceSchemaV2:
		reference, err := DecodeRecoveryReference(payload)
		if err != nil {
			return result, err
		}
		return s.importRecoveryReference(ctx, anchor, reference)
	case RecoveryExportBundleSchemaV1:
		var bundle recoveryExportBundle
		if err := decodeStrictRecord(payload, &bundle); err != nil {
			return result, fmt.Errorf("decode recovery bundle: %w", err)
		}
		return s.importRecoveryBundle(ctx, anchor, bundle)
	default:
		return result, fmt.Errorf("unsupported recovery artifact schema %q", header.Schema)
	}
}

// MigrateRecoveryArtifact converts the legacy v1 recovery export into the
// independently retainable v2 reference. Migration is a read/validate/write
// operation: it never deletes or overwrites the source artifact and never
// publishes a second snapshot. The repository's authenticated commit and
// portable fact closure remain authoritative for the successor reference.
func (s *Service) MigrateRecoveryArtifact(ctx context.Context, artifactPath, trustAnchorPath, destination, publicationDomain string) (ExportResult, error) {
	var result ExportResult
	if strings.TrimSpace(destination) == "" {
		return result, errors.New("migration destination is required")
	}
	anchor, err := s.loadRecoveryImportAnchor(trustAnchorPath, publicationDomain)
	if err != nil {
		return result, err
	}
	payload, err := readRecoveryArtifact(artifactPath)
	if err != nil {
		return result, err
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(payload, &header); err != nil {
		return result, fmt.Errorf("decode recovery artifact header: %w", err)
	}
	var reference RecoveryReference
	switch header.Schema {
	case RecoveryExportBundleSchemaV1:
		var bundle recoveryExportBundle
		if err := decodeStrictRecord(payload, &bundle); err != nil {
			return result, fmt.Errorf("decode recovery bundle: %w", err)
		}
		if _, _, _, err := s.verifyImportBundle(ctx, anchor, bundle); err != nil {
			return result, fmt.Errorf("validate legacy recovery bundle for migration: %w", err)
		}
		// BuildRecoveryReference reads the authenticated repository lineage and
		// current complete-state child; it does not trust mutable v1 fields. Use
		// a catalog-free reader with the independently loaded anchor rather than
		// copying the caller's synchronization state.
		reader := &Service{
			Repo:                     s.Repo,
			TrustAnchor:              &anchor,
			PublicationDomain:        s.PublicationDomain,
			RequireSignedPublication: true,
		}
		if reader.PublicationDomain == "" {
			reader.PublicationDomain = anchor.PublicationDomain
		}
		reference, err = reader.BuildRecoveryReference(ctx, bundle.SnapshotRef)
		if err != nil {
			return result, fmt.Errorf("build v2 recovery successor: %w", err)
		}
		if reference.PublicationCommitDigest != bundle.PublicationCommitDigest ||
			reference.PreparedClosure.Manifest.ManifestDigest != bundle.PreparedClosure.Manifest.ManifestDigest {
			return result, errors.New("migration changed the authenticated publication identity")
		}
	case RecoveryReferenceSchemaV2:
		reference, err = DecodeRecoveryReference(payload)
		if err != nil {
			return result, err
		}
		if err := reference.ValidateAgainstRepository(ctx, s.Repo, anchor); err != nil {
			return result, fmt.Errorf("validate v2 recovery reference for migration: %w", err)
		}
	default:
		return result, fmt.Errorf("unsupported recovery artifact schema %q", header.Schema)
	}
	// Revalidate the exact successor against the supplied repository and
	// independent anchor before writing any bytes to the destination.
	if err := reference.ValidateAgainstRepository(ctx, s.Repo, anchor); err != nil {
		return result, fmt.Errorf("validate migrated recovery reference: %w", err)
	}
	encoded, err := MarshalRecoveryReference(reference)
	if err != nil {
		return result, err
	}
	path, err := writeNewRecoveryFile(destination, encoded)
	if err != nil {
		return result, err
	}
	files, bytes := manifestFileTotals(reference.PreparedClosure.Manifest)
	return ExportResult{
		SnapshotRef: reference.SnapshotRef, Schema: RecoveryReferenceSchemaV2,
		ManifestDigest: reference.PreparedClosure.Manifest.ManifestDigest,
		ArtifactPath:   path, Length: int64(len(encoded)), Files: files, Bytes: bytes,
		IndependentlyStored: true,
	}, nil
}

// ImportRecoveryBundle admits a portable recovery export produced by
// ExportRecovery. The bundle carries the signed PUBLICATION_COMMIT and
// PREPARED_CLOSURE records; the trust anchor is loaded independently from
// trustAnchorPath and is the verification root. The operation fails closed on
// a corrupt anchor, a tampered bundle, a domain mismatch, or a bundle whose
// lineage contradicts the repository it is being admitted into.
//
// The caller chooses the repository profile. A clean-install reader uses
// repository.OpenProfileReadOnly and a Service with no Store; in that path
// CatalogCreated stays false and the Service remains catalog-free. When a
// Store is present the import optionally reconciles the rebuildable SQLite
// publication projection without weakening the repository authority.
func (s *Service) ImportRecoveryBundle(ctx context.Context, artifactPath, trustAnchorPath, publicationDomain string) (RecoveryImportResult, error) {
	var result RecoveryImportResult
	if err := s.requireRepository(); err != nil {
		return result, err
	}
	if strings.TrimSpace(artifactPath) == "" {
		return result, errors.New("recovery bundle and trust anchor paths are required")
	}
	anchor, err := s.loadRecoveryImportAnchor(trustAnchorPath, publicationDomain)
	if err != nil {
		return result, err
	}
	bundle, err := readRecoveryBundle(artifactPath)
	if err != nil {
		return result, err
	}
	return s.importRecoveryBundle(ctx, anchor, bundle)
}

func (s *Service) loadRecoveryImportAnchor(trustAnchorPath, publicationDomain string) (TrustAnchor, error) {
	if strings.TrimSpace(trustAnchorPath) == "" {
		return TrustAnchor{}, errors.New("trust anchor path is required")
	}
	anchor, err := LoadTrustAnchor(trustAnchorPath)
	if err != nil {
		return TrustAnchor{}, fmt.Errorf("load trust anchor: %w", err)
	}
	if err := anchor.validate(); err != nil {
		return TrustAnchor{}, err
	}
	if publicationDomain != "" && publicationDomain != anchor.PublicationDomain {
		return TrustAnchor{}, fmt.Errorf("publication domain %q differs from trust anchor domain %q", publicationDomain, anchor.PublicationDomain)
	}
	if s.PublicationDomain != "" && s.PublicationDomain != anchor.PublicationDomain {
		return TrustAnchor{}, fmt.Errorf("reader publication domain %q differs from trust anchor domain %q", s.PublicationDomain, anchor.PublicationDomain)
	}
	if s.TrustAnchor != nil {
		configuredDigest, digestErr := DigestCanonicalJSON(*s.TrustAnchor)
		if digestErr != nil {
			return TrustAnchor{}, digestErr
		}
		suppliedDigest, digestErr := DigestCanonicalJSON(anchor)
		if digestErr != nil {
			return TrustAnchor{}, digestErr
		}
		if configuredDigest != suppliedDigest {
			return TrustAnchor{}, fmt.Errorf("%w: supplied anchor differs from the reader trust anchor", ErrRecoveryTrustAnchor)
		}
	}
	return anchor, nil
}

func (s *Service) importRecoveryBundle(ctx context.Context, anchor TrustAnchor, bundle recoveryExportBundle) (RecoveryImportResult, error) {
	var result RecoveryImportResult
	commitDigest, preparedDigest, anchorDigest, err := s.verifyImportBundle(ctx, anchor, bundle)
	if err != nil {
		return result, err
	}
	files, bytes := manifestFileTotals(bundle.PreparedClosure.Manifest)
	result = RecoveryImportResult{
		Schema:                bundle.Schema,
		SnapshotRef:           bundle.SnapshotRef,
		ManifestDigest:        bundle.PreparedClosure.Manifest.ManifestDigest,
		CommitDigest:          commitDigest,
		PreparedClosureDigest: preparedDigest,
		Generation:            bundle.PublicationCommit.Generation,
		TrustAnchorDigest:     anchorDigest,
		FactHealth:            RecoveryFactHealthIncomplete,
		Files:                 files,
		Bytes:                 bytes,
	}
	if s.Store != nil {
		created, err := s.reconcileImportedPublication(ctx, bundle)
		if err != nil {
			return result, err
		}
		result.CatalogCreated = created
	}
	return result, nil
}

func (s *Service) importRecoveryReference(ctx context.Context, anchor TrustAnchor, reference RecoveryReference) (RecoveryImportResult, error) {
	var result RecoveryImportResult
	if err := reference.ValidateAgainstRepository(ctx, s.Repo, anchor); err != nil {
		return result, err
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return result, err
	}
	commits, err := listCommitMarkers(ctx, driver, anchor, anchor.PublicationDomain)
	if err != nil {
		return result, err
	}
	if err := verifyImportLineage(reference.PublicationCommit, reference.PublicationCommitDigest, commits); err != nil {
		return result, err
	}
	anchorDigest, err := DigestCanonicalJSON(anchor)
	if err != nil {
		return result, err
	}
	files, bytes := manifestFileTotals(reference.PreparedClosure.Manifest)
	result = RecoveryImportResult{
		Schema:                reference.Schema,
		SnapshotRef:           reference.SnapshotRef,
		ManifestDigest:        reference.PreparedClosure.Manifest.ManifestDigest,
		CommitDigest:          reference.PublicationCommitDigest,
		PreparedClosureDigest: reference.PreparedClosureDigest,
		Generation:            reference.PublicationCommit.Generation,
		TrustAnchorDigest:     anchorDigest,
		FactHealth:            reference.FactHealth,
		Files:                 files,
		Bytes:                 bytes,
	}
	if s.Store != nil {
		bundle := recoveryExportBundle{
			Schema: RecoveryExportBundleSchemaV1, SnapshotRef: reference.SnapshotRef,
			PublicationCommitDigest: reference.PublicationCommitDigest, PublicationCommit: reference.PublicationCommit,
			PreparedClosureDigest: reference.PreparedClosureDigest, PreparedClosure: reference.PreparedClosure,
			RequiredTrustAnchorKeyID: anchor.KeyID, RequiredTrustAnchorDigest: anchorDigest,
		}
		created, err := s.reconcileImportedPublication(ctx, bundle)
		if err != nil {
			return result, err
		}
		result.CatalogCreated = created
	}
	return result, nil
}

func readRecoveryArtifact(artifactPath string) ([]byte, error) {
	if strings.TrimSpace(artifactPath) == "" {
		return nil, errors.New("recovery artifact path is required")
	}
	file, err := openRecoveryInput(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("open recovery artifact: %w", err)
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, portableRecordReadLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > portableRecordReadLimit {
		return nil, errors.New("recovery artifact exceeds reader limit")
	}
	return payload, nil
}

// readRecoveryBundle loads exactly one bounded v1 recovery export bundle.
func readRecoveryBundle(artifactPath string) (recoveryExportBundle, error) {
	var bundle recoveryExportBundle
	payload, err := readRecoveryArtifact(artifactPath)
	if err != nil {
		return bundle, err
	}
	if err := decodeStrictRecord(payload, &bundle); err != nil {
		return bundle, fmt.Errorf("decode recovery bundle: %w", err)
	}
	return bundle, nil
}

// verifyImportBundle authenticates every portable record and binding inside a
// recovery bundle against the supplied anchor and the target repository. It
// returns the commit digest, the prepared-closure digest, and the anchor
// digest, all recomputed from the authenticated records.
func (s *Service) verifyImportBundle(ctx context.Context, anchor TrustAnchor, bundle recoveryExportBundle) (commitDigest, preparedDigest, anchorDigest string, err error) {
	if bundle.Schema != RecoveryExportBundleSchemaV1 {
		return "", "", "", fmt.Errorf("unsupported recovery bundle schema %q", bundle.Schema)
	}
	if bundle.SnapshotRef != bundle.PublicationCommit.SnapshotRef ||
		bundle.PublicationCommit.PublicationDomain != anchor.PublicationDomain ||
		bundle.PreparedClosure.Prepared.PublicationDomain != anchor.PublicationDomain {
		return "", "", "", errors.New("recovery bundle publication domain differs from trust anchor")
	}
	if bundle.RequiredTrustAnchorKeyID != anchor.KeyID {
		return "", "", "", fmt.Errorf("recovery bundle requires anchor key %q, got %q", bundle.RequiredTrustAnchorKeyID, anchor.KeyID)
	}
	anchorDigest, err = DigestCanonicalJSON(anchor)
	if err != nil {
		return "", "", "", err
	}
	if bundle.RequiredTrustAnchorDigest != anchorDigest {
		return "", "", "", errors.New("recovery bundle trust-anchor digest mismatch")
	}
	if err := bundle.PublicationCommit.Verify(anchor); err != nil {
		return "", "", "", fmt.Errorf("verify publication commit: %w", err)
	}
	if err := bundle.PreparedClosure.Prepared.Verify(anchor); err != nil {
		return "", "", "", fmt.Errorf("verify prepared closure: %w", err)
	}
	commitDigest, err = bundle.PublicationCommit.Digest()
	if err != nil {
		return "", "", "", err
	}
	if commitDigest != bundle.PublicationCommitDigest {
		return "", "", "", errors.New("recovery bundle publication commit digest mismatch")
	}
	preparedBytes, err := CanonicalJSON(bundle.PreparedClosure)
	if err != nil {
		return "", "", "", err
	}
	preparedDigest = DigestBytes(preparedBytes)
	if preparedDigest != bundle.PreparedClosureDigest || preparedDigest != bundle.PublicationCommit.PreparedObjectDigest {
		return "", "", "", errors.New("recovery bundle prepared closure digest mismatch")
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return "", "", "", err
	}
	if bundle.PublicationCommit.TargetIdentity != driver.RepositoryIdentity() {
		return "", "", "", errors.New("recovery bundle publication targets another repository")
	}
	if err := validatePreparedEnvelope(driver, anchor, bundle.PublicationCommit, bundle.PreparedClosure, int64(len(preparedBytes))); err != nil {
		return "", "", "", fmt.Errorf("validate recovery bundle prepared closure: %w", err)
	}
	// The legacy v1 bundle carries the authenticated payload receipt but not a
	// separate v2 reference reader envelope. Validate every exact object here so
	// import and migration reject a missing or tampered payload before reporting
	// a clean-install recovery artifact as admitted.
	for _, object := range bundle.PreparedClosure.PayloadReceipt.Objects {
		if err := verifyExactObjectReadback(ctx, s.Repo, object.ContentID, object.LogicalBytes); err != nil {
			return "", "", "", fmt.Errorf("verify recovery bundle payload %s: %w", object.ContentID, err)
		}
	}
	domain := s.PublicationDomain
	if domain == "" {
		domain = anchor.PublicationDomain
	}
	commits, err := listCommitMarkers(ctx, driver, anchor, domain)
	if err != nil {
		return "", "", "", err
	}
	if err := verifyImportLineage(bundle.PublicationCommit, commitDigest, commits); err != nil {
		return "", "", "", err
	}
	return commitDigest, preparedDigest, anchorDigest, nil
}

// verifyImportLineage checks the bundle commit's generation/parent shape and
// its presence in the target repository's authenticated commit lineage. The
// bundle must name an already committed publication: the repository, not the
// bundle, is the recovery authority, and the reader restores from repository
// records.
func verifyImportLineage(commit PublicationCommitRecord, commitDigest string, commits []committedPublication) error {
	if commit.Generation == 0 {
		return errors.New("recovery bundle publication generation is invalid")
	}
	if commit.Generation == 1 {
		if commit.ParentCommitDigest != "" {
			return errors.New("recovery bundle genesis publication names a parent")
		}
	} else if !validExactContentID(commit.ParentCommitDigest) {
		return errors.New("recovery bundle publication successor has no valid parent")
	}
	if len(commits) == 0 {
		return errors.New("repository has no committed publications to admit the recovery bundle into")
	}
	for _, publication := range commits {
		if publication.CommitDigest == commitDigest {
			return nil
		}
	}
	return errors.New("recovery bundle publication is absent from the repository commit lineage")
}

// reconcileImportedPublication mirrors the ReconcileIngestPublication
// projection path: when the rebuildable catalog already carries the staged
// namespace for the imported snapshot, the immutable publication row is
// projected from the portable commit. A catalog with no staged namespace is
// left untouched (CatalogCreated stays false).
func (s *Service) reconcileImportedPublication(ctx context.Context, bundle recoveryExportBundle) (bool, error) {
	if s.Store == nil {
		return false, nil
	}
	commit := bundle.PublicationCommit
	root, err := s.Store.GetNamespaceRootBySnapshotRef(ctx, commit.SnapshotRef)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	scan, err := s.Store.GetScanGeneration(ctx, root.WorkspaceID, root.ScanGenerationID)
	if err != nil {
		return false, err
	}
	if scan.SourceID != root.SourceID || bundle.PreparedClosure.Manifest.Binding.IdentityDigest() != root.AuthorityDigest {
		return false, errors.New("imported publication differs from staged capture identity")
	}
	metadata, _ := json.Marshal(map[string]any{
		"config_digest":             bundle.PreparedClosure.Manifest.ConfigDigest,
		"prepared_closure_digest":   commit.PreparedObjectDigest,
		"publication_commit_digest": commitDigestForImport(commit),
		"publication_generation":    commit.Generation,
		"projection_reconciled":     true,
	})
	publication := sqlite.Publication{
		ID: commit.PublicationID, WorkspaceID: root.WorkspaceID,
		PlanDigest: commit.PlanDigest, SnapshotRef: commit.SnapshotRef,
		ScanGenerationID: root.ScanGenerationID, BindingID: scan.CaptureSetID,
		NamespaceRootID: root.ID, ManifestDigest: commit.ManifestDigest,
		CommittedAt: commit.SignedAt, Metadata: metadata,
	}
	if err := s.Store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertPublication(ctx, &publication)
	}); err != nil {
		if !errors.Is(err, sqlite.ErrConflict) {
			return false, err
		}
		existing, lookupErr := s.Store.GetPublicationByPlanDigest(ctx, root.WorkspaceID, commit.PlanDigest)
		if lookupErr != nil {
			return false, err
		}
		if existing.SnapshotRef != publication.SnapshotRef || existing.ManifestDigest != publication.ManifestDigest {
			return false, errors.New("imported plan digest projects a different publication")
		}
	}
	return true, nil
}

func commitDigestForImport(commit PublicationCommitRecord) string {
	digest, err := commit.Digest()
	if err != nil {
		return ""
	}
	return digest
}
