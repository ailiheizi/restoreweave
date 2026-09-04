package releasequal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

func TestBuildAdmittedSemanticBundleArchiveRejectsMissingSourceWithoutOutput(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "bundle.tar.gz")
	_, err := BuildAdmittedSemanticBundleArchive(context.Background(), destination, filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("missing semantic bundle was accepted")
	}
	if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed build created output: %v", statErr)
	}
}

func TestBuildAdmittedSemanticBundleArchiveRealDarwinBundle(t *testing.T) {
	sourceRoot := os.Getenv("RESTOREWEAVE_REAL_SEMANTIC_BUNDLE")
	if sourceRoot == "" {
		t.Skip("RESTOREWEAVE_REAL_SEMANTIC_BUNDLE is not set; no live model lookup or download is permitted")
	}
	admission, err := search.LoadSemanticBundle(sourceRoot)
	if err != nil {
		t.Fatalf("load real admitted bundle: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "semantic-bundle.tar.gz")
	archive, err := BuildAdmittedSemanticBundleArchive(context.Background(), archivePath, sourceRoot)
	if err != nil {
		t.Fatalf("build real admitted archive: %v", err)
	}
	if archive.ProfileDigest != admission.ProfileDigest || archive.PlatformOS != "darwin" || archive.PlatformArch != "arm64" || archive.Size == 0 || archive.SHA256 == "" {
		t.Fatalf("archive identity = %+v, admission = %+v", archive, admission)
	}
	installed, err := search.InstallDefaultSemanticBundleFromArchive(context.Background(), t.TempDir(), archivePath)
	if err != nil {
		t.Fatalf("temporary offline install: %v", err)
	}
	if installed.ProfileDigest != admission.ProfileDigest {
		t.Fatalf("installed profile digest = %q, want %q", installed.ProfileDigest, admission.ProfileDigest)
	}
}
