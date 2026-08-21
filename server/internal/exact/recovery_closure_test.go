package exact

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const testPublicationDomain = "workspace:default"

func testIdentity(t *testing.T) SigningIdentity {
	t.Helper()
	identity, err := NewSigningIdentity()
	if err != nil {
		t.Fatalf("new signing identity: %v", err)
	}
	return identity
}

func testPreparedClosure(t *testing.T, identity SigningIdentity) SignedPreparedClosure {
	t.Helper()
	return SignedPreparedClosure{
		Schema:                    PreparedClosureSchemaV1,
		SignatureDomain:           RecoverySignatureDomainV1,
		RecordKind:                PreparedClosureKind,
		PublicationID:             "pub_01",
		PublicationDomain:         testPublicationDomain,
		Generation:                7,
		SnapshotRef:               "snap_01",
		RRFRootDigest:             "sha256:rrf",
		ManifestDigest:            "sha256:manifest",
		PayloadReceiptDigest:      "sha256:payload-receipt",
		PayloadReceiptLength:      1234,
		PayloadReceiptObjectCount: 3,
		PlanDigest:                "sha256:plan",
		CaptureDigest:             "sha256:capture",
		PolicyDigest:              "sha256:policy",
		VerificationDigest:        "sha256:verification",
		TargetIdentity:            "repo:default",
		FenceToken:                11,
		WriterIdentity:            identity.WriterIdentity,
		KeyID:                     identity.KeyID,
		SignedAt:                  time.Date(2026, 8, 19, 10, 11, 12, 123456789, time.FixedZone("CST", 8*60*60)),
	}
}

func testCommit(t *testing.T, identity SigningIdentity) PublicationCommitRecord {
	t.Helper()
	return PublicationCommitRecord{
		Schema:                    PublicationCommitSchemaV1,
		SignatureDomain:           RecoverySignatureDomainV1,
		RecordKind:                PublicationCommitKind,
		PublicationID:             "pub_01",
		PublicationDomain:         testPublicationDomain,
		Generation:                7,
		SnapshotRef:               "snap_01",
		RRFRootDigest:             "sha256:rrf",
		ManifestDigest:            "sha256:manifest",
		PayloadReceiptDigest:      "sha256:payload-receipt",
		PayloadReceiptLength:      1234,
		PayloadReceiptObjectCount: 3,
		PlanDigest:                "sha256:plan",
		CaptureDigest:             "sha256:capture",
		PolicyDigest:              "sha256:policy",
		VerificationDigest:        "sha256:verification",
		TargetIdentity:            "repo:default",
		PreparedObjectDigest:      "sha256:prepared-object",
		PreparedReceiptDigest:     "sha256:prepared-receipt",
		FenceToken:                11,
		WriterIdentity:            identity.WriterIdentity,
		KeyID:                     identity.KeyID,
		SignedAt:                  time.Date(2026, 8, 19, 10, 11, 12, 123456789, time.FixedZone("CST", 8*60*60)),
		ParentCommitDigest:        "sha256:parent",
	}
}

func TestRecoveryIdentityAndTrustAnchor(t *testing.T) {
	identity := testIdentity(t)
	if len(identity.PrivateKey) != ed25519.PrivateKeySize || len(identity.PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("unexpected Ed25519 key sizes: private=%d public=%d", len(identity.PrivateKey), len(identity.PublicKey))
	}
	anchor, err := NewTrustAnchor(identity, testPublicationDomain)
	if err != nil {
		t.Fatalf("new trust anchor: %v", err)
	}
	if err := anchor.validate(); err != nil {
		t.Fatalf("validate trust anchor: %v", err)
	}
	anchor.PublicKeyDigest = "sha256:wrong"
	if err := anchor.validate(); !errors.Is(err, ErrRecoveryTrustAnchor) {
		t.Fatalf("tampered trust anchor error = %v, want trust-anchor error", err)
	}
}

