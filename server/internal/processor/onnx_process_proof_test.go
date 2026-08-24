//go:build unix

package processor

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestONNXWorkerProofHelper(t *testing.T) {
	if os.Getenv(onnxWorkerSocketEnv) == "" {
		return
	}
	nonceFD, err := strconv.Atoi(os.Getenv(onnxWorkerNonceFDE))
	if err != nil || nonceFD < 3 {
		os.Exit(20)
	}
	nonceFile := os.NewFile(uintptr(nonceFD), "onnx-worker-nonce")
	if nonceFile == nil {
		os.Exit(20)
	}
	nonce := make([]byte, 32)
	if _, err := io.ReadFull(nonceFile, nonce); err != nil {
		os.Exit(20)
	}
	_ = nonceFile.Close()

	conn, err := net.Dial("unix", os.Getenv(onnxWorkerSocketEnv))
	if err != nil {
		os.Exit(21)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		os.Exit(22)
	}
	var challenge onnxWorkerChallenge
	if err := json.Unmarshal(line, &challenge); err != nil {
		os.Exit(23)
	}
	nonceSum := sha256.Sum256(nonce)
	if challenge.NonceDigest != "sha256:"+hex.EncodeToString(nonceSum[:]) {
		os.Exit(24)
	}
	mode := strings.TrimPrefix(challenge.GenerationID, "test-generation-")
	if mode == "stall" {
		select {}
	}
	sandboxDigest := os.Getenv(onnxWorkerSandboxEnv)
	if sandboxDigest == "" {
		os.Exit(24)
	}
	ready := onnxWorkerReady{
		Schema: challenge.Schema, SessionID: challenge.SessionID, Protocol: challenge.Protocol,
		ProfileDigest: challenge.ProfileDigest, WorkerDigest: challenge.WorkerDigest,
		GenerationID: challenge.GenerationID, FenceToken: challenge.FenceToken,
		PeerPID: challenge.PeerPID, PID: os.Getpid(),
		SandboxPolicyDigest: sandboxDigest,
	}
	if mode == "bad-pid" {
		ready.PeerPID++
	}
	if mode == "bad-profile" {
		ready.ProfileDigest = "sha256:" + strings.Repeat("d", 64)
	}
	if mode == "bad-fence" {
		ready.FenceToken = "8"
	}
	binding := onnxWorkerHandshakeBinding{
		Schema: ready.Schema, SessionID: ready.SessionID, Protocol: ready.Protocol,
		ProfileDigest: ready.ProfileDigest, WorkerDigest: ready.WorkerDigest,
		GenerationID: ready.GenerationID, FenceToken: ready.FenceToken,
		SandboxPolicyDigest: ready.SandboxPolicyDigest,
		PeerPID:             ready.PeerPID, PeerUID: challenge.PeerUID, PeerGID: challenge.PeerGID,
	}
	ready.MAC = onnxWorkerHandshakeMAC(nonce, binding)
	if mode == "bad-nonce" {
		wrongNonce := append([]byte(nil), nonce...)
		wrongNonce[0] ^= 0xff
		ready.MAC = onnxWorkerHandshakeMAC(wrongNonce, binding)
	}
	if mode == "bad-mac" {
		ready.MAC = strings.Repeat("0", 64)
	}
	data, _ := json.Marshal(ready)
	data = append(data, '\n')
	_, _ = conn.Write(data)
	if mode == "disconnect" {
		return
	}
	if mode == "exit-hold-connection" {
		unixConn, ok := conn.(*net.UnixConn)
		if !ok {
			os.Exit(25)
		}
		connectionFile, err := unixConn.File()
		if err != nil {
			os.Exit(25)
		}
		holder := exec.Command(os.Args[0], "-test.run=TestONNXWorkerProofConnectionHolder", "-test.v=false")
		holder.Env = []string{"RESTOREWEAVE_ONNX_HOLD_FD=3"}
		holder.ExtraFiles = []*os.File{connectionFile}
		if err := holder.Start(); err != nil {
			_ = connectionFile.Close()
			os.Exit(25)
		}
		_ = connectionFile.Close()
		return
	}
	select {}
}

func TestONNXWorkerProofConnectionHolder(t *testing.T) {
	if os.Getenv("RESTOREWEAVE_ONNX_HOLD_FD") != "3" {
		return
	}
	connectionFile := os.NewFile(3, "onnx-worker-held-connection")
	if connectionFile == nil {
		os.Exit(30)
	}
	defer connectionFile.Close()
	// Keep the inherited descriptor open long enough for the host to observe
	// process exit independently of transport EOF, including under -race.
	time.Sleep(5 * time.Second)
}

