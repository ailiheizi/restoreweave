//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package exact

import (
	"os"
	"syscall"
)

// createRestoreFile rejects a symlink at the final path component on hosts
// that expose O_NOFOLLOW. Manifest validation handles symlink parents; this
// also closes the final-component replacement window during file creation.
func createRestoreFile(path string, mode os.FileMode) (*os.File, error) {
	flags := syscall.O_WRONLY | syscall.O_CREAT | syscall.O_EXCL | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
	fd, err := syscall.Open(path, flags, uint32(mode.Perm()))
	if err != nil {
		return nil, os.NewSyscallError("open", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, os.ErrInvalid
	}
	return file, nil
}
