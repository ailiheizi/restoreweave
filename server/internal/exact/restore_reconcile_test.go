package exact

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestReconcileRestorePlanRequiresCompleteUnchangedOutput(t *testing.T) {
	ctx := context.Background()
	service, repo := newRestoreReconcileTestService(t)
	manifest := restoreReconcileManifest(t, ctx, repo)

	t.Run("absent and empty are not executed", func(t *testing.T) {
		for _, name := range []string{"absent", "empty"} {
			destination := filepath.Join(t.TempDir(), name)
			if name == "empty" {
				if err := os.MkdirAll(destination, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := service.InspectRestore(ctx, manifest.SnapshotRef, destination)
			if err != nil {
				t.Fatalf("inspect %s: %v", name, err)
			}
			if _, err := service.ReconcileRestorePlan(ctx, plan); !errors.Is(err, ErrRestoreNotExecuted) {
				t.Fatalf("reconcile %s error = %v, want ErrRestoreNotExecuted", name, err)
			}
		}
	})

	t.Run("complete output is recognized", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "complete")
		plan, err := service.InspectRestore(ctx, manifest.SnapshotRef, destination)
		if err != nil {
			t.Fatal(err)
		}
		want, err := service.ApplyRestorePlan(ctx, plan)
		if err != nil {
			t.Fatalf("apply restore: %v", err)
		}
		got, err := service.ReconcileRestorePlan(ctx, plan)
		if err != nil {
			t.Fatalf("reconcile complete output: %v", err)
		}
		if got.SnapshotRef != want.SnapshotRef || got.Files != want.Files || got.Bytes != want.Bytes {
			t.Fatalf("reconciled result = %+v, applied = %+v", got, want)
		}
	})

	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, destination string)
	}{
		{name: "tampered file", mutate: func(t *testing.T, destination string) {
			if err := os.WriteFile(filepath.Join(destination, "folder", "payload.txt"), []byte("tampered"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "extra path", mutate: func(t *testing.T, destination string) {
			if err := os.WriteFile(filepath.Join(destination, "unplanned.txt"), []byte("extra"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "partial output", mutate: func(t *testing.T, destination string) {
			if err := os.Remove(filepath.Join(destination, "folder", "payload.txt")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "invalid")
			plan, err := service.InspectRestore(ctx, manifest.SnapshotRef, destination)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.ApplyRestorePlan(ctx, plan); err != nil {
				t.Fatalf("apply restore: %v", err)
			}
			test.mutate(t, destination)
			if _, err := service.ReconcileRestorePlan(ctx, plan); !errors.Is(err, ErrBlocked) {
				t.Fatalf("reconcile %s error = %v, want ErrBlocked", test.name, err)
			}
		})
	}
}

func restoreReconcileManifest(t *testing.T, ctx context.Context, repo *repository.Dir) Manifest {
	t.Helper()
	payload := []byte("restore reconcile payload")
	receipt, err := repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("place payload: %v", err)
	}
	size := int64(len(payload))
	manifest, err := writeManifest(repo.Root(), Manifest{
		Schema: SnapshotSchemaV1, SnapshotRef: "restore-reconcile",
		Entries: []ManifestEntry{
			{RelativePath: "folder", RawPath: []byte("folder"), EntryType: string(sqlite.EntryDirectory)},
			{RelativePath: "folder/payload.txt", RawPath: []byte("folder/payload.txt"), EntryType: string(sqlite.EntryFile), ContentID: receipt.ContentID, LogicalSize: &size},
			{RelativePath: "source-link", RawPath: []byte("source-link"), EntryType: string(sqlite.EntrySymlink), SymlinkTarget: []byte("folder/payload.txt")},
		},
	})
	if err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return manifest
}

func newRestoreReconcileTestService(t *testing.T) (*Service, *repository.Dir) {
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
