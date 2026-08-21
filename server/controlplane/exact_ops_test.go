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

func TestDispatcherExactIngestAndRestore(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("exact-lane"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ingested := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": root}))
	if ingested.Status != command.StatusSucceeded {
		t.Fatalf("ingest = %q: %+v", ingested.Status, ingested.Reasons)
	}
	var ingestData command.PlanIngestData
	if err := json.Unmarshal(ingested.Data, &ingestData); err != nil {
		t.Fatalf("decode ingest: %v", err)
	}
	if ingestData.SnapshotRef != "" || ingestData.Files < 1 || !ingestData.Executable || ingestData.State != "READY" {
		t.Fatalf("ingest data = %+v", ingestData)
	}
	if ingestData.PlanID == "" || ingestData.PlanDigest == "" {
		t.Fatalf("ingest plan receipt missing: %+v", ingestData)
	}
	gotPlan := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanGet, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
	}))
	if gotPlan.Status != command.StatusSucceeded {
		t.Fatalf("plan.get = %q: %+v", gotPlan.Status, gotPlan.Reasons)
	}
	var planData command.PlanGetData
	if err := json.Unmarshal(gotPlan.Data, &planData); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if planData.Applied || !planData.Executable || planData.PlanDigest != ingestData.PlanDigest {
		t.Fatalf("plan data = %+v", planData)
	}
	applied := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
		"plan_digest":  ingestData.PlanDigest,
	}))
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("plan.apply = %q: %+v", applied.Status, applied.Reasons)
	}
	var applyData command.PlanApplyData
	if err := json.Unmarshal(applied.Data, &applyData); err != nil {
		t.Fatalf("decode apply: %v", err)
	}
	if applyData.AlreadyApplied || applyData.SnapshotRef == "" {
		t.Fatalf("apply data = %+v", applyData)
	}
	ingestData.SnapshotRef = applyData.SnapshotRef
	ingestData.ManifestDigest = applyData.ManifestDigest
	ingestData.RootID = applyData.RootID
	ingestData.ScanID = applyData.ScanID
	replayed := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
		"plan_digest":  ingestData.PlanDigest,
	}))
	var replayData command.PlanApplyData
	if err := json.Unmarshal(replayed.Data, &replayData); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replayed.Status != command.StatusSucceeded || !replayData.AlreadyApplied || replayData.SnapshotRef != ingestData.SnapshotRef {
		t.Fatalf("replayed apply = %q %+v", replayed.Status, replayData)
	}
	wrong := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
		"plan_digest":  "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}))
	if wrong.Status != command.StatusFailed || !hasReasonCode(wrong, ReasonCodeConflict) {
		t.Fatalf("wrong digest apply: status = %q reasons = %+v", wrong.Status, wrong.Reasons)
	}

	listed := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotList, map[string]any{}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.list = %q: %+v", listed.Status, listed.Reasons)
	}

	verified := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.verify = %q: %+v", verified.Status, verified.Reasons)
	}
	var verifyData command.SnapshotVerifyData
	if err := json.Unmarshal(verified.Data, &verifyData); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	if !verifyData.OK || verifyData.Mode != command.VerifyFullBytes || verifyData.RestoreVerified || verifyData.CatalogUsed {
		t.Fatalf("default verify = %+v", verifyData)
	}

	probe := filepath.Join(t.TempDir(), "must-not-be-created")
	preflight := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if preflight.Status != command.StatusSucceeded {
		t.Fatalf("restore preflight = %q: %+v", preflight.Status, preflight.Reasons)
	}
	var preflightData command.PlanRestoreData
	if err := json.Unmarshal(preflight.Data, &preflightData); err != nil {
		t.Fatalf("decode preflight: %v", err)
	}
	if preflightData.Wrote || preflightData.Files < 1 || preflightData.Destination != "" || preflightData.PlanID == "" {
		t.Fatalf("preflight = %+v", preflightData)
	}
	if _, err := os.Lstat(probe); !os.IsNotExist(err) {
		t.Fatalf("preflight created %s: %v", probe, err)
	}
	preflightAgain := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	var preflightAgainData command.PlanRestoreData
	if err := json.Unmarshal(preflightAgain.Data, &preflightAgainData); err != nil {
		t.Fatalf("decode second preflight: %v", err)
	}
	if preflightAgainData.PlanID != preflightData.PlanID || preflightAgainData.PlanDigest != preflightData.PlanDigest {
		t.Fatalf("preflight not idempotent: %+v vs %+v", preflightData, preflightAgainData)
	}
	applyPreflight := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      preflightData.PlanID,
		"plan_digest":  preflightData.PlanDigest,
	}))
	if applyPreflight.Status != command.StatusFailed || !hasReasonCode(applyPreflight, ReasonCodeConflict) {
		t.Fatalf("apply preflight restore = %q: %+v", applyPreflight.Status, applyPreflight.Reasons)
	}

	dest := filepath.Join(t.TempDir(), "out")
	restored := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"destination":  dest,
	}))
	if restored.Status != command.StatusSucceeded {
		t.Fatalf("restore = %q: %+v", restored.Status, restored.Reasons)
	}
	var restorePlan command.PlanRestoreData
	if err := json.Unmarshal(restored.Data, &restorePlan); err != nil {
		t.Fatalf("decode restore plan: %v", err)
	}
	if restorePlan.Wrote || !restorePlan.Executable {
		t.Fatalf("restore plan wrote early: %+v", restorePlan)
	}
	restoreApplied := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      restorePlan.PlanID,
		"plan_digest":  restorePlan.PlanDigest,
	}))
	if restoreApplied.Status != command.StatusSucceeded {
		t.Fatalf("restore apply = %q: %+v", restoreApplied.Status, restoreApplied.Reasons)
	}
	got, err := os.ReadFile(filepath.Join(dest, "blob.bin"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(got) != "exact-lane" {
		t.Fatalf("restored payload = %q", got)
	}

	artifact := filepath.Join(t.TempDir(), "recovery.json")
	exported := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpRecoveryExport, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"destination":  artifact,
	}))
	if exported.Status != command.StatusSucceeded {
		t.Fatalf("recovery.export = %q: %+v", exported.Status, exported.Reasons)
	}
	var exportData command.RecoveryExportData
	if err := json.Unmarshal(exported.Data, &exportData); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if !exportData.IndependentlyStored || exportData.Schema == "" || exportData.ManifestDigest == "" || exportData.Files < 1 {
		t.Fatalf("export data = %+v", exportData)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("exported artifact missing: %v", err)
	}
	again := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpRecoveryExport, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"destination":  artifact,
	}))
	if again.Status != command.StatusFailed || !hasReasonCode(again, ReasonCodeConflict) {
		t.Fatalf("overwrite export: status = %q reasons = %+v", again.Status, again.Reasons)
	}

	secondRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(secondRoot, "blob.bin"), []byte("exact-lane"), 0o600); err != nil {
		t.Fatalf("write second blob: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secondRoot, "extra.txt"), []byte("second"), 0o600); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	secondData := mustAppliedIngest(t, context.Background(), dispatcher, map[string]any{"root": secondRoot})
	diffed := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotDiff, map[string]any{
		"from_snapshot_ref": ingestData.SnapshotRef,
		"to_snapshot_ref":   secondData.SnapshotRef,
	}))
	if diffed.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.diff = %q: %+v", diffed.Status, diffed.Reasons)
	}
	var diffData command.SnapshotDiffData
	if err := json.Unmarshal(diffed.Data, &diffData); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	foundAdded := false
	for _, change := range diffData.Changes {
		if change.Kind == command.DiffAdded && change.Path == "extra.txt" {
			foundAdded = true
		}
	}
	if !foundAdded {
		t.Fatalf("diff missing extra.txt add: %+v", diffData.Changes)
	}

	resolved := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpNamespaceResolve, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
		"path":         "blob.bin",
	}))
	if resolved.Status != command.StatusSucceeded {
		t.Fatalf("namespace.resolve = %q: %+v", resolved.Status, resolved.Reasons)
	}
	var resolveData command.NamespaceResolveData
	if err := json.Unmarshal(resolved.Data, &resolveData); err != nil {
		t.Fatalf("decode resolve: %v", err)
	}
	if resolveData.PathRef == "" || resolveData.Entry.ContentID == "" {
		t.Fatalf("resolve data = %+v", resolveData)
	}

	reps := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpRepresentationList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"subject_ref":  resolveData.PathRef,
	}))
	if reps.Status != command.StatusSucceeded {
		t.Fatalf("representation.list = %q: %+v", reps.Status, reps.Reasons)
	}
	var repData command.RepresentationListData
	if err := json.Unmarshal(reps.Data, &repData); err != nil {
		t.Fatalf("decode representations: %v", err)
	}
	if len(repData.Representations) != 1 {
		t.Fatalf("representations = %+v", repData.Representations)
	}
	item := repData.Representations[0]
	if item.Class != command.RepresentationClassExact || !item.Authoritative ||
		item.Placement != command.RepresentationPlacementPresent || item.Verified == nil || !*item.Verified {
		t.Fatalf("exact representation = %+v", item)
	}
}

