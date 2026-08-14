// Package repository implements the first exact-lane RepositoryDriver: a
// content-addressed blob store used to prove placement, independent readback,
// and catalog-free restore before a mature engine is selected.
package repository

import (
	"bytes"
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
)

const (
	AlgorithmSHA256 = "sha256"
	blobDirName     = "blobs"
	tmpDirName      = "tmp"
	hexPrefixLen    = 2
)

var (
	ErrInvalidContentID = errors.New("invalid content id")
	ErrNotFound         = errors.New("repository object not found")
	ErrDigestMismatch   = errors.New("placed bytes do not match the expected content id")
)

// Receipt is returned after an idempotent placement.
type Receipt struct {
	ContentID string
	Bytes     int64
	Existed   bool
}

// Driver stores and reads exact byte objects addressed by sha256 content IDs.
type Driver interface {
	Place(ctx context.Context, body io.Reader) (Receipt, error)
	PlaceExact(ctx context.Context, contentID string, body io.Reader) (Receipt, error)
	Open(ctx context.Context, contentID string) (io.ReadCloser, error)
	Verify(ctx context.Context, contentID string) error
	Root() string
}

// Dir is a filesystem CAS. Layout: <root>/blobs/sha256/<ab>/<hex>.
type Dir struct {
	root string
}

// OpenDir creates or opens a directory-backed repository.
func OpenDir(path string) (*Dir, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("repository path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	for _, name := range []string{
		absolute,
		filepath.Join(absolute, blobDirName, AlgorithmSHA256),
		filepath.Join(absolute, tmpDirName),
	} {
		if err := os.MkdirAll(name, 0o700); err != nil {
			return nil, fmt.Errorf("create repository directory: %w", err)
		}
	}
	return &Dir{root: absolute}, nil
}

func (repo *Dir) Root() string { return repo.root }

func (repo *Dir) Place(ctx context.Context, body io.Reader) (Receipt, error) {
	return repo.place(ctx, "", body)
}

func (repo *Dir) PlaceExact(ctx context.Context, contentID string, body io.Reader) (Receipt, error) {
	if _, err := parseContentID(contentID); err != nil {
		return Receipt{}, err
	}
	return repo.place(ctx, contentID, body)
}

func (repo *Dir) place(ctx context.Context, expectedID string, body io.Reader) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if body == nil {
		return Receipt{}, errors.New("placement body is required")
	}
	temp, err := os.CreateTemp(filepath.Join(repo.root, tmpDirName), "place-*.blob")
	if err != nil {
		return Receipt{}, fmt.Errorf("create placement tempfile: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()

	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, digest), body)
	if err != nil {
		return Receipt{}, fmt.Errorf("write placement: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return Receipt{}, fmt.Errorf("sync placement: %w", err)
	}
	if err := temp.Close(); err != nil {
		return Receipt{}, fmt.Errorf("close placement: %w", err)
	}

	contentID := AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	if expectedID != "" && expectedID != contentID {
		return Receipt{}, fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, contentID, expectedID)
	}
	dest := blobPath(repo.root, contentID)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return Receipt{}, fmt.Errorf("create blob directory: %w", err)
	}
	if _, err := os.Lstat(dest); err == nil {
		if err := verifyFile(dest, contentID); err != nil {
			return Receipt{}, err
		}
		return Receipt{ContentID: contentID, Bytes: written, Existed: true}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Receipt{}, err
	}
	if err := os.Rename(tempName, dest); err != nil {
		return Receipt{}, fmt.Errorf("commit blob: %w", err)
	}
	return Receipt{ContentID: contentID, Bytes: written, Existed: false}, nil
}

func (repo *Dir) Open(ctx context.Context, contentID string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := parseContentID(contentID); err != nil {
		return nil, err
	}
	file, err := os.Open(blobPath(repo.root, contentID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, contentID)
	}
	return file, err
}

func (repo *Dir) Verify(ctx context.Context, contentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return verifyFile(blobPath(repo.root, contentID), contentID)
}

func verifyFile(path, contentID string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, contentID)
	}
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	got := AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	if got != contentID {
		return fmt.Errorf("%w: %s", ErrDigestMismatch, contentID)
	}
	return nil
}

func blobPath(root, contentID string) string {
	hexID, _ := parseContentID(contentID)
	prefix := hexID
	if len(prefix) >= hexPrefixLen {
		prefix = prefix[:hexPrefixLen]
	}
	return filepath.Join(root, blobDirName, AlgorithmSHA256, prefix, hexID)
}

func parseContentID(contentID string) (string, error) {
	algorithm, payload, ok := strings.Cut(contentID, ":")
	if !ok || algorithm != AlgorithmSHA256 || len(payload) != 64 {
		return "", fmt.Errorf("%w: %q", ErrInvalidContentID, contentID)
	}
	if _, err := hex.DecodeString(payload); err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidContentID, contentID)
	}
	if payload != strings.ToLower(payload) {
		return "", fmt.Errorf("%w: %q", ErrInvalidContentID, contentID)
	}
	return payload, nil
}

// Memory is an in-process CAS used by tests that do not need a directory.
type Memory struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func NewMemory() *Memory {
	return &Memory{blobs: make(map[string][]byte)}
}

func (repo *Memory) Root() string { return ":memory:" }

func (repo *Memory) Place(ctx context.Context, body io.Reader) (Receipt, error) {
	return repo.PlaceExact(ctx, "", body)
}

func (repo *Memory) PlaceExact(ctx context.Context, contentID string, body io.Reader) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return Receipt{}, err
	}
	sum := sha256.Sum256(payload)
	got := AlgorithmSHA256 + ":" + hex.EncodeToString(sum[:])
	if contentID != "" && contentID != got {
		return Receipt{}, fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, got, contentID)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	_, existed := repo.blobs[got]
	if !existed {
		repo.blobs[got] = append([]byte(nil), payload...)
	}
	return Receipt{ContentID: got, Bytes: int64(len(payload)), Existed: existed}, nil
}

func (repo *Memory) Open(ctx context.Context, contentID string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo.mu.Lock()
	payload, ok := repo.blobs[contentID]
	repo.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, contentID)
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

func (repo *Memory) Verify(ctx context.Context, contentID string) error {
	body, err := repo.Open(ctx, contentID)
	if err != nil {
		return err
	}
	defer body.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, body); err != nil {
		return err
	}
	got := AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	if got != contentID {
		return fmt.Errorf("%w: %s", ErrDigestMismatch, contentID)
	}
	return nil
}
