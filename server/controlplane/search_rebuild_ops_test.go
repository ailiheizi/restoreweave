package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func newSearchRebuildDispatcher(t *testing.T, fixtures bool) *Dispatcher {
	t.Helper()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	opts := []DispatcherOption{WithExact(&exact.Service{Store: store, Repo: repo})}
	if fixtures {
		opts = append(opts, WithFixtureDimensions())
	}
	return NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", opts...)
}

func TestSearchRebuildCreatesLexicalGenerationAndReportsSemanticDegraded(t *testing.T) {
	ctx := context.Background()
	dispatcher := newSearchRebuildDispatcher(t, false)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "rebuild.txt"), []byte("rebuild search"), 0o600); err != nil {
		t.Fatal(err)
	}
	ingested := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	result := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchRebuild, map[string]any{
		"workspace_id": ingested.WorkspaceID,
	}))
	if result.Status != command.StatusDegraded {
		t.Fatalf("search.rebuild = %q: %+v", result.Status, result.Reasons)
	}
	var data command.SearchRebuildData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode search.rebuild: %v", err)
	}
	if data.WorkspaceID != ingested.WorkspaceID || data.SnapshotRef != ingested.SnapshotRef || data.NamespaceRootID != ingested.RootID {
		t.Fatalf("publication projection = %+v", data)
	}
	if data.LexicalGenerationRef == "" || data.LexicalState != command.CapabilityAvailable {
		t.Fatalf("lexical projection = %+v", data)
	}
	measuredCoverage, err := dispatcher.search.Coverage(ctx, ingested.WorkspaceID)
	if err != nil {
		t.Fatalf("measure lexical coverage: %v", err)
	}
	if want := projectSearchCoverage(measuredCoverage); !reflect.DeepEqual(data.LexicalCoverage, want) {
		t.Fatalf("lexical coverage projection = %+v, want %+v", data.LexicalCoverage, want)
	}
	lexical, err := dispatcher.search.Store.GetIndexGeneration(ctx, data.LexicalGenerationRef)
	if err != nil {
		t.Fatalf("get rebuilt lexical generation: %v", err)
	}
	if data.SnapshotRef != lexical.SnapshotRef || data.NamespaceRootID != lexical.NamespaceRootID || data.WorkspaceID != lexical.WorkspaceID {
		t.Fatalf("rebuild response escaped lexical generation: data=%+v generation=%+v", data, lexical)
	}
	if data.SemanticState != command.CapabilityUnavailable || strings.TrimSpace(data.SemanticFailure) == "" {
		t.Fatalf("semantic degradation = %+v", data)
	}
}

func TestSearchRebuildReportsFixtureSemanticReady(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	manifest := search.EmbeddingGenerationManifest{
		RuntimeDigest: "runtime", ModelDigest: "model", TokenizerDigest: "tokenizer", PreprocessingDigest: "preprocess",
		Pooling: "cls", Normalization: "l2", ElementType: "float32", Dimension: 2, VectorSchema: "float32:2",
		SemanticSpace: "test-space", Distance: "cosine", IndexConfig: search.ZvecIndexConfigV1, QueryConfig: search.ZvecQueryConfigV1,
		ProviderDigest: "provider", ConfigDigest: "config",
	}
	zvec := &rebuildSemanticZvec{}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock",
		WithConfigDigest(manifest.ConfigDigest),
		WithSemanticIndexerBinding(rebuildSemanticProvider{}, zvec, "/tmp/libzvec", "sha256:"+strings.Repeat("a", 64), manifest),
		WithExact(&exact.Service{Store: store, Repo: repo, ConfigDigest: manifest.ConfigDigest}))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte("fixture search"), 0o600); err != nil {
		t.Fatal(err)
	}
	ingested := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	resolved := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceResolve, map[string]any{
		"workspace_id": ingested.WorkspaceID, "root_id": ingested.RootID, "path": "fixture.txt",
	}))
	if resolved.Status != command.StatusSucceeded {
		t.Fatalf("namespace.resolve = %q: %+v", resolved.Status, resolved.Reasons)
	}
	var entry command.NamespaceResolveData
	if err := json.Unmarshal(resolved.Data, &entry); err != nil {
		t.Fatal(err)
	}
	described := dispatcher.Handle(ctx, mustEnvelope(t, command.OpDescriptionCreate, map[string]any{
		"workspace_id": ingested.WorkspaceID, "subject_ref": entry.PathRef, "kind": "USER", "body": "fixture semantic text", "language": "en", "accepted": true,
	}))
	if described.Status != command.StatusSucceeded {
		t.Fatalf("description.create = %q: %+v", described.Status, described.Reasons)
	}
	result := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchRebuild, map[string]any{
		"workspace_id": ingested.WorkspaceID,
	}))
	if result.Status != command.StatusDegraded {
		t.Fatalf("search.rebuild = %q: %+v", result.Status, result.Reasons)
	}
	var data command.SearchRebuildData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode search.rebuild: %v", err)
	}
	if data.LexicalGenerationRef == "" || data.SemanticGenerationRef == "" ||
		data.LexicalState != command.CapabilityAvailable || data.SemanticState != command.CapabilityAvailable {
		t.Fatalf("ready projection = %+v", data)
	}
	if data.LexicalCoverage.Dimension != search.DimensionLexical || !data.LexicalCoverage.Available || data.LexicalCoverage.Complete || len(data.LexicalCoverage.Missing) == 0 {
		t.Fatalf("lexical coverage = %+v", data.LexicalCoverage)
	}
	semantic, err := dispatcher.search.Store.LatestIndexGeneration(ctx, ingested.WorkspaceID, search.DimensionSemantic)
	if err != nil {
		t.Fatalf("get semantic generation: %v", err)
	}
	mismatchID, err := sqlite.NewStableID(sqlite.IDPrefixIndexGeneration)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := semantic
	mismatch.ID = mismatchID
	mismatch.SnapshotRef = "snapshot-mismatch"
	mismatch.CreatedAt = time.Now().Add(time.Hour)
	if err := dispatcher.search.Store.InsertIndexGeneration(ctx, &mismatch); err != nil {
		t.Fatalf("insert mismatched semantic generation: %v", err)
	}
	degraded := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchRebuild, map[string]any{
		"workspace_id": ingested.WorkspaceID,
	}))
	if degraded.Status != command.StatusDegraded {
		t.Fatalf("mismatched semantic generation status = %q: %+v", degraded.Status, degraded.Reasons)
	}
	var degradedData command.SearchRebuildData
	if err := json.Unmarshal(degraded.Data, &degradedData); err != nil {
		t.Fatal(err)
	}
	if degradedData.SemanticState != command.CapabilityUnavailable || degradedData.SemanticGenerationRef != "" || degradedData.SemanticFailure != "semantic generation does not match rebuilt lexical snapshot" {
		t.Fatalf("mismatched semantic generation projection = %+v", degradedData)
	}
}

