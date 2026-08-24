package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
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
	if !filepath.IsAbs(spec.Binary) || filepath.Clean(spec.Binary) != spec.Binary ||
		!filepath.IsAbs(spec.StagingDir) || filepath.Clean(spec.StagingDir) != spec.StagingDir {
		return fmt.Errorf("%w: binary and staging paths must be absolute and canonical", ErrInvalidSpec)
	}
	if spec.Network {
		return ErrNetworkRequested
	}
	if len(spec.ExtraBinds) > 0 {
		return ErrExtraBinds
	}
	seenFD := make(map[int]struct{}, len(spec.PreserveFDs))
	for _, fd := range spec.PreserveFDs {
		if fd != 3 {
			return fmt.Errorf("%w: only nonce fd 3 may be preserved", ErrInvalidSpec)
		}
		if _, ok := seenFD[fd]; ok {
			return fmt.Errorf("%w: duplicate preserved fd %d", ErrInvalidSpec, fd)
		}
		seenFD[fd] = struct{}{}
	}
	for key := range spec.Env {
		if _, ok := allowedEnv[key]; !ok {
			return fmt.Errorf("%w: environment key %q is not allowlisted", ErrInvalidSpec, key)
		}
	}
	if spec.NonceFilePath != "" && (len(spec.PreserveFDs) != 0 || spec.NonceFilePath != noncePath) {
		return fmt.Errorf("%w: nonce file path is fixed and mutually exclusive with preserved fds", ErrInvalidSpec)
	}
	return nil
}

// BuildArgv returns bubblewrap arguments after the bwrap binary. It never
// bind-mounts an ambient source tree and never shares the network namespace.
func BuildArgv(spec Spec) ([]string, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.ReadOnlyStaging {
		info, err := os.Stat("/bin")
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%w: read-only worker profile requires /bin", ErrInvalidSpec)
		}
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
		"--chdir", stagePath,
	}
	if spec.ReadOnlyStaging {
		// Dynamically linked workers need fixed read-only system libraries. These
		// mounts do not expose source data or writable state; model/runtime assets
		// remain confined to the private staging tree.
		for _, dir := range []string{"/bin", "/usr", "/lib", "/lib64", "/usr/lib64", "/sys"} {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				argv = append(argv, "--ro-bind", dir, dir)
			}
		}
		argv = append(argv, "--proc", "/proc")
		argv = append(argv, "--tmpfs", "/tmp")
	}
	stageBind := "--bind"
	if spec.ReadOnlyStaging {
		stageBind = "--ro-bind"
	}
	// Keep the staging bind adjacent to the worker mount. The worker receives
	// only this private tree and the explicitly selected executable.
	argv = append(argv, stageBind, spec.StagingDir, stagePath)
	if spec.NonceFilePath != "" {
		argv = append(argv, "--file", "3", spec.NonceFilePath)
	} else {
		for _, fd := range spec.PreserveFDs {
			argv = append(argv, "--preserve-fd", fmt.Sprint(fd))
		}
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
