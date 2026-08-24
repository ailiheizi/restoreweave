//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package repository

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// openRepositoryFile resolves a repository-relative path from an opened root.
// Every directory component and the final object is opened with O_NOFOLLOW so
// a relocated or tampered repository cannot redirect a read through a symlink.
func openRepositoryFile(root, path string) (*os.File, error) {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || relative == "." || relative == ".." ||
		len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return nil, fmt.Errorf("repository object path is outside repository root: %q", path)
	}
	components := splitRepositoryPath(relative)
	if len(components) == 0 {
		return nil, fmt.Errorf("repository object path is empty")
	}

	rootFD, err := unix.Open(filepath.Clean(root), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, os.NewSyscallError("open repository root", err)
	}
	current := rootFD
	defer func() {
		if current >= 0 {
			_ = unix.Close(current)
		}
	}()

	for _, component := range components[:len(components)-1] {
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			return nil, os.NewSyscallError("open repository directory", openErr)
		}
		if current != rootFD {
			_ = unix.Close(current)
		}
		current = next
	}

	fd, err := unix.Openat(current, components[len(components)-1], unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, os.NewSyscallError("open repository object", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, os.ErrInvalid
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, statErr
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("repository object is not a regular file: %q", path)
	}
	return file, nil
}

func splitRepositoryPath(path string) []string {
	clean := filepath.Clean(path)
	parts := make([]string, 0, 4)
	for clean != "." && clean != string(filepath.Separator) {
		parent, base := filepath.Split(clean)
		if base != "" {
			parts = append(parts, base)
		}
		parent = filepath.Clean(parent)
		if parent == clean {
			break
		}
		clean = parent
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return parts
}
