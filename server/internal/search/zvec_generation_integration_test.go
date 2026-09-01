//go:build purego && zvec_integration && (darwin || linux) && arm64

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

func TestZvecGenerationLifecycle(t *testing.T) {
	libraryPath := strings.TrimSpace(os.Getenv("RESTOREWEAVE_ZVEC_LIBRARY"))
	if libraryPath == "" {
		t.Skip("RESTOREWEAVE_ZVEC_LIBRARY is required for the opt-in native test")
	}
	manifest := testZvecManifest()
	payload, err := os.ReadFile(libraryPath)
	if err != nil {
		t.Fatalf("read native library: %v", err)
	}
	digest := sha256.Sum256(payload)
	spec := ZvecGenerationSpec{Path: filepath.Join(t.TempDir(), "generation"), LibraryPath: libraryPath, LibraryDigest: "sha256:" + hex.EncodeToString(digest[:]), Manifest: manifest, ProfileDigest: manifest.CanonicalDigest()}
	driver := NewZvecGenerationDriver(libraryPath)
	segments := []ZvecSegment{
		{SubjectID: "subject-a", SegmentID: "segment-a", Vector: []float32{1, 0, 0, 0}},
		{SubjectID: "subject-b", SegmentID: "segment-b", Vector: []float32{0, 1, 0, 0}},
	}
	receipt, err := driver.Build(context.Background(), spec, segments)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if receipt.SegmentCount != len(segments) || receipt.Dimension != manifest.Dimension {
		t.Fatalf("receipt = %+v", receipt)
	}
	if receipt.LibraryDigest != spec.LibraryDigest {
		t.Fatalf("receipt library digest = %q, want %q", receipt.LibraryDigest, spec.LibraryDigest)
	}
	pairs, err := driver.(interface {
		CoveragePairs(context.Context, ZvecGenerationSpec) ([]ZvecCoverageIdentity, error)
	}).CoveragePairs(context.Background(), spec)
	if err != nil {
		t.Fatalf("CoveragePairs() error = %v", err)
	}
	if len(pairs) != len(segments) {
		t.Fatalf("CoveragePairs() returned %d pairs, want %d", len(pairs), len(segments))
	}
	for i, pair := range pairs {
		if pair != (ZvecCoverageIdentity{SubjectID: segments[i].SubjectID, SegmentID: segments[i].SegmentID}) {
			t.Fatalf("CoveragePairs()[%d] = %+v, want %+v", i, pair, segments[i])
		}
	}
	verifier := driver.(interface {
		VerifyMembership(context.Context, ZvecGenerationSpec, []ZvecCoverageIdentity) error
	})
	if err := verifier.VerifyMembership(context.Background(), spec, pairs[:1]); err != nil {
		t.Fatalf("VerifyMembership() error = %v", err)
	}
	wrongSubject := []ZvecCoverageIdentity{{SubjectID: "wrong-subject", SegmentID: segments[0].SegmentID}}
	if err := verifier.VerifyMembership(context.Background(), spec, wrongSubject); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("wrong-subject VerifyMembership() error = %v, want ErrZvecUnavailable", err)
	}
	unknownSegment := []ZvecCoverageIdentity{{SubjectID: segments[0].SubjectID, SegmentID: "unknown-segment"}}
	if err := verifier.VerifyMembership(context.Background(), spec, unknownSegment); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("unknown-segment VerifyMembership() error = %v, want ErrZvecUnavailable", err)
	}
	second := spec
	second.Path = filepath.Join(t.TempDir(), "generation-second")
	if _, err := driver.Build(context.Background(), second, segments); err != nil {
		t.Fatalf("Build() with a second generation path error = %v", err)
	}
	opened, err := driver.Open(context.Background(), spec)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	hits, err := opened.Query(context.Background(), []float32{1, 0, 0, 0}, 1)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) != 1 || hits[0].SubjectID != "subject-a" || hits[0].SegmentID != "segment-a" {
		t.Fatalf("hits = %+v", hits)
	}
	if _, err := opened.Query(context.Background(), []float32{1, 0, 0}, 1); !errors.Is(err, ErrInvalidZvecGeneration) {
		t.Fatalf("wrong query dimension error = %v, want ErrInvalidZvecGeneration", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := opened.Query(context.Background(), []float32{1, 0, 0, 0}, 1); !errors.Is(err, ErrZvecGenerationClosed) {
		t.Fatalf("query after close error = %v, want ErrZvecGenerationClosed", err)
	}

	wrongDimension := spec
	wrongDimension.Manifest.Dimension = 5
	wrongDimension.Manifest.VectorSchema = "float32:5"
	wrongDimension.ProfileDigest = wrongDimension.Manifest.CanonicalDigest()
	if _, err := driver.Open(context.Background(), wrongDimension); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("wrong dimension error = %v, want ErrZvecUnavailable", err)
	}
	wrongProfile := spec
	wrongProfile.ProfileDigest = strings.Repeat("0", 72)
	if _, err := driver.Open(context.Background(), wrongProfile); !errors.Is(err, ErrInvalidZvecGeneration) {
		t.Fatalf("wrong profile error = %v, want ErrInvalidZvecGeneration", err)
	}
	wrongDigest := spec
	wrongDigest.LibraryDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := driver.Open(context.Background(), wrongDigest); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("wrong library digest error = %v, want ErrZvecUnavailable", err)
	}
	missing := spec
	missing.Path = filepath.Join(t.TempDir(), "missing")
	if _, err := driver.Open(context.Background(), missing); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("missing generation error = %v, want ErrZvecUnavailable", err)
	}
	wrongLibrary := spec
	wrongLibrary.LibraryPath = filepath.Join(t.TempDir(), "other-lib.dylib")
	if _, err := driver.Open(context.Background(), wrongLibrary); !errors.Is(err, ErrInvalidZvecGeneration) {
		t.Fatalf("wrong library path error = %v, want ErrInvalidZvecGeneration", err)
	}
	relocatedPath := filepath.Join(t.TempDir(), "relocated")
	if err := os.Rename(spec.Path, relocatedPath); err != nil {
		t.Fatalf("relocate generation: %v", err)
	}
	relocated := spec
	relocated.Path = relocatedPath
	if _, err := driver.Open(context.Background(), relocated); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("relocated path error = %v, want ErrZvecUnavailable", err)
	}
}

