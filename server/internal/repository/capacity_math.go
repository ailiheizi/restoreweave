package repository

import "fmt"

func capacityBytes(blocks, blockSize uint64) (uint64, error) {
	if blocks != 0 && blockSize > ^uint64(0)/blocks {
		return 0, fmt.Errorf("filesystem capacity overflows uint64")
	}
	return blocks * blockSize, nil
}
