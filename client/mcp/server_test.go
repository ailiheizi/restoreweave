package mcp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/client/transport"
	"github.com/ailiheizi/restoreweave/server/controlplane"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// startDaemon runs the real control-plane server for the duration of the test
// and returns a transport connection to it.
func startDaemon(t *testing.T) (*transport.Conn, *testutil.NamespaceSeed) {
	t.Helper()
	store := testutil.OpenStore(t, ":memory:")
	seed := testutil.SeedNamespace(t, store)
	socketPath := testutil.TempSocketPath(t)
	dispatcher := controlplane.NewDispatcher(store, filepath.Join(t.TempDir(), "catalog.sqlite"), socketPath)
	server, err := controlplane.NewServer(dispatcher, socketPath)
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
	conn, err := transport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, seed
}

// TestToolsRoundTripOverPipes runs the MCP server over newline-delimited
// JSON-RPC pipes (the same IO shape as stdio) and drives it with the SDK's
// real client.
func TestToolsRoundTripOverPipes(t *testing.T) {
	ctx := context.Background()
	conn, seed := startDaemon(t)
	server := New(conn)

	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	serverTransport := &mcpsdk.IOTransport{Reader: serverRead, Writer: serverWrite}
	clientTransport := &mcpsdk.IOTransport{Reader: clientRead, Writer: clientWrite}

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "harness-test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	statusResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      command.OpStatusGet,
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("status.get call: %v", err)
	}
	if statusResult.IsError {
		t.Fatalf("status.get returned an error result")
	}
	text := contentText(t, statusResult)
	if !strings.Contains(text, "controller: restoreweaved") || !strings.Contains(text, "catalog ok: true") {
		t.Fatalf("status.get text = %q", text)
	}

	namespaceResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpNamespaceList,
		Arguments: map[string]any{
			"workspace_id": seed.WorkspaceID,
			"root_id":      seed.RootID,
			"parent_id":    seed.DirEntryID,
		},
	})
	if err != nil {
		t.Fatalf("namespace.list call: %v", err)
	}
	if namespaceResult.IsError {
		t.Fatalf("namespace.list returned an error result")
	}
	if listText := contentText(t, namespaceResult); !strings.Contains(listText, seed.FileEntryID) {
		t.Fatalf("namespace.list text = %q", listText)
	}

	resolveResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpNamespaceResolve,
		Arguments: map[string]any{
			"workspace_id": seed.WorkspaceID,
			"root_id":      seed.RootID,
			"path":         "Music",
		},
	})
	if err != nil {
		t.Fatalf("namespace.resolve call: %v", err)
	}
	if resolveResult.IsError {
		t.Fatalf("namespace.resolve returned an error result")
	}
	if resolveText := contentText(t, resolveResult); !strings.Contains(resolveText, seed.DirEntryID) {
		t.Fatalf("namespace.resolve text = %q", resolveText)
	}

	representationResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpRepresentationList,
		Arguments: map[string]any{
			"workspace_id": seed.WorkspaceID,
			"subject_ref":  seed.FileEntryID,
		},
	})
	if err != nil {
		t.Fatalf("representation.list call: %v", err)
	}
	if representationResult.IsError {
		t.Fatalf("representation.list returned an error result")
	}
	if representationText := contentText(t, representationResult); !strings.Contains(representationText, "restic-stream/v1") {
		t.Fatalf("representation.list text = %q", representationText)
	}

	searchResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      command.OpSearchQuery,
		Arguments: map[string]any{"query": "flac"},
	})
	if err != nil {
		t.Fatalf("search.query call: %v", err)
	}
	if !searchResult.IsError {
		t.Fatalf("search.query must report the daemon's unimplemented outcome as an error result")
	}
	if searchText := contentText(t, searchResult); !strings.Contains(searchText, "unimplemented") {
		t.Fatalf("search.query text = %q", searchText)
	}

	audioResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      command.OpAudioList,
		Arguments: map[string]any{"workspace_id": seed.WorkspaceID},
	})
	if err != nil {
		t.Fatalf("audio.list call: %v", err)
	}
	if !audioResult.IsError {
		t.Fatalf("audio.list must report the daemon's unimplemented outcome as an error result")
	}
	if audioText := contentText(t, audioResult); !strings.Contains(audioText, "unimplemented") {
		t.Fatalf("audio.list text = %q", audioText)
	}

	booksResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      command.OpBooksList,
		Arguments: map[string]any{"workspace_id": seed.WorkspaceID},
	})
	if err != nil {
		t.Fatalf("books.list call: %v", err)
	}
	if !booksResult.IsError {
		t.Fatalf("books.list must report the daemon's unimplemented outcome as an error result")
	}
	if booksText := contentText(t, booksResult); !strings.Contains(booksText, "unimplemented") {
		t.Fatalf("books.list text = %q", booksText)
	}
}

// TestStructuredContentCarriesRealData verifies the structured content of a
// tool result decodes to the daemon's actual response payload.
func TestStructuredContentCarriesRealData(t *testing.T) {
	ctx := context.Background()
	conn, _ := startDaemon(t)
	server := New(conn)

	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	serverSession, err := server.Connect(ctx,
		&mcpsdk.IOTransport{Reader: serverRead, Writer: serverWrite}, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "harness-test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx,
		&mcpsdk.IOTransport{Reader: clientRead, Writer: clientWrite}, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      command.OpCapabilityList,
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("capability.list call: %v", err)
	}
	if result.StructuredContent == nil {
		t.Fatalf("capability.list result has no structured content: %+v", result)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("re-encode structured content: %v", err)
	}
	var data command.CapabilityListData
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	found := false
	for _, capability := range data.Capabilities {
		if capability.ID == command.OpStatusGet && capability.State == command.CapabilityAvailable {
			found = true
		}
	}
	if !found {
		t.Fatalf("capability list missing status.get AVAILABLE: %+v", data.Capabilities)
	}
}

