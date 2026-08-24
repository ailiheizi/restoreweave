package search

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"
)

// SemanticBundleAsset identifies one immutable, locally retained bundle file.
// Path is relative to the bundle root; it is never interpreted as a path on its
// own and is checked component-by-component during admission.
type SemanticBundleAsset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   uint64 `json:"size"`
}

// SemanticBundleDescriptor is an offline admission record for the reference
// local semantic profile. It describes artifacts; it does not load a runtime,
// execute a model, or imply provider readiness.
type SemanticBundleDescriptor struct {
	Schema    string `json:"schema"`
	ProfileID string `json:"profile_id"`

	PlatformOS   string `json:"platform_os"`
	PlatformArch string `json:"platform_arch"`

	ONNXRuntimeVersion string `json:"onnx_runtime_version"`
	ONNXRuntimeBuild   string `json:"onnx_runtime_build"`
	// The release number and C API table version are separate facts. A Go
	// binding tag can advance without updating the headers it vendors, so both
	// the runtime and the worker's binding headers must agree before admission.
	ONNXRuntimeCAPI     int    `json:"onnx_runtime_c_api"`
	ONNXGoBindingCommit string `json:"onnx_go_binding_commit"`
	ONNXGoBindingDigest string `json:"onnx_go_binding_digest"`
	ONNXGoBindingCAPI   int    `json:"onnx_go_binding_c_api"`
	ModelID             string `json:"model_id"`
	ModelRevision       string `json:"model_revision"`
	ModelExport         string `json:"model_export"`
	ONNXOpset           int    `json:"onnx_opset"`
	ModelLicenseID      string `json:"model_license_id"`
	TokenizerVersion    string `json:"tokenizer_version"`
	TokenizerRevision   string `json:"tokenizer_revision"`
	ZvecVersion         string `json:"zvec_version"`
	ZvecBuild           string `json:"zvec_build"`
	ZvecGoVersion       string `json:"zvec_go_version"`
	ZvecGoCommit        string `json:"zvec_go_commit"`
	LicenseExpression   string `json:"license_expression"`

	PreprocessingDigest string `json:"preprocessing_digest"`
	QueryPrefix         string `json:"query_prefix"`
	DocumentPrefix      string `json:"document_prefix"`
	MaxTokens           int    `json:"max_tokens"`
	Pooling             string `json:"pooling"`
	Normalization       string `json:"normalization"`
	ElementType         string `json:"element_type"`
	Dimension           int    `json:"dimension"`
	VectorSchema        string `json:"vector_schema"`
	SemanticSpace       string `json:"semantic_space"`
	Distance            string `json:"distance"`
	IndexConfig         string `json:"index_config"`
	QueryConfig         string `json:"query_config"`

	Runtime     SemanticBundleAsset `json:"runtime"`
	ONNXBinding SemanticBundleAsset `json:"onnx_binding"`
	ONNXCAPI    SemanticBundleAsset `json:"onnx_c_api"`
	Model       SemanticBundleAsset `json:"model"`
	Tokenizer   SemanticBundleAsset `json:"tokenizer"`
	Profile     SemanticBundleAsset `json:"profile"`
	Zvec        SemanticBundleAsset `json:"zvec"`
	ZvecGo      SemanticBundleAsset `json:"zvec_go"`
	License     SemanticBundleAsset `json:"license"`
	Notice      SemanticBundleAsset `json:"notice"`
	SBOM        SemanticBundleAsset `json:"sbom"`
}

var (
	ErrInvalidSemanticBundle = errors.New("invalid semantic bundle")
	ErrSemanticBundleAsset   = errors.New("invalid semantic bundle asset")
)