func TestDispatcherRecoveryAnchorExportIsPublicAndExclusive(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	_, anchor, err := exact.OpenSigningMaterial(filepath.Join(t.TempDir(), "recovery"), exact.DefaultPublicationDomain, true)
	if err != nil {
		t.Fatalf("open signing material: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store, Repo: repo, TrustAnchor: &anchor,
	}))

	destination := filepath.Join(t.TempDir(), "trust-anchor.json")
	exported := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpRecoveryAnchorExport, map[string]any{
		"destination": destination,
	}))
	if exported.Status != command.StatusSucceeded {
		t.Fatalf("anchor export = %q: %+v", exported.Status, exported.Reasons)
	}
	var data command.RecoveryAnchorExportData
	if err := json.Unmarshal(exported.Data, &data); err != nil {
		t.Fatalf("decode anchor export: %v", err)
	}
	if data.ArtifactPath != destination || data.Schema != exact.RecoveryTrustAnchorSchemaV1 || data.KeyID != anchor.KeyID {
		t.Fatalf("anchor export data = %+v", data)
	}
	payload, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read exported anchor: %v", err)
	}
	if strings.Contains(string(payload), "private_key") {
		t.Fatalf("exported trust anchor contains private key material: %s", payload)
	}

	again := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpRecoveryAnchorExport, map[string]any{
		"destination": destination,
	}))
	if again.Status != command.StatusFailed || !hasReasonCode(again, ReasonCodeConflict) {
		t.Fatalf("anchor overwrite = %q: %+v", again.Status, again.Reasons)
	}
}

