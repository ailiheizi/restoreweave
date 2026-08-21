package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// ReconcileIngestPublication reconstructs the result of an ingest whose
// publication commit completed before its PLAN_APPLY job was recorded as
// successful. It is deliberately read-only: the immutable publication,
// snapshot manifest, and catalog identity rows are the recovery evidence.
func (s *Service) ReconcileIngestPublication(ctx context.Context, workspaceID, executionKey string) (IngestResult, error) {
	var result IngestResult
	if err := s.require(); err != nil {
		return result, err
	}
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(executionKey) == "" {
		return result, fmt.Errorf("workspace id and execution key are required")
	}
	publication, err := s.Store.GetPublicationByPlanDigest(ctx, workspaceID, executionKey)
	if err != nil {
		if !isNotFound(err) || !s.signedPublicationEnabled() {
			return result, err
		}
		portable, portableErr := s.committedPublicationByPlanDigest(ctx, executionKey)
		if portableErr != nil {
			if isNotFound(portableErr) && s.signedPublicationEnabled() {
				prepared, preparedErr := s.preparedPublicationByPlanDigest(ctx, executionKey)
				if preparedErr == nil {
					return result, &PublicationOutcomeError{
						PlanDigest: executionKey, SnapshotRef: prepared.Prepared.SnapshotRef,
						PublicationID: prepared.Prepared.PublicationID,
						Role:          repository.RecordPreparedClosure,
						Cause:         errors.New("prepared closure is durable but no publication commit is present"),
					}
				}
				if !isNotFound(preparedErr) {
					return result, preparedErr
				}
			}
			return result, portableErr
		}
		publication, err = s.projectPortablePublication(ctx, workspaceID, portable)
		if err != nil {
			return result, err
		}
	}
	manifest, err := s.loadManifest(ctx, publication.SnapshotRef)
	if err != nil {
		return result, err
	}
	if manifest.ManifestDigest != publication.ManifestDigest {
		return result, fmt.Errorf("publication manifest digest differs from snapshot")
	}
	binding, err := s.Store.GetCaptureRootBinding(ctx, workspaceID, publication.BindingID)
	if err != nil {
		return result, err
	}
	scan, err := s.Store.GetScanGeneration(ctx, workspaceID, publication.ScanGenerationID)
	if err != nil {
		return result, err
	}
	root, err := s.Store.GetNamespaceRoot(ctx, workspaceID, publication.NamespaceRootID)
	if err != nil {
		return result, err
	}
	if root.SnapshotRef != publication.SnapshotRef || root.ScanGenerationID != publication.ScanGenerationID ||
		root.SourceID != scan.SourceID || binding.SourceID != scan.SourceID {
		return result, fmt.Errorf("publication catalog identity is inconsistent")
	}
	files, bytes := manifestFileTotals(manifest)
	decisions, localFiles, localBytes, linkOnlyFiles, locatorCount := manifestProtectionSummary(manifest)
	result = IngestResult{
		WorkspaceID: publication.WorkspaceID, SourceID: scan.SourceID,
		ScanID: publication.ScanGenerationID, BindingID: publication.BindingID,
		RootID: publication.NamespaceRootID, SnapshotRef: publication.SnapshotRef,
		ManifestDigest: publication.ManifestDigest, ConfigDigest: manifest.ConfigDigest,
		ProtectionDigest: manifest.ProtectionDigest, ProtectionDecisions: decisions,
		Files: files, Bytes: bytes, LocalFiles: localFiles, LocalBytes: localBytes,
		LinkOnlyFiles: linkOnlyFiles, LocatorCount: locatorCount,
	}
	if s.signedPublicationEnabled() {
		portable, err := s.committedPublicationForSnapshot(ctx, publication.SnapshotRef)
		if err != nil {
			return IngestResult{}, err
		}
		result.PreparedClosureDigest = portable.Commit.PreparedObjectDigest
		result.PublicationCommitDigest = portable.CommitDigest
		result.PublicationGeneration = portable.Commit.Generation
	}
	return result, nil
}

