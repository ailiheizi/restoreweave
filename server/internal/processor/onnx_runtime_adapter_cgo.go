//go:build cgo && (darwin || linux)

package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ailiheizi/restoreweave/server/internal/search"
	ort "github.com/yalue/onnxruntime_go"
)

type onnxRuntimeBackendNative struct {
	mu             sync.Mutex
	runtimeVersion string
	tokenizer      *bertTokenizer
	session        *ort.DynamicAdvancedSession
	closed         bool
}

var ortEnvironment = struct {
	sync.Mutex
	initialized bool
	stagingRoot string
	path        string
	digest      string
	version     string
	refs        int
}{}

func newONNXRuntimeBackend(ctx context.Context, assets validatedONNXRuntimeAssets) (onnxRuntimeBackend, error) {
	if err := ctx.Err(); err != nil {
		return nil, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, err.Error())
	}
	if err := acquireORTEnvironment(assets.runtimeBytes, assets.runtimeDigest, assets.runtimeVersion); err != nil {
		return nil, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, err.Error())
	}
	cleanup := func() { _ = releaseORTEnvironment() }
	tokenizer, err := loadBertTokenizer(assets.tokenizerBytes, search.SemanticBundleBGEMaxTokens)
	if err != nil {
		cleanup()
		return nil, workerError(ONNXWorkerReasonRuntimeMismatch, ErrONNXRuntimeUnavailable, err.Error())
	}
	if err := validateONNXModelInfo(assets.modelBytes); err != nil {
		cleanup()
		return nil, workerError(ONNXWorkerReasonRuntimeMismatch, ErrONNXRuntimeUnavailable, err.Error())
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return nil, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, err.Error())
	}
	session, err := ort.NewDynamicAdvancedSessionWithONNXData(assets.modelBytes,
		[]string{"input_ids", "attention_mask", "token_type_ids"}, []string{"last_hidden_state"}, nil)
	if err != nil {
		cleanup()
		return nil, workerError(ONNXWorkerReasonRuntimeMismatch, ErrONNXRuntimeUnavailable, fmt.Sprintf("create admitted model session: %v", err))
	}
	if err := ctx.Err(); err != nil {
		_ = session.Destroy()
		cleanup()
		return nil, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, err.Error())
	}
	return &onnxRuntimeBackendNative{runtimeVersion: assets.runtimeVersion, tokenizer: tokenizer, session: session}, nil
}

func validateONNXModelInfo(model []byte) error {
	inputs, outputs, err := ort.GetInputOutputInfoWithONNXData(model)
	if err != nil {
		return fmt.Errorf("inspect admitted ONNX model: %w", err)
	}
	wantInputs := map[string]bool{"input_ids": false, "attention_mask": false, "token_type_ids": false}
	if len(inputs) != len(wantInputs) {
		return fmt.Errorf("model has %d inputs, want %d", len(inputs), len(wantInputs))
	}
	for _, input := range inputs {
		seen, ok := wantInputs[input.Name]
		if !ok || seen || input.OrtValueType != ort.ONNXTypeTensor || input.DataType != ort.TensorElementDataTypeInt64 || len(input.Dimensions) != 2 {
			return fmt.Errorf("model input %q does not match int64 rank-2 BGE profile", input.Name)
		}
		wantInputs[input.Name] = true
	}
	if len(outputs) != 1 || outputs[0].Name != "last_hidden_state" || outputs[0].OrtValueType != ort.ONNXTypeTensor ||
		outputs[0].DataType != ort.TensorElementDataTypeFloat || len(outputs[0].Dimensions) != 3 || outputs[0].Dimensions[2] != search.SemanticBundleBGEDimension {
		return errors.New("model output does not match float32 rank-3 BGE profile")
	}
	return nil
}

