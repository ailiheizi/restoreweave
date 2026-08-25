package controlplane

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type planIngestInput struct {
	Root             string                       `json:"root"`
	ProtectionMode   string                       `json:"protection_mode,omitempty"`
	FileProtection   map[string]string            `json:"file_protection,omitempty"`
	ConfirmLinkOnly  bool                         `json:"confirm_link_only,omitempty"`
	ExternalLocators []command.IngestLocatorInput `json:"external_locators,omitempty"`
}

type planRestoreInput struct {
	SnapshotRef string `json:"snapshot_ref"`
	Destination string `json:"destination"`
}

type snapshotRefInput struct {
	SnapshotRef string `json:"snapshot_ref"`
}

type snapshotVerifyInput struct {
	SnapshotRef string `json:"snapshot_ref"`
	Mode        string `json:"mode"`
	Destination string `json:"destination"`
}

func (d *Dispatcher) handlePlanIngest(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input planIngestInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.Root) == "" {
		return invalidInputResult(env, started, errString("root is required"))
	}
	locators := make([]exact.IngestLocator, 0, len(input.ExternalLocators))
	for _, locator := range input.ExternalLocators {
		locators = append(locators, exact.IngestLocator{
			Path: locator.Path, Kind: locator.Kind, Locator: locator.Locator,
			DisplayLocator: locator.DisplayLocator, CredentialRef: locator.CredentialRef,
			RightsEvidenceRef: locator.RightsEvidenceRef,
		})
	}
	fileProtection := make(map[string]sqlite.ProtectionMode, len(input.FileProtection))
	for path, mode := range input.FileProtection {
		fileProtection[path] = sqlite.ProtectionMode(mode)
	}
	inspected, err := d.exact.InspectIngest(ctx, input.Root, exact.IngestOptions{
		ProtectionMode:   sqlite.ProtectionMode(input.ProtectionMode),
		FileProtection:   fileProtection,
		ConfirmLinkOnly:  input.ConfirmLinkOnly,
		ExternalLocators: locators,
	})
	if err != nil {
		if errors.Is(err, exact.ErrBlocked) {
			return invalidInputResult(env, started, err)
		}
		return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
	}
	workspaceID, err := d.ensurePlanningSource(ctx, &inspected)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	planID, digest, jobID, err := d.recordIngestPlan(ctx, workspaceID, inspected)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	data := command.PlanIngestData{
		WorkspaceID:         workspaceID,
		SourceID:            inspected.SourceID,
		PlanID:              planID,
		PlanDigest:          digest,
		JobID:               jobID,
		ProtectionMode:      string(inspected.ProtectionMode),
		ProtectionDigest:    inspected.ProtectionDigest,
		FileProtection:      protectionModesForCommand(inspected.FileProtection),
		ProtectionDecisions: protectionDecisionsForCommand(inspected.ProtectionDecisions),
		BlockedEntries:      ingestPlanIssuesForCommand(inspected.BlockedEntries),
		Files:               inspected.Estimate.Files,
		Bytes:               inspected.Estimate.Bytes,
		LocalFiles:          inspected.Estimate.LocalFiles,
		LocalBytes:          inspected.Estimate.LocalBytes,
		NewBytes:            inspected.Estimate.NewBytes,
		LinkOnlyFiles:       inspected.Estimate.LinkOnlyFiles,
		LocatorCount:        inspected.Estimate.LocatorCount,
		State:               string(sqlite.PlanReady),
		Executable:          inspected.Executable,
		ConfigDigest:        firstNonEmpty(inspected.ConfigDigest, d.configDigest),
		SourceBasisDigest:   inspected.CaptureBasisDigest,
	}
	return succeeded(env, started, data)
}

func ingestPlanIssuesForCommand(issues []exact.IngestPlanIssue) []command.IngestPlanIssueData {
	if len(issues) == 0 {
		return nil
	}
	result := make([]command.IngestPlanIssueData, 0, len(issues))
	for _, issue := range issues {
		result = append(result, command.IngestPlanIssueData{
			RelativePath:   issue.RelativePath,
			Mode:           string(issue.Mode),
			PlannedOutcome: string(issue.PlannedOutcome),
			State:          issue.State,
			ReasonCode:     issue.ReasonCode,
			Message:        issue.Message,
		})
	}
	return result
}

