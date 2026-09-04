package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestFailedRebuildRevokesLexicalReadinessUntilSuccessfulRetry(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	engineDir := t.TempDir()
	indexer := &Indexer{Store: store, Engine: &Engine{Dir: engineDir}}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:lexical-health", seed.RootID); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	if indexer.LexicalUnavailable() || indexer.LexicalFailure() != "" {
		t.Fatal("successful rebuild left lexical health failed")
	}

	brokenDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(brokenDir, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	indexer.Engine.Dir = brokenDir
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:lexical-failed", seed.RootID); err == nil {
		t.Fatal("failed rebuild returned nil error")
	}
	if !indexer.LexicalUnavailable() || indexer.LexicalFailure() == "" {
		t.Fatal("failed rebuild did not revoke lexical readiness")
	}
	// The previously published FTS file remains present, but its stale
	// generation must not be advertised as current while the latest rebuild is
	// known to have failed.
	indexer.Engine.Dir = engineDir
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:lexical-retry", seed.RootID); err != nil {
		t.Fatalf("successful retry: %v", err)
	}
	if indexer.LexicalUnavailable() || indexer.LexicalFailure() != "" {
		t.Fatal("successful retry did not clear lexical health failure")
	}
}

func TestLexicalFailureIsScopedToWorkspace(t *testing.T) {
	indexer := &Indexer{}
	indexer.setLexicalWorkspaceFailure("workspace:failed", "engine unavailable")

	if !indexer.LexicalUnavailableForWorkspace("workspace:failed") {
		t.Fatal("failed workspace was not marked unavailable")
	}
	if got := indexer.LexicalFailureForWorkspace("workspace:failed"); got != "engine unavailable" {
		t.Fatalf("failure = %q, want engine unavailable", got)
	}
	if indexer.LexicalUnavailableForWorkspace("workspace:healthy") {
		t.Fatal("failure leaked into another workspace")
	}
	if got := indexer.LexicalFailureForWorkspace("workspace:healthy"); got != "" {
		t.Fatalf("healthy workspace failure = %q, want empty", got)
	}

	indexer.setLexicalWorkspaceFailure("workspace:failed", "")
	if indexer.LexicalUnavailableForWorkspace("workspace:failed") {
		t.Fatal("successful retry did not clear workspace failure")
	}
}

func TestSemanticQueryCannotRestoreReadinessFromPartialHits(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	manifest := testZvecManifest()
	driver := &integrationSemanticGenerationDriver{}
	indexer := &Indexer{
		Store: store, Engine: &Engine{Dir: t.TempDir()}, ConfigDigest: manifest.ConfigDigest,
		LexicalProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1), SemanticManifest: manifest,
		SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:semantic-query-gate", seed.RootID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	generation, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionSemantic)
	if err != nil {
		t.Fatalf("latest semantic generation: %v", err)
	}
	// Add an unbound backend row. A query may still return valid rows, but this
	// local hit must not re-authorize the tampered generation.
	driver.tamper(generation.DBPath, ZvecSegment{SubjectID: seed.FileEntryID, SegmentID: "unknown-segment", Vector: []float32{1, 0, 0, 0}})
	if _, _, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "seed"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("partial semantic query error = %v, want ErrUnavailable", err)
	}
	if indexer.semanticIndexReady.Load() || IndexerReadiness(indexer).SemanticReal {
		t.Fatal("partial semantic query restored semantic readiness")
	}
}

func TestSemanticQueryUsesFrozenGenerationCoverageCache(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	manifest := testZvecManifest()
	driver := &integrationSemanticGenerationDriver{}
	indexer := &Indexer{
		Store: store, Engine: &Engine{Dir: t.TempDir()}, ConfigDigest: manifest.ConfigDigest,
		LexicalProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1), SemanticManifest: manifest,
		SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:semantic-frozen", seed.RootID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	generation, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionSemantic)
	if err != nil {
		t.Fatalf("latest semantic generation: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, GenerationID: generation.ID, Dimension: DimensionSemantic, Text: "seed"}); err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
	}
	if calls := driver.coverageProbeCalls(); calls != 1 {
		t.Fatalf("coverage probe calls after cached queries = %d, want 1", calls)
	}
	annotationID := mustSearchID(t, sqlite.IDPrefixAnnotation)
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertAnnotation(ctx, &sqlite.Annotation{ID: annotationID, WorkspaceID: seed.WorkspaceID, SubjectRef: seed.FileEntryID, Kind: sqlite.AnnotationNote, Body: "new note after frozen generation", Revision: 1})
	}); err != nil {
		t.Fatalf("insert later note: %v", err)
	}
	if _, _, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, GenerationID: generation.ID, Dimension: DimensionSemantic, Text: "seed"}); err != nil {
		t.Fatalf("query after later note: %v", err)
	}
	if calls := driver.coverageProbeCalls(); calls != 1 {
		t.Fatalf("coverage probe calls after later note = %d, want 1", calls)
	}
}

