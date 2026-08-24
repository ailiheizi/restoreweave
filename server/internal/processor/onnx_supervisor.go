//go:build unix

package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/processor/sandbox"
	"github.com/ailiheizi/restoreweave/server/internal/search"
)

// ONNXWorkerSupervisorOptions is the host-owned input to the real local
// semantic worker. A caller must supply a lease validator; a guessed positive
// fencing token is not sufficient to keep a worker alive.
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

// StartONNXWorker performs bundle admission, an isolated worker-process
// launch, nonce/peer proof, runtime probing, and host-side negotiation. Linux
// hosts with bubblewrap additionally receive the namespace sandbox; other Unix
// hosts still use the independently launched, authenticated worker process.
func StartONNXWorker(ctx context.Context, options ONNXWorkerSupervisorOptions) (NegotiatedONNXWorker, func() error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Command == "" {
		command, err := os.Executable()
		if err != nil {
			return NegotiatedONNXWorker{}, nil, err
		}
		options.Command = command
	}
	command, err := filepath.Abs(options.Command)
	if err != nil {
		return NegotiatedONNXWorker{}, nil, err
	}
	bundle, err := search.LoadSemanticBundle(options.BundleRoot)
	if err != nil {
		return NegotiatedONNXWorker{}, nil, err
	}
	if options.FenceToken <= 0 || options.FenceValidator == nil {
		return NegotiatedONNXWorker{}, nil, errors.New("ONNX worker requires a positive lease fencing token and validator")
	}
	if options.HandshakeTimeout <= 0 {
		options.HandshakeTimeout = onnxWorkerMaxHandshake
	}
	if options.SandboxPolicyDigest == "" {
		return NegotiatedONNXWorker{}, nil, errors.New("ONNX worker sandbox policy digest is required")
	}
	workerDigest := "sha256:" + bundle.AssetDigests["onnx_binding"]
	executableDigest, err := digestONNXExecutable(command)
	if err != nil {
		return NegotiatedONNXWorker{}, nil, err
	}
	admission, err := LoadONNXWorkerAdmission(options.BundleRoot, bundle.ProfileDigest, options.ConfigDigest)
	if err != nil {
		return NegotiatedONNXWorker{}, nil, err
	}
	if admission.WorkerDigest != workerDigest {
		return NegotiatedONNXWorker{}, nil, errors.New("ONNX worker binding digest changed during admission")
	}
	sandboxed := sandbox.Supported()
	session, err := startONNXWorkerSession(ctx, onnxWorkerProofConfig{
		Command: command, Args: []string{"--onnx-worker-process"}, WorkingDir: filepath.Dir(command), ProfileDigest: admission.ProfileDigest,
		ConfigDigest: options.ConfigDigest, WorkerDigest: admission.WorkerDigest,
		ExecutableDigest: executableDigest, GenerationID: options.GenerationID,
		FenceToken: fmt.Sprint(options.FenceToken), SandboxPolicyDigest: options.SandboxPolicyDigest,
		HandshakeTimeout: options.HandshakeTimeout, Sandbox: sandboxed, BundleRoot: options.BundleRoot,
		FenceValidator: options.FenceValidator,
	})
	if err != nil {
		return NegotiatedONNXWorker{}, nil, err
	}
	closeSession := session.close
	runtime, err := newQualifiedONNXWorkerTransportRuntime(session, admission)
	if err != nil {
		_ = closeSession()
		return NegotiatedONNXWorker{}, nil, err
	}
	textHandles := options.TextHandles
	if textHandles == nil {
		textHandles, err = NewTextHandleStore(MaxEmbedTextInputBytes*2, MaxEmbedTextInputBytes)
		if err != nil {
			_ = closeSession()
			return NegotiatedONNXWorker{}, nil, err
		}
	}
	worker, err := admission.Negotiate(ctx, ONNXWorkerHostCapabilities{
		Protocol: ONNXWorkerProtocol, Schema: ONNXWorkerSchema, Platform: CurrentONNXWorkerPlatform(),
		MaxInputBytes: MaxEmbedTextInputBytes, MaxInputTokens: MaxEmbedTextInputTokens,
		MaxOutputBytes: MaxEmbedTextOutputBytes, Runtime: runtime, TextHandles: textHandles,
		processProof: &onnxWorkerProcessAttestation{identity: session.identity},
	})
	if err != nil {
		_ = closeSession()
		return NegotiatedONNXWorker{}, nil, err
	}
	return worker, closeSession, nil
}
