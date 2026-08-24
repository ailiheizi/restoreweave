//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package search

import (
	"fmt"
	"os"
	"path/filepath"
)

// Unsupported platforms retain the strict final Lstat check. The reference
// native profile is qualified only on platforms with descriptor-level no
// follow support.
func openZvecLibraryNoFollow(path string) (*os.File, error) {
	clean := filepath.Clean(path)
	if clean == "" || !filepath.IsAbs(clean) || clean != path {
		return nil, fmt.Errorf("native library path must be absolute and clean")
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("native library must be a regular non-symlink file")
	}
	return os.Open(clean)
}
