package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// sourceFeedProvider deliberately gives filename and extracted text separate
// vectors so this integration test proves that each source is independently
// queryable, rather than merely being present in one undifferentiated feed.
type sourceFeedProvider struct{}

func (sourceFeedProvider) Embed(_ context.Context, req SemanticEmbeddingRequest) ([]SemanticVector, error) {
	if err := validateSemanticEmbeddingRequest(req); err != nil {
		return nil, err
	}
	results := make([]SemanticVector, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		text := strings.ToLower(input.Text)
		vector := []float32{0, 0, 0, 0}
		if strings.Contains(text, "track") {
			vector[0] = 1
		}
		if strings.Contains(text, "extracted") {
			vector[1] = 1
		}
		if req.Purpose == SemanticEmbeddingQuery && vector[0] == 0 && vector[1] == 0 {
			return nil, errors.New("source-feed test query is outside its controlled vocabulary")
		}
		if vector[0] == 0 && vector[1] == 0 {
			// Document vectors are required to be non-zero when the profile
			// declares L2 normalization. Keep this fixture deterministic while
			// retaining a separate vector for the known source dimensions.
			vector[2] = 1
		}
		results = append(results, SemanticVector{SubjectID: input.SubjectID, SegmentID: input.SegmentID, Vector: vector})
	}
	return results, validateSemanticEmbeddingResults(req, results)
}

type selectiveSourceFeedDriver struct {
	mu               sync.Mutex
	byPath           map[string][]ZvecSegment
	membershipByPath map[string]map[ZvecCoverageIdentity]struct{}
}

func (d *selectiveSourceFeedDriver) Build(_ context.Context, spec ZvecGenerationSpec, segments []ZvecSegment) (ZvecGenerationReceipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.byPath == nil {
		d.byPath = map[string][]ZvecSegment{}
	}
	if d.membershipByPath == nil {
		d.membershipByPath = map[string]map[ZvecCoverageIdentity]struct{}{}
	}
	d.byPath[spec.Path] = append([]ZvecSegment(nil), segments...)
	membership := make(map[ZvecCoverageIdentity]struct{}, len(segments))
	for _, segment := range segments {
		membership[ZvecCoverageIdentity{SubjectID: segment.SubjectID, SegmentID: segment.SegmentID}] = struct{}{}
	}
	d.membershipByPath[spec.Path] = membership
	return ZvecGenerationReceipt{Path: spec.Path, ProfileDigest: spec.ProfileDigest, LibraryDigest: spec.LibraryDigest, Dimension: spec.Manifest.Dimension, SegmentCount: len(segments)}, nil
}

func (d *selectiveSourceFeedDriver) Open(_ context.Context, spec ZvecGenerationSpec) (ZvecGeneration, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return selectiveSourceFeedGeneration{segments: append([]ZvecSegment(nil), d.byPath[spec.Path]...), dimension: spec.Manifest.Dimension}, nil
}

func (d *selectiveSourceFeedDriver) Coverage(_ context.Context, spec ZvecGenerationSpec) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := make([]string, 0, len(d.byPath[spec.Path]))
	for _, segment := range d.byPath[spec.Path] {
		ids = append(ids, segment.SegmentID)
	}
	return ids, nil
}

func (d *selectiveSourceFeedDriver) CoveragePairs(_ context.Context, spec ZvecGenerationSpec) ([]ZvecCoverageIdentity, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	pairs := make([]ZvecCoverageIdentity, 0, len(d.byPath[spec.Path]))
	for _, segment := range d.byPath[spec.Path] {
		pairs = append(pairs, ZvecCoverageIdentity{SubjectID: segment.SubjectID, SegmentID: segment.SegmentID})
	}
	return pairs, nil
}

func (d *selectiveSourceFeedDriver) VerifyMembership(_ context.Context, spec ZvecGenerationSpec, candidates []ZvecCoverageIdentity) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	known := d.membershipByPath[spec.Path]
	for _, candidate := range candidates {
		if _, ok := known[candidate]; !ok {
			return ErrZvecUnavailable
		}
	}
	return nil
}

type selectiveSourceFeedGeneration struct {
	segments  []ZvecSegment
	dimension int
}

