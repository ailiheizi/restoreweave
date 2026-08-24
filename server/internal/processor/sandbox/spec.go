// Package sandbox is the host-owned Processor isolation policy.
// It plans a bubblewrap command line with no network and no ambient source
// tree. Large bytes stay in host-owned staging. This is not a public ABI,
// not an out-of-process RUN_STAGE implementation, and not a selected worker
// runtime.
//
// Execution is Linux-only because bubblewrap needs Linux namespaces,
// seccomp, and cgroup v2. Darwin is Unix, but those are not POSIX
// interfaces; sandbox-exec is a different ABI and does not qualify this
// profile. Argv planning is tested on every OS.
package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

var (
	ErrUnsupportedPlatform = errors.New("processor sandbox execution needs Linux bubblewrap")
	ErrInvalidSpec         = errors.New("sandbox spec cannot be confirmed")
	ErrNetworkRequested    = errors.New("network is refused")
	ErrExtraBinds          = errors.New("extra binds are refused")
	ErrBubblewrapMissing   = errors.New("host-owned bwrap executable is unavailable")
)

const (
	workerPath   = "/worker"
	stagePath    = "/stage"
	hostname     = "restoreweave-processor"
	noncePath    = "/restoreweave-worker-nonce"
	policyIDFD   = "restoreweave.processor.sandbox.v11:ro-bin:ro-stage:proc:ro-sys:tmpfs-tmp:unshare-net-pid-ipc-uts:preserve-fd-3"
	policyIDFile = "restoreweave.processor.sandbox.v11:ro-bin:ro-stage:proc:ro-sys:tmpfs-tmp:unshare-net-pid-ipc-uts:file-from-fd-3"
)

// PolicyDigest binds the exact argv policy to a worker handshake. It is a
// description of enforced arguments, not an attestation by itself.
func PolicyDigest() string {
	policyID := policyIDFile
	if PreserveFDSupported() {
		policyID = policyIDFD
	}
	sum := sha256.Sum256([]byte(policyID))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Spec is the host-owned isolation request. Callers cannot attach the source
// tree or enable networking.
type Spec struct {
	Binary     string
	Args       []string
	StagingDir string
	Env        map[string]string
	// ReadOnlyStaging prevents the worker from modifying supervisor state.
	ReadOnlyStaging bool
	// PreserveFDs is intentionally narrow: the ONNX supervisor uses fd 3 for
	// the nonce handoff and does not admit arbitrary descriptor inheritance.
	PreserveFDs []int
	// NonceFilePath requests bwrap's --file fd-3 fallback on versions without
	// --preserve-fd. It is restricted to the fixed sandbox path.
	NonceFilePath string
	Network       bool
	ExtraBinds    []Bind
}

// Bind would attach another host path. The first sandbox profile rejects every
// extra bind so the source tree cannot leak in.
type Bind struct {
	Host  string
	Dest  string
	Write bool
}

var allowedEnv = map[string]struct{}{
	"PATH":                                {},
	"LANG":                                {},
	"LC_ALL":                              {},
	"TZ":                                  {},
	"HOME":                                {},
	"RESTOREWEAVE_ONNX_WORKER_SOCKET":     {},
	"RESTOREWEAVE_ONNX_WORKER_PROTOCOL":   {},
	"RESTOREWEAVE_ONNX_WORKER_PROFILE":    {},
	"RESTOREWEAVE_ONNX_WORKER_CONFIG":     {},
	"RESTOREWEAVE_ONNX_WORKER_DIGEST":     {},
	"RESTOREWEAVE_ONNX_WORKER_EXECUTABLE": {},
	"RESTOREWEAVE_ONNX_WORKER_GENERATION": {},
	"RESTOREWEAVE_ONNX_WORKER_FENCE":      {},
	"RESTOREWEAVE_ONNX_WORKER_SANDBOX":    {},
	"RESTOREWEAVE_ONNX_WORKER_NONCE_FD":   {},
	"RESTOREWEAVE_ONNX_WORKER_NONCE_PATH": {},
}
