//go:build purego

package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/zvec-ai/zvec-go"
)

type puregoZvecGenerationDriver struct {
	libraryPath string
}

type puregoZvecGeneration struct {
	collection *zvec.Collection
	spec       ZvecGenerationSpec
	mu         sync.Mutex
	closed     bool
}

var puregoZvecMu sync.Mutex
var puregoZvecLoadedPath string
var puregoZvecLoadedDigest string

func newZvecGenerationBackend(libraryPath string) ZvecGenerationDriver {
	return &puregoZvecGenerationDriver{libraryPath: libraryPath}
}

// ZvecReady performs the same explicit library and version admission that a
// generation build/open uses. Capability reporting must not infer readiness
// from a non-nil driver or an admitted descriptor alone.
func (d *puregoZvecGenerationDriver) ZvecReady(libraryPath, libraryDigest string, manifest EmbeddingGenerationManifest) bool {
	if d == nil || strings.TrimSpace(d.libraryPath) == "" || d.libraryPath != libraryPath ||
		strings.TrimSpace(libraryDigest) == "" || manifest.Validate() != nil {
		return false
	}
	if err := validateZvecLibraryDigest(libraryDigest); err != nil {
		return false
	}
	actual, err := zvecLibraryDigest(libraryPath)
	if err != nil || actual != libraryDigest {
		return false
	}
	stagingDir, err := os.MkdirTemp("", "restoreweave-zvec-readiness-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(stagingDir)
	stagedPath, err := stageZvecLibrary(ZvecGenerationSpec{
		Path: filepath.Join(stagingDir, "generation"), LibraryPath: libraryPath, LibraryDigest: libraryDigest,
	})
	if err != nil {
		return false
	}
	err = withExplicitZvecLibrary(stagedPath, libraryDigest, func() error {
		if !zvec.IsInitialized() {
			if err := zvec.Initialize(nil); err != nil {
				return err
			}
		}
		if zvec.GetVersionMajor() != 0 || zvec.GetVersionMinor() != 6 {
			return fmt.Errorf("zvec version %s is not 0.6.x", zvec.GetVersion())
		}
		return nil
	})
	return err == nil
}

func (d *puregoZvecGenerationDriver) prepare(spec ZvecGenerationSpec) error {
	if d == nil || d.libraryPath == "" || spec.LibraryPath != d.libraryPath {
		return fmt.Errorf("%w: driver and generation library paths differ", ErrInvalidZvecGeneration)
	}
	if err := validateZvecGenerationSpec(spec); err != nil {
		return err
	}
	stagedPath, err := stageZvecLibrary(spec)
	if err != nil {
		return err
	}
	return withExplicitZvecLibrary(stagedPath, spec.LibraryDigest, func() error {
		if !zvec.IsInitialized() {
			if err := zvec.Initialize(nil); err != nil {
				return fmt.Errorf("%w: initialize: %v", ErrZvecUnavailable, err)
			}
		}
		if zvec.GetVersionMajor() != 0 || zvec.GetVersionMinor() != 6 {
			return fmt.Errorf("%w: native version %s is not zvec 0.6.x", ErrZvecUnavailable, zvec.GetVersion())
		}
		return nil
	})
}

func withExplicitZvecLibrary(path, digest string, fn func() error) error {
	puregoZvecMu.Lock()
	defer puregoZvecMu.Unlock()
	if puregoZvecLoadedDigest != "" && puregoZvecLoadedDigest != digest {
		return fmt.Errorf("%w: native library digest changed from %q to %q", ErrZvecUnavailable, puregoZvecLoadedDigest, digest)
	}
	if zvec.IsInitialized() && puregoZvecLoadedDigest == "" {
		return fmt.Errorf("%w: zvec was initialized outside the staged library binding", ErrZvecUnavailable)
	}
	verifyLoadedPath := puregoZvecLoadedDigest == ""
	old, had := os.LookupEnv("ZVEC_LIBRARY_PATH")
	if err := os.Setenv("ZVEC_LIBRARY_PATH", path); err != nil {
		return fmt.Errorf("%w: set explicit library path: %v", ErrZvecUnavailable, err)
	}
	defer func() {
		if had {
			_ = os.Setenv("ZVEC_LIBRARY_PATH", old)
		} else {
			_ = os.Unsetenv("ZVEC_LIBRARY_PATH")
		}
	}()
	if err := fn(); err != nil {
		return err
	}
	if verifyLoadedPath {
		if err := verifyZvecLibraryLoaded(path, digest); err != nil {
			return err
		}
	}
	if puregoZvecLoadedPath == "" {
		puregoZvecLoadedPath = path
	}
	puregoZvecLoadedDigest = digest
	return nil
}

func (d *puregoZvecGenerationDriver) Build(ctx context.Context, spec ZvecGenerationSpec, segments []ZvecSegment) (ZvecGenerationReceipt, error) {
	if err := validateZvecSegments(segments, spec.Manifest.Dimension); err != nil {
		return ZvecGenerationReceipt{}, err
	}
	if err := d.prepare(spec); err != nil {
		return ZvecGenerationReceipt{}, err
	}
	if _, err := os.Lstat(spec.Path); err == nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: generation path already exists", ErrInvalidZvecGeneration)
	} else if !os.IsNotExist(err) {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: inspect generation path: %v", ErrInvalidZvecGeneration, err)
	}
	select {
	case <-ctx.Done():
		return ZvecGenerationReceipt{}, ctx.Err()
	default:
	}
	return d.buildNative(spec, segments)
}

