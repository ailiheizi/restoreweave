package search

import (
	"bytes"
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
)

func TestAdmitSemanticBundleRejectsUnsafeOrMismatchedBundles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string, d *SemanticBundleDescriptor)
	}{
		{
			name: "missing asset",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				if err := os.Remove(filepath.Join(root, d.Model.Path)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink asset",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				modelPath := filepath.Join(root, d.Model.Path)
				if err := os.Remove(modelPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "runtime.bin"), modelPath); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "path escape",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.Model.Path = "../outside-model.bin"
				d.Model.SHA256 = strings.Repeat("0", 64)
			},
		},
		{
			name: "wrong digest",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.Model.SHA256 = strings.Repeat("0", 64)
			},
		},
		{
			name: "wrong ONNX version",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.ONNXRuntimeVersion = "1.28.0"
			},
		},
		{
			name: "binding C API mismatch",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.ONNXGoBindingCAPI = 25
			},
		},
		{
			name: "binding asset digest mismatch",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.ONNXGoBindingDigest = strings.Repeat("9", 64)
			},
		},
		{
			name: "non-numeric ONNX patch",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.ONNXRuntimeVersion = "1.29.x"
			},
		},
		{
			name: "wrong zvec version",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.ZvecVersion = "0.5.9"
			},
		},
		{
			name: "wrong platform",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.PlatformArch = "not-the-host"
			},
		},
		{
			name: "wrong model",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.ModelID = "some-other-model"
			},
		},
		{
			name: "wrong schema",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.Schema = "restoreweave.semantic-bundle.v2"
			},
		},
		{
			name: "missing query prefix",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.QueryPrefix = ""
			},
		},
		{
			name: "oversized declaration",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.Model.Size = SemanticBundleMaxAssetBytes + 1
			},
		},
		{
			name: "wrong license expression",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.LicenseExpression = "MIT"
			},
		},
		{
			name: "wrong notice digest",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.Notice.SHA256 = strings.Repeat("f", 64)
			},
		},
		{
			name: "wrong SBOM digest",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.SBOM.SHA256 = strings.Repeat("e", 64)
			},
		},
		{
			name: "duplicate role path",
			mutate: func(t *testing.T, root string, d *SemanticBundleDescriptor) {
				d.Profile.Path = d.Runtime.Path
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, descriptor := testSemanticBundle(t)
			tt.mutate(t, root, &descriptor)
			if _, err := AdmitSemanticBundle(root, descriptor); err == nil {
				t.Fatalf("admission succeeded for %s", tt.name)
			}
		})
	}
}

func TestAdmitSemanticBundleAcceptsLocalRegularFilesAndIsDeterministic(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	first, err := AdmitSemanticBundle(root, descriptor)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	second, err := AdmitSemanticBundle(root, descriptor)
	if err != nil {
		t.Fatalf("second admit: %v", err)
	}
	if first.ProfileDigest == "" || first.ProfileDigest != second.ProfileDigest {
		t.Fatalf("profile digest is not deterministic: %q vs %q", first.ProfileDigest, second.ProfileDigest)
	}
	if len(first.AssetDigests) != 11 {
		t.Fatalf("asset digest count = %d, want 11", len(first.AssetDigests))
	}
	if _, ok := first.AssetDigests["profile"]; !ok {
		t.Fatal("profile asset digest missing")
	}
	manifest, err := first.EmbeddingGenerationManifest("sha256:" + strings.Repeat("c", 64))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("bound manifest: %v", err)
	}
	if manifest.ProviderDigest != first.ProfileDigest || manifest.ConfigDigest == "" || manifest.Dimension != 512 {
		t.Fatalf("manifest binding = %+v", manifest)
	}
	if _, err := first.EmbeddingGenerationManifest(""); err == nil {
		t.Fatal("empty config digest was admitted")
	}
	if err := first.VerifyPinnedProfile(first.ProfileDigest); err != nil {
		t.Fatalf("verify pinned profile: %v", err)
	}
	if err := first.VerifyPinnedProfile("sha256:" + strings.Repeat("e", 64)); err == nil {
		t.Fatal("wrong pinned profile digest was accepted")
	}
	first.AssetDigests["model"] = strings.Repeat("d", 64)
	if _, err := first.EmbeddingGenerationManifest("sha256:" + strings.Repeat("c", 64)); err == nil {
		t.Fatal("tampered admission digest was projected into a generation manifest")
	}
}

