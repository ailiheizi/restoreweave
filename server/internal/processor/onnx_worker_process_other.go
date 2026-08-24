//go:build !unix

package processor

import (
	"context"
	"errors"
)

// RunONNXWorkerProcess is unavailable on platforms without the Unix socket
// and descriptor-passing primitives required by the private worker protocol.
func RunONNXWorkerProcess(context.Context, string) error {
	return errors.New("ONNX worker process transport is unavailable on this platform")
}
