package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const (
	descriptionSegmentMaxBytes  = 1024
	descriptionMaxBodyBytes     = 16 << 20
	descriptionListDefaultLimit = 100
	descriptionListMaxLimit     = 1000
)

type descriptionListInput struct {
	WorkspaceID string `json:"workspace_id"`
	SubjectRef  string `json:"subject_ref,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type descriptionGetInput struct {
	WorkspaceID string `json:"workspace_id"`
	DocumentID  string `json:"description_document_id"`
}

type descriptionCreateInput struct {
	WorkspaceID     string          `json:"workspace_id"`
	SubjectRef      string          `json:"subject_ref"`
	Kind            string          `json:"kind"`
	Title           string          `json:"title,omitempty"`
	Language        string          `json:"language,omitempty"`
	Body            string          `json:"body"`
	SourceRef       string          `json:"source_ref,omitempty"`
	ProducerProfile string          `json:"producer_profile,omitempty"`
	Confidence      *float64        `json:"confidence,omitempty"`
	Coverage        *float64        `json:"coverage,omitempty"`
	Visibility      string          `json:"visibility,omitempty"`
	Accepted        bool            `json:"accepted,omitempty"`
	PredecessorID   string          `json:"predecessor_id,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

func (d *Dispatcher) handleDescriptionList(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input descriptionListInput
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
	limit := input.Limit
	if limit == 0 {
		limit = descriptionListDefaultLimit
	}
	if limit < 1 || limit > descriptionListMaxLimit {
		return invalidInputResult(env, started, fmt.Errorf("limit must be between 1 and %d", descriptionListMaxLimit))
	}
	documents, err := d.store.ListDescriptionSummaries(ctx, input.WorkspaceID, input.SubjectRef, limit+1)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	truncated := len(documents) > limit
	if len(documents) > limit {
		documents = documents[:limit]
	}
	projected := make([]command.DescriptionSummaryData, 0, len(documents))
	for _, document := range documents {
		projected = append(projected, projectDescriptionSummary(document))
	}
	return succeeded(env, started, command.DescriptionListData{Documents: projected, Truncated: truncated})
}

func (d *Dispatcher) handleDescriptionGet(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input descriptionGetInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("description_document_id", input.DocumentID); err != nil {
		return invalidInputResult(env, started, err)
	}
	document, err := d.store.GetDescriptionDocument(ctx, input.WorkspaceID, input.DocumentID)
	if err != nil {
		if containsNotFound(err) {
			return notFoundResult(env, started, "description document not found")
		}
		return catalogErrorResult(env, started, err)
	}
	segments, err := d.store.ListSemanticSegments(ctx, input.WorkspaceID, input.DocumentID)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, command.DescriptionGetData{Document: projectDescription(document, segments)})
}

