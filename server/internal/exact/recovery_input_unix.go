//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package exact

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openRecoveryInput(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open recovery input: invalid file descriptor")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("recovery input is not a regular file")
	}
	return file, nil
}
