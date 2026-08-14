package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
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
