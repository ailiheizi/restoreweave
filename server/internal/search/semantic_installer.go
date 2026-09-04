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
	"time"
)

const (
	semanticInstallerModelRevision = "75c43b069aac4d136ba6bc1122f995fedcfd2781"
	semanticInstallerBaseRevision  = "7999e1d3359715c523056ef9478215996d62a620"
	semanticInstallerZvecCommit    = "9199195b29dac4bf369bb16954464ddf2d73e932"
	semanticInstallerZvecGoModule  = "github.com/zvec-ai/zvec-go"
	// The upstream v0.6.0 tag moved; retain this 0.6.x pseudo-version as the
	// immutable public proxy+sumdb-authenticated dependency. Do not re-resolve
	// it with go get @commit or GOPROXY=direct.
	semanticInstallerZvecGoModuleVersion = "v0.6.1-0.20260721023313-9199195b29da"
	semanticInstallerBindingCommit       = "b4e0f0b495a4ad1eb8a2f61c0286b6e670771525"
	semanticInstallerBindingDigest       = "c702ee797dbe5fe07125b2e9f30496ffcb9dff3559ababfe6ba382e4f7307091"
	semanticInstallerHeaderDigest        = "acc0cf4b3f28d39339c76770d76164bb7a0637dc89f5fde764b4017b632f6743"
	semanticInstallerModelDigest         = "69a0b846f4f116b5e6aabf9546ea6754d02264f3211a13a1bd69b31b8040749a"
	semanticInstallerModelSize           = uint64(94851877)
	semanticInstallerTokenizerDigest     = "48cea5d44424912a6fd1ea647bf4fe50b55ab8b1e5879c3275f80e339e8fae26"
	semanticInstallerTokenizerSize       = uint64(439125)
	semanticInstallerHeaderSize          = uint64(398209)
	// This receipt is generated from the immutable zvec-go module identity
	// above. Keep both facts pinned: a self-consistent descriptor cannot make a
	// replaced provenance receipt admissible.
	semanticInstallerZvecGoReceiptDigest = "c4844b64f066fc24daf39d274bf380fe08cfcf37facb08509a74d724cafb2092"
	semanticInstallerZvecGoReceiptSize   = uint64(134)

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

// SemanticBundleArchive is the independently retainable identity of an
// offline bundle package. The archive digest covers the deterministic tar.gz
// bytes; ProfileDigest covers the descriptor and all declared asset digests.
// Neither digest is inferred from a URL or from the destination path.
type SemanticBundleArchive struct {
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	Size          uint64 `json:"size"`
	ProfileDigest string `json:"profile_digest"`
	PlatformOS    string `json:"platform_os"`
	PlatformArch  string `json:"platform_arch"`
}

const (
	semanticInstallerArchivePrefix = ".restoreweave-semantic-archive-"
	semanticInstallerArchiveMax    = uint64(512 << 20)
	// The v1 package has exactly one manifest and the eleven fixed bundle
	// assets. Rejecting more entries before extraction bounds zero-byte entry
	// floods as well as the byte-size limit.
	semanticInstallerArchiveMaxEntries = 12
)

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
		return semanticBundleInstallPlatform{
			OS: goos, Arch: goarch,
			ORTURL: base + "onnxruntime-osx-arm64-1.29.0.tgz", ORTSHA: "d0706fc34f315d8c88639d0a8c81f2e09e815f282cabed3493c06a054352cf92",
			ZvecURL: zbase + "zvec-libs-darwin-arm64.tar.gz", ZvecSHA: "7ee1f84a2b044458f1d9864c54e80f320a1d2101829f7a744d30a43be25bd6a9",
			ORTBuild: "onnxruntime-cpu-darwin-arm64-1.29.0", ZvecBuild: "zvec-cpu-darwin-arm64-0.6.0",
		}, nil
	case goos == "linux" && goarch == "arm64":
		return semanticBundleInstallPlatform{
			OS: goos, Arch: goarch,
			ORTURL: base + "onnxruntime-linux-aarch64-1.29.0.tgz", ORTSHA: "e1799098ebc054b370f6176a450f158720f297818c613e5dc99b92e2ec82346f",
			ZvecURL: zbase + "zvec-libs-linux-arm64.tar.gz", ZvecSHA: "a3354e7eff8c8c43fcd04f00cd93829e178794256740752fcd9d47f0301225a3",
			ORTBuild: "onnxruntime-cpu-linux-arm64-1.29.0", ZvecBuild: "zvec-cpu-linux-arm64-0.6.0",
		}, nil
	case goos == "linux" && goarch == "amd64":
		return semanticBundleInstallPlatform{
			OS: goos, Arch: goarch,
			ORTURL: base + "onnxruntime-linux-x64-1.29.0.tgz", ORTSHA: "c3fddc4f139a045b0c4902c57410f0694f1c2fdf9b6939fbe38b1aeae7cd14ba",
			ZvecURL: zbase + "zvec-libs-linux-x64.tar.gz", ZvecSHA: "770009b0e79a2dc6d4b2278da7119d4e47493c8f52006f0289f87d3eee4078db",
			ORTBuild: "onnxruntime-cpu-linux-amd64-1.29.0", ZvecBuild: "zvec-cpu-linux-amd64-0.6.0",
		}, nil
	default:
		return semanticBundleInstallPlatform{}, fmt.Errorf("%w: default semantic bundle is unavailable for %s/%s", ErrInvalidSemanticBundle, goos, goarch)
	}
}

func installDefaultSemanticBundleForPlatform(ctx context.Context, modelsRoot string, platform semanticBundleInstallPlatform, client semanticBundleHTTPClient) (semanticBundleInstallResult, error) {
	return installDefaultSemanticBundleForPlatformWithSpecsPolicy(ctx, modelsRoot, platform, client, semanticDefaultInstallDownloads(platform), true)
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
	return installDefaultSemanticBundleForPlatformWithSpecsPolicy(ctx, modelsRoot, platform, client, downloads, false)
}

