package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirPlaceReadbackAndIdempotent(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatalf("open dir repo: %v", err)
	}
	payload := []byte("unknown-binary-fixture")
	first, err := repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if first.Existed || first.Bytes != int64(len(payload)) || first.ContentID == "" {
		t.Fatalf("unexpected receipt: %+v", first)
	}
	if err := repo.Verify(ctx, first.ContentID); err != nil {
		t.Fatalf("verify: %v", err)
	}
	body, err := repo.Open(ctx, first.ContentID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("readback = %q, err=%v", got, err)
	}
	second, err := repo.PlaceExact(ctx, first.ContentID, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("place exact: %v", err)
	}
	if !second.Existed || second.ContentID != first.ContentID {
		t.Fatalf("idempotent receipt = %+v", second)
	}
}

func TestPlaceExactRejectsMismatch(t *testing.T) {
	ctx := context.Background()
	repo := NewMemory()
	_, err := repo.PlaceExact(ctx, "sha256:0000000000000000000000000000000000000000000000000000000000000000", bytes.NewReader([]byte("x")))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error = %v, want digest mismatch", err)
	}
}

func TestDirRepairReplacesCorruptObject(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "repo")
	repo, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("repairable exact payload")
	receipt, err := repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	path := blobPath(root, receipt.ContentID)
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repo.Verify(ctx, receipt.ContentID); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("verify before repair = %v, want digest mismatch", err)
	}
	repaired, err := repo.Repair(ctx, receipt.ContentID, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if !repaired.Existed || repaired.Bytes != int64(len(payload)) {
		t.Fatalf("repair receipt = %+v", repaired)
	}
	if err := repo.Verify(ctx, receipt.ContentID); err != nil {
		t.Fatal(err)
	}
	body, err := repo.Open(ctx, receipt.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(body)
	_ = body.Close()
	if readErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("repaired readback = %q, err=%v", got, readErr)
	}
}

func TestRepairRejectsWrongIdentity(t *testing.T) {
	repo := NewMemory()
	_, err := repo.Repair(context.Background(), "sha256:"+strings.Repeat("0", 64), bytes.NewReader([]byte("wrong")))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("repair error = %v, want digest mismatch", err)
	}
}
