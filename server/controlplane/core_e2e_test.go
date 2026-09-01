package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	rwconfig "github.com/ailiheizi/restoreweave/config"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// TestCoreWorkflowThroughDispatcher proves the ordinary first-run loop at the
// command boundary. It intentionally uses the development exact repository
// and the real lexical projection; semantic readiness is a separate capability
// and must not be faked by a fixture in this test.
func TestCoreWorkflowThroughDispatcher(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "restoreweave.toml")
	resolved, err := rwconfig.Init(configPath)
	if err != nil {
		t.Fatalf("initialize config: %v", err)
	}
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock",
		WithOperatorConfig(resolved), WithExact(&exact.Service{Store: store, Repo: repo}))

	configured := dispatcher.Handle(ctx, mustEnvelope(t, command.OpConfigGet, map[string]any{}))
	if configured.Status != command.StatusSucceeded {
		t.Fatalf("config.get = %q: %+v", configured.Status, configured.Reasons)
	}
	var configData command.ConfigData
	if err := json.Unmarshal(configured.Data, &configData); err != nil {
		t.Fatalf("decode config.get: %v", err)
	}
	if configData.ConfigPath != configPath || configData.ConfigDigest != resolved.Digest || configData.RestartRequired {
		t.Fatalf("config receipt = %+v", configData)
	}

	root := t.TempDir()
	payload := []byte("quarterly recovery note")
	paths := []string{"docs/report.txt", "inbox/report-copy.txt"}
	for _, relative := range paths {
		absolute := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatalf("create source directory: %v", err)
		}
		if err := os.WriteFile(absolute, payload, 0o600); err != nil {
			t.Fatalf("write source %s: %v", relative, err)
		}
	}

	planned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": root}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var ingestPlan command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &ingestPlan); err != nil {
		t.Fatalf("decode plan.ingest: %v", err)
	}
	applied := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestPlan.WorkspaceID,
		"plan_id":      ingestPlan.PlanID,
		"plan_digest":  ingestPlan.PlanDigest,
	}))
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("plan.apply = %q: %+v", applied.Status, applied.Reasons)
	}
	var ingest command.PlanApplyData
	if err := json.Unmarshal(applied.Data, &ingest); err != nil {
		t.Fatalf("decode plan.apply: %v", err)
	}
	if ingest.Files != len(paths) || ingest.LocalBytes != int64(len(payload))*2 ||
		ingest.NewBytes != int64(len(payload)) || ingest.NewPhysicalBytes != int64(len(payload)) ||
		!ingest.SavingsMeasured {
		t.Fatalf("dedup receipt = %+v", ingest)
	}

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentList, map[string]any{
		"workspace_id": ingest.WorkspaceID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("content.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var contentData command.ContentListData
	if err := json.Unmarshal(listed.Data, &contentData); err != nil {
		t.Fatalf("decode content.list: %v", err)
	}
	if len(contentData.Items) != len(paths) {
		t.Fatalf("content items = %+v", contentData.Items)
	}
	byPath := make(map[string]command.ContentItemData, len(contentData.Items))
	for _, item := range contentData.Items {
		byPath[item.Path] = item
	}
	first, firstOK := byPath[paths[0]]
	second, secondOK := byPath[paths[1]]
	if !firstOK || !secondOK || first.SubjectRef == "" || second.SubjectRef == "" ||
		first.SubjectRef == second.SubjectRef || first.ContentID == "" || first.ContentID != second.ContentID {
		t.Fatalf("content identity projection = %+v", contentData.Items)
	}

	for _, annotation := range []struct {
		kind string
		body string
	}{
		{kind: "TAG", body: "review:important"},
		{kind: "TAG", body: "quarterly"},
		{kind: "NOTE", body: "operator-noted"},
	} {
		result := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
			"workspace_id": ingest.WorkspaceID,
			"subject_ref":  first.SubjectRef,
			"kind":         annotation.kind,
			"body":         annotation.body,
		}))
		if result.Status != command.StatusSucceeded {
			t.Fatalf("annotation %s = %q: %+v", annotation.kind, result.Status, result.Reasons)
		}
	}
	annotations := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationList, map[string]any{
		"workspace_id": ingest.WorkspaceID,
		"subject_ref":  first.SubjectRef,
	}))
	if annotations.Status != command.StatusSucceeded {
		t.Fatalf("annotation.list = %q: %+v", annotations.Status, annotations.Reasons)
	}
	var annotationData command.AnnotationListData
	if err := json.Unmarshal(annotations.Data, &annotationData); err != nil {
		t.Fatalf("decode annotation.list: %v", err)
	}
	if len(annotationData.Annotations) != 3 {
		t.Fatalf("annotations = %+v", annotationData.Annotations)
	}

	searched := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingest.WorkspaceID,
		"dimension":    search.DimensionLexical,
		"query":        "operator-noted",
	}))
	if searched.Status != command.StatusSucceeded {
		t.Fatalf("search note = %q: %+v", searched.Status, searched.Reasons)
	}
	var searchData command.SearchQueryData
	if err := json.Unmarshal(searched.Data, &searchData); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(searchData.Hits) != 1 || searchData.Hits[0].SubjectRef != first.SubjectRef || searchData.GenerationID == "" {
		t.Fatalf("search result = %+v", searchData)
	}

	destination := filepath.Join(t.TempDir(), "restored")
	mustAppliedRestore(t, ctx, dispatcher, ingest.WorkspaceID, ingest.SnapshotRef, destination)
	for _, relative := range paths {
		got, err := os.ReadFile(filepath.Join(destination, relative))
		if err != nil {
			t.Fatalf("read restored %s: %v", relative, err)
		}
		if string(got) != string(payload) {
			t.Fatalf("restored %s = %q", relative, got)
		}
	}
}
