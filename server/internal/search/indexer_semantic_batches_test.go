package search

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type recordingSemanticBatchProvider struct {
	requests     []SemanticEmbeddingRequest
	failLanguage string
	omitResult   bool
}

func (p *recordingSemanticBatchProvider) Embed(_ context.Context, request SemanticEmbeddingRequest) ([]SemanticVector, error) {
	if err := validateSemanticEmbeddingRequest(request); err != nil {
		return nil, err
	}
	copyRequest := request
	copyRequest.Inputs = append([]SemanticTextInput(nil), request.Inputs...)
	p.requests = append(p.requests, copyRequest)
	if p.failLanguage != "" && request.Inputs[0].Language == p.failLanguage {
		return nil, fmt.Errorf("%w: rejected %s batch", ErrSemanticProviderUnavailable, p.failLanguage)
	}
	results := make([]SemanticVector, 0, len(request.Inputs))
	limit := len(request.Inputs)
	if p.omitResult {
		limit--
	}
	for i := limit - 1; i >= 0; i-- {
		input := request.Inputs[i]
		results = append(results, SemanticVector{SubjectID: input.SubjectID, SegmentID: input.SegmentID, Vector: []float32{1, 0, 0, 0}})
	}
	return results, nil
}

func TestEmbedSemanticDocumentBatchesSplitsLanguageAndRestoresOrder(t *testing.T) {
	manifest := testZvecManifest()
	inputs := make([]SemanticTextInput, 0, 603)
	for i := 0; i < 603; i++ {
		language := "und"
		if i%2 == 1 {
			language = "zh"
		}
		inputs = append(inputs, SemanticTextInput{
			SubjectID: "subject", SegmentID: fmt.Sprintf("segment-%03d", i), Language: language,
			Text: fmt.Sprintf("text %d", i),
		})
	}
	provider := &recordingSemanticBatchProvider{}
	results, err := embedSemanticDocumentBatches(context.Background(), provider, manifest, "generation:test", inputs)
	if err != nil {
		t.Fatalf("embedSemanticDocumentBatches() error = %v", err)
	}
	if len(results) != len(inputs) {
		t.Fatalf("result count = %d, want %d", len(results), len(inputs))
	}
	if len(provider.requests) != 4 {
		t.Fatalf("provider request count = %d, want 4 (two languages, each split at 256)", len(provider.requests))
	}
	counts := map[string]int{}
	for i, request := range provider.requests {
		if len(request.Inputs) == 0 || len(request.Inputs) > 256 {
			t.Fatalf("request[%d] input count = %d, want 1..256", i, len(request.Inputs))
		}
		language := request.Inputs[0].Language
		for _, input := range request.Inputs {
			if input.Language != language {
				t.Fatalf("request[%d] mixes languages: %+v", i, request.Inputs)
			}
		}
		counts[language] += len(request.Inputs)
		if request.GenerationID != "generation:test" || request.Purpose != SemanticEmbeddingDocument {
			t.Fatalf("request[%d] binding = %+v", i, request)
		}
	}
	if counts["und"] != 302 || counts["zh"] != 301 {
		t.Fatalf("language batch counts = %+v, want und=302 zh=301", counts)
	}
	for i, result := range results {
		wantID := fmt.Sprintf("segment-%03d", i)
		if result.SegmentID != wantID || result.SubjectID != "subject" {
			t.Fatalf("result[%d] = %+v, want segment %s bound to subject", i, result, wantID)
		}
	}
}

func TestEmbedSemanticDocumentBatchesFailsClosedOnBatchFailureOrMissingVector(t *testing.T) {
	manifest := testZvecManifest()
	inputs := []SemanticTextInput{
		{SubjectID: "subject", SegmentID: "segment-und", Language: "und", Text: "filename"},
		{SubjectID: "subject", SegmentID: "segment-zh", Language: "zh", Text: "描述"},
	}
	tests := []struct {
		name         string
		provider     *recordingSemanticBatchProvider
		wantProvider error
	}{
		{name: "batch failure", provider: &recordingSemanticBatchProvider{failLanguage: "zh"}, wantProvider: ErrSemanticProviderUnavailable},
		{name: "missing vector", provider: &recordingSemanticBatchProvider{omitResult: true}, wantProvider: ErrSemanticProviderUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := embedSemanticDocumentBatches(context.Background(), test.provider, manifest, "generation:test", inputs)
			if err == nil {
				t.Fatal("embedSemanticDocumentBatches() unexpectedly succeeded")
			}
			if !errors.Is(err, test.wantProvider) {
				t.Fatalf("error = %v, want %v", err, test.wantProvider)
			}
			if results != nil {
				t.Fatalf("partial results = %d, want nil", len(results))
			}
		})
	}
}