// PackageSemanticBundle copies an already-described set of local assets into
// a new install root and writes the descriptor there. It is a host-owned
// qualification boundary, not a downloader: every source path is explicit,
// absolute, regular, and content-addressed by the descriptor. The package is
// staged beside the destination and atomically published only after a clean
// LoadSemanticBundle admission succeeds.
func PackageSemanticBundle(destination string, descriptor SemanticBundleDescriptor, sources map[string]string) (SemanticBundleAdmission, error) {
	if err := descriptor.validateFacts(); err != nil {
		return SemanticBundleAdmission{}, err
	}
	destination, err := canonicalSemanticBundleRoot(destination)
	if err != nil {
		return SemanticBundleAdmission{}, err
	}
	if err := validateSemanticBundlePath(filepath.Dir(destination), true); err != nil {
		return SemanticBundleAdmission{}, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: destination already exists", ErrInvalidSemanticBundle)
	} else if !errors.Is(err, os.ErrNotExist) {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: inspect destination: %v", ErrInvalidSemanticBundle, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: create destination parent: %v", ErrInvalidSemanticBundle, err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".restoreweave-semantic-bundle-")
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: stage package: %v", ErrInvalidSemanticBundle, err)
	}
	defer os.RemoveAll(stage)

	for _, entry := range descriptor.assets() {
		source, ok := sources[entry.Name]
		if !ok || strings.TrimSpace(source) == "" || !filepath.IsAbs(source) || filepath.Clean(source) != source {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: explicit absolute source is required", ErrSemanticBundleAsset, entry.Name)
		}
		if err := validateSemanticBundlePath(source, false); err != nil {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: source path: %v", ErrSemanticBundleAsset, entry.Name, err)
		}
		info, err := os.Lstat(source)
		if err != nil {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: source: %v", ErrSemanticBundleAsset, entry.Name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: source must be a regular non-symlink file", ErrSemanticBundleAsset, entry.Name)
		}
		if uint64(info.Size()) != entry.Asset.Size {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: source size %d does not match declared %d", ErrSemanticBundleAsset, entry.Name, info.Size(), entry.Asset.Size)
		}
		input, err := openBundleFileNoFollow(filepath.Dir(source), filepath.Base(source))
		if err != nil {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: open source: %v", ErrSemanticBundleAsset, entry.Name, err)
		}
		inputInfo, err := input.Stat()
		if err != nil {
			_ = input.Close()
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: stat source: %v", ErrSemanticBundleAsset, entry.Name, err)
		}
		if uint64(inputInfo.Size()) != entry.Asset.Size || !inputInfo.Mode().IsRegular() {
			_ = input.Close()
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: opened source changed", ErrSemanticBundleAsset, entry.Name)
		}
		destinationPath := filepath.Join(stage, filepath.FromSlash(entry.Asset.Path))
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
			_ = input.Close()
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: create asset directory: %v", ErrSemanticBundleAsset, entry.Name, err)
		}
		output, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			_ = input.Close()
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: create asset: %v", ErrSemanticBundleAsset, entry.Name, err)
		}
		digest := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(digest, output), io.LimitReader(input, int64(entry.Asset.Size)+1))
		inputCloseErr := input.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: copy source: %v", ErrSemanticBundleAsset, entry.Name, copyErr)
		}
		if inputCloseErr != nil || outputCloseErr != nil {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: close source/asset: %v %v", ErrSemanticBundleAsset, entry.Name, inputCloseErr, outputCloseErr)
		}
		if written != int64(entry.Asset.Size) {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: copied %d bytes, want %d", ErrSemanticBundleAsset, entry.Name, written, entry.Asset.Size)
		}
		if got := hex.EncodeToString(digest.Sum(nil)); got != entry.Asset.SHA256 {
			return SemanticBundleAdmission{}, fmt.Errorf("%w %s: source digest %s does not match declared %s", ErrSemanticBundleAsset, entry.Name, got, entry.Asset.SHA256)
		}
	}

	manifest, err := json.Marshal(descriptor)
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: encode descriptor: %v", ErrInvalidSemanticBundle, err)
	}
	manifestPath := filepath.Join(stage, SemanticBundleManifestName)
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: write descriptor: %v", ErrInvalidSemanticBundle, err)
	}
	_, err = LoadSemanticBundle(stage)
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: staged admission: %v", ErrInvalidSemanticBundle, err)
	}
	if err := os.Rename(stage, destination); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: publish install root: %v", ErrInvalidSemanticBundle, err)
	}
	return LoadSemanticBundle(destination)
}

// validateSemanticBundlePath rejects a symlink source or immediate source
// parent before a package is copied. Platform aliases such as macOS /var are
// allowed; the staged install root is validated completely by LoadSemanticBundle.
func validateSemanticBundlePath(path string, allowMissingFinal bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%w: path must be absolute and canonical", ErrInvalidSemanticBundle)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && allowMissingFinal {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: symlink component %q", ErrInvalidSemanticBundle, path)
	}
	parent := filepath.Dir(path)
	if parent != path {
		parentInfo, parentErr := os.Lstat(parent)
		if parentErr == nil && parentInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink component %q", ErrInvalidSemanticBundle, parent)
		}
	}
	return nil
}