func TestDispatcherLinkOnlyIngestIsExplicitAndVisible(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store, Repo: repo, AllowLinkOnly: true, LinkOnlyRequiresConfirmation: true,
	}))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "release.bin"), []byte("link-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{
		"root":            root,
		"protection_mode": "LINK_ONLY",
		"external_locators": []map[string]any{{
			"locator": "https://downloads.example.test/release.bin",
		}},
	}
	unconfirmed := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanIngest, input))
	if unconfirmed.Status != command.StatusFailed || !hasReasonCode(unconfirmed, ReasonCodeInvalidInput) {
		t.Fatalf("unconfirmed LINK_ONLY = %s %+v", unconfirmed.Status, unconfirmed.Reasons)
	}
	input["confirm_link_only"] = true
	data := mustAppliedIngest(t, context.Background(), dispatcher, input)
	if data.ProtectionMode != "LINK_ONLY" || data.Files != 1 || data.LocalFiles != 0 ||
		data.LocalBytes != 0 || data.NewBytes != 0 || data.LinkOnlyFiles != 1 || data.LocatorCount != 1 {
		t.Fatalf("LINK_ONLY data = %+v", data)
	}
	preflight := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": data.SnapshotRef,
	}))
	if preflight.Status != command.StatusFailed || !hasReasonCode(preflight, ReasonCodeUnavailable) || hasReasonCode(preflight, ReasonCodeCatalogError) {
		t.Fatalf("LINK_ONLY restore preflight = %s %+v", preflight.Status, preflight.Reasons)
	}
	verified := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": data.SnapshotRef,
	}))
	if verified.Status != command.StatusFailed || !hasReasonCode(verified, ReasonCodeUnavailable) || hasReasonCode(verified, ReasonCodeCatalogError) {
		t.Fatalf("LINK_ONLY full verify = %s %+v", verified.Status, verified.Reasons)
	}
	if len(verified.Reasons) == 0 || strings.Contains(verified.Reasons[0].Message, "exact ingest is blocked") {
		t.Fatalf("LINK_ONLY blocked message = %+v", verified.Reasons)
	}
}