// installDefaultSemanticBundleForPlatformWithSpecsPolicy keeps the test-only
// specification seam separate from the real pinned installer.  The former is
// intentionally allowed to use tiny synthetic archives; the latter must also
// match the trusted extracted-binary manifest before it can be published.
func installDefaultSemanticBundleForPlatformWithSpecsPolicy(ctx context.Context, modelsRoot string, platform semanticBundleInstallPlatform, client semanticBundleHTTPClient, downloads []semanticBundleInstallDownload, requirePinned bool) (semanticBundleInstallResult, error) {
	if client == nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: HTTP client is required", ErrInvalidSemanticBundle)
	}
	modelsRoot, err := canonicalSemanticBundleInstallRoot(modelsRoot)
	if err != nil {
		return semanticBundleInstallResult{}, err
	}
	destination := filepath.Join(modelsRoot, SemanticBundleBGEProfileID, platform.OS+"-"+platform.Arch)
	if err := validateSemanticBundlePath(filepath.Dir(destination), true); err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: bundle parent: %v", ErrInvalidSemanticBundle, err)
	}
	var recoveryValidator func(SemanticBundleAdmission) error
	if requirePinned {
		recoveryValidator = func(admission SemanticBundleAdmission) error {
			return validateSemanticInstallerPinnedAdmission(admission, platform)
		}
	}
	if err := recoverSemanticBundleInstallWithValidator(destination, recoveryValidator); err != nil {
		return semanticBundleInstallResult{}, err
	}
	if existing, err := LoadSemanticBundle(destination); err == nil {
		if semanticInstallerExistingMatches(existing, platform, downloads, requirePinned) {
			return semanticBundleInstallResult{Destination: destination, Admission: existing}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		// A malformed or old bundle is replaceable, but is never removed.  It is
		// moved aside only after the replacement has been completely staged.
		if _, statErr := os.Lstat(destination); statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return semanticBundleInstallResult{}, fmt.Errorf("%w: inspect existing bundle: %v", ErrInvalidSemanticBundle, statErr)
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
		if err := ctx.Err(); err != nil {
			return semanticBundleInstallResult{}, fmt.Errorf("%w: installation canceled: %v", ErrInvalidSemanticBundle, err)
		}
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
	if requirePinned {
		if err := validateSemanticInstallerBinaryPin(platform, runtimeDigest, runtimeSize, zvecDigest, zvecSize); err != nil {
			return semanticBundleInstallResult{}, err
		}
	}
	// Keep the Go binding's module identity in the generated SBOM rather than
	// relying on the descriptor's abbreviated version/commit fields.  The
	// detached zvec-go.txt receipt is itself packaged and hashed, so a clean
	// install can audit exactly which immutable dependency was used.
	zvecGoPath := filepath.Join(stage, "zvec-go.txt")
	if err := writeSemanticInstallerAsset(zvecGoPath, []byte("zvec-go 0.6.0\nmodule "+semanticInstallerZvecGoModule+" version "+semanticInstallerZvecGoModuleVersion+"\n"+semanticInstallerZvecCommit+"\n")); err != nil {
		return semanticBundleInstallResult{}, err
	}
	zvecGoDigest, zvecGoSize, err := semanticFileDigest(zvecGoPath)
	if err != nil {
		return semanticBundleInstallResult{}, err
	}
	zvecGoAsset := map[string]any{"path": "zvec-go.txt", "sha256": zvecGoDigest, "size": zvecGoSize}
	sbom := map[string]any{
		"schema":                "restoreweave.semantic-bundle.sbom.v1",
		"profile":               SemanticBundleBGEProfileID,
		"base_model":            "BAAI/bge-small-zh-v1.5@" + semanticInstallerBaseRevision,
		"onnx_converter_source": "Xenova/bge-small-zh-v1.5@" + semanticInstallerModelRevision,
		"assets": map[string]any{
			"onnxruntime_archive": map[string]any{"url": platform.ORTURL, "sha256": platform.ORTSHA},
			"zvec_archive":        map[string]any{"url": platform.ZvecURL, "sha256": platform.ZvecSHA},
			"runtime":             map[string]any{"sha256": runtimeDigest, "size": runtimeSize},
			"zvec":                map[string]any{"sha256": zvecDigest, "size": zvecSize},
			"zvec_go":             zvecGoAsset,
		},
		"dependencies": map[string]any{
			"zvec_go": map[string]any{
				"module":  semanticInstallerZvecGoModule,
				"version": semanticInstallerZvecGoModuleVersion,
				"commit":  semanticInstallerZvecCommit,
				"license": "Apache-2.0",
				"asset":   zvecGoAsset,
			},
		},
	}
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
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: create bundle parent: %v", ErrInvalidSemanticBundle, err)
	}
	if err := validateSemanticBundlePath(filepath.Dir(destination), false); err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: bundle parent: %v", ErrInvalidSemanticBundle, err)
	}
	// PackageSemanticBundle has its own private staging directory and refuses
	// an existing destination.  Give it a unique sibling candidate, then swap
	// that already-admitted tree into place below.  Thus no partially downloaded
	// tree can ever be mistaken for a ready bundle.
	candidate, err := semanticInstallerSiblingPath(filepath.Dir(destination), semanticInstallerCandidatePrefix)
	if err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: candidate path: %v", ErrInvalidSemanticBundle, err)
	}
	defer os.RemoveAll(candidate)
	admission, err := PackageSemanticBundle(candidate, descriptor, map[string]string{"runtime": assets["runtime"].source, "onnx_binding": assets["onnx_binding"].source, "onnx_c_api": assets["onnx_c_api"].source, "model": assets["model"].source, "tokenizer": assets["tokenizer"].source, "profile": assets["profile"].source, "zvec": assets["zvec"].source, "zvec_go": assets["zvec_go"].source, "license": assets["license"].source, "notice": assets["notice"].source, "sbom": assets["sbom"].source})
	if err != nil {
		return semanticBundleInstallResult{}, err
	}
	if requirePinned {
		if err := validateSemanticInstallerPinnedAdmission(admission, platform); err != nil {
			return semanticBundleInstallResult{}, err
		}
	}
	verify := func() error {
		published, err := LoadSemanticBundle(destination)
		if err != nil {
			return err
		}
		if requirePinned {
			if err := validateSemanticInstallerPinnedAdmission(published, platform); err != nil {
				return err
			}
		}
		return nil
	}
	if err := publishSemanticBundleReplacement(candidate, destination, verify); err != nil {
		return semanticBundleInstallResult{}, err
	}
	published, err := LoadSemanticBundle(destination)
	if err != nil {
		return semanticBundleInstallResult{}, fmt.Errorf("%w: published bundle readback: %v", ErrInvalidSemanticBundle, err)
	}
	return semanticBundleInstallResult{Destination: destination, Admission: published}, nil
}