type rebuildSemanticProvider struct{}

func (rebuildSemanticProvider) SemanticReady() bool { return true }

func (rebuildSemanticProvider) Embed(_ context.Context, req search.SemanticEmbeddingRequest) ([]search.SemanticVector, error) {
	results := make([]search.SemanticVector, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		results = append(results, search.SemanticVector{SubjectID: input.SubjectID, SegmentID: input.SegmentID, Vector: []float32{1, 0}})
	}
	return results, nil
}

type rebuildSemanticZvec struct {
	mu               sync.Mutex
	membershipByPath map[string]map[search.ZvecCoverageIdentity]struct{}
	coverageByPath   map[string][]search.ZvecCoverageIdentity
}

func (*rebuildSemanticZvec) ZvecReady(string, string, search.EmbeddingGenerationManifest) bool {
	return true
}

func (d *rebuildSemanticZvec) Build(_ context.Context, spec search.ZvecGenerationSpec, segments []search.ZvecSegment) (search.ZvecGenerationReceipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.membershipByPath == nil {
		d.membershipByPath = make(map[string]map[search.ZvecCoverageIdentity]struct{})
	}
	if d.coverageByPath == nil {
		d.coverageByPath = make(map[string][]search.ZvecCoverageIdentity)
	}
	membership := make(map[search.ZvecCoverageIdentity]struct{}, len(segments))
	coverage := make([]search.ZvecCoverageIdentity, 0, len(segments))
	for _, segment := range segments {
		identity := search.ZvecCoverageIdentity{SubjectID: segment.SubjectID, SegmentID: segment.SegmentID}
		membership[identity] = struct{}{}
		coverage = append(coverage, identity)
	}
	d.membershipByPath[spec.Path] = membership
	d.coverageByPath[spec.Path] = coverage
	return search.ZvecGenerationReceipt{Path: spec.Path, LibraryDigest: spec.LibraryDigest, ProfileDigest: spec.ProfileDigest, Dimension: spec.Manifest.Dimension, SegmentCount: len(segments)}, nil
}

func (d *rebuildSemanticZvec) CoveragePairs(_ context.Context, spec search.ZvecGenerationSpec) ([]search.ZvecCoverageIdentity, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]search.ZvecCoverageIdentity(nil), d.coverageByPath[spec.Path]...), nil
}

func (d *rebuildSemanticZvec) VerifyMembership(_ context.Context, spec search.ZvecGenerationSpec, candidates []search.ZvecCoverageIdentity) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	known := d.membershipByPath[spec.Path]
	for _, candidate := range candidates {
		if _, ok := known[candidate]; !ok {
			return search.ErrZvecUnavailable
		}
	}
	return nil
}

func (*rebuildSemanticZvec) Open(context.Context, search.ZvecGenerationSpec) (search.ZvecGeneration, error) {
	return nil, nil
}

func TestSearchRebuildFailsClosedForUnknownOrUnpublishedWorkspace(t *testing.T) {
	ctx := context.Background()
	dispatcher := newSearchRebuildDispatcher(t, false)
	unknown := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchRebuild, map[string]any{
		"workspace_id": "wsp_00000000000000000000000000000000",
	}))
	if unknown.Status != command.StatusFailed || !hasReasonCode(unknown, ReasonCodeNotFound) {
		t.Fatalf("unknown workspace = %q: %+v", unknown.Status, unknown.Reasons)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "planned.txt"), []byte("planned"), 0o600); err != nil {
		t.Fatal(err)
	}
	planned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": root}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var plan command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &plan); err != nil {
		t.Fatal(err)
	}
	unpublished := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchRebuild, map[string]any{
		"workspace_id": plan.WorkspaceID,
	}))
	if unpublished.Status != command.StatusFailed || !hasReasonCode(unpublished, ReasonCodeNotFound) {
		t.Fatalf("unpublished workspace = %q: %+v", unpublished.Status, unpublished.Reasons)
	}
	malformed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchRebuild, map[string]any{
		"workspace_id": "not-a-stable-id",
	}))
	if malformed.Status != command.StatusFailed || !hasReasonCode(malformed, ReasonCodeInvalidInput) {
		t.Fatalf("malformed workspace = %q: %+v", malformed.Status, malformed.Reasons)
	}
}
