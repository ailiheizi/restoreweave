//go:build darwin

package scanner

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// statRelative returns no-follow metadata for relPath. macOS has no O_PATH,
// so the final component is opened with O_SYMLINK: the descriptor then refers
// to the symlink itself instead of its target, which makes the resulting
// FileInfo an ordinary os fileStat compatible with os.SameFile. O_NOFOLLOW is
// deliberately not combined with O_SYMLINK (on darwin the two flags together
// fail with ELOOP on symlinks). The open is verified against an immediate
// fstatat of the same name so a swap between open and verification fails
// closed. Weakness versus Linux: a Unix-domain socket in the namespace cannot
// be opened this way and its lstat fails closed instead of recording it.
func statRelative(rootFd int, relPath string) (fs.FileInfo, error) {
	parentFd, name, err := resolveParent(rootFd, relPath)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parentFd)

	fd, err := unix.Openat(parentFd, name, unix.O_RDONLY|unix.O_SYMLINK|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, os.NewSyscallError("openat", err)
	}
	var opened, named unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return nil, os.NewSyscallError("fstat", err)
	}
	if err := unix.Fstatat(parentFd, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		_ = unix.Close(fd)
		return nil, os.NewSyscallError("fstatat", err)
	}
	if opened.Dev != named.Dev || opened.Ino != named.Ino {
		_ = unix.Close(fd)
		return nil, &os.PathError{Op: "openat", Path: relPath, Err: os.ErrInvalid}
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
