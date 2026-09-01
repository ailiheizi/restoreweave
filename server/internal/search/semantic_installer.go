package search

// This file is deliberately a small, fixed installer for the personal local
// semantic profile.  It is not a general model manager: callers cannot supply
// URLs or select an unreviewed model.  Downloading only happens when the
// installer is explicitly called.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	semanticInstallerModelRevision   = "75c43b069aac4d136ba6bc1122f995fedcfd2781"
	semanticInstallerBaseRevision    = "7999e1d3359715c523056ef9478215996d62a620"
	semanticInstallerZvecCommit      = "f5c6c6cb3dca02b14bf406ca33b86e0c134c179f"
	semanticInstallerBindingCommit   = "b4e0f0b495a4ad1eb8a2f61c0286b6e670771525"
	semanticInstallerBindingDigest   = "c702ee797dbe5fe07125b2e9f30496ffcb9dff3559ababfe6ba382e4f7307091"
	semanticInstallerHeaderDigest    = "acc0cf4b3f28d39339c76770d76164bb7a0637dc89f5fde764b4017b632f6743"
	semanticInstallerModelDigest     = "69a0b846f4f116b5e6aabf9546ea6754d02264f3211a13a1bd69b31b8040749a"
	semanticInstallerModelSize       = uint64(94851877)
	semanticInstallerTokenizerDigest = "48cea5d44424912a6fd1ea647bf4fe50b55ab8b1e5879c3275f80e339e8fae26"
	semanticInstallerTokenizerSize   = uint64(439125)
	semanticInstallerHeaderSize      = uint64(398209)

	semanticInstallerORTVersion          = "1.29.0"
	semanticInstallerZvecVersion         = "0.6.0"
	semanticInstallerORTCAPI             = 29
	semanticInstallerPreprocessingDigest = "sha256:02c794b19d805eff54b90c4eb7d7f75b17c1a3e5103730af2147408d57a7ed0e"

	semanticInstallerMaxDownload = uint64(512 << 20)
	semanticInstallerMaxExtract  = uint64(512 << 20)
)

type semanticBundleHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type semanticBundleInstallResult struct {
	Destination string
	Admission   SemanticBundleAdmission
}

type semanticBundleInstallPlatform struct {
	OS, Arch            string
	ORTURL, ORTSHA      string
	ZvecURL, ZvecSHA    string
	ORTBuild, ZvecBuild string
}

type semanticBundleInstallDownload struct {
	Name   string
	URL    string
	SHA256 string
	Size   uint64
	Max    uint64
}

// InstallDefaultSemanticBundle downloads and atomically installs the pinned
// BGE text profile for the host. It has no first-query or startup download
// path; a caller must invoke it explicitly.
func InstallDefaultSemanticBundle(ctx context.Context, modelsRoot string) (SemanticBundleAdmission, error) {
	result, err := installDefaultSemanticBundle(ctx, modelsRoot, semanticInstallerHTTPClient())
	if err != nil {
		return SemanticBundleAdmission{}, err
	}
	if err := ValidateDefaultSemanticBundleAdmission(result.Admission); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: installed bundle is not the pinned default: %v", ErrInvalidSemanticBundle, err)
	}
	return result.Admission, nil
}

func semanticInstallerHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: semanticInstallerCheckRedirect}
}

func semanticInstallerCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("%w: refusing more than 10 redirects", ErrInvalidSemanticBundle)
	}
	if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("%w: refusing HTTPS to HTTP redirect", ErrInvalidSemanticBundle)
	}
	return nil
}

func installDefaultSemanticBundle(ctx context.Context, modelsRoot string, client semanticBundleHTTPClient) (semanticBundleInstallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return semanticBundleInstallResult{}, err
	}
	return installDefaultSemanticBundleForPlatform(ctx, modelsRoot, platform, client)
}