func semanticInstallerExistingMatches(existing SemanticBundleAdmission, platform semanticBundleInstallPlatform, downloads []semanticBundleInstallDownload, requirePinned bool) bool {
	if !defaultSemanticBundleMatches(existing.Descriptor, platform) {
		return false
	}
	if requirePinned {
		return validateSemanticInstallerPinnedAdmission(existing, platform) == nil
	}
	// The private spec seam is used by tests with synthetic assets. Compare
	// every directly downloaded payload so a changed test/profile manifest is
	// not mistaken for an idempotent install. Archive digests are intentionally
	// excluded: their extracted canonical library digest is the admission fact.
	for _, spec := range downloads {
		name := ""
		switch spec.Name {
		case "model.onnx":
			name = "model"
		case "tokenizer.json":
			name = "tokenizer"
		case "onnx-c-api.h":
			name = "onnx_c_api"
		}
		if name != "" && existing.AssetDigests[name] != spec.SHA256 {
			return false
		}
	}
	return true
}

const (
	semanticInstallerOldPrefix       = ".restoreweave-semantic-old-"
	semanticInstallerActiveSuffix    = ".restoreweave-semantic-active-old"
	semanticInstallerRejectedPrefix  = ".restoreweave-semantic-rejected-"
	semanticInstallerCandidatePrefix = ".restoreweave-semantic-candidate-"
)

// semanticInstallerSiblingPath reserves a name without leaving a directory
// at that name. It is used only for installer-owned candidates/backups; user
// data is never removed to make room for one of these names.
func semanticInstallerSiblingPath(parent, prefix string) (string, error) {
	if err := validateSemanticBundlePath(parent, false); err != nil {
		return "", err
	}
	path, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func semanticInstallerActiveBackupPath(destination string) string {
	return destination + semanticInstallerActiveSuffix
}

// recoverSemanticBundleInstall closes the only crash window in which the old
// destination has been renamed aside but the new candidate has not yet been
// published. The active backup has one deterministic name; historical copies
// are never guessed during recovery. If a new destination was already
// published before a crash, the active backup is finalized as history.
func recoverSemanticBundleInstall(destination string) error {
	return recoverSemanticBundleInstallWithValidator(destination, nil)
}

// recoverSemanticBundleInstallWithValidator is kept private so the synthetic
// installer seam can retain its generic Load-only behavior. The fixed default
// installer supplies a pinned validator: a self-consistent but non-default
// live tree is an interrupted candidate, not a successful publication.
func recoverSemanticBundleInstallWithValidator(destination string, validate func(SemanticBundleAdmission) error) error {
	parent := filepath.Dir(destination)
	active := semanticInstallerActiveBackupPath(destination)
	if _, err := os.Lstat(active); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("%w: inspect active install backup: %v", ErrInvalidSemanticBundle, err)
	}
	activeInfo, err := os.Lstat(active)
	if err != nil {
		return fmt.Errorf("%w: inspect active install backup: %v", ErrInvalidSemanticBundle, err)
	}
	if !activeInfo.IsDir() || activeInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: active install backup is not a real directory", ErrInvalidSemanticBundle)
	}
	// Never restore a backup based only on its directory shape. Load performs
	// complete descriptor/asset admission, including no-follow and digest
	// checks. A pinned caller gets the stronger profile check before any rename.
	activeAdmission, err := LoadSemanticBundle(active)
	if err != nil {
		return fmt.Errorf("%w: active install backup admission: %v", ErrInvalidSemanticBundle, err)
	}
	// The active tree is about to become the live destination in the
	// interrupted-replacement case.  Validate it against the same admission
	// policy before any rename, regardless of whether the destination is
	// missing or merely malformed.  A generic-valid/non-pinned tree must never
	// be restored into the fixed default path, even transiently.
	if validate != nil {
		if err := validate(activeAdmission); err != nil {
			return fmt.Errorf("%w: active install backup is not admitted: %v", ErrInvalidSemanticBundle, err)
		}
	}
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(active, destination); err != nil {
			return fmt.Errorf("%w: restore interrupted bundle: %v", ErrInvalidSemanticBundle, err)
		}
		if err := syncSemanticInstallerDirectory(parent); err != nil {
			return fmt.Errorf("%w: persist recovered bundle: %v", ErrInvalidSemanticBundle, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("%w: inspect destination: %v", ErrInvalidSemanticBundle, err)
	}
	// A valid live destination means the process reached the new publication
	// before interruption. Keep the old tree, but move it to a historical
	// sibling so only the deterministic active marker participates in recovery.
	if admission, err := LoadSemanticBundle(destination); err == nil && (validate == nil || validate(admission) == nil) {
		if _, err := finalizeSemanticBundleActiveBackup(destination); err != nil {
			return err
		}
		return nil
	}
	// The live destination is an incomplete/corrupt candidate. Preserve it in
	// a rejection sibling, then restore the active old tree before retrying.
	rejected, err := semanticInstallerSiblingPath(parent, semanticInstallerRejectedPrefix)
	if err != nil {
		return fmt.Errorf("%w: reserve rejected bundle path: %v", ErrInvalidSemanticBundle, err)
	}
	if err := os.Rename(destination, rejected); err != nil {
		return fmt.Errorf("%w: quarantine interrupted candidate: %v", ErrInvalidSemanticBundle, err)
	}
	if err := os.Rename(active, destination); err != nil {
		return fmt.Errorf("%w: restore interrupted bundle: %v", ErrInvalidSemanticBundle, err)
	}
	if err := syncSemanticInstallerDirectory(parent); err != nil {
		return fmt.Errorf("%w: persist recovered bundle: %v", ErrInvalidSemanticBundle, err)
	}
	return nil
}

