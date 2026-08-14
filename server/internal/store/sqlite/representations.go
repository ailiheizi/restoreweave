package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (tx *Tx) InsertRepresentation(ctx context.Context, record *Representation) error {
	if record == nil {
		return errors.New("representation is required")
	}
	if err := requireID("representation id", record.ID); err != nil {
		return err
	}
	if err := requireID("workspace id", record.WorkspaceID); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"representation content id":    record.ContentID,
		"representation codec profile": record.CodecProfileRef,
		"representation record digest": record.RecordDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if record.DecodedLength < 0 || record.MinimumReadableUnit < 0 || record.SeekCheckpointInterval < 0 {
		return errors.New("representation lengths and intervals cannot be negative")
	}
	if !validOwnershipMode(record.OwnershipMode) {
		return fmt.Errorf("invalid representation ownership mode %q", record.OwnershipMode)
	}
	if !validAccessMode(record.AccessMode) {
		return fmt.Errorf("invalid representation access mode %q", record.AccessMode)
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("representation metadata: %w", err)
	}
	record.Metadata = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	err = insertOne(ctx, tx.tx, `
INSERT INTO representations(
    representation_id, workspace_id, content_id, decoded_length,
    ownership_mode, codec_profile_ref, access_mode, minimum_readable_unit,
    seek_checkpoint_interval, whole_read_required_to_verify, record_digest,
    metadata_json, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.ContentID, record.DecodedLength,
		record.OwnershipMode, record.CodecProfileRef, record.AccessMode,
		record.MinimumReadableUnit, record.SeekCheckpointInterval,
		boolInt(record.WholeReadRequiredToVerify), record.RecordDigest,
		string(metadata), record.CreatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert representation: %w", err)
	}
	return nil
}

func (s *Store) ListRepresentationsByContentID(ctx context.Context, workspaceID, contentID string) ([]Representation, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	if err := requireText("content id", contentID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT representation_id, workspace_id, content_id, decoded_length,
       ownership_mode, codec_profile_ref, access_mode, minimum_readable_unit,
       seek_checkpoint_interval, whole_read_required_to_verify, record_digest,
       metadata_json, created_at_ns
FROM representations
WHERE workspace_id = ? AND content_id = ?
ORDER BY representation_id`, workspaceID, contentID)
	if err != nil {
		return nil, fmt.Errorf("list representations: %w", err)
	}
	defer rows.Close()
	var records []Representation
	for rows.Next() {
		record, err := scanRepresentation(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate representations: %w", err)
	}
	return records, nil
}

func (s *Store) GetRepresentation(ctx context.Context, workspaceID, representationID string) (Representation, error) {
	return scanRepresentation(s.db.QueryRowContext(ctx, `
SELECT representation_id, workspace_id, content_id, decoded_length,
       ownership_mode, codec_profile_ref, access_mode, minimum_readable_unit,
       seek_checkpoint_interval, whole_read_required_to_verify, record_digest,
       metadata_json, created_at_ns
FROM representations WHERE workspace_id = ? AND representation_id = ?`, workspaceID, representationID))
}

func scanRepresentation(scanner rowScanner) (Representation, error) {
	var record Representation
	var wholeRead int
	var metadata string
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.ContentID, &record.DecodedLength,
		&record.OwnershipMode, &record.CodecProfileRef, &record.AccessMode,
		&record.MinimumReadableUnit, &record.SeekCheckpointInterval, &wholeRead,
		&record.RecordDigest, &metadata, &created,
	); err != nil {
		return record, rowError("representation", err)
	}
	record.WholeReadRequiredToVerify = wholeRead == 1
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

func (tx *Tx) InsertFileVersion(ctx context.Context, record *FileVersion) error {
	if record == nil {
		return errors.New("file version is required")
	}
	for name, value := range map[string]string{
		"file version id":                 record.ID,
		"workspace id":                    record.WorkspaceID,
		"scan generation id":              record.ScanGenerationID,
		"observation id":                  record.ObservationID,
		"authoritative representation id": record.AuthoritativeRepresentationID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"file content id":            record.ContentID,
		"file hashing profile":       record.HashingProfile,
		"file extent-set digest":     record.ExtentSetDigest,
		"file-version record digest": record.RecordDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if record.LogicalSize < 0 {
		return errors.New("file version logical size cannot be negative")
	}
	sparseEvidence, err := normalizeJSON(record.SparseEvidence)
	if err != nil {
		return fmt.Errorf("file version sparse evidence: %w", err)
	}
	var representationContentID string
	var decodedLength int64
	if err := tx.tx.QueryRowContext(ctx, `
SELECT content_id, decoded_length FROM representations
WHERE workspace_id = ? AND representation_id = ?`,
		record.WorkspaceID, record.AuthoritativeRepresentationID).Scan(
		&representationContentID, &decodedLength); err != nil {
		return rowError("authoritative representation", err)
	}
	if representationContentID != record.ContentID || decodedLength != record.LogicalSize {
		return errors.New("file version content identity or length differs from its authoritative representation")
	}
	var observationContentID string
	if err := tx.tx.QueryRowContext(ctx, `
SELECT content_id FROM observations
WHERE workspace_id = ? AND scan_generation_id = ? AND observation_id = ?`,
		record.WorkspaceID, record.ScanGenerationID, record.ObservationID).Scan(&observationContentID); err != nil {
		return rowError("file observation", err)
	}
	if observationContentID != "" && observationContentID != record.ContentID {
		return errors.New("file version content identity differs from its source observation")
	}
	record.SparseEvidence = sparseEvidence
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	err = insertOne(ctx, tx.tx, `
INSERT INTO file_versions(
    file_version_id, workspace_id, scan_generation_id, observation_id,
    asset_id, content_id, logical_size, hashing_profile,
    authoritative_representation_id, extent_set_digest, hardlink_group_id,
    sparse_evidence_json, verification_ref, record_digest, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.ScanGenerationID, record.ObservationID,
		record.AssetID, record.ContentID, record.LogicalSize, record.HashingProfile,
		record.AuthoritativeRepresentationID, record.ExtentSetDigest,
		record.HardlinkGroupID, string(sparseEvidence), record.VerificationRef,
		record.RecordDigest, record.CreatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert file version: %w", err)
	}
	return nil
}

func (s *Store) GetFileVersion(ctx context.Context, workspaceID, fileVersionID string) (FileVersion, error) {
	var record FileVersion
	var sparseEvidence string
	var created int64
	err := s.db.QueryRowContext(ctx, `
SELECT file_version_id, workspace_id, scan_generation_id, observation_id,
       asset_id, content_id, logical_size, hashing_profile,
       authoritative_representation_id, extent_set_digest, hardlink_group_id,
       sparse_evidence_json, verification_ref, record_digest, created_at_ns
FROM file_versions WHERE workspace_id = ? AND file_version_id = ?`, workspaceID, fileVersionID).Scan(
		&record.ID, &record.WorkspaceID, &record.ScanGenerationID,
		&record.ObservationID, &record.AssetID, &record.ContentID,
		&record.LogicalSize, &record.HashingProfile,
		&record.AuthoritativeRepresentationID, &record.ExtentSetDigest,
		&record.HardlinkGroupID, &sparseEvidence, &record.VerificationRef,
		&record.RecordDigest, &created,
	)
	if err != nil {
		return record, rowError("file version", err)
	}
	record.SparseEvidence = json.RawMessage(sparseEvidence)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

func (tx *Tx) InsertEngineReadRef(ctx context.Context, record *EngineReadRef) error {
	if record == nil {
		return errors.New("engine read ref is required")
	}
	for name, value := range map[string]string{
		"engine read ref id": record.ID,
		"workspace id":       record.WorkspaceID,
		"representation id":  record.RepresentationID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"engine repository id":            record.RepositoryID,
		"engine snapshot reference":       record.EngineSnapshotRef,
		"engine receipt reference":        record.EngineReceiptRef,
		"placement checkpoint id":         record.PlacementCheckpointID,
		"placement checkpoint digest":     record.PlacementCheckpointDigest,
		"engine reader profile reference": record.ReaderProfileRef,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if record.EnginePathKey == nil {
		return errors.New("engine path key must be present; an empty byte key is valid")
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("engine read ref metadata: %w", err)
	}
	record.Metadata = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	err = insertOne(ctx, tx.tx, `
INSERT INTO engine_read_refs(
    engine_read_ref_id, workspace_id, representation_id, repository_id,
    engine_snapshot_ref, engine_receipt_ref, engine_path_key,
    placement_checkpoint_id, placement_checkpoint_digest,
    reader_profile_ref, metadata_json, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.RepresentationID,
		record.RepositoryID, record.EngineSnapshotRef, record.EngineReceiptRef,
		record.EnginePathKey, record.PlacementCheckpointID,
		record.PlacementCheckpointDigest, record.ReaderProfileRef,
		string(metadata), record.CreatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert engine read ref: %w", err)
	}
	return nil
}

func (s *Store) ListEngineReadRefs(
	ctx context.Context,
	workspaceID, representationID string,
) ([]EngineReadRef, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT engine_read_ref_id, workspace_id, representation_id, repository_id,
       engine_snapshot_ref, engine_receipt_ref, engine_path_key,
       placement_checkpoint_id, placement_checkpoint_digest,
       reader_profile_ref, metadata_json, created_at_ns
FROM engine_read_refs
WHERE workspace_id = ? AND representation_id = ?
ORDER BY repository_id, engine_read_ref_id`, workspaceID, representationID)
	if err != nil {
		return nil, fmt.Errorf("list engine read refs: %w", err)
	}
	defer rows.Close()
	var records []EngineReadRef
	for rows.Next() {
		var record EngineReadRef
		var metadata string
		var created int64
		if err := rows.Scan(
			&record.ID, &record.WorkspaceID, &record.RepresentationID,
			&record.RepositoryID, &record.EngineSnapshotRef,
			&record.EngineReceiptRef, &record.EnginePathKey,
			&record.PlacementCheckpointID, &record.PlacementCheckpointDigest,
			&record.ReaderProfileRef, &metadata, &created,
		); err != nil {
			return nil, fmt.Errorf("scan engine read ref: %w", err)
		}
		record.Metadata = json.RawMessage(metadata)
		record.CreatedAt = time.Unix(0, created).UTC()
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate engine read refs: %w", err)
	}
	return records, nil
}

func (s *Store) ListPhysicalLocatorsForRepresentation(
	ctx context.Context,
	workspaceID, representationID string,
) ([]PhysicalLocator, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT physical_locator_id, workspace_id, representation_id, content_id,
       ownership_mode, locator_kind, backend_id, repository_id,
       placement_generation, container_ref, byte_offset, byte_length,
       encoded_length, encoded_digest, authority_ref, reader_profile_ref,
       locator_json, created_at_ns
FROM physical_locator_projections
WHERE workspace_id = ? AND representation_id = ?
ORDER BY placement_generation DESC, repository_id, physical_locator_id`,
		workspaceID, representationID)
	if err != nil {
		return nil, fmt.Errorf("list physical locator projections: %w", err)
	}
	defer rows.Close()
	var records []PhysicalLocator
	for rows.Next() {
		record, err := scanPhysicalLocator(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate physical locator projections: %w", err)
	}
	return records, nil
}

func validAccessMode(value AccessMode) bool {
	switch value {
	case AccessRandomNative, AccessRandomCheckpointed, AccessSequentialStream, AccessWholeObjectOnly:
		return true
	default:
		return false
	}
}