func semanticBundleInstallPlatformFor(goos, goarch string) (semanticBundleInstallPlatform, error) {
	base := "https://github.com/microsoft/onnxruntime/releases/download/v1.29.0/"
	zbase := "https://github.com/zvec-ai/zvec-go/releases/download/v0.6.0/"
	switch {
	case goos == "darwin" && goarch == "arm64":
		return semanticBundleInstallPlatform{goos, goarch,
			base + "onnxruntime-osx-arm64-1.29.0.tgz", "d0706fc34f315d8c88639d0a8c81f2e09e815f282cabed3493c06a054352cf92",
			zbase + "zvec-libs-darwin-arm64.tar.gz", "7ee1f84a2b044458f1d9864c54e80f320a1d2101829f7a744d30a43be25bd6a9",
			"onnxruntime-cpu-darwin-arm64-1.29.0", "zvec-cpu-darwin-arm64-0.6.0"}, nil
	case goos == "linux" && goarch == "arm64":
		return semanticBundleInstallPlatform{goos, goarch,
			base + "onnxruntime-linux-aarch64-1.29.0.tgz", "e1799098ebc054b370f6176a450f158720f297818c613e5dc99b92e2ec82346f",
			zbase + "zvec-libs-linux-arm64.tar.gz", "a3354e7eff8c8c43fcd04f00cd93829e178794256740752fcd9d47f0301225a3",
			"onnxruntime-cpu-linux-arm64-1.29.0", "zvec-cpu-linux-arm64-0.6.0"}, nil
	case goos == "linux" && goarch == "amd64":
		return semanticBundleInstallPlatform{goos, goarch,
			base + "onnxruntime-linux-x64-1.29.0.tgz", "c3fddc4f139a045b0c4902c57410f0694f1c2fdf9b6939fbe38b1aeae7cd14ba",
			zbase + "zvec-libs-linux-x64.tar.gz", "770009b0e79a2dc6d4b2278da7119d4e47493c8f52006f0289f87d3eee4078db",
			"onnxruntime-cpu-linux-amd64-1.29.0", "zvec-cpu-linux-amd64-0.6.0"}, nil
	default:
		return semanticBundleInstallPlatform{}, fmt.Errorf("%w: default semantic bundle is unavailable for %s/%s", ErrInvalidSemanticBundle, goos, goarch)
	}
}

func installDefaultSemanticBundleForPlatform(ctx context.Context, modelsRoot string, platform semanticBundleInstallPlatform, client semanticBundleHTTPClient) (semanticBundleInstallResult, error) {
	return installDefaultSemanticBundleForPlatformWithSpecs(ctx, modelsRoot, platform, client, semanticDefaultInstallDownloads(platform))
}

func semanticDefaultInstallDownloads(platform semanticBundleInstallPlatform) []semanticBundleInstallDownload {
	return []semanticBundleInstallDownload{
		{Name: "runtime.archive", URL: platform.ORTURL, SHA256: platform.ORTSHA, Max: semanticInstallerMaxDownload},
		{Name: "zvec.archive", URL: platform.ZvecURL, SHA256: platform.ZvecSHA, Max: semanticInstallerMaxDownload},
		{Name: "model.onnx", URL: "https://huggingface.co/Xenova/bge-small-zh-v1.5/resolve/" + semanticInstallerModelRevision + "/onnx/model.onnx", SHA256: semanticInstallerModelDigest, Size: semanticInstallerModelSize, Max: SemanticBundleMaxAssetBytes},
		{Name: "tokenizer.json", URL: "https://huggingface.co/Xenova/bge-small-zh-v1.5/resolve/" + semanticInstallerModelRevision + "/tokenizer.json", SHA256: semanticInstallerTokenizerDigest, Size: semanticInstallerTokenizerSize, Max: SemanticBundleMaxAssetBytes},
		{Name: "onnx-c-api.h", URL: "https://raw.githubusercontent.com/yalue/onnxruntime_go/" + semanticInstallerBindingCommit + "/onnxruntime_c_api.h", SHA256: semanticInstallerHeaderDigest, Size: semanticInstallerHeaderSize, Max: SemanticBundleMaxAssetBytes},
	}
}

