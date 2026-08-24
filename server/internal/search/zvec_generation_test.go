package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testZvecManifest() EmbeddingGenerationManifest {
	return EmbeddingGenerationManifest{
		RuntimeDigest: "sha256:" + strings.Repeat("1", 64), ModelDigest: "sha256:" + strings.Repeat("2", 64),
		TokenizerDigest: "sha256:" + strings.Repeat("3", 64), PreprocessingDigest: "sha256:" + strings.Repeat("4", 64),
		Pooling: "cls", Normalization: "l2", ElementType: "float32", Dimension: 4, VectorSchema: "float32:4",
		SemanticSpace: "test-cosine", Distance: "cosine", IndexConfig: ZvecIndexConfigV1, QueryConfig: ZvecQueryConfigV1,
		ProviderDigest: "sha256:" + strings.Repeat("5", 64), ConfigDigest: "sha256:" + strings.Repeat("6", 64),
	}
}

func TestZvecGenerationUnavailableByDefault(t *testing.T) {
	manifest := testZvecManifest()
	spec := ZvecGenerationSpec{
		Path: filepath.Join(t.TempDir(), "generation"), LibraryPath: filepath.Join(t.TempDir(), "libzvec_c_api.dylib"),
		LibraryDigest: "sha256:" + strings.Repeat("0", 64),
		Manifest:      manifest, ProfileDigest: manifest.CanonicalDigest(),
	}
	driver := NewZvecGenerationDriver(spec.LibraryPath)
	_, err := driver.Build(context.Background(), spec, []ZvecSegment{{SubjectID: "subject", SegmentID: "segment", Vector: []float32{1, 0, 0, 0}}})
	if !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("Build error = %v, want ErrZvecUnavailable", err)
	}
}

func TestZvecGenerationRejectsUnboundManifest(t *testing.T) {
	manifest := testZvecManifest()
	spec := ZvecGenerationSpec{Path: filepath.Join(t.TempDir(), "generation"), LibraryPath: "/private/explicit/libzvec_c_api.dylib", Manifest: manifest, ProfileDigest: "wrong"}
	if err := validateZvecGenerationSpec(spec); !errors.Is(err, ErrInvalidZvecGeneration) {
		t.Fatalf("validation error = %v, want ErrInvalidZvecGeneration", err)
	}
}

func TestZvecGenerationStagesAndBindsLibraryDigest(t *testing.T) {
	root := t.TempDir()
	libraryPath := filepath.Join(root, "libzvec_c_api.dylib")
	payload := []byte("host-owned native library bytes")
	if err := os.WriteFile(libraryPath, payload, 0o500); err != nil {
		t.Fatalf("write library fixture: %v", err)
	}
	digest := sha256.Sum256(payload)
	manifest := testZvecManifest()
	spec := ZvecGenerationSpec{
		Path: filepath.Join(root, "generation"), LibraryPath: libraryPath,
		LibraryDigest: "sha256:" + hex.EncodeToString(digest[:]), Manifest: manifest,
		ProfileDigest: manifest.CanonicalDigest(),
	}
	if err := validateZvecGenerationSpec(spec); err != nil {
		t.Fatalf("validateZvecGenerationSpec() error = %v", err)
	}
	staged, err := stageZvecLibrary(spec)
	if err != nil {
		t.Fatalf("stageZvecLibrary() error = %v", err)
	}
	if staged != spec.Path+".restoreweave-zvec-library" {
		t.Fatalf("staged path = %q", staged)
	}
	info, err := os.Lstat(staged)
	if err != nil {
		t.Fatalf("stat staged library: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o400 {
		t.Fatalf("staged mode = %v, want regular 0400", info.Mode())
	}
	if got, err := zvecLibraryDigest(staged); err != nil || got != spec.LibraryDigest {
		t.Fatalf("staged digest = %q, err = %v, want %q", got, err, spec.LibraryDigest)
	}

	wrongDigest := spec
	wrongDigest.LibraryDigest = "sha256:" + strings.Repeat("f", 64)
	if err := validateZvecGenerationSpec(wrongDigest); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("wrong library digest error = %v, want ErrZvecUnavailable", err)
	}

	if err := os.Remove(libraryPath); err != nil {
		t.Fatalf("remove source library: %v", err)
	}
	if err := os.Symlink(staged, libraryPath); err != nil {
		t.Fatalf("replace source with symlink: %v", err)
	}
	if err := validateZvecGenerationSpec(spec); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("symlink replacement error = %v, want ErrZvecUnavailable", err)
	}
	if _, err := stageZvecLibrary(spec); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("stage after symlink replacement error = %v, want ErrZvecUnavailable", err)
	}
}

func TestZvecGenerationRejectsStagedLibraryReplacement(t *testing.T) {
	root := t.TempDir()
	libraryPath := filepath.Join(root, "libzvec_c_api.dylib")
	payload := []byte("fixed native library")
	if err := os.WriteFile(libraryPath, payload, 0o500); err != nil {
		t.Fatalf("write library fixture: %v", err)
	}
	digest := sha256.Sum256(payload)
	manifest := testZvecManifest()
	spec := ZvecGenerationSpec{
		Path: filepath.Join(root, "generation"), LibraryPath: libraryPath,
		LibraryDigest: "sha256:" + hex.EncodeToString(digest[:]), Manifest: manifest,
		ProfileDigest: manifest.CanonicalDigest(),
	}
	staged, err := stageZvecLibrary(spec)
	if err != nil {
		t.Fatalf("stageZvecLibrary() error = %v", err)
	}
	if err := os.Remove(staged); err != nil {
		t.Fatalf("remove staged library: %v", err)
	}
	if err := os.Symlink(libraryPath, staged); err != nil {
		t.Fatalf("replace staged library with symlink: %v", err)
	}
	if _, err := stageZvecLibrary(spec); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("staged symlink error = %v, want ErrZvecUnavailable", err)
	}
}
