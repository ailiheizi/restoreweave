package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
)

func TestProcessorArtifactBindsBodyDigestAndLength(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	defer store.Close()
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "artifact validation"}
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertWorkspace(ctx, &workspace) }); err != nil {
		t.Fatal(err)
	}
	body := "portable artifact"
	sum := sha256.Sum256([]byte(body))
	base := ProcessorArtifact{
		ID: testID(t, IDPrefixArtifact), WorkspaceID: workspace.ID,
		SubjectRef: testID(t, IDPrefixNamespaceEntry), SnapshotRef: "snap:test",
		RouteDigest: "sha256:route", Stage: "EXTRACT", CapabilityID: "extract:test",
		SchemaRef: "schema:test", State: ArtifactAdmitted, AuthorityClass: "STAGED_ARTIFACT",
		LifecycleClass: "REBUILDABLE", MediaType: "text/plain", ByteLength: int64(len(body)),
		Digest: "sha256:" + hex.EncodeToString(sum[:]), Body: body,
		AttemptID: testID(t, IDPrefixAttempt), FenceToken: 1, ProducerDigest: "sha256:producer",
	}
	if err := store.InsertProcessorAttempt(ctx, &ProcessorAttempt{
		ID: base.AttemptID, WorkspaceID: base.WorkspaceID, SubjectRef: base.SubjectRef,
		SnapshotRef: base.SnapshotRef, RouteDigest: base.RouteDigest,
		Route: []byte(`{"kind":"PROCESSING","nodes":[]}`), Stage: base.Stage,
		CapabilityID: base.CapabilityID, Status: "SUCCEEDED", ReasonCode: "ADMITTED_ARTIFACT",
		Provenance: []byte(`{"test":true}`), FenceToken: base.FenceToken,
		ProcessorDigest: base.ProducerDigest,
	}); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	if err := store.InsertProcessorArtifact(ctx, &base); err != nil {
		t.Fatalf("insert valid artifact: %v", err)
	}
	for name, mutate := range map[string]func(*ProcessorArtifact){
		"length": func(record *ProcessorArtifact) { record.ByteLength++ },
		"digest": func(record *ProcessorArtifact) { record.Digest = "sha256:wrong" },
	} {
		t.Run(name, func(t *testing.T) {
			record := base
			record.ID = testID(t, IDPrefixArtifact)
			mutate(&record)
			if err := store.InsertProcessorArtifact(ctx, &record); err == nil {
				t.Fatal("invalid artifact unexpectedly inserted")
			}
		})
	}
}
