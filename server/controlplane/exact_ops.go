package controlplane

import (
	"context"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
)

type planIngestInput struct {
	Root string `json:"root"`
}

type planRestoreInput struct {
	SnapshotRef string `json:"snapshot_ref"`
	Destination string `json:"destination"`
}

type snapshotRefInput struct {
	SnapshotRef string `json:"snapshot_ref"`
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
	ingested, err := d.exact.Ingest(ctx, input.Root)
	if err != nil {
		return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
	}
	return succeeded(env, started, command.PlanIngestData{
		WorkspaceID:    ingested.WorkspaceID,
		SourceID:       ingested.SourceID,
		ScanID:         ingested.ScanID,
		RootID:         ingested.RootID,
		SnapshotRef:    ingested.SnapshotRef,
		ManifestDigest: ingested.ManifestDigest,
		Files:          ingested.Files,
		Bytes:          ingested.Bytes,
	})
}

func (d *Dispatcher) handlePlanRestore(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input planRestoreInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.SnapshotRef) == "" || strings.TrimSpace(input.Destination) == "" {
		return invalidInputResult(env, started, errString("snapshot_ref and destination are required"))
	}
	restored, err := d.exact.Restore(ctx, input.SnapshotRef, input.Destination)
	if err != nil {
		return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
	}
	return succeeded(env, started, command.PlanRestoreData{
		SnapshotRef: restored.SnapshotRef,
		Destination: restored.Destination,
		Files:       restored.Files,
		Bytes:       restored.Bytes,
	})
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
		summaries = append(summaries, command.SnapshotSummary{
			SnapshotRef:    manifest.SnapshotRef,
			CreatedAt:      manifest.CreatedAt.UTC().Format(time.RFC3339),
			DisplayPath:    manifest.Binding.DisplayPath,
			ManifestDigest: manifest.ManifestDigest,
		})
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
	exported, err := d.exact.ExportRecovery(ctx, input.SnapshotRef, input.Destination)
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

func (d *Dispatcher) handleSnapshotVerify(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input snapshotRefInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.SnapshotRef) == "" {
		return invalidInputResult(env, started, errString("snapshot_ref is required"))
	}
	verified, err := d.exact.Verify(ctx, input.SnapshotRef)
	if err != nil {
		return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
	}
	return succeeded(env, started, command.SnapshotVerifyData{
		SnapshotRef: verified.SnapshotRef,
		Entries:     verified.Entries,
		Files:       verified.Files,
		Bytes:       verified.Bytes,
		OK:          true,
	})
}

type stringError string

func (err stringError) Error() string { return string(err) }

func errString(message string) error { return stringError(message) }
