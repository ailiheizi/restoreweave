package processor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestTextExtractAdmitsUTF8AndSkipsBinary(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	text := []byte("unique-extract-token for processor host")
	if err := os.WriteFile(filepath.Join(source, "note.txt"), text, 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "blob.bin"), []byte{0x00, 0xff, 0x80, 0x01}, 0o644); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	store, repo := testLane(t)
	host := NewHost(store, repo, Options{StagingDir: t.TempDir()})
	service := &exact.Service{Store: store, Repo: repo, Processor: host}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	artifacts, err := store.ListAdmittedArtifacts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("admitted artifacts = %d, want 1: %+v", len(artifacts), artifacts)
	}
	if artifacts[0].Body != string(text) || artifacts[0].Stage != "EXTRACT" {
		t.Fatalf("admitted artifact = %+v", artifacts[0])
	}
	if artifacts[0].State != sqlite.ArtifactAdmitted {
		t.Fatalf("state = %s", artifacts[0].State)
	}
	attempts, err := store.ListProcessorAttempts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	assertAttempt(t, attempts, CapabilityTextExtract, string(StatusSucceeded), "ADMITTED_ARTIFACT")
	assertAttempt(t, attempts, CapabilityTextEmbedding, string(StatusInapplicable), "CAPABILITY_NOT_CONFIGURED")
}

func TestProcessorRescanUsesStableSubjectRefForAttemptsAndArtifacts(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "stable.txt"), []byte("stable processor subject"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, repo := testLane(t)
	host := NewHost(store, repo, Options{StagingDir: t.TempDir()})
	service := &exact.Service{Store: store, Repo: repo, Processor: host}

	first, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	firstEntries, err := store.ListNamespaceContent(ctx, first.WorkspaceID, first.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstEntries) != 1 || firstEntries[0].SubjectRef == "" {
		t.Fatalf("first namespace content = %+v", firstEntries)
	}
	firstEntry := firstEntries[0]

	second, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	secondEntries, err := store.ListNamespaceContent(ctx, second.WorkspaceID, second.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondEntries) != 1 {
		t.Fatalf("second namespace content = %+v", secondEntries)
	}
	secondEntry := secondEntries[0]
	if secondEntry.ID == firstEntry.ID || secondEntry.SubjectRef != firstEntry.SubjectRef {
		t.Fatalf("processor rescan subject mapping = first=%+v second=%+v", firstEntry, secondEntry)
	}

	for _, result := range []struct {
		name        string
		workspace   string
		snapshot    string
		wantSubject string
	}{
		{name: "first", workspace: first.WorkspaceID, snapshot: first.SnapshotRef, wantSubject: firstEntry.SubjectRef},
		{name: "second", workspace: second.WorkspaceID, snapshot: second.SnapshotRef, wantSubject: secondEntry.SubjectRef},
	} {
		t.Run(result.name, func(t *testing.T) {
			attempts, err := store.ListProcessorAttempts(ctx, result.workspace, result.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if len(attempts) == 0 {
				t.Fatalf("processor attempts = %+v", attempts)
			}
			for _, attempt := range attempts {
				if attempt.SubjectRef != result.wantSubject {
					t.Fatalf("attempt subject ref = %q, want %q: %+v", attempt.SubjectRef, result.wantSubject, attempt)
				}
			}
			artifacts, err := store.ListAdmittedArtifacts(ctx, result.workspace, result.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if len(artifacts) == 0 {
				t.Fatalf("processor artifacts = %+v", artifacts)
			}
			for _, artifact := range artifacts {
				if artifact.SubjectRef != result.wantSubject {
					t.Fatalf("artifact subject ref = %q, want %q: %+v", artifact.SubjectRef, result.wantSubject, artifact)
				}
			}
		})
	}
}

func TestUnsealedOutputIsNotAdmitted(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "note.txt"), []byte("should-not-admit"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	store, repo := testLane(t)
	host := NewHost(store, repo, Options{
		StagingDir: t.TempDir(),
		Processors: []Processor{unsealedProc{}},
	})
	service := &exact.Service{Store: store, Repo: repo, Processor: host}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	artifacts, err := store.ListAdmittedArtifacts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("unsealed output was admitted: %+v", artifacts)
	}
	attempts, err := store.ListProcessorAttempts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	assertAttempt(t, attempts, CapabilityTextExtract, string(StatusFailed), "PROCESSOR_STAGE_FAILED")
}

func TestProcessorPanicDoesNotBlockExactLane(t *testing.T) {
	ctx := context.Background()
	source, payload := unknownTree(t)
	store, repo := testLane(t)
	host := NewHost(store, repo, Options{
		StagingDir:   t.TempDir(),
		Processors:   []Processor{panicProc{}},
		StageTimeout: 200 * time.Millisecond,
	})
	service := &exact.Service{Store: store, Repo: repo, Processor: host}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest with panicking processor: %v", err)
	}
	if len(ingested.Warnings) != 1 ||
		!strings.Contains(ingested.Warnings[0], "subject ") ||
		!strings.Contains(ingested.Warnings[0], CapabilityTextExtract) ||
		!strings.Contains(ingested.Warnings[0], "PROCESSOR_STAGE_FAILED") {
		t.Fatalf("panicking processor warnings = %+v", ingested.Warnings)
	}
	if _, err := service.Verify(ctx, ingested.SnapshotRef); err != nil {
		t.Fatalf("verify: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := service.Restore(ctx, ingested.SnapshotRef, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "unknown.bin"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("restored bytes changed")
	}
	artifacts, err := store.ListAdmittedArtifacts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("panic processor admitted artifacts: %+v", artifacts)
	}
	attempts, err := store.ListProcessorAttempts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	assertAttempt(t, attempts, CapabilityTextExtract, string(StatusFailed), "PROCESSOR_STAGE_FAILED")
}

