package exact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestRestoreRejectsSymlinkParentBeforeWriting(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Schema:      SnapshotSchemaV1,
		SnapshotRef: "symlink-parent",
		Entries: []ManifestEntry{
			{RelativePath: "link", RawPath: []byte("link"), EntryType: string(sqlite.EntrySymlink), SymlinkTarget: []byte("/outside")},
			{RelativePath: "link/escaped.txt", RawPath: []byte("link/escaped.txt"), EntryType: string(sqlite.EntryFile), ContentID: "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"},
		},
	}
	if _, err := writeManifest(repo.Root(), manifest); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "restore")
	_, err = (&Service{Store: store, Repo: repo}).Restore(ctx, manifest.SnapshotRef, destination)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("restore error = %v, want ErrBlocked", err)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("restore destination was created after rejected manifest: %v", statErr)
	}
}

func TestRestoreRejectsConflictingManifestEntries(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Schema:      SnapshotSchemaV1,
		SnapshotRef: "conflicting-entry",
		Entries: []ManifestEntry{
			{RelativePath: "same", RawPath: []byte("same"), EntryType: string(sqlite.EntryDirectory)},
			{RelativePath: "same", RawPath: []byte("same"), EntryType: string(sqlite.EntryFile), ContentID: "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"},
		},
	}
	if _, err := writeManifest(repo.Root(), manifest); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "restore")
	_, err = (&Service{Store: store, Repo: repo}).Restore(ctx, manifest.SnapshotRef, destination)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("restore error = %v, want ErrBlocked", err)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("restore destination was created after rejected manifest: %v", statErr)
	}
}

func TestApplyRestorePlanRejectsChangedDestination(t *testing.T) {
	ctx := context.Background()
	service, repo := newRestorePlanTestService(t)
	manifest, err := writeManifest(repo.Root(), Manifest{
		Schema: SnapshotSchemaV1, SnapshotRef: "destination-drift",
	})
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	plan, err := service.InspectRestore(ctx, manifest.SnapshotRef, destination)
	if err != nil {
		t.Fatalf("inspect restore: %v", err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatalf("create destination after planning: %v", err)
	}

	if _, err := service.ApplyRestorePlan(ctx, plan); !errors.Is(err, ErrRestorePlanStale) {
		t.Fatalf("apply changed-destination plan error = %v, want ErrRestorePlanStale", err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 0 {
		t.Fatalf("changed destination was mutated: entries=%d err=%v", len(entries), err)
	}
}

func TestApplyRestorePlanRejectsChangedManifest(t *testing.T) {
	ctx := context.Background()
	service, repo := newRestorePlanTestService(t)
	manifest, err := writeManifest(repo.Root(), Manifest{
		Schema: SnapshotSchemaV1, SnapshotRef: "manifest-drift",
	})
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	plan, err := service.InspectRestore(ctx, manifest.SnapshotRef, destination)
	if err != nil {
		t.Fatalf("inspect restore: %v", err)
	}
	manifest.Entries = []ManifestEntry{{
		RelativePath: "added-after-planning",
		RawPath:      []byte("added-after-planning"),
		EntryType:    string(sqlite.EntryDirectory),
	}}
	if _, err := writeManifest(repo.Root(), manifest); err != nil {
		t.Fatalf("replace manifest: %v", err)
	}

	if _, err := service.ApplyRestorePlan(ctx, plan); !errors.Is(err, ErrRestorePlanStale) {
		t.Fatalf("apply changed-manifest plan error = %v, want ErrRestorePlanStale", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("changed manifest created destination: %v", err)
	}
}

func newRestorePlanTestService(t *testing.T) (*Service, *repository.Dir) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: store, Repo: repo}, repo
}
