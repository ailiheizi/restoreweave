package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type planGetInput struct {
	WorkspaceID string `json:"workspace_id"`
	PlanID      string `json:"plan_id"`
}

type planApplyInput struct {
	WorkspaceID string `json:"workspace_id"`
	PlanID      string `json:"plan_id"`
	PlanRef     string `json:"plan_ref"`
	PlanDigest  string `json:"plan_digest"`
}

type planReviseInput struct {
	WorkspaceID string          `json:"workspace_id"`
	PlanID      string          `json:"plan_id"`
	PlanRef     string          `json:"plan_ref"`
	PlanDigest  string          `json:"plan_digest"`
	Decisions   json.RawMessage `json:"decisions"`
}

type planAbandonInput struct {
	WorkspaceID string `json:"workspace_id"`
	PlanID      string `json:"plan_id"`
	PlanRef     string `json:"plan_ref"`
	PlanDigest  string `json:"plan_digest"`
}

type planBody struct {
	Schema              string             `json:"schema"`
	Kind                string             `json:"kind"`
	ConfigDigest        string             `json:"config_digest,omitempty"`
	Applied             bool               `json:"applied"`
	SnapshotRef         string             `json:"snapshot_ref,omitempty"`
	ManifestDigest      string             `json:"manifest_digest,omitempty"`
	RootID              string             `json:"root_id,omitempty"`
	Files               int                `json:"files,omitempty"`
	Bytes               int64              `json:"bytes,omitempty"`
	ProtectionMode      string             `json:"protection_mode,omitempty"`
	LocalFiles          int                `json:"local_files,omitempty"`
	LocalBytes          int64              `json:"local_bytes,omitempty"`
	NewBytes            int64              `json:"new_bytes,omitempty"`
	LinkOnlyFiles       int                `json:"link_only_files,omitempty"`
	LocatorCount        int                `json:"locator_count,omitempty"`
	BasePlanID          string             `json:"base_plan_id,omitempty"`
	BasePlanDigest      string             `json:"base_plan_digest,omitempty"`
	AbandonedPlanID     string             `json:"abandoned_plan_id,omitempty"`
	AbandonedPlanDigest string             `json:"abandoned_plan_digest,omitempty"`
	Decisions           json.RawMessage    `json:"decisions,omitempty"`
	Destination         string             `json:"destination,omitempty"`
	Wrote               bool               `json:"wrote,omitempty"`
	Note                string             `json:"note,omitempty"`
	SourceBasisDigest   string             `json:"source_basis_digest,omitempty"`
	RepositoryRoot      string             `json:"repository_root,omitempty"`
	Ingest              *exact.IngestPlan  `json:"ingest,omitempty"`
	Restore             *exact.RestorePlan `json:"restore,omitempty"`
}

const (
	planSchemaV2       = "org.restoreweave.plan.v2"
	planInspectJobKind = "PLAN_INSPECT"
	planApplyJobKind   = "PLAN_APPLY"
)

func (d *Dispatcher) handlePlanGet(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input planGetInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("plan_id", input.PlanID); err != nil {
		return invalidInputResult(env, started, err)
	}
	record, err := d.store.GetPlan(ctx, input.WorkspaceID, input.PlanID)
	if err != nil {
		if containsNotFound(err) {
			return notFoundResult(env, started, "plan not found")
		}
		return catalogErrorResult(env, started, err)
	}
	abandoned, err := d.planAbandoned(ctx, input.WorkspaceID, record.ID)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	applied, executionExists, err := d.planExecutionStatus(ctx, record)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, projectPlan(record, abandoned, applied, executionExists))
}

func (d *Dispatcher) handlePlanApply(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input planApplyInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if d.recoveryReader != nil {
		return d.applyRecoveryRestorePlan(ctx, env, started, input)
	}
	planID := firstNonEmpty(input.PlanID, input.PlanRef)
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("plan_id", planID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.PlanDigest) == "" {
		return invalidInputResult(env, started, errString("plan_digest is required"))
	}
	record, err := d.store.GetPlan(ctx, input.WorkspaceID, planID)
	if err != nil {
		if containsNotFound(err) {
			return notFoundResult(env, started, "plan not found")
		}
		return catalogErrorResult(env, started, err)
	}
	if record.PlanDigest != input.PlanDigest {
		return conflictResult(env, started, "plan digest does not match")
	}
	abandoned, err := d.planAbandoned(ctx, input.WorkspaceID, record.ID)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	if abandoned {
		return conflictResult(env, started, "plan is abandoned")
	}
	body, err := decodePlanBody(record.Plan)
	if err != nil {
		return catalogErrorResult(env, started, fmt.Errorf("decode plan: %w", err))
	}
	if body.Applied {
		return succeeded(env, started, command.PlanApplyData{
			PlanID: record.ID, PlanDigest: record.PlanDigest, AlreadyApplied: true,
			SnapshotRef: body.SnapshotRef, State: string(sqlite.JobSucceeded),
		})
	}
	if !planBodyExecutable(body) {
		return conflictResult(env, started, "plan is not executable; create a plan with a concrete source or destination")
	}
	if body.ConfigDigest != d.effectiveConfigDigest() {
		return conflictResult(env, started, "effective configuration changed after planning")
	}
	if d.exact == nil || d.exact.Repo == nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, "exact repository is unavailable"))
	}
	if body.RepositoryRoot != "" && body.RepositoryRoot != d.exact.Repo.Root() {
		return conflictResult(env, started, "repository target changed after planning")
	}

	job, claimed, err := d.claimPlanApplyJob(ctx, env, record)
	if err != nil {
		if errors.Is(err, sqlite.ErrConflict) {
			return conflictResult(env, started, "plan apply is already running")
		}
		return catalogErrorResult(env, started, err)
	}
	if !claimed {
		return d.replayPlanApplyJob(ctx, env, started, record, body, job)
	}

	data, applyErr := d.executePlanBody(ctx, record, body)
	data.JobID = job.ID
	if applyErr != nil {
		code := planApplyReasonCode(applyErr)
		state := sqlite.JobFailed
		if errors.Is(applyErr, exact.ErrUnknownExternalOutcome) || errors.Is(applyErr, exact.ErrNeedsReconciliation) {
			state = sqlite.JobNeedsReconcile
		}
		finishCtx := ctx
		if state == sqlite.JobNeedsReconcile {
			// The operation may have crossed an external durability boundary
			// before its request context expired. Persist the reconciliation state
			// with a detached context so the next plan.apply can reconcile the
			// immutable publication instead of replaying the mutating lane.
			finishCtx = context.WithoutCancel(ctx)
		}
		if err := d.finishPlanApplyJob(finishCtx, env, job, state, data, code, applyErr.Error()); err != nil {
			return catalogErrorResult(env, started, err)
		}
		if state == sqlite.JobNeedsReconcile {
			return unknownExternalOutcomeResult(env, started, applyErr)
		}
		return failed(env, started, newReason(code, applyErr.Error()))
	}
	if err := d.finishPlanApplyJob(ctx, env, job, sqlite.JobSucceeded, data, "", ""); err != nil {
		return catalogErrorResult(env, started, err)
	}
	if len(data.Warnings) > 0 {
		return degradedResult(env, started, data, "exact publication succeeded, but post-publication processing is unavailable: "+strings.Join(data.Warnings, "; "))
	}
	return succeeded(env, started, data)
}

