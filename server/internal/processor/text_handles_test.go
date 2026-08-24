package processor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func testHandleBinding() TextHandleBinding {
	return testEmbedRequest().Binding
}

func TestTextHandleIssueResolveBindsUTF8DigestAndLength(t *testing.T) {
	store, err := NewTextHandleStore(128, 64)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("恢复核心")
	binding := testHandleBinding()
	handle, err := store.Issue(context.Background(), binding, want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(handle.ID, textHandlePrefix) || handle.Bytes != int64(len(want)) || !strings.HasPrefix(handle.Digest, "sha256:") {
		t.Fatalf("handle = %+v", handle)
	}
	if strings.Contains(handle.ID, string(want)) {
		t.Fatal("handle appears to contain text")
	}
	got, err := store.Resolve(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("resolved text = %q, want %q", got, want)
	}
	got[0] = 'X'
	if _, err := store.Resolve(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleNotFound) {
		t.Fatalf("resolve was not single-use: %v", err)
	}
	// A retry receives a newly issued handle rather than replaying the consumed
	// invocation binding.
	retry, err := store.Issue(context.Background(), binding, want)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Consume(context.Background(), retry.ID, binding, retry.Digest, retry.Bytes)
	if err != nil || string(again) != string(want) {
		t.Fatalf("consume retry = %q, %v", again, err)
	}
}

func TestTextHandleConsumeAcceptsQueryAndDocumentExactlyOnce(t *testing.T) {
	for _, purpose := range []EmbedTextPurpose{EmbedTextPurposeQuery, EmbedTextPurposeDocument} {
		t.Run(string(purpose), func(t *testing.T) {
			store, err := NewTextHandleStore(128, 64)
			if err != nil {
				t.Fatal(err)
			}
			binding := testHandleBinding()
			binding.Purpose = purpose
			if purpose == EmbedTextPurposeQuery {
				binding.AppliedPreprocessingDigest = testDigest("c")
			}
			handle, err := store.Issue(context.Background(), binding, []byte("bounded text"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Consume(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes); err != nil {
				t.Fatalf("consume: %v", err)
			}
			if _, err := store.Consume(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleNotFound) {
				t.Fatalf("replay = %v", err)
			}
		})
	}
}

func TestTextHandleConsumeRejectsEveryStaleInvocationField(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*TextHandleBinding)
	}{
		{"purpose", func(b *TextHandleBinding) { b.Purpose = EmbedTextPurposeQuery }},
		{"session", func(b *TextHandleBinding) { b.SessionID = "session-2" }},
		{"operation", func(b *TextHandleBinding) { b.OperationID = "operation-2" }},
		{"request", func(b *TextHandleBinding) { b.RequestID = "request-2" }},
		{"invocation", func(b *TextHandleBinding) { b.InvocationID = "invocation-2" }},
		{"attempt", func(b *TextHandleBinding) { b.AttemptID = "attempt-2" }},
		{"idempotency", func(b *TextHandleBinding) { b.IdempotencyKey = "idempotency-2" }},
		{"lease", func(b *TextHandleBinding) { b.LeaseID = "lease-2" }},
		{"fence", func(b *TextHandleBinding) { b.FenceToken++ }},
		{"generation", func(b *TextHandleBinding) { b.GenerationID = "generation-2" }},
		{"worker", func(b *TextHandleBinding) { b.WorkerDigest = testDigest("9") }},
		{"profile", func(b *TextHandleBinding) { b.WorkerProfileDigest = testDigest("9") }},
		{"preprocessing", func(b *TextHandleBinding) { b.AppliedPreprocessingDigest = testDigest("9") }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewTextHandleStore(128, 64)
			if err != nil {
				t.Fatal(err)
			}
			binding := testHandleBinding()
			handle, err := store.Issue(context.Background(), binding, []byte("text"))
			if err != nil {
				t.Fatal(err)
			}
			stale := binding
			tc.mutate(&stale)
			if _, err := store.Consume(context.Background(), handle.ID, stale, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleBindingMismatch) {
				t.Fatalf("stale consume = %v", err)
			}
			if _, err := store.Consume(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes); err != nil {
				t.Fatalf("stale attempt consumed valid handle: %v", err)
			}
		})
	}
}

