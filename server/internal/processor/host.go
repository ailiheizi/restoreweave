package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	exactcore "github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/identify"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

var ErrUnavailable = errors.New("processor host is not configured")

type Options struct {
	StagingDir     string
	MaxSourceBytes int64
	StageTimeout   time.Duration
	MaxConcurrent  int
	Identify       *identify.Detector
	Processors     []Processor
	Now            func() time.Time
}

// Host runs host-owned identification routing and RUN_STAGE admission.
// Exact ingest must not depend on this host succeeding.
type Host struct {
	Store  *sqlite.Store
	Repo   repository.Driver
	opts   Options
	pool   *pool
	byCap  map[string]Processor
	detect *identify.Detector
}

type attemptContext struct {
	fenceToken           int64
	retryJobID           string
	retryOwner           string
	retryAttempt         int64
	retryIdempotencyKey  string
	retryLeaseToken      string
	predecessorAttemptID string
}

type Report struct {
	Attempted  int
	Admitted   int
	Rejected   int
	Skipped    int
	Failed     int
	Cancelled  int
	IssueCount int
	Issues     []SubjectIssue
}

const maxReportedSubjectIssues = 256

// SubjectIssue is a bounded, user-visible failure from one post-publication
// processor branch. It never changes the subject's exact protection claim.
type SubjectIssue struct {
	SubjectRef  string       `json:"subject_ref"`
	ContentID   string       `json:"content_id,omitempty"`
	DisplayName string       `json:"display_name,omitempty"`
	Stage       Stage        `json:"stage,omitempty"`
	Capability  string       `json:"capability_id,omitempty"`
	Status      ResultStatus `json:"status"`
	ReasonCode  string       `json:"reason_code"`
	Message     string       `json:"message,omitempty"`
}

// PublicationIssuesError lets the exact lane expose per-subject degradation
// while keeping publication successful and independently recoverable.
type PublicationIssuesError struct {
	Count        int
	Issues       []SubjectIssue
	retryTargets []exactcore.ProcessorRetryTarget
}

func (e *PublicationIssuesError) ProcessorRetryTargets() []exactcore.ProcessorRetryTarget {
	if e == nil {
		return nil
	}
	return append([]exactcore.ProcessorRetryTarget(nil), e.retryTargets...)
}

func (e *PublicationIssuesError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%d post-publication processor issue(s)", e.Count)
}

// PublicationWarnings is intentionally a small structural interface consumed
// by exact without importing this package.
func (e *PublicationIssuesError) PublicationWarnings() []string {
	if e == nil {
		return nil
	}
	warnings := make([]string, 0, len(e.Issues)+1)
	for _, issue := range e.Issues {
		message := fmt.Sprintf("subject %s", issue.SubjectRef)
		if issue.DisplayName != "" {
			message += fmt.Sprintf(" (%q)", boundedIssueText(issue.DisplayName))
		}
		if issue.Capability != "" {
			message += " " + boundedIssueText(issue.Capability)
		}
		message += ": " + boundedIssueText(issue.ReasonCode)
		if issue.Message != "" {
			message += ": " + boundedIssueText(issue.Message)
		}
		warnings = append(warnings, message)
	}
	if omitted := e.Count - len(e.Issues); omitted > 0 {
		warnings = append(warnings, fmt.Sprintf("%d additional processor issue(s) omitted", omitted))
	}
	return warnings
}

func boundedIssueText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > 512 {
		return string(runes[:512]) + "..."
	}
	return value
}

