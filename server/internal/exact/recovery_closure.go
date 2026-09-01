package exact

import (
	"crypto"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	RecoveryTrustAnchorSchemaV1     = "org.restoreweave.trust-anchor.v1"
	PreparedClosureSchemaV1         = "org.restoreweave.prepared-closure.v1"
	PublicationCommitSchemaV1       = "org.restoreweave.publication-commit.v1"
	ProcessorAttemptClosureSchemaV1 = "org.restoreweave.processor-attempt-closure.v1"
	ProcessorAttemptClosureSchemaV2 = "org.restoreweave.processor-attempt-closure.v2"
	PortableFactClosureSchemaV1     = "org.restoreweave.portable-fact-closure.v1"
	PortableFactClosureSchemaV2     = "org.restoreweave.portable-fact-closure.v2"
	RecoverySignatureDomainV1       = "org.restoreweave.rw-mvp-1.recovery.v1"

	PreparedClosureKind         = "PREPARED_CLOSURE"
	PublicationCommitKind       = "PUBLICATION_COMMIT"
	ProcessorAttemptClosureKind = "PROCESSOR_ATTEMPT_CLOSURE"
	PortableFactClosureKind     = "PORTABLE_FACT_CLOSURE"
	DefaultPublicationDomain    = "workspace:default"
)

var (
	ErrRecoveryRecordInvalid       = errors.New("recovery record is invalid")
	ErrRecoverySignature           = errors.New("recovery record signature is invalid")
	ErrRecoveryTrustAnchor         = errors.New("recovery trust anchor is invalid")
	ErrPublicationAlreadyCommitted = errors.New("publication plan is already committed")
)

// SigningIdentity holds the local signing key. PrivateKey is deliberately not
// serializable as part of any portable recovery record.
type SigningIdentity struct {
	WriterIdentity string
	KeyID          string
	PrivateKey     ed25519.PrivateKey
	PublicKey      ed25519.PublicKey
}

// NewSigningIdentity creates a fresh Ed25519 identity using crypto/rand. The
// generated IDs are stable fingerprints of the public key and are suitable for
// tests and the single-administrator MVP profile.
func NewSigningIdentity() (SigningIdentity, error) {
	return NewSigningIdentityFromReader(cryptorand.Reader)
}

