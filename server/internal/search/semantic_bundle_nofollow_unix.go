//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package search

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openBundleFileNoFollow(root, relative string) (*os.File, error) {
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open bundle root without following symlink: %w", err)
	}
	defer unix.Close(rootFD)

	parts := strings.Split(filepath.ToSlash(relative), "/")
	dirFD := rootFD
	closeDir := false
	defer func() {
		if closeDir {
			_ = unix.Close(dirFD)
			closeDir = false
		}
	}()
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		fd, openErr := unix.Openat(dirFD, part, flags, 0)
		if openErr != nil {
			return nil, fmt.Errorf("open asset component %q without following symlink: %w", part, openErr)
		}
		if closeDir {
			_ = unix.Close(dirFD)
		}
		if i == len(parts)-1 {
			file := os.NewFile(uintptr(fd), filepath.Join(root, filepath.FromSlash(relative)))
			if file == nil {
				_ = unix.Close(fd)
				return nil, fmt.Errorf("open asset: invalid file descriptor")
			}
			info, statErr := file.Stat()
			if statErr != nil {
				_ = file.Close()
				return nil, statErr
			}
			if !info.Mode().IsRegular() {
				_ = file.Close()
				return nil, fmt.Errorf("asset is not a regular file")
			}
			return file, nil
		}
		dirFD = fd
		closeDir = true
	}
	return nil, fmt.Errorf("asset path is empty")
}