func proofTestConfig(t *testing.T, mode string) onnxWorkerProofConfig {
	t.Helper()
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return onnxWorkerProofConfig{
		Command:             command,
		Args:                []string{"-test.run=TestONNXWorkerProofHelper", "-test.v=false"},
		WorkingDir:          t.TempDir(),
		ProfileDigest:       "sha256:" + strings.Repeat("a", 64),
		WorkerDigest:        "sha256:" + strings.Repeat("b", 64),
		GenerationID:        "test-generation-" + mode,
		FenceToken:          "7",
		SandboxPolicyDigest: "sha256:" + strings.Repeat("c", 64),
		HandshakeTimeout:    3 * time.Second,
	}
}

func TestONNXWorkerSupervisorBindsPeerNonceAndLiveness(t *testing.T) {
	cfg := proofTestConfig(t, "")
	session, err := startONNXWorkerSession(context.Background(), cfg)
	if err != nil {
		t.Fatalf("start proof session: %v", err)
	}
	defer session.close()
	if !session.alive() {
		t.Fatal("newly attested worker session is not alive")
	}
	if session.peer.PID != session.cmd.Process.Pid || session.peer.UID != os.Getuid() {
		t.Fatalf("peer identity = %+v process=%d uid=%d", session.peer, session.cmd.Process.Pid, os.Getuid())
	}
	if err := session.close(); err != nil {
		t.Fatalf("close session: %v", err)
	}
	if session.alive() {
		t.Fatal("closed worker session remained alive")
	}
}

func TestONNXWorkerSupervisorInvalidatesDisconnectedSession(t *testing.T) {
	session, err := startONNXWorkerSession(context.Background(), proofTestConfig(t, "disconnect"))
	if err != nil {
		t.Fatalf("start disconnecting proof session: %v", err)
	}
	defer session.close()
	deadline := time.Now().Add(3 * time.Second)
	for session.alive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.alive() {
		t.Fatal("disconnected worker session remained alive")
	}
}

func TestONNXWorkerSupervisorInvalidatesExitedProcessWithOpenConnection(t *testing.T) {
	session, err := startONNXWorkerSession(context.Background(), proofTestConfig(t, "exit-hold-connection"))
	if err != nil {
		t.Fatalf("start exiting proof session: %v", err)
	}
	defer session.close()
	select {
	case <-session.done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker process did not exit")
	}
	select {
	case <-session.connDone:
		t.Fatal("inherited connection closed before process liveness was checked")
	default:
	}
	if session.alive() {
		t.Fatal("exited worker session remained alive while its connection was inherited")
	}
}

func TestONNXWorkerSupervisorRejectsTamperedReadinessBindings(t *testing.T) {
	for _, mode := range []string{"bad-nonce", "bad-mac", "bad-profile", "bad-fence", "bad-pid"} {
		t.Run(mode, func(t *testing.T) {
			cfg := proofTestConfig(t, mode)
			if _, err := startONNXWorkerSession(context.Background(), cfg); err == nil {
				t.Fatal("malformed worker readiness was accepted")
			} else if !errors.Is(err, errONNXWorkerProofInvalid) {
				t.Fatalf("malformed readiness error = %v, want invalid proof", err)
			}
		})
	}
}

func TestONNXWorkerSupervisorBoundsStalledHandshake(t *testing.T) {
	cfg := proofTestConfig(t, "stall")
	cfg.HandshakeTimeout = 100 * time.Millisecond
	started := time.Now()
	if _, err := startONNXWorkerSession(context.Background(), cfg); err == nil {
		t.Fatal("stalled worker handshake succeeded")
	} else if !errors.Is(err, errONNXWorkerProofUnavailable) {
		t.Fatalf("stalled handshake error = %v, want unavailable proof", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("stalled handshake returned after %v, want bounded failure", elapsed)
	}
}

func TestONNXWorkerExecutableDigestAndNoFollowStaging(t *testing.T) {
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	digest, err := digestONNXExecutable(command)
	if err != nil {
		t.Fatalf("digest executable: %v", err)
	}
	root := t.TempDir()
	staged, err := stageONNXExecutable(root, command, digest)
	if err != nil {
		t.Fatalf("stage executable: %v", err)
	}
	got, err := digestONNXExecutable(staged)
	if err != nil || got != digest {
		t.Fatalf("staged digest = %q, err=%v, want %q", got, err, digest)
	}
	if _, err := stageONNXExecutable(t.TempDir(), command, "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("digest mismatch was accepted")
	}
	link := filepath.Join(t.TempDir(), "worker-link")
	if err := os.Symlink(command, link); err != nil {
		t.Fatal(err)
	}
	if _, err := stageONNXExecutable(t.TempDir(), link, digest); err == nil {
		t.Fatal("executable symlink was followed")
	}
}
