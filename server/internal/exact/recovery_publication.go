package exact

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const (
	PayloadReceiptSchemaV1             = "org.restoreweave.payload-receipt.v1"
	MetadataEvidenceSchemaV1           = "org.restoreweave.authenticated-metadata-evidence.v1"
	MetadataEvidenceSchemaV2           = "org.restoreweave.authenticated-metadata-evidence.v2"
	PreparedEnvelopeSchemaV1           = "org.restoreweave.prepared-envelope.v1"
	PreparedEnvelopeSchemaV2           = "org.restoreweave.prepared-envelope.v2"
	portableRecordReadLimit      int64 = 16 << 20
	portableFactsCoveragePartial       = "PARTIAL"
)

var portableFactsV1Omissions = []string{
	"annotations",
	"descriptions",
	"name-and-relation-per-field-provenance",
	"ownership-mode-and-time-per-field-state",
	"processor-attempts",
}

var (
	// ErrUnknownExternalOutcome means a repository operation may have crossed
	// its durable placement boundary, but the caller cannot prove the result.
	// Callers must reconcile the repository before retrying the operation.
	ErrUnknownExternalOutcome = errors.New("UNKNOWN_EXTERNAL_OUTCOME")
	// ErrNeedsReconciliation is the operator/action classification for an
	// unknown repository placement outcome.
	ErrNeedsReconciliation = errors.New("NEEDS_RECONCILIATION")
	// ErrPublicationLeaseRelease records a failed release after a publication
	// side effect. The immutable record may already be committed, so callers
	// must reconcile the publication before treating the operation as terminal.
	ErrPublicationLeaseRelease = errors.New("PUBLICATION_LEASE_RELEASE_FAILED")
)

// PublicationOutcomeError preserves the operation identity needed to perform
// bounded reconciliation after a repository returns an error at a placement
// boundary. It deliberately does not claim that a prepared-only object is a
// committed publication.
type PublicationOutcomeError struct {
	PlanDigest    string
	SnapshotRef   string
	PublicationID string
	Role          repository.RecordRole
	Cause         error
}

