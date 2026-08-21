package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func validDescriptionKind(value DescriptionKind) bool {
	switch value {
	case DescriptionUser, DescriptionImported, DescriptionExtracted,
		DescriptionAISummary, DescriptionAIAnalysis:
		return true
	default:
		return false
	}
}

func validateConfidence(name string, value *float64) error {
	if value != nil && (*value < 0 || *value > 1) {
		return fmt.Errorf("%s must be between 0 and 1", name)
	}
	return nil
}

func (tx *Tx) InsertMetadataFact(ctx context.Context, record *MetadataFact) error {
	if record == nil {
		return errors.New("metadata fact is required")
	}
	for name, value := range map[string]string{
		"metadata fact id": record.ID,
		"workspace id":     record.WorkspaceID,
		"subject ref":      record.SubjectRef,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"metadata namespace":  record.Namespace,
		"metadata key":        record.Key,
		"metadata value type": record.ValueType,
		"metadata authority":  record.AuthorityClass,
		"metadata source ref": record.SourceRef,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if err := validateConfidence("metadata confidence", record.Confidence); err != nil {
		return err
	}
	if record.Revision == 0 {
		record.Revision = 1
	}
	if record.Revision < 1 {
		return errors.New("metadata fact revision must be positive")
	}
	value, err := normalizeJSON(record.Value)
	if err != nil {
		return fmt.Errorf("metadata fact value: %w", err)
	}
	record.Value = value
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if err := insertOne(ctx, tx.tx, `
INSERT INTO metadata_facts(
    metadata_fact_id, workspace_id, subject_ref, namespace, fact_key,
    value_json, value_type, authority_class, source_ref, confidence,
    revision, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SubjectRef, record.Namespace,
		record.Key, string(value), record.ValueType, record.AuthorityClass,
		record.SourceRef, record.Confidence, record.Revision, record.CreatedAt.UnixNano()); err != nil {
		return fmt.Errorf("insert metadata fact: %w", err)
	}
	return nil
}

func (s *Store) InsertMetadataFact(ctx context.Context, record *MetadataFact) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.InsertMetadataFact(ctx, record) })
}

const metadataFactSelect = `
SELECT metadata_fact_id, workspace_id, subject_ref, namespace, fact_key,
       value_json, value_type, authority_class, source_ref, confidence,
       revision, created_at_ns
FROM metadata_facts `

func scanMetadataFact(scanner rowScanner) (MetadataFact, error) {
	var record MetadataFact
	var value string
	var confidence sql.NullFloat64
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SubjectRef, &record.Namespace,
		&record.Key, &value, &record.ValueType, &record.AuthorityClass,
		&record.SourceRef, &confidence, &record.Revision, &created,
	); err != nil {
		return record, rowError("metadata fact", err)
	}
	if confidence.Valid {
		record.Confidence = &confidence.Float64
	}
	record.Value = json.RawMessage(value)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

func (s *Store) GetMetadataFact(ctx context.Context, workspaceID, factID string) (MetadataFact, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return MetadataFact{}, err
	}
	if err := requireID("metadata fact id", factID); err != nil {
		return MetadataFact{}, err
	}
	return scanMetadataFact(s.db.QueryRowContext(ctx, metadataFactSelect+
		`WHERE workspace_id = ? AND metadata_fact_id = ?`, workspaceID, factID))
}

func (s *Store) ListMetadataFacts(ctx context.Context, workspaceID, subjectRef string) ([]MetadataFact, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	query := metadataFactSelect + `WHERE workspace_id = ?`
	args := []any{workspaceID}
	if strings.TrimSpace(subjectRef) != "" {
		if err := requireID("subject ref", subjectRef); err != nil {
			return nil, err
		}
		query += ` AND subject_ref = ?`
		args = append(args, subjectRef)
	}
	query += ` ORDER BY subject_ref, namespace, fact_key, revision, metadata_fact_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list metadata facts: %w", err)
	}
	defer rows.Close()
	var records []MetadataFact
	for rows.Next() {
		record, err := scanMetadataFact(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate metadata facts: %w", err)
	}
	return records, nil
}

func (tx *Tx) InsertDescriptionDocument(ctx context.Context, record *DescriptionDocument) error {
	if record == nil {
		return errors.New("description document is required")
	}
	for name, value := range map[string]string{
		"description document id": record.ID,
		"workspace id":            record.WorkspaceID,
		"subject ref":             record.SubjectRef,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	if !validDescriptionKind(record.Kind) {
		return fmt.Errorf("invalid description kind %q", record.Kind)
	}
	if strings.TrimSpace(record.Body) == "" {
		return errors.New("description body is required")
	}
	if err := validateConfidence("description confidence", record.Confidence); err != nil {
		return err
	}
	if err := validateConfidence("description coverage", record.Coverage); err != nil {
		return err
	}
	if record.PredecessorID != "" {
		if err := requireID("description predecessor id", record.PredecessorID); err != nil {
			return err
		}
	}
	if record.Revision == 0 {
		record.Revision = 1
	}
	if record.Revision < 1 {
		return errors.New("description revision must be positive")
	}
	if strings.TrimSpace(record.Language) == "" {
		record.Language = "und"
	}
	if record.BodyDigest == "" {
		record.BodyDigest = digestText(record.Body)
	}
	if strings.TrimSpace(record.Visibility) == "" {
		record.Visibility = "private"
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("description metadata: %w", err)
	}
	record.Metadata = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	if err := insertOne(ctx, tx.tx, `
INSERT INTO description_documents(
    description_document_id, workspace_id, subject_ref, kind, title,
    language, body, body_digest, source_ref, producer_profile, confidence,
    coverage, visibility, accepted, revision, predecessor_id, metadata_json,
    created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SubjectRef, record.Kind, record.Title,
		record.Language, record.Body, record.BodyDigest, record.SourceRef,
		record.ProducerProfile, record.Confidence, record.Coverage, record.Visibility,
		boolInt(record.Accepted), record.Revision, nullableString(record.PredecessorID),
		string(metadata), record.CreatedAt.UnixNano(), record.UpdatedAt.UnixNano()); err != nil {
		return fmt.Errorf("insert description document: %w", err)
	}
	return nil
}

func (s *Store) InsertDescriptionDocument(ctx context.Context, record *DescriptionDocument) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.InsertDescriptionDocument(ctx, record) })
}

const descriptionDocumentSelect = `
SELECT description_document_id, workspace_id, subject_ref, kind, title,
       language, body, body_digest, source_ref, producer_profile, confidence,
       coverage, visibility, accepted, revision, predecessor_id, metadata_json,
       created_at_ns, updated_at_ns
FROM description_documents `

func scanDescriptionDocument(scanner rowScanner) (DescriptionDocument, error) {
	var record DescriptionDocument
	var confidence, coverage sql.NullFloat64
	var accepted int
	var predecessor sql.NullString
	var metadata string
	var created, updated int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SubjectRef, &record.Kind,
		&record.Title, &record.Language, &record.Body, &record.BodyDigest,
		&record.SourceRef, &record.ProducerProfile, &confidence, &coverage,
		&record.Visibility, &accepted, &record.Revision, &predecessor, &metadata,
		&created, &updated,
	); err != nil {
		return record, rowError("description document", err)
	}
	if confidence.Valid {
		record.Confidence = &confidence.Float64
	}
	if coverage.Valid {
		record.Coverage = &coverage.Float64
	}
	record.Accepted = accepted == 1
	if predecessor.Valid {
		record.PredecessorID = predecessor.String
	}
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	record.UpdatedAt = time.Unix(0, updated).UTC()
	return record, nil
}

func (s *Store) GetDescriptionDocument(ctx context.Context, workspaceID, documentID string) (DescriptionDocument, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return DescriptionDocument{}, err
	}
	if err := requireID("description document id", documentID); err != nil {
		return DescriptionDocument{}, err
	}
	return scanDescriptionDocument(s.db.QueryRowContext(ctx, descriptionDocumentSelect+
		`WHERE workspace_id = ? AND description_document_id = ?`, workspaceID, documentID))
}

func (s *Store) ListDescriptionDocuments(ctx context.Context, workspaceID, subjectRef string) ([]DescriptionDocument, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	query := descriptionDocumentSelect + `WHERE workspace_id = ?`
	args := []any{workspaceID}
	if strings.TrimSpace(subjectRef) != "" {
		if err := requireID("subject ref", subjectRef); err != nil {
			return nil, err
		}
		query += ` AND subject_ref = ?`
		args = append(args, subjectRef)
	}
	query += ` ORDER BY subject_ref, revision, description_document_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list description documents: %w", err)
	}
	defer rows.Close()
	var records []DescriptionDocument
	for rows.Next() {
		record, err := scanDescriptionDocument(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate description documents: %w", err)
	}
	return records, nil
}

const descriptionSummarySelect = `
SELECT description_document_id, workspace_id, subject_ref, kind, title,
       language, body_digest, source_ref, producer_profile, confidence,
       coverage, visibility, accepted, revision, predecessor_id,
       created_at_ns, updated_at_ns
FROM description_documents `

func scanDescriptionSummary(scanner rowScanner) (DescriptionDocument, error) {
	var record DescriptionDocument
	var confidence, coverage sql.NullFloat64
	var accepted int
	var predecessor sql.NullString
	var created, updated int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SubjectRef, &record.Kind,
		&record.Title, &record.Language, &record.BodyDigest, &record.SourceRef,
		&record.ProducerProfile, &confidence, &coverage, &record.Visibility,
		&accepted, &record.Revision, &predecessor, &created, &updated,
	); err != nil {
		return record, rowError("description summary", err)
	}
	if confidence.Valid {
		record.Confidence = &confidence.Float64
	}
	if coverage.Valid {
		record.Coverage = &coverage.Float64
	}
	record.Accepted = accepted == 1
	if predecessor.Valid {
		record.PredecessorID = predecessor.String
	}
	record.CreatedAt = time.Unix(0, created).UTC()
	record.UpdatedAt = time.Unix(0, updated).UTC()
	return record, nil
}

// ListDescriptionSummaries returns bounded metadata without loading long
// bodies or segment text. Callers may request one extra row to report that a
// page was truncated.
func (s *Store) ListDescriptionSummaries(ctx context.Context, workspaceID, subjectRef string, limit int) ([]DescriptionDocument, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1001 {
		return nil, errors.New("description summary limit must be between 1 and 1001")
	}
	query := descriptionSummarySelect + `WHERE workspace_id = ?`
	args := []any{workspaceID}
	if strings.TrimSpace(subjectRef) != "" {
		if err := requireID("subject ref", subjectRef); err != nil {
			return nil, err
		}
		query += ` AND subject_ref = ?`
		args = append(args, subjectRef)
	}
	query += ` ORDER BY subject_ref, revision, description_document_id LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list description summaries: %w", err)
	}
	defer rows.Close()
	records := make([]DescriptionDocument, 0, limit)
	for rows.Next() {
		record, err := scanDescriptionSummary(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate description summaries: %w", err)
	}
	return records, nil
}

func (tx *Tx) InsertSemanticSegment(ctx context.Context, record *SemanticSegment) error {
	if record == nil {
		return errors.New("semantic segment is required")
	}
	for name, value := range map[string]string{
		"semantic segment id":     record.ID,
		"workspace id":            record.WorkspaceID,
		"description document id": record.DocumentID,
		"subject ref":             record.SubjectRef,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	if record.Ordinal < 0 {
		return errors.New("semantic segment ordinal cannot be negative")
	}
	if strings.TrimSpace(record.Text) == "" {
		return errors.New("semantic segment text is required")
	}
	if strings.TrimSpace(record.Language) == "" {
		record.Language = "und"
	}
	if record.TextDigest == "" {
		record.TextDigest = digestText(record.Text)
	}
	sourceSpan, err := normalizeJSON(record.SourceSpan)
	if err != nil {
		return fmt.Errorf("semantic segment source span: %w", err)
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("semantic segment metadata: %w", err)
	}
	record.SourceSpan, record.Metadata = sourceSpan, metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if err := insertOne(ctx, tx.tx, `
INSERT INTO semantic_segments(
    semantic_segment_id, workspace_id, description_document_id, subject_ref,
    ordinal, text, text_digest, language, section, source_span_json,
    metadata_json, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.DocumentID, record.SubjectRef,
		record.Ordinal, record.Text, record.TextDigest, record.Language, record.Section,
		string(sourceSpan), string(metadata), record.CreatedAt.UnixNano()); err != nil {
		return fmt.Errorf("insert semantic segment: %w", err)
	}
	return nil
}

func (s *Store) InsertSemanticSegment(ctx context.Context, record *SemanticSegment) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.InsertSemanticSegment(ctx, record) })
}

