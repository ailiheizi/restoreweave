package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const ProcessorAttemptExportSchema = "restoreweave.processor-attempts/v1"

// ProcessorAttemptExport is a portable projection of one attempt. It does
// not include processor output bytes; ArtifactRefs point to separately
// exported, content-addressed artifacts when one was admitted.
type ProcessorAttemptExport struct {
	AttemptID       string          `json:"attempt_id"`
	WorkspaceID     string          `json:"workspace_id"`
	SubjectRef      string          `json:"subject_ref"`
	SnapshotRef     string          `json:"snapshot_ref"`
	RouteDigest     string          `json:"route_digest"`
	Route           json.RawMessage `json:"route"`
	Stage           string          `json:"stage"`
	CapabilityID    string          `json:"capability_id"`
	Status          string          `json:"status"`
	ReasonCode      string          `json:"reason_code"`
	Reason          string          `json:"reason"`
	Provenance      json.RawMessage `json:"provenance"`
	FenceToken      int64           `json:"fence_token"`
	ProcessorDigest string          `json:"processor_digest"`
	CreatedAt       time.Time       `json:"created_at"`
	FinishedAt      time.Time       `json:"finished_at"`
	ArtifactRefs    []string        `json:"artifact_refs,omitempty"`
}

type processorAttemptExportBundle struct {
	Schema      string                   `json:"schema"`
	WorkspaceID string                   `json:"workspace_id"`
	SnapshotRef string                   `json:"snapshot_ref,omitempty"`
	Attempts    []ProcessorAttemptExport `json:"attempts"`
}

