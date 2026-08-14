package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/access"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/identify"
	"github.com/ailiheizi/restoreweave/server/internal/processor"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// Dispatcher routes command envelopes to implemented operations and returns a
// truthful result for every operation, including known-but-unimplemented ones.
// It only reads the rebuildable SQLite projection; it never fabricates success
// for operations that have no implementation in this build.
type Dispatcher struct {
	store         *sqlite.Store
	catalogPath   string
	socketPath    string
	now           func() time.Time
	exact         *exact.Service
	search        *search.Indexer
	sessions      *contentSessions
	access        *access.Service
	implemented   map[string]bool
	unimplemented []string
}

// DispatcherOption configures optional exact-lane handlers.
type DispatcherOption func(*Dispatcher)

// WithExact enables capture-qualified ingest, snapshot list/diff/verify,
// recovery.export, and catalog-free restore when a repository-backed exact
// service is available.
func WithExact(service *exact.Service) DispatcherOption {
	return func(d *Dispatcher) {
		d.exact = service
	}
}

// WithExactDir opens a directory CAS at repoDir and enables the exact lane.
// Harness tests outside server/internal use this instead of constructing
// internal exact.Service values.
func WithExactDir(store *sqlite.Store, repoDir string) (DispatcherOption, error) {
	if store == nil {
		return nil, errors.New("catalog is required")
	}
	repo, err := repository.OpenDir(repoDir)
	if err != nil {
		return nil, err
	}
	return WithExact(&exact.Service{Store: store, Repo: repo}), nil
}

// NewDispatcher wires the control plane to the opened catalog. catalogPath is
// reported by status.get; socketPath is reported as the listen address.
func NewDispatcher(store *sqlite.Store, catalogPath, socketPath string, opts ...DispatcherOption) *Dispatcher {
	implemented := map[string]bool{
		command.OpStatusGet:          true,
		command.OpCapabilityList:     true,
		command.OpNamespaceList:      true,
		command.OpNamespaceResolve:   true,
		command.OpNamespaceStat:      true,
		command.OpNamespaceReadlink:  true,
		command.OpRepresentationList: true,
	}
	dispatcher := &Dispatcher{
		store:       store,
		catalogPath: catalogPath,
		socketPath:  socketPath,
		now:         time.Now,
		implemented: implemented,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(dispatcher)
		}
	}
	if dispatcher.exact != nil {
		implemented[command.OpPlanIngest] = true
		implemented[command.OpPlanRestore] = true
		implemented[command.OpSnapshotList] = true
		implemented[command.OpSnapshotDiff] = true
		implemented[command.OpSnapshotVerify] = true
		implemented[command.OpRecoveryExport] = true
		implemented[command.OpAnnotationList] = true
		implemented[command.OpAnnotationUpsert] = true
		implemented[command.OpAnnotationDelete] = true
		implemented[command.OpAnnotationExport] = true
		implemented[command.OpAnnotationImport] = true
		implemented[command.OpSearchQuery] = true
		implemented[command.OpContentOpen] = true
		implemented[command.OpContentRead] = true
		implemented[command.OpContentClose] = true
		if dispatcher.search == nil && dispatcher.exact.Repo != nil {
			dir := filepath.Join(dispatcher.exact.Repo.Root(), "indexes")
			dispatcher.search = &search.Indexer{
				Store:  store,
				Engine: &search.Engine{Dir: dir},
			}
		}
		if dispatcher.exact.Indexer == nil {
			dispatcher.exact.Indexer = dispatcher.search
		}
		if dispatcher.exact.Processor == nil && dispatcher.exact.Repo != nil {
			dispatcher.exact.Processor = processor.NewHost(store, dispatcher.exact.Repo, processor.Options{
				StagingDir: filepath.Join(dispatcher.exact.Repo.Root(), "staging"),
			})
		}
		if dispatcher.sessions == nil && dispatcher.exact.Repo != nil {
			dispatcher.sessions = newContentSessions(dispatcher.exact.Repo)
		}
		if dispatcher.access == nil && dispatcher.exact.Repo != nil {
			dispatcher.access = &access.Service{Store: store, Repo: dispatcher.exact.Repo}
		}
		implemented[command.OpAudioList] = true
		implemented[command.OpBooksList] = true
	}
	var unimplemented []string
	for _, operation := range command.KnownOperations() {
		if !implemented[operation] {
			unimplemented = append(unimplemented, operation)
		}
	}
	sort.Strings(unimplemented)
	dispatcher.unimplemented = unimplemented
	return dispatcher
}

