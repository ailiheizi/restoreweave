package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestSemanticGenerationReceiptBindsGenerationSnapshotAndRoot(t *testing.T) {
	generation := sqlite.IndexGeneration{
		ID: "generation:test-receipt", WorkspaceID: "workspace:test", SnapshotRef: "snapshot:test",
		NamespaceRootID: "root:test", DBPath: filepath.Join(t.TempDir(), "semantic.zvec"),
		ConfigDigest: "config:test", ProviderProfileDigest: "profile:test", SemanticSpace: "space:test",
	}
	identities := []ZvecCoverageIdentity{{SubjectID: "subject:test", SegmentID: "segment:test"}}
	if err := writeSemanticGenerationReceipt(generation, "sha256:"+strings.Repeat("0", 64), identities); err != nil {
		t.Fatalf("write receipt: %v", err)
	}
	got, gotDigest, gotLibrary, err := readSemanticGenerationReceipt(generation)
	if err != nil {
		t.Fatalf("read valid receipt: %v", err)
	}
	if len(got) != len(identities) || got[0] != identities[0] || gotDigest == "" || gotLibrary == "" {
		t.Fatalf("valid receipt = identities=%+v digest=%q library=%q", got, gotDigest, gotLibrary)
	}
	mutated := generation
	mutated.SnapshotRef = "snapshot:other"
	if _, _, _, err := readSemanticGenerationReceipt(mutated); err == nil {
		t.Fatal("receipt accepted a different generation snapshot")
	}
	mutated = generation
	mutated.NamespaceRootID = "root:other"
	if _, _, _, err := readSemanticGenerationReceipt(mutated); err == nil {
		t.Fatal("receipt accepted a different primary root")
	}
	if err := writeSemanticGenerationReceipt(generation, "sha256:"+strings.Repeat("0", 64), identities); err == nil {
		t.Fatal("receipt publication replaced an existing final receipt")
	}
}

func TestReadSemanticGenerationReceiptRejectsSymlinkOversizeAndTruncation(t *testing.T) {
	identities := []ZvecCoverageIdentity{{SubjectID: "subject:test", SegmentID: "segment:test"}}
	tests := []struct {
		name   string
		mutate func(t *testing.T, path string)
	}{
		{
			name: "symlink",
			mutate: func(t *testing.T, path string) {
				target := path + ".target"
				if err := os.WriteFile(target, []byte(`{"not":"a receipt"}`), 0o600); err != nil {
					t.Fatalf("write symlink target: %v", err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatalf("remove receipt: %v", err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("symlink receipt: %v", err)
				}
			},
		},
		{
			name: "non-regular",
			mutate: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatalf("remove receipt: %v", err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("create non-regular receipt: %v", err)
				}
			},
		},
		{
			name: "oversize",
			mutate: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatalf("remove receipt: %v", err)
				}
				file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
				if err != nil {
					t.Fatalf("create oversized receipt: %v", err)
				}
				if err := file.Truncate(int64(semanticGenerationReceiptMaxBytes + 1)); err != nil {
					_ = file.Close()
					t.Fatalf("truncate oversized receipt: %v", err)
				}
				if err := file.Close(); err != nil {
					t.Fatalf("close oversized receipt: %v", err)
				}
			},
		},
		{
			name: "truncation",
			mutate: func(t *testing.T, path string) {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("stat receipt: %v", err)
				}
				if err := os.Truncate(path, info.Size()-1); err != nil {
					t.Fatalf("truncate receipt: %v", err)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			generation := sqlite.IndexGeneration{
				ID: "generation:unsafe-receipt", WorkspaceID: "workspace:test", SnapshotRef: "snapshot:test",
				NamespaceRootID: "root:test", DBPath: filepath.Join(t.TempDir(), "semantic.zvec"),
				ConfigDigest: "config:test", ProviderProfileDigest: "profile:test", SemanticSpace: "space:test",
			}
			if err := writeSemanticGenerationReceipt(generation, "sha256:"+strings.Repeat("0", 64), identities); err != nil {
				t.Fatalf("write receipt: %v", err)
			}
			path := semanticGenerationReceiptPath(generation.DBPath)
			tc.mutate(t, path)
			if _, _, _, err := readSemanticGenerationReceipt(generation); err == nil {
				t.Fatalf("unsafe %s receipt was accepted", tc.name)
			}
		})
	}
}