func protectionDecisionsForCommand(decisions []exact.IngestProtectionDecision) []command.IngestProtectionDecisionData {
	if len(decisions) == 0 {
		return nil
	}
	result := make([]command.IngestProtectionDecisionData, 0, len(decisions))
	for _, decision := range decisions {
		result = append(result, command.IngestProtectionDecisionData{
			RelativePath:         decision.RelativePath,
			Mode:                 string(decision.Mode),
			PlannedOutcome:       string(decision.PlannedOutcome),
			ReasonCode:           decision.ReasonCode,
			ExpectedContentID:    decision.ExpectedContentID,
			ExpectedLogicalBytes: decision.ExpectedLogicalBytes,
			LocatorCount:         decision.LocatorCount,
		})
	}
	return result
}

func protectionModesForCommand(modes map[string]sqlite.ProtectionMode) map[string]string {
	if len(modes) == 0 {
		return nil
	}
	result := make(map[string]string, len(modes))
	for path, mode := range modes {
		result[path] = string(mode)
	}
	return result
}

func (d *Dispatcher) handlePlanRestore(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input planRestoreInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.SnapshotRef) == "" {
		return invalidInputResult(env, started, errString("snapshot_ref is required"))
	}
	inspected, err := d.exact.InspectRestore(ctx, input.SnapshotRef, strings.TrimSpace(input.Destination))
	if err != nil {
		return exactOperationErrorResult(env, started, err)
	}
	var planID, digest, workspaceID string
	if d.recoveryReader != nil {
		planID, digest, workspaceID, err = d.recordRecoveryRestorePlan(inspected)
	} else {
		planID, digest, workspaceID, err = d.recordRestorePlan(ctx, inspected)
	}
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	data := command.PlanRestoreData{
		WorkspaceID: workspaceID,
		SnapshotRef: inspected.SnapshotRef,
		Destination: inspected.Destination,
		Files:       inspected.Files,
		Bytes:       inspected.Bytes,
		Wrote:       false,
		PlanID:      planID,
		PlanDigest:  digest,
		State:       string(sqlite.PlanReady),
		Executable:  inspected.Destination != "",
	}
	return succeeded(env, started, data)
}

func (d *Dispatcher) handleSnapshotList(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	manifests, err := d.exact.ListSnapshots(ctx)
	if err != nil {
		return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
	}
	summaries := make([]command.SnapshotSummary, 0, len(manifests))
	for _, manifest := range manifests {
		summary := command.SnapshotSummary{
			SnapshotRef:    manifest.SnapshotRef,
			CreatedAt:      manifest.CreatedAt.UTC().Format(time.RFC3339),
			DisplayPath:    manifest.Binding.DisplayPath,
			ManifestDigest: manifest.ManifestDigest,
		}
		// Namespace browsing is an optional operational projection. Snapshot
		// discovery and recovery remain repository-backed when the catalog is
		// absent, including in the clean-install reader.
		if d.exact.Store != nil {
			if root, rootErr := d.exact.Store.GetNamespaceRootBySnapshotRef(ctx, manifest.SnapshotRef); rootErr == nil {
				summary.WorkspaceID = root.WorkspaceID
				summary.NamespaceRootID = root.ID
			}
		}
		summaries = append(summaries, summary)
	}
	return succeeded(env, started, command.SnapshotListData{Snapshots: summaries})
}

type snapshotDiffInput struct {
	FromSnapshotRef string `json:"from_snapshot_ref"`
	ToSnapshotRef   string `json:"to_snapshot_ref"`
}

type recoveryExportInput struct {
	SnapshotRef string `json:"snapshot_ref"`
	Destination string `json:"destination"`
}

type recoveryAnchorExportInput struct {
	Destination string `json:"destination"`
}