// Handle executes one envelope and always returns a command.Result. Malformed
// envelopes and unknown operations fail explicitly; no status is ever inferred
// from an absent implementation.
func (d *Dispatcher) Handle(ctx context.Context, raw command.Envelope) command.Result {
	started := d.now().UTC()
	env, err := command.NormalizeEnvelope(raw)
	if err != nil {
		return failedRawResult(raw, started, newReason(ReasonCodeInvalidRequest, err.Error()))
	}
	switch env.Operation {
	case command.OpStatusGet:
		return d.handleStatusGet(ctx, env, started)
	case command.OpCapabilityList:
		return d.handleCapabilityList(env, started)
	case command.OpNamespaceList:
		return d.handleNamespaceList(ctx, env, started)
	case command.OpNamespaceResolve:
		return d.handleNamespaceResolve(ctx, env, started)
	case command.OpNamespaceStat:
		return d.handleNamespaceStat(ctx, env, started)
	case command.OpNamespaceReadlink:
		return d.handleNamespaceReadlink(ctx, env, started)
	case command.OpRepresentationList:
		return d.handleRepresentationList(ctx, env, started)
	case command.OpPlanIngest:
		return d.handlePlanIngest(ctx, env, started)
	case command.OpPlanRestore:
		return d.handlePlanRestore(ctx, env, started)
	case command.OpSnapshotList:
		return d.handleSnapshotList(ctx, env, started)
	case command.OpSnapshotDiff:
		return d.handleSnapshotDiff(ctx, env, started)
	case command.OpSnapshotVerify:
		return d.handleSnapshotVerify(ctx, env, started)
	case command.OpRecoveryExport:
		return d.handleRecoveryExport(ctx, env, started)
	case command.OpAnnotationList:
		return d.handleAnnotationList(ctx, env, started)
	case command.OpAnnotationUpsert:
		return d.handleAnnotationUpsert(ctx, env, started)
	case command.OpAnnotationDelete:
		return d.handleAnnotationDelete(ctx, env, started)
	case command.OpAnnotationExport:
		return d.handleAnnotationExport(ctx, env, started)
	case command.OpAnnotationImport:
		return d.handleAnnotationImport(ctx, env, started)
	case command.OpSearchQuery:
		return d.handleSearchQuery(ctx, env, started)
	case command.OpContentOpen:
		return d.handleContentOpen(ctx, env, started)
	case command.OpContentRead:
		return d.handleContentRead(ctx, env, started)
	case command.OpContentClose:
		return d.handleContentClose(ctx, env, started)
	case command.OpGatewayMount:
		return d.handleGatewayMount(ctx, env, started)
	case command.OpGatewayUnmount:
		return d.handleGatewayUnmount(ctx, env, started)
	case command.OpAudioList:
		return d.handleAudioList(ctx, env, started)
	case command.OpBooksList:
		return d.handleBooksList(ctx, env, started)
	default:
		if command.IsKnown(env.Operation) {
			return unimplementedResult(env, started)
		}
		return unknownOperationResult(env, started)
	}
}

// UnimplementedOperations lists every known operation this build does not
// implement, in deterministic order. status.get reports it verbatim.
func (d *Dispatcher) UnimplementedOperations() []string {
	return append([]string(nil), d.unimplemented...)
}

func (d *Dispatcher) handleStatusGet(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	catalogOK := true
	if err := d.store.Ping(ctx); err != nil {
		catalogOK = false
	}
	data := command.StatusData{
		Controller: "restoreweaved",
		Catalog: command.CatalogStatus{
			Path: d.catalogPath,
			OK:   catalogOK,
		},
		Identify: command.IdentifyStatus{
			ID:          IdentifyBuiltinID,
			RulesDigest: identify.RulesDigest(),
		},
		Listen:        d.socketPath,
		Unimplemented: d.unimplemented,
	}
	return succeeded(env, started, data)
}

