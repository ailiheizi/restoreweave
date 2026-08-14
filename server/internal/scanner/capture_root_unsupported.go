//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package scanner

import (
	"errors"
	"io/fs"
)

// This platform has no descriptor-rooted capture support. Rooted mode fails
// closed: OpenCaptureRoot and every rooted resolution return an error instead
// of degrading to ambient path traversal.

var errCaptureRootUnsupported = errors.New("descriptor-rooted capture is not supported on this platform")

const (
	rootedRegularOpenFlags = 0
	rootedDirOpenFlags     = 0
)

func closeFd(fd int) error {
	return errCaptureRootUnsupported
}

func openRootFd(path string) (int, error) {
	return -1, errCaptureRootUnsupported
}

func statFd(fd int) (device, inode uint64, err error) {
	return 0, 0, errCaptureRootUnsupported
}

func statPathNoFollow(path string) (device, inode uint64, err error) {
	return 0, 0, errCaptureRootUnsupported
}

func resolveBeneath(rootFd int, relPath string, flags int) (int, error) {
	return -1, errCaptureRootUnsupported
}

func resolveParent(rootFd int, relPath string) (int, string, error) {
	return -1, "", errCaptureRootUnsupported
}

func statRelative(rootFd int, relPath string) (fs.FileInfo, error) {
	return nil, errCaptureRootUnsupported
}

func readlinkRelative(rootFd int, relPath string) (string, error) {
	return "", errCaptureRootUnsupported
}
