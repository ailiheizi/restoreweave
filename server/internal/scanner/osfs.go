package scanner

import (
	"io/fs"
	"os"
)

// OSFileSystem is the production host-filesystem implementation.
type OSFileSystem struct{}

func (OSFileSystem) Lstat(path string) (fs.FileInfo, error) {
	return os.Lstat(path)
}

func (OSFileSystem) Readlink(path string) (string, error) {
	return os.Readlink(path)
}

func (OSFileSystem) OpenRegularNoFollow(path string) (ReadStatCloser, error) {
	return openPathNoFollow(path, false)
}

func (OSFileSystem) OpenDirNoFollow(path string) (ReadDirStatCloser, error) {
	return openPathNoFollow(path, true)
}
