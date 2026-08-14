//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package scanner

import (
	"os"
	"syscall"
)

func openPathNoFollow(path string, directory bool) (*os.File, error) {
	flags := syscall.O_RDONLY | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
	if directory {
		flags |= syscall.O_DIRECTORY
	}

	fd, err := syscall.Open(path, flags, 0)
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