func TestAdmitSemanticBundleProfileDigestBindsOutputFacts(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	baseline, err := AdmitSemanticBundle(root, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*SemanticBundleDescriptor)
	}{
		{"model revision", func(d *SemanticBundleDescriptor) { d.ModelRevision += "-other" }},
		{"tokenizer revision", func(d *SemanticBundleDescriptor) { d.TokenizerRevision += "-other" }},
		{"index config", func(d *SemanticBundleDescriptor) { d.IndexConfig += ",ef=80" }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			mutated := descriptor
			tt.mutate(&mutated)
			admission, err := AdmitSemanticBundle(root, mutated)
			if err != nil {
				t.Fatalf("admit mutated descriptor: %v", err)
			}
			if admission.ProfileDigest == baseline.ProfileDigest {
				t.Fatalf("%s did not change profile digest", tt.name)
			}
		})
	}
}

func TestAdmitSemanticBundleRejectsWrongBGEOutputFacts(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*SemanticBundleDescriptor)
	}{
		{"query prefix", func(d *SemanticBundleDescriptor) { d.QueryPrefix += "other" }},
		{"document prefix", func(d *SemanticBundleDescriptor) { d.DocumentPrefix = "passage: " }},
		{"token limit", func(d *SemanticBundleDescriptor) { d.MaxTokens++ }},
		{"pooling", func(d *SemanticBundleDescriptor) { d.Pooling = "mean" }},
		{"normalization", func(d *SemanticBundleDescriptor) { d.Normalization = "none" }},
		{"element type", func(d *SemanticBundleDescriptor) { d.ElementType = "float16" }},
		{"dimension", func(d *SemanticBundleDescriptor) { d.Dimension++ }},
		{"vector schema", func(d *SemanticBundleDescriptor) { d.VectorSchema = "float32:384" }},
		{"semantic space", func(d *SemanticBundleDescriptor) { d.SemanticSpace = "other-space" }},
		{"distance", func(d *SemanticBundleDescriptor) { d.Distance = "euclidean" }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			root, descriptor := testSemanticBundle(t)
			tt.mutate(&descriptor)
			if _, err := AdmitSemanticBundle(root, descriptor); err == nil {
				t.Fatalf("wrong BGE output fact %s was admitted", tt.name)
			}
		})
	}
}

func TestBGEProfileUsesOfficialRetrievalSemantics(t *testing.T) {
	if SemanticBundleBGEQueryPrefix != "\u4e3a\u8fd9\u4e2a\u53e5\u5b50\u751f\u6210\u8868\u793a\u4ee5\u7528\u4e8e\u68c0\u7d22\u76f8\u5173\u6587\u7ae0\uff1a" {
		t.Fatalf("query prefix = %q", SemanticBundleBGEQueryPrefix)
	}
	if SemanticBundleBGEDocumentPrefix != "" || SemanticBundleBGEMaxTokens != 512 ||
		SemanticBundleBGEPooling != "cls" || SemanticBundleBGENormalization != "l2" ||
		SemanticBundleBGEDimension != 512 || SemanticBundleBGEVectorSchema != "float32:512" {
		t.Fatalf("unexpected BGE output profile: prefix=%q max_tokens=%d pooling=%q normalization=%q dimension=%d schema=%q",
			SemanticBundleBGEDocumentPrefix, SemanticBundleBGEMaxTokens, SemanticBundleBGEPooling,
			SemanticBundleBGENormalization, SemanticBundleBGEDimension, SemanticBundleBGEVectorSchema)
	}
}

func TestLoadSemanticBundleUsesStrictBoundedDescriptor(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	payload, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, SemanticBundleManifestName), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSemanticBundle(root)
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	direct, err := AdmitSemanticBundle(root, descriptor)
	if err != nil {
		t.Fatalf("direct admission: %v", err)
	}
	if loaded.ProfileDigest != direct.ProfileDigest {
		t.Fatalf("loaded digest = %q, direct = %q", loaded.ProfileDigest, direct.ProfileDigest)
	}

	var unknown map[string]any
	if err := json.Unmarshal(payload, &unknown); err != nil {
		t.Fatal(err)
	}
	unknown["unexpected"] = true
	mutated, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, SemanticBundleManifestName), mutated, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSemanticBundle(root); err == nil {
		t.Fatal("descriptor with unknown field was admitted")
	}
}

func TestAdmitSemanticBundleRejectsDeclaredSizeMismatch(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	descriptor.Model.Size++
	if _, err := AdmitSemanticBundle(root, descriptor); err == nil {
		t.Fatal("wrong declared size was admitted")
	}
}

