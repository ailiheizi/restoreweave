//go:build !darwin && !freebsd && !linux && !netbsd

package scanner

import "time"

func captureRootedFilesystemFacts(_ int, _ string, kind EntryKind, now time.Time) FilesystemFacts {
	return emptyFilesystemFacts(now, kind)
}
