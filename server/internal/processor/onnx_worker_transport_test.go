//go:build unix

package processor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestONNXWorkerTransportHelper(t *testing.T) {
	if os.Getenv(onnxWorkerSocketEnv) == "" {
		return
	}
	nonceFD, err := strconv.Atoi(os.Getenv(onnxWorkerNonceFDE))
	if err != nil || nonceFD != 3 {
		os.Exit(40)
	}
	nonceFile := os.NewFile(uintptr(nonceFD), "onnx-worker-transport-nonce")
	if nonceFile == nil {
		os.Exit(40)
	}
	nonce := make([]byte, onnxWorkerNonceBytes)
	if _, err := io.ReadFull(nonceFile, nonce); err != nil {
		os.Exit(40)
	}
	_ = nonceFile.Close()
	conn, err := net.Dial("unix", os.Getenv(onnxWorkerSocketEnv))
	if err != nil {
		os.Exit(41)
	}
	defer conn.Close()
	var challenge onnxWorkerChallenge
	if err := readONNXWorkerHandshake(conn, &challenge); err != nil {
		os.Exit(42)
	}
	nonceSum := sha256.Sum256(nonce)
	if challenge.NonceDigest != "sha256:"+hex.EncodeToString(nonceSum[:]) {
		os.Exit(43)
	}
	sandboxDigest := os.Getenv(onnxWorkerSandboxEnv)
	binding := onnxWorkerHandshakeBinding{
		Schema: challenge.Schema, SessionID: challenge.SessionID, Protocol: challenge.Protocol,
		ProfileDigest: challenge.ProfileDigest, WorkerDigest: challenge.WorkerDigest,
		GenerationID: challenge.GenerationID, FenceToken: challenge.FenceToken,
		SandboxPolicyDigest: sandboxDigest, PeerPID: challenge.PeerPID, PeerUID: challenge.PeerUID, PeerGID: challenge.PeerGID,
	}
	ready := onnxWorkerReady{
		Schema: challenge.Schema, SessionID: challenge.SessionID, Protocol: challenge.Protocol,
		ProfileDigest: challenge.ProfileDigest, WorkerDigest: challenge.WorkerDigest,
		GenerationID: challenge.GenerationID, FenceToken: challenge.FenceToken,
		PeerPID: challenge.PeerPID, SandboxPolicyDigest: sandboxDigest, PID: os.Getpid(),
		MAC: onnxWorkerHandshakeMAC(nonce, binding),
	}
	clear(nonce)
	if err := writeONNXWorkerHandshake(conn, ready); err != nil {
		os.Exit(44)
	}
	mode := strings.TrimPrefix(challenge.GenerationID, "transport-")
	transportBinding := onnxWorkerTransportBinding{
		SessionID: challenge.SessionID, ProfileDigest: challenge.ProfileDigest, WorkerDigest: challenge.WorkerDigest,
		GenerationID: challenge.GenerationID, FenceToken: challenge.FenceToken,
	}
	switch mode {
	case "success":
		_ = serveONNXWorkerTransport(context.Background(), conn, transportBinding,
			func(_ context.Context, req EmbedTextRequest, _ []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
				return validONNXBatch(req), nil
			})
	case "malformed":
		_, _ = readONNXWorkerTransportFrame(bufio.NewReader(conn))
		_, _ = conn.Write([]byte("{\"schema\":\n"))
	case "timeout":
		_, _ = readONNXWorkerTransportFrame(bufio.NewReader(conn))
		select {}
	case "eof":
		_, _ = readONNXWorkerTransportFrame(bufio.NewReader(conn))
		return
	case "bad-binding":
		frame, err := readONNXWorkerTransportFrame(bufio.NewReader(conn))
		if err != nil {
			os.Exit(45)
		}
		var request onnxWorkerTransportEnvelope
		if err := decodeONNXWorkerTransportFrame(frame, &request); err != nil || request.Request == nil {
			os.Exit(45)
		}
		batch := validONNXBatch(*request.Request)
		response := onnxWorkerTransportEnvelope{
			Schema: onnxWorkerTransportSchema, Kind: onnxWorkerTransportResponseKind,
			SessionID: request.SessionID, ProfileDigest: request.ProfileDigest, WorkerDigest: request.WorkerDigest,
			GenerationID: request.GenerationID, FenceToken: "8", RequestID: request.RequestID,
			RequestDigest: request.RequestDigest, Sequence: request.Sequence, Response: &batch,
		}
		encoded, _ := encodeONNXWorkerTransportFrame(response)
		_, _ = conn.Write(encoded)
	case "replay":
		reader := bufio.NewReader(conn)
		firstFrame, _, err := readONNXWorkerTransportPacket(reader, true)
		if err != nil {
			os.Exit(47)
		}
		var first onnxWorkerTransportEnvelope
		if err := decodeONNXWorkerTransportFrame(firstFrame, &first); err != nil || first.Request == nil {
			os.Exit(47)
		}
		batch := validONNXBatch(*first.Request)
		stale := onnxWorkerTransportEnvelope{
			Schema: onnxWorkerTransportSchema, Kind: onnxWorkerTransportResponseKind,
			SessionID: first.SessionID, ProfileDigest: first.ProfileDigest, WorkerDigest: first.WorkerDigest,
			GenerationID: first.GenerationID, FenceToken: first.FenceToken, RequestID: first.RequestID,
			RequestDigest: first.RequestDigest, Sequence: first.Sequence, Response: &batch,
		}
		encoded, _ := encodeONNXWorkerTransportFrame(stale)
		_ = writeONNXWorkerTransportBytes(conn, encoded)
		if _, _, err := readONNXWorkerTransportPacket(reader, true); err != nil {
			os.Exit(47)
		}
		_ = writeONNXWorkerTransportBytes(conn, encoded)
	case "exit-descendant-response":
		reader := bufio.NewReader(conn)
		frame, _, err := readONNXWorkerTransportPacket(reader, true)
		if err != nil {
			os.Exit(48)
		}
		var request onnxWorkerTransportEnvelope
		if err := decodeONNXWorkerTransportFrame(frame, &request); err != nil || request.Request == nil {
			os.Exit(48)
		}
		batch := validONNXBatch(*request.Request)
		response := onnxWorkerTransportEnvelope{
			Schema: onnxWorkerTransportSchema, Kind: onnxWorkerTransportResponseKind,
			SessionID: request.SessionID, ProfileDigest: request.ProfileDigest, WorkerDigest: request.WorkerDigest,
			GenerationID: request.GenerationID, FenceToken: request.FenceToken, RequestID: request.RequestID,
			RequestDigest: request.RequestDigest, Sequence: request.Sequence, Response: &batch,
		}
		encoded, err := encodeONNXWorkerTransportFrame(response)
		if err != nil {
			os.Exit(48)
		}
		unixConn, ok := conn.(*net.UnixConn)
		if !ok {
			os.Exit(48)
		}
		connectionFile, err := unixConn.File()
		if err != nil {
			os.Exit(48)
		}
		descendant := exec.Command(os.Args[0], "-test.run=TestONNXWorkerTransportDescendantResponseHolder", "-test.v=false")
		descendant.Env = []string{
			"RESTOREWEAVE_ONNX_DESCENDANT_FD=3",
			"RESTOREWEAVE_ONNX_DESCENDANT_RESPONSE=" + base64.StdEncoding.EncodeToString(encoded),
			"RESTOREWEAVE_ONNX_DESCENDANT_PARENT_PID=" + strconv.Itoa(os.Getpid()),
		}
		descendant.ExtraFiles = []*os.File{connectionFile}
		if err := descendant.Start(); err != nil {
			_ = connectionFile.Close()
			os.Exit(48)
		}
		_ = connectionFile.Close()
		_ = descendant.Process.Release()
	default:
		os.Exit(46)
	}
}

