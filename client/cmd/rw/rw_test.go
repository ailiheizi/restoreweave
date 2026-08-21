package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
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

func TestCLIConfigLifecycleWithoutDaemon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restoreweave", "config.yaml")
	code, stdout, stderr := runCLI(t, "config", "init", "--path", path)
	if code != 0 {
		t.Fatalf("config init code = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, path) || !strings.Contains(stdout, "sha256:") {
		t.Fatalf("config init output:\n%s", stdout)
	}
	code, stdout, stderr = runCLI(t, "config", "validate", "--path", path)
	if code != 0 || !strings.Contains(stdout, "valid:") {
		t.Fatalf("config validate code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	code, stdout, stderr = runCLI(t, "config", "show", "--path", path)
	if code != 0 {
		t.Fatalf("config show code=%d stderr=%s", code, stderr)
	}
	for _, fragment := range []string{
		"repository_profile: directory-cas-dev-v1",
		"compression_profile: identity-v1",
		"embedding_mode: local",
		"local_profile: bge-small-zh-v1.5",
		"vector_backend: zvec",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("config show missing %q:\n%s", fragment, stdout)
		}
	}
}

func TestCLIDescriptionCreateListAndGet(t *testing.T) {
	socketPath, seed := startDaemon(t)
	body := "主角在雪后的城市中寻找旧档案。\n\n这是用户保存的完整描述。"
	bodyPath := filepath.Join(t.TempDir(), "description.txt")
	if err := os.WriteFile(bodyPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, "--socket", socketPath, "description", "create", seed.FileEntryID,
		"--workspace", seed.WorkspaceID, "--kind", "USER", "--language", "zh", "--title", "剧情与感想",
		"--body-file", bodyPath, "--accepted")
	if code != 0 {
		t.Fatalf("description create code=%d stderr=%s", code, stderr)
	}
	var documentID string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "description: ") {
			documentID = strings.TrimSpace(strings.TrimPrefix(line, "description: "))
		}
	}
	if documentID == "" || !strings.Contains(stdout, "segments:") {
		t.Fatalf("description create output:\n%s", stdout)
	}

	code, stdout, stderr = runCLI(t, "--socket", socketPath, "description", "list", seed.FileEntryID,
		"--workspace", seed.WorkspaceID)
	if code != 0 || !strings.Contains(stdout, documentID) || strings.Contains(stdout, body) {
		t.Fatalf("description list code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}

	code, stdout, stderr = runCLI(t, "--socket", socketPath, "description", "get", documentID,
		"--workspace", seed.WorkspaceID)
	if code != 0 || !strings.Contains(stdout, body) {
		t.Fatalf("description get code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestCLIDescriptionRejectsAmbiguousBodyInput(t *testing.T) {
	code, _, stderr := runCLI(t, "description", "create", "nse_00000000000000000000000000000000",
		"--workspace", "wsp_00000000000000000000000000000000", "--kind", "USER",
		"--body", "inline", "--body-file", "other.txt")
	if code == 0 || !strings.Contains(stderr, "exactly one") {
		t.Fatalf("ambiguous body code=%d stderr=%s", code, stderr)
	}
}

func TestParseIngestLocatorFlag(t *testing.T) {
	tests := []struct {
		input   string
		path    string
		locator string
	}{
		{"https://example.test/file.bin", "", "https://example.test/file.bin"},
		{"nested/file.bin=https://example.test/file.bin", "nested/file.bin", "https://example.test/file.bin"},
		{"release.bin=ipfs://bafy-example/release.bin", "release.bin", "ipfs://bafy-example/release.bin"},
	}
	for _, test := range tests {
		got, err := parseIngestLocatorFlag(test.input)
		if err != nil {
			t.Fatalf("parse %q: %v", test.input, err)
		}
		if got.Path != test.path || got.Locator != test.locator {
			t.Fatalf("parse %q = %+v", test.input, got)
		}
	}
	if _, err := parseIngestLocatorFlag("relative/path"); err == nil {
		t.Fatal("locator without URI scheme was accepted")
	}
	for _, unsafe := range []string{
		"https://user:secret@example.test/file.bin",
		"https://example.test/file.bin?token=secret",
		"https://example.test/file.bin#secret",
	} {
		if _, err := parseIngestLocatorFlag(unsafe); err == nil {
			t.Fatalf("unsafe locator %q was accepted", unsafe)
		}
	}
}

func TestRenderPlanIngestShowsReadyPlanAndApplyCommand(t *testing.T) {
	var out bytes.Buffer
	renderPlanIngest(&out, command.PlanIngestData{
		WorkspaceID:       "wsp_test",
		SourceID:          "src_test",
		SnapshotRef:       "snap_should_not_be_printed",
		ManifestDigest:    "sha256:manifest_should_not_be_printed",
		PlanID:            "pln_test",
		PlanDigest:        "sha256:plan_test",
		State:             "READY",
		Executable:        true,
		ConfigDigest:      "sha256:config_test",
		SourceBasisDigest: "sha256:basis_test",
		ProtectionDigest:  "sha256:protection_test",
		FileProtection:    map[string]string{"nested/link.bin": "LINK_ONLY"},
		ProtectionDecisions: []command.IngestProtectionDecisionData{{
			RelativePath: "nested/link.bin", Mode: "LINK_ONLY",
			PlannedOutcome: "LINK_ONLY_UNPROTECTED", ReasonCode: "EXTERNAL_LOCATOR_UNVALIDATED",
			ExpectedContentID: "sha256:link", ExpectedLogicalBytes: 42, LocatorCount: 1,
		}},
		Files: 3,
		Bytes: 42,
	})
	text := out.String()
	for _, fragment := range []string{
		"state:       READY",
		"executable:  true",
		"workspace:   wsp_test",
		"source:      src_test",
		"config:      sha256:config_test",
		"source basis: sha256:basis_test",
		"protection digest: sha256:protection_test",
		"nested/link.bin: LINK_ONLY",
		"nested/link.bin: LINK_ONLY -> LINK_ONLY_UNPROTECTED (EXTERNAL_LOCATOR_UNVALIDATED)",
		"estimated files: 3",
		"estimated bytes: 42",
		"next:        rw plan apply pln_test --workspace wsp_test --digest sha256:plan_test",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("plan ingest output missing %q:\n%s", fragment, text)
		}
	}
	for _, forbidden := range []string{"snap_should_not_be_printed", "manifest_should_not_be_printed"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("plan ingest output presented applied-only field %q:\n%s", forbidden, text)
		}
	}
}

func TestRenderPlanIngestExplainsBlockedEntries(t *testing.T) {
	var out bytes.Buffer
	renderPlanIngest(&out, command.PlanIngestData{
		WorkspaceID: "wsp_test",
		SourceID:    "src_test",
		PlanID:      "pln_blocked",
		PlanDigest:  "sha256:blocked",
		State:       "READY",
		Executable:  false,
		BlockedEntries: []command.IngestPlanIssueData{{
			RelativePath:   "changing.bin",
			Mode:           "STORE_EXACT",
			PlannedOutcome: "BLOCKED",
			State:          "UNSTABLE",
			ReasonCode:     "CONTENT_CHANGED_DURING_READ",
			Message:        "file changed while it was being hashed",
		}},
	})
	text := out.String()
	for _, fragment := range []string{
		"executable:  false",
		"blocked entries:",
		"changing.bin: STORE_EXACT -> BLOCKED; UNSTABLE (CONTENT_CHANGED_DURING_READ): file changed while it was being hashed",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("blocked plan output missing %q:\n%s", fragment, text)
		}
	}
	if strings.Contains(text, "rw plan apply") {
		t.Fatalf("blocked plan printed an apply command:\n%s", text)
	}
}

