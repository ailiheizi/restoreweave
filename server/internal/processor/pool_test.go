package processor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPoolTimeoutDoesNotWaitForUncooperativeProcessor(t *testing.T) {
	pool := newPool(1, 20*time.Millisecond)
	release := make(chan struct{})
	started := time.Now()
	err := pool.run(context.Background(), func(context.Context) error {
		<-release
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("timeout waited %s for an uncooperative processor", elapsed)
	}

	blockedCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := pool.run(blockedCtx, func(context.Context) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("occupied pool error = %v, want deadline exceeded", err)
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for {
		err = pool.run(context.Background(), func(context.Context) error { return nil })
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pool slot was not released after processor exit: %v", err)
		}
	}
}