func finalizeSemanticBundleActiveBackup(destination string) (bool, error) {
	active := semanticInstallerActiveBackupPath(destination)
	parent := filepath.Dir(destination)
	history, err := semanticInstallerSiblingPath(parent, "."+filepath.Base(destination)+semanticInstallerOldPrefix)
	if err != nil {
		return false, fmt.Errorf("%w: reserve old bundle history path: %v", ErrInvalidSemanticBundle, err)
	}
	if err := os.Rename(active, history); err != nil {
		return false, fmt.Errorf("%w: retain old bundle history: %v", ErrInvalidSemanticBundle, err)
	}
	if err := syncSemanticInstallerDirectory(parent); err != nil {
		// The rename is complete and the new destination has already passed
		// admission. Returning the fact that it happened prevents callers from
		// trying to restore through the now-missing active marker.
		return true, fmt.Errorf("%w: persist old bundle history: %v", ErrInvalidSemanticBundle, err)
	}
	return true, nil
}

// publishSemanticBundleReplacement swaps an already complete candidate into
// the stable path. The old path is first isolated as a sibling, never deleted.
// If the rename, directory sync, or post-publish verifier fails, the new tree
// is quarantined and the old path is restored. Leaving a rejected tree aside
// makes every failed state inspectable and avoids a false ready directory.
func publishSemanticBundleReplacement(candidate, destination string, verify func() error) error {
	parent := filepath.Dir(destination)
	if err := validateSemanticBundlePath(parent, false); err != nil {
		return fmt.Errorf("%w: install parent: %v", ErrInvalidSemanticBundle, err)
	}
	candidateInfo, err := os.Lstat(candidate)
	if err != nil {
		return fmt.Errorf("%w: inspect candidate: %v", ErrInvalidSemanticBundle, err)
	}
	if !candidateInfo.IsDir() || candidateInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: candidate must be a real directory", ErrInvalidSemanticBundle)
	}
	active := semanticInstallerActiveBackupPath(destination)
	if _, activeErr := os.Lstat(active); activeErr == nil {
		return fmt.Errorf("%w: active old bundle marker already exists", ErrInvalidSemanticBundle)
	} else if !errors.Is(activeErr, os.ErrNotExist) {
		return fmt.Errorf("%w: inspect active old bundle marker: %v", ErrInvalidSemanticBundle, activeErr)
	}
	var backup bool
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, active); err != nil {
			return fmt.Errorf("%w: isolate old bundle: %v", ErrInvalidSemanticBundle, err)
		}
		backup = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: inspect old bundle: %v", ErrInvalidSemanticBundle, err)
	}
	restore := func(cause error) error {
		if _, err := os.Lstat(destination); err == nil {
			rejected, reserveErr := semanticInstallerSiblingPath(parent, semanticInstallerRejectedPrefix)
			if reserveErr == nil {
				if renameErr := os.Rename(destination, rejected); renameErr != nil {
					reserveErr = renameErr
				}
			}
			if reserveErr != nil {
				return fmt.Errorf("%w; quarantine failed: %v", cause, reserveErr)
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w; inspect failed candidate: %v", cause, err)
		}
		if backup {
			if err := os.Rename(active, destination); err != nil {
				return fmt.Errorf("%w; restore old bundle failed: %v", cause, err)
			}
			if err := syncSemanticInstallerDirectory(parent); err != nil {
				return fmt.Errorf("%w; persist old bundle restore failed: %v", cause, err)
			}
		}
		return cause
	}
	if err := os.Rename(candidate, destination); err != nil {
		return restore(fmt.Errorf("%w: publish candidate: %v", ErrInvalidSemanticBundle, err))
	}
	if err := syncSemanticInstallerDirectory(parent); err != nil {
		return restore(fmt.Errorf("%w: persist candidate publication: %v", ErrInvalidSemanticBundle, err))
	}
	if verify != nil {
		if err := verify(); err != nil {
			return restore(fmt.Errorf("%w: candidate readback: %v", ErrInvalidSemanticBundle, err))
		}
	}
	if backup {
		if renamed, err := finalizeSemanticBundleActiveBackup(destination); err != nil {
			if renamed {
				// The verified new tree and retained old history are the safest
				// state after a post-rename durability error. There is no active
				// marker left to restore, so do not quarantine the live bundle.
				return err
			}
			return restore(err)
		}
	}
	return nil
}

func syncSemanticInstallerDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func semanticInstallerBinaryPin(platform semanticBundleInstallPlatform) (runtimeDigest string, runtimeSize uint64, zvecDigest string, zvecSize uint64) {
	// Measured from each fixed archive's canonical regular library. These are
	// intentionally separate from archive SHA so extraction/layout drift cannot
	// bypass admission.
	switch platform.OS + "/" + platform.Arch {
	case "darwin/arm64":
		return "c04fe65021445904a3cae047272cad05e648282c75bf1f9eb7b3440120ae13dc", 43184400, "c461a253d5b5dcf010c95847a56657930af2eaac94b5a30637bb0ae8574c00b3", 41594848
	case "linux/arm64":
		return "a27d21126db312aa8f02f3d5eaebe466e991f51f469882e6d0407d5a8b64afda", 24538024, "3d80fa6bbb193a37afe036c2c414cb3fed63c1ca02e1930aebd35265780e7189", 53714192
	case "linux/amd64":
		return "5715f06d8992ca8eeeddcce43df3a7d38f97d537052126f558e912cb312460ca", 28497752, "b2f3e406f9930ce0606e61dc69267bec26ea3cd9a56621688bc082076744626b", 59775584
	default:
		return "", 0, "", 0
	}
}

