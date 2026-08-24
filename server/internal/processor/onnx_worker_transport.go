//go:build unix

package processor

// This file contains the host-owned control transport used after the private
// process proof. It carries bounded JSON control records followed by a bounded
// binary text payload resolved from host-issued handles. Vectors are still
// admitted by the host-side EMBED_TEXT validator. The transport is deliberately
// not a READY claim; a runtime probe must establish the pinned ONNX/BGE contract
// separately.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	onnxWorkerTransportSchema       = "restoreweave.onnx-worker-embed.v1"
	onnxWorkerTransportRequestKind  = "EMBED_TEXT_REQUEST"
	onnxWorkerTransportResponseKind = "EMBED_TEXT_RESPONSE"
	onnxWorkerTransportFrameLimit   = 128 << 20
	onnxWorkerTransportInputLimit   = MaxEmbedTextInputBytes + 8*MaxEmbedTextSegments
	onnxWorkerTransportTimeout      = 30 * time.Second
)

var (
	errONNXWorkerTransportUnavailable = errors.New("ONNX worker transport is unavailable")
	errONNXWorkerTransportInvalid     = errors.New("ONNX worker transport frame is invalid")
	errONNXWorkerTransportTimeout     = errors.New("ONNX worker transport timed out")
)

// The envelope repeats the session bindings even though the Unix connection
// is peer-authenticated. This prevents a worker from replaying a response for
// another generation, fence, or request on the same connection.
type onnxWorkerTransportEnvelope struct {
	Schema        string                         `json:"schema"`
	Kind          string                         `json:"kind"`
	SessionID     string                         `json:"session_id"`
	ProfileDigest string                         `json:"profile_digest"`
	WorkerDigest  string                         `json:"worker_digest"`
	GenerationID  string                         `json:"generation_id"`
	FenceToken    string                         `json:"fence_token"`
	RequestID     string                         `json:"request_id"`
	RequestDigest string                         `json:"request_digest"`
	Sequence      uint64                         `json:"sequence"`
	Request       *EmbedTextRequest              `json:"request,omitempty"`
	Inputs        []onnxWorkerTransportInputMeta `json:"inputs,omitempty"`
	PayloadBytes  int64                          `json:"payload_bytes,omitempty"`
	PayloadDigest string                         `json:"payload_digest,omitempty"`
	Response      *EmbedTextResultBatch          `json:"response,omitempty"`
	Error         string                         `json:"error,omitempty"`
}

type onnxWorkerTransportInputMeta struct {
	SegmentID  string `json:"segment_id"`
	TextDigest string `json:"text_digest"`
	TextBytes  int64  `json:"text_bytes"`
}

type onnxWorkerTransportResult struct {
	env onnxWorkerTransportEnvelope
	err error
}

// onnxWorkerTransport owns all post-handshake reads. A single in-flight
// request keeps request IDs and response ordering unambiguous while the pump
// lets EOF invalidate the session even when no request is waiting.
type onnxWorkerTransport struct {
	conn    net.Conn
	session *onnxWorkerSession

	requestMu  sync.Mutex
	writeMu    sync.Mutex
	results    chan onnxWorkerTransportResult
	done       chan struct{}
	closeOnce  sync.Once
	errMu      sync.RWMutex
	err        error
	sequence   uint64
	requestIDs map[string]struct{}
}

func newONNXWorkerTransport(conn net.Conn, session *onnxWorkerSession) *onnxWorkerTransport {
	return &onnxWorkerTransport{
		conn: conn, session: session, results: make(chan onnxWorkerTransportResult, 1), done: make(chan struct{}),
		requestIDs: make(map[string]struct{}),
	}
}