type planApplyJobResult struct {
	Data       command.PlanApplyData `json:"data"`
	ReasonCode string                `json:"reason_code,omitempty"`
	Error      string                `json:"error,omitempty"`
}

func planBodyExecutable(body planBody) bool {
	switch body.Kind {
	case "INGEST":
		return body.Ingest != nil && body.Ingest.Executable && len(body.Ingest.BlockedEntries) == 0
	case "RESTORE":
		return body.Restore != nil && body.Restore.Destination != ""
	default:
		return false
	}
}

func (d *Dispatcher) effectiveConfigDigest() string {
	if d.configDigest != "" {
		return d.configDigest
	}
	if d.exact != nil {
		return d.exact.ConfigDigest
	}
	return ""
}

func (d *Dispatcher) claimPlanApplyJob(
	ctx context.Context,
	env command.Envelope,
	plan sqlite.Plan,
) (sqlite.Job, bool, error) {
	job, err := d.store.GetJobByPlanKind(ctx, plan.WorkspaceID, plan.ID, planApplyJobKind)
	if containsNotFound(err) {
		jobID, idErr := sqlite.NewStableID(sqlite.IDPrefixJob)
		if idErr != nil {
			return sqlite.Job{}, false, idErr
		}
		input, marshalErr := json.Marshal(map[string]string{
			"plan_id": plan.ID, "plan_digest": plan.PlanDigest,
		})
		if marshalErr != nil {
			return sqlite.Job{}, false, marshalErr
		}
		candidate := sqlite.Job{
			ID: jobID, WorkspaceID: plan.WorkspaceID, PlanID: plan.ID,
			Kind: planApplyJobKind, State: sqlite.JobQueued, Input: input, MaxAttempts: 3,
		}
		insertErr := d.store.Update(ctx, func(tx *sqlite.Tx) error {
			return tx.InsertJob(ctx, &candidate)
		})
		if insertErr != nil && !errors.Is(insertErr, sqlite.ErrConflict) {
			return sqlite.Job{}, false, insertErr
		}
		job, err = d.store.GetJobByPlanKind(ctx, plan.WorkspaceID, plan.ID, planApplyJobKind)
	}
	if err != nil {
		return sqlite.Job{}, false, err
	}
	if jobTerminal(job.State) {
		return job, false, nil
	}

	now := d.now().UTC()
	leaseToken := env.RequestID + ":" + job.ID
	var fencingToken int64
	err = d.store.Update(ctx, func(tx *sqlite.Tx) error {
		var acquireErr error
		fencingToken, acquireErr = tx.AcquireJobLease(
			ctx, job.WorkspaceID, job.ID, job.Revision,
			"restoreweaved", leaseToken, now, now.Add(10*time.Minute),
		)
		if acquireErr != nil {
			return acquireErr
		}
		details, _ := json.Marshal(map[string]any{
			"plan_id": plan.ID, "plan_digest": plan.PlanDigest,
			"attempt": job.Attempt + 1,
		})
		return tx.AppendAuditEvent(ctx, &sqlite.AuditEvent{
			ID: mustAuditID(), WorkspaceID: job.WorkspaceID, Actor: "restoreweaved",
			Action: "JOB_STARTED", TargetType: "JOB", TargetID: job.ID,
			RequestID: env.RequestID, Attempt: job.Attempt + 1,
			FencingToken: fencingToken, Outcome: "SUCCEEDED", Details: details,
		})
	})
	if err != nil {
		return job, false, err
	}
	job, err = d.store.GetJob(ctx, job.WorkspaceID, job.ID)
	if err != nil {
		return sqlite.Job{}, false, err
	}
	return job, true, nil
}

