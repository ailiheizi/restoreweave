package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type annotationListInput struct {
	WorkspaceID string `json:"workspace_id"`
	SubjectRef  string `json:"subject_ref,omitempty"`
}

type annotationUpsertInput struct {
	WorkspaceID      string `json:"workspace_id"`
	SubjectRef       string `json:"subject_ref"`
	Kind             string `json:"kind"`
	Body             string `json:"body"`
	AnnotationID     string `json:"annotation_id,omitempty"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type annotationDeleteInput struct {
	WorkspaceID      string `json:"workspace_id"`
	AnnotationID     string `json:"annotation_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type annotationExportInput struct {
	WorkspaceID string `json:"workspace_id"`
	SubjectRef  string `json:"subject_ref,omitempty"`
}

type searchQueryInput struct {
	Query        json.RawMessage `json:"query"`
	WorkspaceID  string          `json:"workspace_id,omitempty"`
	GenerationID string          `json:"index_generation_ref,omitempty"`
	Dimension    string          `json:"dimension,omitempty"`
	Axes         []string        `json:"construct_axes,omitempty"`
	Fuse         []string        `json:"fuse,omitempty"`
	Filters      search.Filters  `json:"filters,omitempty"`
}

func (d *Dispatcher) handleAnnotationList(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input annotationListInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if input.SubjectRef != "" {
		if err := requireStableID("subject_ref", input.SubjectRef); err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	records, err := d.store.ListAnnotations(ctx, input.WorkspaceID, input.SubjectRef, false)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, command.AnnotationListData{Annotations: projectAnnotations(records)})
}

func (d *Dispatcher) handleAnnotationUpsert(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input annotationUpsertInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("subject_ref", input.SubjectRef); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.Body) == "" {
		return invalidInputResult(env, started, errString("body is required"))
	}
	kind := sqlite.AnnotationKind(strings.ToUpper(strings.TrimSpace(input.Kind)))
	var record sqlite.Annotation
	var err error
	switch kind {
	case sqlite.AnnotationTag:
		record, err = d.upsertTag(ctx, input)
	case sqlite.AnnotationNote:
		record, err = d.upsertNote(ctx, input)
	case sqlite.AnnotationProgress:
		record, err = d.upsertProgress(ctx, input)
	default:
		return invalidInputResult(env, started, errString("kind must be TAG, NOTE, or PROGRESS"))
	}
	if err != nil {
		return annotationWriteResult(env, started, err)
	}
	if kind != sqlite.AnnotationProgress {
		d.rebuildSearch(ctx, input.WorkspaceID)
	}
	return succeeded(env, started, command.AnnotationUpsertData{Annotation: projectAnnotation(record)})
}

