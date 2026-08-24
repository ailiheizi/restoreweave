//go:build !purego

package search

import (
	"context"
	"fmt"
)

type unavailableZvecGenerationDriver struct{}

func newZvecGenerationBackend(string) ZvecGenerationDriver {
	return unavailableZvecGenerationDriver{}
}

func (unavailableZvecGenerationDriver) Build(context.Context, ZvecGenerationSpec, []ZvecSegment) (ZvecGenerationReceipt, error) {
	return ZvecGenerationReceipt{}, fmt.Errorf("%w: build requires the opt-in purego zvec backend", ErrZvecUnavailable)
}

func (unavailableZvecGenerationDriver) Open(context.Context, ZvecGenerationSpec) (ZvecGeneration, error) {
	return nil, fmt.Errorf("%w: open requires the opt-in purego zvec backend", ErrZvecUnavailable)
}

func (unavailableZvecGenerationDriver) Coverage(context.Context, ZvecGenerationSpec) ([]string, error) {
	return nil, fmt.Errorf("%w: coverage requires the opt-in purego zvec backend", ErrZvecUnavailable)
}

func (unavailableZvecGenerationDriver) ZvecReady(string, string, EmbeddingGenerationManifest) bool {
	return false
}
