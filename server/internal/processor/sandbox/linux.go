//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
)

// Supported reports whether this build may execute bubblewrap.
func Supported() bool { return true }

// Run executes spec under bubblewrap when bwrap is on PATH. Tests may call
// BuildArgv without executing.
func Run(ctx context.Context, spec Spec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	bin, err := exec.LookPath("bwrap")
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
