package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// RecoveryReferenceSchemaV2 is an independently retainable selector and
// validation envelope. It is deliberately separate from the legacy v1 export
// bundle, whose shape contains only a commit and prepared closure.
const RecoveryReferenceSchemaV2 = "org.restoreweave.recovery-reference.v2"

const (
	RecoveryFactHealthComplete   = "COMPLETE"
	RecoveryFactHealthIncomplete = "INCOMPLETE"
)

// RecoveryRepositoryBinding identifies the repository profile and identity
// expected by a clean reader. The path itself is intentionally not portable;
// the operator supplies the relocated repository target separately.
type RecoveryRepositoryBinding struct {
	RepositoryID string `json:"repository_id"`
	Profile      string `json:"repository_profile"`
	Compression  string `json:"compression_profile"`
}

// RecoveryTrustAnchorBinding describes the independently retained public
// anchor required to authenticate the embedded signed records. It is not a
// trust root and contains no key material.
type RecoveryTrustAnchorBinding struct {
	PublicationDomain string `json:"publication_domain"`
	KeyID             string `json:"key_id"`
	Digest            string `json:"digest"`
}

// RecoveryFactClosureReference carries the complete signed fact successor
// chain. Attachment bytes remain repository objects addressed by the signed
// descriptors in Envelope.Bundle.
type RecoveryFactClosureReference struct {
	RecordDigest  string                      `json:"record_digest"`
	ClosureDigest string                      `json:"closure_digest"`
	Envelope      PortableFactClosureEnvelope `json:"envelope"`
}

// RecoveryReference is a bounded, self-digested clean-reader input. It
// contains the signed publication root and the latest complete portable-fact
// chain available when it was exported. Exact restore can still be performed
// when FactHealth is INCOMPLETE, but a reader must expose that degraded state.
type RecoveryReference struct {
	Schema                     string                         `json:"schema"`
	SnapshotRef                string                         `json:"snapshot_ref"`
	PublicationDomain          string                         `json:"publication_domain"`
	Repository                 RecoveryRepositoryBinding      `json:"repository"`
	RequiredTrustAnchor        RecoveryTrustAnchorBinding     `json:"required_trust_anchor"`
	PublicationCommitDigest    string                         `json:"publication_commit_digest"`
	PublicationCommit          PublicationCommitRecord        `json:"publication_commit"`
	PreparedClosureDigest      string                         `json:"prepared_closure_digest"`
	PreparedClosure            PreparedClosureEnvelope        `json:"prepared_closure"`
	FactHealth                 string                         `json:"fact_health"`
	PortableFactClosures       []RecoveryFactClosureReference `json:"portable_fact_closures"`
	RequiredReaderDependencies []string                       `json:"required_reader_dependencies"`
	CriticalExtensions         []string                       `json:"critical_extensions"`
	OptionalExtensions         json.RawMessage                `json:"optional_extensions"`
	SelfDigest                 string                         `json:"self_digest,omitempty"`
}

