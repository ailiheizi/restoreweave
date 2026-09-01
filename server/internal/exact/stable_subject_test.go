package exact

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestIngestStableSubjectContinuityAndPathSeparation(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("stable subject payload")
	if err := os.WriteFile(filepath.Join(source, "a.txt"), payload, 0o600); err != nil {
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
	service := &Service{Store: store, Repo: repo}

	first, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	firstEntries, err := store.ListNamespaceContent(ctx, first.WorkspaceID, first.RootID)
	if err != nil {
		t.Fatal(err)
	}
	firstEntry := findStableSubjectTestEntry(t, firstEntries, "a.txt")
	if !strings.HasPrefix(firstEntry.SubjectRef, sqlite.IDPrefixSubject+"_") {
		t.Fatalf("first observation subject = %q, want subj_*", firstEntry.SubjectRef)
	}

	second, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	secondEntries, err := store.ListNamespaceContent(ctx, second.WorkspaceID, second.RootID)
	if err != nil {
		t.Fatal(err)
	}
	secondEntry := findStableSubjectTestEntry(t, secondEntries, "a.txt")
	if secondEntry.ID == firstEntry.ID {
		t.Fatal("second observation reused the snapshot-local namespace entry ID")
	}
	if secondEntry.SubjectRef != firstEntry.SubjectRef {
		t.Fatalf("same source/path subject changed: first=%q second=%q", firstEntry.SubjectRef, secondEntry.SubjectRef)
	}

	if err := os.WriteFile(filepath.Join(source, "b.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("third ingest: %v", err)
	}
	thirdEntries, err := store.ListNamespaceContent(ctx, third.WorkspaceID, third.RootID)
	if err != nil {
		t.Fatal(err)
	}
	aThird := findStableSubjectTestEntry(t, thirdEntries, "a.txt")
	bThird := findStableSubjectTestEntry(t, thirdEntries, "b.txt")
	if aThird.SubjectRef != firstEntry.SubjectRef {
		t.Fatalf("existing path lost subject continuity: got=%q want=%q", aThird.SubjectRef, firstEntry.SubjectRef)
	}
	if bThird.SubjectRef == aThird.SubjectRef {
		t.Fatal("different paths with identical bytes were merged into one subject")
	}
	if bThird.ContentID != aThird.ContentID {
		t.Fatalf("test did not exercise identical content: a=%q b=%q", aThird.ContentID, bThird.ContentID)
	}
}

func findStableSubjectTestEntry(t *testing.T, entries []sqlite.NamespaceEntry, name string) sqlite.NamespaceEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.DisplayName == name {
			return entry
		}
	}
	t.Fatalf("namespace entry %q not found in %+v", name, entries)
	return sqlite.NamespaceEntry{}
}
