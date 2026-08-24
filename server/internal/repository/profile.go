package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// RepositoryProfileDirectoryCASDev is the uncompressed development CAS.
	RepositoryProfileDirectoryCASDev = "directory-cas-dev-v1"
	// RepositoryProfileLocalZstdV1 is the single-process, embedded zstd profile.
	RepositoryProfileLocalZstdV1 = "local-zstd-v1"
	CompressionProfileIdentity   = "identity-v1"
	CompressionProfileZstdV1     = "zstd-v1"
	repositoryProfileFile        = "repository.profile"
)

// DriverRecord is the narrow interface required by the control plane: payload
// placement plus portable recovery records.
type DriverRecord interface {
	Driver
	RecordDriver
}

// ProfileDescription is diagnostic metadata for a configured driver. It is
// deliberately outside Driver so third-party adapters do not gain a new
// storage requirement merely to support status output.
type ProfileDescription struct {
	Repository  string
	Compression string
	Encryption  string
}

type profileReporter interface {
	RepositoryProfile() ProfileDescription
}

var ErrReadOnly = errors.New("repository is open for recovery reads only")

// OpenProfileReadOnly opens an existing repository without creating paths,
// profile markers, identities, temporary files, or any other state. It is the
// only repository constructor used by the clean-install recovery reader.
func OpenProfileReadOnly(profile, path string) (DriverRecord, error) {
	return OpenProfileReadOnlyWithKeyProvider(profile, path, nil)
}

// OpenProfileReadOnlyWithKeyProvider is the clean-install reader constructor
// for profiles whose host-owned key dependency is explicitly available.
func OpenProfileReadOnlyWithKeyProvider(profile, path string, provider KeyProvider) (DriverRecord, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("repository path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("open existing repository: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("repository path is not a directory")
	}
	marker, err := os.ReadFile(filepath.Join(absolute, repositoryProfileFile))
	if err != nil {
		return nil, fmt.Errorf("read repository profile: %w", err)
	}
	if got := strings.TrimSpace(string(marker)); got != profile {
		return nil, fmt.Errorf("repository profile mismatch: found %q, want %q", got, profile)
	}
	identityBytes, err := os.ReadFile(filepath.Join(absolute, repositoryIdentityFile))
	if err != nil {
		return nil, fmt.Errorf("read repository identity: %w", err)
	}
	identity, err := validateRepositoryIdentity(string(identityBytes))
	if err != nil {
		return nil, err
	}
	base := &Dir{root: absolute, identity: identity}
	var driver DriverRecord
	switch profile {
	case RepositoryProfileDirectoryCASDev:
		driver = base
	case RepositoryProfileLocalZstdV1:
		driver = &ZstdDir{Dir: base}
	case RepositoryProfileLocalZstdEncryptedV1:
		metadata, metadataErr := readEncryptionMetadata(absolute, profile)
		if metadataErr != nil {
			return nil, metadataErr
		}
		key, keyErr := resolveEncryptionKey(context.Background(), provider, metadata.KeyRef)
		if keyErr != nil {
			return nil, keyErr
		}
		driver = &ZstdDir{Dir: base, encryption: &zstdEncryption{keyRef: metadata.KeyRef, key: key}}
	default:
		return nil, fmt.Errorf("unsupported repository profile %q", profile)
	}
	return &readOnlyDriver{DriverRecord: driver}, nil
}

