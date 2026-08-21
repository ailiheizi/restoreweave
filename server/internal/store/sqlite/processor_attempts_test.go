package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProcessorAttemptsAreAppendOnlyAndExportable(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, "file:processor-attempts-test?mode=memory&cache=shared")
	defer store.Close()
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "attempts"}
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertWorkspace(ctx, &workspace) }); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	attempt := ProcessorAttempt{
		ID: testID(t, IDPrefixAttempt), WorkspaceID: workspace.ID,
		SubjectRef: testID(t, IDPrefixNamespaceEntry), SnapshotRef: "snap-1",
		RouteDigest: "sha256:route", Route: json.RawMessage(`{"kind":"PROCESSING","nodes":[]}`),
		Stage: "EXTRACT", CapabilityID: "extract.text.v1", Status: "FAILED",
		ReasonCode: "PROCESSOR_STAGE_FAILED", Reason: "fixture failure",
		Provenance: json.RawMessage(`{"source_content_id":"sha256:content"}`),
		FenceToken: 1, ProcessorDigest: "sha256:processor",
		CreatedAt: testEpoch, FinishedAt: testEpoch.Add(time.Millisecond),
	}
	if err := store.InsertProcessorAttempt(ctx, &attempt); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	rows, err := store.ListProcessorAttempts(ctx, workspace.ID, "snap-1")
	if err != nil || len(rows) != 1 || rows[0].ReasonCode != attempt.ReasonCode {
		t.Fatalf("listed attempts = %+v, err=%v", rows, err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE processor_attempts SET reason = 'changed' WHERE attempt_id = ?`, attempt.ID); err == nil {
		t.Fatal("processor attempt update unexpectedly succeeded")
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM processor_attempts WHERE attempt_id = ?`, attempt.ID); err == nil {
		t.Fatal("processor attempt delete unexpectedly succeeded")
	}
	exported, err := store.ExportProcessorAttempts(ctx, workspace.ID, "snap-1")
	if err != nil {
		t.Fatalf("export attempts: %v", err)
	}
	if !strings.Contains(string(exported), ProcessorAttemptExportSchema) ||
		!strings.Contains(string(exported), attempt.ID) ||
		!strings.Contains(string(exported), "PROCESSOR_STAGE_FAILED") {
		t.Fatalf("exported attempts = %s", exported)
	}
	var decoded map[string]any
	if err := json.Unmarshal(exported, &decoded); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
}
