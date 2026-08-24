package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Component is one QueryProvider invocation inside host-owned result fusion.
type Component struct {
	Dimension      string
	Provider       string
	GenerationID   string
	ScoreSemantics string
	Status         string
	Hits           int
}

// FusedHit is a subject that one or more components returned.
type FusedHit struct {
	Hit
	Dimensions []string
}

// FuseResult unions typed component results without inventing a hybrid score.
type FuseResult struct {
	Hits       []FusedHit
	Components []Component
}

// Fuse queries each named dimension separately and unions authorized
// candidates by SubjectID. Score semantics stay per component.
func (idx *Indexer) Fuse(ctx context.Context, req QueryRequest) (FuseResult, error) {
	var result FuseResult
	if idx == nil || idx.Store == nil || idx.Engine == nil {
		return result, errors.New("search indexer requires a catalog and engine")
	}
	filters, err := NormalizeFilters(req.Filters)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}
	req.Filters = filters
	dims, err := NormalizeFuse(req.Fuse)
	if err != nil {
		return result, err
	}
	seen := map[string]int{}
	for _, id := range dims {
		dimension, ok := LookupDimension(id, IndexerReadiness(idx))
		component := Component{
			Dimension:      id,
			Provider:       dimension.Provider,
			ScoreSemantics: dimension.ScoreSemantics,
			Status:         "DEGRADED",
		}
		if !ok || dimension.State != "AVAILABLE" {
			result.Components = append(result.Components, component)
			continue
		}
		generation, hits, err := idx.Query(ctx, QueryRequest{
			WorkspaceID: req.WorkspaceID,
			Dimension:   id,
			Text:        req.Text,
			Axes:        req.Axes,
			Filters:     req.Filters,
		})
		component.GenerationID = generation.ID
		if err != nil {
			result.Components = append(result.Components, component)
			continue
		}
		component.Status = "SUCCEEDED"
		component.Hits = len(hits)
		result.Components = append(result.Components, component)
		for _, hit := range hits {
			if at, ok := seen[hit.SubjectID]; ok {
				result.Hits[at].Dimensions = appendUnique(result.Hits[at].Dimensions, id)
				continue
			}
			seen[hit.SubjectID] = len(result.Hits)
			result.Hits = append(result.Hits, FusedHit{Hit: hit, Dimensions: []string{id}})
		}
	}
	return result, nil
}

func FuseSucceeded(result FuseResult) bool {
	for _, component := range result.Components {
		if component.Status == "SUCCEEDED" {
			return true
		}
	}
	return false
}

func appendUnique(values []string, add string) []string {
	add = strings.TrimSpace(add)
	for _, value := range values {
		if value == add {
			return values
		}
	}
	return append(values, add)
}
