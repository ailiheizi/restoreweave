package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/client/transport"
	rwconfig "github.com/ailiheizi/restoreweave/config"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestValidateRuntimeStorageProfileRejectsSilentlyIgnoredProfiles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*rwconfig.Config)
		want   string
	}{
		{
			name: "repository",
			mutate: func(cfg *rwconfig.Config) {
				cfg.Storage.RepositoryProfile = "local-qualified"
			},
			want: "repository profile",
		},
		{
			name: "compression",
			mutate: func(cfg *rwconfig.Config) {
				cfg.Storage.CompressionProfile = "lossless-default"
			},
			want: "compression profile",
		},
		{
			name: "neural codec",
			mutate: func(cfg *rwconfig.Config) {
				cfg.Storage.NeuralCodec = "rwkv-experimental"
			},
			want: "neural codec",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := rwconfig.Default()
			test.mutate(&cfg)
			if err := validateRuntimeStorageProfile(cfg); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRuntimeStorageProfileAcceptsCurrentDevelopmentProfile(t *testing.T) {
	if err := validateRuntimeStorageProfile(rwconfig.Default()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRuntimeStorageProfileAcceptsLocalZstdCandidate(t *testing.T) {
	cfg := rwconfig.Default()
	cfg.Storage.RepositoryProfile = rwconfig.RepositoryProfileLocalZstdV1
	cfg.Storage.CompressionProfile = rwconfig.CompressionProfileZstdV1
	if err := validateRuntimeStorageProfile(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryReaderDaemonIsCatalogAndSigningMaterialFree(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	want := []byte("real recovery reader daemon")
	if err := os.WriteFile(filepath.Join(source, "payload.bin"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(t.TempDir(), "repository")
	repo, err := repository.OpenDir(repoRoot)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	identityDir := t.TempDir()
	identity, anchor, err := exact.OpenSigningMaterial(identityDir, exact.DefaultPublicationDomain, true)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	writer := &exact.Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: exact.DefaultPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := writer.InspectIngest(ctx, source, exact.IngestOptions{})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	result, err := writer.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:recovery-reader-daemon-plan")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "recovery-bundle.json")
	if _, err := writer.ExportRecovery(ctx, result.SnapshotRef, bundlePath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	referencePath := filepath.Join(t.TempDir(), "recovery-reference.json")
	if _, err := writer.ExportRecoveryReference(ctx, result.SnapshotRef, referencePath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	anchorPath := filepath.Join(t.TempDir(), "trust-anchor.json")
	if _, err := exact.ExportTrustAnchor(anchor, anchorPath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(identityDir); err != nil {
		t.Fatal(err)
	}

	socketPath := filepath.Join("/tmp", "rw-recovery-"+filepath.Base(t.TempDir())+".sock")
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	readerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readerErr := make(chan error, 1)
	go func() {
		readerErr <- runWithOptions(readerCtx, daemonOptions{
			socketPath: socketPath, recoveryReader: true,
			recoveryReference: referencePath, trustAnchorPath: anchorPath,
			repositoryPath: repoRoot,
		})
	}()

	conn := waitForRecoveryReader(t, socketPath, readerErr)
	defer conn.Close()
	call := func(operation string, input any) command.Result {
		t.Helper()
		payload, marshalErr := json.Marshal(input)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		result, callErr := conn.Do(context.Background(), command.Envelope{Operation: operation, Input: payload})
		if callErr != nil {
			t.Fatal(callErr)
		}
		return result
	}
	imported := call(command.OpRecoveryImport, map[string]any{
		"artifact_path": bundlePath, "trust_anchor_path": anchorPath,
	})
	if imported.Status != command.StatusSucceeded {
		t.Fatalf("recovery.import = %s, reasons=%+v", imported.Status, imported.Reasons)
	}
	listed := call(command.OpSnapshotList, map[string]any{})
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.list = %s, reasons=%+v", listed.Status, listed.Reasons)
	}
	verified := call(command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": result.SnapshotRef, "mode": command.VerifyFullBytes,
	})
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.verify = %s, reasons=%+v", verified.Status, verified.Reasons)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	planned := call(command.OpPlanRestore, map[string]any{
		"snapshot_ref": result.SnapshotRef, "destination": destination,
	})
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.restore = %s, reasons=%+v", planned.Status, planned.Reasons)
	}
	var restorePlan command.PlanRestoreData
	if err := json.Unmarshal(planned.Data, &restorePlan); err != nil {
		t.Fatal(err)
	}
	applied := call(command.OpPlanApply, map[string]any{
		"workspace_id": restorePlan.WorkspaceID, "plan_id": restorePlan.PlanID,
		"plan_digest": restorePlan.PlanDigest,
	})
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("plan.apply = %s, reasons=%+v", applied.Status, applied.Reasons)
	}
	got, err := os.ReadFile(filepath.Join(destination, "payload.bin"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("restored payload = %q, err=%v", got, err)
	}
	if _, err := os.Stat(catalogPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery reader created catalog: stat err=%v", err)
	}
	for _, path := range []string{identityDir, filepath.Join(repoRoot, "indexes"), filepath.Join(repoRoot, "staging")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovery reader created runtime state %s: stat err=%v", path, err)
		}
	}
	cancel()
	select {
	case err := <-readerErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery reader did not stop")
	}
}

func TestRecoveryReaderCleanInstallUsesRealDaemonAndCLIProcesses(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	want := []byte("clean install across real daemon and CLI processes")
	if err := os.WriteFile(filepath.Join(source, "payload.bin"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(t.TempDir(), "writer-catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(t.TempDir(), "repository")
	repo, err := repository.OpenDir(repoRoot)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	signingRoot := t.TempDir()
	identity, anchor, err := exact.OpenSigningMaterial(signingRoot, exact.DefaultPublicationDomain, true)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	writer := &exact.Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: exact.DefaultPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := writer.InspectIngest(ctx, source, exact.IngestOptions{})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	published, err := writer.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:real-process-clean-install")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	referencePath := filepath.Join(t.TempDir(), "recovery-reference.json")
	if _, err := writer.ExportRecoveryReference(ctx, published.SnapshotRef, referencePath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	anchorPath := filepath.Join(t.TempDir(), "trust-anchor.json")
	if _, err := exact.ExportTrustAnchor(anchor, anchorPath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(signingRoot); err != nil {
		t.Fatal(err)
	}
	repositoryBefore := recoveryProcessTree(t, repoRoot)

	workspaceRoot := testWorkspaceRoot(t)
	binDir := t.TempDir()
	daemonBin := buildTestBinary(t, workspaceRoot, "./server/cmd/restoreweaved", filepath.Join(binDir, "restoreweaved"))
	rwBin := buildTestBinary(t, workspaceRoot, "./client/cmd/rw", filepath.Join(binDir, "rw"))
	socketPath := fmt.Sprintf("/tmp/rw-clean-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	probeRoot := t.TempDir()
	catalogProbe := filepath.Join(probeRoot, "must-not-create", "catalog.sqlite")
	recoveryProbe := filepath.Join(probeRoot, "must-not-create", "signing")
	vectorsProbe := filepath.Join(probeRoot, "must-not-create", "vectors")
	daemon := exec.Command(daemonBin,
		"--recovery-reader", "--repository", repoRoot,
		"--recovery-reference", referencePath, "--trust-anchor", anchorPath,
		"--socket", socketPath,
	)
	daemon.Env = append(os.Environ(),
		"RESTOREWEAVE_CONFIG="+filepath.Join(probeRoot, "missing-config.yaml"),
		"RESTOREWEAVE_CATALOG="+catalogProbe,
		"RESTOREWEAVE_RECOVERY_RECORDS="+recoveryProbe,
		"RESTOREWEAVE_VECTORS="+vectorsProbe,
	)
	var daemonLog bytes.Buffer
	daemon.Stdout = &daemonLog
	daemon.Stderr = &daemonLog
	if err := daemon.Start(); err != nil {
		t.Fatalf("start recovery reader process: %v", err)
	}
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.Wait() }()
	stopped := false
	defer func() {
		if stopped {
			return
		}
		_ = daemon.Process.Kill()
		<-daemonDone
	}()
	waitForReaderCLI(t, rwBin, socketPath, daemonDone, &daemonLog)

	imported := runRWProcess(t, rwBin, socketPath, "recovery", "import", referencePath, anchorPath)
	var importData command.RecoveryImportData
	decodeProcessResult(t, imported, &importData)
	if importData.Schema != exact.RecoveryReferenceSchemaV2 || importData.SnapshotRef != published.SnapshotRef || importData.CatalogCreated {
		t.Fatalf("real CLI recovery.import = %+v", importData)
	}
	listed := runRWProcess(t, rwBin, socketPath, "snapshot", "list")
	var listData command.SnapshotListData
	decodeProcessResult(t, listed, &listData)
	if len(listData.Snapshots) != 1 || listData.Snapshots[0].SnapshotRef != published.SnapshotRef {
		t.Fatalf("real CLI snapshot.list = %+v", listData)
	}
	verified := runRWProcess(t, rwBin, socketPath, "snapshot", "verify", published.SnapshotRef, "--mode", command.VerifyFullBytes)
	var verifyData command.SnapshotVerifyData
	decodeProcessResult(t, verified, &verifyData)
	if !verifyData.OK || verifyData.CatalogUsed {
		t.Fatalf("real CLI snapshot.verify = %+v", verifyData)
	}

	destination := filepath.Join(t.TempDir(), "restored")
	planned := runRWProcess(t, rwBin, socketPath, "restore", published.SnapshotRef, destination)
	var restorePlan command.PlanRestoreData
	decodeProcessResult(t, planned, &restorePlan)
	if restorePlan.Wrote || restorePlan.PlanID == "" || restorePlan.PlanDigest == "" || restorePlan.WorkspaceID == "" {
		t.Fatalf("real CLI restore plan = %+v", restorePlan)
	}
	applied := runRWProcess(t, rwBin, socketPath, "plan", "apply", restorePlan.PlanID,
		"--workspace", restorePlan.WorkspaceID, "--digest", restorePlan.PlanDigest)
	var applyData command.PlanApplyData
	decodeProcessResult(t, applied, &applyData)
	if applyData.SnapshotRef != published.SnapshotRef || applyData.Files != 1 {
		t.Fatalf("real CLI plan.apply = %+v", applyData)
	}
	got, err := os.ReadFile(filepath.Join(destination, "payload.bin"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("real process restored payload = %q, err=%v", got, err)
	}
	if after := recoveryProcessTree(t, repoRoot); !reflect.DeepEqual(after, repositoryBefore) {
		t.Fatalf("recovery reader process changed repository tree\nbefore=%v\nafter=%v", repositoryBefore, after)
	}
	for _, path := range []string{catalogPath, signingRoot, catalogProbe, recoveryProbe, vectorsProbe, filepath.Join(repoRoot, "indexes"), filepath.Join(repoRoot, "staging")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovery reader process created forbidden state %s: %v", path, err)
		}
	}

	if err := daemon.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("stop recovery reader process: %v", err)
	}
	select {
	case err := <-daemonDone:
		stopped = true
		if err != nil {
			t.Fatalf("recovery reader process exit: %v\n%s", err, daemonLog.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("recovery reader process did not stop\n%s", daemonLog.String())
	}
}

func testWorkspaceRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("workspace go.mod not found")
		}
		dir = parent
	}
}

func buildTestBinary(t *testing.T, workspaceRoot, target, destination string) string {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", destination, target)
	cmd.Dir = workspaceRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", target, err, output)
	}
	return destination
}

func waitForReaderCLI(t *testing.T, rwBin, socketPath string, daemonDone <-chan error, daemonLog *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-daemonDone:
			t.Fatalf("recovery reader exited before readiness: %v\n%s", err, daemonLog.String())
		default:
		}
		cmd := exec.Command(rwBin, "--socket", socketPath, "--json", "snapshot", "list")
		if err := cmd.Run(); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("recovery reader CLI readiness timed out\n%s", daemonLog.String())
}

func runRWProcess(t *testing.T, rwBin, socketPath string, args ...string) command.Result {
	t.Helper()
	fullArgs := append([]string{"--socket", socketPath, "--json"}, args...)
	cmd := exec.Command(rwBin, fullArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rw %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	var result command.Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode rw %s result: %v\n%s", strings.Join(args, " "), err, output)
	}
	if result.Status != command.StatusSucceeded {
		t.Fatalf("rw %s status = %s, reasons=%+v", strings.Join(args, " "), result.Status, result.Reasons)
	}
	return result
}

func decodeProcessResult(t *testing.T, result command.Result, target any) {
	t.Helper()
	if err := json.Unmarshal(result.Data, target); err != nil {
		t.Fatal(err)
	}
}

func recoveryProcessTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(payload)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		tree[rel] = hex.EncodeToString(digest[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func waitForRecoveryReader(t *testing.T, socketPath string, readerErr <-chan error) *transport.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-readerErr:
			t.Fatalf("recovery reader exited before listen: %v", err)
		default:
		}
		conn, err := transport.Dial(socketPath)
		if err == nil {
			return conn
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("recovery reader did not listen on %s", socketPath)
	return nil
}
