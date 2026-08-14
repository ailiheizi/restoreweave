package exact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/scanner"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestIngestPlacesAndRestoresAfterCatalogLoss(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte{0x00, 0x01, 0xde, 0xad, 0xbe, 0xef, 0xff}
	if err := os.Mkdir(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	unknown := filepath.Join(source, "nested", "unknown.bin")
	if err := os.WriteFile(unknown, payload, 0o600); err != nil {
		t.Fatalf("write unknown binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "readme.txt"), []byte("hello restoreweave"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if err := os.Symlink("nested/unknown.bin", filepath.Join(source, "alias.bin")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	repoPath := filepath.Join(t.TempDir(), "repository")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	repo, err := repository.OpenDir(repoPath)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	service := &Service{Store: store, Repo: repo}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if ingested.Files < 2 || ingested.SnapshotRef == "" || ingested.RootID == "" {
		t.Fatalf("incomplete ingest result: %+v", ingested)
	}
	if _, err := service.Verify(ctx, ingested.SnapshotRef); err != nil {
		t.Fatalf("verify: %v", err)
	}

	listed, err := store.ListNamespaceChildren(ctx, ingested.WorkspaceID, ingested.RootID, "")
	if err != nil {
		t.Fatalf("list namespace: %v", err)
	}
	if len(listed) == 0 {
		t.Fatal("namespace is empty after ingest")
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close catalog: %v", err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatalf("delete catalog: %v", err)
	}

	fresh, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "empty.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatalf("open empty catalog: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	restorer := &Service{Store: fresh, Repo: repo}
	dest := filepath.Join(t.TempDir(), "restored")
	restored, err := restorer.Restore(ctx, ingested.SnapshotRef, dest)
	if err != nil {
		t.Fatalf("restore after catalog loss: %v", err)
	}
	if restored.Files < 2 {
		t.Fatalf("restored files = %d", restored.Files)
	}

	got, err := os.ReadFile(filepath.Join(dest, "nested", "unknown.bin"))
	if err != nil {
		t.Fatalf("read restored binary: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("restored bytes = %x, want %x", got, payload)
	}
	sum := sha256.Sum256(got)
	want := "sha256:" + hex.EncodeToString(sum[:])
	originalSum := sha256.Sum256(payload)
	originalID := "sha256:" + hex.EncodeToString(originalSum[:])
	if want != originalID {
		t.Fatalf("sha256 %s != %s", want, originalID)
	}
	target, err := os.Readlink(filepath.Join(dest, "alias.bin"))
	if err != nil {
		t.Fatalf("read restored symlink: %v", err)
	}
	if target != "nested/unknown.bin" {
		t.Fatalf("symlink target = %q", target)
	}
}

func TestRequireQualifiedRejectsPathString(t *testing.T) {
	err := requireQualified(scanner.CaptureModePathString, scanner.ScanResult{State: scanner.ScanComplete})
	if !errors.Is(err, ErrNotQualified) {
		t.Fatalf("error = %v, want ErrNotQualified", err)
	}
}
