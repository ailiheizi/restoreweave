package processor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"
)

const textHandlePrefix = "th_"

var (
	ErrTextHandleClosed          = errors.New("text handle store is closed")
	ErrTextHandleInvalid         = errors.New("text handle is invalid")
	ErrTextHandleNotFound        = errors.New("text handle was not found")
	ErrTextHandleExpired         = errors.New("text handle has expired")
	ErrTextHandleNotUTF8         = errors.New("text handle text is not valid UTF-8")
	ErrTextHandleLimit           = errors.New("text handle byte limit exceeded")
	ErrTextHandleDigestMismatch  = errors.New("text handle digest does not match")
	ErrTextHandleLengthMismatch  = errors.New("text handle length does not match")
	ErrTextHandleBindingMismatch = errors.New("text handle invocation binding does not match")
)

// TextHandle is the host-owned reference passed to an embedding worker. The
// text bytes are deliberately absent; callers must retain only this binding.
type TextHandle struct {
	ID      string
	Digest  string
	Bytes   int64
	Binding TextHandleBinding
}

// TextHandleBinding is exactly the comparable host-owned EMBED_TEXT invocation
// identity. A handle cannot cross a purpose, request, attempt, lease, fence,
// generation, worker profile, or preprocessing boundary.
type TextHandleBinding = EmbedTextInvocationBinding

type textHandleEntry struct {
	text      []byte
	digest    string
	binding   TextHandleBinding
	expiresAt time.Time
}

// TextHandleStore is the host-owned seam used by EMBED_TEXT. Implementations
// must keep text out of the request and must not resolve ambient paths.
type TextHandleStore interface {
	Issue(context.Context, TextHandleBinding, []byte) (TextHandle, error)
	Consume(context.Context, string, TextHandleBinding, string, int64) ([]byte, error)
	Resolve(context.Context, string, TextHandleBinding, string, int64) ([]byte, error)
	Revoke(context.Context, string, TextHandleBinding) error
	Close() error
}

// InMemoryTextHandleStore keeps bounded UTF-8 text in memory for one host operation.
// It has no filesystem or ambient-path behavior and is safe for concurrent
// issue, resolve, revoke, and close calls.
type InMemoryTextHandleStore struct {
	mu             sync.RWMutex
	handles        map[string]textHandleEntry
	maxTotalBytes  int64
	maxHandleBytes int64
	ttl            time.Duration
	totalBytes     int64
	closed         bool
}

func NewTextHandleStore(maxTotalBytes, maxHandleBytes int64) (*InMemoryTextHandleStore, error) {
	return newTextHandleStore(maxTotalBytes, maxHandleBytes, 0)
}

// NewExpiringTextHandleStore enables an optional host-wide lifetime. A zero
// lifetime means handles remain valid until Revoke or Close.
func NewExpiringTextHandleStore(maxTotalBytes, maxHandleBytes int64, ttl time.Duration) (*InMemoryTextHandleStore, error) {
	if ttl < 0 {
		return nil, fmt.Errorf("%w: expiry must not be negative", ErrTextHandleLimit)
	}
	return newTextHandleStore(maxTotalBytes, maxHandleBytes, ttl)
}

