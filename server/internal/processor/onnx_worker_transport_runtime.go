//go:build unix

package processor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
)

// onnxWorkerTransportRuntime binds runtime calls to one kernel-authenticated
// process session. It deliberately does not implement
// onnxWorkerProcessIdentity: the transport and peer proof alone are not a
// qualified supervisor attestation and therefore cannot make Negotiate READY.
type onnxWorkerTransportRuntime struct {
	session   *onnxWorkerSession
	admission ONNXWorkerAdmission
	qualified bool
}

func newONNXWorkerTransportRuntime(session *onnxWorkerSession, admission ONNXWorkerAdmission) (*onnxWorkerTransportRuntime, error) {
	if session == nil || !session.alive() || session.transport == nil {
		return nil, fmt.Errorf("%w: process session is not alive", errONNXWorkerTransportUnavailable)
	}
	if admission.Capability.State != ONNXWorkerStateAdmitted || admission.ProfileDigest != session.profile || admission.WorkerDigest != session.worker {
		return nil, fmt.Errorf("%w: admitted runtime does not match process session", errONNXWorkerTransportInvalid)
	}
	return &onnxWorkerTransportRuntime{session: session, admission: admission}, nil
}

type qualifiedONNXWorkerTransportRuntime struct{ *onnxWorkerTransportRuntime }

func newQualifiedONNXWorkerTransportRuntime(session *onnxWorkerSession, admission ONNXWorkerAdmission) (*qualifiedONNXWorkerTransportRuntime, error) {
	if session == nil || !session.processAttested {
		return nil, fmt.Errorf("%w: host supervisor did not establish worker-process attestation", errONNXWorkerTransportUnavailable)
	}
	runtime, err := newONNXWorkerTransportRuntime(session, admission)
	if err != nil {
		return nil, err
	}
	runtime.qualified = true
	return &qualifiedONNXWorkerTransportRuntime{onnxWorkerTransportRuntime: runtime}, nil
}

func (r *onnxWorkerTransportRuntime) Probe(context.Context, ONNXWorkerAdmission) (ONNXWorkerProbeResult, error) {
	if !r.qualified || r.session == nil || r.session.probe.CapabilityID == "" {
		return ONNXWorkerProbeResult{}, workerError(ONNXWorkerReasonRuntime, ErrONNXWorkerUnavailable,
			"process transport has no qualified worker-process attestation")
	}
	return r.session.probe, nil
}

func (r *onnxWorkerTransportRuntime) EmbedTextWithText(ctx context.Context, req EmbedTextRequest, inputs []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
	if r == nil || r.session == nil || !r.session.alive() {
		return unavailableEmbedTextBatch(req, errONNXWorkerTransportUnavailable)
	}
	if err := validateONNXWorkerRequest(r.admission, req); err != nil {
		return unavailableEmbedTextBatch(req, err)
	}
	return r.session.embedText(ctx, req, inputs)
}

func (r *qualifiedONNXWorkerTransportRuntime) onnxWorkerProcessIdentity() *onnxWorkerProcessIdentity {
	if r == nil || !r.qualified || r.session == nil {
		return nil
	}
	return r.session.identity
}

func (r *qualifiedONNXWorkerTransportRuntime) onnxWorkerInvocationBinding() (string, string, string) {
	if r == nil || r.session == nil || !r.qualified {
		return "", "", ""
	}
	return r.session.sessionID, r.session.generation, r.session.fence
}

type onnxWorkerTransportBinding struct {
	SessionID     string
	ProfileDigest string
	WorkerDigest  string
	GenerationID  string
	FenceToken    string
}

type onnxWorkerTransportExecute func(context.Context, EmbedTextRequest, []ONNXWorkerTextInput) (EmbedTextResultBatch, error)

// serveONNXRuntimeAdapterTransport is the private worker-side connection from
// the admitted adapter to the process transport. It does not expose the
// adapter's in-process measurement method or establish worker READY status.
func serveONNXRuntimeAdapterTransport(ctx context.Context, conn net.Conn, binding onnxWorkerTransportBinding, adapter *ONNXRuntimeAdapter) error {
	if adapter == nil {
		return fmt.Errorf("%w: ONNX Runtime adapter is missing", errONNXWorkerTransportUnavailable)
	}
	return serveONNXWorkerTransport(ctx, conn, binding, adapter.measureEmbedTextWithText)
}