func TestAdmitSemanticBundleRejectsRootSymlinkAndDirectoryAsset(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	alias := filepath.Join(t.TempDir(), "bundle-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := AdmitSemanticBundle(alias, descriptor); err == nil {
		t.Fatal("root symlink was admitted")
	}
	root, descriptor = testSemanticBundle(t)
	if err := os.Remove(filepath.Join(root, descriptor.Model.Path)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, descriptor.Model.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := AdmitSemanticBundle(root, descriptor); err == nil {
		t.Fatal("directory asset was admitted")
	}
}

func TestAdmitSemanticBundleRejectsIntermediateSymlinkAndRelativeRoot(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	outside := t.TempDir()
	modelBytes, err := os.ReadFile(filepath.Join(root, descriptor.Model.Path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "model.onnx"), modelBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "nested")); err != nil {
		t.Fatal(err)
	}
	descriptor.Model.Path = "nested/model.onnx"
	if _, err := AdmitSemanticBundle(root, descriptor); err == nil {
		t.Fatal("intermediate symlink was admitted")
	}
	if _, err := AdmitSemanticBundle("relative/bundle", descriptor); err == nil {
		t.Fatal("relative bundle root was admitted")
	}
}

func TestReadSemanticBundleAssetRevalidatesBytesAndHonorsCancellation(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	admission, err := AdmitSemanticBundle(root, descriptor)
	if err != nil {
		t.Fatalf("admit bundle: %v", err)
	}
	want := []byte("bge-model-fixture")
	got, err := ReadSemanticBundleAsset(context.Background(), root, admission, "model")
	if err != nil {
		t.Fatalf("read model asset: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("asset bytes = %q, want %q", got, want)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadSemanticBundleAsset(ctx, root, admission, "model"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v, want context.Canceled", err)
	}
	if err := os.WriteFile(filepath.Join(root, descriptor.Model.Path), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSemanticBundleAsset(context.Background(), root, admission, "model"); err == nil {
		t.Fatal("tampered asset was read as admitted")
	}
	if _, err := ReadSemanticBundleAsset(context.Background(), root, admission, "unknown"); err == nil {
		t.Fatal("unknown asset was accepted")
	}
}

func TestPackageSemanticBundleSurvivesSourceRemoval(t *testing.T) {
	sourceRoot, descriptor := testSemanticBundle(t)
	sources := make(map[string]string, len(descriptor.assets()))
	for _, entry := range descriptor.assets() {
		sources[entry.Name] = filepath.Join(sourceRoot, filepath.FromSlash(entry.Asset.Path))
	}
	destination := filepath.Join(t.TempDir(), "installed", "semantic")
	packaged, err := PackageSemanticBundle(destination, descriptor, sources)
	if err != nil {
		t.Fatalf("package semantic bundle: %v", err)
	}
	admitted, err := LoadSemanticBundle(destination)
	if err != nil {
		t.Fatalf("load installed semantic bundle: %v", err)
	}
	if packaged.ProfileDigest != admitted.ProfileDigest {
		t.Fatalf("packaged profile digest = %q, loaded = %q", packaged.ProfileDigest, admitted.ProfileDigest)
	}
	if err := os.RemoveAll(sourceRoot); err != nil {
		t.Fatalf("remove source bundle: %v", err)
	}
	loaded, err := LoadSemanticBundle(destination)
	if err != nil {
		t.Fatalf("clean-install load after source removal: %v", err)
	}
	model, err := ReadSemanticBundleAsset(context.Background(), destination, loaded, "model")
	if err != nil {
		t.Fatalf("read installed model after source removal: %v", err)
	}
	if !bytes.Equal(model, []byte("bge-model-fixture")) {
		t.Fatalf("installed model = %q", model)
	}
}

func TestPackageSemanticBundleRejectsUntrustedSourcesAndDestination(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, sourceRoot, destination string, descriptor *SemanticBundleDescriptor, sources map[string]string)
	}{
		{
			name: "source symlink",
			mutate: func(t *testing.T, sourceRoot, _ string, descriptor *SemanticBundleDescriptor, sources map[string]string) {
				model := sources["model"]
				if err := os.Remove(model); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "model.onnx")
				if err := os.WriteFile(outside, []byte("bge-model-fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, model); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "intermediate source symlink",
			mutate: func(t *testing.T, sourceRoot, _ string, _ *SemanticBundleDescriptor, sources map[string]string) {
				model := sources["model"]
				payload, err := os.ReadFile(model)
				if err != nil {
					t.Fatal(err)
				}
				outside := t.TempDir()
				if err := os.WriteFile(filepath.Join(outside, "model.onnx"), payload, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(model); err != nil {
					t.Fatal(err)
				}
				nested := filepath.Join(sourceRoot, "nested")
				if err := os.Symlink(outside, nested); err != nil {
					t.Fatal(err)
				}
				sources["model"] = filepath.Join(nested, "model.onnx")
			},
		},
		{
			name: "source digest mismatch",
			mutate: func(t *testing.T, _ string, _ string, _ *SemanticBundleDescriptor, sources map[string]string) {
				if err := os.WriteFile(sources["model"], []byte("tampered"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "destination exists",
			mutate: func(t *testing.T, _ string, destination string, _ *SemanticBundleDescriptor, _ map[string]string) {
				if err := os.MkdirAll(destination, 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sourceRoot, descriptor := testSemanticBundle(t)
			sources := make(map[string]string, len(descriptor.assets()))
			for _, entry := range descriptor.assets() {
				sources[entry.Name] = filepath.Join(sourceRoot, filepath.FromSlash(entry.Asset.Path))
			}
			destination := filepath.Join(t.TempDir(), "installed", "semantic")
			test.mutate(t, sourceRoot, destination, &descriptor, sources)
			if _, err := PackageSemanticBundle(destination, descriptor, sources); err == nil {
				t.Fatal("untrusted semantic bundle package was accepted")
			}
		})
	}
}

func testSemanticBundle(t *testing.T) (string, SemanticBundleDescriptor) {
	t.Helper()
	root := t.TempDir()
	files := map[string][]byte{
		"runtime.bin":  []byte("onnx-runtime-fixture"),
		"onnx-binding": []byte("onnx-go-binding-compiled-with-ort-api-29"),
		"onnx-c-api.h": []byte("#define ORT_API_VERSION 29\n"),
		"model.onnx":   []byte("bge-model-fixture"),
		"tokenizer":    []byte("bge-tokenizer-fixture"),
		"profile.json": []byte("bge-small-zh-v1.5-profile-fixture"),
		"zvec.dylib":   []byte("zvec-native-fixture"),
		"zvec-go.txt":  []byte("zvec-go-binding-fixture"),
		"LICENSE":      []byte("MIT\nApache-2.0\n"),
		"NOTICE":       []byte("RestoreWeave semantic bundle notices\n"),
		"sbom.json":    []byte(`{"name":"restoreweave-semantic-bundle"}`),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	asset := func(name string) SemanticBundleAsset {
		sum := sha256.Sum256(files[name])
		return SemanticBundleAsset{Path: name, SHA256: hex.EncodeToString(sum[:]), Size: uint64(len(files[name]))}
	}
	return root, SemanticBundleDescriptor{
		Schema: SemanticBundleSchemaV1, ProfileID: SemanticBundleBGEProfileID,
		PlatformOS: runtime.GOOS, PlatformArch: runtime.GOARCH,
		ONNXRuntimeVersion: "1.29.0", ONNXRuntimeBuild: "onnxruntime-cpu-darwin-arm64", ONNXRuntimeCAPI: SemanticBundleONNXRuntimeCAPI,
		ONNXGoBindingCommit: "onnxruntime-go-commit", ONNXGoBindingCAPI: SemanticBundleONNXRuntimeCAPI,
		ONNXGoBindingDigest: asset("onnx-binding").SHA256, ModelID: "BAAI/bge-small-zh-v1.5",
		ModelRevision: "bge-revision", ModelExport: "bge-small-zh-v1.5-opset17", ONNXOpset: 17,
		ModelLicenseID:   "BAAI/bge-small-zh-v1.5:MIT",
		TokenizerVersion: "bge-tokenizer-v1", TokenizerRevision: "tokenizer-revision",
		ZvecVersion: "0.6.0", ZvecBuild: "zvec-cpu-darwin-arm64", ZvecGoVersion: "0.6.0", ZvecGoCommit: "zvec-go-commit",
		LicenseExpression:   SemanticBundleLicenseExpression,
		PreprocessingDigest: "sha256:" + strings.Repeat("b", 64), Pooling: SemanticBundleBGEPooling, Normalization: SemanticBundleBGENormalization,
		ElementType: SemanticBundleBGEElementType, Dimension: SemanticBundleBGEDimension, VectorSchema: SemanticBundleBGEVectorSchema,
		SemanticSpace: SemanticBundleBGESemanticSpace, Distance: SemanticBundleBGEDistance,
		IndexConfig: "hnsw:m=16", QueryConfig: "ef=64", QueryPrefix: SemanticBundleBGEQueryPrefix,
		DocumentPrefix: SemanticBundleBGEDocumentPrefix, MaxTokens: SemanticBundleBGEMaxTokens,
		Runtime: asset("runtime.bin"), ONNXBinding: asset("onnx-binding"), ONNXCAPI: asset("onnx-c-api.h"),
		Model: asset("model.onnx"), Tokenizer: asset("tokenizer"),
		Profile: asset("profile.json"), Zvec: asset("zvec.dylib"), ZvecGo: asset("zvec-go.txt"),
		License: asset("LICENSE"), Notice: asset("NOTICE"), SBOM: asset("sbom.json"),
	}
}
