package processor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

// ONNXSemanticEmbeddingProviderFactory binds one admitted bundle and host
// lease policy to per-generation workers. A worker session is intentionally
// scoped to the request's generation ID, so document rebuilds and queries
// cannot cross-replay a generation-bound transport session.
type ONNXSemanticEmbeddingProviderFactory struct {
	options        ONNXWorkerSupervisorOptions
	leaseValidator func(context.Context) error
	ready          atomic.Bool
	invalid        atomic.Bool
	failure        atomic.Value
	mu             sync.Mutex
	workers        map[uint64]func() error
	nextID         uint64
}

func NewONNXSemanticEmbeddingProviderFactory(options ONNXWorkerSupervisorOptions) (*ONNXSemanticEmbeddingProviderFactory, error) {
	if options.BundleRoot == "" || options.ConfigDigest == "" || options.FenceToken <= 0 || options.FenceValidator == nil {
		return nil, errors.New("ONNX semantic provider factory requires bundle, config, and lease bindings")
	}
	factory := &ONNXSemanticEmbeddingProviderFactory{
		options:        options,
		leaseValidator: options.FenceValidator,
		workers:        make(map[uint64]func() error),
	}
	factory.options.FenceValidator = factory.validateFence
	return factory, nil
}

func (f *ONNXSemanticEmbeddingProviderFactory) Embed(ctx context.Context, req search.SemanticEmbeddingRequest) ([]search.SemanticVector, error) {
	if f == nil || f.options.BundleRoot == "" || req.GenerationID == "" {
		return nil, search.ErrSemanticProviderUnavailable
	}
	if err := f.validateFence(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", search.ErrSemanticProviderUnavailable, err)
	}
	textHandles := f.options.TextHandles
	ownedHandles := false
	if textHandles == nil {
		var err error
		textHandles, err = NewTextHandleStore(MaxEmbedTextInputBytes*2, MaxEmbedTextInputBytes)
		if err != nil {
			return nil, search.ErrSemanticProviderUnavailable
		}
		ownedHandles = true
	}
	options := f.options
	options.GenerationID = req.GenerationID
	options.TextHandles = textHandles
	worker, closeWorker, err := StartONNXWorker(ctx, options)
	if err != nil {
		if ownedHandles {
			_ = textHandles.Close()
		}
		return nil, err
	}
	workerID, ok := f.registerWorker(closeWorker)
	if !ok {
		_ = closeWorker()
		if ownedHandles {
			_ = textHandles.Close()
		}
		return nil, search.ErrSemanticProviderUnavailable
	}
	defer func() {
		f.unregisterWorker(workerID)
		_ = closeWorker()
		if ownedHandles {
			_ = textHandles.Close()
		}
	}()
	provider, err := NewONNXSemanticEmbeddingProvider(worker)
	if err != nil {
		return nil, err
	}
	results, err := provider.Embed(ctx, req)
	if err == nil {
		f.ready.Store(true)
	}
	return results, err
}

// SemanticReady reports whether at least one real worker has
// completed a validated embedding call. Merely loading a descriptor never
// advertises semantic availability.
func (f *ONNXSemanticEmbeddingProviderFactory) SemanticReady() bool {
	return f != nil && f.ready.Load() && !f.invalid.Load()
}

// SemanticFailure exposes the host-owned reason that made the real provider
// unavailable without leaking worker details into the public search shape.
func (f *ONNXSemanticEmbeddingProviderFactory) SemanticFailure() string {
	if f == nil {
		return ""
	}
	value := f.failure.Load()
	failure, _ := value.(string)
	return failure
}

// Invalidate fails the provider closed and stops every active worker. A
// daemon restart must acquire a fresh lease before semantic work can resume.
func (f *ONNXSemanticEmbeddingProviderFactory) Invalidate(err error) {
	if f == nil {
		return
	}
	if err == nil {
		err = errors.New("semantic worker lease is unavailable")
	}
	f.failure.Store(err.Error())
	f.invalid.Store(true)
	f.ready.Store(false)
	f.mu.Lock()
	workers := make([]func() error, 0, len(f.workers))
	for workerID, closeWorker := range f.workers {
		workers = append(workers, closeWorker)
		delete(f.workers, workerID)
	}
	f.mu.Unlock()
	for _, closeWorker := range workers {
		_ = closeWorker()
	}
}

func (f *ONNXSemanticEmbeddingProviderFactory) validateFence(ctx context.Context) error {
	if f == nil || f.invalid.Load() {
		return errors.New("semantic worker lease is unavailable")
	}
	validator := f.leaseValidator
	if validator == nil {
		return errors.New("semantic worker lease validator is unavailable")
	}
	if err := validator(ctx); err != nil {
		f.Invalidate(err)
		return err
	}
	return nil
}

func (f *ONNXSemanticEmbeddingProviderFactory) registerWorker(closeWorker func() error) (uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.invalid.Load() {
		return 0, false
	}
	f.nextID++
	f.workers[f.nextID] = closeWorker
	return f.nextID, true
}

func (f *ONNXSemanticEmbeddingProviderFactory) unregisterWorker(workerID uint64) {
	f.mu.Lock()
	delete(f.workers, workerID)
	f.mu.Unlock()
}

var _ search.SemanticEmbeddingProvider = (*ONNXSemanticEmbeddingProviderFactory)(nil)