func TestProcessorTimeoutDoesNotBlockExactLane(t *testing.T) {
	ctx := context.Background()
	source, payload := unknownTree(t)
	store, repo := testLane(t)
	host := NewHost(store, repo, Options{
		StagingDir:    t.TempDir(),
		Processors:    []Processor{hangProc{}},
		StageTimeout:  50 * time.Millisecond,
		MaxConcurrent: 1,
	})
	service := &exact.Service{Store: store, Repo: repo, Processor: host}
	started := time.Now()
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest with hanging processor: %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("ingest blocked on hanging processor for %s", time.Since(started))
	}
	if _, err := service.Verify(ctx, ingested.SnapshotRef); err != nil {
		t.Fatalf("verify: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := service.Restore(ctx, ingested.SnapshotRef, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "unknown.bin"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("restored bytes changed")
	}
	attempts, err := store.ListProcessorAttempts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	assertAttempt(t, attempts, CapabilityTextExtract, string(StatusCancelled), "PROCESSOR_STAGE_CANCELLED")
}

func TestHostRetryWorkerRerunsOnlyFailedNodeAndPublishesSignedSuccessor(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "retry.txt"), []byte("semantic retry content"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, repo := testLane(t)
	flaky := &failOnceTextProc{}
	host := NewHost(store, repo, Options{StagingDir: t.TempDir(), Processors: []Processor{flaky}})
	identity, anchor, err := exact.OpenSigningMaterial(t.TempDir(), "workspace:retry-host", true)
	if err != nil {
		t.Fatal(err)
	}
	service := &exact.Service{
		Store: store, Repo: repo, Processor: host, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: "workspace:retry-host", RequireSignedPublication: true,
		ConfigDigest: "sha256:retry-host-config",
	}
	plan, err := service.InspectIngest(ctx, source, exact.IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:host-retry-plan")
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.RunProcessorRetryWorker(workerCtx, exact.ProcessorRetryWorkerOptions{
			Owner: "host-retry-worker", PollInterval: 5 * time.Millisecond, LeaseTTL: time.Minute,
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	var retryJob sqlite.Job
	for time.Now().Before(deadline) {
		jobs, listErr := store.ListRecentJobs(ctx, 10)
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(jobs) == 1 && jobs[0].State == sqlite.JobSucceeded {
			retryJob = jobs[0]
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if retryJob.State != sqlite.JobSucceeded || retryJob.Attempt != 1 {
		t.Fatalf("host retry job = %+v", retryJob)
	}
	attempts, err := store.ListProcessorAttempts(ctx, result.WorkspaceID, result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	var extract []sqlite.ProcessorAttempt
	for _, attempt := range attempts {
		if attempt.CapabilityID == CapabilityTextExtract {
			extract = append(extract, attempt)
		}
	}
	if len(extract) != 2 || extract[0].Status != string(StatusFailed) || extract[1].Status != string(StatusSucceeded) || extract[1].FenceToken != retryJob.FencingToken {
		t.Fatalf("host retry attempts = %+v", extract)
	}
	if flaky.calls != 2 {
		t.Fatalf("processor calls = %d, want initial failure plus one retry", flaky.calls)
	}
	closures, err := service.ListProcessorAttemptClosures(ctx, result.SnapshotRef)
	if err != nil || len(closures) != 2 || closures[1].Closure.AttemptCount != int64(len(attempts)) {
		t.Fatalf("host retry signed closures = %+v, err=%v", closures, err)
	}
}

func TestHostRetryRejectsStaleWorkerPublicationAfterLeaseTakeover(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "retry.txt"), []byte("stale retry content"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, repo := testLane(t)
	proc := &takeoverTextProc{started: make(chan struct{}), release: make(chan struct{})}
	clock := time.Now().UTC()
	var clockMu sync.Mutex
	now := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}
	host := NewHost(store, repo, Options{StagingDir: t.TempDir(), Processors: []Processor{proc}, Now: now})
	identity, anchor, err := exact.OpenSigningMaterial(t.TempDir(), "workspace:stale-retry", true)
	if err != nil {
		t.Fatal(err)
	}
	service := &exact.Service{
		Store: store, Repo: repo, Processor: host, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: "workspace:stale-retry", RequireSignedPublication: true,
		ConfigDigest: "sha256:stale-retry-config",
	}
	plan, err := service.InspectIngest(ctx, source, exact.IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:stale-retry-plan")
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := store.ListRecentJobs(ctx, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("retry jobs = %+v, err=%v", jobs, err)
	}
	oldLeaseUntil := clock.Add(time.Second)
	var oldFence int64
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		var acquireErr error
		oldFence, acquireErr = tx.AcquireJobLease(ctx, result.WorkspaceID, jobs[0].ID, jobs[0].Revision,
			"old-worker", "old-lease", clock, oldLeaseUntil)
		return acquireErr
	}); err != nil {
		t.Fatal(err)
	}
	leasedJob, err := store.GetJob(ctx, result.WorkspaceID, jobs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := store.ListProcessorAttempts(ctx, result.WorkspaceID, result.SnapshotRef)
	if err != nil {
		t.Fatalf("initial attempts = %+v, err=%v", attempts, err)
	}
	initialAttemptCount := len(attempts)
	var previous sqlite.ProcessorAttempt
	for _, attempt := range attempts {
		if attempt.CapabilityID == CapabilityTextExtract && attempt.Status == string(StatusFailed) {
			previous = attempt
			break
		}
	}
	if previous.ID == "" {
		t.Fatalf("missing failed extract attempt in %+v", attempts)
	}
	retryDone := make(chan error, 1)
	go func() {
		retryDone <- host.RetryPublication(ctx, result.WorkspaceID, result.SnapshotRef, result.RootID, exact.ProcessorRetryInvocation{
			JobID: jobs[0].ID, Owner: "old-worker", Attempt: leasedJob.Attempt,
			IdempotencyKey: "processor-retry:sha256:stale-retry-plan", LeaseToken: "old-lease", FenceToken: oldFence,
			Targets: []exact.ProcessorRetryTarget{{
				SubjectRef: previous.SubjectRef, RouteDigest: previous.RouteDigest, Stage: previous.Stage,
				CapabilityID: previous.CapabilityID, PredecessorAttemptID: previous.ID, ReasonCode: previous.ReasonCode,
			}},
		})
	}()
	<-proc.started
	clockMu.Lock()
	clock = oldLeaseUntil.Add(time.Second)
	takeoverNow := clock
	clockMu.Unlock()
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		_, acquireErr := tx.AcquireJobLease(ctx, result.WorkspaceID, jobs[0].ID, leasedJob.Revision,
			"new-worker", "new-lease", takeoverNow, takeoverNow.Add(time.Minute))
		return acquireErr
	}); err != nil {
		t.Fatal(err)
	}
	close(proc.release)
	if err := <-retryDone; err == nil {
		t.Fatalf("stale retry publication error = %v", err)
	}
	attempts, err = store.ListProcessorAttempts(ctx, result.WorkspaceID, result.SnapshotRef)
	if err != nil || len(attempts) != initialAttemptCount {
		t.Fatalf("stale worker changed attempts = %+v, err=%v", attempts, err)
	}
}

func assertAttempt(t *testing.T, attempts []sqlite.ProcessorAttempt, capabilityID, status, reasonCode string) {
	t.Helper()
	for _, attempt := range attempts {
		if attempt.CapabilityID == capabilityID && attempt.Status == status && attempt.ReasonCode == reasonCode {
			return
		}
	}
	t.Fatalf("missing attempt capability=%q status=%q reason=%q in %+v", capabilityID, status, reasonCode, attempts)
}

func testLane(t *testing.T) (*sqlite.Store, repository.Driver) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	return store, repo
}

func unknownTree(t *testing.T) (string, []byte) {
	t.Helper()
	source := t.TempDir()
	payload := []byte{0x00, 0x01, 0xde, 0xad}
	if err := os.WriteFile(filepath.Join(source, "unknown.bin"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "note.txt"), []byte("hang-or-panic subject"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}
	return source, payload
}

type panicProc struct{}

func (panicProc) CapabilityID() string { return CapabilityTextExtract }
func (panicProc) Stage() Stage         { return StageExtract }
func (panicProc) RunStage(context.Context, Invocation) (StageResult, error) {
	panic("processor exploded")
}

type hangProc struct{}

func (hangProc) CapabilityID() string { return CapabilityTextExtract }
func (hangProc) Stage() Stage         { return StageExtract }
func (hangProc) RunStage(ctx context.Context, _ Invocation) (StageResult, error) {
	<-ctx.Done()
	return StageResult{Status: StatusCancelled, Reason: ctx.Err().Error()}, nil
}

type unsealedProc struct{}

func (unsealedProc) CapabilityID() string { return CapabilityTextExtract }
func (unsealedProc) Stage() Stage         { return StageExtract }
func (unsealedProc) RunStage(_ context.Context, inv Invocation) (StageResult, error) {
	_, _ = inv.Staging.Write([]byte("unsealed"))
	return StageResult{
		Status:    StatusSucceeded,
		SchemaRef: SchemaRefExtractedText(),
		MediaType: MediaTypeUTF8Text,
		Sealed:    false,
	}, nil
}

type failOnceTextProc struct {
	mu    sync.Mutex
	calls int
}

type takeoverTextProc struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (*takeoverTextProc) CapabilityID() string { return CapabilityTextExtract }
func (*takeoverTextProc) Stage() Stage         { return StageExtract }
func (p *takeoverTextProc) RunStage(_ context.Context, inv Invocation) (StageResult, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		return StageResult{Status: StatusFailed, Reason: "temporary processor failure"}, nil
	}
	close(p.started)
	<-p.release
	if _, err := inv.Staging.Write([]byte("stale retry content")); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, err
	}
	if err := inv.Staging.Seal(); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, err
	}
	return StageResult{
		Status: StatusSucceeded, DeterminismClass: DeterminismByteExact,
		SchemaRef: SchemaRefExtractedText(), MediaType: MediaTypeUTF8Text, Sealed: true,
	}, nil
}

func (*failOnceTextProc) CapabilityID() string { return CapabilityTextExtract }
func (*failOnceTextProc) Stage() Stage         { return StageExtract }
func (p *failOnceTextProc) RunStage(_ context.Context, inv Invocation) (StageResult, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		return StageResult{Status: StatusFailed, Reason: "temporary processor failure"}, nil
	}
	if _, err := inv.Staging.Write([]byte("semantic retry content")); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, err
	}
	if err := inv.Staging.Seal(); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, err
	}
	return StageResult{
		Status: StatusSucceeded, DeterminismClass: DeterminismByteExact,
		SchemaRef: SchemaRefExtractedText(), MediaType: MediaTypeUTF8Text, Sealed: true,
	}, nil
}
