package processor

// This file is deliberately an admission and execution boundary, not an ONNX
// runtime implementation. The reference runtime is a separately retained,
// platform-specific package. Until that package is loaded and probed, this
// worker reports UNAVAILABLE and can only return typed provider failures.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

const (
	// ONNXWorkerCapabilityID is a stable capability identity. It is not a
	// readiness claim and is never registered by DefaultProcessors.
	ONNXWorkerCapabilityID          = "embed.text.onnx.bge-small-zh-v1.5.v1"
	ONNXWorkerProtocol              = "restoreweave.processor.embed-text.v1"
	ONNXWorkerSchema                = "restoreweave.processor.embed-text-result.v1"
	ONNXWorkerReasonBundle          = "BUNDLE_UNAVAILABLE"
	ONNXWorkerReasonProfile         = "PROFILE_MISMATCH"
	ONNXWorkerReasonRuntime         = "RUNTIME_UNAVAILABLE"
	ONNXWorkerReasonRuntimeMismatch = "RUNTIME_INCOMPATIBLE"
	ONNXWorkerReasonProtocol        = "PROTOCOL_UNSUPPORTED"
	ONNXWorkerReasonSchema          = "SCHEMA_UNSUPPORTED"
	ONNXWorkerReasonPlatform        = "PLATFORM_UNSUPPORTED"
	ONNXWorkerReasonBudget          = "BUDGET_UNSUPPORTED"
	ONNXWorkerReasonRequest         = "REQUEST_INVALID"
	ONNXWorkerReasonOutput          = "OUTPUT_INVALID"
)

// ONNXWorkerState is intentionally separate from search.ProviderReadiness.
// Admission proves only that immutable assets match a trusted profile;
// READY additionally requires an independently probed runtime.
type ONNXWorkerState string

const (
	ONNXWorkerStateUnavailable ONNXWorkerState = "UNAVAILABLE"
	ONNXWorkerStateAdmitted    ONNXWorkerState = "ADMITTED"
	ONNXWorkerStateReady       ONNXWorkerState = "READY"
)

// ONNXWorkerCapability is safe to expose in diagnostics. It contains profile
// identities and a typed state, but no paths, credentials, or model bytes.
type ONNXWorkerCapability struct {
	CapabilityID    string          `json:"capability_id"`
	Protocol        string          `json:"protocol"`
	Schema          string          `json:"schema"`
	State           ONNXWorkerState `json:"state"`
	ReasonCode      string          `json:"reason_code,omitempty"`
	ProfileDigest   string          `json:"profile_digest,omitempty"`
	WorkerDigest    string          `json:"worker_digest,omitempty"`
	ConfigDigest    string          `json:"config_digest,omitempty"`
	RuntimeDigest   string          `json:"runtime_digest,omitempty"`
	ModelDigest     string          `json:"model_digest,omitempty"`
	TokenizerDigest string          `json:"tokenizer_digest,omitempty"`
	CAPI            int             `json:"onnx_runtime_c_api,omitempty"`
}

// ONNXWorkerPlatform is the host tuple bound during admission and negotiation.
type ONNXWorkerPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

func CurrentONNXWorkerPlatform() ONNXWorkerPlatform {
	return ONNXWorkerPlatform{OS: runtime.GOOS, Arch: runtime.GOARCH}
}

// ONNXWorkerAdmission is the immutable result of bundle/profile admission.
// It is not executable until Negotiate succeeds with a runtime probe.
type ONNXWorkerAdmission struct {
	ProfileDigest               string                             `json:"profile_digest"`
	WorkerDigest                string                             `json:"worker_digest"`
	QueryPreprocessingDigest    string                             `json:"query_preprocessing_digest"`
	DocumentPreprocessingDigest string                             `json:"document_preprocessing_digest"`
	RuntimeVersion              string                             `json:"runtime_version"`
	ModelID                     string                             `json:"model_id"`
	ModelRevision               string                             `json:"model_revision"`
	Manifest                    search.EmbeddingGenerationManifest `json:"manifest"`
	Platform                    ONNXWorkerPlatform                 `json:"platform"`
	Capability                  ONNXWorkerCapability               `json:"capability"`
	Assets                      ONNXWorkerAssetPaths               `json:"-"`
}

