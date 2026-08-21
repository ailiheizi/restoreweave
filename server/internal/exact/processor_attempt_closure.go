package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const ProcessorAttemptClosureEnvelopeSchemaV1 = "org.restoreweave.processor-attempt-closure-envelope.v1"

// ProcessorAttemptClosureEnvelope keeps the signed child and the exact
// deterministic SQLite projection together. The bundle is supplemental: its
// absence never makes the exact publication undiscoverable.
type ProcessorAttemptClosureEnvelope struct {
	Schema  string                        `json:"schema"`
	Closure ProcessorAttemptClosureRecord `json:"closure"`
	Bundle  json.RawMessage               `json:"bundle"`
}

type processorAttemptBundle struct {
	Schema      string                          `json:"schema"`
	WorkspaceID string                          `json:"workspace_id"`
	SnapshotRef string                          `json:"snapshot_ref,omitempty"`
	Attempts    []sqlite.ProcessorAttemptExport `json:"attempts"`
}

// publishProcessorAttemptClosure publishes one signed child after the exact
// commit and after all in-process terminal attempt rows have been recorded.
// Failure is returned to the caller as a warning; it never rolls back exact
// publication.
func (s *Service) publishProcessorAttemptClosure(ctx context.Context, workspaceID, snapshotRef, parentDigest string) error {
	if !s.signedPublicationEnabled() {
		return nil
	}
	if s.SigningIdentity == nil || s.TrustAnchor == nil || strings.TrimSpace(s.PublicationDomain) == "" {
		return errors.New("processor attempt closure requires signing identity, trust anchor, and publication domain")
	}
	if strings.TrimSpace(parentDigest) == "" {
		return errors.New("processor attempt closure requires parent publication commit")
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return err
	}
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	lease, err := s.acquirePublicationFence(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = lease.release() }()
	parentBytes, err := readRecord(ctx, driver, repository.RecordPublicationCommit, parentDigest)
	if err != nil {
		return fmt.Errorf("read processor closure parent: %w", err)
	}
	var parent PublicationCommitRecord
	if err := decodeStrictRecord(parentBytes, &parent); err != nil {
		return fmt.Errorf("decode processor closure parent: %w", err)
	}
	if err := parent.Verify(*s.TrustAnchor); err != nil {
		return fmt.Errorf("verify processor closure parent: %w", err)
	}
	if digest, err := parent.Digest(); err != nil || digest != parentDigest {
		return errors.New("processor closure parent digest mismatch")
	}
	if parent.TargetIdentity != driver.RepositoryIdentity() || parent.PublicationDomain != s.PublicationDomain ||
		parent.SnapshotRef != snapshotRef || parent.PublicationID == "" {
		return errors.New("processor closure parent binding mismatch")
	}
	bundle, err := s.Store.ExportProcessorAttempts(ctx, workspaceID, snapshotRef)
	if err != nil {
		return fmt.Errorf("export processor attempts: %w", err)
	}
	parsed, err := validateProcessorAttemptBundle(bundle, workspaceID, snapshotRef)
	if err != nil {
		return err
	}
	bundleDigest := DigestBytes(bundle)
	if existing, err := s.processorAttemptClosures(ctx, snapshotRef); err != nil {
		return err
	} else {
		for _, candidate := range existing {
			if candidate.Closure.ParentCommitDigest != parentDigest {
				continue
			}
			if candidate.Closure.AttemptBundleDigest != bundleDigest {
				return errors.New("conflicting processor attempt closure for parent publication")
			}
			return nil
		}
	}
	closure, err := SignProcessorAttemptClosure(*s.SigningIdentity, ProcessorAttemptClosureRecord{
		Schema: ProcessorAttemptClosureSchemaV1, SignatureDomain: RecoverySignatureDomainV1,
		RecordKind: ProcessorAttemptClosureKind, WorkspaceID: workspaceID,
		PublicationID: parent.PublicationID, PublicationDomain: s.PublicationDomain,
		SnapshotRef: snapshotRef, ManifestDigest: parent.ManifestDigest,
		ParentCommitDigest: parentDigest, AttemptBundleSchema: parsed.Schema,
		AttemptBundleDigest: bundleDigest, AttemptBundleLength: int64(len(bundle)),
		AttemptCount: int64(len(parsed.Attempts)), TargetIdentity: driver.RepositoryIdentity(),
		WriterIdentity: s.SigningIdentity.WriterIdentity, KeyID: s.SigningIdentity.KeyID,
		FenceToken: parent.FenceToken, SignedAt: s.now(),
	})
	if err != nil {
		return err
	}
	if err := closure.Verify(*s.TrustAnchor); err != nil {
		return fmt.Errorf("verify signed processor attempt closure: %w", err)
	}
	payload, err := CanonicalJSON(ProcessorAttemptClosureEnvelope{
		Schema: ProcessorAttemptClosureEnvelopeSchemaV1, Closure: closure, Bundle: json.RawMessage(bundle),
	})
	if err != nil {
		return err
	}
	if err := s.validatePublicationFence(ctx, lease); err != nil {
		return err
	}
	receipt, err := driver.PlaceRecord(ctx, repository.RecordProcessorAttemptClosure, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("place processor attempt closure: %w", err)
	}
	if err := driver.VerifyRecord(ctx, receipt); err != nil {
		return fmt.Errorf("verify processor attempt closure: %w", err)
	}
	return nil
}

