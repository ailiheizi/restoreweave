//go:build !linux

package sandbox

import "context"

// Supported reports whether this build may execute bubblewrap.
func Supported() bool { return false }

// Run refuses to start bubblewrap on hosts without Linux namespaces.
func Run(_ context.Context, spec Spec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	return ErrUnsupportedPlatform
}
