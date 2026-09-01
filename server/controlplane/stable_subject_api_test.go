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

func TestSubjectRefFollowsLatestWhileEntryIDRemainsSnapshotLocal(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "payload.txt")
	original := []byte("original snapshot bytes")
	updated := []byte("updated current bytes")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{Store: store, Repo: repo}))
	first := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": first.WorkspaceID, "root_id": first.RootID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("namespace.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var namespace command.NamespaceListData
	if err := json.Unmarshal(listed.Data, &namespace); err != nil {
		t.Fatal(err)
	}
	if len(namespace.Entries) != 1 {
		t.Fatalf("namespace entries = %+v", namespace.Entries)
	}
	oldEntry := namespace.Entries[0]
	if oldEntry.SubjectRef == "" || oldEntry.SubjectRef == oldEntry.ID {
		t.Fatalf("stable subject mapping = %+v", oldEntry)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	second := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	if second.SnapshotRef == first.SnapshotRef {
		t.Fatal("rescan did not publish a new snapshot")
	}

	readVia := func(input map[string]any) []byte {
		t.Helper()
		opened := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentOpen, input))
		if opened.Status != command.StatusSucceeded {
			t.Fatalf("content.open = %q: %+v", opened.Status, opened.Reasons)
		}
		var open command.ContentOpenData
		if err := json.Unmarshal(opened.Data, &open); err != nil {
			t.Fatal(err)
		}
		read := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentRead, map[string]any{
			"handle": open.Handle, "offset": 0, "length": 1 << 20,
		}))
		if read.Status != command.StatusSucceeded {
			t.Fatalf("content.read = %q: %+v", read.Status, read.Reasons)
		}
		var data command.ContentReadData
		if err := json.Unmarshal(read.Data, &data); err != nil {
			t.Fatal(err)
		}
		return data.Bytes
	}
	if got := readVia(map[string]any{"workspace_id": first.WorkspaceID, "subject_ref": oldEntry.SubjectRef}); string(got) != string(updated) {
		t.Fatalf("stable subject bytes = %q, want current %q", got, updated)
	}
	if got := readVia(map[string]any{"workspace_id": first.WorkspaceID, "entry_id": oldEntry.ID}); string(got) != string(original) {
		t.Fatalf("snapshot entry bytes = %q, want original %q", got, original)
	}
}