func (g selectiveSourceFeedGeneration) Query(_ context.Context, vector []float32, topK int) ([]ZvecHit, error) {
	if len(vector) != g.dimension {
		return nil, ErrInvalidZvecGeneration
	}
	hits := make([]ZvecHit, 0, len(g.segments))
	for _, segment := range g.segments {
		var score float32
		for i, value := range vector {
			score += value * segment.Vector[i]
		}
		if score > 0 {
			hits = append(hits, ZvecHit{SubjectID: segment.SubjectID, SegmentID: segment.SegmentID, Score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].SegmentID < hits[j].SegmentID })
	if topK < len(hits) {
		hits = hits[:topK]
	}
	return hits, nil
}

func (selectiveSourceFeedGeneration) Close() error { return nil }

func TestIndexerSemanticSourceFeedFilenameAndAdmittedExtractArtifact(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	manifest := testZvecManifest()
	driver := &selectiveSourceFeedDriver{}
	artifactID := mustSearchID(t, sqlite.IDPrefixArtifact)
	attemptID := mustSearchID(t, sqlite.IDPrefixAttempt)
	body := "extracted semantic prose"
	digest := sha256.Sum256([]byte(body))
	if err := store.InsertProcessorAttempt(ctx, &sqlite.ProcessorAttempt{
		ID: attemptID, WorkspaceID: seed.WorkspaceID, SubjectRef: seed.FileEntryID,
		SnapshotRef: "apfs-snapshot:2026-08-11T12:00:00Z", RouteDigest: "sha256:source-feed-route",
		Route: json.RawMessage(`{"capability":"extract.text.v1"}`), Stage: "EXTRACT",
		CapabilityID: "extract.text.v1", Status: "SUCCEEDED", ReasonCode: "ADMITTED_ARTIFACT",
		Provenance: json.RawMessage(`{}`), FenceToken: 1, ProcessorDigest: "sha256:source-feed-producer",
	}); err != nil {
		t.Fatalf("insert extract attempt: %v", err)
	}
	if err := store.InsertProcessorArtifact(ctx, &sqlite.ProcessorArtifact{
		ID: artifactID, WorkspaceID: seed.WorkspaceID, SubjectRef: seed.FileEntryID,
		SnapshotRef: "apfs-snapshot:2026-08-11T12:00:00Z", RouteDigest: "sha256:source-feed-route",
		Stage: "EXTRACT", CapabilityID: "extract.text.v1", SchemaRef: "text/plain:v1",
		State: sqlite.ArtifactAdmitted, AuthorityClass: "STAGED_ARTIFACT", LifecycleClass: "DURABLE",
		MediaType: "text/plain", ByteLength: int64(len(body)), Digest: "sha256:" + hex.EncodeToString(digest[:]), Body: body,
		AttemptID: attemptID, FenceToken: 1, ProducerDigest: "sha256:source-feed-producer",
		Envelope: json.RawMessage(`{"source":"controlled-test"}`),
	}); err != nil {
		t.Fatalf("insert admitted extract artifact: %v", err)
	}
	indexer := &Indexer{
		Store: store, Engine: &Engine{Dir: t.TempDir()}, ConfigDigest: manifest.ConfigDigest,
		LexicalProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1), SemanticManifest: manifest,
		SemanticProvider: sourceFeedProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:source-feed", seed.RootID); err != nil {
		t.Fatalf("rebuild source feed: %v", err)
	}
	semantic, err := store.LatestIndexGeneration(ctx, seed.WorkspaceID, DimensionSemantic)
	if err != nil {
		t.Fatalf("read semantic generation: %v", err)
	}
	if got := len(driver.byPath[semantic.DBPath]); got != 4 {
		t.Fatalf("source feed segment count = %d, want three filenames plus artifact", got)
	}
	coverage, err := indexer.SemanticCoverage(ctx, seed.WorkspaceID)
	if err != nil || !coverage.Available || !coverage.Complete || coverage.Expected != 4 || coverage.Indexed != 4 {
		t.Fatalf("source feed coverage = %+v, err=%v", coverage, err)
	}

	_, filenameHits, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "track"})
	if err != nil || len(filenameHits) != 1 || filenameHits[0].SubjectID != seed.FileEntryID || len(filenameHits[0].Segments) != 1 {
		t.Fatalf("filename semantic hit = %+v, err=%v", filenameHits, err)
	}
	filename := filenameHits[0].Segments[0]
	if filename.SourceType != "FILENAME" || filename.SourceID != seed.FileEntryID || filename.SegmentID != seed.FileEntryID || filename.MatchedText != "\\xfftrack.flac" {
		t.Fatalf("filename provenance = %+v", filename)
	}

	_, artifactHits, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "extracted"})
	if err != nil || len(artifactHits) != 1 || artifactHits[0].SubjectID != seed.FileEntryID || len(artifactHits[0].Segments) != 1 {
		t.Fatalf("artifact semantic hit = %+v, err=%v", artifactHits, err)
	}
	artifact := artifactHits[0].Segments[0]
	if artifact.SourceType != "ARTIFACT" || artifact.SourceID != artifactID || artifact.SegmentID != artifactID || artifact.MatchedText != body || artifact.Kind != "EXTRACT" || artifact.Producer != "sha256:source-feed-producer" || !artifact.Accepted {
		t.Fatalf("artifact provenance = %+v", artifact)
	}
}

