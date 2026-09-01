package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAnnotationsSurviveIndependentOfIndexPointers(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ":memory:")
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Annotation workspace"}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertWorkspace(ctx, &workspace)
	}); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	subject := testID(t, IDPrefixNamespaceEntry)
	tag := &Annotation{
		ID:          testID(t, IDPrefixAnnotation),
		WorkspaceID: workspace.ID,
		SubjectRef:  subject,
		Kind:        AnnotationTag,
		Body:        "reviewed",
		Revision:    1,
	}
	if err := store.CreateAnnotation(ctx, tag); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if err := store.CreateAnnotation(ctx, tag); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate tag id = %v, want conflict", err)
	}

	live, err := store.FindLiveTag(ctx, workspace.ID, subject, "reviewed")
	if err != nil {
		t.Fatalf("FindLiveTag: %v", err)
	}
	if live.Revision != 1 || live.Tombstoned {
		t.Fatalf("live tag = %+v", live)
	}

	if err := store.ReviseAnnotation(ctx, workspace.ID, tag.ID, 99, tag.Body, true, testEpoch); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale tombstone = %v, want conflict", err)
	}
	if err := store.ReviseAnnotation(ctx, workspace.ID, tag.ID, 1, tag.Body, true, testEpoch); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if _, err := store.FindLiveTag(ctx, workspace.ID, subject, "reviewed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tombstoned tag still live: %v", err)
	}

	note := &Annotation{
		ID:          testID(t, IDPrefixAnnotation),
		WorkspaceID: workspace.ID,
		SubjectRef:  subject,
		Kind:        AnnotationNote,
		Body:        "keep this note",
		Revision:    1,
	}
	if err := store.CreateAnnotation(ctx, note); err != nil {
		t.Fatalf("create note: %v", err)
	}
	listed, err := store.ListAnnotations(ctx, workspace.ID, subject, false)
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	if len(listed) != 1 || listed[0].Kind != AnnotationNote {
		t.Fatalf("live annotations = %+v", listed)
	}
	all, err := store.ListAnnotations(ctx, workspace.ID, subject, true)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all annotations = %+v", all)
	}
	revisions, err := store.ListAnnotationRevisions(ctx, workspace.ID, subject)
	if err != nil {
		t.Fatalf("list revisions: %v", err)
	}
	var tagRevisions []AnnotationRevision
	for _, revision := range revisions {
		if revision.AnnotationID == tag.ID {
			tagRevisions = append(tagRevisions, revision)
		}
	}
	if len(revisions) != 3 || len(tagRevisions) != 2 || tagRevisions[0].Revision != 1 ||
		tagRevisions[1].Revision != 2 || !tagRevisions[1].Tombstoned ||
		tagRevisions[1].PredecessorID != tagRevisions[0].ID || !tagRevisions[1].HistoryComplete {
		t.Fatalf("annotation revisions = %+v", revisions)
	}

	progress := &Annotation{
		ID:          testID(t, IDPrefixAnnotation),
		WorkspaceID: workspace.ID,
		SubjectRef:  subject,
		Kind:        AnnotationProgress,
		Body:        `{"position_ms":12000,"completed":false,"source":"opensubsonic"}`,
		Revision:    1,
	}
	if err := store.CreateAnnotation(ctx, progress); err != nil {
		t.Fatalf("create progress: %v", err)
	}
	liveProgress, err := store.FindLiveProgress(ctx, workspace.ID, subject)
	if err != nil {
		t.Fatalf("FindLiveProgress: %v", err)
	}
	if liveProgress.Kind != AnnotationProgress || liveProgress.Body != progress.Body {
		t.Fatalf("live progress = %+v", liveProgress)
	}

	generation := &IndexGeneration{
		ID:                    testID(t, IDPrefixIndexGeneration),
		WorkspaceID:           workspace.ID,
		SnapshotRef:           "snap:test",
		NamespaceRootID:       testID(t, IDPrefixNamespaceRoot),
		DBPath:                "/tmp/missing.sqlite",
		Dimension:             "semantic-embedding",
		ConfigDigest:          "sha256:config-a",
		ProviderProfileDigest: "sha256:provider-a",
		SemanticSpace:         "space-a",
	}
	if err := store.InsertIndexGeneration(ctx, generation); err != nil {
		t.Fatalf("insert generation: %v", err)
	}
	got, err := store.LatestIndexGeneration(ctx, workspace.ID, generation.Dimension)
	if err != nil {
		t.Fatalf("latest generation: %v", err)
	}
	if got.ID != generation.ID || got.Dimension != generation.Dimension ||
		got.ConfigDigest != generation.ConfigDigest ||
		got.ProviderProfileDigest != generation.ProviderProfileDigest ||
		got.SemanticSpace != generation.SemanticSpace {
		t.Fatalf("latest = %+v", got)
	}
}

