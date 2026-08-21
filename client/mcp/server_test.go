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

	statResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpNamespaceStat,
		Arguments: map[string]any{
			"workspace_id": seed.WorkspaceID,
			"entry_id":     seed.FileEntryID,
		},
	})
	if err != nil {
		t.Fatalf("namespace.stat call: %v", err)
	}
	if statResult.IsError {
		t.Fatalf("namespace.stat returned an error result")
	}
	if statText := contentText(t, statResult); !strings.Contains(statText, seed.FileEntryID) {
		t.Fatalf("namespace.stat text = %q", statText)
	}

	readlinkResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpNamespaceReadlink,
		Arguments: map[string]any{
			"workspace_id": seed.WorkspaceID,
			"entry_id":     seed.SymlinkEntryID,
		},
	})
	if err != nil {
		t.Fatalf("namespace.readlink call: %v", err)
	}
	if readlinkResult.IsError {
		t.Fatalf("namespace.readlink returned an error result")
	}
	if readlinkText := contentText(t, readlinkResult); !strings.Contains(readlinkText, seed.SymlinkEntryID) {
		t.Fatalf("namespace.readlink text = %q", readlinkText)
	}

	snapshotResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      command.OpSnapshotList,
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("snapshot.list call: %v", err)
	}
	if !snapshotResult.IsError {
		t.Fatalf("snapshot.list must report the daemon's unimplemented outcome as an error result")
	}
	if snapshotText := contentText(t, snapshotResult); !strings.Contains(snapshotText, "unimplemented") {
		t.Fatalf("snapshot.list text = %q", snapshotText)
	}

	openResult, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpContentOpen,
		Arguments: map[string]any{
			"workspace_id": seed.WorkspaceID,
			"entry_id":     seed.FileEntryID,
		},
	})
	if err != nil {
		t.Fatalf("content.open call: %v", err)
	}
	if !openResult.IsError {
		t.Fatalf("content.open must report the daemon's unimplemented outcome as an error result")
	}
	if openText := contentText(t, openResult); !strings.Contains(openText, "unimplemented") {
		t.Fatalf("content.open text = %q", openText)
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
	ingestData := mustAppliedMCPIngest(t, ctx, dispatcher, root)

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
	ingestData := mustAppliedMCPIngest(t, ctx, dispatcher, root)

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

func TestSnapshotListAndDiffMCPAfterIngest(t *testing.T) {
	ctx := context.Background()
	conn, dispatcher := startExactDaemon(t)

	firstRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(firstRoot, "blob.bin"), []byte("first"), 0o600); err != nil {
		t.Fatalf("write first: %v", err)
	}
	firstData := mustAppliedMCPIngest(t, ctx, dispatcher, firstRoot)
	secondRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(secondRoot, "blob.bin"), []byte("first"), 0o600); err != nil {
		t.Fatalf("write second blob: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secondRoot, "extra.txt"), []byte("second"), 0o600); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	secondData := mustAppliedMCPIngest(t, ctx, dispatcher, secondRoot)

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

	listed, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      command.OpSnapshotList,
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("snapshot.list call: %v", err)
	}
	if listed.IsError {
		t.Fatalf("snapshot.list error: %s", contentText(t, listed))
	}
	listText := contentText(t, listed)
	if !strings.Contains(listText, firstData.SnapshotRef) || !strings.Contains(listText, secondData.SnapshotRef) {
		t.Fatalf("snapshot.list text = %q", listText)
	}

	diffed, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpSnapshotDiff,
		Arguments: map[string]any{
			"from_snapshot_ref": firstData.SnapshotRef,
			"to_snapshot_ref":   secondData.SnapshotRef,
		},
	})
	if err != nil {
		t.Fatalf("snapshot.diff call: %v", err)
	}
	if diffed.IsError {
		t.Fatalf("snapshot.diff error: %s", contentText(t, diffed))
	}
	if diffText := contentText(t, diffed); !strings.Contains(diffText, "added extra.txt") {
		t.Fatalf("snapshot.diff text = %q", diffText)
	}
}