// preparedPublicationByPlanDigest finds a valid prepared closure that names an
// execution key but has no committed marker. Such an object is evidence of an
// interrupted publication, never evidence of a published snapshot.
func (s *Service) preparedPublicationByPlanDigest(ctx context.Context, planDigest string) (PreparedClosureEnvelope, error) {
	var empty PreparedClosureEnvelope
	if s == nil || s.TrustAnchor == nil || strings.TrimSpace(s.PublicationDomain) == "" {
		return empty, errors.New("prepared publication discovery requires an independent trust anchor and publication domain")
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return empty, err
	}
	digests, err := driver.ListRecordDigests(ctx, repository.RecordPreparedClosure)
	if err != nil {
		return empty, err
	}
	var found *PreparedClosureEnvelope
	for _, digest := range digests {
		payload, err := readRecord(ctx, driver, repository.RecordPreparedClosure, digest)
		if err != nil {
			return empty, err
		}
		var envelope PreparedClosureEnvelope
		if err := decodeStrictRecord(payload, &envelope); err != nil {
			return empty, fmt.Errorf("decode prepared closure %s: %w", digest, err)
		}
		if envelope.Prepared.PublicationDomain != s.PublicationDomain || envelope.Prepared.PlanDigest != planDigest {
			continue
		}
		if err := validatePreparedCandidate(driver, *s.TrustAnchor, envelope); err != nil {
			return empty, fmt.Errorf("validate prepared closure %s: %w", digest, err)
		}
		if found != nil {
			return empty, errors.New("conflicting prepared closures bind one execution key")
		}
		copy := envelope
		found = &copy
	}
	if found == nil {
		return empty, fmt.Errorf("%w: prepared plan %s", sqlite.ErrNotFound, planDigest)
	}
	return *found, nil
}

func validatePreparedCandidate(driver repository.RecordDriver, anchor TrustAnchor, envelope PreparedClosureEnvelope) error {
	if envelope.Schema != PreparedEnvelopeSchemaV1 && envelope.Schema != PreparedEnvelopeSchemaV2 {
		return fmt.Errorf("unsupported prepared envelope schema %q", envelope.Schema)
	}
	if envelope.Schema == PreparedEnvelopeSchemaV1 && envelope.Manifest.Schema != SnapshotSchemaV1 {
		return errors.New("prepared envelope v1 must contain snapshot v1")
	}
	if envelope.Schema == PreparedEnvelopeSchemaV2 && envelope.Manifest.Schema != SnapshotSchemaV2 {
		return errors.New("prepared envelope v2 must contain snapshot v2")
	}
	if err := envelope.Prepared.Verify(anchor); err != nil {
		return err
	}
	if envelope.Prepared.TargetIdentity != driver.RepositoryIdentity() || envelope.Prepared.FenceToken < 1 ||
		envelope.Prepared.RRFRootDigest != envelope.Manifest.ManifestDigest ||
		envelope.Prepared.ManifestDigest != envelope.Manifest.ManifestDigest ||
		envelope.Prepared.SnapshotRef != envelope.Manifest.SnapshotRef {
		return errors.New("prepared closure identity differs from its manifest or repository")
	}
	if err := authenticateManifest(envelope.Manifest); err != nil {
		return err
	}
	if err := validatePayloadReceipt(envelope.PayloadReceipt); err != nil {
		return err
	}
	if envelope.PayloadReceipt.RepositoryID != driver.RepositoryIdentity() ||
		envelope.PayloadReceipt.PublicationID != envelope.Prepared.PublicationID ||
		envelope.PayloadReceipt.SnapshotRef != envelope.Prepared.SnapshotRef {
		return errors.New("prepared payload receipt publication binding mismatch")
	}
	payloadDigest, err := DigestCanonicalJSON(envelope.PayloadReceipt)
	if err != nil || payloadDigest != envelope.Prepared.PayloadReceiptDigest ||
		envelope.PayloadReceipt.TotalBytes != envelope.Prepared.PayloadReceiptLength ||
		int64(len(envelope.PayloadReceipt.Objects)) != envelope.Prepared.PayloadReceiptObjectCount {
		return errors.New("prepared payload receipt digest or totals mismatch")
	}
	evidence, err := buildMetadataEvidence(envelope.Manifest)
	if err != nil {
		return err
	}
	evidenceDigest, err := DigestCanonicalJSON(evidence)
	verificationDigest, verificationErr := DigestCanonicalJSON(envelope.VerificationEvidence)
	if err != nil || verificationErr != nil || evidenceDigest != envelope.Prepared.VerificationDigest ||
		evidenceDigest != verificationDigest {
		return errors.New("prepared metadata evidence mismatch")
	}
	return compareManifestPayloadReceipt(envelope.Manifest, envelope.PayloadReceipt)
}

func (s *Service) committedPublicationByPlanDigest(ctx context.Context, planDigest string) (committedPublication, error) {
	publications, err := s.committedPublications(ctx)
	if err != nil {
		return committedPublication{}, err
	}
	var found *committedPublication
	for i := range publications {
		if publications[i].Commit.PlanDigest != planDigest {
			continue
		}
		if found != nil {
			return committedPublication{}, fmt.Errorf("multiple committed publications bind plan %s", planDigest)
		}
		copy := publications[i]
		found = &copy
	}
	if found == nil {
		return committedPublication{}, fmt.Errorf("%w: committed plan %s", sqlite.ErrNotFound, planDigest)
	}
	return *found, nil
}