func TestSemanticCoverageCanonicalizesLegacyFactSubjectRefs(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	legacyEntryID := mustSearchID(t, sqlite.IDPrefixNamespaceEntry)
	stableSubject := mustSearchID(t, sqlite.IDPrefixSubject)
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		if err := tx.InsertNamespaceEntry(ctx, &sqlite.NamespaceEntry{
			ID: legacyEntryID, SubjectRef: stableSubject, WorkspaceID: seed.WorkspaceID,
			RootID: seed.RootID, ParentID: seed.DirEntryID, RawName: []byte("legacy-track.txt"),
			DisplayName: "legacy-track.txt", FullPathKey: []byte("Music\x00legacy-track.txt"), EntryType: sqlite.EntryFile,
		}); err != nil {
			return err
		}
		return tx.InsertAnnotation(ctx, &sqlite.Annotation{
			ID: mustSearchID(t, sqlite.IDPrefixAnnotation), WorkspaceID: seed.WorkspaceID,
			// This is the pre-stable-subject form. Rebuild and coverage must
			// resolve it through the active namespace entry projection.
			SubjectRef: legacyEntryID, Kind: sqlite.AnnotationNote, Body: "legacy note", Revision: 1,
		})
	}); err != nil {
		t.Fatalf("insert legacy-subject records: %v", err)
	}
	descriptionID := mustSearchID(t, sqlite.IDPrefixDescription)
	segmentID := mustSearchID(t, sqlite.IDPrefixSemanticSegment)
	if err := store.InsertDescriptionDocument(ctx, &sqlite.DescriptionDocument{
		ID: descriptionID, WorkspaceID: seed.WorkspaceID, SubjectRef: legacyEntryID,
		Kind: sqlite.DescriptionUser, Body: "legacy description", SourceRef: "user:test",
		ProducerProfile: "human", Accepted: true,
	}); err != nil {
		t.Fatalf("insert legacy description: %v", err)
	}
	if err := store.InsertSemanticSegment(ctx, &sqlite.SemanticSegment{
		ID: segmentID, WorkspaceID: seed.WorkspaceID, DocumentID: descriptionID,
		SubjectRef: legacyEntryID, Ordinal: 0, Text: "legacy description", Language: "en", Section: "body",
	}); err != nil {
		t.Fatalf("insert legacy segment: %v", err)
	}
	manifest := testZvecManifest()
	driver := &selectiveSourceFeedDriver{}
	indexer := &Indexer{
		Store: store, Engine: &Engine{Dir: t.TempDir()}, ConfigDigest: manifest.ConfigDigest,
		LexicalProfileDigest: ProfileDigest(DimensionLexical, LexicalProfileV1), SemanticManifest: manifest,
		SemanticProvider: sourceFeedProvider{}, SemanticZvec: driver,
		SemanticLibraryPath: "/private/explicit/libzvec_c_api.dylib", SemanticLibraryDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if _, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:legacy-subject", seed.RootID); err != nil {
		t.Fatalf("rebuild legacy subject: %v", err)
	}
	coverage, err := indexer.SemanticCoverage(ctx, seed.WorkspaceID)
	if err != nil || !coverage.Available || !coverage.Complete || coverage.Expected != 6 || coverage.Indexed != 6 {
		t.Fatalf("legacy subject coverage = %+v, err=%v", coverage, err)
	}
	_, hits, err := indexer.Query(ctx, QueryRequest{WorkspaceID: seed.WorkspaceID, Dimension: DimensionSemantic, Text: "track"})
	var stableHit *Hit
	for i := range hits {
		if hits[i].SubjectID == stableSubject {
			stableHit = &hits[i]
			break
		}
	}
	if err != nil || stableHit == nil {
		t.Fatalf("legacy filename subject hit = %+v, err=%v", hits, err)
	}
	if len(stableHit.Segments) != 1 || stableHit.Segments[0].SourceType != "FILENAME" || stableHit.Segments[0].SourceID != legacyEntryID {
		t.Fatalf("legacy filename provenance = %+v", hits)
	}
}
