// Package fuse is the bundled read-only Linux FUSE adapter. It is a private
// presentation layer over SnapshotTree and FileAccess; go-fuse types never
// enter portable records.
package fuse

import (
	"errors"
	"fmt"
	"strings"
)

const (
	fsName     = "restoreweave"
	subtype    = "restoreweave"
	defaultTTL = 1.0
)

var (
	ErrUnsupportedPlatform = errors.New("kernel FUSE mount needs the Linux /dev/fuse ABI")
	ErrInvalidMountOptions = errors.New("mount policy cannot be confirmed")
	ErrAllowOther          = errors.New("allow_other is refused")
)

// Options are host-owned mount constraints. Callers cannot pass arbitrary
// fusermount options; the adapter always requires ro,nodev,nosuid,noexec and
// refuses allow_other.
type Options struct {
	Mountpoint  string
	SnapshotRef string
	AllowOther  bool
	Extra       []string
}

// RequiredFlags are the kernel flags this adapter will not start without.
func RequiredFlags() []string {
	return []string{"ro", "nodev", "nosuid", "noexec"}
}

func (opts Options) Validate() error {
	if strings.TrimSpace(opts.Mountpoint) == "" {
		return fmt.Errorf("%w: mountpoint is required", ErrInvalidMountOptions)
	}
	if strings.TrimSpace(opts.SnapshotRef) == "" {
		return fmt.Errorf("%w: snapshot ref is required", ErrInvalidMountOptions)
	}
	if opts.AllowOther {
		return ErrAllowOther
	}
	for _, option := range opts.Extra {
		switch strings.TrimSpace(option) {
		case "", "ro", "nodev", "nosuid", "noexec":
			continue
		default:
			return fmt.Errorf("%w: unqualified option %q", ErrInvalidMountOptions, option)
		}
	}
	return nil
}

func (opts Options) effectiveFlags() []string {
	return RequiredFlags()
}

// MutationErrno is the POSIX error every write-capable FUSE opcode must
// return. Tests and the Linux adapter share this value so Darwin can still
// verify the policy table.
const MutationErrno = 30 // EROFS
