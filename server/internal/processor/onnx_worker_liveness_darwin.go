//go:build darwin

package processor

import (
	"errors"
	"os/exec"
	"syscall"
)

func newONNXWorkerProcessLiveness(cmd *exec.Cmd) (*onnxWorkerProcessLiveness, error) {
	if cmd == nil || cmd.Process == nil {
		return nil, errors.New("worker process is missing")
	}
	pid := cmd.Process.Pid
	return &onnxWorkerProcessLiveness{
		cmd: cmd,
		probe: func() bool {
			return syscall.Kill(pid, syscall.Signal(0)) == nil
		},
	}, nil
}
