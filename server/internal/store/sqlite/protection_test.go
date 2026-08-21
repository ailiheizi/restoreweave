package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestProtectionRecoveryAndExternalBindingRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	defer store.Close()

	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Protection workspace"}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertWorkspace(ctx, &workspace)
	}); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	contentID := "sha256:content"
	representation := &Representation{
		ID:              testID(t, IDPrefixRepresentation),
		WorkspaceID:     workspace.ID,
		ContentID:       contentID,
		DecodedLength:   12,
		OwnershipMode:   OwnershipRestoreWeavePacks,
		CodecProfileRef: "raw-v1",
		AccessMode:      AccessWholeObjectOnly,
		RecordDigest:    "sha256:representation-record",
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertRepresentation(ctx, representation)
	}); err != nil {
		t.Fatalf("insert representation: %v", err)
	}

	subject := testID(t, IDPrefixNamespaceEntry)
	verifiedAt := testEpoch.Add(time.Minute)
	protection := &ProtectionRecord{
		ID:                    testID(t, IDPrefixProtectionRecord),
		WorkspaceID:           workspace.ID,
		SubjectRef:            subject,
		Mode:                  ProtectionStoreExactWithExternalFallback,
		Outcome:               ProtectionExactProtected,
		ExpectedContentID:     contentID,
		ExpectedLogicalLength: int64Ptr(12),
		LocalRepresentationID: representation.ID,
		PolicyDecisionRef:     "decision:exact",
		LastVerificationRef:   "verification:exact",
		LastVerifiedAt:        &verifiedAt,
		Metadata:              json.RawMessage(`{"source":"test"}`),
	}
	if err := store.InsertProtectionRecord(ctx, protection); err != nil {
		t.Fatalf("insert protection record: %v", err)
	}
	gotProtection, err := store.GetProtectionRecordBySubject(ctx, workspace.ID, subject)
	if err != nil {
		t.Fatalf("get protection record: %v", err)
	}
	if gotProtection.Mode != ProtectionStoreExactWithExternalFallback ||
		gotProtection.LocalRepresentationID != representation.ID ||
		gotProtection.ExpectedLogicalLength == nil || *gotProtection.ExpectedLogicalLength != 12 {
		t.Fatalf("protection record = %+v", gotProtection)
	}

	binding := &ExternalBinding{
		ID:                testID(t, IDPrefixExternalBinding),
		WorkspaceID:       workspace.ID,
		SubjectRef:        subject,
		ProviderKind:      "HTTPS",
		StableIdentity:    "https://example.test/item/42",
		BindingDigest:     "sha256:binding",
		CredentialRef:     "secret:example",
		RightsEvidenceRef: "rights:public",
		Metadata:          json.RawMessage(`{"edition":"42"}`),
	}
	if err := store.InsertExternalBinding(ctx, binding); err != nil {
		t.Fatalf("insert external binding: %v", err)
	}
	locator := &ExternalLocator{
		ID:                testID(t, IDPrefixExternalLocator),
		WorkspaceID:       workspace.ID,
		BindingID:         binding.ID,
		Priority:          2,
		Kind:              "HTTPS",
		Locator:           "https://example.test/item/42/file.bin",
		ExpectedContentID: contentID,
		CredentialRef:     "secret:example",
		RightsEvidenceRef: "rights:public",
		ValidationRef:     "validation:none",
		Availability:      "AVAILABLE",
		ValidationStatus:  "VERIFIED",
	}
	if err := store.InsertExternalLocator(ctx, locator); err != nil {
		t.Fatalf("insert external locator: %v", err)
	}

	reference := &RecoveryReference{
		ID:                    testID(t, IDPrefixRecoveryReference),
		WorkspaceID:           workspace.ID,
		ProtectionRecordID:    protection.ID,
		SubjectRef:            subject,
		Kind:                  RecoveryExternalLocator,
		Priority:              10,
		Claim:                 RecoveryClaimExternalReplayable,
		ExpectedContentID:     contentID,
		ExpectedLogicalLength: int64Ptr(12),
		ExternalBindingID:     binding.ID,
		Status:                "VERIFIED",
		PolicyRef:             "policy:external",
		RecordDigest:          "sha256:reference",
		Recipe:                json.RawMessage(`{"method":"download"}`),
		Verification:          json.RawMessage(`{"verified":true}`),
	}
	if err := store.InsertRecoveryReference(ctx, reference); err != nil {
		t.Fatalf("insert recovery reference: %v", err)
	}

	refs, err := store.ListRecoveryReferencesBySubject(ctx, workspace.ID, subject)
	if err != nil {
		t.Fatalf("list recovery references: %v", err)
	}
	if len(refs) != 1 || refs[0].ExternalBindingID != binding.ID || refs[0].Claim != RecoveryClaimExternalReplayable {
		t.Fatalf("recovery references = %+v", refs)
	}
	locators, err := store.ListExternalLocators(ctx, workspace.ID, binding.ID)
	if err != nil {
		t.Fatalf("list external locators: %v", err)
	}
	if len(locators) != 1 || locators[0].Locator != locator.Locator || locators[0].ValidationStatus != "VERIFIED" {
		t.Fatalf("external locators = %+v", locators)
	}
}

