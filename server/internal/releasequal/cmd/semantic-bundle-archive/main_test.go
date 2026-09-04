package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSemanticBundleArchiveCommandHelpAndAbsolutePathValidation(t *testing.T) {
	root := repositoryRoot(t)
	command := func(args ...string) ([]byte, error) {
		cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
		cmd.Dir = filepath.Join(root, "server", "internal", "releasequal", "cmd", "semantic-bundle-archive")
		return cmd.CombinedOutput()
	}
	if output, err := command("--help"); err != nil || !strings.Contains(string(output), "source") {
		t.Fatalf("help: err=%v output=%s", err, output)
	}
	if output, err := command("--source", "relative", "--output", "/tmp/semantic-bundle.tar.gz"); err == nil || !strings.Contains(string(output), "absolute") {
		t.Fatalf("relative source accepted: err=%v output=%s", err, output)
	}
}

func TestSemanticBundleArchiveCommandMissingSourceDoesNotCreateOutput(t *testing.T) {
	root := repositoryRoot(t)
	output := filepath.Join(t.TempDir(), "bundle.tar.gz")
	cmd := exec.Command("go", "run", ".", "--source", filepath.Join(t.TempDir(), "missing"), "--output", output)
	cmd.Dir = filepath.Join(root, "server", "internal", "releasequal", "cmd", "semantic-bundle-archive")
	if result, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("missing source accepted: %s", result)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("missing source created output: %v", err)
	}
	if err := os.WriteFile(output, []byte("existing sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("go", "run", ".", "--source", filepath.Join(t.TempDir(), "missing"), "--output", output)
	cmd.Dir = filepath.Join(root, "server", "internal", "releasequal", "cmd", "semantic-bundle-archive")
	if result, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("existing output accepted: %s", result)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) != "existing sentinel" {
		t.Fatalf("existing output changed: bytes=%q err=%v", got, err)
	}
}

func TestSemanticBundleArchiveCommandRealBundleAndInstaller(t *testing.T) {
	source := os.Getenv("RESTOREWEAVE_REAL_SEMANTIC_BUNDLE")
	if source == "" {
		t.Skip("RESTOREWEAVE_REAL_SEMANTIC_BUNDLE is not set; no live model lookup or download is permitted")
	}
	root := repositoryRoot(t)
	archive := filepath.Join(t.TempDir(), "semantic-bundle.tar.gz")
	cmd := exec.Command("go", "run", ".", "--source", source, "--output", archive)
	cmd.Dir = filepath.Join(root, "server", "internal", "releasequal", "cmd", "semantic-bundle-archive")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build command: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "CANDIDATE_ONLY_NOT_SUPPORTED") {
		t.Fatalf("candidate status missing: %s", output)
	}
	if _, err := search.InstallDefaultSemanticBundleFromArchive(context.Background(), t.TempDir(), archive); err != nil {
		t.Fatalf("temporary offline installer: %v", err)
	}
}
