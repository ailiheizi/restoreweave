package exact

import (
	"context"
	"encoding/json"
	"errors"
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
	if first.PublicationDomain != testPublicationDomain {
		t.Fatalf("token publication domain = %q, want %q", first.PublicationDomain, testPublicationDomain)
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

func TestRecoveryTokenRepositoryProofAuthenticatesCommitPreparedAndMapping(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	if err := token.ValidateAgainstRepository(ctx, service.Repo, anchor); err != nil {
		t.Fatalf("repository-backed token proof rejected: %v", err)
	}
	if err := VerifyRecoveryToken(ctx, service.Repo, token, anchor); err != nil {
		t.Fatalf("package token verifier rejected: %v", err)
	}
	if err := service.VerifyRecoveryToken(ctx, token, anchor); err != nil {
		t.Fatalf("service token verifier rejected: %v", err)
	}
}

func TestRecoveryTokenRepositoryProofRejectsMissingOrCorruptExactPayload(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
			token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
			if err != nil {
				t.Fatal(err)
			}
			hexDigest := strings.TrimPrefix(token.ExpectedContentID, "sha256:")
			path := filepath.Join(service.Repo.(*repository.Dir).Root(), "blobs", "sha256", hexDigest[:2], hexDigest)
			if mode == "missing" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("corrupt payload"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := token.ValidateAgainstRepository(ctx, service.Repo, anchor); err == nil {
				t.Fatalf("recovery token accepted a %s exact payload", mode)
			}
		})
	}
}

func TestRecoveryTokenRepositoryProofRejectsTamperedExactPayload(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	hexDigest := strings.TrimPrefix(token.ExpectedContentID, "sha256:")
	path := filepath.Join(service.Repo.(*repository.Dir).Root(), "blobs", "sha256", hexDigest[:2], hexDigest)
	if !flipFileByte(t, path) {
		t.Fatal("exact payload tamper did not change bytes")
	}
	if err := token.ValidateAgainstRepository(ctx, service.Repo, anchor); err == nil {
		t.Fatal("recovery token accepted a tampered exact payload")
	}
}

func TestRecoveryTokenRepositoryProofRejectsUnimplementedExactReversible(t *testing.T) {
	ctx := context.Background()
	service, _, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", *service.TrustAnchor)
	if err != nil {
		t.Fatal(err)
	}
	length := token.ExpectedLength
	reference := ManifestRecoveryReference{
		ReferenceID:      token.RecoveryReferenceID,
		Kind:             string(sqlite.RecoveryExactReversible),
		RepresentationID: "representation:reversible",
		Claim:            string(sqlite.RecoveryClaimRestoreVerified),
	}
	entry := ManifestEntry{Protection: ManifestProtection{
		ExpectedContentID:     token.ExpectedContentID,
		ExpectedLogicalLength: &length,
	}}
	err = validateTokenLocalRecoveryReference(ctx, service.Repo, reference, entry)
	if !errors.Is(err, ErrRecoveryTokenInvalid) || !strings.Contains(err.Error(), "decoder closure") {
		t.Fatalf("EXACT_REVERSIBLE validation error = %v, want explicit unavailable decoder failure", err)
	}
}

