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
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
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

	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

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
	if searchData.Dimension != search.DimensionLexical || searchData.Provider != search.ProviderLexicalFTS5 {
		t.Fatalf("search provenance = %+v", searchData)
	}
	if searchData.GenerationID == "" || searchData.ScoreSemantics != search.ScoreLexicalRank {
		t.Fatalf("search generation/score = %+v", searchData)
	}

	taggedHits := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "reviewed",
	}))
	if taggedHits.Status != command.StatusSucceeded {
		t.Fatalf("search tag = %q: %+v", taggedHits.Status, taggedHits.Reasons)
	}

	generation, err := store.LatestIndexGeneration(ctx, ingestData.WorkspaceID, search.DimensionLexical)
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
	mustAppliedRestore(t, ctx, dispatcher, ingestData.WorkspaceID, ingestData.SnapshotRef, dest)
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

func TestContentListProjectsPublishedWorkspaceWithoutDirectoryNavigation(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "docs", "report.txt"), []byte("report"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ingest := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentList, map[string]any{
		"workspace_id": ingest.WorkspaceID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("content.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var data command.ContentListData
	if err := json.Unmarshal(listed.Data, &data); err != nil {
		t.Fatalf("decode content.list: %v", err)
	}
	if data.WorkspaceID != ingest.WorkspaceID || data.RootID != ingest.RootID || len(data.Items) != 1 {
		t.Fatalf("content.list data = %+v", data)
	}
	item := data.Items[0]
	if item.SubjectRef == "" || item.Name != "report.txt" || item.Path != "docs/report.txt" ||
		item.EntryType != "REGULAR_FILE" || item.ContentID == "" || item.LogicalSize == nil || *item.LogicalSize != 6 {
		t.Fatalf("content item = %+v", item)
	}

	// A valid subject/root from another workspace cannot be used to widen this
	// projection because the operation accepts and authorizes workspace scope.
	other, err := sqlite.NewStableID(sqlite.IDPrefixWorkspace)
	if err != nil {
		t.Fatalf("new workspace id: %v", err)
	}
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertWorkspace(ctx, &sqlite.Workspace{ID: other, Name: "other"})
	}); err != nil {
		t.Fatalf("insert other workspace: %v", err)
	}
	cross := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentList, map[string]any{
		"workspace_id": other,
	}))
	if cross.Status == command.StatusSucceeded {
		t.Fatalf("cross-workspace content.list unexpectedly succeeded: %+v", cross)
	}
}