func NewHost(store *sqlite.Store, repo repository.Driver, opts Options) *Host {
	if opts.MaxSourceBytes <= 0 {
		opts.MaxSourceBytes = defaultMaxSourceBytes
	}
	if opts.StageTimeout <= 0 {
		opts.StageTimeout = defaultStageTimeout
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = defaultMaxConcurrent
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if strings.TrimSpace(opts.StagingDir) == "" && repo != nil && repo.Root() != "" && repo.Root() != ":memory:" {
		opts.StagingDir = filepath.Join(repo.Root(), "staging")
	}
	processors := opts.Processors
	if processors == nil {
		processors = DefaultProcessors()
	}
	byCap := make(map[string]Processor, len(processors))
	for _, item := range processors {
		if item != nil {
			byCap[item.CapabilityID()] = item
		}
	}
	detect := opts.Identify
	if detect == nil {
		detect = identify.NewDetector(0)
	}
	return &Host{
		Store:  store,
		Repo:   repo,
		opts:   opts,
		pool:   newPool(opts.MaxConcurrent, opts.StageTimeout),
		byCap:  byCap,
		detect: detect,
	}
}

// ProcessPublication runs optional processing after exact publication.
// Per-subject failures are returned as a typed degradation report. The exact
// publication is already committed and is never rolled back by this error.
func (h *Host) ProcessPublication(ctx context.Context, workspaceID, snapshotRef, rootID string) error {
	report, err := h.Process(ctx, workspaceID, snapshotRef, rootID)
	if err != nil {
		return err
	}
	if report.IssueCount == 0 {
		return nil
	}
	targets, targetErr := h.processorRetryTargets(ctx, workspaceID, snapshotRef, report.Issues)
	if targetErr != nil {
		return targetErr
	}
	return &PublicationIssuesError{Count: report.IssueCount, Issues: append([]SubjectIssue(nil), report.Issues...), retryTargets: targets}
}

// RetryPublication reruns only the explicitly fenced terminal failures named
// by the retry job. Successful and inapplicable nodes are never rerun.
func (h *Host) RetryPublication(ctx context.Context, workspaceID, snapshotRef, rootID string, invocation exactcore.ProcessorRetryInvocation) error {
	if h == nil || h.Store == nil || h.Repo == nil {
		return ErrUnavailable
	}
	if invocation.JobID == "" || invocation.Owner == "" || invocation.Attempt < 1 || invocation.IdempotencyKey == "" ||
		invocation.LeaseToken == "" || invocation.FenceToken < 1 || len(invocation.Targets) == 0 {
		return errors.New("processor retry invocation is incomplete")
	}
	nodes, err := h.Store.ListNamespaceSubtree(ctx, workspaceID, rootID, "")
	if err != nil {
		return err
	}
	entries := make(map[string]sqlite.NamespaceEntry, len(nodes))
	for _, node := range nodes {
		entries[node.Entry.ID] = node.Entry
	}
	attempts, err := h.Store.ListProcessorAttempts(ctx, workspaceID, snapshotRef)
	if err != nil {
		return err
	}
	latest := make(map[string]sqlite.ProcessorAttempt)
	for _, attempt := range attempts {
		key := processorAttemptKey(attempt.SubjectRef, attempt.RouteDigest, attempt.Stage, attempt.CapabilityID)
		latest[key] = attempt
	}
	var issues []SubjectIssue
	for _, target := range invocation.Targets {
		key := processorAttemptKey(target.SubjectRef, target.RouteDigest, target.Stage, target.CapabilityID)
		previous, ok := latest[key]
		if !ok {
			return fmt.Errorf("processor retry target %s has no terminal predecessor", target.SubjectRef)
		}
		if previous.Status == string(StatusSucceeded) || previous.Status == string(StatusInapplicable) {
			continue
		}
		if previous.ID != target.PredecessorAttemptID && !retryAttemptBelongsToJob(previous.Provenance, invocation.JobID, invocation.IdempotencyKey) {
			return errors.New("processor retry target has an unrelated terminal successor")
		}
		if retryAttemptAlreadyRecorded(previous.Provenance, invocation.JobID, invocation.Attempt, invocation.IdempotencyKey) {
			issues = append(issues, SubjectIssue{
				SubjectRef: previous.SubjectRef, Stage: Stage(previous.Stage), Capability: previous.CapabilityID,
				Status: ResultStatus(previous.Status), ReasonCode: previous.ReasonCode, Message: previous.Reason,
			})
			continue
		}
		if err := h.Store.ValidateJobLease(ctx, workspaceID, invocation.JobID, invocation.Owner, invocation.LeaseToken, invocation.FenceToken, h.opts.Now().UTC()); err != nil {
			return fmt.Errorf("validate processor retry lease: %w", err)
		}
		entry, ok := entries[target.SubjectRef]
		if !ok {
			return errors.New("processor retry target is outside the published namespace root")
		}
		source, err := openSource(ctx, h.Repo, entry.ContentID, h.opts.MaxSourceBytes)
		if err != nil {
			return err
		}
		probe, err := source.ReadAll(ctx)
		if err != nil {
			source.Close()
			return err
		}
		identified, err := h.detect.Detect(ctx, entry.DisplayName, probe)
		if err != nil {
			source.Close()
			return err
		}
		classification := ClassificationRecord{State: identified.State, Evidence: identified.Evidence}
		route := buildProcessingRoute(classification)
		if route.Digest() != target.RouteDigest {
			source.Close()
			return errors.New("processor retry route changed from authenticated predecessor")
		}
		var node *RouteNode
		for index := range route.Nodes {
			candidate := &route.Nodes[index]
			if string(candidate.Stage) == target.Stage && candidate.CapabilityID == target.CapabilityID {
				node = candidate
				break
			}
		}
		if node == nil {
			source.Close()
			return errors.New("processor retry node is absent from authenticated route")
		}
		proc, ok := h.byCap[node.CapabilityID]
		if !ok {
			source.Close()
			return errors.New("processor retry capability is unavailable")
		}
		status, runErr := h.admitNode(ctx, workspaceID, snapshotRef, entry, source, classification, route, *node, proc, attemptContext{
			fenceToken: invocation.FenceToken, retryJobID: invocation.JobID,
			retryOwner:   invocation.Owner,
			retryAttempt: invocation.Attempt, retryIdempotencyKey: invocation.IdempotencyKey,
			retryLeaseToken: invocation.LeaseToken, predecessorAttemptID: previous.ID,
		})
		source.Close()
		if status == StatusFailed || status == StatusCancelled || runErr != nil {
			message := "processor retry did not produce an admitted artifact"
			if runErr != nil {
				message = runErr.Error()
			}
			issues = append(issues, SubjectIssue{
				SubjectRef: entry.ID, ContentID: entry.ContentID, DisplayName: entry.DisplayName,
				Stage: node.Stage, Capability: node.CapabilityID, Status: status,
				ReasonCode: "PROCESSOR_RETRY_FAILED", Message: message,
			})
		}
	}
	if len(issues) == 0 {
		return nil
	}
	targets, err := h.processorRetryTargets(ctx, workspaceID, snapshotRef, issues)
	if err != nil {
		return err
	}
	return &PublicationIssuesError{Count: len(issues), Issues: issues, retryTargets: targets}
}

func retryAttemptBelongsToJob(provenance json.RawMessage, jobID, idempotencyKey string) bool {
	var value struct {
		RetryJobID          string `json:"retry_job_id"`
		RetryIdempotencyKey string `json:"retry_idempotency_key"`
	}
	return json.Unmarshal(provenance, &value) == nil && value.RetryJobID == jobID && value.RetryIdempotencyKey == idempotencyKey
}

func processorAttemptKey(subject, route, stage, capability string) string {
	return subject + "\x00" + route + "\x00" + stage + "\x00" + capability
}

func retryAttemptAlreadyRecorded(provenance json.RawMessage, jobID string, attempt int64, idempotencyKey string) bool {
	var value struct {
		RetryJobID          string `json:"retry_job_id"`
		RetryAttempt        int64  `json:"retry_attempt"`
		RetryIdempotencyKey string `json:"retry_idempotency_key"`
	}
	return json.Unmarshal(provenance, &value) == nil && value.RetryJobID == jobID &&
		value.RetryAttempt == attempt && value.RetryIdempotencyKey == idempotencyKey
}

func (h *Host) processorRetryTargets(ctx context.Context, workspaceID, snapshotRef string, issues []SubjectIssue) ([]exactcore.ProcessorRetryTarget, error) {
	attempts, err := h.Store.ListProcessorAttempts(ctx, workspaceID, snapshotRef)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]sqlite.ProcessorAttempt)
	for _, attempt := range attempts {
		key := processorAttemptKey(attempt.SubjectRef, attempt.RouteDigest, attempt.Stage, attempt.CapabilityID)
		latest[key] = attempt
	}
	targets := make([]exactcore.ProcessorRetryTarget, 0, len(issues))
	seen := make(map[string]struct{})
	for _, issue := range issues {
		if issue.SubjectRef == "" || issue.Stage == "" || issue.Capability == "" ||
			(issue.Status != StatusFailed && issue.Status != StatusCancelled) {
			continue
		}
		for key, attempt := range latest {
			if attempt.SubjectRef != issue.SubjectRef || attempt.Stage != string(issue.Stage) || attempt.CapabilityID != issue.Capability ||
				(attempt.Status != string(StatusFailed) && attempt.Status != string(StatusCancelled)) {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			targets = append(targets, exactcore.ProcessorRetryTarget{
				SubjectRef: attempt.SubjectRef, RouteDigest: attempt.RouteDigest, Stage: attempt.Stage,
				CapabilityID: attempt.CapabilityID, PredecessorAttemptID: attempt.ID, ReasonCode: attempt.ReasonCode,
			})
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		a, b := targets[i], targets[j]
		if a.SubjectRef != b.SubjectRef {
			return a.SubjectRef < b.SubjectRef
		}
		if a.RouteDigest != b.RouteDigest {
			return a.RouteDigest < b.RouteDigest
		}
		if a.Stage != b.Stage {
			return a.Stage < b.Stage
		}
		return a.CapabilityID < b.CapabilityID
	})
	return targets, nil
}