const (
	SemanticBundleSchemaV1     = "restoreweave.semantic-bundle.v1"
	SemanticBundleBGEProfileID = "bge-small-zh-v1.5"
	// These are the output-affecting facts of the official BGE v1.5
	// sentence-transformers profile. Keep them in the bundle admission layer
	// so a descriptor cannot silently select a different embedding space.
	SemanticBundleBGEQueryPrefix    = "\u4e3a\u8fd9\u4e2a\u53e5\u5b50\u751f\u6210\u8868\u793a\u4ee5\u7528\u4e8e\u68c0\u7d22\u76f8\u5173\u6587\u7ae0\uff1a"
	SemanticBundleBGEDocumentPrefix = ""
	SemanticBundleBGEMaxTokens      = 512
	SemanticBundleBGEPooling        = "cls"
	SemanticBundleBGENormalization  = "l2"
	SemanticBundleBGEElementType    = "float32"
	SemanticBundleBGEDimension      = 512
	SemanticBundleBGEVectorSchema   = "float32:512"
	SemanticBundleBGESemanticSpace  = "bge-small-zh-v1.5-cosine"
	SemanticBundleBGEDistance       = "cosine"
	// ORT_API_VERSION 29 is the C API table selected by the official 1.29.x
	// runtime. The Go binding must be built with the same header family.
	SemanticBundleONNXRuntimeCAPI = 29
	// SemanticBundleManifestName is the only descriptor filename accepted by
	// LoadSemanticBundle. A caller still supplies the already-resolved bundle
	// root; no current-directory or network lookup is performed.
	SemanticBundleManifestName = "semantic-bundle.json"
	// Keep admission bounded even when a malformed descriptor points at a
	// sparse or device-backed file. The reference model/runtime fit below this
	// ceiling; larger release assets require an explicitly reviewed profile.
	SemanticBundleMaxAssetBytes    = uint64(4 << 30)
	SemanticBundleMaxManifestBytes = uint64(1 << 20)
)

// SemanticBundleAdmission is the deterministic result of offline validation.
// It is intentionally not a readiness or health result.
type SemanticBundleAdmission struct {
	Descriptor    SemanticBundleDescriptor `json:"descriptor"`
	ProfileDigest string                   `json:"profile_digest"`
	AssetDigests  map[string]string        `json:"asset_digests"`
}

type semanticBundleAssetEntry struct {
	Name  string
	Asset SemanticBundleAsset
}

func (d SemanticBundleDescriptor) assets() []semanticBundleAssetEntry {
	return []semanticBundleAssetEntry{
		{Name: "runtime", Asset: d.Runtime}, {Name: "onnx_binding", Asset: d.ONNXBinding},
		{Name: "onnx_c_api", Asset: d.ONNXCAPI}, {Name: "model", Asset: d.Model},
		{Name: "tokenizer", Asset: d.Tokenizer}, {Name: "profile", Asset: d.Profile},
		{Name: "zvec", Asset: d.Zvec},
		{Name: "zvec_go", Asset: d.ZvecGo}, {Name: "license", Asset: d.License},
		{Name: "notice", Asset: d.Notice}, {Name: "sbom", Asset: d.SBOM},
	}
}