func (d *Dispatcher) handleAnnotationDelete(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input annotationDeleteInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("annotation_id", input.AnnotationID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if input.ExpectedRevision < 1 {
		return invalidInputResult(env, started, errString("expected_revision is required"))
	}
	existing, err := d.store.GetAnnotation(ctx, input.WorkspaceID, input.AnnotationID)
	if err != nil {
		return annotationWriteResult(env, started, err)
	}
	if existing.Tombstoned {
		return notFoundResult(env, started, "annotation not found")
	}
	if err := d.store.ReviseAnnotation(ctx, input.WorkspaceID, input.AnnotationID, input.ExpectedRevision, existing.Body, true, time.Time{}); err != nil {
		return annotationWriteResult(env, started, err)
	}
	updated, err := d.store.GetAnnotation(ctx, input.WorkspaceID, input.AnnotationID)
	if err != nil {
		return annotationWriteResult(env, started, err)
	}
	d.rebuildSearch(ctx, input.WorkspaceID)
	return succeeded(env, started, command.AnnotationUpsertData{Annotation: projectAnnotation(updated)})
}

func (d *Dispatcher) handleAnnotationExport(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input annotationExportInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if input.SubjectRef != "" {
		if err := requireStableID("subject_ref", input.SubjectRef); err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	records, err := d.store.ListAnnotations(ctx, input.WorkspaceID, input.SubjectRef, true)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, command.AnnotationExportData{
		Schema:      command.AnnotationBundleSchema,
		Annotations: projectAnnotations(records),
	})
}

func (d *Dispatcher) handleAnnotationImport(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var bundle command.AnnotationExportData
	if err := decodeInput(env.Input, &bundle); err != nil {
		return invalidInputResult(env, started, err)
	}
	if bundle.Schema != "" && bundle.Schema != command.AnnotationBundleSchema {
		return invalidInputResult(env, started, errString("unsupported annotation bundle schema"))
	}
	policy, err := normalizeAnnotationConflict(bundle.Conflict)
	if err != nil {
		return invalidInputResult(env, started, err)
	}
	imported := make([]command.AnnotationData, 0, len(bundle.Annotations))
	workspaces := map[string]struct{}{}
	for _, item := range bundle.Annotations {
		if err := requireStableID("annotation_id", item.ID); err != nil {
			return invalidInputResult(env, started, err)
		}
		if err := requireStableID("workspace_id", item.WorkspaceID); err != nil {
			return invalidInputResult(env, started, err)
		}
		if err := requireStableID("subject_ref", item.SubjectRef); err != nil {
			return invalidInputResult(env, started, err)
		}
		existing, err := d.store.GetAnnotation(ctx, item.WorkspaceID, item.ID)
		if err == nil {
			same := existing.Revision == item.Revision && existing.Body == item.Body && existing.Tombstoned == item.Tombstoned
			if same {
				imported = append(imported, projectAnnotation(existing))
				continue
			}
			switch policy {
			case command.AnnotationConflictKeepLocal:
				imported = append(imported, projectAnnotation(existing))
				continue
			case command.AnnotationConflictKeepImported:
				if existing.Tombstoned {
					return conflictResult(env, started, "annotation "+item.ID+" is tombstoned locally; keep-imported will not rewrite history")
				}
				if err := d.store.ReviseAnnotation(ctx, item.WorkspaceID, item.ID, existing.Revision, item.Body, item.Tombstoned, time.Time{}); err != nil {
					return annotationWriteResult(env, started, err)
				}
				updated, getErr := d.store.GetAnnotation(ctx, item.WorkspaceID, item.ID)
				if getErr != nil {
					return catalogErrorResult(env, started, getErr)
				}
				imported = append(imported, projectAnnotation(updated))
				workspaces[item.WorkspaceID] = struct{}{}
				continue
			default:
				return conflictResult(env, started, "annotation "+item.ID+" already exists with a different revision")
			}
		}
		if !containsNotFound(err) {
			return catalogErrorResult(env, started, err)
		}
		revision := item.Revision
		if revision < 1 {
			revision = 1
		}
		record := &sqlite.Annotation{
			ID:                  item.ID,
			WorkspaceID:         item.WorkspaceID,
			SubjectRef:          item.SubjectRef,
			Kind:                sqlite.AnnotationKind(item.Kind),
			Body:                item.Body,
			Revision:            revision,
			PredecessorRevision: item.PredecessorRevision,
			Tombstoned:          item.Tombstoned,
		}
		if err := d.store.CreateAnnotation(ctx, record); err != nil {
			return annotationWriteResult(env, started, err)
		}
		created, err := d.store.GetAnnotation(ctx, item.WorkspaceID, item.ID)
		if err != nil {
			return catalogErrorResult(env, started, err)
		}
		imported = append(imported, projectAnnotation(created))
		workspaces[item.WorkspaceID] = struct{}{}
	}
	for workspaceID := range workspaces {
		d.rebuildSearch(ctx, workspaceID)
	}
	return succeeded(env, started, command.AnnotationExportData{
		Schema:      command.AnnotationBundleSchema,
		Annotations: imported,
		Conflict:    policy,
	})
}

func normalizeAnnotationConflict(policy string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", command.AnnotationConflictFail:
		return command.AnnotationConflictFail, nil
	case command.AnnotationConflictKeepLocal, command.AnnotationConflictKeepImported:
		return strings.ToLower(strings.TrimSpace(policy)), nil
	default:
		return "", errString("conflict must be fail, keep-local, or keep-imported")
	}
}