func (h *Host) Process(ctx context.Context, workspaceID, snapshotRef, rootID string) (Report, error) {
	var report Report
	if h == nil || h.Store == nil || h.Repo == nil {
		return report, ErrUnavailable
	}
	nodes, err := h.Store.ListNamespaceSubtree(ctx, workspaceID, rootID, "")
	if err != nil {
		return report, err
	}
	var mu sync.Mutex
	for _, node := range nodes {
		entry := node.Entry
		if entry.EntryType != sqlite.EntryFile || entry.ContentID == "" {
			continue
		}
		protection, protectionErr := h.Store.GetProtectionRecordBySubject(ctx, workspaceID, entry.ID)
		if protectionErr == nil && protection.LocalRepresentationID == "" {
			switch protection.Mode {
			case sqlite.ProtectionLinkOnly, sqlite.ProtectionMetadataOnly:
				// These subjects intentionally have no local payload. Their
				// absence is a policy outcome, not a processor failure.
				report.Skipped++
				continue
			default:
				report.Failed++
				appendSubjectIssue(&report, SubjectIssue{
					SubjectRef: entry.ID, ContentID: entry.ContentID, DisplayName: entry.DisplayName,
					Status: StatusFailed, ReasonCode: "LOCAL_REPRESENTATION_UNAVAILABLE",
					Message: "exact protection record has no local representation",
				})
				continue
			}
		}
		if protectionErr != nil && !errors.Is(protectionErr, sqlite.ErrNotFound) {
			return report, protectionErr
		}
		if err := ctx.Err(); err != nil {
			mu.Lock()
			report.Cancelled++
			appendSubjectIssue(&report, SubjectIssue{
				SubjectRef: entry.ID, ContentID: entry.ContentID, DisplayName: entry.DisplayName,
				Status: StatusCancelled, ReasonCode: "PROCESSOR_HOST_CANCELLED", Message: err.Error(),
			})
			mu.Unlock()
			continue
		}
		err := h.pool.run(ctx, func(runCtx context.Context) error {
			outcome, issues, processErr := h.processEntry(runCtx, workspaceID, snapshotRef, entry)
			mu.Lock()
			defer mu.Unlock()
			report.Attempted++
			for _, issue := range issues {
				appendSubjectIssue(&report, issue)
			}
			if processErr != nil && len(issues) == 0 {
				appendSubjectIssue(&report, SubjectIssue{
					SubjectRef: entry.ID, ContentID: entry.ContentID, DisplayName: entry.DisplayName,
					Status: StatusFailed, ReasonCode: "PROCESSOR_SUBJECT_FAILED", Message: processErr.Error(),
				})
			}
			switch outcome {
			case StatusSucceeded:
				report.Admitted++
			case StatusInapplicable:
				report.Skipped++
			case StatusCancelled:
				report.Cancelled++
			default:
				report.Failed++
			}
			return nil
		})
		if err != nil {
			mu.Lock()
			report.Failed++
			appendSubjectIssue(&report, SubjectIssue{
				SubjectRef: entry.ID, ContentID: entry.ContentID, DisplayName: entry.DisplayName,
				Status: StatusFailed, ReasonCode: "PROCESSOR_HOST_FAILED", Message: err.Error(),
			})
			mu.Unlock()
		}
	}
	return report, nil
}

