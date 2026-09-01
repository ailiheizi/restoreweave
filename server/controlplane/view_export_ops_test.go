package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// handleViewExport routes one view/export envelope to the handler method.
// The coordinator wires these operations into Dispatcher.Handle after this
// slice; tests exercise the handlers directly so dispatcher.go stays outside
// this slice's file scope.
func handleViewExport(d *Dispatcher, env command.Envelope) command.Result {
	started := time.Now().UTC()
	switch env.Operation {
	case command.OpViewSave:
		return d.handleViewSave(context.Background(), env, started)
	case command.OpViewGet:
		return d.handleViewGet(context.Background(), env, started)
	case command.OpViewEvaluate:
		return d.handleViewEvaluate(context.Background(), env, started)
	case command.OpViewList:
		return d.handleViewList(context.Background(), env, started)
	case command.OpExportList:
		return d.handleExportList(context.Background(), env, started)
	case command.OpExportPlan:
		return d.handleExportPlan(context.Background(), env, started)
	case command.OpExportApply:
		return d.handleExportApply(context.Background(), env, started)
	case command.OpExportVerify:
		return d.handleExportVerify(context.Background(), env, started)
	default:
		return unimplementedResult(env, started)
	}
}

func TestExportListReturnsFrozenManifestData(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.bin"), []byte("export list bytes"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	h := newViewExportHarness(t, root)

	planned := h.dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpExportPlan, map[string]any{
		"subjects": []string{h.subjectRef},
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("export.plan = %q: %+v", planned.Status, planned.Reasons)
	}
	var manifest command.ExportManifestData
	if err := json.Unmarshal(planned.Data, &manifest); err != nil {
		t.Fatalf("decode export.plan: %v", err)
	}

	listed := h.dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpExportList, nil))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("export.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var manifests []command.ExportManifestData
	if err := json.Unmarshal(listed.Data, &manifests); err != nil {
		t.Fatalf("decode export.list: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("export.list manifests = %+v", manifests)
	}
	got := manifests[0]
	if got.ManifestID != manifest.ManifestID || got.ManifestDigest != manifest.ManifestDigest ||
		got.SubjectCount != manifest.SubjectCount || got.Representation != manifest.Representation ||
		len(got.Items) != len(manifest.Items) || got.Items[0] != manifest.Items[0] {
		t.Fatalf("export.list manifest = %+v, planned = %+v", got, manifest)
	}
}

// viewExportHarness wires a dispatcher with the exact lane and a published
// ingest fixture so view.evaluate and export.* can operate on real subjects.
type viewExportHarness struct {
	dispatcher  *Dispatcher
	ingest      command.PlanIngestData
	subjectRef  string
	entryID     string
	contentID   string
	subjectName string
}

func newViewExportHarness(t *testing.T, root string) viewExportHarness {
	t.Helper()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))
	ctx := context.Background()
	ingest := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	children := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": ingest.WorkspaceID,
		"root_id":      ingest.RootID,
	}))
	if children.Status != command.StatusSucceeded {
		t.Fatalf("namespace.list = %q: %+v", children.Status, children.Reasons)
	}
	var listData command.NamespaceListData
	if err := json.Unmarshal(children.Data, &listData); err != nil {
		t.Fatalf("decode namespace.list: %v", err)
	}
	if len(listData.Entries) != 1 {
		t.Fatalf("ingest root children = %+v", listData.Entries)
	}
	entry := listData.Entries[0]
	return viewExportHarness{
		dispatcher: dispatcher, ingest: ingest,
		subjectRef: entry.SubjectRef, entryID: entry.ID, contentID: entry.ContentID,
		subjectName: entry.DisplayName,
	}
}

