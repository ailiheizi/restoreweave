package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

type testONNXRuntime struct {
	probe      ONNXWorkerProbeResult
	probeErr   error
	batch      EmbedTextResultBatch
	embedErr   error
	embedCalls int
	lastInputs []ONNXWorkerTextInput
}

type forgedTextHandleStore struct {
	*InMemoryTextHandleStore
	forged []byte
}

// testProcessAttestedONNXRuntime models the identity handoff that a real
// supervisor will provide. It is intentionally test-only: production code has
// no constructor that can manufacture process attestation from an in-memory
// runtime.
type testProcessAttestedONNXRuntime struct {
	ONNXWorkerRuntime
	identity *onnxWorkerProcessIdentity
}

func (r *testProcessAttestedONNXRuntime) onnxWorkerProcessIdentity() *onnxWorkerProcessIdentity {
	if r == nil {
		return nil
	}
	return r.identity
}

func attestONNXWorkerProcessRuntimeForTest(runtimeAdapter ONNXWorkerRuntime) (ONNXWorkerRuntime, *onnxWorkerProcessAttestation) {
	if runtimeAdapter == nil {
		return nil, nil
	}
	identity := &onnxWorkerProcessIdentity{}
	return &testProcessAttestedONNXRuntime{ONNXWorkerRuntime: runtimeAdapter, identity: identity},
		&onnxWorkerProcessAttestation{identity: identity}
}

func (s *forgedTextHandleStore) Consume(context.Context, string, TextHandleBinding, string, int64) ([]byte, error) {
	return append([]byte(nil), s.forged...), nil
}

func (r *testONNXRuntime) Probe(context.Context, ONNXWorkerAdmission) (ONNXWorkerProbeResult, error) {
	return r.probe, r.probeErr
}

func (r *testONNXRuntime) EmbedTextWithText(_ context.Context, _ EmbedTextRequest, inputs []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
	r.embedCalls++
	r.lastInputs = inputs
	return r.batch, r.embedErr
}

func TestLoadONNXWorkerAdmissionRejectsMissingOrUnpinnedBundle(t *testing.T) {
	if _, err := LoadONNXWorkerAdmission(filepath.Join(t.TempDir(), "missing"), "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)); err == nil {
		t.Fatal("missing bundle was admitted")
	} else {
		var workerErr *ONNXWorkerError
		if !errors.As(err, &workerErr) || workerErr.ReasonCode != ONNXWorkerReasonBundle {
			t.Fatalf("missing bundle error = %v, want typed %s", err, ONNXWorkerReasonBundle)
		}
		if !errors.Is(err, ErrONNXWorkerAdmission) {
			t.Fatalf("missing bundle error does not unwrap admission failure: %v", err)
		}
	}
	root, _ := testONNXBundle(t)
	for _, path := range []string{" " + root, root + " ", "\t" + root + "\n"} {
		if _, err := LoadONNXWorkerAdmission(path, "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)); err == nil {
			t.Fatalf("bundle path with surrounding whitespace was accepted: %q", path)
		} else {
			var workerErr *ONNXWorkerError
			if !errors.As(err, &workerErr) || workerErr.ReasonCode != ONNXWorkerReasonBundle {
				t.Fatalf("whitespace path error = %v, want typed %s", err, ONNXWorkerReasonBundle)
			}
		}
	}
	root, descriptor := testONNXBundle(t)
	payload, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, search.SemanticBundleManifestName), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadONNXWorkerAdmission(root, "sha256:"+strings.Repeat("f", 64), "sha256:"+strings.Repeat("b", 64)); err == nil {
		t.Fatal("bundle with wrong trusted profile digest was admitted")
	} else {
		var workerErr *ONNXWorkerError
		if !errors.As(err, &workerErr) || workerErr.ReasonCode != ONNXWorkerReasonProfile {
			t.Fatalf("wrong pin error = %v, want typed %s", err, ONNXWorkerReasonProfile)
		}
	}
}