func (tx *Tx) InsertProcessorAttempt(ctx context.Context, record *ProcessorAttempt) error {
	if record == nil {
		return errors.New("processor attempt is required")
	}
	for name, value := range map[string]string{
		"attempt id":       record.ID,
		"workspace id":     record.WorkspaceID,
		"subject ref":      record.SubjectRef,
		"snapshot ref":     record.SnapshotRef,
		"route digest":     record.RouteDigest,
		"stage":            record.Stage,
		"capability id":    record.CapabilityID,
		"reason code":      record.ReasonCode,
		"processor digest": record.ProcessorDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if err := validateStableID(record.ID); err != nil {
		return fmt.Errorf("attempt id: %w", err)
	}
	switch record.Status {
	case "SUCCEEDED", "INAPPLICABLE", "FAILED", "CANCELLED":
	default:
		return fmt.Errorf("invalid processor attempt status %q", record.Status)
	}
	if record.FenceToken < 1 {
		return errors.New("processor attempt fence token must be positive")
	}
	route, err := canonicalProcessorJSON(record.Route)
	if err != nil {
		return fmt.Errorf("processor attempt route: %w", err)
	}
	provenance, err := canonicalProcessorJSON(record.Provenance)
	if err != nil {
		return fmt.Errorf("processor attempt provenance: %w", err)
	}
	record.Route = route
	record.Provenance = provenance
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	record.FinishedAt = recordTime(record.FinishedAt, tx.now)
	if record.FinishedAt.Before(record.CreatedAt) {
		return errors.New("processor attempt finished time cannot precede creation time")
	}
	err = insertOne(ctx, tx.tx, `
INSERT INTO processor_attempts(
    attempt_id, workspace_id, subject_ref, snapshot_ref, route_digest,
    route_json, stage, capability_id, status, reason_code, reason,
    provenance_json, fence_token, processor_digest, created_at_ns, finished_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SubjectRef, record.SnapshotRef,
		record.RouteDigest, string(route), record.Stage, record.CapabilityID,
		record.Status, record.ReasonCode, record.Reason, string(provenance),
		record.FenceToken, record.ProcessorDigest, record.CreatedAt.UnixNano(),
		record.FinishedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert processor attempt: %w", err)
	}
	return nil
}

// canonicalProcessorJSON normalizes object key ordering through the standard
// JSON encoder, making the exported attempt projection stable across writes.
func canonicalProcessorJSON(value json.RawMessage) (json.RawMessage, error) {
	if len(value) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		return nil, errors.New("value is not valid JSON")
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func (s *Store) InsertProcessorAttempt(ctx context.Context, record *ProcessorAttempt) error {
	return s.Update(ctx, func(tx *Tx) error {
		return tx.InsertProcessorAttempt(ctx, record)
	})
}

// ListProcessorAttempts returns immutable outcomes in completion order. The
// snapshot filter is optional so replay callers can inspect a whole workspace.
func (s *Store) ListProcessorAttempts(ctx context.Context, workspaceID, snapshotRef string) ([]ProcessorAttempt, error) {
	query := processorAttemptSelect + `WHERE workspace_id = ?`
	args := []any{workspaceID}
	if snapshotRef != "" {
		query += ` AND snapshot_ref = ?`
		args = append(args, snapshotRef)
	}
	query += ` ORDER BY created_at_ns, attempt_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list processor attempts: %w", err)
	}
	defer rows.Close()
	var records []ProcessorAttempt
	for rows.Next() {
		record, err := scanProcessorAttempt(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate processor attempts: %w", err)
	}
	return records, nil
}

// ExportProcessorAttempts returns a deterministic, snapshot-bound JSON
// projection suitable for a sidecar export. It does not mutate the exact
// manifest or claim to be recovery authority for content bytes.
func (s *Store) ExportProcessorAttempts(ctx context.Context, workspaceID, snapshotRef string) ([]byte, error) {
	records, err := s.ListProcessorAttempts(ctx, workspaceID, snapshotRef)
	if err != nil {
		return nil, err
	}
	refs, err := s.processorArtifactRefs(ctx, workspaceID, snapshotRef)
	if err != nil {
		return nil, err
	}
	bundle := processorAttemptExportBundle{
		Schema: ProcessorAttemptExportSchema, WorkspaceID: workspaceID,
		SnapshotRef: snapshotRef, Attempts: make([]ProcessorAttemptExport, 0, len(records)),
	}
	for _, record := range records {
		bundle.Attempts = append(bundle.Attempts, ProcessorAttemptExport{
			AttemptID: record.ID, WorkspaceID: record.WorkspaceID,
			SubjectRef: record.SubjectRef, SnapshotRef: record.SnapshotRef,
			RouteDigest: record.RouteDigest, Route: record.Route,
			Stage: record.Stage, CapabilityID: record.CapabilityID,
			Status: record.Status, ReasonCode: record.ReasonCode, Reason: record.Reason,
			Provenance: record.Provenance, FenceToken: record.FenceToken,
			ProcessorDigest: record.ProcessorDigest, CreatedAt: record.CreatedAt,
			FinishedAt: record.FinishedAt, ArtifactRefs: refs[record.ID],
		})
	}
	return json.Marshal(bundle)
}

func (s *Store) processorArtifactRefs(ctx context.Context, workspaceID, snapshotRef string) (map[string][]string, error) {
	query := `SELECT attempt_id, artifact_id FROM processor_artifacts WHERE workspace_id = ? AND state = 'POLICY_ADMITTED'`
	args := []any{workspaceID}
	if snapshotRef != "" {
		query += ` AND snapshot_ref = ?`
		args = append(args, snapshotRef)
	}
	query += ` ORDER BY attempt_id, artifact_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list processor artifact references: %w", err)
	}
	defer rows.Close()
	refs := make(map[string][]string)
	for rows.Next() {
		var attemptID, artifactID string
		if err := rows.Scan(&attemptID, &artifactID); err != nil {
			return nil, fmt.Errorf("scan processor artifact reference: %w", err)
		}
		refs[attemptID] = append(refs[attemptID], artifactID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate processor artifact references: %w", err)
	}
	return refs, nil
}

const processorAttemptSelect = `
SELECT attempt_id, workspace_id, subject_ref, snapshot_ref, route_digest,
       route_json, stage, capability_id, status, reason_code, reason,
       provenance_json, fence_token, processor_digest, created_at_ns, finished_at_ns
FROM processor_attempts `

func scanProcessorAttempt(scanner rowScanner) (ProcessorAttempt, error) {
	var record ProcessorAttempt
	var route, provenance string
	var created, finished int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SubjectRef, &record.SnapshotRef,
		&record.RouteDigest, &route, &record.Stage, &record.CapabilityID,
		&record.Status, &record.ReasonCode, &record.Reason, &provenance,
		&record.FenceToken, &record.ProcessorDigest, &created, &finished,
	); err != nil {
		return record, rowError("processor attempt", err)
	}
	record.Route = json.RawMessage(route)
	record.Provenance = json.RawMessage(provenance)
	record.CreatedAt = time.Unix(0, created).UTC()
	record.FinishedAt = time.Unix(0, finished).UTC()
	return record, nil
}
