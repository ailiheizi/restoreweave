package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func addClosureTestAttempt(t *testing.T, fixture signedPublicationFixture, result IngestResult) {
	t.Helper()
	attemptID, err := sqlite.NewStableID(sqlite.IDPrefixAttempt)
	if err != nil {
		t.Fatal(err)
	}
	attempt := &sqlite.ProcessorAttempt{
		ID: attemptID, WorkspaceID: result.WorkspaceID, SubjectRef: "ns:test-subject",
		SnapshotRef: result.SnapshotRef, RouteDigest: "sha256:route",
		Route: json.RawMessage(`{"kind":"PROCESSING","nodes":[]}`), Stage: "EXTRACT",
		CapabilityID: "extract.text.v1", Status: "FAILED", ReasonCode: "PROCESSOR_STAGE_FAILED",
		Reason: "fixture failure", Provenance: json.RawMessage(`{"source_content_id":"sha256:content"}`),
		FenceToken: 1, ProcessorDigest: "sha256:processor",
		CreatedAt: time.Unix(1, 0).UTC(), FinishedAt: time.Unix(2, 0).UTC(),
	}
	if err := fixture.store.InsertProcessorAttempt(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
}

func TestProcessorAttemptClosureIsCatalogFreeAndBindsParent(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "attempt.txt", []byte("attempt closure"))
	result := fixture.ingest(t, "sha256:attempt-plan")
	addClosureTestAttempt(t, fixture, result)
	if err := fixture.service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatalf("publish processor closure: %v", err)
	}
	reader := &Service{
		Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	closures, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatalf("read processor closure: %v", err)
	}
	if len(closures) != 1 || closures[0].Closure.ParentCommitDigest != result.PublicationCommitDigest || closures[0].Closure.AttemptCount != 1 {
		t.Fatalf("closures = %+v", closures)
	}
	// The second publication attempt is idempotent for the same deterministic
	// attempt bundle and does not create a second child record.
	if err := fixture.service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatalf("idempotent processor closure: %v", err)
	}
	closures, err = reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 1 {
		t.Fatalf("idempotent closures = %d, err=%v", len(closures), err)
	}
}

func TestProcessorAttemptClosureSerializesSameParentAcrossServices(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "concurrent-attempt.txt", []byte("concurrent attempt closure"))
	result := fixture.ingest(t, "sha256:concurrent-attempt-plan")
	addClosureTestAttempt(t, fixture, result)
	second := &Service{
		Store: fixture.store, Repo: fixture.repo,
		SigningIdentity: fixture.service.SigningIdentity, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, service := range []*Service{fixture.service, second} {
		wg.Add(1)
		go func(service *Service) {
			defer wg.Done()
			errs <- service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
		}(service)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent processor closure: %v", err)
		}
	}
	closures, err := fixture.service.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(closures) != 1 {
		t.Fatalf("same-parent processor closures = %+v, want one child", closures)
	}
}

