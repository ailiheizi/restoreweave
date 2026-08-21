package processor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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