func installDefaultSemanticBundleForPlatformWithSpecs(ctx context.Context, modelsRoot string, platform semanticBundleInstallPlatform, client semanticBundleHTTPClient, downloads []semanticBundleInstallDownload) (semanticBundleInstallResult, error) {
	if client == nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: HTTP client is required", ErrInvalidSemanticBundle)
	}
	modelsRoot, err := canonicalSemanticBundleInstallRoot(modelsRoot)
	if err != nil {
		return semanticBundleInstallResult{}, err
	}
	destination := filepath.Join(modelsRoot, SemanticBundleBGEProfileID, platform.OS+"-"+platform.Arch)
	if existing, err := LoadSemanticBundle(destination); err == nil {
		if defaultSemanticBundleMatches(existing.Descriptor, platform) {
			return semanticBundleInstallResult{Destination: destination, Admission: existing}, nil
		}
		return semanticBundleInstallResult{}, fmt.Errorf("%w: existing bundle does not match pinned profile", ErrInvalidSemanticBundle)
	} else if !errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Lstat(destination); statErr == nil {
			return semanticBundleInstallResult{}, fmt.Errorf("%w: existing bundle is invalid: %v", ErrInvalidSemanticBundle, err)
		}
	}
	// Check all existing ancestors before MkdirAll.  Creating the missing
	// suffix first would allow a configured root beneath an untrusted symlink
	// to be materialized outside the intended models tree.
	if err := validateSemanticBundlePath(modelsRoot, true); err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: models root: %v", ErrInvalidSemanticBundle, err)
	}
	if err := os.MkdirAll(modelsRoot, 0o700); err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: create models root: %v", ErrInvalidSemanticBundle, err)
	}
	if err := validateSemanticBundlePath(modelsRoot, false); err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: models root: %v", ErrInvalidSemanticBundle, err)
	}
	stage, err := os.MkdirTemp(modelsRoot, ".restoreweave-semantic-install-")
	if err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: stage install: %v", ErrInvalidSemanticBundle, err)
	}
	defer os.RemoveAll(stage)

	paths := make(map[string]string, len(downloads))
	for _, spec := range downloads {
		path := filepath.Join(stage, spec.Name)
		if err := downloadSemanticInstallerAsset(ctx, client, spec, path); err != nil {
			return semanticBundleInstallResult{}, err
		}
		paths[spec.Name] = path
	}
	archiveRoot := filepath.Join(stage, "unpacked")
	if err := os.MkdirAll(archiveRoot, 0o700); err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: create unpack root: %v", ErrInvalidSemanticBundle, err)
	}
	ortRoot := filepath.Join(archiveRoot, "ort")
	zvecRoot := filepath.Join(archiveRoot, "zvec")
	if err := extractSemanticTarGz(paths["runtime.archive"], ortRoot); err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: ONNX Runtime archive: %v", ErrInvalidSemanticBundle, err)
	}
	if err := extractSemanticTarGz(paths["zvec.archive"], zvecRoot); err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: zvec archive: %v", ErrInvalidSemanticBundle, err)
	}
	runtimePath, err := selectSemanticLibrary(ortRoot, "onnxruntime")
	if err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: ONNX Runtime archive: %v", ErrInvalidSemanticBundle, err)
	}
	zvecPath, err := selectSemanticLibrary(zvecRoot, "zvec_c_api")
	if err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: zvec archive: %v", ErrInvalidSemanticBundle, err)
	}
	generated := map[string]string{}
	generated["onnx-binding.txt"] = filepath.Join(stage, "onnx-binding.txt")
	generated["profile.json"] = filepath.Join(stage, "profile.json")
	generated["LICENSE"] = filepath.Join(stage, "LICENSE")
	generated["NOTICE"] = filepath.Join(stage, "NOTICE")
	generated["sbom.json"] = filepath.Join(stage, "sbom.json")
	if err := writeSemanticInstallerAsset(generated["onnx-binding.txt"], []byte("github.com/yalue/onnxruntime_go@v1.33.0\n"+semanticInstallerBindingCommit+"\n")); err != nil {
		return semanticBundleInstallResult{}, err
	}
	profile := map[string]any{"profile": SemanticBundleBGEProfileID, "model_id": "BAAI/bge-small-zh-v1.5", "base_model": "BAAI/bge-small-zh-v1.5@" + semanticInstallerBaseRevision, "onnx_converter_source": "Xenova/bge-small-zh-v1.5@" + semanticInstallerModelRevision, "model_export": "onnx-single-file;converter=Xenova", "preprocessing_digest": semanticInstallerPreprocessingDigest}
	if err := writeSemanticInstallerJSON(generated["profile.json"], profile); err != nil {
		return semanticBundleInstallResult{}, err
	}
	license := "RestoreWeave semantic bundle license inventory\n\n" +
		"BAAI/bge-small-zh-v1.5 and its Xenova ONNX conversion: MIT (base model).\n" +
		"ONNX Runtime: MIT. github.com/microsoft/onnxruntime\n" +
		"onnxruntime_go: MIT. github.com/yalue/onnxruntime_go\n" +
		"zvec-go and native zvec libraries: Apache-2.0. github.com/zvec-ai/zvec-go\n"
	if err := writeSemanticInstallerAsset(generated["LICENSE"], []byte(license)); err != nil {
		return semanticBundleInstallResult{}, err
	}
	notice := "This bundle uses the MIT BAAI/bge-small-zh-v1.5@" + semanticInstallerBaseRevision + " model via the pinned Xenova/bge-small-zh-v1.5@" + semanticInstallerModelRevision + " ONNX conversion; it is not a BAAI-published ONNX artifact.\n" +
		"Runtime: ONNX Runtime 1.29.0. Vector index: zvec 0.6.0. See LICENSE and SBOM for provenance.\n"
	if err := writeSemanticInstallerAsset(generated["NOTICE"], []byte(notice)); err != nil {
		return semanticBundleInstallResult{}, err
	}
	runtimeDigest, runtimeSize, err := semanticFileDigest(runtimePath)
	if err != nil {
		return semanticBundleInstallResult{}, err
	}
	zvecDigest, zvecSize, err := semanticFileDigest(zvecPath)
	if err != nil {
		return semanticBundleInstallResult{}, err
	}
	sbom := map[string]any{"schema": "restoreweave.semantic-bundle.sbom.v1", "profile": SemanticBundleBGEProfileID, "base_model": "BAAI/bge-small-zh-v1.5@" + semanticInstallerBaseRevision, "onnx_converter_source": "Xenova/bge-small-zh-v1.5@" + semanticInstallerModelRevision, "assets": map[string]any{"onnxruntime_archive": map[string]any{"url": platform.ORTURL, "sha256": platform.ORTSHA}, "zvec_archive": map[string]any{"url": platform.ZvecURL, "sha256": platform.ZvecSHA}, "runtime": map[string]any{"sha256": runtimeDigest, "size": runtimeSize}, "zvec": map[string]any{"sha256": zvecDigest, "size": zvecSize}}}
	if err := writeSemanticInstallerJSON(generated["sbom.json"], sbom); err != nil {
		return semanticBundleInstallResult{}, err
	}

	descriptor := SemanticBundleDescriptor{Schema: SemanticBundleSchemaV1, ProfileID: SemanticBundleBGEProfileID, PlatformOS: platform.OS, PlatformArch: platform.Arch, ONNXRuntimeVersion: semanticInstallerORTVersion, ONNXRuntimeBuild: platform.ORTBuild, ONNXRuntimeCAPI: semanticInstallerORTCAPI, ONNXGoBindingCommit: semanticInstallerBindingCommit, ONNXGoBindingDigest: semanticInstallerBindingDigest, ONNXGoBindingCAPI: semanticInstallerORTCAPI, ModelID: "BAAI/bge-small-zh-v1.5", ModelRevision: semanticInstallerModelRevision, ModelExport: "onnx-single-file;converter=Xenova", ONNXOpset: 11, ModelLicenseID: "BAAI/bge-small-zh-v1.5:MIT", TokenizerVersion: "huggingface-tokenizers", TokenizerRevision: semanticInstallerModelRevision, ZvecVersion: semanticInstallerZvecVersion, ZvecBuild: platform.ZvecBuild, ZvecGoVersion: semanticInstallerZvecVersion, ZvecGoCommit: semanticInstallerZvecCommit, LicenseExpression: SemanticBundleLicenseExpression, PreprocessingDigest: semanticInstallerPreprocessingDigest, QueryPrefix: SemanticBundleBGEQueryPrefix, DocumentPrefix: SemanticBundleBGEDocumentPrefix, MaxTokens: SemanticBundleBGEMaxTokens, Pooling: SemanticBundleBGEPooling, Normalization: SemanticBundleBGENormalization, ElementType: SemanticBundleBGEElementType, Dimension: SemanticBundleBGEDimension, VectorSchema: SemanticBundleBGEVectorSchema, SemanticSpace: SemanticBundleBGESemanticSpace, Distance: SemanticBundleBGEDistance, IndexConfig: "hnsw:m=16", QueryConfig: "ef=64"}
	assets := map[string]struct{ path, source string }{}
	assets["runtime"] = struct{ path, source string }{"runtime.bin", runtimePath}
	assets["onnx_binding"] = struct{ path, source string }{"onnx-binding.txt", generated["onnx-binding.txt"]}
	assets["onnx_c_api"] = struct{ path, source string }{"onnx-c-api.h", paths["onnx-c-api.h"]}
	assets["model"] = struct{ path, source string }{"model.onnx", paths["model.onnx"]}
	assets["tokenizer"] = struct{ path, source string }{"tokenizer.json", paths["tokenizer.json"]}
	assets["profile"] = struct{ path, source string }{"profile.json", generated["profile.json"]}
	assets["zvec"] = struct{ path, source string }{"zvec.dylib", zvecPath}
	assets["license"] = struct{ path, source string }{"LICENSE", generated["LICENSE"]}
	assets["notice"] = struct{ path, source string }{"NOTICE", generated["NOTICE"]}
	assets["sbom"] = struct{ path, source string }{"sbom.json", generated["sbom.json"]}
	// zvec-go.txt is intentionally a separate generated file, despite sharing
	// the small staging directory. Its contents are the pinned zvec-go commit.
	zvecGoPath := filepath.Join(stage, "zvec-go.txt")
	if err := writeSemanticInstallerAsset(zvecGoPath, []byte("zvec-go 0.6.0\n"+semanticInstallerZvecCommit+"\n")); err != nil {
		return semanticBundleInstallResult{}, err
	}
	assets["zvec_go"] = struct{ path, source string }{"zvec-go.txt", zvecGoPath}
	for name, item := range assets {
		asset, err := semanticInstallerAsset(item.path, item.source)
		if err != nil {
			return semanticBundleInstallResult{}, fmt.Errorf("%w %s: %v", ErrSemanticBundleAsset, name, err)
		}
		switch name {
		case "runtime":
			descriptor.Runtime = asset
		case "onnx_binding":
			descriptor.ONNXBinding = asset
		case "onnx_c_api":
			descriptor.ONNXCAPI = asset
		case "model":
			descriptor.Model = asset
		case "tokenizer":
			descriptor.Tokenizer = asset
		case "profile":
			descriptor.Profile = asset
		case "zvec":
			descriptor.Zvec = asset
		case "zvec_go":
			descriptor.ZvecGo = asset
		case "license":
			descriptor.License = asset
		case "notice":
			descriptor.Notice = asset
		case "sbom":
			descriptor.SBOM = asset
		}
	}
	admission, err := PackageSemanticBundle(destination, descriptor, map[string]string{"runtime": assets["runtime"].source, "onnx_binding": assets["onnx_binding"].source, "onnx_c_api": assets["onnx_c_api"].source, "model": assets["model"].source, "tokenizer": assets["tokenizer"].source, "profile": assets["profile"].source, "zvec": assets["zvec"].source, "zvec_go": assets["zvec_go"].source, "license": assets["license"].source, "notice": assets["notice"].source, "sbom": assets["sbom"].source})
	if err != nil {
		return semanticBundleInstallResult{}, err
	}
	return semanticBundleInstallResult{Destination: destination, Admission: admission}, nil
}

func canonicalSemanticBundleInstallRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(root) != root || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", fmt.Errorf("%w: models root must be absolute and canonical", ErrInvalidSemanticBundle)
	}
	return root, nil
}

// DefaultSemanticBundleDestination returns the only install location admitted
// for the pinned local profile on this host.
func DefaultSemanticBundleDestination(modelsRoot string) (string, error) {
	modelsRoot, err := canonicalSemanticBundleInstallRoot(modelsRoot)
	if err != nil {
		return "", err
	}
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	return filepath.Join(modelsRoot, SemanticBundleBGEProfileID, platform.OS+"-"+platform.Arch), nil
}

func defaultSemanticBundleMatches(d SemanticBundleDescriptor, p semanticBundleInstallPlatform) bool {
	return d.Schema == SemanticBundleSchemaV1 && d.ProfileID == SemanticBundleBGEProfileID && d.PlatformOS == p.OS && d.PlatformArch == p.Arch && d.ONNXRuntimeVersion == semanticInstallerORTVersion && d.ONNXRuntimeBuild == p.ORTBuild && d.ONNXRuntimeCAPI == semanticInstallerORTCAPI && d.ONNXGoBindingCommit == semanticInstallerBindingCommit && d.ONNXGoBindingDigest == semanticInstallerBindingDigest && d.ONNXGoBindingCAPI == semanticInstallerORTCAPI && d.ZvecVersion == semanticInstallerZvecVersion && d.ZvecBuild == p.ZvecBuild && d.ZvecGoVersion == semanticInstallerZvecVersion && d.ZvecGoCommit == semanticInstallerZvecCommit && d.ModelID == "BAAI/bge-small-zh-v1.5" && d.ModelRevision == semanticInstallerModelRevision && d.ModelExport == "onnx-single-file;converter=Xenova" && d.ONNXOpset == 11 && d.ModelLicenseID == "BAAI/bge-small-zh-v1.5:MIT" && d.TokenizerVersion == "huggingface-tokenizers" && d.TokenizerRevision == semanticInstallerModelRevision && d.LicenseExpression == SemanticBundleLicenseExpression && d.PreprocessingDigest == semanticInstallerPreprocessingDigest && d.QueryPrefix == SemanticBundleBGEQueryPrefix && d.DocumentPrefix == SemanticBundleBGEDocumentPrefix && d.MaxTokens == SemanticBundleBGEMaxTokens && d.Pooling == SemanticBundleBGEPooling && d.Normalization == SemanticBundleBGENormalization && d.ElementType == SemanticBundleBGEElementType && d.Dimension == SemanticBundleBGEDimension && d.VectorSchema == SemanticBundleBGEVectorSchema && d.SemanticSpace == SemanticBundleBGESemanticSpace && d.Distance == SemanticBundleBGEDistance && d.IndexConfig == "hnsw:m=16" && d.QueryConfig == "ef=64"
}