func TestContentListMergesLatestPublicationPerSource(t *testing.T) {
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

	firstRoot := filepath.Join(t.TempDir(), "photos")
	if err := os.Mkdir(firstRoot, 0o755); err != nil {
		t.Fatalf("mkdir first source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(firstRoot, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write first source: %v", err)
	}
	first := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": firstRoot})

	secondRoot := filepath.Join(t.TempDir(), "photos")
	if err := os.Mkdir(secondRoot, 0o755); err != nil {
		t.Fatalf("mkdir second source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secondRoot, "second.txt"), []byte("second"), 0o600); err != nil {
		t.Fatalf("write second source: %v", err)
	}
	second := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": secondRoot})
	if first.WorkspaceID != second.WorkspaceID {
		t.Fatalf("sources did not share the default workspace: %q vs %q", first.WorkspaceID, second.WorkspaceID)
	}

	if err := os.Remove(filepath.Join(firstRoot, "old.txt")); err != nil {
		t.Fatalf("remove superseded source file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(firstRoot, "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatalf("write latest source file: %v", err)
	}
	latestFirst := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": firstRoot})
	if latestFirst.WorkspaceID != first.WorkspaceID {
		t.Fatalf("latest source changed workspace: %q vs %q", latestFirst.WorkspaceID, first.WorkspaceID)
	}

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentList, map[string]any{
		"workspace_id": first.WorkspaceID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("content.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var data command.ContentListData
	if err := json.Unmarshal(listed.Data, &data); err != nil {
		t.Fatalf("decode content.list: %v", err)
	}
	if data.RootID != "" {
		t.Fatalf("multi-source content.list exposed one root %q", data.RootID)
	}
	if len(data.Roots) != 2 {
		t.Fatalf("multi-source content.list roots = %+v, want two browse choices", data.Roots)
	}
	wantSourcePaths := map[string]string{
		latestFirst.RootID: firstRoot,
		second.RootID:      secondRoot,
	}
	for _, root := range data.Roots {
		if root.RootID == "" || root.Name == "" {
			t.Fatalf("multi-source content.list has incomplete browse choice: %+v", root)
		}
		if root.Name != "photos" || root.SourcePath != wantSourcePaths[root.RootID] {
			t.Fatalf("multi-source content.list source projection = %+v, want source path %q", root, wantSourcePaths[root.RootID])
		}
	}
	if len(data.Items) != 2 {
		t.Fatalf("content.list items = %+v, want latest entries from two sources", data.Items)
	}
	seen := make(map[string]command.ContentItemData, len(data.Items))
	for _, item := range data.Items {
		seen[item.Name] = item
	}
	if _, ok := seen["old.txt"]; ok {
		t.Fatalf("content.list retained superseded source entry: %+v", data.Items)
	}
	for _, name := range []string{"new.txt", "second.txt"} {
		item, ok := seen[name]
		if !ok || item.SubjectRef == "" || item.EntryType != "REGULAR_FILE" || item.ContentID == "" || item.LogicalSize == nil {
			t.Fatalf("content.list missing valid latest item %q: %+v", name, data.Items)
		}
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

func TestAnnotationSubjectMustBelongToWorkspace(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))
	first := testutil.SeedNamespace(t, store)
	second := testutil.SeedNamespace(t, store)

	missingSubject, err := sqlite.NewStableID(sqlite.IDPrefixNamespaceEntry)
	if err != nil {
		t.Fatalf("new missing subject id: %v", err)
	}
	for name, subject := range map[string]string{
		"missing subject":         missingSubject,
		"cross-workspace subject": second.FileEntryID,
	} {
		result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
			"workspace_id": first.WorkspaceID,
			"subject_ref":  subject,
			"kind":         "NOTE",
			"body":         name,
		}))
		if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeNotFound) {
			t.Fatalf("%s result = %q reasons=%+v, want not_found", name, result.Status, result.Reasons)
		}
		if len(result.Reasons) == 0 || result.Reasons[0].Message != "subject not found" {
			t.Fatalf("%s reasons = %+v, want subject not found", name, result.Reasons)
		}
	}

	for _, annotation := range []map[string]any{
		{"kind": "TAG", "body": "topic:one"},
		{"kind": "TAG", "body": "media:text"},
		{"kind": "NOTE", "body": "durable context"},
	} {
		result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
			"workspace_id": first.WorkspaceID,
			"subject_ref":  first.FileEntryID,
			"kind":         annotation["kind"],
			"body":         annotation["body"],
		}))
		if result.Status != command.StatusSucceeded {
			t.Fatalf("valid %v result = %q reasons=%+v", annotation, result.Status, result.Reasons)
		}
	}
	annotations := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationList, map[string]any{
		"workspace_id": first.WorkspaceID,
		"subject_ref":  first.FileEntryID,
	}))
	if annotations.Status != command.StatusSucceeded {
		t.Fatalf("annotation.list = %q reasons=%+v", annotations.Status, annotations.Reasons)
	}
	var data command.AnnotationListData
	if err := json.Unmarshal(annotations.Data, &data); err != nil {
		t.Fatalf("decode annotations: %v", err)
	}
	if len(data.Annotations) != 3 {
		t.Fatalf("annotations = %+v, want 3 valid records", data.Annotations)
	}

	var note command.AnnotationData
	for _, annotation := range data.Annotations {
		if annotation.Kind == "NOTE" {
			note = annotation
			break
		}
	}
	wrongSubject := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
		"workspace_id":      first.WorkspaceID,
		"subject_ref":       first.DirEntryID,
		"kind":              "NOTE",
		"body":              "must not move",
		"annotation_id":     note.ID,
		"expected_revision": note.Revision,
	}))
	if wrongSubject.Status != command.StatusFailed || !hasReasonCode(wrongSubject, ReasonCodeInvalidInput) {
		t.Fatalf("wrong-subject note update = %q reasons=%+v, want invalid_input", wrongSubject.Status, wrongSubject.Reasons)
	}
	unchanged, err := store.GetAnnotation(context.Background(), first.WorkspaceID, note.ID)
	if err != nil {
		t.Fatalf("get unchanged note: %v", err)
	}
	if unchanged.SubjectRef != first.FileEntryID || unchanged.Body != "durable context" || unchanged.Revision != 1 {
		t.Fatalf("note changed after wrong-subject update: %+v", unchanged)
	}
}

