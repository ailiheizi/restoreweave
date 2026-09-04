package search

import (
	"context"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestLexicalSubjectCoverageIsGenerationBound(t *testing.T) {
	ctx := context.Background()
	path, err := (&Engine{Dir: t.TempDir()}).Build(ctx, "generation", []Document{
		{SubjectID: "subject-a", Name: "a"}, {SubjectID: "subject-a", Name: "a-copy"},
	})
	if err != nil {
		t.Fatal(err)
	}
	covered, err := LexicalSubjectCoverage(ctx, sqlite.IndexGeneration{Dimension: DimensionLexical, DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(covered) != 1 {
		t.Fatalf("coverage = %#v, want one subject", covered)
	}
	if _, ok := covered["subject-a"]; !ok {
		t.Fatalf("coverage = %#v, missing subject-a", covered)
	}
}

func TestLexicalSubjectCoverageRejectsMissingGeneration(t *testing.T) {
	_, err := LexicalSubjectCoverage(context.Background(), sqlite.IndexGeneration{Dimension: DimensionLexical, DBPath: "/missing/generation.sqlite"})
	if err == nil {
		t.Fatal("missing generation unexpectedly reported coverage")
	}
}
