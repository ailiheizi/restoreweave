package exact

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func buildSignedTokenFixture(t *testing.T, subjectPath string) (service *Service, anchor TrustAnchor, snapshotRef string) {
	t.Helper()
	fixture := newSignedPublicationFixture(t, "token.bin", []byte("deterministic recovery token"))
	result := fixture.ingest(t, "sha256:token-plan")
	anchorPath := filepath.Join(t.TempDir(), "trust-anchor.json")
	if _, err := ExportTrustAnchor(*fixture.service.TrustAnchor, anchorPath); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustAnchor(anchorPath)
	if err != nil {
		t.Fatal(err)
	}
	reader := &Service{
		Repo: fixture.repo, TrustAnchor: &loaded, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true,
	}
	return reader, loaded, result.SnapshotRef
}

func TestRecoveryTokenRoundTripIsDeterministicAndSelfAuthenticating(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	first, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatalf("build token: %v", err)
	}
	second, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatalf("rebuild token: %v", err)
	}
	if first.TokenDigest != second.TokenDigest {
		t.Fatalf("token digest is not deterministic: %q vs %q", first.TokenDigest, second.TokenDigest)
	}
	firstBytes, err := CanonicalJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := CanonicalJSON(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatalf("token bytes differ across identical builds")
	}
	if first.TokenSchema != RecoveryTokenSchemaV1 || first.PublicationCommitRef == "" ||
		first.RecoveryReferenceID == "" || first.SubjectRef == "" {
		t.Fatalf("incomplete token envelope: %+v", first)
	}
	if !strings.HasPrefix(first.ExpectedContentID, "sha256:") || first.RecipeDigest == "" {
		t.Fatalf("token recovery identity = %+v", first)
	}
	if err := first.Validate(anchor); err != nil {
		t.Fatalf("validate token: %v", err)
	}
	digest, err := first.Digest()
	if err != nil || digest != first.TokenDigest {
		t.Fatalf("recomputed token digest = %q, want %q", digest, first.TokenDigest)
	}
	payload, err := CanonicalJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), first.TokenDigest) {
		t.Fatalf("token digest does not cover its serialized fields")
	}
}

func TestRecoveryTokenWrongAnchorInvalidates(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	wrongIdentity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrongAnchor, err := NewTrustAnchor(wrongIdentity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	if err := token.Validate(wrongAnchor); err == nil {
		t.Fatal("token validated against the wrong trust anchor")
	}
	if _, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", wrongAnchor); err == nil {
		t.Fatal("token built against the wrong trust anchor")
	}
}

func TestRecoveryTokenTamperedFieldFailsDigest(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	tampered := token
	tampered.RecoveryReferenceID = "rrf_tampered"
	if err := tampered.Validate(anchor); err == nil {
		t.Fatal("tampered recovery reference was accepted")
	}
	tampered = token
	tampered.ExpectedContentID = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := tampered.Validate(anchor); err == nil {
		t.Fatal("tampered expected identity was accepted")
	}
	tampered = token
	tampered.PublicationCommitRef = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := tampered.Validate(anchor); err == nil {
		t.Fatal("tampered publication reference was accepted")
	}
}

func TestRecoveryTokenDigestCoversEveryField(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	base, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	originalDigest := base.TokenDigest
	for name, mutate := range map[string]func(*RecoveryToken){
		"expected length": func(token *RecoveryToken) { token.ExpectedLength++ },
		"recipe digest":   func(token *RecoveryToken) { token.RecipeDigest = "sha256:" + strings.Repeat("1", 64) },
		"expiry":          func(token *RecoveryToken) { token.Expiry = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339) },
	} {
		mutated := base
		mutate(&mutated)
		digest, err := mutated.Digest()
		if err != nil {
			t.Fatal(err)
		}
		if digest == originalDigest {
			t.Fatalf("field %q did not change the token digest", name)
		}
	}
}

func TestRecoveryTokenRejectsEmptyAndUnsafeSubjects(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	for _, subject := range []string{"", "   ", "..", "token.bin/../secret", "/absolute"} {
		if _, err := service.BuildRecoveryToken(ctx, snapshotRef, subject, anchor); err == nil {
			t.Fatalf("token accepted unsafe subject %q", subject)
		}
	}
	if _, err := service.BuildRecoveryToken(ctx, "", "token.bin", anchor); err == nil {
		t.Fatal("token accepted an empty snapshot reference")
	}
	if _, err := service.BuildRecoveryToken(ctx, "snapshot:missing", "token.bin", anchor); err == nil {
		t.Fatal("token accepted a missing snapshot")
	}
}

func TestRecoveryTokenExpiredFailsClosed(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	token.Expiry = time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	token.TokenDigest = ""
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	token.TokenDigest = digest
	if err := token.Validate(anchor); err == nil {
		t.Fatal("expired token was accepted")
	}
}

func TestRecoveryTokenMetadataOnlySubjectFailsClosed(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	filePath := filepath.Join(source, "unreadable.bin")
	if err := os.WriteFile(filePath, []byte("unreadable fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	identity, anchor, err := OpenSigningMaterial(t.TempDir(), testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := service.InspectIngest(ctx, source, IngestOptions{
		FileProtection:          map[string]sqlite.ProtectionMode{"unreadable.bin": sqlite.ProtectionMetadataOnly},
		MetadataOnlyResolutions: []string{"unreadable.bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:metadata-only-token-plan")
	if err != nil {
		t.Fatal(err)
	}
	reader := &Service{
		Repo: repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true,
	}
	if _, err := reader.BuildRecoveryToken(ctx, result.SnapshotRef, "unreadable.bin", anchor); err == nil {
		t.Fatal("metadata-only subject produced a recovery token")
	}
}

func TestRecoveryTokenEnvelopeMatchesCommandDataShape(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := CanonicalJSON(token)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"token_schema", "snapshot_ref", "subject_ref", "recovery_reference_id",
		"expected_content_id", "expected_length", "recipe_digest",
		"publication_commit_ref", "trust_anchor_ref", "token_digest",
	} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("token envelope missing field %q", name)
		}
	}
}