// ValidateDefaultSemanticBundleAdmission verifies the complete admission and
// the immutable facts of the one supported personal default. The installer
// additionally pins downloaded archive/model digests; this function protects
// callbacks and explicit local-bundle overrides from accepting a self-
// consistent descriptor for a different model revision or output space.
func ValidateDefaultSemanticBundleAdmission(admission SemanticBundleAdmission) error {
	if err := admission.Validate(); err != nil {
		return err
	}
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	if !defaultSemanticBundleMatches(admission.Descriptor, platform) {
		return fmt.Errorf("%w: bundle facts do not match the pinned default", ErrInvalidSemanticBundle)
	}
	if admission.AssetDigests["model"] != semanticInstallerModelDigest || admission.Descriptor.Model.Size != semanticInstallerModelSize {
		return fmt.Errorf("%w: model asset does not match the pinned default", ErrInvalidSemanticBundle)
	}
	if admission.AssetDigests["tokenizer"] != semanticInstallerTokenizerDigest || admission.Descriptor.Tokenizer.Size != semanticInstallerTokenizerSize {
		return fmt.Errorf("%w: tokenizer asset does not match the pinned default", ErrInvalidSemanticBundle)
	}
	if admission.AssetDigests["onnx_c_api"] != semanticInstallerHeaderDigest || admission.Descriptor.ONNXCAPI.Size != semanticInstallerHeaderSize {
		return fmt.Errorf("%w: ONNX C API asset does not match the pinned default", ErrInvalidSemanticBundle)
	}
	return nil
}

