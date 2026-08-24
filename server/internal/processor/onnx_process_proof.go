//go:build unix

package processor

// This file is a private host-owned process-session proof. It intentionally
// stops before the ONNX request transport: a valid session proves only that a
// specifically launched child owns a nonce-bound Unix connection and remains
// alive. It does not manufacture an ONNX READY attestation.

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/processor/sandbox"
	"github.com/ailiheizi/restoreweave/server/internal/search"
)

const (
	onnxWorkerSocketEnv     = "RESTOREWEAVE_ONNX_WORKER_SOCKET"
	onnxWorkerProtocolEnv   = "RESTOREWEAVE_ONNX_WORKER_PROTOCOL"
	onnxWorkerProfileEnv    = "RESTOREWEAVE_ONNX_WORKER_PROFILE"
	onnxWorkerConfigEnv     = "RESTOREWEAVE_ONNX_WORKER_CONFIG"
	onnxWorkerDigestEnv     = "RESTOREWEAVE_ONNX_WORKER_DIGEST"
	onnxWorkerExecutableEnv = "RESTOREWEAVE_ONNX_WORKER_EXECUTABLE"
	onnxWorkerGenerationEnv = "RESTOREWEAVE_ONNX_WORKER_GENERATION"
	onnxWorkerFenceEnv      = "RESTOREWEAVE_ONNX_WORKER_FENCE"
	onnxWorkerSandboxEnv    = "RESTOREWEAVE_ONNX_WORKER_SANDBOX"
	onnxWorkerNoncePathEnv  = "RESTOREWEAVE_ONNX_WORKER_NONCE_PATH"
	onnxWorkerNonceFDE      = "RESTOREWEAVE_ONNX_WORKER_NONCE_FD"
	// Alias retained for package-local launchers that use the clearer suffix.
	onnxWorkerNonceFDEnv     = onnxWorkerNonceFDE
	onnxWorkerHandshakeV1    = "restoreweave.onnx-worker-handshake.v1"
	onnxWorkerHandshakeLimit = 16 << 10

	onnxWorkerNonceBytes   = 32
	onnxWorkerMaxHandshake = 30 * time.Second
)

var (
	errONNXWorkerProofUnavailable = errors.New("ONNX worker process proof is unavailable")
	errONNXWorkerProofInvalid     = errors.New("ONNX worker process proof is invalid")
)

// onnxWorkerProofConfig is private launcher input. The parent never inherits
// its ambient environment, and this proof currently admits no caller-supplied
// environment entries.
type onnxWorkerProofConfig struct {
	Command       string
	Args          []string
	WorkingDir    string
	Environment   []string
	ProfileDigest string
	ConfigDigest  string
	WorkerDigest  string
	// ExecutableDigest pins the staged executable identity. WorkerDigest is
	// reserved for the admitted ONNX binding/profile identity.
	ExecutableDigest    string
	GenerationID        string
	FenceToken          string
	SandboxPolicyDigest string
	PeerUID             int
	PeerGID             int
	HandshakeTimeout    time.Duration
	Sandbox             bool
	BundleRoot          string
	// FenceValidator is host-owned lease authority. A positive fence integer
	// alone is not sufficient to admit a sandbox worker.
	FenceValidator func(context.Context) error
}

// Keep the earlier package-local name available without a second contract.
type onnxWorkerSupervisorConfig = onnxWorkerProofConfig