func TestZvecGenerationRejectsTamperedStagedLibrary(t *testing.T) {
	libraryPath := strings.TrimSpace(os.Getenv("RESTOREWEAVE_ZVEC_LIBRARY"))
	if libraryPath == "" {
		t.Skip("RESTOREWEAVE_ZVEC_LIBRARY is required for the opt-in native test")
	}
	payload, err := os.ReadFile(libraryPath)
	if err != nil {
		t.Fatalf("read native library: %v", err)
	}
	digest := sha256.Sum256(payload)
	spec := ZvecGenerationSpec{Path: filepath.Join(t.TempDir(), "generation"), LibraryPath: libraryPath, LibraryDigest: "sha256:" + hex.EncodeToString(digest[:]), Manifest: testZvecManifest()}
	spec.ProfileDigest = spec.Manifest.CanonicalDigest()
	staged := spec.Path + ".restoreweave-zvec-library"
	if err := os.MkdirAll(filepath.Dir(staged), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("tampered staged library"), 0o600); err != nil {
		t.Fatal(err)
	}
	driver := NewZvecGenerationDriver(libraryPath)
	if _, err := driver.Open(context.Background(), spec); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("tampered staged library error = %v, want ErrZvecUnavailable", err)
	}
}

func TestZvecGenerationRejectsAmbientFallbackWhenExplicitLibraryIsMissing(t *testing.T) {
	libraryPath := strings.TrimSpace(os.Getenv("RESTOREWEAVE_ZVEC_LIBRARY"))
	if libraryPath == "" {
		t.Skip("RESTOREWEAVE_ZVEC_LIBRARY is required for the opt-in native test")
	}
	payload, err := os.ReadFile(libraryPath)
	if err != nil {
		t.Fatalf("read native library: %v", err)
	}
	digest := sha256.Sum256(payload)
	ambient := t.TempDir()
	if err := os.WriteFile(filepath.Join(ambient, "libzvec_c_api.dylib"), payload, 0o500); err != nil {
		t.Fatalf("write ambient candidate: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(ambient); err != nil {
		t.Fatalf("change to ambient candidate directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	manifest := testZvecManifest()
	spec := ZvecGenerationSpec{
		Path:          filepath.Join(t.TempDir(), "missing-generation"),
		LibraryPath:   filepath.Join(t.TempDir(), "missing", "libzvec_c_api.dylib"),
		LibraryDigest: "sha256:" + hex.EncodeToString(digest[:]),
		Manifest:      manifest, ProfileDigest: manifest.CanonicalDigest(),
	}
	driver := NewZvecGenerationDriver(spec.LibraryPath)
	if _, err := driver.Open(context.Background(), spec); !errors.Is(err, ErrZvecUnavailable) {
		t.Fatalf("missing explicit library error = %v, want ErrZvecUnavailable", err)
	}
}