func TestAnnotationImportConflictPolicy(t *testing.T) {
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
	created := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
		"workspace_id": seed.WorkspaceID,
		"subject_ref":  seed.FileEntryID,
		"kind":         "NOTE",
		"body":         "local",
	}))
	if created.Status != command.StatusSucceeded {
		t.Fatalf("create = %q: %+v", created.Status, created.Reasons)
	}
	var local command.AnnotationUpsertData
	if err := json.Unmarshal(created.Data, &local); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fork := command.AnnotationExportData{
		Schema: command.AnnotationBundleSchema,
		Annotations: []command.AnnotationData{{
			ID:          local.Annotation.ID,
			WorkspaceID: seed.WorkspaceID,
			SubjectRef:  seed.FileEntryID,
			Kind:        "NOTE",
			Body:        "imported",
			Revision:    9,
		}},
	}

	fail := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationImport, fork))
	if fail.Status != command.StatusFailed || !hasReasonCode(fail, ReasonCodeConflict) {
		t.Fatalf("default import = %q: %+v", fail.Status, fail.Reasons)
	}
	still, err := store.GetAnnotation(context.Background(), seed.WorkspaceID, local.Annotation.ID)
	if err != nil || still.Body != "local" || still.Revision != 1 {
		t.Fatalf("fail policy mutated local: %+v %v", still, err)
	}

	fork.Conflict = command.AnnotationConflictKeepLocal
	keep := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationImport, fork))
	if keep.Status != command.StatusSucceeded {
		t.Fatalf("keep-local = %q: %+v", keep.Status, keep.Reasons)
	}
	kept, err := store.GetAnnotation(context.Background(), seed.WorkspaceID, local.Annotation.ID)
	if err != nil || kept.Body != "local" || kept.Revision != 1 {
		t.Fatalf("keep-local changed local: %+v %v", kept, err)
	}

	fork.Conflict = command.AnnotationConflictKeepImported
	take := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationImport, fork))
	if take.Status != command.StatusSucceeded {
		t.Fatalf("keep-imported = %q: %+v", take.Status, take.Reasons)
	}
	updated, err := store.GetAnnotation(context.Background(), seed.WorkspaceID, local.Annotation.ID)
	if err != nil || updated.Body != "imported" || updated.Revision != 2 {
		t.Fatalf("keep-imported = %+v %v", updated, err)
	}
}