// NewSigningIdentityFromReader is deterministic when supplied a deterministic
// reader, which keeps signature tests independent of global randomness.
func NewSigningIdentityFromReader(reader io.Reader) (SigningIdentity, error) {
	if reader == nil {
		return SigningIdentity{}, errors.New("signing identity reader is required")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(reader)
	if err != nil {
		return SigningIdentity{}, fmt.Errorf("generate signing identity: %w", err)
	}
	keyDigest := DigestBytes(publicKey)
	keyID := "ed25519:" + keyDigest
	return SigningIdentity{
		WriterIdentity: "writer:" + keyID,
		KeyID:          keyID,
		PrivateKey:     append(ed25519.PrivateKey(nil), privateKey...),
		PublicKey:      append(ed25519.PublicKey(nil), publicKey...),
	}, nil
}

// NewTrustAnchor derives the public verification material that must be
// retained independently from a candidate closure or commit marker.
func NewTrustAnchor(identity SigningIdentity, publicationDomain string) (TrustAnchor, error) {
	if err := identity.validate(); err != nil {
		return TrustAnchor{}, err
	}
	if strings.TrimSpace(publicationDomain) == "" {
		return TrustAnchor{}, fmt.Errorf("%w: publication domain is required", ErrRecoveryTrustAnchor)
	}
	return TrustAnchor{
		Schema:            RecoveryTrustAnchorSchemaV1,
		SignatureDomain:   RecoverySignatureDomainV1,
		PublicationDomain: publicationDomain,
		WriterIdentity:    identity.WriterIdentity,
		KeyID:             identity.KeyID,
		Algorithm:         "Ed25519",
		PublicKey:         append([]byte(nil), identity.PublicKey...),
		PublicKeyDigest:   DigestBytes(identity.PublicKey),
	}, nil
}

// TrustAnchor is public verification material, not a source of signing
// authority. Its bytes must be retained outside the repository being checked.
type TrustAnchor struct {
	Schema            string `json:"schema"`
	SignatureDomain   string `json:"signature_domain"`
	PublicationDomain string `json:"publication_domain"`
	WriterIdentity    string `json:"writer_identity"`
	KeyID             string `json:"key_id"`
	Algorithm         string `json:"algorithm"`
	PublicKey         []byte `json:"public_key"`
	PublicKeyDigest   string `json:"public_key_digest"`
}

// SignedPreparedClosure is the signed, pre-publication RRF closure. It binds
// the exact payload and verification basis without asserting that publication
// has completed.
type SignedPreparedClosure struct {
	Schema                    string    `json:"schema"`
	SignatureDomain           string    `json:"signature_domain"`
	RecordKind                string    `json:"record_kind"`
	PublicationID             string    `json:"publication_id"`
	PublicationDomain         string    `json:"publication_domain"`
	Generation                uint64    `json:"generation"`
	SnapshotRef               string    `json:"snapshot_ref"`
	RRFRootDigest             string    `json:"rrf_root_digest"`
	ManifestDigest            string    `json:"manifest_digest"`
	PayloadReceiptDigest      string    `json:"payload_receipt_digest"`
	PayloadReceiptLength      int64     `json:"payload_receipt_length"`
	PayloadReceiptObjectCount int64     `json:"payload_receipt_object_count"`
	PlanDigest                string    `json:"plan_digest"`
	CaptureDigest             string    `json:"capture_digest"`
	PolicyDigest              string    `json:"policy_digest"`
	VerificationDigest        string    `json:"verification_digest"`
	TargetIdentity            string    `json:"target_identity"`
	FenceToken                uint64    `json:"fence_token"`
	WriterIdentity            string    `json:"writer_identity"`
	KeyID                     string    `json:"key_id"`
	SignedAt                  time.Time `json:"signed_at"`
	ParentCommitDigest        string    `json:"parent_commit_digest,omitempty"`
	Signature                 []byte    `json:"signature,omitempty"`
}

// PublicationCommitRecord is the portable logical commit point. It binds the
// previously reconciled prepared object and receipt. It intentionally has no
// field for its own placement receipt: that receipt would create a digest cycle
// and is authenticated separately by marker bytes and repository evidence.
type PublicationCommitRecord struct {
	Schema                    string    `json:"schema"`
	SignatureDomain           string    `json:"signature_domain"`
	RecordKind                string    `json:"record_kind"`
	PublicationID             string    `json:"publication_id"`
	PublicationDomain         string    `json:"publication_domain"`
	Generation                uint64    `json:"generation"`
	SnapshotRef               string    `json:"snapshot_ref"`
	RRFRootDigest             string    `json:"rrf_root_digest"`
	ManifestDigest            string    `json:"manifest_digest"`
	PayloadReceiptDigest      string    `json:"payload_receipt_digest"`
	PayloadReceiptLength      int64     `json:"payload_receipt_length"`
	PayloadReceiptObjectCount int64     `json:"payload_receipt_object_count"`
	PlanDigest                string    `json:"plan_digest"`
	CaptureDigest             string    `json:"capture_digest"`
	PolicyDigest              string    `json:"policy_digest"`
	VerificationDigest        string    `json:"verification_digest"`
	TargetIdentity            string    `json:"target_identity"`
	PreparedObjectDigest      string    `json:"prepared_object_digest"`
	PreparedReceiptDigest     string    `json:"prepared_receipt_digest"`
	FenceToken                uint64    `json:"fence_token"`
	WriterIdentity            string    `json:"writer_identity"`
	KeyID                     string    `json:"key_id"`
	SignedAt                  time.Time `json:"signed_at"`
	ParentCommitDigest        string    `json:"parent_commit_digest,omitempty"`
	Signature                 []byte    `json:"signature,omitempty"`
}

// ProcessorAttemptClosureRecord is an independently stored, signed child of
// a committed publication. It authenticates the deterministic SQLite export
// of post-publication processor attempts without making those attempts part
// of the exact publication transaction.
type ProcessorAttemptClosureRecord struct {
	Schema                   string    `json:"schema"`
	SignatureDomain          string    `json:"signature_domain"`
	RecordKind               string    `json:"record_kind"`
	WorkspaceID              string    `json:"workspace_id"`
	PublicationID            string    `json:"publication_id"`
	PublicationDomain        string    `json:"publication_domain"`
	SnapshotRef              string    `json:"snapshot_ref"`
	ManifestDigest           string    `json:"manifest_digest"`
	ParentCommitDigest       string    `json:"parent_commit_digest"`
	ClosureSequence          uint64    `json:"closure_sequence,omitempty"`
	PredecessorClosureDigest string    `json:"predecessor_closure_digest,omitempty"`
	AttemptBundleSchema      string    `json:"attempt_bundle_schema"`
	AttemptBundleDigest      string    `json:"attempt_bundle_digest"`
	AttemptBundleLength      int64     `json:"attempt_bundle_length"`
	AttemptCount             int64     `json:"attempt_count"`
	TargetIdentity           string    `json:"target_identity"`
	WriterIdentity           string    `json:"writer_identity"`
	KeyID                    string    `json:"key_id"`
	FenceToken               uint64    `json:"fence_token"`
	SignedAt                 time.Time `json:"signed_at"`
	Signature                []byte    `json:"signature,omitempty"`
}

// PortableFactClosureRecord is a signed complete-state child of one committed
// publication. The bundle is intentionally separate from the publication
// commit so durable catalog facts cannot delay exact commitment.
type PortableFactClosureRecord struct {
	Schema                     string          `json:"schema"`
	SignatureDomain            string          `json:"signature_domain"`
	RecordKind                 string          `json:"record_kind"`
	WorkspaceID                string          `json:"workspace_id"`
	PublicationID              string          `json:"publication_id"`
	PublicationDomain          string          `json:"publication_domain"`
	SnapshotRef                string          `json:"snapshot_ref"`
	ManifestDigest             string          `json:"manifest_digest"`
	ParentCommitDigest         string          `json:"parent_commit_digest"`
	ParentGeneration           uint64          `json:"parent_generation"`
	ClosureSequence            uint64          `json:"closure_sequence"`
	PredecessorClosureDigest   string          `json:"predecessor_closure_digest,omitempty"`
	BundleSchema               string          `json:"bundle_schema"`
	BundleDigest               string          `json:"bundle_digest"`
	BundleLength               int64           `json:"bundle_length"`
	RecordCount                int64           `json:"record_count"`
	AttachmentCount            int64           `json:"attachment_count"`
	ProcessorAttemptDigest     string          `json:"processor_attempt_digest,omitempty"`
	TargetIdentity             string          `json:"target_identity"`
	WriterIdentity             string          `json:"writer_identity"`
	KeyID                      string          `json:"key_id"`
	FenceToken                 uint64          `json:"fence_token"`
	RequiredReaderDependencies []string        `json:"required_reader_dependencies"`
	CanonicalizationProfile    string          `json:"canonicalization_profile"`
	CriticalExtensions         []string        `json:"critical_extensions"`
	OptionalExtensions         json.RawMessage `json:"optional_extensions"`
	SignedAt                   time.Time       `json:"signed_at"`
	Signature                  []byte          `json:"signature,omitempty"`
}

func (identity SigningIdentity) validate() error {
	if strings.TrimSpace(identity.WriterIdentity) == "" {
		return fmt.Errorf("%w: writer identity is required", ErrRecoveryRecordInvalid)
	}
	if strings.TrimSpace(identity.KeyID) == "" {
		return fmt.Errorf("%w: key id is required", ErrRecoveryRecordInvalid)
	}
	if len(identity.PrivateKey) != ed25519.PrivateKeySize || len(identity.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: Ed25519 key sizes are invalid", ErrRecoveryRecordInvalid)
	}
	if !identity.PrivateKey.Public().(ed25519.PublicKey).Equal(identity.PublicKey) {
		return fmt.Errorf("%w: public key does not match private key", ErrRecoveryRecordInvalid)
	}
	return nil
}

func (anchor TrustAnchor) validate() error {
	if anchor.Schema != RecoveryTrustAnchorSchemaV1 {
		return fmt.Errorf("%w: schema %q", ErrRecoveryTrustAnchor, anchor.Schema)
	}
	if anchor.SignatureDomain != RecoverySignatureDomainV1 {
		return fmt.Errorf("%w: signature domain %q", ErrRecoveryTrustAnchor, anchor.SignatureDomain)
	}
	if strings.TrimSpace(anchor.PublicationDomain) == "" || strings.TrimSpace(anchor.WriterIdentity) == "" || strings.TrimSpace(anchor.KeyID) == "" {
		return fmt.Errorf("%w: publication domain, writer identity, and key id are required", ErrRecoveryTrustAnchor)
	}
	if anchor.Algorithm != "Ed25519" || len(anchor.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: Ed25519 public key is required", ErrRecoveryTrustAnchor)
	}
	if anchor.PublicKeyDigest != DigestBytes(anchor.PublicKey) {
		return fmt.Errorf("%w: public key digest mismatch", ErrRecoveryTrustAnchor)
	}
	return nil
}

func (record SignedPreparedClosure) validate() error {
	if record.Schema != PreparedClosureSchemaV1 || record.RecordKind != PreparedClosureKind {
		return fmt.Errorf("%w: prepared closure schema or kind is invalid", ErrRecoveryRecordInvalid)
	}
	return validateCommonRecord(record.SignatureDomain, record.PublicationID, record.PublicationDomain, record.Generation, record.SnapshotRef, record.RRFRootDigest, record.ManifestDigest, record.PayloadReceiptDigest, record.PayloadReceiptLength, record.PayloadReceiptObjectCount, record.PlanDigest, record.CaptureDigest, record.PolicyDigest, record.VerificationDigest, record.TargetIdentity, record.WriterIdentity, record.KeyID, record.SignedAt)
}

func (record PublicationCommitRecord) validate() error {
	if record.Schema != PublicationCommitSchemaV1 || record.RecordKind != PublicationCommitKind {
		return fmt.Errorf("%w: publication commit schema or kind is invalid", ErrRecoveryRecordInvalid)
	}
	if err := validateCommonRecord(record.SignatureDomain, record.PublicationID, record.PublicationDomain, record.Generation, record.SnapshotRef, record.RRFRootDigest, record.ManifestDigest, record.PayloadReceiptDigest, record.PayloadReceiptLength, record.PayloadReceiptObjectCount, record.PlanDigest, record.CaptureDigest, record.PolicyDigest, record.VerificationDigest, record.TargetIdentity, record.WriterIdentity, record.KeyID, record.SignedAt); err != nil {
		return err
	}
	if strings.TrimSpace(record.PreparedObjectDigest) == "" || strings.TrimSpace(record.PreparedReceiptDigest) == "" {
		return fmt.Errorf("%w: prepared object and receipt digests are required", ErrRecoveryRecordInvalid)
	}
	return nil
}

func validateCommonRecord(signatureDomain, publicationID, publicationDomain string, generation uint64, snapshotRef, rrfRootDigest, manifestDigest, payloadReceiptDigest string, payloadLength, payloadObjectCount int64, planDigest, captureDigest, policyDigest, verificationDigest, targetIdentity, writerIdentity, keyID string, signedAt time.Time) error {
	if signatureDomain != RecoverySignatureDomainV1 {
		return fmt.Errorf("%w: signature domain %q", ErrRecoveryRecordInvalid, signatureDomain)
	}
	for name, value := range map[string]string{
		"publication id": publicationID, "publication domain": publicationDomain, "snapshot ref": snapshotRef,
		"RRF root digest": rrfRootDigest, "manifest digest": manifestDigest, "payload receipt digest": payloadReceiptDigest,
		"plan digest": planDigest, "capture digest": captureDigest, "policy digest": policyDigest,
		"verification digest": verificationDigest, "target identity": targetIdentity, "writer identity": writerIdentity, "key id": keyID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrRecoveryRecordInvalid, name)
		}
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("%w: %s contains NUL", ErrRecoveryRecordInvalid, name)
		}
	}
	if generation == 0 || payloadLength < 0 || payloadObjectCount < 0 || signedAt.IsZero() {
		return fmt.Errorf("%w: generation, payload counts, and signed time are invalid", ErrRecoveryRecordInvalid)
	}
	return nil
}

