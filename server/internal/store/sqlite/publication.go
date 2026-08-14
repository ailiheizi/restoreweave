package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

func (s *Store) GetWorkspaceByName(ctx context.Context, name string) (Workspace, error) {
	if err := requireText("workspace name", name); err != nil {
		return Workspace{}, err
	}
	return scanWorkspace(s.db.QueryRowContext(ctx, `
SELECT workspace_id, name, metadata_json, revision, created_at_ns, updated_at_ns
FROM workspaces WHERE name = ? ORDER BY created_at_ns ASC, workspace_id ASC LIMIT 1`, name))
}

func (s *Store) GetSourceByStableKey(ctx context.Context, workspaceID, stableKey string) (Source, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return Source{}, err
	}
	if err := requireText("source stable key", stableKey); err != nil {
		return Source{}, err
	}
	return scanSource(s.db.QueryRowContext(ctx, `
SELECT source_id, workspace_id, stable_key, kind, locator, identity_fingerprint,
       state, metadata_json, revision, created_at_ns, updated_at_ns
FROM sources WHERE workspace_id = ? AND stable_key = ?`, workspaceID, stableKey))
}

func (tx *Tx) NextScanGeneration(ctx context.Context, workspaceID, sourceID string) (int64, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return 0, err
	}
	if err := requireID("source id", sourceID); err != nil {
		return 0, err
	}
	var current sql.NullInt64
	if err := tx.tx.QueryRowContext(ctx, `
SELECT MAX(generation) FROM scan_generations WHERE workspace_id = ? AND source_id = ?`,
		workspaceID, sourceID).Scan(&current); err != nil {
		return 0, fmt.Errorf("read next scan generation: %w", err)
	}
	if !current.Valid {
		return 1, nil
	}
	return current.Int64 + 1, nil
}