func TestProcessorAttemptClosureFiltersMultipleSnapshots(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "first.txt", []byte("first closure"))
	first := fixture.ingest(t, "sha256:first-attempt-plan")
	addClosureTestAttempt(t, fixture, first)
	if err := fixture.service.publishProcessorAttemptClosure(context.Background(), first.WorkspaceID, first.SnapshotRef, first.PublicationCommitDigest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.source, "second.txt"), []byte("second closure"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := fixture.ingest(t, "sha256:second-attempt-plan")
	addClosureTestAttempt(t, fixture, second)
	if err := fixture.service.publishProcessorAttemptClosure(context.Background(), second.WorkspaceID, second.SnapshotRef, second.PublicationCommitDigest); err != nil {
		t.Fatal(err)
	}
	reader := &Service{
		Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	for _, result := range []IngestResult{first, second} {
		closures, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
		if err != nil || len(closures) != 1 || closures[0].Closure.ParentCommitDigest != result.PublicationCommitDigest {
			t.Fatalf("snapshot %s closures = %+v, err=%v", result.SnapshotRef, closures, err)
		}
	}
	all, err := reader.ListProcessorAttemptClosures(context.Background(), "")
	if err != nil || len(all) != 2 {
		t.Fatalf("all closures = %d, err=%v", len(all), err)
	}
}

func TestProcessorAttemptClosureRejectsTamperingWithoutBlockingExactRestore(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "attempt.txt", []byte("attempt closure"))
	result := fixture.ingest(t, "sha256:attempt-plan")
	addClosureTestAttempt(t, fixture, result)
	if err := fixture.service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatal(err)
	}
	driver := fixture.repo
	digests, err := driver.ListRecordDigests(context.Background(), repository.RecordProcessorAttemptClosure)
	if err != nil || len(digests) != 1 {
		t.Fatalf("closure digests = %v, err=%v", digests, err)
	}
	payload, err := readRecord(context.Background(), driver, repository.RecordProcessorAttemptClosure, digests[0])
	if err != nil {
		t.Fatal(err)
	}
	var envelope ProcessorAttemptClosureEnvelope
	if err := decodeStrictRecord(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(payload, []byte("fixture failure"), []byte("fixture failurE"), 1)
	if bytes.Equal(tampered, payload) {
		t.Fatal("tamper fixture did not change payload")
	}
	if _, err := driver.PlaceRecord(context.Background(), repository.RecordProcessorAttemptClosure, bytes.NewReader(tampered)); err != nil {
		t.Fatal(err)
	}
	reader := &Service{
		Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	if _, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef); err == nil {
		t.Fatal("tampered processor closure was accepted")
	}
	listed, err := reader.ListSnapshots(context.Background())
	if err != nil || len(listed) != 1 || listed[0].SnapshotRef != result.SnapshotRef {
		t.Fatalf("supplement tampering affected exact discovery: %+v, err=%v", listed, err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err := reader.Restore(context.Background(), result.SnapshotRef, destination); err != nil {
		t.Fatalf("supplement tampering affected exact restore: %v", err)
	}
}

func TestProcessorAttemptClosureRejectsConflictingReplay(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "attempt.txt", []byte("attempt closure"))
	result := fixture.ingest(t, "sha256:attempt-plan")
	addClosureTestAttempt(t, fixture, result)
	if err := fixture.service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatal(err)
	}
	driver := fixture.repo
	digests, err := driver.ListRecordDigests(context.Background(), repository.RecordProcessorAttemptClosure)
	if err != nil || len(digests) != 1 {
		t.Fatalf("closure digests = %v, err=%v", digests, err)
	}
	payload, err := readRecord(context.Background(), driver, repository.RecordProcessorAttemptClosure, digests[0])
	if err != nil {
		t.Fatal(err)
	}
	var envelope ProcessorAttemptClosureEnvelope
	if err := decodeStrictRecord(payload, &envelope); err != nil {
		t.Fatal(err)
	}

	// A different signed bundle for the same parent is a replay conflict.
	var changed processorAttemptBundle
	if err := json.Unmarshal(envelope.Bundle, &changed); err != nil {
		t.Fatal(err)
	}
	changed.Attempts[0].Reason = "conflicting terminal result"
	changedBundle, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	conflict := envelope.Closure
	conflict.AttemptBundleDigest = DigestBytes(changedBundle)
	conflict.AttemptBundleLength = int64(len(changedBundle))
	conflict, err = SignProcessorAttemptClosure(*fixture.service.SigningIdentity, conflict)
	if err != nil {
		t.Fatal(err)
	}
	conflictPayload, err := CanonicalJSON(ProcessorAttemptClosureEnvelope{Schema: ProcessorAttemptClosureEnvelopeSchemaV1, Closure: conflict, Bundle: changedBundle})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.PlaceRecord(context.Background(), repository.RecordProcessorAttemptClosure, bytes.NewReader(conflictPayload)); err != nil {
		t.Fatal(err)
	}
	reader := &Service{
		Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	if _, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef); err == nil ||
		!strings.Contains(err.Error(), "conflicting") {
		t.Fatal("conflicting processor closure was accepted")
	}
}

func TestProcessorAttemptClosureRejectsMissingCommittedParent(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "attempt.txt", []byte("attempt closure"))
	result := fixture.ingest(t, "sha256:attempt-plan")
	addClosureTestAttempt(t, fixture, result)
	if err := fixture.service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatal(err)
	}
	digests, err := fixture.repo.ListRecordDigests(context.Background(), repository.RecordProcessorAttemptClosure)
	if err != nil || len(digests) != 1 {
		t.Fatalf("closure digests = %v, err=%v", digests, err)
	}
	payload, err := readRecord(context.Background(), fixture.repo, repository.RecordProcessorAttemptClosure, digests[0])
	if err != nil {
		t.Fatal(err)
	}
	var envelope ProcessorAttemptClosureEnvelope
	if err := decodeStrictRecord(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	// Re-signing a child with a nonexistent parent is also rejected by the
	// catalog-free reader, even though the child signature itself is valid.
	missing := envelope.Closure
	missing.ParentCommitDigest = "sha256:" + repeatByte('f', 64)
	missing, err = SignProcessorAttemptClosure(*fixture.service.SigningIdentity, missing)
	if err != nil {
		t.Fatal(err)
	}
	missingPayload, err := CanonicalJSON(ProcessorAttemptClosureEnvelope{Schema: ProcessorAttemptClosureEnvelopeSchemaV1, Closure: missing, Bundle: envelope.Bundle})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.PlaceRecord(context.Background(), repository.RecordProcessorAttemptClosure, bytes.NewReader(missingPayload)); err != nil {
		t.Fatal(err)
	}
	reader := &Service{
		Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	if _, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef); err == nil ||
		!strings.Contains(err.Error(), "not a committed publication") {
		t.Fatal("missing parent closure was accepted")
	}
}