func (record SignedPreparedClosure) unsignedCanonical() ([]byte, error) {
	record.Signature = nil
	record.SignedAt = record.SignedAt.UTC()
	return canonicalJSON(record)
}

func (record PublicationCommitRecord) unsignedCanonical() ([]byte, error) {
	record.Signature = nil
	record.SignedAt = record.SignedAt.UTC()
	return canonicalJSON(record)
}

func (record ProcessorAttemptClosureRecord) unsignedCanonical() ([]byte, error) {
	record.Signature = nil
	record.SignedAt = record.SignedAt.UTC()
	return canonicalJSON(record)
}

func (record PortableFactClosureRecord) unsignedCanonical() ([]byte, error) {
	record.Signature = nil
	record.SignedAt = record.SignedAt.UTC()
	return canonicalJSON(record)
}

func signRecord(identity SigningIdentity, schema string, domain string, payload []byte) ([]byte, error) {
	if err := identity.validate(); err != nil {
		return nil, err
	}
	return ed25519.Sign(identity.PrivateKey, signingMessage(schema, domain, payload)), nil
}

func verifyRecord(anchor TrustAnchor, schema, domain string, payload, signature []byte, writerIdentity, keyID string, publicationDomain string) error {
	if err := anchor.validate(); err != nil {
		return err
	}
	if anchor.SignatureDomain != domain || anchor.PublicationDomain != publicationDomain || anchor.WriterIdentity != writerIdentity || anchor.KeyID != keyID {
		return fmt.Errorf("%w: signer or publication binding differs from trust anchor", ErrRecoveryTrustAnchor)
	}
	if len(signature) != ed25519.SignatureSize || !ed25519.Verify(anchor.PublicKey, signingMessage(schema, domain, payload), signature) {
		return ErrRecoverySignature
	}
	return nil
}