func TestONNXWorkerTransportDescendantResponseHolder(t *testing.T) {
	if os.Getenv("RESTOREWEAVE_ONNX_DESCENDANT_FD") != "3" {
		return
	}
	connectionFile := os.NewFile(3, "onnx-worker-descendant-connection")
	if connectionFile == nil {
		os.Exit(49)
	}
	conn, err := net.FileConn(connectionFile)
	_ = connectionFile.Close()
	if err != nil {
		os.Exit(49)
	}
	defer conn.Close()
	parentPID, err := strconv.Atoi(os.Getenv("RESTOREWEAVE_ONNX_DESCENDANT_PARENT_PID"))
	if err != nil || parentPID <= 0 {
		os.Exit(49)
	}
	// Wait until the original worker has actually exited (and is no longer
	// visible to kill(2)). This makes the descendant response test independent
	// of scheduler and race-detector timing.
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := syscall.Kill(parentPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil && !errors.Is(err, syscall.EPERM) || time.Now().After(deadline) {
			os.Exit(49)
		}
		time.Sleep(10 * time.Millisecond)
	}
	response, err := base64.StdEncoding.DecodeString(os.Getenv("RESTOREWEAVE_ONNX_DESCENDANT_RESPONSE"))
	if err != nil || len(response) == 0 {
		os.Exit(49)
	}
	if err := writeONNXWorkerTransportBytes(conn, response); err != nil {
		os.Exit(49)
	}
	// Keep the inherited connection open until the host has rejected the
	// response specifically because the nonce-bound process exited.
	_, _ = io.Copy(io.Discard, conn)
}