func (e *PublicationOutcomeError) Error() string {
	if e == nil {
		return ErrUnknownExternalOutcome.Error() + "; " + ErrNeedsReconciliation.Error()
	}
	message := ErrUnknownExternalOutcome.Error() + "; " + ErrNeedsReconciliation.Error()
	if e.Role != "" {
		message += fmt.Sprintf(" after %s placement", e.Role)
	}
	if e.PlanDigest != "" {
		message += fmt.Sprintf(" for plan %s", e.PlanDigest)
	}
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

func (e *PublicationOutcomeError) Unwrap() error { return e.Cause }

func (e *PublicationOutcomeError) Is(target error) bool {
	return target == ErrUnknownExternalOutcome || target == ErrNeedsReconciliation
}

// PayloadPlacementOutcomeError identifies an exact blob whose placement
// response could not be proven. The content identity is retained so a caller
// can reconcile the object without replaying source reads or creating a
// second logical payload.
type PayloadPlacementOutcomeError struct {
	ContentID string
	Cause     error
}

func (e *PayloadPlacementOutcomeError) Error() string {
	message := ErrUnknownExternalOutcome.Error() + "; " + ErrNeedsReconciliation.Error()
	if e != nil && e.ContentID != "" {
		message += fmt.Sprintf(" after payload %s placement", e.ContentID)
	}
	if e != nil && e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

func (e *PayloadPlacementOutcomeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *PayloadPlacementOutcomeError) Is(target error) bool {
	return target == ErrUnknownExternalOutcome || target == ErrNeedsReconciliation
}

// placeExactWithReadback treats every post-boundary placement error as an
// unknown outcome until the expected content identity is independently
// verified. A successful readback returns a deterministic synthetic receipt;
// receipt transport itself is not recovery authority.
func placeExactWithReadback(ctx context.Context, driver repository.Driver, contentID string, logicalBytes int64, body io.Reader) (repository.Receipt, error) {
	receipt, placeErr := driver.PlaceExact(ctx, contentID, body)
	if placeErr == nil {
		// Hash the logical stream exposed by the reader rather than trusting a
		// backend Verify result. A replaceable driver may report backend health
		// while returning bytes that do not satisfy the signed exact identity.
		if verifyErr := verifyExactObjectReadback(ctx, driver, contentID, logicalBytes); verifyErr == nil {
			return receipt, nil
		} else {
			placeErr = fmt.Errorf("exact payload readback after successful placement: %w", verifyErr)
		}
	}
	if verifyErr := verifyExactObjectReadback(context.WithoutCancel(ctx), driver, contentID, logicalBytes); verifyErr == nil {
		return repository.Receipt{ContentID: contentID, Bytes: logicalBytes, StoredBytes: logicalBytes, Existed: true}, nil
	} else {
		placeErr = fmt.Errorf("observe exact payload %s: %w; original placement: %v", contentID, verifyErr, placeErr)
	}
	return repository.Receipt{}, &PayloadPlacementOutcomeError{ContentID: contentID, Cause: placeErr}
}

type PayloadObjectReceipt struct {
	ContentID    string `json:"content_id"`
	LogicalBytes int64  `json:"logical_bytes"`
}

// PayloadReceipt is a deterministic aggregate of the exact objects read back
// before the prepared closure is signed. It is embedded in that closure.
type PayloadReceipt struct {
	Schema        string                 `json:"schema"`
	RepositoryID  string                 `json:"repository_id"`
	PublicationID string                 `json:"publication_id"`
	SnapshotRef   string                 `json:"snapshot_ref"`
	Objects       []PayloadObjectReceipt `json:"objects"`
	TotalBytes    int64                  `json:"total_bytes"`
}

type AuthenticatedMetadataEvidence struct {
	Schema                  string   `json:"schema"`
	ManifestDigest          string   `json:"manifest_digest"`
	PortableFactsSchema     string   `json:"portable_facts_schema,omitempty"`
	PortableFactsDigest     string   `json:"portable_facts_digest,omitempty"`
	PortableFactsEntryCount int64    `json:"portable_facts_entry_count,omitempty"`
	PortableFactsCoverage   string   `json:"portable_facts_coverage,omitempty"`
	PortableFactsOmissions  []string `json:"portable_facts_omissions,omitempty"`
	EntryCount              int64    `json:"entry_count"`
	FileCount               int64    `json:"file_count"`
	LocallyProtectedFiles   int64    `json:"locally_protected_files"`
	ExplicitlyUnprotected   int64    `json:"explicitly_unprotected_files"`
	ContentCoverageComplete bool     `json:"content_coverage_complete"`
}

type PreparedClosureEnvelope struct {
	Schema               string                        `json:"schema"`
	Prepared             SignedPreparedClosure         `json:"prepared"`
	PayloadReceipt       PayloadReceipt                `json:"payload_receipt"`
	VerificationEvidence AuthenticatedMetadataEvidence `json:"verification_evidence"`
	Manifest             Manifest                      `json:"manifest"`
}

type stableRecordReceipt struct {
	RepositoryID string                `json:"repository_id"`
	Role         repository.RecordRole `json:"role"`
	Digest       string                `json:"digest"`
	Bytes        int64                 `json:"bytes"`
}

type PublicationClosureResult struct {
	PreparedDigest string
	CommitDigest   string
	Generation     uint64
}

type committedPublication struct {
	CommitDigest string
	Commit       PublicationCommitRecord
	Prepared     PreparedClosureEnvelope
	Manifest     Manifest
}

func (s *Service) signedPublicationEnabled() bool {
	return s != nil && (s.RequireSignedPublication || s.SigningIdentity != nil || s.TrustAnchor != nil)
}

func (s *Service) publicationRecordDriver() (repository.RecordDriver, error) {
	driver, ok := s.Repo.(repository.RecordDriver)
	if !ok {
		return nil, errors.New("repository profile does not support portable publication records")
	}
	return driver, nil
}

func buildPayloadReceipt(driver repository.RecordDriver, publicationID, snapshotRef string, placed placedSet) (PayloadReceipt, error) {
	receipt := PayloadReceipt{
		Schema: PayloadReceiptSchemaV1, RepositoryID: driver.RepositoryIdentity(),
		PublicationID: publicationID, SnapshotRef: snapshotRef,
		Objects: make([]PayloadObjectReceipt, 0, len(placed.payloadReceipts)),
	}
	seen := make(map[string]int64, len(placed.payloadReceipts))
	for _, object := range placed.payloadReceipts {
		if previous, ok := seen[object.ContentID]; ok {
			if previous != object.Bytes {
				return PayloadReceipt{}, fmt.Errorf("payload %s has conflicting lengths", object.ContentID)
			}
			continue
		}
		seen[object.ContentID] = object.Bytes
		receipt.Objects = append(receipt.Objects, PayloadObjectReceipt{ContentID: object.ContentID, LogicalBytes: object.Bytes})
		receipt.TotalBytes += object.Bytes
	}
	sort.Slice(receipt.Objects, func(i, j int) bool { return receipt.Objects[i].ContentID < receipt.Objects[j].ContentID })
	return receipt, validatePayloadReceipt(receipt)
}

func validatePayloadReceipt(receipt PayloadReceipt) error {
	if receipt.Schema != PayloadReceiptSchemaV1 || strings.TrimSpace(receipt.RepositoryID) == "" ||
		strings.TrimSpace(receipt.PublicationID) == "" || strings.TrimSpace(receipt.SnapshotRef) == "" || receipt.TotalBytes < 0 {
		return errors.New("payload receipt is incomplete")
	}
	var total int64
	previous := ""
	for _, object := range receipt.Objects {
		if !validExactContentID(object.ContentID) || object.LogicalBytes < 0 || (previous != "" && object.ContentID <= previous) {
			return errors.New("payload receipt objects must be unique, sorted, and non-negative")
		}
		previous = object.ContentID
		total += object.LogicalBytes
	}
	if total != receipt.TotalBytes {
		return fmt.Errorf("payload receipt total is %d, want %d", receipt.TotalBytes, total)
	}
	return nil
}

func validExactContentID(contentID string) bool {
	algorithm, payload, ok := strings.Cut(contentID, ":")
	if !ok || algorithm != repository.AlgorithmSHA256 || len(payload) != 64 || payload != strings.ToLower(payload) {
		return false
	}
	_, err := hex.DecodeString(payload)
	return err == nil
}

func buildMetadataEvidence(manifest Manifest) (AuthenticatedMetadataEvidence, error) {
	evidence := AuthenticatedMetadataEvidence{
		Schema: MetadataEvidenceSchemaV1, ManifestDigest: manifest.ManifestDigest,
		EntryCount: int64(len(manifest.Entries)), ContentCoverageComplete: true,
	}
	switch manifest.Schema {
	case SnapshotSchemaV1:
	case SnapshotSchemaV2:
		factsDigest, err := manifestFactsDigest(manifest)
		if err != nil {
			return AuthenticatedMetadataEvidence{}, err
		}
		evidence.Schema = MetadataEvidenceSchemaV2
		evidence.PortableFactsSchema = PortableFactsSchemaV1
		evidence.PortableFactsDigest = factsDigest
		evidence.PortableFactsEntryCount = int64(len(manifest.Entries))
		evidence.PortableFactsCoverage = portableFactsCoveragePartial
		evidence.PortableFactsOmissions = append([]string(nil), portableFactsV1Omissions...)
	default:
		return AuthenticatedMetadataEvidence{}, fmt.Errorf("unsupported manifest schema %q", manifest.Schema)
	}
	for _, entry := range manifest.Entries {
		if sqlite.NamespaceEntryType(entry.EntryType) != sqlite.EntryFile {
			continue
		}
		evidence.FileCount++
		if entry.Protection.LocalRepresentationID != "" && entry.ContentID != "" {
			evidence.LocallyProtectedFiles++
			continue
		}
		evidence.ExplicitlyUnprotected++
		evidence.ContentCoverageComplete = false
	}
	return evidence, nil
}

func (s *Service) publishRecoveryClosure(ctx context.Context, adopted adopted, manifest Manifest, placed placedSet, planDigest, captureDigest, policyDigest string) (result PublicationClosureResult, retErr error) {
	if !s.signedPublicationEnabled() {
		return result, nil
	}
	if s.SigningIdentity == nil || s.TrustAnchor == nil {
		return result, errors.New("signed publication requires a signing identity and trust anchor")
	}
	if strings.TrimSpace(s.PublicationDomain) == "" || strings.TrimSpace(planDigest) == "" {
		return result, errors.New("signed publication requires a publication domain and immutable plan digest")
	}
	if s.TrustAnchor.PublicationDomain != s.PublicationDomain {
		return result, errors.New("trust anchor publication domain differs from service configuration")
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return result, err
	}
	// The in-process mutex keeps one process from double-acquiring the same
	// owner; the cross-process fence below protects concurrent processes.
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	lease, err := s.acquirePublicationFence(ctx)
	if err != nil {
		return result, err
	}
	ctx = lease.context()
	defer func() {
		if releaseErr := lease.release(); releaseErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("%w: %w: %v", ErrNeedsReconciliation, ErrPublicationLeaseRelease, releaseErr))
		}
	}()
	existing, err := listCommitMarkers(ctx, driver, *s.TrustAnchor, s.PublicationDomain)
	if err != nil {
		return result, err
	}
	for _, publication := range existing {
		if publication.Commit.PlanDigest == planDigest {
			return result, fmt.Errorf("%w: plan %s", ErrPublicationAlreadyCommitted, planDigest)
		}
	}
	payloadReceipt, err := buildPayloadReceipt(driver, adopted.publicationID, manifest.SnapshotRef, placed)
	if err != nil {
		return result, err
	}
	if err := authenticateManifest(manifest); err != nil {
		return result, fmt.Errorf("authenticate prepared manifest: %w", err)
	}
	if err := compareManifestPayloadReceipt(manifest, payloadReceipt); err != nil {
		return result, fmt.Errorf("verify prepared payload coverage: %w", err)
	}
	payloadDigest, err := DigestCanonicalJSON(payloadReceipt)
	if err != nil {
		return result, err
	}
	evidence, err := buildMetadataEvidence(manifest)
	if err != nil {
		return result, err
	}
	verificationDigest, err := DigestCanonicalJSON(evidence)
	if err != nil {
		return result, err
	}
	generation, parentDigest := uint64(1), ""
	recordFenceToken := uint64(FallbackFenceToken)
	if len(existing) > 0 {
		latest := existing[len(existing)-1]
		generation, parentDigest = latest.Commit.Generation+1, latest.CommitDigest
		if latest.Commit.FenceToken == ^uint64(0) {
			return result, errors.New("publication fence token exhausted")
		}
		recordFenceToken = latest.Commit.FenceToken + 1
	}
	// The signed token must advance both the authenticated commit lineage and
	// the coordination lease counter. An abandoned lease can advance the
	// latter without producing a commit; reusing a smaller lineage token would
	// let a stale writer appear newer to readers.
	if lease.coordinationToken > 0 && uint64(lease.coordinationToken) > recordFenceToken {
		recordFenceToken = uint64(lease.coordinationToken)
	}
	if recordFenceToken == 0 {
		return result, errors.New("publication fence token exhausted")
	}
	lease.token = recordFenceToken
	prepared, err := SignPreparedClosure(*s.SigningIdentity, SignedPreparedClosure{
		Schema: PreparedClosureSchemaV1, SignatureDomain: RecoverySignatureDomainV1,
		RecordKind: PreparedClosureKind, PublicationID: adopted.publicationID,
		PublicationDomain: s.PublicationDomain, Generation: generation,
		SnapshotRef: manifest.SnapshotRef, RRFRootDigest: manifest.ManifestDigest,
		ManifestDigest: manifest.ManifestDigest, PayloadReceiptDigest: payloadDigest,
		PayloadReceiptLength:      payloadReceipt.TotalBytes,
		PayloadReceiptObjectCount: int64(len(payloadReceipt.Objects)), PlanDigest: planDigest,
		CaptureDigest: captureDigest, PolicyDigest: policyDigest,
		VerificationDigest: verificationDigest, TargetIdentity: driver.RepositoryIdentity(),
		FenceToken: lease.token, WriterIdentity: s.SigningIdentity.WriterIdentity,
		KeyID: s.SigningIdentity.KeyID, SignedAt: s.now(), ParentCommitDigest: parentDigest,
	})
	if err != nil {
		return result, err
	}
	envelopeSchema := PreparedEnvelopeSchemaV1
	if manifest.Schema == SnapshotSchemaV2 {
		envelopeSchema = PreparedEnvelopeSchemaV2
	}
	envelope := PreparedClosureEnvelope{
		Schema: envelopeSchema, Prepared: prepared, PayloadReceipt: payloadReceipt,
		VerificationEvidence: evidence, Manifest: manifest,
	}
	preparedBytes, err := CanonicalJSON(envelope)
	if err != nil {
		return result, err
	}
	if err := s.validatePublicationFence(ctx, lease); err != nil {
		return result, err
	}
	preparedReceipt, err := driver.PlaceRecord(ctx, repository.RecordPreparedClosure, bytes.NewReader(preparedBytes))
	if err != nil {
		return s.reconcileUnknownPublicationOutcome(ctx, driver, adopted.publicationID, manifest.SnapshotRef, planDigest, repository.RecordPreparedClosure, err)
	}
	if err := driver.VerifyRecord(ctx, preparedReceipt); err != nil {
		return s.reconcileUnknownPublicationOutcome(ctx, driver, adopted.publicationID, manifest.SnapshotRef, planDigest, repository.RecordPreparedClosure, fmt.Errorf("reconcile prepared closure: %w", err))
	}
	if err := s.validatePublicationFence(ctx, lease); err != nil {
		return s.reconcileUnknownPublicationOutcome(ctx, driver, adopted.publicationID, manifest.SnapshotRef, planDigest, repository.RecordPreparedClosure, err)
	}
	preparedReceiptDigest, err := DigestCanonicalJSON(stableReceipt(preparedReceipt))
	if err != nil {
		return result, err
	}
	commit, err := SignPublicationCommit(*s.SigningIdentity, PublicationCommitRecord{
		Schema: PublicationCommitSchemaV1, SignatureDomain: RecoverySignatureDomainV1,
		RecordKind: PublicationCommitKind, PublicationID: adopted.publicationID,
		PublicationDomain: s.PublicationDomain, Generation: generation,
		SnapshotRef: manifest.SnapshotRef, RRFRootDigest: manifest.ManifestDigest,
		ManifestDigest: manifest.ManifestDigest, PayloadReceiptDigest: payloadDigest,
		PayloadReceiptLength:      payloadReceipt.TotalBytes,
		PayloadReceiptObjectCount: int64(len(payloadReceipt.Objects)), PlanDigest: planDigest,
		CaptureDigest: captureDigest, PolicyDigest: policyDigest,
		VerificationDigest: verificationDigest, TargetIdentity: driver.RepositoryIdentity(),
		PreparedObjectDigest: preparedReceipt.Digest, PreparedReceiptDigest: preparedReceiptDigest,
		FenceToken: lease.token, WriterIdentity: s.SigningIdentity.WriterIdentity,
		KeyID: s.SigningIdentity.KeyID, SignedAt: s.now(), ParentCommitDigest: parentDigest,
	})
	if err != nil {
		return result, err
	}
	commitBytes, err := CanonicalJSON(commit)
	if err != nil {
		return result, err
	}
	if err := s.validatePublicationFence(ctx, lease); err != nil {
		return result, err
	}
	commitReceipt, err := driver.PlaceRecord(ctx, repository.RecordPublicationCommit, bytes.NewReader(commitBytes))
	if err != nil {
		return s.reconcileUnknownPublicationOutcome(ctx, driver, adopted.publicationID, manifest.SnapshotRef, planDigest, repository.RecordPublicationCommit, err)
	}
	if err := driver.VerifyRecord(ctx, commitReceipt); err != nil {
		return s.reconcileUnknownPublicationOutcome(ctx, driver, adopted.publicationID, manifest.SnapshotRef, planDigest, repository.RecordPublicationCommit, fmt.Errorf("reconcile publication commit: %w", err))
	}
	if err := s.validatePublicationFence(ctx, lease); err != nil {
		return s.reconcileUnknownPublicationOutcome(ctx, driver, adopted.publicationID, manifest.SnapshotRef, planDigest, repository.RecordPublicationCommit, err)
	}
	return PublicationClosureResult{PreparedDigest: preparedReceipt.Digest, CommitDigest: commitReceipt.Digest, Generation: generation}, nil
}

