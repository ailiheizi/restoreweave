//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package scanner

import (
	"fmt"
	"os"
)

// The fallback cannot express O_NOFOLLOW through the standard library. The
// scanner still compares lstat with the opened handle before any read, so a
// replaced final component is rejected rather than accepted as content. Hosts
// that require strict no-open semantics should inject a native FileSystem.
func openPathNoFollow(path string, directory bool) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if directory != info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("opened object has unexpected type: %w", os.ErrInvalid)
	}
	return file, nil
}
