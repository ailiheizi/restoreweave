package search

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
)

// SemanticEmbeddingPurpose distinguishes document/segment vectors from query
// vectors. The preprocessing contract is part of the bound manifest; callers
// must not reuse a query vector as a document projection.
type SemanticEmbeddingPurpose string

const (
	SemanticEmbeddingDocument SemanticEmbeddingPurpose = "DOCUMENT"
	SemanticEmbeddingQuery    SemanticEmbeddingPurpose = "QUERY"
)

type SemanticTextInput struct {
	SubjectID             string
	SegmentID             string
	DescriptionDocumentID string
	Ordinal               int64
	Language              string
	Text                  string
}

type SemanticVector struct {
	SubjectID string
	SegmentID string
	Vector    []float32
}

// SemanticEmbeddingRequest is a host-owned, bounded provider request. The
// provider receives durable segment text only; it never receives repository
// paths, index paths, credentials, or catalog write authority.
type SemanticEmbeddingRequest struct {
	Purpose      SemanticEmbeddingPurpose
	GenerationID string
	Manifest     EmbeddingGenerationManifest
	Inputs       []SemanticTextInput
}

// SemanticEmbeddingProvider is the narrow bridge between the processor host
// and the disposable semantic index. A provider must return exactly one
// finite vector for every input or fail closed.
type SemanticEmbeddingProvider interface {
	Embed(context.Context, SemanticEmbeddingRequest) ([]SemanticVector, error)
}

var ErrSemanticProviderUnavailable = errors.New("semantic embedding provider unavailable")

func validateSemanticEmbeddingRequest(req SemanticEmbeddingRequest) error {
	if err := req.Manifest.Validate(); err != nil {
		return fmt.Errorf("%w: manifest: %v", ErrSemanticProviderUnavailable, err)
	}
	if req.Purpose != SemanticEmbeddingDocument && req.Purpose != SemanticEmbeddingQuery {
		return fmt.Errorf("%w: unknown embedding purpose %q", ErrSemanticProviderUnavailable, req.Purpose)
	}
	if len(req.Inputs) == 0 || len(req.Inputs) > 256 {
		return fmt.Errorf("%w: input count is outside bounds", ErrSemanticProviderUnavailable)
	}
	seen := make(map[string]struct{}, len(req.Inputs))
	for _, input := range req.Inputs {
		if strings.TrimSpace(input.Text) == "" {
			return fmt.Errorf("%w: empty input text", ErrSemanticProviderUnavailable)
		}
		key := input.SegmentID
		if req.Purpose == SemanticEmbeddingQuery {
			key = input.SubjectID + "\x00" + input.SegmentID
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate input identity", ErrSemanticProviderUnavailable)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateSemanticEmbeddingResults(req SemanticEmbeddingRequest, results []SemanticVector) error {
	if len(results) != len(req.Inputs) {
		return fmt.Errorf("%w: provider returned %d vectors for %d inputs", ErrSemanticProviderUnavailable, len(results), len(req.Inputs))
	}
	want := make(map[string]SemanticTextInput, len(req.Inputs))
	for _, input := range req.Inputs {
		key := input.SegmentID
		if req.Purpose == SemanticEmbeddingQuery {
			key = input.SubjectID + "\x00" + input.SegmentID
		}
		want[key] = input
	}
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		key := result.SegmentID
		if req.Purpose == SemanticEmbeddingQuery {
			key = result.SubjectID + "\x00" + result.SegmentID
		}
		input, ok := want[key]
		if !ok {
			return fmt.Errorf("%w: provider returned an unknown input identity", ErrSemanticProviderUnavailable)
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: provider returned duplicate input identity", ErrSemanticProviderUnavailable)
		}
		seen[key] = struct{}{}
		if len(result.Vector) != req.Manifest.Dimension {
			return fmt.Errorf("%w: vector dimension %d does not match %d", ErrSemanticProviderUnavailable, len(result.Vector), req.Manifest.Dimension)
		}
		for _, value := range result.Vector {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return fmt.Errorf("%w: provider returned a non-finite vector", ErrSemanticProviderUnavailable)
			}
		}
		if req.Purpose == SemanticEmbeddingDocument && result.SubjectID != input.SubjectID {
			return fmt.Errorf("%w: provider changed subject identity", ErrSemanticProviderUnavailable)
		}
	}
	return nil
}