func validateSemanticInstallerBinaryPin(platform semanticBundleInstallPlatform, runtimeDigest string, runtimeSize uint64, zvecDigest string, zvecSize uint64) error {
	wantRuntimeDigest, wantRuntimeSize, wantZvecDigest, wantZvecSize := semanticInstallerBinaryPin(platform)
	if wantRuntimeDigest == "" || wantZvecDigest == "" || wantRuntimeSize == 0 || wantZvecSize == 0 {
		return fmt.Errorf("%w: no trusted extracted runtime/zvec binary pin for %s/%s", ErrInvalidSemanticBundle, platform.OS, platform.Arch)
	}
	if runtimeDigest != wantRuntimeDigest || runtimeSize != wantRuntimeSize {
		return fmt.Errorf("%w: ONNX Runtime binary %s/%d does not match pinned %s/%d", ErrInvalidSemanticBundle, runtimeDigest, runtimeSize, wantRuntimeDigest, wantRuntimeSize)
	}
	if zvecDigest != wantZvecDigest || zvecSize != wantZvecSize {
		return fmt.Errorf("%w: zvec binary %s/%d does not match pinned %s/%d", ErrInvalidSemanticBundle, zvecDigest, zvecSize, wantZvecDigest, wantZvecSize)
	}
	return nil
}

func validateSemanticInstallerPinnedAdmission(admission SemanticBundleAdmission, platform semanticBundleInstallPlatform) error {
	if err := admission.Validate(); err != nil {
		return err
	}
	if !defaultSemanticBundleMatches(admission.Descriptor, platform) {
		return fmt.Errorf("%w: bundle facts do not match the pinned default", ErrInvalidSemanticBundle)
	}
	if err := validateSemanticInstallerBinaryPin(platform, admission.AssetDigests["runtime"], admission.Descriptor.Runtime.Size, admission.AssetDigests["zvec"], admission.Descriptor.Zvec.Size); err != nil {
		return err
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
	if admission.AssetDigests["zvec_go"] != semanticInstallerZvecGoReceiptDigest || admission.Descriptor.ZvecGo.Size != semanticInstallerZvecGoReceiptSize {
		return fmt.Errorf("%w: zvec-go receipt does not match the pinned default", ErrInvalidSemanticBundle)
	}
	return nil
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
	if !(d.Schema == SemanticBundleSchemaV1 && d.ProfileID == SemanticBundleBGEProfileID && d.PlatformOS == p.OS && d.PlatformArch == p.Arch && d.ONNXRuntimeVersion == semanticInstallerORTVersion && d.ONNXRuntimeBuild == p.ORTBuild && d.ONNXRuntimeCAPI == semanticInstallerORTCAPI && d.ONNXGoBindingCommit == semanticInstallerBindingCommit && d.ONNXGoBindingDigest == semanticInstallerBindingDigest && d.ONNXGoBindingCAPI == semanticInstallerORTCAPI && d.ZvecVersion == semanticInstallerZvecVersion && d.ZvecBuild == p.ZvecBuild && d.ZvecGoVersion == semanticInstallerZvecVersion && d.ZvecGoCommit == semanticInstallerZvecCommit && d.ModelID == "BAAI/bge-small-zh-v1.5" && d.ModelRevision == semanticInstallerModelRevision && d.ModelExport == "onnx-single-file;converter=Xenova" && d.ONNXOpset == 11 && d.ModelLicenseID == "BAAI/bge-small-zh-v1.5:MIT" && d.TokenizerVersion == "huggingface-tokenizers" && d.TokenizerRevision == semanticInstallerModelRevision && d.LicenseExpression == SemanticBundleLicenseExpression && d.PreprocessingDigest == semanticInstallerPreprocessingDigest && d.QueryPrefix == SemanticBundleBGEQueryPrefix && d.DocumentPrefix == SemanticBundleBGEDocumentPrefix && d.MaxTokens == SemanticBundleBGEMaxTokens && d.Pooling == SemanticBundleBGEPooling && d.Normalization == SemanticBundleBGENormalization && d.ElementType == SemanticBundleBGEElementType && d.Dimension == SemanticBundleBGEDimension && d.VectorSchema == SemanticBundleBGEVectorSchema && d.SemanticSpace == SemanticBundleBGESemanticSpace && d.Distance == SemanticBundleBGEDistance && d.IndexConfig == "hnsw:m=16" && d.QueryConfig == "ef=64") {
		return false
	}
	paths := map[string]string{
		"runtime": "runtime.bin", "onnx_binding": "onnx-binding.txt", "onnx_c_api": "onnx-c-api.h",
		"model": "model.onnx", "tokenizer": "tokenizer.json", "profile": "profile.json", "zvec": "zvec.dylib",
		"zvec_go": "zvec-go.txt", "license": "LICENSE", "notice": "NOTICE", "sbom": "sbom.json",
	}
	for _, entry := range d.assets() {
		if paths[entry.Name] != entry.Asset.Path {
			return false
		}
	}
	return true
}

// ValidateDefaultSemanticBundleAdmission verifies the complete admission and
// the immutable facts of the one supported personal default. The installer
// additionally pins downloaded archive/model digests; this function protects
// callbacks and explicit local-bundle overrides from accepting a self-
// consistent descriptor for a different model revision or output space.
func ValidateDefaultSemanticBundleAdmission(admission SemanticBundleAdmission) error {
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	return validateSemanticInstallerPinnedAdmission(admission, platform)
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

// InstallDefaultSemanticBundleFromDirectory installs the pinned local BGE
// bundle from an already packaged directory. The source directory is an
// offline custody boundary: it is loaded and fully content-validated before
// any destination is staged, and no network client or ambient model lookup is
// involved. Only the one host/platform-specific default destination is
// admitted.
//
// The source remains untouched. Installation is idempotent when the live
// destination has the same admitted profile, and replacement uses the same
// staged, atomic, recoverable publication path as the online installer.
func InstallDefaultSemanticBundleFromDirectory(ctx context.Context, modelsRoot, sourceRoot string) (SemanticBundleAdmission, error) {
	return installSemanticBundleFromDirectory(ctx, modelsRoot, sourceRoot, true)
}

// PackageSemanticBundleArchive creates a deterministic, self-contained offline
// artifact from an admitted bundle. It reopens and verifies the source bundle
// before writing any output, and refuses to replace an existing artifact.
func PackageSemanticBundleArchive(ctx context.Context, destination, sourceRoot string, admission SemanticBundleAdmission) (SemanticBundleArchive, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SemanticBundleArchive{}, err
	}
	destination, err := canonicalSemanticBundleArchivePath(destination)
	if err != nil {
		return SemanticBundleArchive{}, err
	}
	sourceRoot, err = canonicalSemanticBundleRoot(sourceRoot)
	if err != nil {
		return SemanticBundleArchive{}, err
	}
	if err := validateSemanticOfflineBundleSourceRoot(sourceRoot); err != nil {
		return SemanticBundleArchive{}, err
	}
	if err := admission.Validate(); err != nil {
		return SemanticBundleArchive{}, err
	}
	loaded, err := LoadSemanticBundle(sourceRoot)
	if err != nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: source admission: %v", ErrInvalidSemanticBundle, err)
	}
	if loaded.ProfileDigest != admission.ProfileDigest {
		return SemanticBundleArchive{}, fmt.Errorf("%w: source profile differs from admission", ErrInvalidSemanticBundle)
	}
	for _, entry := range loaded.Descriptor.assets() {
		if loaded.AssetDigests[entry.Name] != admission.AssetDigests[entry.Name] {
			return SemanticBundleArchive{}, fmt.Errorf("%w: source asset %s differs from admission", ErrInvalidSemanticBundle, entry.Name)
		}
	}
	if err := validateSemanticBundlePath(filepath.Dir(destination), true); err != nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: archive parent: %v", ErrInvalidSemanticBundle, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: create archive parent: %v", ErrInvalidSemanticBundle, err)
	}
	if _, err := os.Lstat(destination); err == nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: archive destination already exists", ErrInvalidSemanticBundle)
	} else if !errors.Is(err, os.ErrNotExist) {
		return SemanticBundleArchive{}, fmt.Errorf("%w: inspect archive destination: %v", ErrInvalidSemanticBundle, err)
	}
	temporary, err := semanticInstallerSiblingPath(filepath.Dir(destination), semanticInstallerArchivePrefix)
	if err != nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: reserve archive path: %v", ErrInvalidSemanticBundle, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	output, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: create archive: %v", ErrInvalidSemanticBundle, err)
	}
	digest := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(output, digest))
	// Fixed gzip metadata and tar metadata make repeated packaging byte-identical.
	gz.Header.ModTime = time.Unix(0, 0)
	gz.Header.OS = 255
	tarWriter := tar.NewWriter(gz)
	manifest, err := json.Marshal(loaded.Descriptor)
	if err == nil {
		err = writeSemanticBundleArchiveBytes(tarWriter, SemanticBundleManifestName, manifest)
	}
	entries := append([]semanticBundleAssetEntry(nil), loaded.Descriptor.assets()...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Asset.Path < entries[j].Asset.Path })
	if err == nil {
		for _, entry := range entries {
			if writeErr := writeSemanticBundleArchiveAsset(ctx, tarWriter, sourceRoot, entry); writeErr != nil {
				err = writeErr
				break
			}
		}
	}
	if closeErr := tarWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gz.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = output.Sync()
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: write archive: %v", ErrInvalidSemanticBundle, err)
	}
	info, err := os.Stat(temporary)
	if err != nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: stat archive: %v", ErrInvalidSemanticBundle, err)
	}
	if info.Size() <= 0 || uint64(info.Size()) > semanticInstallerArchiveMax {
		return SemanticBundleArchive{}, fmt.Errorf("%w: archive size %d is outside bounds", ErrInvalidSemanticBundle, info.Size())
	}
	if err := os.Rename(temporary, destination); err != nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: publish archive: %v", ErrInvalidSemanticBundle, err)
	}
	if err := syncSemanticInstallerDirectory(filepath.Dir(destination)); err != nil {
		return SemanticBundleArchive{}, fmt.Errorf("%w: persist archive: %v", ErrInvalidSemanticBundle, err)
	}
	committed = true
	return SemanticBundleArchive{
		Path: destination, SHA256: hex.EncodeToString(digest.Sum(nil)), Size: uint64(info.Size()),
		ProfileDigest: loaded.ProfileDigest, PlatformOS: loaded.Descriptor.PlatformOS, PlatformArch: loaded.Descriptor.PlatformArch,
	}, nil
}

