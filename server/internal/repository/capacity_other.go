//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package repository

import (
	"context"
	"errors"
)

const CapacityAvailable = "AVAILABLE"

func probeFilesystemCapacity(ctx context.Context, path string) (uint64, uint64, uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, 0, err
	}
	return 0, 0, 0, errors.New("filesystem capacity probe unavailable on this platform")
}
