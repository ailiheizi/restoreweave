package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SavedView is a dynamic, revisioned query and presentation policy. It is not
// a frozen export and not a garbage-collection root by itself.
type SavedView struct {
	ViewID      string
	Name        string
	Query       string
	Fields      []string
	Scope       string
	Sort        string
	OutputNames string
	Required    []string
	WhenMissing string
	Revision    int64
	CreatedAtNS int64
	UpdatedAtNS int64
}

// ExportManifest is a frozen set of subjects, selected representations, output
// names, target profile, and a canonical digest.
type ExportManifest struct {
	ManifestID     string
	ManifestDigest string
	ViewID         string
	Representation string
	Target         string
	SubjectCount   int
	CreatedAtNS    int64
	Items          []string
}

// InsertSavedView creates the first revision of a named view. A conflicting
// name returns ErrConflict without mutating the existing revision.
func (s *Store) InsertSavedView(ctx context.Context, view SavedView) (SavedView, error) {
	if strings.TrimSpace(view.ViewID) == "" {
		return view, errors.New("view id is required")
	}
	if strings.TrimSpace(view.Name) == "" {
		return view, errors.New("view name is required")
	}
	if strings.TrimSpace(view.Query) == "" {
		return view, errors.New("view query is required")
	}
	if view.Revision < 1 {
		view.Revision = 1
	}
	fieldsJSON, err := json.Marshal(view.Fields)
	if err != nil {
		return view, fmt.Errorf("encode view fields: %w", err)
	}
	requiredJSON, err := json.Marshal(view.Required)
	if err != nil {
		return view, fmt.Errorf("encode view required capabilities: %w", err)
	}
	now := s.now().UTC().UnixNano()
	view.CreatedAtNS = now
	view.UpdatedAtNS = now
	err = s.Update(ctx, func(tx *Tx) error {
		var existing int
		err := tx.tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM saved_views WHERE name = ?`, view.Name).Scan(&existing)
		if err != nil {
			return err
		}
		if existing > 0 {
			return ErrConflict
		}
		_, err = tx.tx.ExecContext(ctx, `
INSERT INTO saved_views(
    view_id, name, query, fields_json, scope, sort, output_names,
    required_json, when_missing, revision, created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			view.ViewID, view.Name, view.Query, string(fieldsJSON), view.Scope, view.Sort,
			view.OutputNames, string(requiredJSON), view.WhenMissing, view.Revision,
			view.CreatedAtNS, view.UpdatedAtNS)
		return err
	})
	if err != nil {
		return view, err
	}
	return view, nil
}

// UpdateSavedView writes a successor revision of a named view. It never edits
// a historical revision in place.
func (s *Store) UpdateSavedView(ctx context.Context, view SavedView) (SavedView, error) {
	if strings.TrimSpace(view.Name) == "" {
		return view, errors.New("view name is required")
	}
	if strings.TrimSpace(view.Query) == "" {
		return view, errors.New("view query is required")
	}
	fieldsJSON, err := json.Marshal(view.Fields)
	if err != nil {
		return view, fmt.Errorf("encode view fields: %w", err)
	}
	requiredJSON, err := json.Marshal(view.Required)
	if err != nil {
		return view, fmt.Errorf("encode view required capabilities: %w", err)
	}
	now := s.now().UTC().UnixNano()
	err = s.Update(ctx, func(tx *Tx) error {
		var revision int64
		err := tx.tx.QueryRowContext(ctx,
			`SELECT revision FROM saved_views WHERE name = ?`, view.Name).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		next := revision + 1
		_, err = tx.tx.ExecContext(ctx, `
UPDATE saved_views
SET query = ?, fields_json = ?, scope = ?, sort = ?, output_names = ?,
    required_json = ?, when_missing = ?, revision = ?, updated_at_ns = ?
WHERE name = ?`,
			view.Query, string(fieldsJSON), view.Scope, view.Sort, view.OutputNames,
			string(requiredJSON), view.WhenMissing, next, now, view.Name)
		if err != nil {
			return err
		}
		view.Revision = next
		view.UpdatedAtNS = now
		return nil
	})
	return view, err
}

