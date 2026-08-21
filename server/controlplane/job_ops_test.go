package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestJobEventsAndCancelAfterIngest(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("job-lane"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	if ingestData.JobID == "" {
		t.Fatalf("ingest missing job_id: %+v", ingestData)
	}

	page := dispatcher.Handle(ctx, mustEnvelope(t, command.OpJobEvents, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"job_id":       ingestData.JobID,
		"limit":        1,
	}))
	if page.Status != command.StatusSucceeded {
		t.Fatalf("job.events = %q: %+v", page.Status, page.Reasons)
	}
	var first command.JobEventsData
	if err := json.Unmarshal(page.Data, &first); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if first.JobState != string(sqlite.JobSucceeded) || !first.Terminal || len(first.Events) != 1 {
		t.Fatalf("first page = %+v", first)
	}
	if first.Events[0].Action != "JOB_STARTED" {
		t.Fatalf("first event = %+v", first.Events[0])
	}
	rest := dispatcher.Handle(ctx, mustEnvelope(t, command.OpJobEvents, map[string]any{
		"workspace_id":   ingestData.WorkspaceID,
		"job_id":         ingestData.JobID,
		"after_sequence": first.NextSequence,
	}))
	var second command.JobEventsData
	if err := json.Unmarshal(rest.Data, &second); err != nil {
		t.Fatalf("decode rest: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].Action != "JOB_SUCCEEDED" || !second.Terminal {
		t.Fatalf("second page = %+v", second)
	}

	cancel := dispatcher.Handle(ctx, mustEnvelope(t, command.OpJobCancel, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"job_id":       ingestData.JobID,
	}))
	if cancel.Status != command.StatusSucceeded {
		t.Fatalf("cancel succeeded job = %q: %+v", cancel.Status, cancel.Reasons)
	}
	var cancelData command.JobCancelData
	if err := json.Unmarshal(cancel.Data, &cancelData); err != nil {
		t.Fatalf("decode cancel: %v", err)
	}
	if !cancelData.AlreadyTerminal || cancelData.Cancelled {
		t.Fatalf("cancel data = %+v", cancelData)
	}

	missing := dispatcher.Handle(ctx, mustEnvelope(t, command.OpJobEvents, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"job_id":       "job_00000000000000000000000000000000",
	}))
	if missing.Status != command.StatusFailed || !hasReasonCode(missing, ReasonCodeNotFound) {
		t.Fatalf("missing job = %q: %+v", missing.Status, missing.Reasons)
	}

	queuedID, err := sqlite.NewStableID(sqlite.IDPrefixJob)
	if err != nil {
		t.Fatalf("job id: %v", err)
	}
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertJob(ctx, &sqlite.Job{
			ID: queuedID, WorkspaceID: ingestData.WorkspaceID, PlanID: ingestData.PlanID,
			Kind: "INGEST", State: sqlite.JobQueued, MaxAttempts: 1,
		})
	}); err != nil {
		t.Fatalf("insert queued job: %v", err)
	}
	queuedCancel := dispatcher.Handle(ctx, mustEnvelope(t, command.OpJobCancel, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"job_id":       queuedID,
	}))
	if queuedCancel.Status != command.StatusSucceeded {
		t.Fatalf("cancel queued = %q: %+v", queuedCancel.Status, queuedCancel.Reasons)
	}
	var queuedData command.JobCancelData
	if err := json.Unmarshal(queuedCancel.Data, &queuedData); err != nil {
		t.Fatalf("decode queued cancel: %v", err)
	}
	if !queuedData.Cancelled || queuedData.JobState != string(sqlite.JobCancelled) || queuedData.AlreadyTerminal {
		t.Fatalf("queued cancel = %+v", queuedData)
	}
}

func TestStatusGetReportsJobsAndReapsHandles(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("status-lane"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	status := dispatcher.Handle(ctx, mustEnvelope(t, command.OpStatusGet, map[string]any{}))
	if status.Status != command.StatusSucceeded {
		t.Fatalf("status = %q: %+v", status.Status, status.Reasons)
	}
	var statusData command.StatusData
	if err := json.Unmarshal(status.Data, &statusData); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if statusData.Plans < 1 || statusData.Jobs < 1 || len(statusData.RecentJobs) < 1 || len(statusData.RecentPlans) < 1 {
		t.Fatalf("status missing jobs/plans: %+v", statusData)
	}
	if statusData.RecentJobs[0].JobID != ingestData.JobID || statusData.RecentJobs[0].State != string(sqlite.JobSucceeded) {
		t.Fatalf("recent job = %+v", statusData.RecentJobs[0])
	}
	if statusData.RecentPlans[0].PlanID != ingestData.PlanID || statusData.RecentPlans[0].Kind != "INGEST" {
		t.Fatalf("recent plan = %+v", statusData.RecentPlans[0])
	}

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
	}))
	var listData command.NamespaceListData
	if err := json.Unmarshal(listed.Data, &listData); err != nil {
		t.Fatalf("decode namespace: %v", err)
	}
	var fileID string
	for _, entry := range listData.Entries {
		if entry.DisplayName == "blob.bin" {
			fileID = entry.ID
		}
	}
	if fileID == "" {
		t.Fatalf("blob.bin missing: %+v", listData.Entries)
	}
	opened := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentOpen, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"entry_id":     fileID,
	}))
	if opened.Status != command.StatusSucceeded {
		t.Fatalf("content.open = %q: %+v", opened.Status, opened.Reasons)
	}
	var openData command.ContentOpenData
	if err := json.Unmarshal(opened.Data, &openData); err != nil {
		t.Fatalf("decode open: %v", err)
	}
	openStatus := dispatcher.Handle(ctx, mustEnvelope(t, command.OpStatusGet, map[string]any{}))
	var openStatusData command.StatusData
	if err := json.Unmarshal(openStatus.Data, &openStatusData); err != nil {
		t.Fatalf("decode open status: %v", err)
	}
	if openStatusData.OpenHandles != 1 || openStatusData.ReapedHandles != 0 {
		t.Fatalf("open status = %+v", openStatusData)
	}

	dispatcher.sessions.expireHandle(openData.Handle, time.Now().Add(-time.Second))
	reaped := dispatcher.Handle(ctx, mustEnvelope(t, command.OpStatusGet, map[string]any{}))
	var reapedData command.StatusData
	if err := json.Unmarshal(reaped.Data, &reapedData); err != nil {
		t.Fatalf("decode reaped status: %v", err)
	}
	if reapedData.ReapedHandles != 1 || reapedData.OpenHandles != 0 {
		t.Fatalf("reaped status = %+v", reapedData)
	}
	read := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentRead, map[string]any{
		"handle": openData.Handle,
		"offset": 0,
		"length": 4,
	}))
	if read.Status != command.StatusFailed || !hasReasonCode(read, ReasonCodeNotFound) {
		t.Fatalf("expired read = %q: %+v", read.Status, read.Reasons)
	}
}