// BuildRecoveryReference creates a v2 reference from an authenticated
// publication. It does not consult SQLite and never writes repository state.
// A missing portable-fact child is represented explicitly as INCOMPLETE;
// malformed or conflicting child records fail closed.
func (s *Service) BuildRecoveryReference(ctx context.Context, snapshotRef string) (RecoveryReference, error) {
	var reference RecoveryReference
	if err := s.requireRepository(); err != nil {
		return reference, err
	}
	if strings.TrimSpace(snapshotRef) == "" {
		return reference, errors.New("snapshot reference is required")
	}
	if !s.signedPublicationEnabled() || s.TrustAnchor == nil {
		return reference, errors.New("recovery reference requires signed publication and trust anchor")
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return reference, err
	}
	publications, err := s.committedPublications(ctx)
	if err != nil {
		return reference, err
	}
	var selected *committedPublication
	for i := range publications {
		if publications[i].Manifest.SnapshotRef != snapshotRef {
			continue
		}
		if selected != nil {
			return reference, fmt.Errorf("conflicting committed publications for snapshot %s", snapshotRef)
		}
		copy := publications[i]
		selected = &copy
	}
	if selected == nil {
		return reference, fmt.Errorf("committed snapshot %s is unavailable", snapshotRef)
	}
	anchorDigest, err := DigestCanonicalJSON(*s.TrustAnchor)
	if err != nil {
		return reference, err
	}
	profile := repository.DescribeProfile(s.Repo)
	closures, err := listPortableFactClosures(ctx, s.Repo, driver, *s.TrustAnchor, s.PublicationDomain, selected.CommitDigest)
	if err != nil {
		return reference, err
	}
	deps := portableFactReaderDependencies(s.Repo)
	reference = RecoveryReference{
		Schema:                     RecoveryReferenceSchemaV2,
		SnapshotRef:                snapshotRef,
		PublicationDomain:          s.PublicationDomain,
		Repository:                 RecoveryRepositoryBinding{RepositoryID: driver.RepositoryIdentity(), Profile: profile.Repository, Compression: profile.Compression},
		RequiredTrustAnchor:        RecoveryTrustAnchorBinding{PublicationDomain: s.PublicationDomain, KeyID: s.TrustAnchor.KeyID, Digest: anchorDigest},
		PublicationCommitDigest:    selected.CommitDigest,
		PublicationCommit:          selected.Commit,
		PreparedClosureDigest:      selected.Commit.PreparedObjectDigest,
		PreparedClosure:            selected.Prepared,
		FactHealth:                 RecoveryFactHealthIncomplete,
		PortableFactClosures:       make([]RecoveryFactClosureReference, 0, len(closures)),
		RequiredReaderDependencies: append([]string(nil), deps...),
		CriticalExtensions:         []string{},
		OptionalExtensions:         json.RawMessage(`{}`),
	}
	for _, envelope := range closures {
		envelopeBytes, err := CanonicalJSON(envelope)
		if err != nil {
			return RecoveryReference{}, err
		}
		closureDigest, err := envelope.Closure.Digest()
		if err != nil {
			return RecoveryReference{}, err
		}
		reference.PortableFactClosures = append(reference.PortableFactClosures, RecoveryFactClosureReference{
			RecordDigest: DigestBytes(envelopeBytes), ClosureDigest: closureDigest, Envelope: envelope,
		})
	}
	if len(reference.PortableFactClosures) > 0 {
		reference.FactHealth = RecoveryFactHealthComplete
	}
	if err := reference.validateShape(); err != nil {
		return RecoveryReference{}, err
	}
	return reference, nil
}