func (d SemanticBundleDescriptor) validateFacts() error {
	if d.Schema != SemanticBundleSchemaV1 || d.ProfileID != SemanticBundleBGEProfileID {
		return fmt.Errorf("%w: schema/profile must be %q/%q", ErrInvalidSemanticBundle, SemanticBundleSchemaV1, SemanticBundleBGEProfileID)
	}
	if strings.TrimSpace(d.PlatformOS) != runtime.GOOS || strings.TrimSpace(d.PlatformArch) != runtime.GOARCH {
		return fmt.Errorf("%w: platform %q/%q does not match host %q/%q", ErrInvalidSemanticBundle, d.PlatformOS, d.PlatformArch, runtime.GOOS, runtime.GOARCH)
	}
	if !onnxRuntime129(d.ONNXRuntimeVersion) {
		return fmt.Errorf("%w: ONNX Runtime version must be 1.29.x, got %q", ErrInvalidSemanticBundle, d.ONNXRuntimeVersion)
	}
	if strings.TrimSpace(d.ONNXRuntimeBuild) == "" || strings.TrimSpace(d.ONNXGoBindingCommit) == "" || validateSHA256(d.ONNXGoBindingDigest) != nil {
		return fmt.Errorf("%w: ONNX Go binding commit and sha256 digest are required", ErrInvalidSemanticBundle)
	}
	if d.ONNXRuntimeCAPI != SemanticBundleONNXRuntimeCAPI || d.ONNXGoBindingCAPI != d.ONNXRuntimeCAPI {
		return fmt.Errorf("%w: runtime and Go binding C API versions must both be %d", ErrInvalidSemanticBundle, SemanticBundleONNXRuntimeCAPI)
	}
	if strings.TrimSpace(d.ModelID) != "BAAI/bge-small-zh-v1.5" {
		return fmt.Errorf("%w: unsupported model %q", ErrInvalidSemanticBundle, d.ModelID)
	}
	if strings.TrimSpace(d.ModelLicenseID) != "BAAI/bge-small-zh-v1.5:MIT" {
		return fmt.Errorf("%w: unsupported model license identity %q", ErrInvalidSemanticBundle, d.ModelLicenseID)
	}
	if strings.TrimSpace(d.ModelRevision) == "" || strings.TrimSpace(d.ModelExport) == "" || d.ONNXOpset <= 0 {
		return fmt.Errorf("%w: model revision, export identity, and ONNX opset are required", ErrInvalidSemanticBundle)
	}
	if !semver06(d.ZvecVersion) || !semver06(d.ZvecGoVersion) {
		return fmt.Errorf("%w: zvec and zvec-go versions must be 0.6.x", ErrInvalidSemanticBundle)
	}
	if strings.TrimSpace(d.LicenseExpression) != SemanticBundleLicenseExpression {
		return fmt.Errorf("%w: license expression must be %q", ErrInvalidSemanticBundle, SemanticBundleLicenseExpression)
	}
	if strings.TrimSpace(d.TokenizerVersion) == "" || strings.TrimSpace(d.TokenizerRevision) == "" ||
		strings.TrimSpace(d.ZvecBuild) == "" || strings.TrimSpace(d.ZvecGoCommit) == "" {
		return fmt.Errorf("%w: tokenizer and zvec build identities are required", ErrInvalidSemanticBundle)
	}
	if validateSHA256(strings.TrimPrefix(strings.TrimSpace(d.PreprocessingDigest), "sha256:")) != nil ||
		d.Pooling != SemanticBundleBGEPooling || d.Normalization != SemanticBundleBGENormalization ||
		d.ElementType != SemanticBundleBGEElementType || d.Dimension != SemanticBundleBGEDimension ||
		d.VectorSchema != SemanticBundleBGEVectorSchema || d.SemanticSpace != SemanticBundleBGESemanticSpace ||
		d.Distance != SemanticBundleBGEDistance || d.QueryPrefix != SemanticBundleBGEQueryPrefix ||
		d.DocumentPrefix != SemanticBundleBGEDocumentPrefix || d.MaxTokens != SemanticBundleBGEMaxTokens ||
		strings.TrimSpace(d.IndexConfig) == "" || strings.TrimSpace(d.QueryConfig) == "" {
		return fmt.Errorf("%w: incomplete embedding output profile", ErrInvalidSemanticBundle)
	}
	seen := make(map[string]string, len(d.assets()))
	for _, entry := range d.assets() {
		path := entry.Asset.Path
		if previous, ok := seen[path]; ok && path != "" {
			return fmt.Errorf("%w: assets %s and %s reuse path %q", ErrInvalidSemanticBundle, previous, entry.Name, path)
		}
		if path != "" {
			seen[path] = entry.Name
		}
		if entry.Asset.Size == 0 || entry.Asset.Size > SemanticBundleMaxAssetBytes {
			return fmt.Errorf("%w: asset %s has invalid declared size %d", ErrInvalidSemanticBundle, entry.Name, entry.Asset.Size)
		}
	}
	return nil
}

const SemanticBundleLicenseExpression = "MIT AND Apache-2.0"

func onnxRuntime129(version string) bool { return versionInMinorSeries(version, 1, 29) }

func semver06(version string) bool { return versionInMinorSeries(version, 0, 6) }

