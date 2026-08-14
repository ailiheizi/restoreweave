package controlplane

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	payload := []byte(`{"schema":"org.restoreweave.command.v1","operation":"status.get"}`)
	if err := WriteFrame(&buffer, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	got, err := ReadFrame(&buffer, MaxFrameBytes)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("frame payload changed: got %q want %q", got, payload)
	}
}

func TestFrameRoundTripEmpty(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteFrame(&buffer, nil); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	got, err := ReadFrame(&buffer, MaxFrameBytes)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty frame payload = %d bytes", len(got))
	}
}

func TestFrameMultipleInOneStream(t *testing.T) {
	var buffer bytes.Buffer
	for i := 0; i < 3; i++ {
		if err := WriteFrame(&buffer, []byte(fmt.Sprintf(`{"n":%d}`, i))); err != nil {
			t.Fatalf("WriteFrame %d: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := ReadFrame(&buffer, MaxFrameBytes); err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
	}
	if _, err := ReadFrame(&buffer, MaxFrameBytes); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF after last frame, got %v", err)
	}
}

func TestFrameRejectsOversizedDeclaredLength(t *testing.T) {
	var buffer bytes.Buffer
	buffer.Write([]byte{0xff, 0xff, 0xff, 0xff})
	_, err := ReadFrame(&buffer, MaxFrameBytes)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}

func TestFrameRejectsTruncatedPayload(t *testing.T) {
	var buffer bytes.Buffer
	payload := []byte("hello")
	if err := WriteFrame(&buffer, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	truncated := bytes.NewReader(buffer.Bytes()[:6])
	_, err := ReadFrame(truncated, MaxFrameBytes)
	if err == nil || !strings.Contains(err.Error(), "payload") {
		t.Fatalf("expected payload read error, got %v", err)
	}
}

func TestFrameRejectsPartialHeader(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader([]byte{0x00, 0x01}), MaxFrameBytes)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	err := WriteFrame(&bytes.Buffer{}, make([]byte, MaxFrameBytes+1))
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}