func validateProcessorAttemptBundle(payload []byte, workspaceID, snapshotRef string) (processorAttemptBundle, error) {
	var bundle processorAttemptBundle
	if len(payload) == 0 || int64(len(payload)) > portableRecordReadLimit {
		return bundle, errors.New("processor attempt bundle is empty or exceeds reader limit")
	}
	if err := decodeStrictRecord(payload, &bundle); err != nil {
		return bundle, fmt.Errorf("decode processor attempt bundle: %w", err)
	}
	if bundle.Schema != sqlite.ProcessorAttemptExportSchema || bundle.WorkspaceID != workspaceID || bundle.SnapshotRef != snapshotRef {
		return bundle, errors.New("processor attempt bundle binding mismatch")
	}
	if bundle.Attempts == nil {
		return bundle, errors.New("processor attempt bundle attempts must be an array")
	}
	canonical, err := json.Marshal(bundle)
	if err != nil || !bytes.Equal(payload, canonical) {
		return bundle, errors.New("processor attempt bundle is not canonical")
	}
	seen := make(map[string]struct{}, len(bundle.Attempts))
	for i, attempt := range bundle.Attempts {
		if attempt.WorkspaceID != workspaceID || attempt.SnapshotRef != snapshotRef || strings.TrimSpace(attempt.AttemptID) == "" ||
			strings.TrimSpace(attempt.SubjectRef) == "" || strings.TrimSpace(attempt.RouteDigest) == "" ||
			strings.TrimSpace(attempt.Stage) == "" || strings.TrimSpace(attempt.CapabilityID) == "" ||
			strings.TrimSpace(attempt.ReasonCode) == "" || strings.TrimSpace(attempt.ProcessorDigest) == "" {
			return bundle, errors.New("processor attempt bundle contains an incomplete attempt")
		}
		if _, ok := seen[attempt.AttemptID]; ok {
			return bundle, errors.New("processor attempt bundle contains duplicate attempt ids")
		}
		seen[attempt.AttemptID] = struct{}{}
		switch attempt.Status {
		case "SUCCEEDED", "INAPPLICABLE", "FAILED", "CANCELLED":
		default:
			return bundle, errors.New("processor attempt bundle contains an invalid terminal status")
		}
		if attempt.FenceToken < 1 || attempt.CreatedAt.IsZero() || attempt.FinishedAt.IsZero() || attempt.FinishedAt.Before(attempt.CreatedAt) {
			return bundle, errors.New("processor attempt bundle contains invalid fencing or time evidence")
		}
		if len(attempt.Route) == 0 || !json.Valid(attempt.Route) || len(attempt.Provenance) == 0 || !json.Valid(attempt.Provenance) {
			return bundle, errors.New("processor attempt bundle contains invalid route or provenance JSON")
		}
		if i > 0 {
			previous := bundle.Attempts[i-1]
			if attempt.CreatedAt.Before(previous.CreatedAt) ||
				(attempt.CreatedAt.Equal(previous.CreatedAt) && attempt.AttemptID <= previous.AttemptID) {
				return bundle, errors.New("processor attempt bundle is not in deterministic order")
			}
		}
		previousArtifact := ""
		for _, artifactRef := range attempt.ArtifactRefs {
			if strings.TrimSpace(artifactRef) == "" || (previousArtifact != "" && artifactRef <= previousArtifact) {
				return bundle, errors.New("processor attempt artifact references must be nonempty, unique, and sorted")
			}
			previousArtifact = artifactRef
		}
	}
	return bundle, nil
}