func signingMessage(schema, domain string, payload []byte) []byte {
	prefix := "restoreweave-signature-v1\x00" + schema + "\x00" + domain + "\x00"
	return append([]byte(prefix), payload...)
}

// SignPreparedClosure signs the canonical unsigned closure and returns a copy
// with the signature and normalized UTC timestamp populated.
func SignPreparedClosure(identity SigningIdentity, record SignedPreparedClosure) (SignedPreparedClosure, error) {
	record.SignedAt = record.SignedAt.UTC()
	record.Signature = nil
	if err := record.validate(); err != nil {
		return SignedPreparedClosure{}, err
	}
	if record.WriterIdentity != identity.WriterIdentity || record.KeyID != identity.KeyID {
		return SignedPreparedClosure{}, fmt.Errorf("%w: record signer differs from signing identity", ErrRecoveryRecordInvalid)
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return SignedPreparedClosure{}, err
	}
	record.Signature, err = signRecord(identity, record.Schema, record.SignatureDomain, payload)
	if err != nil {
		return SignedPreparedClosure{}, err
	}
	return record, nil
}

// Verify checks the exact schema, all mandatory fields, trust-anchor binding,
// and the Ed25519 signature over the unsigned canonical record.
func (record SignedPreparedClosure) Verify(anchor TrustAnchor) error {
	record.SignedAt = record.SignedAt.UTC()
	if err := record.validate(); err != nil {
		return err
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return err
	}
	return verifyRecord(anchor, record.Schema, record.SignatureDomain, payload, record.Signature, record.WriterIdentity, record.KeyID, record.PublicationDomain)
}

