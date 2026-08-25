//go:build cgo && purego && arm64 && supervised_integration && (darwin || linux)

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	rwconfig "github.com/ailiheizi/restoreweave/config"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// TestRealDaemonSemanticEndToEnd proves the production daemon path after the
// worker and zvec component proofs: a real bundle is admitted, durable
// description segments publish a semantic generation, capability.list reports
// the real provider, and deleting that disposable generation degrades until a
// subsequent rebuild restores the same profile-bound search.
func TestRealDaemonSemanticEndToEnd(t *testing.T) {
	if strings.TrimSpace(os.Getenv("RESTOREWEAVE_RUN_SUPERVISED_ONNX")) != "1" {
		t.Skip("RESTOREWEAVE_RUN_SUPERVISED_ONNX=1 is required for daemon qualification")
	}
	runtimeSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_ONNX_RUNTIME"))
	modelSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_ONNX_MODEL"))
	tokenizerSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_TOKENIZER"))
	zvecSource := strings.TrimSpace(os.Getenv("RESTOREWEAVE_ZVEC_LIBRARY"))
	if runtimeSource == "" || modelSource == "" || tokenizerSource == "" || zvecSource == "" {
		t.Skip("real BGE, ONNX runtime, and zvec asset paths are required")
	}
	workspaceRoot := testWorkspaceRoot(t)
	binDir := t.TempDir()
	daemonBin := buildTestBinaryWithTags(t, workspaceRoot, "./server/cmd/restoreweaved", filepath.Join(binDir, "restoreweaved"), "purego")
	rwBin := buildTestBinaryWithTags(t, workspaceRoot, "./client/cmd/rw", filepath.Join(binDir, "rw"), "purego")

	root := t.TempDir()
	dataHome := filepath.Join(root, "xdg-data")
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "semantic.txt"), []byte("洪水城市档案恢复说明"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config", "config.yaml")
	env := append(os.Environ(), "XDG_DATA_HOME="+dataHome)
	initConfig := exec.Command(rwBin, "config", "init", "--path", configPath)
	initConfig.Env = env
	if output, err := initConfig.CombinedOutput(); err != nil {
		t.Fatalf("rw config init: %v\n%s", err, output)
	}
	config, err := rwconfig.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.Semantic.EmbeddingMode != "local" || config.Semantic.LocalProfile != search.SemanticBundleBGEProfileID || config.Semantic.VectorBackend != "zvec" {
		t.Fatalf("generated semantic selection = %+v", config.Semantic)
	}
	bundleRoot := filepath.Join(config.Paths.Models, search.SemanticBundleBGEProfileID, runtime.GOOS+"-"+runtime.GOARCH)
	writeRealSemanticBundle(t, bundleRoot, runtimeSource, modelSource, tokenizerSource, zvecSource)

	// macOS has a short Unix-domain socket path limit; keep this test endpoint
	// independent from the long testing.TempDir path.
	socketPath := filepath.Join(os.TempDir(), "restoreweave-semantic-test.sock")
	_ = os.Remove(socketPath)
	defer os.Remove(socketPath)
	daemon, daemonDone, daemonLog := startDaemonProcessWithEnv(t, env, daemonBin, socketPath, "--config", configPath)
	defer stopDaemonProcess(t, daemon, daemonDone, daemonLog)
	waitForCLI(t, rwBin, socketPath, daemonDone, daemonLog, "status")

	initialCaps := runRWProcess(t, rwBin, socketPath, "capability", "list")
	assertSemanticCapability(t, initialCaps, command.CapabilityUnavailable)
	assertModelBundleCapability(t, initialCaps, command.CapabilityAvailable)

	planned := runRWProcess(t, rwBin, socketPath, "ingest", source)
	var ingest command.PlanIngestData
	decodeProcessResult(t, planned, &ingest)
	applied := runRWProcess(t, rwBin, socketPath, "plan", "apply", ingest.PlanID, "--workspace", ingest.WorkspaceID, "--digest", ingest.PlanDigest)
	var appliedData command.PlanApplyData
	decodeProcessResult(t, applied, &appliedData)
	resolved := runRWProcess(t, rwBin, socketPath, "namespace", "resolve", "semantic.txt", "--workspace", appliedData.WorkspaceID, "--root", appliedData.RootID)
	var resolvedData command.NamespaceResolveData
	decodeProcessResult(t, resolved, &resolvedData)
	created := runRWProcess(t, rwBin, socketPath, "description", "create", resolvedData.PathRef, "--workspace", appliedData.WorkspaceID, "--kind", "USER", "--language", "zh", "--title", "语义恢复", "--body", "洪水城市档案恢复说明", "--accepted")
	var description command.DescriptionCreateData
	decodeProcessResult(t, created, &description)

	semanticQuery := runRWProcess(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--dimension", search.DimensionSemantic)
	var semanticData command.SearchQueryData
	decodeProcessResult(t, semanticQuery, &semanticData)
	if semanticData.Dimension != search.DimensionSemantic || semanticData.Provider != search.ProviderSemanticONNX || len(semanticData.Hits) != 1 {
		t.Fatalf("real daemon semantic query = %+v", semanticData)
	}
	if semanticData.Hits[0].SubjectRef != resolvedData.PathRef || len(semanticData.Hits[0].Segments) != 1 || semanticData.Hits[0].Segments[0].DescriptionDocumentID != description.Document.ID {
		t.Fatalf("real daemon semantic provenance = %+v", semanticData.Hits[0])
	}
	assertSemanticCapability(t, runRWProcess(t, rwBin, socketPath, "capability", "list"), command.CapabilityAvailable)

	fused := runRWProcess(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--fuse", search.DimensionLexical, "--fuse", search.DimensionSemantic)
	var fusedData command.SearchQueryData
	decodeProcessResult(t, fused, &fusedData)
	if len(fusedData.Hits) != 1 || len(fusedData.Components) != 2 {
		t.Fatalf("real daemon fused semantic query = %+v", fusedData)
	}
	for _, component := range fusedData.Components {
		if component.Dimension == search.DimensionSemantic && component.Status != "SUCCEEDED" {
			t.Fatalf("real daemon semantic component = %+v", component)
		}
	}

	semanticGenerationID := semanticData.GenerationID
	if err := daemon.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := <-daemonDone; err != nil {
		t.Fatalf("daemon stop: %v\n%s", err, daemonLog.String())
	}
	store, err := sqlite.Open(context.Background(), config.Paths.Catalog, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.GetIndexGeneration(context.Background(), semanticGenerationID)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := os.RemoveAll(generation.DBPath); err != nil {
		_ = store.Close()
		t.Fatalf("remove semantic generation: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	daemon, daemonDone, daemonLog = startDaemonProcessWithEnv(t, env, daemonBin, socketPath, "--config", configPath)
	defer stopDaemonProcess(t, daemon, daemonDone, daemonLog)
	waitForCLI(t, rwBin, socketPath, daemonDone, daemonLog, "status")
	degraded := runRWProcessAllowStatus(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--dimension", search.DimensionSemantic)
	if degraded.Status != command.StatusDegraded {
		t.Fatalf("deleted semantic generation status = %s, reasons=%+v", degraded.Status, degraded.Reasons)
	}
	assertSemanticCapability(t, runRWProcess(t, rwBin, socketPath, "capability", "list"), command.CapabilityUnavailable)
	createdAgain := runRWProcess(t, rwBin, socketPath, "description", "create", resolvedData.PathRef, "--workspace", appliedData.WorkspaceID, "--kind", "USER", "--language", "zh", "--title", "语义恢复 2", "--body", "洪水城市档案恢复说明", "--accepted")
	if createdAgain.Status != command.StatusSucceeded {
		t.Fatalf("semantic rebuild description status = %s, reasons=%+v", createdAgain.Status, createdAgain.Reasons)
	}
	rebuilt := runRWProcess(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--dimension", search.DimensionSemantic)
	var rebuiltData command.SearchQueryData
	decodeProcessResult(t, rebuilt, &rebuiltData)
	if rebuiltData.GenerationID == semanticGenerationID || len(rebuiltData.Hits) == 0 {
		t.Fatalf("rebuilt semantic generation = %+v", rebuiltData)
	}
	for _, hit := range rebuiltData.Hits {
		if hit.SubjectRef != resolvedData.PathRef {
			t.Fatalf("rebuilt semantic hit escaped subject = %+v", hit)
		}
	}
	assertSemanticCapability(t, runRWProcess(t, rwBin, socketPath, "capability", "list"), command.CapabilityAvailable)
}

func assertSemanticCapability(t *testing.T, result command.Result, want string) {
	t.Helper()
	if result.Status != command.StatusSucceeded {
		t.Fatalf("capability.list status = %s, reasons=%+v", result.Status, result.Reasons)
	}
	var data command.CapabilityListData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	for _, capability := range data.Capabilities {
		if capability.Kind == search.CapabilityKindDimension && capability.ID == search.DimensionSemantic {
			if capability.State != want {
				t.Fatalf("semantic capability = %+v, want %s", capability, want)
			}
			if want == command.CapabilityAvailable && capability.Source != search.ProviderSemanticONNX {
				t.Fatalf("semantic capability source = %q, want %q", capability.Source, search.ProviderSemanticONNX)
			}
			return
		}
	}
	t.Fatalf("semantic capability was not declared")
}

func assertModelBundleCapability(t *testing.T, result command.Result, want string) {
	t.Helper()
	if result.Status != command.StatusSucceeded {
		t.Fatalf("capability.list status = %s, reasons=%+v", result.Status, result.Reasons)
	}
	var data command.CapabilityListData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	for _, capability := range data.Capabilities {
		if capability.Kind == "model-bundle" && capability.ID == search.SemanticBundleBGEProfileID {
			if capability.State != want {
				t.Fatalf("model bundle capability = %+v, want %s", capability, want)
			}
			return
		}
	}
	t.Fatalf("model bundle capability was not declared")
}

func runRWProcessAllowStatus(t *testing.T, rwBin, socketPath string, args ...string) command.Result {
	t.Helper()
	fullArgs := append([]string{"--socket", socketPath, "--json"}, args...)
	output, processErr := exec.Command(rwBin, fullArgs...).CombinedOutput()
	var result command.Result
	if decodeErr := json.Unmarshal(output, &result); decodeErr != nil {
		t.Fatalf("decode rw %s result: %v (process error %v)\n%s", strings.Join(args, " "), decodeErr, processErr, output)
	}
	if processErr != nil && result.Status != command.StatusDegraded {
		t.Fatalf("rw %s: %v, status=%s, reasons=%+v", strings.Join(args, " "), processErr, result.Status, result.Reasons)
	}
	return result
}

func buildTestBinaryWithTags(t *testing.T, workspaceRoot, target, destination string, tags ...string) string {
	t.Helper()
	args := []string{"build"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, "-o", destination, target)
	cmd := exec.Command("go", args...)
	cmd.Dir = workspaceRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", target, err, output)
	}
	return destination
}

func writeRealSemanticBundle(t *testing.T, root, runtimeSource, modelSource, tokenizerSource, zvecSource string) {
	t.Helper()
	sourceRoot := t.TempDir()
	assets := map[string]string{
		"runtime": runtimeSource, "model": modelSource, "tokenizer": tokenizerSource, "zvec": zvecSource,
	}
	runtimePath := "onnxruntime/lib/libonnxruntime.so.1.29.0"
	zvecPath := "zvec/libzvec_c_api.so"
	if runtime.GOOS == "darwin" {
		runtimePath = "onnxruntime/lib/libonnxruntime.1.29.0.dylib"
		zvecPath = "zvec/libzvec_c_api.dylib"
	}
	paths := map[string]string{
		"runtime": runtimePath, "model": "model.onnx",
		"tokenizer": "tokenizer.json", "zvec": zvecPath,
	}
	for name, source := range assets {
		destination := filepath.Join(sourceRoot, filepath.FromSlash(paths[name]))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		payload, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(destination, payload, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeAsset := func(name, path string, payload []byte) search.SemanticBundleAsset {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(sourceRoot, filepath.FromSlash(path))), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sourceRoot, filepath.FromSlash(path)), payload, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return bundleAsset(t, sourceRoot, path)
	}
	asset := func(name string) search.SemanticBundleAsset { return bundleAsset(t, sourceRoot, paths[name]) }
	binding := writeAsset("onnx_binding", "onnx-binding", []byte("github.com/yalue/onnxruntime_go@v1.33.0\nb4e0f0b495a4ad1eb8a2f61c0286b6e670771525"))
	api := writeAsset("onnx_c_api", "onnx-c-api.h", []byte("#define ORT_API_VERSION 29\n"))
	profile := writeAsset("profile", "profile.json", []byte(`{"profile":"bge-small-zh-v1.5"}`))
	license := writeAsset("license", "LICENSE", []byte("MIT AND Apache-2.0\n"))
	notice := writeAsset("notice", "NOTICE", []byte("RestoreWeave supervised integration\n"))
	sbom := writeAsset("sbom", "sbom.json", []byte(`{"profile":"bge-small-zh-v1.5"}`))
	zvecGo := writeAsset("zvec_go", "zvec-go.txt", []byte("zvec-go 0.6.0\n"))
	descriptor := search.SemanticBundleDescriptor{
		Schema: search.SemanticBundleSchemaV1, ProfileID: search.SemanticBundleBGEProfileID,
		PlatformOS: runtime.GOOS, PlatformArch: runtime.GOARCH,
		ONNXRuntimeVersion: "1.29.0", ONNXRuntimeBuild: "official-cpu-opt-in", ONNXRuntimeCAPI: search.SemanticBundleONNXRuntimeCAPI,
		ONNXGoBindingCommit: "b4e0f0b495a4ad1eb8a2f61c0286b6e670771525", ONNXGoBindingDigest: binding.SHA256, ONNXGoBindingCAPI: search.SemanticBundleONNXRuntimeCAPI,
		ModelID: "BAAI/bge-small-zh-v1.5", ModelRevision: "opt-in-pinned-asset", ModelExport: "onnx-last-hidden-state", ONNXOpset: 17, ModelLicenseID: "BAAI/bge-small-zh-v1.5:MIT",
		TokenizerVersion: "huggingface-tokenizers", TokenizerRevision: "opt-in-pinned-asset", ZvecVersion: "0.6.0", ZvecBuild: runtime.GOOS + "-" + runtime.GOARCH + "-opt-in", ZvecGoVersion: "0.6.0", ZvecGoCommit: "zvec-go-0.6.0", LicenseExpression: search.SemanticBundleLicenseExpression,
		PreprocessingDigest: "sha256:" + strings.Repeat("b", 64), QueryPrefix: search.SemanticBundleBGEQueryPrefix, DocumentPrefix: search.SemanticBundleBGEDocumentPrefix, MaxTokens: search.SemanticBundleBGEMaxTokens,
		Pooling: search.SemanticBundleBGEPooling, Normalization: search.SemanticBundleBGENormalization, ElementType: search.SemanticBundleBGEElementType, Dimension: search.SemanticBundleBGEDimension,
		VectorSchema: search.SemanticBundleBGEVectorSchema, SemanticSpace: search.SemanticBundleBGESemanticSpace, Distance: search.SemanticBundleBGEDistance, IndexConfig: search.ZvecIndexConfigV1, QueryConfig: search.ZvecQueryConfigV1,
		Runtime: asset("runtime"), ONNXBinding: binding, ONNXCAPI: api, Model: asset("model"), Tokenizer: asset("tokenizer"), Profile: profile, Zvec: asset("zvec"), ZvecGo: zvecGo, License: license, Notice: notice, SBOM: sbom,
	}
	sources := make(map[string]string, len(descriptorAssets(descriptor)))
	for name, path := range descriptorAssets(descriptor) {
		sources[name] = filepath.Join(sourceRoot, filepath.FromSlash(path))
	}
	if _, err := search.PackageSemanticBundle(root, descriptor, sources); err != nil {
		t.Fatalf("package generated semantic bundle: %v", err)
	}
	if err := os.RemoveAll(sourceRoot); err != nil {
		t.Fatalf("remove source staging: %v", err)
	}
}

func descriptorAssets(descriptor search.SemanticBundleDescriptor) map[string]string {
	return map[string]string{
		"runtime": descriptor.Runtime.Path, "onnx_binding": descriptor.ONNXBinding.Path,
		"onnx_c_api": descriptor.ONNXCAPI.Path, "model": descriptor.Model.Path,
		"tokenizer": descriptor.Tokenizer.Path, "profile": descriptor.Profile.Path,
		"zvec": descriptor.Zvec.Path, "zvec_go": descriptor.ZvecGo.Path,
		"license": descriptor.License.Path, "notice": descriptor.Notice.Path,
		"sbom": descriptor.SBOM.Path,
	}
}

func bundleAsset(t *testing.T, root, path string) search.SemanticBundleAsset {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read bundle asset %s: %v", path, err)
	}
	digest := sha256.Sum256(payload)
	return search.SemanticBundleAsset{Path: path, SHA256: hex.EncodeToString(digest[:]), Size: uint64(len(payload))}
}
