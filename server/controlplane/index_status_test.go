package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestContentIndexStatusDoesNotAdvertiseStaleLexicalGenerationAfterFailedRebuild(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	engineDir := t.TempDir()
	indexer := &search.Indexer{Store: store, Engine: &search.Engine{Dir: engineDir}}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:status-health", seed.RootID); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	entries, err := store.ListLatestNamespaceEntries(ctx, seed.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock")
	dispatcher.search = indexer
	ready, err := dispatcher.contentIndexStatuses(ctx, seed.WorkspaceID, entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if ready[entry.SubjectRef] == nil || ready[entry.SubjectRef].Lexical != "READY" {
			t.Fatalf("initial lexical status = %+v, want READY", ready[entry.SubjectRef])
		}
	}
	brokenDir := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(brokenDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	indexer.Engine.Dir = brokenDir
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:status-failed", seed.RootID); err == nil {
		t.Fatal("failed rebuild returned nil error")
	}
	degraded, err := dispatcher.contentIndexStatuses(ctx, seed.WorkspaceID, entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if degraded[entry.SubjectRef] == nil || degraded[entry.SubjectRef].Lexical != "UNAVAILABLE" {
			t.Fatalf("failed rebuild lexical status = %+v, want UNAVAILABLE", degraded[entry.SubjectRef])
		}
	}
	indexer.Engine.Dir = engineDir
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:status-retry", seed.RootID); err != nil {
		t.Fatalf("successful retry: %v", err)
	}
	recovered, err := dispatcher.contentIndexStatuses(ctx, seed.WorkspaceID, entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if recovered[entry.SubjectRef] == nil || recovered[entry.SubjectRef].Lexical != "READY" {
			t.Fatalf("retried lexical status = %+v, want READY", recovered[entry.SubjectRef])
		}
	}
}

func TestContentIndexStatusDistinguishesSemanticNotBuiltFromUnavailable(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	workspaceID, err := sqlite.NewStableID(sqlite.IDPrefixWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertWorkspace(ctx, &sqlite.Workspace{ID: workspaceID, Name: "status-test"})
	}); err != nil {
		t.Fatal(err)
	}

	// A bound provider/backend with no semantic generation means the file has
	// not been analysed yet, rather than that semantic search is broken.
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock")
	dispatcher.search = &search.Indexer{
		Store:            store,
		Engine:           &search.Engine{Dir: t.TempDir()},
		SemanticProvider: dispatcherSemanticProvider{},
		SemanticZvec:     dispatcherSemanticZvec{},
		SemanticManifest: search.EmbeddingGenerationManifest{SemanticSpace: "status-test-space"},
	}
	entries := []sqlite.NamespaceEntry{{SubjectRef: "subject-status-test"}}
	statuses, err := dispatcher.contentIndexStatuses(ctx, workspaceID, entries)
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses[entries[0].SubjectRef]; got == nil || got.Semantic != "NOT_BUILT" {
		t.Fatalf("semantic status with bound provider and no generation = %+v, want NOT_BUILT", got)
	}

	// Without a provider/backend there is no way to build or verify the
	// dimension, so the same missing generation is honestly unavailable.
	dispatcher.search = &search.Indexer{Store: store, Engine: &search.Engine{Dir: t.TempDir()}}
	statuses, err = dispatcher.contentIndexStatuses(ctx, workspaceID, entries)
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses[entries[0].SubjectRef]; got == nil || got.Semantic != "UNAVAILABLE" {
		t.Fatalf("semantic status without provider = %+v, want UNAVAILABLE", got)
	}
}