func downloadSemanticInstallerAsset(ctx context.Context, client semanticBundleHTTPClient, spec semanticBundleInstallDownload, destination string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return fmt.Errorf("%w %s: request: %v", ErrSemanticBundleAsset, spec.Name, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w %s: download: %v", ErrSemanticBundleAsset, spec.Name, err)
	}
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("%w %s: empty response", ErrSemanticBundleAsset, spec.Name)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w %s: HTTP status %d", ErrSemanticBundleAsset, spec.Name, resp.StatusCode)
	}
	if spec.Max == 0 {
		spec.Max = semanticInstallerMaxDownload
	}
	if resp.ContentLength > int64(spec.Max) {
		return fmt.Errorf("%w %s: response is too large", ErrSemanticBundleAsset, spec.Name)
	}
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%w %s: create staging file: %v", ErrSemanticBundleAsset, spec.Name, err)
	}
	verified := false
	defer func() {
		if !verified {
			_ = os.Remove(destination)
		}
	}()
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, int64(spec.Max)+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("%w %s: write: %v %v", ErrSemanticBundleAsset, spec.Name, copyErr, closeErr)
	}
	if uint64(n) > spec.Max || spec.Size != 0 && uint64(n) != spec.Size {
		return fmt.Errorf("%w %s: size %d does not match %d", ErrSemanticBundleAsset, spec.Name, n, spec.Size)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != spec.SHA256 {
		return fmt.Errorf("%w %s: digest %s does not match %s", ErrSemanticBundleAsset, spec.Name, got, spec.SHA256)
	}
	verified = true
	return nil
}

