package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RootedFileSystem is a FileSystem whose every operation is resolved
// component-relative to a retained CaptureRoot descriptor. The absolute paths
// it receives are display strings; the security basis is the descriptor, and
// no operation ever resolves an ambient path. The capture root is revalidated
// before every operation and root replacement fails closed.
type RootedFileSystem struct {
	capture  *CaptureRoot
	rootPath string
}

// NewRootedFileSystem wraps capture with display path capture.Path().
func NewRootedFileSystem(capture *CaptureRoot) *RootedFileSystem {
	return &RootedFileSystem{capture: capture, rootPath: capture.Path()}
}

// verifyRoot revalidates the binding immediately before every operation.
func (fileSystem *RootedFileSystem) verifyRoot() error {
	return fileSystem.capture.VerifyRoot()
}

// relative converts a display absolute path into the sanitized relative path
// resolved against the root descriptor. A path outside the display namespace
// is rejected even though it could never escape the descriptor itself.
func (fileSystem *RootedFileSystem) relative(absolutePath string) (string, error) {
	if absolutePath == fileSystem.rootPath {
		return ".", nil
	}
	prefix := fileSystem.rootPath + string(filepath.Separator)
	if !strings.HasPrefix(absolutePath, prefix) {
		return "", fmt.Errorf("%w: path %q is outside capture root %q", ErrInvalidRequest, absolutePath, fileSystem.rootPath)
	}
	return sanitizeRelativePath(strings.TrimPrefix(absolutePath, prefix))
}

func (fileSystem *RootedFileSystem) Lstat(path string) (fs.FileInfo, error) {
	if err := fileSystem.verifyRoot(); err != nil {
		return nil, err
	}
	rel, err := fileSystem.relative(path)
	if err != nil {
		return nil, err
	}
	return statRelative(fileSystem.capture.fd, rel)
}

func (fileSystem *RootedFileSystem) Readlink(path string) (string, error) {
	if err := fileSystem.verifyRoot(); err != nil {
		return "", err
	}
	rel, err := fileSystem.relative(path)
	if err != nil {
		return "", err
	}
	return readlinkRelative(fileSystem.capture.fd, rel)
}

func (fileSystem *RootedFileSystem) OpenRegularNoFollow(path string) (ReadStatCloser, error) {
	if err := fileSystem.verifyRoot(); err != nil {
		return nil, err
	}
	rel, err := fileSystem.relative(path)
	if err != nil {
		return nil, err
	}
	fd, err := resolveBeneath(fileSystem.capture.fd, rel, rootedRegularOpenFlags)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = closeFd(fd)
		return nil, os.ErrInvalid
	}
	return &rootedRegularFile{capture: fileSystem.capture, file: file}, nil
}

func (fileSystem *RootedFileSystem) OpenDirNoFollow(path string) (ReadDirStatCloser, error) {
	if err := fileSystem.verifyRoot(); err != nil {
		return nil, err
	}
	rel, err := fileSystem.relative(path)
	if err != nil {
		return nil, err
	}
	fd, err := resolveBeneath(fileSystem.capture.fd, rel, rootedDirOpenFlags)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = closeFd(fd)
		return nil, os.ErrInvalid
	}
	return &rootedDirFile{capture: fileSystem.capture, file: file}, nil
}

// CaptureFilesystemFacts reads optional metadata through the same retained
// root descriptor as traversal. It never reopens the display path from the
// ambient process working directory.
func (fileSystem *RootedFileSystem) CaptureFilesystemFacts(path string, kind EntryKind) FilesystemFacts {
	now := time.Now().UTC()
	if err := fileSystem.verifyRoot(); err != nil {
		facts := emptyFilesystemFacts(now, kind)
		facts.XAttrs.State = CaptureFactUnobserved
		facts.XAttrs.ReasonCode = "CAPTURE_ROOT_UNAVAILABLE"
		facts.ACLs.State = CaptureFactUnobserved
		facts.ACLs.ReasonCode = "CAPTURE_ROOT_UNAVAILABLE"
		return facts
	}
	rel, err := fileSystem.relative(path)
	if err != nil {
		facts := emptyFilesystemFacts(now, kind)
		facts.XAttrs.State = CaptureFactUnobserved
		facts.XAttrs.ReasonCode = "CAPTURE_PATH_INVALID"
		facts.ACLs.State = CaptureFactUnobserved
		facts.ACLs.ReasonCode = "CAPTURE_PATH_INVALID"
		return facts
	}
	return captureRootedFilesystemFacts(fileSystem.capture.fd, rel, kind, now)
}

// rootedRegularFile is a no-follow regular-file handle. Stat revalidates the
// capture root so that a root replacement detected after the handle was
// opened still fails closed instead of trusting the handle.
type rootedRegularFile struct {
	capture *CaptureRoot
	file    *os.File
}

func (handle *rootedRegularFile) Read(buffer []byte) (int, error) {
	return handle.file.Read(buffer)
}

func (handle *rootedRegularFile) Close() error {
	return handle.file.Close()
}

func (handle *rootedRegularFile) Stat() (fs.FileInfo, error) {
	if err := handle.capture.VerifyRoot(); err != nil {
		return nil, err
	}
	return handle.file.Stat()
}

// rootedDirFile is a no-follow directory handle with the same revalidation.
type rootedDirFile struct {
	capture *CaptureRoot
	file    *os.File
}

func (handle *rootedDirFile) Close() error {
	return handle.file.Close()
}

func (handle *rootedDirFile) Stat() (fs.FileInfo, error) {
	if err := handle.capture.VerifyRoot(); err != nil {
		return nil, err
	}
	return handle.file.Stat()
}

func (handle *rootedDirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	return handle.file.ReadDir(n)
}