func TestIndexerFeedsDurableFactsProtectionAndDescriptions(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	representationID := mustSearchID(t, sqlite.IDPrefixRepresentation)
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertRepresentation(ctx, &sqlite.Representation{
			ID: representationID, WorkspaceID: seed.WorkspaceID,
			ContentID: "sha256:file-content", DecodedLength: 16,
			OwnershipMode: sqlite.OwnershipEngineManaged, CodecProfileRef: "test-raw/v1",
			AccessMode: sqlite.AccessSequentialStream, RecordDigest: "sha256:test-representation",
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertMetadataFact(ctx, &sqlite.MetadataFact{
		ID:             mustSearchID(t, sqlite.IDPrefixMetadataFact),
		WorkspaceID:    seed.WorkspaceID,
		SubjectRef:     seed.FileEntryID,
		Namespace:      "media.game",
		Key:            "platform",
		Value:          json.RawMessage(`"dreamcast"`),
		ValueType:      "string",
		AuthorityClass: "USER_CONFIRMED",
		SourceRef:      "user:test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertProtectionRecord(ctx, &sqlite.ProtectionRecord{
		ID:                    mustSearchID(t, sqlite.IDPrefixProtectionRecord),
		WorkspaceID:           seed.WorkspaceID,
		SubjectRef:            seed.FileEntryID,
		Mode:                  sqlite.ProtectionStoreExact,
		Outcome:               sqlite.ProtectionExactProtected,
		ExpectedContentID:     "sha256:file-content",
		LocalRepresentationID: representationID,
		LastVerificationRef:   "readback:sha256:file-content",
	}); err != nil {
		t.Fatal(err)
	}
	descriptionID := mustSearchID(t, sqlite.IDPrefixDescription)
	segmentID := mustSearchID(t, sqlite.IDPrefixSemanticSegment)
	if err := store.InsertDescriptionDocument(ctx, &sqlite.DescriptionDocument{
		ID:              descriptionID,
		WorkspaceID:     seed.WorkspaceID,
		SubjectRef:      seed.FileEntryID,
		Kind:            sqlite.DescriptionUser,
		Title:           "Personal review",
		Language:        "en",
		Body:            "A melancholy adventure about rebuilding a flooded city",
		SourceRef:       "user:test",
		ProducerProfile: "human",
		Accepted:        true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSemanticSegment(ctx, &sqlite.SemanticSegment{
		ID:          segmentID,
		WorkspaceID: seed.WorkspaceID,
		DocumentID:  descriptionID,
		SubjectRef:  seed.FileEntryID,
		Ordinal:     0,
		Text:        "A melancholy adventure about rebuilding a flooded city",
		Language:    "en",
		Section:     "body",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertProcessorAttempt(ctx, &sqlite.ProcessorAttempt{
		ID:              mustSearchID(t, sqlite.IDPrefixAttempt),
		WorkspaceID:     seed.WorkspaceID,
		SubjectRef:      seed.FileEntryID,
		SnapshotRef:     "snapshot:test",
		RouteDigest:     "sha256:route",
		Route:           json.RawMessage(`{"kind":"PROCESSING"}`),
		Stage:           "EXTRACT",
		CapabilityID:    "extract.text.v1",
		Status:          "SUCCEEDED",
		ReasonCode:      "PROCESSOR_STAGE_SUCCEEDED",
		Reason:          "",
		Provenance:      json.RawMessage(`{}`),
		FenceToken:      1,
		ProcessorDigest: "sha256:proc",
	}); err != nil {
		t.Fatal(err)
	}

	configDigest := "sha256:config-rebuild"
	lexicalProfileDigest := ProfileDigest(DimensionLexical, LexicalProfileV1)
	indexer := &Indexer{
		Store:                store,
		Engine:               &Engine{Dir: t.TempDir()},
		ConfigDigest:         configDigest,
		LexicalProfileDigest: lexicalProfileDigest,
	}
	generation, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:test", seed.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if generation.ConfigDigest != configDigest || generation.ProviderProfileDigest != lexicalProfileDigest {
		t.Fatalf("rebuilt generation binding = %+v", generation)
	}
	readback, err := store.GetIndexGeneration(ctx, generation.ID)
	if err != nil {
		t.Fatalf("read rebuilt generation: %v", err)
	}
	if readback.ConfigDigest != configDigest || readback.ProviderProfileDigest != lexicalProfileDigest {
		t.Fatalf("readback generation binding = %+v", readback)
	}
	for query, axis := range map[string]string{
		"dreamcast":  AxisMetadata,
		"PROTECTED":  AxisProtection,
		"melancholy": AxisDescriptions,
		"sha256":     AxisChecksum,
		"EN":         AxisLanguage,
		"SUCCEEDED":  AxisProcessing,
		"EXTRACT":    AxisProcessing,
	} {
		hits, err := indexer.Engine.Query(ctx, generation.DBPath, query, []string{axis})
		if err != nil || len(hits) != 1 || hits[0].SubjectID != seed.FileEntryID {
			t.Fatalf("query %q on %s = %+v, err=%v", query, axis, hits, err)
		}
	}

	hits, err := indexer.Engine.Query(ctx, generation.DBPath, "flooded", nil)
	if err != nil || len(hits) != 1 || len(hits[0].Segments) != 1 {
		t.Fatalf("segment provenance = %+v err=%v", hits, err)
	}
	if ref := hits[0].Segments[0]; ref.DescriptionDocumentID != descriptionID || ref.SegmentID != segmentID ||
		ref.Kind != "USER" || !ref.Accepted {
		t.Fatalf("segment ref = %+v", ref)
	}

	// A protection-mode filter against the STORE_EXACT record must keep the
	// file, and a LINK_ONLY filter must exclude it honestly.
	kept, err := indexer.Engine.QueryFiltered(ctx, generation.DBPath, "", nil, Filters{
		ProtectionMode: "STORE_EXACT",
	})
	if err != nil || len(kept) != 1 || kept[0].SubjectID != seed.FileEntryID {
		t.Fatalf("protection filter kept = %+v err=%v", kept, err)
	}
	excluded, err := indexer.Engine.QueryFiltered(ctx, generation.DBPath, "", nil, Filters{
		ProtectionMode: "LINK_ONLY",
	})
	if err != nil || len(excluded) != 0 {
		t.Fatalf("protection filter excluded = %+v err=%v", excluded, err)
	}

	coverage, err := indexer.Coverage(ctx, seed.WorkspaceID)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if !coverage.Available || !coverage.Fields[AxisProtection] || !coverage.Fields[AxisDescriptions] ||
		!coverage.Fields[AxisProcessing] || !coverage.Fields[AxisLanguage] {
		t.Fatalf("coverage = %+v", coverage)
	}
	if coverage.Fields[AxisMtime] {
		t.Fatalf("mtime falsely present: %+v", coverage.Fields)
	}
	if !coverage.Fields[AxisSize] {
		t.Fatalf("seed carries a logical size so the size facet must be present: %+v", coverage.Fields)
	}
}

func TestIndexerRebuildTokensPersistsSemanticBinding(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	nodes, err := store.ListNamespaceSubtree(ctx, seed.WorkspaceID, seed.RootID, "")
	if err != nil {
		t.Fatalf("list namespace: %v", err)
	}
	byID := make(map[string]sqlite.NamespaceEntry, len(nodes))
	for _, node := range nodes {
		byID[node.Entry.ID] = node.Entry
	}
	space := "semantic-space-bound-v1"
	profileDigest := ProfileDigest(DimensionSemantic, "provider-profile-bound-v1")
	indexer := &Indexer{
		Store:                 store,
		Engine:                &Engine{Dir: t.TempDir()},
		ConfigDigest:          "sha256:config-semantic",
		SemanticProfileDigest: profileDigest,
		SemanticSpace:         space,
	}
	token := fixtureToken("sem1", "query")
	body, err := json.Marshal(map[string]any{
		"space": space, "token": token, "not_content_identity": true,
	})
	if err != nil {
		t.Fatalf("marshal feature artifact: %v", err)
	}
	generation, err := indexer.rebuildTokens(ctx, seed.WorkspaceID, "snapshot:semantic", seed.RootID,
		byID, []sqlite.ProcessorArtifact{{
			SubjectRef: seed.FileEntryID, Stage: "ENRICH",
			CapabilityID: "embed.text.fixture.v1", Body: string(body),
		}}, DimensionSemantic, "ENRICH", "embed.text.fixture.v1")
	if err != nil {
		t.Fatalf("rebuild semantic tokens: %v", err)
	}
	if generation.ConfigDigest != indexer.ConfigDigest ||
		generation.ProviderProfileDigest != profileDigest || generation.SemanticSpace != space {
		t.Fatalf("rebuilt semantic binding = %+v", generation)
	}
	readback, err := store.GetIndexGeneration(ctx, generation.ID)
	if err != nil {
		t.Fatalf("read semantic generation: %v", err)
	}
	if readback.ConfigDigest != indexer.ConfigDigest ||
		readback.ProviderProfileDigest != profileDigest || readback.SemanticSpace != space {
		t.Fatalf("readback semantic binding = %+v", readback)
	}
}

func TestIndexerMultimodalRealBindingFailsClosedWithoutProvider(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	engine := &Engine{Dir: t.TempDir()}
	binding := FixtureIndexBinding("sha256:config-multimodal")
	manifest := binding.MultimodalManifest
	manifest.SemanticSpace = "real-multimodal-space-v1"
	manifest.ProviderDigest = ProfileDigest(DimensionMultimodal, "real-multimodal-provider-v1")
	manifestDigest := manifest.CanonicalDigest()

	generationID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	dbPath, err := engine.BuildTokens(ctx, generationID, []TokenDocument{{
		SubjectID: seed.FileEntryID,
		Token:     fixtureToken("multimodal", "query"),
		Space:     manifest.SemanticSpace,
		Path:      "query.txt",
		Name:      "query.txt",
		EntryType: string(sqlite.EntryFile),
	}})
	if err != nil {
		t.Fatalf("build multimodal projection: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: generationID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:multimodal",
		NamespaceRootID: seed.RootID, DBPath: dbPath, Dimension: DimensionMultimodal,
		ConfigDigest: binding.ConfigDigest, ProviderProfileDigest: manifestDigest,
		SemanticSpace: manifest.SemanticSpace,
	}); err != nil {
		t.Fatalf("insert multimodal generation: %v", err)
	}

	indexer := &Indexer{
		Store: store, Engine: engine, ConfigDigest: binding.ConfigDigest,
		MultimodalManifest: manifest,
	}
	_, _, err = indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, GenerationID: generationID,
		Dimension: DimensionMultimodal, Text: "query",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("real multimodal query without provider err=%v, want ErrUnavailable", err)
	}
}

func mustSearchID(t *testing.T, prefix string) string {
	t.Helper()
	id, err := sqlite.NewStableID(prefix)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIndexerPinnedGenerationRequiresNonEmptyMatchingWorkspace(t *testing.T) {
	ctx := context.Background()
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	store := testutil.OpenStore(t, catalogPath)
	seed := testutil.SeedNamespace(t, store)
	engine := &Engine{Dir: t.TempDir()}
	generationID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	dbPath, err := engine.Build(ctx, generationID, []Document{{
		SubjectID: seed.FileEntryID,
		Name:      "query.txt",
		EntryType: string(sqlite.EntryFile),
	}})
	if err != nil {
		t.Fatalf("build lexical: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: generationID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:workspace-bound",
		NamespaceRootID: seed.RootID, DBPath: dbPath, Dimension: DimensionLexical,
	}); err != nil {
		t.Fatalf("insert lexical generation: %v", err)
	}

	// Simulate a malformed legacy or externally corrupted catalog row. The
	// supported writer rejects an empty workspace, but a pinned query must
	// still fail closed if one is encountered during readback.
	rawDB, err := sql.Open("sqlite", catalogPath)
	if err != nil {
		t.Fatalf("open raw catalog: %v", err)
	}
	defer rawDB.Close()
	if _, err := rawDB.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable raw foreign keys: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
UPDATE index_generations SET workspace_id = '' WHERE generation_id = ?`, generationID); err != nil {
		t.Fatalf("corrupt generation workspace: %v", err)
	}

	if _, _, err := (&Indexer{Store: store, Engine: engine}).Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, GenerationID: generationID,
		Dimension: DimensionLexical, Text: "query",
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty generation workspace error = %v, want ErrUnavailable", err)
	}
}

func TestIndexerEmptyProviderCandidatesRequireUsableLexicalAuthority(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	engine := &Engine{Dir: t.TempDir()}

	lexicalID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	lexicalPath, err := engine.Build(ctx, lexicalID, []Document{{
		SubjectID: seed.FileEntryID,
		Name:      "query.txt",
		EntryType: string(sqlite.EntryFile),
	}})
	if err != nil {
		t.Fatalf("build lexical: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: lexicalID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:filter-authority",
		NamespaceRootID: seed.RootID, DBPath: lexicalPath, Dimension: DimensionLexical,
	}); err != nil {
		t.Fatalf("insert lexical generation: %v", err)
	}

	semanticID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	semanticPath, err := engine.BuildTokens(ctx, semanticID, []TokenDocument{{
		SubjectID: seed.FileEntryID,
		Token:     fixtureToken("sem1", "query"),
		Space:     SemanticFixtureProfileV1,
		Name:      "query.txt",
		EntryType: string(sqlite.EntryFile),
	}})
	if err != nil {
		t.Fatalf("build semantic: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: semanticID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:filter-authority",
		NamespaceRootID: seed.RootID, DBPath: semanticPath, Dimension: DimensionSemantic,
	}); err != nil {
		t.Fatalf("insert semantic generation: %v", err)
	}
	indexer := &Indexer{Store: store, Engine: engine, EnableFixtureDimensions: true}
	query := QueryRequest{
		WorkspaceID: seed.WorkspaceID, GenerationID: semanticID,
		Dimension: DimensionSemantic, Text: "no-provider-candidate",
		Filters: Filters{EntryType: string(sqlite.EntryFile)},
	}

	if err := engine.RemoveFile(lexicalPath); err != nil {
		t.Fatalf("remove lexical projection: %v", err)
	}
	if _, _, err := indexer.Query(ctx, query); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty candidates with missing lexical authority error = %v, want ErrUnavailable", err)
	}

	wrongSchemaID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	wrongSchemaPath, err := engine.BuildTokens(ctx, wrongSchemaID, nil)
	if err != nil {
		t.Fatalf("build wrong-schema projection: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: mustSearchID(t, sqlite.IDPrefixIndexGeneration), WorkspaceID: seed.WorkspaceID,
		SnapshotRef: "snapshot:filter-authority", NamespaceRootID: seed.RootID,
		DBPath: wrongSchemaPath, Dimension: DimensionLexical, CreatedAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("insert wrong-schema lexical generation: %v", err)
	}
	if _, _, err := indexer.Query(ctx, query); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty candidates with wrong-schema lexical authority error = %v, want ErrUnavailable", err)
	}
}

func TestIndexerNonLexicalQueriesUseLexicalFilterAllowlist(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	engine := &Engine{Dir: t.TempDir()}

	lexicalID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	logicalSize := int64(16)
	lexicalDocs := make([]Document, 0, 1002)
	for i := 0; i < 1001; i++ {
		lexicalDocs = append(lexicalDocs, Document{
			SubjectID: fmt.Sprintf("filler-%04d", i),
			Path:      fmt.Sprintf("filler/%04d.txt", i),
			Name:      fmt.Sprintf("%04d.txt", i),
			Suffix:    "txt",
			EntryType: string(sqlite.EntryFile),
		})
	}
	lexicalDocs = append(lexicalDocs, Document{
		SubjectID:    seed.FileEntryID,
		Path:         "Music/query.flac",
		Name:         "query.flac",
		Suffix:       "flac",
		EntryType:    string(sqlite.EntryFile),
		ContentID:    "sha256:file-content",
		LogicalSize:  &logicalSize,
		Descriptions: "query",
	})
	lexicalPath, err := engine.Build(ctx, lexicalID, lexicalDocs)
	if err != nil {
		t.Fatalf("build lexical: %v", err)
	}
	lexicalGeneration := &sqlite.IndexGeneration{
		ID: lexicalID, WorkspaceID: seed.WorkspaceID, DBPath: lexicalPath,
		NamespaceRootID: seed.RootID, SnapshotRef: "snapshot:test",
		Dimension: DimensionLexical,
	}
	if err := store.InsertIndexGeneration(ctx, lexicalGeneration); err != nil {
		t.Fatalf("insert lexical generation: %v", err)
	}

	semanticID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	semanticPath, err := engine.BuildTokens(ctx, semanticID, []TokenDocument{{
		SubjectID: seed.FileEntryID,
		Token:     fixtureToken("sem1", "query"),
		Space:     "semantic-fixture-v1",
		Path:      "Music/query.flac",
		Name:      "query.flac",
		EntryType: string(sqlite.EntryFile),
		ContentID: "sha256:file-content",
	}})
	if err != nil {
		t.Fatalf("build semantic: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: semanticID, WorkspaceID: seed.WorkspaceID, DBPath: semanticPath,
		NamespaceRootID: seed.RootID, SnapshotRef: "snapshot:test",
		Dimension: DimensionSemantic,
	}); err != nil {
		t.Fatalf("insert semantic generation: %v", err)
	}

	graphID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	graphPath, err := engine.BuildGraph(ctx, graphID, []GraphEdge{{
		SubjectID: seed.FileEntryID,
		Relation:  RelArtist,
		Value:     "Example Artist",
		Path:      "Music/query.flac",
		Name:      "query.flac",
		EntryType: string(sqlite.EntryFile),
		ContentID: "sha256:file-content",
	}})
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: graphID, WorkspaceID: seed.WorkspaceID, DBPath: graphPath,
		NamespaceRootID: seed.RootID, SnapshotRef: "snapshot:test",
		Dimension: DimensionGraph,
	}); err != nil {
		t.Fatalf("insert graph generation: %v", err)
	}

	indexer := &Indexer{Store: store, Engine: engine, EnableFixtureDimensions: true}
	wrong := Filters{EntryType: string(sqlite.EntryDirectory)}
	right := Filters{EntryType: string(sqlite.EntryFile)}

	_, hits, err := indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic,
		Text: "query", Filters: wrong,
	})
	if err != nil {
		t.Fatalf("semantic mismatch query: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("semantic mismatch hits = %+v", hits)
	}
	_, hits, err = indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic,
		Text: "query", Filters: right,
	})
	if err != nil || len(hits) != 1 || hits[0].SubjectID != seed.FileEntryID {
		t.Fatalf("semantic matching hits = %+v err=%v", hits, err)
	}
	_, hits, err = indexer.Query(ctx, QueryRequest{
		GenerationID: semanticID, Dimension: DimensionSemantic,
		Text: "query", Filters: right,
	})
	if err != nil || len(hits) != 1 || hits[0].SubjectID != seed.FileEntryID {
		t.Fatalf("semantic generation-fallback hits = %+v err=%v", hits, err)
	}

	_, hits, err = indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, Dimension: DimensionGraph,
		Text: "artist:Example Artist", Filters: wrong,
	})
	if err != nil {
		t.Fatalf("graph mismatch query: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("graph mismatch hits = %+v", hits)
	}
	_, hits, err = indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, Dimension: DimensionGraph,
		Text: "artist:Example Artist", Filters: right,
	})
	if err != nil || len(hits) != 1 || hits[0].SubjectID != seed.FileEntryID {
		t.Fatalf("graph matching hits = %+v err=%v", hits, err)
	}

	fused, err := indexer.Fuse(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID,
		Text:        "query",
		Fuse:        []string{DimensionLexical, DimensionSemantic},
		Filters:     wrong,
	})
	if err != nil {
		t.Fatalf("fused mismatch query: %v", err)
	}
	if len(fused.Hits) != 0 {
		t.Fatalf("fused mismatch hits = %+v", fused.Hits)
	}
	fused, err = indexer.Fuse(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID,
		Text:        "query",
		Fuse:        []string{DimensionLexical, DimensionSemantic},
		Filters:     right,
	})
	if err != nil || len(fused.Hits) != 1 || fused.Hits[0].SubjectID != seed.FileEntryID {
		t.Fatalf("fused matching hits = %+v err=%v", fused.Hits, err)
	}

	// A pinned provider generation must not use a newer lexical projection
	// from another snapshot as its filter authority.
	newLexicalID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: newLexicalID, WorkspaceID: seed.WorkspaceID, DBPath: lexicalPath,
		NamespaceRootID: seed.RootID, SnapshotRef: "snapshot:newer",
		Dimension: DimensionLexical, CreatedAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("insert newer lexical generation: %v", err)
	}
	_, _, err = indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, GenerationID: semanticID,
		Dimension: DimensionSemantic, Text: "query", Filters: right,
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("pinned provider with mismatched lexical snapshot err=%v, want ErrUnavailable", err)
	}
	_, _, err = indexer.Query(ctx, QueryRequest{
		WorkspaceID: "workspace_other", GenerationID: lexicalID,
		Dimension: DimensionLexical, Text: "query",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("pinned lexical cross-workspace query err=%v, want ErrUnavailable", err)
	}

	if err := engine.RemoveFile(lexicalPath); err != nil {
		t.Fatalf("remove lexical projection: %v", err)
	}
	_, _, err = indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic,
		Text: "no-provider-candidate", Filters: right,
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty semantic query without lexical filter projection err=%v, want ErrUnavailable", err)
	}
	_, _, err = indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic,
		Text: "query", Filters: right,
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("semantic query without lexical filter projection err=%v, want ErrUnavailable", err)
	}
}

func TestIndexerBoundGenerationRequiresMatchingConfigProfileAndSemanticSpace(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	engine := &Engine{Dir: t.TempDir()}
	binding := FixtureIndexBinding("sha256:config-a")

	lexicalID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	lexicalPath, err := engine.Build(ctx, lexicalID, []Document{{
		SubjectID: seed.FileEntryID, Path: "query.txt", Name: "query.txt",
		EntryType: string(sqlite.EntryFile), Descriptions: "query",
	}})
	if err != nil {
		t.Fatalf("build lexical: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: lexicalID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:bound",
		NamespaceRootID: seed.RootID, DBPath: lexicalPath, Dimension: DimensionLexical,
		ConfigDigest: binding.ConfigDigest, ProviderProfileDigest: binding.LexicalProfileDigest,
	}); err != nil {
		t.Fatalf("insert lexical generation: %v", err)
	}

	semanticID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	semanticPath, err := engine.BuildTokens(ctx, semanticID, []TokenDocument{{
		SubjectID: seed.FileEntryID, Token: fixtureToken("sem1", "query"),
		Space: binding.SemanticSpace, Path: "query.txt", Name: "query.txt",
		EntryType: string(sqlite.EntryFile),
	}})
	if err != nil {
		t.Fatalf("build semantic: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: semanticID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:bound",
		NamespaceRootID: seed.RootID, DBPath: semanticPath, Dimension: DimensionSemantic,
		ConfigDigest: binding.ConfigDigest, ProviderProfileDigest: binding.SemanticProfileDigest,
		SemanticSpace: binding.SemanticSpace,
	}); err != nil {
		t.Fatalf("insert semantic generation: %v", err)
	}

	indexer := &Indexer{
		Store: store, Engine: engine, ConfigDigest: binding.ConfigDigest,
		LexicalProfileDigest:  binding.LexicalProfileDigest,
		SemanticProfileDigest: binding.SemanticProfileDigest,
		SemanticSpace:         binding.SemanticSpace,
		SemanticManifest:      binding.SemanticManifest,
	}
	if _, hits, err := indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, GenerationID: semanticID,
		Dimension: DimensionSemantic, Text: "query",
	}); err != nil || len(hits) != 1 || hits[0].SubjectID != seed.FileEntryID {
		t.Fatalf("matching bound query = %+v err=%v", hits, err)
	}

	legacySemanticID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: legacySemanticID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:bound",
		NamespaceRootID: seed.RootID, DBPath: semanticPath, Dimension: DimensionSemantic,
	}); err != nil {
		t.Fatalf("insert legacy semantic generation: %v", err)
	}
	if _, _, err := indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, GenerationID: legacySemanticID,
		Dimension: DimensionSemantic, Text: "query",
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("legacy bound query error = %v, want ErrUnavailable", err)
	}
	legacyLexicalID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: legacyLexicalID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:bound",
		NamespaceRootID: seed.RootID, DBPath: lexicalPath, Dimension: DimensionLexical,
	}); err != nil {
		t.Fatalf("insert legacy lexical generation: %v", err)
	}
	if _, _, err := indexer.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, GenerationID: legacyLexicalID,
		Dimension: DimensionLexical, Text: "query",
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("legacy lexical query error = %v, want ErrUnavailable", err)
	}

	for i, digest := range []string{
		ProfileDigest(DimensionLexical, "tampered-provider"),
		"sha256:tampered-config",
	} {
		tampered := &sqlite.IndexGeneration{
			ID: mustSearchID(t, sqlite.IDPrefixIndexGeneration), WorkspaceID: seed.WorkspaceID,
			SnapshotRef: "snapshot:bound", NamespaceRootID: seed.RootID, DBPath: lexicalPath,
			Dimension: DimensionLexical, ConfigDigest: binding.ConfigDigest,
			ProviderProfileDigest: digest, CreatedAt: time.Now().Add(time.Duration(i+1) * time.Hour),
		}
		if i == 1 {
			tampered.ConfigDigest = digest
			tampered.ProviderProfileDigest = binding.LexicalProfileDigest
		}
		if err := store.InsertIndexGeneration(ctx, tampered); err != nil {
			t.Fatalf("insert tampered lexical generation: %v", err)
		}
		if _, _, err := indexer.Query(ctx, QueryRequest{
			WorkspaceID: seed.WorkspaceID, GenerationID: semanticID,
			Dimension: DimensionSemantic, Text: "query", Filters: Filters{EntryType: string(sqlite.EntryFile)},
		}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("tampered lexical authority %d error = %v, want ErrUnavailable", i, err)
		}
	}

	cases := []struct {
		name   string
		mutate func(*Indexer)
	}{
		{name: "config", mutate: func(idx *Indexer) { idx.ConfigDigest = "sha256:config-b" }},
		{name: "provider", mutate: func(idx *Indexer) { idx.SemanticManifest.ProviderDigest = "different-provider" }},
		{name: "semantic space", mutate: func(idx *Indexer) { idx.SemanticManifest.SemanticSpace = "different-space" }},
		{name: "incomplete manifest", mutate: func(idx *Indexer) { idx.SemanticManifest.Pooling = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := &Indexer{
				Store:                 indexer.Store,
				Engine:                indexer.Engine,
				ConfigDigest:          indexer.ConfigDigest,
				LexicalProfileDigest:  indexer.LexicalProfileDigest,
				SemanticProfileDigest: indexer.SemanticProfileDigest,
				SemanticSpace:         indexer.SemanticSpace,
				SemanticManifest:      indexer.SemanticManifest,
			}
			tc.mutate(candidate)
			_, _, err := candidate.Query(ctx, QueryRequest{
				WorkspaceID: seed.WorkspaceID, GenerationID: semanticID,
				Dimension: DimensionSemantic, Text: "query",
			})
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("query error = %v, want ErrUnavailable", err)
			}
		})
	}

	missingSpace := &Indexer{
		Store:                 indexer.Store,
		Engine:                indexer.Engine,
		ConfigDigest:          indexer.ConfigDigest,
		LexicalProfileDigest:  indexer.LexicalProfileDigest,
		SemanticProfileDigest: indexer.SemanticProfileDigest,
	}
	missingSpace.SemanticSpace = ""
	if _, _, err := missingSpace.Query(ctx, QueryRequest{
		WorkspaceID: seed.WorkspaceID, GenerationID: semanticID,
		Dimension: DimensionSemantic, Text: "query",
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing semantic space error = %v, want ErrUnavailable", err)
	}
}