func TestAnnotationRejectsMismatchedBodyDigest(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ":memory:")
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "digest mismatch"}
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertWorkspace(ctx, &workspace) }); err != nil {
		t.Fatal(err)
	}
	err := store.CreateAnnotation(ctx, &Annotation{
		ID: testID(t, IDPrefixAnnotation), WorkspaceID: workspace.ID,
		SubjectRef: testID(t, IDPrefixNamespaceEntry), Kind: AnnotationNote,
		Body: "retained body", BodyDigest: "sha256:" + strings.Repeat("0", 64), Revision: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "body digest") {
		t.Fatalf("mismatched digest error = %v", err)
	}
}

func TestIndexGenerationRootScopeIsAtomicAndExact(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ":memory:")
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "generation roots"}
	sourceA := Source{ID: testID(t, IDPrefixSource), WorkspaceID: workspace.ID, StableKey: "source:a", Kind: "LOCAL_ROOT", Locator: "/a", State: SourceActive}
	sourceB := Source{ID: testID(t, IDPrefixSource), WorkspaceID: workspace.ID, StableKey: "source:b", Kind: "LOCAL_ROOT", Locator: "/b", State: SourceActive}
	scanA := ScanGeneration{ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID, SourceID: sourceA.ID, Generation: 1, CaptureSetID: "capture:a", CaptureSetDigest: "sha256:capture-a"}
	scanB := ScanGeneration{ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID, SourceID: sourceB.ID, Generation: 1, CaptureSetID: "capture:b", CaptureSetDigest: "sha256:capture-b"}
	rootA := NamespaceRoot{ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID, SourceID: sourceA.ID, ScanGenerationID: scanA.ID, SnapshotRef: "snapshot:a", Name: "A", RootPathKey: []byte{}, FilesystemSemantics: "TEST", AuthorityDigest: "sha256:root-a"}
	rootB := NamespaceRoot{ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID, SourceID: sourceB.ID, ScanGenerationID: scanB.ID, SnapshotRef: "snapshot:b", Name: "B", RootPathKey: []byte{}, FilesystemSemantics: "TEST", AuthorityDigest: "sha256:root-b"}
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertWorkspace(ctx, &workspace); err != nil {
			return err
		}
		for _, record := range []*Source{&sourceA, &sourceB} {
			if err := tx.InsertSource(ctx, record); err != nil {
				return err
			}
		}
		for _, record := range []*ScanGeneration{&scanA, &scanB} {
			if err := tx.InsertScanGeneration(ctx, record); err != nil {
				return err
			}
		}
		for _, record := range []*NamespaceRoot{&rootA, &rootB} {
			if err := tx.InsertNamespaceRoot(ctx, record); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("insert root fixtures: %v", err)
	}

	generation := &IndexGeneration{ID: testID(t, IDPrefixIndexGeneration), WorkspaceID: workspace.ID, SnapshotRef: "snapshot:combined", NamespaceRootID: rootA.ID, DBPath: "/tmp/combined.sqlite", Dimension: "semantic-embedding"}
	if err := store.InsertIndexGenerationWithRoots(ctx, generation, []string{rootA.ID, rootB.ID}); err != nil {
		t.Fatalf("insert generation with roots: %v", err)
	}
	roots, err := store.ListIndexGenerationRoots(ctx, workspace.ID, generation.ID)
	rootSet := make(map[string]bool, len(roots))
	for _, root := range roots {
		rootSet[root.ID] = true
	}
	if err != nil || len(roots) != 2 || !rootSet[rootA.ID] || !rootSet[rootB.ID] {
		t.Fatalf("generation roots = %+v, err=%v", roots, err)
	}

	legacy := &IndexGeneration{ID: testID(t, IDPrefixIndexGeneration), WorkspaceID: workspace.ID, SnapshotRef: "snapshot:legacy", NamespaceRootID: testID(t, IDPrefixNamespaceRoot), DBPath: "/tmp/legacy.sqlite"}
	if err := store.InsertIndexGeneration(ctx, legacy); err != nil {
		t.Fatalf("legacy generation insert: %v", err)
	}
	legacyRoots, err := store.ListIndexGenerationRoots(ctx, workspace.ID, legacy.ID)
	if err != nil || len(legacyRoots) != 0 {
		t.Fatalf("legacy generation roots = %+v, err=%v", legacyRoots, err)
	}

	for name, badRoots := range map[string][]string{
		"duplicate":       {rootA.ID, rootA.ID},
		"unknown":         {rootA.ID, testID(t, IDPrefixNamespaceRoot)},
		"missing-primary": {rootB.ID},
	} {
		candidate := &IndexGeneration{ID: testID(t, IDPrefixIndexGeneration), WorkspaceID: workspace.ID, SnapshotRef: "snapshot:" + name, NamespaceRootID: rootA.ID, DBPath: "/tmp/" + name + ".sqlite"}
		if err := store.InsertIndexGenerationWithRoots(ctx, candidate, badRoots); err == nil {
			t.Fatalf("%s roots unexpectedly accepted", name)
		}
		if _, err := store.GetIndexGeneration(ctx, candidate.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s left generation after failed atomic insert: %v", name, err)
		}
	}

	otherWorkspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "other"}
	otherSource := Source{ID: testID(t, IDPrefixSource), WorkspaceID: otherWorkspace.ID, StableKey: "source:other", Kind: "LOCAL_ROOT", Locator: "/other", State: SourceActive}
	otherScan := ScanGeneration{ID: testID(t, IDPrefixScanGeneration), WorkspaceID: otherWorkspace.ID, SourceID: otherSource.ID, Generation: 1, CaptureSetID: "capture:other", CaptureSetDigest: "sha256:capture-other"}
	otherRoot := NamespaceRoot{ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: otherWorkspace.ID, SourceID: otherSource.ID, ScanGenerationID: otherScan.ID, SnapshotRef: "snapshot:other", Name: "Other", RootPathKey: []byte{}, FilesystemSemantics: "TEST", AuthorityDigest: "sha256:root-other"}
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertWorkspace(ctx, &otherWorkspace); err != nil {
			return err
		}
		if err := tx.InsertSource(ctx, &otherSource); err != nil {
			return err
		}
		if err := tx.InsertScanGeneration(ctx, &otherScan); err != nil {
			return err
		}
		return tx.InsertNamespaceRoot(ctx, &otherRoot)
	}); err != nil {
		t.Fatalf("insert other workspace: %v", err)
	}
	cross := &IndexGeneration{ID: testID(t, IDPrefixIndexGeneration), WorkspaceID: workspace.ID, SnapshotRef: "snapshot:cross", NamespaceRootID: rootA.ID, DBPath: "/tmp/cross.sqlite"}
	if err := store.InsertIndexGenerationWithRoots(ctx, cross, []string{otherRoot.ID}); err == nil {
		t.Fatal("cross-workspace root unexpectedly accepted")
	}
	if _, err := store.GetIndexGeneration(ctx, cross.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace failure left generation: %v", err)
	}
	if _, err := store.ListIndexGenerationRoots(ctx, workspace.ID, testID(t, IDPrefixIndexGeneration)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown generation roots err=%v, want ErrNotFound", err)
	}
}