func (d *Dispatcher) executePlanBody(ctx context.Context, plan sqlite.Plan, body planBody) (command.PlanApplyData, error) {
	data := command.PlanApplyData{
		PlanID: plan.ID, PlanDigest: plan.PlanDigest, State: string(sqlite.JobSucceeded),
	}
	switch body.Kind {
	case "INGEST":
		// A process can crash after exact publication commits but before the
		// PLAN_APPLY job records its result. The publication's plan digest is
		// the execution key; recover that result before rescanning or creating
		// another snapshot/publication.
		result, reconcileErr := d.exact.ReconcileIngestPublicationCompletion(ctx, plan.WorkspaceID, plan.PlanDigest)
		if reconcileErr == nil {
			return planApplyDataFromIngestResult(plan, result), nil
		}
		if !containsNotFound(reconcileErr) {
			return command.PlanApplyData{PlanID: plan.ID, PlanDigest: plan.PlanDigest}, reconcileErr
		}
		result, err := d.exact.ApplyIngestPlanWithExecutionKey(ctx, *body.Ingest, plan.PlanDigest)
		if err != nil {
			// A root commit can be fully durable while a deterministic
			// post-commit child still has an unknown placement outcome. Preserve
			// the authenticated partial result so the job becomes durably
			// reconcilable; do not collapse it to root-only success below.
			if (errors.Is(err, exact.ErrUnknownExternalOutcome) || errors.Is(err, exact.ErrNeedsReconciliation)) && result.PublicationCommitDigest != "" {
				return planApplyDataFromIngestResult(plan, result), err
			}
			// A concurrent publisher may have won the unique execution-key
			// constraint after the first lookup. Reconcile its committed result
			// instead of allowing this retry to publish a second snapshot.
			if recovered, recoverErr := d.exact.ReconcileIngestPublicationCompletion(ctx, plan.WorkspaceID, plan.PlanDigest); recoverErr == nil {
				return planApplyDataFromIngestResult(plan, recovered), nil
			}
			return data, err
		}
		return planApplyDataFromIngestResult(plan, result), nil
	case "RESTORE":
		// A restore can finish writing its complete destination and then crash
		// before the PLAN_APPLY job records success. Reconcile that immutable
		// output first; only an absent or empty destination may be written.
		result, reconcileErr := d.exact.ReconcileRestorePlan(ctx, *body.Restore)
		if reconcileErr == nil {
			data.SnapshotRef = result.SnapshotRef
			data.Destination = result.Destination
			data.Files = result.Files
			data.Bytes = result.Bytes
			return data, nil
		}
		if !errors.Is(reconcileErr, exact.ErrRestoreNotExecuted) {
			return data, reconcileErr
		}
		result, err := d.exact.ApplyRestorePlan(ctx, *body.Restore)
		if err != nil {
			return data, err
		}
		data.SnapshotRef = result.SnapshotRef
		data.Destination = result.Destination
		data.Files = result.Files
		data.Bytes = result.Bytes
		return data, nil
	default:
		return data, fmt.Errorf("unsupported plan kind %q", body.Kind)
	}
}

func planApplyDataFromIngestResult(plan sqlite.Plan, result exact.IngestResult) command.PlanApplyData {
	return command.PlanApplyData{
		PlanID: plan.ID, PlanDigest: plan.PlanDigest, State: string(sqlite.JobSucceeded),
		WorkspaceID: result.WorkspaceID, SourceID: result.SourceID, ScanID: result.ScanID,
		RootID: result.RootID, SnapshotRef: result.SnapshotRef, ManifestDigest: result.ManifestDigest,
		ProtectionDigest:    result.ProtectionDigest,
		ProtectionDecisions: protectionDecisionsForCommand(result.ProtectionDecisions),
		Files:               result.Files, Bytes: result.Bytes, Warnings: append([]string(nil), result.Warnings...),
	}
}

func (d *Dispatcher) finishPlanApplyJob(
	ctx context.Context,
	env command.Envelope,
	job sqlite.Job,
	state sqlite.JobState,
	data command.PlanApplyData,
	reasonCode, message string,
) error {
	data.State = string(state)
	payload, err := json.Marshal(planApplyJobResult{Data: data, ReasonCode: reasonCode, Error: message})
	if err != nil {
		return err
	}
	return d.store.Update(ctx, func(tx *sqlite.Tx) error {
		if err := tx.UpdateJob(ctx, sqlite.JobUpdate{
			WorkspaceID: job.WorkspaceID, JobID: job.ID, ExpectedRevision: job.Revision,
			State: state, Result: payload, ErrorCode: reasonCode, Attempt: job.Attempt,
		}); err != nil {
			return err
		}
		action, outcome := "JOB_SUCCEEDED", "SUCCEEDED"
		if state == sqlite.JobNeedsReconcile {
			action, outcome = "JOB_NEEDS_RECONCILIATION", "UNKNOWN_EXTERNAL_OUTCOME"
		} else if state != sqlite.JobSucceeded {
			action, outcome = "JOB_FAILED", "FAILED"
		}
		return tx.AppendAuditEvent(ctx, &sqlite.AuditEvent{
			ID: mustAuditID(), WorkspaceID: job.WorkspaceID, Actor: "restoreweaved",
			Action: action, TargetType: "JOB", TargetID: job.ID,
			RequestID: env.RequestID, Attempt: job.Attempt,
			FencingToken: job.FencingToken, Outcome: outcome, Details: payload,
		})
	})
}

