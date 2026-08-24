//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package repository

import (
	"context"
	"errors"
	"io"
)

var ErrPublicationLockUnavailable = errors.New("repository publication lock is unavailable")

// AcquirePublicationLock is unavailable where the Unix flock implementation
// is not supported. SQLite publication fencing remains the portable lease
// boundary; callers fail closed if neither coordination mechanism is present.
func (*Dir) AcquirePublicationLock(context.Context, string) (io.Closer, error) {
	return nil, ErrPublicationLockUnavailable
}
