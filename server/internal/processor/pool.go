package processor

import (
	"context"
	"fmt"
	"time"
)

type pool struct {
	slots   chan struct{}
	timeout time.Duration
}

func newPool(size int, timeout time.Duration) *pool {
	if size < 1 {
		size = 1
	}
	if timeout <= 0 {
		timeout = defaultStageTimeout
	}
	slots := make(chan struct{}, size)
	for i := 0; i < size; i++ {
		slots <- struct{}{}
	}
	return &pool{slots: slots, timeout: timeout}
}

func (p *pool) run(ctx context.Context, fn func(context.Context) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.slots:
	}

	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var runErr error
		defer func() {
			if rec := recover(); rec != nil {
				runErr = fmt.Errorf("processor panic: %v", rec)
			}
			done <- runErr
			// A non-cooperative processor keeps its concurrency slot until it
			// actually exits. This bounds leaked work after the caller's
			// deadline without making the caller wait indefinitely.
			p.slots <- struct{}{}
		}()
		runErr = fn(runCtx)
	}()
	select {
	case err := <-done:
		return err
	case <-runCtx.Done():
		return runCtx.Err()
	}
}
