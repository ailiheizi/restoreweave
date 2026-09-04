package exact

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type retryFixtureProcessor struct {
	store        *sqlite.Store
	mu           sync.Mutex
	failRetries  int
	retryCalls   int
	lastFence    int64
	retryStarted chan struct{}
	retryBlock   <-chan struct{}
	retryOnce    sync.Once
}

type retryFixtureError struct{ targets []ProcessorRetryTarget }

type retryUnknownClosureRepo struct {
	*repository.Dir
	failRole repository.RecordRole
	failed   bool
}

func (r *retryUnknownClosureRepo) PlaceRecord(ctx context.Context, role repository.RecordRole, body io.Reader) (repository.RecordReceipt, error) {
	if role == r.failRole && !r.failed {
		r.failed = true
		return repository.RecordReceipt{}, errors.New("retry closure placement outcome unavailable")
	}
	return r.Dir.PlaceRecord(ctx, role, body)
}

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
	if p.retryStarted != nil {
		p.retryOnce.Do(func() { close(p.retryStarted) })
	}
	if p.retryBlock != nil {
		select {
		case <-p.retryBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
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

// TestProcessorRetryQualificationMatrix is the Phase 2 gate evidence for the
// bounded same-plan retry contract. Channels and an injected clock make lease
// races deterministic without sleeps.
func TestProcessorRetryQualificationMatrix(t *testing.T) {
	t.Run("competing workers execute once", func(t *testing.T) {
		fixture := newSignedPublicationFixture(t, "retry-qualification-race.txt", []byte("qualification race"))
		release := make(chan struct{})
		started := make(chan struct{})
		processor := &retryFixtureProcessor{store: fixture.store, retryStarted: started, retryBlock: release}
		fixture.service.Processor = processor
		result := fixture.ingest(t, "sha256:processor-retry-qualification-race")
		jobs, err := fixture.store.ListRecentJobs(context.Background(), 10)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("retry jobs = %+v, err=%v", jobs, err)
		}
		options := ProcessorRetryWorkerOptions{Owner: "qualification-a", LeaseTTL: time.Minute, BatchSize: 1, Now: time.Now}
		done := make(chan error, 1)
		go func() { done <- fixture.service.runProcessorRetryBatch(context.Background(), processor, options) }()
		<-started
		if err := fixture.service.runProcessorRetryBatch(context.Background(), processor, ProcessorRetryWorkerOptions{Owner: "qualification-b", LeaseTTL: time.Minute, BatchSize: 1, Now: time.Now}); err != nil {
			t.Fatal(err)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if processor.retryCalls != 1 {
			t.Fatalf("competing workers executed processor %d times", processor.retryCalls)
		}
		job, err := fixture.store.GetJob(context.Background(), result.WorkspaceID, jobs[0].ID)
		if err != nil || job.State != sqlite.JobSucceeded {
			t.Fatalf("competing worker job = %+v, err=%v", job, err)
		}
	})

	t.Run("expired lease fences old worker", func(t *testing.T) {
		fixture := newSignedPublicationFixture(t, "retry-qualification-fence.txt", []byte("qualification fence"))
		release := make(chan struct{})
		started := make(chan struct{})
		processor := &retryFixtureProcessor{store: fixture.store, retryStarted: started, retryBlock: release}
		fixture.service.Processor = processor
		result := fixture.ingest(t, "sha256:processor-retry-qualification-fence")
		jobs, err := fixture.store.ListRecentJobs(context.Background(), 10)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("retry jobs = %+v, err=%v", jobs, err)
		}
		start := time.Now().UTC()
		oldDone := make(chan error, 1)
		go func() {
			oldDone <- fixture.service.runProcessorRetryJob(context.Background(), processor, ProcessorRetryWorkerOptions{Owner: "qualification-old", LeaseTTL: time.Second, BatchSize: 1, Now: func() time.Time { return start }}, jobs[0])
		}()
		<-started
		var takeover int64
		if err := fixture.store.Update(context.Background(), func(tx *sqlite.Tx) error {
			var err error
			takeover, err = tx.AcquireJobLease(context.Background(), result.WorkspaceID, jobs[0].ID, jobs[0].Revision+1, "qualification-new", "new-lease", start.Add(2*time.Second), start.Add(3*time.Second))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if takeover != 2 {
			t.Fatalf("takeover fence = %d, want 2", takeover)
		}
		close(release)
		if err := <-oldDone; !errors.Is(err, sqlite.ErrConflict) {
			t.Fatalf("old worker error = %v, want fencing conflict", err)
		}
		job, err := fixture.store.GetJob(context.Background(), result.WorkspaceID, jobs[0].ID)
		if err != nil || job.FencingToken != takeover || job.State != sqlite.JobRunning {
			t.Fatalf("fenced job = %+v, err=%v", job, err)
		}
	})
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

func TestProcessorRetryWorkerRejectsTamperedInputBeforeProcessorCall(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "retry-tampered.txt", []byte("tampered retry input"))
	processor := &retryFixtureProcessor{store: fixture.store}
	fixture.service.Processor = processor
	result := fixture.ingest(t, "sha256:processor-retry-tampered-plan")
	jobs, err := fixture.store.ListRecentJobs(context.Background(), 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("scheduled retry jobs = %+v, err=%v", jobs, err)
	}

	jobID, err := sqlite.NewStableID(sqlite.IDPrefixJob)
	if err != nil {
		t.Fatal(err)
	}
	var tampered processorRetryJobInput
	if err := decodeStrictRecord(jobs[0].Input, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.ParentPublicationDigest = "sha256:tampered-parent-publication"
	input, err := json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.Update(context.Background(), func(tx *sqlite.Tx) error {
		return tx.InsertJob(context.Background(), &sqlite.Job{ID: jobID, WorkspaceID: result.WorkspaceID, PlanID: jobs[0].PlanID, Kind: processorRetryJobKind, State: sqlite.JobQueued, Input: input, MaxAttempts: 3})
	}); err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.GetJob(context.Background(), result.WorkspaceID, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.runProcessorRetryJob(context.Background(), processor, ProcessorRetryWorkerOptions{Owner: "tamper-worker", LeaseTTL: time.Minute, BatchSize: 4, Now: time.Now}, candidate); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), result.WorkspaceID, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != sqlite.JobFailed || job.ErrorCode != "PROCESSOR_RETRY_INPUT_INVALID" {
		t.Fatalf("tampered retry job = %+v", job)
	}
	if processor.retryCalls != 0 {
		t.Fatalf("tampered retry invoked processor %d times", processor.retryCalls)
	}
}

func TestProcessorRetryWorkerReconcilesUnknownClosureWithoutReexecution(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "retry-unknown.txt", []byte("unknown closure keeps exact bytes"))
	processor := &retryFixtureProcessor{store: fixture.store}
	fixture.service.Processor = processor
	result := fixture.ingest(t, "sha256:processor-retry-unknown-plan")
	fixture.service.Repo = &retryUnknownClosureRepo{Dir: fixture.repo, failRole: repository.RecordProcessorAttemptClosure}

	jobs, err := fixture.store.ListRecentJobs(context.Background(), 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("retry jobs = %+v, err=%v", jobs, err)
	}
	options := ProcessorRetryWorkerOptions{Owner: "retry-worker-unknown", LeaseTTL: time.Minute, BatchSize: 4, Now: time.Now}
	if err := fixture.service.runProcessorRetryBatch(context.Background(), processor, options); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), result.WorkspaceID, jobs[0].ID)
	if err != nil || job.State != sqlite.JobNeedsReconcile || processor.retryCalls != 1 {
		t.Fatalf("unknown retry job = %+v, calls=%d, err=%v", job, processor.retryCalls, err)
	}
	if err := fixture.service.runProcessorRetryBatch(context.Background(), processor, options); err != nil {
		t.Fatal(err)
	}
	job, err = fixture.store.GetJob(context.Background(), result.WorkspaceID, jobs[0].ID)
	if err != nil || job.State != sqlite.JobSucceeded || processor.retryCalls != 1 {
		t.Fatalf("reconciled retry job = %+v, calls=%d, err=%v", job, processor.retryCalls, err)
	}
	attempts, err := fixture.store.ListProcessorAttempts(context.Background(), result.WorkspaceID, result.SnapshotRef)
	if err != nil || len(attempts) != 2 || attempts[1].Status != "SUCCEEDED" {
		t.Fatalf("reconciled retry attempts = %+v, err=%v", attempts, err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err := fixture.service.Restore(context.Background(), result.SnapshotRef, destination); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(destination, "retry-unknown.txt"))
	if err != nil || string(payload) != "unknown closure keeps exact bytes" {
		t.Fatalf("restored unknown retry payload = %q, err=%v", payload, err)
	}
}

func TestProcessorRetryWorkerReconcilesUnknownPortableFactsWithoutReexecution(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "retry-portable-unknown.txt", []byte("unknown portable facts keep exact bytes"))
	initialProcessor := &retryFixtureProcessor{store: fixture.store}
	fixture.service.Processor = initialProcessor
	result := fixture.ingest(t, "sha256:processor-retry-portable-unknown-plan")
	insertRaceDescription(t, fixture, result, "portable retry unknown outcome")
	fixture.service.Repo = &retryUnknownClosureRepo{Dir: fixture.repo, failRole: repository.RecordPortableFactClosure}

	jobs, err := fixture.store.ListRecentJobs(context.Background(), 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("retry jobs = %+v, err=%v", jobs, err)
	}
	options := ProcessorRetryWorkerOptions{Owner: "retry-worker-portable-unknown", LeaseTTL: time.Minute, BatchSize: 4, Now: time.Now}
	if err := fixture.service.runProcessorRetryBatch(context.Background(), initialProcessor, options); err != nil {
		t.Fatal(err)
	}
	job, err := fixture.store.GetJob(context.Background(), result.WorkspaceID, jobs[0].ID)
	if err != nil || job.State != sqlite.JobNeedsReconcile || initialProcessor.retryCalls != 1 {
		t.Fatalf("unknown portable retry job = %+v, calls=%d, err=%v", job, initialProcessor.retryCalls, err)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatalf("close catalog for portable retry restart: %v", err)
	}
	reopened, err := sqlite.Open(context.Background(), fixture.catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatalf("reopen catalog for portable retry: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	fixture.store = reopened
	fixture.service.Store = reopened
	resumedProcessor := &retryFixtureProcessor{store: reopened}
	fixture.service.Processor = resumedProcessor
	if err := fixture.service.runProcessorRetryBatch(context.Background(), resumedProcessor, options); err != nil {
		t.Fatal(err)
	}
	job, err = reopened.GetJob(context.Background(), result.WorkspaceID, jobs[0].ID)
	if err != nil || job.State != sqlite.JobSucceeded || resumedProcessor.retryCalls != 0 {
		t.Fatalf("reconciled portable retry job = %+v, calls=%d, err=%v", job, resumedProcessor.retryCalls, err)
	}
	attempts, err := reopened.ListProcessorAttempts(context.Background(), result.WorkspaceID, result.SnapshotRef)
	if err != nil || len(attempts) != 2 || attempts[1].Status != "SUCCEEDED" {
		t.Fatalf("reconciled portable retry attempts = %+v, err=%v", attempts, err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err := fixture.service.Restore(context.Background(), result.SnapshotRef, destination); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(destination, "retry-portable-unknown.txt"))
	if err != nil || string(payload) != "unknown portable facts keep exact bytes" {
		t.Fatalf("restored unknown portable retry payload = %q, err=%v", payload, err)
	}
}