func (d *Dispatcher) replayPlanApplyJob(
	ctx context.Context,
	env command.Envelope,
	started time.Time,
	plan sqlite.Plan,
	body planBody,
	job sqlite.Job,
) command.Result {
	var stored planApplyJobResult
	_ = json.Unmarshal(job.Result, &stored)
	switch job.State {
	case sqlite.JobSucceeded:
		stored.Data.PlanID = plan.ID
		stored.Data.PlanDigest = plan.PlanDigest
		stored.Data.JobID = job.ID
		stored.Data.State = string(job.State)
		stored.Data.AlreadyApplied = true
		if len(stored.Data.Warnings) > 0 {
			return degradedResult(env, started, stored.Data, "exact publication succeeded, but post-publication processing is unavailable: "+strings.Join(stored.Data.Warnings, "; "))
		}
		return succeeded(env, started, stored.Data)
	case sqlite.JobFailed:
		code := stored.ReasonCode
		if code == "" {
			code = firstNonEmpty(job.ErrorCode, ReasonCodeCatalogError)
		}
		message := firstNonEmpty(stored.Error, "plan apply previously failed")
		return failed(env, started, newReason(code, message))
	case sqlite.JobNeedsReconcile:
		// A reconciliation retry is deliberately read-only. It may promote the
		// existing job only after the immutable publication/output evidence is
		// complete and bound to this exact execution key. It must never fall
		// through to Apply* and create a second publication or overwrite an
		// ambiguous destination.
		if d.exact != nil {
			switch body.Kind {
			case "INGEST":
				if reconciled, err := d.exact.ReconcileIngestPublicationCompletion(ctx, plan.WorkspaceID, plan.PlanDigest); err == nil {
					data := planApplyDataFromIngestResult(plan, reconciled)
					data.JobID = job.ID
					data.AlreadyApplied = true
					if finishErr := d.finishPlanApplyJob(ctx, env, job, sqlite.JobSucceeded, data, "", ""); finishErr != nil {
						return catalogErrorResult(env, started, finishErr)
					}
					if len(data.Warnings) > 0 {
						return degradedResult(env, started, data, "exact publication succeeded, but post-publication processing is unavailable: "+strings.Join(data.Warnings, "; "))
					}
					return succeeded(env, started, data)
				}
			case "RESTORE":
				if body.Restore != nil {
					if reconciled, err := d.exact.ReconcileRestorePlan(ctx, *body.Restore); err == nil {
						data := command.PlanApplyData{
							PlanID: plan.ID, PlanDigest: plan.PlanDigest, JobID: job.ID,
							State: string(sqlite.JobSucceeded), AlreadyApplied: true,
							SnapshotRef: reconciled.SnapshotRef, Destination: reconciled.Destination,
							Files: reconciled.Files, Bytes: reconciled.Bytes,
						}
						if finishErr := d.finishPlanApplyJob(ctx, env, job, sqlite.JobSucceeded, data, "", ""); finishErr != nil {
							return catalogErrorResult(env, started, finishErr)
						}
						return succeeded(env, started, data)
					}
				}
			}
		}
		message := firstNonEmpty(stored.Error, "plan apply requires external outcome reconciliation")
		return unknownExternalOutcomeResult(env, started, errors.New(message))
	case sqlite.JobCancelled:
		return conflictResult(env, started, "plan apply was cancelled")
	default:
		return conflictResult(env, started, "plan apply is already running")
	}
}

func planApplyReasonCode(err error) string {
	switch {
	case errors.Is(err, exact.ErrIngestPlanStale),
		errors.Is(err, exact.ErrIngestPlanConfigChanged),
		errors.Is(err, exact.ErrIngestPlanProtectionChanged),
		errors.Is(err, exact.ErrIngestPlanBlocked),
		errors.Is(err, exact.ErrInvalidIngestPlan),
		errors.Is(err, exact.ErrRestorePlanStale),
		errors.Is(err, exact.ErrInvalidRestorePlan):
		return ReasonCodeConflict
	case errors.Is(err, exact.ErrBlocked):
		return ReasonCodeUnavailable
	case errors.Is(err, exact.ErrUnknownExternalOutcome), errors.Is(err, exact.ErrNeedsReconciliation):
		return ReasonCodeUnknownExternalOutcome
	default:
		return ReasonCodeCatalogError
	}
}

