package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenProfileReadOnlyDoesNotCreateRepositoryState(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, missing); err == nil {
		t.Fatal("missing repository opened successfully")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing repository was created: %v", err)
	}

	root := t.TempDir()
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, root); err == nil {
		t.Fatal("unmarked repository opened successfully")
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("read-only open changed directory entries: before=%d after=%d", len(before), len(after))
	}

	if err := os.WriteFile(filepath.Join(root, repositoryProfileFile), []byte(RepositoryProfileDirectoryCASDev+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, root); err == nil {
		t.Fatal("repository without identity opened successfully")
	}
	if _, err := os.Stat(filepath.Join(root, repositoryIdentityFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only open created repository identity: %v", err)
	}
}

func TestOpenProfileReadOnlyReadsRelocatedRawRepository(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "raw")
	writable, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("portable recovery payload")
	receipt, err := writable.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	record, err := writable.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString("commit"))
	if err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(t.TempDir(), "relocated")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	readonly, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, moved)
	if err != nil {
		t.Fatal(err)
	}
	if readonly.Root() != moved {
		t.Fatalf("repository root = %q, want %q", readonly.Root(), moved)
	}
	if readonly.RepositoryIdentity() != writable.RepositoryIdentity() {
		t.Fatalf("repository identity changed after relocation: %q != %q", readonly.RepositoryIdentity(), writable.RepositoryIdentity())
	}
	if err := readonly.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatal(err)
	}
	body, err := readonly.Open(ctx, receipt.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("read-only payload = %q, read=%v close=%v", got, readErr, closeErr)
	}
	if err := readonly.VerifyRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	digests, err := readonly.ListRecordDigests(ctx, RecordPublicationCommit)
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 1 || digests[0] != record.Digest {
		t.Fatalf("record digests = %v, want [%s]", digests, record.Digest)
	}
}

func TestOpenProfileReadOnlyRejectsWrites(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "raw")
	writable, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	readonly, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, root)
	if err != nil {
		t.Fatal(err)
	}
	assertReadOnly := func(name string, call func() error) {
		t.Helper()
		if err := call(); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("%s error = %v, want ErrReadOnly", name, err)
		}
	}
	assertReadOnly("Place", func() error {
		_, err := readonly.Place(ctx, bytes.NewBufferString("new"))
		return err
	})
	assertReadOnly("PlaceExact", func() error {
		_, err := readonly.PlaceExact(ctx, "sha256:0000000000000000000000000000000000000000000000000000000000000000", bytes.NewBufferString("new"))
		return err
	})
	assertReadOnly("PlaceRecord", func() error {
		_, err := readonly.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString("new"))
		return err
	})

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || writable.Root() != root {
		t.Fatal("unexpected repository state after rejected writes")
	}
}

func TestOpenProfileReadOnlyZstdProfileAndCompressionMismatch(t *testing.T) {
	ctx := context.Background()
	zstdRoot := filepath.Join(t.TempDir(), "zstd")
	writable, err := OpenZstdDir(zstdRoot)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("zstd recovery payload "), 128)
	receipt, err := writable.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	readonly, err := OpenProfileReadOnly(RepositoryProfileLocalZstdV1, zstdRoot)
	if err != nil {
		t.Fatal(err)
	}
	if profile := DescribeProfile(readonly); profile.Repository != RepositoryProfileLocalZstdV1 || profile.Compression != CompressionProfileZstdV1 {
		t.Fatalf("read-only profile = %+v", profile)
	}
	if err := readonly.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatal(err)
	}
	body, err := readonly.Open(ctx, receipt.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("zstd read-only payload mismatch: read=%v close=%v", readErr, closeErr)
	}

	if _, err := OpenProfileReadOnly(RepositoryProfileDirectoryCASDev, zstdRoot); err == nil {
		t.Fatal("raw read-only profile opened zstd repository")
	}
	if got, err := DetectProfileReadOnly(zstdRoot); err != nil || got != RepositoryProfileLocalZstdV1 {
		t.Fatalf("detected profile = %q, err=%v", got, err)
	}
}
