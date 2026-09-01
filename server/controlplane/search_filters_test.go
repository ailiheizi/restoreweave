package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestProjectSearchSegmentsPreservesAnnotationProvenance(t *testing.T) {
	projected := projectSearchSegments([]search.SegmentRef{{
		SourceType: "ANNOTATION", SourceID: "ann_0123456789abcdef0123456789abcdef",
		SegmentID: "ann_0123456789abcdef0123456789abcdef", MatchedText: "durable note",
		Kind: "NOTE", Producer: "USER", Accepted: true, Language: "und",
	}})
	if len(projected) != 1 || projected[0].DescriptionDocumentID != "" ||
		projected[0].SourceType != "ANNOTATION" || projected[0].SourceID != "ann_0123456789abcdef0123456789abcdef" ||
		projected[0].MatchedText != "durable note" {
		t.Fatalf("annotation segment projection = %+v", projected)
	}
}

func TestAuthorizeHitsReconstructsDisplayPathForProviderHit(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock")
	hits := dispatcher.authorizeHits(context.Background(), seed.WorkspaceID, "", []search.Hit{{
		SubjectID: seed.FileEntryID,
	}}, nil)
	if len(hits) != 1 || hits[0].Path != "Music/\\xfftrack.flac" {
		t.Fatalf("authorized provider hit path = %+v", hits)
	}
}

// TestSearchQueryTypedStructuredFilters proves search.query accepts typed
// structured filters and narrows lexical results through the dispatcher,
// while an unmatched constraint excludes the subject.
func TestSearchQueryTypedStructuredFilters(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))

	root := t.TempDir()
	payload := []byte("quarterly experiment report")
	if err := writeFile(filepath.Join(root, "docs", "quarterly-report.txt"), payload); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := writeFile(filepath.Join(root, "docs", "quarterly-untyped.txt"), []byte("quarterly file without a description")); err != nil {
		t.Fatalf("write untyped fixture: %v", err)
	}
	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	resolved := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceResolve, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
		"path":         "docs/quarterly-report.txt",
	}))
	if resolved.Status != command.StatusSucceeded {
		t.Fatalf("namespace.resolve = %q: %+v", resolved.Status, resolved.Reasons)
	}
	var resolvedData command.NamespaceResolveData
	if err := json.Unmarshal(resolved.Data, &resolvedData); err != nil {
		t.Fatalf("decode namespace.resolve: %v", err)
	}
	described := dispatcher.Handle(ctx, mustEnvelope(t, command.OpDescriptionCreate, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"subject_ref":  resolvedData.PathRef,
		"kind":         "USER",
		"language":     "EN",
		"body":         "quarterly report in English",
	}))
	if described.Status != command.StatusSucceeded {
		t.Fatalf("description.create = %q: %+v", described.Status, described.Reasons)
	}

	entryTypeFilter := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionLexical,
		"query":        "quarterly",
		"filters": map[string]any{
			"entry_type":      "REGULAR_FILE",
			"size_min":        1,
			"size_max":        1 << 20,
			"mtime_before":    1893456000000,
			"protection_mode": "STORE_EXACT",
		},
	}))
	if entryTypeFilter.Status != command.StatusSucceeded {
		t.Fatalf("filtered search = %q: %+v", entryTypeFilter.Status, entryTypeFilter.Reasons)
	}
	var data command.SearchQueryData
	if err := json.Unmarshal(entryTypeFilter.Data, &data); err != nil {
		t.Fatalf("decode filtered search: %v", err)
	}
	if len(data.Hits) != 2 {
		t.Fatalf("filtered hits = %+v", data.Hits)
	}

	languageMatch := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionLexical,
		"query":        "quarterly",
		"filters": map[string]any{
			"language": "en",
		},
	}))
	if languageMatch.Status != command.StatusSucceeded {
		t.Fatalf("language-filtered search = %q: %+v", languageMatch.Status, languageMatch.Reasons)
	}
	var languageData command.SearchQueryData
	if err := json.Unmarshal(languageMatch.Data, &languageData); err != nil {
		t.Fatalf("decode language-filtered search: %v", err)
	}
	if len(languageData.Hits) != 1 || languageData.Hits[0].SubjectRef != resolvedData.Entry.SubjectRef || languageData.Hits[0].EntryID != resolvedData.PathRef {
		t.Fatalf("language-filtered hits = %+v, want only described subject %s", languageData.Hits, resolvedData.Entry.SubjectRef)
	}

	missingLanguage := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionLexical,
		"query":        "quarterly",
		"filters": map[string]any{
			"language": "fr",
		},
	}))
	if missingLanguage.Status != command.StatusSucceeded {
		t.Fatalf("missing-language search = %q: %+v", missingLanguage.Status, missingLanguage.Reasons)
	}
	var missingLanguageData command.SearchQueryData
	if err := json.Unmarshal(missingLanguage.Data, &missingLanguageData); err != nil {
		t.Fatalf("decode missing-language search: %v", err)
	}
	if len(missingLanguageData.Hits) != 0 {
		t.Fatalf("missing-language hits = %+v, want zero", missingLanguageData.Hits)
	}

	// A suffix that does not exist must fail closed with zero hits.
	noMatch := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionLexical,
		"query":        "quarterly",
		"filters": map[string]any{
			"suffix": "zzz",
		},
	}))
	if noMatch.Status != command.StatusSucceeded {
		t.Fatalf("no-match search = %q: %+v", noMatch.Status, noMatch.Reasons)
	}
	var noData command.SearchQueryData
	if err := json.Unmarshal(noMatch.Data, &noData); err != nil {
		t.Fatalf("decode no-match search: %v", err)
	}
	if len(noData.Hits) != 0 {
		t.Fatalf("suffix-mismatch hits = %+v", noData.Hits)
	}

	// A filters-only request (no free text) must still be valid.
	filtersOnly := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionLexical,
		"filters": map[string]any{
			"entry_type": "REGULAR_FILE",
		},
	}))
	if filtersOnly.Status != command.StatusSucceeded {
		t.Fatalf("filters-only search = %q: %+v", filtersOnly.Status, filtersOnly.Reasons)
	}

	// Invalid filter ranges must fail closed as invalid input.
	invalid := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"filters": map[string]any{
			"size_min": 100,
			"size_max": 10,
		},
	}))
	if invalid.Status != command.StatusFailed || !hasReasonCode(invalid, ReasonCodeInvalidInput) {
		t.Fatalf("invalid range = %q: %+v", invalid.Status, invalid.Reasons)
	}
}