func (d *Dispatcher) handlePlanRevise(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input planReviseInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	planID := firstNonEmpty(input.PlanID, input.PlanRef)
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("plan_id", planID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.PlanDigest) == "" {
		return invalidInputResult(env, started, errString("plan_digest is required"))
	}
	base, err := d.store.GetPlan(ctx, input.WorkspaceID, planID)
	if err != nil {
		if containsNotFound(err) {
			return notFoundResult(env, started, "plan not found")
		}
		return catalogErrorResult(env, started, err)
	}
	if base.PlanDigest != input.PlanDigest {
		return conflictResult(env, started, "plan digest does not match")
	}
	baseBody, err := decodePlanBody(base.Plan)
	if err != nil {
		return catalogErrorResult(env, started, fmt.Errorf("decode plan: %w", err))
	}
	if baseBody.Kind == "ABANDON" {
		return conflictResult(env, started, "cannot revise an abandonment record")
	}
	if baseBody.Kind != "INGEST" || baseBody.Ingest == nil {
		return conflictResult(env, started, "plan.revise currently supports only ingest plans")
	}
	abandoned, err := d.planAbandoned(ctx, input.WorkspaceID, base.ID)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	if abandoned {
		return conflictResult(env, started, "cannot revise an abandoned plan")
	}
	applied, executionExists, err := d.planExecutionStatus(ctx, base)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	if applied || executionExists {
		return conflictResult(env, started, "cannot revise a plan after apply has started")
	}
	fileProtection, decisions, err := reviseIngestProtection(baseBody.Ingest.FileProtection, input.Decisions)
	if err != nil {
		return invalidInputResult(env, started, err)
	}
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	metadataOnlyResolutions, err := metadataOnlyResolutionPaths(baseBody.Ingest.BlockedEntries, decisions)
	if err != nil {
		return invalidInputResult(env, started, err)
	}
	// Revision is still a read-only operation. Re-inspecting the source binds
	// the successor to a fresh capture basis and recomputes every consequence
	// that depends on the per-file protection policy.
	revisedIngest, err := d.exact.InspectIngest(ctx, baseBody.Ingest.Root, exact.IngestOptions{
		ProtectionMode:          baseBody.Ingest.ProtectionMode,
		FileProtection:          fileProtection,
		ConfirmLinkOnly:         baseBody.Ingest.ConfirmLinkOnly,
		ExternalLocators:        append([]exact.IngestLocator(nil), baseBody.Ingest.ExternalLocators...),
		MetadataOnlyResolutions: metadataOnlyResolutions,
	})
	if err != nil {
		if errors.Is(err, exact.ErrBlocked) {
			return conflictResult(env, started, err.Error())
		}
		return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
	}
	// Keep the source identity stable when the inspection found the same
	// configured source. InspectIngest may allocate an ephemeral source ID;
	// the original plan's source is the durable planning reference.
	revisedIngest.SourceID = baseBody.Ingest.SourceID
	successorID, err := sqlite.NewStableID(sqlite.IDPrefixPlan)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	body := baseBody
	body.Schema = planSchemaV2
	body.Applied = false
	body.BasePlanID = base.ID
	body.BasePlanDigest = base.PlanDigest
	body.Decisions = decisions
	body.ConfigDigest = d.effectiveConfigDigest()
	body.SourceBasisDigest = revisedIngest.CaptureBasisDigest
	body.Files = revisedIngest.Estimate.Files
	body.Bytes = revisedIngest.Estimate.Bytes
	body.ProtectionMode = string(revisedIngest.ProtectionMode)
	body.LocalFiles = revisedIngest.Estimate.LocalFiles
	body.LocalBytes = revisedIngest.Estimate.LocalBytes
	body.NewBytes = revisedIngest.Estimate.NewBytes
	body.LinkOnlyFiles = revisedIngest.Estimate.LinkOnlyFiles
	body.LocatorCount = revisedIngest.Estimate.LocatorCount
	body.Ingest = &revisedIngest
	body.Note = "immutable successor retaining the reviewed source and target basis"
	payload, err := json.Marshal(body)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	digest := "sha256:" + hex.EncodeToString(sha256Sum(payload))
	err = d.store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertPlan(ctx, &sqlite.Plan{
			ID:               successorID,
			WorkspaceID:      input.WorkspaceID,
			ScanGenerationID: base.ScanGenerationID,
			Kind:             base.Kind,
			State:            sqlite.PlanReady,
			Plan:             payload,
			PlanDigest:       digest,
		})
	})
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, command.PlanReviseData{
		PlanID:      successorID,
		PlanDigest:  digest,
		WorkspaceID: input.WorkspaceID,
		BasePlanID:  base.ID,
		BaseDigest:  base.PlanDigest,
		Kind:        base.Kind,
		State:       string(sqlite.PlanReady),
		Applied:     false,
		Executable:  planBodyExecutable(body),
		SnapshotRef: body.SnapshotRef,
	})
}

// metadataOnlyResolutionPaths turns only explicit decisions for entries that
// were already retained as blocked into operator approvals. A metadata-only
// mode on an ordinary readable file is just a normal policy change; it does
// not authorize a failed or unstable read.
func metadataOnlyResolutionPaths(blocked []exact.IngestPlanIssue, canonical json.RawMessage) ([]string, error) {
	if len(blocked) == 0 || len(strings.TrimSpace(string(canonical))) == 0 {
		return nil, nil
	}
	blockedPaths := make(map[string]struct{}, len(blocked))
	for _, issue := range blocked {
		blockedPaths[issue.RelativePath] = struct{}{}
	}
	var decisions []planRevisionDecision
	if err := json.Unmarshal(canonical, &decisions); err != nil {
		return nil, fmt.Errorf("decode canonical revision decisions: %w", err)
	}
	paths := make([]string, 0)
	seen := make(map[string]struct{})
	for _, decision := range decisions {
		if decision.Path == "" || strings.ToUpper(strings.TrimSpace(decision.Mode)) != string(sqlite.ProtectionMetadataOnly) {
			continue
		}
		if _, blocked := blockedPaths[decision.Path]; !blocked {
			continue
		}
		if _, duplicate := seen[decision.Path]; duplicate {
			continue
		}
		seen[decision.Path] = struct{}{}
		paths = append(paths, decision.Path)
	}
	sort.Strings(paths)
	return paths, nil
}

