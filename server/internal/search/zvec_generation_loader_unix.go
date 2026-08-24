//go:build purego && (darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package search

import (
	"fmt"

	"github.com/ebitengine/purego"
)

// verifyZvecLibraryLoaded uses RTLD_NOLOAD after zvec-go initialization. This
// never loads a second C++ image (which can duplicate glog); it proves that the
// exact staged path is already resident. If zvec-go fell through to cwd,
// executable, module, or system candidates, the explicit path is not resident
// and the generation is rejected closed.
func verifyZvecLibraryLoaded(path, expectedDigest string) error {
	actualDigest, err := zvecLibraryDigest(path)
	if err != nil || actualDigest != expectedDigest {
		return fmt.Errorf("%w: staged native library digest changed after load", ErrZvecUnavailable)
	}
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|zvecRTLDNoLoad)
	if err != nil {
		return fmt.Errorf("%w: explicit staged library was not loaded (ambient fallback rejected): %v", ErrZvecUnavailable, err)
	}
	defer purego.Dlclose(handle)
	if _, err := purego.Dlsym(handle, "zvec_get_version"); err != nil {
		return fmt.Errorf("%w: loaded staged library has no zvec API: %v", ErrZvecUnavailable, err)
	}
	return nil
}