func TestPlanRevisionDecisionsFileAndRendering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.json")
	payload := `[{"path":"payload.txt","mode":"METADATA_ONLY","reason":"regenerable"}]`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPlanRevisionDecisions(path)
	if err != nil {
		t.Fatalf("load decisions: %v", err)
	}
	if string(loaded) != payload {
		t.Fatalf("loaded decisions = %s", loaded)
	}

	invalid := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(invalid, []byte("["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPlanRevisionDecisions(invalid); err == nil {
		t.Fatal("invalid decisions JSON was accepted")
	}

	var out bytes.Buffer
	renderPlanRevision(&out, command.PlanReviseData{
		PlanID:      "pln_successor",
		PlanDigest:  "sha256:successor",
		WorkspaceID: "wsp_test",
		BasePlanID:  "pln_base",
		BaseDigest:  "sha256:base",
		State:       "READY",
		Executable:  true,
	})
	for _, fragment := range []string{
		"plan_id:          pln_successor",
		"base plan:        pln_base",
		"next:              rw plan apply pln_successor --workspace wsp_test --digest sha256:successor",
	} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("revision output missing %q:\n%s", fragment, out.String())
		}
	}
}

func TestCLIRegistersPlanRevisionCommands(t *testing.T) {
	root := NewRootCommand()
	for _, path := range [][]string{{"plan", "revise"}, {"plan", "abandon"}} {
		commandNode, _, err := root.Find(path)
		if err != nil || commandNode == nil || commandNode.Name() != path[len(path)-1] {
			t.Fatalf("find %v = command=%v err=%v", path, commandNode, err)
		}
	}
}