// ONNXWorkerAssetPaths names only files inside the already-admitted bundle.
// They are never included in capability/status JSON or an EmbedText request.
type ONNXWorkerAssetPaths struct {
	Root      string
	Runtime   string
	Model     string
	Tokenizer string
	Zvec      string
}

// ONNXWorkerProbeResult is measured after the isolated runtime opens the
// admitted assets and creates the model/tokenizer session.
type ONNXWorkerProbeResult struct {
	CapabilityID      string
	Protocol          string
	RuntimeVersion    string
	RuntimeCAPI       int
	RuntimeDigest     string
	ModelDigest       string
	TokenizerDigest   string
	ModelLoaded       bool
	TokenizerLoaded   bool
	IsolationClass    string
	InputNames        []string
	InputElementType  string
	InputRank         int
	OutputName        string
	OutputElementType string
	OutputRank        int
	OutputDimension   int
	MaxTokens         int
}

const ONNXWorkerIsolationProcess = "ISOLATED_PROCESS"

// ONNXWorkerTextInput is the only text payload a runtime may receive. It is a
// bounded copy resolved by the host; it contains no path, repository, or
// credential reference.
type ONNXWorkerTextInput struct {
	SegmentID string
	Text      []byte
}

// ONNXWorkerTextRuntime is the execution seam for a real worker.
type ONNXWorkerTextRuntime interface {
	EmbedTextWithText(context.Context, EmbedTextRequest, []ONNXWorkerTextInput) (EmbedTextResultBatch, error)
}

// ONNXWorkerRuntime is the narrow host boundary for a future real runtime.
// Implementations must load only the already-admitted bundle and must not
// access ambient paths, repositories, zvec, or credentials.
type ONNXWorkerRuntime interface {
	ONNXWorkerTextRuntime
	Probe(context.Context, ONNXWorkerAdmission) (ONNXWorkerProbeResult, error)
}

// onnxWorkerProcessAttestation is created only after the host-owned process
// supervisor completes the nonce, peer, MAC, liveness, and runtime checks.
type onnxWorkerProcessAttestation struct {
	identity *onnxWorkerProcessIdentity
}

type onnxWorkerProcessIdentity struct{ _ byte }

func (a *onnxWorkerProcessAttestation) matches(runtime ONNXWorkerRuntime) bool {
	if a == nil || a.identity == nil || runtime == nil {
		return false
	}
	attested, ok := runtime.(interface {
		onnxWorkerProcessIdentity() *onnxWorkerProcessIdentity
	})
	return ok && attested.onnxWorkerProcessIdentity() == a.identity
}

// ONNXWorkerHostCapabilities describes the host protocol and bounded quotas.
// A runtime is deliberately injected so a missing/unsupported runtime cannot
// be mistaken for a fixture or a successful semantic provider.
type ONNXWorkerHostCapabilities struct {
	Protocol       string
	Schema         string
	Platform       ONNXWorkerPlatform
	MaxInputBytes  int64
	MaxInputTokens int64
	MaxOutputBytes int64
	Runtime        ONNXWorkerRuntime
	TextHandles    TextHandleStore
	processProof   *onnxWorkerProcessAttestation
}

// NegotiatedONNXWorker is executable only after admission and runtime probe.
type NegotiatedONNXWorker struct {
	Admission      ONNXWorkerAdmission
	maxInputBytes  int64
	maxInputTokens int64
	maxOutputBytes int64
	runtime        ONNXWorkerRuntime
	textHandles    TextHandleStore
	processProof   *onnxWorkerProcessAttestation
}