// reconcileUnknownPublicationOutcome observes the repository after an error
// from a placement boundary. A valid commit matching this operation is the
// only evidence that permits success; prepared-only or unverifiable evidence
// remains an explicit unknown outcome and must not be retried blindly.
func (s *Service) reconcileUnknownPublicationOutcome(ctx context.Context, driver repository.RecordDriver, publicationID, snapshotRef, planDigest string, role repository.RecordRole, cause error) (PublicationClosureResult, error) {
	reconcileCtx := context.WithoutCancel(ctx)
	if s != nil && s.TrustAnchor != nil && strings.TrimSpace(s.PublicationDomain) != "" {
		commits, err := listCommitMarkers(reconcileCtx, driver, *s.TrustAnchor, s.PublicationDomain)
		if err == nil {
			var match *committedPublication
			for i := range commits {
				candidate := commits[i]
				if candidate.Commit.PlanDigest != planDigest || candidate.Commit.PublicationID != publicationID || candidate.Commit.SnapshotRef != snapshotRef {
					continue
				}
				if match != nil && match.CommitDigest != candidate.CommitDigest {
					return PublicationClosureResult{}, &PublicationOutcomeError{
						PlanDigest: planDigest, SnapshotRef: snapshotRef, PublicationID: publicationID,
						Role: role, Cause: errors.New("conflicting committed publications for one execution key"),
					}
				}
				match = &candidate
			}
			if match != nil {
				return PublicationClosureResult{
					PreparedDigest: match.Commit.PreparedObjectDigest,
					CommitDigest:   match.CommitDigest,
					Generation:     match.Commit.Generation,
				}, nil
			}
		} else {
			cause = fmt.Errorf("observe committed publication: %w; original placement: %v", err, cause)
		}
	}
	return PublicationClosureResult{}, &PublicationOutcomeError{
		PlanDigest: planDigest, SnapshotRef: snapshotRef, PublicationID: publicationID,
		Role: role, Cause: cause,
	}
}

