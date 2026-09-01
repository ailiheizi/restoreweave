package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func (tx *Tx) InsertWorkspace(ctx context.Context, record *Workspace) error {
	if record == nil {
		return errors.New("workspace is required")
	}
	if err := requireID("workspace id", record.ID); err != nil {
		return err
	}
	if err := requireText("workspace name", record.Name); err != nil {
		return err
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("workspace metadata: %w", err)
	}
	if record.Revision == 0 {
		record.Revision = 1
	}
	if record.Revision < 1 {
		return errors.New("workspace revision must be positive")
	}
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	record.Metadata = metadata
	err = insertOne(ctx, tx.tx, `
INSERT INTO workspaces(
    workspace_id, name, metadata_json, revision, created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.Name, string(metadata), record.Revision,
		record.CreatedAt.UnixNano(), record.UpdatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert workspace: %w", err)
	}
	return nil
}

func (s *Store) GetWorkspace(ctx context.Context, workspaceID string) (Workspace, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return Workspace{}, err
	}
	return scanWorkspace(s.db.QueryRowContext(ctx, `
SELECT workspace_id, name, metadata_json, revision, created_at_ns, updated_at_ns
FROM workspaces WHERE workspace_id = ?`, workspaceID))
}

func scanWorkspace(scanner rowScanner) (Workspace, error) {
	var record Workspace
	var metadata string
	var created, updated int64
	if err := scanner.Scan(&record.ID, &record.Name, &metadata, &record.Revision, &created, &updated); err != nil {
		return record, rowError("workspace", err)
	}
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	record.UpdatedAt = time.Unix(0, updated).UTC()
	return record, nil
}

func (tx *Tx) InsertSource(ctx context.Context, record *Source) error {
	if record == nil {
		return errors.New("source is required")
	}
	if err := requireID("source id", record.ID); err != nil {
		return err
	}
	if err := requireID("workspace id", record.WorkspaceID); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"source stable key": record.StableKey,
		"source kind":       record.Kind,
		"source locator":    record.Locator,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if !validSourceState(record.State) {
		return fmt.Errorf("invalid source state %q", record.State)
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("source metadata: %w", err)
	}
	if record.Revision == 0 {
		record.Revision = 1
	}
	if record.Revision < 1 {
		return errors.New("source revision must be positive")
	}
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	record.Metadata = metadata
	err = insertOne(ctx, tx.tx, `
INSERT INTO sources(
    source_id, workspace_id, stable_key, kind, locator, identity_fingerprint,
    state, metadata_json, revision, created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.StableKey, record.Kind, record.Locator,
		record.IdentityFingerprint, record.State, string(metadata), record.Revision,
		record.CreatedAt.UnixNano(), record.UpdatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert source: %w", err)
	}
	return nil
}

func (s *Store) GetSource(ctx context.Context, workspaceID, sourceID string) (Source, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return Source{}, err
	}
	if err := requireID("source id", sourceID); err != nil {
		return Source{}, err
	}
	return scanSource(s.db.QueryRowContext(ctx, `
SELECT source_id, workspace_id, stable_key, kind, locator, identity_fingerprint,
       state, metadata_json, revision, created_at_ns, updated_at_ns
FROM sources WHERE workspace_id = ? AND source_id = ?`, workspaceID, sourceID))
}

// ListSources returns the durable source records for one workspace. It is a
// read-only provenance query; callers must obtain reachability separately and
// must not turn a failed probe into a SourceState transition.
func (s *Store) ListSources(ctx context.Context, workspaceID string) ([]Source, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT source_id, workspace_id, stable_key, kind, locator, identity_fingerprint,
       state, metadata_json, revision, created_at_ns, updated_at_ns
FROM sources WHERE workspace_id = ? ORDER BY created_at_ns ASC, source_id ASC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()
	var records []Source
	for rows.Next() {
		record, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sources: %w", err)
	}
	return records, nil
}

// LatestScanGeneration returns the newest scan attempt for one source,
// including incomplete or failed attempts so operators can see why a source
// has not produced a newer publication.
func (s *Store) LatestScanGeneration(ctx context.Context, workspaceID, sourceID string) (ScanGeneration, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return ScanGeneration{}, err
	}
	if err := requireID("source id", sourceID); err != nil {
		return ScanGeneration{}, err
	}
	return scanScanGeneration(s.db.QueryRowContext(ctx, `
SELECT scan_generation_id, workspace_id, source_id, generation,
       parent_scan_generation_id, capture_set_id, capture_set_digest, state,
       full_traversal, summary_json, started_at_ns, finished_at_ns
FROM scan_generations
WHERE workspace_id = ? AND source_id = ?
ORDER BY generation DESC, started_at_ns DESC, scan_generation_id DESC
LIMIT 1`, workspaceID, sourceID))
}

// LatestPublicationForSource returns the newest committed snapshot for a
// source. The scan generation is used as the primary ordering key so a newer
// completed scan cannot be hidden by an older publication's timestamp.
func (s *Store) LatestPublicationForSource(ctx context.Context, workspaceID, sourceID string) (Publication, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return Publication{}, err
	}
	if err := requireID("source id", sourceID); err != nil {
		return Publication{}, err
	}
	return scanPublication(s.db.QueryRowContext(ctx, `
SELECT p.publication_id, p.workspace_id, p.snapshot_ref, p.scan_generation_id,
       p.binding_id, p.namespace_root_id, p.manifest_digest, p.committed_at_ns,
       p.metadata_json, p.plan_digest
FROM publications AS p
JOIN scan_generations AS sg
  ON sg.workspace_id = p.workspace_id
 AND sg.scan_generation_id = p.scan_generation_id
WHERE p.workspace_id = ? AND sg.source_id = ?
ORDER BY sg.generation DESC, p.committed_at_ns DESC, p.snapshot_ref DESC
LIMIT 1`, workspaceID, sourceID))
}

