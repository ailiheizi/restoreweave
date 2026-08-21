package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// recoveryHandlerHarness publishes a signed snapshot through the dispatcher
// exact lane and retains the exported bundle, anchor, and snapshot reference.
func newRecoveryHandlerHarness(t *testing.T) (*Dispatcher, command.RecoveryExportData, string, string) {
	t.Helper()
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	identity, anchor, err := exact.OpenSigningMaterial(filepath.Join(t.TempDir(), "recovery"), exact.DefaultPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: exact.DefaultPublicationDomain, RequireSignedPublication: true,
	}))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("recovery handler payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	ingested := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": root}))
	if ingested.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", ingested.Status, ingested.Reasons)
	}
	applied := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": mustPlanData(t, ingested).WorkspaceID,
		"plan_id":      mustPlanData(t, ingested).PlanID,
		"plan_digest":  mustPlanData(t, ingested).PlanDigest,
	}))
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("plan.apply = %q: %+v", applied.Status, applied.Reasons)
	}
	var applyData command.PlanApplyData
	if err := json.Unmarshal(applied.Data, &applyData); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(t.TempDir(), "recovery.json")
	exported := dispatcher.Handle(ctx, mustEnvelope(t, command.OpRecoveryExport, map[string]any{
		"snapshot_ref": applyData.SnapshotRef,
		"destination":  artifact,
	}))
	if exported.Status != command.StatusSucceeded {
		t.Fatalf("recovery.export = %q: %+v", exported.Status, exported.Reasons)
	}
	var exportData command.RecoveryExportData
	if err := json.Unmarshal(exported.Data, &exportData); err != nil {
		t.Fatal(err)
	}
	anchorPath := filepath.Join(t.TempDir(), "trust-anchor.json")
	if _, err := exact.ExportTrustAnchor(anchor, anchorPath); err != nil {
		t.Fatal(err)
	}
	return dispatcher, exportData, anchorPath, applyData.SnapshotRef
}

func mustPlanData(t *testing.T, result command.Result) command.PlanIngestData {
	t.Helper()
	var data command.PlanIngestData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDispatcherRecoveryImportAdmitsBundle(t *testing.T) {
	dispatcher, exportData, anchorPath, _ := newRecoveryHandlerHarness(t)
	ctx := context.Background()
	result := dispatcher.handleRecoveryImport(ctx, mustEnvelope(t, command.OpRecoveryImport, map[string]any{
		"artifact_path":     exportData.ArtifactPath,
		"trust_anchor_path": anchorPath,
	}), dispatcher.now().UTC())
	if result.Status != command.StatusSucceeded {
		t.Fatalf("recovery.import = %q: %+v", result.Status, result.Reasons)
	}
	var data command.RecoveryImportData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.SnapshotRef != exportData.SnapshotRef || data.ManifestDigest == "" ||
		data.CommitDigest == "" || data.PreparedClosureDigest == "" || data.Generation != 1 ||
		data.TrustAnchorDigest == "" || data.Schema != exact.RecoveryReferenceSchemaV2 ||
		data.FactHealth != exact.RecoveryFactHealthComplete || data.Files < 1 || !data.CatalogCreated {
		t.Fatalf("recovery.import data = %+v", data)
	}
}

func TestDispatcherRecoveryImportInvalidInputFails(t *testing.T) {
	dispatcher, exportData, anchorPath, _ := newRecoveryHandlerHarness(t)
	ctx := context.Background()
	for _, input := range []map[string]any{
		{},
		{"artifact_path": exportData.ArtifactPath},
		{"trust_anchor_path": anchorPath},
	} {
		result := dispatcher.handleRecoveryImport(ctx, mustEnvelope(t, command.OpRecoveryImport, input), dispatcher.now().UTC())
		if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeInvalidInput) {
			t.Fatalf("input %+v: status = %q reasons = %+v", input, result.Status, result.Reasons)
		}
	}
}

func TestDispatcherRecoveryTokenExportDerivesEnvelope(t *testing.T) {
	dispatcher, _, anchorPath, snapshotRef := newRecoveryHandlerHarness(t)
	ctx := context.Background()
	result := dispatcher.handleRecoveryTokenExport(ctx, mustEnvelope(t, command.OpRecoveryTokenExport, map[string]any{
		"snapshot_ref":      snapshotRef,
		"subject_path":      "blob.bin",
		"trust_anchor_path": anchorPath,
	}), dispatcher.now().UTC())
	if result.Status != command.StatusSucceeded {
		t.Fatalf("recovery.token.export = %q: %+v", result.Status, result.Reasons)
	}
	var data command.RecoveryTokenData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.TokenSchema != exact.RecoveryTokenSchemaV1 || data.SnapshotRef != snapshotRef ||
		data.SubjectRef == "" || data.RecoveryReferenceID == "" || data.PublicationCommitRef == "" ||
		data.TrustAnchorRef == "" || data.TokenDigest == "" {
		t.Fatalf("recovery.token.export data = %+v", data)
	}
	if !strings.HasPrefix(data.ExpectedContentID, "sha256:") {
		t.Fatalf("token expected identity = %+v", data)
	}

	second := dispatcher.handleRecoveryTokenExport(ctx, mustEnvelope(t, command.OpRecoveryTokenExport, map[string]any{
		"snapshot_ref":      snapshotRef,
		"subject_path":      "blob.bin",
		"trust_anchor_path": anchorPath,
	}), dispatcher.now().UTC())
	var secondData command.RecoveryTokenData
	if err := json.Unmarshal(second.Data, &secondData); err != nil {
		t.Fatal(err)
	}
	if secondData.TokenDigest != data.TokenDigest {
		t.Fatalf("token digest is not deterministic: %q vs %q", data.TokenDigest, secondData.TokenDigest)
	}
}

func TestDispatcherRecoveryTokenExportMissingSubjectFails(t *testing.T) {
	dispatcher, _, anchorPath, snapshotRef := newRecoveryHandlerHarness(t)
	ctx := context.Background()
	result := dispatcher.handleRecoveryTokenExport(ctx, mustEnvelope(t, command.OpRecoveryTokenExport, map[string]any{
		"snapshot_ref":      snapshotRef,
		"subject_path":      "missing.bin",
		"trust_anchor_path": anchorPath,
	}), dispatcher.now().UTC())
	if result.Status != command.StatusFailed {
		t.Fatalf("missing subject token = %q: %+v", result.Status, result.Reasons)
	}
}