func TestSnapshotVerifyModes(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))
	root := t.TempDir()
	for name, body := range map[string]string{
		"a.txt": "one", "b.txt": "two", "c.txt": "three", "d.txt": "four",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	ingestData := mustAppliedIngest(t, context.Background(), dispatcher, map[string]any{"root": root})

	meta := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"mode":         command.VerifyAuthenticatedMetadata,
	}))
	var metaData command.SnapshotVerifyData
	if err := json.Unmarshal(meta.Data, &metaData); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.Status != command.StatusSucceeded || metaData.AcceptedLevel != command.VerifyAuthenticatedMetadata ||
		metaData.AttemptedFiles != 4 || metaData.RestoreVerified {
		t.Fatalf("metadata verify = %q %+v", meta.Status, metaData)
	}

	sampled := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"mode":         command.VerifySampledContent,
	}))
	var sampledData command.SnapshotVerifyData
	if err := json.Unmarshal(sampled.Data, &sampledData); err != nil {
		t.Fatalf("decode sampled: %v", err)
	}
	if sampled.Status != command.StatusSucceeded || sampledData.AcceptedLevel != command.VerifySampledContent ||
		sampledData.Files != 4 || sampledData.AttemptedFiles < 1 || sampledData.AttemptedFiles >= 4 ||
		sampledData.AcceptedLevel == command.VerifyFullBytes {
		t.Fatalf("sampled verify = %q %+v", sampled.Status, sampledData)
	}

	clean := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"mode":         command.VerifyCleanRecovery,
	}))
	var cleanData command.SnapshotVerifyData
	if err := json.Unmarshal(clean.Data, &cleanData); err != nil {
		t.Fatalf("decode clean: %v", err)
	}
	if clean.Status != command.StatusSucceeded || cleanData.CatalogUsed || cleanData.RestoreVerified ||
		cleanData.AttemptedFiles != 4 {
		t.Fatalf("clean-recovery = %q %+v", clean.Status, cleanData)
	}

	missingDest := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"mode":         command.VerifyRestoreDrill,
	}))
	if missingDest.Status != command.StatusFailed {
		t.Fatalf("restore-drill without dest = %q: %+v", missingDest.Status, missingDest.Reasons)
	}

	dest := filepath.Join(t.TempDir(), "drill")
	drill := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"mode":         command.VerifyRestoreDrill,
		"destination":  dest,
	}))
	var drillData command.SnapshotVerifyData
	if err := json.Unmarshal(drill.Data, &drillData); err != nil {
		t.Fatalf("decode drill: %v", err)
	}
	if drill.Status != command.StatusSucceeded || !drillData.RestoreVerified || drillData.AcceptedLevel != command.VerifyRestoreDrill {
		t.Fatalf("restore-drill = %q %+v", drill.Status, drillData)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Fatalf("drill dest: %v", err)
	}

	unknown := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"mode":         "looks-fine",
	}))
	if unknown.Status != command.StatusFailed {
		t.Fatalf("unknown mode = %q: %+v", unknown.Status, unknown.Reasons)
	}
}
