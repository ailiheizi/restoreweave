package search

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

type TokenDocument struct {
	SubjectID string
	Token     string
	Space     string
	Path      string
	Name      string
	EntryType string
	ContentID string
}

func (engine *Engine) BuildTokens(ctx context.Context, generationID string, docs []TokenDocument) (string, error) {
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
CREATE TABLE tokens (
    subject_id TEXT NOT NULL,
    token TEXT NOT NULL,
    space TEXT NOT NULL,
    path TEXT,
    name TEXT,
    entry_type TEXT,
    content_id TEXT
);
CREATE INDEX tokens_token_idx ON tokens(token);`); err != nil {
		return "", fmt.Errorf("create token table: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO tokens(subject_id, token, space, path, name, entry_type, content_id)
VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return "", err
	}
	defer stmt.Close()
	for _, doc := range docs {
		if _, err := stmt.ExecContext(ctx, doc.SubjectID, normalizeFingerprint(doc.Token),
			doc.Space, doc.Path, doc.Name, doc.EntryType, doc.ContentID); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return path, nil
}

func (engine *Engine) QueryTokens(ctx context.Context, dbPath, token string) ([]Hit, error) {
	token = normalizeFingerprint(token)
	if token == "" {
		return nil, errors.New("token query is required")
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
FROM tokens
WHERE token = ?
LIMIT 100`, token)
	if err != nil {
		return nil, fmt.Errorf("query tokens: %w", err)
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

func queryToken(dimension, text string) string {
	switch dimension {
	case DimensionSemantic:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "sem1:") {
			return strings.TrimSpace(text)
		}
		return fixtureToken("sem1", text)
	case DimensionMultimodal:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "clip1:") {
			return strings.TrimSpace(text)
		}
		return fixtureToken("clip1", text)
	default:
		return text
	}
}

func fixtureToken(prefix, text string) string {
	normalized := normalizeFeatureText(text)
	return sketchPrefixed(prefix, []byte(normalized))
}

func normalizeFeatureText(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func sketchPrefixed(prefix string, body []byte) string {
	const buckets = 16
	var acc [buckets]byte
	n := len(body)
	if n == 0 {
		return prefix + ":" + hex.EncodeToString(acc[:])
	}
	for i := 0; i < n; i++ {
		j := i % buckets
		acc[j] ^= body[i]
		acc[j] += byte(i) + byte(n>>uint(j%8))
		acc[(j+3)%buckets] ^= byte(n + i*31)
	}
	return prefix + ":" + hex.EncodeToString(acc[:])
}

func parseFeatureArtifact(body string) (token, space string, ok bool) {
	var record struct {
		Space              string `json:"space"`
		Token              string `json:"token"`
		NotContentIdentity bool   `json:"not_content_identity"`
	}
	if err := json.Unmarshal([]byte(body), &record); err != nil {
		return "", "", false
	}
	token = strings.TrimSpace(record.Token)
	if token == "" || !record.NotContentIdentity {
		return "", "", false
	}
	return token, strings.TrimSpace(record.Space), true
}