func TestONNXWorkerTransportBindsSessionRuntimeAndRoundTrips(t *testing.T) {
	admission := testONNXAdmission(t)
	session := startTransportTestSession(t, admission, "success")
	defer session.close()
	runtimeAdapter, err := newONNXWorkerTransportRuntime(session, admission)
	if err != nil {
		t.Fatalf("bind process runtime: %v", err)
	}
	if _, ok := any(runtimeAdapter).(interface {
		onnxWorkerProcessIdentity() *onnxWorkerProcessIdentity
	}); ok {
		t.Fatal("transport runtime manufactured a process attestation identity")
	}
	if _, err := runtimeAdapter.Probe(context.Background(), admission); err == nil || !errors.Is(err, ErrONNXWorkerUnavailable) {
		t.Fatalf("transport-only probe = %v, want typed unavailable", err)
	}
	req, inputs := transportTestRequest(t, session, admission)
	batch, err := runtimeAdapter.EmbedTextWithText(context.Background(), req, inputs)
	if err != nil {
		t.Fatalf("embed round trip: %v", err)
	}
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		t.Fatalf("host rejected round-trip batch: %v", err)
	}
	if !session.alive() {
		t.Fatal("successful transport invalidated its process session")
	}
}

type transportONNXRuntimeBackend struct {
	calls int
}

func (b *transportONNXRuntimeBackend) Probe(context.Context) (onnxRuntimeProbeFacts, error) {
	return onnxRuntimeProbeFacts{}, nil
}

func (b *transportONNXRuntimeBackend) Close() error { return nil }

func (b *transportONNXRuntimeBackend) measureEmbedTextWithText(_ context.Context, req EmbedTextRequest, _ []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
	b.calls++
	return validONNXBatch(req), nil
}

