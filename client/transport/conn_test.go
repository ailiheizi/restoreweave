package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestClientFramingRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	payload := []byte(`{"operation":"status.get"}`)
	if err := WriteFrame(&buffer, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	got, err := ReadFrame(&buffer, MaxFrameBytes)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: %q", got)
	}
}

func TestClientFramingRejectsOversized(t *testing.T) {
	var buffer bytes.Buffer
	buffer.Write([]byte{0x7f, 0xff, 0xff, 0xff})
	if _, err := ReadFrame(&buffer, MaxFrameBytes); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}

// echoServer accepts one connection and replies to every envelope frame with
// a canned SUCCEEDED result carrying the request id.
func echoServer(t *testing.T, socketPath string) {
	t.Helper()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				for {
					payload, err := ReadFrame(conn, MaxFrameBytes)
					if err != nil {
						return
					}
					var env command.Envelope
					if err := json.Unmarshal(payload, &env); err != nil {
						return
					}
					result := command.NewResult(env, command.StatusSucceeded,
						time.Now().UTC(), time.Now().UTC(), map[string]string{"echo": env.Operation})
					if err := WriteFrame(conn, mustMarshal(t, result)); err != nil {
						return
					}
				}
			}()
		}
	}()
}

func TestConnDoRoundTrip(t *testing.T) {
	socketPath := testutil.TempSocketPath(t)
	echoServer(t, socketPath)

	conn, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	result, err := conn.Do(context.Background(), command.Envelope{Operation: command.OpStatusGet})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if result.Status != command.StatusSucceeded || result.Operation != command.OpStatusGet {
		t.Fatalf("result = %+v", result)
	}
	var data map[string]string
	if err := json.Unmarshal(result.Data, &data); err != nil || data["echo"] != command.OpStatusGet {
		t.Fatalf("result data = %s", result.Data)
	}
}

func TestConnDoServerClosed(t *testing.T) {
	socketPath := testutil.TempSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			// Read the envelope frame, then close without responding so the
			// client's read deterministically observes the EOF.
			_, _ = ReadFrame(conn, MaxFrameBytes)
		}
	}()

	conn, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Do(context.Background(), command.Envelope{Operation: command.OpStatusGet})
	if !errors.Is(err, ErrServerClosed) {
		t.Fatalf("expected ErrServerClosed, got %v", err)
	}
}

func TestConnDoContextCancelled(t *testing.T) {
	socketPath := testutil.TempSocketPath(t)
	echoServer(t, socketPath)

	conn, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := conn.Do(ctx, command.Envelope{Operation: command.OpStatusGet}); err == nil {
		t.Fatal("expected cancelled context error, got nil")
	}
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return payload
}
