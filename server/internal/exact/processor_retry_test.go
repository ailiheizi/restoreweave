package exact

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type retryFixtureProcessor struct {
	store       *sqlite.Store
	mu          sync.Mutex
	failRetries int
	retryCalls  int
	lastFence   int64
}

type retryFixtureError struct{ targets []ProcessorRetryTarget }

func (e *retryFixtureError) Error() string                 { return "retry fixture failure" }
func (e *retryFixtureError) PublicationWarnings() []string { return []string{e.Error()} }
func (e *retryFixtureError) ProcessorRetryTargets() []ProcessorRetryTarget {
	return append([]ProcessorRetryTarget(nil), e.targets...)
}

func (p *retryFixtureProcessor) ProcessPublication(ctx context.Context, workspaceID, snapshotRef, rootID string) error {
	nodes, err := p.store.ListNamespaceSubtree(ctx, workspaceID, rootID, "")
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Entry.EntryType != sqlite.EntryFile || node.Entry.ContentID == "" {
			continue
		}
		attempt, err := p.insertAttempt(ctx, workspaceID, snapshotRef, node.Entry.SubjectRef, "FAILED", "PROCESSOR_STAGE_FAILED", 1, nil)
		if err != nil {
			return err
		}
		return &retryFixtureError{targets: []ProcessorRetryTarget{{
			SubjectRef: node.Entry.SubjectRef, RouteDigest: attempt.RouteDigest, Stage: attempt.Stage,
			CapabilityID: attempt.CapabilityID, PredecessorAttemptID: attempt.ID, ReasonCode: attempt.ReasonCode,
		}}}
	}
	return errors.New("retry fixture has no file subject")
}

func (p *retryFixtureProcessor) RetryPublication(ctx context.Context, workspaceID, snapshotRef, _ string, invocation ProcessorRetryInvocation) error {
	p.mu.Lock()
	p.retryCalls++
	p.lastFence = invocation.FenceToken
	shouldFail := p.retryCalls <= p.failRetries
	p.mu.Unlock()
	target := invocation.Targets[0]
	status, reason := "SUCCEEDED", "ADMITTED_ARTIFACT"
	if shouldFail {
		status, reason = "FAILED", "PROCESSOR_STAGE_FAILED"
	}
	provenance := map[string]any{
		"retry_job_id": invocation.JobID, "retry_attempt": invocation.Attempt,
		"retry_idempotency_key": invocation.IdempotencyKey, "retry_lease_token": invocation.LeaseToken,
		"predecessor_attempt_id": target.PredecessorAttemptID,
	}
	attempt, err := p.insertAttempt(ctx, workspaceID, snapshotRef, target.SubjectRef, status, reason, invocation.FenceToken, provenance)
	if err != nil {
		return err
	}
	if !shouldFail {
		return nil
	}
	return &retryFixtureError{targets: []ProcessorRetryTarget{{
		SubjectRef: target.SubjectRef, RouteDigest: attempt.RouteDigest, Stage: attempt.Stage,
		CapabilityID: attempt.CapabilityID, PredecessorAttemptID: attempt.ID, ReasonCode: attempt.ReasonCode,
	}}}
}

func (p *retryFixtureProcessor) insertAttempt(ctx context.Context, workspaceID, snapshotRef, subject, status, reason string, fence int64, extra map[string]any) (sqlite.ProcessorAttempt, error) {
	id, err := sqlite.NewStableID(sqlite.IDPrefixAttempt)
	if err != nil {
		return sqlite.ProcessorAttempt{}, err
	}
	provenance := map[string]any{"source_content_id": "sha256:retry-fixture"}
	for key, value := range extra {
		provenance[key] = value
	}
	provenanceJSON, _ := json.Marshal(provenance)
	now := time.Now().UTC()
	attempt := sqlite.ProcessorAttempt{
		ID: id, WorkspaceID: workspaceID, SubjectRef: subject, SnapshotRef: snapshotRef,
		RouteDigest: "sha256:retry-route", Route: json.RawMessage(`{"kind":"PROCESSING","nodes":[]}`),
		Stage: "EXTRACT", CapabilityID: "extract.retry.fixture.v1", Status: status,
		ReasonCode: reason, Reason: reason, Provenance: provenanceJSON, FenceToken: fence,
		ProcessorDigest: "sha256:retry-processor", CreatedAt: now, FinishedAt: now,
	}
	return attempt, p.store.InsertProcessorAttempt(ctx, &attempt)
}

