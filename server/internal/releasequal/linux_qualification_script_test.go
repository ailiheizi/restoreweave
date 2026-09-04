package releasequal

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLinuxQualificationScriptSyntaxAndRequiredGates(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "scripts", "linux-qualification.sh")
	if output, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("bash syntax: %v\n%s", err, output)
	}
	payload, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, required := range []string{
		"semantic bundle install",
		"--semantic-archive",
		"--offline",
		"--archive \"$OFFLINE_ARCHIVE\"",
		"semantic_install_mode",
		"TestSupervisedONNXSemanticQualification",
		"TestRealBGEEmbeddingBuildsAndQueriesNativeZvec",
		"TestRealDaemonSemanticEndToEnd",
		"--corpus-profile heterogeneous",
		`Action == "skip"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("qualification script missing required gate %q", required)
		}
	}
	if strings.Contains(strings.ToLower(text), "docker") {
		t.Fatal("native qualification script must not invoke Docker")
	}
}

func TestLinuxQualificationHelpHasNoHostPrerequisite(t *testing.T) {
	script := filepath.Join(repositoryRoot(t), "scripts", "linux-qualification.sh")
	command := exec.Command("bash", script, "--help")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("help: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "--artifacts DIR") {
		t.Fatalf("help output = %q", output)
	}
}

func TestLinuxQualificationExtractsTopLevelBundleAssetsSafely(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to exercise the qualification manifest parser")
	}
	script := filepath.Join(repositoryRoot(t), "scripts", "linux-qualification.sh")
	root := filepath.Join(t.TempDir(), "bundle")
	manifest := filepath.Join(t.TempDir(), "semantic-bundle.json")
	fixture := map[string]any{
		"schema":    "restoreweave.semantic-bundle.v1",
		"runtime":   map[string]any{"path": "runtime/libonnxruntime.so"},
		"model":     map[string]any{"path": "models/foo..bar.onnx"},
		"tokenizer": map[string]any{"path": "tokenizer.json"},
		"zvec":      map[string]any{"path": "zvec/libzvec.so"},
	}
	payload, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("bash", script, "--inspect-bundle", manifest, root).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect top-level manifest: %v\n%s", err, output)
	}
	want := strings.Join([]string{
		filepath.Join(root, "runtime/libonnxruntime.so"),
		filepath.Join(root, "models/foo..bar.onnx"),
		filepath.Join(root, "tokenizer.json"),
		filepath.Join(root, "zvec/libzvec.so"),
	}, "\n") + "\n"
	if string(output) != want {
		t.Fatalf("asset paths = %q, want %q", output, want)
	}

	fixture["model"] = map[string]any{"path": "models/../escape.onnx"}
	payload, err = json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("bash", script, "--inspect-bundle", manifest, root).CombinedOutput(); err == nil {
		t.Fatalf("traversal manifest was accepted: %s", output)
	}

	fixture["model"] = map[string]any{"path": "models/model.onnx"}
	fixture["schema"] = "restoreweave.semantic-bundle.v2"
	payload, err = json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("bash", script, "--inspect-bundle", manifest, root).CombinedOutput(); err == nil {
		t.Fatalf("wrong manifest schema was accepted: %s", output)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}