func (t *onnxWorkerTransport) run() {
	reader := bufio.NewReaderSize(t.conn, 32<<10)
	for {
		line, err := readONNXWorkerTransportFrame(reader)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				t.setErr(fmt.Errorf("%w: %v", errONNXWorkerTransportTimeout, err))
			} else if !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
				t.setErr(fmt.Errorf("%w: read response: %v", errONNXWorkerTransportInvalid, err))
			} else {
				t.setErr(fmt.Errorf("%w: response stream ended: %v", errONNXWorkerTransportUnavailable, err))
			}
			return
		}
		var env onnxWorkerTransportEnvelope
		if err := decodeONNXWorkerTransportFrame(line, &env); err != nil {
			t.setErr(err)
			return
		}
		if err := validateONNXWorkerTransportEnvelopeShape(env); err != nil {
			t.setErr(err)
			return
		}
		select {
		case t.results <- onnxWorkerTransportResult{env: env}:
		case <-t.done:
			return
		default:
			t.setErr(fmt.Errorf("%w: unsolicited or concurrent response", errONNXWorkerTransportInvalid))
			return
		}
	}
}

func (t *onnxWorkerTransport) setErr(err error) {
	if err == nil {
		err = errONNXWorkerTransportUnavailable
	}
	t.errMu.Lock()
	if t.err == nil {
		t.err = err
	}
	t.errMu.Unlock()
	t.closeOnce.Do(func() {
		close(t.done)
		_ = t.conn.Close()
		if t.session != nil {
			t.session.mu.RLock()
			sessionClosing, cmd := t.session.closed, t.session.cmd
			t.session.mu.RUnlock()
			if !sessionClosing && cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			t.session.markConnectionDone()
		}
	})
}

func (t *onnxWorkerTransport) currentErr() error {
	t.errMu.RLock()
	defer t.errMu.RUnlock()
	return t.err
}

func (t *onnxWorkerTransport) close() error {
	t.setErr(errONNXWorkerTransportUnavailable)
	return nil
}