// ListProcessorAttemptClosures returns authenticated post-publication attempt
// bundles without consulting SQLite. Exact restore does not depend on these
// supplemental records.
func (s *Service) ListProcessorAttemptClosures(ctx context.Context, snapshotRef string) ([]ProcessorAttemptClosureEnvelope, error) {
	return s.processorAttemptClosures(ctx, snapshotRef)
}

// processorAttemptClosures validates supplemental records independently of
// SQLite. Exact publication discovery deliberately does not call this method.
func (s *Service) processorAttemptClosures(ctx context.Context, snapshotRef string) ([]ProcessorAttemptClosureEnvelope, error) {
	if s.TrustAnchor == nil || strings.TrimSpace(s.PublicationDomain) == "" {
		return nil, errors.New("processor closure discovery requires trust anchor and publication domain")
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return nil, err
	}
	publications, err := s.committedPublications(ctx)
	if err != nil {
		return nil, err
	}
	parents := make(map[string]committedPublication, len(publications))
	for _, publication := range publications {
		parents[publication.CommitDigest] = publication
	}
	digests, err := driver.ListRecordDigests(ctx, repository.RecordProcessorAttemptClosure)
	if err != nil {
		return nil, err
	}
	var result []ProcessorAttemptClosureEnvelope
	byParent := make(map[string]string)
	for _, digest := range digests {
		payload, err := readRecord(ctx, driver, repository.RecordProcessorAttemptClosure, digest)
		if err != nil {
			return nil, err
		}
		var envelope ProcessorAttemptClosureEnvelope
		if err := decodeStrictRecord(payload, &envelope); err != nil {
			return nil, fmt.Errorf("decode processor attempt closure %s: %w", digest, err)
		}
		if envelope.Schema != ProcessorAttemptClosureEnvelopeSchemaV1 {
			return nil, errors.New("unsupported processor attempt closure envelope schema")
		}
		if objectDigest := DigestBytes(payload); objectDigest != digest {
			return nil, errors.New("processor attempt closure object digest mismatch")
		}
		closure := envelope.Closure
		if closure.PublicationDomain != s.PublicationDomain {
			continue
		}
		if err := closure.Verify(*s.TrustAnchor); err != nil {
			return nil, fmt.Errorf("verify processor attempt closure %s: %w", digest, err)
		}
		if closure.TargetIdentity != driver.RepositoryIdentity() {
			return nil, errors.New("processor attempt closure repository binding mismatch")
		}
		bundle, err := validateProcessorAttemptBundle(envelope.Bundle, closure.WorkspaceID, closure.SnapshotRef)
		if err != nil {
			return nil, err
		}
		if DigestBytes(envelope.Bundle) != closure.AttemptBundleDigest || int64(len(envelope.Bundle)) != closure.AttemptBundleLength ||
			int64(len(bundle.Attempts)) != closure.AttemptCount || bundle.Schema != closure.AttemptBundleSchema {
			return nil, errors.New("processor attempt closure bundle digest or count mismatch")
		}
		publication, ok := parents[closure.ParentCommitDigest]
		if !ok {
			return nil, errors.New("processor attempt closure parent is not a committed publication")
		}
		parent := publication.Commit
		if parent.TargetIdentity != closure.TargetIdentity || parent.PublicationDomain != closure.PublicationDomain || parent.PublicationID != closure.PublicationID ||
			parent.SnapshotRef != closure.SnapshotRef || parent.ManifestDigest != closure.ManifestDigest ||
			parent.FenceToken != closure.FenceToken || closure.SignedAt.Before(parent.SignedAt) {
			return nil, errors.New("processor attempt closure parent binding mismatch")
		}
		if previous, ok := byParent[closure.ParentCommitDigest]; ok && previous != closure.AttemptBundleDigest {
			return nil, errors.New("conflicting processor attempt closure bundles for parent")
		} else if ok {
			continue
		}
		byParent[closure.ParentCommitDigest] = closure.AttemptBundleDigest
		if snapshotRef != "" && closure.SnapshotRef != snapshotRef {
			continue
		}
		result = append(result, envelope)
	}
	return result, nil
}
