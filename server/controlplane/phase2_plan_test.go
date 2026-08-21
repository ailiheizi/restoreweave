package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// phase2Harness deliberately keeps the repository and catalog paths separate:
// a READY plan may be recorded in SQLite, but it must not publish an object,
// snapshot, or publication until plan.apply.
type phase2Harness struct {
	dispatcher *Dispatcher
	store      *sqlite.Store
	repo       *repository.Dir
	root       string
}

func newPhase2Harness(t *testing.T) phase2Harness {
	t.Helper()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.txt"), []byte("phase two payload"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return phase2Harness{
		dispatcher: NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
			Store: store,
			Repo:  repo,
		})),
		store: store,
		repo:  repo,
		root:  root,
	}
}

func TestPhase2PlanIngestIsReadyAndApplyPublishesIdempotently(t *testing.T) {
	h := newPhase2Harness(t)
	ctx := context.Background()
	repoBefore := phase2RepositoryDigest(t, h.repo.Root())
	publicationsBefore := phase2PublicationCount(t, h.store)

	planned := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{
		"root": h.root,
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var planData command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &planData); err != nil {
		t.Fatalf("decode plan.ingest: %v", err)
	}
	if planData.PlanID == "" || planData.PlanDigest == "" || planData.JobID == "" {
		t.Fatalf("plan receipt missing identity: %+v", planData)
	}
	if planData.State != string(sqlite.PlanReady) || !planData.Executable {
		t.Fatalf("plan.ingest state = %q executable=%t, want READY/true", planData.State, planData.Executable)
	}
	if planData.SourceBasisDigest == "" {
		t.Fatalf("plan.ingest omitted source basis digest: %+v", planData)
	}
	if planData.ProtectionDigest == "" || len(planData.ProtectionDecisions) != 1 ||
		planData.ProtectionDecisions[0].PlannedOutcome == "" || planData.ProtectionDecisions[0].ReasonCode == "" {
		t.Fatalf("plan.ingest omitted per-file protection outcome: %+v", planData)
	}
	if planData.SnapshotRef != "" {
		t.Fatalf("READY plan already has snapshot %q", planData.SnapshotRef)
	}
	if got := phase2RepositoryDigest(t, h.repo.Root()); got != repoBefore {
		t.Fatalf("plan.ingest changed repository: before=%s after=%s", repoBefore, got)
	}
	if got := phase2PublicationCount(t, h.store); got != publicationsBefore {
		t.Fatalf("plan.ingest changed publication count: before=%d after=%d", publicationsBefore, got)
	}

	gotPlan := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanGet, map[string]any{
		"workspace_id": planData.WorkspaceID,
		"plan_id":      planData.PlanID,
	}))
	if gotPlan.Status != command.StatusSucceeded {
		t.Fatalf("plan.get = %q: %+v", gotPlan.Status, gotPlan.Reasons)
	}
	var planGet command.PlanGetData
	if err := json.Unmarshal(gotPlan.Data, &planGet); err != nil {
		t.Fatalf("decode plan.get: %v", err)
	}
	if planGet.State != string(sqlite.PlanReady) || planGet.Applied || !planGet.Executable {
		t.Fatalf("plan.get = %+v, want READY/unapplied/executable", planGet)
	}
	if planGet.SourceBasisDigest != planData.SourceBasisDigest {
		t.Fatalf("plan basis changed between responses: ingest=%q get=%q", planData.SourceBasisDigest, planGet.SourceBasisDigest)
	}

	applied := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": planData.WorkspaceID,
		"plan_id":      planData.PlanID,
		"plan_digest":  planData.PlanDigest,
	}))
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("plan.apply = %q: %+v", applied.Status, applied.Reasons)
	}
	var applyData command.PlanApplyData
	if err := json.Unmarshal(applied.Data, &applyData); err != nil {
		t.Fatalf("decode plan.apply: %v", err)
	}
	if applyData.AlreadyApplied || applyData.SnapshotRef == "" ||
		applyData.ProtectionDigest != planData.ProtectionDigest || len(applyData.ProtectionDecisions) != 1 {
		t.Fatalf("first apply = %+v, want new snapshot", applyData)
	}
	if got := phase2PublicationCount(t, h.store); got != publicationsBefore+1 {
		t.Fatalf("publication count after apply = %d, want %d", got, publicationsBefore+1)
	}
	repoAfterApply := phase2RepositoryDigest(t, h.repo.Root())

	repeated := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": planData.WorkspaceID,
		"plan_id":      planData.PlanID,
		"plan_digest":  planData.PlanDigest,
	}))
	if repeated.Status != command.StatusSucceeded {
		t.Fatalf("repeated plan.apply = %q: %+v", repeated.Status, repeated.Reasons)
	}
	var repeatedData command.PlanApplyData
	if err := json.Unmarshal(repeated.Data, &repeatedData); err != nil {
		t.Fatalf("decode repeated plan.apply: %v", err)
	}
	if !repeatedData.AlreadyApplied || repeatedData.SnapshotRef != applyData.SnapshotRef {
		t.Fatalf("repeated apply = %+v, want same snapshot and already_applied", repeatedData)
	}
	if got := phase2PublicationCount(t, h.store); got != publicationsBefore+1 {
		t.Fatalf("repeated apply changed publication count: %d", got)
	}
	if got := phase2RepositoryDigest(t, h.repo.Root()); got != repoAfterApply {
		t.Fatalf("repeated apply changed repository: before=%s after=%s", repoAfterApply, got)
	}
}

