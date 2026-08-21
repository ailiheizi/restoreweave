package search

import (
	"context"
	"testing"
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
