//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package search

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Platforms without openat-style no-follow primitives use a strict component
// walk and final Lstat immediately before opening. Such builds remain
// conservative about symlinks but do not claim Unix descriptor-level fencing.
func openBundleFileNoFollow(root, relative string) (*os.File, error) {
	current := root
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink is not allowed")
		}
	}
	return os.Open(current)
}