// GetSavedViewByName reads the current revision of a named view.
func (s *Store) GetSavedViewByName(ctx context.Context, name string) (SavedView, error) {
	var view SavedView
	var fieldsJSON, requiredJSON string
	err := s.db.QueryRowContext(ctx, `
SELECT view_id, name, query, fields_json, scope, sort, output_names,
       required_json, when_missing, revision, created_at_ns, updated_at_ns
FROM saved_views WHERE name = ?`, name).Scan(
		&view.ViewID, &view.Name, &view.Query, &fieldsJSON, &view.Scope, &view.Sort,
		&view.OutputNames, &requiredJSON, &view.WhenMissing, &view.Revision,
		&view.CreatedAtNS, &view.UpdatedAtNS)
	if errors.Is(err, sql.ErrNoRows) {
		return view, ErrNotFound
	}
	if err != nil {
		return view, fmt.Errorf("read saved view: %w", err)
	}
	if err := json.Unmarshal([]byte(fieldsJSON), &view.Fields); err != nil {
		return view, fmt.Errorf("decode view fields: %w", err)
	}
	if err := json.Unmarshal([]byte(requiredJSON), &view.Required); err != nil {
		return view, fmt.Errorf("decode view required capabilities: %w", err)
	}
	return view, nil
}

// GetSavedViewByID reads the current revision of a view by stable ID.
func (s *Store) GetSavedViewByID(ctx context.Context, viewID string) (SavedView, error) {
	var view SavedView
	var fieldsJSON, requiredJSON string
	err := s.db.QueryRowContext(ctx, `
SELECT view_id, name, query, fields_json, scope, sort, output_names,
       required_json, when_missing, revision, created_at_ns, updated_at_ns
FROM saved_views WHERE view_id = ?`, viewID).Scan(
		&view.ViewID, &view.Name, &view.Query, &fieldsJSON, &view.Scope, &view.Sort,
		&view.OutputNames, &requiredJSON, &view.WhenMissing, &view.Revision,
		&view.CreatedAtNS, &view.UpdatedAtNS)
	if errors.Is(err, sql.ErrNoRows) {
		return view, ErrNotFound
	}
	if err != nil {
		return view, fmt.Errorf("read saved view: %w", err)
	}
	if err := json.Unmarshal([]byte(fieldsJSON), &view.Fields); err != nil {
		return view, fmt.Errorf("decode view fields: %w", err)
	}
	if err := json.Unmarshal([]byte(requiredJSON), &view.Required); err != nil {
		return view, fmt.Errorf("decode view required capabilities: %w", err)
	}
	return view, nil
}