func (d *Dispatcher) handlePlanAbandon(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input planAbandonInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	planID := firstNonEmpty(input.PlanID, input.PlanRef)
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("plan_id", planID); err != nil {
		return invalidInputResult(env, started, err)
	}
	record, err := d.store.GetPlan(ctx, input.WorkspaceID, planID)
	if err != nil {
		if containsNotFound(err) {
			return notFoundResult(env, started, "plan not found")
		}
		return catalogErrorResult(env, started, err)
	}
	if input.PlanDigest != "" && record.PlanDigest != input.PlanDigest {
		return conflictResult(env, started, "plan digest does not match")
	}
	body, _ := decodePlanBody(record.Plan)
	if body.Kind == "ABANDON" {
		return conflictResult(env, started, "cannot abandon an abandonment record")
	}
	if marker, ok, err := d.abandonmentOf(ctx, input.WorkspaceID, record.ID); err != nil {
		return catalogErrorResult(env, started, err)
	} else if ok {
		return succeeded(env, started, command.PlanAbandonData{
			PlanID:           marker.ID,
			PlanDigest:       marker.PlanDigest,
			AbandonedPlanID:  record.ID,
			AlreadyAbandoned: true,
		})
	}
	applied, executionExists, err := d.planExecutionStatus(ctx, record)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	if body.Applied || applied || executionExists || (record.State == sqlite.PlanCommitted && body.Kind != "ABANDON") {
		return conflictResult(env, started, "cannot abandon an applied ingest receipt; snapshots stay")
	}
	markerID, err := sqlite.NewStableID(sqlite.IDPrefixPlan)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	markerBody := planBody{
		Schema:              planSchemaV2,
		Kind:                "ABANDON",
		ConfigDigest:        d.effectiveConfigDigest(),
		Applied:             false,
		AbandonedPlanID:     record.ID,
		AbandonedPlanDigest: record.PlanDigest,
		Note:                "marks one unapplied plan abandoned; does not delete source data or published snapshots",
	}
	payload, err := json.Marshal(markerBody)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	digest := "sha256:" + hex.EncodeToString(sha256Sum(payload))
	err = d.store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertPlan(ctx, &sqlite.Plan{
			ID:          markerID,
			WorkspaceID: input.WorkspaceID,
			Kind:        "ABANDON",
			State:       sqlite.PlanCommitted,
			Plan:        payload,
			PlanDigest:  digest,
		})
	})
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, command.PlanAbandonData{
		PlanID:           markerID,
		PlanDigest:       digest,
		AbandonedPlanID:  record.ID,
		AlreadyAbandoned: false,
	})
}

func (d *Dispatcher) ensurePlanningSource(ctx context.Context, inspected *exact.IngestPlan) (string, error) {
	if inspected == nil {
		return "", errors.New("ingest inspection is required")
	}
	workspace, err := d.store.GetWorkspaceByName(ctx, "default")
	createWorkspace := containsNotFound(err)
	if err != nil && !createWorkspace {
		return "", err
	}
	if createWorkspace {
		workspaceID, idErr := sqlite.NewStableID(sqlite.IDPrefixWorkspace)
		if idErr != nil {
			return "", idErr
		}
		workspace = sqlite.Workspace{ID: workspaceID, Name: "default"}
	}
	stableKey := "local-tree:" + inspected.Root
	source, sourceErr := d.store.GetSourceByStableKey(ctx, workspace.ID, stableKey)
	createSource := containsNotFound(sourceErr)
	if sourceErr != nil && !createSource {
		return "", sourceErr
	}
	if createSource {
		if inspected.SourceID == "" {
			return "", errors.New("ingest inspection did not bind a source id")
		}
		source = sqlite.Source{
			ID: inspected.SourceID, WorkspaceID: workspace.ID, StableKey: stableKey,
			Kind: "LOCAL_TREE", Locator: inspected.Root,
			IdentityFingerprint: inspected.Binding.SourceFingerprint(), State: sqlite.SourceActive,
		}
	} else {
		inspected.SourceID = source.ID
	}
	if err := d.store.Update(ctx, func(tx *sqlite.Tx) error {
		if createWorkspace {
			if err := tx.InsertWorkspace(ctx, &workspace); err != nil {
				return err
			}
		}
		if createSource {
			return tx.InsertSource(ctx, &source)
		}
		return nil
	}); err != nil {
		return "", err
	}
	return workspace.ID, nil
}