func appendSubjectIssue(report *Report, issue SubjectIssue) {
	if report == nil {
		return
	}
	report.IssueCount++
	if len(report.Issues) < maxReportedSubjectIssues {
		report.Issues = append(report.Issues, issue)
	}
}

func (h *Host) processEntry(ctx context.Context, workspaceID, snapshotRef string, entry sqlite.NamespaceEntry) (ResultStatus, []SubjectIssue, error) {
	source, err := openSource(ctx, h.Repo, entry.ContentID, h.opts.MaxSourceBytes)
	if err != nil {
		return StatusFailed, nil, err
	}
	defer source.Close()

	probe, err := source.ReadAll(ctx)
	if err != nil {
		return StatusFailed, nil, err
	}
	identified, err := h.detect.Detect(ctx, entry.DisplayName, probe)
	if err != nil {
		return StatusFailed, nil, err
	}
	classification := ClassificationRecord{State: identified.State, Evidence: identified.Evidence}
	route := buildProcessingRoute(classification)
	if len(route.Nodes) == 0 {
		return StatusInapplicable, nil, nil
	}
	last := StatusInapplicable
	admitted := false
	var issues []SubjectIssue
	for _, node := range route.Nodes {
		proc, ok := h.byCap[node.CapabilityID]
		if !ok {
			attemptID, attemptErr := sqlite.NewStableID(sqlite.IDPrefixAttempt)
			if attemptErr != nil {
				return StatusFailed, issues, attemptErr
			}
			if attemptErr := h.recordProcessorAttempt(context.WithoutCancel(ctx), attemptID, workspaceID, snapshotRef,
				entry, classification, route, node, StatusInapplicable,
				"CAPABILITY_NOT_CONFIGURED", "capability is not configured", h.opts.Now().UTC()); attemptErr != nil {
				return StatusFailed, issues, attemptErr
			}
			continue
		}
		status, err := h.admitNode(ctx, workspaceID, snapshotRef, entry, source, classification, route, node, proc, attemptContext{fenceToken: 1})
		if err != nil && status == "" {
			status = StatusFailed
		}
		last = status
		if status == StatusSucceeded {
			admitted = true
		} else if status == StatusFailed || status == StatusCancelled {
			reasonCode := "PROCESSOR_STAGE_FAILED"
			if status == StatusCancelled {
				reasonCode = "PROCESSOR_STAGE_CANCELLED"
			}
			message := "processor did not produce an admitted artifact"
			if err != nil {
				message = err.Error()
			}
			issues = append(issues, SubjectIssue{
				SubjectRef: entry.ID, ContentID: entry.ContentID, DisplayName: entry.DisplayName,
				Stage: node.Stage, Capability: node.CapabilityID, Status: status,
				ReasonCode: reasonCode, Message: message,
			})
		}
	}
	if admitted {
		return StatusSucceeded, issues, nil
	}
	return last, issues, nil
}