func TestProcessorRetryWorkerPublishesSuccessorAndIsIdempotent(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "retry.txt", []byte("retry keeps exact bytes"))
	processor := &retryFixtureProcessor{store: fixture.store}
	fixture.service.Processor = processor
	result := fixture.ingest(t, "sha256:processor-retry-plan")

	jobs, err := fixture.store.ListRecentJobs(context.Background(), 10)
	if err != nil || len(jobs) != 1 || jobs[0].Kind != processorRetryJobKind || jobs[0].State != sqlite.JobQueued {
		t.Fatalf("scheduled processor retry jobs = %+v, err=%v warnings=%v", jobs, err, result.Warnings)
	}
	options := ProcessorRetryWorkerOptions{Owner: "retry-worker-a", LeaseTTL: time.Minute, BatchSize: 4, Now: time.Now}
	if err := fixture.service.runProcessorRetryBatch(context.Background(), processor, options); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), result.WorkspaceID, jobs[0].ID)
	if err != nil || job.State != sqlite.JobSucceeded || job.Attempt != 1 {
		t.Fatalf("processor retry job = %+v, err=%v", job, err)
	}
	attempts, err := fixture.store.ListProcessorAttempts(context.Background(), result.WorkspaceID, result.SnapshotRef)
	if err != nil || len(attempts) != 2 || attempts[0].Status != "FAILED" || attempts[1].Status != "SUCCEEDED" {
		t.Fatalf("processor retry attempts = %+v, err=%v", attempts, err)
	}
	closures, err := fixture.service.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 2 || closures[1].Closure.ClosureSequence != 2 || closures[1].Closure.AttemptCount != 2 {
		t.Fatalf("processor retry closure chain = %+v, err=%v", closures, err)
	}
	if err := fixture.service.runProcessorRetryBatch(context.Background(), processor, options); err != nil {
		t.Fatal(err)
	}
	attempts, _ = fixture.store.ListProcessorAttempts(context.Background(), result.WorkspaceID, result.SnapshotRef)
	if len(attempts) != 2 || processor.retryCalls != 1 {
		t.Fatalf("idempotent retry attempts=%d calls=%d", len(attempts), processor.retryCalls)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err := fixture.service.Restore(context.Background(), result.SnapshotRef, destination); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(destination, "retry.txt"))
	if err != nil || string(payload) != "retry keeps exact bytes" {
		t.Fatalf("restored retry payload = %q, err=%v", payload, err)
	}
}

func TestProcessorRetryWorkerTakesOverExpiredLeaseAndStopsAtLimit(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "retry-limit.txt", []byte("retry limit"))
	processor := &retryFixtureProcessor{store: fixture.store, failRetries: 3}
	fixture.service.Processor = processor
	result := fixture.ingest(t, "sha256:processor-retry-limit-plan")
	jobs, err := fixture.store.ListRecentJobs(context.Background(), 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("retry jobs = %+v, err=%v warnings=%v", jobs, err, result.Warnings)
	}
	start := time.Now().UTC()
	if err := fixture.store.Update(context.Background(), func(tx *sqlite.Tx) error {
		_, err := tx.AcquireJobLease(context.Background(), result.WorkspaceID, jobs[0].ID, jobs[0].Revision, "dead-worker", "dead-lease", start, start.Add(time.Second))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	clock := start.Add(2 * time.Second)
	options := ProcessorRetryWorkerOptions{Owner: "retry-worker-b", LeaseTTL: time.Minute, BatchSize: 4, Now: func() time.Time { return clock }}
	for index := 0; index < 3; index++ {
		if err := fixture.service.runProcessorRetryBatch(context.Background(), processor, options); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Second)
	}
	job, err := fixture.store.GetJob(context.Background(), result.WorkspaceID, jobs[0].ID)
	if err != nil || job.State != sqlite.JobFailed || job.Attempt != job.MaxAttempts || job.FencingToken != 3 {
		t.Fatalf("exhausted retry job = %+v, err=%v", job, err)
	}
	if processor.retryCalls != 2 || processor.lastFence != 3 {
		t.Fatalf("retry calls=%d last fence=%d", processor.retryCalls, processor.lastFence)
	}
	attempts, err := fixture.store.ListProcessorAttempts(context.Background(), result.WorkspaceID, result.SnapshotRef)
	if err != nil || len(attempts) != 3 {
		t.Fatalf("exhausted retry attempts = %+v, err=%v", attempts, err)
	}
	if attempts[2].Status != "FAILED" || attempts[2].FenceToken != 3 {
		t.Fatalf("exhausted retry terminal attempt = %+v", attempts[2])
	}
	closures, err := fixture.service.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 3 || closures[2].Closure.ClosureSequence != 3 || closures[2].Closure.AttemptCount != 3 {
		t.Fatalf("exhausted retry closure chain = %+v, err=%v", closures, err)
	}
	if err := fixture.service.runProcessorRetryBatch(context.Background(), processor, options); err != nil {
		t.Fatal(err)
	}
	if processor.retryCalls != 2 {
		t.Fatalf("retry worker reran exhausted job: calls=%d", processor.retryCalls)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err := fixture.service.Restore(context.Background(), result.SnapshotRef, destination); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(destination, "retry-limit.txt"))
	if err != nil || string(payload) != "retry limit" {
		t.Fatalf("restored exhausted retry payload = %q, err=%v", payload, err)
	}
}