func TestONNXRuntimeAdapterTransportUsesPrivateMeasurementPath(t *testing.T) {
	admission := testONNXAdmission(t)
	backend := &transportONNXRuntimeBackend{}
	adapter := &ONNXRuntimeAdapter{admission: admission, backend: backend}
	hostConn, workerConn := net.Pipe()
	defer hostConn.Close()
	defer workerConn.Close()
	binding := onnxWorkerTransportBinding{
		SessionID: "adapter-session", ProfileDigest: admission.ProfileDigest, WorkerDigest: admission.WorkerDigest,
		GenerationID: "adapter-generation", FenceToken: "7",
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- serveONNXRuntimeAdapterTransport(context.Background(), workerConn, binding, adapter)
	}()
	session := &onnxWorkerSession{
		sessionID: binding.SessionID, profile: binding.ProfileDigest, worker: binding.WorkerDigest,
		generation: binding.GenerationID, fence: binding.FenceToken,
	}
	req, inputs := transportTestRequest(t, session, admission)
	digest, err := req.CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	packet, err := encodeONNXWorkerTransportRequestFrame(onnxWorkerTransportEnvelope{
		Schema: onnxWorkerTransportSchema, Kind: onnxWorkerTransportRequestKind,
		SessionID: binding.SessionID, ProfileDigest: binding.ProfileDigest, WorkerDigest: binding.WorkerDigest,
		GenerationID: binding.GenerationID, FenceToken: binding.FenceToken, RequestID: req.Binding.RequestID,
		RequestDigest: digest, Sequence: 1, Request: &req,
	}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeONNXWorkerTransportBytes(hostConn, packet); err != nil {
		t.Fatalf("write adapter request: %v", err)
	}
	frame, err := readONNXWorkerTransportFrame(bufio.NewReader(hostConn))
	if err != nil {
		t.Fatalf("read adapter response: %v", err)
	}
	var response onnxWorkerTransportEnvelope
	if err := decodeONNXWorkerTransportFrame(frame, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "" || response.Response == nil {
		t.Fatalf("adapter transport response = %+v", response)
	}
	if err := ValidateEmbedTextResult(req, *response.Response); err != nil {
		t.Fatalf("adapter transport returned invalid result: %v", err)
	}
	_ = hostConn.Close()
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("serve adapter transport: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("adapter transport did not stop after connection close")
	}
	if backend.calls != 1 {
		t.Fatalf("private adapter measurement calls = %d, want 1", backend.calls)
	}
}

func TestONNXWorkerTransportControlEnvelopeOmitsTextPayload(t *testing.T) {
	admission := testONNXAdmission(t)
	session := &onnxWorkerSession{sessionID: "session-transport", profile: admission.ProfileDigest, worker: admission.WorkerDigest, generation: "generation-1", fence: "7"}
	req, inputs := transportTestRequest(t, session, admission)
	digest, err := req.CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	frame, err := encodeONNXWorkerTransportRequestFrame(onnxWorkerTransportEnvelope{
		Schema: onnxWorkerTransportSchema, Kind: onnxWorkerTransportRequestKind,
		SessionID: session.sessionID, ProfileDigest: session.profile, WorkerDigest: session.worker,
		GenerationID: session.generation, FenceToken: session.fence, RequestID: req.Binding.RequestID,
		RequestDigest: digest, Sequence: 1, Request: &req,
	}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	controlEnd := bytes.IndexByte(frame, '\n')
	if controlEnd < 0 {
		t.Fatal("request frame has no control delimiter")
	}
	for _, input := range inputs {
		if bytes.Contains(frame[:controlEnd], input.Text) {
			t.Fatal("source text appeared in the JSON control envelope")
		}
	}
}

func TestONNXWorkerTransportRejectsTamperedTextPayload(t *testing.T) {
	admission := testONNXAdmission(t)
	session := &onnxWorkerSession{sessionID: "session-transport", profile: admission.ProfileDigest, worker: admission.WorkerDigest, generation: "generation-1", fence: "7"}
	req, inputs := transportTestRequest(t, session, admission)
	digest, err := req.CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	packet, err := encodeONNXWorkerTransportRequestFrame(onnxWorkerTransportEnvelope{
		Schema: onnxWorkerTransportSchema, Kind: onnxWorkerTransportRequestKind,
		SessionID: session.sessionID, ProfileDigest: session.profile, WorkerDigest: session.worker,
		GenerationID: session.generation, FenceToken: session.fence, RequestID: req.Binding.RequestID,
		RequestDigest: digest, Sequence: 1, Request: &req,
	}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	packet[len(packet)-1] ^= 0xff
	if _, _, err := readONNXWorkerTransportPacket(bufio.NewReader(bytes.NewReader(packet)), true); err == nil || !errors.Is(err, errONNXWorkerTransportInvalid) {
		t.Fatalf("tampered text payload error = %v, want invalid transport", err)
	}
}

func TestONNXWorkerTransportRetriesShortWrites(t *testing.T) {
	written := &shortONNXWorkerTransportWriter{max: 3}
	want := []byte("bounded transport frame")
	if err := writeONNXWorkerTransportBytes(written, want); err != nil {
		t.Fatalf("write short chunks: %v", err)
	}
	if !bytes.Equal(written.Bytes(), want) || written.calls < 2 {
		t.Fatalf("short write result = %q in %d calls", written.Bytes(), written.calls)
	}
}

type invalidCountONNXWorkerTransportWriter func([]byte) (int, error)

func (f invalidCountONNXWorkerTransportWriter) Write(value []byte) (int, error) {
	return f(value)
}

func TestONNXWorkerTransportRejectsInvalidWriterCounts(t *testing.T) {
	for _, tt := range []struct {
		name  string
		write invalidCountONNXWorkerTransportWriter
	}{
		{name: "negative", write: func([]byte) (int, error) { return -1, nil }},
		{name: "too large", write: func(value []byte) (int, error) { return len(value) + 1, nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := writeONNXWorkerTransportBytes(tt.write, []byte("frame")); err == nil || !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("invalid writer count error = %v, want io.ErrShortWrite", err)
			}
		})
	}
}

type shortONNXWorkerTransportWriter struct {
	bytes.Buffer
	max   int
	calls int
}

func (w *shortONNXWorkerTransportWriter) Write(value []byte) (int, error) {
	w.calls++
	if len(value) > w.max {
		value = value[:w.max]
	}
	return w.Buffer.Write(value)
}

func TestONNXWorkerTransportRejectsRequestSessionMismatchBeforeSend(t *testing.T) {
	admission := testONNXAdmission(t)
	session := startTransportTestSession(t, admission, "success")
	defer session.close()
	runtimeAdapter, err := newONNXWorkerTransportRuntime(session, admission)
	if err != nil {
		t.Fatal(err)
	}
	req, inputs := transportTestRequest(t, session, admission)
	req.Binding.SessionID = "other-session"
	if _, err := runtimeAdapter.EmbedTextWithText(context.Background(), req, inputs); err == nil || !errors.Is(err, errONNXWorkerTransportInvalid) {
		t.Fatalf("mismatched request error = %v, want invalid transport", err)
	}
	if !session.alive() {
		t.Fatal("locally rejected request invalidated healthy worker session")
	}
}

func TestONNXWorkerTransportRejectsReplayedResponse(t *testing.T) {
	admission := testONNXAdmission(t)
	session := startTransportTestSession(t, admission, "replay")
	defer session.close()
	runtimeAdapter, err := newONNXWorkerTransportRuntime(session, admission)
	if err != nil {
		t.Fatal(err)
	}
	req, inputs := transportTestRequest(t, session, admission)
	if _, err := runtimeAdapter.EmbedTextWithText(context.Background(), req, inputs); err != nil {
		t.Fatalf("first response: %v", err)
	}
	req.Binding.RequestID = "request-transport-2"
	if _, err := runtimeAdapter.EmbedTextWithText(context.Background(), req, inputs); err == nil || !errors.Is(err, errONNXWorkerTransportInvalid) {
		t.Fatalf("replayed response error = %v, want invalid transport", err)
	}
	select {
	case <-session.done:
	case <-time.After(2 * time.Second):
		t.Fatal("replayed response did not terminate worker")
	}
}

func TestONNXWorkerTransportRejectsDuplicateRequestID(t *testing.T) {
	admission := testONNXAdmission(t)
	session := startTransportTestSession(t, admission, "success")
	defer session.close()
	runtimeAdapter, err := newONNXWorkerTransportRuntime(session, admission)
	if err != nil {
		t.Fatal(err)
	}
	req, inputs := transportTestRequest(t, session, admission)
	if _, err := runtimeAdapter.EmbedTextWithText(context.Background(), req, inputs); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := runtimeAdapter.EmbedTextWithText(context.Background(), req, inputs); err == nil || !errors.Is(err, errONNXWorkerTransportInvalid) {
		t.Fatalf("duplicate request error = %v, want invalid transport", err)
	}
	if !session.alive() {
		t.Fatal("locally rejected duplicate invalidated healthy worker session")
	}
}

func TestONNXWorkerTransportRejectsResponseFromExitedWorkerDescendant(t *testing.T) {
	admission := testONNXAdmission(t)
	session := startTransportTestSession(t, admission, "exit-descendant-response")
	defer session.close()
	runtimeAdapter, err := newONNXWorkerTransportRuntime(session, admission)
	if err != nil {
		t.Fatal(err)
	}
	req, inputs := transportTestRequest(t, session, admission)
	if _, err := runtimeAdapter.EmbedTextWithText(context.Background(), req, inputs); err == nil ||
		!errors.Is(err, errONNXWorkerTransportUnavailable) || !strings.Contains(err.Error(), "worker process is no longer alive") {
		t.Fatalf("descendant response error = %v, want unavailable after worker exit", err)
	}
	if session.alive() {
		t.Fatal("original worker process remained alive after handing off the connection")
	}
	// Once the process binding is dead, a later request must not be sent to the
	// inherited connection even while the descendant keeps its file descriptor.
	req.Binding.RequestID = "request-transport-descendant-2"
	if _, err := runtimeAdapter.EmbedTextWithText(context.Background(), req, inputs); err == nil || !errors.Is(err, errONNXWorkerTransportUnavailable) {
		t.Fatalf("post-exit request error = %v, want unavailable", err)
	}
}

func TestONNXWorkerTransportFailsClosedOnMalformedTimeoutEOFAndBinding(t *testing.T) {
	for _, mode := range []string{"malformed", "timeout", "eof", "bad-binding"} {
		t.Run(mode, func(t *testing.T) {
			admission := testONNXAdmission(t)
			session := startTransportTestSession(t, admission, mode)
			defer session.close()
			runtimeAdapter, err := newONNXWorkerTransportRuntime(session, admission)
			if err != nil {
				t.Fatal(err)
			}
			req, inputs := transportTestRequest(t, session, admission)
			ctx := context.Background()
			if mode == "timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			batch, err := runtimeAdapter.EmbedTextWithText(ctx, req, inputs)
			if err == nil {
				t.Fatalf("%s worker response was accepted", mode)
			}
			if validateErr := ValidateEmbedTextResult(req, batch); validateErr != nil {
				t.Fatalf("%s fallback batch is invalid: %v", mode, validateErr)
			}
			deadline := time.Now().Add(2 * time.Second)
			for session.alive() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if session.alive() {
				t.Fatalf("%s failure left process session alive", mode)
			}
			select {
			case <-session.done:
			case <-time.After(2 * time.Second):
				t.Fatalf("%s failure did not terminate worker process", mode)
			}
		})
	}
}

func startTransportTestSession(t *testing.T, admission ONNXWorkerAdmission, mode string) *onnxWorkerSession {
	t.Helper()
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	cfg := onnxWorkerProofConfig{
		Command: command, Args: []string{"-test.run=TestONNXWorkerTransportHelper", "-test.v=false"}, WorkingDir: t.TempDir(),
		ProfileDigest: admission.ProfileDigest, WorkerDigest: admission.WorkerDigest,
		GenerationID: "transport-" + mode, FenceToken: "7", SandboxPolicyDigest: testDigest("6"),
		HandshakeTimeout: 3 * time.Second,
	}
	session, err := startONNXWorkerSession(context.Background(), cfg)
	if err != nil {
		t.Fatalf("start %s transport session: %v", mode, err)
	}
	return session
}

func TestQualifiedONNXWorkerRequiresSandboxAttestation(t *testing.T) {
	if _, err := newQualifiedONNXWorkerTransportRuntime(nil, ONNXWorkerAdmission{}); err == nil {
		t.Fatal("nil transport session was accepted as qualified")
	} else if !errors.Is(err, errONNXWorkerTransportUnavailable) {
		t.Fatalf("qualification error = %v, want unavailable", err)
	}
}

func transportTestRequest(t *testing.T, session *onnxWorkerSession, admission ONNXWorkerAdmission) (EmbedTextRequest, []ONNXWorkerTextInput) {
	t.Helper()
	req := testONNXRequest(admission)
	req.Binding.SessionID = session.sessionID
	req.Binding.GenerationID = session.generation
	req.Binding.FenceToken = 7
	req.MaxOutputBytes = 4096
	inputs := make([]ONNXWorkerTextInput, 0, len(req.Segments))
	for i := range req.Segments {
		text := []byte("transport text " + strconv.Itoa(i))
		sum := sha256.Sum256(text)
		req.Segments[i].TextBytes = int64(len(text))
		req.Segments[i].TextDigest = "sha256:" + hex.EncodeToString(sum[:])
		inputs = append(inputs, ONNXWorkerTextInput{SegmentID: req.Segments[i].ID, Text: text})
	}
	if err := ValidateEmbedTextRequest(req); err != nil {
		t.Fatalf("transport request: %v", err)
	}
	return req, inputs
}
