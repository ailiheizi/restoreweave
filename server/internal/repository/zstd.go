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

	"github.com/klauspost/compress/zstd"
)

// ZstdDir is a transparent, whole-file zstd repository. Content IDs and
// Receipt.Bytes always refer to the decompressed bytes; the object on disk is
// a checksummed zstd frame and Receipt.StoredBytes reports its physical size.
// Portable recovery records are inherited from Dir and remain uncompressed
// JSON so they can be inspected and imported independently.
type ZstdDir struct {
	*Dir
	encryption *zstdEncryption
}

type zstdEncryption struct {
	keyRef string
	key    []byte
}

var _ DriverRecord = (*ZstdDir)(nil)

func (repo *ZstdDir) RepositoryProfile() ProfileDescription {
	profile := ProfileDescription{Repository: RepositoryProfileLocalZstdV1, Compression: CompressionProfileZstdV1, Encryption: EncryptionNone}
	if repo != nil && repo.encryption != nil {
		profile.Repository = RepositoryProfileLocalZstdEncryptedV1
		profile.Encryption = EncryptionProfileAES256GCMZstdV1
	}
	return profile
}

// OpenZstdDir creates or opens the embedded local-zstd-v1 repository.
func OpenZstdDir(path string) (*ZstdDir, error) {
	return openZstdDir(path, false, "", nil)
}

// OpenEncryptedZstdDir creates or opens an explicitly encrypted zstd
// repository. keyRef is persisted as non-secret metadata; the key itself is
// resolved only through provider. An empty keyRef opens an existing encrypted
// repository using the reference already persisted in that repository.
func OpenEncryptedZstdDir(path, keyRef string, provider KeyProvider) (*ZstdDir, error) {
	return openZstdDir(path, true, keyRef, provider)
}

func openZstdDir(path string, encrypted bool, keyRef string, provider KeyProvider) (*ZstdDir, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("repository path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	profile := RepositoryProfileLocalZstdV1
	if encrypted {
		profile = RepositoryProfileLocalZstdEncryptedV1
	}
	var preResolvedKey []byte
	if encrypted && strings.TrimSpace(keyRef) != "" {
		validatedRef, err := validateKeyRef(keyRef)
		if err != nil {
			return nil, err
		}
		preResolvedKey, err = resolveEncryptionKey(context.Background(), provider, validatedRef)
		if err != nil {
			return nil, err
		}
		keyRef = validatedRef
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
	if err := ensureRepositoryProfile(absolute, profile); err != nil {
		return nil, err
	}
	var encryption *zstdEncryption
	if encrypted {
		if strings.TrimSpace(keyRef) == "" {
			metadata, err := readEncryptionMetadata(absolute, profile)
			if err != nil {
				return nil, err
			}
			keyRef = metadata.KeyRef
		}
		validatedRef, err := validateKeyRef(keyRef)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(keyRef) != "" {
			if err := ensureEncryptionMetadata(absolute, validatedRef); err != nil {
				return nil, err
			}
		}
		key := preResolvedKey
		if key == nil {
			key, err = resolveEncryptionKey(context.Background(), provider, validatedRef)
			if err != nil {
				return nil, err
			}
		}
		encryption = &zstdEncryption{keyRef: validatedRef, key: key}
	} else if err := rejectEncryptionMetadata(absolute); err != nil {
		return nil, err
	}
	identity, err := loadOrCreateRepositoryIdentity(absolute)
	if err != nil {
		return nil, err
	}
	return &ZstdDir{Dir: &Dir{root: absolute, identity: identity}, encryption: encryption}, nil
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

// Repair replaces a damaged zstd object only after compressing and hashing the
// supplied logical bytes. The replacement is staged privately and verified by
// decoding before the destination is made visible.
func (repo *ZstdDir) Repair(ctx context.Context, contentID string, body io.Reader) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if _, err := parseContentID(contentID); err != nil {
		return Receipt{}, err
	}
	if body == nil {
		return Receipt{}, errors.New("repair body is required")
	}
	temp, err := os.CreateTemp(filepath.Join(repo.root, tmpDirName), "repair-*.zst")
	if err != nil {
		return Receipt{}, fmt.Errorf("create zstd repair tempfile: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	digest := sha256.New()
	encoder, err := zstd.NewWriter(temp, zstd.WithEncoderCRC(true), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return Receipt{}, fmt.Errorf("create zstd repair encoder: %w", err)
	}
	written, copyErr := io.Copy(io.MultiWriter(encoder, digest), body)
	closeErr := encoder.Close()
	if copyErr != nil {
		return Receipt{}, fmt.Errorf("write zstd repair: %w", copyErr)
	}
	if closeErr != nil {
		return Receipt{}, fmt.Errorf("close zstd repair: %w", closeErr)
	}
	got := AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	if got != contentID {
		return Receipt{}, fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, got, contentID)
	}
	if err := temp.Sync(); err != nil {
		return Receipt{}, fmt.Errorf("sync zstd repair: %w", err)
	}
	if err := temp.Close(); err != nil {
		return Receipt{}, fmt.Errorf("close zstd repair: %w", err)
	}
	if repo.encryption != nil {
		tempName, err = sealEncryptedZstd(ctx, tempName, repo.root, RepositoryProfileLocalZstdEncryptedV1, repo.encryption.keyRef, repo.encryption.key)
		if err != nil {
			return Receipt{}, err
		}
	}
	dest := blobPath(repo.root, contentID)
	if err := validateRepositoryParentChain(repo.root, filepath.Dir(dest)); err != nil {
		return Receipt{}, fmt.Errorf("zstd repair destination: %w", err)
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
		return Receipt{}, fmt.Errorf("atomically replace repaired zstd object: %w", err)
	}
	if err := syncFilesystemParentChain(repo.root); err != nil {
		return Receipt{}, err
	}
	if err := repo.Verify(ctx, contentID); err != nil {
		return Receipt{}, err
	}
	stored, err := fileSize(dest)
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{ContentID: contentID, Bytes: written, StoredBytes: stored, Existed: true}, nil
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
	if repo.encryption != nil {
		tempName, err = sealEncryptedZstd(ctx, tempName, repo.root, RepositoryProfileLocalZstdEncryptedV1, repo.encryption.keyRef, repo.encryption.key)
		if err != nil {
			return Receipt{}, err
		}
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
	file, err := openRepositoryFile(repo.root, blobPath(repo.root, contentID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, contentID)
	}
	if err != nil {
		return nil, err
	}
	if repo.encryption != nil {
		compressed, decryptErr := decryptEncryptedZstd(ctx, file, RepositoryProfileLocalZstdEncryptedV1, repo.encryption.keyRef, repo.encryption.key)
		closeErr := file.Close()
		if decryptErr != nil {
			return nil, decryptErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		decoder, err := zstd.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, fmt.Errorf("open encrypted zstd object: %w", err)
		}
		return &zstdReadCloser{decoder: decoder, closer: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	decoder, err := zstd.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open zstd object: %w", err)
	}
	return &zstdReadCloser{decoder: decoder, closer: file}, nil
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
	closer  io.Closer
}

func (r *zstdReadCloser) Read(p []byte) (int, error) { return r.decoder.Read(p) }

func (r *zstdReadCloser) Close() error {
	r.decoder.Close()
	return r.closer.Close()
}
