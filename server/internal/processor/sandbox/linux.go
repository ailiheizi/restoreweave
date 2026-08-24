//go:build linux

package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

var preserveFDSupport = struct {
	sync.Once
	ok bool
}{}

// Supported reports whether this host can execute the bubblewrap profile.
// Linux alone is insufficient; the worker still requires a host-owned bwrap.
func Supported() bool {
	_, err := BubblewrapPath()
	return err == nil
}

// PreserveFDSupported reports whether the host bwrap can directly preserve
// fd 3. Older versions use the nonce-file fallback in the worker launcher.
func PreserveFDSupported() bool {
	path, err := BubblewrapPath()
	if err != nil {
		return false
	}
	preserveFDSupport.Do(func() {
		output, runErr := exec.Command(path, "--help").CombinedOutput()
		preserveFDSupport.ok = runErr == nil && bytes.Contains(output, []byte("--preserve-fd"))
	})
	return preserveFDSupport.ok
}

// BubblewrapPath returns a host-owned absolute executable path. Ambient PATH
// lookup is intentionally excluded from the qualified worker launch policy.
func BubblewrapPath() (string, error) {
	for _, candidate := range []string{"/usr/bin/bwrap", "/bin/bwrap", "/usr/local/bin/bwrap"} {
		info, err := os.Lstat(candidate)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}
	return "", ErrBubblewrapMissing
}

// Run executes spec under the host-owned bubblewrap path. Tests may call
// BuildArgv without executing.
func Run(ctx context.Context, spec Spec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if len(spec.PreserveFDs) > 0 && !PreserveFDSupported() {
		return ErrUnsupportedPlatform
	}
	bin, err := BubblewrapPath()
	if err != nil {
		return ErrBubblewrapMissing
	}
	argv, err := BuildArgv(spec)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, argv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bwrap: %w\n%s", err, out)
	}
	return nil
}
