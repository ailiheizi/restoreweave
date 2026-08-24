//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package search

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// openZvecLibraryNoFollow uses the package's descriptor-relative no-follow
// opener. Absolute paths are reduced to a root-relative component sequence so
// neither the source nor a staged copy can be silently replaced by a symlink.
func openZvecLibraryNoFollow(path string) (*os.File, error) {
	clean := filepath.Clean(path)
	if clean == "" || !filepath.IsAbs(clean) || filepath.Clean(clean) != clean {
		return nil, fmt.Errorf("native library path must be absolute and clean")
	}
	// Darwin commonly exposes /tmp and Go's test temp roots through an
	// ancestor symlink (/var -> /private/var). Resolve only the ancestor; the
	// final library component is still opened with O_NOFOLLOW below.
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return nil, fmt.Errorf("resolve native library parent: %w", err)
	}
	canonical := filepath.Join(parent, filepath.Base(clean))
	relative := strings.TrimPrefix(filepath.ToSlash(canonical), "/")
	if relative == "" {
		return nil, fmt.Errorf("native library path is a directory")
	}
	return openBundleFileNoFollow(string(filepath.Separator), relative)
}
