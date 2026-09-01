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

func TestSourceListShowsScanPublicationAndTransientOfflineProbe(t *testing.T) {
	ctx := context.Background()
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
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("source list"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	ingested := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSourceList, map[string]any{
		"workspace_id": ingested.WorkspaceID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("source.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var data command.SourceListData
	if err := json.Unmarshal(listed.Data, &data); err != nil {
		t.Fatalf("decode source.list: %v", err)
	}
	if data.WorkspaceID != ingested.WorkspaceID || len(data.Sources) != 1 {
		t.Fatalf("source.list data = %+v", data)
	}
	source := data.Sources[0]
	if source.SourceRef != ingested.SourceID || source.Kind != "LOCAL_TREE" || source.Locator != root || source.State != "ACTIVE" {
		t.Fatalf("source identity = %+v", source)
	}
	if source.Reachability != "AVAILABLE" || source.ReachabilityCheckedAt == "" || source.ReachabilityMessage != "" {
		t.Fatalf("source reachability = %+v", source)
	}
	if source.LatestScan == nil || source.LatestScan.State != "COMPLETE" || !source.LatestScan.FullTraversal || source.LatestScan.RegularFiles != 1 || source.LatestScan.BytesHashed != int64(len("source list")) {
		t.Fatalf("source latest scan = %+v", source.LatestScan)
	}
	if source.LatestSnapshotRef != ingested.SnapshotRef || source.LatestNamespaceRootID != ingested.RootID {
		t.Fatalf("source publication = %+v", source)
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove source fixture: %v", err)
	}
	offline := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSourceList, map[string]any{
		"workspace_id": ingested.WorkspaceID,
	}))
	if offline.Status != command.StatusSucceeded {
		t.Fatalf("offline source.list = %q: %+v", offline.Status, offline.Reasons)
	}
	if err := json.Unmarshal(offline.Data, &data); err != nil {
		t.Fatalf("decode offline source.list: %v", err)
	}
	source = data.Sources[0]
	if source.State != "ACTIVE" || source.Reachability != "UNAVAILABLE" || source.ReachabilityMessage == "" {
		t.Fatalf("offline source state = %+v", source)
	}
	if source.LatestSnapshotRef != ingested.SnapshotRef || source.LatestScan == nil || source.LatestScan.State != "COMPLETE" {
		t.Fatalf("offline source lost published evidence = %+v", source)
	}
}

func TestSourceListShowsPlannedSourceBeforeFirstPublication(t *testing.T) {
	ctx := context.Background()
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
	if err := os.WriteFile(filepath.Join(root, "planned.txt"), []byte("planned"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	planned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": root}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var plan command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &plan); err != nil {
		t.Fatalf("decode plan.ingest: %v", err)
	}
	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSourceList, map[string]any{
		"workspace_id": plan.WorkspaceID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("source.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var data command.SourceListData
	if err := json.Unmarshal(listed.Data, &data); err != nil {
		t.Fatalf("decode source.list: %v", err)
	}
	if len(data.Sources) != 1 || data.Sources[0].SourceRef != plan.SourceID || data.Sources[0].LatestScan != nil || data.Sources[0].LatestSnapshotRef != "" {
		t.Fatalf("planned source = %+v", data.Sources)
	}
	if data.Sources[0].Reachability != "AVAILABLE" || data.Sources[0].State != "ACTIVE" {
		t.Fatalf("planned source health = %+v", data.Sources[0])
	}
}

func TestSourceListRejectsUnknownWorkspace(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))
	result := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSourceList, map[string]any{
		"workspace_id": "wsp_00000000000000000000000000000000",
	}))
	if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeNotFound) {
		t.Fatalf("unknown workspace = %q: %+v", result.Status, result.Reasons)
	}
	if len(result.Reasons) == 0 || result.Reasons[0].Message != "workspace not found" {
		t.Fatalf("unknown workspace reason = %+v", result.Reasons)
	}
}