// SignPublicationCommit signs the canonical unsigned commit marker. There is
// deliberately no placement receipt field for this marker's own placement.
func SignPublicationCommit(identity SigningIdentity, record PublicationCommitRecord) (PublicationCommitRecord, error) {
	record.SignedAt = record.SignedAt.UTC()
	record.Signature = nil
	if err := record.validate(); err != nil {
		return PublicationCommitRecord{}, err
	}
	if record.WriterIdentity != identity.WriterIdentity || record.KeyID != identity.KeyID {
		return PublicationCommitRecord{}, fmt.Errorf("%w: record signer differs from signing identity", ErrRecoveryRecordInvalid)
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return PublicationCommitRecord{}, err
	}
	record.Signature, err = signRecord(identity, record.Schema, record.SignatureDomain, payload)
	if err != nil {
		return PublicationCommitRecord{}, err
	}
	return record, nil
}

// Verify checks a portable commit marker against an independently retained
// trust anchor.
func (record PublicationCommitRecord) Verify(anchor TrustAnchor) error {
	record.SignedAt = record.SignedAt.UTC()
	if err := record.validate(); err != nil {
		return err
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return err
	}
	return verifyRecord(anchor, record.Schema, record.SignatureDomain, payload, record.Signature, record.WriterIdentity, record.KeyID, record.PublicationDomain)
}

