package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestSemanticBundleInstallRequiresInjectedCallback(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock")
	result := dispatcher.Handle(context.Background(), command.Envelope{
		Operation: command.OpSemanticBundleInstall, Input: []byte(`{}`),
	})
	if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeUnimplemented) {
		t.Fatalf("without installer = %s, reasons=%+v; want unavailable operation", result.Status, result.Reasons)
	}
	capabilities := capabilityData(t, dispatcher.Handle(context.Background(), command.Envelope{
		Operation: command.OpCapabilityList, Input: []byte(`{}`),
	}))
	if capability := findCapability(capabilities, "operation", command.OpSemanticBundleInstall); capability.State != command.CapabilityUnavailable {
		t.Fatalf("installer capability = %+v, want unavailable", capability)
	}
}

func TestSemanticBundleInstallStrictInputAndIndependentCapability(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	modelsRoot := filepath.Join(t.TempDir(), "models")
	var callbackCalls int
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock",
		WithExact(&exact.Service{Store: store, Repo: repo}),
		WithSemanticBundleInstaller(modelsRoot, func(_ context.Context, root string) (SemanticBundleInstallReceipt, error) {
			callbackCalls++
			if root != modelsRoot {
				return SemanticBundleInstallReceipt{}, errors.New("wrong models root")
			}
			return controlPlaneTestSemanticBundle(t, modelsRoot)
		}),
	)

	result := dispatcher.Handle(context.Background(), command.Envelope{
		Operation: command.OpSemanticBundleInstall, Input: []byte(`{}`),
	})
	if result.Status != command.StatusSucceeded {
		t.Fatalf("install = %s, reasons=%+v", result.Status, result.Reasons)
	}
	var data command.SemanticBundleInstallData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.ProfileID != search.SemanticBundleBGEProfileID || data.ProfileDigest == "" || data.Destination == "" || !data.Changed || !data.RestartRequired {
		t.Fatalf("install data = %+v", data)
	}
	if callbackCalls != 1 {
		t.Fatalf("callback calls = %d, want one", callbackCalls)
	}

	capabilities := capabilityData(t, dispatcher.Handle(context.Background(), command.Envelope{
		Operation: command.OpCapabilityList, Input: []byte(`{}`),
	}))
	bundle := findCapability(capabilities, "model-bundle", search.SemanticBundleBGEProfileID)
	if bundle.State != command.CapabilityAvailable || bundle.Version != data.ProfileDigest {
		t.Fatalf("bundle capability = %+v", bundle)
	}
	semantic := findCapability(capabilities, search.CapabilityKindDimension, search.DimensionSemantic)
	if semantic.State != command.CapabilityUnavailable {
		t.Fatalf("semantic dimension = %+v, want unavailable until restart/rebuild", semantic)
	}

	for _, input := range []string{`{"url":"https://example.invalid"}`, `{"archive_path":" /tmp/bundle.tar.gz"}`, `{"archive_path":"bundle.tar.gz"}`, `null`, `[]`} {
		bad := dispatcher.Handle(context.Background(), command.Envelope{
			Operation: command.OpSemanticBundleInstall, Input: []byte(input),
		})
		if bad.Status != command.StatusFailed || !hasReasonCode(bad, ReasonCodeInvalidInput) {
			t.Fatalf("input %s = %s, reasons=%+v; want invalid input", input, bad.Status, bad.Reasons)
		}
	}
	if callbackCalls != 1 {
		t.Fatalf("invalid inputs invoked callback %d times", callbackCalls)
	}
}

func TestSemanticBundleInstallArchiveInputUsesOfflineInstaller(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	modelsRoot := filepath.Join(t.TempDir(), "models")
	const archivePath = "/var/lib/restoreweave/bge.tar.gz"
	var onlineCalls, offlineCalls int
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock",
		WithSemanticBundleInstaller(modelsRoot, func(context.Context, string) (SemanticBundleInstallReceipt, error) {
			onlineCalls++
			return SemanticBundleInstallReceipt{}, errors.New("online installer must not be selected")
		}),
		WithSemanticBundleArchiveInstaller(modelsRoot, func(_ context.Context, root, path string) (SemanticBundleInstallReceipt, error) {
			offlineCalls++
			if root != modelsRoot || path != archivePath {
				return SemanticBundleInstallReceipt{}, errors.New("offline installer received wrong paths")
			}
			return controlPlaneTestSemanticBundle(t, modelsRoot)
		}),
	)
	result := dispatcher.Handle(context.Background(), command.Envelope{
		Operation: command.OpSemanticBundleInstall,
		Input:     []byte(`{"archive_path":"/var/lib/restoreweave/bge.tar.gz"}`),
	})
	if result.Status != command.StatusSucceeded {
		t.Fatalf("offline install = %s, reasons=%+v", result.Status, result.Reasons)
	}
	if onlineCalls != 0 || offlineCalls != 1 {
		t.Fatalf("installer calls online=%d offline=%d", onlineCalls, offlineCalls)
	}
}