func (h *Host) admitNode(ctx context.Context, workspaceID, snapshotRef string, entry sqlite.NamespaceEntry, source *SourceHandle, classification ClassificationRecord, route Route, node RouteNode, proc Processor, attemptCtx attemptContext) (status ResultStatus, err error) {
	if attemptCtx.fenceToken < 1 {
		return StatusFailed, errors.New("processor attempt fence token must be positive")
	}
	var attemptID string
	var processorReason string
	var admittedArtifact *sqlite.ProcessorArtifact
	startedAt := h.opts.Now().UTC()
	defer func() {
		if rec := recover(); rec != nil {
			status = StatusFailed
			err = fmt.Errorf("processor panic: %v", rec)
		}
		if attemptID == "" {
			return
		}
		if status == "" {
			status = StatusFailed
		}
		reasonCode, reason := processorAttemptReason(status, processorReason, err)
		artifact := admittedArtifact
		if status != StatusSucceeded {
			artifact = nil
		}
		if recordErr := h.recordProcessorAttemptWithArtifact(context.WithoutCancel(ctx), attemptID, workspaceID, snapshotRef,
			entry, classification, route, node, status, reasonCode, reason, startedAt, artifact, attemptCtx); recordErr != nil {
			status = StatusFailed
			if err == nil {
				err = recordErr
			}
		}
	}()

	attemptID, err = sqlite.NewStableID(sqlite.IDPrefixAttempt)
	if err != nil {
		return StatusFailed, err
	}
	stagingDir := h.opts.StagingDir
	if stagingDir == "" {
		stagingDir = filepath.Join(h.Repo.Root(), "staging")
	}
	staging, err := openStaging(stagingDir, attemptID, h.opts.MaxSourceBytes)
	if err != nil {
		return StatusFailed, err
	}
	defer staging.Close()

	result, runErr := proc.RunStage(ctx, Invocation{
		AttemptID:      attemptID,
		FenceToken:     attemptCtx.fenceToken,
		Route:          route,
		Node:           node,
		Classification: classification,
		Source:         source,
		Staging:        staging,
		MaxOutputBytes: h.opts.MaxSourceBytes,
	})
	if runErr != nil {
		if result.Reason == "" {
			result.Reason = runErr.Error()
		}
		if result.Status == "" {
			result.Status = StatusFailed
		}
	}
	processorReason = result.Reason
	if result.Status != StatusSucceeded {
		return result.Status, nil
	}
	if !result.Sealed {
		return StatusFailed, nil
	}
	body, digest, err := staging.sealedBytes()
	if err != nil {
		return StatusFailed, nil
	}
	if !admittedSchema(result.SchemaRef, result.MediaType) {
		return StatusFailed, nil
	}
	envelope, err := json.Marshal(map[string]any{
		"schema_ref":        result.SchemaRef,
		"determinism_class": result.DeterminismClass,
		"classification":    classification,
		"route":             route,
		"capability_id":     node.CapabilityID,
		"attempt_id":        attemptID,
		"fence_token":       attemptCtx.fenceToken,
		"source_content_id": entry.ContentID,
	})
	if err != nil {
		return StatusFailed, err
	}
	artifactID, err := sqlite.NewStableID(sqlite.IDPrefixArtifact)
	if err != nil {
		return StatusFailed, err
	}
	admittedArtifact = &sqlite.ProcessorArtifact{
		ID:             artifactID,
		WorkspaceID:    workspaceID,
		SubjectRef:     entry.ID,
		SnapshotRef:    snapshotRef,
		RouteDigest:    route.Digest(),
		Stage:          string(node.Stage),
		CapabilityID:   node.CapabilityID,
		SchemaRef:      result.SchemaRef,
		State:          sqlite.ArtifactAdmitted,
		AuthorityClass: AuthorityStagedArtifact,
		LifecycleClass: LifecycleRebuildable,
		MediaType:      result.MediaType,
		ByteLength:     int64(len(body)),
		Digest:         digest,
		Body:           string(body),
		AttemptID:      attemptID,
		FenceToken:     attemptCtx.fenceToken,
		ProducerDigest: ProducerDigest(node.CapabilityID),
		Envelope:       envelope,
		CreatedAt:      h.opts.Now().UTC(),
	}
	return StatusSucceeded, nil
}