// embedText is intentionally package-private. Callers must still pass the
// result through NegotiatedONNXWorker/ValidateEmbedTextResult before any
// index generation can consume it.
func (s *onnxWorkerSession) embedText(ctx context.Context, req EmbedTextRequest, inputs []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
	if s == nil || s.transport == nil {
		return unavailableEmbedTextBatch(req, errONNXWorkerTransportUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ValidateEmbedTextRequest(req); err != nil {
		return EmbedTextResultBatch{}, err
	}
	if req.Binding.SessionID != s.sessionID || req.Binding.WorkerDigest != s.worker ||
		req.Binding.WorkerProfileDigest != s.profile || req.Binding.GenerationID != s.generation ||
		fmt.Sprint(req.Binding.FenceToken) != s.fence {
		return unavailableEmbedTextBatch(req, fmt.Errorf("%w: request session binding does not match worker session", errONNXWorkerTransportInvalid))
	}
	if err := validateONNXTextInputs(req, inputs); err != nil {
		return unavailableEmbedTextBatch(req, fmt.Errorf("%w: %v", errONNXWorkerTransportInvalid, err))
	}
	return s.transport.request(ctx, req, inputs)
}

func (t *onnxWorkerTransport) request(ctx context.Context, req EmbedTextRequest, inputs []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
	t.requestMu.Lock()
	defer t.requestMu.Unlock()
	if err := t.currentErr(); err != nil {
		return unavailableEmbedTextBatch(req, err)
	}
	if t.session == nil || !t.session.alive() {
		return unavailableEmbedTextBatch(req, errONNXWorkerTransportUnavailable)
	}
	digest, err := req.CanonicalDigest()
	if err != nil {
		return EmbedTextResultBatch{}, err
	}
	if _, exists := t.requestIDs[req.Binding.RequestID]; exists {
		return unavailableEmbedTextBatch(req, fmt.Errorf("%w: request ID was already used in this session", errONNXWorkerTransportInvalid))
	}
	if t.sequence == ^uint64(0) {
		t.setErr(fmt.Errorf("%w: request sequence exhausted", errONNXWorkerTransportInvalid))
		return unavailableEmbedTextBatch(req, t.currentErr())
	}
	sequence := t.sequence + 1
	env := onnxWorkerTransportEnvelope{
		Schema: onnxWorkerTransportSchema, Kind: onnxWorkerTransportRequestKind,
		SessionID: t.session.sessionID, ProfileDigest: t.session.profile, WorkerDigest: t.session.worker,
		GenerationID: t.session.generation, FenceToken: t.session.fence, RequestID: req.Binding.RequestID,
		RequestDigest: digest, Sequence: sequence, Request: &req,
	}
	frame, err := encodeONNXWorkerTransportRequestFrame(env, inputs)
	if err != nil {
		return EmbedTextResultBatch{}, err
	}
	t.sequence = sequence
	t.requestIDs[req.Binding.RequestID] = struct{}{}
	deadline, hasDeadline := onnxWorkerTransportDeadline(ctx)
	if hasDeadline {
		if err := t.conn.SetDeadline(deadline); err != nil {
			return unavailableEmbedTextBatch(req, fmt.Errorf("%w: set deadline: %v", errONNXWorkerTransportUnavailable, err))
		}
	} else {
		_ = t.conn.SetDeadline(time.Now().Add(onnxWorkerTransportTimeout))
	}
	t.writeMu.Lock()
	writeErr := writeONNXWorkerTransportBytes(t.conn, frame)
	t.writeMu.Unlock()
	if writeErr != nil {
		t.setErr(fmt.Errorf("%w: write request: %v", errONNXWorkerTransportUnavailable, writeErr))
		return unavailableEmbedTextBatch(req, t.currentErr())
	}
	select {
	case result := <-t.results:
		_ = t.conn.SetDeadline(time.Time{})
		if result.err != nil {
			return unavailableEmbedTextBatch(req, result.err)
		}
		if err := validateONNXWorkerTransportResponse(t.session, req, digest, sequence, result.env); err != nil {
			t.setErr(err)
			return unavailableEmbedTextBatch(req, err)
		}
		return *result.env.Response, nil
	case <-ctx.Done():
		t.setErr(fmt.Errorf("%w: %v", errONNXWorkerTransportTimeout, ctx.Err()))
		return unavailableEmbedTextBatch(req, t.currentErr())
	case <-t.done:
		return unavailableEmbedTextBatch(req, t.currentErr())
	case <-time.After(onnxWorkerTransportTimeout):
		t.setErr(errONNXWorkerTransportTimeout)
		return unavailableEmbedTextBatch(req, t.currentErr())
	}
}

func onnxWorkerTransportDeadline(ctx context.Context) (time.Time, bool) {
	deadline := time.Now().Add(onnxWorkerTransportTimeout)
	if ctx != nil {
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			return ctxDeadline, true
		}
	}
	return deadline, true
}

func validateONNXWorkerTransportResponse(session *onnxWorkerSession, req EmbedTextRequest, digest string, sequence uint64, env onnxWorkerTransportEnvelope) error {
	if session == nil {
		return fmt.Errorf("%w: worker process session is missing", errONNXWorkerTransportUnavailable)
	}
	if env.Schema != onnxWorkerTransportSchema || env.Kind != onnxWorkerTransportResponseKind {
		return fmt.Errorf("%w: response envelope shape", errONNXWorkerTransportInvalid)
	}
	if env.SessionID != session.sessionID || env.ProfileDigest != session.profile || env.WorkerDigest != session.worker ||
		env.GenerationID != session.generation || env.FenceToken != session.fence || env.RequestID != req.Binding.RequestID ||
		env.RequestDigest != digest || env.Sequence != sequence {
		return fmt.Errorf("%w: response binding mismatch", errONNXWorkerTransportInvalid)
	}
	if !session.alive() {
		return fmt.Errorf("%w: worker process is no longer alive", errONNXWorkerTransportUnavailable)
	}
	if env.Error != "" {
		return fmt.Errorf("%w: worker returned failure: %s", errONNXWorkerTransportUnavailable, env.Error)
	}
	if env.Response == nil {
		return fmt.Errorf("%w: response body is missing", errONNXWorkerTransportInvalid)
	}
	if err := ValidateEmbedTextResult(req, *env.Response); err != nil {
		return fmt.Errorf("%w: host result validation: %v", errONNXWorkerTransportInvalid, err)
	}
	return nil
}

func validateONNXWorkerTransportEnvelopeShape(env onnxWorkerTransportEnvelope) error {
	if env.Schema != onnxWorkerTransportSchema || env.Kind != onnxWorkerTransportResponseKind ||
		env.SessionID == "" || env.ProfileDigest == "" || env.WorkerDigest == "" || env.GenerationID == "" ||
		env.FenceToken == "" || env.RequestID == "" || env.RequestDigest == "" || env.Sequence == 0 {
		return fmt.Errorf("%w: malformed response envelope", errONNXWorkerTransportInvalid)
	}
	if env.Request != nil || len(env.Inputs) != 0 || env.PayloadBytes != 0 || env.PayloadDigest != "" {
		return fmt.Errorf("%w: response contains a request body", errONNXWorkerTransportInvalid)
	}
	if env.Error != "" && !validateEmbedTextReason(env.Error) {
		return fmt.Errorf("%w: response error is not bounded", errONNXWorkerTransportInvalid)
	}
	return nil
}

func encodeONNXWorkerTransportFrame(env onnxWorkerTransportEnvelope) ([]byte, error) {
	if env.Schema != onnxWorkerTransportSchema {
		return nil, fmt.Errorf("%w: unsupported schema", errONNXWorkerTransportInvalid)
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal: %v", errONNXWorkerTransportInvalid, err)
	}
	if len(data)+1 > onnxWorkerTransportFrameLimit {
		return nil, fmt.Errorf("%w: frame exceeds %d bytes", errONNXWorkerTransportInvalid, onnxWorkerTransportFrameLimit)
	}
	return append(data, '\n'), nil
}

func writeONNXWorkerTransportBytes(writer io.Writer, data []byte) error {
	for len(data) != 0 {
		n, err := writer.Write(data)
		if n < 0 || n > len(data) {
			return fmt.Errorf("%w: writer returned %d bytes for a %d-byte buffer", io.ErrShortWrite, n, len(data))
		}
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func readONNXWorkerTransportFrame(reader *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		frame = append(frame, chunk...)
		if len(frame) > onnxWorkerTransportFrameLimit {
			return nil, fmt.Errorf("%w: frame exceeds %d bytes", errONNXWorkerTransportInvalid, onnxWorkerTransportFrameLimit)
		}
		if err == nil {
			return frame[:len(frame)-1], nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return nil, err
	}
}

// Request text travels in a bounded binary payload after the JSON control
// envelope. The envelope carries only segment IDs and digests, so control
// records never accidentally log or replay source text.
func encodeONNXWorkerTransportRequestFrame(env onnxWorkerTransportEnvelope, inputs []ONNXWorkerTextInput) ([]byte, error) {
	if env.Kind != onnxWorkerTransportRequestKind || env.Request == nil || env.Sequence == 0 {
		return nil, fmt.Errorf("%w: request envelope required", errONNXWorkerTransportInvalid)
	}
	if err := validateONNXTextInputs(*env.Request, inputs); err != nil {
		return nil, fmt.Errorf("%w: input payload: %v", errONNXWorkerTransportInvalid, err)
	}
	payload := make([]byte, 0)
	meta := make([]onnxWorkerTransportInputMeta, 0, len(inputs))
	for _, input := range inputs {
		if int64(len(payload)) > onnxWorkerTransportInputLimit-int64(len(input.Text))-8 {
			return nil, fmt.Errorf("%w: input payload exceeds bound", errONNXWorkerTransportInvalid)
		}
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(input.Text)))
		payload = append(payload, length[:]...)
		payload = append(payload, input.Text...)
		textDigest := sha256.Sum256(input.Text)
		meta = append(meta, onnxWorkerTransportInputMeta{
			SegmentID: input.SegmentID, TextDigest: "sha256:" + hex.EncodeToString(textDigest[:]), TextBytes: int64(len(input.Text)),
		})
	}
	payloadDigest := sha256.Sum256(payload)
	env.Inputs = meta
	env.PayloadBytes = int64(len(payload))
	env.PayloadDigest = "sha256:" + hex.EncodeToString(payloadDigest[:])
	frame, err := encodeONNXWorkerTransportFrame(env)
	if err != nil {
		return nil, err
	}
	if int64(len(frame))+int64(len(payload)) > onnxWorkerTransportFrameLimit {
		return nil, fmt.Errorf("%w: request packet exceeds frame bound", errONNXWorkerTransportInvalid)
	}
	return append(frame, payload...), nil
}

func readONNXWorkerTransportPacket(reader *bufio.Reader, wantPayload bool) ([]byte, []byte, error) {
	frame, err := readONNXWorkerTransportFrame(reader)
	if err != nil {
		return nil, nil, err
	}
	if !wantPayload {
		return frame, nil, nil
	}
	var env onnxWorkerTransportEnvelope
	if err := decodeONNXWorkerTransportFrame(frame, &env); err != nil {
		return nil, nil, err
	}
	if env.PayloadBytes <= 0 || env.PayloadBytes > onnxWorkerTransportInputLimit || env.PayloadDigest == "" {
		return nil, nil, fmt.Errorf("%w: invalid input payload bounds", errONNXWorkerTransportInvalid)
	}
	payload := make([]byte, env.PayloadBytes)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, nil, fmt.Errorf("%w: input payload truncated: %v", errONNXWorkerTransportInvalid, err)
	}
	sum := sha256.Sum256(payload)
	if env.PayloadDigest != "sha256:"+hex.EncodeToString(sum[:]) {
		return nil, nil, fmt.Errorf("%w: input payload digest mismatch", errONNXWorkerTransportInvalid)
	}
	return frame, payload, nil
}

