package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

const (
	onnxRuntimeGoModulePath    = "github.com/yalue/onnxruntime_go"
	onnxRuntimeGoModuleVersion = "v1.33.0"
	onnxRuntimeGoModuleSum     = "h1:QP+hMVtjC/PTEoCcJ+od8HfvHf2Yabpp96bifrhP0Sk="
	onnxRuntimeGoBindingCommit = "b4e0f0b495a4ad1eb8a2f61c0286b6e670771525"
	onnxRuntimeGoBindingCAPI   = 29
	onnxRuntimeProbeIsolation  = "IN_PROCESS_PROBE_ONLY"
	maxONNXRuntimeAssetBytes   = uint64(256 << 20)
	maxONNXModelAssetBytes     = uint64(256 << 20)
	maxONNXTokenizerAssetBytes = uint64(16 << 20)
)

var ErrONNXRuntimeUnavailable = errors.New("ONNX Runtime adapter is unavailable")

// ONNXRuntimeAdapterOptions is reserved for future host-owned transport
// options. Callers cannot assert an isolation class; only a process
// supervisor may establish the isolated-worker contract.
type ONNXRuntimeAdapterOptions struct{}

// ONNXRuntimeAdapter verifies pinned assets and can run package-local
// conformance measurements. Its exported worker runtime remains unavailable
// until a separate process supervisor establishes the isolation contract.
type ONNXRuntimeAdapter struct {
	mu        sync.RWMutex
	admission ONNXWorkerAdmission
	backend   onnxRuntimeBackend
	closed    bool
}

type onnxRuntimeBackend interface {
	Probe(context.Context) (onnxRuntimeProbeFacts, error)
	Close() error
}

// onnxRuntimeTextBackend is an optional execution extension. Probe-only test
// backends intentionally do not implement it, so native bytes/session
// execution cannot be mistaken for a ready isolated worker.
type onnxRuntimeTextBackend interface {
	measureEmbedTextWithText(context.Context, EmbedTextRequest, []ONNXWorkerTextInput) (EmbedTextResultBatch, error)
}

type onnxRuntimeProbeFacts struct {
	RuntimeVersion string
	RuntimeCAPI    int
}

type validatedONNXRuntimeAssets struct {
	runtimeBytes   []byte
	runtimeVersion string
	runtimeDigest  string
	modelBytes     []byte
	tokenizerBytes []byte
}

type onnxRuntimeAdmissionError struct {
	reason string
	err    error
}

func (e *onnxRuntimeAdmissionError) Error() string { return e.err.Error() }
func (e *onnxRuntimeAdmissionError) Unwrap() error { return e.err }

// NewONNXRuntimeAdapter revalidates the complete offline bundle before any
// native loading. A previously admitted path is never trusted by itself.
func NewONNXRuntimeAdapter(ctx context.Context, admission ONNXWorkerAdmission, _ ONNXRuntimeAdapterOptions) (*ONNXRuntimeAdapter, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, err.Error())
	}
	assets, err := revalidateONNXRuntimeAdmission(ctx, admission)
	if err != nil {
		reason := ONNXWorkerReasonProfile
		var admissionErr *onnxRuntimeAdmissionError
		if errors.As(err, &admissionErr) {
			reason = admissionErr.reason
		}
		return nil, workerError(reason, ErrONNXRuntimeUnavailable, err.Error())
	}
	if err := ctx.Err(); err != nil {
		return nil, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, err.Error())
	}
	backend, err := newONNXRuntimeBackend(ctx, assets)
	if err != nil {
		return nil, err
	}
	return &ONNXRuntimeAdapter{admission: admission, backend: backend}, nil
}