func (d *Dispatcher) handleSearchQuery(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.search == nil {
		return unimplementedResult(env, started)
	}
	var input searchQueryInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	queryText, dimensionID, requestedAxes, fuseIDs, filters := parseSearchRequest(input)
	if queryText == "" && !filters.Has() {
		return invalidInputResult(env, started, errString("query or filters are required"))
	}
	filters, err := search.NormalizeFilters(filters)
	if err != nil {
		return invalidInputResult(env, started, err)
	}
	if input.WorkspaceID != "" {
		if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	if len(fuseIDs) > 0 {
		if strings.TrimSpace(dimensionID) != "" {
			return invalidInputResult(env, started, errString("fuse and dimension cannot be set together"))
		}
		if input.GenerationID != "" {
			return invalidInputResult(env, started, errString("fuse cannot pin one index_generation_ref"))
		}
		return d.handleFusedSearch(ctx, env, started, input.WorkspaceID, queryText, requestedAxes, fuseIDs, filters)
	}
	dimension, ok := search.LookupDimension(dimensionID, search.IndexerReadiness(d.search))
	if !ok {
		return invalidInputResult(env, started, errString("dimension is not a declared index dimension"))
	}
	if dimension.ID != search.DimensionLexical && len(requestedAxes) > 0 {
		return invalidInputResult(env, started, errString("construct_axes are lexical only"))
	}
	var axes []string
	if dimension.ID == search.DimensionLexical {
		var err error
		axes, err = search.NormalizeConstructAxes(requestedAxes)
		if err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	if input.GenerationID != "" {
		if err := requireStableID("index_generation_ref", input.GenerationID); err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	empty := command.SearchQueryData{
		Dimension:      dimension.ID,
		Provider:       dimension.Provider,
		ScoreSemantics: dimension.ScoreSemantics,
		ConstructAxes:  axes,
		Hits:           []command.SearchHitData{},
	}
	if dimension.State != command.CapabilityAvailable {
		empty.ConstructAxes = nil
		return degradedResult(env, started, empty,
			dimension.ID+" is unavailable in this build; lexical search, namespace, annotations, and restore are unaffected")
	}
	generation, hits, err := d.search.Query(ctx, search.QueryRequest{
		WorkspaceID:  input.WorkspaceID,
		GenerationID: input.GenerationID,
		Dimension:    dimension.ID,
		Text:         queryText,
		Axes:         axes,
		Filters:      filters,
	})
	if err != nil {
		if errors.Is(err, search.ErrInvalidQuery) {
			return invalidInputResult(env, started, err)
		}
		if errors.Is(err, search.ErrUnavailable) {
			empty.GenerationID = generation.ID
			return degradedResult(env, started, empty, "search index is unavailable; namespace, annotations, and restore are unaffected")
		}
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, command.SearchQueryData{
		GenerationID:   generation.ID,
		Dimension:      dimension.ID,
		Provider:       dimension.Provider,
		ScoreSemantics: dimension.ScoreSemantics,
		ConstructAxes:  axes,
		Hits:           d.authorizeHits(ctx, input.WorkspaceID, generation.WorkspaceID, hits, nil),
	})
}

func (d *Dispatcher) handleFusedSearch(ctx context.Context, env command.Envelope, started time.Time, workspaceID, queryText string, requestedAxes, fuseIDs []string, filters search.Filters) command.Result {
	dims, err := search.NormalizeFuse(fuseIDs)
	if err != nil {
		return invalidInputResult(env, started, err)
	}
	var axes []string
	for _, id := range dims {
		if id == search.DimensionLexical {
			axes, err = search.NormalizeConstructAxes(requestedAxes)
			if err != nil {
				return invalidInputResult(env, started, err)
			}
			break
		}
	}
	if axes == nil && len(requestedAxes) > 0 {
		return invalidInputResult(env, started, errString("construct_axes are lexical only"))
	}
	fused, err := d.search.Fuse(ctx, search.QueryRequest{
		WorkspaceID: workspaceID,
		Text:        queryText,
		Axes:        axes,
		Fuse:        dims,
		Filters:     filters,
	})
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	components := make([]command.SearchComponentData, 0, len(fused.Components))
	for _, component := range fused.Components {
		components = append(components, command.SearchComponentData{
			Dimension:      component.Dimension,
			Provider:       component.Provider,
			GenerationID:   component.GenerationID,
			ScoreSemantics: component.ScoreSemantics,
			Status:         component.Status,
			Hits:           component.Hits,
		})
	}
	hits := make([]search.Hit, 0, len(fused.Hits))
	dimBySubject := map[string][]string{}
	for _, hit := range fused.Hits {
		hits = append(hits, hit.Hit)
		dimBySubject[hit.SubjectID] = hit.Dimensions
	}
	data := command.SearchQueryData{
		Provider:        search.ProviderBrokerFuse,
		ScoreSemantics:  search.ScoreComponentUnion,
		FusedDimensions: dims,
		ConstructAxes:   axes,
		Components:      components,
		Hits:            d.authorizeHits(ctx, workspaceID, "", hits, dimBySubject),
	}
	if !search.FuseSucceeded(fused) {
		return degradedResult(env, started, data, "all fused dimensions were unavailable; namespace, annotations, and restore are unaffected")
	}
	return succeeded(env, started, data)
}

func (d *Dispatcher) authorizeHits(ctx context.Context, workspaceID, fallbackWorkspace string, hits []search.Hit, dimensions map[string][]string) []command.SearchHitData {
	authorized := make([]command.SearchHitData, 0, len(hits))
	if workspaceID == "" {
		workspaceID = fallbackWorkspace
	}
	for _, hit := range hits {
		entry, err := d.store.GetNamespaceEntry(ctx, workspaceID, hit.SubjectID)
		if err != nil {
			continue
		}
		authorized = append(authorized, command.SearchHitData{
			SubjectRef:    entry.ID,
			Path:          hit.Path,
			Name:          entry.DisplayName,
			EntryType:     string(entry.EntryType),
			ContentID:     entry.ContentID,
			ConstructAxes: hit.ConstructAxes,
			Dimensions:    dimensions[hit.SubjectID],
		})
	}
	return authorized
}

func (d *Dispatcher) upsertTag(ctx context.Context, input annotationUpsertInput) (sqlite.Annotation, error) {
	existing, err := d.store.FindLiveTag(ctx, input.WorkspaceID, input.SubjectRef, input.Body)
	if err == nil {
		return existing, nil
	}
	if !containsNotFound(err) {
		return sqlite.Annotation{}, err
	}
	if input.ExpectedRevision != 0 {
		return sqlite.Annotation{}, sqlite.ErrConflict
	}
	id, err := sqlite.NewStableID(sqlite.IDPrefixAnnotation)
	if err != nil {
		return sqlite.Annotation{}, err
	}
	record := &sqlite.Annotation{
		ID:          id,
		WorkspaceID: input.WorkspaceID,
		SubjectRef:  input.SubjectRef,
		Kind:        sqlite.AnnotationTag,
		Body:        input.Body,
		Revision:    1,
	}
	if err := d.store.CreateAnnotation(ctx, record); err != nil {
		return sqlite.Annotation{}, err
	}
	return d.store.GetAnnotation(ctx, input.WorkspaceID, id)
}

func (d *Dispatcher) upsertNote(ctx context.Context, input annotationUpsertInput) (sqlite.Annotation, error) {
	if input.AnnotationID == "" {
		if input.ExpectedRevision != 0 {
			return sqlite.Annotation{}, sqlite.ErrConflict
		}
		id, err := sqlite.NewStableID(sqlite.IDPrefixAnnotation)
		if err != nil {
			return sqlite.Annotation{}, err
		}
		record := &sqlite.Annotation{
			ID:          id,
			WorkspaceID: input.WorkspaceID,
			SubjectRef:  input.SubjectRef,
			Kind:        sqlite.AnnotationNote,
			Body:        input.Body,
			Revision:    1,
		}
		if err := d.store.CreateAnnotation(ctx, record); err != nil {
			return sqlite.Annotation{}, err
		}
		return d.store.GetAnnotation(ctx, input.WorkspaceID, id)
	}
	if err := requireStableID("annotation_id", input.AnnotationID); err != nil {
		return sqlite.Annotation{}, err
	}
	existing, err := d.store.GetAnnotation(ctx, input.WorkspaceID, input.AnnotationID)
	if err != nil {
		return sqlite.Annotation{}, err
	}
	if existing.Kind != sqlite.AnnotationNote || existing.Tombstoned {
		return sqlite.Annotation{}, sqlite.ErrNotFound
	}
	if err := d.store.ReviseAnnotation(ctx, input.WorkspaceID, input.AnnotationID, input.ExpectedRevision, input.Body, false, time.Time{}); err != nil {
		return sqlite.Annotation{}, err
	}
	return d.store.GetAnnotation(ctx, input.WorkspaceID, input.AnnotationID)
}

func (d *Dispatcher) upsertProgress(ctx context.Context, input annotationUpsertInput) (sqlite.Annotation, error) {
	existing, err := d.store.FindLiveProgress(ctx, input.WorkspaceID, input.SubjectRef)
	if err == nil {
		expected := input.ExpectedRevision
		if expected == 0 {
			expected = existing.Revision
		}
		if err := d.store.ReviseAnnotation(ctx, input.WorkspaceID, existing.ID, expected, input.Body, false, time.Time{}); err != nil {
			return sqlite.Annotation{}, err
		}
		return d.store.GetAnnotation(ctx, input.WorkspaceID, existing.ID)
	}
	if !containsNotFound(err) {
		return sqlite.Annotation{}, err
	}
	if input.ExpectedRevision != 0 {
		return sqlite.Annotation{}, sqlite.ErrConflict
	}
	id := input.AnnotationID
	if id == "" {
		id, err = sqlite.NewStableID(sqlite.IDPrefixAnnotation)
		if err != nil {
			return sqlite.Annotation{}, err
		}
	} else if err := requireStableID("annotation_id", id); err != nil {
		return sqlite.Annotation{}, err
	}
	record := &sqlite.Annotation{
		ID:          id,
		WorkspaceID: input.WorkspaceID,
		SubjectRef:  input.SubjectRef,
		Kind:        sqlite.AnnotationProgress,
		Body:        input.Body,
		Revision:    1,
	}
	if err := d.store.CreateAnnotation(ctx, record); err != nil {
		return sqlite.Annotation{}, err
	}
	return d.store.GetAnnotation(ctx, input.WorkspaceID, id)
}

func (d *Dispatcher) rebuildSearch(ctx context.Context, workspaceID string) {
	if d.search == nil {
		return
	}
	_, _ = d.search.RebuildLatest(ctx, workspaceID)
}

func annotationWriteResult(env command.Envelope, started time.Time, err error) command.Result {
	if containsNotFound(err) {
		return notFoundResult(env, started, "annotation not found")
	}
	if errors.Is(err, sqlite.ErrConflict) {
		return conflictResult(env, started, "annotation revision conflict")
	}
	return catalogErrorResult(env, started, err)
}

func projectAnnotations(records []sqlite.Annotation) []command.AnnotationData {
	projected := make([]command.AnnotationData, 0, len(records))
	for _, record := range records {
		projected = append(projected, projectAnnotation(record))
	}
	return projected
}

func projectAnnotation(record sqlite.Annotation) command.AnnotationData {
	return command.AnnotationData{
		ID:                  record.ID,
		WorkspaceID:         record.WorkspaceID,
		SubjectRef:          record.SubjectRef,
		Kind:                string(record.Kind),
		Body:                record.Body,
		BodyDigest:          record.BodyDigest,
		Revision:            record.Revision,
		PredecessorRevision: record.PredecessorRevision,
		Tombstoned:          record.Tombstoned,
		CreatedAt:           record.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:           record.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func parseQueryText(raw json.RawMessage) string {
	text, _, _, _, _ := parseSearchQueryObject(raw)
	return text
}

func parseSearchRequest(input searchQueryInput) (text, dimension string, axes, fuse []string, filters search.Filters) {
	text, nestedDimension, nestedAxes, nestedFuse, nestedFilters := parseSearchQueryObject(input.Query)
	dimension = strings.TrimSpace(input.Dimension)
	if dimension == "" {
		dimension = nestedDimension
	}
	axes = append([]string(nil), input.Axes...)
	if len(axes) == 0 {
		axes = nestedAxes
	}
	fuse = append([]string(nil), input.Fuse...)
	if len(fuse) == 0 {
		fuse = nestedFuse
	}
	filters = input.Filters
	if !filters.Has() {
		filters = nestedFilters
	}
	return text, dimension, axes, fuse, filters
}

func parseSearchQueryObject(raw json.RawMessage) (text, dimension string, axes, fuse []string, filters search.Filters) {
	if len(raw) == 0 {
		return "", "", nil, nil, search.Filters{}
	}
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text), "", nil, nil, search.Filters{}
	}
	var nested struct {
		Text        string         `json:"text"`
		Fingerprint string         `json:"fingerprint"`
		Relation    string         `json:"relation"`
		Value       string         `json:"value"`
		Dimension   string         `json:"dimension"`
		Axes        []string       `json:"construct_axes"`
		Fuse        []string       `json:"fuse"`
		Filters     search.Filters `json:"filters"`
	}
	if err := json.Unmarshal(raw, &nested); err == nil {
		text := strings.TrimSpace(nested.Text)
		if text == "" {
			text = strings.TrimSpace(nested.Fingerprint)
		}
		if text == "" && strings.TrimSpace(nested.Relation) != "" {
			text = strings.TrimSpace(nested.Relation) + ":" + strings.TrimSpace(nested.Value)
		}
		return text, strings.TrimSpace(nested.Dimension), nested.Axes, nested.Fuse, nested.Filters
	}
	return "", "", nil, nil, search.Filters{}
}