func decodeONNXWorkerTransportInputs(env onnxWorkerTransportEnvelope, payload []byte) ([]ONNXWorkerTextInput, error) {
	if env.PayloadBytes != int64(len(payload)) || len(env.Inputs) == 0 {
		return nil, fmt.Errorf("%w: input payload metadata mismatch", errONNXWorkerTransportInvalid)
	}
	inputs := make([]ONNXWorkerTextInput, 0, len(env.Inputs))
	offset := 0
	for _, meta := range env.Inputs {
		if meta.TextBytes < 0 || offset > len(payload)-8 {
			return nil, fmt.Errorf("%w: input payload length", errONNXWorkerTransportInvalid)
		}
		length := binary.BigEndian.Uint64(payload[offset : offset+8])
		offset += 8
		if length != uint64(meta.TextBytes) || length > uint64(len(payload)-offset) {
			return nil, fmt.Errorf("%w: input payload segment length", errONNXWorkerTransportInvalid)
		}
		text := append([]byte(nil), payload[offset:offset+int(length)]...)
		offset += int(length)
		sum := sha256.Sum256(text)
		if meta.TextDigest != "sha256:"+hex.EncodeToString(sum[:]) {
			return nil, fmt.Errorf("%w: input segment digest", errONNXWorkerTransportInvalid)
		}
		inputs = append(inputs, ONNXWorkerTextInput{SegmentID: meta.SegmentID, Text: text})
	}
	if offset != len(payload) {
		return nil, fmt.Errorf("%w: trailing input payload", errONNXWorkerTransportInvalid)
	}
	return inputs, nil
}

func decodeONNXWorkerTransportFrame(frame []byte, value any) error {
	if len(frame) == 0 || len(frame) > onnxWorkerTransportFrameLimit {
		return fmt.Errorf("%w: frame length", errONNXWorkerTransportInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: decode: %v", errONNXWorkerTransportInvalid, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON", errONNXWorkerTransportInvalid)
	}
	return nil
}