func TestAnnotationImportRejectsOrphanSubjectsBeforeMutation(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))
	first := testutil.SeedNamespace(t, store)
	second := testutil.SeedNamespace(t, store)
	validID, err := sqlite.NewStableID(sqlite.IDPrefixAnnotation)
	if err != nil {
		t.Fatalf("new valid annotation id: %v", err)
	}
	invalidID, err := sqlite.NewStableID(sqlite.IDPrefixAnnotation)
	if err != nil {
		t.Fatalf("new invalid annotation id: %v", err)
	}
	missingSubject, err := sqlite.NewStableID(sqlite.IDPrefixNamespaceEntry)
	if err != nil {
		t.Fatalf("new missing subject id: %v", err)
	}

	for name, subject := range map[string]string{
		"missing subject":         missingSubject,
		"cross-workspace subject": second.FileEntryID,
	} {
		bundle := command.AnnotationExportData{
			Schema: command.AnnotationBundleSchema,
			Annotations: []command.AnnotationData{
				{ID: validID, WorkspaceID: first.WorkspaceID, SubjectRef: first.FileEntryID, Kind: "TAG", Body: "topic:valid", Revision: 1},
				{ID: invalidID, WorkspaceID: first.WorkspaceID, SubjectRef: subject, Kind: "NOTE", Body: name, Revision: 1},
			},
		}
		result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationImport, bundle))
		if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeInvalidInput) {
			t.Fatalf("%s import = %q reasons=%+v, want invalid_input", name, result.Status, result.Reasons)
		}
		if _, err := store.GetAnnotation(context.Background(), first.WorkspaceID, validID); !containsNotFound(err) {
			t.Fatalf("%s import partially created valid prefix: %v", name, err)
		}
		if _, err := store.GetAnnotation(context.Background(), first.WorkspaceID, invalidID); !containsNotFound(err) {
			t.Fatalf("%s import created invalid annotation: %v", name, err)
		}
	}

	for _, test := range []struct {
		name                string
		kind                string
		body                string
		bodyDigest          string
		revision            int64
		predecessorRevision int64
	}{
		{name: "invalid kind", kind: "LABEL", body: "invalid", revision: 1},
		{name: "empty body", kind: "NOTE", body: "   ", revision: 1},
		{name: "body digest mismatch", kind: "NOTE", body: "invalid digest", bodyDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000", revision: 1},
		{name: "zero revision", kind: "NOTE", body: "invalid revision", revision: 0},
		{name: "negative predecessor", kind: "NOTE", body: "invalid predecessor", revision: 1, predecessorRevision: -1},
		{name: "non-earlier predecessor", kind: "NOTE", body: "invalid predecessor", revision: 2, predecessorRevision: 2},
	} {
		bundle := command.AnnotationExportData{
			Schema: command.AnnotationBundleSchema,
			Annotations: []command.AnnotationData{
				{ID: validID, WorkspaceID: first.WorkspaceID, SubjectRef: first.FileEntryID, Kind: "TAG", Body: "topic:valid", Revision: 1},
				{ID: invalidID, WorkspaceID: first.WorkspaceID, SubjectRef: first.FileEntryID, Kind: test.kind, Body: test.body, BodyDigest: test.bodyDigest, Revision: test.revision, PredecessorRevision: test.predecessorRevision},
			},
		}
		result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationImport, bundle))
		if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeInvalidInput) {
			t.Fatalf("%s import = %q reasons=%+v, want invalid_input", test.name, result.Status, result.Reasons)
		}
		if _, err := store.GetAnnotation(context.Background(), first.WorkspaceID, validID); !containsNotFound(err) {
			t.Fatalf("%s import partially created valid prefix: %v", test.name, err)
		}
		if _, err := store.GetAnnotation(context.Background(), first.WorkspaceID, invalidID); !containsNotFound(err) {
			t.Fatalf("%s import created invalid annotation: %v", test.name, err)
		}
	}

	duplicate := command.AnnotationExportData{
		Schema: command.AnnotationBundleSchema,
		Annotations: []command.AnnotationData{
			{ID: validID, WorkspaceID: first.WorkspaceID, SubjectRef: first.FileEntryID, Kind: "TAG", Body: "topic:valid", Revision: 1},
			{ID: validID, WorkspaceID: first.WorkspaceID, SubjectRef: first.FileEntryID, Kind: "TAG", Body: "topic:valid", Revision: 1},
		},
	}
	duplicateResult := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationImport, duplicate))
	if duplicateResult.Status != command.StatusFailed || !hasReasonCode(duplicateResult, ReasonCodeInvalidInput) {
		t.Fatalf("duplicate annotation import = %q reasons=%+v, want invalid_input", duplicateResult.Status, duplicateResult.Reasons)
	}
	if _, err := store.GetAnnotation(context.Background(), first.WorkspaceID, validID); !containsNotFound(err) {
		t.Fatalf("duplicate annotation import created a record: %v", err)
	}

	created := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
		"workspace_id": first.WorkspaceID,
		"subject_ref":  first.FileEntryID,
		"kind":         "NOTE",
		"body":         "original subject",
	}))
	if created.Status != command.StatusSucceeded {
		t.Fatalf("create note = %q reasons=%+v", created.Status, created.Reasons)
	}
	var createdData command.AnnotationUpsertData
	if err := json.Unmarshal(created.Data, &createdData); err != nil {
		t.Fatalf("decode created note: %v", err)
	}
	conflicting := command.AnnotationExportData{
		Schema: command.AnnotationBundleSchema,
		Annotations: []command.AnnotationData{
			{ID: validID, WorkspaceID: first.WorkspaceID, SubjectRef: first.FileEntryID, Kind: "TAG", Body: "topic:valid", Revision: 1},
			{ID: createdData.Annotation.ID, WorkspaceID: first.WorkspaceID, SubjectRef: first.FileEntryID, Kind: "NOTE", Body: "conflicting body", Revision: 9},
		},
	}
	conflict := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationImport, conflicting))
	if conflict.Status != command.StatusFailed || !hasReasonCode(conflict, ReasonCodeConflict) {
		t.Fatalf("conflicting suffix import = %q reasons=%+v, want conflict", conflict.Status, conflict.Reasons)
	}
	if _, err := store.GetAnnotation(context.Background(), first.WorkspaceID, validID); !containsNotFound(err) {
		t.Fatalf("conflicting suffix import partially created valid prefix: %v", err)
	}
	unchangedAfterConflict, err := store.GetAnnotation(context.Background(), first.WorkspaceID, createdData.Annotation.ID)
	if err != nil || unchangedAfterConflict.Body != "original subject" || unchangedAfterConflict.Revision != 1 {
		t.Fatalf("conflicting suffix changed local annotation: %+v %v", unchangedAfterConflict, err)
	}
	rebind := command.AnnotationExportData{
		Schema: command.AnnotationBundleSchema,
		Annotations: []command.AnnotationData{{
			ID: createdData.Annotation.ID, WorkspaceID: first.WorkspaceID,
			SubjectRef: first.DirEntryID, Kind: "NOTE", Body: "different subject", Revision: 2,
		}},
	}
	result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAnnotationImport, rebind))
	if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeInvalidInput) {
		t.Fatalf("subject-rebinding import = %q reasons=%+v, want invalid_input", result.Status, result.Reasons)
	}
	unchanged, err := store.GetAnnotation(context.Background(), first.WorkspaceID, createdData.Annotation.ID)
	if err != nil {
		t.Fatalf("get unchanged imported note: %v", err)
	}
	if unchanged.SubjectRef != first.FileEntryID || unchanged.Body != "original subject" || unchanged.Revision != 1 {
		t.Fatalf("import rebound note: %+v", unchanged)
	}
}