func TestValidateSemanticGenerationMappingBindsPrimaryRootSnapshot(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	generation := &sqlite.IndexGeneration{
		ID: mustSearchID(t, sqlite.IDPrefixIndexGeneration), WorkspaceID: seed.WorkspaceID,
		SnapshotRef: "snapshot:not-the-root", NamespaceRootID: seed.RootID,
		Dimension: DimensionSemantic, DBPath: filepath.Join(t.TempDir(), "semantic.zvec"),
	}
	if err := store.InsertIndexGenerationWithRoots(ctx, generation, []string{seed.RootID}); err != nil {
		t.Fatalf("insert generation: %v", err)
	}
	if err := validateSemanticGenerationMapping(ctx, store, *generation); err == nil {
		t.Fatal("primary root snapshot mismatch was accepted")
	}
}

func TestEnsureSemanticGenerationVerificationDoesNotSetReadiness(t *testing.T) {
	manifest := testZvecManifest()
	idx := &Indexer{
		SemanticManifest: manifest, SemanticZvec: &integrationSemanticGenerationDriver{},
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	generation := sqlite.IndexGeneration{
		ID: "generation:old", WorkspaceID: "workspace:test", SnapshotRef: "snapshot:test", NamespaceRootID: "root:test",
		DBPath: filepath.Join(t.TempDir(), "semantic.zvec"), ConfigDigest: manifest.ConfigDigest,
		ProviderProfileDigest: manifest.CanonicalDigest(), SemanticSpace: manifest.SemanticSpace,
	}
	identities := []ZvecCoverageIdentity{{SubjectID: "subject:test", SegmentID: "segment:test"}}
	if err := writeSemanticGenerationReceipt(generation, idx.SemanticLibraryDigest, identities); err != nil {
		t.Fatalf("write receipt: %v", err)
	}
	idx.cacheSemanticGeneration(generation, identities)
	if err := idx.ensureSemanticGenerationVerified(context.Background(), generation); err != nil {
		t.Fatalf("cached generation verification: %v", err)
	}
	if idx.semanticIndexReady.Load() {
		t.Fatal("pinned verification restored global readiness")
	}
}

func TestRestartedSemanticGenerationRejectsDeletedSegment(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	manifest := testZvecManifest()
	manifest.SemanticSpace = SemanticBundleBGESemanticSpace
	driver := &integrationSemanticGenerationDriver{}
	indexer := &Indexer{
		Store: store, Engine: &Engine{Dir: t.TempDir()}, ConfigDigest: manifest.ConfigDigest,
		LexicalProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1), SemanticManifest: manifest,
		SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:receipt-restart", seed.RootID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	generation, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionSemantic)
	if err != nil {
		t.Fatalf("latest semantic generation: %v", err)
	}
	segments := driver.byPath[generation.DBPath]
	if len(segments) < 2 {
		t.Fatalf("semantic segments = %d, want at least 2", len(segments))
	}
	driver.remove(generation.DBPath, segments[len(segments)-1].SegmentID)
	restarted := &Indexer{
		Store: store, Engine: indexer.Engine, ConfigDigest: manifest.ConfigDigest,
		LexicalProfileDigest: indexer.LexicalProfileDigest, SemanticManifest: manifest,
		SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: indexer.SemanticLibraryPath, SemanticLibraryDigest: indexer.SemanticLibraryDigest,
	}
	if err := restarted.WarmSemanticGeneration(ctx, seed.WorkspaceID); err == nil {
		t.Fatal("warm accepted a generation with a deleted non-hot segment")
	}
	if _, _, err := restarted.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "seed"}); err == nil {
		t.Fatal("query accepted a generation with a deleted non-hot segment")
	}
}

func TestPinnedOldSemanticGenerationCannotSetLatestReadiness(t *testing.T) {
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
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:readiness-a", seed.RootID); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	old, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionSemantic)
	if err != nil {
		t.Fatalf("old semantic generation: %v", err)
	}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:readiness-b", seed.RootID); err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	indexer.semanticIndexReady.Store(false)
	if _, _, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, GenerationID: old.ID, Dimension: DimensionSemantic, Text: "seed"}); err != nil {
		t.Fatalf("pinned old query: %v", err)
	}
	if indexer.semanticIndexReady.Load() {
		t.Fatal("pinned old generation restored latest readiness")
	}
}

type closeErrorSemanticDriver struct {
	base  *integrationSemanticGenerationDriver
	close bool
}