// ListSavedViews returns every current view revision in name order.
func (s *Store) ListSavedViews(ctx context.Context) ([]SavedView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT view_id, name, query, fields_json, scope, sort, output_names,
       required_json, when_missing, revision, created_at_ns, updated_at_ns
FROM saved_views ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list saved views: %w", err)
	}
	defer rows.Close()
	var views []SavedView
	for rows.Next() {
		var view SavedView
		var fieldsJSON, requiredJSON string
		if err := rows.Scan(
			&view.ViewID, &view.Name, &view.Query, &fieldsJSON, &view.Scope, &view.Sort,
			&view.OutputNames, &requiredJSON, &view.WhenMissing, &view.Revision,
			&view.CreatedAtNS, &view.UpdatedAtNS); err != nil {
			return nil, fmt.Errorf("scan saved view: %w", err)
		}
		if err := json.Unmarshal([]byte(fieldsJSON), &view.Fields); err != nil {
			return nil, fmt.Errorf("decode view fields: %w", err)
		}
		if err := json.Unmarshal([]byte(requiredJSON), &view.Required); err != nil {
			return nil, fmt.Errorf("decode view required capabilities: %w", err)
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return views, nil
}

// InsertExportManifest stores a frozen manifest. A duplicate digest is
// idempotent; a manifest with the same ID but a different digest is rejected.
func (s *Store) InsertExportManifest(ctx context.Context, manifest ExportManifest) (ExportManifest, error) {
	if strings.TrimSpace(manifest.ManifestID) == "" {
		return manifest, errors.New("manifest id is required")
	}
	if strings.TrimSpace(manifest.ManifestDigest) == "" {
		return manifest, errors.New("manifest digest is required")
	}
	itemsJSON, err := json.Marshal(manifest.Items)
	if err != nil {
		return manifest, fmt.Errorf("encode manifest items: %w", err)
	}
	manifest.CreatedAtNS = s.now().UTC().UnixNano()
	err = s.Update(ctx, func(tx *Tx) error {
		var existingDigest string
		err := tx.tx.QueryRowContext(ctx,
			`SELECT manifest_digest FROM export_manifests WHERE manifest_id = ?`, manifest.ManifestID).Scan(&existingDigest)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.tx.ExecContext(ctx, `
INSERT INTO export_manifests(
    manifest_id, manifest_digest, view_id, representation, target,
    subject_count, created_at_ns, items_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				manifest.ManifestID, manifest.ManifestDigest, manifest.ViewID,
				manifest.Representation, manifest.Target, manifest.SubjectCount,
				manifest.CreatedAtNS, string(itemsJSON))
			return err
		}
		if err != nil {
			return err
		}
		if existingDigest != manifest.ManifestDigest {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return manifest, err
	}
	return manifest, nil
}

// GetExportManifestByID reads one frozen manifest.
func (s *Store) GetExportManifestByID(ctx context.Context, manifestID string) (ExportManifest, error) {
	var manifest ExportManifest
	var itemsJSON string
	err := s.db.QueryRowContext(ctx, `
SELECT manifest_id, manifest_digest, view_id, representation, target,
       subject_count, created_at_ns, items_json
FROM export_manifests WHERE manifest_id = ?`, manifestID).Scan(
		&manifest.ManifestID, &manifest.ManifestDigest, &manifest.ViewID,
		&manifest.Representation, &manifest.Target, &manifest.SubjectCount,
		&manifest.CreatedAtNS, &itemsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return manifest, ErrNotFound
	}
	if err != nil {
		return manifest, fmt.Errorf("read export manifest: %w", err)
	}
	if err := json.Unmarshal([]byte(itemsJSON), &manifest.Items); err != nil {
		return manifest, fmt.Errorf("decode manifest items: %w", err)
	}
	return manifest, nil
}

// GetExportManifestByDigest reads one frozen manifest by its canonical digest.
func (s *Store) GetExportManifestByDigest(ctx context.Context, digest string) (ExportManifest, error) {
	var manifest ExportManifest
	var itemsJSON string
	err := s.db.QueryRowContext(ctx, `
SELECT manifest_id, manifest_digest, view_id, representation, target,
       subject_count, created_at_ns, items_json
FROM export_manifests WHERE manifest_digest = ?`, digest).Scan(
		&manifest.ManifestID, &manifest.ManifestDigest, &manifest.ViewID,
		&manifest.Representation, &manifest.Target, &manifest.SubjectCount,
		&manifest.CreatedAtNS, &itemsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return manifest, ErrNotFound
	}
	if err != nil {
		return manifest, fmt.Errorf("read export manifest: %w", err)
	}
	if err := json.Unmarshal([]byte(itemsJSON), &manifest.Items); err != nil {
		return manifest, fmt.Errorf("decode manifest items: %w", err)
	}
	return manifest, nil
}

// ListExportManifests returns every frozen manifest, newest first.
func (s *Store) ListExportManifests(ctx context.Context) ([]ExportManifest, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT manifest_id, manifest_digest, view_id, representation, target,
       subject_count, created_at_ns, items_json
FROM export_manifests ORDER BY created_at_ns DESC`)
	if err != nil {
		return nil, fmt.Errorf("list export manifests: %w", err)
	}
	defer rows.Close()
	var manifests []ExportManifest
	for rows.Next() {
		var manifest ExportManifest
		var itemsJSON string
		if err := rows.Scan(
			&manifest.ManifestID, &manifest.ManifestDigest, &manifest.ViewID,
			&manifest.Representation, &manifest.Target, &manifest.SubjectCount,
			&manifest.CreatedAtNS, &itemsJSON); err != nil {
			return nil, fmt.Errorf("scan export manifest: %w", err)
		}
		if err := json.Unmarshal([]byte(itemsJSON), &manifest.Items); err != nil {
			return nil, fmt.Errorf("decode manifest items: %w", err)
		}
		manifests = append(manifests, manifest)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return manifests, nil
}
