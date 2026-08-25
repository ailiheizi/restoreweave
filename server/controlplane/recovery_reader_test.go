package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

func openRecoveryReaderDispatcher(t *testing.T) (*Dispatcher, *Dispatcher, command.RecoveryExportData, string, string) {
	t.Helper()
	writer, exported, anchorPath, snapshotRef := newRecoveryHandlerHarness(t)
	readonly, err := repository.OpenProfileReadOnly(repository.RepositoryProfileDirectoryCASDev, writer.exact.Repo.Root())
	if err != nil {
		t.Fatalf("open repository for clean reader: %v", err)
	}
	anchor, err := exact.LoadTrustAnchor(anchorPath)
	if err != nil {
		t.Fatalf("load independently retained trust anchor: %v", err)
	}
	reader, err := NewRecoveryDispatcher("/tmp/restoreweave/recovery-reader-test.sock", &exact.Service{
		Repo: readonly, TrustAnchor: &anchor,
		PublicationDomain: exact.DefaultPublicationDomain, RequireSignedPublication: true,
	})
	if err != nil {
		t.Fatalf("new recovery dispatcher: %v", err)
	}
	return reader, writer, exported, anchorPath, snapshotRef
}

func TestNewRecoveryDispatcherRequiresCatalogFreeSignedReader(t *testing.T) {
	writer, _, anchorPath, _ := newRecoveryHandlerHarness(t)
	anchor, err := exact.LoadTrustAnchor(anchorPath)
	if err != nil {
		t.Fatal(err)
	}
	base := func() *exact.Service {
		return &exact.Service{
			Repo: writer.exact.Repo, TrustAnchor: &anchor,
			PublicationDomain: exact.DefaultPublicationDomain, RequireSignedPublication: true,
		}
	}
	if _, err := NewRecoveryDispatcher("/tmp/restoreweave/recovery-reader-test.sock", nil); err == nil {
		t.Fatal("nil service was accepted")
	}
	withStore := base()
	withStore.Store = writer.exact.Store
	if _, err := NewRecoveryDispatcher("/tmp/restoreweave/recovery-reader-test.sock", withStore); err == nil {
		t.Fatal("reader accepted an operational catalog")
	}
	withSigner := base()
	withSigner.SigningIdentity = writer.exact.SigningIdentity
	if _, err := NewRecoveryDispatcher("/tmp/restoreweave/recovery-reader-test.sock", withSigner); err == nil {
		t.Fatal("reader accepted a signing identity")
	}

	reader, _, _, _, _ := openRecoveryReaderDispatcher(t)
	want := map[string]bool{
		command.OpRecoveryImport: true,
		command.OpSnapshotList:   true,
		command.OpSnapshotDiff:   true,
		command.OpSnapshotVerify: true,
		command.OpPlanRestore:    true,
		command.OpPlanApply:      true,
	}
	if !reflect.DeepEqual(reader.implemented, want) {
		t.Fatalf("reader operations = %v, want exactly %v", reader.implemented, want)
	}
	for _, operation := range command.KnownOperations() {
		if reader.implemented[operation] != want[operation] {
			t.Fatalf("operation %q exposure = %v, want %v", operation, reader.implemented[operation], want[operation])
		}
	}
}