func (d *puregoZvecGenerationDriver) buildNative(spec ZvecGenerationSpec, segments []ZvecSegment) (ZvecGenerationReceipt, error) {
	schema := zvec.NewCollectionSchema("restoreweave_semantic_generation")
	if schema == nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: create schema", ErrZvecUnavailable)
	}
	defer schema.Destroy()
	for _, field := range []*zvec.FieldSchema{
		zvec.NewFieldSchema(ZvecSubjectField, zvec.DataTypeString, false, 0),
		zvec.NewFieldSchema(ZvecSegmentField, zvec.DataTypeString, false, 0),
	} {
		if field == nil {
			return ZvecGenerationReceipt{}, fmt.Errorf("%w: create payload schema", ErrZvecUnavailable)
		}
		if err := schema.AddField(field); err != nil {
			field.Destroy()
			return ZvecGenerationReceipt{}, fmt.Errorf("%w: add payload schema: %v", ErrZvecUnavailable, err)
		}
		field.Destroy()
	}
	params, err := zvec.NewHNSWIndexParams(zvec.MetricTypeCosine, 16, 200)
	if err != nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: create vector index: %v", ErrZvecUnavailable, err)
	}
	defer params.Destroy()
	vectorField := zvec.NewFieldSchema(ZvecEmbeddingField, zvec.DataTypeVectorFP32, false, uint32(spec.Manifest.Dimension))
	if vectorField == nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: create vector schema", ErrZvecUnavailable)
	}
	defer vectorField.Destroy()
	if err := vectorField.SetIndexParams(params); err != nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: bind vector index: %v", ErrZvecUnavailable, err)
	}
	if err := schema.AddField(vectorField); err != nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: add vector schema: %v", ErrZvecUnavailable, err)
	}
	options := zvec.NewCollectionOptions()
	if options == nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: create collection options", ErrZvecUnavailable)
	}
	defer options.Destroy()
	if err := options.SetEnableMmap(false); err != nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: configure collection: %v", ErrZvecUnavailable, err)
	}
	collection, err := zvec.CreateAndOpen(spec.Path, schema, options)
	if err != nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: create generation: %v", ErrZvecUnavailable, err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = collection.Close()
		}
	}()
	docs := make([]*zvec.Doc, 0, len(segments))
	for _, segment := range segments {
		doc := zvec.NewDoc()
		if doc == nil {
			zvec.FreeDocs(docs)
			return ZvecGenerationReceipt{}, fmt.Errorf("%w: create document", ErrZvecUnavailable)
		}
		doc.SetPK(segment.SegmentID)
		if err := doc.AddStringField(ZvecSubjectField, segment.SubjectID); err != nil {
			doc.Destroy()
			zvec.FreeDocs(docs)
			return ZvecGenerationReceipt{}, fmt.Errorf("%w: add subject payload: %v", ErrZvecUnavailable, err)
		}
		if err := doc.AddStringField(ZvecSegmentField, segment.SegmentID); err != nil {
			doc.Destroy()
			zvec.FreeDocs(docs)
			return ZvecGenerationReceipt{}, fmt.Errorf("%w: add segment payload: %v", ErrZvecUnavailable, err)
		}
		if err := doc.AddVectorFP32Field(ZvecEmbeddingField, segment.Vector); err != nil {
			doc.Destroy()
			zvec.FreeDocs(docs)
			return ZvecGenerationReceipt{}, fmt.Errorf("%w: add vector: %v", ErrZvecUnavailable, err)
		}
		docs = append(docs, doc)
	}
	result, err := collection.Insert(docs)
	zvec.FreeDocs(docs)
	if err != nil || result == nil || result.SuccessCount != uint64(len(segments)) {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: insert result=%+v err=%v", ErrZvecUnavailable, result, err)
	}
	if err := collection.Flush(); err != nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: flush: %v", ErrZvecUnavailable, err)
	}
	if err := collection.Close(); err != nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: close: %v", ErrZvecUnavailable, err)
	}
	closed = true
	segmentIDs := make([]string, 0, len(segments))
	for _, segment := range segments {
		segmentIDs = append(segmentIDs, segment.SegmentID)
	}
	sort.Strings(segmentIDs)
	metadata := zvecGenerationMetadata{Schema: zvecGenerationMetadataSchema, Path: spec.Path, LibraryDigest: spec.LibraryDigest, ProfileDigest: spec.ProfileDigest, Manifest: spec.Manifest, SegmentIDs: segmentIDs}
	if err := writeZvecGenerationMetadata(spec.Path, metadata); err != nil {
		return ZvecGenerationReceipt{}, fmt.Errorf("%w: write generation metadata: %v", ErrZvecUnavailable, err)
	}
	return ZvecGenerationReceipt{Path: spec.Path, LibraryDigest: spec.LibraryDigest, ProfileDigest: spec.ProfileDigest, Dimension: spec.Manifest.Dimension, SegmentCount: len(segments)}, nil
}