func extractSemanticTarGz(archivePath, destination string) error {
	input, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer input.Close()
	gz, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	defer gz.Close()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var total uint64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		clean, err := semanticArchivePath(hdr.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(clean))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if seen[clean] {
				return fmt.Errorf("duplicate archive path %q", clean)
			}
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			seen[clean] = true
		case tar.TypeReg, tar.TypeRegA:
			if seen[clean] {
				return fmt.Errorf("duplicate archive path %q", clean)
			}
			if hdr.Size < 0 || uint64(hdr.Size) > semanticInstallerMaxExtract-total {
				return fmt.Errorf("archive exceeds extraction limit")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(f, tr, hdr.Size)
			closeErr := f.Close()
			if copyErr != nil || closeErr != nil {
				return fmt.Errorf("extract %q: %v %v", clean, copyErr, closeErr)
			}
			seen[clean] = true
			total += uint64(hdr.Size)
		case tar.TypeSymlink, tar.TypeLink:
			// Release archives use links for library aliases. Never materialize
			// or follow one: its target is intentionally not interpreted. Keep
			// the entry in the path set so a later file cannot silently replace
			// an archive member with the same normalized name.
			if seen[clean] {
				return fmt.Errorf("duplicate archive path %q", clean)
			}
			seen[clean] = true
		default:
			return fmt.Errorf("unsupported archive entry %q type %d", clean, hdr.Typeflag)
		}
	}
	return nil
}

func semanticArchivePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("absolute archive path")
	}
	// The official zvec release archive prefixes every member with "./".
	// Normalize that one harmless packaging prefix, while retaining the
	// canonical-path checks below. A second prefix is not needed by the
	// release format and remains rejected as non-canonical.
	if strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
		if name == "" {
			return ".", nil // the archive's root directory entry ("./")
		}
		if strings.HasPrefix(name, "./") {
			return "", fmt.Errorf("non-canonical archive path")
		}
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path traversal in archive")
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("non-canonical archive path")
		}
	}
	return clean, nil
}

func selectSemanticLibrary(root, stem string) (string, error) {
	var candidates []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			if semanticCoreLibraryBasename(info.Name(), stem) {
				candidates = append(candidates, path)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return "", fmt.Errorf("want exactly one regular lib%s candidate, found %d", stem, len(candidates))
	}
	// Some official archives retain both the canonical unversioned library
	// and a versioned regular copy, alongside a symlink alias. Prefer the
	// canonical regular name when it is present; multiple versioned regular
	// libraries remain ambiguous and are rejected below.
	canonicalSuffixes := []string{".dylib", ".so"}
	for _, suffix := range canonicalSuffixes {
		var canonical []string
		for _, candidate := range candidates {
			if filepath.Base(candidate) == "lib"+stem+suffix {
				canonical = append(canonical, candidate)
			}
		}
		if len(canonical) == 1 {
			return canonical[0], nil
		}
		if len(canonical) > 1 {
			return "", fmt.Errorf("want exactly one regular lib%s candidate, found %d", stem, len(canonical))
		}
	}
	if len(candidates) != 1 {
		return "", fmt.Errorf("want exactly one regular lib%s candidate, found %d", stem, len(candidates))
	}
	return candidates[0], nil
}

// semanticCoreLibraryBasename accepts the platform's core library name and
// versioned variants, but never provider plugins such as
// libonnxruntime_providers_shared.so. This is intentionally narrower than a
// prefix match because official archives commonly ship both files.
func semanticCoreLibraryBasename(base, stem string) bool {
	prefix := "lib" + stem
	if base == prefix+".dylib" || base == prefix+".so" {
		return true
	}
	if !strings.HasPrefix(base, prefix+".") {
		return false
	}
	suffix := strings.TrimPrefix(base, prefix+".")
	if strings.HasPrefix(suffix, "so.") {
		return semanticNumericVersion(strings.TrimPrefix(suffix, "so."))
	}
	if strings.HasSuffix(suffix, ".dylib") {
		return semanticNumericVersion(strings.TrimSuffix(suffix, ".dylib"))
	}
	return false
}

func semanticNumericVersion(value string) bool {
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func writeSemanticInstallerAsset(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
func writeSemanticInstallerJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeSemanticInstallerAsset(path, data)
}
func semanticFileDigest(path string) (string, uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), uint64(n), nil
}
func semanticInstallerAsset(path, source string) (SemanticBundleAsset, error) {
	digest, size, err := semanticFileDigest(source)
	if err != nil {
		return SemanticBundleAsset{}, err
	}
	return SemanticBundleAsset{Path: path, SHA256: digest, Size: size}, nil
}