func TestLinkOnlyProtectionRequiresVisibleNonExactOutcome(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ":memory:")
	defer store.Close()
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Link-only workspace"}
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertWorkspace(ctx, &workspace) }); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	base := ProtectionRecord{
		ID:          testID(t, IDPrefixProtectionRecord),
		WorkspaceID: workspace.ID,
		SubjectRef:  testID(t, IDPrefixNamespaceEntry),
		Mode:        ProtectionLinkOnly,
		Outcome:     ProtectionExactProtected,
	}
	if err := store.InsertProtectionRecord(ctx, &base); err == nil {
		t.Fatal("link-only exact outcome unexpectedly succeeded")
	}
	base.ID = testID(t, IDPrefixProtectionRecord)
	base.Outcome = ProtectionLinkOnlyUnprotected
	if err := store.InsertProtectionRecord(ctx, &base); err != nil {
		t.Fatalf("insert valid link-only record: %v", err)
	}
	if _, err := store.GetProtectionRecord(ctx, workspace.ID, testID(t, IDPrefixProtectionRecord)); err == nil {
		t.Fatal("unexpected protection record for unused id")
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing protection record = %v, want ErrNotFound", err)
	}
}

func TestProtectionOutcomeModeMatrix(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	defer store.Close()
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Protection outcome matrix"}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertWorkspace(ctx, &workspace)
	}); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	representation := &Representation{
		ID:              testID(t, IDPrefixRepresentation),
		WorkspaceID:     workspace.ID,
		ContentID:       "sha256:matrix",
		DecodedLength:   4,
		OwnershipMode:   OwnershipRestoreWeavePacks,
		CodecProfileRef: "raw-v1",
		AccessMode:      AccessWholeObjectOnly,
		RecordDigest:    "sha256:matrix-representation",
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertRepresentation(ctx, representation)
	}); err != nil {
		t.Fatalf("insert representation: %v", err)
	}

	valid := []struct {
		name    string
		mode    ProtectionMode
		outcome ProtectionOutcome
		local   bool
	}{
		{"exact fallback", ProtectionStoreExact, ProtectionExactFallback, true},
		{"fallback with external", ProtectionStoreExactWithExternalFallback, ProtectionExactFallback, true},
		{"exact blocked", ProtectionStoreExact, ProtectionBlocked, false},
		{"link explicitly unprotected", ProtectionLinkOnly, ProtectionExplicitlyUnprotected, false},
		{"metadata explicitly unprotected", ProtectionMetadataOnly, ProtectionExplicitlyUnprotected, false},
		{"metadata blocked", ProtectionMetadataOnly, ProtectionBlocked, false},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			record := &ProtectionRecord{
				ID:          testID(t, IDPrefixProtectionRecord),
				WorkspaceID: workspace.ID,
				SubjectRef:  testID(t, IDPrefixNamespaceEntry),
				Mode:        test.mode,
				Outcome:     test.outcome,
			}
			if test.local {
				record.LocalRepresentationID = representation.ID
				record.LastVerificationRef = "readback:" + representation.ContentID
			}
			if err := store.InsertProtectionRecord(ctx, record); err != nil {
				t.Fatalf("insert %s/%s: %v", test.mode, test.outcome, err)
			}
		})
	}

	invalid := []struct {
		name    string
		mode    ProtectionMode
		outcome ProtectionOutcome
		local   bool
	}{
		{"exact explicitly unprotected", ProtectionStoreExact, ProtectionExplicitlyUnprotected, false},
		{"link exact fallback", ProtectionLinkOnly, ProtectionExactFallback, false},
		{"metadata external replayable", ProtectionMetadataOnly, ProtectionExternalReplayable, false},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			record := &ProtectionRecord{
				ID:          testID(t, IDPrefixProtectionRecord),
				WorkspaceID: workspace.ID,
				SubjectRef:  testID(t, IDPrefixNamespaceEntry),
				Mode:        test.mode,
				Outcome:     test.outcome,
			}
			if test.local {
				record.LocalRepresentationID = representation.ID
			}
			if err := store.InsertProtectionRecord(ctx, record); err == nil {
				t.Fatalf("insert %s/%s unexpectedly succeeded", test.mode, test.outcome)
			}
		})
	}
}

func int64Ptr(value int64) *int64 { return &value }