func (d *Dispatcher) handleDescriptionCreate(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input descriptionCreateInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	if _, err := d.store.GetWorkspace(ctx, input.WorkspaceID); err != nil {
		if containsNotFound(err) {
			return notFoundResult(env, started, "workspace not found")
		}
		return catalogErrorResult(env, started, err)
	}
	if err := requireStableID("subject_ref", input.SubjectRef); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.Body) == "" {
		return invalidInputResult(env, started, errString("body is required"))
	}
	if len(input.Body) > descriptionMaxBodyBytes {
		return invalidInputResult(env, started, fmt.Errorf("body exceeds maximum size of %d bytes", descriptionMaxBodyBytes))
	}
	if !utf8.ValidString(input.Body) {
		return invalidInputResult(env, started, errString("body must be valid UTF-8"))
	}
	kind := sqlite.DescriptionKind(strings.ToUpper(strings.TrimSpace(input.Kind)))
	if !validDescriptionKind(kind) {
		return invalidInputResult(env, started, errString("kind must be USER, IMPORTED, EXTRACTED, AI_SUMMARY, or AI_ANALYSIS"))
	}
	if err := validateDescriptionRange("confidence", input.Confidence); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := validateDescriptionRange("coverage", input.Coverage); err != nil {
		return invalidInputResult(env, started, err)
	}

	revision := int64(1)
	if input.PredecessorID != "" {
		if err := requireStableID("predecessor_id", input.PredecessorID); err != nil {
			return invalidInputResult(env, started, err)
		}
		predecessor, err := d.store.GetDescriptionDocument(ctx, input.WorkspaceID, input.PredecessorID)
		if err != nil {
			return invalidInputResult(env, started, fmt.Errorf("predecessor_id is invalid: %w", err))
		}
		if predecessor.SubjectRef != input.SubjectRef {
			return invalidInputResult(env, started, errString("predecessor_id belongs to a different subject"))
		}
		if predecessor.Kind != kind {
			return invalidInputResult(env, started, errString("predecessor_id belongs to a different description kind"))
		}
		revision = predecessor.Revision + 1
	}

	documentID, err := sqlite.NewStableID(sqlite.IDPrefixDescription)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	document := &sqlite.DescriptionDocument{
		ID:              documentID,
		WorkspaceID:     input.WorkspaceID,
		SubjectRef:      input.SubjectRef,
		Kind:            kind,
		Title:           input.Title,
		Language:        input.Language,
		Body:            input.Body,
		SourceRef:       strings.TrimSpace(input.SourceRef),
		ProducerProfile: strings.TrimSpace(input.ProducerProfile),
		Confidence:      input.Confidence,
		Coverage:        input.Coverage,
		Visibility:      input.Visibility,
		Accepted:        input.Accepted,
		Revision:        revision,
		PredecessorID:   input.PredecessorID,
		Metadata:        input.Metadata,
	}
	if document.SourceRef == "" {
		document.SourceRef = "command:description.create"
	}
	if document.ProducerProfile == "" {
		document.ProducerProfile = "command"
	}
	chunks := splitDescriptionBody(document.Body)
	segments := make([]sqlite.SemanticSegment, 0, len(chunks))
	for ordinal, chunk := range chunks {
		segmentID, idErr := sqlite.NewStableID(sqlite.IDPrefixSemanticSegment)
		if idErr != nil {
			return catalogErrorResult(env, started, idErr)
		}
		span, marshalErr := json.Marshal(descriptionSourceSpan{StartByte: chunk.start, EndByte: chunk.end})
		if marshalErr != nil {
			return catalogErrorResult(env, started, marshalErr)
		}
		segments = append(segments, sqlite.SemanticSegment{
			ID:          segmentID,
			WorkspaceID: document.WorkspaceID,
			DocumentID:  document.ID,
			SubjectRef:  document.SubjectRef,
			Ordinal:     int64(ordinal),
			Text:        chunk.text,
			Language:    document.Language,
			Section:     "body",
			SourceSpan:  span,
		})
	}
	if err := d.store.Update(ctx, func(tx *sqlite.Tx) error {
		if err := tx.InsertDescriptionDocument(ctx, document); err != nil {
			return err
		}
		for i := range segments {
			if err := tx.InsertSemanticSegment(ctx, &segments[i]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return descriptionWriteResult(env, started, err)
	}
	d.rebuildSearch(ctx, input.WorkspaceID)
	return succeeded(env, started, command.DescriptionCreateData{Document: projectDescription(*document, segments)})
}

func projectDescriptionSummary(document sqlite.DescriptionDocument) command.DescriptionSummaryData {
	return command.DescriptionSummaryData{
		ID:              document.ID,
		WorkspaceID:     document.WorkspaceID,
		SubjectRef:      document.SubjectRef,
		Kind:            string(document.Kind),
		Title:           document.Title,
		Language:        document.Language,
		BodyDigest:      document.BodyDigest,
		SourceRef:       document.SourceRef,
		ProducerProfile: document.ProducerProfile,
		Confidence:      document.Confidence,
		Coverage:        document.Coverage,
		Visibility:      document.Visibility,
		Accepted:        document.Accepted,
		Revision:        document.Revision,
		PredecessorID:   document.PredecessorID,
		CreatedAt:       document.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       document.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func projectDescription(document sqlite.DescriptionDocument, segments []sqlite.SemanticSegment) command.DescriptionDocumentData {
	projectedSegments := make([]command.SemanticSegmentData, 0, len(segments))
	for _, segment := range segments {
		projectedSegments = append(projectedSegments, command.SemanticSegmentData{
			ID:          segment.ID,
			WorkspaceID: segment.WorkspaceID,
			DocumentID:  segment.DocumentID,
			SubjectRef:  segment.SubjectRef,
			Ordinal:     segment.Ordinal,
			Text:        segment.Text,
			TextDigest:  segment.TextDigest,
			Language:    segment.Language,
			Section:     segment.Section,
			SourceSpan:  segment.SourceSpan,
			Metadata:    segment.Metadata,
			CreatedAt:   segment.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return command.DescriptionDocumentData{
		ID:              document.ID,
		WorkspaceID:     document.WorkspaceID,
		SubjectRef:      document.SubjectRef,
		Kind:            string(document.Kind),
		Title:           document.Title,
		Language:        document.Language,
		Body:            document.Body,
		BodyDigest:      document.BodyDigest,
		SourceRef:       document.SourceRef,
		ProducerProfile: document.ProducerProfile,
		Confidence:      document.Confidence,
		Coverage:        document.Coverage,
		Visibility:      document.Visibility,
		Accepted:        document.Accepted,
		Revision:        document.Revision,
		PredecessorID:   document.PredecessorID,
		Metadata:        document.Metadata,
		CreatedAt:       document.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       document.UpdatedAt.UTC().Format(time.RFC3339),
		Segments:        projectedSegments,
	}
}

type descriptionSourceSpan struct {
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`
}

type descriptionChunk struct {
	start int
	end   int
	text  string
}

// splitDescriptionBody makes contiguous chunks at UTF-8 rune boundaries. A
// natural whitespace or sentence boundary is preferred within the byte cap;
// the exact source bytes are retained in each chunk so spans are verifiable.
func splitDescriptionBody(body string) []descriptionChunk {
	chunks := make([]descriptionChunk, 0, 1)
	for start := 0; start < len(body); {
		remaining := len(body) - start
		end := len(body)
		if remaining > descriptionSegmentMaxBytes {
			end = start + descriptionSegmentMaxBytes
			for end > start && !utf8.RuneStart(body[end]) {
				end--
			}
			preferred := end
			for cursor := end; cursor > start; {
				runeValue, size := utf8.DecodeLastRuneInString(body[start:cursor])
				if unicode.IsSpace(runeValue) || strings.ContainsRune(".!?。！？", runeValue) {
					preferred = cursor
					break
				}
				cursor -= size
			}
			if preferred > start {
				end = preferred
			}
		}
		if end <= start {
			_, size := utf8.DecodeRuneInString(body[start:])
			end = start + size
		}
		chunks = append(chunks, descriptionChunk{start: start, end: end, text: body[start:end]})
		start = end
	}
	// A long whitespace run can otherwise become a rejected, whitespace-only
	// segment. Attach such runs to an adjacent meaningful segment while
	// preserving contiguous source spans.
	meaningful := make([]descriptionChunk, 0, len(chunks))
	for i := range chunks {
		chunk := chunks[i]
		if strings.TrimSpace(chunk.text) != "" {
			meaningful = append(meaningful, chunk)
			continue
		}
		if len(meaningful) > 0 {
			previous := &meaningful[len(meaningful)-1]
			previous.end = chunk.end
			previous.text = body[previous.start:previous.end]
			continue
		}
		if i+1 < len(chunks) {
			chunks[i+1].start = chunk.start
			chunks[i+1].text = body[chunks[i+1].start:chunks[i+1].end]
		}
	}
	return meaningful
}

func validDescriptionKind(kind sqlite.DescriptionKind) bool {
	switch kind {
	case sqlite.DescriptionUser, sqlite.DescriptionImported, sqlite.DescriptionExtracted,
		sqlite.DescriptionAISummary, sqlite.DescriptionAIAnalysis:
		return true
	default:
		return false
	}
}

func validateDescriptionRange(name string, value *float64) error {
	if value != nil && (*value < 0 || *value > 1) {
		return fmt.Errorf("%s must be between 0 and 1", name)
	}
	return nil
}

func descriptionWriteResult(env command.Envelope, started time.Time, err error) command.Result {
	if containsNotFound(err) {
		return notFoundResult(env, started, "description predecessor or workspace not found")
	}
	if errors.Is(err, sqlite.ErrConflict) {
		return conflictResult(env, started, "description revision conflict")
	}
	if strings.Contains(err.Error(), "description_documents_predecessor_idx") ||
		strings.Contains(err.Error(), "description_documents.workspace_id, description_documents.predecessor_id") {
		return conflictResult(env, started, "description predecessor already has a successor")
	}
	return catalogErrorResult(env, started, err)
}
