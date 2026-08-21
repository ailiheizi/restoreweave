// Package qualify is the in-tree RepositoryDriver qualification harness.
// It does not select a release engine. The raw and local-zstd in-tree profiles
// always run; Kopia and Restic CLI probes run only when those binaries exist.
package qualify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

// DriverGates are the RestoreWeave-owned checks a RepositoryDriver must pass
// before it can be considered for the exact lane. Engine-level backup CLI
// probes live beside these gates and never replace them.
func DriverGates(ctx context.Context, repo repository.Driver, payload []byte) error {
	if len(payload) == 0 {
		return errors.New("qualification payload is required")
	}
	want := contentID(payload)
	first, err := repo.PlaceExact(ctx, want, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("place: %w", err)
	}
	if first.ContentID != want {
		return fmt.Errorf("place content id = %s, want %s", first.ContentID, want)
	}
	if first.Bytes != int64(len(payload)) || first.StoredBytes <= 0 {
		return fmt.Errorf("place receipt lengths = logical %d stored %d", first.Bytes, first.StoredBytes)
	}
	if err := repo.Verify(ctx, want); err != nil {
		return fmt.Errorf("verify after place: %w", err)
	}
	body, err := repo.Open(ctx, want)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	got, err := io.ReadAll(body)
	closeErr := body.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if !bytes.Equal(got, payload) {
		return errors.New("readback bytes do not match placed payload")
	}
	if contentID(got) != want {
		return errors.New("independent sha256 of readback does not match content id")
	}
	second, err := repo.PlaceExact(ctx, want, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("idempotent place: %w", err)
	}
	if !second.Existed || second.ContentID != want {
		return fmt.Errorf("idempotent receipt = %+v", second)
	}
	if second.StoredBytes != first.StoredBytes {
		return fmt.Errorf("idempotent stored bytes = %d, want %d", second.StoredBytes, first.StoredBytes)
	}
	return nil
}

// CorruptStoredObject flips one byte in the first blob under a directory CAS
// root. The driver must then fail Verify rather than return a healthy object.
func CorruptStoredObject(root string) error {
	var target string
	err := filepath.WalkDir(filepath.Join(root, "blobs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || target != "" {
			return err
		}
		target = path
		return fs.SkipAll
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return err
	}
	if target == "" {
		return errors.New("no stored blob to corrupt")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("stored blob is empty")
	}
	data[0] ^= 0xff
	return os.WriteFile(target, data, 0o600)
}

func contentID(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