func TestONNXWorkerAdmissionBindsAssetsAndPreprocessing(t *testing.T) {
	root, descriptor := testONNXBundle(t)
	payload, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, search.SemanticBundleManifestName), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := search.LoadSemanticBundle(root)
	if err != nil {
		t.Fatal(err)
	}
	configDigest := "sha256:" + strings.Repeat("c", 64)
	admission, err := LoadONNXWorkerAdmission(root, bundle.ProfileDigest, configDigest)
	if err != nil {
		t.Fatalf("load admission: %v", err)
	}
	if admission.Capability.State != ONNXWorkerStateAdmitted || admission.ProfileDigest != bundle.ProfileDigest ||
		admission.WorkerDigest != "sha256:"+bundle.AssetDigests["onnx_binding"] || admission.Manifest.ConfigDigest != configDigest {
		t.Fatalf("admission binding = %+v", admission)
	}
	if admission.QueryPreprocessingDigest == admission.DocumentPreprocessingDigest {
		t.Fatal("query and document preprocessing unexpectedly share a digest")
	}
	if admission.Assets.Root != root || admission.Assets.Model == "" || admission.Assets.Tokenizer == "" {
		t.Fatalf("admitted asset paths = %+v", admission.Assets)
	}
}

func TestONNXWorkerNegotiationFailsClosedWithoutRuntimeOrProbe(t *testing.T) {
	admission := testONNXAdmission(t)
	host := testONNXHost(admission, nil)
	if _, err := admission.Negotiate(context.Background(), host); err == nil {
		t.Fatal("negotiation succeeded without a runtime")
	} else {
		var workerErr *ONNXWorkerError
		if !errors.As(err, &workerErr) || workerErr.ReasonCode != ONNXWorkerReasonRuntime || !errors.Is(err, ErrONNXWorkerUnavailable) {
			t.Fatalf("missing runtime error = %v", err)
		}
	}
	probeFailure := &testONNXRuntime{probeErr: errors.New("session load failed")}
	if _, err := admission.Negotiate(context.Background(), testONNXHost(admission, probeFailure)); err == nil {
		t.Fatal("negotiation succeeded after failed runtime probe")
	} else {
		var workerErr *ONNXWorkerError
		if !errors.As(err, &workerErr) || workerErr.ReasonCode != ONNXWorkerReasonRuntime {
			t.Fatalf("probe failure error = %v", err)
		}
	}
}

func TestONNXWorkerRejectsRuntimeSelfReportedIsolationWithoutHostProof(t *testing.T) {
	admission := testONNXAdmission(t)
	runtimeAdapter := &testONNXRuntime{probe: validONNXProbe(admission)}
	host := testONNXHost(admission, runtimeAdapter)
	host.Runtime = runtimeAdapter
	host.processProof = nil
	if _, err := admission.Negotiate(context.Background(), host); err == nil {
		t.Fatal("self-reported isolated runtime negotiated without host proof")
	} else {
		var workerErr *ONNXWorkerError
		if !errors.As(err, &workerErr) || workerErr.ReasonCode != ONNXWorkerReasonRuntime || !errors.Is(err, ErrONNXWorkerUnavailable) {
			t.Fatalf("unattested runtime error = %v", err)
		}
	}
}

func TestONNXWorkerProbeMustMatchActualBGEContract(t *testing.T) {
	admission := testONNXAdmission(t)
	probe := validONNXProbe(admission)
	probe.RuntimeCAPI = 25
	runtimeProbe := &testONNXRuntime{probe: probe}
	if _, err := admission.Negotiate(context.Background(), testONNXHost(admission, runtimeProbe)); err == nil {
		t.Fatal("runtime with C API 25 was negotiated")
	} else {
		var workerErr *ONNXWorkerError
		if !errors.As(err, &workerErr) || workerErr.ReasonCode != ONNXWorkerReasonRuntimeMismatch {
			t.Fatalf("C API mismatch error = %v", err)
		}
	}
	probe = validONNXProbe(admission)
	probe.InputNames = []string{"input_ids", "attention_mask"}
	if _, err := admission.Negotiate(context.Background(), testONNXHost(admission, &testONNXRuntime{probe: probe})); err == nil {
		t.Fatal("runtime with incomplete inputs was negotiated")
	}
}

func TestONNXWorkerRejectsUnboundDeterminismClaim(t *testing.T) {
	admission := testONNXAdmission(t)
	req := testONNXRequest(admission)
	req.Profile.DeterminismClass = EmbedTextDeterminismByte
	if err := validateONNXWorkerRequest(admission, req); err == nil {
		t.Fatal("byte-deterministic claim was accepted for the semantic-deterministic ONNX profile")
	}
}

