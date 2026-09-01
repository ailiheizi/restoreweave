package exact

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type representationPolicy struct {
	ownership sqlite.OwnershipMode
	codec     string
	access    sqlite.AccessMode
	wholeRead bool
	metadata  json.RawMessage
}

func representationProfile(mode sqlite.ProtectionMode) representationPolicy {
	if mode == sqlite.ProtectionStoreExact || mode == sqlite.ProtectionStoreExactWithExternalFallback {
		return representationPolicy{
			ownership: sqlite.OwnershipRestoreWeavePacks,
			codec:     "identity/sha256-v1",
			access:    sqlite.AccessRandomNative,
			metadata:  json.RawMessage(`{"availability":"local-verified"}`),
		}
	}
	codec := "observed-reference/sha256-v1"
	availability := "source-observed-not-retained"
	if mode == sqlite.ProtectionLinkOnly {
		codec = "external-reference/sha256-v1"
		availability = "external-unvalidated"
	}
	metadata, _ := json.Marshal(map[string]string{
		"availability":    availability,
		"protection_mode": string(mode),
	})
	return representationPolicy{
		ownership: sqlite.OwnershipEngineManaged,
		codec:     codec,
		access:    sqlite.AccessWholeObjectOnly,
		wholeRead: true,
		metadata:  metadata,
	}
}

func fileVersionVerificationRef(item prepared) string {
	if item.protectionMode == sqlite.ProtectionStoreExact || item.protectionMode == sqlite.ProtectionStoreExactWithExternalFallback {
		return "readback:" + item.entry.Content.ContentID
	}
	return "source-observation:" + item.entry.Content.ContentID
}

func protectionPolicyRef(item prepared) string {
	ref := "policy:" + string(item.protectionMode)
	if item.protectionMode == sqlite.ProtectionLinkOnly {
		ref += ":operator-confirmed"
	}
	if item.protectionDigest != "" {
		ref += "@" + item.protectionDigest
	}
	if item.protectionReason != "" {
		ref += ":" + item.protectionReason
	}
	return ref
}

