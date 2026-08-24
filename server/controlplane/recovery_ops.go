package controlplane

import (
	"context"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
)

// handleRecoveryImport verifies and admits a portable recovery artifact produced
// by recovery.export against an independently supplied trust anchor. The
// repository, not the bundle, remains the recovery authority; the result is
// the authenticated admission summary.
func (d *Dispatcher) handleRecoveryImport(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input command.RecoveryImportInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.ArtifactPath) == "" || strings.TrimSpace(input.TrustAnchorPath) == "" {
		return invalidInputResult(env, started, errString("artifact_path and trust_anchor_path are required"))
	}
	imported, err := d.exact.ImportRecoveryArtifact(ctx, input.ArtifactPath, input.TrustAnchorPath, input.PublicationDomain)
	if err != nil {
		return exactOperationErrorResult(env, started, err)
	}
	return succeeded(env, started, command.RecoveryImportData{
		Schema:                imported.Schema,
		SnapshotRef:           imported.SnapshotRef,
		ManifestDigest:        imported.ManifestDigest,
		CommitDigest:          imported.CommitDigest,
		PreparedClosureDigest: imported.PreparedClosureDigest,
		Generation:            imported.Generation,
		TrustAnchorDigest:     imported.TrustAnchorDigest,
		FactHealth:            imported.FactHealth,
		Files:                 imported.Files,
		Bytes:                 imported.Bytes,
		CatalogCreated:        imported.CatalogCreated,
	})
}

// handleRecoveryTokenExport derives the deterministic proof envelope for one
// subject of a committed snapshot. The trust anchor is loaded from the
// optional trust_anchor_path, or the daemon's configured anchor is used.
func (d *Dispatcher) handleRecoveryTokenExport(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input command.RecoveryTokenExportInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.SnapshotRef) == "" || strings.TrimSpace(input.SubjectPath) == "" {
		return invalidInputResult(env, started, errString("snapshot_ref and subject_path are required"))
	}
	anchor := d.exact.TrustAnchor
	if strings.TrimSpace(input.TrustAnchorPath) != "" {
		loaded, err := exact.LoadTrustAnchor(input.TrustAnchorPath)
		if err != nil {
			return exactOperationErrorResult(env, started, err)
		}
		anchor = &loaded
	}
	if anchor == nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, "recovery trust anchor is unavailable"))
	}
	set, err := d.exact.BuildRecoveryTokenSet(ctx, input.SnapshotRef, input.SubjectPath, *anchor)
	if err != nil {
		return exactOperationErrorResult(env, started, err)
	}
	data := command.RecoveryTokenData{
		TokenSetSchema:    set.Schema,
		SnapshotRef:       set.SnapshotRef,
		PublicationDomain: set.PublicationDomain,
		SubjectPath:       set.SubjectPath,
		SubjectRef:        set.SubjectRef,
		ProtectionOutcome: set.ProtectionOutcome,
		TokenSetDigest:    set.SetDigest,
		Tokens:            make([]command.RecoveryTokenItemData, 0, len(set.Tokens)),
	}
	for _, token := range set.Tokens {
		item := command.RecoveryTokenItemData{
			TokenSchema:          token.TokenSchema,
			SnapshotRef:          token.SnapshotRef,
			PublicationDomain:    token.PublicationDomain,
			SubjectRef:           token.SubjectRef,
			RecoveryReferenceID:  token.RecoveryReferenceID,
			ReferenceKind:        token.ReferenceKind,
			ProtectionClaim:      token.ProtectionClaim,
			ExpectedContentID:    token.ExpectedContentID,
			ExpectedLength:       token.ExpectedLength,
			RecipeDigest:         token.RecipeDigest,
			LocatorSetDigest:     token.LocatorSetDigest,
			PublicationCommitRef: token.PublicationCommitRef,
			TrustAnchorRef:       token.TrustAnchorRef,
			Expiry:               token.Expiry,
			TokenDigest:          token.TokenDigest,
		}
		data.Tokens = append(data.Tokens, item)
	}
	if set.Unprotected != nil {
		data.Unprotected = &command.RecoveryUnprotectedData{
			Schema:               set.Unprotected.Schema,
			SnapshotRef:          set.Unprotected.SnapshotRef,
			SubjectRef:           set.Unprotected.SubjectRef,
			SubjectPath:          set.Unprotected.SubjectPath,
			ProtectionMode:       set.Unprotected.ProtectionMode,
			ProtectionOutcome:    set.Unprotected.ProtectionOutcome,
			ReasonCode:           set.Unprotected.ReasonCode,
			ExpectedLogicalBytes: set.Unprotected.ExpectedLogicalBytes,
			RecordDigest:         set.Unprotected.RecordDigest,
		}
	}
	// Keep the first-token projection for existing clients that predate
	// subject-scope token sets.
	if len(set.Tokens) > 0 {
		token := set.Tokens[0]
		data.TokenSchema = token.TokenSchema
		data.RecoveryReferenceID = token.RecoveryReferenceID
		data.ReferenceKind = token.ReferenceKind
		data.ProtectionClaim = token.ProtectionClaim
		data.ExpectedContentID = token.ExpectedContentID
		data.ExpectedLength = token.ExpectedLength
		data.RecipeDigest = token.RecipeDigest
		data.LocatorSetDigest = token.LocatorSetDigest
		data.PublicationCommitRef = token.PublicationCommitRef
		data.TrustAnchorRef = token.TrustAnchorRef
		data.Expiry = token.Expiry
		data.TokenDigest = token.TokenDigest
	}
	return succeeded(env, started, data)
}
