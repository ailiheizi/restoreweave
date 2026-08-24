//go:build unix

package processor

import (
	"os/exec"
	"sync"
)

// onnxWorkerProcessLiveness keeps the host's process handle and an immutable
// PID identity. Platform files may add a pidfd; the command wait path is still
// required so an exited child cannot remain READY.
type onnxWorkerProcessLiveness struct {
	mu     sync.RWMutex
	cmd    *exec.Cmd
	exited bool
	close  func() error
	probe  func() bool
}

func (p *onnxWorkerProcessLiveness) markExited() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.exited = true
	p.mu.Unlock()
}

func (p *onnxWorkerProcessLiveness) alive() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	exited := p.exited
	probe := p.probe
	p.mu.RUnlock()
	return !exited && probe != nil && probe()
}

func (p *onnxWorkerProcessLiveness) closeHandle() error {
	if p == nil || p.close == nil {
		return nil
	}
	return p.close()
}
