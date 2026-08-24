//go:build unix

package processor

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// RunONNXWorkerProcess is the private child entrypoint used by a host-owned
// supervisor. The bundle root is supplied explicitly by the parent (typically
// through a read-only sandbox mount); no current-directory or network lookup
// is performed. It returns only after the authenticated transport closes.
func RunONNXWorkerProcess(ctx context.Context, bundleRoot string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(bundleRoot) != bundleRoot || bundleRoot == "" {
		return fmt.Errorf("%w: bundle root is required", errONNXWorkerProofInvalid)
	}
	socketPath := os.Getenv(onnxWorkerSocketEnv)
	profileDigest := os.Getenv(onnxWorkerProfileEnv)
	workerDigest := os.Getenv(onnxWorkerDigestEnv)
	executableDigest := os.Getenv(onnxWorkerExecutableEnv)
	generationID := os.Getenv(onnxWorkerGenerationEnv)
	fenceToken := os.Getenv(onnxWorkerFenceEnv)
	sandboxDigest := os.Getenv(onnxWorkerSandboxEnv)
	configDigest := os.Getenv(onnxWorkerConfigEnv)
	if socketPath == "" || profileDigest == "" || workerDigest == "" || generationID == "" || fenceToken == "" || sandboxDigest == "" {
		return fmt.Errorf("%w: worker protocol environment is incomplete", errONNXWorkerProofInvalid)
	}
	noncePath := os.Getenv(onnxWorkerNoncePathEnv)
	var nonceFile *os.File
	if noncePath != "" {
		if noncePath != "/restoreweave-worker-nonce" {
			return fmt.Errorf("%w: nonce path is not the fixed sandbox path", errONNXWorkerProofInvalid)
		}
		var err error
		nonceFile, err = os.Open(noncePath)
		if err != nil {
			return fmt.Errorf("%w: open nonce file: %v", errONNXWorkerProofInvalid, err)
		}
	} else {
		nonceFile = os.NewFile(uintptr(3), "restoreweave-onnx-worker-nonce")
		if nonceFile == nil {
			return fmt.Errorf("%w: nonce descriptor is unavailable", errONNXWorkerProofInvalid)
		}
	}
	nonce := make([]byte, onnxWorkerNonceBytes)
	if _, err := io.ReadFull(nonceFile, nonce); err != nil {
		return fmt.Errorf("%w: read nonce: %v", errONNXWorkerProofInvalid, err)
	}
	_ = nonceFile.Close()
	conn, err := net.DialTimeout("unix", socketPath, onnxWorkerMaxHandshake)
	if err != nil {
		return fmt.Errorf("%w: connect supervisor: %v", errONNXWorkerProofUnavailable, err)
	}
	defer conn.Close()
	deadline := time.Now().Add(onnxWorkerMaxHandshake)
	_ = conn.SetDeadline(deadline)
	var challenge onnxWorkerChallenge
	if err := readONNXWorkerHandshake(conn, &challenge); err != nil {
		return fmt.Errorf("%w: read challenge: %v", errONNXWorkerProofInvalid, err)
	}
	if challenge.Schema != onnxWorkerHandshakeV1 || challenge.Protocol != ONNXWorkerProtocol ||
		challenge.ProfileDigest != profileDigest || challenge.WorkerDigest != workerDigest ||
		challenge.GenerationID != generationID || challenge.FenceToken != fenceToken || challenge.ExecutableDigest != executableDigest || challenge.ConfigDigest != configDigest {
		return fmt.Errorf("%w: challenge binding does not match child environment", errONNXWorkerProofInvalid)
	}
	admission, err := LoadONNXWorkerAdmission(bundleRoot, profileDigest, configDigest)
	if err != nil {
		return err
	}
	adapter, err := NewONNXRuntimeAdapter(ctx, admission, ONNXRuntimeAdapterOptions{})
	if err != nil {
		return err
	}
	defer adapter.Close()
	probe, err := adapter.ProbeForIsolatedWorker(ctx, admission)
	if err != nil {
		return err
	}
	probeDigest, err := onnxWorkerProbeDigest(probe)
	if err != nil {
		return fmt.Errorf("%w: digest probe: %v", errONNXWorkerProofUnavailable, err)
	}
	ready := onnxWorkerReady{
		Schema: challenge.Schema, SessionID: challenge.SessionID, Protocol: challenge.Protocol,
		ProfileDigest: challenge.ProfileDigest, WorkerDigest: challenge.WorkerDigest,
		GenerationID: challenge.GenerationID, FenceToken: challenge.FenceToken,
		ExecutableDigest: challenge.ExecutableDigest,
		ConfigDigest:     challenge.ConfigDigest,
		PeerPID:          challenge.PeerPID, PID: os.Getpid(), SandboxPolicyDigest: sandboxDigest, Probe: probe, ProbeDigest: probeDigest,
	}
	binding := onnxWorkerHandshakeBinding{
		Schema: ready.Schema, SessionID: ready.SessionID, Protocol: ready.Protocol,
		ProfileDigest: ready.ProfileDigest, WorkerDigest: ready.WorkerDigest,
		GenerationID: ready.GenerationID, FenceToken: ready.FenceToken,
		ExecutableDigest:    ready.ExecutableDigest,
		ConfigDigest:        ready.ConfigDigest,
		SandboxPolicyDigest: ready.SandboxPolicyDigest, PeerPID: ready.PeerPID,
		PeerUID: challenge.PeerUID, PeerGID: challenge.PeerGID,
		ProbeDigest: ready.ProbeDigest,
	}
	ready.MAC = onnxWorkerHandshakeMAC(nonce, binding)
	if err := writeONNXWorkerHandshake(conn, ready); err != nil {
		return fmt.Errorf("%w: send readiness: %v", errONNXWorkerProofUnavailable, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return serveONNXRuntimeAdapterTransport(ctx, conn, onnxWorkerTransportBinding{
		SessionID: ready.SessionID, ProfileDigest: ready.ProfileDigest, WorkerDigest: ready.WorkerDigest,
		GenerationID: ready.GenerationID, FenceToken: ready.FenceToken,
	}, adapter)
}
