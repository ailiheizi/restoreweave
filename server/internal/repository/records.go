package repository

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	RecordPreparedClosure         RecordRole = "PREPARED_CLOSURE"
	RecordPublicationCommit       RecordRole = "PUBLICATION_COMMIT"
	RecordProcessorAttemptClosure RecordRole = "PROCESSOR_ATTEMPT_CLOSURE"
	RecordPortableFactClosure     RecordRole = "PORTABLE_FACT_CLOSURE"

	recoveryDirName          = "recovery"
	repositoryIdentityFile   = "repository.identity"
	repositoryEncryptionFile = "repository.encryption"
	maxRecordBytes           = int64(16 << 20)
)

// RecordRole distinguishes portable recovery metadata from payload objects.
// Both roles remain in one product-level repository placement.
type RecordRole string

// RecordReceipt is host-verifiable evidence for an immutable portable record.
// Digest addresses the exact bytes; it is never a database or backend-private
// object identifier.
type RecordReceipt struct {
	RepositoryID string     `json:"repository_id"`
	Role         RecordRole `json:"role"`
	Digest       string     `json:"digest"`
	Bytes        int64      `json:"bytes"`
	Existed      bool       `json:"existed"`
}

// RecordDriver is the narrow portable-publication extension implemented by a
// repository profile. Discovery enumerates commit candidates by role and then
// authenticates their bytes; listing alone never establishes publication.
type RecordDriver interface {
	RepositoryIdentity() string
	PlaceRecord(ctx context.Context, role RecordRole, body io.Reader) (RecordReceipt, error)
	OpenRecord(ctx context.Context, role RecordRole, digest string) (io.ReadCloser, error)
	VerifyRecord(ctx context.Context, receipt RecordReceipt) error
	ListRecordDigests(ctx context.Context, role RecordRole) ([]string, error)
}

func (repo *Dir) RepositoryIdentity() string { return repo.identity }

