//go:build !unix

package processor

// Non-Unix builds cannot establish the required kernel-authenticated local
// process session and therefore never satisfy the process-proof gate.
type onnxWorkerSession struct{}

func (*onnxWorkerSession) alive() bool { return false }