func scanSource(scanner rowScanner) (Source, error) {
	var record Source
	var metadata string
	var created, updated int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.StableKey, &record.Kind,
		&record.Locator, &record.IdentityFingerprint, &record.State, &metadata,
		&record.Revision, &created, &updated,
	); err != nil {
		return record, rowError("source", err)
	}
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	record.UpdatedAt = time.Unix(0, updated).UTC()
	return record, nil
}

func (tx *Tx) InsertScanGeneration(ctx context.Context, record *ScanGeneration) error {
	if record == nil {
		return errors.New("scan generation is required")
	}
	for name, value := range map[string]string{
		"scan generation id": record.ID,
		"workspace id":       record.WorkspaceID,
		"source id":          record.SourceID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	if record.ParentID != "" {
		if err := requireID("parent scan generation id", record.ParentID); err != nil {
			return err
		}
	}
	if record.Generation < 1 {
		return errors.New("scan generation number must be positive")
	}
	if record.State == "" {
		record.State = ScanRunning
	}
	if record.State != ScanRunning {
		return errors.New("a new scan generation must start in RUNNING state")
	}
	for name, value := range map[string]string{
		"capture set id":     record.CaptureSetID,
		"capture set digest": record.CaptureSetDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	summary, err := normalizeJSON(record.Summary)
	if err != nil {
		return fmt.Errorf("scan summary: %w", err)
	}
	record.StartedAt = recordTime(record.StartedAt, tx.now)
	record.FinishedAt = nil
	record.Summary = summary
	err = insertOne(ctx, tx.tx, `
INSERT INTO scan_generations(
    scan_generation_id, workspace_id, source_id, generation,
    parent_scan_generation_id, capture_set_id, capture_set_digest, state,
    full_traversal, summary_json, started_at_ns, finished_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SourceID, record.Generation,
		nullableString(record.ParentID), record.CaptureSetID, record.CaptureSetDigest,
		record.State, boolInt(record.FullTraversal), string(summary), record.StartedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert scan generation: %w", err)
	}
	return nil
}

func (tx *Tx) FinishScanGeneration(
	ctx context.Context,
	workspaceID, scanID string,
	state ScanState,
	fullTraversal bool,
	summary json.RawMessage,
	finishedAt time.Time,
) error {
	if err := requireID("workspace id", workspaceID); err != nil {
		return err
	}
	if err := requireID("scan generation id", scanID); err != nil {
		return err
	}
	if state != ScanComplete && state != ScanIncomplete && state != ScanFailed && state != ScanCancelled {
		return fmt.Errorf("invalid terminal scan state %q", state)
	}
	if state == ScanComplete && !fullTraversal {
		return errors.New("a COMPLETE scan requires full traversal")
	}
	summaryJSON, err := normalizeJSON(summary)
	if err != nil {
		return fmt.Errorf("scan summary: %w", err)
	}
	finishedAt = recordTime(finishedAt, tx.now)
	result, err := tx.tx.ExecContext(ctx, `
UPDATE scan_generations
SET state = ?, full_traversal = ?, summary_json = ?, finished_at_ns = ?
WHERE workspace_id = ? AND scan_generation_id = ? AND state = 'RUNNING'`,
		state, boolInt(fullTraversal), string(summaryJSON), finishedAt.UnixNano(), workspaceID, scanID)
	if err != nil {
		return fmt.Errorf("finish scan generation: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("finish scan generation: %w", err)
	}
	if changed != 1 {
		return ErrInvalidTransition
	}
	return nil
}

func (s *Store) GetScanGeneration(ctx context.Context, workspaceID, scanID string) (ScanGeneration, error) {
	return scanScanGeneration(s.db.QueryRowContext(ctx, `
SELECT scan_generation_id, workspace_id, source_id, generation,
       parent_scan_generation_id, capture_set_id, capture_set_digest, state,
       full_traversal, summary_json, started_at_ns, finished_at_ns
FROM scan_generations WHERE workspace_id = ? AND scan_generation_id = ?`, workspaceID, scanID))
}

func scanScanGeneration(scanner rowScanner) (ScanGeneration, error) {
	var record ScanGeneration
	var parent sql.NullString
	var fullTraversal int
	var summary string
	var started int64
	var finished sql.NullInt64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SourceID, &record.Generation,
		&parent, &record.CaptureSetID, &record.CaptureSetDigest, &record.State,
		&fullTraversal, &summary, &started, &finished,
	); err != nil {
		return record, rowError("scan generation", err)
	}
	record.ParentID = parent.String
	record.FullTraversal = fullTraversal == 1
	record.Summary = json.RawMessage(summary)
	record.StartedAt = time.Unix(0, started).UTC()
	if finished.Valid {
		value := time.Unix(0, finished.Int64).UTC()
		record.FinishedAt = &value
	}
	return record, nil
}

func (tx *Tx) InsertObservation(ctx context.Context, record *Observation) error {
	if record == nil {
		return errors.New("observation is required")
	}
	for name, value := range map[string]string{
		"observation id":     record.ID,
		"workspace id":       record.WorkspaceID,
		"source id":          record.SourceID,
		"scan generation id": record.ScanGenerationID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	if len(record.PathKey) == 0 || len(record.RawPath) == 0 {
		return errors.New("observation path key and raw path are required")
	}
	if !validEntryType(record.EntryType) {
		return fmt.Errorf("invalid observation entry type %q", record.EntryType)
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("observation metadata: %w", err)
	}
	record.ObservedAt = recordTime(record.ObservedAt, tx.now)
	record.Metadata = metadata
	err = insertOne(ctx, tx.tx, `
INSERT INTO observations(
    observation_id, workspace_id, source_id, scan_generation_id, path_key,
    raw_path, display_path, entry_type, content_id, file_version_id,
    stat_digest, logical_size, allocated_size, read_state, metadata_json,
    observed_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SourceID, record.ScanGenerationID,
		record.PathKey, record.RawPath, record.DisplayPath, record.EntryType,
		record.ContentID, record.FileVersionID, record.StatDigest,
		nullableInt64(record.LogicalSize), nullableInt64(record.AllocatedSize),
		record.ReadState, string(metadata), record.ObservedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert observation: %w", err)
	}
	return nil
}

func (s *Store) GetObservation(ctx context.Context, workspaceID, observationID string) (Observation, error) {
	return scanObservation(s.db.QueryRowContext(ctx, `
SELECT observation_id, workspace_id, source_id, scan_generation_id, path_key,
       raw_path, display_path, entry_type, content_id, file_version_id,
       stat_digest, logical_size, allocated_size, read_state, metadata_json,
       observed_at_ns
FROM observations WHERE workspace_id = ? AND observation_id = ?`, workspaceID, observationID))
}

func scanObservation(scanner rowScanner) (Observation, error) {
	var record Observation
	var logical, allocated sql.NullInt64
	var metadata string
	var observed int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SourceID, &record.ScanGenerationID,
		&record.PathKey, &record.RawPath, &record.DisplayPath, &record.EntryType,
		&record.ContentID, &record.FileVersionID, &record.StatDigest, &logical,
		&allocated, &record.ReadState, &metadata, &observed,
	); err != nil {
		return record, rowError("observation", err)
	}
	record.LogicalSize = int64Pointer(logical)
	record.AllocatedSize = int64Pointer(allocated)
	record.Metadata = json.RawMessage(metadata)
	record.ObservedAt = time.Unix(0, observed).UTC()
	return record, nil
}

func (tx *Tx) InsertDetectionEvidence(ctx context.Context, record *DetectionEvidence) error {
	if record == nil {
		return errors.New("detection evidence is required")
	}
	for name, value := range map[string]string{
		"detection evidence id": record.ID,
		"workspace id":          record.WorkspaceID,
		"observation id":        record.ObservationID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"detector id":         record.DetectorID,
		"detector digest":     record.DetectorDigest,
		"evidence kind":       record.EvidenceKind,
		"evidence digest":     record.EvidenceDigest,
		"sandbox policy hash": record.SandboxPolicyHash,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if record.ExecutionClass != "BYTE_DETERMINISTIC" &&
		record.ExecutionClass != "SEEDED_STOCHASTIC" &&
		record.ExecutionClass != "OPAQUE_NONDETERMINISTIC" {
		return fmt.Errorf("invalid execution class %q", record.ExecutionClass)
	}
	if record.Confidence != nil && (*record.Confidence < 0 || *record.Confidence > 1) {
		return errors.New("detection confidence must be between zero and one")
	}
	evidence, err := normalizeJSON(record.Evidence)
	if err != nil {
		return fmt.Errorf("detection evidence: %w", err)
	}
	record.StartedAt = recordTime(record.StartedAt, tx.now)
	record.FinishedAt = recordTime(record.FinishedAt, tx.now)
	if record.FinishedAt.Before(record.StartedAt) {
		return errors.New("detection finish time precedes start time")
	}
	record.Evidence = evidence
	err = insertOne(ctx, tx.tx, `
INSERT INTO detection_evidence(
    detection_evidence_id, workspace_id, observation_id, detector_id,
    detector_digest, evidence_kind, candidate_format, candidate_mime,
    confidence, execution_class, evidence_json, evidence_digest,
    sandbox_policy_hash, started_at_ns, finished_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.ObservationID, record.DetectorID,
		record.DetectorDigest, record.EvidenceKind, record.CandidateFormat,
		record.CandidateMIME, nullableFloat64(record.Confidence), record.ExecutionClass,
		string(evidence), record.EvidenceDigest, record.SandboxPolicyHash,
		record.StartedAt.UnixNano(), record.FinishedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert detection evidence: %w", err)
	}
	return nil
}

func (s *Store) GetDetectionEvidence(
	ctx context.Context,
	workspaceID, evidenceID string,
) (DetectionEvidence, error) {
	var record DetectionEvidence
	var confidence sql.NullFloat64
	var evidenceJSON string
	var started, finished int64
	err := s.db.QueryRowContext(ctx, `
SELECT detection_evidence_id, workspace_id, observation_id, detector_id,
       detector_digest, evidence_kind, candidate_format, candidate_mime,
       confidence, execution_class, evidence_json, evidence_digest,
       sandbox_policy_hash, started_at_ns, finished_at_ns
FROM detection_evidence
WHERE workspace_id = ? AND detection_evidence_id = ?`, workspaceID, evidenceID).Scan(
		&record.ID, &record.WorkspaceID, &record.ObservationID, &record.DetectorID,
		&record.DetectorDigest, &record.EvidenceKind, &record.CandidateFormat,
		&record.CandidateMIME, &confidence, &record.ExecutionClass, &evidenceJSON,
		&record.EvidenceDigest, &record.SandboxPolicyHash, &started, &finished,
	)
	if err != nil {
		return record, rowError("detection evidence", err)
	}
	if confidence.Valid {
		value := confidence.Float64
		record.Confidence = &value
	}
	record.Evidence = json.RawMessage(evidenceJSON)
	record.StartedAt = time.Unix(0, started).UTC()
	record.FinishedAt = time.Unix(0, finished).UTC()
	return record, nil
}

func (tx *Tx) InsertPlan(ctx context.Context, record *Plan) error {
	if record == nil {
		return errors.New("plan is required")
	}
	if err := requireID("plan id", record.ID); err != nil {
		return err
	}
	if err := requireID("workspace id", record.WorkspaceID); err != nil {
		return err
	}
	if record.ScanGenerationID != "" {
		if err := requireID("scan generation id", record.ScanGenerationID); err != nil {
			return err
		}
	}
	if !validPlanState(record.State) {
		return fmt.Errorf("invalid plan state %q", record.State)
	}
	for name, value := range map[string]string{
		"plan kind":   record.Kind,
		"plan digest": record.PlanDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	planJSON, err := normalizeJSON(record.Plan)
	if err != nil {
		return fmt.Errorf("plan body: %w", err)
	}
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	record.Plan = planJSON
	err = insertOne(ctx, tx.tx, `
INSERT INTO plans(
    plan_id, workspace_id, scan_generation_id, kind, state,
    policy_revision, plan_json, plan_digest, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, nullableString(record.ScanGenerationID),
		record.Kind, record.State, record.PolicyRevision, string(planJSON),
		record.PlanDigest, record.CreatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert plan: %w", err)
	}
	return nil
}

func (s *Store) GetPlanByDigest(ctx context.Context, workspaceID, digest string) (Plan, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return Plan{}, err
	}
	if err := requireText("plan digest", digest); err != nil {
		return Plan{}, err
	}
	return scanPlan(s.db.QueryRowContext(ctx, `
SELECT plan_id, workspace_id, scan_generation_id, kind, state,
       policy_revision, plan_json, plan_digest, created_at_ns
FROM plans WHERE workspace_id = ? AND plan_digest = ?`, workspaceID, digest))
}

func (s *Store) GetPlan(ctx context.Context, workspaceID, planID string) (Plan, error) {
	return scanPlan(s.db.QueryRowContext(ctx, `
SELECT plan_id, workspace_id, scan_generation_id, kind, state,
       policy_revision, plan_json, plan_digest, created_at_ns
FROM plans WHERE workspace_id = ? AND plan_id = ?`, workspaceID, planID))
}

func scanPlan(scanner rowScanner) (Plan, error) {
	var record Plan
	var scanID sql.NullString
	var planJSON string
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &scanID, &record.Kind, &record.State,
		&record.PolicyRevision, &planJSON, &record.PlanDigest, &created,
	); err != nil {
		return record, rowError("plan", err)
	}
	record.ScanGenerationID = scanID.String
	record.Plan = json.RawMessage(planJSON)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

func (s *Store) ListPlans(ctx context.Context, workspaceID string) ([]Plan, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT plan_id, workspace_id, scan_generation_id, kind, state,
       policy_revision, plan_json, plan_digest, created_at_ns
FROM plans WHERE workspace_id = ? ORDER BY created_at_ns ASC, plan_id ASC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()
	var plans []Plan
	for rows.Next() {
		var record Plan
		var scanID sql.NullString
		var planJSON string
		var created int64
		if err := rows.Scan(
			&record.ID, &record.WorkspaceID, &scanID, &record.Kind, &record.State,
			&record.PolicyRevision, &planJSON, &record.PlanDigest, &created,
		); err != nil {
			return nil, rowError("plan", err)
		}
		record.ScanGenerationID = scanID.String
		record.Plan = json.RawMessage(planJSON)
		record.CreatedAt = time.Unix(0, created).UTC()
		plans = append(plans, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	return plans, nil
}

func (tx *Tx) InsertJob(ctx context.Context, record *Job) error {
	if record == nil {
		return errors.New("job is required")
	}
	if err := requireID("job id", record.ID); err != nil {
		return err
	}
	if err := requireID("workspace id", record.WorkspaceID); err != nil {
		return err
	}
	if record.PlanID != "" {
		if err := requireID("plan id", record.PlanID); err != nil {
			return err
		}
	}
	if err := requireText("job kind", record.Kind); err != nil {
		return err
	}
	if record.State == "" {
		record.State = JobQueued
	}
	if !validJobState(record.State) {
		return fmt.Errorf("invalid job state %q", record.State)
	}
	input, err := normalizeJSON(record.Input)
	if err != nil {
		return fmt.Errorf("job input: %w", err)
	}
	checkpoint, err := normalizeJSON(record.Checkpoint)
	if err != nil {
		return fmt.Errorf("job checkpoint: %w", err)
	}
	resultJSON, err := normalizeJSON(record.Result)
	if err != nil {
		return fmt.Errorf("job result: %w", err)
	}
	if record.MaxAttempts == 0 {
		record.MaxAttempts = 1
	}
	if record.MaxAttempts < 1 || record.Attempt < 0 || record.Attempt > record.MaxAttempts {
		return errors.New("invalid job attempt bounds")
	}
	if record.Revision == 0 {
		record.Revision = 1
	}
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	record.Input, record.Checkpoint, record.Result = input, checkpoint, resultJSON
	err = insertOne(ctx, tx.tx, `
INSERT INTO jobs(
    job_id, workspace_id, plan_id, kind, state, input_json, checkpoint_json,
    result_json, error_code, attempt, max_attempts, lease_owner, lease_token,
    fencing_token, lease_until_ns, cancellation_asked, revision,
    created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, nullableString(record.PlanID), record.Kind,
		record.State, string(input), string(checkpoint), string(resultJSON),
		record.ErrorCode, record.Attempt, record.MaxAttempts, record.LeaseOwner,
		record.LeaseToken, record.FencingToken, nullableTime(record.LeaseUntil),
		boolInt(record.CancellationAsked), record.Revision,
		record.CreatedAt.UnixNano(), record.UpdatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

func (s *Store) CountPlans(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plans`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count plans: %w", err)
	}
	return count, nil
}

func (s *Store) ListRecentPlans(ctx context.Context, limit int) ([]Plan, error) {
	if limit <= 0 || limit > 100 {
		return nil, errors.New("plan list limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT plan_id, workspace_id, scan_generation_id, kind, state,
       policy_revision, plan_json, plan_digest, created_at_ns
FROM plans ORDER BY created_at_ns DESC, plan_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent plans: %w", err)
	}
	defer rows.Close()
	var plans []Plan
	for rows.Next() {
		record, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list recent plans: %w", err)
	}
	return plans, nil
}

func (s *Store) CountJobs(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count jobs: %w", err)
	}
	return count, nil
}

func (s *Store) ListRecentJobs(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 100 {
		return nil, errors.New("job list limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT job_id, workspace_id, plan_id, kind, state, input_json, checkpoint_json,
       result_json, error_code, attempt, max_attempts, lease_owner, lease_token,
       fencing_token, lease_until_ns, cancellation_asked, revision,
       created_at_ns, updated_at_ns
FROM jobs ORDER BY updated_at_ns DESC, job_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent jobs: %w", err)
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		record, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list recent jobs: %w", err)
	}
	return jobs, nil
}

// ListClaimableJobs returns bounded retry work whose lease is absent or has
// expired. Claiming remains a separate fenced transaction.
func (s *Store) ListClaimableJobs(ctx context.Context, kind string, now time.Time, limit int) ([]Job, error) {
	if err := requireText("job kind", kind); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		return nil, errors.New("claimable job limit must be between 1 and 100")
	}
	now = now.UTC()
	rows, err := s.db.QueryContext(ctx, `
SELECT job_id, workspace_id, plan_id, kind, state, input_json, checkpoint_json,
       result_json, error_code, attempt, max_attempts, lease_owner, lease_token,
       fencing_token, lease_until_ns, cancellation_asked, revision,
       created_at_ns, updated_at_ns
FROM jobs
WHERE kind = ?
  AND state IN ('QUEUED', 'RUNNING', 'NEEDS_RECONCILIATION')
  AND attempt < max_attempts
  AND (lease_until_ns IS NULL OR lease_until_ns <= ?)
ORDER BY created_at_ns, job_id
LIMIT ?`, kind, now.UnixNano(), limit)
	if err != nil {
		return nil, fmt.Errorf("list claimable jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]Job, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimable jobs: %w", err)
	}
	return jobs, nil
}

func (s *Store) GetJob(ctx context.Context, workspaceID, jobID string) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `
SELECT job_id, workspace_id, plan_id, kind, state, input_json, checkpoint_json,
       result_json, error_code, attempt, max_attempts, lease_owner, lease_token,
       fencing_token, lease_until_ns, cancellation_asked, revision,
       created_at_ns, updated_at_ns
FROM jobs WHERE workspace_id = ? AND job_id = ?`, workspaceID, jobID))
}

// GetJobByPlanKind returns the single durable execution record for a plan and
// job kind. A partial unique index prevents two controllers from creating
// competing logical executions for the same immutable plan.
func (s *Store) GetJobByPlanKind(ctx context.Context, workspaceID, planID, kind string) (Job, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return Job{}, err
	}
	if err := requireID("plan id", planID); err != nil {
		return Job{}, err
	}
	if err := requireText("job kind", kind); err != nil {
		return Job{}, err
	}
	return scanJob(s.db.QueryRowContext(ctx, `
SELECT job_id, workspace_id, plan_id, kind, state, input_json, checkpoint_json,
       result_json, error_code, attempt, max_attempts, lease_owner, lease_token,
       fencing_token, lease_until_ns, cancellation_asked, revision,
       created_at_ns, updated_at_ns
FROM jobs WHERE workspace_id = ? AND plan_id = ? AND kind = ?`, workspaceID, planID, kind))
}

func scanJob(scanner rowScanner) (Job, error) {
	var record Job
	var planID sql.NullString
	var input, checkpoint, resultJSON string
	var leaseUntil sql.NullInt64
	var cancellation int
	var created, updated int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &planID, &record.Kind, &record.State,
		&input, &checkpoint, &resultJSON, &record.ErrorCode, &record.Attempt,
		&record.MaxAttempts, &record.LeaseOwner, &record.LeaseToken,
		&record.FencingToken, &leaseUntil, &cancellation, &record.Revision,
		&created, &updated,
	); err != nil {
		return record, rowError("job", err)
	}
	record.PlanID = planID.String
	record.Input = json.RawMessage(input)
	record.Checkpoint = json.RawMessage(checkpoint)
	record.Result = json.RawMessage(resultJSON)
	if leaseUntil.Valid {
		value := time.Unix(0, leaseUntil.Int64).UTC()
		record.LeaseUntil = &value
	}
	record.CancellationAsked = cancellation == 1
	record.CreatedAt = time.Unix(0, created).UTC()
	record.UpdatedAt = time.Unix(0, updated).UTC()
	return record, nil
}