func TestSemanticBundleInstallFailureIsNotPartialSuccess(t *testing.T) {
	for _, test := range []struct {
		name      string
		installer SemanticBundleInstaller
	}{
		{name: "callback error", installer: func(context.Context, string) (SemanticBundleInstallReceipt, error) {
			return SemanticBundleInstallReceipt{}, errors.New("download failed")
		}},
		{name: "invalid receipt", installer: func(context.Context, string) (SemanticBundleInstallReceipt, error) {
			return SemanticBundleInstallReceipt{Destination: "/tmp/but-no-admission"}, nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
			repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
			if err != nil {
				t.Fatal(err)
			}
			dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock",
				WithExact(&exact.Service{Store: store, Repo: repo}),
				WithSemanticBundleCapability(command.Capability{
					Kind: "model-bundle", ID: search.SemanticBundleBGEProfileID,
					State: command.CapabilityUnavailable, Version: "before", Source: "pinned",
				}),
				WithSemanticBundleInstaller(filepath.Join(t.TempDir(), "models"), test.installer),
			)
			before := capabilityData(t, dispatcher.Handle(context.Background(), command.Envelope{
				Operation: command.OpCapabilityList, Input: []byte(`{}`),
			}))
			result := dispatcher.Handle(context.Background(), command.Envelope{
				Operation: command.OpSemanticBundleInstall, Input: []byte(`{}`),
			})
			if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeUnavailable) {
				t.Fatalf("failed install = %s, reasons=%+v; want FAILED/unavailable", result.Status, result.Reasons)
			}
			after := capabilityData(t, dispatcher.Handle(context.Background(), command.Envelope{
				Operation: command.OpCapabilityList, Input: []byte(`{}`),
			}))
			for _, id := range []string{search.SemanticBundleBGEProfileID, search.DimensionLexical, search.DimensionSemantic} {
				beforeCapability := findCapabilityByID(before, id)
				afterCapability := findCapabilityByID(after, id)
				if beforeCapability.State != afterCapability.State || beforeCapability.Version != afterCapability.Version {
					t.Fatalf("capability %q changed: before=%+v after=%+v", id, beforeCapability, afterCapability)
				}
			}
		})
	}
}

func capabilityData(t *testing.T, result command.Result) []command.Capability {
	t.Helper()
	if result.Status != command.StatusSucceeded {
		t.Fatalf("capability.list = %s, reasons=%+v", result.Status, result.Reasons)
	}
	var data command.CapabilityListData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	return data.Capabilities
}

func findCapability(capabilities []command.Capability, kind, id string) command.Capability {
	for _, capability := range capabilities {
		if capability.Kind == kind && capability.ID == id {
			return capability
		}
	}
	return command.Capability{Kind: kind, ID: id, State: command.CapabilityUnavailable}
}

func findCapabilityByID(capabilities []command.Capability, id string) command.Capability {
	for _, capability := range capabilities {
		if capability.ID == id {
			return capability
		}
	}
	return command.Capability{ID: id, State: command.CapabilityUnavailable}
}