func (record ProcessorAttemptClosureRecord) validate() error {
	if (record.Schema != ProcessorAttemptClosureSchemaV1 && record.Schema != ProcessorAttemptClosureSchemaV2) || record.RecordKind != ProcessorAttemptClosureKind {
		return fmt.Errorf("%w: processor attempt closure schema or kind is invalid", ErrRecoveryRecordInvalid)
	}
	if record.SignatureDomain != RecoverySignatureDomainV1 {
		return fmt.Errorf("%w: signature domain %q", ErrRecoveryRecordInvalid, record.SignatureDomain)
	}
	for name, value := range map[string]string{
		"workspace id": record.WorkspaceID, "publication id": record.PublicationID, "publication domain": record.PublicationDomain,
		"snapshot ref": record.SnapshotRef, "manifest digest": record.ManifestDigest,
		"parent commit digest": record.ParentCommitDigest, "attempt bundle schema": record.AttemptBundleSchema,
		"attempt bundle digest": record.AttemptBundleDigest, "target identity": record.TargetIdentity,
		"writer identity": record.WriterIdentity, "key id": record.KeyID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrRecoveryRecordInvalid, name)
		}
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("%w: %s contains NUL", ErrRecoveryRecordInvalid, name)
		}
	}
	if !validExactContentID(record.ManifestDigest) || !validExactContentID(record.ParentCommitDigest) ||
		!validExactContentID(record.AttemptBundleDigest) {
		return fmt.Errorf("%w: processor attempt closure digests are invalid", ErrRecoveryRecordInvalid)
	}
	if record.Schema == ProcessorAttemptClosureSchemaV1 {
		if record.ClosureSequence != 0 || record.PredecessorClosureDigest != "" {
			return fmt.Errorf("%w: v1 processor attempt closure cannot declare successor lineage", ErrRecoveryRecordInvalid)
		}
	} else {
		if record.ClosureSequence == 0 {
			return fmt.Errorf("%w: processor attempt closure sequence must be positive", ErrRecoveryRecordInvalid)
		}
		if record.ClosureSequence == 1 && record.PredecessorClosureDigest != "" {
			return fmt.Errorf("%w: processor attempt closure sequence one cannot have a predecessor", ErrRecoveryRecordInvalid)
		}
		if record.ClosureSequence > 1 && !validExactContentID(record.PredecessorClosureDigest) {
			return fmt.Errorf("%w: processor attempt closure predecessor digest is invalid", ErrRecoveryRecordInvalid)
		}
	}
	if record.AttemptBundleLength == 0 || record.AttemptCount < 0 || record.FenceToken == 0 || record.SignedAt.IsZero() {
		return fmt.Errorf("%w: attempt bundle size/count, fence, and signed time are invalid", ErrRecoveryRecordInvalid)
	}
	return nil
}

