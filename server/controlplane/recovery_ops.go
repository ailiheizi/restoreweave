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
	token, err := d.exact.BuildRecoveryToken(ctx, input.SnapshotRef, input.SubjectPath, *anchor)
	if err != nil {
		return exactOperationErrorResult(env, started, err)
	}
	return succeeded(env, started, command.RecoveryTokenData{
		TokenSchema:          token.TokenSchema,
		SnapshotRef:          token.SnapshotRef,
		SubjectRef:           token.SubjectRef,
		RecoveryReferenceID:  token.RecoveryReferenceID,
		ExpectedContentID:    token.ExpectedContentID,
		ExpectedLength:       token.ExpectedLength,
		RecipeDigest:         token.RecipeDigest,
		PublicationCommitRef: token.PublicationCommitRef,
		TrustAnchorRef:       token.TrustAnchorRef,
		Expiry:               token.Expiry,
		TokenDigest:          token.TokenDigest,
	})
}