func TestViewSaveGetListAndRevisionBump(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)

	saved := handleViewExport(dispatcher, mustEnvelope(t, command.OpViewSave, map[string]any{
		"name": "novels", "query": "tag:novel", "fields": []string{"entry_type=REGULAR_FILE"},
	}))
	if saved.Status != command.StatusSucceeded {
		t.Fatalf("view.save = %q: %+v", saved.Status, saved.Reasons)
	}
	var first command.ViewData
	if err := json.Unmarshal(saved.Data, &first); err != nil {
		t.Fatalf("decode view.save: %v", err)
	}
	if first.ViewID == "" || first.Revision != 1 || first.Name != "novels" || first.Query != "tag:novel" {
		t.Fatalf("first view = %+v", first)
	}
	if first.Fields == nil || len(first.Fields) != 1 || first.Fields[0] != "entry_type=REGULAR_FILE" {
		t.Fatalf("first fields = %+v", first.Fields)
	}

	got := handleViewExport(dispatcher, mustEnvelope(t, command.OpViewGet, map[string]any{"view_id": first.ViewID}))
	if got.Status != command.StatusSucceeded {
		t.Fatalf("view.get by id = %q: %+v", got.Status, got.Reasons)
	}
	var byID command.ViewData
	if err := json.Unmarshal(got.Data, &byID); err != nil {
		t.Fatalf("decode view.get: %v", err)
	}
	if byID.ViewID != first.ViewID || byID.Name != "novels" || byID.Revision != 1 {
		t.Fatalf("view.get = %+v", byID)
	}

	gotByName := handleViewExport(dispatcher, mustEnvelope(t, command.OpViewGet, map[string]any{"name": "novels"}))
	if gotByName.Status != command.StatusSucceeded {
		t.Fatalf("view.get by name = %q: %+v", gotByName.Status, gotByName.Reasons)
	}
	var byName command.ViewData
	if err := json.Unmarshal(gotByName.Data, &byName); err != nil {
		t.Fatalf("decode view.get by name: %v", err)
	}
	if byName.ViewID != first.ViewID || byName.Name != "novels" {
		t.Fatalf("view.get by name = %+v", byName)
	}

	// Saving the same name writes a successor revision.
	updated := handleViewExport(dispatcher, mustEnvelope(t, command.OpViewSave, map[string]any{
		"name": "novels", "query": "tag:novel AND language:zh",
	}))
	if updated.Status != command.StatusSucceeded {
		t.Fatalf("view.save update = %q: %+v", updated.Status, updated.Reasons)
	}
	var second command.ViewData
	if err := json.Unmarshal(updated.Data, &second); err != nil {
		t.Fatalf("decode view.save update: %v", err)
	}
	if second.ViewID != first.ViewID || second.Revision != 2 || second.Query != "tag:novel AND language:zh" {
		t.Fatalf("updated view = %+v", second)
	}

	listed := handleViewExport(dispatcher, mustEnvelope(t, command.OpViewList, map[string]any{}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("view.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var views []command.ViewData
	if err := json.Unmarshal(listed.Data, &views); err != nil {
		t.Fatalf("decode view.list: %v", err)
	}
	if len(views) != 1 || views[0].ViewID != first.ViewID || views[0].Revision != 2 {
		t.Fatalf("view.list = %+v", views)
	}
}

func TestViewEvaluateReturnsHitsAfterIngest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "annual-report.txt"), []byte("annual report content"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	h := newViewExportHarness(t, root)

	saved := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpViewSave, map[string]any{
		"name": "reports", "query": "annual",
	}))
	if saved.Status != command.StatusSucceeded {
		t.Fatalf("view.save = %q: %+v", saved.Status, saved.Reasons)
	}
	var view command.ViewData
	if err := json.Unmarshal(saved.Data, &view); err != nil {
		t.Fatalf("decode view.save: %v", err)
	}

	evaluated := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpViewEvaluate, map[string]any{
		"view_id": view.ViewID,
	}))
	if evaluated.Status != command.StatusSucceeded {
		t.Fatalf("view.evaluate = %q: %+v", evaluated.Status, evaluated.Reasons)
	}
	var data command.ViewEvaluateData
	if err := json.Unmarshal(evaluated.Data, &data); err != nil {
		t.Fatalf("decode view.evaluate: %v", err)
	}
	if len(data.Hits) != 1 || data.Hits[0].SubjectRef != h.subjectRef {
		t.Fatalf("evaluate hits = %+v, want %s", data.Hits, h.subjectRef)
	}
	if data.ViewID != view.ViewID || data.Query != "annual" {
		t.Fatalf("evaluate data = %+v", data)
	}
}

