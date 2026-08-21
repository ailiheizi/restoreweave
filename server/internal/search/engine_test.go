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
			SubjectID:    "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Path:         "docs/readme.txt",
			Name:         "readme.txt",
			Suffix:       "txt",
			EntryType:    "REGULAR_FILE",
			ContentID:    "sha256:abc",
			Metadata:     "platform pc year 2024",
			Duplicates:   "duplicate same-content count 2",
			Protection:   "STORE_EXACT EXACT_PROTECTED",
			Locators:     "HTTPS example.test/archive",
			Tags:         "reviewed inbox",
			Notes:        "quarterly experiment report",
			Descriptions: "主角在废墟中寻找失落城市",
			Extracted:    "unique extract token",
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	hits, err := engine.Query(ctx, path, "readme", nil)
	if err != nil {
		t.Fatalf("Query readme: %v", err)
	}
	if len(hits) != 1 || hits[0].Name != "readme.txt" {
		t.Fatalf("readme hits = %+v", hits)
	}
	if !containsAxis(hits[0].ConstructAxes, AxisName) || !containsAxis(hits[0].ConstructAxes, AxisPath) {
		t.Fatalf("readme axes = %v", hits[0].ConstructAxes)
	}

	hits, err = engine.Query(ctx, path, "reviewed", nil)
	if err != nil {
		t.Fatalf("Query tag: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("tag hits = %+v", hits)
	}

	hits, err = engine.Query(ctx, path, "quarterly", nil)
	if err != nil {
		t.Fatalf("Query note: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("note hits = %+v", hits)
	}

	hits, err = engine.Query(ctx, path, "unique", nil)
	if err != nil {
		t.Fatalf("Query extracted: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("extracted hits = %+v", hits)
	}

	hits, err = engine.Query(ctx, path, "unique", []string{AxisTags})
	if err != nil {
		t.Fatalf("Query unique on tags: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("tags-only unique hits = %+v", hits)
	}

	hits, err = engine.Query(ctx, path, "reviewed", []string{AxisTags})
	if err != nil {
		t.Fatalf("Query reviewed on tags: %v", err)
	}
	if len(hits) != 1 || !containsAxis(hits[0].ConstructAxes, AxisTags) {
		t.Fatalf("tags-only reviewed hits = %+v", hits)
	}

	for query, axis := range map[string]string{
		"REGULAR":   AxisType,
		"sha256":    AxisChecksum,
		"platform":  AxisMetadata,
		"duplicate": AxisDuplicates,
		"PROTECTED": AxisProtection,
		"example":   AxisLocators,
		"主角":        AxisDescriptions,
	} {
		hits, err = engine.Query(ctx, path, query, []string{axis})
		if err != nil || len(hits) != 1 || !containsAxis(hits[0].ConstructAxes, axis) {
			t.Fatalf("query %q on %s = %+v, err=%v", query, axis, hits, err)
		}
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	_, err = engine.Query(ctx, path, "readme", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Query after delete = %v, want ErrUnavailable", err)
	}
	if _, err := os.Stat(filepath.Join(engine.Dir)); err != nil {
		t.Fatalf("index directory should remain: %v", err)
	}
}

func TestAcousticEngineExactLookupAndUnavailableAfterDelete(t *testing.T) {
	ctx := context.Background()
	engine := &Engine{Dir: t.TempDir()}
	path, err := engine.BuildAcoustic(ctx, "idx_testacoustic00000000000000000", []AcousticDocument{{
		SubjectID:   "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Fingerprint: "fix1:abcd",
		Algorithm:   "fixture-v1",
		Path:        "song.mp3",
		Name:        "song.mp3",
		EntryType:   "REGULAR_FILE",
		ContentID:   "sha256:abc",
	}})
	if err != nil {
		t.Fatalf("BuildAcoustic: %v", err)
	}
	hits, err := engine.QueryAcoustic(ctx, path, "FIX1:ABCD")
	if err != nil {
		t.Fatalf("QueryAcoustic: %v", err)
	}
	if len(hits) != 1 || hits[0].SubjectID != "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("hits = %+v", hits)
	}
	hits, err = engine.QueryAcoustic(ctx, path, "fix1:ffff")
	if err != nil || len(hits) != 0 {
		t.Fatalf("miss hits = %+v err=%v", hits, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	_, err = engine.QueryAcoustic(ctx, path, "fix1:abcd")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("QueryAcoustic after delete = %v", err)
	}
}

func TestGraphEngineRelationLookup(t *testing.T) {
	ctx := context.Background()
	engine := &Engine{Dir: t.TempDir()}
	path, err := engine.BuildGraph(ctx, "idx_testgraph000000000000000000000", []GraphEdge{
		{SubjectID: "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Relation: RelArtist, Value: "Example Artist", Path: "song.mp3", Name: "song.mp3", EntryType: "REGULAR_FILE"},
		{SubjectID: "nse_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Relation: RelContains, Value: "nsr_cccccccccccccccccccccccccccccccc", Path: "docs/note.txt", Name: "note.txt", EntryType: "REGULAR_FILE"},
	})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	hits, err := engine.QueryGraph(ctx, path, "artist:Example Artist")
	if err != nil || len(hits) != 1 || hits[0].SubjectID != "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("artist hits = %+v %v", hits, err)
	}
	hits, err = engine.QueryGraph(ctx, path, "contains:nsr_cccccccccccccccccccccccccccccccc")
	if err != nil || len(hits) != 1 || hits[0].Name != "note.txt" {
		t.Fatalf("contains hits = %+v %v", hits, err)
	}
	_, err = engine.QueryGraph(ctx, path, "lyrics:foo")
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("unknown relation = %v", err)
	}
}

func TestTokenEngineExactLookup(t *testing.T) {
	ctx := context.Background()
	engine := &Engine{Dir: t.TempDir()}
	token := fixtureToken("sem1", "quarterly experiment report")
	path, err := engine.BuildTokens(ctx, "idx_testtokens0000000000000000000", []TokenDocument{{
		SubjectID: "nse_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Token:     token,
		Space:     "fixture-text-v1",
		Path:      "note.txt",
		Name:      "note.txt",
		EntryType: "REGULAR_FILE",
		ContentID: "sha256:abc",
	}})
	if err != nil {
		t.Fatalf("BuildTokens: %v", err)
	}
	hits, err := engine.QueryTokens(ctx, path, queryToken(DimensionSemantic, "quarterly experiment report"))
	if err != nil || len(hits) != 1 {
		t.Fatalf("QueryTokens = %+v %v", hits, err)
	}
}

func containsAxis(axes []string, want string) bool {
	for _, axis := range axes {
		if axis == want {
			return true
		}
	}
	return false
}
