package controlplane

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func newTestDispatcher(t *testing.T) (*Dispatcher, *sqlite.Store) {
	t.Helper()
	store := testutil.OpenStore(t, ":memory:")
	dispatcher := NewDispatcher(store, "/catalog/harness.sqlite", "/tmp/restoreweave/harness.sock")
	return dispatcher, store
}

func mustEnvelope(t *testing.T, operation string, input any) command.Envelope {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	env, err := command.NormalizeEnvelope(command.Envelope{Operation: operation, Input: raw})
	if err != nil {
		t.Fatalf("normalize envelope: %v", err)
	}
	return env
}

// mustAppliedIngest keeps tests that need a published namespace explicit about
// the Phase 2 boundary: planning is read-only and only plan.apply mutates the
// repository/catalog publication state.
func mustAppliedIngest(t *testing.T, ctx context.Context, dispatcher *Dispatcher, input any) command.PlanIngestData {
	t.Helper()
	planned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, input))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var plan command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &plan); err != nil {
		t.Fatalf("decode plan.ingest: %v", err)
	}
	applied := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": plan.WorkspaceID,
		"plan_id":      plan.PlanID,
		"plan_digest":  plan.PlanDigest,
	}))
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("plan.apply = %q: %+v", applied.Status, applied.Reasons)
	}
	var result command.PlanApplyData
	if err := json.Unmarshal(applied.Data, &result); err != nil {
		t.Fatalf("decode plan.apply: %v", err)
	}
	plan.WorkspaceID = firstNonEmpty(result.WorkspaceID, plan.WorkspaceID)
	plan.SourceID = firstNonEmpty(result.SourceID, plan.SourceID)
	plan.ScanID = result.ScanID
	plan.RootID = result.RootID
	plan.SnapshotRef = result.SnapshotRef
	plan.ManifestDigest = result.ManifestDigest
	plan.Files = result.Files
	plan.Bytes = result.Bytes
	plan.JobID = result.JobID
	return plan
}

func mustAppliedRestore(
	t *testing.T,
	ctx context.Context,
	dispatcher *Dispatcher,
	workspaceID, snapshotRef, destination string,
) command.PlanApplyData {
	t.Helper()
	planned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": snapshotRef,
		"destination":  destination,
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.restore = %q: %+v", planned.Status, planned.Reasons)
	}
	var plan command.PlanRestoreData
	if err := json.Unmarshal(planned.Data, &plan); err != nil {
		t.Fatalf("decode plan.restore: %v", err)
	}
	applied := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": workspaceID,
		"plan_id":      plan.PlanID,
		"plan_digest":  plan.PlanDigest,
	}))
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("apply restore = %q: %+v", applied.Status, applied.Reasons)
	}
	var result command.PlanApplyData
	if err := json.Unmarshal(applied.Data, &result); err != nil {
		t.Fatalf("decode restore apply: %v", err)
	}
	return result
}

func TestDispatcherUnknownOperationFails(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	result := dispatcher.Handle(context.Background(), mustEnvelope(t, "controller.dance", map[string]any{}))
	if result.Status != command.StatusFailed {
		t.Fatalf("status = %q, want FAILED", result.Status)
	}
	if !hasReasonCode(result, ReasonCodeUnknownOperation) {
		t.Fatalf("missing unknown_operation reason: %+v", result.Reasons)
	}
}

func TestDispatcherKnownButUnimplementedOperationFails(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	for _, operation := range []string{
		command.OpPlanIngest, command.OpSearchQuery,
		command.OpContentOpen, command.OpSnapshotVerify, command.OpAnnotationList,
		command.OpRecoveryExport, command.OpAudioList, command.OpBooksList,
	} {
		result := dispatcher.Handle(context.Background(), mustEnvelope(t, operation, map[string]any{}))
		if result.Status != command.StatusFailed {
			t.Fatalf("%s status = %q, want FAILED", operation, result.Status)
		}
		if !hasReasonCode(result, ReasonCodeUnimplemented) {
			t.Fatalf("%s missing unimplemented reason: %+v", operation, result.Reasons)
		}
	}
}

func TestDispatcherRejectsInvalidEnvelope(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	result := dispatcher.Handle(context.Background(), command.Envelope{Operation: "  "})
	if result.Status != command.StatusFailed {
		t.Fatalf("status = %q, want FAILED", result.Status)
	}
	if !hasReasonCode(result, ReasonCodeInvalidRequest) {
		t.Fatalf("missing invalid_request reason: %+v", result.Reasons)
	}
}