func versionInMinorSeries(version string, major, minor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 || parts[0] != fmt.Sprint(major) || parts[1] != fmt.Sprint(minor) || parts[2] == "" {
		return false
	}
	for _, r := range parts[2] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validateSHA256(value string) error {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return errors.New("sha256 must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("sha256: %w", err)
	}
	return nil
}

func canonicalSemanticBundleRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(root) != root || !filepath.IsAbs(root) {
		return "", fmt.Errorf("%w: bundle root must be an absolute resolved path", ErrInvalidSemanticBundle)
	}
	clean := filepath.Clean(root)
	if clean != root {
		return "", fmt.Errorf("%w: bundle root must be canonical", ErrInvalidSemanticBundle)
	}
	return clean, nil
}

// readBundleAsset validates every path component and hashes the descriptor
// returned by the platform no-follow opener. Unix builds use descriptor-
// relative openat calls; fallback builds retain the strict component walk.
func openBundleAsset(root string, entry semanticBundleAssetEntry) (*os.File, error) {
	path := filepath.Clean(filepath.FromSlash(entry.Asset.Path))
	portablePath := pathpkg.Clean(entry.Asset.Path)
	if strings.TrimSpace(entry.Asset.Path) == "" || filepath.IsAbs(entry.Asset.Path) || pathpkg.IsAbs(entry.Asset.Path) || portablePath == "." || portablePath == ".." || strings.HasPrefix(portablePath, "../") || filepath.ToSlash(path) != entry.Asset.Path {
		return nil, fmt.Errorf("%w %s: path must be a relative path inside bundle root", ErrSemanticBundleAsset, entry.Name)
	}
	for _, part := range strings.Split(entry.Asset.Path, "/") {
		if part == ".." || part == "." || part == "" {
			return nil, fmt.Errorf("%w %s: path contains non-canonical component", ErrSemanticBundleAsset, entry.Name)
		}
	}
	if err := validateSHA256(entry.Asset.SHA256); err != nil {
		return nil, fmt.Errorf("%w %s: %v", ErrSemanticBundleAsset, entry.Name, err)
	}
	rootAbs, err := canonicalSemanticBundleRoot(root)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("bundle root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("bundle root must not be a symlink")
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("bundle root is not a directory")
	}
	parts := strings.Split(path, string(filepath.Separator))
	current := rootAbs
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return nil, fmt.Errorf("%s %s: %w", entry.Name, entry.Asset.Path, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w %s: symlink is not allowed", ErrSemanticBundleAsset, entry.Name)
		}
		if current != rootAbs && !info.IsDir() && current != filepath.Join(rootAbs, path) {
			return nil, fmt.Errorf("%w %s: path component is not a directory", ErrSemanticBundleAsset, entry.Name)
		}
	}
	info, err := os.Lstat(filepath.Join(rootAbs, path))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", entry.Name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w %s: asset must be a regular file", ErrSemanticBundleAsset, entry.Name)
	}
	f, err := openBundleFileNoFollow(rootAbs, path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", entry.Name, err)
	}
	info, err = f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%s: stat: %w", entry.Name, err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%w %s: asset is not a regular file", ErrSemanticBundleAsset, entry.Name)
	}
	if info.Size() < 0 || uint64(info.Size()) != entry.Asset.Size {
		_ = f.Close()
		return nil, fmt.Errorf("%w %s: size %d does not match declared %d", ErrSemanticBundleAsset, entry.Name, info.Size(), entry.Asset.Size)
	}
	return f, nil
}

