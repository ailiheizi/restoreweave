package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
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
	wantDigest := digestText(record.Body)
	if record.BodyDigest == "" {
		record.BodyDigest = wantDigest
	} else if record.BodyDigest != wantDigest {
		return fmt.Errorf("annotation body digest is %q, want %q", record.BodyDigest, wantDigest)
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
	revision := AnnotationRevision{
		ID: annotationRevisionID(record.ID, record.Revision), AnnotationID: record.ID,
		WorkspaceID: record.WorkspaceID, SubjectRef: record.SubjectRef, Kind: record.Kind,
		Body: record.Body, BodyDigest: record.BodyDigest, Revision: record.Revision,
		Tombstoned: record.Tombstoned, HistoryComplete: record.Revision == 1,
		CreatedAt: record.UpdatedAt,
	}
	if record.PredecessorRevision > 0 {
		revision.PredecessorID = annotationRevisionID(record.ID, record.PredecessorRevision)
	}
	return tx.insertAnnotationRevision(ctx, revision)
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
	if !tombstone && strings.TrimSpace(body) == "" {
		return errors.New("annotation body is required")
	}
	current, err := scanAnnotation(tx.tx.QueryRowContext(ctx, annotationSelect+`
WHERE workspace_id = ? AND annotation_id = ?`, workspaceID, annotationID))
	if err != nil {
		return err
	}
	if current.Revision != expectedRevision || current.Tombstoned {
		return ErrConflict
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
	previous, err := tx.getAnnotationRevision(ctx, workspaceID, annotationRevisionID(annotationID, expectedRevision))
	if err != nil {
		return err
	}
	return tx.insertAnnotationRevision(ctx, AnnotationRevision{
		ID: annotationRevisionID(annotationID, expectedRevision+1), AnnotationID: annotationID,
		WorkspaceID: workspaceID, SubjectRef: current.SubjectRef, Kind: current.Kind,
		Body: body, BodyDigest: digest, Revision: expectedRevision + 1,
		PredecessorID: previous.ID, Tombstoned: tombstone,
		HistoryComplete: previous.HistoryComplete, CreatedAt: updatedAt,
	})
}

func annotationRevisionID(annotationID string, revision int64) string {
	return fmt.Sprintf("%s@%d", annotationID, revision)
}

func (tx *Tx) insertAnnotationRevision(ctx context.Context, record AnnotationRevision) error {
	var predecessor any
	if record.PredecessorID != "" {
		predecessor = record.PredecessorID
	}
	return insertOne(ctx, tx.tx, `
INSERT INTO annotation_revisions(
    annotation_revision_id, annotation_id, workspace_id, subject_ref, kind,
    body, body_digest, revision, predecessor_revision_id, tombstoned,
    history_complete, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.AnnotationID, record.WorkspaceID, record.SubjectRef,
		record.Kind, record.Body, record.BodyDigest, record.Revision, predecessor,
		boolInt(record.Tombstoned), boolInt(record.HistoryComplete),
		record.CreatedAt.UTC().UnixNano())
}

func (tx *Tx) getAnnotationRevision(ctx context.Context, workspaceID, revisionID string) (AnnotationRevision, error) {
	return scanAnnotationRevision(tx.tx.QueryRowContext(ctx, annotationRevisionSelect+`
WHERE workspace_id = ? AND annotation_revision_id = ?`, workspaceID, revisionID))
}

func (s *Store) ListAnnotationRevisions(ctx context.Context, workspaceID, subjectRef string) ([]AnnotationRevision, error) {
	query := annotationRevisionSelect + `WHERE workspace_id = ?`
	args := []any{workspaceID}
	if subjectRef != "" {
		query += ` AND subject_ref = ?`
		args = append(args, subjectRef)
	}
	query += ` ORDER BY subject_ref, annotation_id, revision`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list annotation revisions: %w", err)
	}
	defer rows.Close()
	var records []AnnotationRevision
	for rows.Next() {
		record, err := scanAnnotationRevision(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate annotation revisions: %w", err)
	}
	return records, nil
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

func (tx *Tx) GetAnnotation(ctx context.Context, workspaceID, annotationID string) (Annotation, error) {
	return scanAnnotation(tx.tx.QueryRowContext(ctx, annotationSelect+`
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
		if strings.TrimSpace(record.Dimension) == "" {
			record.Dimension = "lexical-metadata-fts"
		}
		record.CreatedAt = recordTime(record.CreatedAt, tx.now)
		return insertOne(ctx, tx.tx, `
INSERT INTO index_generations(
    generation_id, workspace_id, snapshot_ref, namespace_root_id, db_path, dimension,
    config_digest, provider_profile_digest, semantic_space, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			record.ID, record.WorkspaceID, record.SnapshotRef, record.NamespaceRootID,
			record.DBPath, record.Dimension, record.ConfigDigest,
			record.ProviderProfileDigest, record.SemanticSpace, record.CreatedAt.UnixNano())
	})
}

func (s *Store) GetIndexGeneration(ctx context.Context, generationID string) (IndexGeneration, error) {
	return scanIndexGeneration(s.db.QueryRowContext(ctx, `
SELECT generation_id, workspace_id, snapshot_ref, namespace_root_id, db_path, dimension,
       config_digest, provider_profile_digest, semantic_space, created_at_ns
FROM index_generations WHERE generation_id = ?`, generationID))
}

func (s *Store) LatestIndexGeneration(ctx context.Context, workspaceID, dimension string) (IndexGeneration, error) {
	if strings.TrimSpace(dimension) == "" {
		dimension = "lexical-metadata-fts"
	}
	query := `
SELECT generation_id, workspace_id, snapshot_ref, namespace_root_id, db_path, dimension,
       config_digest, provider_profile_digest, semantic_space, created_at_ns
FROM index_generations WHERE dimension = ?`
	args := []any{dimension}
	if workspaceID != "" {
		query += ` AND workspace_id = ?`
		args = append(args, workspaceID)
	}
	query += ` ORDER BY created_at_ns DESC, generation_id DESC LIMIT 1`
	return scanIndexGeneration(s.db.QueryRowContext(ctx, query, args...))
}

func (s *Store) LatestPublication(ctx context.Context, workspaceID string) (Publication, error) {
	query := `
SELECT publication_id, workspace_id, snapshot_ref, scan_generation_id,
       binding_id, namespace_root_id, manifest_digest, committed_at_ns, metadata_json,
       plan_digest
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

const annotationRevisionSelect = `
SELECT annotation_revision_id, annotation_id, workspace_id, subject_ref, kind,
       body, body_digest, revision, predecessor_revision_id, tombstoned,
       history_complete, created_at_ns
FROM annotation_revisions `

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

func scanAnnotationRevision(scanner rowScanner) (AnnotationRevision, error) {
	var record AnnotationRevision
	var predecessor sql.NullString
	var tombstoned, historyComplete int
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.AnnotationID, &record.WorkspaceID, &record.SubjectRef,
		&record.Kind, &record.Body, &record.BodyDigest, &record.Revision,
		&predecessor, &tombstoned, &historyComplete, &created,
	); err != nil {
		return record, rowError("annotation revision", err)
	}
	if predecessor.Valid {
		record.PredecessorID = predecessor.String
	}
	record.Tombstoned = tombstoned == 1
	record.HistoryComplete = historyComplete == 1
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

func scanIndexGeneration(scanner rowScanner) (IndexGeneration, error) {
	var record IndexGeneration
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SnapshotRef,
		&record.NamespaceRootID, &record.DBPath, &record.Dimension,
		&record.ConfigDigest, &record.ProviderProfileDigest, &record.SemanticSpace, &created,
	); err != nil {
		return record, rowError("index generation", err)
	}
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}
