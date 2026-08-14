package sqlite

import (
	"context"
	"errors"
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
		ID:              testID(t, IDPrefixIndexGeneration),
		WorkspaceID:     workspace.ID,
		SnapshotRef:     "snap:test",
		NamespaceRootID: testID(t, IDPrefixNamespaceRoot),
		DBPath:          "/tmp/missing.sqlite",
	}
	if err := store.InsertIndexGeneration(ctx, generation); err != nil {
		t.Fatalf("insert generation: %v", err)
	}
	got, err := store.LatestIndexGeneration(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("latest generation: %v", err)
	}
	if got.ID != generation.ID {
		t.Fatalf("latest = %+v", got)
	}
}
