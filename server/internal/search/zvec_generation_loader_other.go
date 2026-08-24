//go:build purego && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package search

import "fmt"

func verifyZvecLibraryLoaded(string, string) error {
	return fmt.Errorf("%w: exact staged native loader verification is unavailable on this platform", ErrZvecUnavailable)
}
