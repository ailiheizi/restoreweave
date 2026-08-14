package access

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/ailiheizi/restoreweave/server/internal/decode"
	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type readSession struct {
	service *Service
	view    *catalogView
	info    readsvc.ReadSessionInfo
	size    uint64
	opens   atomic.Uint32
	closed  atomic.Bool
}

func (s *readSession) Info() readsvc.ReadSessionInfo { return s.info }

func (s *readSession) Close() error {
	s.closed.Store(true)
	return nil
}

func (s *readSession) Open(ctx context.Context) (readsvc.RandomAccessFile, error) {
	if s.closed.Load() {
		return nil, errors.New("read session is closed")
	}
	if !s.service.now().Before(s.info.Limits.ExpiresAt) {
		return nil, readsvc.ErrSessionExpired
	}
	if s.opens.Add(1) > s.info.Limits.MaxOpens {
		s.opens.Add(^uint32(0))
		return nil, errors.New("read session open limit exceeded")
	}
	body, err := s.service.Repo.Open(ctx, s.info.Pin.ContentID)
	if err != nil {
		s.opens.Add(^uint32(0))
		return nil, err
	}
	encoded, err := io.ReadAll(body)
	closeErr := body.Close()
	if err != nil {
		s.opens.Add(^uint32(0))
		return nil, err
	}
	if closeErr != nil {
		s.opens.Add(^uint32(0))
		return nil, closeErr
	}
	sum := sha256.Sum256(encoded)
	got := "sha256:" + hex.EncodeToString(sum[:])
	if got != s.info.Pin.ContentID {
		s.opens.Add(^uint32(0))
		return nil, repository.ErrDigestMismatch
	}
	var decoded bytes.Buffer
	_, err = s.service.decoder().DecodeRange(ctx, readsvc.DecodeRequest{
		Representation: readsvc.RepresentationRef{
			ID:              s.info.Pin.RepresentationID,
			ContentID:       s.info.Pin.ContentID,
			LogicalSize:     uint64(len(encoded)),
			CodecProfileRef: decode.IdentityCodec,
			AccessMode:      readsvc.AccessRandomNative,
		},
		OutputRange: readsvc.ByteRange{Offset: 0, Length: uint64(len(encoded))},
		Budget:      readsvc.ReadBudget{MaxSourceBytes: uint64(len(encoded)), MaxOutputBytes: uint64(len(encoded))},
	}, encodedCAS{body: encoded}, &decoded)
	if err != nil {
		s.opens.Add(^uint32(0))
		return nil, err
	}
	payload := decoded.Bytes()
	decodedSum := sha256.Sum256(payload)
	decodedID := "sha256:" + hex.EncodeToString(decodedSum[:])
	if decodedID != s.info.Pin.ContentID {
		s.opens.Add(^uint32(0))
		return nil, repository.ErrDigestMismatch
	}
	return &randomAccessFile{session: s, payload: payload}, nil
}

type randomAccessFile struct {
	session *readSession
	payload []byte
	mu      sync.Mutex
	closed  bool
}

func (f *randomAccessFile) Pin() readsvc.ReadPin { return f.session.info.Pin }
func (f *randomAccessFile) Size() uint64         { return f.session.size }
func (f *randomAccessFile) ETag() string         { return f.session.info.Pin.ContentID }

func (f *randomAccessFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		f.session.opens.Add(^uint32(0))
	}
	return nil
}

func (f *randomAccessFile) ReadAt(ctx context.Context, dest []byte, offset uint64) (readsvc.RangeReadResult, error) {
	if err := ctx.Err(); err != nil {
		return readsvc.RangeReadResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return readsvc.RangeReadResult{}, errors.New("file handle is closed")
	}
	now := f.session.service.now()
	if !now.Before(f.session.info.Limits.ExpiresAt) {
		return readsvc.RangeReadResult{}, readsvc.ErrSessionExpired
	}
	length := uint64(len(dest))
	if offset > f.session.size {
		return readsvc.RangeReadResult{}, readsvc.ErrRangeOutOfBounds
	}
	remaining := f.session.size - offset
	if length > remaining {
		length = remaining
	}
	requested := readsvc.ByteRange{Offset: offset, Length: length}
	if err := requested.Validate(f.session.size); err != nil {
		return readsvc.RangeReadResult{}, err
	}
	if !f.session.info.Limits.Allows(requested, f.session.size) {
		return readsvc.RangeReadResult{}, errors.New("read exceeds session limits")
	}
	if length > 0 {
		copy(dest, f.payload[offset:offset+length])
	}
	acceptanceID, err := sqlite.NewStableID("acc")
	if err != nil {
		return readsvc.RangeReadResult{}, err
	}
	scope := readsvc.VerificationRangeFrameVerified
	if requested.Offset == 0 && requested.Length == f.session.size {
		scope = readsvc.VerificationWholeContentVerified
	}
	return readsvc.RangeReadResult{
		Requested:       requested,
		Returned:        requested,
		BytesRead:       length,
		SourceBytesRead: length,
		Verification: readsvc.AcceptedVerification{
			Scope:        scope,
			AcceptanceID: acceptanceID,
			PolicyRef:    localPolicyRef,
			AcceptedAt:   now,
			EvidenceRefs: []string{f.session.info.Pin.ContentID},
		},
	}, nil
}
