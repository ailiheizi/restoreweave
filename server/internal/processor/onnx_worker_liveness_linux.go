//go:build linux

package processor

import (
	"errors"
	"golang.org/x/sys/unix"
	"os/exec"
)

func newONNXWorkerProcessLiveness(cmd *exec.Cmd) (*onnxWorkerProcessLiveness, error) {
	if cmd == nil || cmd.Process == nil {
		return nil, errors.New("worker process is missing")
	}
	fd, err := unix.PidfdOpen(cmd.Process.Pid, 0)
	if err != nil {
		// A PID-reuse-resistant proof is mandatory for the Linux profile. Do
		// not silently fall back to kill(pid, 0).
		return nil, err
	}
	return &onnxWorkerProcessLiveness{
		cmd: cmd,
		probe: func() bool {
			if err := unix.PidfdSendSignal(fd, 0, nil, 0); err != nil {
				return false
			}
			return true
		},
		close: func() error { return unix.Close(fd) },
	}, nil
}
