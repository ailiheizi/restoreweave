package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrZvecUnavailable is returned when the real semantic index cannot be used.
// Exact and lexical search remain usable in this state.
var ErrZvecUnavailable = errors.New("zvec semantic index unavailable")

var (
	ErrInvalidZvecGeneration = errors.New("invalid zvec generation")
	ErrZvecGenerationClosed  = errors.New("zvec generation is closed")
)

const (
	ZvecEmbeddingField = "embedding"
	ZvecSubjectField   = "subject_id"
	ZvecSegmentField   = "segment_id"
	// These values match the pinned local BGE semantic profile. They are part
	// of the manifest digest and are rejected when a caller selects another
	// native index/query configuration.
	ZvecIndexConfigV1  = "hnsw:m=16"
	ZvecQueryConfigV1  = "ef=64"
	zvecGenerationMeta = "restoreweave-zvec-generation.json"
)

// ZvecGenerationSpec identifies one immutable, disposable semantic generation.
// Path and LibraryPath are explicit resolved paths; neither may be inferred
// from the process working directory or a bundled library search path.
type ZvecGenerationSpec struct {
	Path          string
	LibraryPath   string
	LibraryDigest string
	ProfileDigest string
	Manifest      EmbeddingGenerationManifest
}

// ZvecSegment is one durable semantic segment projection. SubjectID and
// SegmentID are returned as payload, never inferred from a zvec row ID.
type ZvecSegment struct {
	SubjectID string
	SegmentID string
	Vector    []float32
}

type ZvecGenerationReceipt struct {
	Path          string
	LibraryDigest string
	ProfileDigest string
	Dimension     int
	SegmentCount  int
}

type ZvecHit struct {
	SubjectID string
	SegmentID string
	Score     float32
}

// ZvecGeneration is a read-only opened generation.
type ZvecGeneration interface {
	Query(ctx context.Context, vector []float32, topK int) ([]ZvecHit, error)
	Close() error
}

// ZvecGenerationDriver is the narrow replaceable boundary for semantic index
// generations. The host owns publication and generation metadata.
type ZvecGenerationDriver interface {
	Build(ctx context.Context, spec ZvecGenerationSpec, segments []ZvecSegment) (ZvecGenerationReceipt, error)
	Open(ctx context.Context, spec ZvecGenerationSpec) (ZvecGeneration, error)
}

// ZvecGenerationReadiness is an optional health boundary for a real semantic
// backend. The base driver stays replaceable, but a backend must provide an
// explicit runtime probe before the host advertises semantic capability as
// real. Missing readiness evidence is treated as unavailable by Indexer.
type ZvecGenerationReadiness interface {
	ZvecReady(libraryPath, libraryDigest string, manifest EmbeddingGenerationManifest) bool
}

func NewZvecGenerationDriver(libraryPath string) ZvecGenerationDriver {
	return newZvecGenerationBackend(libraryPath)
}

type zvecGenerationMetadata struct {
	Schema        string                      `json:"schema"`
	Path          string                      `json:"path"`
	LibraryDigest string                      `json:"library_digest"`
	ProfileDigest string                      `json:"profile_digest"`
	Manifest      EmbeddingGenerationManifest `json:"manifest"`
	SegmentIDs    []string                    `json:"segment_ids,omitempty"`
}

const zvecGenerationMetadataSchema = "restoreweave.zvec-generation.v1"

