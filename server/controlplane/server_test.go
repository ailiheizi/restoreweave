package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/client/transport"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// startServer runs a real control-plane server for the duration of the test.
func startServer(t *testing.T, store *sqlite.Store, socketPath, catalogPath string) *Server {
	t.Helper()
	dispatcher := NewDispatcher(store, catalogPath, socketPath)
	server, err := NewServer(dispatcher, socketPath)
	if err != nil {
		t.Fatalf("start control plane server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
		<-done
	})
	return server
}

// TestServerRealRoundTrip drives a real daemon through the client transport
// and verifies status.get and namespace.list end to end over the socket.
func TestServerRealRoundTrip(t *testing.T) {
	store := testutil.OpenStore(t, ":memory:")
	seed := testutil.SeedNamespace(t, store)
	socketPath := testutil.TempSocketPath(t)
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	startServer(t, store, socketPath, catalogPath)

	ctx := context.Background()
	conn, err := transport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	result, err := conn.Do(ctx, command.Envelope{Operation: command.OpStatusGet})
	if err != nil {
		t.Fatalf("status.get round trip: %v", err)
	}
	if result.Status != command.StatusSucceeded {
		t.Fatalf("status.get = %q: %+v", result.Status, result.Reasons)
	}
	var status command.StatusData
	if err := json.Unmarshal(result.Data, &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Catalog.OK || status.Catalog.Path != catalogPath {
		t.Fatalf("catalog = %+v", status.Catalog)
	}
	if status.Listen != socketPath {
		t.Fatalf("listen = %q", status.Listen)
	}

	raw, _ := json.Marshal(map[string]any{
		"workspace_id": seed.WorkspaceID,
		"root_id":      seed.RootID,
		"parent_id":    seed.DirEntryID,
	})
	listed, err := conn.Do(ctx, command.Envelope{Operation: command.OpNamespaceList, Input: raw})
	if err != nil {
		t.Fatalf("namespace.list round trip: %v", err)
	}
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("namespace.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var listData command.NamespaceListData
	if err := json.Unmarshal(listed.Data, &listData); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listData.Entries) != 2 {
		t.Fatalf("entries = %+v", listData.Entries)
	}
}

func TestServerUnknownAndUnimplementedOverSocket(t *testing.T) {
	store := testutil.OpenStore(t, ":memory:")
	socketPath := testutil.TempSocketPath(t)
	startServer(t, store, socketPath, filepath.Join(t.TempDir(), "catalog.sqlite"))

	conn, err := transport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	unknown, err := conn.Do(context.Background(), command.Envelope{Operation: "controller.dance"})
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if unknown.Status != command.StatusFailed || !hasReasonCode(unknown, ReasonCodeUnknownOperation) {
		t.Fatalf("unknown operation result = %+v", unknown)
	}

	unimplemented, err := conn.Do(context.Background(), command.Envelope{Operation: command.OpPlanIngest})
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if unimplemented.Status != command.StatusFailed || !hasReasonCode(unimplemented, ReasonCodeUnimplemented) {
		t.Fatalf("unimplemented operation result = %+v", unimplemented)
	}
}

func TestServerRejectsMalformedJSON(t *testing.T) {
	store := testutil.OpenStore(t, ":memory:")
	socketPath := testutil.TempSocketPath(t)
	startServer(t, store, socketPath, filepath.Join(t.TempDir(), "catalog.sqlite"))

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := transport.WriteFrame(conn, []byte(`{not json`)); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	payload, err := transport.ReadFrame(conn, transport.MaxFrameBytes)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var result command.Result
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeInvalidRequest) {
		t.Fatalf("malformed envelope result = %+v", result)
	}

	// The connection must stay usable after a malformed envelope.
	raw, _ := json.Marshal(command.Envelope{Operation: command.OpStatusGet})
	if err := transport.WriteFrame(conn, raw); err != nil {
		t.Fatalf("write status.get frame: %v", err)
	}
	healthyPayload, err := transport.ReadFrame(conn, transport.MaxFrameBytes)
	if err != nil {
		t.Fatalf("read status.get response: %v", err)
	}
	var healthy command.Result
	if err := json.Unmarshal(healthyPayload, &healthy); err != nil {
		t.Fatalf("decode status.get result: %v", err)
	}
	if healthy.Status != command.StatusSucceeded {
		t.Fatalf("status.get = %+v", healthy)
	}
}

func TestServerGracefulShutdownClosesListener(t *testing.T) {
	store := testutil.OpenStore(t, ":memory:")
	socketPath := testutil.TempSocketPath(t)
	dispatcher := NewDispatcher(store, "catalog.sqlite", socketPath)
	server, err := NewServer(dispatcher, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(ctx)
	}()
	cancel()
	<-done
	if err := server.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := transport.Dial(socketPath); err == nil {
		t.Fatal("dial after shutdown unexpectedly succeeded")
	}
}

func TestServerRejectsSecondListener(t *testing.T) {
	store := testutil.OpenStore(t, ":memory:")
	socketPath := testutil.TempSocketPath(t)
	startServer(t, store, socketPath, "catalog.sqlite")

	dispatcher := NewDispatcher(store, "catalog.sqlite", socketPath)
	if _, err := NewServer(dispatcher, socketPath); !errors.Is(err, ErrSocketInUse) {
		t.Fatalf("second listener error = %v, want ErrSocketInUse", err)
	}
}

func TestServerTimeoutOnUnresponsivePeer(t *testing.T) {
	// A silent listener that accepts connections but never answers exercises
	// the client deadline without depending on server timing.
	socketPath := testutil.TempSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Never read or respond; the connection is intentionally silent.
			defer conn.Close()
		}
	}()

	conn, err := transport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	deadlineCtx, cancelDeadline := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelDeadline()
	_, err = conn.Do(deadlineCtx, command.Envelope{Operation: command.OpStatusGet})
	if err == nil {
		t.Fatal("expected deadline error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") && deadlineCtx.Err() != context.DeadlineExceeded {
		t.Fatalf("expected a deadline timeout, got %v", err)
	}
}

func TestClientConnectionClosedBeforeResult(t *testing.T) {
	store := testutil.OpenStore(t, ":memory:")
	socketPath := testutil.TempSocketPath(t)
	dispatcher := NewDispatcher(store, "catalog.sqlite", socketPath)
	server, err := NewServer(dispatcher, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = server.Serve(ctx) }()

	conn, err := transport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Close the server while the client is connected; the client's next Do
	// must observe the closed connection instead of hanging.
	_ = server.Close()
	cancel()
	_, err = conn.Do(context.Background(), command.Envelope{Operation: command.OpStatusGet})
	if err == nil {
		t.Fatal("expected error after server closed, got nil")
	}
}
