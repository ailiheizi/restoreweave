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
			if existing.Revision != item.Revision || existing.Body != item.Body || existing.Tombstoned != item.Tombstoned {
				return conflictResult(env, started, "annotation "+item.ID+" already exists with a different revision")
			}
			imported = append(imported, projectAnnotation(existing))
			continue
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
	})
}

func (d *Dispatcher) handleSearchQuery(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.search == nil {
		return unimplementedResult(env, started)
	}
	var input searchQueryInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	queryText := parseQueryText(input.Query)
	if queryText == "" {
		return invalidInputResult(env, started, errString("query is required"))
	}
	if input.WorkspaceID != "" {
		if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	if input.GenerationID != "" {
		if err := requireStableID("index_generation_ref", input.GenerationID); err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	generation, hits, err := d.search.Query(ctx, input.WorkspaceID, input.GenerationID, queryText)
	if err != nil {
		if errors.Is(err, search.ErrUnavailable) {
			return degradedResult(env, started, command.SearchQueryData{
				GenerationID: generation.ID,
				Hits:         []command.SearchHitData{},
			}, "search index is unavailable; namespace, annotations, and restore are unaffected")
		}
		return catalogErrorResult(env, started, err)
	}
	authorized := make([]command.SearchHitData, 0, len(hits))
	workspaceID := input.WorkspaceID
	if workspaceID == "" {
		workspaceID = generation.WorkspaceID
	}
	for _, hit := range hits {
		entry, err := d.store.GetNamespaceEntry(ctx, workspaceID, hit.SubjectID)
		if err != nil {
			continue
		}
		authorized = append(authorized, command.SearchHitData{
			SubjectRef: entry.ID,
			Path:       hit.Path,
			Name:       entry.DisplayName,
			EntryType:  string(entry.EntryType),
			ContentID:  entry.ContentID,
		})
	}
	return succeeded(env, started, command.SearchQueryData{
		GenerationID: generation.ID,
		Hits:         authorized,
	})
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
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var nested struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &nested); err == nil {
		return strings.TrimSpace(nested.Text)
	}
	return ""
}