func writeSemanticBundleArchiveBytes(writer *tar.Writer, name string, payload []byte) error {
	if err := validateSemanticBundleAssetPath(name); err != nil && name != SemanticBundleManifestName {
		return err
	}
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Typeflag: tar.TypeReg, Size: int64(len(payload)), Uid: 0, Gid: 0, ModTime: time.Unix(0, 0)}); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}

func writeSemanticBundleArchiveAsset(ctx context.Context, writer *tar.Writer, root string, entry semanticBundleAssetEntry) error {
	f, err := openBundleAsset(root, entry)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := writer.WriteHeader(&tar.Header{Name: entry.Asset.Path, Mode: 0o600, Typeflag: tar.TypeReg, Size: int64(entry.Asset.Size), Uid: 0, Gid: 0, ModTime: time.Unix(0, 0)}); err != nil {
		return err
	}
	digest := sha256.New()
	reader := &semanticBundleContextReader{ctx: ctx, reader: io.TeeReader(f, digest)}
	written, err := io.Copy(writer, io.LimitReader(reader, int64(entry.Asset.Size)+1))
	if err != nil {
		return err
	}
	if written != int64(entry.Asset.Size) {
		return fmt.Errorf("%w %s: read %d bytes, want %d", ErrSemanticBundleAsset, entry.Name, written, entry.Asset.Size)
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != entry.Asset.SHA256 {
		return fmt.Errorf("%w %s: digest %s does not match declared %s", ErrSemanticBundleAsset, entry.Name, got, entry.Asset.SHA256)
	}
	return ctx.Err()
}

// InstallDefaultSemanticBundleFromArchive installs a pinned bundle from one
// local tar.gz artifact. Extraction is bounded and contains no network or
// ambient model lookup; the existing directory installer performs the final
// pinned admission and atomic publication.
func InstallDefaultSemanticBundleFromArchive(ctx context.Context, modelsRoot, archivePath string) (SemanticBundleAdmission, error) {
	return installSemanticBundleFromArchive(ctx, modelsRoot, archivePath, true)
}

func installSemanticBundleFromArchive(ctx context.Context, modelsRoot, archivePath string, requirePinned bool) (SemanticBundleAdmission, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SemanticBundleAdmission{}, err
	}
	archivePath, err := canonicalSemanticBundleArchivePath(archivePath)
	if err != nil {
		return SemanticBundleAdmission{}, err
	}
	if err := validateSemanticBundlePath(filepath.Dir(archivePath), false); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: archive parent: %v", ErrInvalidSemanticBundle, err)
	}
	info, err := os.Lstat(archivePath)
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: archive: %v", ErrInvalidSemanticBundle, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: archive must be a regular non-symlink file", ErrInvalidSemanticBundle)
	}
	if info.Size() <= 0 || uint64(info.Size()) > semanticInstallerArchiveMax {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: archive size %d is outside bounds", ErrInvalidSemanticBundle, info.Size())
	}
	input, err := openBundleFileNoFollow(filepath.Dir(archivePath), filepath.Base(archivePath))
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: open archive without following symlink: %v", ErrInvalidSemanticBundle, err)
	}
	defer input.Close()
	openedInfo, err := input.Stat()
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: stat opened archive: %v", ErrInvalidSemanticBundle, err)
	}
	if !openedInfo.Mode().IsRegular() || openedInfo.Size() != info.Size() {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: archive changed while opening", ErrInvalidSemanticBundle)
	}
	stage, err := os.MkdirTemp("", ".restoreweave-semantic-archive-install-")
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: stage archive: %v", ErrInvalidSemanticBundle, err)
	}
	defer os.RemoveAll(stage)
	if err := extractSemanticBundleArchive(input, stage); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: extract archive: %v", ErrInvalidSemanticBundle, err)
	}
	admission, err := LoadSemanticBundle(stage)
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: archive admission: %v", ErrInvalidSemanticBundle, err)
	}
	if err := validateSemanticBundleArchiveContents(stage, admission); err != nil {
		return SemanticBundleAdmission{}, err
	}
	if requirePinned {
		if err := ValidateDefaultSemanticBundleAdmission(admission); err != nil {
			return SemanticBundleAdmission{}, fmt.Errorf("%w: archive is not the pinned default: %v", ErrInvalidSemanticBundle, err)
		}
	}
	return installSemanticBundleFromDirectory(ctx, modelsRoot, stage, requirePinned)
}

func canonicalSemanticBundleArchivePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(path) != path || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("%w: archive path must be absolute and canonical", ErrInvalidSemanticBundle)
	}
	return path, nil
}

func extractSemanticBundleArchive(input *os.File, destination string) error {
	if input == nil {
		return errors.New("archive file is required")
	}
	gz, err := gzip.NewReader(io.LimitReader(input, int64(semanticInstallerArchiveMax)+1))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var total uint64
	entryCount := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		entryCount++
		if entryCount > semanticInstallerArchiveMaxEntries {
			return fmt.Errorf("archive contains more than %d entries", semanticInstallerArchiveMaxEntries)
		}
		name, err := semanticBundleArchivePath(hdr.Name)
		if err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("duplicate archive path %q", name)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return fmt.Errorf("archive entry %q is not a regular file", name)
		}
		if hdr.Size < 0 || uint64(hdr.Size) > semanticInstallerArchiveMax-total {
			return fmt.Errorf("archive exceeds extraction limit")
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
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
			return fmt.Errorf("extract %q: %v %v", name, copyErr, closeErr)
		}
		seen[name] = true
		total += uint64(hdr.Size)
	}
	return nil
}

func semanticBundleArchivePath(name string) (string, error) {
	original := name
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	if name == "" || name != filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) {
		return "", fmt.Errorf("non-canonical archive path %q", original)
	}
	if _, err := semanticArchivePath(name); err != nil {
		return "", err
	}
	return name, nil
}

