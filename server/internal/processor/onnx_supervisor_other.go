//go:build !unix

package processor

import (
	"context"
	"errors"
	"time"
)

// ONNXWorkerSupervisorOptions is retained on unsupported platforms so callers
// receive a typed unavailable result instead of a build-specific API gap.
type ONNXWorkerSupervisorOptions struct {
	Command             string
	BundleRoot          string
	ConfigDigest        string
	GenerationID        string
	FenceToken          int64
	SandboxPolicyDigest string
	FenceValidator      func(context.Context) error
	TextHandles         TextHandleStore
	HandshakeTimeout    time.Duration
}

func StartONNXWorker(context.Context, ONNXWorkerSupervisorOptions) (NegotiatedONNXWorker, func() error, error) {
	return NegotiatedONNXWorker{}, nil, errors.New("ONNX worker supervisor is unavailable on this platform")
}