func TestPhase2PlanIngestRejectsChangedSourceWithoutPublication(t *testing.T) {
	h := newPhase2Harness(t)
	ctx := context.Background()
	planned := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{
		"root": h.root,
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var planData command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &planData); err != nil {
		t.Fatalf("decode plan.ingest: %v", err)
	}
	repoBefore := phase2RepositoryDigest(t, h.repo.Root())
	if err := os.WriteFile(filepath.Join(h.root, "payload.txt"), []byte("changed after planning"), 0o600); err != nil {
		t.Fatalf("change fixture: %v", err)
	}

	applied := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": planData.WorkspaceID,
		"plan_id":      planData.PlanID,
		"plan_digest":  planData.PlanDigest,
	}))
	if applied.Status != command.StatusFailed || !hasReasonCode(applied, ReasonCodeConflict) {
		t.Fatalf("stale plan.apply = %q reasons=%+v, want conflict", applied.Status, applied.Reasons)
	}
	if got := phase2PublicationCount(t, h.store); got != 0 {
		t.Fatalf("stale apply published %d snapshots", got)
	}
	if got := phase2RepositoryDigest(t, h.repo.Root()); got != repoBefore {
		t.Fatalf("stale apply changed repository: before=%s after=%s", repoBefore, got)
	}
}

func TestPhase2PlanApplyReconcilesCommittedPublicationAfterJobCrash(t *testing.T) {
	h := newPhase2Harness(t)
	ctx := context.Background()
	planned := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{
		"root": h.root,
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var planData command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &planData); err != nil {
		t.Fatalf("decode plan.ingest: %v", err)
	}
	plan, err := h.store.GetPlan(ctx, planData.WorkspaceID, planData.PlanID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	body, err := decodePlanBody(plan.Plan)
	if err != nil || body.Ingest == nil {
		t.Fatalf("decode ingest body: %v", err)
	}

	// Model the crash window: exact publication committed with the immutable
	// plan key, but the worker did not yet finish its PLAN_APPLY job.
	committed, err := h.dispatcher.exact.ApplyIngestPlanWithExecutionKey(ctx, *body.Ingest, plan.PlanDigest)
	if err != nil {
		t.Fatalf("commit publication: %v", err)
	}
	jobID, err := sqlite.NewStableID(sqlite.IDPrefixJob)
	if err != nil {
		t.Fatalf("job id: %v", err)
	}
	expired := time.Now().UTC().Add(-time.Minute)
	if err := h.store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertJob(ctx, &sqlite.Job{
			ID: jobID, WorkspaceID: plan.WorkspaceID, PlanID: plan.ID, Kind: planApplyJobKind,
			State: sqlite.JobRunning, Attempt: 1, MaxAttempts: 3, LeaseUntil: &expired,
		})
	}); err != nil {
		t.Fatalf("insert crashed job: %v", err)
	}
	publicationCount := phase2PublicationCount(t, h.store)

	retried := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": plan.WorkspaceID,
		"plan_id":      plan.ID,
		"plan_digest":  plan.PlanDigest,
	}))
	if retried.Status != command.StatusSucceeded {
		t.Fatalf("reconciled plan.apply = %q: %+v", retried.Status, retried.Reasons)
	}
	var applyData command.PlanApplyData
	if err := json.Unmarshal(retried.Data, &applyData); err != nil {
		t.Fatalf("decode reconciled apply: %v", err)
	}
	if applyData.SnapshotRef != committed.SnapshotRef || applyData.ManifestDigest != committed.ManifestDigest ||
		applyData.ProtectionDigest != committed.ProtectionDigest || len(applyData.ProtectionDecisions) != len(committed.ProtectionDecisions) {
		t.Fatalf("reconciled result = %+v, committed = %+v", applyData, committed)
	}
	if got := phase2PublicationCount(t, h.store); got != publicationCount {
		t.Fatalf("reconciliation created a second publication: before=%d after=%d", publicationCount, got)
	}
	job, err := h.store.GetJobByPlanKind(ctx, plan.WorkspaceID, plan.ID, planApplyJobKind)
	if err != nil {
		t.Fatalf("get reconciled job: %v", err)
	}
	if job.State != sqlite.JobSucceeded {
		t.Fatalf("reconciled job state = %q, want SUCCEEDED", job.State)
	}
}

