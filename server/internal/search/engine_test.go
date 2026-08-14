package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEngineBuildQueryAndUnavailableAfterDelete(t *testing.T) {
	ctx := context.Background()
	engine := &Engine{Dir: t.TempDir()}
	path, err := engine.Build(ctx, "idx_testgeneration0000000000000000", []Document{
		{
			SubjectID: "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Path:      "docs/readme.txt",
			Name:      "readme.txt",
			Suffix:    "txt",
			EntryType: "REGULAR_FILE",
			ContentID: "sha256:abc",
			Tags:      "reviewed inbox",
			Notes:     "quarterly experiment report",
			Extracted: "unique extract token",
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	hits, err := engine.Query(ctx, path, "readme")
	if err != nil {
		t.Fatalf("Query readme: %v", err)
	}
	if len(hits) != 1 || hits[0].Name != "readme.txt" {
		t.Fatalf("readme hits = %+v", hits)
	}

	hits, err = engine.Query(ctx, path, "reviewed")
	if err != nil {
		t.Fatalf("Query tag: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("tag hits = %+v", hits)
	}

	hits, err = engine.Query(ctx, path, "quarterly")
	if err != nil {
		t.Fatalf("Query note: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("note hits = %+v", hits)
	}

	hits, err = engine.Query(ctx, path, "unique")
	if err != nil {
		t.Fatalf("Query extracted: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("extracted hits = %+v", hits)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	_, err = engine.Query(ctx, path, "readme")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Query after delete = %v, want ErrUnavailable", err)
	}
	if _, err := os.Stat(filepath.Join(engine.Dir)); err != nil {
		t.Fatalf("index directory should remain: %v", err)
	}
}
