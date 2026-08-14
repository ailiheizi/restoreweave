package sandbox

import (
	"fmt"
	"sort"
	"strings"
)

// Validate rejects network, extra binds, and missing host-owned paths.
func (spec Spec) Validate() error {
	if strings.TrimSpace(spec.Binary) == "" {
		return fmt.Errorf("%w: worker binary is required", ErrInvalidSpec)
	}
	if strings.TrimSpace(spec.StagingDir) == "" {
		return fmt.Errorf("%w: host-owned staging dir is required", ErrInvalidSpec)
	}
	if spec.Network {
		return ErrNetworkRequested
	}
	if len(spec.ExtraBinds) > 0 {
		return ErrExtraBinds
	}
	for key := range spec.Env {
		if _, ok := allowedEnv[key]; !ok {
			return fmt.Errorf("%w: environment key %q is not allowlisted", ErrInvalidSpec, key)
		}
	}
	return nil
}

// BuildArgv returns bubblewrap arguments after the bwrap binary. It never
// bind-mounts an ambient source tree and never shares the network namespace.
func BuildArgv(spec Spec) ([]string, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	argv := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-net",
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--hostname", hostname,
		"--clearenv",
		"--ro-bind", spec.Binary, workerPath,
		"--bind", spec.StagingDir, stagePath,
		"--chdir", stagePath,
	}
	keys := make([]string, 0, len(spec.Env))
	for key := range spec.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		argv = append(argv, "--setenv", key, spec.Env[key])
	}
	argv = append(argv, workerPath)
	argv = append(argv, spec.Args...)
	return argv, nil
}
