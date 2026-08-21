package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

func (tx *Tx) InsertProcessorArtifact(ctx context.Context, record *ProcessorArtifact) error {
	if record == nil {
		return errors.New("processor artifact is required")
	}
	for name, value := range map[string]string{
		"artifact id":  record.ID,
		"workspace id": record.WorkspaceID,
		"subject ref":  record.SubjectRef,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"snapshot ref":    record.SnapshotRef,
		"route digest":    record.RouteDigest,
		"stage":           record.Stage,
		"capability id":   record.CapabilityID,
		"schema ref":      record.SchemaRef,
		"authority class": record.AuthorityClass,
		"lifecycle class": record.LifecycleClass,
		"media type":      record.MediaType,
		"digest":          record.Digest,
		"attempt id":      record.AttemptID,
		"producer digest": record.ProducerDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if record.State != ArtifactAdmitted && record.State != ArtifactRejected {
		return fmt.Errorf("invalid processor artifact state %q", record.State)
	}
	if record.ByteLength < 0 {
		return errors.New("processor artifact byte length cannot be negative")
	}
	body := []byte(record.Body)
	if record.ByteLength != int64(len(body)) {
		return fmt.Errorf("processor artifact byte length is %d, want %d", record.ByteLength, len(body))
	}
	digest := sha256.Sum256(body)
	expectedDigest := "sha256:" + hex.EncodeToString(digest[:])
	if record.Digest != expectedDigest {
		return fmt.Errorf("processor artifact digest is %q, want %q", record.Digest, expectedDigest)
	}
	if record.FenceToken < 1 {
		return errors.New("processor artifact fence token must be positive")
	}
	metadata, err := normalizeJSON(record.Envelope)
	if err != nil {
		return fmt.Errorf("processor artifact envelope: %w", err)
	}
	record.Envelope = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	err = insertOne(ctx, tx.tx, `
INSERT INTO processor_artifacts(
    artifact_id, workspace_id, subject_ref, snapshot_ref, route_digest, stage,
    capability_id, schema_ref, state, authority_class, lifecycle_class,
    media_type, byte_length, digest, body, attempt_id, fence_token,
    producer_digest, envelope_json, created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SubjectRef, record.SnapshotRef,
		record.RouteDigest, record.Stage, record.CapabilityID, record.SchemaRef,
		record.State, record.AuthorityClass, record.LifecycleClass, record.MediaType,
		record.ByteLength, record.Digest, record.Body, record.AttemptID, record.FenceToken,
		record.ProducerDigest, string(metadata), record.CreatedAt.UnixNano(),
		record.UpdatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert processor artifact: %w", err)
	}
	return nil
}

func (s *Store) InsertProcessorArtifact(ctx context.Context, record *ProcessorArtifact) error {
	return s.Update(ctx, func(tx *Tx) error {
		return tx.InsertProcessorArtifact(ctx, record)
	})
}

func (s *Store) GetProcessorArtifact(ctx context.Context, workspaceID, artifactID string) (ProcessorArtifact, error) {
	return scanProcessorArtifact(s.db.QueryRowContext(ctx, processorArtifactSelect+`
WHERE workspace_id = ? AND artifact_id = ?`, workspaceID, artifactID))
}

func (s *Store) ListAdmittedArtifacts(ctx context.Context, workspaceID, snapshotRef string) ([]ProcessorArtifact, error) {
	query := processorArtifactSelect + `WHERE workspace_id = ? AND state = 'POLICY_ADMITTED'`
	args := []any{workspaceID}
	if snapshotRef != "" {
		query += ` AND snapshot_ref = ?`
		args = append(args, snapshotRef)
	}
	query += ` ORDER BY subject_ref, stage, capability_id, artifact_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list admitted artifacts: %w", err)
	}
	defer rows.Close()
	var records []ProcessorArtifact
	for rows.Next() {
		record, err := scanProcessorArtifact(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admitted artifacts: %w", err)
	}
	return records, nil
}

const processorArtifactSelect = `
SELECT artifact_id, workspace_id, subject_ref, snapshot_ref, route_digest, stage,
       capability_id, schema_ref, state, authority_class, lifecycle_class,
       media_type, byte_length, digest, body, attempt_id, fence_token,
       producer_digest, envelope_json, created_at_ns, updated_at_ns
FROM processor_artifacts `

func scanProcessorArtifact(scanner rowScanner) (ProcessorArtifact, error) {
	var record ProcessorArtifact
	var envelope string
	var created, updated int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SubjectRef, &record.SnapshotRef,
		&record.RouteDigest, &record.Stage, &record.CapabilityID, &record.SchemaRef,
		&record.State, &record.AuthorityClass, &record.LifecycleClass, &record.MediaType,
		&record.ByteLength, &record.Digest, &record.Body, &record.AttemptID, &record.FenceToken,
		&record.ProducerDigest, &envelope, &created, &updated,
	); err != nil {
		return record, rowError("processor artifact", err)
	}
	record.Envelope = []byte(envelope)
	record.CreatedAt = time.Unix(0, created).UTC()
	record.UpdatedAt = time.Unix(0, updated).UTC()
	return record, nil
}