func (tx *Tx) UpdateJob(ctx context.Context, update JobUpdate) error {
	if err := requireID("workspace id", update.WorkspaceID); err != nil {
		return err
	}
	if err := requireID("job id", update.JobID); err != nil {
		return err
	}
	if update.ExpectedRevision < 1 {
		return errors.New("expected job revision must be positive")
	}
	if !validJobState(update.State) {
		return fmt.Errorf("invalid job state %q", update.State)
	}
	var current JobState
	var currentRevision int64
	if err := tx.tx.QueryRowContext(ctx, `
SELECT state, revision FROM jobs WHERE workspace_id = ? AND job_id = ?`,
		update.WorkspaceID, update.JobID).Scan(&current, &currentRevision); err != nil {
		return rowError("job", err)
	}
	if currentRevision != update.ExpectedRevision {
		return ErrConflict
	}
	if !validJobTransition(current, update.State) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current, update.State)
	}
	checkpoint, err := normalizeJSON(update.Checkpoint)
	if err != nil {
		return fmt.Errorf("job checkpoint: %w", err)
	}
	resultJSON, err := normalizeJSON(update.Result)
	if err != nil {
		return fmt.Errorf("job result: %w", err)
	}
	update.UpdatedAt = recordTime(update.UpdatedAt, tx.now)
	result, err := tx.tx.ExecContext(ctx, `
UPDATE jobs SET
    state = ?, checkpoint_json = ?, result_json = ?, error_code = ?,
    attempt = ?, cancellation_asked = ?, revision = revision + 1,
	lease_owner = '', lease_token = '', lease_until_ns = NULL,
    updated_at_ns = ?
WHERE workspace_id = ? AND job_id = ? AND revision = ?`,
		update.State, string(checkpoint), string(resultJSON), update.ErrorCode,
		update.Attempt, boolInt(update.CancellationAsked), update.UpdatedAt.UnixNano(),
		update.WorkspaceID, update.JobID, update.ExpectedRevision)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}