func insertPreparedProtection(ctx context.Context, tx *sqlite.Tx, workspaceID string, item prepared) (string, error) {
	protection := sqlite.ProtectionRecord{
		ID:                  item.protectionID,
		WorkspaceID:         workspaceID,
		SubjectRef:          item.subjectRef,
		Mode:                sqlite.ProtectionMetadataOnly,
		Outcome:             sqlite.ProtectionUnavailable,
		PolicyDecisionRef:   "policy:metadata-only-non-file",
		LastVerificationRef: "",
	}
	if item.entryType == sqlite.EntryFile {
		protection.Mode = item.protectionMode
		protection.Outcome = item.protectionOutcome
		if item.entry.Content != nil {
			size := item.entry.Content.BytesRead
			protection.ExpectedContentID = item.entry.Content.ContentID
			protection.ExpectedLogicalLength = &size
		} else if item.entry.Before != nil {
			size := item.entry.Before.Size
			protection.ExpectedLogicalLength = &size
		}
		protection.Metadata, _ = json.Marshal(map[string]string{
			"reason_code":       item.protectionReason,
			"protection_digest": item.protectionDigest,
		})
		switch item.protectionMode {
		case sqlite.ProtectionStoreExact, sqlite.ProtectionStoreExactWithExternalFallback:
			protection.LocalRepresentationID = item.representation
			protection.PolicyDecisionRef = protectionPolicyRef(item)
			protection.LastVerificationRef = "readback:" + item.entry.Content.ContentID
		case sqlite.ProtectionLinkOnly:
			protection.PolicyDecisionRef = protectionPolicyRef(item)
		case sqlite.ProtectionMetadataOnly:
			protection.PolicyDecisionRef = protectionPolicyRef(item)
		}
	}
	storedProtection, err := tx.UpsertProtectionRecord(ctx, &protection)
	if err != nil {
		return "", err
	}
	// Protection is a current-state projection keyed by stable subject. A
	// rescan may revise that row while immutable recovery references retain
	// the actual protection record ID returned by the upsert.
	protection = storedProtection
	if item.entryType != sqlite.EntryFile {
		return protection.ID, nil
	}

	if item.entry.Content == nil {
		return protection.ID, nil
	}
	size := item.entry.Content.BytesRead
	if item.recoveryID != "" {
		recipe, verification, err := exactRecoveryRecords(
			item.entry.Content.ContentID, size, item.representation,
		)
		if err != nil {
			return "", err
		}
		if err := tx.InsertRecoveryReference(ctx, &sqlite.RecoveryReference{
			ID:                    item.recoveryID,
			WorkspaceID:           workspaceID,
			ProtectionRecordID:    protection.ID,
			SubjectRef:            item.subjectRef,
			Kind:                  sqlite.RecoveryExactRepresentation,
			Priority:              0,
			Claim:                 sqlite.RecoveryClaimRestoreVerified,
			ExpectedContentID:     item.entry.Content.ContentID,
			ExpectedLogicalLength: &size,
			RepresentationID:      item.representation,
			CodecProfileRef:       "identity/sha256-v1",
			Recipe:                recipe,
			Verification:          verification,
			Status:                "VERIFIED",
			PolicyRef:             protectionPolicyRef(item),
			RecordDigest:          item.entry.Content.ContentID,
		}); err != nil {
			return "", err
		}
	}
	if item.externalBindingID == "" {
		return protection.ID, nil
	}

	digest, err := locatorBindingDigest(item.entry.Content.ContentID, ingestLocators(item.externalLocators))
	if err != nil {
		return "", err
	}
	bindingMetadata, _ := json.Marshal(map[string]any{
		"capture_relative_path": item.entry.RelativePath,
		"validated":             false,
	})
	if err := tx.InsertExternalBinding(ctx, &sqlite.ExternalBinding{
		ID:             item.externalBindingID,
		WorkspaceID:    workspaceID,
		SubjectRef:     item.subjectRef,
		ProviderKind:   "OPERATOR_SUPPLIED_LOCATORS",
		StableIdentity: "content:" + item.entry.Content.ContentID,
		BindingDigest:  digest,
		Metadata:       bindingMetadata,
	}); err != nil {
		return "", err
	}
	for _, prepared := range item.externalLocators {
		locator := prepared.locator
		if err := tx.InsertExternalLocator(ctx, &sqlite.ExternalLocator{
			ID:                    prepared.id,
			WorkspaceID:           workspaceID,
			BindingID:             item.externalBindingID,
			Priority:              prepared.priority,
			Kind:                  locator.Kind,
			Locator:               locator.Locator,
			DisplayLocator:        locator.DisplayLocator,
			ExpectedContentID:     item.entry.Content.ContentID,
			ExpectedLogicalLength: &size,
			CredentialRef:         locator.CredentialRef,
			RightsEvidenceRef:     locator.RightsEvidenceRef,
			Availability:          "UNKNOWN",
			ValidationStatus:      "UNVALIDATED",
			Metadata:              json.RawMessage(`{"operator_supplied":true}`),
		}); err != nil {
			return "", err
		}
	}
	recipe, verification, err := externalRecoveryRecords(item, digest)
	if err != nil {
		return "", err
	}
	priority := int64(0)
	if item.recoveryID != "" {
		priority = 1
	}
	if err := tx.InsertRecoveryReference(ctx, &sqlite.RecoveryReference{
		ID:                    item.externalRecoveryID,
		WorkspaceID:           workspaceID,
		ProtectionRecordID:    protection.ID,
		SubjectRef:            item.subjectRef,
		Kind:                  sqlite.RecoveryExternalLocator,
		Priority:              priority,
		Claim:                 sqlite.RecoveryClaimLinkOnlyUnprotected,
		ExpectedContentID:     item.entry.Content.ContentID,
		ExpectedLogicalLength: &size,
		ExternalBindingID:     item.externalBindingID,
		Recipe:                recipe,
		Verification:          verification,
		Status:                "UNVALIDATED",
		PolicyRef:             protectionPolicyRef(item),
		OperatorDecisionRef:   "operator:ingest",
		RecordDigest:          digest,
	}); err != nil {
		return "", err
	}
	return protection.ID, nil
}

