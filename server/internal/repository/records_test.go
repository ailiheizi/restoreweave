package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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