func (tx *Tx) InsertCaptureRootBinding(ctx context.Context, record *CaptureRootBinding) error {
	if record == nil {
		return errors.New("capture root binding is required")
	}
	for name, value := range map[string]string{
		"binding id":         record.ID,
		"workspace id":       record.WorkspaceID,
		"source id":          record.SourceID,
		"scan generation id": record.ScanGenerationID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"capture mode":      record.CaptureMode,
		"capture profile":   record.Profile,
		"display path":      record.DisplayPath,
		"consistency claim": record.ConsistencyClaim,
		"identity digest":   record.IdentityDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if record.CaptureMode != "ROOTED_FD" {
		return errors.New("only ROOTED_FD capture bindings may be stored")
	}
	device, err := uint64ToInt64("device id", record.DeviceID)
	if err != nil {
		return err
	}
	inode, err := uint64ToInt64("inode", record.Inode)
	if err != nil {
		return err
	}
	payload, err := normalizeJSON(record.Record)
	if err != nil {
		return fmt.Errorf("binding record: %w", err)
	}
	record.Record = payload
	record.BoundAt = recordTime(record.BoundAt, tx.now)
	err = insertOne(ctx, tx.tx, `
INSERT INTO capture_root_bindings(
    binding_id, workspace_id, source_id, scan_generation_id, capture_mode,
    profile, display_path, device_id, inode, consistency_claim,
    identity_digest, bound_at_ns, record_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SourceID, record.ScanGenerationID,
		record.CaptureMode, record.Profile, record.DisplayPath, device, inode,
		record.ConsistencyClaim, record.IdentityDigest, record.BoundAt.UnixNano(),
		string(payload))
	if err != nil {
		return fmt.Errorf("insert capture root binding: %w", err)
	}
	return nil
}

func (s *Store) GetCaptureRootBinding(ctx context.Context, workspaceID, bindingID string) (CaptureRootBinding, error) {
	return scanCaptureRootBinding(s.db.QueryRowContext(ctx, `
SELECT binding_id, workspace_id, source_id, scan_generation_id, capture_mode,
       profile, display_path, device_id, inode, consistency_claim,
       identity_digest, bound_at_ns, record_json
FROM capture_root_bindings WHERE workspace_id = ? AND binding_id = ?`,
		workspaceID, bindingID))
}

func scanCaptureRootBinding(scanner rowScanner) (CaptureRootBinding, error) {
	var record CaptureRootBinding
	var device, inode, bound int64
	var payload string
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SourceID, &record.ScanGenerationID,
		&record.CaptureMode, &record.Profile, &record.DisplayPath, &device, &inode,
		&record.ConsistencyClaim, &record.IdentityDigest, &bound, &payload,
	); err != nil {
		return record, rowError("capture root binding", err)
	}
	record.DeviceID = uint64(device)
	record.Inode = uint64(inode)
	record.BoundAt = time.Unix(0, bound).UTC()
	record.Record = json.RawMessage(payload)
	return record, nil
}

func (tx *Tx) InsertPublication(ctx context.Context, record *Publication) error {
	if record == nil {
		return errors.New("publication is required")
	}
	for name, value := range map[string]string{
		"publication id":     record.ID,
		"workspace id":       record.WorkspaceID,
		"scan generation id": record.ScanGenerationID,
		"binding id":         record.BindingID,
		"namespace root id":  record.NamespaceRootID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"snapshot ref":    record.SnapshotRef,
		"manifest digest": record.ManifestDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("publication metadata: %w", err)
	}
	record.Metadata = metadata
	record.CommittedAt = recordTime(record.CommittedAt, tx.now)
	err = insertOne(ctx, tx.tx, `
INSERT INTO publications(
    publication_id, workspace_id, snapshot_ref, scan_generation_id,
    binding_id, namespace_root_id, manifest_digest, committed_at_ns, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SnapshotRef, record.ScanGenerationID,
		record.BindingID, record.NamespaceRootID, record.ManifestDigest,
		record.CommittedAt.UnixNano(), string(metadata))
	if err != nil {
		return fmt.Errorf("insert publication: %w", err)
	}
	return nil
}

func (s *Store) GetPublication(ctx context.Context, workspaceID, publicationID string) (Publication, error) {
	return scanPublication(s.db.QueryRowContext(ctx, `
SELECT publication_id, workspace_id, snapshot_ref, scan_generation_id,
       binding_id, namespace_root_id, manifest_digest, committed_at_ns, metadata_json
FROM publications WHERE workspace_id = ? AND publication_id = ?`, workspaceID, publicationID))
}

func (s *Store) GetPublicationBySnapshotRef(ctx context.Context, workspaceID, snapshotRef string) (Publication, error) {
	query := `
SELECT publication_id, workspace_id, snapshot_ref, scan_generation_id,
       binding_id, namespace_root_id, manifest_digest, committed_at_ns, metadata_json
FROM publications WHERE snapshot_ref = ?`
	args := []any{snapshotRef}
	if workspaceID != "" {
		query += ` AND workspace_id = ?`
		args = append(args, workspaceID)
	}
	query += ` LIMIT 1`
	return scanPublication(s.db.QueryRowContext(ctx, query, args...))
}

func (s *Store) ListPublications(ctx context.Context) ([]Publication, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT publication_id, workspace_id, snapshot_ref, scan_generation_id,
       binding_id, namespace_root_id, manifest_digest, committed_at_ns, metadata_json
FROM publications ORDER BY committed_at_ns ASC, snapshot_ref ASC`)
	if err != nil {
		return nil, fmt.Errorf("list publications: %w", err)
	}
	defer rows.Close()
	var records []Publication
	for rows.Next() {
		record, err := scanPublication(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate publications: %w", err)
	}
	return records, nil
}

func scanPublication(scanner rowScanner) (Publication, error) {
	var record Publication
	var metadata string
	var committed int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SnapshotRef, &record.ScanGenerationID,
		&record.BindingID, &record.NamespaceRootID, &record.ManifestDigest,
		&committed, &metadata,
	); err != nil {
		return record, rowError("publication", err)
	}
	record.CommittedAt = time.Unix(0, committed).UTC()
	record.Metadata = json.RawMessage(metadata)
	return record, nil
}

func uint64ToInt64(name string, value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("%s exceeds catalog integer range", name)
	}
	return int64(value), nil
}
