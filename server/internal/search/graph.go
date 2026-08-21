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

const (
	RelContains    = "contains"
	RelSameContent = "same_content"
	RelTagged      = "tagged"
	RelArtist      = "artist"
	RelAlbum       = "album"
	RelAuthor      = "author"
)

// GraphEdge is one rebuildable relation from an existing catalog fact.
// Artist/album/author values are labels, not new catalog subjects.
type GraphEdge struct {
	SubjectID string
	Relation  string
	Value     string
	Path      string
	Name      string
	EntryType string
	ContentID string
}

func (engine *Engine) BuildGraph(ctx context.Context, generationID string, edges []GraphEdge) (string, error) {
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
CREATE TABLE relations (
    subject_id TEXT NOT NULL,
    relation TEXT NOT NULL,
    value TEXT NOT NULL,
    path TEXT,
    name TEXT,
    entry_type TEXT,
    content_id TEXT
);
CREATE INDEX relations_rel_val_idx ON relations(relation, value);
CREATE INDEX relations_subject_idx ON relations(subject_id, relation);`); err != nil {
		return "", fmt.Errorf("create graph table: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO relations(subject_id, relation, value, path, name, entry_type, content_id)
VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return "", err
	}
	defer stmt.Close()
	for _, edge := range edges {
		if _, err := stmt.ExecContext(ctx, edge.SubjectID, edge.Relation, normalizeGraphValue(edge.Value),
			edge.Path, edge.Name, edge.EntryType, edge.ContentID); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return path, nil
}

func (engine *Engine) QueryGraph(ctx context.Context, dbPath, text string) ([]Hit, error) {
	relation, value, err := parseGraphQuery(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
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
	if relation == RelSameContent && looksLikeSubjectRef(value) {
		var contentID string
		if err := db.QueryRowContext(ctx, `
SELECT content_id FROM relations WHERE subject_id = ? AND relation = ? LIMIT 1`,
			value, RelSameContent).Scan(&contentID); err == nil && contentID != "" {
			value = contentID
		}
	}
	rows, err := db.QueryContext(ctx, `
SELECT subject_id, path, name, entry_type, content_id
FROM relations
WHERE relation = ? AND value = ?
LIMIT 100`, relation, normalizeGraphValue(value))
	if err != nil {
		return nil, fmt.Errorf("query graph: %w", err)
	}
	defer rows.Close()
	seen := map[string]struct{}{}
	var hits []Hit
	for rows.Next() {
		var hit Hit
		if err := rows.Scan(&hit.SubjectID, &hit.Path, &hit.Name, &hit.EntryType, &hit.ContentID); err != nil {
			return nil, err
		}
		if _, ok := seen[hit.SubjectID]; ok {
			continue
		}
		seen[hit.SubjectID] = struct{}{}
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func parseGraphQuery(text string) (relation, value string, err error) {
	text = strings.TrimSpace(text)
	relation, value, ok := strings.Cut(text, ":")
	if !ok {
		return "", "", errors.New("graph query must be relation:value")
	}
	relation = strings.ToLower(strings.TrimSpace(relation))
	value = strings.TrimSpace(value)
	switch relation {
	case RelContains, RelSameContent, RelTagged, RelArtist, RelAlbum, RelAuthor:
	default:
		return "", "", fmt.Errorf("graph relation %q is not a catalog projection", relation)
	}
	if value == "" {
		return "", "", errors.New("graph query value is required")
	}
	return relation, value, nil
}

func normalizeGraphValue(value string) string {
	value = strings.TrimSpace(value)
	if looksLikeSubjectRef(value) || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func looksLikeSubjectRef(value string) bool {
	return strings.HasPrefix(value, "nse_") || strings.HasPrefix(value, "nsr_")
}

func parseAudioLabels(body string) (artist, album string) {
	var parsed struct {
		Artist string `json:"artist"`
		Album  string `json:"album"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return "", ""
	}
	return strings.TrimSpace(parsed.Artist), strings.TrimSpace(parsed.Album)
}

func parseAuthorLabel(body string) string {
	var parsed struct {
		Author string `json:"author"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Author)
}
