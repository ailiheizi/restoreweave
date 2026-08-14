package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/controlplane"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// startDaemon runs the real control-plane server for the duration of the
// test and returns its socket path plus the seeded namespace IDs.
func startDaemon(t *testing.T) (string, *testutil.NamespaceSeed) {
	t.Helper()
	store := testutil.OpenStore(t, ":memory:")
	seed := testutil.SeedNamespace(t, store)
	socketPath := testutil.TempSocketPath(t)
	dispatcher := controlplane.NewDispatcher(store, filepath.Join(t.TempDir(), "catalog.sqlite"), socketPath)
	server, err := controlplane.NewServer(dispatcher, socketPath)
	if err != nil {
		t.Fatalf("start control plane server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
		<-done
	})
	return socketPath, seed
}

// runCLI executes the root command with the given args and returns the exit
// code and captured stdout/stderr.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return root.Code(), stdout.String(), stderr.String()
}

func TestCLIStatus(t *testing.T) {
	socketPath, _ := startDaemon(t)
	code, stdout, stderr := runCLI(t, "--socket", socketPath, "status")
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	for _, fragment := range []string{
		"controller:    restoreweaved",
		"catalog ok:    true",
		"identify id:   identify:builtin",
		"rules digest:  sha256:",
		"unimplemented: ",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("status output missing %q:\n%s", fragment, stdout)
		}
	}
}

func TestCLIStatusJSON(t *testing.T) {
	socketPath, _ := startDaemon(t)
	code, stdout, _ := runCLI(t, "--socket", socketPath, "--json", "status")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	for _, fragment := range []string{
		`"schema": "org.restoreweave.result.v1"`,
		`"status": "SUCCEEDED"`,
		`"operation": "status.get"`,
		`"controller": "restoreweaved"`,
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("status --json output missing %q:\n%s", fragment, stdout)
		}
	}
}

func TestCLICapabilityList(t *testing.T) {
	socketPath, _ := startDaemon(t)
	code, stdout, _ := runCLI(t, "--socket", socketPath, "capability", "list")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stdout, "operation") || !strings.Contains(stdout, "status.get") ||
		!strings.Contains(stdout, "AVAILABLE") {
		t.Fatalf("capability list output:\n%s", stdout)
	}
	if !strings.Contains(stdout, "search.query") || !strings.Contains(stdout, "UNAVAILABLE") {
		t.Fatalf("capability list must show search.query UNAVAILABLE:\n%s", stdout)
	}
}

func TestCLINamespaceList(t *testing.T) {
	socketPath, seed := startDaemon(t)
	code, stdout, stderr := runCLI(t, "--socket", socketPath, "namespace", "list", seed.RootID,
		"--workspace", seed.WorkspaceID)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "DIRECTORY") || !strings.Contains(stdout, "Music") {
		t.Fatalf("namespace list output:\n%s", stdout)
	}

	code, stdout, stderr = runCLI(t, "--socket", socketPath, "namespace", "list", seed.RootID,
		"--workspace", seed.WorkspaceID, "--parent", seed.DirEntryID)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, seed.FileEntryID) || !strings.Contains(stdout, "\\xfftrack.flac") {
		t.Fatalf("namespace children output:\n%s", stdout)
	}
}

func TestCLINamespaceStatAndReadlink(t *testing.T) {
	socketPath, seed := startDaemon(t)
	code, stdout, stderr := runCLI(t, "--socket", socketPath, "namespace", "stat", seed.FileEntryID,
		"--workspace", seed.WorkspaceID)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "entry_type:      REGULAR_FILE") || !strings.Contains(stdout, "logical_size:    16") {
		t.Fatalf("namespace stat output:\n%s", stdout)
	}

	code, stdout, stderr = runCLI(t, "--socket", socketPath, "namespace", "readlink", seed.SymlinkEntryID,
		"--workspace", seed.WorkspaceID)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "target:   \\xfftrack.flac") {
		t.Fatalf("namespace readlink output:\n%s", stdout)
	}
}

func TestCLINamespaceResolve(t *testing.T) {
	socketPath, seed := startDaemon(t)
	code, stdout, stderr := runCLI(t, "--socket", socketPath, "namespace", "resolve", "Music",
		"--workspace", seed.WorkspaceID, "--root", seed.RootID)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, seed.DirEntryID) || !strings.Contains(stdout, "DIRECTORY") {
		t.Fatalf("namespace resolve output:\n%s", stdout)
	}
}

func TestCLIRepresentationList(t *testing.T) {
	socketPath, seed := startDaemon(t)
	code, stdout, stderr := runCLI(t, "--socket", socketPath, "representation", "list", seed.FileEntryID,
		"--workspace", seed.WorkspaceID)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "RECORDED") || !strings.Contains(stdout, "restic-stream/v1") {
		t.Fatalf("representation list output:\n%s", stdout)
	}
}

func TestCLIContentUnimplemented(t *testing.T) {
	socketPath, _ := startDaemon(t)
	code, _, stderr := runCLI(t, "--socket", socketPath, "content", "open")
	if code != 4 {
		t.Fatalf("exit code = %d, want 4 (FAILED)", code)
	}
	if !strings.Contains(stderr, "unimplemented") {
		t.Fatalf("content open stderr:\n%s", stderr)
	}

	code, _, stderr = runCLI(t, "--socket", socketPath, "content", "read", "handle-1", "0", "1024")
	if code != 4 || !strings.Contains(stderr, "unimplemented") {
		t.Fatalf("content read: code=%d stderr=%s", code, stderr)
	}

	code, _, stderr = runCLI(t, "--socket", socketPath, "content", "close", "handle-1")
	if code != 4 || !strings.Contains(stderr, "unimplemented") {
		t.Fatalf("content close: code=%d stderr=%s", code, stderr)
	}
}

func TestCLICannotReachDaemon(t *testing.T) {
	code, _, stderr := runCLI(t, "--socket", filepath.Join(t.TempDir(), "missing.sock"), "status")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "cannot reach restoreweaved at") {
		t.Fatalf("stderr:\n%s", stderr)
	}
}

func TestCLINamespaceRejectsMalformedID(t *testing.T) {
	socketPath, _ := startDaemon(t)
	code, _, stderr := runCLI(t, "--socket", socketPath, "namespace", "stat", "not-a-stable-id",
		"--workspace", "wsp_00000000000000000000000000000000")
	if code != 4 {
		t.Fatalf("exit code = %d, want 4", code)
	}
	if !strings.Contains(stderr, "invalid_input") {
		t.Fatalf("stderr:\n%s", stderr)
	}
}