func (repo *Dir) PlaceRecord(ctx context.Context, role RecordRole, body io.Reader) (RecordReceipt, error) {
	if err := ctx.Err(); err != nil {
		return RecordReceipt{}, err
	}
	if err := validateRecordRole(role); err != nil {
		return RecordReceipt{}, err
	}
	if body == nil {
		return RecordReceipt{}, errors.New("record placement body is required")
	}
	temp, err := os.CreateTemp(filepath.Join(repo.root, tmpDirName), "record-*.json")
	if err != nil {
		return RecordReceipt{}, fmt.Errorf("create record placement tempfile: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, digest), io.LimitReader(body, maxRecordBytes+1))
	if err != nil {
		return RecordReceipt{}, fmt.Errorf("write record placement: %w", err)
	}
	if written > maxRecordBytes {
		return RecordReceipt{}, fmt.Errorf("portable record exceeds %d bytes", maxRecordBytes)
	}
	if err := temp.Sync(); err != nil {
		return RecordReceipt{}, fmt.Errorf("sync record placement: %w", err)
	}
	if err := temp.Close(); err != nil {
		return RecordReceipt{}, fmt.Errorf("close record placement: %w", err)
	}
	recordDigest := AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	dest, err := repo.recordPath(role, recordDigest)
	if err != nil {
		return RecordReceipt{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return RecordReceipt{}, fmt.Errorf("create record directory: %w", err)
	}
	receipt := RecordReceipt{RepositoryID: repo.identity, Role: role, Digest: recordDigest, Bytes: written}
	if _, err := os.Lstat(dest); err == nil {
		receipt.Existed = true
		if err := repo.VerifyRecord(ctx, receipt); err != nil {
			return RecordReceipt{}, err
		}
		return receipt, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return RecordReceipt{}, err
	}
	if err := publishNoReplace(tempName, dest, repo.root); err != nil {
		if errors.Is(err, os.ErrExist) {
			receipt.Existed = true
			if verifyErr := repo.VerifyRecord(ctx, receipt); verifyErr != nil {
				return RecordReceipt{}, verifyErr
			}
			return receipt, nil
		}
		return RecordReceipt{}, fmt.Errorf("commit portable record: %w", err)
	}
	if err := repo.VerifyRecord(ctx, receipt); err != nil {
		return RecordReceipt{}, err
	}
	return receipt, nil
}

func (repo *Dir) OpenRecord(ctx context.Context, role RecordRole, digest string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := repo.recordPath(role, digest)
	if err != nil {
		return nil, err
	}
	file, err := openRepositoryFile(repo.root, path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, role, digest)
	}
	return file, err
}

func (repo *Dir) VerifyRecord(ctx context.Context, receipt RecordReceipt) error {
	if receipt.RepositoryID != repo.identity {
		return fmt.Errorf("record repository identity mismatch: got %q want %q", receipt.RepositoryID, repo.identity)
	}
	body, err := repo.OpenRecord(ctx, receipt.Role, receipt.Digest)
	if err != nil {
		return err
	}
	defer body.Close()
	digest := sha256.New()
	read, err := io.Copy(digest, body)
	if err != nil {
		return err
	}
	if read != receipt.Bytes {
		return fmt.Errorf("record length mismatch: got %d want %d", read, receipt.Bytes)
	}
	got := AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	if got != receipt.Digest {
		return fmt.Errorf("%w: got %s want %s", ErrDigestMismatch, got, receipt.Digest)
	}
	return nil
}

func (repo *Dir) ListRecordDigests(ctx context.Context, role RecordRole) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRecordRole(role); err != nil {
		return nil, err
	}
	root := filepath.Join(repo.root, recoveryDirName, recordRoleDir(role), AlgorithmSHA256)
	var digests []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && path == root {
				return fs.SkipDir
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("portable record %q is not a regular file", path)
		}
		if len(name) != 64 {
			return fmt.Errorf("portable record %q has invalid digest name", path)
		}
		candidate := AlgorithmSHA256 + ":" + name
		if _, err := parseContentID(candidate); err != nil {
			return fmt.Errorf("portable record %q has invalid digest: %w", path, err)
		}
		if prefix := filepath.Base(filepath.Dir(path)); prefix != name[:hexPrefixLen] {
			return fmt.Errorf("portable record %q is in prefix directory %q, want %q", path, prefix, name[:hexPrefixLen])
		}
		digests = append(digests, candidate)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(digests)
	return digests, nil
}

func (repo *Dir) recordPath(role RecordRole, digest string) (string, error) {
	hexDigest, err := parseContentID(digest)
	if err != nil {
		return "", err
	}
	if err := validateRecordRole(role); err != nil {
		return "", err
	}
	return filepath.Join(repo.root, recoveryDirName, recordRoleDir(role), AlgorithmSHA256, hexDigest[:hexPrefixLen], hexDigest), nil
}

func (repo *Memory) RepositoryIdentity() string { return repo.identity }

func (repo *Memory) PlaceRecord(ctx context.Context, role RecordRole, body io.Reader) (RecordReceipt, error) {
	if err := ctx.Err(); err != nil {
		return RecordReceipt{}, err
	}
	if err := validateRecordRole(role); err != nil {
		return RecordReceipt{}, err
	}
	payload, err := io.ReadAll(io.LimitReader(body, maxRecordBytes+1))
	if err != nil {
		return RecordReceipt{}, err
	}
	if int64(len(payload)) > maxRecordBytes {
		return RecordReceipt{}, fmt.Errorf("portable record exceeds %d bytes", maxRecordBytes)
	}
	sum := sha256.Sum256(payload)
	digest := AlgorithmSHA256 + ":" + hex.EncodeToString(sum[:])
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.records == nil {
		repo.records = make(map[RecordRole]map[string][]byte)
	}
	if repo.records[role] == nil {
		repo.records[role] = make(map[string][]byte)
	}
	_, existed := repo.records[role][digest]
	if !existed {
		repo.records[role][digest] = append([]byte(nil), payload...)
	}
	return RecordReceipt{RepositoryID: repo.identity, Role: role, Digest: digest, Bytes: int64(len(payload)), Existed: existed}, nil
}

func (repo *Memory) OpenRecord(ctx context.Context, role RecordRole, digest string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRecordRole(role); err != nil {
		return nil, err
	}
	if _, err := parseContentID(digest); err != nil {
		return nil, err
	}
	repo.mu.Lock()
	payload, ok := repo.records[role][digest]
	repo.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, role, digest)
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), payload...))), nil
}