// reconcileUnknownChildOutcome observes one immutable post-publication child
// after PlaceRecord or VerifyRecord returned an error. The expected payload
// digest is known before placement, so an exact readback proves that the
// child committed; a missing, unreadable, or changed object remains an
// explicit unknown outcome and must not be retried blindly.
func (s *Service) reconcileUnknownChildOutcome(ctx context.Context, driver repository.RecordDriver, role repository.RecordRole, payload []byte, publicationID, snapshotRef, parentDigest, planDigest string, cause error) error {
	reconcileCtx := context.WithoutCancel(ctx)
	recordDigest := DigestBytes(payload)
	observed, err := readRecord(reconcileCtx, driver, role, recordDigest)
	if err == nil && bytes.Equal(observed, payload) {
		return nil
	}
	if err != nil {
		cause = fmt.Errorf("observe %s %s: %w; original placement: %v", role, recordDigest, err, cause)
	} else {
		cause = fmt.Errorf("observed %s %s differs from the signed payload; original placement: %v", role, recordDigest, cause)
	}
	return &PublicationOutcomeError{
		PlanDigest: planDigest, SnapshotRef: snapshotRef, PublicationID: publicationID,
		Role: role, Cause: cause,
	}
}

func stableReceipt(receipt repository.RecordReceipt) stableRecordReceipt {
	return stableRecordReceipt{RepositoryID: receipt.RepositoryID, Role: receipt.Role, Digest: receipt.Digest, Bytes: receipt.Bytes}
}

