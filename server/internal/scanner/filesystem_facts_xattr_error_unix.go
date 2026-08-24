//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package scanner

import (
	"errors"

	"golang.org/x/sys/unix"
)

func isUnsupportedXAttrError(err error) bool {
	return errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL)
}
