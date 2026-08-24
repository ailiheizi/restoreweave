//go:build !cgo || (!darwin && !linux)

package processor

import "context"

func newONNXRuntimeBackend(context.Context, validatedONNXRuntimeAssets) (onnxRuntimeBackend, error) {
	return nil, workerError(ONNXWorkerReasonRuntime, ErrONNXRuntimeUnavailable, "ONNX Runtime requires a supported cgo host")
}
