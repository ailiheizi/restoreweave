package search

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// integrationSemanticProvider models the narrow host/provider contract. It
// deliberately returns the same stable vector for document and query text so
// the test proves generation wiring and provenance rather than ranking quality.
type integrationSemanticProvider struct{}

func (integrationSemanticProvider) Embed(_ context.Context, req SemanticEmbeddingRequest) ([]SemanticVector, error) {
	if err := validateSemanticEmbeddingRequest(req); err != nil {
		return nil, err
	}
	results := make([]SemanticVector, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		results = append(results, SemanticVector{SubjectID: input.SubjectID, SegmentID: input.SegmentID, Vector: []float32{1, 0, 0, 0}})
	}
	return results, validateSemanticEmbeddingResults(req, results)
}

type failingSemanticProvider struct{}

func (failingSemanticProvider) Embed(context.Context, SemanticEmbeddingRequest) ([]SemanticVector, error) {
	return nil, errors.New("runtime probe failed")
}

type integrationSemanticGenerationDriver struct {
	mu     sync.Mutex
	byPath map[string][]ZvecSegment
}

func (d *integrationSemanticGenerationDriver) ZvecReady(string, string, EmbeddingGenerationManifest) bool {
	return d != nil
}

func (d *integrationSemanticGenerationDriver) tamper(path string, segment ZvecSegment) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.byPath[path] = append(d.byPath[path], segment)
}

func (d *integrationSemanticGenerationDriver) Build(_ context.Context, spec ZvecGenerationSpec, segments []ZvecSegment) (ZvecGenerationReceipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.byPath == nil {
		d.byPath = make(map[string][]ZvecSegment)
	}
	copySegments := make([]ZvecSegment, len(segments))
	copy(copySegments, segments)
	d.byPath[spec.Path] = copySegments
	return ZvecGenerationReceipt{Path: spec.Path, LibraryDigest: spec.LibraryDigest, ProfileDigest: spec.ProfileDigest, Dimension: spec.Manifest.Dimension, SegmentCount: len(segments)}, nil
}

func (d *integrationSemanticGenerationDriver) Open(_ context.Context, spec ZvecGenerationSpec) (ZvecGeneration, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	segments := append([]ZvecSegment(nil), d.byPath[spec.Path]...)
	return integrationSemanticGeneration{segments: segments, dimension: spec.Manifest.Dimension}, nil
}

func (d *integrationSemanticGenerationDriver) Coverage(_ context.Context, spec ZvecGenerationSpec) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	segments := d.byPath[spec.Path]
	ids := make([]string, 0, len(segments))
	for _, segment := range segments {
		ids = append(ids, segment.SegmentID)
	}
	return ids, nil
}

func (d *integrationSemanticGenerationDriver) remove(path, segmentID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	segments := d.byPath[path]
	filtered := segments[:0]
	for _, segment := range segments {
		if segment.SegmentID != segmentID {
			filtered = append(filtered, segment)
		}
	}
	d.byPath[path] = filtered
}

type integrationSemanticGeneration struct {
	segments  []ZvecSegment
	dimension int
}

func (g integrationSemanticGeneration) Query(_ context.Context, vector []float32, topK int) ([]ZvecHit, error) {
	if len(vector) != g.dimension {
		return nil, ErrInvalidZvecGeneration
	}
	hits := make([]ZvecHit, 0, len(g.segments))
	for _, segment := range g.segments {
		var score float32
		for i, value := range vector {
			score += value * segment.Vector[i]
		}
		hits = append(hits, ZvecHit{SubjectID: segment.SubjectID, SegmentID: segment.SegmentID, Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].SegmentID < hits[j].SegmentID
	})
	if topK < len(hits) {
		hits = hits[:topK]
	}
	return hits, nil
}

func (integrationSemanticGeneration) Close() error { return nil }