func (repo *Memory) VerifyRecord(ctx context.Context, receipt RecordReceipt) error {
	if receipt.RepositoryID != repo.identity {
		return fmt.Errorf("record repository identity mismatch: got %q want %q", receipt.RepositoryID, repo.identity)
	}
	body, err := repo.OpenRecord(ctx, receipt.Role, receipt.Digest)
	if err != nil {
		return err
	}
	defer body.Close()
	payload, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(payload)) != receipt.Bytes {
		return fmt.Errorf("record length mismatch: got %d want %d", len(payload), receipt.Bytes)
	}
	sum := sha256.Sum256(payload)
	got := AlgorithmSHA256 + ":" + hex.EncodeToString(sum[:])
	if got != receipt.Digest {
		return fmt.Errorf("%w: got %s want %s", ErrDigestMismatch, got, receipt.Digest)
	}
	return nil
}

func (repo *Memory) ListRecordDigests(ctx context.Context, role RecordRole) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRecordRole(role); err != nil {
		return nil, err
	}
	repo.mu.Lock()
	digests := make([]string, 0, len(repo.records[role]))
	for digest := range repo.records[role] {
		digests = append(digests, digest)
	}
	repo.mu.Unlock()
	sort.Strings(digests)
	return digests, nil
}

func validateRecordRole(role RecordRole) error {
	switch role {
	case RecordPreparedClosure, RecordPublicationCommit, RecordProcessorAttemptClosure, RecordPortableFactClosure:
		return nil
	default:
		return fmt.Errorf("unsupported portable record role %q", role)
	}
}

func recordRoleDir(role RecordRole) string {
	if role == RecordPreparedClosure {
		return "prepared"
	}
	if role == RecordProcessorAttemptClosure {
		return "processor-attempts"
	}
	if role == RecordPortableFactClosure {
		return "portable-facts"
	}
	return "commits"
}

func loadOrCreateRepositoryIdentity(root string) (string, error) {
	path := filepath.Join(root, repositoryIdentityFile)
	if payload, err := os.ReadFile(path); err == nil {
		return validateRepositoryIdentity(string(payload))
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read repository identity: %w", err)
	}
	identity := newInMemoryRepositoryIdentity()
	temp, err := os.CreateTemp(root, "repository-identity-*")
	if err != nil {
		return "", err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := temp.WriteString(identity + "\n"); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Link(tempName, path); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("publish repository identity: %w", err)
		}
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		identity, validateErr := validateRepositoryIdentity(string(payload))
		if validateErr != nil {
			return "", validateErr
		}
		if syncErr := syncFilesystemParentChain(root); syncErr != nil {
			return "", fmt.Errorf("sync repository identity: %w", syncErr)
		}
		return identity, nil
	}
	if err := syncFilesystemParentChain(root); err != nil {
		return "", fmt.Errorf("sync repository identity: %w", err)
	}
	return identity, nil
}

func newInMemoryRepositoryIdentity() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate repository identity: %v", err))
	}
	return "repo:" + hex.EncodeToString(value[:])
}

func validateRepositoryIdentity(value string) (string, error) {
	value = strings.TrimSpace(value)
	payload, ok := strings.CutPrefix(value, "repo:")
	if !ok || len(payload) != 32 {
		return "", fmt.Errorf("invalid repository identity %q", value)
	}
	if _, err := hex.DecodeString(payload); err != nil || payload != strings.ToLower(payload) {
		return "", fmt.Errorf("invalid repository identity %q", value)
	}
	return value, nil
}