func controlPlaneTestSemanticBundle(t *testing.T, modelsRoot string) (SemanticBundleInstallReceipt, error) {
	t.Helper()
	sourceRoot := t.TempDir()
	files := map[string][]byte{
		"runtime.bin":  []byte("onnx-runtime-fixture"),
		"binding.txt":  []byte("onnx-go-binding-fixture"),
		"onnx-c-api.h": []byte("#define ORT_API_VERSION 29\n"),
		"model.onnx":   []byte("bge-model-fixture"),
		"tokenizer":    []byte("bge-tokenizer-fixture"),
		"profile.json": []byte("bge-profile-fixture"),
		"zvec.dylib":   []byte("zvec-native-fixture"),
		"zvec-go.txt":  []byte("zvec-go-fixture"),
		"LICENSE":      []byte("MIT\nApache-2.0\n"),
		"NOTICE":       []byte("RestoreWeave semantic bundle notices\n"),
		"sbom.json":    []byte(`{"name":"restoreweave-semantic-bundle"}`),
	}
	assets := make(map[string]search.SemanticBundleAsset, len(files))
	sources := make(map[string]string, len(files))
	for name, data := range files {
		path := filepath.Join(sourceRoot, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return SemanticBundleInstallReceipt{}, err
		}
		digest := sha256.Sum256(data)
		assets[name] = search.SemanticBundleAsset{Path: name, SHA256: hex.EncodeToString(digest[:]), Size: uint64(len(data))}
	}
	for _, entry := range []struct{ name, file string }{
		{"runtime", "runtime.bin"}, {"onnx_binding", "binding.txt"}, {"onnx_c_api", "onnx-c-api.h"},
		{"model", "model.onnx"}, {"tokenizer", "tokenizer"}, {"profile", "profile.json"}, {"zvec", "zvec.dylib"},
		{"zvec_go", "zvec-go.txt"}, {"license", "LICENSE"}, {"notice", "NOTICE"}, {"sbom", "sbom.json"},
	} {
		sources[entry.name] = filepath.Join(sourceRoot, entry.file)
	}
	descriptor := search.SemanticBundleDescriptor{
		Schema: search.SemanticBundleSchemaV1, ProfileID: search.SemanticBundleBGEProfileID,
		PlatformOS: runtime.GOOS, PlatformArch: runtime.GOARCH,
		ONNXRuntimeVersion: "1.29.0", ONNXRuntimeBuild: "test-runtime", ONNXRuntimeCAPI: search.SemanticBundleONNXRuntimeCAPI,
		ONNXGoBindingCommit: "test-binding", ONNXGoBindingCAPI: search.SemanticBundleONNXRuntimeCAPI,
		ModelID: "BAAI/bge-small-zh-v1.5", ModelRevision: "test-model", ModelExport: "test-export", ONNXOpset: 11,
		ModelLicenseID: "BAAI/bge-small-zh-v1.5:MIT", TokenizerVersion: "test-tokenizer", TokenizerRevision: "test-tokenizer-revision",
		ZvecVersion: "0.6.0", ZvecBuild: "test-zvec", ZvecGoVersion: "0.6.0", ZvecGoCommit: "test-zvec-go",
		LicenseExpression: search.SemanticBundleLicenseExpression, PreprocessingDigest: "sha256:" + strings.Repeat("b", 64),
		QueryPrefix: search.SemanticBundleBGEQueryPrefix, DocumentPrefix: search.SemanticBundleBGEDocumentPrefix,
		MaxTokens: search.SemanticBundleBGEMaxTokens, Pooling: search.SemanticBundleBGEPooling, Normalization: search.SemanticBundleBGENormalization,
		ElementType: search.SemanticBundleBGEElementType, Dimension: search.SemanticBundleBGEDimension, VectorSchema: search.SemanticBundleBGEVectorSchema,
		SemanticSpace: search.SemanticBundleBGESemanticSpace, Distance: search.SemanticBundleBGEDistance, IndexConfig: "hnsw:m=16", QueryConfig: "ef=64",
		Runtime: assets["runtime.bin"], ONNXBinding: assets["binding.txt"], ONNXCAPI: assets["onnx-c-api.h"], Model: assets["model.onnx"],
		Tokenizer: assets["tokenizer"], Profile: assets["profile.json"], Zvec: assets["zvec.dylib"], ZvecGo: assets["zvec-go.txt"],
		License: assets["LICENSE"], Notice: assets["NOTICE"], SBOM: assets["sbom.json"],
	}
	descriptor.ONNXGoBindingDigest = descriptor.ONNXBinding.SHA256
	destination := filepath.Join(modelsRoot, search.SemanticBundleBGEProfileID, runtime.GOOS+"-"+runtime.GOARCH)
	admission, err := search.PackageSemanticBundle(destination, descriptor, sources)
	if err != nil {
		return SemanticBundleInstallReceipt{}, err
	}
	return SemanticBundleInstallReceipt{Admission: admission, Destination: destination, Changed: true}, nil
}
