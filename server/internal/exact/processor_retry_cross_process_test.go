package exact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const (
	processorRetryHelperMode       = "RW_PROCESSOR_RETRY_HELPER_MODE"
	processorRetryHelperCatalog    = "RW_PROCESSOR_RETRY_HELPER_CATALOG"
	processorRetryHelperRepository = "RW_PROCESSOR_RETRY_HELPER_REPOSITORY"
	processorRetryHelperSigning    = "RW_PROCESSOR_RETRY_HELPER_SIGNING"
	processorRetryHelperReady      = "RW_PROCESSOR_RETRY_HELPER_READY"
)

type crossProcessRetryProcessor struct {
	store     *sqlite.Store
	mode      string
	readyPath string
}

func (p *crossProcessRetryProcessor) ProcessPublication(context.Context, string, string, string) error {
	return nil
}

func (p *crossProcessRetryProcessor) RetryPublication(ctx context.Context, workspaceID, snapshotRef, rootID string, invocation ProcessorRetryInvocation) error {
	if p.mode == "crash" {
		if err := os.WriteFile(p.readyPath, []byte("leased\n"), 0o600); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}
	return (&retryFixtureProcessor{store: p.store}).RetryPublication(ctx, workspaceID, snapshotRef, rootID, invocation)
}

func TestProcessorRetryWorkerCrossProcessCrashTakeover(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	catalogPath := filepath.Join(root, "catalog.sqlite")
	repositoryRoot := filepath.Join(root, "repository")
	signingRoot := filepath.Join(root, "signing")
	readyPath := filepath.Join(root, "crash-worker.ready")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("cross-process retry keeps exact bytes")
	if err := os.WriteFile(filepath.Join(source, "retry.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := repository.OpenDir(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity, anchor, err := OpenSigningMaterial(signingRoot, testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	initialProcessor := &retryFixtureProcessor{store: store}
	service := &Service{
		Store: store, Repo: repo, Processor: initialProcessor,
		SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
		ConfigDigest: "sha256:processor-retry-cross-process-config",
	}
	plan, err := service.InspectIngest(ctx, source, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:processor-retry-cross-process-plan")
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := store.ListRecentJobs(ctx, 10)
	if err != nil || len(jobs) != 1 || jobs[0].State != sqlite.JobQueued {
		t.Fatalf("scheduled retry jobs = %+v, err=%v", jobs, err)
	}
	initialAttempts, err := store.ListProcessorAttempts(ctx, result.WorkspaceID, result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	crash := processorRetryHelperCommand("crash", catalogPath, repositoryRoot, signingRoot, readyPath)
	var crashOutput bytes.Buffer
	crash.Stdout = &crashOutput
	crash.Stderr = &crashOutput
	if err := crash.Start(); err != nil {
		t.Fatal(err)
	}
	waitForRetryHelperFile(t, readyPath, crash, 10*time.Second)
	if err := crash.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := crash.Wait(); err == nil {
		t.Fatal("crash worker exited successfully after forced termination")
	}

	waitForRetryJobClaimable(t, catalogPath, 10*time.Second)
	takeover := processorRetryHelperCommand("takeover", catalogPath, repositoryRoot, signingRoot, "")
	takeoverOutput, err := takeover.CombinedOutput()
	if err != nil {
		t.Fatalf("takeover worker failed: %v\n%s", err, takeoverOutput)
	}

	reopened, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	job, err := reopened.GetJob(ctx, result.WorkspaceID, jobs[0].ID)
	if err != nil || job.State != sqlite.JobSucceeded || job.Attempt != 2 || job.FencingToken != 2 {
		t.Fatalf("cross-process takeover job = %+v, err=%v", job, err)
	}
	attempts, err := reopened.ListProcessorAttempts(ctx, result.WorkspaceID, result.SnapshotRef)
	if err != nil || len(attempts) != len(initialAttempts)+1 || attempts[len(attempts)-1].Status != "SUCCEEDED" {
		t.Fatalf("cross-process retry attempts = %+v, err=%v", attempts, err)
	}
	closures, err := serviceWithStoreAndRepository(reopened, repo, &identity, &anchor).ListProcessorAttemptClosures(ctx, result.SnapshotRef)
	if err != nil || len(closures) != 2 || closures[1].Closure.AttemptCount != int64(len(attempts)) {
		t.Fatalf("cross-process retry closures = %+v, err=%v", closures, err)
	}
	destination := filepath.Join(root, "restore")
	reader := &Service{Repo: repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.Restore(ctx, result.SnapshotRef, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(destination, "retry.txt"))
	if err != nil || !bytes.Equal(restored, payload) {
		t.Fatalf("restored payload = %q, err=%v", restored, err)
	}
}

func TestProcessorRetryCrossProcessHelper(t *testing.T) {
	mode := os.Getenv(processorRetryHelperMode)
	if mode == "" {
		return
	}
	catalogPath := os.Getenv(processorRetryHelperCatalog)
	repositoryRoot := os.Getenv(processorRetryHelperRepository)
	signingRoot := os.Getenv(processorRetryHelperSigning)
	readyPath := os.Getenv(processorRetryHelperReady)
	if catalogPath == "" || repositoryRoot == "" || signingRoot == "" || (mode == "crash" && readyPath == "") {
		t.Fatal("processor retry helper configuration is incomplete")
	}
	ctx := context.Background()
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo, err := repository.OpenDir(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity, anchor, err := OpenSigningMaterial(signingRoot, testPublicationDomain, false)
	if err != nil {
		t.Fatal(err)
	}
	processor := &crossProcessRetryProcessor{store: store, mode: mode, readyPath: readyPath}
	service := serviceWithStoreAndRepository(store, repo, &identity, &anchor)
	service.Processor = processor
	if err := service.runProcessorRetryBatch(ctx, processor, ProcessorRetryWorkerOptions{
		Owner: "cross-process-" + mode, LeaseTTL: 750 * time.Millisecond, BatchSize: 1, Now: time.Now,
	}); err != nil {
		t.Fatal(err)
	}
	if mode != "crash" {
		fmt.Println("TAKEOVER_SUCCEEDED")
	}
}

func serviceWithStoreAndRepository(store *sqlite.Store, repo repository.Driver, identity *SigningIdentity, anchor *TrustAnchor) *Service {
	return &Service{
		Store: store, Repo: repo, SigningIdentity: identity, TrustAnchor: anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
		ConfigDigest: "sha256:processor-retry-cross-process-config",
	}
}

func processorRetryHelperCommand(mode, catalogPath, repositoryRoot, signingRoot, readyPath string) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestProcessorRetryCrossProcessHelper$", "-test.v=false")
	command.Env = append(os.Environ(),
		processorRetryHelperMode+"="+mode,
		processorRetryHelperCatalog+"="+catalogPath,
		processorRetryHelperRepository+"="+repositoryRoot,
		processorRetryHelperSigning+"="+signingRoot,
		processorRetryHelperReady+"="+readyPath,
	)
	return command
}

func waitForRetryHelperFile(t *testing.T, path string, command *exec.Cmd, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if command.ProcessState != nil && command.ProcessState.Exited() {
			t.Fatalf("processor retry helper exited before readiness: %v", command.ProcessState)
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = command.Process.Kill()
	t.Fatal("timed out waiting for processor retry helper readiness")
}

func waitForRetryJobClaimable(t *testing.T, catalogPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		store, err := sqlite.Open(context.Background(), catalogPath, sqlite.Options{})
		if err != nil {
			t.Fatal(err)
		}
		jobs, listErr := store.ListClaimableJobs(context.Background(), processorRetryJobKind, time.Now().UTC(), 1)
		_ = store.Close()
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(jobs) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for crashed retry lease to expire")
}