func TestContentHandleMCPAfterIngest(t *testing.T) {
	ctx := context.Background()
	conn, dispatcher := startExactDaemon(t)
	root := t.TempDir()
	payload := []byte("hello-mcp-content")
	if err := os.WriteFile(filepath.Join(root, "note.txt"), payload, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ingestData := mustAppliedMCPIngest(t, ctx, dispatcher, root)
	listed := dispatcher.Handle(ctx, command.Envelope{
		Operation: command.OpNamespaceList,
		Input:     mustJSON(t, map[string]any{"workspace_id": ingestData.WorkspaceID, "root_id": ingestData.RootID}),
	})
	var listData command.NamespaceListData
	if err := json.Unmarshal(listed.Data, &listData); err != nil {
		t.Fatalf("decode namespace: %v", err)
	}
	var fileID string
	for _, entry := range listData.Entries {
		if entry.DisplayName == "note.txt" {
			fileID = entry.ID
		}
	}
	if fileID == "" {
		t.Fatalf("note.txt missing: %+v", listData.Entries)
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

	opened, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpContentOpen,
		Arguments: map[string]any{
			"workspace_id": ingestData.WorkspaceID,
			"entry_id":     fileID,
		},
	})
	if err != nil {
		t.Fatalf("content.open call: %v", err)
	}
	if opened.IsError {
		t.Fatalf("content.open error: %s", contentText(t, opened))
	}
	openText := contentText(t, opened)
	if !strings.Contains(openText, "handle: hdl_") {
		t.Fatalf("content.open text = %q", openText)
	}
	encoded, err := json.Marshal(opened.StructuredContent)
	if err != nil {
		t.Fatalf("encode open: %v", err)
	}
	var openData command.ContentOpenData
	if err := json.Unmarshal(encoded, &openData); err != nil {
		t.Fatalf("decode open: %v", err)
	}

	tooBig, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpContentRead,
		Arguments: map[string]any{
			"handle": openData.Handle,
			"length": 5000,
		},
	})
	if err != nil {
		t.Fatalf("oversized content.read call: %v", err)
	}
	if !tooBig.IsError || !strings.Contains(contentText(t, tooBig), "4096") {
		t.Fatalf("oversized content.read = %q", contentText(t, tooBig))
	}

	read, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: command.OpContentRead,
		Arguments: map[string]any{
			"handle": openData.Handle,
			"offset": 0,
			"length": int64(len(payload)),
		},
	})
	if err != nil {
		t.Fatalf("content.read call: %v", err)
	}
	if read.IsError {
		t.Fatalf("content.read error: %s", contentText(t, read))
	}
	readText := contentText(t, read)
	if !strings.Contains(readText, "hello-mcp-content") || !strings.Contains(readText, "digest: sha256:") {
		t.Fatalf("content.read text = %q", readText)
	}
	if strings.Contains(readText, "unbounded") {
		t.Fatalf("content.read leaked bulk framing: %q", readText)
	}
	readEncoded, err := json.Marshal(read.StructuredContent)
	if err != nil {
		t.Fatalf("encode read: %v", err)
	}
	var readData contentReadMCPData
	if err := json.Unmarshal(readEncoded, &readData); err != nil {
		t.Fatalf("decode read: %v", err)
	}
	if !readData.EOF || readData.Preview != string(payload) || !readData.Untrusted {
		t.Fatalf("content.read structured = %+v", readData)
	}

	closed, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      command.OpContentClose,
		Arguments: map[string]any{"handle": openData.Handle},
	})
	if err != nil {
		t.Fatalf("content.close call: %v", err)
	}
	if closed.IsError {
		t.Fatalf("content.close error: %s", contentText(t, closed))
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

func mustAppliedMCPIngest(t *testing.T, ctx context.Context, dispatcher *controlplane.Dispatcher, root string) command.PlanApplyData {
	t.Helper()
	planned := dispatcher.Handle(ctx, command.Envelope{
		Operation: command.OpPlanIngest,
		Input:     mustJSON(t, map[string]any{"root": root}),
	})
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var plan command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &plan); err != nil {
		t.Fatalf("decode plan.ingest: %v", err)
	}
	applied := dispatcher.Handle(ctx, command.Envelope{
		Operation: command.OpPlanApply,
		Input: mustJSON(t, map[string]any{
			"workspace_id": plan.WorkspaceID,
			"plan_id":      plan.PlanID,
			"plan_digest":  plan.PlanDigest,
		}),
	})
	if applied.Status != command.StatusSucceeded && applied.Status != command.StatusDegraded {
		t.Fatalf("plan.apply = %q: %+v", applied.Status, applied.Reasons)
	}
	var result command.PlanApplyData
	if err := json.Unmarshal(applied.Data, &result); err != nil {
		t.Fatalf("decode plan.apply: %v", err)
	}
	if result.SnapshotRef == "" || result.RootID == "" {
		t.Fatalf("plan.apply omitted publication identity: %+v", result)
	}
	return result
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