func (c onnxWorkerProofConfig) validate() error {
	if strings.TrimSpace(c.Command) != c.Command || !filepath.IsAbs(c.Command) {
		return fmt.Errorf("%w: worker command must be an absolute path", errONNXWorkerProofInvalid)
	}
	info, err := os.Lstat(c.Command)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%w: worker command must be a no-follow regular executable", errONNXWorkerProofInvalid)
	}
	if strings.TrimSpace(c.WorkingDir) != c.WorkingDir || !filepath.IsAbs(c.WorkingDir) {
		return fmt.Errorf("%w: worker directory must be an absolute path", errONNXWorkerProofInvalid)
	}
	if dir, err := os.Lstat(c.WorkingDir); err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: worker directory must be a no-follow directory", errONNXWorkerProofInvalid)
	}
	for name, digest := range map[string]string{
		"profile": c.ProfileDigest, "worker": c.WorkerDigest, "sandbox": c.SandboxPolicyDigest,
	} {
		if err := ValidateEmbedTextDigest(digest); err != nil {
			return fmt.Errorf("%w: %s digest is invalid", errONNXWorkerProofInvalid, name)
		}
	}
	if !validateEmbedTextToken(c.GenerationID) || strings.TrimSpace(c.FenceToken) == "" {
		return fmt.Errorf("%w: generation and fence bindings are required", errONNXWorkerProofInvalid)
	}
	fence, err := strconv.ParseInt(c.FenceToken, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: fence token is invalid", errONNXWorkerProofInvalid)
	}
	if fence <= 0 {
		return fmt.Errorf("%w: fence token must be positive", errONNXWorkerProofInvalid)
	}
	if len(c.Environment) != 0 {
		return fmt.Errorf("%w: worker environment must be empty", errONNXWorkerProofInvalid)
	}
	if c.HandshakeTimeout <= 0 || c.HandshakeTimeout > onnxWorkerMaxHandshake {
		return fmt.Errorf("%w: handshake timeout is outside bounds", errONNXWorkerProofInvalid)
	}
	if c.Sandbox {
		if !sandbox.Supported() {
			return fmt.Errorf("%w: sandbox is unavailable on this platform", errONNXWorkerProofUnavailable)
		}
	}
	if c.BundleRoot != "" {
		if err := ValidateEmbedTextDigest(c.ExecutableDigest); err != nil {
			return fmt.Errorf("%w: executable digest is invalid", errONNXWorkerProofInvalid)
		}
		if strings.TrimSpace(c.BundleRoot) != c.BundleRoot || !filepath.IsAbs(c.BundleRoot) {
			return fmt.Errorf("%w: bundle root must be absolute", errONNXWorkerProofInvalid)
		}
		if c.FenceValidator == nil {
			return fmt.Errorf("%w: real worker requires a host-owned fence validator", errONNXWorkerProofInvalid)
		}
	}
	return nil
}

// The challenge contains no secret. The nonce itself travels only over fd 3.
type onnxWorkerChallenge struct {
	Schema           string `json:"schema"`
	SessionID        string `json:"session_id"`
	Protocol         string `json:"protocol"`
	ProfileDigest    string `json:"profile_digest"`
	WorkerDigest     string `json:"worker_digest"`
	ExecutableDigest string `json:"executable_digest,omitempty"`
	ConfigDigest     string `json:"config_digest"`
	GenerationID     string `json:"generation_id"`
	FenceToken       string `json:"fence_token"`
	PeerPID          int    `json:"peer_pid"`
	PeerUID          int    `json:"peer_uid"`
	PeerGID          int    `json:"peer_gid"`
	NonceDigest      string `json:"nonce_digest"`
}

type onnxWorkerReady struct {
	Schema              string                `json:"schema"`
	SessionID           string                `json:"session_id"`
	Protocol            string                `json:"protocol"`
	ProfileDigest       string                `json:"profile_digest"`
	WorkerDigest        string                `json:"worker_digest"`
	ExecutableDigest    string                `json:"executable_digest,omitempty"`
	ConfigDigest        string                `json:"config_digest"`
	GenerationID        string                `json:"generation_id"`
	FenceToken          string                `json:"fence_token"`
	PeerPID             int                   `json:"peer_pid"`
	SandboxPolicyDigest string                `json:"sandbox_policy_digest"`
	PID                 int                   `json:"pid"`
	MAC                 string                `json:"mac"`
	Probe               ONNXWorkerProbeResult `json:"probe,omitempty"`
	ProbeDigest         string                `json:"probe_digest,omitempty"`
}