func validateZvecGenerationSpec(spec ZvecGenerationSpec) error {
	if err := spec.Manifest.Validate(); err != nil {
		return fmt.Errorf("%w: manifest: %v", ErrInvalidZvecGeneration, err)
	}
	if strings.TrimSpace(spec.ProfileDigest) == "" || spec.ProfileDigest != spec.Manifest.CanonicalDigest() {
		return fmt.Errorf("%w: profile digest does not match manifest", ErrInvalidZvecGeneration)
	}
	if spec.Manifest.ElementType != "float32" || spec.Manifest.VectorSchema != fmt.Sprintf("float32:%d", spec.Manifest.Dimension) {
		return fmt.Errorf("%w: unsupported vector schema", ErrInvalidZvecGeneration)
	}
	if spec.Manifest.Distance != "cosine" || spec.Manifest.IndexConfig != ZvecIndexConfigV1 || spec.Manifest.QueryConfig != ZvecQueryConfigV1 {
		return fmt.Errorf("%w: zvec profile does not bind the selected index/query configuration", ErrInvalidZvecGeneration)
	}
	if spec.Path == "" || !filepath.IsAbs(spec.Path) || filepath.Clean(spec.Path) != spec.Path || spec.Path == string(filepath.Separator) {
		return fmt.Errorf("%w: generation path must be an absolute clean non-root path", ErrInvalidZvecGeneration)
	}
	if spec.LibraryPath == "" || !filepath.IsAbs(spec.LibraryPath) || filepath.Clean(spec.LibraryPath) != spec.LibraryPath {
		return fmt.Errorf("%w: native library path must be an absolute clean path", ErrInvalidZvecGeneration)
	}
	if err := validateZvecLibraryDigest(spec.LibraryDigest); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidZvecGeneration, err)
	}
	actual, err := zvecLibraryDigest(spec.LibraryPath)
	if err != nil {
		return fmt.Errorf("%w: native library: %v", ErrZvecUnavailable, err)
	}
	if actual != spec.LibraryDigest {
		return fmt.Errorf("%w: native library digest %s does not match expected %s", ErrZvecUnavailable, actual, spec.LibraryDigest)
	}
	return nil
}

func validateZvecLibraryDigest(value string) error {
	if !strings.HasPrefix(value, "sha256:") {
		return errors.New("native library digest must use sha256:<lowercase-hex>")
	}
	if err := validateSHA256(strings.TrimPrefix(value, "sha256:")); err != nil {
		return fmt.Errorf("native library digest: %v", err)
	}
	return nil
}

func zvecLibraryDigest(path string) (string, error) {
	file, err := openZvecLibraryNoFollow(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("native library must be a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// stageZvecLibrary creates a host-owned immutable copy beside the generation.
// The sibling path is deterministic so a clean reader can reconstruct it from
// the explicit generation path without consulting cwd or a bundled search path.
func stageZvecLibrary(spec ZvecGenerationSpec) (string, error) {
	if err := validateZvecLibraryDigest(spec.LibraryDigest); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidZvecGeneration, err)
	}
	if spec.Path == "" || !filepath.IsAbs(spec.Path) || filepath.Clean(spec.Path) != spec.Path {
		return "", fmt.Errorf("%w: generation path is not canonical", ErrInvalidZvecGeneration)
	}
	staged := spec.Path + ".restoreweave-zvec-library"
	if staged == spec.LibraryPath {
		return "", fmt.Errorf("%w: library path cannot be its own staging path", ErrInvalidZvecGeneration)
	}
	actual, err := zvecLibraryDigest(spec.LibraryPath)
	if err != nil {
		return "", fmt.Errorf("%w: source library: %v", ErrZvecUnavailable, err)
	}
	if actual != spec.LibraryDigest {
		return "", fmt.Errorf("%w: source library digest %s does not match expected %s", ErrZvecUnavailable, actual, spec.LibraryDigest)
	}

	if info, statErr := os.Lstat(staged); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
			return "", fmt.Errorf("%w: staged library is not a host-owned regular file", ErrZvecUnavailable)
		}
		stagedDigest, digestErr := zvecLibraryDigest(staged)
		if digestErr != nil {
			return "", fmt.Errorf("%w: staged library: %v", ErrZvecUnavailable, digestErr)
		}
		if stagedDigest != spec.LibraryDigest {
			return "", fmt.Errorf("%w: staged library digest %s does not match expected %s", ErrZvecUnavailable, stagedDigest, spec.LibraryDigest)
		}
		return staged, nil
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("%w: inspect staged library: %v", ErrZvecUnavailable, statErr)
	}

	parent := filepath.Dir(staged)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return "", fmt.Errorf("%w: staging parent: %v", ErrZvecUnavailable, err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() || parentInfo.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("%w: staging parent is not host-owned", ErrZvecUnavailable)
	}

	source, err := openZvecLibraryNoFollow(spec.LibraryPath)
	if err != nil {
		return "", fmt.Errorf("%w: reopen source library: %v", ErrZvecUnavailable, err)
	}
	defer source.Close()
	tmp, err := os.CreateTemp(parent, ".restoreweave-zvec-library-*")
	if err != nil {
		return "", fmt.Errorf("%w: create staged temporary file: %v", ErrZvecUnavailable, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o400); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("%w: protect staged temporary file: %v", ErrZvecUnavailable, err)
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), source); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("%w: stage native library: %v", ErrZvecUnavailable, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("%w: sync staged native library: %v", ErrZvecUnavailable, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("%w: close staged native library: %v", ErrZvecUnavailable, err)
	}
	stagedDigest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if stagedDigest != spec.LibraryDigest {
		return "", fmt.Errorf("%w: source changed while staging: got %s want %s", ErrZvecUnavailable, stagedDigest, spec.LibraryDigest)
	}
	if err := os.Link(tmpName, staged); err != nil {
		if !os.IsExist(err) {
			return "", fmt.Errorf("%w: publish staged native library: %v", ErrZvecUnavailable, err)
		}
		info, statErr := os.Lstat(staged)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
			return "", fmt.Errorf("%w: concurrent staged library is not host-owned", ErrZvecUnavailable)
		}
		stagedDigest, digestErr := zvecLibraryDigest(staged)
		if digestErr != nil || stagedDigest != spec.LibraryDigest {
			return "", fmt.Errorf("%w: concurrent staged library digest mismatch", ErrZvecUnavailable)
		}
	}
	return staged, nil
}

