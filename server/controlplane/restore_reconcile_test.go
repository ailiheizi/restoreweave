package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestPlanApplyRestoreReconcilesOutputAfterJobCrash(t *testing.T) {
	h := newPhase2Harness(t)
	ctx := context.Background()
	ingest := mustAppliedIngest(t, ctx, h.dispatcher, map[string]any{"root": h.root})
	destination := filepath.Join(t.TempDir(), "restore-target")
	planned := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": ingest.SnapshotRef,
		"destination":  destination,
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.restore = %q: %+v", planned.Status, planned.Reasons)
	}
	var restoreData command.PlanRestoreData
	if err := json.Unmarshal(planned.Data, &restoreData); err != nil {
		t.Fatalf("decode plan.restore: %v", err)
	}
	record, err := h.store.GetPlan(ctx, ingest.WorkspaceID, restoreData.PlanID)
	if err != nil {
		t.Fatalf("get restore plan: %v", err)
	}
	body, err := decodePlanBody(record.Plan)
	if err != nil || body.Restore == nil {
		t.Fatalf("decode restore plan body: %v", err)
	}

	// The writer completed, but the process crashed before PLAN_APPLY could
	// persist its result. The retry must validate and claim this output.
	committed, err := h.dispatcher.exact.ApplyRestorePlan(ctx, *body.Restore)
	if err != nil {
		t.Fatalf("commit restore output: %v", err)
	}
	jobID, err := sqlite.NewStableID(sqlite.IDPrefixJob)
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute)
	if err := h.store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertJob(ctx, &sqlite.Job{
			ID: jobID, WorkspaceID: record.WorkspaceID, PlanID: record.ID,
			Kind: planApplyJobKind, State: sqlite.JobRunning, Attempt: 1,
			MaxAttempts: 3, LeaseUntil: &expired,
		})
	}); err != nil {
		t.Fatalf("insert crashed restore job: %v", err)
	}

	retried := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": record.WorkspaceID,
		"plan_id":      record.ID,
		"plan_digest":  record.PlanDigest,
	}))
	if retried.Status != command.StatusSucceeded {
		t.Fatalf("reconciled restore apply = %q: %+v", retried.Status, retried.Reasons)
	}
	var applied command.PlanApplyData
	if err := json.Unmarshal(retried.Data, &applied); err != nil {
		t.Fatalf("decode reconciled apply: %v", err)
	}
	if applied.SnapshotRef != committed.SnapshotRef || applied.Destination != committed.Destination ||
		applied.Files != committed.Files || applied.Bytes != committed.Bytes {
		t.Fatalf("reconciled result = %+v, committed = %+v", applied, committed)
	}
	job, err := h.store.GetJobByPlanKind(ctx, record.WorkspaceID, record.ID, planApplyJobKind)
	if err != nil {
		t.Fatalf("get reconciled job: %v", err)
	}
	if job.State != sqlite.JobSucceeded {
		t.Fatalf("reconciled job state = %q, want SUCCEEDED", job.State)
	}
	if _, err := os.Stat(filepath.Join(destination, "payload.txt")); err != nil {
		t.Fatalf("reconciled output missing: %v", err)
	}
}
