package search

import (
	"errors"
	"fmt"
	"strings"
)

// Named discovery dimensions. One QueryProvider invocation queries exactly
// one dimension and one IndexGenerationRef. Later acoustic/semantic providers
// attach to the same SubjectRef and never become ContentIdentity.
const (
	CapabilityKindDimension = "index-dimension"

	// SemanticIndexUnavailableReason is the stable operator-facing reason used
	// while the configured real embedding provider has not been wired. Fixture
	// embeddings are deliberately not a substitute for this capability.
	SemanticIndexUnavailableReason = "SEMANTIC_INDEX_UNAVAILABLE"

	DimensionLexical    = "lexical-metadata-fts"
	DimensionAcoustic   = "acoustic-fingerprint"
	DimensionSemantic   = "semantic-embedding"
	DimensionMultimodal = "multimodal-clip"
	DimensionGraph      = "graph-relation"

	ProviderLexicalFTS5   = "query.lexical.fts5.v1"
	ProviderAcousticFix   = "query.acoustic.fixture.v1"
	ProviderSemanticFix   = "query.semantic.fixture.v1"
	ProviderMultimodalFix = "query.multimodal.fixture.v1"
	ProviderBrokerFuse    = "query.broker.fuse.v1"
	ProviderGraphCatalog  = "query.graph.catalog.v1"
	ScoreLexicalRank      = "fts5-rank"
	ScoreGraphExact       = "relation-exact"
	ScoreAcousticExact    = "fixture-exact"
	ScoreSemanticExact    = "fixture-exact"
	ScoreMultimodalExact  = "fixture-exact"
	ScoreComponentUnion   = "component-union"
	CapabilityKindBroker  = "query-broker"

	AxisPath            = "path"
	AxisName            = "name"
	AxisSuffix          = "suffix"
	AxisType            = "type"
	AxisChecksum        = "checksum"
	AxisMetadata        = "metadata"
	AxisDuplicates      = "duplicates"
	AxisDuplicateGroup  = "duplicate_group"
	AxisProtection      = "protection"
	AxisLocators        = "locators"
	AxisTags            = "tags"
	AxisNotes           = "notes"
	AxisDescriptions    = "descriptions"
	AxisExtracted       = "extracted"
	AxisDetection       = "detection"
	AxisProcessing      = "processing"
	AxisRepresentations = "representations"
	AxisLanguage        = "language"

	// AxisSize and AxisMtime are numeric facets, not FTS5 text columns. They
	// are constrained through typed Filters and reported by coverage, never
	// by a MATCH clause.
	AxisSize  = "size"
	AxisMtime = "mtime"
)

// LexicalConstructAxes are the rebuildable text fields of the bundled FTS5
// generation. Size and mtime are numeric facets, not text axes: they are
// searchable through typed structured filters, never through MATCH.
// These are not six products and not recovery evidence.
var LexicalConstructAxes = []string{
	AxisPath, AxisName, AxisSuffix, AxisType, AxisChecksum, AxisMetadata,
	AxisDuplicates, AxisDuplicateGroup, AxisProtection, AxisLocators,
	AxisTags, AxisNotes, AxisDescriptions, AxisExtracted,
	AxisDetection, AxisProcessing, AxisRepresentations, AxisLanguage,
}

// Dimension is the capability projection of one named retrieval space.
type Dimension struct {
	ID             string
	Provider       string
	State          string
	Version        string
	ScoreSemantics string
	ConstructAxes  []string
	Notes          string
}

// QueryRequest is one search.query dispatch against a named generation.
type QueryRequest struct {
	WorkspaceID  string
	GenerationID string
	Dimension    string
	Text         string
	Axes         []string
	Filters      Filters
	Fuse         []string
}

// Filters are typed structured constraints applied on top of the free-text
// MATCH expression. Empty fields are absent constraints. Times are
// milliseconds since the Unix epoch so the command surface can pass them as
// plain JSON integers without a time layout. JSON tags keep the command
// surface's snake_case keys stable across package boundaries.
type Filters struct {
	EntryType      string `json:"entry_type"`
	ContentID      string `json:"content_id"`
	DuplicateGroup string `json:"duplicate_group"`
	ProtectionMode string `json:"protection_mode"`
	Language       string `json:"language"`
	Suffix         string `json:"suffix"`
	SizeMin        *int64 `json:"size_min"`
	SizeMax        *int64 `json:"size_max"`
	MtimeAfter     *int64 `json:"mtime_after"`
	MtimeBefore    *int64 `json:"mtime_before"`
}

// Has reports whether at least one structured constraint is set.
func (f Filters) Has() bool {
	if f.EntryType != "" || f.ContentID != "" || f.DuplicateGroup != "" ||
		f.ProtectionMode != "" || f.Language != "" || f.Suffix != "" {
		return true
	}
	return f.SizeMin != nil || f.SizeMax != nil || f.MtimeAfter != nil || f.MtimeBefore != nil
}

