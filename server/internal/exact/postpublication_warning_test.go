package exact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type failingPublicationProcessor struct{}

func (failingPublicationProcessor) ProcessPublication(context.Context, string, string, string) error {
	return errors.New("processor unavailable")
}

func TestIngestKeepsPublicationSuccessWhenPostPublicationServicesWarn(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.txt"), []byte("durable payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}

	result, err := (&Service{
		Store:     store,
		Repo:      repo,
		Processor: failingPublicationProcessor{},
		Indexer:   &search.Indexer{},
	}).Ingest(ctx, root)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if result.SnapshotRef == "" || result.ManifestDigest == "" {
		t.Fatalf("publication result = %+v", result)
	}
	if len(result.Warnings) != 2 || result.Warnings[0] != "processor: processor unavailable" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if result.Warnings[1] == "" {
		t.Fatal("indexer warning is empty")
	}
	publications, err := store.ListPublications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(publications) != 1 {
		t.Fatalf("publications = %d, want 1", len(publications))
	}
}
