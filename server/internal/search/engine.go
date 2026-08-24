// Package search implements the bundled lexical IndexProvider/QueryProvider:
// one physically separate disposable SQLite FTS5 database per index generation.
package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

var (
	ErrUnavailable  = errors.New("search index is unavailable")
	ErrNotFound     = errors.New("index generation not found")
	ErrInvalidQuery = errors.New("search query is invalid")
)

type Document struct {
	SubjectID       string
	Path            string
	Name            string
	Suffix          string
	EntryType       string
	ContentID       string
	Metadata        string
	Duplicates      string
	DuplicateGroup  string
	Protection      string
	Locators        string
	Tags            string
	Notes           string
	Descriptions    string
	Extracted       string
	Detection       string
	Processing      string
	Representations string
	Language        string
	LogicalSize     *int64
	MtimeMillis     *int64
	Segments        string
}

// SegmentRef is provenance for one durable text span inside a hit.
type SegmentRef struct {
	DescriptionDocumentID string
	SourceType            string
	SourceID              string
	SegmentID             string
	Ordinal               int64
	MatchedText           string
	Kind                  string
	Producer              string
	Accepted              bool
	Language              string
}

type Hit struct {
	SubjectID     string
	Path          string
	Name          string
	EntryType     string
	ContentID     string
	ConstructAxes []string
	Segments      []SegmentRef
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
    entry_type,
    content_id,
    metadata,
    duplicates,
    protection,
    locators,
    tags,
    notes,
    descriptions,
    extracted,
    detection,
    processing,
    representations,
    language,
    size_facet UNINDEXED,
    mtime_facet UNINDEXED,
    duplicate_group UNINDEXED,
    segments UNINDEXED,
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
INSERT INTO documents(
    subject_id, path, name, suffix, entry_type, content_id, metadata,
    duplicates, protection, locators, tags, notes, descriptions, extracted,
    detection, processing, representations, language, size_facet, mtime_facet,
    duplicate_group, segments
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return "", err
	}
	defer stmt.Close()
	for _, doc := range docs {
		if _, err := stmt.ExecContext(ctx, doc.SubjectID, doc.Path, doc.Name, doc.Suffix,
			doc.EntryType, doc.ContentID, doc.Metadata, doc.Duplicates,
			doc.Protection, doc.Locators, doc.Tags, doc.Notes,
			doc.Descriptions, doc.Extracted, doc.Detection, doc.Processing,
			doc.Representations, doc.Language, nullableInt64Value(doc.LogicalSize),
			nullableInt64Value(doc.MtimeMillis), doc.DuplicateGroup, doc.Segments); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return path, nil
}

func (engine *Engine) Query(ctx context.Context, dbPath, text string, axes []string) ([]Hit, error) {
	return engine.QueryFiltered(ctx, dbPath, text, axes, Filters{})
}

// QueryFiltered is the structured form of Query. Typed filters become precise
// post-filter predicates; free-text behavior is unchanged from Query.
func (engine *Engine) QueryFiltered(ctx context.Context, dbPath, text string, axes []string, filters Filters) ([]Hit, error) {
	normalizedFilters, err := NormalizeFilters(filters)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}
	filters = normalizedFilters
	if strings.TrimSpace(text) == "" && !filters.Has() {
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
	tokens := queryTokens(text)
	// FTS5 MATCH requires at least one term. With no free text the structured
	// filters still run, so fall back to a plain full-table scan; typed
	// post-filter predicates do the real constraint work in both cases.
	where := ""
	args := []any{}
	if len(tokens) > 0 {
		where = "WHERE documents MATCH ?"
		args = append(args, ftsQuery(text, axes))
	}
	query := `
SELECT subject_id, path, name, suffix, entry_type, content_id, metadata,
       duplicates, protection, locators, tags, notes, descriptions, extracted,
       detection, processing, representations, language,
       size_facet, mtime_facet, duplicate_group, segments
FROM documents ` + where
	if len(tokens) > 0 {
		query += ` ORDER BY rank, rowid`
	} else {
		// A deterministic order makes candidate paging stable while filters
		// are applied after the row is decoded.
		query += ` ORDER BY rowid`
	}
	const (
		candidatePageSize = 1000
		resultLimit       = 1000
	)
	var hits []Hit
	for offset := 0; len(hits) < resultLimit; offset += candidatePageSize {
		pageArgs := append(append([]any(nil), args...), candidatePageSize, offset)
		rows, queryErr := db.QueryContext(ctx, query+` LIMIT ? OFFSET ?`, pageArgs...)
		if queryErr != nil {
			return nil, fmt.Errorf("query fts5: %w", queryErr)
		}
		pageRows := 0
		for rows.Next() {
			pageRows++
			var hit Hit
			var suffix, metadata, duplicates, protection, locators, tags, notes, descriptions, extracted string
			var detection, processing, representations, language string
			var sizeFacet, mtimeFacet sql.NullInt64
			var duplicateGroup, segments string
			if err := rows.Scan(
				&hit.SubjectID, &hit.Path, &hit.Name, &suffix, &hit.EntryType,
				&hit.ContentID, &metadata, &duplicates, &protection, &locators,
				&tags, &notes, &descriptions, &extracted, &detection, &processing,
				&representations, &language, &sizeFacet, &mtimeFacet, &duplicateGroup,
				&segments,
			); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if !matchesFilters(hit, filters, sizeFacet, mtimeFacet, duplicateGroup, suffix, protection, language) {
				continue
			}
			hit.ConstructAxes = matchedConstructAxes(hit, suffix, metadata, duplicates,
				protection, locators, tags, notes, descriptions, extracted,
				detection, processing, representations, language, duplicateGroup, tokens)
			hit.Segments = matchedDescriptionSegments(segments, tokens)
			hits = append(hits, hit)
			if len(hits) >= resultLimit {
				break
			}
		}
		rowsErr := rows.Err()
		closeErr := rows.Close()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if pageRows < candidatePageSize || len(hits) >= resultLimit {
			break
		}
	}
	return hits, nil
}

// filterCandidates applies lexical typed facets to an already bounded set of
// provider hits. The provider's path/type fields are display data only; filter
// authority comes from the generation-aligned lexical row.
func (engine *Engine) filterCandidates(ctx context.Context, dbPath string, candidates []Hit, filters Filters) ([]Hit, error) {
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
	if len(candidates) == 0 {
		// Opening the generation is part of the filter dependency check. An
		// empty provider result must not hide a missing lexical authority.
		if err := db.PingContext(ctx); err != nil {
			return nil, fmt.Errorf("open candidate filter authority: %w", err)
		}
		return nil, nil
	}
	stmt, err := db.PrepareContext(ctx, `
SELECT entry_type, content_id, duplicate_group, suffix, protection, language,
       size_facet, mtime_facet
FROM documents
WHERE subject_id = ?
LIMIT 1`)
	if err != nil {
		return nil, fmt.Errorf("prepare candidate filter: %w", err)
	}
	defer stmt.Close()
	filtered := make([]Hit, 0, len(candidates))
	for _, candidate := range candidates {
		var indexed Hit
		var duplicateGroup, suffix, protection, language string
		var sizeFacet, mtimeFacet sql.NullInt64
		err := stmt.QueryRowContext(ctx, candidate.SubjectID).Scan(
			&indexed.EntryType, &indexed.ContentID, &duplicateGroup, &suffix,
			&protection, &language, &sizeFacet, &mtimeFacet,
		)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("filter provider candidate %q: %w", candidate.SubjectID, err)
		}
		if matchesFilters(indexed, filters, sizeFacet, mtimeFacet, duplicateGroup, suffix, protection, language) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered, nil
}

// matchesFilters applies typed structured constraints as post-filter
// predicates over the FTS row. Numeric facets are stored in UNINDEXED columns
// so MATCH can never touch them; constraints never invent a hit.
func matchesFilters(hit Hit, filters Filters, sizeFacet, mtimeFacet sql.NullInt64, duplicateGroup, suffix, protection, language string) bool {
	if filters.EntryType != "" && strings.ToUpper(filters.EntryType) != strings.ToUpper(hit.EntryType) {
		return false
	}
	if filters.ContentID != "" && hit.ContentID != filters.ContentID {
		return false
	}
	if filters.DuplicateGroup != "" && duplicateGroup != filters.DuplicateGroup {
		return false
	}
	if filters.ProtectionMode != "" && !containsWord(filters.ProtectionMode, protection) {
		return false
	}
	// Language is a structured exact facet. Compare canonicalized display
	// values without treating missing or whitespace-only values as a match.
	if want := strings.TrimSpace(filters.Language); want != "" &&
		!strings.EqualFold(want, strings.TrimSpace(language)) {
		return false
	}
	if filters.Suffix != "" && strings.TrimPrefix(strings.ToLower(suffix), ".") != strings.TrimPrefix(strings.ToLower(filters.Suffix), ".") {
		return false
	}
	if filters.SizeMin != nil {
		if !sizeFacet.Valid || sizeFacet.Int64 < *filters.SizeMin {
			return false
		}
	}
	if filters.SizeMax != nil {
		if !sizeFacet.Valid || sizeFacet.Int64 > *filters.SizeMax {
			return false
		}
	}
	if filters.MtimeAfter != nil {
		if !mtimeFacet.Valid || mtimeFacet.Int64 <= *filters.MtimeAfter {
			return false
		}
	}
	if filters.MtimeBefore != nil {
		if !mtimeFacet.Valid || mtimeFacet.Int64 >= *filters.MtimeBefore {
			return false
		}
	}
	return true
}

func (engine *Engine) RemoveFile(dbPath string) error {
	if err := os.Remove(dbPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	return nil
}

func ftsQuery(text string, axes []string) string {
	tokens := queryTokens(text)
	if len(tokens) == 0 {
		return `""`
	}
	if !restrictToAxes(axes) {
		parts := make([]string, len(tokens))
		for i, token := range tokens {
			parts[i] = `"` + token + `"*`
		}
		return strings.Join(parts, " AND ")
	}
	groups := make([]string, 0, len(tokens))
	for _, token := range tokens {
		alts := make([]string, 0, len(axes))
		for _, axis := range axes {
			alts = append(alts, ftsColumnForAxis(axis)+`:"`+token+`"*`)
		}
		if len(alts) == 1 {
			groups = append(groups, alts[0])
			continue
		}
		groups = append(groups, "("+strings.Join(alts, " OR ")+")")
	}
	return strings.Join(groups, " AND ")
}

func ftsColumnForAxis(axis string) string {
	switch axis {
	case AxisType:
		return "entry_type"
	case AxisChecksum:
		return "content_id"
	default:
		return axis
	}
}

func queryTokens(text string) []string {
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
			tokens = append(tokens, token)
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
	return tokens
}

func restrictToAxes(axes []string) bool {
	return len(axes) > 0 && len(axes) < len(LexicalConstructAxes)
}

func matchedConstructAxes(hit Hit, suffix, metadata, duplicates, protection, locators, tags, notes, descriptions, extracted string,
	detection, processing, representations, language, duplicateGroup string, tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	fields := []struct {
		axis string
		text string
	}{
		{AxisPath, hit.Path},
		{AxisName, hit.Name},
		{AxisSuffix, suffix},
		{AxisType, hit.EntryType},
		{AxisChecksum, hit.ContentID},
		{AxisMetadata, metadata},
		{AxisDuplicates, duplicates},
		{AxisDuplicateGroup, duplicateGroup},
		{AxisProtection, protection},
		{AxisLocators, locators},
		{AxisTags, tags},
		{AxisNotes, notes},
		{AxisDescriptions, descriptions},
		{AxisExtracted, extracted},
		{AxisDetection, detection},
		{AxisProcessing, processing},
		{AxisRepresentations, representations},
		{AxisLanguage, language},
	}
	var matched []string
	for _, field := range fields {
		lower := strings.ToLower(field.text)
		for _, token := range tokens {
			if token != "" && strings.Contains(lower, strings.ToLower(token)) {
				matched = append(matched, field.axis)
				break
			}
		}
	}
	return matched
}

// matchedDescriptionSegments returns provenance for description matches. The
// segments UNINDEXED column carries the durable segment text digests; a hit
// reports exactly the segments whose text matched a query token. No token
// match means no segment is claimed, even when the whole-body axis matched.
func matchedDescriptionSegments(segmentsJSON string, tokens []string) []SegmentRef {
	if len(tokens) == 0 || strings.TrimSpace(segmentsJSON) == "" {
		return nil
	}
	var segments []SegmentRef
	if err := json.Unmarshal([]byte(segmentsJSON), &segments); err != nil {
		return nil
	}
	var refs []SegmentRef
	for _, segment := range segments {
		lower := strings.ToLower(segment.MatchedText)
		for _, token := range tokens {
			if token != "" && strings.Contains(lower, strings.ToLower(token)) {
				refs = append(refs, segment)
				break
			}
		}
	}
	return refs
}

func containsWord(word, text string) bool {
	word = strings.ToLower(word)
	text = strings.ToLower(text)
	for _, field := range strings.Fields(text) {
		if field == word || strings.HasPrefix(field, word+"_") {
			return true
		}
	}
	return false
}

func nullableInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
