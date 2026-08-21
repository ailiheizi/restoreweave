package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestPublicationFenceContendsExpiresAndPersistsMonotonicToken(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	first := openTestStore(t, path)
	second, err := Open(ctx, path, Options{BusyTimeout: 2 * time.Second, Now: func() time.Time { return testEpoch }})
	if err != nil {
		t.Fatalf("open second store: %v", err)
	}
	defer first.Close()
	defer second.Close()

	domain := "workspace/publication"
	start := testEpoch
	until := start.Add(time.Minute)
	var firstFence PublicationFence
	if err := first.Update(ctx, func(tx *Tx) error {
		var err error
		firstFence, err = tx.AcquirePublicationFence(ctx, domain, "worker-a", "lease-a", start, until)
		return err
	}); err != nil {
		t.Fatalf("first acquisition: %v", err)
	}
	if firstFence.FencingToken != 1 {
		t.Fatalf("first fencing token = %d, want 1", firstFence.FencingToken)
	}
	if err := second.Update(ctx, func(tx *Tx) error {
		_, err := tx.AcquirePublicationFence(ctx, domain, "worker-b", "lease-b", start, until)
		return err
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("active competing acquisition = %v, want ErrConflict", err)
	}
	if err := first.Update(ctx, func(tx *Tx) error {
		if err := tx.ValidatePublicationFence(ctx, domain, "worker-a", "lease-a", 1, start.Add(10*time.Second)); err != nil {
			return err
		}
		renewed, err := tx.AcquirePublicationFence(ctx, domain, "worker-a", "lease-a", start.Add(10*time.Second), start.Add(2*time.Minute))
		if err != nil {
			return err
		}
		if renewed.FencingToken != 1 {
			return errors.New("idempotent renewal advanced fencing token")
		}
		return nil
	}); err != nil {
		t.Fatalf("same-token renewal: %v", err)
	}

	expired := start.Add(3 * time.Minute)
	var secondFence PublicationFence
	if err := second.Update(ctx, func(tx *Tx) error {
		var err error
		secondFence, err = tx.AcquirePublicationFence(ctx, domain, "worker-b", "lease-b", expired, expired.Add(time.Minute))
		return err
	}); err != nil {
		t.Fatalf("expired takeover: %v", err)
	}
	if secondFence.FencingToken != 2 {
		t.Fatalf("takeover fencing token = %d, want 2", secondFence.FencingToken)
	}
	if err := first.Update(ctx, func(tx *Tx) error {
		return tx.ValidatePublicationFence(ctx, domain, "worker-a", "lease-a", 1, expired)
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale validation = %v, want ErrConflict", err)
	}
	if err := first.Update(ctx, func(tx *Tx) error {
		return tx.ReleasePublicationFence(ctx, domain, "worker-a", "lease-a", 1, expired)
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale release = %v, want ErrConflict", err)
	}
	if err := second.Update(ctx, func(tx *Tx) error {
		if err := tx.ValidatePublicationFence(ctx, domain, "worker-b", "lease-b", 2, expired); err != nil {
			return err
		}
		return tx.ReleasePublicationFence(ctx, domain, "worker-b", "lease-b", 2, expired)
	}); err != nil {
		t.Fatalf("current validation/release: %v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second: %v", err)
	}
	reopened, err := Open(ctx, path, Options{BusyTimeout: 2 * time.Second, Now: func() time.Time { return testEpoch }})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	if err := reopened.Update(ctx, func(tx *Tx) error {
		fence, err := tx.AcquirePublicationFence(ctx, domain, "worker-c", "lease-c", expired.Add(time.Minute), expired.Add(2*time.Minute))
		if err != nil {
			return err
		}
		if fence.FencingToken != 3 {
			return errors.New("reopened store did not preserve monotonic fencing token")
		}
		return nil
	}); err != nil {
		t.Fatalf("reopened acquisition: %v", err)
	}
}

func TestPublicationFenceRejectsExpiredTokenReuse(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	defer store.Close()
	start := testEpoch
	if err := store.Update(ctx, func(tx *Tx) error {
		_, err := tx.AcquirePublicationFence(ctx, "domain", "worker", "lease", start, start.Add(time.Second))
		return err
	}); err != nil {
		t.Fatalf("initial acquisition: %v", err)
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		_, err := tx.AcquirePublicationFence(ctx, "domain", "worker", "lease", start.Add(2*time.Second), start.Add(3*time.Second))
		return err
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired token reuse = %v, want ErrConflict", err)
	}
}