// SignProcessorAttemptClosure signs the canonical unsigned child record.
func SignProcessorAttemptClosure(identity SigningIdentity, record ProcessorAttemptClosureRecord) (ProcessorAttemptClosureRecord, error) {
	record.SignedAt = record.SignedAt.UTC()
	record.Signature = nil
	if err := record.validate(); err != nil {
		return ProcessorAttemptClosureRecord{}, err
	}
	if record.WriterIdentity != identity.WriterIdentity || record.KeyID != identity.KeyID {
		return ProcessorAttemptClosureRecord{}, fmt.Errorf("%w: record signer differs from signing identity", ErrRecoveryRecordInvalid)
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return ProcessorAttemptClosureRecord{}, err
	}
	record.Signature, err = signRecord(identity, record.Schema, record.SignatureDomain, payload)
	if err != nil {
		return ProcessorAttemptClosureRecord{}, err
	}
	return record, nil
}

// Verify checks a processor-attempt child against the retained trust anchor.
func (record ProcessorAttemptClosureRecord) Verify(anchor TrustAnchor) error {
	record.SignedAt = record.SignedAt.UTC()
	if err := record.validate(); err != nil {
		return err
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return err
	}
	return verifyRecord(anchor, record.Schema, record.SignatureDomain, payload, record.Signature, record.WriterIdentity, record.KeyID, record.PublicationDomain)
}

func (record PortableFactClosureRecord) validate() error {
	if (record.Schema != PortableFactClosureSchemaV1 && record.Schema != PortableFactClosureSchemaV2) || record.RecordKind != PortableFactClosureKind || record.SignatureDomain != RecoverySignatureDomainV1 {
		return fmt.Errorf("%w: portable fact closure schema, kind, or signature domain is invalid", ErrRecoveryRecordInvalid)
	}
	for name, value := range map[string]string{
		"workspace id": record.WorkspaceID, "publication id": record.PublicationID,
		"publication domain": record.PublicationDomain, "snapshot ref": record.SnapshotRef,
		"manifest digest": record.ManifestDigest, "parent commit digest": record.ParentCommitDigest,
		"bundle schema": record.BundleSchema, "bundle digest": record.BundleDigest,
		"target identity": record.TargetIdentity, "writer identity": record.WriterIdentity, "key id": record.KeyID,
		"canonicalization profile": record.CanonicalizationProfile,
	} {
		if strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
			return fmt.Errorf("%w: portable fact closure %s is required", ErrRecoveryRecordInvalid, name)
		}
	}
	if !validExactContentID(record.ManifestDigest) || !validExactContentID(record.ParentCommitDigest) || !validExactContentID(record.BundleDigest) {
		return fmt.Errorf("%w: portable fact closure digest is invalid", ErrRecoveryRecordInvalid)
	}
	if record.Schema == PortableFactClosureSchemaV1 && record.BundleSchema != PortableFactBundleSchemaV1 || record.Schema == PortableFactClosureSchemaV2 && record.BundleSchema != PortableFactBundleSchemaV2 {
		return fmt.Errorf("%w: unsupported portable fact bundle schema %q", ErrRecoveryRecordInvalid, record.BundleSchema)
	}
	if record.ProcessorAttemptDigest != "" && !validExactContentID(record.ProcessorAttemptDigest) {
		return fmt.Errorf("%w: portable fact processor-attempt digest is invalid", ErrRecoveryRecordInvalid)
	}
	if record.ParentGeneration == 0 || record.ClosureSequence == 0 || record.BundleLength <= 0 || record.RecordCount < 0 || record.AttachmentCount < 0 || record.FenceToken == 0 || record.SignedAt.IsZero() {
		return fmt.Errorf("%w: portable fact closure sequence, sizes, fence, or time is invalid", ErrRecoveryRecordInvalid)
	}
	if record.ClosureSequence == 1 && record.PredecessorClosureDigest != "" || record.ClosureSequence > 1 && !validExactContentID(record.PredecessorClosureDigest) {
		return fmt.Errorf("%w: portable fact closure predecessor is invalid", ErrRecoveryRecordInvalid)
	}
	if record.OptionalExtensions == nil || !json.Valid(record.OptionalExtensions) || record.RequiredReaderDependencies == nil || record.CriticalExtensions == nil {
		return fmt.Errorf("%w: portable fact closure extension fields are invalid", ErrRecoveryRecordInvalid)
	}
	var optional map[string]json.RawMessage
	if err := json.Unmarshal(record.OptionalExtensions, &optional); err != nil || optional == nil {
		return fmt.Errorf("%w: portable fact optional extensions must be an object", ErrRecoveryRecordInvalid)
	}
	if len(record.CriticalExtensions) != 0 {
		return fmt.Errorf("%w: unsupported critical extension %q", ErrRecoveryRecordInvalid, record.CriticalExtensions[0])
	}
	return nil
}