// serveONNXWorkerTransport is the worker-side loop after the nonce handshake.
// This function grants no path, repository, index, or credential access.
func serveONNXWorkerTransport(ctx context.Context, conn net.Conn, binding onnxWorkerTransportBinding, execute onnxWorkerTransportExecute) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if conn == nil || execute == nil || binding.SessionID == "" || binding.ProfileDigest == "" || binding.WorkerDigest == "" ||
		binding.GenerationID == "" || binding.FenceToken == "" {
		return fmt.Errorf("%w: worker binding is incomplete", errONNXWorkerTransportInvalid)
	}
	reader := bufio.NewReaderSize(conn, 32<<10)
	expectedSequence := uint64(1)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, payload, err := readONNXWorkerTransportPacket(reader, true)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		var env onnxWorkerTransportEnvelope
		if err := decodeONNXWorkerTransportFrame(frame, &env); err != nil {
			return err
		}
		if err := validateONNXWorkerTransportRequest(binding, expectedSequence, env); err != nil {
			return err
		}
		expectedSequence++
		inputs, err := decodeONNXWorkerTransportInputs(env, payload)
		if err != nil {
			return err
		}
		if err := validateONNXTextInputs(*env.Request, inputs); err != nil {
			return fmt.Errorf("%w: resolved text: %v", errONNXWorkerTransportInvalid, err)
		}
		batch, executeErr := execute(ctx, *env.Request, inputs)
		response := onnxWorkerTransportEnvelope{
			Schema: onnxWorkerTransportSchema, Kind: onnxWorkerTransportResponseKind,
			SessionID: binding.SessionID, ProfileDigest: binding.ProfileDigest, WorkerDigest: binding.WorkerDigest,
			GenerationID: binding.GenerationID, FenceToken: binding.FenceToken,
			RequestID: env.RequestID, RequestDigest: env.RequestDigest, Sequence: env.Sequence,
		}
		if executeErr != nil {
			response.Error = boundedIssueText(executeErr.Error())
		} else if err := ValidateEmbedTextResult(*env.Request, batch); err != nil {
			response.Error = boundedIssueText(fmt.Sprintf("worker result validation: %v", err))
		} else {
			response.Response = &batch
		}
		encoded, err := encodeONNXWorkerTransportFrame(response)
		if err != nil {
			return err
		}
		if err := writeONNXWorkerTransportBytes(conn, encoded); err != nil {
			return err
		}
	}
}

func validateONNXWorkerTransportRequest(binding onnxWorkerTransportBinding, expectedSequence uint64, env onnxWorkerTransportEnvelope) error {
	if env.Schema != onnxWorkerTransportSchema || env.Kind != onnxWorkerTransportRequestKind || env.Request == nil ||
		env.Response != nil || env.Error != "" || env.Sequence != expectedSequence {
		return fmt.Errorf("%w: malformed request envelope", errONNXWorkerTransportInvalid)
	}
	if env.SessionID != binding.SessionID || env.ProfileDigest != binding.ProfileDigest || env.WorkerDigest != binding.WorkerDigest ||
		env.GenerationID != binding.GenerationID || env.FenceToken != binding.FenceToken {
		return fmt.Errorf("%w: request process binding mismatch", errONNXWorkerTransportInvalid)
	}
	if err := ValidateEmbedTextRequest(*env.Request); err != nil {
		return fmt.Errorf("%w: request contract: %v", errONNXWorkerTransportInvalid, err)
	}
	if env.Request.Binding.SessionID != binding.SessionID || env.Request.Binding.WorkerDigest != binding.WorkerDigest ||
		env.Request.Binding.WorkerProfileDigest != binding.ProfileDigest || env.Request.Binding.GenerationID != binding.GenerationID ||
		fmt.Sprint(env.Request.Binding.FenceToken) != binding.FenceToken || env.RequestID != env.Request.Binding.RequestID {
		return fmt.Errorf("%w: request invocation binding mismatch", errONNXWorkerTransportInvalid)
	}
	digest, err := env.Request.CanonicalDigest()
	if err != nil || digest != env.RequestDigest {
		return fmt.Errorf("%w: request digest mismatch", errONNXWorkerTransportInvalid)
	}
	if len(env.Inputs) != len(env.Request.Segments) || env.PayloadBytes <= 0 || env.PayloadBytes > onnxWorkerTransportInputLimit ||
		ValidateEmbedTextDigest(env.PayloadDigest) != nil {
		return fmt.Errorf("%w: resolved text payload metadata", errONNXWorkerTransportInvalid)
	}
	return nil
}
