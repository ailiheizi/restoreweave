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
	"time"

	"github.com/klauspost/compress/zstd"
)

// CapabilityProfile is the host-visible part of the RepositoryDriver
// capability contract. Backend-private object names and credentials are not
// included; unsupported features remain explicit instead of being inferred.
type CapabilityProfile struct {
	Driver             string   `json:"driver"`
	RepositoryFormat   string   `json:"repository_format"`
	Consistency        string   `json:"consistency"`
	Reader             string   `json:"reader"`
	ReaderDependencies []string `json:"reader_dependencies,omitempty"`
	Encryption         string   `json:"encryption"`
	Compression        string   `json:"compression"`
	Chunking           string   `json:"chunking"`
	SupportsWrite      bool     `json:"supports_write"`
	SupportsReadOnly   bool     `json:"supports_read_only"`
	SupportsRepair     bool     `json:"supports_repair"`
}

// HealthProfile makes unavailable and unverified states visible to callers.
// Capacity is advisory: a platform or filesystem that cannot measure it is
// reported as UNKNOWN rather than receiving synthesized numbers.
type HealthProfile struct {
	Available          bool   `json:"available"`
	ReaderReady        bool   `json:"reader_ready"`
	KeyState           string `json:"key_state"`
	CorruptionState    string `json:"corruption_state"`
	CapacityState      string `json:"capacity_state"`
	CapacityTotal      uint64 `json:"capacity_total,omitempty"`
	CapacityFree       uint64 `json:"capacity_free,omitempty"`
	CapacityUsed       uint64 `json:"capacity_used,omitempty"`
	CapacityMeasuredAt string `json:"capacity_measured_at,omitempty"`
	CapacityReason     string `json:"capacity_reason,omitempty"`
	LastVerified       string `json:"last_verified_boundary,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

// CapabilityReporter is optional on third-party drivers while the narrow
// Driver interface remains source-compatible. Core-owned drivers implement it
// so qualification and status code can fail closed on unavailable features.
type CapabilityReporter interface {
	DescribeCapabilities() CapabilityProfile
	DescribeHealthAndCapacity(context.Context) (HealthProfile, error)
}

type TargetValidation struct {
	Path               string             `json:"path"`
	Existing           bool               `json:"existing"`
	Compatible         bool               `json:"compatible"`
	RepositoryIdentity string             `json:"repository_identity,omitempty"`
	Profile            ProfileDescription `json:"profile"`
	Reason             string             `json:"reason,omitempty"`
}

type TargetValidator interface {
	ValidateTarget(context.Context) (TargetValidation, error)
}

type PlacementEstimate struct {
	ContentID                string `json:"content_id"`
	LogicalBytes             int64  `json:"logical_bytes"`
	ProbableNewPhysicalBytes int64  `json:"probable_new_physical_bytes"`
	TemporaryBytes           int64  `json:"temporary_bytes"`
	Existing                 bool   `json:"existing"`
	Supported                bool   `json:"supported"`
	Uncertainty              string `json:"uncertainty"`
}

type PlacementEstimator interface {
	EstimatePlacement(context.Context, string, io.Reader) (PlacementEstimate, error)
}

const (
	ConsistencySingleHost = "SINGLE_HOST"
	ReaderEmbedded        = "EMBEDDED"
	ReaderCleanInstall    = "CLEAN_INSTALL"
	EncryptionNone        = "NONE"
	EncryptionAES256GCM   = EncryptionProfileAES256GCMZstdV1
	CompressionIdentity   = "IDENTITY"
	ChunkingWholeFile     = "WHOLE_FILE"
	KeyStateNotRequired   = "NOT_REQUIRED"
	KeyStateAvailable     = "AVAILABLE"
	KeyStateMissing       = "MISSING"
	KeyStateInvalid       = "INVALID"
	KeyStateRejected      = "REJECTED"
	CorruptionNotChecked  = "NOT_CHECKED"
	CorruptionDetected    = "DETECTED"
	CapacityUnknown       = "UNKNOWN"
)

func (repo *Dir) DescribeCapabilities() CapabilityProfile {
	return CapabilityProfile{
		Driver:             "restoreweave.directory-cas",
		RepositoryFormat:   RepositoryProfileDirectoryCASDev,
		Consistency:        ConsistencySingleHost,
		Reader:             ReaderCleanInstall,
		ReaderDependencies: []string{"restoreweave.repository.directory-cas-dev-v1"},
		Encryption:         EncryptionNone,
		Compression:        CompressionIdentity,
		Chunking:           ChunkingWholeFile,
		SupportsWrite:      true,
		SupportsReadOnly:   true,
		SupportsRepair:     true,
	}
}

func (repo *ZstdDir) DescribeCapabilities() CapabilityProfile {
	capability := CapabilityProfile{
		Driver:             "restoreweave.local-zstd",
		RepositoryFormat:   RepositoryProfileLocalZstdV1,
		Consistency:        ConsistencySingleHost,
		Reader:             ReaderCleanInstall,
		ReaderDependencies: []string{"restoreweave.repository.local-zstd-v1", "klauspost/compress/zstd@v1.18.7"},
		Encryption:         EncryptionNone,
		Compression:        CompressionProfileZstdV1,
		Chunking:           ChunkingWholeFile,
		SupportsWrite:      true,
		SupportsReadOnly:   true,
		SupportsRepair:     true,
	}
	if repo != nil && repo.encryption != nil {
		capability.Driver = "restoreweave.local-zstd-encrypted"
		capability.RepositoryFormat = RepositoryProfileLocalZstdEncryptedV1
		capability.Encryption = EncryptionAES256GCM
		capability.ReaderDependencies = append(capability.ReaderDependencies, "host-key-provider:v1")
	}
	return capability
}

func (repo *Memory) DescribeCapabilities() CapabilityProfile {
	return CapabilityProfile{
		Driver:           "restoreweave.memory-test",
		RepositoryFormat: "memory-test-v1",
		Consistency:      "PROCESS_LOCAL",
		Reader:           ReaderEmbedded,
		Encryption:       EncryptionNone,
		Compression:      CompressionIdentity,
		Chunking:         ChunkingWholeFile,
		SupportsWrite:    true,
		SupportsReadOnly: false,
		SupportsRepair:   false,
	}
}

func (repo *Dir) DescribeHealthAndCapacity(ctx context.Context) (HealthProfile, error) {
	return describeDirectoryHealth(ctx, repo.root, RepositoryProfileDirectoryCASDev)
}

func (repo *ZstdDir) DescribeHealthAndCapacity(ctx context.Context) (HealthProfile, error) {
	profile := RepositoryProfileLocalZstdV1
	if repo != nil && repo.encryption != nil {
		profile = RepositoryProfileLocalZstdEncryptedV1
	}
	health, err := describeDirectoryHealth(ctx, repo.root, profile)
	if err != nil {
		return health, err
	}
	if profile == RepositoryProfileLocalZstdEncryptedV1 {
		health.KeyState = KeyStateAvailable
		payloads, listErr := listRepositoryPayloadIDs(repo.root)
		if listErr != nil {
			health.Available = false
			health.ReaderReady = false
			health.KeyState = KeyStateRejected
			health.Reason = listErr.Error()
			return health, nil
		}
		if len(payloads) > 0 {
			if verifyErr := repo.Verify(ctx, payloads[0]); verifyErr != nil {
				health.Available = false
				health.ReaderReady = false
				if errors.Is(verifyErr, ErrKeyMismatch) {
					health.KeyState = KeyStateRejected
				} else {
					health.CorruptionState = CorruptionDetected
				}
				health.Reason = verifyErr.Error()
			}
		}
	}
	return health, nil
}

func (repo *Memory) DescribeHealthAndCapacity(ctx context.Context) (HealthProfile, error) {
	if err := ctx.Err(); err != nil {
		return HealthProfile{}, err
	}
	return HealthProfile{
		Available:       true,
		ReaderReady:     true,
		KeyState:        KeyStateNotRequired,
		CorruptionState: CorruptionNotChecked,
		CapacityState:   CapacityUnknown,
	}, nil
}

func (repo *Dir) ValidateTarget(ctx context.Context) (TargetValidation, error) {
	return validateDirectoryTarget(ctx, repo.root, RepositoryProfileDirectoryCASDev)
}

func (repo *ZstdDir) ValidateTarget(ctx context.Context) (TargetValidation, error) {
	return validateDirectoryTarget(ctx, repo.root, RepositoryProfileLocalZstdV1)
}

func (repo *Memory) ValidateTarget(ctx context.Context) (TargetValidation, error) {
	if err := ctx.Err(); err != nil {
		return TargetValidation{}, err
	}
	return TargetValidation{Path: ":memory:", Existing: true, Compatible: true, Profile: DescribeProfile(repo)}, nil
}

func (repo *readOnlyDriver) ValidateTarget(ctx context.Context) (TargetValidation, error) {
	validator, ok := repo.DriverRecord.(TargetValidator)
	if !ok {
		return TargetValidation{}, errors.New("repository driver does not validate targets")
	}
	return validator.ValidateTarget(ctx)
}

func (repo *Dir) EstimatePlacement(ctx context.Context, contentID string, body io.Reader) (PlacementEstimate, error) {
	return estimatePlacement(ctx, repo, contentID, body, false, 0)
}

func (repo *ZstdDir) EstimatePlacement(ctx context.Context, contentID string, body io.Reader) (PlacementEstimate, error) {
	overhead := int64(0)
	if repo != nil && repo.encryption != nil {
		overhead = int64(len(encryptedBlobMagic) + 12 + 16)
	}
	return estimatePlacement(ctx, repo, contentID, body, true, overhead)
}

func (repo *Memory) EstimatePlacement(ctx context.Context, contentID string, body io.Reader) (PlacementEstimate, error) {
	return estimatePlacement(ctx, repo, contentID, body, false, 0)
}

func (repo *readOnlyDriver) EstimatePlacement(ctx context.Context, contentID string, body io.Reader) (PlacementEstimate, error) {
	estimator, ok := repo.DriverRecord.(PlacementEstimator)
	if !ok {
		return PlacementEstimate{}, errors.New("repository driver does not estimate placements")
	}
	return estimator.EstimatePlacement(ctx, contentID, body)
}

func estimatePlacement(ctx context.Context, repo Driver, expectedID string, body io.Reader, compress bool, physicalOverhead int64) (PlacementEstimate, error) {
	if err := ctx.Err(); err != nil {
		return PlacementEstimate{}, err
	}
	if body == nil {
		return PlacementEstimate{}, errors.New("placement estimate body is required")
	}
	hash := sha256.New()
	input := contextReader{ctx: ctx, reader: body}
	var logicalBytes int64
	var physicalBytes countingWriter
	if compress {
		encoder, err := zstd.NewWriter(&physicalBytes, zstd.WithEncoderCRC(true), zstd.WithEncoderConcurrency(1))
		if err != nil {
			return PlacementEstimate{}, err
		}
		logicalBytes, err = io.Copy(encoder, io.TeeReader(&input, hash))
		if closeErr := encoder.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return PlacementEstimate{}, fmt.Errorf("read placement estimate: %w", err)
		}
	} else if _, err := io.Copy(hash, &input); err != nil {
		return PlacementEstimate{}, fmt.Errorf("read placement estimate: %w", err)
	} else {
		logicalBytes = input.bytesRead
	}
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if expectedID != "" && expectedID != digest {
		return PlacementEstimate{}, fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, digest, expectedID)
	}
	estimate := PlacementEstimate{
		ContentID: digest, LogicalBytes: logicalBytes,
		TemporaryBytes: logicalBytes, Supported: true,
		Uncertainty: "EXACT_FOR_CURRENT_PROFILE",
	}
	if existing, err := repo.Open(ctx, digest); err == nil {
		_ = existing.Close()
		if err := repo.Verify(ctx, digest); err != nil {
			return PlacementEstimate{}, fmt.Errorf("verify existing placement: %w", err)
		}
		estimate.Existing = true
		estimate.ProbableNewPhysicalBytes = 0
		return estimate, nil
	} else if !errors.Is(err, ErrNotFound) {
		return PlacementEstimate{}, err
	}
	if !compress {
		estimate.ProbableNewPhysicalBytes = logicalBytes
		return estimate, nil
	}
	estimate.ProbableNewPhysicalBytes = physicalBytes.bytes + physicalOverhead
	estimate.TemporaryBytes += physicalBytes.bytes + physicalOverhead
	return estimate, nil
}

type countingWriter struct{ bytes int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.bytes += int64(len(p))
	return len(p), nil
}

type contextReader struct {
	ctx       context.Context
	reader    io.Reader
	bytesRead int64
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

func validateDirectoryTarget(ctx context.Context, root, profile string) (TargetValidation, error) {
	if err := ctx.Err(); err != nil {
		return TargetValidation{}, err
	}
	profileDescription := ProfileDescription{Repository: profile, Compression: CompressionIdentity, Encryption: EncryptionNone}
	if profile == RepositoryProfileLocalZstdV1 || profile == RepositoryProfileLocalZstdEncryptedV1 {
		profileDescription.Compression = CompressionProfileZstdV1
	}
	if profile == RepositoryProfileLocalZstdEncryptedV1 {
		profileDescription.Encryption = EncryptionAES256GCM
	}
	validation := TargetValidation{Path: filepath.Clean(root), Profile: profileDescription}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		validation.Compatible = true
		validation.Reason = "target does not exist; explicit initialization required"
		return validation, nil
	}
	if err != nil {
		return TargetValidation{}, err
	}
	validation.Existing = true
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		validation.Reason = "target is not a non-symlink directory"
		return validation, nil
	}
	markerPath := filepath.Join(root, repositoryProfileFile)
	markerInfo, markerErr := os.Lstat(markerPath)
	if markerErr != nil || markerInfo.Mode()&os.ModeSymlink != 0 || !markerInfo.Mode().IsRegular() {
		validation.Reason = "profile marker is unavailable or not a regular file"
		return validation, nil
	}
	marker, readErr := os.ReadFile(markerPath)
	if readErr != nil || strings.TrimSpace(string(marker)) != profile {
		validation.Reason = "profile marker is unavailable or mismatched"
		return validation, nil
	}
	if profile == RepositoryProfileLocalZstdEncryptedV1 {
		if _, metadataErr := readEncryptionMetadata(root, profile); metadataErr != nil {
			validation.Reason = metadataErr.Error()
			return validation, nil
		}
	} else if metadataErr := rejectEncryptionMetadata(root); metadataErr != nil {
		validation.Reason = metadataErr.Error()
		return validation, nil
	}
	identityPath := filepath.Join(root, repositoryIdentityFile)
	identityInfo, identityErr := os.Lstat(identityPath)
	if identityErr != nil || identityInfo.Mode()&os.ModeSymlink != 0 || !identityInfo.Mode().IsRegular() {
		validation.Reason = "repository identity is unavailable"
		return validation, nil
	}
	identity, readErr := os.ReadFile(identityPath)
	if readErr != nil {
		validation.Reason = "repository identity is unreadable"
		return validation, nil
	}
	validatedIdentity, identityErr := validateRepositoryIdentity(string(identity))
	if identityErr != nil {
		validation.Reason = "repository identity is invalid"
		return validation, nil
	}
	validation.RepositoryIdentity = validatedIdentity
	validation.Compatible = true
	return validation, nil
}

func (repo *readOnlyDriver) DescribeCapabilities() CapabilityProfile {
	reporter, ok := repo.DriverRecord.(CapabilityReporter)
	if !ok {
		return CapabilityProfile{Reader: ReaderCleanInstall, SupportsReadOnly: true}
	}
	profile := reporter.DescribeCapabilities()
	profile.SupportsWrite = false
	profile.SupportsRepair = false
	profile.SupportsReadOnly = true
	return profile
}

func (repo *readOnlyDriver) DescribeHealthAndCapacity(ctx context.Context) (HealthProfile, error) {
	reporter, ok := repo.DriverRecord.(CapabilityReporter)
	if !ok {
		return HealthProfile{}, errors.New("repository driver does not report health")
	}
	return reporter.DescribeHealthAndCapacity(ctx)
}

func describeDirectoryHealth(ctx context.Context, root, profile string) (HealthProfile, error) {
	if err := ctx.Err(); err != nil {
		return HealthProfile{}, err
	}
	health := HealthProfile{
		KeyState:        KeyStateNotRequired,
		CorruptionState: CorruptionNotChecked,
		CapacityState:   CapacityUnknown,
	}
	info, err := os.Lstat(root)
	if err != nil {
		health.Reason = err.Error()
		return health, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		health.Reason = "repository root is not a directory"
		return health, nil
	}
	markerPath := root + string(os.PathSeparator) + repositoryProfileFile
	markerInfo, err := os.Lstat(markerPath)
	if err != nil || markerInfo.Mode()&os.ModeSymlink != 0 || !markerInfo.Mode().IsRegular() {
		health.Reason = "repository profile marker is unavailable or not a regular file"
		return health, nil
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil || strings.TrimSpace(string(marker)) != profile {
		health.Reason = "repository profile marker is unavailable or mismatched"
		return health, nil
	}
	identityPath := root + string(os.PathSeparator) + repositoryIdentityFile
	identityInfo, err := os.Lstat(identityPath)
	if err != nil || identityInfo.Mode()&os.ModeSymlink != 0 || !identityInfo.Mode().IsRegular() {
		health.Reason = "repository identity is unavailable"
		return health, nil
	}
	health.Available = true
	health.ReaderReady = true
	if total, free, used, capacityErr := probeFilesystemCapacity(ctx, root); capacityErr == nil {
		health.CapacityState = CapacityAvailable
		health.CapacityTotal = total
		health.CapacityFree = free
		health.CapacityUsed = used
		health.CapacityMeasuredAt = time.Now().UTC().Format(time.RFC3339)
	} else {
		// Capacity is advisory. A platform or filesystem that cannot report it
		// must remain usable and must never receive guessed numbers.
		health.CapacityState = CapacityUnknown
		health.CapacityReason = capacityErr.Error()
	}
	return health, nil
}
