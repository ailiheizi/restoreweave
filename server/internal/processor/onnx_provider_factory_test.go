package processor

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

func TestONNXSemanticProviderFactoryInvalidationFailsClosedAndStopsWorkers(t *testing.T) {
	factory, err := NewONNXSemanticEmbeddingProviderFactory(ONNXWorkerSupervisorOptions{
		BundleRoot:     "bundle",
		ConfigDigest:   "sha256:config",
		FenceToken:     7,
		FenceValidator: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	factory.ready.Store(true)
	var closed atomic.Bool
	if _, ok := factory.registerWorker(func() error { closed.Store(true); return nil }); !ok {
		t.Fatal("register active worker")
	}

	factory.Invalidate(errors.New("lease lost"))
	if factory.SemanticReady() {
		t.Fatal("invalidated provider remained ready")
	}
	if !closed.Load() {
		t.Fatal("invalidated provider did not close its active worker")
	}
	if failure := factory.SemanticFailure(); !strings.Contains(failure, "lease lost") {
		t.Fatalf("failure = %q", failure)
	}
	_, err = factory.Embed(context.Background(), search.SemanticEmbeddingRequest{GenerationID: "gen"})
	if !errors.Is(err, search.ErrSemanticProviderUnavailable) {
		t.Fatalf("Embed error = %v, want unavailable", err)
	}
}

func TestONNXSemanticProviderFactoryValidatorFailureInvalidatesProvider(t *testing.T) {
	factory, err := NewONNXSemanticEmbeddingProviderFactory(ONNXWorkerSupervisorOptions{
		BundleRoot:     "bundle",
		ConfigDigest:   "sha256:config",
		FenceToken:     7,
		FenceValidator: func(context.Context) error { return errors.New("renew failed") },
	})
	if err != nil {
		t.Fatal(err)
	}
	factory.ready.Store(true)
	if err := factory.validateFence(context.Background()); err == nil {
		t.Fatal("validator failure was accepted")
	}
	if factory.SemanticReady() {
		t.Fatal("provider remained ready after validator failure")
	}
	if failure := factory.SemanticFailure(); !strings.Contains(failure, "renew failed") {
		t.Fatalf("failure = %q", failure)
	}
}