func (d *Dispatcher) handleCapabilityList(env command.Envelope, started time.Time) command.Result {
	capabilities := make([]command.Capability, 0, len(command.KnownOperations())+2)
	for _, operation := range command.KnownOperations() {
		state := command.CapabilityUnavailable
		notes := "not implemented by this restoreweaved build"
		if d.implemented[operation] {
			state = command.CapabilityAvailable
			notes = "implemented by restoreweaved"
		}
		capabilities = append(capabilities, command.Capability{
			Kind:  "operation",
			ID:    operation,
			State: state,
			Notes: notes,
		})
	}
	capabilities = append(capabilities,
		command.Capability{
			Kind:    "transport",
			ID:      "command-envelope-json-over-unix-socket",
			State:   command.CapabilityAvailable,
			Version: "1",
			Notes:   "client/command Envelope JSON in 4-byte big-endian length-prefixed frames",
		},
		command.Capability{
			Kind:    "identify",
			ID:      IdentifyBuiltinID,
			State:   command.CapabilityAvailable,
			Version: identify.RulesDigest(),
			Notes:   "host-owned suffix and magic-byte detector",
		},
	)
	return succeeded(env, started, command.CapabilityListData{Capabilities: capabilities})
}

// namespaceListInput is the namespace.list input. Workspace, root, and parent
// references are catalog stable IDs, not filesystem paths.
type namespaceListInput struct {
	WorkspaceID string `json:"workspace_id"`
	RootID      string `json:"root_id"`
	ParentID    string `json:"parent_id,omitempty"`
}