func validateSemanticBundleArchiveContents(root string, admission SemanticBundleAdmission) error {
	expected := map[string]bool{SemanticBundleManifestName: true}
	expectedDirs := map[string]bool{}
	for _, entry := range admission.Descriptor.assets() {
		expected[entry.Asset.Path] = true
		for parent := filepath.Dir(filepath.FromSlash(entry.Asset.Path)); parent != "."; parent = filepath.Dir(parent) {
			expectedDirs[filepath.ToSlash(parent)] = true
		}
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			if !expectedDirs[rel] {
				return fmt.Errorf("%w: unexpected archive directory %q", ErrInvalidSemanticBundle, rel)
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !expected[rel] {
			return fmt.Errorf("%w: unexpected archive file %q", ErrInvalidSemanticBundle, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// installSemanticBundleFromDirectory is deliberately private so tests can use
// small synthetic bundles without weakening the public pinned-default
// contract. requirePinned is never false from production code.
func installSemanticBundleFromDirectory(ctx context.Context, modelsRoot, sourceRoot string, requirePinned bool) (SemanticBundleAdmission, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SemanticBundleAdmission{}, err
	}
	modelsRoot, err := canonicalSemanticBundleInstallRoot(modelsRoot)
	if err != nil {
		return SemanticBundleAdmission{}, err
	}
	sourceRoot, err = canonicalSemanticBundleRoot(sourceRoot)
	if err != nil {
		return SemanticBundleAdmission{}, err
	}
	if err := validateSemanticOfflineBundleSourceRoot(sourceRoot); err != nil {
		return SemanticBundleAdmission{}, err
	}
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return SemanticBundleAdmission{}, err
	}
	destination := filepath.Join(modelsRoot, SemanticBundleBGEProfileID, platform.OS+"-"+platform.Arch)
	if err := validateSemanticBundlePath(filepath.Dir(destination), true); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: bundle parent: %v", ErrInvalidSemanticBundle, err)
	}
	overlaps, samePath := semanticInstallerPathsOverlap(sourceRoot, destination)
	if overlaps && !samePath {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: source and destination paths overlap", ErrInvalidSemanticBundle)
	}
	if info, statErr := os.Lstat(destination); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: destination must not be a symlink", ErrInvalidSemanticBundle)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: inspect destination: %v", ErrInvalidSemanticBundle, statErr)
	}
	var pinned func(SemanticBundleAdmission) error
	if requirePinned {
		pinned = func(admission SemanticBundleAdmission) error {
			return validateSemanticInstallerPinnedAdmission(admission, platform)
		}
	}
	source, err := LoadSemanticBundle(sourceRoot)
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: source admission: %v", ErrInvalidSemanticBundle, err)
	}
	if pinned != nil {
		if err := pinned(source); err != nil {
			return SemanticBundleAdmission{}, fmt.Errorf("%w: source is not the pinned default: %v", ErrInvalidSemanticBundle, err)
		}
	}
	// An already complete bundle may be used as its own offline source. Return
	// before recovery/staging so this idempotent case leaves the source tree
	// byte-for-byte untouched.
	if samePath {
		return source, nil
	}
	// Recover a previous interrupted replacement before inspecting whether the
	// destination is reusable. A bad live candidate is quarantined and the
	// active old tree is restored by this helper.
	if err := recoverSemanticBundleInstallWithValidator(destination, pinned); err != nil {
		return SemanticBundleAdmission{}, err
	}
	if err := ctx.Err(); err != nil {
		return SemanticBundleAdmission{}, err
	}
	// A complete, independently revalidated destination is already the desired
	// result. This avoids touching it (and avoids staging a copy) on repeats.
	if existing, loadErr := LoadSemanticBundle(destination); loadErr == nil {
		if existing.ProfileDigest == source.ProfileDigest && (pinned == nil || pinned(existing) == nil) {
			return existing, nil
		}
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		// A malformed existing tree is replaceable, but no error is hidden here:
		// publishSemanticBundleReplacement retains it as history or quarantines
		// it if the replacement later fails.
		if _, statErr := os.Lstat(destination); statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return SemanticBundleAdmission{}, fmt.Errorf("%w: inspect destination: %v", ErrInvalidSemanticBundle, statErr)
		}
	}
	if err := validateSemanticBundlePath(modelsRoot, true); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: models root: %v", ErrInvalidSemanticBundle, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: create bundle parent: %v", ErrInvalidSemanticBundle, err)
	}
	if err := validateSemanticBundlePath(filepath.Dir(destination), false); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: bundle parent: %v", ErrInvalidSemanticBundle, err)
	}
	candidate, err := semanticInstallerSiblingPath(filepath.Dir(destination), semanticInstallerCandidatePrefix)
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: candidate path: %v", ErrInvalidSemanticBundle, err)
	}
	defer os.RemoveAll(candidate)
	sources := make(map[string]string, len(source.Descriptor.assets()))
	for _, entry := range source.Descriptor.assets() {
		sources[entry.Name] = filepath.Join(sourceRoot, filepath.FromSlash(entry.Asset.Path))
	}
	if _, err := PackageSemanticBundle(candidate, source.Descriptor, sources); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: stage offline bundle: %v", ErrInvalidSemanticBundle, err)
	}
	if err := ctx.Err(); err != nil {
		return SemanticBundleAdmission{}, err
	}
	verify := func() error {
		published, err := LoadSemanticBundle(destination)
		if err != nil {
			return err
		}
		if published.ProfileDigest != source.ProfileDigest {
			return fmt.Errorf("%w: published profile differs from source", ErrInvalidSemanticBundle)
		}
		if pinned != nil {
			return pinned(published)
		}
		return nil
	}
	if err := publishSemanticBundleReplacement(candidate, destination, verify); err != nil {
		return SemanticBundleAdmission{}, err
	}
	published, err := LoadSemanticBundle(destination)
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: offline install readback: %v", ErrInvalidSemanticBundle, err)
	}
	if pinned != nil {
		if err := pinned(published); err != nil {
			return SemanticBundleAdmission{}, fmt.Errorf("%w: installed bundle is not pinned: %v", ErrInvalidSemanticBundle, err)
		}
	}
	return published, nil
}

// semanticInstallerPathsOverlap rejects ancestor/descendant source and target
// pairs before any recovery or staging mutation. Both inputs are canonical
// absolute paths, so component-aware Rel checks avoid false positives such as
// /models-a versus /models-ab.
func semanticInstallerPathsOverlap(sourceRoot, destination string) (overlaps, samePath bool) {
	sourceRoot = semanticInstallerResolvedPathForOverlap(sourceRoot)
	destination = semanticInstallerResolvedPathForOverlap(destination)
	if sourceRoot == destination {
		return true, true
	}
	isWithin := func(parent, child string) bool {
		relative, err := filepath.Rel(parent, child)
		if err != nil || relative == "." || filepath.IsAbs(relative) {
			return false
		}
		return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	}
	return isWithin(sourceRoot, destination) || isWithin(destination, sourceRoot), false
}

// semanticInstallerResolvedPathForOverlap resolves the longest existing
// prefix, then appends missing components. This catches Darwin aliases such
// as /tmp and /private/tmp even when the destination has not been created yet.
func semanticInstallerResolvedPathForOverlap(path string) string {
	path = filepath.Clean(path)
	current := path
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			if resolved, resolveErr := filepath.EvalSymlinks(current); resolveErr == nil {
				for i := len(suffix) - 1; i >= 0; i-- {
					resolved = filepath.Join(resolved, suffix[i])
				}
				return filepath.Clean(resolved)
			}
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
	return path
}

// validateSemanticOfflineBundleSourceRoot rejects a symlink root explicitly,
// even on hosts where /var or another platform alias is allowed elsewhere.
func validateSemanticOfflineBundleSourceRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("%w: source root: %v", ErrInvalidSemanticBundle, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: source root must be a real directory", ErrInvalidSemanticBundle)
	}
	if err := validateSemanticBundlePath(root, false); err != nil {
		return fmt.Errorf("%w: source root path: %v", ErrInvalidSemanticBundle, err)
	}
	return nil
}
