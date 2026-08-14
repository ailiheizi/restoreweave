package scanner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrCaptureRootReplaced is the fail-closed sentinel returned when a
// capture-root binding no longer identifies the directory that was bound when
// the capture began. Callers must treat it as a hard abort: the scan must not
// silently retry against a different basis.
var ErrCaptureRootReplaced = errors.New("capture root was replaced")

// CaptureRoot is an opaque, descriptor-rooted binding to the directory that
// defines a capture namespace. All traversal and content reads are resolved
// relative to the retained file descriptor; the recorded path is display-only
// and is additionally revalidated as a replacement canary before every
// operation.
type CaptureRoot struct {
	mu     sync.Mutex
	fd     int
	path   string
	device uint64
	inode  uint64
}

// OpenCaptureRoot binds the directory at path. The root must be a real
// directory: a symbolic-link root, a non-directory, or a path containing NUL
// is rejected. The retained descriptor stays bound to the opened inode even
// if the path is later renamed away.
func OpenCaptureRoot(path string) (*CaptureRoot, error) {
	if strings.IndexByte(path, 0) >= 0 {
		return nil, fmt.Errorf("%w: root path contains NUL", ErrInvalidRequest)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve capture root path: %v", ErrInvalidRequest, err)
	}
	absolute = filepath.Clean(absolute)

	fd, err := openRootFd(absolute)
	if err != nil {
		return nil, err
	}
	capture := &CaptureRoot{fd: fd, path: absolute}
	device, inode, err := statFd(fd)
	if err != nil {
		_ = closeFd(fd)
		return nil, err
	}
	capture.device, capture.inode = device, inode
	return capture, nil
}

// Identity returns the device and inode recorded when the binding was opened.
// It is stable for the lifetime of the binding and never carries a descriptor
// number, so it is safe to serialize into a CaptureRootBindingRecord.
func (capture *CaptureRoot) Identity() (device uint64, inode uint64) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.device, capture.inode
}

// Path returns the display path the binding was opened from. It is a
// convenience field, not the security basis.
func (capture *CaptureRoot) Path() string {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.path
}

// Close releases the retained descriptor. It is idempotent.
func (capture *CaptureRoot) Close() error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.fd < 0 {
		return nil
	}
	err := closeFd(capture.fd)
	capture.fd = -1
	return err
}

// VerifyRoot revalidates the binding immediately before every rooted
// operation. It fails closed (ErrCaptureRootReplaced) when the retained
// descriptor no longer has the recorded identity (for example a closed and
// recycled descriptor) or when the recorded path no longer names the bound
// directory (ancestor rename, root replacement, bind-mount substitution, or
// symlink substitution at the root path). A live descriptor whose path moved
// is also rejected: the capture basis must be stable for the whole scan.
func (capture *CaptureRoot) VerifyRoot() error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.fd < 0 {
		return os.ErrClosed
	}
	device, inode, err := statFd(capture.fd)
	if err != nil {
		return fmt.Errorf("capture root descriptor is unusable: %w", err)
	}
	if device != capture.device || inode != capture.inode {
		return ErrCaptureRootReplaced
	}
	pathDevice, pathInode, err := statPathNoFollow(capture.path)
	if err != nil {
		return fmt.Errorf("%w: root path no longer resolves: %v", ErrCaptureRootReplaced, err)
	}
	if pathDevice != capture.device || pathInode != capture.inode {
		return ErrCaptureRootReplaced
	}
	return nil
}

// sanitizeRelativePath enforces the component grammar shared by every rooted
// operation. It rejects NUL bytes, absolute paths, empty components, and the
// "." and ".." components so that a relative path can never escape the
// capture-root descriptor.
func sanitizeRelativePath(rel string) (string, error) {
	if strings.IndexByte(rel, 0) >= 0 {
		return "", fmt.Errorf("%w: relative path contains NUL", ErrInvalidRequest)
	}
	if rel == "" {
		return "", fmt.Errorf("%w: empty relative path", ErrInvalidRequest)
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: absolute path is not allowed beneath a capture root", ErrInvalidRequest)
	}
	if rel == "." {
		return ".", nil
	}
	for _, component := range strings.Split(rel, "/") {
		if component == "" || component == "." || component == ".." {
			return "", fmt.Errorf("%w: unsafe path component %q", ErrInvalidRequest, component)
		}
	}
	return rel, nil
}

// splitRelative splits a sanitized relative path into its parent components
// and its final name. The root is represented as final "." with no parents.
func splitRelative(rel string) (parentComponents []string, final string, err error) {
	sanitized, err := sanitizeRelativePath(rel)
	if err != nil {
		return nil, "", err
	}
	if sanitized == "." {
		return nil, ".", nil
	}
	parts := strings.Split(sanitized, "/")
	return parts[:len(parts)-1], parts[len(parts)-1], nil
}