func TestPreparedClosureSignVerifyCanonicalAndDigest(t *testing.T) {
	identity := testIdentity(t)
	anchor, err := NewTrustAnchor(identity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := SignPreparedClosure(identity, testPreparedClosure(t, identity))
	if err != nil {
		t.Fatalf("sign prepared closure: %v", err)
	}
	if err := prepared.Verify(anchor); err != nil {
		t.Fatalf("verify prepared closure: %v", err)
	}
	if !prepared.SignedAt.Equal(prepared.SignedAt.UTC()) {
		t.Fatalf("signed time was not normalized to UTC: %v", prepared.SignedAt)
	}
	unsigned, err := prepared.unsignedCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(unsigned, []byte(`"signature":`)) {
		t.Fatalf("signature appeared in signed payload: %s", unsigned)
	}
	if bytes.ContainsAny(unsigned, " \t\r\n") {
		t.Fatalf("canonical JSON is not compact: %s", unsigned)
	}
	var decoded map[string]any
	if err := json.Unmarshal(unsigned, &decoded); err != nil {
		t.Fatalf("decode canonical JSON: %v", err)
	}
	signingDigest, err := prepared.SigningDigest()
	if err != nil {
		t.Fatal(err)
	}
	objectDigest, err := prepared.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if signingDigest == objectDigest || !strings.HasPrefix(objectDigest, "sha256:") {
		t.Fatalf("unexpected signing/object digests: %q %q", signingDigest, objectDigest)
	}
}

func TestPreparedClosureRejectsWrongAnchorAndMutations(t *testing.T) {
	identity := testIdentity(t)
	anchor, err := NewTrustAnchor(identity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := SignPreparedClosure(identity, testPreparedClosure(t, identity))
	if err != nil {
		t.Fatal(err)
	}
	other := testIdentity(t)
	otherAnchor, err := NewTrustAnchor(other, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Verify(otherAnchor); !errors.Is(err, ErrRecoveryTrustAnchor) {
		t.Fatalf("wrong anchor error = %v, want trust-anchor error", err)
	}

	mutated := prepared
	mutated.SnapshotRef = "snap-other"
	if err := mutated.Verify(anchor); !errors.Is(err, ErrRecoverySignature) {
		t.Fatalf("changed field error = %v, want signature error", err)
	}
	mutated = prepared
	mutated.Schema = "org.restoreweave.prepared-closure.v2"
	if err := mutated.Verify(anchor); !errors.Is(err, ErrRecoveryRecordInvalid) {
		t.Fatalf("changed schema error = %v, want record error", err)
	}
	mutated = prepared
	mutated.SignatureDomain = "org.restoreweave.other-domain"
	if err := mutated.Verify(anchor); !errors.Is(err, ErrRecoveryRecordInvalid) {
		t.Fatalf("changed domain error = %v, want record error", err)
	}
}

func TestRecoveryRecordsRejectEmptyMandatoryFields(t *testing.T) {
	identity := testIdentity(t)
	prepared := testPreparedClosure(t, identity)
	prepared.PlanDigest = ""
	if _, err := SignPreparedClosure(identity, prepared); !errors.Is(err, ErrRecoveryRecordInvalid) {
		t.Fatalf("empty prepared field error = %v, want record error", err)
	}
	commit := testCommit(t, identity)
	commit.PreparedReceiptDigest = ""
	if _, err := SignPublicationCommit(identity, commit); !errors.Is(err, ErrRecoveryRecordInvalid) {
		t.Fatalf("empty commit field error = %v, want record error", err)
	}
	prepared = testPreparedClosure(t, identity)
	prepared.KeyID = "different-key"
	if _, err := SignPreparedClosure(identity, prepared); !errors.Is(err, ErrRecoveryRecordInvalid) {
		t.Fatalf("wrong key identity error = %v, want record error", err)
	}
}

func TestPublicationCommitSignVerifyAndNoOwnPlacementReceipt(t *testing.T) {
	identity := testIdentity(t)
	anchor, err := NewTrustAnchor(identity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := SignPublicationCommit(identity, testCommit(t, identity))
	if err != nil {
		t.Fatalf("sign commit: %v", err)
	}
	if err := commit.Verify(anchor); err != nil {
		t.Fatalf("verify commit: %v", err)
	}
	payload, err := CanonicalJSON(commit)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("placement_receipt")) {
		t.Fatalf("commit contains its own placement receipt: %s", payload)
	}
	mutated := commit
	mutated.PreparedObjectDigest = "sha256:other"
	if err := mutated.Verify(anchor); !errors.Is(err, ErrRecoverySignature) {
		t.Fatalf("changed prepared object error = %v, want signature error", err)
	}
	mutated = commit
	mutated.PublicationDomain = "workspace:other"
	if err := mutated.Verify(anchor); !errors.Is(err, ErrRecoveryTrustAnchor) {
		t.Fatalf("changed publication domain error = %v, want trust-anchor error", err)
	}
}
