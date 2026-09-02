//go:build cgo && (darwin || linux)

package processor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageVerifiedONNXRuntimeUsesPrivateImmutableCopy(t *testing.T) {
	payload := []byte("verified-runtime-bytes")
	sum := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	root, path, err := stageVerifiedONNXRuntime(payload, digest)
	if err != nil {
		t.Fatalf("stage runtime: %v", err)
	}
	defer func() {
		if err := removeONNXRuntimeStage(root); err != nil {
			t.Errorf("remove runtime stage: %v", err)
		}
	}()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read staged runtime: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("staged runtime = %q, want %q", got, payload)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("staging root remains writable: %o", info.Mode().Perm())
	}
	if _, _, err := stageVerifiedONNXRuntime(payload, "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("runtime bytes with wrong admitted digest were staged")
	}
}

func TestPinnedONNXRuntimeGoDependencyAndHeader(t *testing.T) {
	command := exec.Command("go", "list", "-m", "-json", onnxRuntimeGoModulePath)
	payload, err := command.Output()
	if err != nil {
		t.Fatalf("inspect linked dependency: %v", err)
	}
	var module struct {
		Path    string
		Version string
		Sum     string
		Dir     string
		Replace *struct{}
	}
	if err := json.Unmarshal(payload, &module); err != nil {
		t.Fatalf("decode linked dependency: %v", err)
	}
	if module.Path != onnxRuntimeGoModulePath || module.Version != onnxRuntimeGoModuleVersion || module.Sum != onnxRuntimeGoModuleSum || module.Replace != nil {
		t.Fatalf("unadmitted ONNX Runtime Go module: %+v", module)
	}
	if onnxRuntimeGoModuleVersion != "v1.33.0" || onnxRuntimeGoBindingCommit != "b4e0f0b495a4ad1eb8a2f61c0286b6e670771525" {
		t.Fatalf("unexpected binding pin %s/%s", onnxRuntimeGoModuleVersion, onnxRuntimeGoBindingCommit)
	}
	if strings.TrimSpace(module.Dir) == "" || !filepath.IsAbs(module.Dir) || filepath.Clean(module.Dir) != module.Dir {
		t.Fatalf("linked dependency has no canonical source directory: %q", module.Dir)
	}
	header := filepath.Join(module.Dir, "onnxruntime_c_api.h")
	payload, err = os.ReadFile(header)
	if err != nil {
		t.Fatalf("read vendored C API header: %v", err)
	}
	want := "#define ORT_API_VERSION 29"
	if !strings.Contains(string(payload), want) || onnxRuntimeGoBindingCAPI != 29 {
		t.Fatalf("vendored binding does not prove %q", want)
	}
}