func (s *Service) projectPortablePublication(ctx context.Context, workspaceID string, portable committedPublication) (sqlite.Publication, error) {
	root, err := s.Store.GetNamespaceRootBySnapshotRef(ctx, portable.Manifest.SnapshotRef)
	if err != nil {
		return sqlite.Publication{}, fmt.Errorf("locate staged namespace for committed publication: %w", err)
	}
	if root.WorkspaceID != workspaceID || root.SnapshotRef != portable.Commit.SnapshotRef {
		return sqlite.Publication{}, errors.New("portable publication differs from staged namespace")
	}
	scan, err := s.Store.GetScanGeneration(ctx, workspaceID, root.ScanGenerationID)
	if err != nil {
		return sqlite.Publication{}, err
	}
	if scan.SourceID != root.SourceID || portable.Manifest.Binding.IdentityDigest() != root.AuthorityDigest {
		return sqlite.Publication{}, errors.New("portable publication differs from staged capture identity")
	}
	metadata, _ := json.Marshal(map[string]any{
		"config_digest":             portable.Manifest.ConfigDigest,
		"prepared_closure_digest":   portable.Commit.PreparedObjectDigest,
		"publication_commit_digest": portable.CommitDigest,
		"publication_generation":    portable.Commit.Generation,
		"projection_reconciled":     true,
	})
	publication := sqlite.Publication{
		ID: portable.Commit.PublicationID, WorkspaceID: workspaceID,
		PlanDigest: portable.Commit.PlanDigest, SnapshotRef: portable.Commit.SnapshotRef,
		ScanGenerationID: root.ScanGenerationID, BindingID: scan.CaptureSetID,
		NamespaceRootID: root.ID, ManifestDigest: portable.Commit.ManifestDigest,
		CommittedAt: portable.Commit.SignedAt, Metadata: metadata,
	}
	if err := s.Store.Update(ctx, func(tx *sqlite.Tx) error { return tx.InsertPublication(ctx, &publication) }); err != nil {
		if existing, lookupErr := s.Store.GetPublicationByPlanDigest(ctx, workspaceID, portable.Commit.PlanDigest); lookupErr == nil {
			if existing.SnapshotRef != publication.SnapshotRef || existing.ManifestDigest != publication.ManifestDigest {
				return sqlite.Publication{}, errors.New("plan digest projects a different publication")
			}
			return existing, nil
		}
		return sqlite.Publication{}, err
	}
	return s.Store.GetPublicationByPlanDigest(ctx, workspaceID, portable.Commit.PlanDigest)
}

func manifestProtectionSummary(manifest Manifest) ([]IngestProtectionDecision, int, int64, int, int) {
	decisions := make([]IngestProtectionDecision, 0)
	localFiles, linkOnlyFiles, locatorCount := 0, 0, 0
	var localBytes int64
	for _, entry := range manifest.Entries {
		if entry.EntryType != string(sqlite.EntryFile) || entry.Protection.ExpectedLogicalLength == nil {
			continue
		}
		decision := IngestProtectionDecision{
			RelativePath:         entry.RelativePath,
			RawPath:              append([]byte(nil), entry.RawPath...),
			Mode:                 sqlite.ProtectionMode(entry.Protection.Mode),
			PlannedOutcome:       sqlite.ProtectionOutcome(entry.Protection.Outcome),
			ReasonCode:           entry.Protection.ReasonCode,
			ExpectedContentID:    entry.Protection.ExpectedContentID,
			ExpectedLogicalBytes: *entry.Protection.ExpectedLogicalLength,
		}
		for _, reference := range entry.Protection.RecoveryReferences {
			decision.LocatorCount += len(reference.ExternalLocators)
		}
		locatorCount += decision.LocatorCount
		if entry.Protection.LocalRepresentationID != "" {
			localFiles++
			localBytes += decision.ExpectedLogicalBytes
		}
		if decision.Mode == sqlite.ProtectionLinkOnly {
			linkOnlyFiles++
		}
		decisions = append(decisions, decision)
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		if comparison := bytes.Compare(decisions[i].RawPath, decisions[j].RawPath); comparison != 0 {
			return comparison < 0
		}
		return decisions[i].RelativePath < decisions[j].RelativePath
	})
	return decisions, localFiles, localBytes, linkOnlyFiles, locatorCount
}