func nextPublicationGeneration(ctx context.Context, driver repository.RecordDriver, anchor TrustAnchor, domain string) (uint64, string, error) {
	commits, err := listCommitMarkers(ctx, driver, anchor, domain)
	if err != nil {
		return 0, "", err
	}
	if len(commits) == 0 {
		return 1, "", nil
	}
	latest := commits[len(commits)-1]
	return latest.Commit.Generation + 1, latest.CommitDigest, nil
}

func listCommitMarkers(ctx context.Context, driver repository.RecordDriver, anchor TrustAnchor, domain string) ([]committedPublication, error) {
	digests, err := driver.ListRecordDigests(ctx, repository.RecordPublicationCommit)
	if err != nil {
		return nil, err
	}
	byGeneration := make(map[uint64]string)
	commits := make([]committedPublication, 0, len(digests))
	for _, digest := range digests {
		payload, err := readRecord(ctx, driver, repository.RecordPublicationCommit, digest)
		if err != nil {
			return nil, err
		}
		var commit PublicationCommitRecord
		if err := decodeStrictRecord(payload, &commit); err != nil {
			return nil, fmt.Errorf("decode publication commit %s: %w", digest, err)
		}
		if commit.PublicationDomain != domain {
			continue
		}
		if commitDigest, err := commit.Digest(); err != nil || commitDigest != digest {
			return nil, fmt.Errorf("publication commit %s object digest mismatch", digest)
		}
		if err := commit.Verify(anchor); err != nil {
			return nil, fmt.Errorf("verify publication commit %s: %w", digest, err)
		}
		if commit.FenceToken == 0 {
			return nil, fmt.Errorf("publication commit %s has zero fencing token", digest)
		}
		if commit.TargetIdentity != driver.RepositoryIdentity() {
			return nil, fmt.Errorf("publication commit %s targets another repository", digest)
		}
		if previous, ok := byGeneration[commit.Generation]; ok && previous != digest {
			return nil, fmt.Errorf("conflicting publication commits at generation %d", commit.Generation)
		}
		byGeneration[commit.Generation] = digest
		commits = append(commits, committedPublication{CommitDigest: digest, Commit: commit})
	}
	sort.Slice(commits, func(i, j int) bool {
		if commits[i].Commit.Generation != commits[j].Commit.Generation {
			return commits[i].Commit.Generation < commits[j].Commit.Generation
		}
		return commits[i].CommitDigest < commits[j].CommitDigest
	})
	for i, commit := range commits {
		if i == 0 {
			if commit.Commit.Generation != 1 {
				return nil, fmt.Errorf("first publication commit has generation %d, want 1", commit.Commit.Generation)
			}
			if commit.Commit.ParentCommitDigest != "" {
				return nil, errors.New("genesis publication commit names a parent")
			}
			continue
		}
		previous := commits[i-1]
		if commit.Commit.Generation != previous.Commit.Generation+1 || commit.Commit.ParentCommitDigest != previous.CommitDigest {
			return nil, fmt.Errorf("publication commit generation %d breaks commit lineage", commit.Commit.Generation)
		}
		if commit.Commit.FenceToken <= previous.Commit.FenceToken {
			return nil, fmt.Errorf("publication commit generation %d has non-increasing fencing token", commit.Commit.Generation)
		}
	}
	return commits, nil
}