// ONNXWorkerError retains a machine-readable reason while allowing callers to
// use errors.Is for the broad admission/availability class.
type ONNXWorkerError struct {
	ReasonCode string
	Err        error
}

func (e *ONNXWorkerError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return "ONNX worker " + e.ReasonCode
	}
	return "ONNX worker " + e.ReasonCode + ": " + e.Err.Error()
}

func (e *ONNXWorkerError) Unwrap() error { return e.Err }

var (
	ErrONNXWorkerUnavailable = errors.New("ONNX worker is unavailable")
	ErrONNXWorkerAdmission   = errors.New("ONNX worker admission failed")
	ErrONNXWorkerNegotiation = errors.New("ONNX worker negotiation failed")
)

func workerError(reason string, base error, detail string) error {
	if base == nil {
		base = ErrONNXWorkerUnavailable
	}
	if detail != "" {
		base = fmt.Errorf("%w: %s", base, detail)
	}
	return &ONNXWorkerError{ReasonCode: reason, Err: base}
}

// LoadONNXWorkerAdmission performs strict no-follow bundle loading through
// the search package's offline admission path. No current-directory lookup,
// download, runtime initialization, or model execution occurs here.
func LoadONNXWorkerAdmission(bundleRoot, expectedProfileDigest, configDigest string) (ONNXWorkerAdmission, error) {
	if bundleRoot == "" || strings.TrimSpace(bundleRoot) != bundleRoot {
		return ONNXWorkerAdmission{}, workerError(ONNXWorkerReasonBundle, ErrONNXWorkerAdmission, "absolute bundle root is required")
	}
	bundle, err := search.LoadSemanticBundle(bundleRoot)
	if err != nil {
		return ONNXWorkerAdmission{}, workerError(ONNXWorkerReasonBundle, ErrONNXWorkerAdmission, err.Error())
	}
	admission, err := AdmitONNXWorker(bundle, expectedProfileDigest, CurrentONNXWorkerPlatform(), configDigest)
	if err != nil {
		return ONNXWorkerAdmission{}, err
	}
	admission.Assets = ONNXWorkerAssetPaths{
		Root:      bundleRoot,
		Runtime:   filepath.Join(bundleRoot, filepath.FromSlash(bundle.Descriptor.Runtime.Path)),
		Model:     filepath.Join(bundleRoot, filepath.FromSlash(bundle.Descriptor.Model.Path)),
		Tokenizer: filepath.Join(bundleRoot, filepath.FromSlash(bundle.Descriptor.Tokenizer.Path)),
		Zvec:      filepath.Join(bundleRoot, filepath.FromSlash(bundle.Descriptor.Zvec.Path)),
	}
	return admission, nil
}

