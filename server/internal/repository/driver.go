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
	// StoredBytes is the number of bytes occupied by the repository object.
	// Bytes always describes the logical, uncompressed payload.
	StoredBytes int64
	Existed     bool
}

// Driver stores and reads exact byte objects addressed by sha256 content IDs.
type Driver interface {
	Place(ctx context.Context, body io.Reader) (Receipt, error)
	PlaceExact(ctx context.Context, contentID string, body io.Reader) (Receipt, error)
	Open(ctx context.Context, contentID string) (io.ReadCloser, error)
	Verify(ctx context.Context, contentID string) error
	Root() string
}

// RepairDriver is an explicit host-owned repair seam. Repair is never
// implicit during Open or Verify: callers provide a fresh exact byte stream,
// and the driver atomically replaces only a verified damaged object.
type RepairDriver interface {
	Repair(ctx context.Context, contentID string, body io.Reader) (Receipt, error)
}

// Dir is a filesystem CAS. Layout: <root>/blobs/sha256/<ab>/<hex>.
type Dir struct {
	root     string
	identity string
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
	if err := ensureRepositoryProfile(absolute, RepositoryProfileDirectoryCASDev); err != nil {
		return nil, err
	}
	identity, err := loadOrCreateRepositoryIdentity(absolute)
	if err != nil {
		return nil, err
	}
	return &Dir{root: absolute, identity: identity}, nil
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

func (repo *Dir) Repair(ctx context.Context, contentID string, body io.Reader) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if _, err := parseContentID(contentID); err != nil {
		return Receipt{}, err
	}
	if body == nil {
		return Receipt{}, errors.New("repair body is required")
	}
	temp, err := os.CreateTemp(filepath.Join(repo.root, tmpDirName), "repair-*.blob")
	if err != nil {
		return Receipt{}, fmt.Errorf("create repair tempfile: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, digest), body)
	if err != nil {
		return Receipt{}, fmt.Errorf("write repair: %w", err)
	}
	got := AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	if got != contentID {
		return Receipt{}, fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, got, contentID)
	}
	if err := temp.Sync(); err != nil {
		return Receipt{}, fmt.Errorf("sync repair: %w", err)
	}
	if err := temp.Close(); err != nil {
		return Receipt{}, fmt.Errorf("close repair: %w", err)
	}
	dest := blobPath(repo.root, contentID)
	if err := validateRepositoryParentChain(repo.root, filepath.Dir(dest)); err != nil {
		return Receipt{}, fmt.Errorf("repair destination: %w", err)
	}
	if info, statErr := os.Lstat(dest); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return Receipt{}, errors.New("repair target is not a regular file")
		}
		if verifyErr := repo.Verify(ctx, contentID); verifyErr == nil {
			stored, sizeErr := fileSize(dest)
			if sizeErr != nil {
				return Receipt{}, sizeErr
			}
			return Receipt{ContentID: contentID, Bytes: written, StoredBytes: stored, Existed: true}, nil
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Receipt{}, statErr
	}
	if err := os.Rename(tempName, dest); err != nil {
		return Receipt{}, fmt.Errorf("atomically replace repaired object: %w", err)
	}
	if err := syncFilesystemParentChain(repo.root); err != nil {
		return Receipt{}, err
	}
	if err := repo.Verify(ctx, contentID); err != nil {
		return Receipt{}, err
	}
	return Receipt{ContentID: contentID, Bytes: written, StoredBytes: written, Existed: true}, nil
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
		if err := verifyFile(repo.root, dest, contentID); err != nil {
			return Receipt{}, err
		}
		stored, err := fileSize(dest)
		if err != nil {
			return Receipt{}, err
		}
		return Receipt{ContentID: contentID, Bytes: written, StoredBytes: stored, Existed: true}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Receipt{}, err
	}
	if err := publishNoReplace(tempName, dest, repo.root); err != nil {
		if errors.Is(err, os.ErrExist) {
			if verifyErr := verifyFile(repo.root, dest, contentID); verifyErr != nil {
				return Receipt{}, verifyErr
			}
			stored, sizeErr := fileSize(dest)
			if sizeErr != nil {
				return Receipt{}, sizeErr
			}
			return Receipt{ContentID: contentID, Bytes: written, StoredBytes: stored, Existed: true}, nil
		}
		return Receipt{}, fmt.Errorf("commit blob: %w", err)
	}
	return Receipt{ContentID: contentID, Bytes: written, StoredBytes: written, Existed: false}, nil
}