func (d *Dispatcher) recordIngestPlan(
	ctx context.Context,
	workspaceID string,
	inspected exact.IngestPlan,
) (string, string, string, error) {
	inspected.Binding.BoundAt = time.Time{}
	configDigest := d.effectiveConfigDigest()
	if inspected.ConfigDigest == "" {
		inspected.ConfigDigest = configDigest
	}
	body := planBody{
		Schema: planSchemaV2, Kind: "INGEST", ConfigDigest: configDigest,
		Applied: false, RootID: "", Files: inspected.Estimate.Files, Bytes: inspected.Estimate.Bytes,
		ProtectionMode: string(inspected.ProtectionMode), LocalFiles: inspected.Estimate.LocalFiles,
		LocalBytes: inspected.Estimate.LocalBytes, NewBytes: inspected.Estimate.NewBytes,
		LinkOnlyFiles: inspected.Estimate.LinkOnlyFiles, LocatorCount: inspected.Estimate.LocatorCount,
		SourceBasisDigest: inspected.CaptureBasisDigest, RepositoryRoot: d.exact.Repo.Root(),
		Ingest: &inspected,
		Note:   "read-only source inspection; plan.apply performs repository and publication mutations",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", "", "", err
	}
	digest := "sha256:" + hex.EncodeToString(sha256Sum(payload))
	if existing, lookupErr := d.store.GetPlanByDigest(ctx, workspaceID, digest); lookupErr == nil {
		job, _ := d.store.GetJobByPlanKind(ctx, workspaceID, existing.ID, planInspectJobKind)
		return existing.ID, existing.PlanDigest, job.ID, nil
	}
	planID, err := sqlite.NewStableID(sqlite.IDPrefixPlan)
	if err != nil {
		return "", "", "", err
	}
	jobID, err := sqlite.NewStableID(sqlite.IDPrefixJob)
	if err != nil {
		return "", "", "", err
	}
	resultJSON, err := json.Marshal(map[string]any{
		"plan_id": planID, "plan_digest": digest, "state": sqlite.PlanReady,
		"source_basis_digest": inspected.CaptureBasisDigest,
		"files":               inspected.Estimate.Files, "bytes": inspected.Estimate.Bytes,
		"new_bytes": inspected.Estimate.NewBytes,
	})
	if err != nil {
		return "", "", "", err
	}
	err = d.store.Update(ctx, func(tx *sqlite.Tx) error {
		if err := tx.InsertPlan(ctx, &sqlite.Plan{
			ID: planID, WorkspaceID: workspaceID, Kind: "INGEST", State: sqlite.PlanReady,
			Plan: payload, PlanDigest: digest,
		}); err != nil {
			return err
		}
		if err := tx.InsertJob(ctx, &sqlite.Job{
			ID: jobID, WorkspaceID: workspaceID, PlanID: planID, Kind: planInspectJobKind,
			State: sqlite.JobSucceeded, Attempt: 1, MaxAttempts: 1, Result: resultJSON,
		}); err != nil {
			return err
		}
		details := json.RawMessage(resultJSON)
		for _, action := range []string{"JOB_STARTED", "JOB_SUCCEEDED"} {
			if err := tx.AppendAuditEvent(ctx, &sqlite.AuditEvent{
				ID: mustAuditID(), WorkspaceID: workspaceID, Actor: "restoreweaved",
				Action: action, TargetType: "JOB", TargetID: jobID, RequestID: jobID,
				Attempt: 1, Outcome: "SUCCEEDED", Details: details,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if existing, lookupErr := d.store.GetPlanByDigest(ctx, workspaceID, digest); lookupErr == nil {
			job, _ := d.store.GetJobByPlanKind(ctx, workspaceID, existing.ID, planInspectJobKind)
			return existing.ID, existing.PlanDigest, job.ID, nil
		}
		return "", "", "", err
	}
	return planID, digest, jobID, nil
}

func (d *Dispatcher) recordRestorePlan(ctx context.Context, inspected exact.RestorePlan) (string, string, string, error) {
	publication, err := d.store.GetPublicationBySnapshotRef(ctx, "", inspected.SnapshotRef)
	if err != nil {
		return "", "", "", err
	}
	if publication.ManifestDigest != inspected.ManifestDigest {
		return "", "", "", fmt.Errorf("manifest digest differs from committed publication")
	}
	body := planBody{
		Schema:         planSchemaV2,
		Kind:           "RESTORE",
		ConfigDigest:   d.effectiveConfigDigest(),
		Applied:        false,
		SnapshotRef:    inspected.SnapshotRef,
		ManifestDigest: publication.ManifestDigest,
		RootID:         publication.NamespaceRootID,
		Files:          inspected.Files,
		Bytes:          inspected.Bytes,
		Destination:    inspected.Destination,
		Wrote:          false,
		RepositoryRoot: d.exact.Repo.Root(),
		Restore:        &inspected,
		Note:           "read-only restore preflight; plan.apply is the only destination writer",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", "", "", err
	}
	digest := "sha256:" + hex.EncodeToString(sha256Sum(payload))
	if existing, err := d.store.GetPlanByDigest(ctx, publication.WorkspaceID, digest); err == nil {
		return existing.ID, existing.PlanDigest, publication.WorkspaceID, nil
	}
	planID, err := sqlite.NewStableID(sqlite.IDPrefixPlan)
	if err != nil {
		return "", "", "", err
	}
	err = d.store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertPlan(ctx, &sqlite.Plan{
			ID:               planID,
			WorkspaceID:      publication.WorkspaceID,
			ScanGenerationID: publication.ScanGenerationID,
			Kind:             "RESTORE",
			State:            sqlite.PlanReady,
			Plan:             payload,
			PlanDigest:       digest,
		})
	})
	if err != nil {
		return "", "", "", err
	}
	if existing, lookupErr := d.store.GetPlanByDigest(ctx, publication.WorkspaceID, digest); lookupErr == nil {
		return existing.ID, existing.PlanDigest, publication.WorkspaceID, nil
	}
	return planID, digest, publication.WorkspaceID, nil
}

func mustAuditID() string {
	id, err := sqlite.NewStableID(sqlite.IDPrefixAuditEvent)
	if err != nil {
		return "aud_" + hex.EncodeToString(sha256Sum([]byte(time.Now().String())))[:32]
	}
	return id
}

func (d *Dispatcher) planAbandoned(ctx context.Context, workspaceID, planID string) (bool, error) {
	_, ok, err := d.abandonmentOf(ctx, workspaceID, planID)
	return ok, err
}

func (d *Dispatcher) abandonmentOf(ctx context.Context, workspaceID, planID string) (sqlite.Plan, bool, error) {
	plans, err := d.store.ListPlans(ctx, workspaceID)
	if err != nil {
		return sqlite.Plan{}, false, err
	}
	for _, candidate := range plans {
		body, _ := decodePlanBody(candidate.Plan)
		if body.Kind == "ABANDON" && body.AbandonedPlanID == planID {
			return candidate, true, nil
		}
	}
	return sqlite.Plan{}, false, nil
}

func (d *Dispatcher) planExecutionStatus(ctx context.Context, record sqlite.Plan) (bool, bool, error) {
	body, err := decodePlanBody(record.Plan)
	if err != nil {
		return false, false, err
	}
	if body.Applied {
		return true, true, nil
	}
	job, err := d.store.GetJobByPlanKind(ctx, record.WorkspaceID, record.ID, planApplyJobKind)
	if containsNotFound(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return job.State == sqlite.JobSucceeded, true, nil
}

func projectPlan(record sqlite.Plan, abandoned, applied, executionExists bool) command.PlanGetData {
	body, _ := decodePlanBody(record.Plan)
	state := string(record.State)
	if abandoned {
		state = "ABANDONED"
	}
	return command.PlanGetData{
		PlanID:            record.ID,
		WorkspaceID:       record.WorkspaceID,
		Kind:              record.Kind,
		State:             state,
		PlanDigest:        record.PlanDigest,
		Applied:           applied,
		Executable:        !abandoned && !executionExists && planBodyExecutable(body),
		Abandoned:         abandoned,
		BasePlanID:        body.BasePlanID,
		Plan:              record.Plan,
		CreatedAt:         record.CreatedAt.UTC().Format(time.RFC3339),
		SourceBasisDigest: body.SourceBasisDigest,
	}
}

func decodePlanBody(raw json.RawMessage) (planBody, error) {
	var body planBody
	if len(raw) == 0 {
		return body, nil
	}
	err := json.Unmarshal(raw, &body)
	return body, err
}

// planRevisionDecision is deliberately narrower than the general policy
// model. A revision can change only one captured file's protection mode. It
// cannot silently change the source, global policy, or external locators.
// KEEP is retained as an explicit, no-op review record for older clients
// which used plan.revise only to attach an operator reason.
type planRevisionDecision struct {
	Path     string `json:"path,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func reviseIngestProtection(
	base map[string]sqlite.ProtectionMode,
	raw json.RawMessage,
) (map[string]sqlite.ProtectionMode, json.RawMessage, error) {
	result := make(map[string]sqlite.ProtectionMode, len(base))
	for key, mode := range base {
		result[key] = mode
	}
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return result, json.RawMessage("[]"), nil
	}
	var decisions []planRevisionDecision
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decisions); err != nil {
		return nil, nil, fmt.Errorf("decisions must be an array of typed file decisions: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, nil, errors.New("decisions must contain exactly one JSON array")
		}
		return nil, nil, fmt.Errorf("invalid trailing decisions JSON: %w", err)
	}
	if decisions == nil {
		return nil, nil, errors.New("decisions must be an array, not null")
	}
	seenPaths := make(map[string]struct{}, len(decisions))
	for index, decision := range decisions {
		if strings.TrimSpace(decision.Reason) == "" && decision.Decision == "" {
			// A reason is optional for a mode change, but an empty object is not
			// a decision and must not be accepted as an implicit policy change.
			if decision.Path == "" || decision.Mode == "" {
				return nil, nil, fmt.Errorf("decisions[%d] requires path and mode", index)
			}
		}
		if decision.Decision != "" {
			if strings.ToUpper(strings.TrimSpace(decision.Decision)) != "KEEP" || decision.Path != "" || decision.Mode != "" {
				return nil, nil, fmt.Errorf("decisions[%d] supports only decision KEEP without path or mode", index)
			}
			decisions[index].Decision = "KEEP"
			continue
		}
		if decision.Path == "" {
			return nil, nil, fmt.Errorf("decisions[%d] path is required", index)
		}
		cleanPath, err := revisePath(decision.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("decisions[%d]: %w", index, err)
		}
		if _, exists := seenPaths[cleanPath]; exists {
			return nil, nil, fmt.Errorf("decisions[%d]: path %q is repeated", index, cleanPath)
		}
		seenPaths[cleanPath] = struct{}{}
		mode, err := reviseProtectionMode(decision.Mode)
		if err != nil {
			return nil, nil, fmt.Errorf("decisions[%d]: %w", index, err)
		}
		result[cleanPath] = mode
		decisions[index].Path = cleanPath
		decisions[index].Mode = string(mode)
	}
	// Canonicalize the decision record before it is included in the successor
	// digest. The actual consequences are in IngestPlan; this is the durable
	// review input and must be deterministic too.
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].Path == decisions[j].Path {
			return decisions[i].Mode < decisions[j].Mode
		}
		return decisions[i].Path < decisions[j].Path
	})
	canonical, err := json.Marshal(decisions)
	if err != nil {
		return nil, nil, err
	}
	return result, canonical, nil
}

func revisePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	cleaned := path.Clean(value)
	if value == "" || cleaned == "." || path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("path must be a safe path relative to the capture root")
	}
	return cleaned, nil
}

func reviseProtectionMode(value string) (sqlite.ProtectionMode, error) {
	normalized := sqlite.ProtectionMode(strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", "_")))
	switch normalized {
	case sqlite.ProtectionStoreExact, sqlite.ProtectionStoreExactWithExternalFallback,
		sqlite.ProtectionLinkOnly, sqlite.ProtectionMetadataOnly:
		return normalized, nil
	default:
		return "", fmt.Errorf("unknown protection mode %q", value)
	}
}

func sha256Sum(payload []byte) []byte {
	sum := sha256.Sum256(payload)
	return sum[:]
}