func ingestLocators(items []preparedLocator) []IngestLocator {
	locators := make([]IngestLocator, 0, len(items))
	for _, item := range items {
		locators = append(locators, item.locator)
	}
	return locators
}

func externalRecoveryRecords(item prepared, bindingDigest string) (json.RawMessage, json.RawMessage, error) {
	locators := make([]map[string]any, 0, len(item.externalLocators))
	for _, prepared := range item.externalLocators {
		locators = append(locators, map[string]any{
			"kind":     prepared.locator.Kind,
			"locator":  prepared.locator.Locator,
			"priority": prepared.priority,
		})
	}
	recipe, err := json.Marshal(map[string]any{
		"schema":            "org.restoreweave.external-recovery-recipe.v1",
		"binding_digest":    bindingDigest,
		"content_id":        item.entry.Content.ContentID,
		"logical_length":    item.entry.Content.BytesRead,
		"relative_path":     item.entry.RelativePath,
		"locators":          locators,
		"acquire_isolated":  true,
		"verify_before_use": true,
	})
	if err != nil {
		return nil, nil, err
	}
	verification, err := json.Marshal(map[string]any{
		"verified": false,
		"status":   "UNVALIDATED",
		"reason":   "operator-supplied locators were not independently reacquired",
	})
	if err != nil {
		return nil, nil, err
	}
	return recipe, verification, nil
}

func manifestExternalReference(item prepared, priority int64) (ManifestRecoveryReference, error) {
	digest, err := locatorBindingDigest(item.entry.Content.ContentID, ingestLocators(item.externalLocators))
	if err != nil {
		return ManifestRecoveryReference{}, err
	}
	recipe, verification, err := externalRecoveryRecords(item, digest)
	if err != nil {
		return ManifestRecoveryReference{}, err
	}
	locators := make([]ManifestExternalLocator, 0, len(item.externalLocators))
	for _, prepared := range item.externalLocators {
		locators = append(locators, ManifestExternalLocator{
			LocatorID:             prepared.id,
			Priority:              prepared.priority,
			Kind:                  prepared.locator.Kind,
			Locator:               prepared.locator.Locator,
			DisplayLocator:        prepared.locator.DisplayLocator,
			ExpectedContentID:     item.entry.Content.ContentID,
			ExpectedLogicalLength: item.entry.Content.BytesRead,
			Availability:          "UNKNOWN",
			ValidationStatus:      "UNVALIDATED",
		})
	}
	return ManifestRecoveryReference{
		ReferenceID:       item.externalRecoveryID,
		Kind:              string(sqlite.RecoveryExternalLocator),
		Claim:             string(sqlite.RecoveryClaimLinkOnlyUnprotected),
		Priority:          priority,
		ExternalBindingID: item.externalBindingID,
		ExternalLocators:  locators,
		Status:            "UNVALIDATED",
		Recipe:            recipe,
		Verification:      verification,
	}, nil
}

func validatePreparedProtection(item prepared) error {
	if item.entryType != sqlite.EntryFile {
		return nil
	}
	if item.entry.Content == nil && item.protectionMode != sqlite.ProtectionMetadataOnly {
		return fmt.Errorf("regular file %q has no content", item.entry.RelativePath)
	}
	if item.entry.Content == nil && !metadataOnlyEntryEvidence(item.entry) {
		return fmt.Errorf("regular file %q has no stable namespace metadata", item.entry.RelativePath)
	}
	if item.protectionMode == sqlite.ProtectionLinkOnly && item.externalBindingID == "" {
		return fmt.Errorf("LINK_ONLY file %q has no external binding", item.entry.RelativePath)
	}
	return nil
}
