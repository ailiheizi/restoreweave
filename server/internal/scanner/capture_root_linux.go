//go:build linux

package scanner

import (
	"fmt"
	"io/fs"
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

// openRootFd opens the capture root with O_PATH so that no read permission is
// required on the root directory itself. O_NOFOLLOW rejects a symlink root
// and O_DIRECTORY rejects a non-directory root.
func openRootFd(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
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

// resolveBeneath opens relPath relative to rootFd with kernel-enforced
// RESOLVE_BENEATH semantics: no component may escape the root, no ".." may be
// traversed, and RESOLVE_NO_MAGICLINKS additionally forbids magic-link
// traversal (proc fd links and similar). O_NOFOLLOW is always applied to the
// final component so that a symlink is never followed for opens.
func resolveBeneath(rootFd int, relPath string, flags int) (int, error) {
	if _, err := sanitizeRelativePath(relPath); err != nil {
		return -1, err
	}
	how := &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	}
	fd, err := unix.Openat2(rootFd, relPath, how)
	if err != nil {
		return -1, os.NewSyscallError("openat2", err)
	}
	return fd, nil
}

// resolveParent walks every parent component of relPath with O_PATH and
// O_NOFOLLOW so that intermediate symlink substitution fails closed, and
// returns a descriptor for the parent directory plus the final name. The
// caller owns the returned descriptor. The final name is intentionally not
// opened here so that lstat and readlink can observe symlinks themselves.
func resolveParent(rootFd int, relPath string) (int, string, error) {
	components, final, err := splitRelative(relPath)
	if err != nil {
		return -1, "", err
	}
	current := rootFd
	for _, component := range components {
		next, err := unix.Openat(current, component, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			if current != rootFd {
				_ = unix.Close(current)
			}
			return -1, "", os.NewSyscallError("openat", err)
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

// statRelative returns no-follow metadata for relPath. O_PATH opens every
// object type, including a final symlink when combined with O_NOFOLLOW, so
// the resulting FileInfo is an ordinary os fileStat compatible with
// os.SameFile.
func statRelative(rootFd int, relPath string) (fs.FileInfo, error) {
	fd, err := resolveBeneath(rootFd, relPath, unix.O_PATH)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), relPath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, os.ErrInvalid
	}
	info, err := file.Stat()
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return info, nil
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