func (d *closeErrorSemanticDriver) Build(ctx context.Context, spec ZvecGenerationSpec, segments []ZvecSegment) (ZvecGenerationReceipt, error) {
	return d.base.Build(ctx, spec, segments)
}

func (d *closeErrorSemanticDriver) Open(ctx context.Context, spec ZvecGenerationSpec) (ZvecGeneration, error) {
	opened, err := d.base.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return closeErrorSemanticGeneration{inner: opened, failClose: d.close}, nil
}

func (d *closeErrorSemanticDriver) CoveragePairs(ctx context.Context, spec ZvecGenerationSpec) ([]ZvecCoverageIdentity, error) {
	return d.base.CoveragePairs(ctx, spec)
}

func (d *closeErrorSemanticDriver) VerifyMembership(ctx context.Context, spec ZvecGenerationSpec, candidates []ZvecCoverageIdentity) error {
	return d.base.VerifyMembership(ctx, spec, candidates)
}

type closeErrorSemanticGeneration struct {
	inner     ZvecGeneration
	failClose bool
}

func (g closeErrorSemanticGeneration) Query(ctx context.Context, vector []float32, topK int) ([]ZvecHit, error) {
	return g.inner.Query(ctx, vector, topK)
}

func (g closeErrorSemanticGeneration) Close() error {
	if err := g.inner.Close(); err != nil {
		return err
	}
	if g.failClose {
		return errors.New("generation close failed")
	}
	return nil
}

func TestSemanticQueryReturnsCloseErrorAndRevokesGeneration(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	manifest := testZvecManifest()
	driver := &closeErrorSemanticDriver{base: &integrationSemanticGenerationDriver{}}
	indexer := &Indexer{
		Store: store, Engine: &Engine{Dir: t.TempDir()}, ConfigDigest: manifest.ConfigDigest,
		LexicalProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1), SemanticManifest: manifest,
		SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:close-error", seed.RootID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	driver.close = true
	if _, _, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "seed"}); err == nil {
		t.Fatal("query accepted a generation whose reader close failed")
	}
	if indexer.semanticIndexReady.Load() {
		t.Fatal("reader close failure left semantic generation ready")
	}
	coverage, err := indexer.SemanticCoverage(ctx, seed.WorkspaceID)
	if err != nil {
		t.Fatalf("coverage after reader close failure: %v", err)
	}
	if coverage.Complete {
		t.Fatalf("coverage reported complete after reader close failure: %+v", coverage)
	}
}

func TestWarmAfterSemanticRebuildFailureRejectsStaleSemanticGeneration(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	manifest := testZvecManifest()
	manifest.SemanticSpace = SemanticBundleBGESemanticSpace
	driver := &integrationSemanticGenerationDriver{}
	indexer := &Indexer{
		Store: store, Engine: &Engine{Dir: t.TempDir()}, ConfigDigest: manifest.ConfigDigest,
		LexicalProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1), SemanticManifest: manifest,
		SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:semantic-before-failure", seed.RootID); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	if _, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionSemantic); err != nil {
		t.Fatalf("initial semantic generation: %v", err)
	}
	oldLexical, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionLexical)
	if err != nil {
		t.Fatalf("initial lexical generation: %v", err)
	}
	lexicalID := mustSearchID(t, sqlite.IDPrefixIndexGeneration)
	lexicalPath, err := indexer.Engine.Build(ctx, lexicalID, []Document{{SubjectID: seed.FileEntryID, Path: "new.txt", Name: "new.txt", EntryType: string(sqlite.EntryFile)}})
	if err != nil {
		t.Fatalf("new lexical build: %v", err)
	}
	if err := store.InsertIndexGeneration(ctx, &sqlite.IndexGeneration{
		ID: lexicalID, WorkspaceID: seed.WorkspaceID, SnapshotRef: "snapshot:lexical-after-failure",
		NamespaceRootID: seed.RootID, DBPath: lexicalPath, Dimension: DimensionLexical,
		ConfigDigest: manifest.ConfigDigest, ProviderProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1),
		CreatedAt: oldLexical.CreatedAt.Add(time.Nanosecond),
	}); err != nil {
		t.Fatalf("publish newer lexical generation: %v", err)
	}
	restarted := &Indexer{
		Store: store, Engine: indexer.Engine, ConfigDigest: manifest.ConfigDigest,
		LexicalProfileDigest: indexer.LexicalProfileDigest, SemanticManifest: manifest,
		SemanticProvider: integrationSemanticProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: indexer.SemanticLibraryPath, SemanticLibraryDigest: indexer.SemanticLibraryDigest,
	}
	if err := restarted.WarmSemanticGeneration(ctx, seed.WorkspaceID); err == nil {
		t.Fatal("restart warm restored semantic generation from an older lexical feed")
	}
	if _, _, err := restarted.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "seed"}); err == nil {
		t.Fatal("restart query restored stale semantic generation")
	}
	if _, hits, err := restarted.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionLexical, Text: "new"}); err != nil || len(hits) != 1 {
		t.Fatalf("lexical query after semantic failure = %+v, err=%v", hits, err)
	}
	if _, err := restarted.Rebuild(ctx, seed.WorkspaceID, "snapshot:semantic-recovered", seed.RootID); err != nil {
		t.Fatalf("successful semantic rebuild: %v", err)
	}
	if !restarted.semanticIndexReady.Load() {
		t.Fatal("successful semantic rebuild did not restore readiness")
	}
	if _, hits, err := restarted.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "seed"}); err != nil || len(hits) == 0 {
		t.Fatalf("semantic query after recovery = %+v, err=%v", hits, err)
	}
}

