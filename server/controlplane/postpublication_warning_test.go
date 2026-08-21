package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

type warningPublicationProcessor struct{}

func (warningPublicationProcessor) ProcessPublication(context.Context, string, string, string) error {
	return errors.New("processor unavailable")
}

func TestPlanApplyReturnsDegradedButPersistsSuccessfulJob(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.txt"), []byte("durable payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store:     store,
		Repo:      repo,
		Processor: warningPublicationProcessor{},
		Indexer:   &search.Indexer{},
	}))

	planned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": root}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var plan command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &plan); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"workspace_id": plan.WorkspaceID, "plan_id": plan.PlanID, "plan_digest": plan.PlanDigest}
	applied := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, input))
	if applied.Status != command.StatusDegraded || !hasReasonCode(applied, ReasonCodeUnavailable) {
		t.Fatalf("plan.apply = %q: %+v", applied.Status, applied.Reasons)
	}
	var data command.PlanApplyData
	if err := json.Unmarshal(applied.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.State != "SUCCEEDED" || data.SnapshotRef == "" || len(data.Warnings) != 2 {
		t.Fatalf("apply data = %+v", data)
	}
	job, err := store.GetJobByPlanKind(ctx, plan.WorkspaceID, plan.PlanID, planApplyJobKind)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "SUCCEEDED" {
		t.Fatalf("job state = %q, want SUCCEEDED", job.State)
	}

	gotPlan := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanGet, map[string]any{
		"workspace_id": plan.WorkspaceID,
		"plan_id":      plan.PlanID,
	}))
	if gotPlan.Status != command.StatusSucceeded {
		t.Fatalf("plan.get = %q: %+v", gotPlan.Status, gotPlan.Reasons)
	}
	var planData command.PlanGetData
	if err := json.Unmarshal(gotPlan.Data, &planData); err != nil {
		t.Fatal(err)
	}
	if !planData.Applied || planData.Executable {
		t.Fatalf("plan.get = %+v", planData)
	}

	replayed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, input))
	if replayed.Status != command.StatusDegraded || !hasReasonCode(replayed, ReasonCodeUnavailable) {
		t.Fatalf("replayed apply = %q: %+v", replayed.Status, replayed.Reasons)
	}
	var replayData command.PlanApplyData
	if err := json.Unmarshal(replayed.Data, &replayData); err != nil {
		t.Fatal(err)
	}
	if !replayData.AlreadyApplied || len(replayData.Warnings) != len(data.Warnings) {
		t.Fatalf("replay data = %+v", replayData)
	}
}
