package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestZstdDirReadbackCompressionAndDeduplication(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenZstdDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("restoreweave semantic content "), 4096)
	first, err := repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if first.Existed || first.Bytes != int64(len(payload)) || first.StoredBytes >= first.Bytes {
		t.Fatalf("unexpected compressed receipt: %+v", first)
	}
	body, err := repo.Open(ctx, first.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("readback bytes mismatch: len=%d read=%v close=%v", len(got), readErr, closeErr)
	}
	if err := repo.Verify(ctx, first.ContentID); err != nil {
		t.Fatal(err)
	}
	second, err := repo.PlaceExact(ctx, first.ContentID, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Existed || second.StoredBytes != first.StoredBytes || second.ContentID != first.ContentID {
		t.Fatalf("deduplicated receipt: %+v then %+v", first, second)
	}
}

func TestZstdDirDetectsTruncationAndCorruption(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "repo")
	repo, err := OpenZstdDir(root)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("checksum-protected payload "), 1000)
	receipt, err := repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	path := blobPath(root, receipt.ContentID)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, info.Size()-1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Verify(ctx, receipt.ContentID); err == nil {
		t.Fatal("truncated zstd frame verified successfully")
	}
	if body, err := repo.Open(ctx, receipt.ContentID); err == nil {
		// Header parsing may reject a damaged object before the first read.
		_, _ = io.Copy(io.Discard, body)
		_ = body.Close()
	}

	// A fresh object exercises the frame checksum path rather than only the
	// decoder's truncated-frame check.
	repo, err = OpenZstdDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	path = blobPath(repo.Root(), receipt.ContentID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repo.Verify(ctx, receipt.ContentID); err == nil {
		t.Fatal("corrupted zstd frame verified successfully")
	}
}

func TestZstdDirRelocationAndProfileMismatch(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "repo")
	repo, err := OpenZstdDir(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := repo.Place(ctx, bytes.NewBufferString("relocatable"))
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenZstdDir(moved)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDir(moved); err == nil {
		t.Fatal("raw profile opened a zstd repository")
	}
	raw, err := OpenDir(filepath.Join(t.TempDir(), "raw"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenZstdDir(raw.Root()); err == nil {
		t.Fatal("zstd profile opened a raw repository")
	}
	if _, err := OpenProfileWithCompression(RepositoryProfileLocalZstdV1, CompressionProfileIdentity, filepath.Join(t.TempDir(), "bad")); err == nil {
		t.Fatal("invalid profile tuple accepted")
	}
}

func TestZstdDirRejectsUnmarkedLegacyRawRepository(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	payload := []byte("legacy raw repository object")
	sum := sha256.Sum256(payload)
	contentID := AlgorithmSHA256 + ":" + hex.EncodeToString(sum[:])
	dest := blobPath(root, contentID)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenZstdDir(root); err == nil {
		t.Fatal("zstd profile adopted an unmarked legacy raw repository")
	}
	if _, err := os.Stat(filepath.Join(root, repositoryProfileFile)); !os.IsNotExist(err) {
		t.Fatalf("failed zstd open wrote a profile marker: %v", err)
	}
	raw, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Verify(ctx, contentID); err != nil {
		t.Fatalf("legacy raw repository migration: %v", err)
	}
}

func TestConcurrentZstdOpenUsesOneProfileAndIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	const workers = 8
	repositories := make([]*ZstdDir, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			repositories[i], errs[i] = OpenZstdDir(root)
		}(i)
	}
	close(start)
	wg.Wait()
	identity := ""
	for i := range repositories {
		if errs[i] != nil {
			t.Fatalf("open %d: %v", i, errs[i])
		}
		if identity == "" {
			identity = repositories[i].RepositoryIdentity()
		}
		if repositories[i].RepositoryIdentity() != identity {
			t.Fatalf("repository identities differ: %q != %q", repositories[i].RepositoryIdentity(), identity)
		}
	}
}

func TestZstdDirRecordDriverAndConcurrentNoReplace(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenProfile(RepositoryProfileLocalZstdV1, filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := repo.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString(`{"commit":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	opened, err := repo.OpenRecord(ctx, record.Role, record.Digest)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(opened)
	_ = opened.Close()
	if err != nil || string(data) != `{"commit":true}` {
		t.Fatalf("record readback = %q, err=%v", data, err)
	}

	payload := bytes.Repeat([]byte("concurrent placement "), 1000)
	const workers = 8
	receipts := make([]Receipt, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			receipts[i], errs[i] = repo.Place(ctx, bytes.NewReader(payload))
		}(i)
	}
	wg.Wait()
	created := 0
	for i := range receipts {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if !receipts[i].Existed {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("concurrent placements created %d objects", created)
	}
}

func TestStoredBytesForRawAndMemory(t *testing.T) {
	ctx := context.Background()
	payload := []byte("logical bytes")
	raw, err := OpenDir(filepath.Join(t.TempDir(), "raw"))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := raw.Place(ctx, bytes.NewReader(payload))
	if err != nil || receipt.StoredBytes != receipt.Bytes {
		t.Fatalf("raw receipt = %+v, err=%v", receipt, err)
	}
	memory := NewMemory()
	receipt, err = memory.Place(ctx, bytes.NewReader(payload))
	if err != nil || receipt.StoredBytes != receipt.Bytes {
		t.Fatalf("memory receipt = %+v, err=%v", receipt, err)
	}
}

func TestDescribeRepositoryProfiles(t *testing.T) {
	raw, err := OpenDir(filepath.Join(t.TempDir(), "raw"))
	if err != nil {
		t.Fatal(err)
	}
	zstdRepo, err := OpenZstdDir(filepath.Join(t.TempDir(), "zstd"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		driver      Driver
		repository  string
		compression string
	}{
		{"raw", raw, RepositoryProfileDirectoryCASDev, CompressionProfileIdentity},
		{"zstd", zstdRepo, RepositoryProfileLocalZstdV1, CompressionProfileZstdV1},
		{"memory", NewMemory(), "memory-test-v1", CompressionProfileIdentity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DescribeProfile(test.driver)
			if got.Repository != test.repository || got.Compression != test.compression {
				t.Fatalf("profile = %+v", got)
			}
		})
	}
}
