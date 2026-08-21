package search

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

// Coverage reports what the lexical generation actually feeds, honestly.
// Missing fields are reported as missing; a generation that does not exist
// yields an unavailable statement that is never complete. This is the
// per-generation view behind capability.list and search.query.
func (idx *Indexer) Coverage(ctx context.Context, workspaceID string) (CoverageStatement, error) {
	var statement CoverageStatement
	if idx == nil || idx.Store == nil || idx.Engine == nil {
		return LexicalCoverage(false, nil), nil
	}
	generation, err := idx.Store.LatestIndexGeneration(ctx, workspaceID, DimensionLexical)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return LexicalCoverage(false, nil), nil
		}
		return statement, err
	}
	if _, err := os.Stat(generation.DBPath); err != nil {
		return LexicalCoverage(false, nil), nil
	}
	perField, err := measureFieldCoverage(ctx, generation.DBPath)
	if err != nil {
		return statement, err
	}
	return LexicalCoverage(true, perField), nil
}

// measureFieldCoverage runs one aggregate query over the built documents
// table and returns which non-empty fields exist. Numeric facets count as
// present when any row carries a non-null value; every other field counts as
// present when any row has non-whitespace content. The query never invents a
// field the schema does not feed.
func measureFieldCoverage(ctx context.Context, dbPath string) (map[string]bool, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var suffix, entryType, contentID, metadata, duplicates, duplicateGroup int
	var protection, locators, tags, notes, descriptions, extracted int
	var detection, processing, representations, language int
	var path, name, sizePresent, mtimePresent int
	row := db.QueryRowContext(ctx, `
SELECT
  COUNT(NULLIF(TRIM(path), '')),
  COUNT(NULLIF(TRIM(name), '')),
  COUNT(NULLIF(TRIM(suffix), '')),
  COUNT(NULLIF(TRIM(entry_type), '')),
  COUNT(NULLIF(TRIM(content_id), '')),
  COUNT(NULLIF(TRIM(metadata), '')),
  COUNT(NULLIF(TRIM(duplicates), '')),
  COUNT(NULLIF(TRIM(duplicate_group), '')),
  COUNT(NULLIF(TRIM(protection), '')),
  COUNT(NULLIF(TRIM(locators), '')),
  COUNT(NULLIF(TRIM(tags), '')),
  COUNT(NULLIF(TRIM(notes), '')),
  COUNT(NULLIF(TRIM(descriptions), '')),
  COUNT(NULLIF(TRIM(extracted), '')),
  COUNT(NULLIF(TRIM(detection), '')),
  COUNT(NULLIF(TRIM(processing), '')),
  COUNT(NULLIF(TRIM(representations), '')),
  COUNT(NULLIF(TRIM(language), '')),
  COUNT(size_facet),
  COUNT(mtime_facet)
FROM documents`)
	if err := row.Scan(&path, &name, &suffix, &entryType, &contentID, &metadata,
		&duplicates, &duplicateGroup, &protection, &locators, &tags, &notes,
		&descriptions, &extracted, &detection, &processing, &representations,
		&language, &sizePresent, &mtimePresent); err != nil {
		return nil, err
	}
	fields := map[string]bool{
		AxisPath:            path > 0,
		AxisName:            name > 0,
		AxisSuffix:          suffix > 0,
		AxisType:            entryType > 0,
		AxisChecksum:        contentID > 0,
		AxisMetadata:        metadata > 0,
		AxisDuplicates:      duplicates > 0,
		AxisDuplicateGroup:  duplicateGroup > 0,
		AxisProtection:      protection > 0,
		AxisLocators:        locators > 0,
		AxisTags:            tags > 0,
		AxisNotes:           notes > 0,
		AxisDescriptions:    descriptions > 0,
		AxisExtracted:       extracted > 0,
		AxisDetection:       detection > 0,
		AxisProcessing:      processing > 0,
		AxisRepresentations: representations > 0,
		AxisLanguage:        language > 0,
		AxisSize:            sizePresent > 0,
		AxisMtime:           mtimePresent > 0,
	}
	// Subject count is the honest denominator: a real generation always has
	// at least the namespace entries, so path/name presence is measured, not
	// assumed. Re-check against an actual row count.
	var subjectCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&subjectCount); err != nil {
		return nil, err
	}
	if subjectCount == 0 {
		return map[string]bool{}, nil
	}
	return fields, nil
}

// NormalizeCoverageFields validates a coverage field name against the
// documented surface. Empty returns every lexical coverage field.
func NormalizeCoverageFields(fields []string) ([]string, error) {
	all := LexicalCoverageFields()
	if len(fields) == 0 {
		return append([]string(nil), all...), nil
	}
	allowed := make(map[string]struct{}, len(all))
	for _, field := range all {
		allowed[field] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(strings.ToLower(field))
		if field == "" {
			continue
		}
		if _, ok := allowed[field]; !ok {
			return nil, errors.New("coverage field " + field + " is not a lexical feed field")
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out, nil
}
