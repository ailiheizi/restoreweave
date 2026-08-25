package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
	_ "modernc.org/sqlite"
)

type preparedOnlyOutcomeRepo struct {
	*repository.Dir
	failed bool
}

type portableChildUnknownOnceRepo struct {
	*repository.Dir
	failed bool
}

type cancelAfterRootCommitRepo struct {
	*repository.Dir
	cancelled bool
	cancel    context.CancelFunc
}

func (r *cancelAfterRootCommitRepo) PlaceRecord(ctx context.Context, role repository.RecordRole, body io.Reader) (repository.RecordReceipt, error) {
	receipt, err := r.Dir.PlaceRecord(ctx, role, body)
	if role == repository.RecordPublicationCommit && err == nil && !r.cancelled {
		r.cancelled = true
		r.cancel()
	}
	return receipt, err
}

func (r *portableChildUnknownOnceRepo) PlaceRecord(ctx context.Context, role repository.RecordRole, body io.Reader) (repository.RecordReceipt, error) {
	if role == repository.RecordPortableFactClosure && !r.failed {
		r.failed = true
		return repository.RecordReceipt{}, errors.New("portable child placement unavailable before commit")
	}
	return r.Dir.PlaceRecord(ctx, role, body)
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
	if len(unknown.Reasons) != 1 || unknown.Reasons[0].Resolution == nil || unknown.Reasons[0].Resolution.Action != command.OpPlanApply {
		t.Fatalf("unknown resolution = %+v", unknown.Reasons)
	}
	if unknown.Reasons[0].Resolution.Arguments["plan_id"] != plan.PlanID ||
		unknown.Reasons[0].Resolution.Arguments["plan_digest"] != plan.PlanDigest ||
		unknown.Reasons[0].Resolution.Arguments["workspace_id"] != plan.WorkspaceID {
		t.Fatalf("unknown resolution is not executable: %+v", unknown.Reasons[0].Resolution)
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

func TestPlanApplyReplayReconcilesNeedsReconciliationJobFromCommittedPublication(t *testing.T) {
	h := newPhase2Harness(t)
	ctx := context.Background()
	planned := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": h.root}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var planData command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &planData); err != nil {
		t.Fatal(err)
	}
	plan, err := h.store.GetPlan(ctx, planData.WorkspaceID, planData.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := decodePlanBody(plan.Plan)
	if err != nil || body.Ingest == nil {
		t.Fatalf("decode ingest plan: %v", err)
	}
	committed, err := h.dispatcher.exact.ApplyIngestPlanWithExecutionKey(ctx, *body.Ingest, plan.PlanDigest)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := sqlite.NewStableID(sqlite.IDPrefixJob)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := json.Marshal(planApplyJobResult{
		Data:       command.PlanApplyData{PlanID: plan.ID, PlanDigest: plan.PlanDigest, State: string(sqlite.JobNeedsReconcile)},
		ReasonCode: ReasonCodeUnknownExternalOutcome,
		Error:      "publication response was lost",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertJob(ctx, &sqlite.Job{
			ID: jobID, WorkspaceID: plan.WorkspaceID, PlanID: plan.ID, Kind: planApplyJobKind,
			State: sqlite.JobNeedsReconcile, Attempt: 1, MaxAttempts: 3, Result: stored,
		})
	}); err != nil {
		t.Fatal(err)
	}
	publicationCount := phase2PublicationCount(t, h.store)

	reconciled := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": plan.WorkspaceID, "plan_id": plan.ID, "plan_digest": plan.PlanDigest,
	}))
	if reconciled.Status != command.StatusSucceeded {
		t.Fatalf("reconciled plan.apply = %q: %+v", reconciled.Status, reconciled.Reasons)
	}
	var data command.PlanApplyData
	if err := json.Unmarshal(reconciled.Data, &data); err != nil {
		t.Fatal(err)
	}
	if !data.AlreadyApplied || data.JobID != jobID || data.SnapshotRef != committed.SnapshotRef || data.ManifestDigest != committed.ManifestDigest {
		t.Fatalf("reconciled result = %+v, committed = %+v", data, committed)
	}
	if data.SavingsMeasured || data.NewPhysicalBytes != 0 || data.CompressionSavedBytes != 0 {
		t.Fatalf("reconciliation inferred savings without persisted placement receipts: %+v", data)
	}
	if got := phase2PublicationCount(t, h.store); got != publicationCount {
		t.Fatalf("reconciliation created a publication: before=%d after=%d", publicationCount, got)
	}
	job, err := h.store.GetJob(ctx, plan.WorkspaceID, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != sqlite.JobSucceeded {
		t.Fatalf("reconciled job state = %q, want SUCCEEDED", job.State)
	}
}

