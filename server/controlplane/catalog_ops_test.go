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
