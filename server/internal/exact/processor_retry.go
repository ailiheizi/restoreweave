package exact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const (
	processorRetryJobKind  = "PROCESSOR_RETRY"
	processorRetrySchemaV1 = "org.restoreweave.processor-retry-job.v1"
)

type ProcessorRetryTarget struct {
	SubjectRef           string `json:"subject_ref"`
	RouteDigest          string `json:"route_digest"`
	Stage                string `json:"stage"`
	CapabilityID         string `json:"capability_id"`
	PredecessorAttemptID string `json:"predecessor_attempt_id"`
	ReasonCode           string `json:"reason_code"`
}

type ProcessorRetryInvocation struct {
	JobID          string
	Owner          string
	Attempt        int64
	IdempotencyKey string
	LeaseToken     string
	FenceToken     int64
	Targets        []ProcessorRetryTarget
}

type processorRetryTargetSource interface {
	ProcessorRetryTargets() []ProcessorRetryTarget
}

type publicationRetryProcessor interface {
	RetryPublication(context.Context, string, string, string, ProcessorRetryInvocation) error
}

type processorRetryJobInput struct {
	Schema                  string                 `json:"schema"`
	WorkspaceID             string                 `json:"workspace_id"`
	SnapshotRef             string                 `json:"snapshot_ref"`
	NamespaceRootID         string                 `json:"namespace_root_id"`
	ParentPublicationDigest string                 `json:"parent_publication_digest"`
	PlanDigest              string                 `json:"plan_digest"`
	IdempotencyKey          string                 `json:"idempotency_key"`
	Targets                 []ProcessorRetryTarget `json:"targets"`
}

type ProcessorRetryWorkerOptions struct {
	Owner        string
	PollInterval time.Duration
	LeaseTTL     time.Duration
	BatchSize    int
	Now          func() time.Time
	OnError      func(error)
}

type processorRetryLease struct {
	cancel context.CancelFunc
	done   <-chan error
}

