//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package repository

import (
	"context"
	"fmt"

	"golang.org/x/sys/unix"
)

const CapacityAvailable = "AVAILABLE"

func probeFilesystemCapacity(ctx context.Context, path string) (uint64, uint64, uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, 0, err
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, 0, 0, fmt.Errorf("stat filesystem capacity: %w", err)
	}
	blockSize := uint64(stat.Bsize)
	if stat.Bsize <= 0 {
		return 0, 0, 0, fmt.Errorf("filesystem reports non-positive block size")
	}
	if stat.Blocks < 0 || stat.Bavail < 0 {
		return 0, 0, 0, fmt.Errorf("filesystem reports negative block count")
	}
	total, err := capacityBytes(uint64(stat.Blocks), blockSize)
	if err != nil {
		return 0, 0, 0, err
	}
	free, err := capacityBytes(uint64(stat.Bavail), blockSize)
	if err != nil {
		return 0, 0, 0, err
	}
	if free > total {
		return 0, 0, 0, fmt.Errorf("filesystem reports free space larger than total")
	}
	return total, free, total - free, nil
}