func processorAttemptReason(status ResultStatus, processorReason string, attemptErr error) (string, string) {
	if attemptErr != nil {
		return "PROCESSOR_STAGE_FAILED", attemptErr.Error()
	}
	switch status {
	case StatusSucceeded:
		return "ADMITTED_ARTIFACT", processorReason
	case StatusInapplicable:
		if processorReason != "" {
			return "PROCESSOR_INAPPLICABLE", processorReason
		}
		return "PROCESSOR_INAPPLICABLE", "processor did not apply"
	case StatusCancelled:
		if processorReason != "" {
			return "PROCESSOR_STAGE_CANCELLED", processorReason
		}
		return "PROCESSOR_STAGE_CANCELLED", "processor stage cancelled"
	default:
		if processorReason != "" {
			return "PROCESSOR_STAGE_FAILED", processorReason
		}
		return "PROCESSOR_STAGE_FAILED", "processor did not produce an admitted artifact"
	}
}

func (h *Host) recordProcessorAttempt(ctx context.Context, attemptID, workspaceID, snapshotRef string,
	entry sqlite.NamespaceEntry, classification ClassificationRecord, route Route, node RouteNode,
	status ResultStatus, reasonCode, reason string, startedAt time.Time) error {
	return h.recordProcessorAttemptWithArtifact(ctx, attemptID, workspaceID, snapshotRef, entry,
		classification, route, node, status, reasonCode, reason, startedAt, nil, attemptContext{fenceToken: 1})
}