func revalidateONNXRuntimeAdmission(ctx context.Context, admission ONNXWorkerAdmission) (validatedONNXRuntimeAssets, error) {
	if admission.Assets.Root == "" {
		return validatedONNXRuntimeAssets{}, &onnxRuntimeAdmissionError{reason: ONNXWorkerReasonBundle, err: errors.New("bundle root is unavailable")}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	bundle, err := search.LoadSemanticBundle(admission.Assets.Root)
	if err != nil {
		return validatedONNXRuntimeAssets{}, &onnxRuntimeAdmissionError{
			reason: ONNXWorkerReasonBundle,
			err:    fmt.Errorf("reload semantic bundle: %w", err),
		}
	}
	descriptor := bundle.Descriptor
	if descriptor.Runtime.Size == 0 || descriptor.Runtime.Size > maxONNXRuntimeAssetBytes ||
		descriptor.Model.Size == 0 || descriptor.Model.Size > maxONNXModelAssetBytes ||
		descriptor.Tokenizer.Size == 0 || descriptor.Tokenizer.Size > maxONNXTokenizerAssetBytes {
		return validatedONNXRuntimeAssets{}, &onnxRuntimeAdmissionError{
			reason: ONNXWorkerReasonBundle,
			err:    errors.New("runtime, model, or tokenizer exceeds the fixed BGE worker bounds"),
		}
	}
	if descriptor.ONNXGoBindingCommit != onnxRuntimeGoBindingCommit {
		return validatedONNXRuntimeAssets{}, fmt.Errorf("ONNX Go binding commit %q does not match pinned %q", descriptor.ONNXGoBindingCommit, onnxRuntimeGoBindingCommit)
	}
	if descriptor.ONNXGoBindingCAPI != onnxRuntimeGoBindingCAPI || descriptor.ONNXRuntimeCAPI != onnxRuntimeGoBindingCAPI || search.SemanticBundleONNXRuntimeCAPI != onnxRuntimeGoBindingCAPI {
		return validatedONNXRuntimeAssets{}, fmt.Errorf("ONNX Runtime and Go binding must use C API %d", onnxRuntimeGoBindingCAPI)
	}

	expected, err := AdmitONNXWorker(bundle, admission.ProfileDigest, admission.Platform, admission.Manifest.ConfigDigest)
	if err != nil {
		return validatedONNXRuntimeAssets{}, fmt.Errorf("recompute worker admission: %w", err)
	}
	expected.Assets = ONNXWorkerAssetPaths{
		Root:      admission.Assets.Root,
		Runtime:   filepath.Join(admission.Assets.Root, filepath.FromSlash(descriptor.Runtime.Path)),
		Model:     filepath.Join(admission.Assets.Root, filepath.FromSlash(descriptor.Model.Path)),
		Tokenizer: filepath.Join(admission.Assets.Root, filepath.FromSlash(descriptor.Tokenizer.Path)),
		Zvec:      filepath.Join(admission.Assets.Root, filepath.FromSlash(descriptor.Zvec.Path)),
	}
	if admission != expected {
		return validatedONNXRuntimeAssets{}, errors.New("worker admission does not match the reloaded bundle")
	}
	runtimeBytes, err := search.ReadSemanticBundleAsset(ctx, admission.Assets.Root, bundle, "runtime")
	if err != nil {
		return validatedONNXRuntimeAssets{}, &onnxRuntimeAdmissionError{reason: ONNXWorkerReasonBundle, err: err}
	}
	modelBytes, err := search.ReadSemanticBundleAsset(ctx, admission.Assets.Root, bundle, "model")
	if err != nil {
		return validatedONNXRuntimeAssets{}, &onnxRuntimeAdmissionError{reason: ONNXWorkerReasonBundle, err: err}
	}
	tokenizerBytes, err := search.ReadSemanticBundleAsset(ctx, admission.Assets.Root, bundle, "tokenizer")
	if err != nil {
		return validatedONNXRuntimeAssets{}, &onnxRuntimeAdmissionError{reason: ONNXWorkerReasonBundle, err: err}
	}
	runtimeSum := sha256.Sum256(runtimeBytes)
	return validatedONNXRuntimeAssets{
		runtimeBytes: runtimeBytes, runtimeVersion: descriptor.ONNXRuntimeVersion,
		runtimeDigest: "sha256:" + hex.EncodeToString(runtimeSum[:]), modelBytes: modelBytes, tokenizerBytes: tokenizerBytes,
	}, nil
}

// Probe reports native runtime facts that were actually observed, then remains
// typed-unavailable. ModelLoaded and TokenizerLoaded describe the isolated
// worker session and deliberately stay false for this in-process adapter.
func (a *ONNXRuntimeAdapter) Probe(ctx context.Context, admission ONNXWorkerAdmission) (ONNXWorkerProbeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed || a.backend == nil {
		return ONNXWorkerProbeResult{}, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, "adapter is closed")
	}
	if admission != a.admission {
		return ONNXWorkerProbeResult{}, workerError(ONNXWorkerReasonProfile, ErrONNXRuntimeUnavailable, "probe admission does not match adapter")
	}
	facts, err := a.backend.Probe(ctx)
	if err != nil {
		return ONNXWorkerProbeResult{}, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, err.Error())
	}
	return ONNXWorkerProbeResult{
		CapabilityID: ONNXWorkerCapabilityID, Protocol: ONNXWorkerProtocol,
		RuntimeVersion: facts.RuntimeVersion, RuntimeCAPI: facts.RuntimeCAPI,
		RuntimeDigest: admission.Manifest.RuntimeDigest, ModelDigest: admission.Manifest.ModelDigest,
		TokenizerDigest: admission.Manifest.TokenizerDigest, IsolationClass: onnxRuntimeProbeIsolation,
	}, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, "isolated worker transport is not installed")
}