// Coverage returns the segment identities admitted to this immutable
// generation. The IDs are backend-owned evidence, not recovery authority;
// callers still intersect them with durable segments before claiming full
// coverage.
func (d *puregoZvecGenerationDriver) Coverage(ctx context.Context, spec ZvecGenerationSpec) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	opened, err := d.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	if err := opened.Close(); err != nil {
		return nil, fmt.Errorf("%w: close generation for coverage: %v", ErrZvecUnavailable, err)
	}
	metadata, err := readZvecGenerationMetadata(spec.Path)
	if err != nil {
		return nil, err
	}
	if metadata.Path != spec.Path || metadata.LibraryDigest != spec.LibraryDigest || metadata.ProfileDigest != spec.ProfileDigest {
		return nil, fmt.Errorf("%w: generation metadata does not match coverage spec", ErrZvecUnavailable)
	}
	return append([]string(nil), metadata.SegmentIDs...), nil
}

func (d *puregoZvecGenerationDriver) Open(ctx context.Context, spec ZvecGenerationSpec) (ZvecGeneration, error) {
	if err := d.prepare(spec); err != nil {
		return nil, err
	}
	info, err := os.Lstat(spec.Path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%w: generation path is missing or invalid", ErrZvecUnavailable)
	}
	metadata, err := readZvecGenerationMetadata(spec.Path)
	if err != nil {
		return nil, err
	}
	if metadata.Path != spec.Path || metadata.LibraryDigest != spec.LibraryDigest || metadata.ProfileDigest != spec.ProfileDigest || metadata.Manifest.Dimension != spec.Manifest.Dimension || metadata.Manifest.SemanticSpace != spec.Manifest.SemanticSpace {
		return nil, fmt.Errorf("%w: generation profile or dimension does not match requested manifest", ErrZvecUnavailable)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return d.openNative(spec)
}

func (d *puregoZvecGenerationDriver) openNative(spec ZvecGenerationSpec) (ZvecGeneration, error) {
	options := zvec.NewCollectionOptions()
	if options == nil {
		return nil, fmt.Errorf("%w: create readonly options", ErrZvecUnavailable)
	}
	defer options.Destroy()
	if err := options.SetReadOnly(true); err != nil {
		return nil, fmt.Errorf("%w: set readonly: %v", ErrZvecUnavailable, err)
	}
	if err := options.SetEnableMmap(false); err != nil {
		return nil, fmt.Errorf("%w: set mmap: %v", ErrZvecUnavailable, err)
	}
	collection, err := zvec.Open(spec.Path, options)
	if err != nil {
		return nil, fmt.Errorf("%w: open generation: %v", ErrZvecUnavailable, err)
	}
	schema, err := collection.GetSchema()
	if err != nil {
		_ = collection.Close()
		return nil, fmt.Errorf("%w: read generation schema: %v", ErrZvecUnavailable, err)
	}
	field := schema.GetField(ZvecEmbeddingField)
	valid := field != nil && field.GetDataType() == zvec.DataTypeVectorFP32 && int(field.GetDimension()) == spec.Manifest.Dimension
	schema.Destroy()
	if !valid {
		_ = collection.Close()
		return nil, fmt.Errorf("%w: generation schema does not match profile dimension", ErrZvecUnavailable)
	}
	return &puregoZvecGeneration{collection: collection, spec: spec}, nil
}

func (g *puregoZvecGeneration) Query(ctx context.Context, vector []float32, topK int) ([]ZvecHit, error) {
	if g == nil {
		return nil, ErrZvecGenerationClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(vector) != g.spec.Manifest.Dimension || topK <= 0 {
		return nil, fmt.Errorf("%w: query dimension/topK mismatch", ErrInvalidZvecGeneration)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.collection == nil {
		return nil, ErrZvecGenerationClosed
	}
	query := zvec.NewSearchQuery()
	if query == nil {
		return nil, fmt.Errorf("%w: create query", ErrZvecUnavailable)
	}
	defer query.Destroy()
	if err := query.SetFieldName(ZvecEmbeddingField); err != nil {
		return nil, fmt.Errorf("%w: query field: %v", ErrZvecUnavailable, err)
	}
	if err := query.SetTopK(topK); err != nil {
		return nil, fmt.Errorf("%w: query topK: %v", ErrZvecUnavailable, err)
	}
	if err := query.SetQueryVector(vector); err != nil {
		return nil, fmt.Errorf("%w: query vector: %v", ErrZvecUnavailable, err)
	}
	queryParams := zvec.NewHNSWQueryParams(64, 0, false, false)
	if queryParams == nil {
		return nil, fmt.Errorf("%w: create query parameters", ErrZvecUnavailable)
	}
	if err := query.SetHNSWParams(queryParams); err != nil {
		queryParams.Destroy()
		return nil, fmt.Errorf("%w: query parameters: %v", ErrZvecUnavailable, err)
	}
	if err := query.SetOutputFields([]string{ZvecSubjectField, ZvecSegmentField}); err != nil {
		return nil, fmt.Errorf("%w: query output: %v", ErrZvecUnavailable, err)
	}
	results, err := g.collection.Query(query)
	if err != nil {
		return nil, fmt.Errorf("%w: query: %v", ErrZvecUnavailable, err)
	}
	defer zvec.FreeDocs(results)
	hits := make([]ZvecHit, 0, len(results))
	for _, result := range results {
		subject, subjectErr := result.GetStringField(ZvecSubjectField)
		segment, segmentErr := result.GetStringField(ZvecSegmentField)
		if subjectErr != nil || segmentErr != nil || subject == "" || segment == "" {
			return nil, fmt.Errorf("%w: result payload is incomplete", ErrZvecUnavailable)
		}
		hits = append(hits, ZvecHit{SubjectID: subject, SegmentID: segment, Score: result.GetScore()})
	}
	return hits, nil
}

func (g *puregoZvecGeneration) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	if g.collection == nil {
		return nil
	}
	err := g.collection.Close()
	g.collection = nil
	return err
}