// Digest returns the self-digest over the canonical reference with
// self_digest omitted. This avoids a digest cycle while binding every other
// field, including extension containers and embedded signed records.
func (reference RecoveryReference) Digest() (string, error) {
	copy := reference
	copy.SelfDigest = ""
	if err := copy.validateShape(); err != nil {
		return "", err
	}
	payload, err := CanonicalJSON(copy)
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// MarshalRecoveryReference returns canonical bounded JSON with SelfDigest
// populated. It refuses to serialize an invalid reference.
func MarshalRecoveryReference(reference RecoveryReference) ([]byte, error) {
	reference.SelfDigest = ""
	digest, err := reference.Digest()
	if err != nil {
		return nil, err
	}
	reference.SelfDigest = digest
	return CanonicalJSON(reference)
}

// DecodeRecoveryReference parses exactly one bounded v2 reference. Optional
// extensions are carried inside the explicit container; unknown top-level
// fields are rejected so a reader cannot accidentally ignore a new critical
// field.
func DecodeRecoveryReference(payload []byte) (RecoveryReference, error) {
	var reference RecoveryReference
	if len(payload) == 0 || int64(len(payload)) > portableRecordReadLimit {
		return reference, fmt.Errorf("recovery reference exceeds %d bytes", portableRecordReadLimit)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&reference); err != nil {
		return reference, fmt.Errorf("decode recovery reference: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return reference, errors.New("recovery reference contains more than one JSON value")
		}
		return reference, err
	}
	if err := reference.validateShape(); err != nil {
		return reference, err
	}
	digest, err := reference.Digest()
	if err != nil {
		return reference, err
	}
	if reference.SelfDigest == "" || reference.SelfDigest != digest {
		return reference, errors.New("recovery reference self digest mismatch")
	}
	return reference, nil
}

// LoadRecoveryReference reads exactly one bounded v2 reference from disk. It
// shares the portable-record limit with repository readers so daemon startup
// cannot allocate an unbounded operator-supplied artifact.
func LoadRecoveryReference(path string) (RecoveryReference, error) {
	var reference RecoveryReference
	if strings.TrimSpace(path) == "" {
		return reference, errors.New("recovery reference path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return reference, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, portableRecordReadLimit+1))
	if err != nil {
		return reference, err
	}
	if int64(len(payload)) > portableRecordReadLimit {
		return reference, fmt.Errorf("recovery reference exceeds %d bytes", portableRecordReadLimit)
	}
	return DecodeRecoveryReference(payload)
}

// Validate authenticates the embedded publication and fact chain against an
// independently supplied anchor. It does not read a repository; callers that
// need payload and attachment readback should use ValidateAgainstRepository.
func (reference RecoveryReference) Validate(anchor TrustAnchor) error {
	if err := reference.validateShape(); err != nil {
		return err
	}
	anchorDigest, err := DigestCanonicalJSON(anchor)
	if err != nil {
		return err
	}
	if reference.RequiredTrustAnchor.Digest != anchorDigest || reference.RequiredTrustAnchor.KeyID != anchor.KeyID || reference.RequiredTrustAnchor.PublicationDomain != anchor.PublicationDomain {
		return fmt.Errorf("%w: supplied anchor does not match recovery reference", ErrRecoveryTrustAnchor)
	}
	if err := reference.PublicationCommit.Verify(anchor); err != nil {
		return err
	}
	if err := reference.PreparedClosure.Prepared.Verify(anchor); err != nil {
		return err
	}
	if err := validateReferencePublicationBinding(reference); err != nil {
		return err
	}
	return validateReferenceFactChain(reference, anchor)
}

// ValidateAgainstRepository extends Validate with profile/identity, signed
// record placement, payload receipt, and attachment readback checks.
func (reference RecoveryReference) ValidateAgainstRepository(ctx context.Context, repo repository.Driver, anchor TrustAnchor) error {
	if repo == nil {
		return errors.New("repository is required")
	}
	if err := reference.Validate(anchor); err != nil {
		return err
	}
	driver, ok := repo.(repository.RecordDriver)
	if !ok {
		return errors.New("repository does not support recovery records")
	}
	profile := repository.DescribeProfile(repo)
	if reference.Repository.RepositoryID != driver.RepositoryIdentity() || reference.Repository.Profile != profile.Repository || reference.Repository.Compression != profile.Compression {
		return errors.New("recovery reference repository binding mismatch")
	}
	if reference.PublicationCommit.TargetIdentity != driver.RepositoryIdentity() {
		return errors.New("publication target identity differs from repository")
	}
	if !sameStrings(reference.RequiredReaderDependencies, portableFactReaderDependencies(repo)) {
		return errors.New("recovery reference reader dependencies are unavailable")
	}
	commitPayload, err := readRecoveryRecord(ctx, driver, repository.RecordPublicationCommit, reference.PublicationCommitDigest)
	if err != nil {
		return err
	}
	if DigestBytes(commitPayload) != reference.PublicationCommitDigest {
		return errors.New("publication commit placement digest mismatch")
	}
	var commit PublicationCommitRecord
	if err := decodeStrictRecord(commitPayload, &commit); err != nil {
		return err
	}
	commitDigest, err := commit.Digest()
	if err != nil || commitDigest != reference.PublicationCommitDigest {
		return errors.New("publication commit bytes differ from reference")
	}
	preparedPayload, err := readRecoveryRecord(ctx, driver, repository.RecordPreparedClosure, reference.PublicationCommit.PreparedObjectDigest)
	if err != nil {
		return err
	}
	if DigestBytes(preparedPayload) != reference.PreparedClosureDigest {
		return errors.New("prepared closure placement digest mismatch")
	}
	if err := validatePreparedEnvelope(driver, anchor, reference.PublicationCommit, reference.PreparedClosure, int64(len(preparedPayload))); err != nil {
		return err
	}
	for _, fact := range reference.PortableFactClosures {
		payload, err := readRecoveryRecord(ctx, driver, repository.RecordPortableFactClosure, fact.RecordDigest)
		if err != nil {
			return err
		}
		if DigestBytes(payload) != fact.RecordDigest {
			return errors.New("portable fact closure placement digest mismatch")
		}
		canonical, err := CanonicalJSON(fact.Envelope)
		if err != nil || !bytes.Equal(payload, canonical) {
			return errors.New("portable fact closure bytes differ from reference")
		}
		bundle, err := validatePortableFactBundle(fact.Envelope.Bundle, fact.Envelope.Closure.WorkspaceID, fact.Envelope.Closure.SnapshotRef)
		if err != nil {
			return err
		}
		if err := validateReferenceProcessorAttemptChild(ctx, driver, anchor, fact.Envelope.Closure, bundle); err != nil {
			return fmt.Errorf("portable processor-attempt child: %w", err)
		}
		for _, attachment := range bundle.Attachments {
			if err := repo.Verify(ctx, attachment.ContentID); err != nil {
				return fmt.Errorf("portable fact attachment %s: %w", attachment.AttachmentID, err)
			}
			body, err := repo.Open(ctx, attachment.ContentID)
			if err != nil {
				return err
			}
			length, readErr := io.Copy(io.Discard, body)
			closeErr := body.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil || length != attachment.LogicalLength {
				return fmt.Errorf("portable fact attachment %s length mismatch", attachment.AttachmentID)
			}
		}
	}
	return nil
}

// ExportRecoveryReference writes the v2 typed reference without overwriting an
// existing destination. ExportRecovery remains the compatibility v1 writer.
func (s *Service) ExportRecoveryReference(ctx context.Context, snapshotRef, destination string) (ExportResult, error) {
	var result ExportResult
	if strings.TrimSpace(destination) == "" {
		return result, errors.New("export destination is required")
	}
	reference, err := s.BuildRecoveryReference(ctx, snapshotRef)
	if err != nil {
		return result, err
	}
	payload, err := MarshalRecoveryReference(reference)
	if err != nil {
		return result, err
	}
	path, err := writeNewRecoveryFile(destination, payload)
	if err != nil {
		return result, err
	}
	files, bytes := manifestFileTotals(reference.PreparedClosure.Manifest)
	return ExportResult{SnapshotRef: snapshotRef, Schema: RecoveryReferenceSchemaV2, ManifestDigest: reference.PreparedClosure.Manifest.ManifestDigest, ArtifactPath: path, Length: int64(len(payload)), Files: files, Bytes: bytes, IndependentlyStored: true}, nil
}

func writeNewRecoveryFile(destination string, payload []byte) (string, error) {
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return "", err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%w: export destination already exists", ErrBlocked)
		}
		return "", err
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		_ = os.Remove(absolute)
		return "", err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(absolute)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(absolute)
		return "", err
	}
	return absolute, nil
}

func (reference RecoveryReference) validateShape() error {
	if reference.Schema != RecoveryReferenceSchemaV2 || strings.TrimSpace(reference.SnapshotRef) == "" || strings.TrimSpace(reference.PublicationDomain) == "" {
		return fmt.Errorf("%w: recovery reference schema or identity is invalid", ErrRecoveryRecordInvalid)
	}
	for name, value := range map[string]string{
		"repository id": reference.Repository.RepositoryID, "repository profile": reference.Repository.Profile,
		"compression profile": reference.Repository.Compression, "anchor domain": reference.RequiredTrustAnchor.PublicationDomain,
		"anchor key id": reference.RequiredTrustAnchor.KeyID, "anchor digest": reference.RequiredTrustAnchor.Digest,
		"publication commit digest": reference.PublicationCommitDigest, "prepared closure digest": reference.PreparedClosureDigest,
	} {
		if strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
			return fmt.Errorf("%w: %s is required", ErrRecoveryRecordInvalid, name)
		}
	}
	if reference.RequiredTrustAnchor.PublicationDomain != reference.PublicationDomain || !validExactContentID(reference.PublicationCommitDigest) || !validExactContentID(reference.PreparedClosureDigest) {
		return fmt.Errorf("%w: recovery reference digest or domain binding is invalid", ErrRecoveryRecordInvalid)
	}
	if reference.FactHealth != RecoveryFactHealthComplete && reference.FactHealth != RecoveryFactHealthIncomplete {
		return fmt.Errorf("%w: recovery fact health is invalid", ErrRecoveryRecordInvalid)
	}
	if reference.RequiredReaderDependencies == nil || reference.CriticalExtensions == nil || reference.OptionalExtensions == nil || !json.Valid(reference.OptionalExtensions) {
		return fmt.Errorf("%w: recovery reference extensions or dependencies are invalid", ErrRecoveryRecordInvalid)
	}
	if err := validateRecoveryExtensions(reference.CriticalExtensions, reference.OptionalExtensions); err != nil {
		return err
	}
	commitDigest, err := reference.PublicationCommit.Digest()
	if err != nil || commitDigest != reference.PublicationCommitDigest {
		return errors.New("recovery reference publication commit digest mismatch")
	}
	preparedDigest, err := DigestCanonicalJSON(reference.PreparedClosure)
	if err != nil || preparedDigest != reference.PreparedClosureDigest {
		return errors.New("recovery reference prepared closure digest mismatch")
	}
	if reference.PublicationCommit.SnapshotRef != reference.SnapshotRef || reference.PublicationCommit.PublicationDomain != reference.PublicationDomain || reference.PreparedClosure.Manifest.SnapshotRef != reference.SnapshotRef {
		return errors.New("recovery reference snapshot or publication binding mismatch")
	}
	if len(reference.PortableFactClosures) == 0 && reference.FactHealth == RecoveryFactHealthComplete {
		return errors.New("complete recovery fact health requires a portable fact closure")
	}
	if len(reference.PortableFactClosures) > 0 && reference.FactHealth != RecoveryFactHealthComplete {
		return errors.New("portable fact closures require complete recovery fact health")
	}
	return nil
}