func SignPortableFactClosure(identity SigningIdentity, record PortableFactClosureRecord) (PortableFactClosureRecord, error) {
	record.SignedAt = record.SignedAt.UTC()
	record.Signature = nil
	if err := record.validate(); err != nil {
		return PortableFactClosureRecord{}, err
	}
	if record.WriterIdentity != identity.WriterIdentity || record.KeyID != identity.KeyID {
		return PortableFactClosureRecord{}, fmt.Errorf("%w: record signer differs from signing identity", ErrRecoveryRecordInvalid)
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return PortableFactClosureRecord{}, err
	}
	record.Signature, err = signRecord(identity, record.Schema, record.SignatureDomain, payload)
	if err != nil {
		return PortableFactClosureRecord{}, err
	}
	return record, nil
}

func (record PortableFactClosureRecord) Verify(anchor TrustAnchor) error {
	record.SignedAt = record.SignedAt.UTC()
	if err := record.validate(); err != nil {
		return err
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return err
	}
	return verifyRecord(anchor, record.Schema, record.SignatureDomain, payload, record.Signature, record.WriterIdentity, record.KeyID, record.PublicationDomain)
}

func (record PortableFactClosureRecord) Digest() (string, error) {
	if err := record.validate(); err != nil {
		return "", err
	}
	payload, err := canonicalJSON(record)
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

func (record ProcessorAttemptClosureRecord) Digest() (string, error) {
	if err := record.validate(); err != nil {
		return "", err
	}
	payload, err := canonicalJSON(record)
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// CanonicalJSON returns compact deterministic JSON for the strongly typed
// records. Record-specific signing methods validate schema and field order
// before invoking this helper.
func CanonicalJSON(value any) ([]byte, error) { return canonicalJSON(value) }

func canonicalJSON(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("canonical JSON: %w", err)
	}
	if !json.Valid(payload) {
		return nil, fmt.Errorf("%w: canonical JSON is invalid", ErrRecoveryRecordInvalid)
	}
	return payload, nil
}

// DigestBytes returns the repository's standard sha256 content identifier for
// canonical bytes.
func DigestBytes(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// DigestCanonicalJSON hashes compact canonical JSON.
func DigestCanonicalJSON(value any) (string, error) {
	payload, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// SigningDigest returns the digest of the unsigned canonical closure.
func (record SignedPreparedClosure) SigningDigest() (string, error) {
	if err := record.validate(); err != nil {
		return "", err
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// Digest returns the digest of the complete signed closure object.
func (record SignedPreparedClosure) Digest() (string, error) {
	if err := record.validate(); err != nil {
		return "", err
	}
	payload, err := canonicalJSON(record)
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// SigningDigest returns the digest of the unsigned canonical commit marker.
func (record PublicationCommitRecord) SigningDigest() (string, error) {
	if err := record.validate(); err != nil {
		return "", err
	}
	payload, err := record.unsignedCanonical()
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// Digest returns the digest of the complete signed commit marker object.
func (record PublicationCommitRecord) Digest() (string, error) {
	if err := record.validate(); err != nil {
		return "", err
	}
	payload, err := canonicalJSON(record)
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// Keep crypto imported as a package-level compile-time assertion when Go's
// ed25519 implementation changes its PublicKey representation.
var _ crypto.Signer = ed25519.PrivateKey{}