type onnxWorkerHandshakeBinding struct {
	Schema              string
	SessionID           string
	Protocol            string
	ProfileDigest       string
	WorkerDigest        string
	ExecutableDigest    string
	ConfigDigest        string
	GenerationID        string
	FenceToken          string
	SandboxPolicyDigest string
	PeerPID             int
	PeerUID             int
	PeerGID             int
	ProbeDigest         string
}

func (b onnxWorkerHandshakeBinding) canonical() []byte {
	var out []byte
	appendString := func(value string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(value)))
		out = append(out, n[:]...)
		out = append(out, value...)
	}
	appendInt := func(value int) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(value))
		out = append(out, n[:]...)
	}
	appendString(onnxWorkerHandshakeV1)
	appendString(b.Schema)
	appendString(b.SessionID)
	appendString(b.Protocol)
	appendString(b.ProfileDigest)
	appendString(b.WorkerDigest)
	appendString(b.ExecutableDigest)
	appendString(b.ConfigDigest)
	appendString(b.GenerationID)
	appendString(b.FenceToken)
	appendString(b.SandboxPolicyDigest)
	appendInt(b.PeerPID)
	appendInt(b.PeerUID)
	appendInt(b.PeerGID)
	appendString(b.ProbeDigest)
	return out
}

func onnxWorkerHandshakeMAC(secret []byte, binding onnxWorkerHandshakeBinding) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(binding.canonical())
	return hex.EncodeToString(mac.Sum(nil))
}

func onnxWorkerProbeDigest(probe ONNXWorkerProbeResult) (string, error) {
	payload, err := json.Marshal(probe)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("restoreweave.onnx-worker-probe.v1\n"), payload...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validONNXWorkerMAC(secret []byte, binding onnxWorkerHandshakeBinding, encoded string) bool {
	got, err := hex.DecodeString(encoded)
	if err != nil || len(got) != sha256.Size {
		return false
	}
	want, err := hex.DecodeString(onnxWorkerHandshakeMAC(secret, binding))
	return err == nil && hmac.Equal(got, want)
}

func newONNXWorkerSessionID() (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

type onnxWorkerSession struct {
	mu              sync.RWMutex
	cmd             *exec.Cmd
	conn            net.Conn
	transport       *onnxWorkerTransport
	peer            onnxWorkerPeer
	sessionID       string
	profile         string
	worker          string
	generation      string
	fence           string
	sandbox         string
	process         *onnxWorkerProcessLiveness
	done            chan struct{}
	connDone        chan struct{}
	connDoneOnce    sync.Once
	root            string
	probe           ONNXWorkerProbeResult
	identity        *onnxWorkerProcessIdentity
	processAttested bool
	leaseDone       chan struct{}
	leaseDoneOnce   sync.Once
	closed          bool
}

func (s *onnxWorkerSession) alive() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	closed, process, connDone, done := s.closed, s.process, s.connDone, s.done
	s.mu.RUnlock()
	if closed || process == nil || !process.alive() || connDone == nil || done == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
	}
	select {
	case <-connDone:
		return false
	default:
		return true
	}
}

func (s *onnxWorkerSession) markConnectionDone() {
	if s == nil || s.connDone == nil {
		return
	}
	s.connDoneOnce.Do(func() { close(s.connDone) })
}

func (s *onnxWorkerSession) close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	conn, transport, cmd, process, done, root, leaseDone := s.conn, s.transport, s.cmd, s.process, s.done, s.root, s.leaseDone
	s.mu.Unlock()
	if transport != nil {
		_ = transport.close()
	}
	if leaseDone != nil {
		s.leaseDoneOnce.Do(func() { close(leaseDone) })
	}
	if conn != nil {
		_ = conn.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-done
		}
	}
	if process != nil {
		_ = process.closeHandle()
	}
	return os.RemoveAll(root)
}

func openONNXExecutableNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Base(path)), nil
}

func digestONNXExecutable(path string) (string, error) {
	file, err := openONNXExecutableNoFollow(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("worker executable is not a regular executable")
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func stageONNXExecutable(root, source, expected string) (string, error) {
	file, err := openONNXExecutableNoFollow(source)
	if err != nil {
		return "", fmt.Errorf("open executable: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("worker executable is not a regular executable")
	}
	target := filepath.Join(root, "worker")
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o500)
	if err != nil {
		return "", fmt.Errorf("stage executable: %w", err)
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), file)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(target)
		if copyErr != nil {
			return "", fmt.Errorf("stage executable bytes: %w", copyErr)
		}
		if syncErr != nil {
			return "", fmt.Errorf("stage executable sync: %w", syncErr)
		}
		return "", fmt.Errorf("stage executable close: %w", closeErr)
	}
	got := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if got != expected {
		_ = os.Remove(target)
		return "", fmt.Errorf("worker executable digest %s does not match expected %s", got, expected)
	}
	return target, nil
}

func stageONNXBundle(ctx context.Context, root, source, expectedProfile string) error {
	bundle, err := search.LoadSemanticBundle(source)
	if err != nil {
		return err
	}
	if err := bundle.VerifyPinnedProfile(expectedProfile); err != nil {
		return err
	}
	bundleRoot := filepath.Join(root, "bundle")
	if err := os.Mkdir(bundleRoot, 0o500); err != nil {
		return fmt.Errorf("create staged bundle: %w", err)
	}
	descriptor, err := json.Marshal(bundle.Descriptor)
	if err != nil {
		return fmt.Errorf("encode staged bundle descriptor: %w", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, search.SemanticBundleManifestName), descriptor, 0o400); err != nil {
		return fmt.Errorf("write staged bundle descriptor: %w", err)
	}
	assets := []struct {
		name string
		path string
	}{
		{"runtime", bundle.Descriptor.Runtime.Path}, {"onnx_binding", bundle.Descriptor.ONNXBinding.Path},
		{"onnx_c_api", bundle.Descriptor.ONNXCAPI.Path}, {"model", bundle.Descriptor.Model.Path},
		{"tokenizer", bundle.Descriptor.Tokenizer.Path}, {"profile", bundle.Descriptor.Profile.Path},
		{"zvec", bundle.Descriptor.Zvec.Path}, {"zvec_go", bundle.Descriptor.ZvecGo.Path},
		{"license", bundle.Descriptor.License.Path}, {"notice", bundle.Descriptor.Notice.Path},
		{"sbom", bundle.Descriptor.SBOM.Path},
	}
	for _, asset := range assets {
		payload, err := search.ReadSemanticBundleAsset(ctx, source, bundle, asset.name)
		if err != nil {
			return fmt.Errorf("stage bundle asset %s: %w", asset.name, err)
		}
		destination := filepath.Join(bundleRoot, filepath.FromSlash(asset.path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o500); err != nil {
			return fmt.Errorf("stage bundle asset directory %s: %w", asset.name, err)
		}
		file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
		if err != nil {
			return fmt.Errorf("stage bundle asset %s: %w", asset.name, err)
		}
		_, writeErr := file.Write(payload)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(destination)
			if writeErr != nil {
				return fmt.Errorf("stage bundle asset %s: %w", asset.name, writeErr)
			}
			return fmt.Errorf("stage bundle asset %s: %w", asset.name, closeErr)
		}
	}
	return nil
}