func TestProcessorAttemptClosureRejectsSignerOutsideTrustAnchorBeforePlacement(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "attempt.txt", []byte("attempt closure"))
	result := fixture.ingest(t, "sha256:attempt-plan")
	addClosureTestAttempt(t, fixture, result)

	otherIdentity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.SigningIdentity = &otherIdentity
	if err := fixture.service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err == nil ||
		!errors.Is(err, ErrRecoveryTrustAnchor) {
		t.Fatalf("publish with mismatched signer error = %v", err)
	}
	digests, err := fixture.repo.ListRecordDigests(context.Background(), repository.RecordProcessorAttemptClosure)
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 0 {
		t.Fatalf("mismatched signer placed processor closures: %v", digests)
	}
}

type attemptRecordingFailure struct{ store *sqlite.Store }

func (processor attemptRecordingFailure) ProcessPublication(ctx context.Context, workspaceID, snapshotRef, _ string) error {
	attemptID, err := sqlite.NewStableID(sqlite.IDPrefixAttempt)
	if err != nil {
		return err
	}
	when := time.Unix(3, 0).UTC()
	if err := processor.store.InsertProcessorAttempt(ctx, &sqlite.ProcessorAttempt{
		ID: attemptID, WorkspaceID: workspaceID, SubjectRef: "ns:failed-subject", SnapshotRef: snapshotRef,
		RouteDigest: "sha256:failure-route", Route: json.RawMessage(`{"kind":"PROCESSING","nodes":[]}`),
		Stage: "EXTRACT", CapabilityID: "extract.failure.v1", Status: "FAILED",
		ReasonCode: "PROCESSOR_STAGE_FAILED", Reason: "expected processor failure",
		Provenance: json.RawMessage(`{"source_content_id":"sha256:fixture"}`), FenceToken: 1,
		ProcessorDigest: "sha256:failure-processor", CreatedAt: when, FinishedAt: when.Add(time.Second),
	}); err != nil {
		return err
	}
	return errors.New("expected processor failure")
}

func TestFailedProcessorAttemptIsSignedWithoutBlockingExactPublication(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "failure.txt", []byte("exact bytes survive"))
	fixture.service.Processor = attemptRecordingFailure{store: fixture.store}
	result := fixture.ingest(t, "sha256:failed-processor-plan")
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "expected processor failure") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	reader := &Service{
		Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	closures, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 1 || closures[0].Closure.AttemptCount != 1 {
		t.Fatalf("failed attempt closures = %+v, err=%v", closures, err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err := reader.Restore(context.Background(), result.SnapshotRef, destination); err != nil {
		t.Fatalf("restore after processor failure: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(destination, "failure.txt"))
	if err != nil || string(restored) != "exact bytes survive" {
		t.Fatalf("restored bytes = %q, err=%v", restored, err)
	}
}

type attemptRecordingCancellation struct {
	store  *sqlite.Store
	cancel context.CancelFunc
}

func (processor attemptRecordingCancellation) ProcessPublication(ctx context.Context, workspaceID, snapshotRef, _ string) error {
	attemptID, err := sqlite.NewStableID(sqlite.IDPrefixAttempt)
	if err != nil {
		return err
	}
	when := time.Unix(5, 0).UTC()
	if err := processor.store.InsertProcessorAttempt(ctx, &sqlite.ProcessorAttempt{
		ID: attemptID, WorkspaceID: workspaceID, SubjectRef: "ns:cancelled-subject", SnapshotRef: snapshotRef,
		RouteDigest: "sha256:cancelled-route", Route: json.RawMessage(`{"kind":"PROCESSING","nodes":[]}`),
		Stage: "EXTRACT", CapabilityID: "extract.cancelled.v1", Status: "CANCELLED",
		ReasonCode: "PROCESSOR_STAGE_CANCELLED", Reason: "expected cancellation",
		Provenance: json.RawMessage(`{"source_content_id":"sha256:fixture"}`), FenceToken: 1,
		ProcessorDigest: "sha256:cancelled-processor", CreatedAt: when, FinishedAt: when.Add(time.Second),
	}); err != nil {
		return err
	}
	processor.cancel()
	return context.Canceled
}

func TestCancelledProcessorAttemptStillGetsSignedPostCommit(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "cancelled.txt", []byte("committed before cancellation"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.service.Processor = attemptRecordingCancellation{store: fixture.store, cancel: cancel}
	plan, err := fixture.service.InspectIngest(ctx, fixture.source, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:cancelled-processor-plan")
	if err != nil {
		t.Fatalf("apply after post-commit cancellation: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], context.Canceled.Error()) {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	reader := &Service{
		Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	closures, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 1 {
		t.Fatalf("cancelled attempt closures = %+v, err=%v", closures, err)
	}
	var bundle processorAttemptBundle
	if err := json.Unmarshal(closures[0].Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Attempts) != 1 || bundle.Attempts[0].Status != "CANCELLED" {
		t.Fatalf("cancelled attempt bundle = %+v", bundle.Attempts)
	}
}

func repeatByte(value byte, count int) string {
	return string(bytes.Repeat([]byte{value}, count))
}