func TestTextHandleResolveRejectsExpectedIdentityMismatch(t *testing.T) {
	store, err := NewTextHandleStore(128, 64)
	if err != nil {
		t.Fatal(err)
	}
	binding := testHandleBinding()
	handle, err := store.Issue(context.Background(), binding, []byte("text"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes+1); !errors.Is(err, ErrTextHandleLengthMismatch) {
		t.Fatalf("length mismatch = %v", err)
	}
	if _, err := store.Resolve(context.Background(), handle.ID, binding, "sha256:"+strings.Repeat("0", 64), handle.Bytes); !errors.Is(err, ErrTextHandleDigestMismatch) {
		t.Fatalf("digest mismatch = %v", err)
	}
	if _, err := store.Resolve(context.Background(), "bad", binding, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleInvalid) {
		t.Fatalf("invalid id = %v", err)
	}
	otherBinding := binding
	otherBinding.SessionID = "session-2"
	if _, err := store.Resolve(context.Background(), handle.ID, otherBinding, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleBindingMismatch) {
		t.Fatalf("cross-session replay = %v", err)
	}
	if _, err := store.Resolve(context.Background(), "th_00000000000000000000000000000000/../x", binding, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleInvalid) {
		t.Fatalf("path-like handle = %v", err)
	}
}

func TestTextHandleRevokeCloseAndLimits(t *testing.T) {
	store, err := NewTextHandleStore(6, 4)
	if err != nil {
		t.Fatal(err)
	}
	binding := testHandleBinding()
	if _, err := store.Issue(context.Background(), binding, []byte("12345")); !errors.Is(err, ErrTextHandleLimit) {
		t.Fatalf("per-handle limit = %v", err)
	}
	handle, err := store.Issue(context.Background(), binding, []byte("1234"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Issue(context.Background(), binding, []byte("12")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Issue(context.Background(), binding, []byte("1")); !errors.Is(err, ErrTextHandleLimit) {
		t.Fatalf("total limit = %v", err)
	}
	if err := store.Revoke(context.Background(), handle.ID, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleNotFound) {
		t.Fatalf("revoked handle = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Issue(context.Background(), binding, []byte("x")); !errors.Is(err, ErrTextHandleClosed) {
		t.Fatalf("issue after close = %v", err)
	}
	if _, err := store.Resolve(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleClosed) {
		t.Fatalf("resolve after close = %v", err)
	}
	if err := store.Revoke(context.Background(), handle.ID, binding); !errors.Is(err, ErrTextHandleClosed) {
		t.Fatalf("revoke after close = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second close = %v", err)
	}
}

func TestTextHandleUnknownAndExpiredHandlesFailClosed(t *testing.T) {
	store, err := NewTextHandleStore(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	binding := testHandleBinding()
	if _, err := store.Resolve(context.Background(), "th_00000000000000000000000000000000", binding, "sha256:"+strings.Repeat("0", 64), 1); !errors.Is(err, ErrTextHandleNotFound) {
		t.Fatalf("unknown handle = %v", err)
	}
	expiring, err := NewExpiringTextHandleStore(64, 64, time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := expiring.Issue(context.Background(), binding, []byte("expire"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, err := expiring.Resolve(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleExpired) {
		t.Fatalf("expired handle = %v", err)
	}
}

func TestTextHandleDoesNotLeakAmbientPaths(t *testing.T) {
	store, err := NewTextHandleStore(128, 128)
	if err != nil {
		t.Fatal(err)
	}
	path := "/private/restoreweave/source/secret.txt"
	handle, err := store.Issue(context.Background(), testHandleBinding(), []byte(path))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(handle.ID, path) || strings.Contains(handle.Digest, path) {
		t.Fatalf("handle leaked source path: %+v", handle)
	}
}

func TestTextHandleRejectsInvalidUTF8AndCanceledContext(t *testing.T) {
	store, err := NewTextHandleStore(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	binding := testHandleBinding()
	if _, err := store.Issue(context.Background(), binding, []byte{0xff}); !errors.Is(err, ErrTextHandleNotUTF8) {
		t.Fatalf("invalid UTF-8 = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Issue(ctx, binding, []byte("text")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled issue = %v", err)
	}
	if _, err := store.Resolve(ctx, "bad", binding, "", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled resolve = %v", err)
	}
	if err := store.Revoke(ctx, "bad", binding); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled revoke = %v", err)
	}
}

func TestTextHandleConcurrentResolveRevoke(t *testing.T) {
	store, err := NewTextHandleStore(1<<20, 1024)
	if err != nil {
		t.Fatal(err)
	}
	binding := testHandleBinding()
	handle, err := store.Issue(context.Background(), binding, []byte("concurrent text"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = store.Resolve(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = store.Revoke(context.Background(), handle.ID, binding)
	}()
	wg.Wait()
	if _, err := store.Resolve(context.Background(), handle.ID, binding, handle.Digest, handle.Bytes); !errors.Is(err, ErrTextHandleNotFound) {
		t.Fatalf("post-revoke resolve = %v", err)
	}
}