func TestONNXWorkerNegotiationRejectsProtocolSchemaPlatformAndProbeShape(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ONNXWorkerHostCapabilities, *ONNXWorkerProbeResult)
	}{
		{"protocol", func(host *ONNXWorkerHostCapabilities, _ *ONNXWorkerProbeResult) { host.Protocol = "other" }},
		{"schema", func(host *ONNXWorkerHostCapabilities, _ *ONNXWorkerProbeResult) { host.Schema = "other" }},
		{"platform", func(host *ONNXWorkerHostCapabilities, _ *ONNXWorkerProbeResult) {
			host.Platform = ONNXWorkerPlatform{OS: "other", Arch: "other"}
		}},
		{"missing input", func(_ *ONNXWorkerHostCapabilities, probe *ONNXWorkerProbeResult) {
			probe.InputNames = []string{"input_ids", "attention_mask"}
		}},
		{"extra input", func(_ *ONNXWorkerHostCapabilities, probe *ONNXWorkerProbeResult) {
			probe.InputNames = []string{"input_ids", "attention_mask", "token_type_ids", "extra"}
		}},
		{"wrong output shape", func(_ *ONNXWorkerHostCapabilities, probe *ONNXWorkerProbeResult) { probe.OutputRank = 2 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			admission := testONNXAdmission(t)
			probe := validONNXProbe(admission)
			runtimeAdapter := &testONNXRuntime{probe: probe}
			host := testONNXHost(admission, runtimeAdapter)
			tc.mutate(&host, &probe)
			runtimeAdapter.probe = probe
			if _, err := admission.Negotiate(context.Background(), host); err == nil {
				t.Fatal("invalid negotiation succeeded")
			}
		})
	}
}

func TestONNXWorkerRechecksNegotiatedQuotasBeforeRuntime(t *testing.T) {
	admission := testONNXAdmission(t)
	runtimeAdapter := &testONNXRuntime{probe: validONNXProbe(admission)}
	host := testONNXHost(admission, runtimeAdapter)
	host.MaxInputBytes = 32
	host.MaxInputTokens = 4
	host.MaxOutputBytes = 16
	worker, err := admission.Negotiate(context.Background(), host)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if worker.maxInputBytes != host.MaxInputBytes || worker.maxInputTokens != host.MaxInputTokens || worker.maxOutputBytes != host.MaxOutputBytes {
		t.Fatalf("negotiated quotas = %d/%d/%d, want %d/%d/%d", worker.maxInputBytes, worker.maxInputTokens, worker.maxOutputBytes, host.MaxInputBytes, host.MaxInputTokens, host.MaxOutputBytes)
	}
	tests := []struct {
		name   string
		mutate func(*EmbedTextRequest)
	}{
		{"input bytes declaration", func(req *EmbedTextRequest) { req.MaxInputBytes = host.MaxInputBytes + 1 }},
		{"input tokens declaration", func(req *EmbedTextRequest) { req.MaxInputTokens = host.MaxInputTokens + 1 }},
		{"output bytes declaration", func(req *EmbedTextRequest) { req.MaxOutputBytes = host.MaxOutputBytes + 1 }},
		{"actual input bytes", func(req *EmbedTextRequest) {
			req.MaxInputBytes = host.MaxInputBytes + 8
			req.Segments[0].TextBytes = 17
			req.Segments[1].TextBytes = 17
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runtimeAdapter.embedCalls = 0
			req := testONNXRequest(admission)
			req.MaxInputBytes = host.MaxInputBytes
			req.MaxInputTokens = host.MaxInputTokens
			req.MaxOutputBytes = host.MaxOutputBytes
			tc.mutate(&req)
			batch, runErr := worker.EmbedText(context.Background(), req)
			if runErr == nil {
				t.Fatal("quota-violating request succeeded")
			}
			var workerErr *ONNXWorkerError
			if !errors.As(runErr, &workerErr) || workerErr.ReasonCode != ONNXWorkerReasonBudget {
				t.Fatalf("quota error = %v", runErr)
			}
			if runtimeAdapter.embedCalls != 0 {
				t.Fatalf("runtime called %d times for rejected request", runtimeAdapter.embedCalls)
			}
			if err := ValidateEmbedTextResult(req, batch); err != nil {
				t.Fatalf("unavailable batch rejected: %v", err)
			}
		})
	}
}