func startONNXWorkerSession(ctx context.Context, cfg onnxWorkerProofConfig) (*onnxWorkerSession, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", errONNXWorkerProofUnavailable, err)
	}
	if cfg.Sandbox || cfg.BundleRoot != "" {
		if err := cfg.FenceValidator(ctx); err != nil {
			return nil, fmt.Errorf("%w: fence lease is not valid: %v", errONNXWorkerProofUnavailable, err)
		}
	}
	nonce := make([]byte, onnxWorkerNonceBytes)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: generate nonce: %v", errONNXWorkerProofUnavailable, err)
	}
	sessionID, err := newONNXWorkerSessionID()
	if err != nil {
		clear(nonce)
		return nil, fmt.Errorf("%w: generate session ID: %v", errONNXWorkerProofUnavailable, err)
	}
	root, err := os.MkdirTemp("", "restoreweave-onnx-worker-")
	if err != nil {
		clear(nonce)
		return nil, fmt.Errorf("%w: create private directory: %v", errONNXWorkerProofUnavailable, err)
	}
	fail := func(cause error, conn net.Conn, cmd *exec.Cmd, process *onnxWorkerProcessLiveness, done chan struct{}) error {
		if conn != nil {
			_ = conn.Close()
		}
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		if done != nil {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		} else if cmd != nil && cmd.Process != nil {
			_ = cmd.Wait()
		}
		if process != nil {
			_ = process.closeHandle()
		}
		clear(nonce)
		_ = os.RemoveAll(root)
		return cause
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fail(fmt.Errorf("%w: protect private directory: %v", errONNXWorkerProofUnavailable, err), nil, nil, nil, nil)
	}
	socketPath := filepath.Join(root, "control.sock")
	lis, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, fail(fmt.Errorf("%w: listen: %v", errONNXWorkerProofUnavailable, err), nil, nil, nil, nil)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = lis.Close()
		return nil, fail(fmt.Errorf("%w: protect socket: %v", errONNXWorkerProofUnavailable, err), nil, nil, nil, nil)
	}
	if err := verifyONNXWorkerProofPermissions(root, socketPath); err != nil {
		_ = lis.Close()
		return nil, fail(err, nil, nil, nil, nil)
	}
	workerCommand := cfg.Command
	workerArgs := append([]string(nil), cfg.Args...)
	workerWorkingDir := cfg.WorkingDir
	workerSocketPath := socketPath
	workerEnv := append([]string(nil), cfg.Environment...)
	if cfg.Sandbox {
		admission, admissionErr := LoadONNXWorkerAdmission(cfg.BundleRoot, cfg.ProfileDigest, cfg.ConfigDigest)
		if admissionErr != nil {
			_ = lis.Close()
			return nil, fail(fmt.Errorf("%w: admit worker bundle: %v", errONNXWorkerProofUnavailable, admissionErr), nil, nil, nil, nil)
		}
		if admission.WorkerDigest != cfg.WorkerDigest {
			_ = lis.Close()
			return nil, fail(fmt.Errorf("%w: worker binding digest does not match bundle", errONNXWorkerProofInvalid), nil, nil, nil, nil)
		}
		if _, err := stageONNXExecutable(root, cfg.Command, cfg.ExecutableDigest); err != nil {
			_ = lis.Close()
			return nil, fail(fmt.Errorf("%w: %v", errONNXWorkerProofUnavailable, err), nil, nil, nil, nil)
		}
		if err := stageONNXBundle(ctx, root, cfg.BundleRoot, cfg.ProfileDigest); err != nil {
			_ = lis.Close()
			return nil, fail(fmt.Errorf("%w: %v", errONNXWorkerProofUnavailable, err), nil, nil, nil, nil)
		}
		workerCommand = filepath.Join(root, "worker")
		workerArgs = append(workerArgs, "/stage/bundle")
		workerWorkingDir = root
		workerSocketPath = "/stage/control.sock"
	}
	nonceRead, nonceWrite, err := os.Pipe()
	if err != nil {
		_ = lis.Close()
		return nil, fail(fmt.Errorf("%w: create nonce pipe: %v", errONNXWorkerProofUnavailable, err), nil, nil, nil, nil)
	}
	envPairs := []string{
		onnxWorkerSocketEnv + "=" + workerSocketPath,
		onnxWorkerProtocolEnv + "=" + ONNXWorkerProtocol,
		onnxWorkerProfileEnv + "=" + cfg.ProfileDigest,
		onnxWorkerConfigEnv + "=" + cfg.ConfigDigest,
		onnxWorkerDigestEnv + "=" + cfg.WorkerDigest,
		onnxWorkerExecutableEnv + "=" + cfg.ExecutableDigest,
		onnxWorkerGenerationEnv + "=" + cfg.GenerationID,
		onnxWorkerFenceEnv + "=" + cfg.FenceToken,
		onnxWorkerSandboxEnv + "=" + cfg.SandboxPolicyDigest,
		onnxWorkerNonceFDE + "=3",
	}
	if cfg.Sandbox {
		workerEnv = nil
		env := make(map[string]string, len(envPairs)+1)
		env["PATH"] = "/usr/bin:/bin"
		for _, pair := range envPairs {
			key, value, ok := strings.Cut(pair, "=")
			if ok {
				env[key] = value
			}
		}
		noncePath := ""
		preserveFDs := []int{3}
		if !sandbox.PreserveFDSupported() {
			noncePath = "/restoreweave-worker-nonce"
			preserveFDs = nil
		}
		if noncePath != "" {
			env[onnxWorkerNoncePathEnv] = noncePath
		}
		argv, buildErr := sandbox.BuildArgv(sandbox.Spec{
			Binary: workerCommand, Args: workerArgs, StagingDir: root,
			Env: env, ReadOnlyStaging: true, PreserveFDs: preserveFDs, NonceFilePath: noncePath,
		})
		if buildErr != nil {
			_ = nonceRead.Close()
			_ = nonceWrite.Close()
			_ = lis.Close()
			return nil, fail(fmt.Errorf("%w: build sandbox: %v", errONNXWorkerProofUnavailable, buildErr), nil, nil, nil, nil)
		}
		bwrap, lookErr := sandbox.BubblewrapPath()
		if lookErr != nil {
			_ = nonceRead.Close()
			_ = nonceWrite.Close()
			_ = lis.Close()
			return nil, fail(fmt.Errorf("%w: locate bubblewrap: %v", errONNXWorkerProofUnavailable, lookErr), nil, nil, nil, nil)
		}
		workerCommand, workerArgs = bwrap, argv
		workerWorkingDir = root
		workerEnv = []string{}
	} else {
		workerEnv = append(workerEnv, envPairs...)
		if cfg.BundleRoot != "" {
			workerArgs = append(workerArgs, cfg.BundleRoot)
		}
	}
	cmd := exec.CommandContext(ctx, workerCommand, workerArgs...)
	cmd.Dir = workerWorkingDir
	cmd.Env = workerEnv
	cmd.ExtraFiles = []*os.File{nonceRead}
	if err := cmd.Start(); err != nil {
		_ = nonceRead.Close()
		_ = nonceWrite.Close()
		_ = lis.Close()
		return nil, fail(fmt.Errorf("%w: start child: %v", errONNXWorkerProofUnavailable, err), nil, nil, nil, nil)
	}
	_ = nonceRead.Close()
	process, err := newONNXWorkerProcessLiveness(cmd)
	if err != nil {
		_ = nonceWrite.Close()
		_ = lis.Close()
		return nil, fail(fmt.Errorf("%w: process liveness: %v", errONNXWorkerProofUnavailable, err), nil, cmd, nil, nil)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		process.markExited()
		close(done)
	}()
	if _, err := nonceWrite.Write(nonce); err != nil {
		_ = nonceWrite.Close()
		_ = lis.Close()
		return nil, fail(fmt.Errorf("%w: transfer nonce: %v", errONNXWorkerProofUnavailable, err), nil, cmd, process, done)
	}
	_ = nonceWrite.Close()
	deadline := time.Now().Add(cfg.HandshakeTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	if err := lis.SetDeadline(deadline); err != nil {
		_ = lis.Close()
		return nil, fail(fmt.Errorf("%w: listener deadline: %v", errONNXWorkerProofUnavailable, err), nil, cmd, process, done)
	}
	conn, err := lis.Accept()
	_ = lis.Close()
	if err != nil {
		return nil, fail(fmt.Errorf("%w: accept child: %v", errONNXWorkerProofUnavailable, err), nil, cmd, process, done)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fail(fmt.Errorf("%w: connection deadline: %v", errONNXWorkerProofUnavailable, err), conn, cmd, process, done)
	}
	peer, err := onnxWorkerPeerIdentityOf(conn)
	if err != nil {
		return nil, fail(fmt.Errorf("%w: peer credentials: %v", errONNXWorkerProofUnavailable, err), conn, cmd, process, done)
	}
	expectedUID, expectedGID := cfg.PeerUID, cfg.PeerGID
	if expectedUID == 0 && os.Getuid() != 0 {
		expectedUID = os.Getuid()
	}
	if expectedGID == 0 && os.Getgid() != 0 {
		expectedGID = os.Getgid()
	}
	if (!cfg.Sandbox && peer.PID != cmd.Process.Pid) || peer.UID != expectedUID || peer.GID != expectedGID {
		return nil, fail(fmt.Errorf("%w: peer identity does not match child", errONNXWorkerProofInvalid), conn, cmd, process, done)
	}
	nonceSum := sha256.Sum256(nonce)
	challenge := onnxWorkerChallenge{
		Schema: onnxWorkerHandshakeV1, SessionID: sessionID, Protocol: ONNXWorkerProtocol,
		ProfileDigest: cfg.ProfileDigest, WorkerDigest: cfg.WorkerDigest, GenerationID: cfg.GenerationID,
		FenceToken: cfg.FenceToken, PeerPID: peer.PID, PeerUID: peer.UID, PeerGID: peer.GID,
		NonceDigest: "sha256:" + hex.EncodeToString(nonceSum[:]),
	}
	challenge.ExecutableDigest = cfg.ExecutableDigest
	challenge.ConfigDigest = cfg.ConfigDigest
	if err := writeONNXWorkerHandshake(conn, challenge); err != nil {
		return nil, fail(fmt.Errorf("%w: send challenge: %v", errONNXWorkerProofUnavailable, err), conn, cmd, process, done)
	}
	var ready onnxWorkerReady
	if err := readONNXWorkerHandshake(conn, &ready); err != nil {
		return nil, fail(fmt.Errorf("%w: read readiness: %v", errONNXWorkerProofUnavailable, err), conn, cmd, process, done)
	}
	binding := onnxWorkerHandshakeBinding{
		Schema: challenge.Schema, SessionID: challenge.SessionID, Protocol: challenge.Protocol,
		ProfileDigest: challenge.ProfileDigest, WorkerDigest: challenge.WorkerDigest,
		GenerationID: challenge.GenerationID, FenceToken: challenge.FenceToken,
		SandboxPolicyDigest: cfg.SandboxPolicyDigest, PeerPID: peer.PID, PeerUID: peer.UID, PeerGID: peer.GID,
		ProbeDigest: ready.ProbeDigest,
	}
	binding.ExecutableDigest = cfg.ExecutableDigest
	binding.ConfigDigest = cfg.ConfigDigest
	if ready.ProbeDigest != "" {
		probeDigest, digestErr := onnxWorkerProbeDigest(ready.Probe)
		if digestErr != nil || probeDigest != ready.ProbeDigest {
			return nil, fail(fmt.Errorf("%w: runtime probe digest mismatch", errONNXWorkerProofInvalid), conn, cmd, process, done)
		}
	}
	if ready.Schema != challenge.Schema || ready.SessionID != challenge.SessionID || ready.Protocol != challenge.Protocol ||
		ready.ProfileDigest != challenge.ProfileDigest || ready.WorkerDigest != challenge.WorkerDigest ||
		ready.GenerationID != challenge.GenerationID || ready.FenceToken != challenge.FenceToken || ready.ExecutableDigest != challenge.ExecutableDigest || ready.ConfigDigest != challenge.ConfigDigest ||
		ready.PeerPID != peer.PID || ready.SandboxPolicyDigest != cfg.SandboxPolicyDigest ||
		!validONNXWorkerMAC(nonce, binding, ready.MAC) {
		return nil, fail(fmt.Errorf("%w: readiness binding mismatch", errONNXWorkerProofInvalid), conn, cmd, process, done)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, fail(fmt.Errorf("%w: clear handshake deadline: %v", errONNXWorkerProofUnavailable, err), conn, cmd, process, done)
	}
	session := &onnxWorkerSession{
		cmd: cmd, conn: conn, peer: peer, sessionID: sessionID,
		profile: cfg.ProfileDigest, worker: cfg.WorkerDigest, generation: cfg.GenerationID, fence: cfg.FenceToken,
		sandbox: cfg.SandboxPolicyDigest, process: process, done: done, connDone: make(chan struct{}), root: root,
		probe: ready.Probe, identity: &onnxWorkerProcessIdentity{}, processAttested: true,
	}
	if cfg.Sandbox || cfg.BundleRoot != "" {
		session.leaseDone = make(chan struct{})
		go monitorONNXWorkerLease(ctx, session, cfg.FenceValidator)
	}
	clear(nonce)
	session.transport = newONNXWorkerTransport(conn, session)
	go session.transport.run()
	return session, nil
}