func (b *onnxRuntimeBackendNative) Probe(ctx context.Context) (onnxRuntimeProbeFacts, error) {
	if err := ctx.Err(); err != nil {
		return onnxRuntimeProbeFacts{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return onnxRuntimeProbeFacts{}, errors.New("runtime probe is closed")
	}
	got := ort.GetVersion()
	if got != b.runtimeVersion {
		return onnxRuntimeProbeFacts{}, fmt.Errorf("loaded ONNX Runtime version %q does not match admitted %q", got, b.runtimeVersion)
	}
	return onnxRuntimeProbeFacts{RuntimeVersion: got, RuntimeCAPI: onnxRuntimeGoBindingCAPI}, nil
}

func (b *onnxRuntimeBackendNative) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	var sessionErr error
	if b.session != nil {
		sessionErr = b.session.Destroy()
		b.session = nil
	}
	b.tokenizer = nil
	return errors.Join(sessionErr, releaseORTEnvironment())
}

func (b *onnxRuntimeBackendNative) measureEmbedTextWithText(ctx context.Context, req EmbedTextRequest, inputs []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.session == nil || b.tokenizer == nil {
		return EmbedTextResultBatch{}, errors.New("native model session is closed")
	}
	if err := ValidateEmbedTextRequest(req); err != nil {
		return EmbedTextResultBatch{}, err
	}
	if err := validateONNXTextInputs(req, inputs); err != nil {
		return EmbedTextResultBatch{}, err
	}
	requestDigest, err := req.CanonicalDigest()
	if err != nil {
		return EmbedTextResultBatch{}, err
	}
	results := make([]EmbedTextResult, 0, len(inputs))
	var totalInputTokens int64
	var peakResourceBytes int64
	for i, input := range inputs {
		if err := ctx.Err(); err != nil {
			return EmbedTextResultBatch{}, err
		}
		text := string(input.Text)
		if req.Binding.Purpose == EmbedTextPurposeQuery {
			text = search.SemanticBundleBGEQueryPrefix + text
		}
		encoded, err := b.tokenizer.encode(text)
		if err != nil {
			return EmbedTextResultBatch{}, err
		}
		if int64(encoded.inputTokens) > req.MaxInputTokens-totalInputTokens {
			return EmbedTextResultBatch{}, errors.New("tokenized input exceeds request budget")
		}
		totalInputTokens += int64(encoded.inputTokens)
		vector, scratchBytes, err := b.runOne(ctx, encoded)
		if err != nil {
			return EmbedTextResultBatch{}, err
		}
		if scratchBytes > peakResourceBytes {
			peakResourceBytes = scratchBytes
		}
		segment := req.Segments[i]
		coverage := EmbedTextCoverageFull
		if encoded.truncated {
			coverage = EmbedTextCoverageTruncated
		}
		results = append(results, EmbedTextResult{
			Binding: req.Binding, SegmentID: segment.ID, Source: segment.Source,
			DescriptionDocumentID: segment.DescriptionDocumentID, Ordinal: segment.Ordinal,
			SubjectRef: segment.SubjectRef, Language: segment.Language, TextHandleID: segment.TextHandleID,
			TextDigest: segment.TextDigest, Status: EmbedTextAccepted, Vector: vector,
			ElementType: req.Profile.ElementType, Dimension: req.Profile.Dimension,
			Normalization: req.Profile.Normalization, Pooling: req.Profile.Pooling,
			SemanticProfileDigest: req.Profile.SemanticProfileDigest, ConfigDigest: req.Profile.ConfigDigest,
			PreprocessingDigest: req.Binding.AppliedPreprocessingDigest, ModelDigest: req.Profile.ModelDigest,
			TokenizerDigest: req.Profile.TokenizerDigest, RuntimeDigest: req.Profile.RuntimeDigest,
			SemanticSpace: req.Profile.SemanticSpace, DeterminismClass: req.Profile.DeterminismClass,
			Coverage: coverage, InputTokens: int64(encoded.inputTokens), EmbeddedTokens: int64(len(encoded.ids)),
		})
	}
	return EmbedTextResultBatch{
		Binding: req.Binding, RequestDigest: requestDigest, PeakResourceBytes: peakResourceBytes,
		ResourceScope: req.ResourceScope, Results: results,
	}, nil
}