// NumericFilters reports which numeric facets are constrained. It is used by
// coverage reporting so absent numeric facets stay visible as absent.
func (f Filters) NumericFilters() []string {
	var out []string
	if f.SizeMin != nil || f.SizeMax != nil {
		out = append(out, AxisSize)
	}
	if f.MtimeAfter != nil || f.MtimeBefore != nil {
		out = append(out, AxisMtime)
	}
	return out
}

// FieldCoverage reports whether one named text facet is present in the feed
// with any non-empty content. It never invents coverage for fields the feed
// does not carry.
func (f FieldCoverage) Present(field string) bool {
	if field == "" {
		return false
	}
	return f[field]
}

// ProviderReadiness says which QueryProviders this process has wired.
// A ready acoustic provider can still degrade when no generation exists.
type ProviderReadiness struct {
	Lexical    bool
	Acoustic   bool
	Semantic   bool
	Multimodal bool
	Graph      bool
}

// DeclaredDimensions returns every discovery dimension the contract names.
func DeclaredDimensions(ready ProviderReadiness) []Dimension {
	lexicalState := "UNAVAILABLE"
	lexicalNotes := "bundled FTS5 IndexProvider/QueryProvider; unavailable until the exact lane wires a search indexer"
	if ready.Lexical {
		lexicalState = "AVAILABLE"
		lexicalNotes = "bundled disposable FTS5 generation over namespace, metadata, checksum, duplicate, protection, locator, annotation, description, and extracted text fields; not recovery authority"
	}
	acousticState := "UNAVAILABLE"
	acousticNotes := "fixture fingerprint QueryProvider; unavailable until the exact lane wires a search indexer"
	if ready.Acoustic {
		acousticState = "AVAILABLE"
		acousticNotes = "disposable fixture-fingerprint generation over admitted FINGERPRINT artifacts; not Chromaprint, not SHA-256, not recovery authority"
	}
	semanticState := "UNAVAILABLE"
	semanticNotes := "real embedding model and vector IndexProvider are not wired (" + SemanticIndexUnavailableReason + "); fixture embeddings require explicit opt-in"
	if ready.Semantic {
		semanticState = "AVAILABLE"
		semanticNotes = "explicitly enabled fixture-embedding generation over admitted ENRICH artifacts; not a model runtime, not SHA-256, not recovery authority"
	}
	multimodalState := "UNAVAILABLE"
	multimodalNotes := "real multimodal model and vector IndexProvider are not wired; fixture CLIP-class search requires explicit opt-in"
	if ready.Multimodal {
		multimodalState = "AVAILABLE"
		multimodalNotes = "explicitly enabled fixture joint-space generation over audio-tag ENRICH artifacts; not CLIP weights, not SHA-256, not recovery authority"
	}
	graphState := "UNAVAILABLE"
	graphNotes := "catalog relation QueryProvider; unavailable until the exact lane wires a search indexer"
	if ready.Graph {
		graphState = "AVAILABLE"
		graphNotes = "disposable relation projection over namespace, tags, and extracted artist/album/author labels; not a second catalog and not recovery authority"
	}
	return []Dimension{
		{
			ID:             DimensionLexical,
			Provider:       ProviderLexicalFTS5,
			State:          lexicalState,
			Version:        "1",
			ScoreSemantics: ScoreLexicalRank,
			ConstructAxes:  append([]string(nil), LexicalConstructAxes...),
			Notes:          lexicalNotes,
		},
		{
			ID:             DimensionAcoustic,
			Provider:       ProviderAcousticFix,
			State:          acousticState,
			Version:        "1",
			ScoreSemantics: ScoreAcousticExact,
			Notes:          acousticNotes,
		},
		{
			ID:             DimensionSemantic,
			Provider:       ProviderSemanticFix,
			State:          semanticState,
			Version:        "1",
			ScoreSemantics: ScoreSemanticExact,
			Notes:          semanticNotes,
		},
		{
			ID:             DimensionMultimodal,
			Provider:       ProviderMultimodalFix,
			State:          multimodalState,
			Version:        "1",
			ScoreSemantics: ScoreMultimodalExact,
			Notes:          multimodalNotes,
		},
		{
			ID:             DimensionGraph,
			Provider:       ProviderGraphCatalog,
			State:          graphState,
			Version:        "1",
			ScoreSemantics: ScoreGraphExact,
			Notes:          graphNotes,
		},
	}
}

// LookupDimension returns a declared dimension by id.
func LookupDimension(id string, ready ProviderReadiness) (Dimension, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = DimensionLexical
	}
	for _, dimension := range DeclaredDimensions(ready) {
		if dimension.ID == id {
			return dimension, true
		}
	}
	return Dimension{}, false
}

// FieldCoverage is a per-field presence map for one lexical generation.
// Absence is an honest reported fact: a field missing from the feed is never
// silently claimed. The map is nil when no generation exists.
type FieldCoverage map[string]bool

