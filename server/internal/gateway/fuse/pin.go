package fuse

// Keep the Linux FUSE pin visible to `go mod tidy` on non-Linux hosts,
// where linux.go is not compiled.
import _ "github.com/hanwen/go-fuse/v2/fs"