func TestIndexerSemanticRebuildQueryFuseAndProvenance(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	descriptionID := mustSearchID(t, sqlite.IDPrefixDescription)
	annotationID := mustSearchID(t, sqlite.IDPrefixAnnotation)
	segmentID := mustSearchID(t, sqlite.IDPrefixSemanticSegment)
	segmentID2 := mustSearchID(t, sqlite.IDPrefixSemanticSegment)
	if err := store.InsertDescriptionDocument(ctx, &sqlite.DescriptionDocument{
		ID: descriptionID, WorkspaceID: seed.WorkspaceID, SubjectRef: seed.FileEntryID,
		Kind: sqlite.DescriptionUser, Title: "Recovery note", Language: "en",
		Body: "A flooded city archive is ready for recovery.", SourceRef: "user:test",
		ProducerProfile: "human", Accepted: true,
	}); err != nil {
		t.Fatalf("insert description: %v", err)
	}
	if err := store.InsertSemanticSegment(ctx, &sqlite.SemanticSegment{
		ID: segmentID, WorkspaceID: seed.WorkspaceID, DocumentID: descriptionID,
		SubjectRef: seed.FileEntryID, Ordinal: 0,
		Text: "A flooded city archive is ready for recovery.", Language: "en", Section: "body",
	}); err != nil {
		t.Fatalf("insert segment: %v", err)
	}
	if err := store.InsertSemanticSegment(ctx, &sqlite.SemanticSegment{
		ID: segmentID2, WorkspaceID: seed.WorkspaceID, DocumentID: descriptionID,
		SubjectRef: seed.FileEntryID, Ordinal: 1,
		Text: "The archive has a verified exact recovery path.", Language: "en", Section: "body",
	}); err != nil {
		t.Fatalf("insert second segment: %v", err)
	}
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertAnnotation(ctx, &sqlite.Annotation{
			ID: annotationID, WorkspaceID: seed.WorkspaceID, SubjectRef: seed.FileEntryID,
			Kind: sqlite.AnnotationNote, Body: "A user note about emergency archive recovery.", Revision: 1,
		})
	}); err != nil {
		t.Fatalf("insert semantic note: %v", err)
	}

	manifest := testZvecManifest()
	driver := &integrationSemanticGenerationDriver{}
	indexer := &Indexer{
		Store: store, Engine: &Engine{Dir: t.TempDir()},
		ConfigDigest: manifest.ConfigDigest, LexicalProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1),
		SemanticManifest: manifest, SemanticProvider: integrationSemanticProvider{},
		SemanticZvec:        driver,
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	lexical, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:semantic-e2e", seed.RootID)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if lexical.Dimension != DimensionLexical {
		t.Fatalf("rebuild returned dimension %q", lexical.Dimension)
	}
	semantic, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionSemantic)
	if err != nil {
		t.Fatalf("latest semantic generation: %v", err)
	}
	if semantic.ProviderProfileDigest != manifest.CanonicalDigest() || semantic.SemanticSpace != manifest.SemanticSpace {
		t.Fatalf("semantic binding = %+v", semantic)
	}
	restartedIndexer := &Indexer{
		Store: store, Engine: indexer.Engine,
		ConfigDigest: manifest.ConfigDigest, LexicalProfileDigest: indexer.LexicalProfileDigest,
		SemanticManifest: manifest, SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: indexer.SemanticLibraryPath, SemanticLibraryDigest: indexer.SemanticLibraryDigest,
	}
	if IndexerReadiness(restartedIndexer).SemanticReal {
		t.Fatal("restarted semantic index was ready before its generation was reopened")
	}
	if err := restartedIndexer.WarmSemanticGeneration(ctx, seed.WorkspaceID); err != nil {
		t.Fatalf("warm restarted semantic generation: %v", err)
	}
	if !IndexerReadiness(restartedIndexer).SemanticReal {
		t.Fatal("restarted semantic index remained unavailable after generation warm-up")
	}

	_, hits, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "flooded city"})
	if err != nil || len(hits) != 1 || hits[0].SubjectID != seed.FileEntryID {
		t.Fatalf("semantic query = %+v, err=%v", hits, err)
	}
	if len(hits[0].Segments) != 3 {
		t.Fatalf("semantic provenance = %+v", hits[0].Segments)
	}
	seenSegments := map[string]bool{}
	for _, segment := range hits[0].Segments {
		if segment.MatchedText == "" || !segment.Accepted {
			t.Fatalf("semantic provenance = %+v", hits[0].Segments)
		}
		if segment.SourceType == "DESCRIPTION" && (segment.DescriptionDocumentID != descriptionID || segment.SourceID != descriptionID || segment.Producer != "human") {
			t.Fatalf("description provenance = %+v", segment)
		}
		if segment.SourceType == "ANNOTATION" && (segment.SourceID != annotationID || segment.Kind != string(sqlite.AnnotationNote) || segment.Producer != "USER") {
			t.Fatalf("annotation provenance = %+v", segment)
		}
		seenSegments[segment.SegmentID] = true
	}
	if !seenSegments[segmentID] || !seenSegments[segmentID2] || !seenSegments[annotationID] {
		t.Fatalf("semantic segment provenance = %+v", hits[0].Segments)
	}

	_, filtered, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "flooded", Filters: Filters{EntryType: string(sqlite.EntryFile), Language: "en"}})
	if err != nil || len(filtered) != 1 {
		t.Fatalf("filtered semantic query = %+v, err=%v", filtered, err)
	}
	_, excluded, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "flooded", Filters: Filters{EntryType: string(sqlite.EntryDirectory)}})
	if err != nil || len(excluded) != 0 {
		t.Fatalf("mismatched semantic filter = %+v, err=%v", excluded, err)
	}

	fused, err := indexer.Fuse(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Text: "flooded", Fuse: []string{DimensionLexical, DimensionSemantic}, Filters: Filters{EntryType: string(sqlite.EntryFile)}})
	if err != nil || len(fused.Hits) != 1 || len(fused.Hits[0].Dimensions) != 2 {
		t.Fatalf("fused semantic query = %+v, components=%+v, err=%v", fused.Hits, fused.Components, err)
	}
	for _, component := range fused.Components {
		if component.Status != "SUCCEEDED" {
			t.Fatalf("fused component = %+v", component)
		}
	}
	if IndexerReadiness(indexer).SemanticReal != true {
		t.Fatal("semantic provider was not reported as real and ready")
	}
	coverage, err := indexer.SemanticCoverage(ctx, seed.WorkspaceID)
	if err != nil || !coverage.Available || !coverage.Complete || coverage.Expected != 3 || coverage.Indexed != 3 || len(coverage.Missing) != 0 {
		t.Fatalf("complete semantic coverage = %+v, err=%v", coverage, err)
	}
	driver.tamper(semantic.DBPath, ZvecSegment{SubjectID: seed.FileEntryID, SegmentID: "unknown-segment", Vector: []float32{1, 0, 0, 0}})
	coverage, err = indexer.SemanticCoverage(ctx, seed.WorkspaceID)
	if err != nil || !coverage.Available || coverage.Complete || coverage.Notes != "semantic generation contains unknown segment identities" {
		t.Fatalf("unknown semantic coverage = %+v, err=%v", coverage, err)
	}
	driver.remove(semantic.DBPath, segmentID2)
	coverage, err = indexer.SemanticCoverage(ctx, seed.WorkspaceID)
	if err != nil || !coverage.Available || coverage.Complete || coverage.Expected != 3 || coverage.Indexed != 2 || len(coverage.Missing) != 1 || coverage.Missing[0] != segmentID2 {
		t.Fatalf("partial semantic coverage = %+v, err=%v", coverage, err)
	}
	// Restore the test generation before exercising provenance tampering below.
	driver.tamper(semantic.DBPath, ZvecSegment{SubjectID: seed.FileEntryID, SegmentID: segmentID2, Vector: []float32{1, 0, 0, 0}})
	driver.tamper(semantic.DBPath, ZvecSegment{SubjectID: seed.DirEntryID, SegmentID: segmentID, Vector: []float32{1, 0, 0, 0}})
	if _, _, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "flooded"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("tampered semantic provenance error = %v, want ErrUnavailable", err)
	}

	// A failed later rebuild must leave exact/lexical work intact while making
	// the prior disposable semantic generation unavailable rather than serving
	// stale vectors as if the provider were healthy.
	indexer.SemanticProvider = failingSemanticProvider{}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:semantic-e2e-next", seed.RootID); err != nil {
		t.Fatalf("lexical rebuild should survive semantic failure: %v", err)
	}
	if IndexerReadiness(indexer).SemanticReal {
		t.Fatal("semantic capability remained ready after failed rebuild")
	}
	if _, _, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "flooded"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale semantic query error = %v, want ErrUnavailable", err)
	}
	coverage, err = indexer.SemanticCoverage(ctx, seed.WorkspaceID)
	if err != nil || coverage.Available || coverage.Complete {
		t.Fatalf("degraded semantic coverage = %+v, err=%v", coverage, err)
	}
}

