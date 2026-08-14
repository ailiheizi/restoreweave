package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testEpoch = time.Date(2026, time.August, 11, 12, 0, 0, 123456789, time.UTC)

func TestStableIDsAreOpaqueTypedAndUnique(t *testing.T) {
	first := testID(t, IDPrefixWorkspace)
	second := testID(t, IDPrefixWorkspace)
	if first == second {
		t.Fatalf("two generated stable ids are equal: %q", first)
	}
	if !strings.HasPrefix(first, IDPrefixWorkspace+"_") {
		t.Fatalf("stable id %q has the wrong diagnostic prefix", first)
	}
	if err := validateStableID(first); err != nil {
		t.Fatalf("validate generated stable id: %v", err)
	}
	if _, err := NewStableID("Bad-Prefix"); err == nil {
		t.Fatal("invalid stable id prefix unexpectedly succeeded")
	}
}

func TestOpenMigratesAndEnforcesSQLiteSafetyPragmas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	store := openTestStore(t, path)

	version, err := store.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != 6 {
		t.Fatalf("schema version = %d, want 6", version)
	}
	pragmas, err := store.RuntimePragmas(ctx)
	if err != nil {
		t.Fatalf("RuntimePragmas: %v", err)
	}
	if !strings.EqualFold(pragmas.JournalMode, "wal") {
		t.Fatalf("journal mode = %q, want WAL", pragmas.JournalMode)
	}
	if !pragmas.ForeignKeys {
		t.Fatal("foreign keys are disabled")
	}
	if pragmas.BusyTimeout != 2*time.Second {
		t.Fatalf("busy timeout = %s, want 2s", pragmas.BusyTimeout)
	}
	if pragmas.Synchronous != 2 {
		t.Fatalf("synchronous mode = %d, want FULL (2)", pragmas.Synchronous)
	}
	var migrationCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != 6 {
		t.Fatalf("migration rows = %d, want 6", migrationCount)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}
	reopened, err := Open(ctx, path, Options{BusyTimeout: 2 * time.Second, Now: func() time.Time { return testEpoch }})
	if err != nil {
		t.Fatalf("reopen migrated store: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}
}

func TestMigrationChecksumDriftFailsClosed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	store := openTestStore(t, path)
	if _, err := store.db.ExecContext(ctx,
		`UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1`); err != nil {
		t.Fatalf("tamper migration checksum: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	_, err := Open(ctx, path, Options{Now: func() time.Time { return testEpoch }})
	if !errors.Is(err, ErrMigrationDrift) {
		t.Fatalf("Open after migration drift error = %v, want ErrMigrationDrift", err)
	}
}

func TestCatalogRecordsTransactionsJobsAuditAndIdempotency(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	defer store.Close()

	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Local workspace"}
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertWorkspace(ctx, &workspace) }); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if workspace.Revision != 1 || !json.Valid(workspace.Metadata) {
		t.Fatalf("workspace defaults not applied: %+v", workspace)
	}

	rolledBackSource := Source{
		ID: testID(t, IDPrefixSource), WorkspaceID: workspace.ID,
		StableKey: "apfs:rollback", Kind: "APFS_ROOT", Locator: "/rollback", State: SourceActive,
	}
	rollbackMarker := errors.New("rollback marker")
	err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertSource(ctx, &rolledBackSource); err != nil {
			return err
		}
		return rollbackMarker
	})
	if !errors.Is(err, rollbackMarker) {
		t.Fatalf("transaction error = %v, want rollback marker", err)
	}
	if _, err := store.GetSource(ctx, workspace.ID, rolledBackSource.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back source lookup error = %v, want ErrNotFound", err)
	}

	missingWorkspaceSource := Source{
		ID: testID(t, IDPrefixSource), WorkspaceID: testID(t, IDPrefixWorkspace),
		StableKey: "apfs:missing", Kind: "APFS_ROOT", Locator: "/missing", State: SourceActive,
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertSource(ctx, &missingWorkspaceSource)
	}); err == nil {
		t.Fatal("foreign key violation unexpectedly succeeded")
	}

	source := Source{
		ID: testID(t, IDPrefixSource), WorkspaceID: workspace.ID,
		StableKey: "apfs:volume-1", Kind: "APFS_ROOT", Locator: "/Users/example",
		IdentityFingerprint: "sha256:source", State: SourceActive,
	}
	scan := ScanGeneration{
		ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID,
		SourceID: source.ID, Generation: 1, CaptureSetID: "capture-1",
		CaptureSetDigest: "sha256:capture", State: ScanRunning,
	}
	logicalSize := int64(12)
	observation := Observation{
		ID: testID(t, IDPrefixObservation), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan.ID,
		PathKey: []byte("docs/report.txt"), RawPath: []byte("docs/report.txt"),
		DisplayPath: "docs/report.txt", EntryType: EntryFile,
		ContentID: "sha256:content", FileVersionID: "file-version-1",
		StatDigest: "sha256:stat", LogicalSize: &logicalSize, ReadState: "READ_OK",
	}
	confidence := 0.99
	evidence := DetectionEvidence{
		ID: testID(t, IDPrefixDetectionEvidence), WorkspaceID: workspace.ID,
		ObservationID: observation.ID, DetectorID: "builtin.magic/v1",
		DetectorDigest: "sha256:detector", EvidenceKind: "MAGIC",
		CandidateFormat: "text/plain", CandidateMIME: "text/plain",
		Confidence: &confidence, ExecutionClass: "BYTE_DETERMINISTIC",
		Evidence:       json.RawMessage(`{"ranges":[[0,12]]}`),
		EvidenceDigest: "sha256:evidence", SandboxPolicyHash: "sha256:sandbox",
		StartedAt: testEpoch, FinishedAt: testEpoch.Add(time.Millisecond),
	}
	plan := Plan{
		ID: testID(t, IDPrefixPlan), WorkspaceID: workspace.ID,
		ScanGenerationID: scan.ID, Kind: "EXACT_BACKUP", State: PlanReady,
		PolicyRevision: "policy-1", Plan: json.RawMessage(`{"strategy":"RAW"}`),
		PlanDigest: "sha256:plan",
	}
	job := Job{
		ID: testID(t, IDPrefixJob), WorkspaceID: workspace.ID, PlanID: plan.ID,
		Kind: "BACKUP", State: JobQueued, MaxAttempts: 3,
		Input: json.RawMessage(`{"repository":"primary"}`),
	}
	firstAudit := AuditEvent{
		ID: testID(t, IDPrefixAuditEvent), WorkspaceID: workspace.ID,
		Actor: "local-admin", Action: "SOURCE_ADDED", TargetType: "SOURCE",
		TargetID: source.ID, RequestID: "request-1", Outcome: "SUCCEEDED",
		Details: json.RawMessage(`{"stableKey":"apfs:volume-1"}`),
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertSource(ctx, &source); err != nil {
			return err
		}
		if err := tx.InsertScanGeneration(ctx, &scan); err != nil {
			return err
		}
		if err := tx.InsertObservation(ctx, &observation); err != nil {
			return err
		}
		if err := tx.InsertDetectionEvidence(ctx, &evidence); err != nil {
			return err
		}
		if err := tx.InsertPlan(ctx, &plan); err != nil {
			return err
		}
		if err := tx.InsertJob(ctx, &job); err != nil {
			return err
		}
		return tx.AppendAuditEvent(ctx, &firstAudit)
	}); err != nil {
		t.Fatalf("insert catalog records: %v", err)
	}
	if firstAudit.Sequence != 1 || !strings.HasPrefix(firstAudit.EventDigest, "sha256:") {
		t.Fatalf("unexpected first audit chain record: %+v", firstAudit)
	}
	storedEvidence, err := store.GetDetectionEvidence(ctx, workspace.ID, evidence.ID)
	if err != nil || storedEvidence.CandidateMIME != "text/plain" || storedEvidence.Confidence == nil {
		t.Fatalf("stored detection evidence = %+v, err=%v", storedEvidence, err)
	}
	storedPlan, err := store.GetPlan(ctx, workspace.ID, plan.ID)
	if err != nil || storedPlan.PlanDigest != plan.PlanDigest {
		t.Fatalf("stored plan = %+v, err=%v", storedPlan, err)
	}

	secondAudit := AuditEvent{
		ID: testID(t, IDPrefixAuditEvent), WorkspaceID: workspace.ID,
		Actor: "worker", Action: "SCAN_STARTED", TargetType: "SCAN",
		TargetID: scan.ID, RequestID: "request-2", Outcome: "SUCCEEDED",
	}
	if err := store.Update(ctx, func(tx *Tx) error { return tx.AppendAuditEvent(ctx, &secondAudit) }); err != nil {
		t.Fatalf("append second audit event: %v", err)
	}
	if secondAudit.PreviousEventDigest != firstAudit.EventDigest || secondAudit.Sequence != 2 {
		t.Fatalf("audit chain did not advance: %+v", secondAudit)
	}
	auditEvents, err := store.ListAuditEvents(ctx, workspace.ID, 0, 10)
	if err != nil || len(auditEvents) != 2 || auditEvents[1].PreviousEventDigest != auditEvents[0].EventDigest {
		t.Fatalf("listed audit events = %+v, err=%v", auditEvents, err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM audit_events WHERE audit_event_id = ?`, firstAudit.ID); err == nil {
		t.Fatal("append-only audit delete unexpectedly succeeded")
	}

	if err := store.Update(ctx, func(tx *Tx) error {
		fence, err := tx.AcquireJobLease(
			ctx, workspace.ID, job.ID, 1, "worker-1", "lease-1",
			testEpoch.Add(time.Second), testEpoch.Add(time.Minute),
		)
		if err != nil {
			return err
		}
		if fence != 1 {
			return fmt.Errorf("fencing token = %d, want 1", fence)
		}
		return nil
	}); err != nil {
		t.Fatalf("acquire job lease: %v", err)
	}
	leasedJob, err := store.GetJob(ctx, workspace.ID, job.ID)
	if err != nil {
		t.Fatalf("get leased job: %v", err)
	}
	if leasedJob.State != JobRunning || leasedJob.Revision != 2 || leasedJob.Attempt != 1 {
		t.Fatalf("unexpected leased job: %+v", leasedJob)
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.UpdateJob(ctx, JobUpdate{
			WorkspaceID: workspace.ID, JobID: job.ID, ExpectedRevision: 2,
			State: JobSucceeded, Attempt: 1,
			Checkpoint: json.RawMessage(`{"uploaded":12}`),
			Result:     json.RawMessage(`{"receipt":"receipt-1"}`),
		})
	}); err != nil {
		t.Fatalf("finish job: %v", err)
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.UpdateJob(ctx, JobUpdate{
			WorkspaceID: workspace.ID, JobID: job.ID, ExpectedRevision: 3,
			State: JobRunning, Attempt: 1,
		})
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal job transition error = %v, want ErrInvalidTransition", err)
	}

	var callbackCount atomic.Int64
	idempotentJobID := testID(t, IDPrefixJob)
	request := IdempotencyRequest{
		WorkspaceID: workspace.ID, Scope: "jobs.create", Key: "client-key-1",
		RequestHash: "sha256:request-1",
	}
	callback := func(tx *Tx) (IdempotencyResult, error) {
		callbackCount.Add(1)
		created := Job{
			ID: idempotentJobID, WorkspaceID: workspace.ID, Kind: "VERIFY",
			State: JobQueued, MaxAttempts: 1,
		}
		if err := tx.InsertJob(ctx, &created); err != nil {
			return IdempotencyResult{}, err
		}
		return IdempotencyResult{
			ResourceType: "job", ResourceID: created.ID,
			Response: json.RawMessage(`{"status":"QUEUED"}`),
		}, nil
	}
	firstResult, replayed, err := store.Idempotent(ctx, request, callback)
	if err != nil || replayed {
		t.Fatalf("first idempotent call result=%+v replayed=%v err=%v", firstResult, replayed, err)
	}
	secondResult, replayed, err := store.Idempotent(ctx, request, func(*Tx) (IdempotencyResult, error) {
		t.Fatal("idempotency replay invoked callback")
		return IdempotencyResult{}, nil
	})
	if err != nil || !replayed || secondResult.ResourceID != firstResult.ResourceID {
		t.Fatalf("idempotency replay result=%+v replayed=%v err=%v", secondResult, replayed, err)
	}
	if callbackCount.Load() != 1 {
		t.Fatalf("idempotency callback count = %d, want 1", callbackCount.Load())
	}
	request.RequestHash = "sha256:different"
	if _, _, err := store.Idempotent(ctx, request, callback); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency hash mismatch error = %v, want ErrIdempotencyConflict", err)
	}

	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.FinishScanGeneration(ctx, workspace.ID, scan.ID, ScanComplete, true,
			json.RawMessage(`{"entries":1}`), testEpoch.Add(2*time.Minute))
	}); err != nil {
		t.Fatalf("finish scan: %v", err)
	}
	lateObservation := observation
	lateObservation.ID = testID(t, IDPrefixObservation)
	lateObservation.PathKey = []byte("late")
	lateObservation.RawPath = []byte("late")
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertObservation(ctx, &lateObservation)
	}); err == nil {
		t.Fatal("observation after completed scan unexpectedly succeeded")
	}
}

func TestConcurrentIdempotencyExecutesSideEffectOnce(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	defer store.Close()
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Concurrent workspace"}
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertWorkspace(ctx, &workspace) }); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	request := IdempotencyRequest{
		WorkspaceID: workspace.ID, Scope: "plans.generate", Key: "same-key",
		RequestHash: "sha256:same-request",
	}
	var sideEffects atomic.Int64
	const callers = 8
	var group sync.WaitGroup
	errorsFromCalls := make(chan error, callers)
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			result, _, err := store.Idempotent(ctx, request, func(*Tx) (IdempotencyResult, error) {
				sideEffects.Add(1)
				return IdempotencyResult{
					ResourceType: "plan", ResourceID: "logical-result",
					Response: json.RawMessage(`{"ok":true}`),
				}, nil
			})
			if err == nil && result.ResourceID != "logical-result" {
				err = fmt.Errorf("resource id = %q", result.ResourceID)
			}
			errorsFromCalls <- err
		}()
	}
	group.Wait()
	close(errorsFromCalls)
	for err := range errorsFromCalls {
		if err != nil {
			t.Fatalf("concurrent idempotent call: %v", err)
		}
	}
	if sideEffects.Load() != 1 {
		t.Fatalf("side effect count = %d, want 1", sideEffects.Load())
	}
}

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(context.Background(), path, Options{
		BusyTimeout:  2 * time.Second,
		MaxOpenConns: 4,
		Now:          func() time.Time { return testEpoch },
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

func testID(t *testing.T, prefix string) string {
	t.Helper()
	id, err := NewStableID(prefix)
	if err != nil {
		t.Fatalf("NewStableID(%q): %v", prefix, err)
	}
	return id
}