// AdmitONNXWorker validates the trusted profile, platform, and all measured
// asset identities. The fixed BGE facts are already enforced by
// search.SemanticBundleAdmission; this function additionally requires the
// profile to produce a complete generation manifest.
func AdmitONNXWorker(bundle search.SemanticBundleAdmission, expectedProfileDigest string, platform ONNXWorkerPlatform, configDigest string) (ONNXWorkerAdmission, error) {
	if err := bundle.VerifyPinnedProfile(strings.TrimSpace(expectedProfileDigest)); err != nil {
		return ONNXWorkerAdmission{}, workerError(ONNXWorkerReasonProfile, ErrONNXWorkerAdmission, err.Error())
	}
	if platform.OS == "" || platform.Arch == "" || platform.OS != bundle.Descriptor.PlatformOS || platform.Arch != bundle.Descriptor.PlatformArch {
		return ONNXWorkerAdmission{}, workerError(ONNXWorkerReasonPlatform, ErrONNXWorkerAdmission, "bundle platform does not match host")
	}
	manifest, err := bundle.EmbeddingGenerationManifest(strings.TrimSpace(configDigest))
	if err != nil {
		return ONNXWorkerAdmission{}, workerError(ONNXWorkerReasonProfile, ErrONNXWorkerAdmission, err.Error())
	}
	if manifest.Dimension != search.SemanticBundleBGEDimension || manifest.ElementType != search.SemanticBundleBGEElementType ||
		manifest.Pooling != search.SemanticBundleBGEPooling || manifest.Normalization != search.SemanticBundleBGENormalization ||
		manifest.SemanticSpace != search.SemanticBundleBGESemanticSpace || manifest.Distance != search.SemanticBundleBGEDistance ||
		bundle.Descriptor.ONNXRuntimeCAPI != search.SemanticBundleONNXRuntimeCAPI {
		return ONNXWorkerAdmission{}, workerError(ONNXWorkerReasonProfile, ErrONNXWorkerAdmission, "fixed BGE output or C API facts do not match")
	}
	queryPreprocessingDigest, err := onnxWorkerPreprocessingDigest(bundle, EmbedTextPurposeQuery)
	if err != nil {
		return ONNXWorkerAdmission{}, workerError(ONNXWorkerReasonProfile, ErrONNXWorkerAdmission, err.Error())
	}
	documentPreprocessingDigest, err := onnxWorkerPreprocessingDigest(bundle, EmbedTextPurposeDocument)
	if err != nil {
		return ONNXWorkerAdmission{}, workerError(ONNXWorkerReasonProfile, ErrONNXWorkerAdmission, err.Error())
	}
	workerDigest := "sha256:" + bundle.AssetDigests["onnx_binding"]
	if err := ValidateEmbedTextDigest(workerDigest); err != nil {
		return ONNXWorkerAdmission{}, workerError(ONNXWorkerReasonProfile, ErrONNXWorkerAdmission, "worker binding digest is invalid")
	}
	capability := ONNXWorkerCapability{
		CapabilityID:    ONNXWorkerCapabilityID,
		Protocol:        ONNXWorkerProtocol,
		Schema:          ONNXWorkerSchema,
		State:           ONNXWorkerStateAdmitted,
		ProfileDigest:   bundle.ProfileDigest,
		WorkerDigest:    workerDigest,
		ConfigDigest:    manifest.ConfigDigest,
		RuntimeDigest:   manifest.RuntimeDigest,
		ModelDigest:     manifest.ModelDigest,
		TokenizerDigest: manifest.TokenizerDigest,
		CAPI:            bundle.Descriptor.ONNXRuntimeCAPI,
	}
	return ONNXWorkerAdmission{
		ProfileDigest: bundle.ProfileDigest, WorkerDigest: workerDigest,
		QueryPreprocessingDigest: queryPreprocessingDigest, DocumentPreprocessingDigest: documentPreprocessingDigest,
		RuntimeVersion: bundle.Descriptor.ONNXRuntimeVersion, ModelID: bundle.Descriptor.ModelID,
		ModelRevision: bundle.Descriptor.ModelRevision, Manifest: manifest, Platform: platform, Capability: capability,
	}, nil
}

