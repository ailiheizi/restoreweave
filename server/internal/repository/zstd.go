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

	"github.com/klauspost/compress/zstd"
)

// ZstdDir is a transparent, whole-file zstd repository. Content IDs and
// Receipt.Bytes always refer to the decompressed bytes; the object on disk is
// a checksummed zstd frame and Receipt.StoredBytes reports its physical size.
// Portable recovery records are inherited from Dir and remain uncompressed
// JSON so they can be inspected and imported independently.
type ZstdDir struct {
	*Dir
}

var _ DriverRecord = (*ZstdDir)(nil)

func (*ZstdDir) RepositoryProfile() ProfileDescription {
	return ProfileDescription{
		Repository:  RepositoryProfileLocalZstdV1,
		Compression: CompressionProfileZstdV1,
	}
}

// OpenZstdDir creates or opens the embedded local-zstd-v1 repository.
func OpenZstdDir(path string) (*ZstdDir, error) {
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
	if err := ensureRepositoryProfile(absolute, RepositoryProfileLocalZstdV1); err != nil {
		return nil, err
	}
	identity, err := loadOrCreateRepositoryIdentity(absolute)
	if err != nil {
		return nil, err
	}
	return &ZstdDir{Dir: &Dir{root: absolute, identity: identity}}, nil
}

func (repo *ZstdDir) Place(ctx context.Context, body io.Reader) (Receipt, error) {
	return repo.place(ctx, "", body)
}

func (repo *ZstdDir) PlaceExact(ctx context.Context, contentID string, body io.Reader) (Receipt, error) {
	if _, err := parseContentID(contentID); err != nil {
		return Receipt{}, err
	}
	return repo.place(ctx, contentID, body)
}

func (repo *ZstdDir) place(ctx context.Context, expectedID string, body io.Reader) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if body == nil {
		return Receipt{}, errors.New("placement body is required")
	}
	temp, err := os.CreateTemp(filepath.Join(repo.root, tmpDirName), "place-*.zst")
	if err != nil {
		return Receipt{}, fmt.Errorf("create placement tempfile: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	digest := sha256.New()
	encoder, err := zstd.NewWriter(temp, zstd.WithEncoderCRC(true), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return Receipt{}, fmt.Errorf("create zstd encoder: %w", err)
	}
	written, copyErr := io.Copy(io.MultiWriter(encoder, digest), body)
	closeErr := encoder.Close()
	if copyErr != nil {
		return Receipt{}, fmt.Errorf("write zstd placement: %w", copyErr)
	}
	if closeErr != nil {
		return Receipt{}, fmt.Errorf("close zstd placement: %w", closeErr)
	}
	if err := temp.Sync(); err != nil {
		return Receipt{}, fmt.Errorf("sync zstd placement: %w", err)
	}
	if err := temp.Close(); err != nil {
		return Receipt{}, fmt.Errorf("close zstd placement: %w", err)
	}
	contentID := AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	if expectedID != "" && expectedID != contentID {
		return Receipt{}, fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, contentID, expectedID)
	}
	dest := blobPath(repo.root, contentID)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return Receipt{}, fmt.Errorf("create blob directory: %w", err)
	}
	stored, err := fileSize(tempName)
	if err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{ContentID: contentID, Bytes: written, StoredBytes: stored}
	if _, err := os.Lstat(dest); err == nil {
		receipt.Existed = true
		if err := repo.Verify(ctx, contentID); err != nil {
			return Receipt{}, err
		}
		receipt.StoredBytes, err = fileSize(dest)
		if err != nil {
			return Receipt{}, err
		}
		return receipt, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Receipt{}, err
	}
	if err := publishNoReplace(tempName, dest, repo.root); err != nil {
		if errors.Is(err, os.ErrExist) {
			receipt.Existed = true
			if verifyErr := repo.Verify(ctx, contentID); verifyErr != nil {
				return Receipt{}, verifyErr
			}
			receipt.StoredBytes, err = fileSize(dest)
			if err != nil {
				return Receipt{}, err
			}
			return receipt, nil
		}
		return Receipt{}, fmt.Errorf("commit zstd blob: %w", err)
	}
	return receipt, nil
}

func (repo *ZstdDir) Open(ctx context.Context, contentID string) (io.ReadCloser, error) {
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
	if err != nil {
		return nil, err
	}
	decoder, err := zstd.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open zstd object: %w", err)
	}
	return &zstdReadCloser{decoder: decoder, file: file}, nil
}

func (repo *ZstdDir) Verify(ctx context.Context, contentID string) error {
	if _, err := parseContentID(contentID); err != nil {
		return err
	}
	body, err := repo.Open(ctx, contentID)
	if err != nil {
		return err
	}
	defer body.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, body); err != nil {
		return fmt.Errorf("verify zstd object: %w", err)
	}
	got := AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	if got != contentID {
		return fmt.Errorf("%w: %s", ErrDigestMismatch, contentID)
	}
	return nil
}

type zstdReadCloser struct {
	decoder *zstd.Decoder
	file    *os.File
}

func (r *zstdReadCloser) Read(p []byte) (int, error) { return r.decoder.Read(p) }

func (r *zstdReadCloser) Close() error {
	r.decoder.Close()
	return r.file.Close()
}