func (d *Dispatcher) handleNamespaceList(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input namespaceListInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("root_id", input.RootID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if input.ParentID != "" {
		if err := requireStableID("parent_id", input.ParentID); err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	entries, err := d.store.ListNamespaceChildren(ctx, input.WorkspaceID, input.RootID, input.ParentID)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	projected := make([]command.NamespaceEntryData, 0, len(entries))
	for _, entry := range entries {
		projected = append(projected, projectNamespaceEntry(entry))
	}
	return succeeded(env, started, command.NamespaceListData{
		RootID:   input.RootID,
		ParentID: input.ParentID,
		Entries:  projected,
	})
}

type namespaceResolveInput struct {
	WorkspaceID string `json:"workspace_id"`
	RootID      string `json:"root_id,omitempty"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
	Path        string `json:"path"`
}

func (d *Dispatcher) handleNamespaceResolve(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input namespaceResolveInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	parts, err := splitDisplayPath(input.Path)
	if err != nil {
		return invalidInputResult(env, started, err)
	}
	rootID := strings.TrimSpace(input.RootID)
	if rootID == "" {
		if strings.TrimSpace(input.SnapshotRef) == "" {
			return invalidInputResult(env, started, errString("root_id or snapshot_ref is required"))
		}
		publication, pubErr := d.store.GetPublicationBySnapshotRef(ctx, input.WorkspaceID, input.SnapshotRef)
		if pubErr != nil {
			if containsNotFound(pubErr) {
				return notFoundResult(env, started, "publication not found")
			}
			return catalogErrorResult(env, started, pubErr)
		}
		rootID = publication.NamespaceRootID
	} else if err := requireStableID("root_id", rootID); err != nil {
		return invalidInputResult(env, started, err)
	}
	parentID := ""
	var entry sqlite.NamespaceEntry
	for i, name := range parts {
		found, lookupErr := d.store.LookupNamespaceChild(ctx, input.WorkspaceID, rootID, parentID, nil, name)
		if lookupErr != nil {
			if containsNotFound(lookupErr) {
				return notFoundResult(env, started, "namespace path not found")
			}
			return catalogErrorResult(env, started, lookupErr)
		}
		if found.EntryType == sqlite.EntrySymlink && i < len(parts)-1 {
			return invalidInputResult(env, started, errString("namespace.resolve does not follow symbolic links"))
		}
		entry = found
		parentID = found.ID
	}
	projected := projectNamespaceEntry(entry)
	return succeeded(env, started, command.NamespaceResolveData{
		WorkspaceID: input.WorkspaceID,
		RootID:      rootID,
		Path:        strings.Join(parts, "/"),
		PathRef:     entry.ID,
		Entry:       projected,
	})
}

func splitDisplayPath(path string) ([]string, error) {
	trimmed := strings.Trim(path, "/")
	if strings.TrimSpace(trimmed) == "" {
		return nil, errString("path is required")
	}
	var parts []string
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return nil, fmt.Errorf("path %q is unsafe", path)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return nil, errString("path is required")
	}
	return parts, nil
}

// namespaceEntryInput is the namespace.stat and namespace.readlink input.
type namespaceEntryInput struct {
	WorkspaceID string `json:"workspace_id"`
	EntryID     string `json:"entry_id"`
}

func (d *Dispatcher) handleNamespaceStat(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	entry, reason, ok := d.lookupEntry(ctx, env, started)
	if !ok {
		return reason
	}
	return succeeded(env, started, command.NamespaceStatData{Entry: projectNamespaceEntry(entry)})
}

func (d *Dispatcher) handleNamespaceReadlink(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	entry, reason, ok := d.lookupEntry(ctx, env, started)
	if !ok {
		return reason
	}
	if entry.EntryType != sqlite.EntrySymlink {
		return failed(env, started, newReason(
			ReasonCodeInvalidInput,
			fmt.Sprintf("entry %s is not a symlink", entry.ID),
		))
	}
	return succeeded(env, started, command.NamespaceReadlinkData{
		EntryID:       entry.ID,
		TargetDisplay: entry.SymlinkTargetDisplay,
		TargetRaw:     entry.SymlinkTargetRaw,
	})
}

// lookupEntry resolves one namespace entry and maps store errors to results.
func (d *Dispatcher) lookupEntry(ctx context.Context, env command.Envelope, started time.Time) (sqlite.NamespaceEntry, command.Result, bool) {
	var input namespaceEntryInput
	if err := decodeInput(env.Input, &input); err != nil {
		return sqlite.NamespaceEntry{}, invalidInputResult(env, started, err), false
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return sqlite.NamespaceEntry{}, invalidInputResult(env, started, err), false
	}
	if err := requireStableID("entry_id", input.EntryID); err != nil {
		return sqlite.NamespaceEntry{}, invalidInputResult(env, started, err), false
	}
	entry, err := d.store.GetNamespaceEntry(ctx, input.WorkspaceID, input.EntryID)
	if err != nil {
		return sqlite.NamespaceEntry{}, namespaceLookupResult(env, started, err), false
	}
	return entry, command.Result{}, true
}

func namespaceLookupResult(env command.Envelope, started time.Time, err error) command.Result {
	if containsNotFound(err) {
		return notFoundResult(env, started, "namespace entry not found")
	}
	return catalogErrorResult(env, started, err)
}

func decodeInput(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return fmt.Errorf("input must be a JSON object")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	return nil
}

func requireStableID(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if !validStableID(value) {
		return fmt.Errorf("%s must be a catalog stable id (prefix_32-hex)", name)
	}
	return nil
}

func invalidInputResult(env command.Envelope, started time.Time, err error) command.Result {
	return failed(env, started, newReason(ReasonCodeInvalidInput, err.Error()))
}

func containsNotFound(err error) bool {
	return errors.Is(err, sqlite.ErrNotFound) || (err != nil && strings.Contains(err.Error(), "not found"))
}

func projectNamespaceEntry(entry sqlite.NamespaceEntry) command.NamespaceEntryData {
	return command.NamespaceEntryData{
		ID:                   entry.ID,
		RootID:               entry.RootID,
		ParentID:             entry.ParentID,
		DisplayName:          entry.DisplayName,
		EntryType:            string(entry.EntryType),
		ContentID:            entry.ContentID,
		FileVersionID:        entry.FileVersionID,
		HardlinkGroupID:      entry.HardlinkGroupID,
		LogicalSize:          entry.LogicalSize,
		AllocatedSize:        entry.AllocatedSize,
		SymlinkTargetDisplay: entry.SymlinkTargetDisplay,
	}
}
