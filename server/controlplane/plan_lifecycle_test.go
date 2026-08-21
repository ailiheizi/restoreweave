package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestPlanReviseAbandonAndDoctor(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("plan-lifecycle"), 0o600); err != nil {
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

	wrong := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanRevise, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
		"plan_digest":  "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"decisions":    []map[string]any{{"decision": "KEEP"}},
	}))
	if wrong.Status != command.StatusFailed || !hasReasonCode(wrong, ReasonCodeConflict) {
		t.Fatalf("revise wrong digest = %q: %+v", wrong.Status, wrong.Reasons)
	}

	revised := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanRevise, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
		"plan_digest":  ingestData.PlanDigest,
		"decisions":    []map[string]any{{"decision": "KEEP", "reason": "operator review"}},
	}))
	if revised.Status != command.StatusSucceeded {
		t.Fatalf("revise = %q: %+v", revised.Status, revised.Reasons)
	}
	var reviseData command.PlanReviseData
	if err := json.Unmarshal(revised.Data, &reviseData); err != nil {
		t.Fatalf("decode revise: %v", err)
	}
	if reviseData.BasePlanID != ingestData.PlanID || reviseData.Applied || !reviseData.Executable || reviseData.PlanID == ingestData.PlanID {
		t.Fatalf("revise data = %+v", reviseData)
	}

	base := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanGet, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
	}))
	var baseData command.PlanGetData
	if err := json.Unmarshal(base.Data, &baseData); err != nil {
		t.Fatalf("decode base: %v", err)
	}
	if baseData.State != "READY" || baseData.Applied || !baseData.Executable || baseData.Abandoned {
		t.Fatalf("base mutated = %+v", baseData)
	}

	got := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanGet, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      reviseData.PlanID,
	}))
	var successor command.PlanGetData
	if err := json.Unmarshal(got.Data, &successor); err != nil {
		t.Fatalf("decode successor: %v", err)
	}
	if successor.Kind != "INGEST" || successor.BasePlanID != ingestData.PlanID || !successor.Executable || successor.Applied {
		t.Fatalf("successor = %+v", successor)
	}

	abandoned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanAbandon, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      reviseData.PlanID,
	}))
	if abandoned.Status != command.StatusSucceeded {
		t.Fatalf("abandon = %q: %+v", abandoned.Status, abandoned.Reasons)
	}
	again := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanAbandon, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      reviseData.PlanID,
	}))
	if again.Status != command.StatusSucceeded {
		t.Fatalf("abandon again = %q: %+v", again.Status, again.Reasons)
	}
	var abandonData command.PlanAbandonData
	if err := json.Unmarshal(again.Data, &abandonData); err != nil {
		t.Fatalf("decode abandon: %v", err)
	}
	if !abandonData.AlreadyAbandoned || abandonData.AbandonedPlanID != reviseData.PlanID {
		t.Fatalf("abandon data = %+v", abandonData)
	}

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanGet, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      reviseData.PlanID,
	}))
	var after command.PlanGetData
	if err := json.Unmarshal(listed.Data, &after); err != nil {
		t.Fatalf("decode abandoned get: %v", err)
	}
	if !after.Abandoned || after.State != "ABANDONED" {
		t.Fatalf("abandoned get = %+v", after)
	}

	reviseAbandoned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanRevise, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      reviseData.PlanID,
		"plan_digest":  reviseData.PlanDigest,
	}))
	if reviseAbandoned.Status != command.StatusFailed || !hasReasonCode(reviseAbandoned, ReasonCodeConflict) {
		t.Fatalf("revise abandoned = %q: %+v", reviseAbandoned.Status, reviseAbandoned.Reasons)
	}

	applyBase := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanApply, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
		"plan_digest":  ingestData.PlanDigest,
	}))
	if applyBase.Status != command.StatusSucceeded {
		t.Fatalf("apply base = %q: %+v", applyBase.Status, applyBase.Reasons)
	}
	abandonApplied := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanAbandon, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"plan_id":      ingestData.PlanID,
	}))
	if abandonApplied.Status != command.StatusFailed || !hasReasonCode(abandonApplied, ReasonCodeConflict) {
		t.Fatalf("abandon applied = %q: %+v", abandonApplied.Status, abandonApplied.Reasons)
	}

	healthy := dispatcher.Handle(ctx, mustEnvelope(t, command.OpDoctorCheck, map[string]any{"source": root}))
	if healthy.Status != command.StatusSucceeded {
		t.Fatalf("doctor = %q: %+v", healthy.Status, healthy.Reasons)
	}
	var doctor command.DoctorData
	if err := json.Unmarshal(healthy.Data, &doctor); err != nil {
		t.Fatalf("decode doctor: %v", err)
	}
	if !doctor.OK {
		t.Fatalf("doctor not ok: %+v", doctor.Checks)
	}
	seen := map[string]bool{}
	engineHonest := false
	for _, check := range doctor.Checks {
		seen[check.ID] = check.OK
		if check.ID == "engine" && check.OK && strings.Contains(check.Message, "not a selected release engine") {
			engineHonest = true
		}
	}
	if !engineHonest {
		t.Fatalf("doctor engine check was not honest: %+v", doctor.Checks)
	}
	for _, id := range []string{"controller", "catalog", "repository", "identify", "processors", "recovery", "engine", "source"} {
		if !seen[id] {
			t.Fatalf("doctor missing %s: %+v", id, doctor.Checks)
		}
	}

	missing := dispatcher.Handle(ctx, mustEnvelope(t, command.OpDoctorCheck, map[string]any{
		"source": filepath.Join(t.TempDir(), "missing"),
	}))
	if missing.Status != command.StatusDegraded {
		t.Fatalf("doctor missing source = %q: %+v", missing.Status, missing.Reasons)
	}
	var degraded command.DoctorData
	if err := json.Unmarshal(missing.Data, &degraded); err != nil {
		t.Fatalf("decode degraded doctor: %v", err)
	}
	if degraded.OK {
		t.Fatalf("missing source reported ok: %+v", degraded.Checks)
	}
}