func TestProcessorRetryWorkerResumesQueuedJobAfterCatalogReopen(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "retry-restart.txt", []byte("retry survives daemon restart"))
	initialProcessor := &retryFixtureProcessor{store: fixture.store}
	fixture.service.Processor = initialProcessor
	result := fixture.ingest(t, "sha256:processor-retry-restart-plan")
	jobs, err := fixture.store.ListRecentJobs(context.Background(), 10)
	if err != nil || len(jobs) != 1 || jobs[0].State != sqlite.JobQueued {
		t.Fatalf("queued retry before restart = %+v, err=%v", jobs, err)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatalf("close catalog for restart: %v", err)
	}

	reopened, err := sqlite.Open(context.Background(), fixture.catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatalf("reopen catalog after restart: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	resumedProcessor := &retryFixtureProcessor{store: reopened}
	restartedService := Service{
		Store:                        reopened,
		Repo:                         fixture.service.Repo,
		ConfigDigest:                 fixture.service.ConfigDigest,
		DefaultProtection:            fixture.service.DefaultProtection,
		AllowLinkOnly:                fixture.service.AllowLinkOnly,
		LinkOnlyRequiresConfirmation: fixture.service.LinkOnlyRequiresConfirmation,
		Capture:                      fixture.service.Capture,
		Identify:                     fixture.service.Identify,
		Indexer:                      fixture.service.Indexer,
		Processor:                    resumedProcessor,
		SigningIdentity:              fixture.service.SigningIdentity,
		TrustAnchor:                  fixture.service.TrustAnchor,
		PublicationDomain:            fixture.service.PublicationDomain,
		RequireSignedPublication:     fixture.service.RequireSignedPublication,
		PublicationFencer:            fixture.service.PublicationFencer,
		Now:                          fixture.service.Now,
	}
	if err := restartedService.runProcessorRetryBatch(context.Background(), resumedProcessor, ProcessorRetryWorkerOptions{
		Owner: "retry-worker-after-restart", LeaseTTL: time.Minute, BatchSize: 4, Now: time.Now,
	}); err != nil {
		t.Fatal(err)
	}
	job, err := reopened.GetJob(context.Background(), result.WorkspaceID, jobs[0].ID)
	if err != nil || job.State != sqlite.JobSucceeded || job.Attempt != 1 {
		t.Fatalf("retry job after restart = %+v, err=%v", job, err)
	}
	attempts, err := reopened.ListProcessorAttempts(context.Background(), result.WorkspaceID, result.SnapshotRef)
	if err != nil || len(attempts) != 2 || attempts[1].Status != "SUCCEEDED" || resumedProcessor.retryCalls != 1 {
		t.Fatalf("retry attempts after restart = %+v, calls=%d, err=%v", attempts, resumedProcessor.retryCalls, err)
	}
}
