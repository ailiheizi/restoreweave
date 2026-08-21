//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package exact

import "os"

// The standard library has no portable no-follow creation flag. O_EXCL still
// prevents replacing an existing final path; manifest validation prevents
// manifest-provided symlink parents on these hosts.
func createRestoreFile(path string, mode os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
}
