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

import "errors"

var (
	ErrUnsupportedPlatform = errors.New("processor sandbox execution needs Linux bubblewrap")
	ErrInvalidSpec         = errors.New("sandbox spec cannot be confirmed")
	ErrNetworkRequested    = errors.New("network is refused")
	ErrExtraBinds          = errors.New("extra binds are refused")
	ErrBubblewrapMissing   = errors.New("bwrap is not on PATH")
)

const (
	workerPath = "/worker"
	stagePath  = "/stage"
	hostname   = "restoreweave-processor"
)

// Spec is the host-owned isolation request. Callers cannot attach the source
// tree or enable networking.
type Spec struct {
	Binary     string
	Args       []string
	StagingDir string
	Env        map[string]string
	Network    bool
	ExtraBinds []Bind
}

// Bind would attach another host path. The first sandbox profile rejects every
// extra bind so the source tree cannot leak in.
type Bind struct {
	Host  string
	Dest  string
	Write bool
}

var allowedEnv = map[string]struct{}{
	"PATH":   {},
	"LANG":   {},
	"LC_ALL": {},
	"TZ":     {},
	"HOME":   {},
}
