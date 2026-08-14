package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

var (
	ErrHandleClosed     = errors.New("processor handle is closed")
	ErrAlreadySealed    = errors.New("staging handle is already sealed")
	ErrNotSealed        = errors.New("staging handle is not sealed")
	ErrOutputTooLarge   = errors.New("staging output exceeds the host budget")
	ErrSourcePathDenied = errors.New("processors cannot open ambient source paths")
)

// SourceHandle is an opaque read of pinned exact bytes. It never exposes a
// repository path or credential.
type SourceHandle struct {
	id      string
	body    []byte
	content string
	closed  atomic.Bool
}

func (h *SourceHandle) ID() string { return h.id }

func (h *SourceHandle) ContentID() string { return h.content }

func (h *SourceHandle) ReadAll(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h == nil || h.closed.Load() {
		return nil, ErrHandleClosed
	}
	return append([]byte(nil), h.body...), nil
}

func (h *SourceHandle) Close() error {
	if h != nil {
		h.closed.Store(true)
	}
	return nil
}

// StagingHandle is an attempt-fenced write object. The processor may write
// only while unsealed and may request exactly one seal.
type StagingHandle struct {
	id       string
	path     string
	maxBytes int64
	keepFile bool
	mu       sync.Mutex
	file     *os.File
	written  int64
	sealed   bool
	digest   string
	closed   bool
}

func (h *StagingHandle) ID() string { return h.id }

func (h *StagingHandle) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0, ErrHandleClosed
	}
	if h.sealed {
		return 0, ErrAlreadySealed
	}
	if h.file == nil {
		return 0, ErrHandleClosed
	}
	if h.maxBytes > 0 && h.written+int64(len(p)) > h.maxBytes {
		return 0, ErrOutputTooLarge
	}
	n, err := h.file.Write(p)
	h.written += int64(n)
	return n, err
}

func (h *StagingHandle) Seal() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrHandleClosed
	}
	if h.sealed {
		return ErrAlreadySealed
	}
	if h.file == nil {
		return ErrHandleClosed
	}
	if err := h.file.Sync(); err != nil {
		return err
	}
	if _, err := h.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, h.file); err != nil {
		return err
	}
	h.digest = "sha256:" + hex.EncodeToString(sum.Sum(nil))
	h.sealed = true
	return nil
}

func (h *StagingHandle) sealedBytes() ([]byte, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.sealed {
		return nil, "", ErrNotSealed
	}
	if h.path != "" {
		body, err := os.ReadFile(h.path)
		if err != nil {
			return nil, "", err
		}
		return body, h.digest, nil
	}
	if h.file == nil {
		return nil, "", ErrHandleClosed
	}
	if _, err := h.file.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	body, err := io.ReadAll(h.file)
	if err != nil {
		return nil, "", err
	}
	return body, h.digest, nil
}

func (h *StagingHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	if h.file != nil {
		_ = h.file.Close()
		h.file = nil
	}
	if !h.keepFile && h.path != "" {
		_ = os.Remove(h.path)
	}
	return nil
}

func openSource(ctx context.Context, repo repository.Driver, contentID string, maxBytes int64) (*SourceHandle, error) {
	body, err := repo.Open(ctx, contentID)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	limited := io.LimitReader(body, maxBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(payload)) > maxBytes {
		payload = payload[:maxBytes]
	}
	sum := sha256.Sum256([]byte(contentID))
	return &SourceHandle{
		id:      "sch_" + hex.EncodeToString(sum[:8]),
		body:    payload,
		content: contentID,
	}, nil
}

func openStaging(dir string, attemptID string, maxBytes int64) (*StagingHandle, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, attemptID+"-*.stage")
	if err != nil {
		return nil, fmt.Errorf("allocate staging: %w", err)
	}
	return &StagingHandle{
		id:       "swh_" + filepath.Base(file.Name()),
		path:     file.Name(),
		maxBytes: maxBytes,
		file:     file,
	}, nil
}

// SourceFromFile reads a bounded snapshot from a pre-opened descriptor.
// Processors still cannot open ambient source paths.
func SourceFromFile(ctx context.Context, file *os.File, maxBytes int64) (*SourceHandle, error) {
	if file == nil {
		return nil, errors.New("source file is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxSourceBytes
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maxBytes {
		payload = payload[:maxBytes]
	}
	sum := sha256.Sum256(payload)
	return &SourceHandle{
		id:      "sch_" + hex.EncodeToString(sum[:8]),
		body:    payload,
		content: "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

// StagingFromFile wraps a pre-opened host-owned staging descriptor. Close does
// not unlink the file; the host retains it for independent digesting.
func StagingFromFile(file *os.File, maxBytes int64) (*StagingHandle, error) {
	if file == nil {
		return nil, errors.New("staging file is required")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return &StagingHandle{
		id:       "swh_fd",
		maxBytes: maxBytes,
		keepFile: true,
		file:     file,
	}, nil
}