const semanticSegmentSelect = `
SELECT semantic_segment_id, workspace_id, description_document_id, subject_ref,
       ordinal, text, text_digest, language, section, source_span_json,
       metadata_json, created_at_ns
FROM semantic_segments `

func scanSemanticSegment(scanner rowScanner) (SemanticSegment, error) {
	var record SemanticSegment
	var sourceSpan, metadata string
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.DocumentID, &record.SubjectRef,
		&record.Ordinal, &record.Text, &record.TextDigest, &record.Language,
		&record.Section, &sourceSpan, &metadata, &created,
	); err != nil {
		return record, rowError("semantic segment", err)
	}
	record.SourceSpan = json.RawMessage(sourceSpan)
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

func (s *Store) GetSemanticSegment(ctx context.Context, workspaceID, segmentID string) (SemanticSegment, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return SemanticSegment{}, err
	}
	if err := requireID("semantic segment id", segmentID); err != nil {
		return SemanticSegment{}, err
	}
	return scanSemanticSegment(s.db.QueryRowContext(ctx, semanticSegmentSelect+
		`WHERE workspace_id = ? AND semantic_segment_id = ?`, workspaceID, segmentID))
}

func (s *Store) ListSemanticSegments(ctx context.Context, workspaceID, documentID string) ([]SemanticSegment, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	if err := requireID("description document id", documentID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, semanticSegmentSelect+
		`WHERE workspace_id = ? AND description_document_id = ? ORDER BY ordinal, semantic_segment_id`,
		workspaceID, documentID)
	if err != nil {
		return nil, fmt.Errorf("list semantic segments: %w", err)
	}
	defer rows.Close()
	var records []SemanticSegment
	for rows.Next() {
		record, err := scanSemanticSegment(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate semantic segments: %w", err)
	}
	return records, nil
}