func (h *Host) recordProcessorAttemptWithArtifact(ctx context.Context, attemptID, workspaceID, snapshotRef string,
	entry sqlite.NamespaceEntry, classification ClassificationRecord, route Route, node RouteNode,
	status ResultStatus, reasonCode, reason string, startedAt time.Time, artifact *sqlite.ProcessorArtifact, attemptCtx attemptContext) error {
	provenanceFields := map[string]any{
		"attempt_id":        attemptID,
		"source_content_id": entry.ContentID,
		"display_name":      entry.DisplayName,
		"classification":    classification,
		"route":             route,
		"processor_digest":  ProducerDigest(node.CapabilityID),
		"fence_token":       attemptCtx.fenceToken,
	}
	if attemptCtx.retryJobID != "" {
		provenanceFields["retry_job_id"] = attemptCtx.retryJobID
		provenanceFields["retry_attempt"] = attemptCtx.retryAttempt
		provenanceFields["retry_idempotency_key"] = attemptCtx.retryIdempotencyKey
		provenanceFields["retry_lease_token"] = attemptCtx.retryLeaseToken
		provenanceFields["predecessor_attempt_id"] = attemptCtx.predecessorAttemptID
	}
	provenance, err := json.Marshal(provenanceFields)
	if err != nil {
		return fmt.Errorf("encode processor attempt provenance: %w", err)
	}
	routeJSON, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("encode processor attempt route: %w", err)
	}
	attempt := &sqlite.ProcessorAttempt{
		ID:              attemptID,
		WorkspaceID:     workspaceID,
		SubjectRef:      entry.ID,
		SnapshotRef:     snapshotRef,
		RouteDigest:     route.Digest(),
		Route:           routeJSON,
		Stage:           string(node.Stage),
		CapabilityID:    node.CapabilityID,
		Status:          string(status),
		ReasonCode:      reasonCode,
		Reason:          reason,
		Provenance:      provenance,
		FenceToken:      attemptCtx.fenceToken,
		ProcessorDigest: ProducerDigest(node.CapabilityID),
		CreatedAt:       startedAt,
		FinishedAt:      h.opts.Now().UTC(),
	}
	return h.Store.Update(ctx, func(tx *sqlite.Tx) error {
		if attemptCtx.retryJobID != "" {
			if err := tx.ValidateJobLease(ctx, workspaceID, attemptCtx.retryJobID, attemptCtx.retryOwner,
				attemptCtx.retryLeaseToken, attemptCtx.fenceToken, h.opts.Now().UTC()); err != nil {
				return fmt.Errorf("validate processor retry lease before publication: %w", err)
			}
		}
		if err := tx.InsertProcessorAttempt(ctx, attempt); err != nil {
			return err
		}
		if artifact == nil {
			return nil
		}
		if artifact.AttemptID != attempt.ID || artifact.WorkspaceID != attempt.WorkspaceID ||
			artifact.SubjectRef != attempt.SubjectRef || artifact.SnapshotRef != attempt.SnapshotRef ||
			artifact.RouteDigest != attempt.RouteDigest || artifact.Stage != attempt.Stage ||
			artifact.CapabilityID != attempt.CapabilityID || artifact.FenceToken != attempt.FenceToken {
			return errors.New("processor artifact does not match its terminal attempt")
		}
		return tx.InsertProcessorArtifact(ctx, artifact)
	})
}

