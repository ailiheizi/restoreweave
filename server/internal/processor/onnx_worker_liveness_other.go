//go:build !unix

package processor

import (
	"errors"
	"os/exec"
)

type onnxWorkerProcessLiveness struct{}

func (*onnxWorkerProcessLiveness) alive() bool        { return false }
func (*onnxWorkerProcessLiveness) markExited()        {}
func (*onnxWorkerProcessLiveness) closeHandle() error { return nil }

func newONNXWorkerProcessLiveness(*exec.Cmd) (*onnxWorkerProcessLiveness, error) {
	return nil, errors.New("worker process liveness is unsupported on this platform")
}
