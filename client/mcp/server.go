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
// daemon connection. Tool names use lowercase dotted names matching the
// command ABI. The initial set is read-only inspect: status, doctor,
// capabilities, plans, jobs, snapshots, namespace, representations, search,
// annotations, catalog slices, and bounded content handles. It does not
// expose ingest, restore, verify, cancel, or annotation mutation. Content
// tool results carry a range digest and a tiny untrusted preview, not
// unbounded base64.
func New(conn *transport.Conn) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: serverName, Version: serverVersion}, nil)
	addStatusGet(server, conn)
	addDoctorCheck(server, conn)
	addCapabilityList(server, conn)
	addPlanGet(server, conn)
	addJobEvents(server, conn)
	addSnapshotList(server, conn)
	addSnapshotDiff(server, conn)
	addNamespaceList(server, conn)
	addNamespaceResolve(server, conn)
	addNamespaceStat(server, conn)
	addNamespaceReadlink(server, conn)
	addRepresentationList(server, conn)
	budget := newContentBudget()
	addContentOpen(server, conn)
	addContentRead(server, conn, budget)
	addContentClose(server, conn)
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

type doctorCheckArgs struct {
	Source     string `json:"source,omitempty"`
	Repository string `json:"repository,omitempty"`
}

func addDoctorCheck(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpDoctorCheck,
		Description: "Read-only readiness report for catalog, repository, identify, processors, and optional source path. Does not select a release engine.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args doctorCheckArgs) (*mcpsdk.CallToolResult, command.DoctorData, error) {
		input := map[string]any{}
		if args.Source != "" {
			input["source"] = args.Source
		}
		if args.Repository != "" {
			input["repository"] = args.Repository
		}
		result, err := call(ctx, conn, command.OpDoctorCheck, input)
		if err != nil {
			return nil, command.DoctorData{}, err
		}
		if result.Status != command.StatusSucceeded && result.Status != command.StatusDegraded {
			return failure(result), command.DoctorData{}, nil
		}
		var data command.DoctorData
		if err := decodeData(result, &data); err != nil {
			return nil, command.DoctorData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: doctorText(data)}}}, data, nil
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

type planGetArgs struct {
	WorkspaceID string `json:"workspace_id"`
	PlanID      string `json:"plan_id"`
}

func addPlanGet(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpPlanGet,
		Description: "Read one immutable ingest or restore plan. This tool is read-only and does not apply plans.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args planGetArgs) (*mcpsdk.CallToolResult, command.PlanGetData, error) {
		result, err := call(ctx, conn, command.OpPlanGet, args)
		if err != nil {
			return nil, command.PlanGetData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.PlanGetData{}, nil
		}
		var data command.PlanGetData
		if err := decodeData(result, &data); err != nil {
			return nil, command.PlanGetData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: planGetText(data)}}}, data, nil
	})
}