func buildProcessingRoute(classification ClassificationRecord) Route {
	if classification.State == identify.IdentificationConflictingEvidence ||
		classification.State == identify.IdentificationUnknown {
		return Route{Kind: RouteProcessing}
	}
	if hasAudioEvidence(classification) {
		return Route{
			Kind: RouteProcessing,
			Nodes: []RouteNode{
				{Stage: StageExtract, CapabilityID: CapabilityAudioTags},
				{Stage: StageFingerprint, CapabilityID: CapabilityAudioFingerprint},
				{Stage: StageEnrich, CapabilityID: CapabilityClipEmbedding},
			},
		}
	}
	if hasBookEvidence(classification) {
		return Route{
			Kind: RouteProcessing,
			Nodes: []RouteNode{{
				Stage:        StageExtract,
				CapabilityID: CapabilityBookMeta,
			}},
		}
	}
	if hasPlainTextEvidence(classification) {
		return Route{
			Kind: RouteProcessing,
			Nodes: []RouteNode{
				{Stage: StageExtract, CapabilityID: CapabilityTextExtract},
				{Stage: StageEnrich, CapabilityID: CapabilityTextEmbedding},
			},
		}
	}
	return Route{Kind: RouteProcessing}
}

func hasAudioEvidence(classification ClassificationRecord) bool {
	for _, item := range classification.Evidence {
		mime := strings.ToLower(item.Candidate.MIME)
		if strings.HasPrefix(mime, "audio/") || mime == "application/ogg" {
			return true
		}
		switch item.Candidate.FormatID {
		case "flac", "mp3", "wave", "id3v2-tagged", "iso-bmff-audio", "ogg":
			return true
		}
	}
	return false
}

func hasBookEvidence(classification ClassificationRecord) bool {
	for _, item := range classification.Evidence {
		mime := strings.ToLower(item.Candidate.MIME)
		if mime == "application/epub+zip" {
			return true
		}
		if item.Candidate.FormatID == "epub" {
			return true
		}
	}
	return false
}

func admittedSchema(schemaRef, mediaType string) bool {
	switch {
	case schemaRef == SchemaRefExtractedText() && mediaType == MediaTypeUTF8Text:
		return true
	case schemaRef == SchemaRefAudioTrack() && mediaType == MediaTypeAudioJSON:
		return true
	case schemaRef == SchemaRefBookWork() && mediaType == MediaTypeBookJSON:
		return true
	case schemaRef == SchemaRefAcousticFingerprint() && mediaType == MediaTypeFingerprintJSON:
		return true
	case schemaRef == SchemaRefTextEmbedding() && mediaType == MediaTypeEmbeddingJSON:
		return true
	case schemaRef == SchemaRefClipEmbedding() && mediaType == MediaTypeEmbeddingJSON:
		return true
	default:
		return false
	}
}

func hasPlainTextEvidence(classification ClassificationRecord) bool {
	for _, item := range classification.Evidence {
		if item.Kind == identify.EvidenceSuffix && item.Candidate.MIME == "text/plain" {
			return true
		}
	}
	return false
}