func (tx *Tx) AcquireJobLease(
	ctx context.Context,
	workspaceID, jobID string,
	expectedRevision int64,
	owner, leaseToken string,
	now, until time.Time,
) (int64, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return 0, err
	}
	if err := requireID("job id", jobID); err != nil {
		return 0, err
	}
	if expectedRevision < 1 {
		return 0, errors.New("expected job revision must be positive")
	}
	if err := requireText("lease owner", owner); err != nil {
		return 0, err
	}
	if err := requireText("lease token", leaseToken); err != nil {
		return 0, err
	}
	now = recordTime(now, tx.now)
	until = until.UTC()
	if !until.After(now) {
		return 0, errors.New("job lease must expire after its acquisition time")
	}
	var fencingToken int64
	err := tx.tx.QueryRowContext(ctx, `
UPDATE jobs SET
    state = 'RUNNING', lease_owner = ?, lease_token = ?,
    fencing_token = fencing_token + 1, lease_until_ns = ?,
    attempt = attempt + 1, revision = revision + 1, updated_at_ns = ?
WHERE workspace_id = ? AND job_id = ? AND revision = ?
  AND state IN ('QUEUED', 'RUNNING', 'NEEDS_RECONCILIATION')
  AND attempt < max_attempts
  AND (lease_until_ns IS NULL OR lease_until_ns <= ?)
RETURNING fencing_token`,
		owner, leaseToken, until.UnixNano(), now.UnixNano(), workspaceID, jobID,
		expectedRevision, now.UnixNano()).Scan(&fencingToken)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, fmt.Errorf("acquire job lease: %w", err)
	}
	return fencingToken, nil
}