type jobEventsArgs struct {
	WorkspaceID   string `json:"workspace_id"`
	JobID         string `json:"job_id"`
	AfterSequence int64  `json:"after_sequence,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

func addJobEvents(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpJobEvents,
		Description: "Read a bounded ordered page of durable job events. Read-only; does not cancel work or roll back snapshots.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args jobEventsArgs) (*mcpsdk.CallToolResult, command.JobEventsData, error) {
		result, err := call(ctx, conn, command.OpJobEvents, args)
		if err != nil {
			return nil, command.JobEventsData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.JobEventsData{}, nil
		}
		var data command.JobEventsData
		if err := decodeData(result, &data); err != nil {
			return nil, command.JobEventsData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: jobEventsText(data)}}}, data, nil
	})
}

// namespaceListArgs mirrors the daemon namespace.list input: catalog stable
// IDs, not filesystem paths.
type namespaceListArgs struct {
	WorkspaceID string `json:"workspace_id"`
	RootID      string `json:"root_id"`
	ParentID    string `json:"parent_id,omitempty"`
}

type snapshotListArgs struct{}

func addSnapshotList(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpSnapshotList,
		Description: "List portable snapshots in the repository. Read-only; does not verify or restore.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ snapshotListArgs) (*mcpsdk.CallToolResult, command.SnapshotListData, error) {
		result, err := call(ctx, conn, command.OpSnapshotList, map[string]any{})
		if err != nil {
			return nil, command.SnapshotListData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.SnapshotListData{}, nil
		}
		var data command.SnapshotListData
		if err := decodeData(result, &data); err != nil {
			return nil, command.SnapshotListData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: snapshotListText(data)}}}, data, nil
	})
}

type snapshotDiffArgs struct {
	FromSnapshotRef string `json:"from_snapshot_ref"`
	ToSnapshotRef   string `json:"to_snapshot_ref"`
}

func addSnapshotDiff(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpSnapshotDiff,
		Description: "Compare two committed snapshots by original path. Read-only and catalog-free.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args snapshotDiffArgs) (*mcpsdk.CallToolResult, command.SnapshotDiffData, error) {
		result, err := call(ctx, conn, command.OpSnapshotDiff, args)
		if err != nil {
			return nil, command.SnapshotDiffData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.SnapshotDiffData{}, nil
		}
		var data command.SnapshotDiffData
		if err := decodeData(result, &data); err != nil {
			return nil, command.SnapshotDiffData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: snapshotDiffText(data)}}}, data, nil
	})
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

type namespaceEntryArgs struct {
	WorkspaceID string `json:"workspace_id"`
	EntryID     string `json:"entry_id"`
}

func addNamespaceStat(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpNamespaceStat,
		Description: "Read one catalog namespace entry by stable id. Read-only; does not follow symbolic links.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args namespaceEntryArgs) (*mcpsdk.CallToolResult, command.NamespaceStatData, error) {
		result, err := call(ctx, conn, command.OpNamespaceStat, args)
		if err != nil {
			return nil, command.NamespaceStatData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), command.NamespaceStatData{}, nil
		}
		var data command.NamespaceStatData
		if err := decodeData(result, &data); err != nil {
			return nil, command.NamespaceStatData{}, err
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: namespaceStatText(data)}}}, data, nil
	})
}

type namespaceReadlinkMCPData struct {
	EntryID       string `json:"entry_id"`
	TargetDisplay string `json:"target_display"`
}

func addNamespaceReadlink(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpNamespaceReadlink,
		Description: "Read the captured symlink target for one catalog entry. Does not follow the link or open a filesystem path.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args namespaceEntryArgs) (*mcpsdk.CallToolResult, namespaceReadlinkMCPData, error) {
		result, err := call(ctx, conn, command.OpNamespaceReadlink, args)
		if err != nil {
			return nil, namespaceReadlinkMCPData{}, err
		}
		if result.Status != command.StatusSucceeded {
			return failure(result), namespaceReadlinkMCPData{}, nil
		}
		var data command.NamespaceReadlinkData
		if err := decodeData(result, &data); err != nil {
			return nil, namespaceReadlinkMCPData{}, err
		}
		projected := namespaceReadlinkMCPData{EntryID: data.EntryID, TargetDisplay: data.TargetDisplay}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: namespaceReadlinkText(data)}}}, projected, nil
	})
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
	Query        string   `json:"query"`
	WorkspaceID  string   `json:"workspace_id,omitempty"`
	GenerationID string   `json:"index_generation_ref,omitempty"`
	Dimension    string   `json:"dimension,omitempty"`
	Axes         []string `json:"construct_axes,omitempty"`
	Fuse         []string `json:"fuse,omitempty"`
}

func addSearchQuery(server *mcpsdk.Server, conn *transport.Conn) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        command.OpSearchQuery,
		Description: "Query one named index dimension, or fuse several. Default is the bundled lexical FTS5 generation. Fixture acoustic/semantic/multimodal dimensions degrade when no generation exists.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, args searchQueryArgs) (*mcpsdk.CallToolResult, any, error) {
		input := map[string]any{"query": args.Query}
		if args.WorkspaceID != "" {
			input["workspace_id"] = args.WorkspaceID
		}
		if args.GenerationID != "" {
			input["index_generation_ref"] = args.GenerationID
		}
		if args.Dimension != "" {
			input["dimension"] = args.Dimension
		}
		if len(args.Axes) > 0 {
			input["construct_axes"] = args.Axes
		}
		if len(args.Fuse) > 0 {
			input["fuse"] = args.Fuse
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
	if data.Repository != nil {
		fmt.Fprintf(&builder, "repository: %s\n", data.Repository.Path)
		fmt.Fprintf(&builder, "repository ok: %t\n", data.Repository.OK)
		fmt.Fprintf(&builder, "snapshots: %d\n", data.Repository.Snapshots)
	}
	fmt.Fprintf(&builder, "publications: %d\n", data.Publications)
	fmt.Fprintf(&builder, "plans: %d\n", data.Plans)
	for _, plan := range data.RecentPlans {
		fmt.Fprintf(&builder, "plan: %s %s %s\n", plan.PlanID, plan.Kind, plan.State)
	}
	fmt.Fprintf(&builder, "jobs: %d\n", data.Jobs)
	if data.OpenHandles > 0 || data.ReapedHandles > 0 {
		fmt.Fprintf(&builder, "open handles: %d\n", data.OpenHandles)
		fmt.Fprintf(&builder, "reaped handles: %d\n", data.ReapedHandles)
	}
	for _, job := range data.RecentJobs {
		fmt.Fprintf(&builder, "job: %s %s %s\n", job.JobID, job.Kind, job.State)
	}
	fmt.Fprintf(&builder, "unimplemented: %s\n", strings.Join(data.Unimplemented, ", "))
	return builder.String()
}

func doctorText(data command.DoctorData) string {
	var builder strings.Builder
	if data.OK {
		builder.WriteString("doctor ok\n")
	} else {
		builder.WriteString("doctor degraded\n")
	}
	for _, check := range data.Checks {
		state := "ok"
		if !check.OK {
			state = "fail"
		}
		fmt.Fprintf(&builder, "%s %s %s\n", check.ID, state, check.Message)
	}
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

func planGetText(data command.PlanGetData) string {
	return fmt.Sprintf("%s %s %s applied=%t executable=%t\n", data.Kind, data.PlanID, data.PlanDigest, data.Applied, data.Executable)
}

func jobEventsText(data command.JobEventsData) string {
	return fmt.Sprintf("%s %s events=%d next=%d terminal=%t\n", data.JobID, data.JobState, len(data.Events), data.NextSequence, data.Terminal)
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

func namespaceStatText(data command.NamespaceStatData) string {
	return fmt.Sprintf("%s %s %s\n", data.Entry.EntryType, data.Entry.ID, data.Entry.DisplayName)
}

func namespaceReadlinkText(data command.NamespaceReadlinkData) string {
	return fmt.Sprintf("%s %s\n", data.EntryID, data.TargetDisplay)
}

func snapshotListText(data command.SnapshotListData) string {
	if len(data.Snapshots) == 0 {
		return "no snapshots\n"
	}
	var builder strings.Builder
	for _, snapshot := range data.Snapshots {
		fmt.Fprintf(&builder, "%s %s %s\n", snapshot.SnapshotRef, snapshot.CreatedAt, snapshot.ManifestDigest)
	}
	return builder.String()
}

func snapshotDiffText(data command.SnapshotDiffData) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s -> %s changes=%d\n", data.FromSnapshotRef, data.ToSnapshotRef, len(data.Changes))
	for _, change := range data.Changes {
		path := change.Path
		if path == "" {
			path = change.ToPath
		}
		fmt.Fprintf(&builder, "%s %s\n", change.Kind, path)
	}
	return builder.String()
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