func newTextHandleStore(maxTotalBytes, maxHandleBytes int64, ttl time.Duration) (*InMemoryTextHandleStore, error) {
	if maxTotalBytes <= 0 || maxHandleBytes <= 0 || maxHandleBytes > maxTotalBytes {
		return nil, fmt.Errorf("%w: total and per-handle limits must be positive and per-handle must not exceed total", ErrTextHandleLimit)
	}
	return &InMemoryTextHandleStore{
		handles:        make(map[string]textHandleEntry),
		maxTotalBytes:  maxTotalBytes,
		maxHandleBytes: maxHandleBytes,
		ttl:            ttl,
	}, nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// Issue copies text into the store and returns its canonical SHA-256 binding.
func (s *InMemoryTextHandleStore) Issue(ctx context.Context, binding TextHandleBinding, text []byte) (TextHandle, error) {
	if err := contextErr(ctx); err != nil {
		return TextHandle{}, err
	}
	if !validTextHandleBinding(binding) {
		return TextHandle{}, ErrTextHandleBindingMismatch
	}
	if !utf8.Valid(text) {
		return TextHandle{}, ErrTextHandleNotUTF8
	}
	length := int64(len(text))
	if length <= 0 {
		return TextHandle{}, fmt.Errorf("%w: text must not be empty", ErrTextHandleLimit)
	}
	sum := sha256.Sum256(text)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := contextErr(ctx); err != nil {
		return TextHandle{}, err
	}
	if s.closed {
		return TextHandle{}, ErrTextHandleClosed
	}
	if length > s.maxHandleBytes || s.totalBytes > s.maxTotalBytes-length {
		return TextHandle{}, ErrTextHandleLimit
	}
	id, err := s.newIDLocked()
	if err != nil {
		return TextHandle{}, err
	}
	var expiresAt time.Time
	if s.ttl > 0 {
		expiresAt = time.Now().Add(s.ttl)
	}
	s.handles[id] = textHandleEntry{text: append([]byte(nil), text...), digest: digest, binding: binding, expiresAt: expiresAt}
	s.totalBytes += length
	return TextHandle{ID: id, Digest: digest, Bytes: length, Binding: binding}, nil
}

func (s *InMemoryTextHandleStore) newIDLocked() (string, error) {
	var raw [16]byte
	for attempts := 0; attempts < 8; attempts++ {
		if _, err := rand.Read(raw[:]); err != nil {
			return "", fmt.Errorf("generate text handle: %w", err)
		}
		id := textHandlePrefix + hex.EncodeToString(raw[:])
		if _, exists := s.handles[id]; !exists {
			return id, nil
		}
	}
	return "", errors.New("generate unique text handle: random collision")
}

// Resolve is a consuming resolution: a valid handle can release its text at
// most once. Callers that retry must issue a new handle.
func (s *InMemoryTextHandleStore) Resolve(ctx context.Context, id string, binding TextHandleBinding, expectedDigest string, expectedBytes int64) ([]byte, error) {
	return s.Consume(ctx, id, binding, expectedDigest, expectedBytes)
}

// Consume atomically validates the invocation and text identity, returns a
// copy, and invalidates the handle before releasing the store lock.
func (s *InMemoryTextHandleStore) Consume(ctx context.Context, id string, binding TextHandleBinding, expectedDigest string, expectedBytes int64) ([]byte, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if !validTextHandleID(id) {
		return nil, ErrTextHandleInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if s.closed {
		return nil, ErrTextHandleClosed
	}
	entry, ok := s.handles[id]
	if !ok {
		return nil, ErrTextHandleNotFound
	}
	if !entry.expiresAt.IsZero() && !time.Now().Before(entry.expiresAt) {
		delete(s.handles, id)
		s.totalBytes -= int64(len(entry.text))
		return nil, ErrTextHandleExpired
	}
	if entry.binding != binding {
		return nil, ErrTextHandleBindingMismatch
	}
	if expectedBytes != int64(len(entry.text)) {
		return nil, ErrTextHandleLengthMismatch
	}
	if !equalTextHandleDigest(expectedDigest, entry.digest) {
		return nil, ErrTextHandleDigestMismatch
	}
	text := append([]byte(nil), entry.text...)
	delete(s.handles, id)
	s.totalBytes -= int64(len(entry.text))
	return text, nil
}

// Revoke invalidates one handle and releases its bounded memory.
func (s *InMemoryTextHandleStore) Revoke(ctx context.Context, id string, binding TextHandleBinding) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if !validTextHandleID(id) {
		return ErrTextHandleInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := contextErr(ctx); err != nil {
		return err
	}
	if s.closed {
		return ErrTextHandleClosed
	}
	entry, ok := s.handles[id]
	if !ok {
		return ErrTextHandleNotFound
	}
	if entry.binding != binding {
		return ErrTextHandleBindingMismatch
	}
	delete(s.handles, id)
	s.totalBytes -= int64(len(entry.text))
	return nil
}

// Close invalidates all handles and is idempotent.
func (s *InMemoryTextHandleStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.handles = nil
	s.totalBytes = 0
	return nil
}

func validTextHandleID(id string) bool {
	if len(id) != len(textHandlePrefix)+16*2 || id[:len(textHandlePrefix)] != textHandlePrefix {
		return false
	}
	for _, r := range id[len(textHandlePrefix):] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func validTextHandleBinding(binding TextHandleBinding) bool {
	if binding.Purpose != EmbedTextPurposeQuery && binding.Purpose != EmbedTextPurposeDocument {
		return false
	}
	if binding.Operation != EmbedTextOperation ||
		!validateEmbedTextToken(binding.SessionID) || !validateEmbedTextToken(binding.OperationID) ||
		!validateEmbedTextToken(binding.RequestID) || !validateEmbedTextToken(binding.InvocationID) ||
		!validateEmbedTextToken(binding.AttemptID) || !validateEmbedTextToken(binding.IdempotencyKey) ||
		!validateEmbedTextToken(binding.LeaseID) || binding.FenceToken <= 0 ||
		!validateEmbedTextToken(binding.GenerationID) {
		return false
	}
	return ValidateEmbedTextDigest(binding.WorkerDigest) == nil &&
		ValidateEmbedTextDigest(binding.WorkerProfileDigest) == nil &&
		ValidateEmbedTextDigest(binding.AppliedPreprocessingDigest) == nil
}

func equalTextHandleDigest(got, want string) bool {
	if len(got) != len("sha256:")+sha256.Size*2 || len(want) != len(got) || got[:len("sha256:")] != "sha256:" || want[:len("sha256:")] != "sha256:" {
		return false
	}
	gotBytes, gotErr := hex.DecodeString(got[len("sha256:"):])
	wantBytes, wantErr := hex.DecodeString(want[len("sha256:"):])
	return gotErr == nil && wantErr == nil && subtle.ConstantTimeCompare(gotBytes, wantBytes) == 1
}
