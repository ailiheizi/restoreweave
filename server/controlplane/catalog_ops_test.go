package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestIndexLossDegradesSearchOnly(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := []byte("quarterly experiment report")
	if err := os.WriteFile(filepath.Join(root, "docs", "quarterly-report.txt"), payload, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ingested := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": root}))
	if ingested.Status != command.StatusSucceeded {
		t.Fatalf("ingest = %q: %+v", ingested.Status, ingested.Reasons)
	}
	var ingestData command.PlanIngestData
	if err := json.Unmarshal(ingested.Data, &ingestData); err != nil {
		t.Fatalf("decode ingest: %v", err)
	}

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("namespace.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var listData command.NamespaceListData
	if err := json.Unmarshal(listed.Data, &listData); err != nil {
		t.Fatalf("decode namespace list: %v", err)
	}
	var docsID string
	for _, entry := range listData.Entries {
		if entry.DisplayName == "docs" {
			docsID = entry.ID
		}
	}
	if docsID == "" {
		t.Fatalf("docs directory missing: %+v", listData.Entries)
	}
	children := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
		"parent_id":    docsID,
	}))
	var childrenData command.NamespaceListData
	if err := json.Unmarshal(children.Data, &childrenData); err != nil {
		t.Fatalf("decode children: %v", err)
	}
	var fileID string
	for _, entry := range childrenData.Entries {
		if entry.DisplayName == "quarterly-report.txt" {
			fileID = entry.ID
		}
	}
	if fileID == "" {
		t.Fatalf("quarterly-report.txt missing: %+v", childrenData.Entries)
	}

	tagged := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"subject_ref":  fileID,
		"kind":         "TAG",
		"body":         "reviewed",
	}))
	if tagged.Status != command.StatusSucceeded {
		t.Fatalf("tag = %q: %+v", tagged.Status, tagged.Reasons)
	}

	found := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "quarterly",
	}))
	if found.Status != command.StatusSucceeded {
		t.Fatalf("search quarterly = %q: %+v", found.Status, found.Reasons)
	}
	var searchData command.SearchQueryData
	if err := json.Unmarshal(found.Data, &searchData); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(searchData.Hits) != 1 || searchData.Hits[0].SubjectRef != fileID {
		t.Fatalf("search hits = %+v", searchData.Hits)
	}

	taggedHits := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "reviewed",
	}))
	if taggedHits.Status != command.StatusSucceeded {
		t.Fatalf("search tag = %q: %+v", taggedHits.Status, taggedHits.Reasons)
	}

	generation, err := store.LatestIndexGeneration(ctx, ingestData.WorkspaceID)
	if err != nil {
		t.Fatalf("latest generation: %v", err)
	}
	if err := os.Remove(generation.DBPath); err != nil {
		t.Fatalf("delete index file: %v", err)
	}

	degraded := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "quarterly",
	}))
	if degraded.Status != command.StatusDegraded || !hasReasonCode(degraded, ReasonCodeUnavailable) {
		t.Fatalf("search after index loss = %q reasons=%+v", degraded.Status, degraded.Reasons)
	}

	stillListed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
		"parent_id":    docsID,
	}))
	if stillListed.Status != command.StatusSucceeded {
		t.Fatalf("namespace after index loss = %q: %+v", stillListed.Status, stillListed.Reasons)
	}
	annotations := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"subject_ref":  fileID,
	}))
	if annotations.Status != command.StatusSucceeded {
		t.Fatalf("annotations after index loss = %q: %+v", annotations.Status, annotations.Reasons)
	}
	var annotationData command.AnnotationListData
	if err := json.Unmarshal(annotations.Data, &annotationData); err != nil {
		t.Fatalf("decode annotations: %v", err)
	}
	if len(annotationData.Annotations) != 1 || annotationData.Annotations[0].Body != "reviewed" {
		t.Fatalf("annotations = %+v", annotationData.Annotations)
	}

	opened := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentOpen, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"entry_id":     fileID,
	}))
	if opened.Status != command.StatusSucceeded {
		t.Fatalf("content.open = %q: %+v", opened.Status, opened.Reasons)
	}
	var openData command.ContentOpenData
	if err := json.Unmarshal(opened.Data, &openData); err != nil {
		t.Fatalf("decode open: %v", err)
	}
	read := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentRead, map[string]any{
		"handle": openData.Handle,
		"offset": 0,
		"length": int64(len(payload)),
	}))
	if read.Status != command.StatusSucceeded {
		t.Fatalf("content.read = %q: %+v", read.Status, read.Reasons)
	}
	var readData command.ContentReadData
	if err := json.Unmarshal(read.Data, &readData); err != nil {
		t.Fatalf("decode read: %v", err)
	}
	if string(readData.Bytes) != string(payload) {
		t.Fatalf("read bytes = %q", readData.Bytes)
	}
	closed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentClose, map[string]any{"handle": openData.Handle}))
	if closed.Status != command.StatusSucceeded {
		t.Fatalf("content.close = %q: %+v", closed.Status, closed.Reasons)
	}

	verified := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("verify after index loss = %q: %+v", verified.Status, verified.Reasons)
	}
	dest := filepath.Join(t.TempDir(), "out")
	restored := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
		"destination":  dest,
	}))
	if restored.Status != command.StatusSucceeded {
		t.Fatalf("restore after index loss = %q: %+v", restored.Status, restored.Reasons)
	}
	got, err := os.ReadFile(filepath.Join(dest, "docs", "quarterly-report.txt"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("restored payload = %q", got)
	}

	indexer := &search.Indexer{Store: store, Engine: &search.Engine{Dir: filepath.Join(repo.Root(), "indexes")}}
	rebuilt, err := indexer.Rebuild(ctx, ingestData.WorkspaceID, ingestData.SnapshotRef, ingestData.RootID)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt.ID == generation.ID {
		t.Fatal("rebuild reused the deleted generation id")
	}
	recovered := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "reviewed",
	}))
	if recovered.Status != command.StatusSucceeded {
		t.Fatalf("search after rebuild = %q: %+v", recovered.Status, recovered.Reasons)
	}
	var recoveredData command.SearchQueryData
	if err := json.Unmarshal(recovered.Data, &recoveredData); err != nil {
		t.Fatalf("decode recovered search: %v", err)
	}
	if len(recoveredData.Hits) != 1 || recoveredData.Hits[0].SubjectRef != fileID {
		t.Fatalf("recovered hits = %+v", recoveredData.Hits)
	}
}