func TestSearchQueryDimensions(t *testing.T) {
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

	caps := dispatcher.Handle(ctx, mustEnvelope(t, command.OpCapabilityList, map[string]any{}))
	if caps.Status != command.StatusSucceeded {
		t.Fatalf("capability.list = %q: %+v", caps.Status, caps.Reasons)
	}
	var capData command.CapabilityListData
	if err := json.Unmarshal(caps.Data, &capData); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	states := map[string]string{}
	for _, capability := range capData.Capabilities {
		states[capability.Kind+":"+capability.ID] = capability.State
	}
	if states["index-dimension:"+search.DimensionLexical] != command.CapabilityAvailable {
		t.Fatalf("lexical dimension = %q, want AVAILABLE", states["index-dimension:"+search.DimensionLexical])
	}
	if states["index-dimension:"+search.DimensionAcoustic] != command.CapabilityUnavailable {
		t.Fatalf("acoustic dimension = %q, want UNAVAILABLE without fixture opt-in", states["index-dimension:"+search.DimensionAcoustic])
	}
	if states["index-dimension:"+search.DimensionSemantic] != command.CapabilityUnavailable {
		t.Fatalf("semantic dimension = %q, want UNAVAILABLE without a real provider", states["index-dimension:"+search.DimensionSemantic])
	}
	if states["index-dimension:"+search.DimensionGraph] != command.CapabilityAvailable {
		t.Fatalf("graph dimension = %q, want AVAILABLE", states["index-dimension:"+search.DimensionGraph])
	}
	if states["query-broker:"+search.ProviderBrokerFuse] != command.CapabilityAvailable {
		t.Fatalf("fuse broker = %q, want AVAILABLE", states["query-broker:"+search.ProviderBrokerFuse])
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "quarterly-report.txt"), []byte("quarterly experiment report"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
	}))
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

	unknown := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "quarterly",
		"dimension":    "not-a-dimension",
	}))
	if unknown.Status != command.StatusFailed || !hasReasonCode(unknown, ReasonCodeInvalidInput) {
		t.Fatalf("unknown dimension = %q: %+v", unknown.Status, unknown.Reasons)
	}

	badAxis := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id":   ingestData.WorkspaceID,
		"query":          "quarterly",
		"construct_axes": []string{"lyrics"},
	}))
	if badAxis.Status != command.StatusFailed || !hasReasonCode(badAxis, ReasonCodeInvalidInput) {
		t.Fatalf("unknown axis = %q: %+v", badAxis.Status, badAxis.Reasons)
	}

	acoustic := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        map[string]any{"text": "quarterly", "dimension": search.DimensionAcoustic},
	}))
	if acoustic.Status != command.StatusDegraded || !hasReasonCode(acoustic, ReasonCodeUnavailable) {
		t.Fatalf("acoustic search = %q reasons=%+v", acoustic.Status, acoustic.Reasons)
	}
	var acousticData command.SearchQueryData
	if err := json.Unmarshal(acoustic.Data, &acousticData); err != nil {
		t.Fatalf("decode acoustic: %v", err)
	}
	if acousticData.Dimension != search.DimensionAcoustic || len(acousticData.Hits) != 0 {
		t.Fatalf("acoustic payload = %+v", acousticData)
	}

	tagsOnly := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id":   ingestData.WorkspaceID,
		"query":          "reviewed",
		"construct_axes": []string{"tags"},
	}))
	if tagsOnly.Status != command.StatusSucceeded {
		t.Fatalf("tags-only search = %q: %+v", tagsOnly.Status, tagsOnly.Reasons)
	}
	var tagsData command.SearchQueryData
	if err := json.Unmarshal(tagsOnly.Data, &tagsData); err != nil {
		t.Fatalf("decode tags-only: %v", err)
	}
	if tagsData.Dimension != search.DimensionLexical || len(tagsData.Hits) != 1 || tagsData.Hits[0].SubjectRef != fileID {
		t.Fatalf("tags-only hits = %+v", tagsData)
	}
	if len(tagsData.ConstructAxes) != 1 || tagsData.ConstructAxes[0] != search.AxisTags {
		t.Fatalf("tags-only axes = %v", tagsData.ConstructAxes)
	}

	extractedOnly := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id":   ingestData.WorkspaceID,
		"query":          "reviewed",
		"construct_axes": []string{"extracted"},
	}))
	if extractedOnly.Status != command.StatusSucceeded {
		t.Fatalf("extracted-only search = %q: %+v", extractedOnly.Status, extractedOnly.Reasons)
	}
	var extractedData command.SearchQueryData
	if err := json.Unmarshal(extractedOnly.Data, &extractedData); err != nil {
		t.Fatalf("decode extracted-only: %v", err)
	}
	if len(extractedData.Hits) != 0 {
		t.Fatalf("extracted-only reviewed hits = %+v", extractedData.Hits)
	}

	verified := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("verify after dimension queries = %q: %+v", verified.Status, verified.Reasons)
	}
}