func (s *Service) scheduleProcessorRetry(ctx context.Context, result IngestResult, planDigest string, processErr error) error {
	if s == nil || s.Store == nil || processErr == nil || strings.TrimSpace(planDigest) == "" {
		return nil
	}
	if _, ok := s.Processor.(publicationRetryProcessor); !ok {
		return nil
	}
	source, ok := processErr.(processorRetryTargetSource)
	if !ok {
		return nil
	}
	targets := source.ProcessorRetryTargets()
	if len(targets) == 0 {
		return nil
	}
	planID := ""
	if plan, err := s.Store.GetPlanByDigest(ctx, result.WorkspaceID, planDigest); err == nil {
		planID = plan.ID
	} else if !errors.Is(err, sqlite.ErrNotFound) {
		return fmt.Errorf("load processor retry plan: %w", err)
	}
	input := processorRetryJobInput{
		Schema: processorRetrySchemaV1, WorkspaceID: result.WorkspaceID,
		SnapshotRef: result.SnapshotRef, NamespaceRootID: result.RootID,
		ParentPublicationDigest: result.PublicationCommitDigest, PlanDigest: planDigest,
		IdempotencyKey: "processor-retry:" + planDigest, Targets: targets,
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	jobDigest := sha256.Sum256([]byte(processorRetryJobKind + "\x00" + result.WorkspaceID + "\x00" + planDigest))
	jobID := sqlite.IDPrefixJob + "_" + hex.EncodeToString(jobDigest[:16])
	job := sqlite.Job{
		ID: jobID, WorkspaceID: result.WorkspaceID, PlanID: planID,
		Kind: processorRetryJobKind, State: sqlite.JobQueued, Input: payload,
		MaxAttempts: 3,
	}
	if err := s.Store.Update(ctx, func(tx *sqlite.Tx) error { return tx.InsertJob(ctx, &job) }); err != nil && !errors.Is(err, sqlite.ErrConflict) {
		return fmt.Errorf("schedule processor retry: %w", err)
	}
	return nil
}

func (s *Service) RunProcessorRetryWorker(ctx context.Context, options ProcessorRetryWorkerOptions) error {
	processor, ok := s.Processor.(publicationRetryProcessor)
	if !ok {
		return nil
	}
	if s.Store == nil {
		return errors.New("processor retry worker requires catalog")
	}
	if strings.TrimSpace(options.Owner) == "" {
		return errors.New("processor retry worker owner is required")
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = time.Minute
	}
	if options.BatchSize <= 0 || options.BatchSize > 100 {
		options.BatchSize = 8
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	ticker := time.NewTicker(options.PollInterval)
	defer ticker.Stop()
	for {
		if err := s.runProcessorRetryBatch(ctx, processor, options); err != nil && !errors.Is(err, context.Canceled) && options.OnError != nil {
			options.OnError(err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Service) runProcessorRetryBatch(ctx context.Context, processor publicationRetryProcessor, options ProcessorRetryWorkerOptions) error {
	now := options.Now().UTC()
	jobs, err := s.Store.ListClaimableJobs(ctx, processorRetryJobKind, now, options.BatchSize)
	if err != nil {
		return err
	}
	for _, candidate := range jobs {
		if err := s.runProcessorRetryJob(ctx, processor, options, candidate); err != nil && !errors.Is(err, sqlite.ErrConflict) {
			return err
		}
	}
	return nil
}

func (s *Service) runProcessorRetryJob(ctx context.Context, processor publicationRetryProcessor, options ProcessorRetryWorkerOptions, candidate sqlite.Job) error {
	var input processorRetryJobInput
	if err := decodeStrictRecord(candidate.Input, &input); err != nil || input.Schema != processorRetrySchemaV1 ||
		input.WorkspaceID != candidate.WorkspaceID || input.SnapshotRef == "" || input.NamespaceRootID == "" ||
		input.ParentPublicationDigest == "" || input.PlanDigest == "" || input.IdempotencyKey == "" || len(input.Targets) == 0 {
		return s.finishProcessorRetryJob(ctx, candidate, sqlite.JobFailed, "PROCESSOR_RETRY_INPUT_INVALID", candidate.Attempt)
	}
	leaseToken := fmt.Sprintf("%s:%s:%d", options.Owner, candidate.ID, candidate.FencingToken+1)
	now := options.Now().UTC()
	var fence int64
	err := s.Store.Update(ctx, func(tx *sqlite.Tx) error {
		var acquireErr error
		fence, acquireErr = tx.AcquireJobLease(ctx, candidate.WorkspaceID, candidate.ID, candidate.Revision, options.Owner, leaseToken, now, now.Add(options.LeaseTTL))
		return acquireErr
	})
	if err != nil {
		return err
	}
	job, err := s.Store.GetJob(ctx, candidate.WorkspaceID, candidate.ID)
	if err != nil {
		return err
	}
	leaseCtx, lease := s.startProcessorRetryLease(ctx, job, options, leaseToken, fence)
	defer lease.cancel()
	finish := func(state sqlite.JobState, code string) error {
		if err := lease.stop(); err != nil {
			return err
		}
		if err := s.validateProcessorRetryLease(ctx, job, options.Owner, leaseToken, fence, options.Now().UTC()); err != nil {
			return err
		}
		return s.finishProcessorRetryJob(ctx, job, state, code, job.Attempt)
	}
	if candidate.State == sqlite.JobNeedsReconcile {
		if err := s.validateProcessorRetryLease(leaseCtx, job, options.Owner, leaseToken, fence, options.Now().UTC()); err != nil {
			return err
		}
		if err := s.PublishProcessorAttemptClosure(leaseCtx, input.WorkspaceID, input.SnapshotRef, input.ParentPublicationDigest); err != nil {
			return finish(sqlite.JobNeedsReconcile, "PROCESSOR_RETRY_RECONCILIATION_UNKNOWN")
		}
	}
	processErr := processor.RetryPublication(leaseCtx, input.WorkspaceID, input.SnapshotRef, input.NamespaceRootID, ProcessorRetryInvocation{
		JobID: candidate.ID, Owner: options.Owner, Attempt: job.Attempt, IdempotencyKey: input.IdempotencyKey,
		LeaseToken: leaseToken, FenceToken: fence, Targets: append([]ProcessorRetryTarget(nil), input.Targets...),
	})
	if err := s.validateProcessorRetryLease(leaseCtx, job, options.Owner, leaseToken, fence, options.Now().UTC()); err != nil {
		return err
	}
	if err := s.PublishProcessorAttemptClosure(leaseCtx, input.WorkspaceID, input.SnapshotRef, input.ParentPublicationDigest); err != nil {
		if errors.Is(err, ErrUnknownExternalOutcome) || errors.Is(err, ErrNeedsReconciliation) {
			return finish(sqlite.JobNeedsReconcile, "PROCESSOR_RETRY_PUBLICATION_UNKNOWN")
		}
		return finish(sqlite.JobFailed, "PROCESSOR_RETRY_PUBLICATION_FAILED")
	}
	if err := s.validateProcessorRetryLease(leaseCtx, job, options.Owner, leaseToken, fence, options.Now().UTC()); err != nil {
		return err
	}
	if err := s.PublishPortableFactClosure(leaseCtx, input.WorkspaceID, input.SnapshotRef, input.ParentPublicationDigest); err != nil {
		if errors.Is(err, ErrUnknownExternalOutcome) || errors.Is(err, ErrNeedsReconciliation) {
			return finish(sqlite.JobNeedsReconcile, "PROCESSOR_RETRY_FACT_PUBLICATION_UNKNOWN")
		}
		return finish(sqlite.JobFailed, "PROCESSOR_RETRY_FACT_PUBLICATION_FAILED")
	}
	if processErr == nil {
		return finish(sqlite.JobSucceeded, "")
	}
	if source, ok := processErr.(processorRetryTargetSource); ok && len(source.ProcessorRetryTargets()) > 0 && job.Attempt < job.MaxAttempts {
		return finish(sqlite.JobQueued, "PROCESSOR_RETRYABLE_FAILURE")
	}
	return finish(sqlite.JobFailed, "PROCESSOR_RETRY_EXHAUSTED")
}

func (s *Service) validateProcessorRetryLease(ctx context.Context, job sqlite.Job, owner, leaseToken string, fence int64, now time.Time) error {
	if err := s.Store.ValidateJobLease(ctx, job.WorkspaceID, job.ID, owner, leaseToken, fence, now); err != nil {
		return fmt.Errorf("validate processor retry lease: %w", err)
	}
	return nil
}

func (s *Service) startProcessorRetryLease(ctx context.Context, job sqlite.Job, options ProcessorRetryWorkerOptions, leaseToken string, fence int64) (context.Context, *processorRetryLease) {
	leaseCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	interval := options.LeaseTTL / 3
	if interval <= 0 {
		interval = time.Millisecond
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				done <- nil
				return
			case <-ticker.C:
				now := options.Now().UTC()
				if err := s.Store.RenewJobLease(leaseCtx, job.WorkspaceID, job.ID, options.Owner, leaseToken, fence, now, now.Add(options.LeaseTTL)); err != nil {
					cancel()
					done <- fmt.Errorf("renew processor retry lease: %w", err)
					return
				}
			}
		}
	}()
	return leaseCtx, &processorRetryLease{cancel: cancel, done: done}
}

func (lease *processorRetryLease) stop() error {
	lease.cancel()
	return <-lease.done
}

func (s *Service) finishProcessorRetryJob(ctx context.Context, job sqlite.Job, state sqlite.JobState, code string, attempt int64) error {
	return s.Store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.UpdateJob(ctx, sqlite.JobUpdate{
			WorkspaceID: job.WorkspaceID, JobID: job.ID, ExpectedRevision: job.Revision,
			State: state, ErrorCode: code, Attempt: attempt,
		})
	})
}