// DetectProfileReadOnly reads the profile marker from an existing repository.
// It never creates or repairs repository state.
func DetectProfileReadOnly(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("repository path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	payload, err := os.ReadFile(filepath.Join(absolute, repositoryProfileFile))
	if err != nil {
		return "", fmt.Errorf("read repository profile: %w", err)
	}
	profile := strings.TrimSpace(string(payload))
	switch profile {
	case RepositoryProfileDirectoryCASDev, RepositoryProfileLocalZstdV1, RepositoryProfileLocalZstdEncryptedV1:
		return profile, nil
	default:
		return "", fmt.Errorf("unsupported repository profile %q", profile)
	}
}

type readOnlyDriver struct{ DriverRecord }

func (repo *readOnlyDriver) RepositoryProfile() ProfileDescription {
	return DescribeProfile(repo.DriverRecord)
}

func (*readOnlyDriver) Place(context.Context, io.Reader) (Receipt, error) {
	return Receipt{}, ErrReadOnly
}

func (*readOnlyDriver) PlaceExact(context.Context, string, io.Reader) (Receipt, error) {
	return Receipt{}, ErrReadOnly
}

func (*readOnlyDriver) Repair(context.Context, string, io.Reader) (Receipt, error) {
	return Receipt{}, ErrReadOnly
}

func (*readOnlyDriver) PlaceRecord(context.Context, RecordRole, io.Reader) (RecordReceipt, error) {
	return RecordReceipt{}, ErrReadOnly
}

var _ DriverRecord = (*Dir)(nil)

// DescribeProfile returns the configured repository tuple without exposing
// backend-private placement details.
func DescribeProfile(driver Driver) ProfileDescription {
	if reporter, ok := driver.(profileReporter); ok {
		return reporter.RepositoryProfile()
	}
	return ProfileDescription{Repository: "external", Compression: "backend-private"}
}

func (*Dir) RepositoryProfile() ProfileDescription {
	return ProfileDescription{
		Repository:  RepositoryProfileDirectoryCASDev,
		Compression: CompressionProfileIdentity,
		Encryption:  EncryptionNone,
	}
}

func (*Memory) RepositoryProfile() ProfileDescription {
	return ProfileDescription{Repository: "memory-test-v1", Compression: CompressionProfileIdentity, Encryption: EncryptionNone}
}

// OpenProfile opens one of the repository profiles owned by this package.
// The profile name fixes the compression tuple; callers cannot accidentally
// interpret a zstd repository as the raw development CAS.
func OpenProfile(profile, path string) (DriverRecord, error) {
	return OpenProfileWithKeyProvider(profile, path, nil)
}

// OpenProfileWithKeyProvider opens a profile with an explicitly injected host
// key provider. Encrypted repositories must already carry their non-secret
// key reference; new encrypted repositories should use OpenEncryptedZstdDir.
func OpenProfileWithKeyProvider(profile, path string, provider KeyProvider) (DriverRecord, error) {
	switch profile {
	case RepositoryProfileDirectoryCASDev:
		return OpenDir(path)
	case RepositoryProfileLocalZstdV1:
		return OpenZstdDir(path)
	case RepositoryProfileLocalZstdEncryptedV1:
		return OpenEncryptedZstdDir(path, "", provider)
	default:
		return nil, fmt.Errorf("unsupported repository profile %q", profile)
	}
}

// OpenProfileWithCompression validates the explicit configuration tuple before
// opening it. It is useful to config loaders that keep the two profile names
// as separate fields.
func OpenProfileWithCompression(repositoryProfile, compressionProfile, path string) (DriverRecord, error) {
	switch {
	case repositoryProfile == RepositoryProfileDirectoryCASDev && compressionProfile == CompressionProfileIdentity:
		return OpenProfile(repositoryProfile, path)
	case repositoryProfile == RepositoryProfileLocalZstdV1 && compressionProfile == CompressionProfileZstdV1:
		return OpenProfile(repositoryProfile, path)
	case repositoryProfile == RepositoryProfileLocalZstdEncryptedV1 && compressionProfile == CompressionProfileZstdV1:
		return OpenProfile(repositoryProfile, path)
	default:
		return nil, fmt.Errorf("unsupported repository/compression profile tuple %q/%q", repositoryProfile, compressionProfile)
	}
}

func ensureRepositoryProfile(root, expected string) error {
	path := filepath.Join(root, repositoryProfileFile)
	payload, err := os.ReadFile(path)
	if err == nil {
		got := strings.TrimSpace(string(payload))
		if got != expected {
			return fmt.Errorf("repository profile mismatch: found %q, want %q", got, expected)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read repository profile: %w", err)
	}
	if expected != RepositoryProfileDirectoryCASDev {
		hasLegacyState, err := repositoryHasUnmarkedState(root)
		if err != nil {
			return err
		}
		if hasLegacyState {
			// Another opener may have published the expected marker after our
			// first read and then created the repository identity. Re-read
			// before classifying the state as a legacy raw repository.
			if current, readErr := os.ReadFile(path); readErr == nil {
				got := strings.TrimSpace(string(current))
				if got != expected {
					return fmt.Errorf("repository profile mismatch: found %q, want %q", got, expected)
				}
				return nil
			} else if !errors.Is(readErr, os.ErrNotExist) {
				return fmt.Errorf("read repository profile: %w", readErr)
			}
			return fmt.Errorf(
				"repository profile marker is missing from a non-empty repository; open it as %q or migrate it explicitly",
				RepositoryProfileDirectoryCASDev,
			)
		}
	}
	temp, err := os.CreateTemp(root, "repository-profile-*")
	if err != nil {
		return fmt.Errorf("create repository profile: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.WriteString(expected + "\n"); err != nil {
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
			return fmt.Errorf("publish repository profile: %w", err)
		}
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		got := strings.TrimSpace(string(payload))
		if got != expected {
			return fmt.Errorf("repository profile mismatch: found %q, want %q", got, expected)
		}
		return nil
	}
	return syncFilesystemParentChain(root)
}

func repositoryHasUnmarkedState(root string) (bool, error) {
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative == tmpDirName {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Dir(relative) == "." &&
			(entry.Name() == repositoryProfileFile || strings.HasPrefix(entry.Name(), "repository-profile-")) {
			return nil
		}
		found = true
		return fs.SkipAll
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return false, fmt.Errorf("inspect unmarked repository: %w", err)
	}
	return found, nil
}