func TestCLIRegistersRecoveryAnchorExport(t *testing.T) {
	root := NewRootCommand()
	commandNode, _, err := root.Find([]string{"recovery", "anchor", "export"})
	if err != nil || commandNode == nil || commandNode.Name() != "export" {
		t.Fatalf("find recovery anchor export = command=%v err=%v", commandNode, err)
	}
}

func TestRenderPlanRestoreIsPlanOnly(t *testing.T) {
	var out bytes.Buffer
	renderPlanRestore(&out, command.PlanRestoreData{
		WorkspaceID: "wsp_restore",
		SnapshotRef: "snap_test",
		Destination: "/tmp/restore-target",
		Files:       2,
		Bytes:       17,
		PlanID:      "pln_restore",
		PlanDigest:  "sha256:restore_plan",
		State:       "READY",
		Executable:  true,
	})
	text := out.String()
	for _, fragment := range []string{
		"state:        READY",
		"executable:   true",
		"wrote:        false (plan only; apply separately)",
		"workspace:    wsp_restore",
		"next:         rw plan apply pln_restore --workspace wsp_restore --digest sha256:restore_plan",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("restore plan output missing %q:\n%s", fragment, text)
		}
	}
}

func TestRenderPlanApplyShowsJobAndResult(t *testing.T) {
	var out bytes.Buffer
	renderPlanApply(&out, command.PlanApplyData{
		PlanID:           "pln_apply",
		PlanDigest:       "sha256:apply_plan",
		JobID:            "job_apply",
		State:            "SUCCEEDED",
		AlreadyApplied:   true,
		SnapshotRef:      "snp_applied",
		ManifestDigest:   "sha256:manifest",
		ProtectionDigest: "sha256:protection",
		ProtectionDecisions: []command.IngestProtectionDecisionData{{
			RelativePath: "unknown.bin", Mode: "STORE_EXACT", PlannedOutcome: "EXACT_FALLBACK",
			ReasonCode: "CONTENT_CLASS_UNRESOLVED_EXACT_FALLBACK", ExpectedContentID: "sha256:unknown", ExpectedLogicalBytes: 99,
		}},
		Files:    4,
		Bytes:    99,
		Warnings: []string{"indexer: unavailable"},
	})
	text := out.String()
	for _, fragment := range []string{
		"state:            SUCCEEDED",
		"result:           already-applied (replayed)",
		"job:              job_apply",
		"plan digest:      sha256:apply_plan",
		"snapshot:         snp_applied",
		"manifest digest:  sha256:manifest",
		"protection digest: sha256:protection",
		"unknown.bin: STORE_EXACT -> EXACT_FALLBACK (CONTENT_CLASS_UNRESOLVED_EXACT_FALLBACK)",
		"files:            4",
		"bytes:            99",
		"warning:          indexer: unavailable",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("plan apply output missing %q:\n%s", fragment, text)
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