func (d *Dispatcher) handleSnapshotDiff(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input snapshotDiffInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.FromSnapshotRef) == "" || strings.TrimSpace(input.ToSnapshotRef) == "" {
		return invalidInputResult(env, started, errString("from_snapshot_ref and to_snapshot_ref are required"))
	}
	diffed, err := d.exact.DiffSnapshots(ctx, input.FromSnapshotRef, input.ToSnapshotRef)
	if err != nil {
		return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
	}
	return succeeded(env, started, command.SnapshotDiffData{
		FromSnapshotRef: diffed.FromSnapshotRef,
		ToSnapshotRef:   diffed.ToSnapshotRef,
		Changes:         diffed.Changes,
	})
}

func (d *Dispatcher) handleRecoveryExport(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input recoveryExportInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.SnapshotRef) == "" || strings.TrimSpace(input.Destination) == "" {
		return invalidInputResult(env, started, errString("snapshot_ref and destination are required"))
	}
	exported, err := d.exact.ExportRecoveryArtifact(ctx, input.SnapshotRef, input.Destination)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return conflictResult(env, started, err.Error())
		}
		return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
	}
	return succeeded(env, started, command.RecoveryExportData{
		SnapshotRef:         exported.SnapshotRef,
		Schema:              exported.Schema,
		ManifestDigest:      exported.ManifestDigest,
		ArtifactPath:        exported.ArtifactPath,
		Length:              exported.Length,
		Files:               exported.Files,
		Bytes:               exported.Bytes,
		IndependentlyStored: exported.IndependentlyStored,
	})
}

func (d *Dispatcher) handleRecoveryAnchorExport(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input recoveryAnchorExportInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.Destination) == "" {
		return invalidInputResult(env, started, errString("destination is required"))
	}
	path, err := d.exact.ExportTrustAnchor(input.Destination)
	if err != nil {
		if errors.Is(err, exact.ErrBlocked) {
			return conflictResult(env, started, err.Error())
		}
		return exactOperationErrorResult(env, started, err)
	}
	anchor := d.exact.TrustAnchor
	if anchor == nil {
		return failed(env, started, newReason(ReasonCodeCatalogError, "recovery trust anchor is unavailable"))
	}
	return succeeded(env, started, command.RecoveryAnchorExportData{
		Schema:            anchor.Schema,
		ArtifactPath:      path,
		PublicationDomain: anchor.PublicationDomain,
		WriterIdentity:    anchor.WriterIdentity,
		KeyID:             anchor.KeyID,
		Algorithm:         anchor.Algorithm,
		PublicKeyDigest:   anchor.PublicKeyDigest,
	})
}

func (d *Dispatcher) handleSnapshotVerify(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input snapshotVerifyInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.SnapshotRef) == "" {
		return invalidInputResult(env, started, errString("snapshot_ref is required"))
	}
	verified, err := d.exact.VerifyMode(ctx, input.SnapshotRef, input.Mode, input.Destination)
	if err != nil {
		return exactOperationErrorResult(env, started, err)
	}
	return succeeded(env, started, command.SnapshotVerifyData{
		SnapshotRef:     verified.SnapshotRef,
		Mode:            verified.Mode,
		AcceptedLevel:   verified.AcceptedLevel,
		Entries:         verified.Entries,
		Files:           verified.Files,
		Bytes:           verified.Bytes,
		AttemptedFiles:  verified.AttemptedFiles,
		AttemptedBytes:  verified.AttemptedBytes,
		PassedFiles:     verified.PassedFiles,
		PassedBytes:     verified.PassedBytes,
		OK:              verified.OK,
		RestoreVerified: verified.RestoreVerified,
		CatalogUsed:     verified.CatalogUsed,
	})
}

func exactOperationErrorResult(env command.Envelope, started time.Time, err error) command.Result {
	if errors.Is(err, exact.ErrRestorePlanStale) {
		return conflictResult(env, started, err.Error())
	}
	if errors.Is(err, exact.ErrBlocked) {
		return failed(env, started, newReason(ReasonCodeUnavailable, err.Error()))
	}
	return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
}

type stringError string

func (err stringError) Error() string { return string(err) }

func errString(message string) error { return stringError(message) }