// CoverageStatement is the operator-facing lexical feed coverage report.
// Complete is true only when every documented baseline feed field is present
// and non-empty for at least one subject. It is never asserted from the index
// schema alone; it requires an actual built generation.
type CoverageStatement struct {
	Dimension string
	Available bool
	Complete  bool
	Fields    FieldCoverage
	Missing   []string
	Notes     string
}

// LexicalCoverage reports what one built generation actually feeds. When the
// generation is unavailable, Fields is nil and Complete is false so a caller
// cannot claim the complete baseline. The coverage map keys are Axis values.
func LexicalCoverage(available bool, perField map[string]bool) CoverageStatement {
	statement := CoverageStatement{
		Dimension: DimensionLexical,
		Available: available,
		Complete:  false,
		Notes:     "field-level presence in the lexical feed; absence is reported, never invented",
	}
	if !available {
		return statement
	}
	if perField == nil {
		return statement
	}
	fields := FieldCoverage{}
	complete := true
	for _, axis := range LexicalCoverageFields() {
		present := perField[axis]
		fields[axis] = present
		if !present {
			complete = false
			statement.Missing = append(statement.Missing, axis)
		}
	}
	statement.Fields = fields
	statement.Complete = complete
	return statement
}

// LexicalCoverageFields is the documented complete-baseline feed surface.
// It pairs the FTS5 text axes with the two numeric facets (size, mtime) that
// structured filters can constrain.
func LexicalCoverageFields() []string {
	fields := make([]string, 0, len(LexicalConstructAxes)+2)
	fields = append(fields, LexicalConstructAxes...)
	fields = append(fields, AxisSize, AxisMtime)
	return fields
}

func IndexerReadiness(idx *Indexer) ProviderReadiness {
	ready := idx != nil && idx.Store != nil && idx.Engine != nil
	fixtures := ready && idx.EnableFixtureDimensions
	return ProviderReadiness{Lexical: ready, Acoustic: fixtures, Semantic: fixtures, Multimodal: fixtures, Graph: ready}
}

// NormalizeFuse accepts two or more declared dimension IDs. Duplicates drop.
func NormalizeFuse(ids []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := LookupDimension(id, ProviderReadiness{}); !ok {
			return nil, fmt.Errorf("fuse dimension %q is not a declared index dimension", id)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("fuse requires at least two dimensions")
	}
	return out, nil
}

// NormalizeConstructAxes accepts an empty list as "all lexical axes" and
// rejects unknown names. Duplicates are dropped in declaration order.
func NormalizeConstructAxes(axes []string) ([]string, error) {
	if len(axes) == 0 {
		return append([]string(nil), LexicalConstructAxes...), nil
	}
	allowed := make(map[string]struct{}, len(LexicalConstructAxes))
	for _, axis := range LexicalConstructAxes {
		allowed[axis] = struct{}{}
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(axes))
	for _, axis := range axes {
		axis = strings.TrimSpace(strings.ToLower(axis))
		if axis == "" {
			continue
		}
		if _, ok := allowed[axis]; !ok {
			return nil, fmt.Errorf("construct axis %q is not a lexical field", axis)
		}
		if _, ok := seen[axis]; ok {
			continue
		}
		seen[axis] = struct{}{}
		normalized = append(normalized, axis)
	}
	if len(normalized) == 0 {
		return append([]string(nil), LexicalConstructAxes...), nil
	}
	return normalized, nil
}

// NormalizeFilters trims structured constraints and rejects invalid numeric
// ranges. It is the search-package contract; the control plane performs the
// same validation so a malformed request fails before any generation lookup.
func NormalizeFilters(f Filters) (Filters, error) {
	f.EntryType = strings.TrimSpace(strings.ToUpper(f.EntryType))
	f.ContentID = strings.TrimSpace(f.ContentID)
	f.DuplicateGroup = strings.TrimSpace(f.DuplicateGroup)
	f.ProtectionMode = strings.TrimSpace(strings.ToUpper(f.ProtectionMode))
	f.Language = strings.TrimSpace(f.Language)
	f.Suffix = strings.TrimSpace(strings.ToLower(f.Suffix))
	if f.SizeMin != nil && f.SizeMax != nil && *f.SizeMin > *f.SizeMax {
		return f, errors.New("size_min must not exceed size_max")
	}
	if f.MtimeAfter != nil && f.MtimeBefore != nil && *f.MtimeAfter > *f.MtimeBefore {
		return f, errors.New("mtime_after must not exceed mtime_before")
	}
	if f.SizeMin != nil && *f.SizeMin < 0 {
		return f, errors.New("size_min cannot be negative")
	}
	if f.SizeMax != nil && *f.SizeMax < 0 {
		return f, errors.New("size_max cannot be negative")
	}
	return f, nil
}