func validateRecoveryExtensions(critical []string, optional json.RawMessage) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(optional, &object); err != nil || object == nil {
		return errors.New("optional extensions must be a JSON object")
	}
	for _, extension := range critical {
		if strings.TrimSpace(extension) == "" {
			return errors.New("critical extension name is empty")
		}
		return fmt.Errorf("unsupported critical extension %q", extension)
	}
	return nil
}

func validateReferencePublicationBinding(reference RecoveryReference) error {
	commit := reference.PublicationCommit
	prepared := reference.PreparedClosure.Prepared
	if prepared.PublicationID != commit.PublicationID || prepared.PublicationDomain != commit.PublicationDomain || prepared.Generation != commit.Generation || prepared.SnapshotRef != commit.SnapshotRef || prepared.RRFRootDigest != commit.RRFRootDigest || prepared.ManifestDigest != commit.ManifestDigest || prepared.PayloadReceiptDigest != commit.PayloadReceiptDigest || prepared.PayloadReceiptLength != commit.PayloadReceiptLength || prepared.PayloadReceiptObjectCount != commit.PayloadReceiptObjectCount || prepared.PlanDigest != commit.PlanDigest || prepared.CaptureDigest != commit.CaptureDigest || prepared.PolicyDigest != commit.PolicyDigest || prepared.VerificationDigest != commit.VerificationDigest || prepared.TargetIdentity != commit.TargetIdentity || prepared.FenceToken != commit.FenceToken || prepared.ParentCommitDigest != commit.ParentCommitDigest {
		return errors.New("prepared closure and publication commit bindings differ")
	}
	if err := authenticateManifest(reference.PreparedClosure.Manifest); err != nil {
		return err
	}
	manifestDigest, err := reference.PreparedClosure.Manifest.Digest()
	if err != nil || manifestDigest != commit.ManifestDigest || manifestDigest != reference.PreparedClosure.Manifest.ManifestDigest {
		return errors.New("recovery reference manifest digest mismatch")
	}
	if err := validatePayloadReceipt(reference.PreparedClosure.PayloadReceipt); err != nil {
		return err
	}
	payloadDigest, err := DigestCanonicalJSON(reference.PreparedClosure.PayloadReceipt)
	if err != nil || payloadDigest != commit.PayloadReceiptDigest || reference.PreparedClosure.PayloadReceipt.TotalBytes != commit.PayloadReceiptLength || int64(len(reference.PreparedClosure.PayloadReceipt.Objects)) != commit.PayloadReceiptObjectCount {
		return errors.New("recovery reference payload receipt mismatch")
	}
	evidenceDigest, err := DigestCanonicalJSON(reference.PreparedClosure.VerificationEvidence)
	if err != nil || evidenceDigest != commit.VerificationDigest {
		return errors.New("recovery reference verification evidence mismatch")
	}
	wantEvidence, err := buildMetadataEvidence(reference.PreparedClosure.Manifest)
	if err != nil {
		return err
	}
	wantEvidenceDigest, err := DigestCanonicalJSON(wantEvidence)
	if err != nil || wantEvidenceDigest != evidenceDigest {
		return errors.New("recovery reference evidence does not describe manifest")
	}
	return compareManifestPayloadReceipt(reference.PreparedClosure.Manifest, reference.PreparedClosure.PayloadReceipt)
}

