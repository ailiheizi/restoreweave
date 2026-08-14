package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (tx *Tx) InsertAnnotation(ctx context.Context, record *Annotation) error {
	if record == nil {
		return errors.New("annotation is required")
	}
	for name, value := range map[string]string{
		"annotation id": record.ID,
		"workspace id":  record.WorkspaceID,
		"subject ref":   record.SubjectRef,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	if record.Kind != AnnotationTag && record.Kind != AnnotationNote && record.Kind != AnnotationProgress {
		return fmt.Errorf("invalid annotation kind %q", record.Kind)
	}
	if strings.TrimSpace(record.Body) == "" {
		return errors.New("annotation body is required")
	}
	if record.Revision < 1 {
		return errors.New("annotation revision must be positive")
	}
	if record.BodyDigest == "" {
		record.BodyDigest = digestText(record.Body)
	}
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	err := insertOne(ctx, tx.tx, `
INSERT INTO annotations(
    annotation_id, workspace_id, subject_ref, kind, body, body_digest,
    revision, predecessor_revision, tombstoned, created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SubjectRef, record.Kind, record.Body,
		record.BodyDigest, record.Revision, record.PredecessorRevision,
		boolInt(record.Tombstoned), record.CreatedAt.UnixNano(), record.UpdatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert annotation: %w", err)
	}
	return nil
}

func (tx *Tx) UpdateAnnotationRevision(
	ctx context.Context,
	workspaceID, annotationID string,
	expectedRevision int64,
	body string,
	tombstone bool,
	updatedAt time.Time,
) error {
	if err := requireID("workspace id", workspaceID); err != nil {
		return err
	}
	if err := requireID("annotation id", annotationID); err != nil {
		return err
	}
	updatedAt = recordTime(updatedAt, tx.now)
	digest := digestText(body)
	result, err := tx.tx.ExecContext(ctx, `
UPDATE annotations
SET body = ?, body_digest = ?, predecessor_revision = revision,
    revision = revision + 1, tombstoned = ?, updated_at_ns = ?
WHERE workspace_id = ? AND annotation_id = ? AND revision = ? AND tombstoned = 0`,
		body, digest, boolInt(tombstone), updatedAt.UnixNano(),
		workspaceID, annotationID, expectedRevision)
	if err != nil {
		return fmt.Errorf("update annotation: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) CreateAnnotation(ctx context.Context, record *Annotation) error {
	return s.Update(ctx, func(tx *Tx) error {
		return tx.InsertAnnotation(ctx, record)
	})
}

func (s *Store) ReviseAnnotation(
	ctx context.Context,
	workspaceID, annotationID string,
	expectedRevision int64,
	body string,
	tombstone bool,
	updatedAt time.Time,
) error {
	return s.Update(ctx, func(tx *Tx) error {
		return tx.UpdateAnnotationRevision(ctx, workspaceID, annotationID, expectedRevision, body, tombstone, updatedAt)
	})
}

func (s *Store) GetAnnotation(ctx context.Context, workspaceID, annotationID string) (Annotation, error) {
	return scanAnnotation(s.db.QueryRowContext(ctx, annotationSelect+`
WHERE workspace_id = ? AND annotation_id = ?`, workspaceID, annotationID))
}

func (s *Store) ListAnnotations(ctx context.Context, workspaceID, subjectRef string, includeTombstones bool) ([]Annotation, error) {
	query := annotationSelect + `WHERE workspace_id = ?`
	args := []any{workspaceID}
	if subjectRef != "" {
		query += ` AND subject_ref = ?`
		args = append(args, subjectRef)
	}
	if !includeTombstones {
		query += ` AND tombstoned = 0`
	}
	query += ` ORDER BY kind, body, annotation_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}
	defer rows.Close()
	var records []Annotation
	for rows.Next() {
		record, err := scanAnnotation(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate annotations: %w", err)
	}
	return records, nil
}

func (s *Store) FindLiveTag(ctx context.Context, workspaceID, subjectRef, body string) (Annotation, error) {
	return scanAnnotation(s.db.QueryRowContext(ctx, annotationSelect+`
WHERE workspace_id = ? AND subject_ref = ? AND kind = 'TAG' AND body = ? AND tombstoned = 0`,
		workspaceID, subjectRef, body))
}

func (s *Store) FindLiveProgress(ctx context.Context, workspaceID, subjectRef string) (Annotation, error) {
	return scanAnnotation(s.db.QueryRowContext(ctx, annotationSelect+`
WHERE workspace_id = ? AND subject_ref = ? AND kind = 'PROGRESS' AND tombstoned = 0`,
		workspaceID, subjectRef))
}

func (s *Store) InsertIndexGeneration(ctx context.Context, record *IndexGeneration) error {
	return s.Update(ctx, func(tx *Tx) error {
		if record == nil {
			return errors.New("index generation is required")
		}
		for name, value := range map[string]string{
			"generation id":     record.ID,
			"workspace id":      record.WorkspaceID,
			"namespace root id": record.NamespaceRootID,
		} {
			if err := requireID(name, value); err != nil {
				return err
			}
		}
		for name, value := range map[string]string{
			"snapshot ref": record.SnapshotRef,
			"db path":      record.DBPath,
		} {
			if err := requireText(name, value); err != nil {
				return err
			}
		}
		record.CreatedAt = recordTime(record.CreatedAt, tx.now)
		return insertOne(ctx, tx.tx, `
INSERT INTO index_generations(
    generation_id, workspace_id, snapshot_ref, namespace_root_id, db_path, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			record.ID, record.WorkspaceID, record.SnapshotRef, record.NamespaceRootID,
			record.DBPath, record.CreatedAt.UnixNano())
	})
}

func (s *Store) GetIndexGeneration(ctx context.Context, generationID string) (IndexGeneration, error) {
	return scanIndexGeneration(s.db.QueryRowContext(ctx, `
SELECT generation_id, workspace_id, snapshot_ref, namespace_root_id, db_path, created_at_ns
FROM index_generations WHERE generation_id = ?`, generationID))
}

func (s *Store) LatestIndexGeneration(ctx context.Context, workspaceID string) (IndexGeneration, error) {
	query := `
SELECT generation_id, workspace_id, snapshot_ref, namespace_root_id, db_path, created_at_ns
FROM index_generations`
	args := []any{}
	if workspaceID != "" {
		query += ` WHERE workspace_id = ?`
		args = append(args, workspaceID)
	}
	query += ` ORDER BY created_at_ns DESC, generation_id DESC LIMIT 1`
	return scanIndexGeneration(s.db.QueryRowContext(ctx, query, args...))
}

func (s *Store) LatestPublication(ctx context.Context, workspaceID string) (Publication, error) {
	query := `
SELECT publication_id, workspace_id, snapshot_ref, scan_generation_id,
       binding_id, namespace_root_id, manifest_digest, committed_at_ns, metadata_json
FROM publications`
	args := []any{}
	if workspaceID != "" {
		query += ` WHERE workspace_id = ?`
		args = append(args, workspaceID)
	}
	query += ` ORDER BY committed_at_ns DESC, snapshot_ref DESC LIMIT 1`
	return scanPublication(s.db.QueryRowContext(ctx, query, args...))
}

const annotationSelect = `
SELECT annotation_id, workspace_id, subject_ref, kind, body, body_digest,
       revision, predecessor_revision, tombstoned, created_at_ns, updated_at_ns
FROM annotations `

func scanAnnotation(scanner rowScanner) (Annotation, error) {
	var record Annotation
	var tombstoned int
	var created, updated int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SubjectRef, &record.Kind,
		&record.Body, &record.BodyDigest, &record.Revision, &record.PredecessorRevision,
		&tombstoned, &created, &updated,
	); err != nil {
		return record, rowError("annotation", err)
	}
	record.Tombstoned = tombstoned == 1
	record.CreatedAt = time.Unix(0, created).UTC()
	record.UpdatedAt = time.Unix(0, updated).UTC()
	return record, nil
}

func scanIndexGeneration(scanner rowScanner) (IndexGeneration, error) {
	var record IndexGeneration
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SnapshotRef,
		&record.NamespaceRootID, &record.DBPath, &created,
	); err != nil {
		return record, rowError("index generation", err)
	}
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}