func TestStableSubjectAnnotationsSearchAndRecoverySurviveRescan(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "payload.txt")
	if err := os.WriteFile(path, []byte("before rescan"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	identity, anchor, err := exact.OpenSigningMaterial(filepath.Join(t.TempDir(), "recovery"), exact.DefaultPublicationDomain, true)
	if err != nil {
		t.Fatalf("open signing material: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: exact.DefaultPublicationDomain, RequireSignedPublication: true,
	}))

	first := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	firstListed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": first.WorkspaceID, "root_id": first.RootID,
	}))
	if firstListed.Status != command.StatusSucceeded {
		t.Fatalf("first namespace.list = %q: %+v", firstListed.Status, firstListed.Reasons)
	}
	var firstNamespace command.NamespaceListData
	if err := json.Unmarshal(firstListed.Data, &firstNamespace); err != nil {
		t.Fatalf("decode first namespace.list: %v", err)
	}
	if len(firstNamespace.Entries) != 1 {
		t.Fatalf("first namespace entries = %+v", firstNamespace.Entries)
	}
	oldEntry := firstNamespace.Entries[0]
	if oldEntry.SubjectRef == "" || oldEntry.SubjectRef == oldEntry.ID {
		t.Fatalf("first stable subject mapping = %+v", oldEntry)
	}

	for _, annotation := range []struct {
		kind string
		body string
	}{
		{kind: "TAG", body: "rescan-tag"},
		{kind: "NOTE", body: "rescan-note"},
	} {
		result := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
			"workspace_id": first.WorkspaceID,
			"subject_ref":  oldEntry.SubjectRef,
			"kind":         annotation.kind,
			"body":         annotation.body,
		}))
		if result.Status != command.StatusSucceeded {
			t.Fatalf("annotation %s = %q: %+v", annotation.kind, result.Status, result.Reasons)
		}
	}

	if err := os.WriteFile(path, []byte("after rescan"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	if second.SnapshotRef == first.SnapshotRef {
		t.Fatal("rescan did not publish a new snapshot")
	}
	secondListed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": second.WorkspaceID, "root_id": second.RootID,
	}))
	if secondListed.Status != command.StatusSucceeded {
		t.Fatalf("second namespace.list = %q: %+v", secondListed.Status, secondListed.Reasons)
	}
	var secondNamespace command.NamespaceListData
	if err := json.Unmarshal(secondListed.Data, &secondNamespace); err != nil {
		t.Fatalf("decode second namespace.list: %v", err)
	}
	if len(secondNamespace.Entries) != 1 {
		t.Fatalf("second namespace entries = %+v", secondNamespace.Entries)
	}
	newEntry := secondNamespace.Entries[0]
	if newEntry.ID == oldEntry.ID || newEntry.SubjectRef != oldEntry.SubjectRef {
		t.Fatalf("rescan identity mapping old=%+v new=%+v", oldEntry, newEntry)
	}
	if newEntry.ContentID == "" || newEntry.ContentID == oldEntry.ContentID {
		t.Fatalf("rescan content identity old=%q new=%q", oldEntry.ContentID, newEntry.ContentID)
	}

	listedAnnotations := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationList, map[string]any{
		"workspace_id": second.WorkspaceID, "subject_ref": newEntry.SubjectRef,
	}))
	if listedAnnotations.Status != command.StatusSucceeded {
		t.Fatalf("annotation.list after rescan = %q: %+v", listedAnnotations.Status, listedAnnotations.Reasons)
	}
	var annotationData command.AnnotationListData
	if err := json.Unmarshal(listedAnnotations.Data, &annotationData); err != nil {
		t.Fatalf("decode annotation.list after rescan: %v", err)
	}
	annotationBodies := make(map[string]bool, len(annotationData.Annotations))
	for _, annotation := range annotationData.Annotations {
		if annotation.SubjectRef != newEntry.SubjectRef {
			t.Fatalf("annotation subject = %q, want %q", annotation.SubjectRef, newEntry.SubjectRef)
		}
		annotationBodies[annotation.Body] = true
	}
	if len(annotationData.Annotations) != 2 || !annotationBodies["rescan-tag"] || !annotationBodies["rescan-note"] {
		t.Fatalf("annotations after rescan = %+v", annotationData.Annotations)
	}

	searched := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": second.WorkspaceID,
		"dimension":    search.DimensionLexical,
		"query":        "rescan-note",
	}))
	if searched.Status != command.StatusSucceeded {
		t.Fatalf("lexical search after rescan = %q: %+v", searched.Status, searched.Reasons)
	}
	var searchData command.SearchQueryData
	if err := json.Unmarshal(searched.Data, &searchData); err != nil {
		t.Fatalf("decode lexical search after rescan: %v", err)
	}
	if len(searchData.Hits) != 1 || searchData.Hits[0].SubjectRef != newEntry.SubjectRef || searchData.Hits[0].EntryID != newEntry.ID {
		t.Fatalf("lexical search identity after rescan = %+v, want subject %q entry %q", searchData.Hits, newEntry.SubjectRef, newEntry.ID)
	}

	artifact := filepath.Join(t.TempDir(), "recovery-reference.json")
	exported := dispatcher.Handle(ctx, mustEnvelope(t, command.OpRecoveryExport, map[string]any{
		"snapshot_ref": second.SnapshotRef, "destination": artifact,
	}))
	if exported.Status != command.StatusSucceeded {
		t.Fatalf("recovery.export after rescan = %q: %+v", exported.Status, exported.Reasons)
	}
	var exportData command.RecoveryExportData
	if err := json.Unmarshal(exported.Data, &exportData); err != nil {
		t.Fatalf("decode recovery.export after rescan: %v", err)
	}
	if exportData.Schema != exact.RecoveryReferenceSchemaV2 || exportData.SnapshotRef != second.SnapshotRef || exportData.Files != 1 {
		t.Fatalf("v2 recovery export = %+v", exportData)
	}
	anchorPath := filepath.Join(t.TempDir(), "trust-anchor.json")
	anchorExport := dispatcher.Handle(ctx, mustEnvelope(t, command.OpRecoveryAnchorExport, map[string]any{
		"destination": anchorPath,
	}))
	if anchorExport.Status != command.StatusSucceeded {
		t.Fatalf("recovery.anchor.export = %q: %+v", anchorExport.Status, anchorExport.Reasons)
	}
	imported := dispatcher.Handle(ctx, mustEnvelope(t, command.OpRecoveryImport, map[string]any{
		"artifact_path": exportData.ArtifactPath, "trust_anchor_path": anchorPath,
	}))
	if imported.Status != command.StatusSucceeded {
		t.Fatalf("recovery.import v2 reference = %q: %+v", imported.Status, imported.Reasons)
	}
	var importData command.RecoveryImportData
	if err := json.Unmarshal(imported.Data, &importData); err != nil {
		t.Fatalf("decode recovery.import v2 reference: %v", err)
	}
	if importData.Schema != exact.RecoveryReferenceSchemaV2 || importData.SnapshotRef != second.SnapshotRef || importData.FactHealth != exact.RecoveryFactHealthComplete {
		t.Fatalf("recovery.import v2 data = %+v", importData)
	}
	reference, err := exact.LoadRecoveryReference(exportData.ArtifactPath)
	if err != nil {
		t.Fatalf("load v2 recovery reference: %v", err)
	}
	if reference.SnapshotRef != second.SnapshotRef || reference.FactHealth != exact.RecoveryFactHealthComplete || len(reference.PortableFactClosures) == 0 {
		t.Fatalf("v2 recovery reference closure = %+v", reference)
	}
	if err := reference.ValidateAgainstRepository(ctx, repo, anchor); err != nil {
		t.Fatalf("verify v2 recovery reference: %v", err)
	}

	verified := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": second.SnapshotRef, "mode": command.VerifyCleanRecovery,
	}))
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("clean recovery verify after rescan = %q: %+v", verified.Status, verified.Reasons)
	}
	var verifyData command.SnapshotVerifyData
	if err := json.Unmarshal(verified.Data, &verifyData); err != nil {
		t.Fatalf("decode clean recovery verify: %v", err)
	}
	if !verifyData.OK || verifyData.SnapshotRef != second.SnapshotRef || verifyData.CatalogUsed {
		t.Fatalf("clean recovery verify = %+v", verifyData)
	}
}