func validateReferenceFactChain(reference RecoveryReference, anchor TrustAnchor) error {
	if len(reference.PortableFactClosures) == 0 {
		return nil
	}
	previous := ""
	for index, fact := range reference.PortableFactClosures {
		if !validExactContentID(fact.RecordDigest) || !validExactContentID(fact.ClosureDigest) {
			return errors.New("portable fact reference digest is invalid")
		}
		recordDigest, err := DigestCanonicalJSON(fact.Envelope)
		if err != nil || recordDigest != fact.RecordDigest {
			return errors.New("portable fact reference envelope digest mismatch")
		}
		closureDigest, err := fact.Envelope.Closure.Digest()
		if err != nil || closureDigest != fact.ClosureDigest {
			return errors.New("portable fact reference closure digest mismatch")
		}
		if err := fact.Envelope.Closure.Verify(anchor); err != nil {
			return err
		}
		closure := fact.Envelope.Closure
		if closure.ParentCommitDigest != reference.PublicationCommitDigest || closure.PublicationDomain != reference.PublicationDomain || closure.SnapshotRef != reference.SnapshotRef || !sameStrings(closure.RequiredReaderDependencies, reference.RequiredReaderDependencies) {
			return errors.New("portable fact reference parent or dependency binding mismatch")
		}
		if index == 0 {
			if closure.ClosureSequence != 1 || closure.PredecessorClosureDigest != "" {
				return errors.New("portable fact reference sequence one is invalid")
			}
		} else if closure.ClosureSequence != uint64(index+1) || closure.PredecessorClosureDigest != previous {
			return errors.New("portable fact reference successor lineage is invalid")
		}
		bundle, err := validatePortableFactBundle(fact.Envelope.Bundle, closure.WorkspaceID, closure.SnapshotRef)
		if err != nil {
			return err
		}
		if DigestBytes(fact.Envelope.Bundle) != closure.BundleDigest || int64(len(fact.Envelope.Bundle)) != closure.BundleLength || int64(len(bundle.Records)) != closure.RecordCount || int64(len(bundle.Attachments)) != closure.AttachmentCount {
			return errors.New("portable fact reference bundle binding mismatch")
		}
		if err := validatePortableFactRecords(bundle); err != nil {
			return err
		}
		previous = closureDigest
	}
	return nil
}

