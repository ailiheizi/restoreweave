package fuse

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/access"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestMountPolicyRejectsAllowOtherAndUnknownOptions(t *testing.T) {
	if err := (Options{Mountpoint: "/mnt", SnapshotRef: "snap"}).Validate(); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
	if err := (Options{Mountpoint: "/mnt", SnapshotRef: "snap", AllowOther: true}).Validate(); err != ErrAllowOther {
		t.Fatalf("allow_other = %v, want ErrAllowOther", err)
	}
	if err := (Options{Mountpoint: "/mnt", SnapshotRef: "snap", Extra: []string{"rw"}}).Validate(); err == nil {
		t.Fatal("rw option unexpectedly accepted")
	}
	if MutationErrno != 30 {
		t.Fatalf("EROFS = %d, want 30", MutationErrno)
	}
	required := map[string]bool{}
	for _, opcode := range MutationOpcodes() {
		required[opcode] = true
	}
	for _, opcode := range []string{"CREATE", "WRITE", "UNLINK", "MKDIR", "SETATTR", "RENAME"} {
		if !required[opcode] {
			t.Fatalf("mutation table missing %s", opcode)
		}
	}
}

func TestInodeMapDoesNotAliasUnrelatedEntries(t *testing.T) {
	inodes := newInodeMap()
	inodes.SetRoot("nsr_root")
	if inodes.Get(inodeKey("nsr_root", "")) != rootInode {
		t.Fatal("root inode was not reserved")
	}
	a := inodes.Get(inodeKey("nse_a", ""))
	b := inodes.Get(inodeKey("nse_b", ""))
	if a == b || a == rootInode || b == rootInode {
		t.Fatalf("inodes collided: root=1 a=%d b=%d", a, b)
	}
	if inodes.Get(inodeKey("nse_a", "")) != a {
		t.Fatal("inode assignment was not stable")
	}
	linked := inodes.Get(inodeKey("nse_c", "hl-1"))
	if inodes.Get(inodeKey("nse_d", "hl-1")) != linked {
		t.Fatal("hard-link group did not share an inode")
	}
}

func TestExportMatchesRestoreBytes(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := []byte("gateway-export-bytes")
	if err := os.WriteFile(filepath.Join(source, "docs", "note.txt"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
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
	svc := &access.Service{Store: store, Repo: repo}
	view, err := svc.OpenView(ctx, readsvc.SnapshotViewRequest{
		Access:   access.LocalAccess(t.Name()),
		Snapshot: access.SelectorFromPublication(publication),
	})
	if err != nil {
		t.Fatalf("open view: %v", err)
	}
	defer view.Close()
	host, err := svc.Host(view)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	export := Export{Host: host, Access: svc}
	got, err := export.ReadFile(ctx, "docs/note.txt")
	if err != nil {
		t.Fatalf("export read: %v", err)
	}
	if !bytes.Equal(got, payload) || !bytes.Equal(got, restored) {
		t.Fatalf("bytes mismatch export=%q restore=%q source=%q", got, restored, payload)
	}
	if !Supported() {
		if err := Serve(ctx, export, Options{Mountpoint: t.TempDir(), SnapshotRef: ingested.SnapshotRef}); err != ErrUnsupportedPlatform {
			t.Fatalf("Serve on this platform = %v, want ErrUnsupportedPlatform", err)
		}
	}
}

func TestProjectedStatCarriesSourceMetadata(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("source-stat-bytes")
	path := filepath.Join(source, "note.txt")
	if err := os.WriteFile(path, payload, 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
	wantTime := time.Date(2024, 5, 1, 12, 30, 0, 0, time.UTC)
	if err := os.Chtimes(path, wantTime, wantTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat: %v", err)
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
	publication, err := store.GetPublicationBySnapshotRef(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("publication: %v", err)
	}
	svc := &access.Service{Store: store, Repo: repo}
	view, err := svc.OpenView(ctx, readsvc.SnapshotViewRequest{
		Access:   access.LocalAccess(t.Name()),
		Snapshot: access.SelectorFromPublication(publication),
	})
	if err != nil {
		t.Fatalf("open view: %v", err)
	}
	defer view.Close()
	files, err := access.CollectFiles(ctx, view)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	entry, ok := files["note.txt"]
	if !ok {
		t.Fatalf("files = %#v", files)
	}
	if entry.ModTime.IsZero() || entry.ModTime.Unix() != info.ModTime().Unix() {
		t.Fatalf("projected mtime = %v, source = %v", entry.ModTime, info.ModTime())
	}
	stat := statFromEntry(entry)
	if stat.ModTime.Unix() != entry.ModTime.Unix() {
		t.Fatalf("portable stat mtime = %v", stat.ModTime)
	}
	if !stat.HasOwnership {
		t.Fatal("projected ownership missing")
	}
}
