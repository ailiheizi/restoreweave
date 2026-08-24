package search

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

type semanticCoverageProbe interface {
	Coverage(context.Context, ZvecGenerationSpec) ([]string, error)
}

type SemanticCoverageStatement struct {
	Dimension     string
	GenerationID  string
	ConfigDigest  string
	ProfileDigest string
	Available     bool
	Complete      bool
	Expected      int
	Indexed       int
	Missing       []string
	Notes         string
}

// SemanticCoverage reports only evidence available from the durable segment
// set and the backend's generation coverage probe. A backend that cannot
// enumerate indexed segment IDs is deliberately partial, never complete.
func (idx *Indexer) SemanticCoverage(ctx context.Context, workspaceID string) (SemanticCoverageStatement, error) {
	statement := SemanticCoverageStatement{Dimension: DimensionSemantic, Notes: "semantic coverage requires backend segment identity evidence"}
	if idx == nil || idx.Store == nil || idx.SemanticProvider == nil || idx.SemanticZvec == nil || idx.SemanticManifest == (EmbeddingGenerationManifest{}) {
		statement.Notes = "semantic provider or backend is unavailable"
		return statement, nil
	}
	if idx.semanticUnavailable.Load() {
		statement.Notes = "semantic provider is degraded"
		return statement, nil
	}
	if health, ok := idx.SemanticProvider.(interface{ SemanticReady() bool }); ok && !health.SemanticReady() {
		statement.Notes = "semantic provider is not ready"
		return statement, nil
	}
	if health, ok := idx.SemanticZvec.(ZvecGenerationReadiness); ok && !health.ZvecReady(idx.SemanticLibraryPath, idx.SemanticLibraryDigest, idx.SemanticManifest) {
		statement.Notes = "semantic backend is not ready"
		return statement, nil
	}
	generation, err := idx.Store.LatestIndexGeneration(ctx, workspaceID, DimensionSemantic)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return statement, nil
		}
		return statement, err
	}
	statement.GenerationID, statement.ConfigDigest, statement.ProfileDigest = generation.ID, generation.ConfigDigest, generation.ProviderProfileDigest
	if !generationBindingMatches(idx, generation, DimensionSemantic) {
		statement.Notes = "semantic generation binding mismatch"
		return statement, nil
	}
	docs, err := idx.Store.ListDescriptionDocuments(ctx, workspaceID, "")
	if err != nil {
		return statement, err
	}
	expected := map[string]struct{}{}
	for _, doc := range docs {
		segments, listErr := idx.Store.ListSemanticSegments(ctx, workspaceID, doc.ID)
		if listErr != nil {
			return statement, listErr
		}
		for _, segment := range segments {
			expected[segment.ID] = struct{}{}
		}
	}
	annotations, err := idx.Store.ListAnnotations(ctx, workspaceID, "", false)
	if err != nil {
		return statement, err
	}
	for _, annotation := range annotations {
		if annotation.Kind == sqlite.AnnotationNote {
			expected[annotation.ID] = struct{}{}
		}
	}
	statement.Expected = len(expected)
	probe, ok := idx.SemanticZvec.(semanticCoverageProbe)
	if !ok {
		statement.Notes = "semantic backend does not provide segment coverage evidence"
		return statement, nil
	}
	spec := ZvecGenerationSpec{Path: generation.DBPath, LibraryPath: idx.SemanticLibraryPath, LibraryDigest: idx.SemanticLibraryDigest, ProfileDigest: idx.SemanticManifest.CanonicalDigest(), Manifest: idx.SemanticManifest}
	indexed, err := probe.Coverage(ctx, spec)
	if err != nil {
		statement.Notes = "semantic coverage probe unavailable"
		return statement, nil
	}
	seen := map[string]struct{}{}
	unknown := false
	duplicate := false
	for _, id := range indexed {
		if _, ok := expected[id]; ok {
			if _, exists := seen[id]; exists {
				duplicate = true
			}
			seen[id] = struct{}{}
		} else {
			unknown = true
		}
	}
	statement.Indexed = len(seen)
	statement.Available = true
	for id := range expected {
		if _, ok := seen[id]; !ok {
			statement.Missing = append(statement.Missing, id)
		}
	}
	sort.Strings(statement.Missing)
	statement.Complete = len(statement.Missing) == 0 && statement.Indexed == statement.Expected && !unknown && !duplicate
	switch {
	case unknown:
		statement.Notes = "semantic generation contains unknown segment identities"
	case duplicate:
		statement.Notes = "semantic generation contains duplicate segment identities"
	case statement.Expected > 0 && statement.Indexed == 0:
		statement.Notes = "semantic generation is empty"
	case !statement.Complete:
		statement.Notes = "semantic generation has incomplete segment coverage"
	default:
		statement.Notes = "semantic generation covers every durable segment"
	}
	return statement, nil
}

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