func TestAudioListMCPAfterIngest(t *testing.T) {
	ctx := context.Background()
	conn, dispatcher := startExactDaemon(t)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), mcpID3v23(map[string]string{
		"TIT2": "Nightfall",
		"TPE1": "Example Artist",
		"TALB": "Demo Album",
		"TRCK": "1",
	}), 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}
	ingested := dispatcher.Handle(ctx, command.Envelope{
		Operation: command.OpPlanIngest,
		Input:     mustJSON(t, map[string]any{"root": root}),
	})
	if ingested.Status != command.StatusSucceeded {
		t.Fatalf("ingest = %q: %+v", ingested.Status, ingested.Reasons)
	}
	var ingestData command.PlanIngestData
	if err := json.Unmarshal(ingested.Data, &ingestData); err != nil {
		t.Fatalf("decode ingest: %v", err)
	}

	server := New(conn)
	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	serverSession, err := server.Connect(ctx,
		&mcpsdk.IOTransport{Reader: serverRead, Writer: serverWrite}, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "harness-test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx,
		&mcpsdk.IOTransport{Reader: clientRead, Writer: clientWrite}, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpAudioList,
		Arguments: map[string]any{
			"workspace_id": ingestData.WorkspaceID,
			"snapshot_ref": ingestData.SnapshotRef,
		},
	})
	if err != nil {
		t.Fatalf("audio.list call: %v", err)
	}
	if result.IsError {
		t.Fatalf("audio.list error: %s", contentText(t, result))
	}
	text := contentText(t, result)
	if !strings.Contains(text, "Nightfall") || !strings.Contains(text, "Demo Album") || !strings.Contains(text, "album\t") {
		t.Fatalf("audio.list text = %q", text)
	}
	if result.StructuredContent == nil {
		t.Fatal("audio.list missing structured content")
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("encode structured content: %v", err)
	}
	var data command.AudioListData
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if len(data.Tracks) != 1 || data.Tracks[0].Title != "Nightfall" || len(data.Albums) != 1 {
		t.Fatalf("structured audio.list = %+v", data)
	}
}

func TestBooksListMCPAfterIngest(t *testing.T) {
	ctx := context.Background()
	conn, dispatcher := startExactDaemon(t)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("# Driftwood\n\nA shoreline story."), 0o644); err != nil {
		t.Fatalf("write md: %v", err)
	}
	ingested := dispatcher.Handle(ctx, command.Envelope{
		Operation: command.OpPlanIngest,
		Input:     mustJSON(t, map[string]any{"root": root}),
	})
	if ingested.Status != command.StatusSucceeded {
		t.Fatalf("ingest = %q: %+v", ingested.Status, ingested.Reasons)
	}
	var ingestData command.PlanIngestData
	if err := json.Unmarshal(ingested.Data, &ingestData); err != nil {
		t.Fatalf("decode ingest: %v", err)
	}

	server := New(conn)
	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	serverSession, err := server.Connect(ctx,
		&mcpsdk.IOTransport{Reader: serverRead, Writer: serverWrite}, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "harness-test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx,
		&mcpsdk.IOTransport{Reader: clientRead, Writer: clientWrite}, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpBooksList,
		Arguments: map[string]any{
			"workspace_id": ingestData.WorkspaceID,
			"snapshot_ref": ingestData.SnapshotRef,
		},
	})
	if err != nil {
		t.Fatalf("books.list call: %v", err)
	}
	if result.IsError {
		t.Fatalf("books.list error: %s", contentText(t, result))
	}
	text := contentText(t, result)
	if !strings.Contains(text, "Driftwood") || !strings.Contains(text, "work\t") {
		t.Fatalf("books.list text = %q", text)
	}
	if result.StructuredContent == nil {
		t.Fatal("books.list missing structured content")
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("encode structured content: %v", err)
	}
	var data command.BookListData
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if len(data.Works) != 1 || data.Works[0].Title != "Driftwood" || data.Works[0].Kind != "text" {
		t.Fatalf("structured books.list = %+v", data)
	}
}

func startExactDaemon(t *testing.T) (*transport.Conn, *controlplane.Dispatcher) {
	t.Helper()
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	store := testutil.OpenStore(t, catalogPath)
	socketPath := testutil.TempSocketPath(t)
	opt, err := controlplane.WithExactDir(store, filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("exact lane: %v", err)
	}
	dispatcher := controlplane.NewDispatcher(store, catalogPath, socketPath, opt)
	server, err := controlplane.NewServer(dispatcher, socketPath)
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
	conn, err := transport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, dispatcher
}

func mustJSON(t *testing.T, input any) []byte {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func mcpID3v23(frames map[string]string) []byte {
	var body []byte
	for _, id := range []string{"TIT2", "TPE1", "TALB", "TRCK", "TYER"} {
		value, ok := frames[id]
		if !ok {
			continue
		}
		data := append([]byte{3}, []byte(value)...)
		frame := []byte(id)
		frame = binary.BigEndian.AppendUint32(frame, uint32(len(data)))
		frame = append(frame, 0, 0)
		frame = append(frame, data...)
		body = append(body, frame...)
	}
	n := len(body)
	header := []byte{
		'I', 'D', '3', 3, 0, 0,
		byte((n >> 21) & 0x7f), byte((n >> 14) & 0x7f), byte((n >> 7) & 0x7f), byte(n & 0x7f),
	}
	return append(header, body...)
}

func contentText(t *testing.T, result *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	textContent, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content is not text: %T", result.Content[0])
	}
	return textContent.Text
}