func validateZvecSegments(segments []ZvecSegment, dimension int) error {
	if len(segments) == 0 {
		return fmt.Errorf("%w: at least one segment is required", ErrInvalidZvecGeneration)
	}
	seen := make(map[string]struct{}, len(segments))
	for _, segment := range segments {
		if strings.TrimSpace(segment.SubjectID) == "" || strings.TrimSpace(segment.SegmentID) == "" {
			return fmt.Errorf("%w: segment subject and segment IDs are required", ErrInvalidZvecGeneration)
		}
		if _, ok := seen[segment.SegmentID]; ok {
			return fmt.Errorf("%w: duplicate segment ID %q", ErrInvalidZvecGeneration, segment.SegmentID)
		}
		seen[segment.SegmentID] = struct{}{}
		if len(segment.Vector) != dimension {
			return fmt.Errorf("%w: segment %q has dimension %d, want %d", ErrInvalidZvecGeneration, segment.SegmentID, len(segment.Vector), dimension)
		}
	}
	return nil
}

func writeZvecGenerationMetadata(path string, metadata zvecGenerationMetadata) error {
	if metadata.Schema != zvecGenerationMetadataSchema || validateZvecLibraryDigest(metadata.LibraryDigest) != nil {
		return fmt.Errorf("%w: metadata schema", ErrInvalidZvecGeneration)
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	tmp := filepath.Join(path, "."+zvecGenerationMeta+".tmp")
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(path, zvecGenerationMeta)); err != nil {
		return err
	}
	return nil
}

func readZvecGenerationMetadata(path string) (zvecGenerationMetadata, error) {
	payload, err := os.ReadFile(filepath.Join(path, zvecGenerationMeta))
	if err != nil {
		return zvecGenerationMetadata{}, fmt.Errorf("%w: generation metadata: %v", ErrZvecUnavailable, err)
	}
	var metadata zvecGenerationMetadata
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return zvecGenerationMetadata{}, fmt.Errorf("%w: generation metadata: %v", ErrZvecUnavailable, err)
	}
	if metadata.Schema != zvecGenerationMetadataSchema || metadata.Path == "" || metadata.ProfileDigest != metadata.Manifest.CanonicalDigest() || validateZvecLibraryDigest(metadata.LibraryDigest) != nil {
		return zvecGenerationMetadata{}, fmt.Errorf("%w: generation metadata binding is invalid", ErrZvecUnavailable)
	}
	seen := make(map[string]struct{}, len(metadata.SegmentIDs))
	normalizedIDs := make([]string, 0, len(metadata.SegmentIDs))
	for _, id := range metadata.SegmentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return zvecGenerationMetadata{}, fmt.Errorf("%w: generation metadata contains an empty segment id", ErrZvecUnavailable)
		}
		if _, ok := seen[id]; ok {
			return zvecGenerationMetadata{}, fmt.Errorf("%w: generation metadata contains duplicate segment id %q", ErrZvecUnavailable, id)
		}
		seen[id] = struct{}{}
		normalizedIDs = append(normalizedIDs, id)
	}
	metadata.SegmentIDs = normalizedIDs
	return metadata, nil
}