func (b *onnxRuntimeBackendNative) runOne(ctx context.Context, encoded bertEncoded) ([]float32, int64, error) {
	shape := ort.NewShape(1, int64(len(encoded.ids)))
	inputIDs, err := ort.NewTensor(shape, encoded.ids)
	if err != nil {
		return nil, 0, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer inputIDs.Destroy()
	attention, err := ort.NewTensor(shape, encoded.attention)
	if err != nil {
		return nil, 0, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer attention.Destroy()
	tokenTypes, err := ort.NewTensor(shape, encoded.tokenTypes)
	if err != nil {
		return nil, 0, fmt.Errorf("create token_type_ids tensor: %w", err)
	}
	defer tokenTypes.Destroy()

	outputs := []ort.Value{nil}
	runOptions, err := ort.NewRunOptions()
	if err != nil {
		return nil, 0, fmt.Errorf("create ONNX run options: %w", err)
	}
	runDone := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			_ = runOptions.Terminate()
		case <-runDone:
		}
	}()
	runErr := b.session.RunWithOptions([]ort.Value{inputIDs, attention, tokenTypes}, outputs, runOptions)
	close(runDone)
	<-watchDone
	destroyOptionsErr := runOptions.Destroy()
	if outputs[0] != nil {
		defer outputs[0].Destroy()
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if runErr != nil {
		return nil, 0, fmt.Errorf("run admitted ONNX model: %w", runErr)
	}
	if destroyOptionsErr != nil {
		return nil, 0, fmt.Errorf("destroy ONNX run options: %w", destroyOptionsErr)
	}
	output, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, 0, errors.New("model returned a non-float32 tensor")
	}
	gotShape := output.GetShape()
	if len(gotShape) != 3 || gotShape[0] != 1 || gotShape[1] != int64(len(encoded.ids)) || gotShape[2] != search.SemanticBundleBGEDimension {
		return nil, 0, fmt.Errorf("model output shape %v does not match [1,%d,%d]", gotShape, len(encoded.ids), search.SemanticBundleBGEDimension)
	}
	data := output.GetData()
	wantElements := len(encoded.ids) * search.SemanticBundleBGEDimension
	if len(data) != wantElements {
		return nil, 0, fmt.Errorf("model output has %d elements, want %d", len(data), wantElements)
	}
	vector := append([]float32(nil), data[:search.SemanticBundleBGEDimension]...)
	var normSquared float64
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, 0, errors.New("model output contains a non-finite value")
		}
		normSquared += float64(value) * float64(value)
	}
	norm := math.Sqrt(normSquared)
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return nil, 0, errors.New("model output has an invalid CLS norm")
	}
	for i := range vector {
		vector[i] = float32(float64(vector[i]) / norm)
	}
	scratchBytes := int64(len(encoded.ids))*3*8 + int64(len(data))*4 + int64(len(vector))*4
	return vector, scratchBytes, nil
}

func acquireORTEnvironment(runtimeBytes []byte, expectedDigest, expectedVersion string) error {
	ortEnvironment.Lock()
	defer ortEnvironment.Unlock()
	if ortEnvironment.initialized {
		if ortEnvironment.digest != expectedDigest || ortEnvironment.version != expectedVersion {
			return errors.New("another ONNX Runtime bundle is already initialized")
		}
		ortEnvironment.refs++
		return nil
	}
	stagingRoot, path, err := stageVerifiedONNXRuntime(runtimeBytes, expectedDigest)
	if err != nil {
		return err
	}
	cleanup := func() { _ = removeONNXRuntimeStage(stagingRoot) }
	ort.SetSharedLibraryPath(path)
	if err := ort.InitializeEnvironment(); err != nil {
		cleanup()
		return fmt.Errorf("initialize pinned ONNX Runtime: %w", err)
	}
	got := ort.GetVersion()
	if got != expectedVersion {
		_ = ort.DestroyEnvironment()
		cleanup()
		return fmt.Errorf("loaded ONNX Runtime version %q does not match admitted %q", got, expectedVersion)
	}
	ortEnvironment.initialized = true
	ortEnvironment.stagingRoot = stagingRoot
	ortEnvironment.path = path
	ortEnvironment.digest = expectedDigest
	ortEnvironment.version = got
	ortEnvironment.refs = 1
	return nil
}

