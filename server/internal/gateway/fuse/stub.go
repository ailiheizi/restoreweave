//go:build !linux

package fuse

import "context"

// Supported reports whether this build can attach a kernel FUSE mount.
// Darwin is Unix, but this adapter speaks the Linux FUSE device ABI
// (/dev/fuse via go-fuse). This host has no /dev/fuse, and macFUSE is a
// different kernel interface. In-process Export walks still run here.
func Supported() bool { return false }

// Serve refuses to attach a kernel mount without Linux /dev/fuse.
func Serve(ctx context.Context, export Export, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	return ErrUnsupportedPlatform
}
