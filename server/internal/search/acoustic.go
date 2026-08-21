package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

type AcousticDocument struct {
	SubjectID   string
	Fingerprint string
	Algorithm   string
	Path        string
	Name        string
	EntryType   string
	ContentID   string
}

func (engine *Engine) BuildAcoustic(ctx context.Context, generationID string, docs []AcousticDocument) (string, error) {
	if strings.TrimSpace(generationID) == "" {
		return "", errors.New("index generation id is required")
	}
	if err := os.MkdirAll(engine.Dir, 0o700); err != nil {
		return "", err
	}
	path := engine.PathFor(generationID)
	_ = os.Remove(path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return "", err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
CREATE TABLE fingerprints (
    subject_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    algorithm TEXT NOT NULL,
    path TEXT,
    name TEXT,
    entry_type TEXT,
    content_id TEXT
);
CREATE INDEX fingerprints_fp_idx ON fingerprints(fingerprint);`); err != nil {
		return "", fmt.Errorf("create acoustic table: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO fingerprints(subject_id, fingerprint, algorithm, path, name, entry_type, content_id)
VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return "", err
	}
	defer stmt.Close()
	for _, doc := range docs {
		if _, err := stmt.ExecContext(ctx, doc.SubjectID, normalizeFingerprint(doc.Fingerprint),
			doc.Algorithm, doc.Path, doc.Name, doc.EntryType, doc.ContentID); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return path, nil
}

func (engine *Engine) QueryAcoustic(ctx context.Context, dbPath, fingerprint string) ([]Hit, error) {
	fingerprint = normalizeFingerprint(fingerprint)
	if fingerprint == "" {
		return nil, errors.New("acoustic query fingerprint is required")
	}
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrUnavailable
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
SELECT subject_id, path, name, entry_type, content_id
FROM fingerprints
WHERE fingerprint = ?
LIMIT 100`, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("query acoustic: %w", err)
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var hit Hit
		if err := rows.Scan(&hit.SubjectID, &hit.Path, &hit.Name, &hit.EntryType, &hit.ContentID); err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func normalizeFingerprint(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func parseFingerprintArtifact(body string) (fingerprint, algorithm string, ok bool) {
	var record struct {
		Algorithm          string `json:"algorithm"`
		Fingerprint        string `json:"fingerprint"`
		NotContentIdentity bool   `json:"not_content_identity"`
	}
	if err := json.Unmarshal([]byte(body), &record); err != nil {
		return "", "", false
	}
	fingerprint = strings.TrimSpace(record.Fingerprint)
	if fingerprint == "" || !record.NotContentIdentity {
		return "", "", false
	}
	algorithm = strings.TrimSpace(record.Algorithm)
	if algorithm == "" {
		algorithm = "fixture-v1"
	}
	return fingerprint, algorithm, true
}
