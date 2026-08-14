package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (tx *Tx) InsertNamespaceRoot(ctx context.Context, record *NamespaceRoot) error {
	if record == nil {
		return errors.New("namespace root is required")
	}
	for name, value := range map[string]string{
		"namespace root id":  record.ID,
		"workspace id":       record.WorkspaceID,
		"source id":          record.SourceID,
		"scan generation id": record.ScanGenerationID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"snapshot reference":    record.SnapshotRef,
		"namespace root name":   record.Name,
		"filesystem semantics":  record.FilesystemSemantics,
		"root authority digest": record.AuthorityDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if record.RootPathKey == nil {
		return errors.New("namespace root path key must be present; an empty byte key is valid")
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("namespace root metadata: %w", err)
	}
	record.Metadata = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	err = insertOne(ctx, tx.tx, `
INSERT INTO namespace_roots(
    namespace_root_id, workspace_id, source_id, scan_generation_id,
    snapshot_ref, name, root_path_key, filesystem_semantics,
    authority_digest, metadata_json, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SourceID, record.ScanGenerationID,
		record.SnapshotRef, record.Name, record.RootPathKey,
		record.FilesystemSemantics, record.AuthorityDigest, string(metadata),
		record.CreatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert namespace root: %w", err)
	}
	return nil
}

func (s *Store) GetNamespaceRoot(ctx context.Context, workspaceID, rootID string) (NamespaceRoot, error) {
	return scanNamespaceRoot(s.db.QueryRowContext(ctx, `
SELECT namespace_root_id, workspace_id, source_id, scan_generation_id,
       snapshot_ref, name, root_path_key, filesystem_semantics,
       authority_digest, metadata_json, created_at_ns
FROM namespace_roots WHERE workspace_id = ? AND namespace_root_id = ?`, workspaceID, rootID))
}

func scanNamespaceRoot(scanner rowScanner) (NamespaceRoot, error) {
	var record NamespaceRoot
	var metadata string
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SourceID, &record.ScanGenerationID,
		&record.SnapshotRef, &record.Name, &record.RootPathKey,
		&record.FilesystemSemantics, &record.AuthorityDigest, &metadata, &created,
	); err != nil {
		return record, rowError("namespace root", err)
	}
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

func (tx *Tx) InsertNamespaceEntry(ctx context.Context, record *NamespaceEntry) error {
	if record == nil {
		return errors.New("namespace entry is required")
	}
	for name, value := range map[string]string{
		"namespace entry id": record.ID,
		"workspace id":       record.WorkspaceID,
		"namespace root id":  record.RootID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	if record.ParentID != "" {
		if err := requireID("parent namespace entry id", record.ParentID); err != nil {
			return err
		}
	}
	if record.ObservationID != "" {
		if err := requireID("observation id", record.ObservationID); err != nil {
			return err
		}
	}
	if len(record.RawName) == 0 {
		return errors.New("raw namespace entry name is required")
	}
	if err := requireByteKey("namespace full path key", record.FullPathKey); err != nil {
		return err
	}
	if !validEntryType(record.EntryType) {
		return fmt.Errorf("invalid namespace entry type %q", record.EntryType)
	}
	if record.EntryType == EntrySymlink && record.SymlinkTargetRaw == nil {
		return errors.New("symlink target bytes must be present")
	}
	if record.EntryType != EntrySymlink && record.SymlinkTargetRaw != nil {
		return errors.New("only symlink entries may carry symlink target bytes")
	}
	if record.EntryType == EntryFile && record.FileVersionID == "" {
		return errors.New("a regular-file namespace entry requires a file version id")
	}
	if record.EntryType != EntryFile && record.FileVersionID != "" {
		return errors.New("only regular-file namespace entries may reference a file version")
	}
	if record.FileVersionID != "" {
		if err := requireID("file version id", record.FileVersionID); err != nil {
			return err
		}
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("namespace entry metadata: %w", err)
	}
	record.Metadata = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	err = insertOne(ctx, tx.tx, `
INSERT INTO namespace_entries(
    namespace_entry_id, workspace_id, namespace_root_id, parent_entry_id,
    observation_id, raw_name, display_name, full_path_key, entry_type,
    metadata_json, content_id, file_version_id, symlink_target_raw,
    symlink_target_display, hardlink_group_id, logical_size,
    allocated_size, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.RootID, nullableString(record.ParentID),
		nullableString(record.ObservationID), record.RawName, record.DisplayName,
		record.FullPathKey, record.EntryType, string(metadata), record.ContentID,
		nullableString(record.FileVersionID), nullableBytes(record.SymlinkTargetRaw),
		record.SymlinkTargetDisplay, record.HardlinkGroupID,
		nullableInt64(record.LogicalSize), nullableInt64(record.AllocatedSize),
		record.CreatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert namespace entry: %w", err)
	}
	return nil
}

func (s *Store) LookupNamespaceEntry(
	ctx context.Context,
	workspaceID, rootID string,
	fullPathKey []byte,
) (NamespaceEntry, error) {
	if fullPathKey == nil {
		return NamespaceEntry{}, errors.New("namespace full path key must be present")
	}
	return scanNamespaceEntry(s.db.QueryRowContext(ctx, namespaceEntrySelect+`
WHERE workspace_id = ? AND namespace_root_id = ? AND full_path_key = ?`,
		workspaceID, rootID, fullPathKey))
}

func (s *Store) GetNamespaceEntry(ctx context.Context, workspaceID, entryID string) (NamespaceEntry, error) {
	return scanNamespaceEntry(s.db.QueryRowContext(ctx, namespaceEntrySelect+`
WHERE workspace_id = ? AND namespace_entry_id = ?`, workspaceID, entryID))
}

func (s *Store) LookupNamespaceChild(
	ctx context.Context,
	workspaceID, rootID, parentID string,
	rawName []byte,
	displayName string,
) (NamespaceEntry, error) {
	if len(rawName) == 0 && displayName == "" {
		return NamespaceEntry{}, errors.New("namespace child name is required")
	}
	query := namespaceEntrySelect + `
WHERE workspace_id = ? AND namespace_root_id = ?`
	args := []any{workspaceID, rootID}
	if parentID == "" || parentID == rootID {
		query += ` AND parent_entry_id IS NULL`
	} else {
		query += ` AND parent_entry_id = ?`
		args = append(args, parentID)
	}
	if len(rawName) > 0 {
		query += ` AND raw_name = ?`
		args = append(args, rawName)
	} else {
		query += ` AND display_name = ?`
		args = append(args, displayName)
	}
	query += ` ORDER BY namespace_entry_id LIMIT 1`
	return scanNamespaceEntry(s.db.QueryRowContext(ctx, query, args...))
}

func (s *Store) ListNamespaceChildren(
	ctx context.Context,
	workspaceID, rootID, parentID string,
) ([]NamespaceEntry, error) {
	query := namespaceEntrySelect + `
WHERE workspace_id = ? AND namespace_root_id = ? AND parent_entry_id IS NULL
ORDER BY raw_name, namespace_entry_id`
	args := []any{workspaceID, rootID}
	if parentID != "" {
		query = namespaceEntrySelect + `
WHERE workspace_id = ? AND namespace_root_id = ? AND parent_entry_id = ?
ORDER BY raw_name, namespace_entry_id`
		args = append(args, parentID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list namespace children: %w", err)
	}
	defer rows.Close()
	var entries []NamespaceEntry
	for rows.Next() {
		entry, err := scanNamespaceEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate namespace children: %w", err)
	}
	return entries, nil
}

// ListNamespaceSubtree returns the selected entry and all descendants. An
// empty startEntryID returns all top-level entries and descendants. Depth is
// relative to the selected entry or logical root.
func (s *Store) ListNamespaceSubtree(
	ctx context.Context,
	workspaceID, rootID, startEntryID string,
) ([]NamespaceNode, error) {
	rows, err := s.db.QueryContext(ctx, `
WITH RECURSIVE tree(namespace_entry_id, depth) AS (
    SELECT namespace_entry_id, 0
    FROM namespace_entries
    WHERE workspace_id = ? AND namespace_root_id = ?
      AND ((? = '' AND parent_entry_id IS NULL) OR namespace_entry_id = ?)
    UNION ALL
    SELECT child.namespace_entry_id, tree.depth + 1
    FROM namespace_entries AS child
    JOIN tree ON child.parent_entry_id = tree.namespace_entry_id
    WHERE child.workspace_id = ? AND child.namespace_root_id = ?
)
SELECT e.namespace_entry_id, e.workspace_id, e.namespace_root_id,
       e.parent_entry_id, e.observation_id, e.raw_name, e.display_name,
       e.full_path_key, e.entry_type, e.metadata_json, e.content_id,
       e.file_version_id, e.symlink_target_raw, e.symlink_target_display,
       e.hardlink_group_id, e.logical_size, e.allocated_size, e.created_at_ns,
       tree.depth
FROM tree
JOIN namespace_entries AS e ON e.namespace_entry_id = tree.namespace_entry_id
ORDER BY tree.depth, e.full_path_key, e.namespace_entry_id`,
		workspaceID, rootID, startEntryID, startEntryID, workspaceID, rootID)
	if err != nil {
		return nil, fmt.Errorf("list namespace subtree: %w", err)
	}
	defer rows.Close()
	var nodes []NamespaceNode
	for rows.Next() {
		entry, depth, err := scanNamespaceEntryWithDepth(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, NamespaceNode{Entry: entry, Depth: depth})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate namespace subtree: %w", err)
	}
	return nodes, nil
}

const namespaceEntrySelect = `
SELECT namespace_entry_id, workspace_id, namespace_root_id, parent_entry_id,
       observation_id, raw_name, display_name, full_path_key, entry_type,
       metadata_json, content_id, file_version_id, symlink_target_raw,
       symlink_target_display, hardlink_group_id, logical_size,
       allocated_size, created_at_ns
FROM namespace_entries `

func scanNamespaceEntry(scanner rowScanner) (NamespaceEntry, error) {
	record, _, err := scanNamespaceEntryFields(scanner, false)
	return record, err
}

func scanNamespaceEntryWithDepth(scanner rowScanner) (NamespaceEntry, int64, error) {
	return scanNamespaceEntryFields(scanner, true)
}

func scanNamespaceEntryFields(scanner rowScanner, withDepth bool) (NamespaceEntry, int64, error) {
	var record NamespaceEntry
	var parentID, observationID, fileVersionID sql.NullString
	var symlinkTarget []byte
	var logical, allocated sql.NullInt64
	var metadata string
	var created int64
	var depth int64
	destinations := []any{
		&record.ID, &record.WorkspaceID, &record.RootID, &parentID,
		&observationID, &record.RawName, &record.DisplayName, &record.FullPathKey,
		&record.EntryType, &metadata, &record.ContentID, &fileVersionID,
		&symlinkTarget, &record.SymlinkTargetDisplay, &record.HardlinkGroupID,
		&logical, &allocated, &created,
	}
	if withDepth {
		destinations = append(destinations, &depth)
	}
	if err := scanner.Scan(destinations...); err != nil {
		return record, 0, rowError("namespace entry", err)
	}
	record.ParentID = parentID.String
	record.ObservationID = observationID.String
	record.FileVersionID = fileVersionID.String
	if record.EntryType == EntrySymlink {
		record.SymlinkTargetRaw = symlinkTarget
	}
	record.Metadata = json.RawMessage(metadata)
	record.LogicalSize = int64Pointer(logical)
	record.AllocatedSize = int64Pointer(allocated)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, depth, nil
}

func (tx *Tx) InsertPhysicalLocator(ctx context.Context, record *PhysicalLocator) error {
	if record == nil {
		return errors.New("physical locator is required")
	}
	if err := requireID("physical locator id", record.ID); err != nil {
		return err
	}
	if err := requireID("workspace id", record.WorkspaceID); err != nil {
		return err
	}
	if record.OwnershipMode != OwnershipRestoreWeavePacks && record.OwnershipMode != OwnershipInline {
		return fmt.Errorf("invalid ownership mode %q", record.OwnershipMode)
	}
	if !validLocatorKind(record.Kind) {
		return fmt.Errorf("invalid physical locator kind %q", record.Kind)
	}
	if err := requireID("representation id", record.RepresentationID); err != nil {
		return err
	}
	if err := requireText("physical locator authority reference", record.AuthorityRef); err != nil {
		return err
	}
	if err := requireText("physical locator reader profile reference", record.ReaderProfileRef); err != nil {
		return err
	}
	if record.PlacementGeneration < 0 {
		return errors.New("physical locator placement generation cannot be negative")
	}
	if (record.ByteOffset == nil) != (record.ByteLength == nil) {
		return errors.New("physical locator byte offset and length must be supplied together")
	}
	if record.ByteOffset != nil && (*record.ByteOffset < 0 || *record.ByteLength <= 0) {
		return errors.New("physical locator byte range is invalid")
	}
	if isRangeLocator(record.Kind) {
		if record.ByteOffset == nil || record.ContainerRef == "" {
			return errors.New("pack-range locators require a container reference and byte range")
		}
	}
	if record.EncodedLength != nil && *record.EncodedLength < 0 {
		return errors.New("physical locator encoded length cannot be negative")
	}
	locatorJSON, err := normalizeJSON(record.Locator)
	if err != nil {
		return fmt.Errorf("physical locator metadata: %w", err)
	}
	record.Locator = locatorJSON
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	err = insertOne(ctx, tx.tx, `
INSERT INTO physical_locator_projections(
    physical_locator_id, workspace_id, representation_id, content_id,
    ownership_mode, locator_kind, backend_id, repository_id,
    placement_generation, container_ref, byte_offset, byte_length,
    encoded_length, encoded_digest, authority_ref, reader_profile_ref,
    locator_json, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.RepresentationID, record.ContentID,
		record.OwnershipMode, record.Kind, record.BackendID, record.RepositoryID,
		record.PlacementGeneration, record.ContainerRef,
		nullableInt64(record.ByteOffset), nullableInt64(record.ByteLength),
		nullableInt64(record.EncodedLength), record.EncodedDigest,
		record.AuthorityRef, record.ReaderProfileRef,
		string(locatorJSON), record.CreatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert physical locator: %w", err)
	}
	return nil
}

func (s *Store) GetPhysicalLocator(ctx context.Context, workspaceID, locatorID string) (PhysicalLocator, error) {
	return scanPhysicalLocator(s.db.QueryRowContext(ctx, `
SELECT physical_locator_id, workspace_id, representation_id, content_id,
       ownership_mode, locator_kind, backend_id, repository_id,
       placement_generation, container_ref, byte_offset, byte_length,
       encoded_length, encoded_digest, authority_ref, reader_profile_ref,
       locator_json, created_at_ns
FROM physical_locator_projections WHERE workspace_id = ? AND physical_locator_id = ?`, workspaceID, locatorID))
}

func scanPhysicalLocator(scanner rowScanner) (PhysicalLocator, error) {
	var record PhysicalLocator
	var offset, length, encodedLength sql.NullInt64
	var locatorJSON string
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.RepresentationID,
		&record.ContentID, &record.OwnershipMode, &record.Kind, &record.BackendID,
		&record.RepositoryID, &record.PlacementGeneration, &record.ContainerRef,
		&offset, &length, &encodedLength, &record.EncodedDigest, &record.AuthorityRef,
		&record.ReaderProfileRef, &locatorJSON, &created,
	); err != nil {
		return record, rowError("physical locator", err)
	}
	record.ByteOffset = int64Pointer(offset)
	record.ByteLength = int64Pointer(length)
	record.EncodedLength = int64Pointer(encodedLength)
	record.Locator = json.RawMessage(locatorJSON)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

func (tx *Tx) InsertContentExtent(ctx context.Context, record *ContentExtent) error {
	if record == nil {
		return errors.New("content extent is required")
	}
	for name, value := range map[string]string{
		"content extent id": record.ID,
		"workspace id":      record.WorkspaceID,
		"file version id":   record.FileVersionID,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	if !validExtentKind(record.Kind) {
		return fmt.Errorf("invalid extent kind %q", record.Kind)
	}
	if record.Ordinal < 0 || record.LogicalOffset < 0 || record.LogicalLength <= 0 {
		return errors.New("content extent ordinal, offset, or length is invalid")
	}
	if record.RepresentationOffset < 0 {
		return errors.New("content extent representation offset cannot be negative")
	}
	if record.Kind == ExtentData {
		if err := requireID("representation id", record.RepresentationID); err != nil {
			return err
		}
	} else if record.RepresentationID != "" || record.RepresentationOffset != 0 {
		return errors.New("a sparse hole cannot reference representation bytes")
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("content extent metadata: %w", err)
	}

	var fileLogicalSize int64
	if err := tx.tx.QueryRowContext(ctx, `
SELECT logical_size FROM file_versions
WHERE workspace_id = ? AND file_version_id = ?`,
		record.WorkspaceID, record.FileVersionID).Scan(&fileLogicalSize); err != nil {
		return rowError("file version", err)
	}
	if record.LogicalOffset > fileLogicalSize || record.LogicalLength > fileLogicalSize-record.LogicalOffset {
		return errors.New("content extent exceeds the file version logical size")
	}
	if record.Kind == ExtentData {
		var representationLength int64
		if err := tx.tx.QueryRowContext(ctx, `
SELECT decoded_length FROM representations
WHERE workspace_id = ? AND representation_id = ?`,
			record.WorkspaceID, record.RepresentationID).Scan(&representationLength); err != nil {
			return rowError("representation", err)
		}
		if record.RepresentationOffset > representationLength ||
			record.LogicalLength > representationLength-record.RepresentationOffset {
			return errors.New("content extent exceeds the representation byte stream")
		}
	}

	var previousOrdinal, previousOffset, previousLength int64
	err = tx.tx.QueryRowContext(ctx, `
SELECT ordinal, logical_offset, logical_length
FROM content_extents
WHERE workspace_id = ? AND file_version_id = ?
ORDER BY ordinal DESC LIMIT 1`, record.WorkspaceID, record.FileVersionID).Scan(
		&previousOrdinal, &previousOffset, &previousLength)
	if errors.Is(err, sql.ErrNoRows) {
		if record.Ordinal != 0 || record.LogicalOffset != 0 {
			return errors.New("the first content extent must start at ordinal and offset zero")
		}
	} else if err != nil {
		return fmt.Errorf("read previous content extent: %w", err)
	} else if record.Ordinal != previousOrdinal+1 ||
		record.LogicalOffset != previousOffset+previousLength {
		return errors.New("content extents must be inserted in contiguous logical order; represent sparse gaps as HOLE extents")
	}

	record.Metadata = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	err = insertOne(ctx, tx.tx, `
INSERT INTO content_extents(
    content_extent_id, workspace_id, file_version_id, ordinal,
    logical_offset, logical_length, extent_kind, representation_id,
    representation_offset, extent_digest, metadata_json, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.FileVersionID, record.Ordinal,
		record.LogicalOffset, record.LogicalLength, record.Kind,
		nullableString(record.RepresentationID), record.RepresentationOffset,
		record.ExtentDigest, string(metadata), record.CreatedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("insert content extent: %w", err)
	}
	return nil
}

func (s *Store) ListContentExtents(
	ctx context.Context,
	workspaceID, fileVersionID string,
) ([]ContentExtent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT content_extent_id, workspace_id, file_version_id, ordinal,
       logical_offset, logical_length, extent_kind, representation_id,
       representation_offset, extent_digest, metadata_json, created_at_ns
FROM content_extents
WHERE workspace_id = ? AND file_version_id = ?
ORDER BY ordinal`, workspaceID, fileVersionID)
	if err != nil {
		return nil, fmt.Errorf("list content extents: %w", err)
	}
	defer rows.Close()
	var extents []ContentExtent
	for rows.Next() {
		extent, err := scanContentExtent(rows)
		if err != nil {
			return nil, err
		}
		extents = append(extents, extent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content extents: %w", err)
	}
	return extents, nil
}

func scanContentExtent(scanner rowScanner) (ContentExtent, error) {
	var record ContentExtent
	var representationID sql.NullString
	var metadata string
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.FileVersionID,
		&record.Ordinal, &record.LogicalOffset, &record.LogicalLength,
		&record.Kind, &representationID, &record.RepresentationOffset,
		&record.ExtentDigest, &metadata, &created,
	); err != nil {
		return record, rowError("content extent", err)
	}
	record.RepresentationID = representationID.String
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

// ValidateFileVersionExtents proves that the ordered extent set covers the
// exact logical stream. It should be called before a namespace materialization
// is made browseable or exported as a durable authenticated shard.
func (s *Store) ValidateFileVersionExtents(
	ctx context.Context,
	workspaceID, fileVersionID string,
) error {
	var logicalSize int64
	if err := s.db.QueryRowContext(ctx, `
SELECT logical_size FROM file_versions
WHERE workspace_id = ? AND file_version_id = ?`, workspaceID, fileVersionID).Scan(&logicalSize); err != nil {
		return rowError("file version", err)
	}
	var count, covered int64
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(MAX(logical_offset + logical_length), 0)
FROM content_extents
WHERE workspace_id = ? AND file_version_id = ?`, workspaceID, fileVersionID).Scan(&count, &covered); err != nil {
		return fmt.Errorf("measure file version extent coverage: %w", err)
	}
	if logicalSize == 0 && count == 0 {
		return nil
	}
	if count == 0 || covered != logicalSize {
		return fmt.Errorf("file version extent coverage is incomplete: covered=%d logical_size=%d", covered, logicalSize)
	}
	return nil
}