func readRecord(ctx context.Context, driver repository.RecordDriver, role repository.RecordRole, digest string) ([]byte, error) {
	body, err := driver.OpenRecord(ctx, role, digest)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	payload, err := io.ReadAll(io.LimitReader(body, portableRecordReadLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > portableRecordReadLimit {
		return nil, errors.New("portable recovery record exceeds reader limit")
	}
	if DigestBytes(payload) != digest {
		return nil, fmt.Errorf("portable record %s digest mismatch", digest)
	}
	receipt := repository.RecordReceipt{
		RepositoryID: driver.RepositoryIdentity(), Role: role, Digest: digest, Bytes: int64(len(payload)),
	}
	if err := driver.VerifyRecord(ctx, receipt); err != nil {
		return nil, err
	}
	return payload, nil
}

func decodeStrictRecord(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("record contains more than one JSON value")
		}
		return err
	}
	return nil
}

func (s *Service) committedPublications(ctx context.Context) ([]committedPublication, error) {
	if s.TrustAnchor == nil || strings.TrimSpace(s.PublicationDomain) == "" {
		return nil, errors.New("committed publication discovery requires an independent trust anchor and publication domain")
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return nil, err
	}
	markers, err := listCommitMarkers(ctx, driver, *s.TrustAnchor, s.PublicationDomain)
	if err != nil {
		return nil, err
	}
	for i := range markers {
		preparedBytes, err := readRecord(ctx, driver, repository.RecordPreparedClosure, markers[i].Commit.PreparedObjectDigest)
		if err != nil {
			return nil, err
		}
		var envelope PreparedClosureEnvelope
		if err := decodeStrictRecord(preparedBytes, &envelope); err != nil {
			return nil, fmt.Errorf("decode prepared closure %s: %w", markers[i].Commit.PreparedObjectDigest, err)
		}
		if err := validatePreparedEnvelope(driver, *s.TrustAnchor, markers[i].Commit, envelope, int64(len(preparedBytes))); err != nil {
			return nil, fmt.Errorf("validate publication %s: %w", markers[i].CommitDigest, err)
		}
		markers[i].Prepared = envelope
		markers[i].Manifest = envelope.Manifest
	}
	return markers, nil
}