// readBundleAsset validates every path component and hashes the descriptor
// returned by the platform no-follow opener. Unix builds use descriptor-
// relative openat calls; fallback builds retain the strict component walk.
func readBundleAsset(root string, entry semanticBundleAssetEntry) (string, error) {
	f, err := openBundleAsset(root, entry)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	readBytes, err := io.Copy(h, io.LimitReader(f, int64(entry.Asset.Size)+1))
	if err != nil {
		return "", fmt.Errorf("%s: read: %w", entry.Name, err)
	}
	if readBytes != int64(entry.Asset.Size) {
		return "", fmt.Errorf("%w %s: read %d bytes, want %d", ErrSemanticBundleAsset, entry.Name, readBytes, entry.Asset.Size)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != entry.Asset.SHA256 {
		return "", fmt.Errorf("%w %s: digest %s does not match declared %s", ErrSemanticBundleAsset, entry.Name, got, entry.Asset.SHA256)
	}
	return got, nil
}

// ReadSemanticBundleAsset reopens one admitted asset through the same
// no-follow path and verifies its size and digest before returning bytes.
// Runtime and tokenizer consumers use these bytes rather than reopening a
// previously checked mutable path.
func ReadSemanticBundleAsset(ctx context.Context, root string, admission SemanticBundleAdmission, name string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := admission.validate(); err != nil {
		return nil, err
	}
	var entry semanticBundleAssetEntry
	found := false
	for _, candidate := range admission.Descriptor.assets() {
		if candidate.Name == name {
			entry = candidate
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: unknown asset %q", ErrSemanticBundleAsset, name)
	}
	if admission.AssetDigests[name] != entry.Asset.SHA256 {
		return nil, fmt.Errorf("%w %s: admitted digest does not match descriptor", ErrSemanticBundleAsset, name)
	}
	maxInt := uint64(^uint(0) >> 1)
	if entry.Asset.Size > maxInt {
		return nil, fmt.Errorf("%w %s: asset is too large for this platform", ErrSemanticBundleAsset, name)
	}
	f, err := openBundleAsset(root, entry)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	payload := make([]byte, int(entry.Asset.Size))
	reader := &semanticBundleContextReader{ctx: ctx, reader: f}
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, fmt.Errorf("%s: read: %w", name, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(payload)
	got := hex.EncodeToString(digest[:])
	if got != entry.Asset.SHA256 {
		return nil, fmt.Errorf("%w %s: digest %s does not match declared %s", ErrSemanticBundleAsset, name, got, entry.Asset.SHA256)
	}
	return payload, nil
}

type semanticBundleContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *semanticBundleContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// LoadSemanticBundle reads the fixed, profile-local descriptor and then
// performs the same content-addressed admission as AdmitSemanticBundle. JSON
// is strict and bounded so unknown fields or an oversized descriptor cannot
// silently change the profile meaning.
func LoadSemanticBundle(root string) (SemanticBundleAdmission, error) {
	root, err := canonicalSemanticBundleRoot(root)
	if err != nil {
		return SemanticBundleAdmission{}, err
	}
	file, err := openBundleFileNoFollow(root, SemanticBundleManifestName)
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: open descriptor: %v", ErrInvalidSemanticBundle, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: stat descriptor: %v", ErrInvalidSemanticBundle, err)
	}
	if info.Size() <= 0 || uint64(info.Size()) > SemanticBundleMaxManifestBytes {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: descriptor size %d is outside bounds", ErrInvalidSemanticBundle, info.Size())
	}
	decoder := json.NewDecoder(io.LimitReader(file, int64(SemanticBundleMaxManifestBytes)+1))
	decoder.DisallowUnknownFields()
	var descriptor SemanticBundleDescriptor
	if err := decoder.Decode(&descriptor); err != nil {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: decode descriptor: %v", ErrInvalidSemanticBundle, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return SemanticBundleAdmission{}, fmt.Errorf("%w: descriptor contains more than one JSON value", ErrInvalidSemanticBundle)
		}
		return SemanticBundleAdmission{}, fmt.Errorf("%w: read descriptor: %v", ErrInvalidSemanticBundle, err)
	}
	return AdmitSemanticBundle(root, descriptor)
}

// EmbeddingGenerationManifest returns the complete output-affecting binding
// for a validated bundle. ConfigDigest is supplied by the host and is never
// inferred from the bundle. This creates an identity for a future generation;
// it does not load or health-check any runtime or index.
func (a SemanticBundleAdmission) EmbeddingGenerationManifest(configDigest string) (EmbeddingGenerationManifest, error) {
	if strings.TrimSpace(configDigest) == "" {
		return EmbeddingGenerationManifest{}, fmt.Errorf("%w: admission and config digest are required", ErrInvalidSemanticBundle)
	}
	if err := a.validate(); err != nil {
		return EmbeddingGenerationManifest{}, err
	}
	manifest := EmbeddingGenerationManifest{
		RuntimeDigest:       "sha256:" + a.AssetDigests["runtime"],
		ModelDigest:         "sha256:" + a.AssetDigests["model"],
		TokenizerDigest:     "sha256:" + a.AssetDigests["tokenizer"],
		PreprocessingDigest: a.Descriptor.PreprocessingDigest,
		Pooling:             a.Descriptor.Pooling,
		Normalization:       a.Descriptor.Normalization,
		ElementType:         a.Descriptor.ElementType,
		Dimension:           a.Descriptor.Dimension,
		VectorSchema:        a.Descriptor.VectorSchema,
		SemanticSpace:       a.Descriptor.SemanticSpace,
		Distance:            a.Descriptor.Distance,
		IndexConfig:         a.Descriptor.IndexConfig,
		QueryConfig:         a.Descriptor.QueryConfig,
		ProviderDigest:      a.ProfileDigest,
		ConfigDigest:        configDigest,
	}
	if err := manifest.Validate(); err != nil {
		return EmbeddingGenerationManifest{}, fmt.Errorf("%w: generation manifest: %v", ErrInvalidSemanticBundle, err)
	}
	return manifest, nil
}