func TestDispatcherStatusGet(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpStatusGet, map[string]any{}))
	if result.Status != command.StatusSucceeded {
		t.Fatalf("status = %q: %+v", result.Status, result.Reasons)
	}
	var data command.StatusData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode status data: %v", err)
	}
	if data.Controller != "restoreweaved" {
		t.Fatalf("controller = %q", data.Controller)
	}
	if !data.Catalog.OK || data.Catalog.Path != "/catalog/harness.sqlite" {
		t.Fatalf("catalog = %+v", data.Catalog)
	}
	if data.Identify.ID == "" || data.Identify.RulesDigest == "" {
		t.Fatalf("identify = %+v", data.Identify)
	}
	if len(data.Unimplemented) == 0 {
		t.Fatal("unimplemented list is empty")
	}
	for _, operation := range []string{command.OpStatusGet, command.OpNamespaceList} {
		if containsString(data.Unimplemented, operation) {
			t.Fatalf("implemented operation %s listed as unimplemented", operation)
		}
	}
	if !containsString(data.Unimplemented, command.OpPlanIngest) {
		t.Fatalf("unimplemented operation %s missing from status", command.OpPlanIngest)
	}
	if containsString(data.Unimplemented, command.OpPlanGet) {
		t.Fatalf("implemented operation %s listed as unimplemented", command.OpPlanGet)
	}
	if data.Publications != 0 {
		t.Fatalf("publications = %d, want 0", data.Publications)
	}
	if data.Repository != nil {
		t.Fatalf("repository present without exact lane: %+v", data.Repository)
	}
}

func TestDispatcherCapabilityList(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpCapabilityList, map[string]any{}))
	if result.Status != command.StatusSucceeded {
		t.Fatalf("status = %q: %+v", result.Status, result.Reasons)
	}
	var data command.CapabilityListData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode capability data: %v", err)
	}
	states := map[string]string{}
	for _, capability := range data.Capabilities {
		states[capability.Kind+":"+capability.ID] = capability.State
	}
	if states["operation:"+command.OpStatusGet] != command.CapabilityAvailable {
		t.Fatalf("status.get capability = %q, want AVAILABLE", states["operation:"+command.OpStatusGet])
	}
	if states["operation:"+command.OpSearchQuery] != command.CapabilityUnavailable {
		t.Fatalf("search.query capability = %q, want UNAVAILABLE", states["operation:"+command.OpSearchQuery])
	}
	if states["index-dimension:"+search.DimensionLexical] != command.CapabilityUnavailable {
		t.Fatalf("lexical dimension = %q, want UNAVAILABLE", states["index-dimension:"+search.DimensionLexical])
	}
	if states["index-dimension:"+search.DimensionAcoustic] != command.CapabilityUnavailable {
		t.Fatalf("acoustic dimension = %q, want UNAVAILABLE", states["index-dimension:"+search.DimensionAcoustic])
	}
	want := len(command.KnownOperations()) + 2 + len(search.DeclaredDimensions(search.ProviderReadiness{})) + 1
	if len(data.Capabilities) != want {
		t.Fatalf("capability count = %d, want %d", len(data.Capabilities), want)
	}
}

func TestDispatcherNamespaceListEmptyCatalog(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": "wsp_00000000000000000000000000000000",
		"root_id":      "nsr_00000000000000000000000000000000",
	}))
	if result.Status != command.StatusSucceeded {
		t.Fatalf("status = %q: %+v", result.Status, result.Reasons)
	}
	var data command.NamespaceListData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode namespace list: %v", err)
	}
	if data.Entries == nil || len(data.Entries) != 0 {
		t.Fatalf("entries = %+v, want empty list", data.Entries)
	}
}

func TestDispatcherNamespaceValidation(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	for _, input := range []map[string]any{
		{"root_id": "nsr_00000000000000000000000000000000"},
		{"workspace_id": "not-an-id", "root_id": "nsr_00000000000000000000000000000000"},
		{"workspace_id": "wsp_00000000000000000000000000000000", "root_id": "oops"},
	} {
		result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpNamespaceList, input))
		if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeInvalidInput) {
			t.Fatalf("input %+v: status = %q reasons = %+v", input, result.Status, result.Reasons)
		}
	}
}

