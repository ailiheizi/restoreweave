package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDirPortableRecordsAreRoleScopedAndDiscoverable(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"schema":"restoreweave.publication-commit.v1"}`)
	receipt, err := repo.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RepositoryID == "" || receipt.Digest == "" || receipt.Bytes != int64(len(payload)) || receipt.Existed {
		t.Fatalf("receipt = %+v", receipt)
	}
	if err := repo.VerifyRecord(ctx, receipt); err != nil {
		t.Fatalf("verify: %v", err)
	}
	again, err := repo.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewReader(payload))
	if err != nil || !again.Existed || again.Digest != receipt.Digest {
		t.Fatalf("idempotent placement = %+v, %v", again, err)
	}
	commits, err := repo.ListRecordDigests(ctx, RecordPublicationCommit)
	if err != nil || len(commits) != 1 || commits[0] != receipt.Digest {
		t.Fatalf("commits = %v, %v", commits, err)
	}
	prepared, err := repo.ListRecordDigests(ctx, RecordPreparedClosure)
	if err != nil || len(prepared) != 0 {
		t.Fatalf("prepared = %v, %v", prepared, err)
	}

	reopened, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.RepositoryIdentity() != repo.RepositoryIdentity() {
		t.Fatalf("repository identity changed: %s != %s", reopened.RepositoryIdentity(), repo.RepositoryIdentity())
	}
}

func TestDirPortableRecordVerificationRejectsTampering(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := repo.PlaceRecord(ctx, RecordPreparedClosure, bytes.NewBufferString("prepared"))
	if err != nil {
		t.Fatal(err)
	}
	hexDigest, _ := parseContentID(receipt.Digest)
	path := filepath.Join(repo.Root(), recoveryDirName, recordRoleDir(receipt.Role), AlgorithmSHA256, hexDigest[:2], hexDigest)
	if err := os.WriteFile(path, []byte("changed!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyRecord(ctx, receipt); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("verify tampered record = %v", err)
	}
}

func TestDirPortableRecordListingRejectsMalformedEntries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := repo.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString("record"))
	if err != nil {
		t.Fatal(err)
	}
	hexDigest, err := parseContentID(receipt.Digest)
	if err != nil {
		t.Fatal(err)
	}
	recordRoot := filepath.Join(root, recoveryDirName, recordRoleDir(receipt.Role), AlgorithmSHA256)
	badName := filepath.Join(recordRoot, hexDigest[:hexPrefixLen], "not-a-digest")
	if err := os.WriteFile(badName, []byte("malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ListRecordDigests(ctx, RecordPublicationCommit); err == nil {
		t.Fatal("record listing accepted a malformed digest name")
	}
	if err := os.Remove(badName); err != nil {
		t.Fatal(err)
	}
	wrongPrefix := filepath.Join(recordRoot, "ff", hexDigest)
	if err := os.MkdirAll(filepath.Dir(wrongPrefix), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrongPrefix, []byte("wrong prefix"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ListRecordDigests(ctx, RecordPublicationCommit); err == nil {
		t.Fatal("record listing accepted a digest under the wrong prefix")
	}
}

func TestDirPortableRecordListingRejectsSymlinkEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink entry qualification is platform-specific on Windows")
	}
	ctx := context.Background()
	root := t.TempDir()
	repo, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := repo.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewBufferString("record"))
	if err != nil {
		t.Fatal(err)
	}
	hexDigest, _ := parseContentID(receipt.Digest)
	path := filepath.Join(root, recoveryDirName, recordRoleDir(receipt.Role), AlgorithmSHA256, hexDigest[:hexPrefixLen], hexDigest+"-link")
	target := filepath.Join(t.TempDir(), "outside-record")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ListRecordDigests(ctx, RecordPublicationCommit); err == nil {
		t.Fatal("record listing followed or ignored a symlink entry")
	}
}

func TestMemoryPortableRecords(t *testing.T) {
	ctx := context.Background()
	repo := NewMemory()
	receipt, err := repo.PlaceRecord(ctx, RecordPreparedClosure, bytes.NewBufferString("prepared"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyRecord(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	listed, err := repo.ListRecordDigests(ctx, RecordPreparedClosure)
	if err != nil || len(listed) != 1 || listed[0] != receipt.Digest {
		t.Fatalf("listed = %v, %v", listed, err)
	}
}