func TestPlanApplyReconcilesProjectionFailureAfterDurableRoot(t *testing.T) {
	ctx := context.Background()
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	store := testutil.OpenStore(t, catalogPath)
	baseRepo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	identity, anchor, err := exact.OpenSigningMaterial(t.TempDir(), "workspace:default", true)
	if err != nil {
		t.Fatal(err)
	}
	applyCtx, cancelApply := context.WithCancel(context.Background())
	repo := &cancelAfterRootCommitRepo{Dir: baseRepo, cancel: cancelApply}
	service := &exact.Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: "workspace:default", RequireSignedPublication: true,
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(service))
	root := t.TempDir()
	if err := writeTestFile(filepath.Join(root, "payload.txt"), []byte("projection failure after root")); err != nil {
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

	// Reject only the rebuildable SQLite projection. The signed root records
	// remain writable, so the apply crosses the durable publication boundary
	// before returning the typed unknown outcome.
	db, err := sql.Open("sqlite", catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const trigger = "reject_publication_projection_test"
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_publication_projection_test
BEFORE INSERT ON publications BEGIN SELECT RAISE(ABORT, 'injected projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP TRIGGER "+trigger) }()

	input := map[string]any{"workspace_id": plan.WorkspaceID, "plan_id": plan.PlanID, "plan_digest": plan.PlanDigest}
	unknown := dispatcher.Handle(applyCtx, mustEnvelope(t, command.OpPlanApply, input))
	if unknown.Status != command.StatusUnknownExternalOutcome || !hasReasonCode(unknown, ReasonCodeUnknownExternalOutcome) {
		t.Fatalf("projection failure plan.apply = %q: %+v", unknown.Status, unknown.Reasons)
	}
	job, err := store.GetJobByPlanKind(ctx, plan.WorkspaceID, plan.PlanID, planApplyJobKind)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != sqlite.JobNeedsReconcile {
		t.Fatalf("projection failure job state = %q, want NEEDS_RECONCILIATION", job.State)
	}
	var partial planApplyJobResult
	if err := json.Unmarshal(job.Result, &partial); err != nil {
		t.Fatal(err)
	}
	if partial.Data.SnapshotRef == "" {
		t.Fatalf("unknown result lost committed snapshot identity: %+v", partial.Data)
	}
	commits, err := repo.ListRecordDigests(ctx, repository.RecordPublicationCommit)
	if err != nil || len(commits) != 1 {
		t.Fatalf("durable root records = %v, err=%v; want one", commits, err)
	}
	if got := phase2PublicationCount(t, store); got != 0 {
		t.Fatalf("injected projection unexpectedly inserted %d publications", got)
	}

	// Replaying the same request must use the read-only reconciliation branch;
	// it must project the authenticated root and never create another root.
	if _, err := db.ExecContext(ctx, "DROP TRIGGER "+trigger); err != nil {
		t.Fatal(err)
	}
	replayed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, input))
	if replayed.Status != command.StatusSucceeded {
		t.Fatalf("reconciled plan.apply = %q: %+v", replayed.Status, replayed.Reasons)
	}
	var replayData command.PlanApplyData
	if err := json.Unmarshal(replayed.Data, &replayData); err != nil {
		t.Fatal(err)
	}
	if !replayData.AlreadyApplied || replayData.SnapshotRef != partial.Data.SnapshotRef {
		t.Fatalf("reconciled result = %+v, partial = %+v", replayData, partial.Data)
	}
	if replayData.SavingsMeasured != partial.Data.SavingsMeasured || replayData.NewPhysicalBytes != partial.Data.NewPhysicalBytes || replayData.CompressionSavedBytes != partial.Data.CompressionSavedBytes {
		t.Fatalf("reconciliation changed persisted savings without new receipts: replay=%+v partial=%+v", replayData, partial.Data)
	}
	commitsAfter, err := repo.ListRecordDigests(ctx, repository.RecordPublicationCommit)
	if err != nil || len(commitsAfter) != 1 || commitsAfter[0] != commits[0] {
		t.Fatalf("reconciliation changed root publication: before=%v after=%v err=%v", commits, commitsAfter, err)
	}
	if got := phase2PublicationCount(t, store); got != 1 {
		t.Fatalf("reconciled publication count = %d, want one", got)
	}
}

func TestPlanApplyPersistsAndExecutesPortableChildReconciliation(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	baseRepo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	repo := &portableChildUnknownOnceRepo{Dir: baseRepo}
	identity, anchor, err := exact.OpenSigningMaterial(t.TempDir(), "workspace:default", true)
	if err != nil {
		t.Fatal(err)
	}
	service := &exact.Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: "workspace:default", RequireSignedPublication: true,
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(service))
	root := t.TempDir()
	if err := writeTestFile(filepath.Join(root, "payload.txt"), []byte("portable child reconciliation")); err != nil {
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
		t.Fatalf("child unknown plan.apply = %q: %+v", unknown.Status, unknown.Reasons)
	}
	job, err := store.GetJobByPlanKind(ctx, plan.WorkspaceID, plan.PlanID, planApplyJobKind)
	if err != nil || job.State != sqlite.JobNeedsReconcile {
		t.Fatalf("child unknown job = %+v, err=%v", job, err)
	}
	commits, err := baseRepo.ListRecordDigests(ctx, repository.RecordPublicationCommit)
	if err != nil || len(commits) != 1 {
		t.Fatalf("committed roots = %v, err=%v", commits, err)
	}

	reconciled := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, input))
	if reconciled.Status != command.StatusSucceeded {
		t.Fatalf("child reconciliation plan.apply = %q: %+v", reconciled.Status, reconciled.Reasons)
	}
	job, err = store.GetJobByPlanKind(ctx, plan.WorkspaceID, plan.PlanID, planApplyJobKind)
	if err != nil || job.State != sqlite.JobSucceeded {
		t.Fatalf("reconciled child job = %+v, err=%v", job, err)
	}
	children, err := baseRepo.ListRecordDigests(ctx, repository.RecordPortableFactClosure)
	if err != nil || len(children) != 1 {
		t.Fatalf("portable children = %v, err=%v", children, err)
	}
	commitsAfter, err := baseRepo.ListRecordDigests(ctx, repository.RecordPublicationCommit)
	if err != nil || len(commitsAfter) != 1 || commitsAfter[0] != commits[0] {
		t.Fatalf("reconciliation changed root publication: before=%v after=%v err=%v", commits, commitsAfter, err)
	}
}

func writeTestFile(path string, payload []byte) error {
	return os.WriteFile(path, payload, 0o600)
}