func validatePreparedEnvelope(driver repository.RecordDriver, anchor TrustAnchor, commit PublicationCommitRecord, envelope PreparedClosureEnvelope, preparedLength int64) error {
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
	p := envelope.Prepared
	if p.PublicationID != commit.PublicationID || p.PublicationDomain != commit.PublicationDomain ||
		p.Generation != commit.Generation || p.SnapshotRef != commit.SnapshotRef ||
		p.RRFRootDigest != commit.RRFRootDigest || p.ManifestDigest != commit.ManifestDigest ||
		p.PayloadReceiptDigest != commit.PayloadReceiptDigest || p.PayloadReceiptLength != commit.PayloadReceiptLength ||
		p.PayloadReceiptObjectCount != commit.PayloadReceiptObjectCount || p.PlanDigest != commit.PlanDigest ||
		p.CaptureDigest != commit.CaptureDigest || p.PolicyDigest != commit.PolicyDigest ||
		p.VerificationDigest != commit.VerificationDigest || p.TargetIdentity != commit.TargetIdentity ||
		p.FenceToken != commit.FenceToken || p.ParentCommitDigest != commit.ParentCommitDigest {
		return errors.New("prepared closure and publication commit bindings differ")
	}
	preparedReceiptDigest, err := DigestCanonicalJSON(stableRecordReceipt{
		RepositoryID: driver.RepositoryIdentity(), Role: repository.RecordPreparedClosure,
		Digest: commit.PreparedObjectDigest, Bytes: preparedLength,
	})
	if err != nil || preparedReceiptDigest != commit.PreparedReceiptDigest {
		return errors.New("prepared closure placement receipt digest mismatch")
	}
	if err := validatePayloadReceipt(envelope.PayloadReceipt); err != nil {
		return err
	}
	if envelope.PayloadReceipt.RepositoryID != driver.RepositoryIdentity() ||
		envelope.PayloadReceipt.PublicationID != commit.PublicationID || envelope.PayloadReceipt.SnapshotRef != commit.SnapshotRef {
		return errors.New("payload receipt publication binding mismatch")
	}
	payloadDigest, _ := DigestCanonicalJSON(envelope.PayloadReceipt)
	if payloadDigest != commit.PayloadReceiptDigest || envelope.PayloadReceipt.TotalBytes != commit.PayloadReceiptLength ||
		int64(len(envelope.PayloadReceipt.Objects)) != commit.PayloadReceiptObjectCount {
		return errors.New("payload receipt digest or totals mismatch")
	}
	verificationDigest, _ := DigestCanonicalJSON(envelope.VerificationEvidence)
	wantEvidenceSchema := MetadataEvidenceSchemaV1
	if envelope.Schema == PreparedEnvelopeSchemaV2 {
		wantEvidenceSchema = MetadataEvidenceSchemaV2
	}
	if envelope.VerificationEvidence.Schema != wantEvidenceSchema || verificationDigest != commit.VerificationDigest {
		return errors.New("authenticated metadata evidence mismatch")
	}
	if envelope.Schema == PreparedEnvelopeSchemaV2 {
		if envelope.VerificationEvidence.PortableFactsSchema != PortableFactsSchemaV1 ||
			envelope.VerificationEvidence.PortableFactsDigest == "" ||
			envelope.VerificationEvidence.PortableFactsEntryCount != int64(len(envelope.Manifest.Entries)) ||
			envelope.VerificationEvidence.PortableFactsCoverage != portableFactsCoveragePartial ||
			!sameStrings(envelope.VerificationEvidence.PortableFactsOmissions, portableFactsV1Omissions) {
			return errors.New("portable file-fact coverage declaration is invalid")
		}
	}
	if err := authenticateManifest(envelope.Manifest); err != nil {
		return err
	}
	manifestDigest, err := envelope.Manifest.Digest()
	if err != nil || manifestDigest != envelope.Manifest.ManifestDigest || manifestDigest != commit.ManifestDigest {
		return errors.New("manifest digest differs from signed publication")
	}
	if envelope.Manifest.SnapshotRef != commit.SnapshotRef || envelope.VerificationEvidence.ManifestDigest != manifestDigest {
		return errors.New("manifest publication binding mismatch")
	}
	if err := compareManifestPayloadReceipt(envelope.Manifest, envelope.PayloadReceipt); err != nil {
		return err
	}
	wantEvidence, err := buildMetadataEvidence(envelope.Manifest)
	if err != nil {
		return err
	}
	wantEvidenceDigest, _ := DigestCanonicalJSON(wantEvidence)
	if wantEvidenceDigest != verificationDigest {
		return errors.New("authenticated metadata evidence does not describe the manifest")
	}
	return nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func compareManifestPayloadReceipt(manifest Manifest, receipt PayloadReceipt) error {
	want := make(map[string]int64)
	for _, entry := range manifest.Entries {
		if sqlite.NamespaceEntryType(entry.EntryType) != sqlite.EntryFile || entry.Protection.LocalRepresentationID == "" {
			continue
		}
		if entry.ContentID == "" || entry.Protection.ExpectedLogicalLength == nil {
			return fmt.Errorf("locally protected manifest entry %q lacks content identity or length", entry.RelativePath)
		}
		if !validExactContentID(entry.ContentID) {
			return fmt.Errorf("locally protected manifest entry %q has invalid content identity", entry.RelativePath)
		}
		length := *entry.Protection.ExpectedLogicalLength
		if previous, ok := want[entry.ContentID]; ok && previous != length {
			return fmt.Errorf("manifest content %s has conflicting lengths", entry.ContentID)
		}
		want[entry.ContentID] = length
	}
	if len(want) != len(receipt.Objects) {
		return errors.New("payload receipt object set differs from manifest")
	}
	for _, object := range receipt.Objects {
		expectedLength, ok := want[object.ContentID]
		if !ok || expectedLength != object.LogicalBytes {
			return fmt.Errorf("payload receipt for %s differs from manifest", object.ContentID)
		}
	}
	return nil
}

func (s *Service) loadManifest(ctx context.Context, snapshotRef string) (Manifest, error) {
	if !s.signedPublicationEnabled() {
		return readManifest(s.Repo.Root(), snapshotRef)
	}
	publication, err := s.committedPublicationForSnapshot(ctx, snapshotRef)
	if err != nil {
		return Manifest{}, err
	}
	return publication.Manifest, nil
}

func (s *Service) committedPublicationForSnapshot(ctx context.Context, snapshotRef string) (committedPublication, error) {
	publications, err := s.committedPublications(ctx)
	if err != nil {
		return committedPublication{}, err
	}
	var found *committedPublication
	for i := range publications {
		if publications[i].Manifest.SnapshotRef != snapshotRef {
			continue
		}
		if found != nil {
			return committedPublication{}, fmt.Errorf("conflicting committed publications for snapshot %s", snapshotRef)
		}
		copy := publications[i]
		found = &copy
	}
	if found == nil {
		return committedPublication{}, fmt.Errorf("%w: committed snapshot %s", repository.ErrNotFound, snapshotRef)
	}
	return *found, nil
}
