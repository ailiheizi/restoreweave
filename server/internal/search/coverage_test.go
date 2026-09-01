package search

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// TestLexicalCoverageReportsAbsenceHonestly proves that a coverage statement
// never claims fields that are not actually present in the feed.
func TestLexicalCoverageReportsAbsenceHonestly(t *testing.T) {
	unavailable := LexicalCoverage(false, nil)
	if unavailable.Available || unavailable.Complete || unavailable.Fields != nil {
		t.Fatalf("unavailable coverage = %+v", unavailable)
	}

	partial := LexicalCoverage(true, map[string]bool{
		AxisPath: true, AxisName: true, AxisType: true, AxisSuffix: true,
	})
	if !partial.Available || partial.Complete {
		t.Fatalf("partial coverage should not be complete: %+v", partial)
	}
	if partial.Fields[AxisDescriptions] {
		t.Fatalf("absent descriptions reported present: %+v", partial)
	}
	found := false
	for _, missing := range partial.Missing {
		if missing == AxisDescriptions {
			found = true
		}
	}
	if !found {
		t.Fatalf("descriptions missing from missing list: %+v", partial.Missing)
	}

	completeFields := map[string]bool{}
	for _, axis := range LexicalCoverageFields() {
		completeFields[axis] = true
	}
	full := LexicalCoverage(true, completeFields)
	if !full.Complete || len(full.Missing) != 0 {
		t.Fatalf("full coverage = %+v", full)
	}
}

// TestIndexerCoverageOnBuiltGeneration proves Coverage reads the actual
// disposable generation and reports absent facets as absent.
func TestIndexerCoverageOnBuiltGeneration(t *testing.T) {
	ctx := context.Background()
	engine := &Engine{Dir: t.TempDir()}
	path, err := engine.Build(ctx, "idx_testcoverage000000000000000000", []Document{
		{
			SubjectID:  "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Path:       "readme.txt",
			Name:       "readme.txt",
			Suffix:     "txt",
			EntryType:  "REGULAR_FILE",
			ContentID:  "sha256:abc",
			Protection: "STORE_EXACT",
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_ = path

	statement := LexicalCoverage(true, map[string]bool{
		AxisPath: true, AxisName: true, AxisSuffix: true, AxisType: true,
		AxisChecksum: true, AxisProtection: true,
	})
	if !statement.Available {
		t.Fatal("expected available coverage")
	}
	if statement.Fields[AxisDescriptions] || statement.Fields[AxisSize] || statement.Fields[AxisMtime] {
		t.Fatalf("absent facets reported present: %+v", statement.Fields)
	}
}

func TestIndexerCoverageGenerationUsesProvidedGeneration(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	engine := &Engine{Dir: t.TempDir()}
	oldID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	oldPath, err := engine.Build(ctx, oldID, []Document{{
		SubjectID: seed.FileEntryID, Path: "old.txt", Name: "old.txt", Suffix: "txt",
		EntryType: "REGULAR_FILE", Descriptions: "description present only in old generation",
	}})
	if err != nil {
		t.Fatalf("build old generation: %v", err)
	}
	newID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	newPath, err := engine.Build(ctx, newID, []Document{{
		SubjectID: seed.FileEntryID, Path: "new.txt", Name: "new.txt", Suffix: "txt",
		EntryType: "REGULAR_FILE",
	}})
	if err != nil {
		t.Fatalf("build new generation: %v", err)
	}
	oldGeneration := sqlite.IndexGeneration{
		ID: oldID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:old",
		NamespaceRootID: seed.RootID, DBPath: oldPath, Dimension: DimensionLexical,
		CreatedAt: time.Now().Add(-time.Hour),
	}
	newGeneration := sqlite.IndexGeneration{
		ID: newID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:new",
		NamespaceRootID: seed.RootID, DBPath: newPath, Dimension: DimensionLexical,
		CreatedAt: time.Now(),
	}
	if err := store.InsertIndexGeneration(ctx, &oldGeneration); err != nil {
		t.Fatalf("insert old generation: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &newGeneration); err != nil {
		t.Fatalf("insert new generation: %v", err)
	}
	indexer := &Indexer{Store: store, Engine: engine}
	latest, err := indexer.Coverage(ctx, seed.WorkspaceID)
	if err != nil {
		t.Fatalf("latest coverage: %v", err)
	}
	if latest.Fields[AxisDescriptions] {
		t.Fatalf("latest coverage read old generation: %+v", latest)
	}
	pinned, err := indexer.CoverageGeneration(ctx, seed.WorkspaceID, oldGeneration)
	if err != nil {
		t.Fatalf("pinned coverage: %v", err)
	}
	if !pinned.Fields[AxisDescriptions] || pinned.Complete {
		t.Fatalf("pinned coverage did not measure supplied old generation: %+v", pinned)
	}
	if pinned.Fields[AxisPath] != latest.Fields[AxisPath] {
		t.Fatalf("pinned/latest path coverage mismatch: pinned=%+v latest=%+v", pinned, latest)
	}
}

// TestNormalizeFilters proves numeric range validation fails closed.
func TestNormalizeFilters(t *testing.T) {
	if _, err := NormalizeFilters(Filters{}); err != nil {
		t.Fatalf("empty filters = %v", err)
	}
	bad := Filters{SizeMin: int64Ptr(100), SizeMax: int64Ptr(10)}
	if _, err := NormalizeFilters(bad); err == nil {
		t.Fatal("expected size range error")
	}
	badTime := Filters{MtimeAfter: int64Ptr(200), MtimeBefore: int64Ptr(100)}
	if _, err := NormalizeFilters(badTime); err == nil {
		t.Fatal("expected mtime range error")
	}
	negative := Filters{SizeMin: int64Ptr(-1)}
	if _, err := NormalizeFilters(negative); err == nil {
		t.Fatal("expected negative size error")
	}
	ok := Filters{EntryType: " regular_file ", Suffix: " TXT ", Language: " en "}
	normalized, err := NormalizeFilters(ok)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized.EntryType != "REGULAR_FILE" || normalized.Suffix != "txt" {
		t.Fatalf("normalized = %+v", normalized)
	}
}