func (s *Store) ValidateJobLease(ctx context.Context, workspaceID, jobID, owner, leaseToken string, fencingToken int64, now time.Time) error {
	if fencingToken < 1 {
		return errors.New("job fencing token must be positive")
	}
	var found int
	err := s.db.QueryRowContext(ctx, `
SELECT 1 FROM jobs
WHERE workspace_id = ? AND job_id = ? AND state = 'RUNNING'
  AND lease_owner = ? AND lease_token = ? AND fencing_token = ?
  AND lease_until_ns > ?`, workspaceID, jobID, owner, leaseToken, fencingToken, now.UTC().UnixNano()).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("validate job lease: %w", err)
	}
	return nil
}

// RenewJobLease extends one still-active lease without changing its fencing
// token or job revision. An expired or superseded lease cannot be revived.
func (s *Store) RenewJobLease(ctx context.Context, workspaceID, jobID, owner, leaseToken string, fencingToken int64, now, until time.Time) error {
	if fencingToken < 1 {
		return errors.New("job fencing token must be positive")
	}
	now = now.UTC()
	until = until.UTC()
	if !until.After(now) {
		return errors.New("job lease renewal must expire after renewal time")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE jobs SET lease_until_ns = ?, updated_at_ns = ?
WHERE workspace_id = ? AND job_id = ? AND state = 'RUNNING'
  AND lease_owner = ? AND lease_token = ? AND fencing_token = ?
  AND lease_until_ns > ?`, until.UnixNano(), now.UnixNano(), workspaceID, jobID,
		owner, leaseToken, fencingToken, now.UnixNano())
	if err != nil {
		return fmt.Errorf("renew job lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("renew job lease: %w", err)
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}

// ValidateJobLease checks a publication lease inside the caller's write
// transaction so takeover cannot race validation and the protected write.
func (tx *Tx) ValidateJobLease(ctx context.Context, workspaceID, jobID, owner, leaseToken string, fencingToken int64, now time.Time) error {
	if fencingToken < 1 {
		return errors.New("job fencing token must be positive")
	}
	var found int
	err := tx.tx.QueryRowContext(ctx, `
SELECT 1 FROM jobs
WHERE workspace_id = ? AND job_id = ? AND state = 'RUNNING'
  AND lease_owner = ? AND lease_token = ? AND fencing_token = ?
  AND lease_until_ns > ?`, workspaceID, jobID, owner, leaseToken, fencingToken, now.UTC().UnixNano()).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("validate job lease: %w", err)
	}
	return nil
}

func (tx *Tx) AppendAuditEvent(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		return errors.New("audit event is required")
	}
	if err := requireID("audit event id", event.ID); err != nil {
		return err
	}
	if err := requireID("workspace id", event.WorkspaceID); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"audit actor":       event.Actor,
		"audit action":      event.Action,
		"audit target type": event.TargetType,
		"audit outcome":     event.Outcome,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if event.Attempt < 0 || event.FencingToken < 0 {
		return errors.New("audit attempt and fencing token cannot be negative")
	}
	details, err := normalizeJSON(event.Details)
	if err != nil {
		return fmt.Errorf("audit details: %w", err)
	}
	event.Details = details
	event.OccurredAt = recordTime(event.OccurredAt, tx.now)

	var latest string
	err = tx.tx.QueryRowContext(ctx, `
SELECT event_digest FROM audit_events
WHERE workspace_id = ? ORDER BY audit_sequence DESC LIMIT 1`, event.WorkspaceID).Scan(&latest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read audit predecessor: %w", err)
	}
	if event.PreviousEventDigest != "" && event.PreviousEventDigest != latest {
		return ErrAuditChain
	}
	event.PreviousEventDigest = latest
	digest, err := auditEventDigest(*event)
	if err != nil {
		return err
	}
	if event.EventDigest != "" && event.EventDigest != digest {
		return errors.New("provided audit event digest does not match canonical event fields")
	}
	event.EventDigest = digest

	result, err := tx.tx.ExecContext(ctx, `
INSERT INTO audit_events(
    audit_event_id, workspace_id, actor, action, target_type, target_id,
    request_id, attempt, source, policy_ref, approval_ref, fencing_token,
    outcome, details_json, previous_event_digest, event_digest, occurred_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.WorkspaceID, event.Actor, event.Action, event.TargetType,
		event.TargetID, event.RequestID, event.Attempt, event.Source, event.PolicyRef,
		event.ApprovalRef, event.FencingToken, event.Outcome, string(details),
		event.PreviousEventDigest, event.EventDigest, event.OccurredAt.UnixNano())
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read audit event sequence: %w", err)
	}
	event.Sequence = sequence
	return nil
}

