package repository

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// relocateDriver opens the given profile at sourcePath, places a payload and
// a portable record, then moves the directory and returns the moved root.
func relocateDriver(t *testing.T, profile, sourcePath string) string {
	t.Helper()
	ctx := context.Background()
	driver, err := OpenProfile(profile, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("profile relocation payload "), 64)
	if _, err := driver.Place(ctx, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString(`{"profile":"relocation"}`)); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "relocated")
	if err := os.Rename(sourcePath, moved); err != nil {
		t.Fatal(err)
	}
	return moved
}

func TestOpenProfileReadOnlyAfterRelocationPreservesRawProfile(t *testing.T) {
	moved := relocateDriver(t, RepositoryProfileDirectoryCASDev, filepath.Join(t.TempDir(), "raw"))
	readonly, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, moved)
	if err != nil {
		t.Fatal(err)
	}
	if profile := DescribeProfile(readonly); profile.Repository != RepositoryProfileDirectoryCASDev || profile.Compression != CompressionProfileIdentity {
		t.Fatalf("relocated raw profile = %+v", profile)
	}
	if _, err := OpenProfileReadOnly(RepositoryProfileLocalZstdV1, moved); err == nil {
		t.Fatal("zstd read-only profile opened a relocated raw repository")
	}
}

func TestOpenProfileReadOnlyAfterRelocationPreservesZstdProfile(t *testing.T) {
	moved := relocateDriver(t, RepositoryProfileLocalZstdV1, filepath.Join(t.TempDir(), "zstd"))
	readonly, err := OpenProfileReadOnly(RepositoryProfileLocalZstdV1, moved)
	if err != nil {
		t.Fatal(err)
	}
	if profile := DescribeProfile(readonly); profile.Repository != RepositoryProfileLocalZstdV1 || profile.Compression != CompressionProfileZstdV1 {
		t.Fatalf("relocated zstd profile = %+v", profile)
	}
	if _, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, moved); err == nil {
		t.Fatal("raw read-only profile opened a relocated zstd repository")
	}
}