func TestPhase2PlanRestoreWithDestinationWritesOnlyOnApply(t *testing.T) {
	h := newPhase2Harness(t)
	ctx := context.Background()
	planned := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{
		"root": h.root,
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var ingestData command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &ingestData); err != nil {
		t.Fatalf("decode plan.ingest: %v", err)
	}
	applyIngest := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
		"plan_digest":  ingestData.PlanDigest,
	}))
	if applyIngest.Status != command.StatusSucceeded {
		t.Fatalf("apply ingest = %q: %+v", applyIngest.Status, applyIngest.Reasons)
	}
	var ingestApplyData command.PlanApplyData
	if err := json.Unmarshal(applyIngest.Data, &ingestApplyData); err != nil {
		t.Fatalf("decode ingest apply: %v", err)
	}

	destination := filepath.Join(t.TempDir(), "restore-target")
	restorePlan := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": ingestApplyData.SnapshotRef,
		"destination":  destination,
	}))
	if restorePlan.Status != command.StatusSucceeded {
		t.Fatalf("plan.restore = %q: %+v", restorePlan.Status, restorePlan.Reasons)
	}
	var restoreData command.PlanRestoreData
	if err := json.Unmarshal(restorePlan.Data, &restoreData); err != nil {
		t.Fatalf("decode plan.restore: %v", err)
	}
	if restoreData.Wrote || restoreData.PlanID == "" || restoreData.PlanDigest == "" {
		t.Fatalf("plan.restore = %+v, want unexecuted destination plan", restoreData)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plan.restore wrote destination before apply: stat err=%v", err)
	}

	restoreAgain := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": ingestApplyData.SnapshotRef,
		"destination":  destination,
	}))
	if restoreAgain.Status != command.StatusSucceeded {
		t.Fatalf("repeated plan.restore = %q: %+v", restoreAgain.Status, restoreAgain.Reasons)
	}
	var restoreAgainData command.PlanRestoreData
	if err := json.Unmarshal(restoreAgain.Data, &restoreAgainData); err != nil {
		t.Fatalf("decode repeated plan.restore: %v", err)
	}
	if restoreAgainData.PlanID != restoreData.PlanID || restoreAgainData.PlanDigest != restoreData.PlanDigest {
		t.Fatalf("restore plan is not idempotent: first=%+v second=%+v", restoreData, restoreAgainData)
	}

	restored := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      restoreData.PlanID,
		"plan_digest":  restoreData.PlanDigest,
	}))
	if restored.Status != command.StatusSucceeded {
		t.Fatalf("apply restore = %q: %+v", restored.Status, restored.Reasons)
	}
	var restoredData command.PlanApplyData
	if err := json.Unmarshal(restored.Data, &restoredData); err != nil {
		t.Fatalf("decode restore apply: %v", err)
	}
	if restoredData.AlreadyApplied || restoredData.SnapshotRef != ingestApplyData.SnapshotRef {
		t.Fatalf("first restore apply = %+v", restoredData)
	}
	want, err := os.ReadFile(filepath.Join(destination, "payload.txt"))
	if err != nil || string(want) != "phase two payload" {
		t.Fatalf("restored payload = %q, err=%v", want, err)
	}

	repeatedRestore := h.dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      restoreData.PlanID,
		"plan_digest":  restoreData.PlanDigest,
	}))
	if repeatedRestore.Status != command.StatusSucceeded {
		t.Fatalf("repeated restore apply = %q: %+v", repeatedRestore.Status, repeatedRestore.Reasons)
	}
	var repeatedRestoreData command.PlanApplyData
	if err := json.Unmarshal(repeatedRestore.Data, &repeatedRestoreData); err != nil {
		t.Fatalf("decode repeated restore apply: %v", err)
	}
	if !repeatedRestoreData.AlreadyApplied || repeatedRestoreData.SnapshotRef != restoredData.SnapshotRef {
		t.Fatalf("repeated restore apply = %+v", repeatedRestoreData)
	}
}

func phase2PublicationCount(t *testing.T, store *sqlite.Store) int {
	t.Helper()
	publications, err := store.ListPublications(context.Background())
	if err != nil {
		t.Fatalf("list publications: %v", err)
	}
	return len(publications)
}

// phase2RepositoryDigest includes both blobs and snapshots and therefore
// catches an implementation that mutates the repository while making a plan.
func phase2RepositoryDigest(t *testing.T, root string) string {
	t.Helper()
	type item struct {
		path string
		data []byte
	}
	var items []item
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("repository contains non-regular file %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		items = append(items, item{path: filepath.ToSlash(rel), data: data})
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	hash := sha256.New()
	for _, item := range items {
		_, _ = hash.Write([]byte(item.path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(item.data)
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