// EmbedText cannot expose accepted output until Probe can satisfy the full
// isolated-worker contract. It returns the normal typed unavailable batch.
func (a *ONNXRuntimeAdapter) EmbedText(ctx context.Context, req EmbedTextRequest) (EmbedTextResultBatch, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed || a.backend == nil {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, "adapter is closed"))
	}
	if err := ctx.Err(); err != nil {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, err.Error()))
	}
	if err := ValidateEmbedTextRequest(req); err != nil {
		return EmbedTextResultBatch{}, workerError(ONNXWorkerReasonRequest, err, err.Error())
	}
	if err := validateONNXWorkerRequest(a.admission, req); err != nil {
		return unavailableEmbedTextBatch(req, err)
	}
	return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, "isolated worker transport is not installed"))
}

// EmbedTextWithText satisfies ONNXWorkerRuntime but cannot expose in-process
// output. A production caller reaches accepted vectors only through a
// separately supervised process transport that can satisfy Negotiate.
func (a *ONNXRuntimeAdapter) EmbedTextWithText(ctx context.Context, req EmbedTextRequest, inputs []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
	return a.EmbedText(ctx, req)
}

// measureEmbedTextWithText executes admitted bytes only for package-local
// conformance and supervisor integration. Keeping this method unexported
// prevents the in-process adapter from becoming an alternate READY path.
func (a *ONNXRuntimeAdapter) measureEmbedTextWithText(ctx context.Context, req EmbedTextRequest, inputs []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed || a.backend == nil {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, "adapter is closed"))
	}
	if err := ValidateEmbedTextRequest(req); err != nil {
		return EmbedTextResultBatch{}, workerError(ONNXWorkerReasonRequest, err, err.Error())
	}
	if err := validateONNXWorkerRequest(a.admission, req); err != nil {
		return unavailableEmbedTextBatch(req, err)
	}
	if err := validateONNXTextInputs(req, inputs); err != nil {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRequest, ErrONNXRuntimeUnavailable, err.Error()))
	}
	backend, ok := a.backend.(onnxRuntimeTextBackend)
	if !ok {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, "native model session is not installed"))
	}
	batch, err := backend.measureEmbedTextWithText(ctx, req, inputs)
	if err != nil {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, err.Error()))
	}
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonOutput, ErrONNXRuntimeUnavailable, err.Error()))
	}
	return batch, nil
}

func validateONNXTextInputs(req EmbedTextRequest, inputs []ONNXWorkerTextInput) error {
	if len(inputs) != len(req.Segments) {
		return fmt.Errorf("runtime received %d text inputs for %d segments", len(inputs), len(req.Segments))
	}
	for i, input := range inputs {
		segment := req.Segments[i]
		if input.SegmentID != segment.ID || !utf8.Valid(input.Text) || int64(len(input.Text)) != segment.TextBytes {
			return fmt.Errorf("runtime text input %q does not match host binding", input.SegmentID)
		}
		sum := sha256.Sum256(input.Text)
		if "sha256:"+hex.EncodeToString(sum[:]) != segment.TextDigest {
			return fmt.Errorf("runtime text input %q digest does not match host binding", input.SegmentID)
		}
	}
	return nil
}

// Close releases the native probe environment. It is idempotent and never
// modifies bundle assets or durable state.
func (a *ONNXRuntimeAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	if a.backend == nil {
		return nil
	}
	err := a.backend.Close()
	a.backend = nil
	if err != nil {
		return fmt.Errorf("close ONNX Runtime adapter: %w", err)
	}
	return nil
}
