package search

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestValidateSemanticEmbeddingResultsRequiresL2Vectors(t *testing.T) {
	manifest := testZvecManifest()
	req := SemanticEmbeddingRequest{
		Purpose: SemanticEmbeddingDocument, GenerationID: "generation", Manifest: manifest,
		Inputs: []SemanticTextInput{{SubjectID: "subject", SegmentID: "segment", Text: "text"}},
	}
	tests := []struct {
		name   string
		vector []float32
		want   string
	}{
		{name: "zero", vector: []float32{0, 0, 0, 0}, want: "zero"},
		{name: "non finite", vector: []float32{1, 0, 0, float32(math.NaN())}, want: "non-finite"},
		{name: "not normalized", vector: []float32{2, 0, 0, 0}, want: "outside tolerance"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateSemanticEmbeddingResults(req, []SemanticVector{{SubjectID: "subject", SegmentID: "segment", Vector: tt.vector}})
			if got == nil || !strings.Contains(got.Error(), tt.want) {
				t.Fatalf("validation error = %v, want substring %q", got, tt.want)
			}
			if !errors.Is(got, ErrSemanticProviderUnavailable) {
				t.Fatalf("validation error = %v, want ErrSemanticProviderUnavailable", got)
			}
		})
	}
}

func TestValidateSemanticEmbeddingResultsAcceptsApproximatelyNormalizedL2(t *testing.T) {
	manifest := testZvecManifest()
	req := SemanticEmbeddingRequest{
		Purpose: SemanticEmbeddingQuery, GenerationID: "generation", Manifest: manifest,
		Inputs: []SemanticTextInput{{SegmentID: "query", Text: "text"}},
	}
	// This norm is within float32/BGE-sized rounding error but not exactly 1.
	vector := []float32{0.6002, 0.7997, 0, 0}
	if err := validateSemanticEmbeddingResults(req, []SemanticVector{{SegmentID: "query", Vector: vector}}); err != nil {
		t.Fatalf("approximately normalized vector rejected: %v", err)
	}
}

func TestValidateSemanticEmbeddingResultsSkipsL2RequirementForOtherNormalization(t *testing.T) {
	manifest := testZvecManifest()
	manifest.Normalization = "none"
	req := SemanticEmbeddingRequest{
		Purpose: SemanticEmbeddingDocument, GenerationID: "generation", Manifest: manifest,
		Inputs: []SemanticTextInput{{SubjectID: "subject", SegmentID: "segment", Text: "text"}},
	}
	if err := validateSemanticEmbeddingResults(req, []SemanticVector{{SubjectID: "subject", SegmentID: "segment", Vector: []float32{2, 0, 0, 0}}}); err != nil {
		t.Fatalf("non-l2 vector rejected: %v", err)
	}
}
