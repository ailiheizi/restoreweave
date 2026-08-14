// Package mcp exposes restoreweaved operations as a local, read-only MCP
// server over stdio. Every tool executes a real command-envelope round trip
// through client/transport; nothing is fabricated when the daemon reports an
// operation as unimplemented.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/client/transport"
)

const (
	serverName    = "restoreweave"
	serverVersion = "0.1.0"
)

// New builds the MCP server with the harness tool set, all wired to one
// daemon connection. Tool names use lowercase dotted names (status.get,
// capability.list, namespace.list, namespace.resolve, representation.list, search.query, annotation.list, audio.list, books.list).
func New(conn *transport.Conn) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: serverName, Version: serverVersion}, nil)
	addStatusGet(server, conn)
	addCapabilityList(server, conn)
	addNamespaceList(server, conn)
	addNamespaceResolve(server, conn)
	addRepresentationList(server, conn)
	addSearchQuery(server, conn)
	addAnnotationList(server, conn)
	addAudioList(server, conn)
	addBooksList(server, conn)
	return server
}

// RunStdio connects to the daemon at socketPath and serves the MCP protocol
// over stdin/stdout until the context is cancelled or the peer disconnects.
func RunStdio(ctx context.Context, socketPath string) error {
	conn, err := transport.DialContext(ctx, socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	return New(conn).Run(ctx, &mcpsdk.StdioTransport{})
}

type statusGetInput struct{}

func addStatusGet(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpStatusGet,
		Description: "Report restoreweaved daemon status: catalog health, identify rules digest, and the list of operations this build does not implement.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ statusGetInput) (*mcpsdk.CallToolResult, command.StatusData, error) {
		result, err := call(ctx, conn, command.OpStatusGet, map[string]any{})
		if err != nil {
			return nil, command.StatusData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.StatusData{}, nil
		}
		var data command.StatusData
		if err := decodeData(result, &data); err != nil {
			return nil, command.StatusData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: statusText(data)}}}, data, nil
	})
}

type capabilityListInput struct{}

func addCapabilityList(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpCapabilityList,
		Description: "List every known command operation and its server-side availability state.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ capabilityListInput) (*mcpsdk.CallToolResult, command.CapabilityListData, error) {
		result, err := call(ctx, conn, command.OpCapabilityList, map[string]any{})
		if err != nil {
			return nil, command.CapabilityListData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.CapabilityListData{}, nil
		}
		var data command.CapabilityListData
		if err := decodeData(result, &data); err != nil {
			return nil, command.CapabilityListData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: capabilitiesText(data)}}}, data, nil
	})
}

// namespaceListArgs mirrors the daemon namespace.list input: catalog stable
// IDs, not filesystem paths.
type namespaceListArgs struct {
	WorkspaceID string `json:"workspace_id"`
	RootID      string `json:"root_id"`
	ParentID    string `json:"parent_id,omitempty"`
}

func addNamespaceList(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpNamespaceList,
		Description: "List namespace entries under a catalog root or a parent entry (workspace/root/parent are catalog stable IDs, not filesystem paths).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args namespaceListArgs) (*mcpsdk.CallToolResult, command.NamespaceListData, error) {
		result, err := call(ctx, conn, command.OpNamespaceList, args)
		if err != nil {
			return nil, command.NamespaceListData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.NamespaceListData{}, nil
		}
		var data command.NamespaceListData
		if err := decodeData(result, &data); err != nil {
			return nil, command.NamespaceListData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: namespaceText(data)}}}, data, nil
	})
}