func (repo *Dir) Open(ctx context.Context, contentID string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := parseContentID(contentID); err != nil {
		return nil, err
	}
	file, err := openRepositoryFile(repo.root, blobPath(repo.root, contentID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, contentID)
	}
	return file, err
}

func (repo *Dir) Verify(ctx context.Context, contentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return verifyFile(repo.root, blobPath(repo.root, contentID), contentID)
}

func verifyFile(root, path, contentID string) error {
	file, err := openRepositoryFile(root, path)
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
	mu       sync.Mutex
	blobs    map[string][]byte
	records  map[RecordRole]map[string][]byte
	identity string
}

func NewMemory() *Memory {
	return &Memory{
		blobs:    make(map[string][]byte),
		records:  make(map[RecordRole]map[string][]byte),
		identity: newInMemoryRepositoryIdentity(),
	}
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
	return Receipt{ContentID: got, Bytes: int64(len(payload)), StoredBytes: int64(len(payload)), Existed: existed}, nil
}

func (repo *Memory) Repair(ctx context.Context, contentID string, body io.Reader) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if _, err := parseContentID(contentID); err != nil {
		return Receipt{}, err
	}
	if body == nil {
		return Receipt{}, errors.New("repair body is required")
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return Receipt{}, err
	}
	sum := sha256.Sum256(payload)
	if got := AlgorithmSHA256 + ":" + hex.EncodeToString(sum[:]); got != contentID {
		return Receipt{}, fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, got, contentID)
	}
	repo.mu.Lock()
	if repo.blobs == nil {
		repo.blobs = make(map[string][]byte)
	}
	repo.blobs[contentID] = append([]byte(nil), payload...)
	repo.mu.Unlock()
	return Receipt{ContentID: contentID, Bytes: int64(len(payload)), StoredBytes: int64(len(payload)), Existed: true}, nil
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

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("repository object is not a regular file: %s", path)
	}
	return info.Size(), nil
}

// publishNoReplace links a fully synced temporary file into place. Unlike
// rename, link never replaces a concurrently published object.
func publishNoReplace(tempName, dest, repositoryRoot string) error {
	if err := os.Link(tempName, dest); err != nil {
		return err
	}
	if err := syncParentChain(filepath.Dir(dest), repositoryRoot); err != nil {
		_ = os.Remove(dest)
		return err
	}
	if err := os.Remove(tempName); err != nil {
		return err
	}
	return nil
}

func syncParentChain(start, stop string) error {
	path := filepath.Clean(start)
	stop = filepath.Clean(stop)
	relative, err := filepath.Rel(stop, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("sync directory %q is outside repository root %q", start, stop)
	}
	for {
		if err := syncDir(path); err != nil {
			return err
		}
		if path == stop {
			return nil
		}
		parent := filepath.Dir(path)
		if parent == path {
			return fmt.Errorf("repository root %q is not an ancestor of %q", stop, start)
		}
		path = parent
	}
}

func syncFilesystemParentChain(start string) error {
	root := filepath.Clean(start)
	for {
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return syncParentChain(start, root)
}

func validateRepositoryParentChain(root, target string) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("repair destination is outside repository root")
	}
	current := root
	if info, err := os.Lstat(current); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("repository root is not a regular directory")
	}
	for _, component := range splitRepositoryRelativePath(relative) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("repository directory %q: %w", current, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("repository directory %q is not a non-symlink directory", current)
		}
	}
	return nil
}

func splitRepositoryRelativePath(path string) []string {
	clean := filepath.Clean(path)
	parts := make([]string, 0, 4)
	for clean != "." && clean != string(filepath.Separator) {
		parent, base := filepath.Split(clean)
		if base != "" {
			parts = append(parts, base)
		}
		parent = filepath.Clean(parent)
		if parent == clean {
			break
		}
		clean = parent
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return parts
}

func syncDir(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
