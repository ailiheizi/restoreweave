// Package search implements the bundled lexical IndexProvider/QueryProvider:
// one physically separate disposable SQLite FTS5 database per index generation.
package search

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

var (
	ErrUnavailable = errors.New("search index is unavailable")
	ErrNotFound    = errors.New("index generation not found")
)

type Document struct {
	SubjectID string
	Path      string
	Name      string
	Suffix    string
	EntryType string
	ContentID string
	Tags      string
	Notes     string
	Extracted string
}

type Hit struct {
	SubjectID string
	Path      string
	Name      string
	EntryType string
	ContentID string
}

type Engine struct {
	Dir string
}

func (engine *Engine) PathFor(generationID string) string {
	return filepath.Join(engine.Dir, generationID+".sqlite")
}

func (engine *Engine) Build(ctx context.Context, generationID string, docs []Document) (string, error) {
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
CREATE VIRTUAL TABLE documents USING fts5(
    subject_id UNINDEXED,
    path,
    name,
    suffix,
    entry_type UNINDEXED,
    content_id,
    tags,
    notes,
    extracted,
    tokenize = 'unicode61'
)`); err != nil {
		return "", fmt.Errorf("create fts5 table: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO documents(subject_id, path, name, suffix, entry_type, content_id, tags, notes, extracted)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return "", err
	}
	defer stmt.Close()
	for _, doc := range docs {
		if _, err := stmt.ExecContext(ctx, doc.SubjectID, doc.Path, doc.Name, doc.Suffix,
			doc.EntryType, doc.ContentID, doc.Tags, doc.Notes, doc.Extracted); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return path, nil
}

func (engine *Engine) Query(ctx context.Context, dbPath, text string) ([]Hit, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("search query text is required")
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
FROM documents
WHERE documents MATCH ?
ORDER BY rank
LIMIT 100`, ftsQuery(text))
	if err != nil {
		return nil, fmt.Errorf("query fts5: %w", err)
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

func (engine *Engine) RemoveFile(dbPath string) error {
	if err := os.Remove(dbPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	return nil
}

func ftsQuery(text string) string {
	text = strings.TrimSpace(text)
	var tokens []string
	var current strings.Builder
	flush := func() {
		token := strings.TrimSpace(current.String())
		current.Reset()
		if token == "" {
			return
		}
		token = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				return r
			}
			return -1
		}, token)
		if token != "" {
			tokens = append(tokens, token+"*")
		}
	}
	for _, r := range text {
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		current.WriteRune(r)
	}
	flush()
	if len(tokens) == 0 {
		return `""`
	}
	return strings.Join(tokens, " AND ")
}
