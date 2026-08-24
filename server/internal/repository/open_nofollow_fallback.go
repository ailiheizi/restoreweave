//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package repository

import (
	"fmt"
	"io/fs"
	"os"
)

// The standard library has no portable no-follow open flag. The fallback
// still checks the opened handle's type before any caller reads it; strict
// no-follow qualification remains platform-specific on these hosts.
func openRepositoryFile(root, path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("repository object is not a regular file: %q", path)
	}
	return file, nil
}