// validateReferenceProcessorAttemptChild resolves the child by the closure
// digest stored in PORTABLE_FACT_CLOSURE. The portable-fact publisher records
// the signed child closure digest (rather than its envelope placement digest),
// so a reference reader must resolve and authenticate that signed child before
// accepting any admitted artifact descriptors.
func validateReferenceProcessorAttemptChild(ctx context.Context, driver repository.RecordDriver, anchor TrustAnchor, closure PortableFactClosureRecord, bundle portableFactBundle) error {
	hasArtifact := false
	for _, record := range bundle.Records {
		if record.RecordKind == "PROCESSOR_ARTIFACT_DESCRIPTOR" {
			hasArtifact = true
			break
		}
	}
	if !hasArtifact && closure.ProcessorAttemptDigest == "" {
		return nil
	}
	if closure.ProcessorAttemptDigest == "" {
		return errors.New("portable processor artifact facts lack processor-attempt child")
	}
	digests, err := driver.ListRecordDigests(ctx, repository.RecordProcessorAttemptClosure)
	if err != nil {
		return err
	}
	for _, digest := range digests {
		payload, err := readRecoveryRecord(ctx, driver, repository.RecordProcessorAttemptClosure, digest)
		if err != nil {
			return err
		}
		var envelope ProcessorAttemptClosureEnvelope
		if err := decodeStrictRecord(payload, &envelope); err != nil {
			return err
		}
		if envelope.Schema != ProcessorAttemptClosureEnvelopeSchemaV1 {
			return errors.New("unsupported processor attempt closure envelope schema")
		}
		// ProcessorAttemptDigest is the signed envelope object digest (the
		// record key), not the closure-field digest. Match the record we just
		// read by its object digest.
		if digest != closure.ProcessorAttemptDigest {
			continue
		}
		if err := envelope.Closure.Verify(anchor); err != nil {
			return err
		}
		if envelope.Closure.ParentCommitDigest != closure.ParentCommitDigest ||
			envelope.Closure.WorkspaceID != closure.WorkspaceID ||
			envelope.Closure.PublicationID != closure.PublicationID ||
			envelope.Closure.PublicationDomain != closure.PublicationDomain ||
			envelope.Closure.SnapshotRef != closure.SnapshotRef ||
			envelope.Closure.ManifestDigest != closure.ManifestDigest ||
			envelope.Closure.TargetIdentity != closure.TargetIdentity ||
			envelope.Closure.FenceToken != closure.FenceToken {
			return errors.New("portable processor-attempt child binding mismatch")
		}
		attempts, err := validateProcessorAttemptBundle(envelope.Bundle, closure.WorkspaceID, closure.SnapshotRef)
		if err != nil {
			return err
		}
		if attempts.Schema != envelope.Closure.AttemptBundleSchema ||
			DigestBytes(envelope.Bundle) != envelope.Closure.AttemptBundleDigest ||
			int64(len(envelope.Bundle)) != envelope.Closure.AttemptBundleLength ||
			int64(len(attempts.Attempts)) != envelope.Closure.AttemptCount {
			return errors.New("portable processor-attempt bundle binding mismatch")
		}
		artifactAttempts := make(map[string]sqlite.ProcessorAttemptExport)
		for _, attempt := range attempts.Attempts {
			for _, artifactID := range attempt.ArtifactRefs {
				if _, exists := artifactAttempts[artifactID]; exists {
					return errors.New("portable processor artifact is referenced by multiple attempts")
				}
				artifactAttempts[artifactID] = attempt
			}
		}
		for _, record := range bundle.Records {
			if record.RecordKind != "PROCESSOR_ARTIFACT_DESCRIPTOR" {
				continue
			}
			var artifact artifactPortablePayload
			if err := decodeStrictRecord(record.Payload, &artifact); err != nil {
				return err
			}
			attempt, exists := artifactAttempts[artifact.ID]
			if !exists || attempt.AttemptID != artifact.AttemptID || attempt.SubjectRef != artifact.SubjectRef || attempt.FenceToken != artifact.FenceToken || attempt.ProcessorDigest != artifact.ProducerDigest {
				return errors.New("portable processor artifact is not admitted by its authenticated attempt")
			}
			delete(artifactAttempts, artifact.ID)
		}
		if len(artifactAttempts) != 0 {
			return errors.New("authenticated processor attempt names a missing portable artifact descriptor")
		}
		return nil
	}
	return errors.New("portable processor-attempt child is missing")
}

func readRecoveryRecord(ctx context.Context, driver repository.RecordDriver, role repository.RecordRole, digest string) ([]byte, error) {
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
		return nil, errors.New("portable recovery record exceeds read limit")
	}
	receipt := repository.RecordReceipt{RepositoryID: driver.RepositoryIdentity(), Role: role, Digest: digest, Bytes: int64(len(payload))}
	if err := driver.VerifyRecord(ctx, receipt); err != nil {
		return nil, err
	}
	return payload, nil
}