func TestPlanBodyExecutableRejectsBlockedIngest(t *testing.T) {
	body := planBody{
		Kind: "INGEST",
		Ingest: &exact.IngestPlan{
			Executable: true,
			BlockedEntries: []exact.IngestPlanIssue{{
				RelativePath: "changing.bin",
				State:        "UNSTABLE",
				ReasonCode:   "CONTENT_CHANGED_DURING_READ",
			}},
		},
	}
	if planBodyExecutable(body) {
		t.Fatal("ingest plan with blocked entries was executable")
	}
	body.Ingest.BlockedEntries = nil
	body.Ingest.Executable = false
	if planBodyExecutable(body) {
		t.Fatal("ingest plan marked non-executable was executable")
	}
	body.Ingest.Executable = true
	if !planBodyExecutable(body) {
		t.Fatal("clean executable ingest plan was rejected")
	}
}

func TestPlanReviseRecomputesIngestProtectionWithoutMutatingBase(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "payload.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("write keep: %v", err)
	}

	planned := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanIngest, map[string]any{"root": root}))
	if planned.Status != command.StatusSucceeded {
		t.Fatalf("plan.ingest = %q: %+v", planned.Status, planned.Reasons)
	}
	var ingest command.PlanIngestData
	if err := json.Unmarshal(planned.Data, &ingest); err != nil {
		t.Fatalf("decode ingest: %v", err)
	}
	base, err := store.GetPlan(ctx, ingest.WorkspaceID, ingest.PlanID)
	if err != nil {
		t.Fatalf("get base plan: %v", err)
	}
	baseBody, err := decodePlanBody(base.Plan)
	if err != nil {
		t.Fatalf("decode base body: %v", err)
	}
	baseDigest := base.PlanDigest

	revised := dispatcher.Handle(ctx, mustEnvelope(t, command.OpPlanRevise, map[string]any{
		"workspace_id": ingest.WorkspaceID,
		"plan_id":      ingest.PlanID,
		"plan_digest":  ingest.PlanDigest,
		"decisions": []map[string]any{{
			"path": "payload.txt", "mode": "METADATA_ONLY", "reason": "regenerable fixture",
		}},
	}))
	if revised.Status != command.StatusSucceeded {
		t.Fatalf("plan.revise = %q: %+v", revised.Status, revised.Reasons)
	}
	var revision command.PlanReviseData
	if err := json.Unmarshal(revised.Data, &revision); err != nil {
		t.Fatalf("decode revision: %v", err)
	}
	successor, err := store.GetPlan(ctx, ingest.WorkspaceID, revision.PlanID)
	if err != nil {
		t.Fatalf("get successor: %v", err)
	}
	successorBody, err := decodePlanBody(successor.Plan)
	if err != nil {
		t.Fatalf("decode successor body: %v", err)
	}
	if successorBody.Ingest == nil || successorBody.Ingest.FileProtection["payload.txt"] != "METADATA_ONLY" {
		t.Fatalf("successor file protection = %+v", successorBody.Ingest.FileProtection)
	}
	if successorBody.Ingest.ProtectionDigest == baseBody.Ingest.ProtectionDigest {
		t.Fatalf("successor reused base protection digest %q", successorBody.Ingest.ProtectionDigest)
	}
	var found bool
	for _, decision := range successorBody.Ingest.ProtectionDecisions {
		if decision.RelativePath == "payload.txt" {
			found = true
			if decision.PlannedOutcome != "EXPLICITLY_UNPROTECTED" {
				t.Fatalf("revised planned outcome = %q", decision.PlannedOutcome)
			}
		}
	}
	if !found {
		t.Fatalf("revised protection decision missing: %+v", successorBody.Ingest.ProtectionDecisions)
	}
	if successorBody.LocalFiles != 1 || successorBody.Files != 2 {
		t.Fatalf("revised estimate = local=%d files=%d", successorBody.LocalFiles, successorBody.Files)
	}
	unchanged, err := store.GetPlan(ctx, ingest.WorkspaceID, ingest.PlanID)
	if err != nil {
		t.Fatalf("get base after revision: %v", err)
	}
	if unchanged.PlanDigest != baseDigest {
		t.Fatalf("base plan digest changed from %q to %q", baseDigest, unchanged.PlanDigest)
	}
}

func TestPlanReviseRejectsUncontrolledIngestDecisions(t *testing.T) {
	base := map[string]sqlite.ProtectionMode{"a.txt": sqlite.ProtectionStoreExact}
	for name, raw := range map[string]json.RawMessage{
		"unknown field": json.RawMessage(`[{"path":"a.txt","mode":"METADATA_ONLY","unexpected":true}]`),
		"unsafe path":   json.RawMessage(`[{"path":"../a.txt","mode":"METADATA_ONLY"}]`),
		"unknown mode":  json.RawMessage(`[{"path":"a.txt","mode":"COMPRESS"}]`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := reviseIngestProtection(base, raw); err == nil {
				t.Fatal("reviseIngestProtection accepted uncontrolled decision")
			}
		})
	}
}

func TestDoctorWithoutExactLane(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpDoctorCheck, map[string]any{}))
	if result.Status != command.StatusDegraded {
		t.Fatalf("doctor without exact = %q: %+v", result.Status, result.Reasons)
	}
	var data command.DoctorData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if data.OK {
		t.Fatalf("doctor without exact reported ok: %+v", data.Checks)
	}
}