func releaseORTEnvironment() error {
	ortEnvironment.Lock()
	defer ortEnvironment.Unlock()
	if !ortEnvironment.initialized {
		return nil
	}
	if ortEnvironment.refs > 1 {
		ortEnvironment.refs--
		return nil
	}
	destroyErr := ort.DestroyEnvironment()
	cleanupErr := removeONNXRuntimeStage(ortEnvironment.stagingRoot)
	ortEnvironment.initialized = false
	ortEnvironment.stagingRoot = ""
	ortEnvironment.path = ""
	ortEnvironment.digest = ""
	ortEnvironment.version = ""
	ortEnvironment.refs = 0
	return errors.Join(destroyErr, cleanupErr)
}

// stageVerifiedONNXRuntime gives dlopen a host-owned immutable pathname. The
// bytes were read from a no-follow bundle descriptor and are rehashed before
// and after the exclusive copy; the original mutable bundle path is never
// passed to the native loader.
func stageVerifiedONNXRuntime(runtimeBytes []byte, expectedDigest string) (root, path string, err error) {
	if len(runtimeBytes) == 0 {
		return "", "", errors.New("verified ONNX Runtime bytes are empty")
	}
	sum := sha256.Sum256(runtimeBytes)
	if "sha256:"+hex.EncodeToString(sum[:]) != expectedDigest {
		return "", "", errors.New("verified ONNX Runtime bytes do not match admitted digest")
	}
	root, err = os.MkdirTemp("", "restoreweave-onnx-runtime-")
	if err != nil {
		return "", "", fmt.Errorf("create ONNX Runtime staging: %w", err)
	}
	defer func() {
		if err != nil {
			_ = removeONNXRuntimeStage(root)
		}
	}()
	name := "libonnxruntime.so"
	if runtime.GOOS == "darwin" {
		name = "libonnxruntime.dylib"
	}
	path = filepath.Join(root, name)
	file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o500)
	if openErr != nil {
		return "", "", fmt.Errorf("create ONNX Runtime copy: %w", openErr)
	}
	written, writeErr := file.Write(runtimeBytes)
	if writeErr != nil {
		_ = file.Close()
		return "", "", fmt.Errorf("write ONNX Runtime copy: %w", writeErr)
	}
	if written != len(runtimeBytes) {
		_ = file.Close()
		return "", "", io.ErrShortWrite
	}
	if syncErr := file.Sync(); syncErr != nil {
		_ = file.Close()
		return "", "", fmt.Errorf("sync ONNX Runtime copy: %w", syncErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		return "", "", fmt.Errorf("close ONNX Runtime copy: %w", closeErr)
	}
	copyFile, openErr := os.Open(path)
	if openErr != nil {
		return "", "", fmt.Errorf("reopen ONNX Runtime copy: %w", openErr)
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, copyFile)
	closeErr := copyFile.Close()
	if copyErr != nil {
		return "", "", fmt.Errorf("hash ONNX Runtime copy: %w", copyErr)
	}
	if closeErr != nil {
		return "", "", fmt.Errorf("close verified ONNX Runtime copy: %w", closeErr)
	}
	if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != expectedDigest {
		return "", "", errors.New("staged ONNX Runtime copy does not match admitted digest")
	}
	if err = os.Chmod(root, 0o500); err != nil {
		return "", "", fmt.Errorf("seal ONNX Runtime staging: %w", err)
	}
	return root, path, nil
}

func removeONNXRuntimeStage(root string) error {
	if root == "" {
		return nil
	}
	if err := os.Chmod(root, 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.RemoveAll(root)
}
