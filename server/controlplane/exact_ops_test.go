package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	if ingestData.SnapshotRef == "" || ingestData.Files < 1 {
		t.Fatalf("ingest data = %+v", ingestData)
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

	dest := filepath.Join(t.TempDir(), "out")
	restored := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"destination":  dest,
	}))
	if restored.Status != command.StatusSucceeded {
		t.Fatalf("restore = %q: %+v", restored.Status, restored.Reasons)
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
	if !exportData.IndependentlyStored || exportData.ManifestDigest == "" || exportData.Files < 1 {
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
	second := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": secondRoot}))
	if second.Status != command.StatusSucceeded {
		t.Fatalf("second ingest = %q: %+v", second.Status, second.Reasons)
	}
	var secondData command.PlanIngestData
	if err := json.Unmarshal(second.Data, &secondData); err != nil {
		t.Fatalf("decode second ingest: %v", err)
	}
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
