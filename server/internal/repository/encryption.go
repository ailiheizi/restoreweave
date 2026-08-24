package repository

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// KeyProvider is the host-owned credential boundary for encrypted repository
// profiles. Implementations resolve an opaque reference; the reference and
// resolved key are never written to portable records or plans.
type KeyProvider interface {
	ResolveKey(context.Context, string) ([]byte, error)
}

// KeyProviderFunc adapts a host callback without adding a secret store to the
// repository package.
type KeyProviderFunc func(context.Context, string) ([]byte, error)

func (f KeyProviderFunc) ResolveKey(ctx context.Context, ref string) ([]byte, error) {
	if f == nil {
		return nil, ErrKeyUnavailable
	}
	return f(ctx, ref)
}

var (
	ErrKeyUnavailable   = errors.New("repository encryption key unavailable")
	ErrInvalidKey       = errors.New("repository encryption key must be 32 bytes")
	ErrKeyMismatch      = errors.New("repository encryption key rejected encrypted object")
	ErrEncryptionConfig = errors.New("repository encryption configuration is invalid")
)

const (
	RepositoryProfileLocalZstdEncryptedV1 = "local-zstd-encrypted-v1"
	EncryptionProfileAES256GCMZstdV1      = "aes-256-gcm-zstd-v1"
	encryptionMetadataSchemaV1            = "restoreweave.repository-encryption.v1"
	encryptedBlobMagic                    = "RWZSTDENC1\x00"
)

type encryptionMetadata struct {
	Schema  string `json:"schema"`
	Profile string `json:"profile"`
	KeyRef  string `json:"key_ref"`
}

func validateKeyRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || len(ref) > 256 {
		return "", fmt.Errorf("%w: key reference is empty or too long", ErrEncryptionConfig)
	}
	for _, r := range ref {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", fmt.Errorf("%w: key reference contains whitespace or control characters", ErrEncryptionConfig)
		}
	}
	return ref, nil
}

func validateEncryptionKey(key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}
	return append([]byte(nil), key...), nil
}

func resolveEncryptionKey(ctx context.Context, provider KeyProvider, ref string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, ErrKeyUnavailable
	}
	key, err := provider.ResolveKey(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyUnavailable, err)
	}
	return validateEncryptionKey(key)
}

func encryptionMetadataPath(root string) string {
	return filepath.Join(root, repositoryEncryptionFile)
}

func readEncryptionMetadata(root, expectedProfile string) (encryptionMetadata, error) {
	var metadata encryptionMetadata
	path := encryptionMetadataPath(root)
	info, err := os.Lstat(path)
	if err != nil {
		return metadata, fmt.Errorf("read encryption metadata: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return metadata, fmt.Errorf("%w: encryption metadata is not a regular file", ErrEncryptionConfig)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return metadata, fmt.Errorf("read encryption metadata: %w", err)
	}
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return metadata, fmt.Errorf("%w: decode encryption metadata: %v", ErrEncryptionConfig, err)
	}
	if metadata.Schema != encryptionMetadataSchemaV1 || metadata.Profile != EncryptionProfileAES256GCMZstdV1 || expectedProfile != RepositoryProfileLocalZstdEncryptedV1 {
		return metadata, fmt.Errorf("%w: encryption metadata profile mismatch", ErrEncryptionConfig)
	}
	if _, err := validateKeyRef(metadata.KeyRef); err != nil {
		return metadata, err
	}
	return metadata, nil
}

func ensureEncryptionMetadata(root, keyRef string) error {
	validated, err := validateKeyRef(keyRef)
	if err != nil {
		return err
	}
	path := encryptionMetadataPath(root)
	if payload, readErr := os.ReadFile(path); readErr == nil {
		var metadata encryptionMetadata
		if err := json.Unmarshal(payload, &metadata); err != nil {
			return fmt.Errorf("%w: decode existing encryption metadata: %v", ErrEncryptionConfig, err)
		}
		if metadata.Schema != encryptionMetadataSchemaV1 || metadata.Profile != EncryptionProfileAES256GCMZstdV1 || metadata.KeyRef != validated {
			return fmt.Errorf("%w: existing key reference or profile differs", ErrEncryptionConfig)
		}
		return nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read existing encryption metadata: %w", readErr)
	}
	metadata := encryptionMetadata{Schema: encryptionMetadataSchemaV1, Profile: EncryptionProfileAES256GCMZstdV1, KeyRef: validated}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(root, "repository-encryption-*")
	if err != nil {
		return fmt.Errorf("create encryption metadata: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(append(payload, '\n')); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(tempName, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("publish encryption metadata: %w", err)
		}
		return ensureEncryptionMetadata(root, validated)
	}
	return syncFilesystemParentChain(root)
}

func rejectEncryptionMetadata(root string) error {
	path := encryptionMetadataPath(root)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect encryption metadata: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: unexpected encryption metadata is not regular", ErrEncryptionConfig)
	}
	return fmt.Errorf("%w: encrypted metadata belongs to a different repository profile", ErrEncryptionConfig)
}

func encryptedAAD(profile, keyRef string) []byte {
	return []byte(profile + "\x00" + keyRef)
}

func sealEncryptedZstd(ctx context.Context, tempName, root, profile, keyRef string, key []byte) (string, error) {
	compressed, err := os.ReadFile(tempName)
	if err != nil {
		return "", fmt.Errorf("read compressed payload for encryption: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key, err = validateEncryptionKey(key)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create encryption cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create encryption mode: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate encryption nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, compressed, encryptedAAD(profile, keyRef))
	protected, err := os.CreateTemp(filepath.Join(root, tmpDirName), "place-encrypted-*.zst")
	if err != nil {
		return "", fmt.Errorf("create encrypted payload tempfile: %w", err)
	}
	protectedName := protected.Name()
	cleanup := true
	defer func() {
		_ = protected.Close()
		if cleanup {
			_ = os.Remove(protectedName)
		}
	}()
	if _, err := protected.Write([]byte(encryptedBlobMagic)); err != nil {
		return "", err
	}
	if _, err := protected.Write(nonce); err != nil {
		return "", err
	}
	if _, err := protected.Write(ciphertext); err != nil {
		return "", err
	}
	if err := protected.Sync(); err != nil {
		return "", err
	}
	if err := protected.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(tempName); err != nil {
		return "", fmt.Errorf("remove unencrypted staging payload: %w", err)
	}
	cleanup = false
	return protectedName, nil
}

func decryptEncryptedZstd(ctx context.Context, file io.Reader, profile, keyRef string, key []byte) ([]byte, error) {
	payload, err := readAllContext(ctx, file)
	if err != nil {
		return nil, err
	}
	if len(payload) < len(encryptedBlobMagic) || string(payload[:len(encryptedBlobMagic)]) != encryptedBlobMagic {
		return nil, fmt.Errorf("%w: encrypted object header is invalid", ErrEncryptionConfig)
	}
	key, err = validateEncryptionKey(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create encryption cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create encryption mode: %w", err)
	}
	start := len(encryptedBlobMagic)
	end := start + gcm.NonceSize()
	if len(payload) < end+gcm.Overhead() {
		return nil, fmt.Errorf("%w: encrypted object is truncated", ErrKeyMismatch)
	}
	plaintext, err := gcm.Open(nil, payload[start:end], payload[end:], encryptedAAD(profile, keyRef))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyMismatch, err)
	}
	return plaintext, nil
}

func readAllContext(ctx context.Context, reader io.Reader) ([]byte, error) {
	return io.ReadAll(&contextReader{ctx: ctx, reader: reader})
}
