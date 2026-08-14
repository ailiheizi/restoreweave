package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const maxContentRead = 1 << 20

type contentOpenInput struct {
	WorkspaceID string `json:"workspace_id"`
	EntryID     string `json:"entry_id"`
}

type contentReadInput struct {
	Handle string `json:"handle"`
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
}

type contentCloseInput struct {
	Handle string `json:"handle"`
}

type contentSession struct {
	handle      string
	workspaceID string
	entryID     string
	contentID   string
	logicalSize int64
}

type contentSessions struct {
	mu       sync.Mutex
	repo     repository.Driver
	byHandle map[string]contentSession
}

func newContentSessions(repo repository.Driver) *contentSessions {
	return &contentSessions{
		repo:     repo,
		byHandle: map[string]contentSession{},
	}
}

func (d *Dispatcher) handleContentOpen(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.sessions == nil {
		return unimplementedResult(env, started)
	}
	var input contentOpenInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("entry_id", input.EntryID); err != nil {
		return invalidInputResult(env, started, err)
	}
	entry, err := d.store.GetNamespaceEntry(ctx, input.WorkspaceID, input.EntryID)
	if err != nil {
		return namespaceLookupResult(env, started, err)
	}
	if entry.EntryType != sqlite.EntryFile {
		return invalidInputResult(env, started, errString("entry is not a regular file"))
	}
	if entry.ContentID == "" {
		return invalidInputResult(env, started, errString("entry has no exact content id"))
	}
	size := int64(0)
	if entry.LogicalSize != nil {
		size = *entry.LogicalSize
	}
	handle, err := newContentHandle()
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	d.sessions.put(contentSession{
		handle:      handle,
		workspaceID: input.WorkspaceID,
		entryID:     entry.ID,
		contentID:   entry.ContentID,
		logicalSize: size,
	})
	return succeeded(env, started, command.ContentOpenData{
		Handle:      handle,
		EntryID:     entry.ID,
		ContentID:   entry.ContentID,
		LogicalSize: size,
		MaxRead:     maxContentRead,
	})
}

func (d *Dispatcher) handleContentRead(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.sessions == nil {
		return unimplementedResult(env, started)
	}
	var input contentReadInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if input.Handle == "" {
		return invalidInputResult(env, started, errString("handle is required"))
	}
	if input.Offset < 0 {
		return invalidInputResult(env, started, errString("offset must be non-negative"))
	}
	session, ok := d.sessions.get(input.Handle)
	if !ok {
		return notFoundResult(env, started, "content handle not found")
	}
	length := input.Length
	if length < 0 {
		return invalidInputResult(env, started, errString("length must be non-negative"))
	}
	if length == 0 {
		remaining := session.logicalSize - input.Offset
		if remaining < 0 {
			remaining = 0
		}
		length = remaining
		if length > maxContentRead {
			length = maxContentRead
		}
	}
	if length > maxContentRead {
		return invalidInputResult(env, started, errString("read length exceeds 1MiB cap"))
	}
	payload, eof, err := d.sessions.read(ctx, session, input.Offset, length)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return notFoundResult(env, started, "exact content object not found")
		}
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, command.ContentReadData{
		Handle: input.Handle,
		Offset: input.Offset,
		Length: int64(len(payload)),
		Bytes:  payload,
		EOF:    eof,
	})
}

func (d *Dispatcher) handleContentClose(_ context.Context, env command.Envelope, started time.Time) command.Result {
	if d.sessions == nil {
		return unimplementedResult(env, started)
	}
	var input contentCloseInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if input.Handle == "" {
		return invalidInputResult(env, started, errString("handle is required"))
	}
	d.sessions.close(input.Handle)
	return succeeded(env, started, command.ContentCloseData{Handle: input.Handle, Closed: true})
}

func (s *contentSessions) put(session contentSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byHandle[session.handle] = session
}

func (s *contentSessions) get(handle string) (contentSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.byHandle[handle]
	return session, ok
}

func (s *contentSessions) close(handle string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byHandle, handle)
}

func (s *contentSessions) read(ctx context.Context, session contentSession, offset, length int64) ([]byte, bool, error) {
	if offset >= session.logicalSize {
		return []byte{}, true, nil
	}
	body, err := s.repo.Open(ctx, session.contentID)
	if err != nil {
		return nil, false, err
	}
	defer body.Close()
	if offset > 0 {
		if seeker, ok := body.(io.Seeker); ok {
			if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
				return nil, false, err
			}
		} else if _, err := io.CopyN(io.Discard, body, offset); err != nil && !errors.Is(err, io.EOF) {
			return nil, false, err
		}
	}
	payload, err := io.ReadAll(io.LimitReader(body, length))
	if err != nil {
		return nil, false, err
	}
	eof := offset+int64(len(payload)) >= session.logicalSize
	return payload, eof, nil
}

func newContentHandle() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "hdl_" + hex.EncodeToString(value[:]), nil
}