func TestExportPlanApplyVerifyEndToEnd(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.bin"), []byte("export exact bytes"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	h := newViewExportHarness(t, root)

	planned := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportPlan, map[string]any{
		"subjects": []string{h.subjectRef}, "output_name": h.subjectName,
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("export.plan = %q: %+v", planned.Status, planned.Reasons)
	}
	var manifest command.ExportManifestData
	if err := json.Unmarshal(planned.Data, &manifest); err != nil {
		t.Fatalf("decode export.plan: %v", err)
	}
	if manifest.ManifestID == "" || manifest.ManifestDigest == "" || manifest.SubjectCount != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if len(manifest.Items) != 1 {
		t.Fatalf("manifest items = %+v", manifest.Items)
	}
	frozen, err := exportItemsFromStrings(manifest.Items)
	if err != nil {
		t.Fatalf("decode manifest items: %v", err)
	}
	if len(frozen) != 1 || frozen[0].SubjectRef != h.entryID || frozen[0].OutputName != h.subjectName {
		t.Fatalf("manifest items = %+v, want frozen entry_id %s", frozen, h.entryID)
	}

	destination := filepath.Join(t.TempDir(), "out")
	applied := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportApply, map[string]any{
		"manifest_id": manifest.ManifestID, "manifest_digest": manifest.ManifestDigest,
		"destination": destination,
	}))
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("export.apply = %q: %+v", applied.Status, applied.Reasons)
	}
	var applyData command.ExportApplyVerifyData
	if err := json.Unmarshal(applied.Data, &applyData); err != nil {
		t.Fatalf("decode export.apply: %v", err)
	}
	if !applyData.Verified || applyData.Items != 1 || applyData.Destination != destination {
		t.Fatalf("apply data = %+v", applyData)
	}
	got, err := os.ReadFile(filepath.Join(destination, h.subjectName))
	if err != nil {
		t.Fatalf("read materialized: %v", err)
	}
	if string(got) != "export exact bytes" {
		t.Fatalf("materialized payload = %q", got)
	}

	verified := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportVerify, map[string]any{
		"manifest_id": manifest.ManifestID, "manifest_digest": manifest.ManifestDigest,
		"destination": destination,
	}))
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("export.verify = %q: %+v", verified.Status, verified.Reasons)
	}
	var verifyData command.ExportApplyVerifyData
	if err := json.Unmarshal(verified.Data, &verifyData); err != nil {
		t.Fatalf("decode export.verify: %v", err)
	}
	if !verifyData.Verified || verifyData.ManifestID != manifest.ManifestID || verifyData.Bytes <= 0 {
		t.Fatalf("verify data = %+v", verifyData)
	}
}

func TestFrozenExportManifestSurvivesRescanAndCurrentSubjectChange(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "payload.bin")
	original := []byte("original export bytes")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	h := newViewExportHarness(t, root)

	planned := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportPlan, map[string]any{
		"subjects": []string{h.subjectRef}, "output_name": h.subjectName,
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("export.plan = %q: %+v", planned.Status, planned.Reasons)
	}
	var manifest command.ExportManifestData
	if err := json.Unmarshal(planned.Data, &manifest); err != nil {
		t.Fatalf("decode export.plan: %v", err)
	}
	frozen, err := exportItemsFromStrings(manifest.Items)
	if err != nil || len(frozen) != 1 || frozen[0].SubjectRef != h.entryID || frozen[0].ContentID == "" || !frozen[0].Exact {
		t.Fatalf("frozen manifest = %+v, err=%v", frozen, err)
	}

	changed := []byte("new bytes at the same path")
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}
	if got := mustAppliedIngest(t, ctx, h.dispatcher, map[string]any{"root": root}); got.SnapshotRef == h.ingest.SnapshotRef {
		t.Fatal("rescan did not publish a new snapshot")
	}

	destination := filepath.Join(t.TempDir(), "out")
	applied := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportApply, map[string]any{
		"manifest_id": manifest.ManifestID, "manifest_digest": manifest.ManifestDigest,
		"destination": destination,
	}))
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("apply frozen manifest after rescan = %q: %+v", applied.Status, applied.Reasons)
	}
	got, err := os.ReadFile(filepath.Join(destination, h.subjectName))
	if err != nil {
		t.Fatalf("read frozen export: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("frozen export bytes = %q, want %q", got, original)
	}
}