func TestAnnotationRevisionConflict(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))
	seed := testutil.SeedNamespace(t, store)
	note := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"subject_ref":  seed.FileEntryID,
		"kind":         "NOTE",
		"body":         "first",
	}))
	if note.Status != command.StatusSucceeded {
		t.Fatalf("create note = %q: %+v", note.Status, note.Reasons)
	}
	var created command.AnnotationUpsertData
	if err := json.Unmarshal(note.Data, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	conflict := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
		"workspace_id":      seed.WorkspaceID,
		"subject_ref":       seed.FileEntryID,
		"kind":              "NOTE",
		"body":              "second",
		"annotation_id":     created.Annotation.ID,
		"expected_revision": created.Annotation.Revision + 1,
	}))
	if conflict.Status != command.StatusFailed || !hasReasonCode(conflict, ReasonCodeConflict) {
		t.Fatalf("stale note update = %q reasons=%+v", conflict.Status, conflict.Reasons)
	}
	got, err := store.GetAnnotation(context.Background(), seed.WorkspaceID, created.Annotation.ID)
	if err != nil {
		t.Fatalf("get annotation: %v", err)
	}
	if got.Body != "first" || got.Revision != 1 {
		t.Fatalf("annotation mutated on conflict: %+v", got)
	}
}
