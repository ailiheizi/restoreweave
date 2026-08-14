package access

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestFileAccessMatchesRestoreBytes(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := []byte("fuse-exact-bytes")
	if err := os.WriteFile(filepath.Join(source, "docs", "note.txt"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink("docs/note.txt", filepath.Join(source, "alias.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	lane := &exact.Service{Store: store, Repo: repo}
	ingested, err := lane.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := lane.Restore(ctx, ingested.SnapshotRef, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(dest, "docs", "note.txt"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}

	publication, err := store.GetPublicationBySnapshotRef(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("publication: %v", err)
	}
	svc := &Service{Store: store, Repo: repo}
	view, err := svc.OpenView(ctx, readsvc.SnapshotViewRequest{
		Access:   LocalAccess(t.Name()),
		Snapshot: SelectorFromPublication(publication),
	})
	if err != nil {
		t.Fatalf("open view: %v", err)
	}
	defer view.Close()

	root, err := view.Root(ctx)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	docs, err := view.Lookup(ctx, root.ID, readsvc.PathComponent{Normalized: "docs", NormalizationProfile: "posix"})
	if err != nil {
		t.Fatalf("lookup docs: %v", err)
	}
	if docs.Kind != readsvc.EntryDirectory {
		t.Fatalf("docs kind = %s", docs.Kind)
	}
	note, err := view.Lookup(ctx, docs.ID, readsvc.PathComponent{Normalized: "note.txt", NormalizationProfile: "posix"})
	if err != nil {
		t.Fatalf("lookup note: %v", err)
	}
	got, err := ReadAll(ctx, svc, view, note.ID)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) || !bytes.Equal(got, restored) {
		t.Fatalf("bytes mismatch: access=%q restore=%q source=%q", got, restored, payload)
	}

	alias, err := view.Lookup(ctx, root.ID, readsvc.PathComponent{Normalized: "alias.txt", NormalizationProfile: "posix"})
	if err != nil {
		t.Fatalf("lookup alias: %v", err)
	}
	target, err := view.ReadLink(ctx, alias.ID)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if string(target) != "docs/note.txt" {
		t.Fatalf("symlink target = %q", target)
	}

	files, err := CollectFiles(ctx, view)
	if err != nil {
		t.Fatalf("CollectFiles: %v", err)
	}
	if _, ok := files["docs/note.txt"]; !ok {
		t.Fatalf("collected files = %v", files)
	}
	if _, err := ReadAll(ctx, svc, view, alias.ID); err == nil {
		t.Fatal("reading a symlink unexpectedly succeeded")
	}
}