func TestExportPlanRequiresViewOrSubjects(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.bin"), []byte("plan probe"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	h := newViewExportHarness(t, root)

	// No view, no subjects: fails closed before any catalog write.
	missing := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportPlan, map[string]any{}))
	if missing.Status != command.StatusFailed || !hasReasonCode(missing, ReasonCodeInvalidInput) {
		t.Fatalf("empty export.plan = %q: %+v", missing.Status, missing.Reasons)
	}

	// An explicit subject that does not exist fails closed.
	unknown := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportPlan, map[string]any{
		"subjects": []string{"nse_00000000000000000000000000000000"},
	}))
	if unknown.Status != command.StatusFailed || !hasReasonCode(unknown, ReasonCodeNotFound) {
		t.Fatalf("unknown subject export.plan = %q: %+v", unknown.Status, unknown.Reasons)
	}

	// Both view_id and subjects together fails closed as invalid input.
	both := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportPlan, map[string]any{
		"view_id": "view_00000000000000000000000000000000", "subjects": []string{h.subjectRef},
	}))
	if both.Status != command.StatusFailed || !hasReasonCode(both, ReasonCodeInvalidInput) {
		t.Fatalf("both view and subjects export.plan = %q: %+v", both.Status, both.Reasons)
	}
}

func TestExportVerifyAgainstWrongDestinationFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.bin"), []byte("verify target bytes"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	h := newViewExportHarness(t, root)

	planned := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportPlan, map[string]any{
		"subjects": []string{h.subjectRef}, "output_name": h.subjectName,
	}))
	var manifest command.ExportManifestData
	if err := json.Unmarshal(planned.Data, &manifest); err != nil {
		t.Fatalf("decode export.plan: %v", err)
	}

	destination := filepath.Join(t.TempDir(), "out")
	if applied := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportApply, map[string]any{
		"manifest_id": manifest.ManifestID, "manifest_digest": manifest.ManifestDigest,
		"destination": destination,
	})); applied.Status != command.StatusSucceeded {
		t.Fatalf("export.apply = %q: %+v", applied.Status, applied.Reasons)
	}

	// Verifying a completely different empty destination fails closed.
	wrongDest := filepath.Join(t.TempDir(), "elsewhere")
	verified := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportVerify, map[string]any{
		"manifest_id": manifest.ManifestID, "manifest_digest": manifest.ManifestDigest,
		"destination": wrongDest,
	}))
	if verified.Status != command.StatusFailed {
		t.Fatalf("wrong-destination verify = %q: %+v", verified.Status, verified.Reasons)
	}

	// Verifying with a wrong digest fails closed before any destination check.
	wrongDigest := handleViewExport(h.dispatcher, mustEnvelope(t, command.OpExportVerify, map[string]any{
		"manifest_id": manifest.ManifestID, "manifest_digest": "sha256:" + "00" + "00000000000000000000000000000000000000000000000000000000000",
		"destination": destination,
	}))
	if wrongDigest.Status != command.StatusFailed {
		t.Fatalf("wrong-digest verify = %q: %+v", wrongDigest.Status, wrongDigest.Reasons)
	}
}
