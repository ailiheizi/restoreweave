package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

type preparedOnlyOutcomeRepo struct {
	*repository.Dir
	failed bool
}

func (r *preparedOnlyOutcomeRepo) PlaceRecord(ctx context.Context, role repository.RecordRole, body io.Reader) (repository.RecordReceipt, error) {
	receipt, err := r.Dir.PlaceRecord(ctx, role, body)
	if err != nil {
		return repository.RecordReceipt{}, err
	}
	if role == repository.RecordPreparedClosure && !r.failed {
		r.failed = true
		return repository.RecordReceipt{}, errors.New("response lost after prepared closure placement")
	}
	return receipt, nil
}

func TestPlanApplyPersistsUnknownExternalOutcomeForReplay(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	identity, anchor, err := exact.OpenSigningMaterial(t.TempDir(), "workspace:default", true)
	if err != nil {
		t.Fatal(err)
	}
	service := &exact.Service{
		Store: store, Repo: &preparedOnlyOutcomeRepo{Dir: repo},
		SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: "workspace:default", RequireSignedPublication: true,
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(service))
	root := t.TempDir()
	if err := writeTestFile(filepath.Join(root, "payload.txt"), []byte("prepared-only ambiguity")); err != nil {
		t.Fatal(err)
	}
	planned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": root}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var plan command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &plan); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"workspace_id": plan.WorkspaceID, "plan_id": plan.PlanID, "plan_digest": plan.PlanDigest}
	unknown := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, input))
	if unknown.Status != command.StatusUnknownExternalOutcome || !hasReasonCode(unknown, ReasonCodeUnknownExternalOutcome) {
		t.Fatalf("unknown plan.apply = %q: %+v", unknown.Status, unknown.Reasons)
	}
	if len(unknown.Reasons) != 1 || unknown.Reasons[0].Resolution == nil || unknown.Reasons[0].Resolution.Action != "reconcile" {
		t.Fatalf("unknown resolution = %+v", unknown.Reasons)
	}
	if unknown.Reasons[0].Details["state"] != "NEEDS_RECONCILIATION" {
		t.Fatalf("unknown details = %+v", unknown.Reasons[0].Details)
	}

	job, err := store.GetJobByPlanKind(ctx, plan.WorkspaceID, plan.PlanID, planApplyJobKind)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "NEEDS_RECONCILIATION" {
		t.Fatalf("job state = %q, want NEEDS_RECONCILIATION", job.State)
	}
	events, err := store.ListJobAuditEvents(ctx, plan.WorkspaceID, job.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Action != "JOB_NEEDS_RECONCILIATION" || events[len(events)-1].Outcome != "UNKNOWN_EXTERNAL_OUTCOME" {
		t.Fatalf("job events = %+v", events)
	}

	replayed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, input))
	if replayed.Status != command.StatusUnknownExternalOutcome || !hasReasonCode(replayed, ReasonCodeUnknownExternalOutcome) {
		t.Fatalf("replayed plan.apply = %q: %+v", replayed.Status, replayed.Reasons)
	}
}

func writeTestFile(path string, payload []byte) error {
	return os.WriteFile(path, payload, 0o600)
}