func monitorONNXWorkerLease(parent context.Context, session *onnxWorkerSession, validate func(context.Context) error) {
	if session == nil || validate == nil {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-session.leaseDone:
			return
		case <-parent.Done():
			_ = session.close()
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := validate(checkCtx)
			cancel()
			if err != nil {
				_ = session.close()
				return
			}
		}
	}
}

func verifyONNXWorkerProofPermissions(root, socket string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 || rootInfo.Mode()&os.ModeSymlink != 0 || !onnxWorkerOwnedByCurrentUser(rootInfo) {
		return fmt.Errorf("%w: worker directory permissions are not 0700", errONNXWorkerProofInvalid)
	}
	socketInfo, err := os.Lstat(socket)
	if err != nil || socketInfo.Mode()&os.ModeSocket == 0 || socketInfo.Mode().Perm() != 0o600 || socketInfo.Mode()&os.ModeSymlink != 0 || !onnxWorkerOwnedByCurrentUser(socketInfo) {
		return fmt.Errorf("%w: worker socket permissions are not 0600", errONNXWorkerProofInvalid)
	}
	return nil
}

func onnxWorkerOwnedByCurrentUser(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return uint64(stat.Uid) == uint64(os.Getuid()) && uint64(stat.Gid) == uint64(os.Getgid())
}

func monitorONNXWorkerConnection(conn net.Conn, session *onnxWorkerSession) {
	defer session.markConnectionDone()
	_, _ = io.Copy(io.Discard, conn)
}

func writeONNXWorkerHandshake(conn net.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > onnxWorkerHandshakeLimit {
		return errONNXWorkerProofInvalid
	}
	for len(data) > 0 {
		n, err := conn.Write(data)
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

func readONNXWorkerHandshake(conn net.Conn, value any) error {
	reader := bufio.NewReader(io.LimitReader(conn, onnxWorkerHandshakeLimit))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return err
	}
	if len(line) > onnxWorkerHandshakeLimit {
		return errONNXWorkerProofInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(line)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errONNXWorkerProofInvalid
	}
	return nil
}
