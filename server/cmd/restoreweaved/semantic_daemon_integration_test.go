//go:build cgo && purego && arm64 && supervised_integration && (darwin || linux)

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

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
	// Build a packaged bundle in an independent offline custody directory,
	// then install it into the fresh configured models root.  The daemon must
	// consume only the installed copy after the source is isolated; this keeps
	// the E2E honest about the offline installation boundary and avoids any
	// first-start network/download path.
	bundleSource := filepath.Join(root, "offline-semantic-bundle")
	writeRealSemanticBundle(t, bundleSource, runtimeSource, modelSource, tokenizerSource, zvecSource)
	installed, err := search.InstallDefaultSemanticBundleFromDirectory(context.Background(), config.Paths.Models, bundleSource)
	if err != nil {
		t.Fatalf("install packaged semantic bundle offline: %v", err)
	}
	bundleRoot := filepath.Join(config.Paths.Models, search.SemanticBundleBGEProfileID, runtime.GOOS+"-"+runtime.GOARCH)
	if installed.Descriptor.ProfileID != search.SemanticBundleBGEProfileID || installed.Descriptor.PlatformOS != runtime.GOOS || installed.Descriptor.PlatformArch != runtime.GOARCH || installed.ProfileDigest == "" {
		t.Fatalf("offline installed semantic bundle = %+v", installed.Descriptor)
	}
	if err := os.RemoveAll(bundleSource); err != nil {
		t.Fatalf("isolate offline semantic bundle source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundleRoot, search.SemanticBundleManifestName)); err != nil {
		t.Fatalf("installed semantic bundle manifest after source isolation: %v", err)
	}

	// macOS has a short Unix-domain socket path limit; keep this test endpoint
	// independent from the long testing.TempDir path.
	socketPath := filepath.Join(os.TempDir(), "restoreweave-semantic-test-"+strconv.Itoa(os.Getpid())+".sock")
	_ = os.Remove(socketPath)
	defer os.Remove(socketPath)
	daemon, daemonDone, daemonLog := startDaemonProcessWithEnv(t, env, daemonBin, socketPath, "--config", configPath)
	defer stopDaemonProcess(t, daemon, daemonDone, daemonLog)
	waitForRealDaemonCLI(t, rwBin, socketPath, daemonDone, daemonLog, "status")

	initialCaps := runRealRWProcess(t, rwBin, socketPath, "capability", "list")
	assertSemanticCapability(t, initialCaps, command.CapabilityUnavailable)
	assertModelBundleCapability(t, initialCaps, command.CapabilityAvailable)

	planned := runRealRWProcess(t, rwBin, socketPath, "ingest", source)
	var ingest command.PlanIngestData
	decodeProcessResult(t, planned, &ingest)
	applied := runRealRWProcess(t, rwBin, socketPath, "plan", "apply", ingest.PlanID, "--workspace", ingest.WorkspaceID, "--digest", ingest.PlanDigest)
	var appliedData command.PlanApplyData
	decodeProcessResult(t, applied, &appliedData)
	resolved := runRealRWProcess(t, rwBin, socketPath, "namespace", "resolve", "semantic.txt", "--workspace", appliedData.WorkspaceID, "--root", appliedData.RootID)
	var resolvedData command.NamespaceResolveData
	decodeProcessResult(t, resolved, &resolvedData)
	created := runRealRWProcess(t, rwBin, socketPath, "description", "create", resolvedData.PathRef, "--workspace", appliedData.WorkspaceID, "--kind", "USER", "--language", "zh", "--title", "语义恢复", "--body", "洪水城市档案恢复说明", "--accepted")
	var description command.DescriptionCreateData
	decodeProcessResult(t, created, &description)

	semanticQuery := runRealRWProcess(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--dimension", search.DimensionSemantic)
	var semanticData command.SearchQueryData
	decodeProcessResult(t, semanticQuery, &semanticData)
	if semanticData.Dimension != search.DimensionSemantic || semanticData.Provider != search.ProviderSemanticONNX || len(semanticData.Hits) != 1 {
		t.Fatalf("real daemon semantic query = %+v", semanticData)
	}
	if semanticData.Hits[0].SubjectRef != resolvedData.Entry.SubjectRef || semanticData.Hits[0].EntryID != resolvedData.PathRef || len(semanticData.Hits[0].Segments) == 0 {
		t.Fatalf("real daemon semantic provenance = %+v", semanticData.Hits[0])
	}
	descriptionMatched := false
	for _, segment := range semanticData.Hits[0].Segments {
		if segment.SourceType == "DESCRIPTION" && segment.DescriptionDocumentID == description.Document.ID {
			descriptionMatched = true
			break
		}
	}
	if !descriptionMatched {
		t.Fatalf("real daemon semantic description provenance = %+v", semanticData.Hits[0])
	}
	assertSemanticCapability(t, runRealRWProcess(t, rwBin, socketPath, "capability", "list"), command.CapabilityAvailable)

	// The ordinary CLI path must use the default fused broker, retaining both
	// lexical and semantic components while applying a typed structured filter.
	defaultSearch := runRealRWProcess(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--filter", "entry_type=REGULAR_FILE")
	var defaultData command.SearchQueryData
	decodeProcessResult(t, defaultSearch, &defaultData)
	if defaultData.Provider != search.ProviderBrokerFuse || defaultData.Dimension != "" || len(defaultData.FusedDimensions) != 2 ||
		defaultData.FusedDimensions[0] != search.DimensionLexical || defaultData.FusedDimensions[1] != search.DimensionSemantic ||
		len(defaultData.Components) != 2 || defaultData.Components[0].Dimension != search.DimensionLexical ||
		defaultData.Components[0].Status != string(command.StatusSucceeded) || defaultData.Components[1].Dimension != search.DimensionSemantic ||
		defaultData.Components[1].Status != string(command.StatusSucceeded) || len(defaultData.Hits) != 1 ||
		defaultData.Hits[0].SubjectRef != resolvedData.Entry.SubjectRef {
		t.Fatalf("real daemon default broker query/filter = %+v", defaultData)
	}

	fused := runRealRWProcess(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--fuse", search.DimensionLexical, "--fuse", search.DimensionSemantic)
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
	// A clean daemon restart must reopen the persisted, profile-compatible
	// generation.  The bundle was already provisioned above and no install
	// operation is issued here: this exercises the offline reopen/query path
	// rather than a fresh generation build or a download.
	if err := daemon.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := <-daemonDone; err != nil {
		t.Fatalf("daemon stop: %v\n%s", err, daemonLog.String())
	}
	daemon, daemonDone, daemonLog = startDaemonProcessWithEnv(t, env, daemonBin, socketPath, "--config", configPath)
	waitForRealDaemonCLI(t, rwBin, socketPath, daemonDone, daemonLog, "status")
	// Startup warm-up must establish semantic readiness before the first
	// reopened query; query execution is not allowed to self-authorize it.
	assertSemanticCapability(t, runRealRWProcess(t, rwBin, socketPath, "capability", "list"), command.CapabilityAvailable)
	reopened := runRealRWProcess(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--dimension", search.DimensionSemantic)
	var reopenedData command.SearchQueryData
	decodeProcessResult(t, reopened, &reopenedData)
	if reopenedData.GenerationID != semanticGenerationID || reopenedData.Provider != search.ProviderSemanticONNX || len(reopenedData.Hits) != 1 {
		t.Fatalf("reopened semantic generation/query = %+v, want generation %s", reopenedData, semanticGenerationID)
	}
	if err := daemon.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := <-daemonDone; err != nil {
		t.Fatalf("daemon stop after reopen: %v\n%s", err, daemonLog.String())
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
	waitForRealDaemonCLI(t, rwBin, socketPath, daemonDone, daemonLog, "status")
	degraded := runRWProcessAllowStatus(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--dimension", search.DimensionSemantic)
	if degraded.Status != command.StatusDegraded {
		t.Fatalf("deleted semantic generation status = %s, reasons=%+v", degraded.Status, degraded.Reasons)
	}
	assertSemanticCapability(t, runRealRWProcess(t, rwBin, socketPath, "capability", "list"), command.CapabilityUnavailable)
	createdAgain := runRealRWProcess(t, rwBin, socketPath, "description", "create", resolvedData.PathRef, "--workspace", appliedData.WorkspaceID, "--kind", "USER", "--language", "zh", "--title", "语义恢复 2", "--body", "洪水城市档案恢复说明", "--accepted")
	if createdAgain.Status != command.StatusSucceeded {
		t.Fatalf("semantic rebuild description status = %s, reasons=%+v", createdAgain.Status, createdAgain.Reasons)
	}
	rebuilt := runRealRWProcess(t, rwBin, socketPath, "search", "洪水城市", "--workspace", appliedData.WorkspaceID, "--dimension", search.DimensionSemantic)
	var rebuiltData command.SearchQueryData
	decodeProcessResult(t, rebuilt, &rebuiltData)
	if rebuiltData.GenerationID == semanticGenerationID || len(rebuiltData.Hits) == 0 {
		t.Fatalf("rebuilt semantic generation = %+v", rebuiltData)
	}
	for _, hit := range rebuiltData.Hits {
		if hit.SubjectRef != resolvedData.Entry.SubjectRef || hit.EntryID != resolvedData.PathRef {
			t.Fatalf("rebuilt semantic hit escaped subject = %+v", hit)
		}
	}
	assertSemanticCapability(t, runRealRWProcess(t, rwBin, socketPath, "capability", "list"), command.CapabilityAvailable)
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

func runRealRWProcess(t *testing.T, rwBin, socketPath string, args ...string) command.Result {
	t.Helper()
	fullArgs := append([]string{"--socket", socketPath, "--timeout", "90s", "--json"}, args...)
	output, err := exec.Command(rwBin, fullArgs...).CombinedOutput()
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

// waitForRealDaemonCLI allows the supervised ONNX worker's native runtime and
// model admission to finish before probing the daemon socket. The ordinary
// daemon helper intentionally has a short timeout for lightweight tests; a
// real BGE startup can take longer while loading its pinned model.
func waitForRealDaemonCLI(t *testing.T, rwBin, socketPath string, daemonDone <-chan error, daemonLog *bytes.Buffer, args ...string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-daemonDone:
			t.Fatalf("restoreweaved exited before real semantic readiness: %v\n%s", err, daemonLog.String())
		default:
		}
		commandArgs := append([]string{"--socket", socketPath, "--json"}, args...)
		if err := exec.Command(rwBin, commandArgs...).Run(); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("restoreweaved real semantic readiness timed out\n%s", daemonLog.String())
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
	auxiliaryRoot := filepath.Dir(runtimeSource)
	assets := map[string]string{
		"runtime": runtimeSource, "model": modelSource, "tokenizer": tokenizerSource, "zvec": zvecSource,
		"onnx_binding": filepath.Join(auxiliaryRoot, "onnx-binding.txt"),
		"onnx_c_api":   filepath.Join(auxiliaryRoot, "onnx-c-api.h"),
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
		"onnx_binding": "onnx-binding.txt", "onnx_c_api": "onnx-c-api.h",
	}
	targetPaths := map[string]string{
		"runtime": "runtime.bin", "model": "model.onnx",
		"tokenizer": "tokenizer.json", "zvec": "zvec.dylib",
		"onnx_binding": "onnx-binding.txt", "onnx_c_api": "onnx-c-api.h",
	}
	for name, source := range assets {
		if _, err := os.Stat(source); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				t.Skipf("required real semantic asset %s is unavailable: %v", name, err)
			}
			t.Fatalf("required real semantic asset %s: %v", name, err)
		}
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
	asset := func(name string) search.SemanticBundleAsset {
		result := bundleAsset(t, sourceRoot, paths[name])
		result.Path = targetPaths[name]
		return result
	}
	binding := asset("onnx_binding")
	api := asset("onnx_c_api")
	// Keep this fixture's described bundle byte-for-byte compatible with the
	// fixed installer profile.  The real daemon test is an admission test, so a
	// plausible self-consistent descriptor is not enough: it must describe the
	// same converter, immutable zvec-go module/version/commit, and receipts as
	// the offline installer.
	profile := writeAsset("profile", "profile.json", []byte(`{"base_model":"BAAI/bge-small-zh-v1.5@7999e1d3359715c523056ef9478215996d62a620","model_export":"onnx-single-file;converter=Xenova","model_id":"BAAI/bge-small-zh-v1.5","onnx_converter_source":"Xenova/bge-small-zh-v1.5@75c43b069aac4d136ba6bc1122f995fedcfd2781","preprocessing_digest":"sha256:02c794b19d805eff54b90c4eb7d7f75b17c1a3e5103730af2147408d57a7ed0e","profile":"bge-small-zh-v1.5"}`))
	license := writeAsset("license", "LICENSE", []byte("RestoreWeave semantic bundle license inventory\n\n"+
		"BAAI/bge-small-zh-v1.5 and its Xenova ONNX conversion: MIT (base model).\n"+
		"ONNX Runtime: MIT. github.com/microsoft/onnxruntime\n"+
		"onnxruntime_go: MIT. github.com/yalue/onnxruntime_go\n"+
		"zvec-go and native zvec libraries: Apache-2.0. github.com/zvec-ai/zvec-go\n"))
	notice := writeAsset("notice", "NOTICE", []byte("This bundle uses the MIT BAAI/bge-small-zh-v1.5@7999e1d3359715c523056ef9478215996d62a620 model via the pinned Xenova/bge-small-zh-v1.5@75c43b069aac4d136ba6bc1122f995fedcfd2781 ONNX conversion; it is not a BAAI-published ONNX artifact.\n"+
		"Runtime: ONNX Runtime 1.29.0. Vector index: zvec 0.6.0. See LICENSE and SBOM for provenance.\n"))
	zvecGoPayload := []byte("zvec-go 0.6.0\nmodule github.com/zvec-ai/zvec-go version v0.6.1-0.20260721023313-9199195b29da\n9199195b29dac4bf369bb16954464ddf2d73e932\n")
	zvecGo := writeAsset("zvec_go", "zvec-go.txt", zvecGoPayload)
	runtimeAsset := asset("runtime")
	zvecAsset := asset("zvec")
	archiveFacts, ok := realSemanticArchiveFacts(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Fatalf("no fixed archive facts for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	sbomPayload, err := json.Marshal(map[string]any{
		"schema": "restoreweave.semantic-bundle.sbom.v1", "profile": "bge-small-zh-v1.5",
		"base_model":            "BAAI/bge-small-zh-v1.5@7999e1d3359715c523056ef9478215996d62a620",
		"onnx_converter_source": "Xenova/bge-small-zh-v1.5@75c43b069aac4d136ba6bc1122f995fedcfd2781",
		"assets": map[string]any{
			"onnxruntime_archive": map[string]any{"url": archiveFacts.ortURL, "sha256": archiveFacts.ortSHA},
			"zvec_archive":        map[string]any{"url": archiveFacts.zvecURL, "sha256": archiveFacts.zvecSHA},
			"runtime":             map[string]any{"sha256": runtimeAsset.SHA256, "size": runtimeAsset.Size},
			"zvec":                map[string]any{"sha256": zvecAsset.SHA256, "size": zvecAsset.Size},
			"zvec_go":             map[string]any{"path": "zvec-go.txt", "sha256": zvecGo.SHA256, "size": zvecGo.Size},
		},
		"dependencies": map[string]any{"zvec_go": map[string]any{
			"module": "github.com/zvec-ai/zvec-go", "version": "v0.6.1-0.20260721023313-9199195b29da",
			"commit": "9199195b29dac4bf369bb16954464ddf2d73e932", "license": "Apache-2.0",
			"asset": map[string]any{"path": "zvec-go.txt", "sha256": zvecGo.SHA256, "size": zvecGo.Size},
		}},
	})
	if err != nil {
		t.Fatalf("encode SBOM: %v", err)
	}
	sbom := writeAsset("sbom", "sbom.json", sbomPayload)
	runtimeBuild := "onnxruntime-cpu-" + runtime.GOOS + "-" + runtime.GOARCH + "-1.29.0"
	zvecBuild := "zvec-cpu-" + runtime.GOOS + "-" + runtime.GOARCH + "-0.6.0"
	descriptor := search.SemanticBundleDescriptor{
		Schema: search.SemanticBundleSchemaV1, ProfileID: search.SemanticBundleBGEProfileID,
		PlatformOS: runtime.GOOS, PlatformArch: runtime.GOARCH,
		ONNXRuntimeVersion: "1.29.0", ONNXRuntimeBuild: runtimeBuild, ONNXRuntimeCAPI: search.SemanticBundleONNXRuntimeCAPI,
		ONNXGoBindingCommit: "b4e0f0b495a4ad1eb8a2f61c0286b6e670771525", ONNXGoBindingDigest: binding.SHA256, ONNXGoBindingCAPI: search.SemanticBundleONNXRuntimeCAPI,
		ModelID: "BAAI/bge-small-zh-v1.5", ModelRevision: "75c43b069aac4d136ba6bc1122f995fedcfd2781", ModelExport: "onnx-single-file;converter=Xenova", ONNXOpset: 11, ModelLicenseID: "BAAI/bge-small-zh-v1.5:MIT",
		TokenizerVersion: "huggingface-tokenizers", TokenizerRevision: "75c43b069aac4d136ba6bc1122f995fedcfd2781", ZvecVersion: "0.6.0", ZvecBuild: zvecBuild, ZvecGoVersion: "0.6.0", ZvecGoCommit: "9199195b29dac4bf369bb16954464ddf2d73e932", LicenseExpression: search.SemanticBundleLicenseExpression,
		PreprocessingDigest: "sha256:02c794b19d805eff54b90c4eb7d7f75b17c1a3e5103730af2147408d57a7ed0e", QueryPrefix: search.SemanticBundleBGEQueryPrefix, DocumentPrefix: search.SemanticBundleBGEDocumentPrefix, MaxTokens: search.SemanticBundleBGEMaxTokens,
		Pooling: search.SemanticBundleBGEPooling, Normalization: search.SemanticBundleBGENormalization, ElementType: search.SemanticBundleBGEElementType, Dimension: search.SemanticBundleBGEDimension,
		VectorSchema: search.SemanticBundleBGEVectorSchema, SemanticSpace: search.SemanticBundleBGESemanticSpace, Distance: search.SemanticBundleBGEDistance, IndexConfig: search.ZvecIndexConfigV1, QueryConfig: search.ZvecQueryConfigV1,
		Runtime: runtimeAsset, ONNXBinding: binding, ONNXCAPI: api, Model: asset("model"), Tokenizer: asset("tokenizer"), Profile: profile, Zvec: zvecAsset, ZvecGo: zvecGo, License: license, Notice: notice, SBOM: sbom,
	}
	sources := make(map[string]string, len(descriptorAssets(descriptor)))
	for name, path := range descriptorAssets(descriptor) {
		sourcePath := path
		if original, ok := paths[name]; ok {
			sourcePath = original
		}
		sources[name] = filepath.Join(sourceRoot, filepath.FromSlash(sourcePath))
	}
	if _, err := search.PackageSemanticBundle(root, descriptor, sources); err != nil {
		t.Fatalf("package generated semantic bundle: %v", err)
	}
	admission, err := search.LoadSemanticBundle(root)
	if err != nil {
		t.Fatalf("load generated semantic bundle: %v", err)
	}
	if err := search.ValidateDefaultSemanticBundleAdmission(admission); err != nil {
		t.Fatalf("generated semantic bundle is not the pinned default: %v", err)
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

type realSemanticArchiveURLs struct {
	ortURL, ortSHA   string
	zvecURL, zvecSHA string
}

func realSemanticArchiveFacts(goos, goarch string) (realSemanticArchiveURLs, bool) {
	const ortBase = "https://github.com/microsoft/onnxruntime/releases/download/v1.29.0/"
	const zvecBase = "https://github.com/zvec-ai/zvec-go/releases/download/v0.6.0/"
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return realSemanticArchiveURLs{
			ortURL: ortBase + "onnxruntime-osx-arm64-1.29.0.tgz", ortSHA: "d0706fc34f315d8c88639d0a8c81f2e09e815f282cabed3493c06a054352cf92",
			zvecURL: zvecBase + "zvec-libs-darwin-arm64.tar.gz", zvecSHA: "7ee1f84a2b044458f1d9864c54e80f320a1d2101829f7a744d30a43be25bd6a9",
		}, true
	case "linux/arm64":
		return realSemanticArchiveURLs{
			ortURL: ortBase + "onnxruntime-linux-aarch64-1.29.0.tgz", ortSHA: "e1799098ebc054b370f6176a450f158720f297818c613e5dc99b92e2ec82346f",
			zvecURL: zvecBase + "zvec-libs-linux-arm64.tar.gz", zvecSHA: "a3354e7eff8c8c43fcd04f00cd93829e178794256740752fcd9d47f0301225a3",
		}, true
	case "linux/amd64":
		return realSemanticArchiveURLs{
			ortURL: ortBase + "onnxruntime-linux-x64-1.29.0.tgz", ortSHA: "c3fddc4f139a045b0c4902c57410f0694f1c2fdf9b6939fbe38b1aeae7cd14ba",
			zvecURL: zvecBase + "zvec-libs-linux-x64.tar.gz", zvecSHA: "770009b0e79a2dc6d4b2278da7119d4e47493c8f52006f0289f87d3eee4078db",
		}, true
	default:
		return realSemanticArchiveURLs{}, false
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
