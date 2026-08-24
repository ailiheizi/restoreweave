package search

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// TestEngineQueryFilteredTypedStructuredFilters proves typed structured
// filters narrow results precisely on top of the free-text MATCH, and that a
// missing facet filters a subject out instead of inventing a match.
func TestEngineQueryFilteredTypedStructuredFilters(t *testing.T) {
	ctx := context.Background()
	engine := &Engine{Dir: t.TempDir()}
	size := int64(1024)
	mtime := int64(1700000000000)
	segments, err := json.Marshal([]SegmentRef{{
		DescriptionDocumentID: "dsc_a",
		SegmentID:             "seg_b",
		Ordinal:               0,
		MatchedText:           "winter solstice ritual",
		Kind:                  "AI_SUMMARY",
		Producer:              "model:summary-v1",
		Accepted:              false,
		Language:              "en",
	}})
	if err != nil {
		t.Fatal(err)
	}
	path, err := engine.Build(ctx, "idx_testfiltered000000000000000000", []Document{
		{
			SubjectID:      "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Path:           "docs/report.txt",
			Name:           "report.txt",
			Suffix:         "txt",
			EntryType:      "REGULAR_FILE",
			ContentID:      "sha256:abc",
			DuplicateGroup: "sha256:abc",
			Protection:     "STORE_EXACT EXACT_PROTECTED",
			Language:       "en",
			LogicalSize:    &size,
			MtimeMillis:    &mtime,
			Segments:       string(segments),
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Filters alone (no free text) still run through the post-filter path.
	hits, err := engine.QueryFiltered(ctx, path, "", nil, Filters{
		EntryType:      "REGULAR_FILE",
		ContentID:      "sha256:abc",
		DuplicateGroup: "sha256:abc",
		ProtectionMode: "STORE_EXACT",
		Language:       "en",
		Suffix:         "txt",
		SizeMin:        int64Ptr(512),
		SizeMax:        int64Ptr(2048),
		MtimeAfter:     int64Ptr(1699999999000),
		MtimeBefore:    int64Ptr(1700000001000),
	})
	if err != nil || len(hits) != 1 || hits[0].SubjectID != "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("all-filters hits = %+v err=%v", hits, err)
	}

	// A mismatching protection mode must exclude the subject.
	hits, err = engine.QueryFiltered(ctx, path, "", nil, Filters{ProtectionMode: "LINK_ONLY"})
	if err != nil || len(hits) != 0 {
		t.Fatalf("protection-mismatch hits = %+v err=%v", hits, err)
	}

	// A size outside the window must exclude the subject, not silently match.
	over := int64(99999)
	hits, err = engine.QueryFiltered(ctx, path, "", nil, Filters{SizeMin: &over})
	if err != nil || len(hits) != 0 {
		t.Fatalf("size-min hits = %+v err=%v", hits, err)
	}

	// Suffix filter matches the exact lowercase suffix.
	hits, err = engine.QueryFiltered(ctx, path, "", nil, Filters{Suffix: ".TXT"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("suffix hits = %+v err=%v", hits, err)
	}
}

func TestEngineQueryFilteredLanguageIsCaseInsensitiveExactAndFailsClosed(t *testing.T) {
	ctx := context.Background()
	engine := &Engine{Dir: t.TempDir()}
	path, err := engine.Build(ctx, "idx_testlanguage000000000000000000", []Document{
		{SubjectID: "subject-en", Name: "english.txt", Language: "EN"},
		{SubjectID: "subject-zh", Name: "chinese.txt", Language: "zh"},
		{SubjectID: "subject-missing", Name: "missing.txt"},
		{SubjectID: "subject-empty", Name: "empty.txt", Language: "  "},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	hits, err := engine.QueryFiltered(ctx, path, "", nil, Filters{Language: " en "})
	if err != nil {
		t.Fatalf("language query: %v", err)
	}
	if len(hits) != 1 || hits[0].SubjectID != "subject-en" {
		t.Fatalf("language hits = %+v, want only subject-en", hits)
	}

	hits, err = engine.QueryFiltered(ctx, path, "", nil, Filters{Language: "ZH"})
	if err != nil {
		t.Fatalf("wrong-language query: %v", err)
	}
	if len(hits) != 1 || hits[0].SubjectID != "subject-zh" {
		t.Fatalf("wrong-language hits = %+v, want only subject-zh", hits)
	}
}

func TestEngineQueryFilteredPagesPastRejectedCandidatesAndBoundsOutput(t *testing.T) {
	ctx := context.Background()
	engine := &Engine{Dir: t.TempDir()}
	docs := make([]Document, 0, 1002)
	for i := 0; i < 1001; i++ {
		docs = append(docs, Document{
			SubjectID: fmt.Sprintf("subject-en-%04d", i),
			Name:      fmt.Sprintf("english-%04d.txt", i),
			Language:  "en",
		})
	}
	docs = append(docs, Document{SubjectID: "subject-zh-last", Name: "chinese.txt", Language: "zh"})
	path, err := engine.Build(ctx, "idx_testfilteredpaging00000000000000", docs)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The only matching row sits beyond the first 1000 candidates. Filtering
	// must continue to the next page instead of reporting a false negative.
	hits, err := engine.QueryFiltered(ctx, path, "", nil, Filters{Language: "zh"})
	if err != nil {
		t.Fatalf("paged language query: %v", err)
	}
	if len(hits) != 1 || hits[0].SubjectID != "subject-zh-last" {
		t.Fatalf("paged language hits = %+v, want subject-zh-last", hits)
	}

	// Candidate paging does not remove the bounded result contract.
	hits, err = engine.QueryFiltered(ctx, path, "", nil, Filters{Language: "en"})
	if err != nil {
		t.Fatalf("bounded language query: %v", err)
	}
	if len(hits) != 1000 {
		t.Fatalf("bounded language hits = %d, want 1000", len(hits))
	}
}

// TestEngineQuerySegmentProvenance proves that a description match carries the
// exact segment that produced it, with kind, producer, and acceptance state.
func TestEngineQuerySegmentProvenance(t *testing.T) {
	ctx := context.Background()
	engine := &Engine{Dir: t.TempDir()}
	segments, err := json.Marshal([]SegmentRef{
		{
			DescriptionDocumentID: "dsc_summary",
			SegmentID:             "seg_1",
			Ordinal:               0,
			MatchedText:           "a hopeful walk across the flooded plains",
			Kind:                  "AI_SUMMARY",
			Producer:              "model:local-summary-v1",
			Accepted:              false,
			Language:              "zh",
		},
		{
			DescriptionDocumentID: "dsc_user",
			SegmentID:             "seg_2",
			Ordinal:               0,
			MatchedText:           "my childhood home at the river bend",
			Kind:                  "USER",
			Producer:              "command",
			Accepted:              true,
			Language:              "en",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	path, err := engine.Build(ctx, "idx_testsegments000000000000000000", []Document{
		{
			SubjectID:    "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Path:         "docs/journal.txt",
			Name:         "journal.txt",
			EntryType:    "REGULAR_FILE",
			Descriptions: "a hopeful walk across the flooded plains my childhood home at the river bend",
			Segments:     string(segments),
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	hits, err := engine.Query(ctx, path, "flooded", nil)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits = %+v err=%v", hits, err)
	}
	if len(hits[0].Segments) != 1 {
		t.Fatalf("segment provenance = %+v", hits[0].Segments)
	}
	ref := hits[0].Segments[0]
	if ref.DescriptionDocumentID != "dsc_summary" || ref.Kind != "AI_SUMMARY" ||
		ref.Producer != "model:local-summary-v1" || ref.Accepted {
		t.Fatalf("summary segment = %+v", ref)
	}

	hits, err = engine.Query(ctx, path, "river", nil)
	if err != nil || len(hits) != 1 || len(hits[0].Segments) != 1 {
		t.Fatalf("user segment hits = %+v err=%v", hits, err)
	}
	if ref := hits[0].Segments[0]; ref.DescriptionDocumentID != "dsc_user" || !ref.Accepted || ref.Kind != "USER" {
		t.Fatalf("user segment = %+v", ref)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
