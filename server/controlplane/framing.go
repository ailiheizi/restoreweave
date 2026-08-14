// Package controlplane implements the restoreweaved daemon control plane:
// a Unix-socket JSON-envelope dispatcher over the shared client/command
// wire protocol. The socket transport is private plumbing; the envelope
// vocabulary is the stable serialized contract.
package controlplane

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// FrameHeaderSize is the length of the big-endian length prefix.
const FrameHeaderSize = 4

// MaxFrameBytes caps a single envelope or result frame. Control messages are
// small; the cap exists so a peer cannot force unbounded allocation.
const MaxFrameBytes = 64 << 20 // 64 MiB

// ErrFrameTooLarge is returned when a frame exceeds MaxFrameBytes.
var ErrFrameTooLarge = errors.New("control plane frame exceeds maximum allowed size")

// WriteFrame writes payload as one length-prefixed frame: a 4-byte big-endian
// byte count followed by the payload.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	var header [FrameHeaderSize]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write frame payload: %w", err)
	}
	return nil
}

// ReadFrame reads one length-prefixed frame. A clean EOF before any header
// byte is returned as io.EOF; truncation is reported as
// io.ErrUnexpectedEOF. Frames larger than maxBytes fail with ErrFrameTooLarge.
func ReadFrame(r io.Reader, maxBytes int) ([]byte, error) {
	var header [FrameHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if int64(length) > int64(maxBytes) {
		return nil, fmt.Errorf("%w: declared %d bytes", ErrFrameTooLarge, length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}
	return payload, nil
}