func TestRecoveryReaderPlanApplyIsCatalogFreeDigestBoundAndIdempotent(t *testing.T) {
	ctx := context.Background()
	reader, _, exported, anchorPath, snapshotRef := openRecoveryReaderDispatcher(t)
	repoRoot := reader.exact.Repo.Root()
	beforeImport := recoveryReaderTree(t, repoRoot)

	imported := reader.Handle(ctx, mustEnvelope(t, command.OpRecoveryImport, map[string]any{
		"artifact_path": exported.ArtifactPath, "trust_anchor_path": anchorPath,
		"publication_domain": exact.DefaultPublicationDomain,
	}))
	if imported.Status != command.StatusSucceeded {
		t.Fatalf("recovery.import = %q: %+v", imported.Status, imported.Reasons)
	}
	var importData command.RecoveryImportData
	if err := json.Unmarshal(imported.Data, &importData); err != nil {
		t.Fatal(err)
	}
	if importData.SnapshotRef != snapshotRef || importData.CatalogCreated {
		t.Fatalf("recovery.import data = %+v", importData)
	}
	if after := recoveryReaderTree(t, repoRoot); !reflect.DeepEqual(after, beforeImport) {
		t.Fatal("recovery.import changed repository bytes")
	}

	listed := reader.Handle(ctx, mustEnvelope(t, command.OpSnapshotList, map[string]any{}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var listData command.SnapshotListData
	if err := json.Unmarshal(listed.Data, &listData); err != nil {
		t.Fatal(err)
	}
	if len(listData.Snapshots) != 1 || listData.Snapshots[0].SnapshotRef != snapshotRef {
		t.Fatalf("snapshot.list data = %+v", listData)
	}
	if listData.Snapshots[0].WorkspaceID != "" || listData.Snapshots[0].NamespaceRootID != "" {
		t.Fatalf("clean reader exposed catalog browse projection: %+v", listData.Snapshots[0])
	}

	verified := reader.Handle(ctx, mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": snapshotRef, "mode": command.VerifyFullBytes,
	}))
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.verify = %q: %+v", verified.Status, verified.Reasons)
	}
	var verifyData command.SnapshotVerifyData
	if err := json.Unmarshal(verified.Data, &verifyData); err != nil {
		t.Fatal(err)
	}
	if !verifyData.OK || verifyData.CatalogUsed || verifyData.SnapshotRef != snapshotRef {
		t.Fatalf("snapshot.verify data = %+v", verifyData)
	}

	diffed := reader.Handle(ctx, mustEnvelope(t, command.OpSnapshotDiff, map[string]any{
		"from_snapshot_ref": snapshotRef, "to_snapshot_ref": snapshotRef,
	}))
	if diffed.Status != command.StatusSucceeded {
		t.Fatalf("snapshot.diff = %q: %+v", diffed.Status, diffed.Reasons)
	}

	destination := filepath.Join(t.TempDir(), "restore")
	beforePlan := recoveryReaderTree(t, repoRoot)
	planned := reader.Handle(ctx, mustEnvelope(t, command.OpPlanRestore, map[string]any{
		"snapshot_ref": snapshotRef, "destination": destination,
	}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.restore = %q: %+v", planned.Status, planned.Reasons)
	}
	var planData command.PlanRestoreData
	if err := json.Unmarshal(planned.Data, &planData); err != nil {
		t.Fatal(err)
	}
	if planData.PlanID == "" || planData.PlanDigest == "" || planData.WorkspaceID == "" || planData.Wrote {
		t.Fatalf("plan.restore data = %+v", planData)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("plan.restore created destination: %v", err)
	}
	if afterPlan := recoveryReaderTree(t, repoRoot); !reflect.DeepEqual(afterPlan, beforePlan) {
		t.Fatal("plan.restore changed repository bytes")
	}

	wrongDigest := reader.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": planData.WorkspaceID, "plan_id": planData.PlanID, "plan_digest": "sha256:" + strings.Repeat("0", 64),
	}))
	if wrongDigest.Status != command.StatusFailed || !hasReasonCode(wrongDigest, ReasonCodeConflict) {
		t.Fatalf("wrong plan digest = %q: %+v", wrongDigest.Status, wrongDigest.Reasons)
	}

	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := reader.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": planData.WorkspaceID, "plan_id": planData.PlanID, "plan_digest": planData.PlanDigest,
	}))
	if stale.Status != command.StatusFailed || !hasReasonCode(stale, ReasonCodeConflict) {
		t.Fatalf("stale destination = %q: %+v", stale.Status, stale.Reasons)
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}

	applied := reader.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": planData.WorkspaceID, "plan_id": planData.PlanID, "plan_digest": planData.PlanDigest,
	}))
	if applied.Status != command.StatusSucceeded {
		t.Fatalf("plan.apply = %q: %+v", applied.Status, applied.Reasons)
	}
	var applyData command.PlanApplyData
	if err := json.Unmarshal(applied.Data, &applyData); err != nil {
		t.Fatal(err)
	}
	if applyData.SnapshotRef != snapshotRef || applyData.Files != 1 || applyData.AlreadyApplied {
		t.Fatalf("first plan.apply data = %+v", applyData)
	}
	got, err := os.ReadFile(filepath.Join(destination, "blob.bin"))
	if err != nil || string(got) != "recovery handler payload" {
		t.Fatalf("restored payload = %q, err=%v", got, err)
	}

	replayed := reader.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": planData.WorkspaceID, "plan_id": planData.PlanID, "plan_digest": planData.PlanDigest,
	}))
	if replayed.Status != command.StatusSucceeded {
		t.Fatalf("replayed plan.apply = %q: %+v", replayed.Status, replayed.Reasons)
	}
	var replayData command.PlanApplyData
	if err := json.Unmarshal(replayed.Data, &replayData); err != nil {
		t.Fatal(err)
	}
	if !replayData.AlreadyApplied || replayData.SnapshotRef != applyData.SnapshotRef || replayData.Files != applyData.Files || replayData.Bytes != applyData.Bytes {
		t.Fatalf("replayed plan.apply data = %+v, first = %+v", replayData, applyData)
	}
	if afterApply := recoveryReaderTree(t, repoRoot); !reflect.DeepEqual(afterApply, beforePlan) {
		t.Fatal("restore apply changed repository bytes")
	}
}

func recoveryReaderTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(payload)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		tree[rel] = hex.EncodeToString(digest[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