func onnxWorkerPreprocessingDigest(bundle search.SemanticBundleAdmission, purpose EmbedTextPurpose) (string, error) {
	prefix := bundle.Descriptor.DocumentPrefix
	if purpose == EmbedTextPurposeQuery {
		prefix = bundle.Descriptor.QueryPrefix
	} else if purpose != EmbedTextPurposeDocument {
		return "", fmt.Errorf("unsupported preprocessing purpose %q", purpose)
	}
	payload, err := json.Marshal(struct {
		Purpose             EmbedTextPurpose `json:"purpose"`
		BundlePreprocessing string           `json:"bundle_preprocessing_digest"`
		TokenizerDigest     string           `json:"tokenizer_digest"`
		Prefix              string           `json:"prefix"`
		MaxTokens           int              `json:"max_tokens"`
	}{purpose, bundle.Descriptor.PreprocessingDigest, "sha256:" + bundle.AssetDigests["tokenizer"], prefix, bundle.Descriptor.MaxTokens})
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("restoreweave.onnx-worker-preprocessing.v1\n"))
	_, _ = h.Write(payload)
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// Negotiate checks protocol/schema/platform/quotas and then performs the
// runtime health probe. A missing runtime always fails closed.
func (a ONNXWorkerAdmission) Negotiate(ctx context.Context, host ONNXWorkerHostCapabilities) (NegotiatedONNXWorker, error) {
	if err := ctx.Err(); err != nil {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonRuntime, ErrONNXWorkerNegotiation, err.Error())
	}
	if a.ProfileDigest == "" || a.Capability.State != ONNXWorkerStateAdmitted {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonProfile, ErrONNXWorkerNegotiation, "admitted profile is missing")
	}
	if host.Protocol != ONNXWorkerProtocol {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonProtocol, ErrONNXWorkerNegotiation, "unsupported protocol")
	}
	if host.Schema != ONNXWorkerSchema {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonSchema, ErrONNXWorkerNegotiation, "unsupported result schema")
	}
	if host.Platform != a.Platform {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonPlatform, ErrONNXWorkerNegotiation, "platform mismatch")
	}
	if host.MaxInputBytes <= 0 || host.MaxInputBytes > MaxEmbedTextInputBytes || host.MaxInputTokens <= 0 || host.MaxInputTokens > MaxEmbedTextInputTokens || host.MaxOutputBytes <= 0 || host.MaxOutputBytes > MaxEmbedTextOutputBytes {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonBudget, ErrONNXWorkerNegotiation, "host quotas are outside EMBED_TEXT bounds")
	}
	if host.Runtime == nil {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonRuntime, ErrONNXWorkerUnavailable, "runtime executor is not installed")
	}
	if !host.processProof.matches(host.Runtime) {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonRuntime, ErrONNXWorkerUnavailable, "runtime process isolation was not established by the host supervisor")
	}
	if host.TextHandles == nil {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonRuntime, ErrONNXWorkerUnavailable, "host text-handle store is not installed")
	}
	probe, err := host.Runtime.Probe(ctx, a)
	if err != nil {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonRuntime, ErrONNXWorkerUnavailable, err.Error())
	}
	if err := validateONNXWorkerProbe(a, probe); err != nil {
		return NegotiatedONNXWorker{}, workerError(ONNXWorkerReasonRuntimeMismatch, ErrONNXWorkerUnavailable, err.Error())
	}
	a.Capability.State = ONNXWorkerStateReady
	a.Capability.ReasonCode = ""
	return NegotiatedONNXWorker{
		Admission:      a,
		runtime:        host.Runtime,
		maxInputBytes:  host.MaxInputBytes,
		maxInputTokens: host.MaxInputTokens,
		maxOutputBytes: host.MaxOutputBytes,
		textHandles:    host.TextHandles,
		processProof:   host.processProof,
	}, nil
}