// ListAuditEvents returns the append-only operational projection in global
// sequence order. Durable audit export remains outside this SQLite database.
func (s *Store) ListAuditEvents(
	ctx context.Context,
	workspaceID string,
	afterSequence int64,
	limit int,
) ([]AuditEvent, error) {
	if limit <= 0 || limit > 10_000 {
		return nil, errors.New("audit event limit must be between 1 and 10000")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT audit_sequence, audit_event_id, workspace_id, actor, action,
       target_type, target_id, request_id, attempt, source, policy_ref,
       approval_ref, fencing_token, outcome, details_json,
       previous_event_digest, event_digest, occurred_at_ns
FROM audit_events
WHERE workspace_id = ? AND audit_sequence > ?
ORDER BY audit_sequence
LIMIT ?`, workspaceID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var details string
		var occurred int64
		if err := rows.Scan(
			&event.Sequence, &event.ID, &event.WorkspaceID, &event.Actor,
			&event.Action, &event.TargetType, &event.TargetID, &event.RequestID,
			&event.Attempt, &event.Source, &event.PolicyRef, &event.ApprovalRef,
			&event.FencingToken, &event.Outcome, &details,
			&event.PreviousEventDigest, &event.EventDigest, &occurred,
		); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		event.Details = json.RawMessage(details)
		event.OccurredAt = time.Unix(0, occurred).UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return events, nil
}

func (s *Store) ListJobAuditEvents(
	ctx context.Context,
	workspaceID, jobID string,
	afterSequence int64,
	limit int,
) ([]AuditEvent, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	if err := requireID("job id", jobID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 10_000 {
		return nil, errors.New("job event limit must be between 1 and 10000")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT audit_sequence, audit_event_id, workspace_id, actor, action,
       target_type, target_id, request_id, attempt, source, policy_ref,
       approval_ref, fencing_token, outcome, details_json,
       previous_event_digest, event_digest, occurred_at_ns
FROM audit_events
WHERE workspace_id = ? AND target_type = 'JOB' AND target_id = ? AND audit_sequence > ?
ORDER BY audit_sequence
LIMIT ?`, workspaceID, jobID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list job audit events: %w", err)
	}
	defer rows.Close()
	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var details string
		var occurred int64
		if err := rows.Scan(
			&event.Sequence, &event.ID, &event.WorkspaceID, &event.Actor,
			&event.Action, &event.TargetType, &event.TargetID, &event.RequestID,
			&event.Attempt, &event.Source, &event.PolicyRef, &event.ApprovalRef,
			&event.FencingToken, &event.Outcome, &details,
			&event.PreviousEventDigest, &event.EventDigest, &occurred,
		); err != nil {
			return nil, fmt.Errorf("scan job audit event: %w", err)
		}
		event.Details = json.RawMessage(details)
		event.OccurredAt = time.Unix(0, occurred).UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list job audit events: %w", err)
	}
	return events, nil
}