func TestONNXWorkerUnavailablePathNeverReturnsAccepted(t *testing.T) {
	admission := testONNXAdmission(t)
	worker, err := admission.Negotiate(context.Background(), testONNXHost(admission, &testONNXRuntime{probe: validONNXProbe(admission), probeErr: errors.New("runtime intentionally unavailable")}))
	if err == nil {
		t.Fatal("unavailable runtime negotiated")
	}
	req := testONNXRequest(admission)
	batch, runErr := worker.EmbedText(context.Background(), req)
	if runErr == nil {
		t.Fatal("zero worker reported semantic success")
	}
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		t.Fatalf("typed unavailable batch rejected: %v", err)
	}
	for _, result := range batch.Results {
		if result.Status == EmbedTextAccepted || len(result.Vector) != 0 {
			t.Fatalf("unavailable worker returned semantic output: %+v", result)
		}
		if result.FailureCode != "PROVIDER_UNAVAILABLE" {
			t.Fatalf("failure code = %q", result.FailureCode)
		}
	}
}

func TestONNXWorkerRejectsInvalidRuntimeOutput(t *testing.T) {
	admission := testONNXAdmission(t)
	runtimeProbe := &testONNXRuntime{probe: validONNXProbe(admission)}
	host := testONNXHost(admission, runtimeProbe)
	worker, err := admission.Negotiate(context.Background(), host)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	req := testONNXRequest(admission)
	issueONNXHandles(t, host.TextHandles, &req)
	runtimeProbe.batch = validEmbedBatch(req)
	batch, runErr := worker.EmbedText(context.Background(), req)
	if runErr == nil {
		t.Fatal("invalid dimension output was accepted")
	}
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		t.Fatalf("invalid-output fallback batch rejected: %v", err)
	}
	for _, result := range batch.Results {
		if result.Status == EmbedTextAccepted {
			t.Fatal("invalid runtime output became accepted semantic output")
		}
	}
}

func TestONNXWorkerConsumesHostHandlesBeforeRuntimeAndRejectsReplay(t *testing.T) {
	admission := testONNXAdmission(t)
	runtimeAdapter := &testONNXRuntime{probe: validONNXProbe(admission)}
	host := testONNXHost(admission, runtimeAdapter)
	worker, err := admission.Negotiate(context.Background(), host)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	req := testONNXRequest(admission)
	req.MaxOutputBytes = host.MaxOutputBytes
	texts := [][]byte{[]byte("第一段 durable text"), []byte("第二段 durable text")}
	for i := range req.Segments {
		handle, issueErr := host.TextHandles.Issue(context.Background(), req.Binding, texts[i])
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		req.Segments[i].TextHandleID = handle.ID
		req.Segments[i].TextDigest = handle.Digest
		req.Segments[i].TextBytes = handle.Bytes
	}
	runtimeAdapter.batch = validONNXBatch(req)
	got, runErr := worker.EmbedText(context.Background(), req)
	if runErr != nil {
		t.Fatalf("embed with issued handles: %v", runErr)
	}
	if err := ValidateEmbedTextResult(req, got); err != nil {
		t.Fatalf("accepted host-resolved batch rejected: %v", err)
	}
	if runtimeAdapter.embedCalls != 1 || len(runtimeAdapter.lastInputs) != len(texts) {
		t.Fatalf("runtime calls/inputs = %d/%d", runtimeAdapter.embedCalls, len(runtimeAdapter.lastInputs))
	}
	for i, input := range runtimeAdapter.lastInputs {
		if string(input.Text) != string(texts[i]) || input.SegmentID != req.Segments[i].ID {
			t.Fatalf("resolved input %d = %+v, want %q/%q", i, input, texts[i], req.Segments[i].ID)
		}
	}
	if _, runErr := worker.EmbedText(context.Background(), req); runErr == nil {
		t.Fatal("consumed handles were replayable")
	}
	if runtimeAdapter.embedCalls != 1 {
		t.Fatalf("runtime called after handle replay: %d", runtimeAdapter.embedCalls)
	}
}