func TestIndexerSemanticProfileSwitchPreservesOldGenerationAndDescriptions(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	descriptionID := mustSearchID(t, sqlite.IDPrefixDescription)
	segmentID := mustSearchID(t, sqlite.IDPrefixSemanticSegment)
	document := &sqlite.DescriptionDocument{
		ID: descriptionID, WorkspaceID: seed.WorkspaceID, SubjectRef: seed.FileEntryID,
		Kind: sqlite.DescriptionUser, Language: "en", Body: "profile switch keeps durable text",
		SourceRef: "user:profile-switch", ProducerProfile: "human", Accepted: true,
	}
	if err := store.InsertDescriptionDocument(ctx, document); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSemanticSegment(ctx, &sqlite.SemanticSegment{
		ID: segmentID, WorkspaceID: seed.WorkspaceID, DocumentID: descriptionID,
		SubjectRef: seed.FileEntryID, Ordinal: 0, Text: document.Body, Language: "en", Section: "body",
	}); err != nil {
		t.Fatal(err)
	}
	manifestA := testZvecManifest()
	driver := &integrationSemanticGenerationDriver{}
	indexer := &Indexer{
		Store: store, Engine: &Engine{Dir: t.TempDir()}, ConfigDigest: manifestA.ConfigDigest,
		LexicalProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1), SemanticManifest: manifestA,
		SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:profile-a", seed.RootID); err != nil {
		t.Fatal(err)
	}
	generationA, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionSemantic)
	if err != nil {
		t.Fatal(err)
	}
	pathA := generationA.DBPath
	bodyDigest := document.BodyDigest

	manifestB := manifestA
	manifestB.ModelDigest = "sha256:" + strings.Repeat("7", 64)
	manifestB.ConfigDigest = "sha256:" + strings.Repeat("8", 64)
	indexer.ConfigDigest = manifestB.ConfigDigest
	indexer.SemanticManifest = manifestB
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:profile-b", seed.RootID); err != nil {
		t.Fatal(err)
	}
	generationB, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionSemantic)
	if err != nil {
		t.Fatal(err)
	}
	if generationA.ID == generationB.ID || generationA.DBPath == generationB.DBPath || generationA.ProviderProfileDigest == generationB.ProviderProfileDigest {
		t.Fatalf("profile switch reused generation: A=%+v B=%+v", generationA, generationB)
	}
	if _, ok := driver.byPath[pathA]; !ok {
		t.Fatal("old zvec generation was removed")
	}
	if _, ok := driver.byPath[generationB.DBPath]; !ok || len(driver.byPath) != 2 {
		t.Fatalf("zvec generation paths = %d, want 2", len(driver.byPath))
	}
	readback, err := store.GetDescriptionDocument(ctx, seed.WorkspaceID, descriptionID)
	if err != nil || readback.BodyDigest != bodyDigest || readback.Body != document.Body {
		t.Fatalf("durable description changed after profile switch: %+v, err=%v", readback, err)
	}

	oldIndexer := &Indexer{
		Store: store, Engine: indexer.Engine, ConfigDigest: manifestA.ConfigDigest,
		LexicalProfileDigest: indexer.LexicalProfileDigest, SemanticManifest: manifestA,
		SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: indexer.SemanticLibraryPath, SemanticLibraryDigest: indexer.SemanticLibraryDigest,
	}
	if _, hits, err := oldIndexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, GenerationID: generationA.ID, Dimension: DimensionSemantic, Text: "profile"}); err != nil || len(hits) != 1 {
		t.Fatalf("old profile generation query = %+v, err=%v", hits, err)
	}
	if _, hits, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, GenerationID: generationB.ID, Dimension: DimensionSemantic, Text: "profile"}); err != nil || len(hits) != 1 {
		t.Fatalf("new profile generation query = %+v, err=%v", hits, err)
	}
}