func validateONNXWorkerProbe(admission ONNXWorkerAdmission, probe ONNXWorkerProbeResult) error {
	if admission.Assets.Root == "" || admission.Assets.Runtime == "" || admission.Assets.Model == "" || admission.Assets.Tokenizer == "" {
		return errors.New("runtime asset paths are unavailable")
	}
	if probe.CapabilityID != ONNXWorkerCapabilityID || probe.Protocol != ONNXWorkerProtocol ||
		probe.RuntimeVersion != admission.RuntimeVersion || probe.RuntimeCAPI != search.SemanticBundleONNXRuntimeCAPI ||
		probe.RuntimeDigest != admission.Manifest.RuntimeDigest || probe.ModelDigest != admission.Manifest.ModelDigest ||
		probe.TokenizerDigest != admission.Manifest.TokenizerDigest || !probe.ModelLoaded || !probe.TokenizerLoaded ||
		probe.IsolationClass != ONNXWorkerIsolationProcess || probe.InputElementType != "int64" || probe.InputRank != 2 ||
		probe.OutputName != "last_hidden_state" || probe.OutputElementType != "float32" || probe.OutputRank != 3 ||
		probe.OutputDimension != admission.Manifest.Dimension || probe.MaxTokens != search.SemanticBundleBGEMaxTokens {
		return errors.New("runtime probe does not match admitted BGE profile")
	}
	wantInputs := map[string]bool{"input_ids": false, "attention_mask": false, "token_type_ids": false}
	if len(probe.InputNames) != len(wantInputs) {
		return errors.New("runtime probe has unexpected model inputs")
	}
	for _, name := range probe.InputNames {
		seen, ok := wantInputs[name]
		if !ok || seen {
			return errors.New("runtime probe has unexpected or duplicate model inputs")
		}
		wantInputs[name] = true
	}
	return nil
}

// EmbedText validates the host request against the admitted profile before
// invoking the runtime. Runtime output is independently validated by the
// existing host-side result contract; invalid output can never be accepted.
func (w NegotiatedONNXWorker) EmbedText(ctx context.Context, req EmbedTextRequest) (EmbedTextResultBatch, error) {
	if w.runtime == nil || w.Admission.Capability.State != ONNXWorkerStateReady || !w.processProof.matches(w.runtime) {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRuntime, ErrONNXWorkerUnavailable, "worker is not ready"))
	}
	if err := ValidateEmbedTextRequest(req); err != nil {
		return EmbedTextResultBatch{}, workerError(ONNXWorkerReasonRequest, err, err.Error())
	}
	if err := validateONNXWorkerRequest(w.Admission, req); err != nil {
		return unavailableEmbedTextBatch(req, err)
	}
	if err := validateONNXWorkerQuotas(req, w.maxInputBytes, w.maxInputTokens, w.maxOutputBytes); err != nil {
		return unavailableEmbedTextBatch(req, err)
	}
	inputs := make([]ONNXWorkerTextInput, 0, len(req.Segments))
	for _, segment := range req.Segments {
		text, err := w.textHandles.Consume(ctx, segment.TextHandleID, req.Binding, segment.TextDigest, segment.TextBytes)
		if err != nil {
			return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRequest, ErrONNXWorkerUnavailable, err.Error()))
		}
		if err := validateResolvedONNXText(segment, text); err != nil {
			return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRequest, ErrONNXWorkerUnavailable, err.Error()))
		}
		inputs = append(inputs, ONNXWorkerTextInput{SegmentID: segment.ID, Text: text})
	}
	batch, err := w.runtime.EmbedTextWithText(ctx, req, inputs)
	if err != nil {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonRuntime, ErrONNXWorkerUnavailable, err.Error()))
	}
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		return unavailableEmbedTextBatch(req, workerError(ONNXWorkerReasonOutput, ErrONNXWorkerUnavailable, err.Error()))
	}
	return batch, nil
}

func validateResolvedONNXText(segment EmbedTextSegment, text []byte) error {
	if !utf8.Valid(text) {
		return errors.New("resolved text is not valid UTF-8")
	}
	if int64(len(text)) != segment.TextBytes {
		return errors.New("resolved text length does not match request")
	}
	sum := sha256.Sum256(text)
	if "sha256:"+hex.EncodeToString(sum[:]) != segment.TextDigest {
		return errors.New("resolved text digest does not match request")
	}
	return nil
}

