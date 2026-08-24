package processor

import (
	"context"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

// ProbeForIsolatedWorker returns complete model/session facts after the
// adapter has been loaded inside the worker child. Only the child entrypoint
// should place these facts in the authenticated readiness envelope.
func (a *ONNXRuntimeAdapter) ProbeForIsolatedWorker(ctx context.Context, admission ONNXWorkerAdmission) (ONNXWorkerProbeResult, error) {
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
		TokenizerDigest: admission.Manifest.TokenizerDigest, ModelLoaded: true, TokenizerLoaded: true,
		IsolationClass:   ONNXWorkerIsolationProcess,
		InputNames:       []string{"input_ids", "attention_mask", "token_type_ids"},
		InputElementType: "int64", InputRank: 2, OutputName: "last_hidden_state",
		OutputElementType: "float32", OutputRank: 3, OutputDimension: admission.Manifest.Dimension,
		MaxTokens: search.SemanticBundleBGEMaxTokens,
	}, nil
}