func TestDispatcherNamespaceSeededRoundTrip(t *testing.T) {
	dispatcher, store := newTestDispatcher(t)
	seed := testutil.SeedNamespace(t, store)
	ctx := context.Background()

	list := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"root_id":      seed.RootID,
	}))
	if list.Status != command.StatusSucceeded {
		t.Fatalf("list root status = %q: %+v", list.Status, list.Reasons)
	}
	var listData command.NamespaceListData
	if err := json.Unmarshal(list.Data, &listData); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listData.Entries) != 1 || listData.Entries[0].ID != seed.DirEntryID {
		t.Fatalf("root children = %+v", listData.Entries)
	}

	children := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"root_id":      seed.RootID,
		"parent_id":    seed.DirEntryID,
	}))
	if children.Status != command.StatusSucceeded {
		t.Fatalf("list children status = %q: %+v", children.Status, children.Reasons)
	}
	var childrenData command.NamespaceListData
	if err := json.Unmarshal(children.Data, &childrenData); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(childrenData.Entries) != 2 {
		t.Fatalf("directory children = %+v", childrenData.Entries)
	}
	byName := map[string]command.NamespaceEntryData{}
	for _, entry := range childrenData.Entries {
		byName[entry.DisplayName] = entry
	}
	fileEntry, ok := byName["\\xfftrack.flac"]
	if !ok || fileEntry.EntryType != string(sqlite.EntryFile) || fileEntry.FileVersionID == "" {
		t.Fatalf("file child missing or incomplete: %+v", byName)
	}
	if _, ok := byName["current"]; !ok {
		t.Fatalf("symlink child missing: %+v", byName)
	}

	stat := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceStat, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"entry_id":     seed.FileEntryID,
	}))
	if stat.Status != command.StatusSucceeded {
		t.Fatalf("stat status = %q: %+v", stat.Status, stat.Reasons)
	}
	var statData command.NamespaceStatData
	if err := json.Unmarshal(stat.Data, &statData); err != nil {
		t.Fatalf("decode stat: %v", err)
	}
	if statData.Entry.ID != seed.FileEntryID || statData.Entry.LogicalSize == nil || *statData.Entry.LogicalSize != 16 {
		t.Fatalf("stat entry = %+v", statData.Entry)
	}

	readlink := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceReadlink, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"entry_id":     seed.SymlinkEntryID,
	}))
	if readlink.Status != command.StatusSucceeded {
		t.Fatalf("readlink status = %q: %+v", readlink.Status, readlink.Reasons)
	}
	var linkData command.NamespaceReadlinkData
	if err := json.Unmarshal(readlink.Data, &linkData); err != nil {
		t.Fatalf("decode readlink: %v", err)
	}
	if linkData.TargetDisplay != "\\xfftrack.flac" || len(linkData.TargetRaw) == 0 {
		t.Fatalf("readlink data = %+v", linkData)
	}

	notSymlink := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceReadlink, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"entry_id":     seed.FileEntryID,
	}))
	if notSymlink.Status != command.StatusFailed || !hasReasonCode(notSymlink, ReasonCodeInvalidInput) {
		t.Fatalf("readlink on file: status = %q reasons = %+v", notSymlink.Status, notSymlink.Reasons)
	}

	missing := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceStat, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"entry_id":     "nse_00000000000000000000000000000000",
	}))
	if missing.Status != command.StatusFailed || !hasReasonCode(missing, ReasonCodeNotFound) {
		t.Fatalf("missing stat: status = %q reasons = %+v", missing.Status, missing.Reasons)
	}

	resolved := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceResolve, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"root_id":      seed.RootID,
		"path":         "Music/\\xfftrack.flac",
	}))
	if resolved.Status != command.StatusSucceeded {
		t.Fatalf("resolve file = %q: %+v", resolved.Status, resolved.Reasons)
	}
	var resolveData command.NamespaceResolveData
	if err := json.Unmarshal(resolved.Data, &resolveData); err != nil {
		t.Fatalf("decode resolve: %v", err)
	}
	if resolveData.PathRef != seed.FileEntryID {
		t.Fatalf("resolve path_ref = %q, want %s", resolveData.PathRef, seed.FileEntryID)
	}

	link := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceResolve, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"root_id":      seed.RootID,
		"path":         "Music/current",
	}))
	if link.Status != command.StatusSucceeded {
		t.Fatalf("resolve symlink = %q: %+v", link.Status, link.Reasons)
	}
	var linkResolve command.NamespaceResolveData
	if err := json.Unmarshal(link.Data, &linkResolve); err != nil {
		t.Fatalf("decode symlink resolve: %v", err)
	}
	if linkResolve.PathRef != seed.SymlinkEntryID || linkResolve.Entry.EntryType != string(sqlite.EntrySymlink) {
		t.Fatalf("symlink resolve = %+v", linkResolve)
	}

	followed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceResolve, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"root_id":      seed.RootID,
		"path":         "Music/current/nested",
	}))
	if followed.Status != command.StatusFailed || !hasReasonCode(followed, ReasonCodeInvalidInput) {
		t.Fatalf("followed symlink: status = %q reasons = %+v", followed.Status, followed.Reasons)
	}

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpRepresentationList, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"subject_ref":  seed.FileEntryID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("representation.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var listedData command.RepresentationListData
	if err := json.Unmarshal(listed.Data, &listedData); err != nil {
		t.Fatalf("decode representation list: %v", err)
	}
	if listedData.FileVersionID != seed.FileVersionID || len(listedData.Representations) != 1 {
		t.Fatalf("representation list = %+v", listedData)
	}
	item := listedData.Representations[0]
	if !item.Authoritative || item.Class != command.RepresentationClassRecorded ||
		item.Placement != command.RepresentationPlacementUnknown || item.CodecProfileRef != "restic-stream/v1" {
		t.Fatalf("representation item = %+v", item)
	}

	empty := dispatcher.Handle(ctx, mustEnvelope(t, command.OpRepresentationList, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"entry_id":     seed.DirEntryID,
	}))
	if empty.Status != command.StatusSucceeded {
		t.Fatalf("directory representation.list = %q: %+v", empty.Status, empty.Reasons)
	}
	var emptyData command.RepresentationListData
	if err := json.Unmarshal(empty.Data, &emptyData); err != nil {
		t.Fatalf("decode empty representation list: %v", err)
	}
	if len(emptyData.Representations) != 0 {
		t.Fatalf("directory representations = %+v", emptyData.Representations)
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
