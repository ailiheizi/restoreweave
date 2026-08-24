package processor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

type probeOnlyONNXBackend struct {
	facts    onnxRuntimeProbeFacts
	probeErr error
	closed   bool
}

func (b *probeOnlyONNXBackend) Probe(context.Context) (onnxRuntimeProbeFacts, error) {
	return b.facts, b.probeErr
}
func (b *probeOnlyONNXBackend) Close() error {
	b.closed = true
	return nil
}

func TestONNXRuntimeAdapterRevalidatesBundleBeforeNativeLoad(t *testing.T) {
	tests := []struct {
		name       string
		wantReason string
		mutate     func(t *testing.T, root string, descriptor *search.SemanticBundleDescriptor, admission *ONNXWorkerAdmission)
	}{
		{
			name:       "binding commit",
			wantReason: ONNXWorkerReasonProfile,
			mutate: func(_ *testing.T, _ string, descriptor *search.SemanticBundleDescriptor, _ *ONNXWorkerAdmission) {
				descriptor.ONNXGoBindingCommit = "unreviewed-binding"
			},
		},
		{
			name:       "binding C API",
			wantReason: ONNXWorkerReasonBundle,
			mutate: func(_ *testing.T, _ string, descriptor *search.SemanticBundleDescriptor, _ *ONNXWorkerAdmission) {
				descriptor.ONNXGoBindingCAPI = onnxRuntimeGoBindingCAPI - 1
			},
		},
		{
			name:       "profile",
			wantReason: ONNXWorkerReasonProfile,
			mutate: func(_ *testing.T, _ string, descriptor *search.SemanticBundleDescriptor, _ *ONNXWorkerAdmission) {
				descriptor.IndexConfig += "-changed"
			},
		},
		{
			name:       "runtime path",
			wantReason: ONNXWorkerReasonProfile,
			mutate: func(_ *testing.T, root string, _ *search.SemanticBundleDescriptor, admission *ONNXWorkerAdmission) {
				admission.Assets.Runtime = filepath.Join(root, "other-runtime")
			},
		},
		{
			name:       "malformed admitted digest",
			wantReason: ONNXWorkerReasonProfile,
			mutate: func(_ *testing.T, _ string, _ *search.SemanticBundleDescriptor, admission *ONNXWorkerAdmission) {
				admission.Manifest.RuntimeDigest = "bad"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, descriptor, admission := testPinnedONNXAdmission(t)
			tt.mutate(t, root, &descriptor, &admission)
			writeONNXBundleDescriptor(t, root, descriptor)
			adapter, err := NewONNXRuntimeAdapter(context.Background(), admission, ONNXRuntimeAdapterOptions{})
			if adapter != nil {
				_ = adapter.Close()
				t.Fatal("mismatched admission created an adapter")
			}
			assertONNXAdapterError(t, err, tt.wantReason)
		})
	}
}

func TestONNXRuntimeAdapterFakeOrMissingRuntimeIsTypedUnavailable(t *testing.T) {
	_, _, admission := testPinnedONNXAdmission(t)
	adapter, err := NewONNXRuntimeAdapter(context.Background(), admission, ONNXRuntimeAdapterOptions{})
	if adapter != nil {
		_ = adapter.Close()
		t.Fatal("fixture runtime created an adapter")
	}
	if !errors.Is(err, ErrONNXRuntimeUnavailable) {
		t.Fatalf("runtime error = %v, want ErrONNXRuntimeUnavailable", err)
	}
	var workerErr *ONNXWorkerError
	if !errors.As(err, &workerErr) || (workerErr.ReasonCode != ONNXWorkerReasonRuntime && workerErr.ReasonCode != ONNXWorkerReasonRuntimeMismatch) {
		t.Fatalf("runtime error = %v, want typed runtime unavailable", err)
	}

	_, _, admission = testPinnedONNXAdmission(t)
	if err := os.Remove(admission.Assets.Runtime); err != nil {
		t.Fatal(err)
	}
	adapter, err = NewONNXRuntimeAdapter(context.Background(), admission, ONNXRuntimeAdapterOptions{})
	if adapter != nil {
		_ = adapter.Close()
		t.Fatal("missing runtime created an adapter")
	}
	assertONNXAdapterError(t, err, ONNXWorkerReasonBundle)
}

func TestONNXRuntimeAdapterCannotReportReadyOrAccepted(t *testing.T) {
	_, _, admission := testPinnedONNXAdmission(t)
	backend := &probeOnlyONNXBackend{facts: onnxRuntimeProbeFacts{RuntimeVersion: admission.RuntimeVersion, RuntimeCAPI: onnxRuntimeGoBindingCAPI}}
	adapter := &ONNXRuntimeAdapter{admission: admission, backend: backend}

	probe, err := adapter.Probe(context.Background(), admission)
	if !errors.Is(err, ErrONNXRuntimeUnavailable) {
		t.Fatalf("probe error = %v, want unavailable", err)
	}
	if probe.ModelLoaded || probe.TokenizerLoaded || probe.IsolationClass == ONNXWorkerIsolationProcess {
		t.Fatalf("probe crossed readiness boundary: %+v", probe)
	}
	if probe.RuntimeVersion != admission.RuntimeVersion || probe.RuntimeCAPI != onnxRuntimeGoBindingCAPI {
		t.Fatalf("measured runtime facts missing from unavailable probe: %+v", probe)
	}

	if _, err := admission.Negotiate(context.Background(), testONNXHost(admission, adapter)); err == nil {
		t.Fatal("probe-only adapter negotiated READY")
	}
	req := testONNXRequest(admission)
	batch, runErr := adapter.EmbedText(context.Background(), req)
	if !errors.Is(runErr, ErrONNXRuntimeUnavailable) {
		t.Fatalf("embed error = %v, want unavailable", runErr)
	}
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		t.Fatalf("typed unavailable result rejected: %v", err)
	}
	for _, result := range batch.Results {
		if result.Status == EmbedTextAccepted || len(result.Vector) != 0 {
			t.Fatalf("probe-only adapter returned accepted output: %+v", result)
		}
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !backend.closed {
		t.Fatal("backend was not closed")
	}
}

func testPinnedONNXAdmission(t *testing.T) (string, search.SemanticBundleDescriptor, ONNXWorkerAdmission) {
	t.Helper()
	root, descriptor := testONNXBundle(t)
	descriptor.ONNXGoBindingCommit = onnxRuntimeGoBindingCommit
	writeONNXBundleDescriptor(t, root, descriptor)
	bundle, err := search.LoadSemanticBundle(root)
	if err != nil {
		t.Fatalf("load pinned bundle: %v", err)
	}
	admission, err := LoadONNXWorkerAdmission(root, bundle.ProfileDigest, "sha256:"+strings.Repeat("c", 64))
	if err != nil {
		t.Fatalf("load worker admission: %v", err)
	}
	return root, descriptor, admission
}

func writeONNXBundleDescriptor(t *testing.T, root string, descriptor search.SemanticBundleDescriptor) {
	t.Helper()
	payload, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, search.SemanticBundleManifestName), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertONNXAdapterError(t *testing.T, err error, reason string) {
	t.Helper()
	if !errors.Is(err, ErrONNXRuntimeUnavailable) {
		t.Fatalf("adapter error = %v, want ErrONNXRuntimeUnavailable", err)
	}
	var workerErr *ONNXWorkerError
	if !errors.As(err, &workerErr) || workerErr.ReasonCode != reason {
		t.Fatalf("adapter error = %v, want reason %s", err, reason)
	}
}