func TestSemanticBuildReceiptMustMatchExactSpec(t *testing.T) {
	spec := ZvecGenerationSpec{
		Path: "/tmp/semantic-generation.zvec", LibraryDigest: "sha256:" + strings.Repeat("0", 64),
		ProfileDigest: "profile", Manifest: EmbeddingGenerationManifest{Dimension: 4},
	}
	base := ZvecGenerationReceipt{Path: spec.Path, LibraryDigest: spec.LibraryDigest, ProfileDigest: spec.ProfileDigest, Dimension: 4, SegmentCount: 2}
	cases := []struct {
		name   string
		mutate func(*ZvecGenerationReceipt)
	}{
		{"path", func(r *ZvecGenerationReceipt) { r.Path = "/tmp/other.zvec" }},
		{"library", func(r *ZvecGenerationReceipt) { r.LibraryDigest = "sha256:" + strings.Repeat("1", 64) }},
		{"profile", func(r *ZvecGenerationReceipt) { r.ProfileDigest = "other-profile" }},
		{"dimension", func(r *ZvecGenerationReceipt) { r.Dimension = 8 }},
		{"segment count", func(r *ZvecGenerationReceipt) { r.SegmentCount = 1 }},
	}
	if err := validateSemanticBuildReceipt(base, spec, 2); err != nil {
		t.Fatalf("matching receipt rejected: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			tc.mutate(&candidate)
			if err := validateSemanticBuildReceipt(candidate, spec, 2); err == nil {
				t.Fatal("malicious build receipt accepted")
			}
		})
	}
}

func TestCompareSemanticCoverageRequiresExactSubjectBoundSet(t *testing.T) {
	expected := []ZvecCoverageIdentity{
		{SubjectID: "subject-a", SegmentID: "segment-a"},
		{SubjectID: "subject-b", SegmentID: "segment-b"},
	}
	tests := []struct {
		name string
		got  []ZvecCoverageIdentity
		want func(semanticCoverageComparison) bool
	}{
		{name: "complete", got: expected, want: func(c semanticCoverageComparison) bool { return c.Complete() }},
		{name: "missing", got: expected[:1], want: func(c semanticCoverageComparison) bool { return len(c.Missing) == 1 && len(c.Extra) == 0 }},
		{name: "extra", got: append(append([]ZvecCoverageIdentity(nil), expected...), ZvecCoverageIdentity{SubjectID: "subject-c", SegmentID: "segment-c"}), want: func(c semanticCoverageComparison) bool { return len(c.Extra) == 1 }},
		{name: "duplicate", got: append(append([]ZvecCoverageIdentity(nil), expected...), expected[0]), want: func(c semanticCoverageComparison) bool { return len(c.Duplicate) == 1 }},
		{name: "subject mismatch", got: []ZvecCoverageIdentity{{SubjectID: "wrong-subject", SegmentID: "segment-a"}, expected[1]}, want: func(c semanticCoverageComparison) bool { return len(c.SubjectMismatch) == 1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			comparison := compareSemanticCoverage(expected, tc.got)
			if !tc.want(comparison) {
				t.Fatalf("comparison = %+v", comparison)
			}
			if tc.name != "complete" && comparison.Complete() {
				t.Fatalf("invalid coverage reported complete: %+v", comparison)
			}
		})
	}
}

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