func TestRecoveryTokenRepositoryProofKeepsLinkOnlyValidWhenExactPayloadIsMissing(t *testing.T) {
	ctx := context.Background()
	fixture := newSignedPublicationFixture(t, "token-link-only.bin", []byte("token link-only payload"))
	fixture.service.AllowLinkOnly = true
	fixture.service.LinkOnlyRequiresConfirmation = true
	plan, err := fixture.service.InspectIngest(ctx, fixture.source, IngestOptions{
		ProtectionMode: sqlite.ProtectionStoreExactWithExternalFallback,
		ExternalLocators: []IngestLocator{
			{Locator: "https://example.test/token-link-only.bin"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:token-link-only-plan")
	if err != nil {
		t.Fatal(err)
	}
	anchor := *fixture.service.TrustAnchor
	set, err := fixture.service.BuildRecoveryTokenSet(ctx, result.SnapshotRef, "token-link-only.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Tokens) != 2 {
		t.Fatalf("token count = %d, want exact and external references", len(set.Tokens))
	}
	var exactToken, linkOnlyToken RecoveryToken
	for _, token := range set.Tokens {
		switch token.ReferenceKind {
		case string(sqlite.RecoveryExactRepresentation):
			exactToken = token
		case string(sqlite.RecoveryExternalLocator):
			linkOnlyToken = token
		}
	}
	if exactToken.ExpectedContentID == "" || linkOnlyToken.RecoveryReferenceID == "" {
		t.Fatalf("token set lacks exact/external references: %+v", set.Tokens)
	}
	hexDigest := strings.TrimPrefix(exactToken.ExpectedContentID, "sha256:")
	if err := os.Remove(filepath.Join(fixture.repo.Root(), "blobs", "sha256", hexDigest[:2], hexDigest)); err != nil {
		t.Fatal(err)
	}
	if err := exactToken.ValidateAgainstRepository(ctx, fixture.repo, anchor); err == nil {
		t.Fatal("exact recovery token accepted a missing exact payload")
	}
	if err := linkOnlyToken.ValidateAgainstRepository(ctx, fixture.repo, anchor); err != nil {
		t.Fatalf("link-only recovery token rejected without local payload: %v", err)
	}
}

func TestRecoveryTokenRepositoryProofRejectsResealedProjectionTampering(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	base, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*RecoveryToken){
		"subject mapping": func(token *RecoveryToken) { token.SubjectRef = "subject:forged" },
		"reference":       func(token *RecoveryToken) { token.RecoveryReferenceID = "rrf:forged" },
		"kind":            func(token *RecoveryToken) { token.ReferenceKind = string(sqlite.RecoveryExactReversible) },
		"claim":           func(token *RecoveryToken) { token.ProtectionClaim = string(sqlite.RecoveryClaimUnavailable) },
		"content id":      func(token *RecoveryToken) { token.ExpectedContentID = "sha256:" + strings.Repeat("0", 64) },
		"length":          func(token *RecoveryToken) { token.ExpectedLength++ },
		"recipe":          func(token *RecoveryToken) { token.RecipeDigest = "sha256:" + strings.Repeat("1", 64) },
		"locator set":     func(token *RecoveryToken) { token.LocatorSetDigest = "sha256:" + strings.Repeat("2", 64) },
		"commit":          func(token *RecoveryToken) { token.PublicationCommitRef = "sha256:" + strings.Repeat("3", 64) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			tampered := base
			mutate(&tampered)
			tampered.TokenDigest, err = tampered.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if err := tampered.Validate(anchor); err != nil {
				t.Fatalf("resealed token did not pass envelope validation: %v", err)
			}
			if err := service.VerifyRecoveryToken(ctx, tampered, anchor); err == nil {
				t.Fatal("repository-backed verifier accepted resealed token tampering")
			}
		})
	}
}

func TestRecoveryTokenRepositoryProofRequiresAuthenticatedSubjectMapping(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	token.SubjectRef = "subject:missing-mapping"
	token.TokenDigest, err = token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.VerifyRecoveryToken(ctx, token, anchor); err == nil {
		t.Fatal("token with no authenticated subject mapping was accepted")
	}
}

func TestRecoveryTokenRepositoryProofRejectsRecomputedExpiry(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	token.Expiry = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	token.TokenDigest, err = token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.VerifyRecoveryToken(ctx, token, anchor); err == nil {
		t.Fatal("repository proof accepted a recomputed expiry absent from the authenticated recovery reference")
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

func TestRecoveryTokenLegacyV1ShapeStillValidates(t *testing.T) {
	ctx := context.Background()
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	current, err := service.BuildRecoveryToken(ctx, snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	legacy := current
	legacy.PublicationDomain = ""
	legacy.ReferenceKind = ""
	legacy.ProtectionClaim = ""
	legacy.LocatorSetDigest = ""
	legacy.TokenDigest = ""
	digest, err := legacy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	legacy.TokenDigest = digest
	payload, err := CanonicalJSON(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RecoveryToken
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(anchor); err != nil {
		t.Fatalf("legacy v1 token failed validation: %v", err)
	}
}

func TestRecoveryTokenValidateRejectsMalformedTrustAnchor(t *testing.T) {
	service, anchor, snapshotRef := buildSignedTokenFixture(t, "token.bin")
	token, err := service.BuildRecoveryToken(context.Background(), snapshotRef, "token.bin", anchor)
	if err != nil {
		t.Fatal(err)
	}
	anchor.PublicKeyDigest = "sha256:" + strings.Repeat("0", 64)
	if err := token.Validate(anchor); err == nil {
		t.Fatal("malformed trust anchor was accepted")
	}
}

func TestRecoveryTokenRecipeDigestUsesCanonicalJSON(t *testing.T) {
	recipe := json.RawMessage(` { "z": 1, "a": [true, {"b":2,"a":1}] } `)
	entry := ManifestEntry{Protection: ManifestProtection{ExpectedLogicalLength: func() *int64 { value := int64(3); return &value }()}}
	reference := ManifestRecoveryReference{
		ReferenceID: "rrf_recipe", Kind: string(sqlite.RecoveryExactReversible),
		Claim: string(sqlite.RecoveryClaimRestoreVerified), Recipe: recipe,
	}
	token, err := buildRecoveryTokenForReference("snapshot:recipe", testPublicationDomain, "recipe.bin", entry, "subject:recipe", reference, "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	want := DigestBytes([]byte(`{"a":[true,{"a":1,"b":2}],"z":1}`))
	if token.RecipeDigest != want {
		t.Fatalf("recipe digest = %q, want canonical digest %q", token.RecipeDigest, want)
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

func TestRecoveryTokenSetExportsEveryReferenceInStableOrder(t *testing.T) {
	ctx := context.Background()
	fixture := newSignedPublicationFixture(t, "set.bin", []byte("token set payload"))
	fixture.service.AllowLinkOnly = true
	fixture.service.LinkOnlyRequiresConfirmation = true
	plan, err := fixture.service.InspectIngest(ctx, fixture.source, IngestOptions{
		ProtectionMode: sqlite.ProtectionStoreExactWithExternalFallback,
		ExternalLocators: []IngestLocator{
			{Locator: "https://example.test/set.bin"},
			{Locator: "ipfs://bafy-set/set.bin"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:token-set-plan")
	if err != nil {
		t.Fatal(err)
	}
	anchor := *fixture.service.TrustAnchor
	reader := &Service{
		Repo: fixture.repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true,
	}
	first, err := reader.BuildRecoveryTokenSet(ctx, result.SnapshotRef, "set.bin", anchor)
	if err != nil {
		t.Fatalf("build token set: %v", err)
	}
	second, err := reader.BuildRecoveryTokenSet(ctx, result.SnapshotRef, "set.bin", anchor)
	if err != nil {
		t.Fatalf("rebuild token set: %v", err)
	}
	if first.Schema != RecoveryTokenSetSchemaV1 || first.PublicationDomain != testPublicationDomain || len(first.Tokens) != 2 || first.Unprotected != nil {
		t.Fatalf("token set = %+v", first)
	}
	if first.Tokens[0].ProtectionClaim != string(sqlite.RecoveryClaimRestoreVerified) ||
		first.Tokens[1].ProtectionClaim != string(sqlite.RecoveryClaimLinkOnlyUnprotected) {
		t.Fatalf("token claims = %+v", first.Tokens)
	}
	if first.Tokens[1].LocatorSetDigest == "" || first.Tokens[1].RecipeDigest == "" {
		t.Fatalf("link-only token omitted locator proof: %+v", first.Tokens[1])
	}
	if first.SetDigest == "" || first.SetDigest != second.SetDigest {
		t.Fatalf("token set is not deterministic: %q / %q", first.SetDigest, second.SetDigest)
	}
	reordered := first
	reordered.Tokens = append([]RecoveryToken(nil), first.Tokens...)
	reordered.Tokens[0], reordered.Tokens[1] = reordered.Tokens[1], reordered.Tokens[0]
	reorderedDigest, err := reordered.Digest()
	if err != nil || reorderedDigest != first.SetDigest {
		t.Fatalf("token set digest depends on reference order: %q / %q", reorderedDigest, first.SetDigest)
	}
	for _, token := range first.Tokens {
		if err := reader.VerifyRecoveryToken(ctx, token, anchor); err != nil {
			t.Fatalf("clean reader rejected token %s: %v", token.RecoveryReferenceID, err)
		}
	}
}

func TestRecoveryTokenSetRequiresAuthenticatedSubjectMapping(t *testing.T) {
	ctx := context.Background()
	fixture := newSignedPublicationFixture(t, "mapped.bin", []byte("mapped token subject"))
	result := fixture.ingest(t, "sha256:mapped-token-plan")
	anchor := *fixture.service.TrustAnchor
	if _, err := fixture.service.BuildRecoveryTokenSet(ctx, result.SnapshotRef, "mapped.bin", anchor); err != nil {
		t.Fatalf("baseline token set: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*portableFactBundle)
	}{
		{name: "missing mapping", mutate: func(bundle *portableFactBundle) {
			for index := range bundle.Records {
				if bundle.Records[index].RecordKind == "SUBJECT_MAPPING" {
					bundle.Records = append(bundle.Records[:index], bundle.Records[index+1:]...)
					return
				}
			}
		}},
		{name: "tampered mapping", mutate: func(bundle *portableFactBundle) {
			for index := range bundle.Records {
				if bundle.Records[index].RecordKind != "SUBJECT_MAPPING" {
					continue
				}
				var mapping subjectMappingPayload
				if err := decodeStrictRecord(bundle.Records[index].Payload, &mapping); err != nil {
					t.Fatalf("decode subject mapping: %v", err)
				}
				mapping.DisplayName = "tampered.bin"
				rewritePortableRecordPayload(t, &bundle.Records[index], mapping)
				return
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Each subtest gets a fresh signed publication because replacing a
			// closure is intentionally irreversible in the content-addressed repo.
			local := newSignedPublicationFixture(t, "mapped.bin", []byte("mapped token subject"))
			localResult := local.ingest(t, "sha256:mapped-token-"+tc.name)
			replacePortableFactBundleForTest(t, local, tc.mutate)
			_, err := local.service.BuildRecoveryTokenSet(ctx, localResult.SnapshotRef, "mapped.bin", *local.service.TrustAnchor)
			if !errors.Is(err, ErrRecoveryTokenInvalid) {
				t.Fatalf("mapping %s error = %v, want ErrRecoveryTokenInvalid", tc.name, err)
			}
		})
	}
}

func TestRecoveryTokenSetMetadataOnlyReturnsExplicitUnprotectedRecord(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "metadata.bin"), []byte("metadata only"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	identity, anchor, err := OpenSigningMaterial(t.TempDir(), testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	plan, err := service.InspectIngest(ctx, source, IngestOptions{
		FileProtection:          map[string]sqlite.ProtectionMode{"metadata.bin": sqlite.ProtectionMetadataOnly},
		MetadataOnlyResolutions: []string{"metadata.bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:metadata-token-set-plan")
	if err != nil {
		t.Fatal(err)
	}
	set, err := service.BuildRecoveryTokenSet(ctx, result.SnapshotRef, "metadata.bin", anchor)
	if err != nil {
		t.Fatalf("build metadata-only set: %v", err)
	}
	if len(set.Tokens) != 0 || set.Unprotected == nil || set.Unprotected.ProtectionOutcome != string(sqlite.ProtectionExplicitlyUnprotected) {
		t.Fatalf("metadata-only token set = %+v", set)
	}
	if set.Unprotected.RecordDigest == "" || set.SetDigest == "" {
		t.Fatalf("metadata-only set is not self-digested: %+v", set)
	}
	if _, err := service.BuildRecoveryToken(ctx, result.SnapshotRef, "metadata.bin", anchor); !errors.Is(err, ErrNoRecoveryPath) {
		t.Fatalf("legacy single-token helper error = %v, want ErrNoRecoveryPath", err)
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
		"token_schema", "snapshot_ref", "publication_domain", "subject_ref", "recovery_reference_id",
		"reference_kind", "protection_claim",
		"expected_content_id", "expected_length", "recipe_digest",
		"publication_commit_ref", "trust_anchor_ref", "token_digest",
	} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("token envelope missing field %q", name)
		}
	}
}
