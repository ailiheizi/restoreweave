package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

type semanticProviderTestRuntime struct {
	admission ONNXWorkerAdmission
}

func (r semanticProviderTestRuntime) Probe(context.Context, ONNXWorkerAdmission) (ONNXWorkerProbeResult, error) {
	return validONNXProbe(r.admission), nil
}

func (r semanticProviderTestRuntime) EmbedTextWithText(_ context.Context, req EmbedTextRequest, inputs []ONNXWorkerTextInput) (EmbedTextResultBatch, error) {
	digest, err := req.CanonicalDigest()
	if err != nil {
		return EmbedTextResultBatch{}, err
	}
	results := make([]EmbedTextResult, 0, len(req.Segments))
	for i, segment := range req.Segments {
		if i >= len(inputs) {
			break
		}
		vector := make([]float32, req.Profile.Dimension)
		vector[0] = 1
		results = append(results, EmbedTextResult{
			Binding: req.Binding, SegmentID: segment.ID, Source: segment.Source,
			DescriptionDocumentID: segment.DescriptionDocumentID, Ordinal: segment.Ordinal,
			SubjectRef: segment.SubjectRef, Language: segment.Language, TextHandleID: segment.TextHandleID,
			TextDigest: segment.TextDigest, Status: EmbedTextAccepted, Vector: vector,
			ElementType: req.Profile.ElementType, Dimension: req.Profile.Dimension,
			Normalization: req.Profile.Normalization, Pooling: req.Profile.Pooling,
			SemanticProfileDigest: req.Profile.SemanticProfileDigest, ConfigDigest: req.Profile.ConfigDigest,
			PreprocessingDigest: req.Binding.AppliedPreprocessingDigest, ModelDigest: req.Profile.ModelDigest,
			TokenizerDigest: req.Profile.TokenizerDigest, RuntimeDigest: req.Profile.RuntimeDigest,
			SemanticSpace: req.Profile.SemanticSpace, DeterminismClass: req.Profile.DeterminismClass,
			Coverage: EmbedTextCoverageFull, InputTokens: 1, EmbeddedTokens: 1,
		})
	}
	return EmbedTextResultBatch{Binding: req.Binding, RequestDigest: digest, PeakResourceBytes: 1,
		ResourceScope: req.ResourceScope, Results: results}, nil
}

func TestONNXSemanticEmbeddingProviderBindsDocumentAndQueryProvenance(t *testing.T) {
	admission := testONNXAdmission(t)
	runtimeAdapter := semanticProviderTestRuntime{admission: admission}
	worker, err := admission.Negotiate(context.Background(), testONNXHost(admission, runtimeAdapter))
	if err != nil {
		t.Fatalf("negotiate worker: %v", err)
	}
	provider, err := NewONNXSemanticEmbeddingProvider(worker)
	if err != nil {
		t.Fatalf("new semantic provider: %v", err)
	}
	document, err := provider.Embed(context.Background(), search.SemanticEmbeddingRequest{
		Purpose: search.SemanticEmbeddingDocument, GenerationID: "index-generation-1", Manifest: admission.Manifest,
		Inputs: []search.SemanticTextInput{{SubjectID: "subject-1", SegmentID: "segment-1", DescriptionDocumentID: "doc-1", Ordinal: 2, Language: "zh", Text: "flooded city archive"}},
	})
	if err != nil {
		t.Fatalf("document embedding: %v", err)
	}
	if len(document) != 1 || document[0].SubjectID != "subject-1" || document[0].SegmentID != "segment-1" || len(document[0].Vector) != admission.Manifest.Dimension {
		t.Fatalf("document vectors = %+v", document)
	}
	query, err := provider.Embed(context.Background(), search.SemanticEmbeddingRequest{
		Purpose: search.SemanticEmbeddingQuery, GenerationID: "index-generation-1", Manifest: admission.Manifest,
		Inputs: []search.SemanticTextInput{{SegmentID: "query-1", Text: "flooded city"}},
	})
	if err != nil {
		t.Fatalf("query embedding: %v", err)
	}
	if len(query) != 1 || query[0].SubjectID != "" || query[0].SegmentID != "query-1" {
		t.Fatalf("query vectors = %+v", query)
	}
}

func TestONNXSemanticEmbeddingProviderRejectsManifestAndNotReadyWorker(t *testing.T) {
	admission := testONNXAdmission(t)
	worker, err := admission.Negotiate(context.Background(), testONNXHost(admission, semanticProviderTestRuntime{admission: admission}))
	if err != nil {
		t.Fatalf("negotiate worker: %v", err)
	}
	badManifest := admission.Manifest
	badManifest.ConfigDigest = digestForProviderTest("different-config")
	provider, err := NewONNXSemanticEmbeddingProvider(worker)
	if err != nil {
		t.Fatalf("new semantic provider: %v", err)
	}
	if _, err := provider.Embed(context.Background(), search.SemanticEmbeddingRequest{Purpose: search.SemanticEmbeddingQuery, GenerationID: "index-generation-1", Manifest: badManifest, Inputs: []search.SemanticTextInput{{SegmentID: "query-1", Text: "query"}}}); err == nil {
		t.Fatal("provider accepted a manifest from another generation")
	}
	worker.Admission.Capability.State = ONNXWorkerStateAdmitted
	if _, err := NewONNXSemanticEmbeddingProvider(worker); err == nil {
		t.Fatal("not-ready worker was accepted")
	}
}

func digestForProviderTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