func auditEventDigest(event AuditEvent) (string, error) {
	material := struct {
		ID                  string          `json:"id"`
		WorkspaceID         string          `json:"workspaceId"`
		Actor               string          `json:"actor"`
		Action              string          `json:"action"`
		TargetType          string          `json:"targetType"`
		TargetID            string          `json:"targetId"`
		RequestID           string          `json:"requestId"`
		Attempt             int64           `json:"attempt"`
		Source              string          `json:"source"`
		PolicyRef           string          `json:"policyRef"`
		ApprovalRef         string          `json:"approvalRef"`
		FencingToken        int64           `json:"fencingToken"`
		Outcome             string          `json:"outcome"`
		Details             json.RawMessage `json:"details"`
		PreviousEventDigest string          `json:"previousEventDigest"`
		OccurredAtNS        int64           `json:"occurredAtNs"`
	}{
		ID: event.ID, WorkspaceID: event.WorkspaceID, Actor: event.Actor,
		Action: event.Action, TargetType: event.TargetType, TargetID: event.TargetID,
		RequestID: event.RequestID, Attempt: event.Attempt, Source: event.Source,
		PolicyRef: event.PolicyRef, ApprovalRef: event.ApprovalRef,
		FencingToken: event.FencingToken, Outcome: event.Outcome, Details: event.Details,
		PreviousEventDigest: event.PreviousEventDigest,
		OccurredAtNS:        event.OccurredAt.UTC().UnixNano(),
	}
	encoded, err := json.Marshal(material)
	if err != nil {
		return "", fmt.Errorf("encode canonical audit event: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func rowError(kind string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, kind)
	}
	return fmt.Errorf("scan %s: %w", kind, err)
}

func int64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	copy := value.Int64
	return &copy
}

func nullableFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func validSourceState(value SourceState) bool {
	switch value {
	case SourceActive, SourceDecommissioned, SourceLost, SourceQuarantined:
		return true
	default:
		return false
	}
}

func validPlanState(value PlanState) bool {
	switch value {
	case PlanDraft, PlanReady, PlanCommitted, PlanSuperseded, PlanRejected:
		return true
	default:
		return false
	}
}

func validJobState(value JobState) bool {
	switch value {
	case JobQueued, JobRunning, JobWaitingApproval, JobSucceeded, JobFailed,
		JobCancelled, JobNeedsReconcile:
		return true
	default:
		return false
	}
}

func validJobTransition(from, to JobState) bool {
	if from == to {
		return true
	}
	switch from {
	case JobQueued:
		return to == JobRunning || to == JobWaitingApproval || to == JobCancelled
	case JobRunning:
		return to == JobWaitingApproval || to == JobSucceeded || to == JobFailed ||
			to == JobCancelled || to == JobNeedsReconcile || to == JobQueued
	case JobWaitingApproval:
		return to == JobQueued || to == JobRunning || to == JobCancelled || to == JobFailed
	case JobNeedsReconcile:
		return to == JobQueued || to == JobRunning || to == JobSucceeded ||
			to == JobFailed || to == JobCancelled
	default:
		return false
	}
}

func validEntryType(value NamespaceEntryType) bool {
	switch value {
	case EntryFile, EntryDirectory, EntrySymlink, EntryFIFO, EntrySocket, EntryDevice, EntrySpecial:
		return true
	default:
		return false
	}
}

func validExtentKind(value ExtentKind) bool {
	switch value {
	case ExtentData, ExtentHole:
		return true
	default:
		return false
	}
}

func validOwnershipMode(value OwnershipMode) bool {
	switch value {
	case OwnershipRestoreWeavePacks, OwnershipEngineManaged, OwnershipInline:
		return true
	default:
		return false
	}
}

func validLocatorKind(value LocatorKind) bool {
	switch value {
	case LocatorPackRange, LocatorObject, LocatorInline:
		return true
	default:
		return false
	}
}

func isRangeLocator(value LocatorKind) bool {
	return value == LocatorPackRange
}

func requireByteKey(name string, value []byte) error {
	if len(value) == 0 {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func trimOrEmpty(value string) string {
	return strings.TrimSpace(value)
}