func (a SemanticBundleAdmission) validate() error {
	if err := a.Descriptor.validateFacts(); err != nil {
		return err
	}
	entries := a.Descriptor.assets()
	if len(a.AssetDigests) != len(entries) {
		return fmt.Errorf("%w: admission asset digest set is incomplete", ErrInvalidSemanticBundle)
	}
	for _, entry := range entries {
		if a.AssetDigests[entry.Name] != entry.Asset.SHA256 {
			return fmt.Errorf("%w: admission digest for %s does not match descriptor", ErrInvalidSemanticBundle, entry.Name)
		}
	}
	if a.Descriptor.ONNXGoBindingDigest != a.Descriptor.ONNXBinding.SHA256 {
		return fmt.Errorf("%w: ONNX binding digest is not the declared binding asset digest", ErrInvalidSemanticBundle)
	}
	want, err := semanticBundleProfileDigest(a.Descriptor, a.AssetDigests)
	if err != nil {
		return err
	}
	if a.ProfileDigest != want {
		return fmt.Errorf("%w: admission profile digest does not match descriptor", ErrInvalidSemanticBundle)
	}
	return nil
}

// VerifyPinnedProfile compares the measured bundle identity with a digest
// supplied by trusted release metadata. Computing an expected digest from the
// bundle being admitted would not establish trust and is intentionally left
// to the caller's package-admission boundary.
func (a SemanticBundleAdmission) VerifyPinnedProfile(expected string) error {
	if err := a.validate(); err != nil {
		return err
	}
	expected = strings.TrimSpace(expected)
	if !strings.HasPrefix(expected, "sha256:") || validateSHA256(strings.TrimPrefix(expected, "sha256:")) != nil {
		return fmt.Errorf("%w: pinned profile digest is invalid", ErrInvalidSemanticBundle)
	}
	if subtle.ConstantTimeCompare([]byte(a.ProfileDigest), []byte(expected)) != 1 {
		return fmt.Errorf("%w: measured profile digest does not match pinned release metadata", ErrInvalidSemanticBundle)
	}
	return nil
}

func semanticBundleProfileDigest(descriptor SemanticBundleDescriptor, digests map[string]string) (string, error) {
	canonical := struct {
		Descriptor SemanticBundleDescriptor `json:"descriptor"`
		Assets     map[string]string        `json:"assets"`
	}{descriptor, digests}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: canonical descriptor: %v", ErrInvalidSemanticBundle, err)
	}
	digest := sha256.Sum256(append([]byte("restoreweave.semantic-bundle.v1\n"), payload...))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// AdmitSemanticBundle validates all required local assets and returns their
// content digests plus a profile digest. It performs no runtime loading or
// network operation.
func AdmitSemanticBundle(root string, descriptor SemanticBundleDescriptor) (SemanticBundleAdmission, error) {
	if err := descriptor.validateFacts(); err != nil {
		return SemanticBundleAdmission{}, err
	}
	root, err := canonicalSemanticBundleRoot(root)
	if err != nil {
		return SemanticBundleAdmission{}, err
	}
	digests := make(map[string]string, len(descriptor.assets()))
	for _, entry := range descriptor.assets() {
		got, err := readBundleAsset(root, entry)
		if err != nil {
			return SemanticBundleAdmission{}, err
		}
		digests[entry.Name] = got
	}
	if descriptor.ONNXGoBindingDigest != digests["onnx_binding"] {
		return SemanticBundleAdmission{}, fmt.Errorf("%w: ONNX binding digest does not match measured binding asset", ErrInvalidSemanticBundle)
	}
	profileDigest, err := semanticBundleProfileDigest(descriptor, digests)
	if err != nil {
		return SemanticBundleAdmission{}, err
	}
	return SemanticBundleAdmission{Descriptor: descriptor, ProfileDigest: profileDigest, AssetDigests: digests}, nil
}