func TestONNXWorkerBindsHandleToLeaseFenceAndDigest(t *testing.T) {
	admission := testONNXAdmission(t)
	runtimeAdapter := &testONNXRuntime{probe: validONNXProbe(admission)}
	host := testONNXHost(admission, runtimeAdapter)
	worker, err := admission.Negotiate(context.Background(), host)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	text := []byte("bound text")

	for name, mutate := range map[string]func(*EmbedTextRequest){
		"lease":      func(req *EmbedTextRequest) { req.Binding.LeaseID = "lease-stale" },
		"fence":      func(req *EmbedTextRequest) { req.Binding.FenceToken++ },
		"generation": func(req *EmbedTextRequest) { req.Binding.GenerationID = "generation-stale" },
		"digest":     func(req *EmbedTextRequest) { req.Segments[0].TextDigest = testDigest("f") },
	} {
		t.Run(name, func(t *testing.T) {
			// Each case gets fresh handles so stale-binding checks remain isolated.
			req := testONNXRequest(admission)
			for i := range req.Segments {
				h, issueErr := host.TextHandles.Issue(context.Background(), req.Binding, text)
				if issueErr != nil {
					t.Fatal(issueErr)
				}
				req.Segments[i].TextHandleID, req.Segments[i].TextDigest, req.Segments[i].TextBytes = h.ID, h.Digest, h.Bytes
			}
			mutate(&req)
			runtimeAdapter.embedCalls = 0
			if _, runErr := worker.EmbedText(context.Background(), req); runErr == nil {
				t.Fatal("stale or tampered handle binding was accepted")
			}
			if runtimeAdapter.embedCalls != 0 {
				t.Fatalf("runtime called for rejected binding: %d", runtimeAdapter.embedCalls)
			}
		})
	}
}

func TestONNXWorkerConsumesStoreBackedQueryAndDocumentHandlesOnce(t *testing.T) {
	for _, purpose := range []EmbedTextPurpose{EmbedTextPurposeDocument, EmbedTextPurposeQuery} {
		t.Run(string(purpose), func(t *testing.T) {
			admission := testONNXAdmission(t)
			runtimeAdapter := &testONNXRuntime{probe: validONNXProbe(admission)}
			host := testONNXHost(admission, runtimeAdapter)
			worker, err := admission.Negotiate(context.Background(), host)
			if err != nil {
				t.Fatalf("negotiate: %v", err)
			}
			req := testONNXRequest(admission)
			if purpose == EmbedTextPurposeQuery {
				req.Binding.Purpose = EmbedTextPurposeQuery
				req.Binding.AppliedPreprocessingDigest = req.Profile.QueryPreprocessingDigest
				req.Segments = []EmbedTextSegment{{
					ID: "query-1", Source: EmbedTextSource{Kind: EmbedTextSourceQuerySegment, Ref: "query-1"},
					Ordinal: 0, Language: req.Language,
				}}
			}
			issueONNXHandles(t, host.TextHandles, &req)
			req.MaxOutputBytes = host.MaxOutputBytes
			runtimeAdapter.batch = validONNXBatch(req)
			batch, err := worker.EmbedText(context.Background(), req)
			if err != nil {
				t.Fatalf("embed %s: %v", purpose, err)
			}
			if err := ValidateEmbedTextResult(req, batch); err != nil {
				t.Fatalf("accepted batch invalid: %v", err)
			}
			if runtimeAdapter.embedCalls != 1 || len(runtimeAdapter.lastInputs) != len(req.Segments) {
				t.Fatalf("runtime inputs/calls = %d/%d", runtimeAdapter.embedCalls, len(runtimeAdapter.lastInputs))
			}
			if _, replayErr := worker.EmbedText(context.Background(), req); replayErr == nil {
				t.Fatal("replayed consumed handles were accepted")
			}
			if runtimeAdapter.embedCalls != 1 {
				t.Fatalf("runtime called on replay: %d", runtimeAdapter.embedCalls)
			}
		})
	}
}

func TestONNXWorkerRechecksResolvedTextIdentityBeforeRuntime(t *testing.T) {
	admission := testONNXAdmission(t)
	runtimeAdapter := &testONNXRuntime{probe: validONNXProbe(admission)}
	host := testONNXHost(admission, runtimeAdapter)
	underlying := host.TextHandles.(*InMemoryTextHandleStore)
	forged := &forgedTextHandleStore{InMemoryTextHandleStore: underlying, forged: []byte("forged text")}
	host.TextHandles = forged
	worker, err := admission.Negotiate(context.Background(), host)
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	req := testONNXRequest(admission)
	issueONNXHandles(t, underlying, &req)
	if _, err := worker.EmbedText(context.Background(), req); err == nil {
		t.Fatal("forged resolved text was accepted")
	}
	if runtimeAdapter.embedCalls != 0 {
		t.Fatalf("runtime called for forged text: %d", runtimeAdapter.embedCalls)
	}
}

