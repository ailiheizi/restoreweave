//go:build purego && windows

package search

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// verifyZvecLibraryLoaded asks Windows for the already-loaded module by its
// exact staged path. It does not load a second image, and therefore catches a
// zvec-go fallback to a different ambient candidate.
func verifyZvecLibraryLoaded(path, expectedDigest string) error {
	actualDigest, err := zvecLibraryDigest(path)
	if err != nil || actualDigest != expectedDigest {
		return fmt.Errorf("%w: staged native library digest changed after load", ErrZvecUnavailable)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("%w: encode staged native library path: %v", ErrZvecUnavailable, err)
	}
	var handle windows.Handle
	if err := windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, name, &handle); err != nil {
		return fmt.Errorf("%w: explicit staged library was not loaded (ambient fallback rejected): %v", ErrZvecUnavailable, err)
	}
	if _, err := windows.GetProcAddress(handle, "zvec_get_version"); err != nil {
		return fmt.Errorf("%w: loaded staged library has no zvec API: %v", ErrZvecUnavailable, err)
	}
	return nil
}
