//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var ErrPublicationLockUnavailable = errors.New("repository publication lock is unavailable")

// AcquirePublicationLock takes an OS-level repository-scoped exclusive lock.
// flock is released by the kernel when the owning process exits, so a killed
// writer cannot strand publication authority behind a stale lock file.
func (repo *Dir) AcquirePublicationLock(ctx context.Context, publicationDomain string) (io.Closer, error) {
	if repo == nil || strings.TrimSpace(repo.root) == "" {
		return nil, ErrPublicationLockUnavailable
	}
	if strings.TrimSpace(publicationDomain) == "" || strings.ContainsRune(publicationDomain, 0) {
		return nil, fmt.Errorf("%w: publication domain is required", ErrPublicationLockUnavailable)
	}
	lockDigest := sha256.Sum256([]byte(publicationDomain))
	lockDir := filepath.Join(repo.root, recoveryDirName, "locks")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: create lock directory: %v", ErrPublicationLockUnavailable, err)
	}
	lockPath := filepath.Join(lockDir, "publication-"+hex.EncodeToString(lockDigest[:])+".lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w: open lock: %v", ErrPublicationLockUnavailable, err)
	}
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return &publicationLock{file: file}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("%w: acquire lock: %v", ErrPublicationLockUnavailable, err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type publicationLock struct {
	file *os.File
	once sync.Once
	err  error
}

func (lock *publicationLock) Close() error {
	if lock == nil {
		return nil
	}
	lock.once.Do(func() {
		if lock.file == nil {
			return
		}
		if err := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN); err != nil {
			lock.err = err
		}
		if err := lock.file.Close(); lock.err == nil {
			lock.err = err
		}
	})
	return lock.err
}
