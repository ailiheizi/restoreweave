//go:build !linux && (darwin || dragonfly || freebsd || netbsd || openbsd)

package scanner

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// rootedRegularOpenFlags and rootedDirOpenFlags are the caller-facing open
// flags consumed by resolveBeneath on this platform.
const (
	rootedRegularOpenFlags = unix.O_RDONLY
	rootedDirOpenFlags     = unix.O_RDONLY | unix.O_DIRECTORY
)

func closeFd(fd int) error {
	return unix.Close(fd)
}

// This is the conservative non-Linux fallback resolver. openat2 is
// unavailable, so every component is resolved with its own openat using
// O_NOFOLLOW, and each step is verified with fstatat before use. The
// semantics are identical to the Linux path — no absolute paths, no "..", no
// symlinked intermediate components, no magic-link traversal (none can be
// followed because no symlink is ever followed) — but each resolution is a
// userspace walk instead of one kernel-enforced syscall, so the guarantee is
// weaker than openat2 and should be treated as such in any hardening review.
// O_NONBLOCK is added to every final open so that a regular-file-to-FIFO race
// cannot block the scanner; the opened object is type-checked by the caller.

// openRootFd opens the capture root read-only. O_NOFOLLOW rejects a symlink
// root and O_DIRECTORY rejects a non-directory root.
func openRootFd(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, os.NewSyscallError("open", err)
	}
	return fd, nil
}

// statFd returns the device and inode of an open descriptor.
func statFd(fd int) (device, inode uint64, err error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return 0, 0, os.NewSyscallError("fstat", err)
	}
	return uint64(stat.Dev), stat.Ino, nil
}

// statPathNoFollow returns the device and inode of a path without following a
// final symlink. It is used only as the root-replacement canary in
// CaptureRoot.VerifyRoot.
func statPathNoFollow(path string) (device, inode uint64, err error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(unix.AT_FDCWD, path, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return 0, 0, os.NewSyscallError("fstatat", err)
	}
	return uint64(stat.Dev), stat.Ino, nil
}

// resolveParent walks every parent component of relPath with openat,
// O_DIRECTORY and O_NOFOLLOW. Each step is verified with fstatat so that a
// component swapped after its open fails closed. The caller owns the returned
// parent descriptor.
func resolveParent(rootFd int, relPath string) (int, string, error) {
	components, final, err := splitRelative(relPath)
	if err != nil {
		return -1, "", err
	}
	current := rootFd
	for _, component := range components {
		next, err := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			if current != rootFd {
				_ = unix.Close(current)
			}
			return -1, "", os.NewSyscallError("openat", err)
		}
		var opened, named unix.Stat_t
		if err := unix.Fstat(next, &opened); err != nil {
			_ = unix.Close(next)
			if current != rootFd {
				_ = unix.Close(current)
			}
			return -1, "", os.NewSyscallError("fstat", err)
		}
		if err := unix.Fstatat(current, component, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			_ = unix.Close(next)
			if current != rootFd {
				_ = unix.Close(current)
			}
			return -1, "", os.NewSyscallError("fstatat", err)
		}
		if opened.Dev != named.Dev || opened.Ino != named.Ino {
			_ = unix.Close(next)
			if current != rootFd {
				_ = unix.Close(current)
			}
			return -1, "", fmt.Errorf("directory component %q swapped during resolution", component)
		}
		if current != rootFd {
			_ = unix.Close(current)
		}
		current = next
	}
	if current == rootFd {
		duplicated, err := unix.Dup(rootFd)
		if err != nil {
			return -1, "", os.NewSyscallError("dup", err)
		}
		return duplicated, final, nil
	}
	return current, final, nil
}

// resolveBeneath opens relPath relative to rootFd, walking intermediate
// components with O_NOFOLLOW and verifying each with fstatat. O_NOFOLLOW on
// the final component rejects symlink substitution for opens. The opened
// handle is verified against an immediate fstatat of the same name so that a
// swap between open and verification fails closed.
func resolveBeneath(rootFd int, relPath string, flags int) (int, error) {
	parentFd, name, err := resolveParent(rootFd, relPath)
	if err != nil {
		return -1, err
	}
	defer unix.Close(parentFd)

	finalFlags := flags | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	fd, err := unix.Openat(parentFd, name, finalFlags, 0)
	if err != nil {
		return -1, os.NewSyscallError("openat", err)
	}
	var opened, named unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return -1, os.NewSyscallError("fstat", err)
	}
	if err := unix.Fstatat(parentFd, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		_ = unix.Close(fd)
		return -1, os.NewSyscallError("fstatat", err)
	}
	if opened.Dev != named.Dev || opened.Ino != named.Ino {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("path %q swapped during open", relPath)
	}
	return fd, nil
}

// readlinkRelative reads the target of the symlink at relPath without
// following any intermediate symlink.
func readlinkRelative(rootFd int, relPath string) (string, error) {
	parentFd, name, err := resolveParent(rootFd, relPath)
	if err != nil {
		return "", err
	}
	defer unix.Close(parentFd)
	buffer := make([]byte, 256)
	for {
		count, err := unix.Readlinkat(parentFd, name, buffer)
		if err == nil {
			return string(buffer[:count]), nil
		}
		if err != unix.EINVAL && err != unix.ENAMETOOLONG {
			return "", os.NewSyscallError("readlinkat", err)
		}
		if len(buffer) >= 1<<20 {
			return "", fmt.Errorf("readlinkat target exceeds 1 MiB")
		}
		buffer = make([]byte, 2*len(buffer))
	}
}