func validateONNXWorkerQuotas(req EmbedTextRequest, maxInputBytes, maxInputTokens, maxOutputBytes int64) error {
	if maxInputBytes <= 0 || maxInputTokens <= 0 || maxOutputBytes <= 0 {
		return workerError(ONNXWorkerReasonBudget, ErrONNXWorkerUnavailable, "negotiated quotas are unavailable")
	}
	var inputBytes int64
	for _, segment := range req.Segments {
		if inputBytes > maxInputBytes-segment.TextBytes {
			return workerError(ONNXWorkerReasonBudget, ErrONNXWorkerUnavailable, "request input exceeds negotiated quota")
		}
		inputBytes += segment.TextBytes
	}
	if req.MaxInputBytes > maxInputBytes || req.MaxInputTokens > maxInputTokens || req.MaxOutputBytes > maxOutputBytes {
		return workerError(ONNXWorkerReasonBudget, ErrONNXWorkerUnavailable, "request budgets exceed negotiated quotas")
	}
	return nil
}

func validateONNXWorkerRequest(admission ONNXWorkerAdmission, req EmbedTextRequest) error {
	want := admission.Manifest
	if req.Binding.WorkerDigest != admission.WorkerDigest || req.Binding.WorkerProfileDigest != admission.ProfileDigest ||
		req.Profile.SemanticProfileDigest != admission.ProfileDigest ||
		req.Profile.ConfigDigest != want.ConfigDigest || req.Profile.ModelDigest != want.ModelDigest ||
		req.Profile.TokenizerDigest != want.TokenizerDigest || req.Profile.RuntimeDigest != want.RuntimeDigest ||
		req.Profile.SemanticSpace != want.SemanticSpace || req.Profile.Dimension != want.Dimension ||
		req.Profile.ElementType != want.ElementType || req.Profile.Pooling != want.Pooling || req.Profile.Normalization != want.Normalization ||
		req.Profile.DeterminismClass != EmbedTextDeterminismSemantic ||
		req.Profile.QueryPreprocessingDigest != admission.QueryPreprocessingDigest ||
		req.Profile.DocumentPreprocessingDigest != admission.DocumentPreprocessingDigest {
		return workerError(ONNXWorkerReasonProfile, ErrONNXWorkerUnavailable, "request profile does not match admitted bundle")
	}
	return nil
}

func unavailableEmbedTextBatch(req EmbedTextRequest, reason error) (EmbedTextResultBatch, error) {
	if err := ValidateEmbedTextRequest(req); err != nil {
		return EmbedTextResultBatch{}, reason
	}
	digest, err := req.CanonicalDigest()
	if err != nil {
		return EmbedTextResultBatch{}, reason
	}
	message := "semantic provider unavailable"
	if reason != nil {
		message = reason.Error()
	}
	message = boundedIssueText(message)
	results := make([]EmbedTextResult, 0, len(req.Segments))
	for _, segment := range req.Segments {
		results = append(results, EmbedTextResult{
			Binding: req.Binding, SegmentID: segment.ID, Source: segment.Source,
			DescriptionDocumentID: segment.DescriptionDocumentID, Ordinal: segment.Ordinal,
			SubjectRef: segment.SubjectRef, Language: segment.Language, TextHandleID: segment.TextHandleID,
			TextDigest: segment.TextDigest, Status: EmbedTextFailed, SemanticProfileDigest: req.Profile.SemanticProfileDigest,
			ConfigDigest: req.Profile.ConfigDigest, PreprocessingDigest: req.Binding.AppliedPreprocessingDigest,
			ModelDigest: req.Profile.ModelDigest, TokenizerDigest: req.Profile.TokenizerDigest,
			RuntimeDigest: req.Profile.RuntimeDigest, SemanticSpace: req.Profile.SemanticSpace,
			ElementType: req.Profile.ElementType, Dimension: req.Profile.Dimension, Normalization: req.Profile.Normalization,
			Pooling: req.Profile.Pooling, DeterminismClass: req.Profile.DeterminismClass,
			Coverage: EmbedTextCoverageNone, FailureCode: "PROVIDER_UNAVAILABLE", Reason: message,
		})
	}
	return EmbedTextResultBatch{Binding: req.Binding, RequestDigest: digest, ResourceScope: req.ResourceScope, Results: results}, reason
}