func issueONNXHandles(t *testing.T, store TextHandleStore, req *EmbedTextRequest) {
	t.Helper()
	for i := range req.Segments {
		text := []byte("host text " + strconv.Itoa(i))
		handle, err := store.Issue(context.Background(), req.Binding, text)
		if err != nil {
			t.Fatalf("issue text handle: %v", err)
		}
		req.Segments[i].TextHandleID = handle.ID
		req.Segments[i].TextDigest = handle.Digest
		req.Segments[i].TextBytes = handle.Bytes
		if req.Binding.Purpose == EmbedTextPurposeQuery {
			req.Segments[i].Source.Revision = handle.Digest
		}
	}
}

func validONNXBatch(req EmbedTextRequest) EmbedTextResultBatch {
	batch := validEmbedBatch(req)
	for i := range batch.Results {
		vector := make([]float32, req.Profile.Dimension)
		vector[0] = 1
		batch.Results[i].Vector = vector
		batch.Results[i].Dimension = req.Profile.Dimension
	}
	return batch
}

func testONNXAdmission(t *testing.T) ONNXWorkerAdmission {
	t.Helper()
	root, descriptor := testONNXBundle(t)
	payload, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, search.SemanticBundleManifestName), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := search.LoadSemanticBundle(root)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := LoadONNXWorkerAdmission(root, bundle.ProfileDigest, "sha256:"+strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

func testONNXHost(admission ONNXWorkerAdmission, runtimeAdapter ONNXWorkerRuntime) ONNXWorkerHostCapabilities {
	textHandles, err := NewTextHandleStore(MaxEmbedTextInputBytes*2, MaxEmbedTextInputBytes)
	if err != nil {
		panic(err)
	}
	runtimeAdapter, processProof := attestONNXWorkerProcessRuntimeForTest(runtimeAdapter)
	return ONNXWorkerHostCapabilities{
		Protocol: ONNXWorkerProtocol, Schema: ONNXWorkerSchema, Platform: admission.Platform,
		MaxInputBytes: MaxEmbedTextInputBytes, MaxInputTokens: MaxEmbedTextInputTokens,
		MaxOutputBytes: MaxEmbedTextOutputBytes, Runtime: runtimeAdapter, TextHandles: textHandles,
		processProof: processProof,
	}
}

func validONNXProbe(admission ONNXWorkerAdmission) ONNXWorkerProbeResult {
	return ONNXWorkerProbeResult{
		CapabilityID: ONNXWorkerCapabilityID, Protocol: ONNXWorkerProtocol,
		RuntimeVersion: admission.RuntimeVersion, RuntimeCAPI: search.SemanticBundleONNXRuntimeCAPI,
		RuntimeDigest: admission.Manifest.RuntimeDigest, ModelDigest: admission.Manifest.ModelDigest,
		TokenizerDigest: admission.Manifest.TokenizerDigest, ModelLoaded: true, TokenizerLoaded: true,
		IsolationClass: ONNXWorkerIsolationProcess, InputNames: []string{"input_ids", "attention_mask", "token_type_ids"},
		InputElementType: "int64", InputRank: 2, OutputName: "last_hidden_state", OutputElementType: "float32",
		OutputRank: 3, OutputDimension: admission.Manifest.Dimension, MaxTokens: search.SemanticBundleBGEMaxTokens,
	}
}

func testONNXRequest(admission ONNXWorkerAdmission) EmbedTextRequest {
	req := testEmbedRequest()
	req.Binding.WorkerDigest = admission.WorkerDigest
	req.Binding.WorkerProfileDigest = admission.ProfileDigest
	req.Binding.AppliedPreprocessingDigest = admission.DocumentPreprocessingDigest
	req.Profile.SemanticProfileDigest = admission.ProfileDigest
	req.Profile.ConfigDigest = admission.Manifest.ConfigDigest
	req.Profile.QueryPreprocessingDigest = admission.QueryPreprocessingDigest
	req.Profile.DocumentPreprocessingDigest = admission.DocumentPreprocessingDigest
	req.Profile.ModelDigest = admission.Manifest.ModelDigest
	req.Profile.TokenizerDigest = admission.Manifest.TokenizerDigest
	req.Profile.RuntimeDigest = admission.Manifest.RuntimeDigest
	req.Profile.SemanticSpace = admission.Manifest.SemanticSpace
	req.Profile.Dimension = admission.Manifest.Dimension
	req.Profile.ElementType = admission.Manifest.ElementType
	req.Profile.Pooling = admission.Manifest.Pooling
	req.Profile.Normalization = admission.Manifest.Normalization
	req.Profile.DeterminismClass = EmbedTextDeterminismSemantic
	req.AuthorizationScope = "authz:" + admission.ProfileDigest + ":local-index-build"
	req.EgressScope = "egress:" + admission.ProfileDigest + ":none"
	return req
}

func testONNXBundle(t *testing.T) (string, search.SemanticBundleDescriptor) {
	t.Helper()
	root := t.TempDir()
	files := testONNXBundleFiles()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	asset := func(name string) search.SemanticBundleAsset {
		sum := sha256.Sum256(files[name])
		return search.SemanticBundleAsset{Path: name, SHA256: hex.EncodeToString(sum[:]), Size: uint64(len(files[name]))}
	}
	return root, search.SemanticBundleDescriptor{
		Schema: search.SemanticBundleSchemaV1, ProfileID: search.SemanticBundleBGEProfileID,
		PlatformOS: runtime.GOOS, PlatformArch: runtime.GOARCH,
		ONNXRuntimeVersion: "1.29.0", ONNXRuntimeBuild: "test-runtime", ONNXRuntimeCAPI: search.SemanticBundleONNXRuntimeCAPI,
		ONNXGoBindingCommit: "test-binding", ONNXGoBindingCAPI: search.SemanticBundleONNXRuntimeCAPI,
		ONNXGoBindingDigest: asset("onnx-binding").SHA256, ModelID: "BAAI/bge-small-zh-v1.5",
		ModelRevision: "test-model-revision", ModelExport: "test-opset17", ONNXOpset: 17,
		ModelLicenseID: "BAAI/bge-small-zh-v1.5:MIT", TokenizerVersion: "test-tokenizer", TokenizerRevision: "test-tokenizer-revision",
		ZvecVersion: "0.6.0", ZvecBuild: "test-zvec", ZvecGoVersion: "0.6.0", ZvecGoCommit: "test-zvec-go",
		LicenseExpression: search.SemanticBundleLicenseExpression, PreprocessingDigest: "sha256:" + strings.Repeat("b", 64),
		Pooling: search.SemanticBundleBGEPooling, Normalization: search.SemanticBundleBGENormalization,
		ElementType: search.SemanticBundleBGEElementType, Dimension: search.SemanticBundleBGEDimension,
		VectorSchema: search.SemanticBundleBGEVectorSchema, SemanticSpace: search.SemanticBundleBGESemanticSpace,
		Distance: search.SemanticBundleBGEDistance, IndexConfig: "test-index", QueryConfig: "test-query",
		QueryPrefix: search.SemanticBundleBGEQueryPrefix, DocumentPrefix: search.SemanticBundleBGEDocumentPrefix,
		MaxTokens: search.SemanticBundleBGEMaxTokens,
		Runtime:   asset("runtime.bin"), ONNXBinding: asset("onnx-binding"), ONNXCAPI: asset("onnx-c-api.h"),
		Model: asset("model.onnx"), Tokenizer: asset("tokenizer"), Profile: asset("profile.json"), Zvec: asset("zvec.dylib"),
		ZvecGo: asset("zvec-go.txt"), License: asset("LICENSE"), Notice: asset("NOTICE"), SBOM: asset("sbom.json"),
	}
}

func testONNXBundleFiles() map[string][]byte {
	return map[string][]byte{
		"runtime.bin": []byte("runtime-asset"), "onnx-binding": []byte("binding-asset"),
		"onnx-c-api.h": []byte("#define ORT_API_VERSION 29\n"), "model.onnx": []byte("model-asset"),
		"tokenizer": []byte("tokenizer-asset"), "profile.json": []byte("profile-asset"),
		"zvec.dylib": []byte("zvec-asset"), "zvec-go.txt": []byte("zvec-go-asset"),
		"LICENSE": []byte("MIT\nApache-2.0\n"), "NOTICE": []byte("notice"), "sbom.json": []byte(`{"name":"test"}`),
	}
}