func writeFile(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func TestSearchQueryDescriptionSegmentProvenanceProjectsForNormalAndFusedHits(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))

	root := t.TempDir()
	if err := writeFile(filepath.Join(root, "provenance.txt"), []byte("description source")); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	resolved := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceResolve, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
		"path":         "provenance.txt",
	}))
	if resolved.Status != command.StatusSucceeded {
		t.Fatalf("namespace.resolve = %q: %+v", resolved.Status, resolved.Reasons)
	}
	var resolvedData command.NamespaceResolveData
	if err := json.Unmarshal(resolved.Data, &resolvedData); err != nil {
		t.Fatalf("decode namespace.resolve: %v", err)
	}
	described := dispatcher.Handle(ctx, mustEnvelope(t, command.OpDescriptionCreate, map[string]any{
		"workspace_id":     ingestData.WorkspaceID,
		"subject_ref":      resolvedData.PathRef,
		"kind":             "AI_SUMMARY",
		"language":         "zh",
		"producer_profile": "model:summary-v1",
		"accepted":         false,
		"body":             "taggedfused tagged fused segment",
	}))
	if described.Status != command.StatusSucceeded {
		t.Fatalf("description.create = %q: %+v", described.Status, described.Reasons)
	}
	var descriptionData command.DescriptionCreateData
	if err := json.Unmarshal(described.Data, &descriptionData); err != nil {
		t.Fatalf("decode description.create: %v", err)
	}
	if len(descriptionData.Document.Segments) != 1 {
		t.Fatalf("description segments = %+v", descriptionData.Document.Segments)
	}
	expectedDocumentID := descriptionData.Document.ID
	expectedSegment := descriptionData.Document.Segments[0]

	// A graph tag makes the same subject eligible for the fused query while the
	// description text supplies the segment provenance through lexical search.
	tagged := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"subject_ref":  resolvedData.PathRef,
		"kind":         "TAG",
		"body":         "fused",
	}))
	if tagged.Status != command.StatusSucceeded {
		t.Fatalf("annotation.upsert = %q: %+v", tagged.Status, tagged.Reasons)
	}

	assertSegment := func(t *testing.T, hit command.SearchHitData) {
		t.Helper()
		if len(hit.Segments) != 1 {
			t.Fatalf("search segments = %+v", hit.Segments)
		}
		segment := hit.Segments[0]
		if segment.DescriptionDocumentID != expectedDocumentID ||
			segment.SegmentID != expectedSegment.ID ||
			segment.Ordinal != expectedSegment.Ordinal ||
			segment.MatchedText != expectedSegment.Text ||
			segment.Kind != "AI_SUMMARY" ||
			segment.Producer != "model:summary-v1" ||
			segment.Accepted ||
			segment.Language != "zh" {
			t.Fatalf("segment provenance = %+v", segment)
		}
	}

	normal := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionLexical,
		"query":        "taggedfused",
	}))
	if normal.Status != command.StatusSucceeded {
		t.Fatalf("normal search = %q: %+v", normal.Status, normal.Reasons)
	}
	var normalData command.SearchQueryData
	if err := json.Unmarshal(normal.Data, &normalData); err != nil {
		t.Fatalf("decode normal search: %v", err)
	}
	if len(normalData.Hits) != 1 {
		t.Fatalf("normal hits = %+v", normalData.Hits)
	}
	assertSegment(t, normalData.Hits[0])

	fused := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"fuse":         []string{"lexical-metadata-fts", "graph-relation"},
		"query":        "tagged:fused",
	}))
	if fused.Status != command.StatusSucceeded {
		t.Fatalf("fused search = %q: %+v", fused.Status, fused.Reasons)
	}
	var fusedData command.SearchQueryData
	if err := json.Unmarshal(fused.Data, &fusedData); err != nil {
		t.Fatalf("decode fused search: %v", err)
	}
	if len(fusedData.Hits) != 1 {
		t.Fatalf("fused hits = %+v", fusedData.Hits)
	}
	assertSegment(t, fusedData.Hits[0])
}