type namespaceResolveArgs struct {
	WorkspaceID string `json:"workspace_id"`
	RootID      string `json:"root_id,omitempty"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
	Path        string `json:"path"`
}

func addNamespaceResolve(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpNamespaceResolve,
		Description: "Resolve slash-separated display-path components to a catalog entry id. Does not follow symbolic links.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args namespaceResolveArgs) (*mcpsdk.CallToolResult, command.NamespaceResolveData, error) {
		result, err := call(ctx, conn, command.OpNamespaceResolve, args)
		if err != nil {
			return nil, command.NamespaceResolveData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.NamespaceResolveData{}, nil
		}
		var data command.NamespaceResolveData
		if err := decodeData(result, &data); err != nil {
			return nil, command.NamespaceResolveData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: namespaceResolveText(data)}}}, data, nil
	})
}

type representationListArgs struct {
	WorkspaceID   string `json:"workspace_id"`
	SubjectRef    string `json:"subject_ref,omitempty"`
	EntryID       string `json:"entry_id,omitempty"`
	FileVersionID string `json:"file_version_id,omitempty"`
}

func addRepresentationList(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpRepresentationList,
		Description: "List representations for one catalog subject or file version. This does not open content.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args representationListArgs) (*mcpsdk.CallToolResult, command.RepresentationListData, error) {
		result, err := call(ctx, conn, command.OpRepresentationList, args)
		if err != nil {
			return nil, command.RepresentationListData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.RepresentationListData{}, nil
		}
		var data command.RepresentationListData
		if err := decodeData(result, &data); err != nil {
			return nil, command.RepresentationListData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: representationText(data)}}}, data, nil
	})
}

type searchQueryArgs struct {
	Query        string `json:"query"`
	WorkspaceID  string `json:"workspace_id,omitempty"`
	GenerationID string `json:"index_generation_ref,omitempty"`
}

func addSearchQuery(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpSearchQuery,
		Description: "Query the bundled lexical index. The daemon returns an unimplemented or degraded outcome when no index generation is available.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args searchQueryArgs) (*mcpsdk.CallToolResult, any, error) {
		input := map[string]any{"query": args.Query}
		if args.WorkspaceID != "" {
			input["workspace_id"] = args.WorkspaceID
		}
		if args.GenerationID != "" {
			input["index_generation_ref"] = args.GenerationID
		}
		result, err := call(ctx, conn, command.OpSearchQuery, input)
		if err != nil {
			return nil, nil, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), nil, nil
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(result.Data)}}}, nil, nil
	})
}

type annotationListArgs struct {
	WorkspaceID string `json:"workspace_id"`
	SubjectRef  string `json:"subject_ref,omitempty"`
}

func addAnnotationList(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpAnnotationList,
		Description: "List durable tags and notes for a workspace or one subject. This tool is read-only.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args annotationListArgs) (*mcpsdk.CallToolResult, command.AnnotationListData, error) {
		result, err := call(ctx, conn, command.OpAnnotationList, args)
		if err != nil {
			return nil, command.AnnotationListData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.AnnotationListData{}, nil
		}
		var data command.AnnotationListData
		if err := decodeData(result, &data); err != nil {
			return nil, command.AnnotationListData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: annotationsText(data)}}}, data, nil
	})
}

type audioListArgs struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
}

func addAudioList(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpAudioList,
		Description: "List admitted ID3/FLAC/OGG tag artifacts as tracks and derived albums. This tool is read-only and is not a player.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args audioListArgs) (*mcpsdk.CallToolResult, command.AudioListData, error) {
		result, err := call(ctx, conn, command.OpAudioList, args)
		if err != nil {
			return nil, command.AudioListData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.AudioListData{}, nil
		}
		var data command.AudioListData
		if err := decodeData(result, &data); err != nil {
			return nil, command.AudioListData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: audioText(data)}}}, data, nil
	})
}

type booksListArgs struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
}

func addBooksList(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpBooksList,
		Description: "List admitted EPUB OPF metadata and TXT/Markdown extracts as works. This tool is read-only and is not a reader.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args booksListArgs) (*mcpsdk.CallToolResult, command.BookListData, error) {
		result, err := call(ctx, conn, command.OpBooksList, args)
		if err != nil {
			return nil, command.BookListData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.BookListData{}, nil
		}
		var data command.BookListData
		if err := decodeData(result, &data); err != nil {
			return nil, command.BookListData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: booksText(data)}}}, data, nil
	})
}

// call executes one envelope round trip against the daemon.
func call(ctx context.Context, conn *transport.Conn, operation string, input any) (command.Result, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return command.Result{}, fmt.Errorf("encode input: %w", err)
	}
	return conn.Do(ctx, command.Envelope{Operation: operation, Input: payload})
}

func decodeData(result command.Result, into any) error {
	if err := json.Unmarshal(result.Data, into); err != nil {
		return fmt.Errorf("decode result data: %w", err)
	}
	return nil
}

// failure converts a daemon FAILED result into an MCP error result with the
// daemon's reasons as text. It never rewrites the outcome.
func failure(result command.Result) *mcpsdk.CallToolResult {
	var messages []string
	for _, reason := range result.Reasons {
		messages = append(messages, fmt.Sprintf("%s: %s", reason.Code, reason.Message))
	}
	text := fmt.Sprintf("operation %s failed: %s", result.Operation, strings.Join(messages, "; "))
	res := &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}}}
	res.IsError = true
	return res
}

func statusText(data command.StatusData) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "controller: %s\n", data.Controller)
	fmt.Fprintf(&builder, "catalog path: %s\n", data.Catalog.Path)
	fmt.Fprintf(&builder, "catalog ok: %t\n", data.Catalog.OK)
	fmt.Fprintf(&builder, "identify id: %s\n", data.Identify.ID)
	fmt.Fprintf(&builder, "identify rules digest: %s\n", data.Identify.RulesDigest)
	if data.Listen != "" {
		fmt.Fprintf(&builder, "listen: %s\n", data.Listen)
	}
	fmt.Fprintf(&builder, "unimplemented: %s\n", strings.Join(data.Unimplemented, ", "))
	return builder.String()
}

func capabilitiesText(data command.CapabilityListData) string {
	var builder strings.Builder
	for _, capability := range data.Capabilities {
		fmt.Fprintf(&builder, "%s %s %s", capability.Kind, capability.ID, capability.State)
		if capability.Version != "" {
			fmt.Fprintf(&builder, " v%s", capability.Version)
		}
		if capability.Notes != "" {
			fmt.Fprintf(&builder, " (%s)", capability.Notes)
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

func namespaceText(data command.NamespaceListData) string {
	var builder strings.Builder
	for _, entry := range data.Entries {
		fmt.Fprintf(&builder, "%s %s %s\n", entry.EntryType, entry.ID, entry.DisplayName)
	}
	return builder.String()
}

func namespaceResolveText(data command.NamespaceResolveData) string {
	return fmt.Sprintf("%s %s %s %s\n", data.Entry.EntryType, data.PathRef, data.Path, data.Entry.DisplayName)
}

func representationText(data command.RepresentationListData) string {
	if len(data.Representations) == 0 {
		return "no representations\n"
	}
	var builder strings.Builder
	for _, item := range data.Representations {
		fmt.Fprintf(&builder, "%s %s %s %s %s\n",
			item.Class, item.ID, item.CodecProfileRef, item.Placement, item.ContentID)
	}
	return builder.String()
}

func annotationsText(data command.AnnotationListData) string {
	var builder strings.Builder
	if len(data.Annotations) == 0 {
		return "no annotations\n"
	}
	for _, item := range data.Annotations {
		fmt.Fprintf(&builder, "%s %s r%d %s %s\n", item.Kind, item.ID, item.Revision, item.SubjectRef, item.Body)
	}
	return builder.String()
}

func audioText(data command.AudioListData) string {
	if len(data.Tracks) == 0 {
		return "no tracks\n"
	}
	var builder strings.Builder
	for _, album := range data.Albums {
		fmt.Fprintf(&builder, "album\t%s\t%s\t%s\t%d\n", album.Artist, album.Title, album.Year, len(album.SubjectRefs))
	}
	for _, track := range data.Tracks {
		fmt.Fprintf(&builder, "track\t%s\t%d\t%s\t%s\t%s\n",
			track.Name, track.Track, track.Title, track.Artist, track.Album)
	}
	return builder.String()
}

func booksText(data command.BookListData) string {
	if len(data.Works) == 0 {
		return "no works\n"
	}
	var builder strings.Builder
	for _, author := range data.Authors {
		fmt.Fprintf(&builder, "author\t%s\t%d\n", author.Name, len(author.SubjectRefs))
	}
	for _, work := range data.Works {
		fmt.Fprintf(&builder, "work\t%s\t%s\t%s\t%s\t%s\n",
			work.Kind, work.Name, work.Title, work.Author, work.Year)
	}
	return builder.String()
}
