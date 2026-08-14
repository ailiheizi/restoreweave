package controlplane

import (
	"context"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type representationListInput struct {
	WorkspaceID   string `json:"workspace_id"`
	SubjectRef    string `json:"subject_ref,omitempty"`
	EntryID       string `json:"entry_id,omitempty"`
	FileVersionID string `json:"file_version_id,omitempty"`
}

func (d *Dispatcher) handleRepresentationList(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input representationListInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	subjectRef := firstNonEmpty(input.SubjectRef, input.EntryID)
	fileVersionID := strings.TrimSpace(input.FileVersionID)
	if subjectRef == "" && fileVersionID == "" {
		return invalidInputResult(env, started, errString("subject_ref, entry_id, or file_version_id is required"))
	}
	if subjectRef != "" {
		if err := requireStableID("subject_ref", subjectRef); err != nil {
			return invalidInputResult(env, started, err)
		}
	}
	if fileVersionID != "" {
		if err := requireStableID("file_version_id", fileVersionID); err != nil {
			return invalidInputResult(env, started, err)
		}
	}

	var contentID string
	var authoritativeID string
	if subjectRef != "" {
		entry, err := d.store.GetNamespaceEntry(ctx, input.WorkspaceID, subjectRef)
		if err != nil {
			return namespaceLookupResult(env, started, err)
		}
		if entry.EntryType != sqlite.EntryFile {
			return succeeded(env, started, command.RepresentationListData{
				WorkspaceID: input.WorkspaceID,
				SubjectRef:  subjectRef,
			})
		}
		if fileVersionID != "" && entry.FileVersionID != "" && entry.FileVersionID != fileVersionID {
			return invalidInputResult(env, started, errString("file_version_id does not match the subject"))
		}
		if fileVersionID == "" {
			fileVersionID = entry.FileVersionID
		}
		contentID = entry.ContentID
	}
	if fileVersionID != "" {
		version, err := d.store.GetFileVersion(ctx, input.WorkspaceID, fileVersionID)
		if err != nil {
			if containsNotFound(err) {
				return notFoundResult(env, started, "file version not found")
			}
			return catalogErrorResult(env, started, err)
		}
		contentID = version.ContentID
		authoritativeID = version.AuthoritativeRepresentationID
	}
	if contentID == "" && fileVersionID == "" {
		return succeeded(env, started, command.RepresentationListData{
			WorkspaceID: input.WorkspaceID,
			SubjectRef:  subjectRef,
		})
	}

	var records []sqlite.Representation
	var err error
	if fileVersionID != "" {
		records, err = d.representationsForFileVersion(ctx, input.WorkspaceID, fileVersionID, authoritativeID)
	} else {
		records, err = d.store.ListRepresentationsByContentID(ctx, input.WorkspaceID, contentID)
	}
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	projected := make([]command.RepresentationData, 0, len(records))
	for _, record := range records {
		placement, verified := d.representationPlacement(ctx, record.ContentID)
		projected = append(projected, projectRepresentation(record, record.ID == authoritativeID, placement, verified))
	}
	return succeeded(env, started, command.RepresentationListData{
		WorkspaceID:     input.WorkspaceID,
		SubjectRef:      subjectRef,
		FileVersionID:   fileVersionID,
		ContentID:       contentID,
		Representations: projected,
	})
}

func (d *Dispatcher) representationsForFileVersion(
	ctx context.Context,
	workspaceID, fileVersionID, authoritativeID string,
) ([]sqlite.Representation, error) {
	seen := map[string]struct{}{}
	var records []sqlite.Representation
	add := func(id string) error {
		if id == "" {
			return nil
		}
		if _, ok := seen[id]; ok {
			return nil
		}
		record, err := d.store.GetRepresentation(ctx, workspaceID, id)
		if err != nil {
			if containsNotFound(err) {
				return nil
			}
			return err
		}
		seen[id] = struct{}{}
		records = append(records, record)
		return nil
	}
	if err := add(authoritativeID); err != nil {
		return nil, err
	}
	extents, err := d.store.ListContentExtents(ctx, workspaceID, fileVersionID)
	if err != nil {
		return nil, err
	}
	for _, extent := range extents {
		if err := add(extent.RepresentationID); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (d *Dispatcher) representationPlacement(ctx context.Context, contentID string) (string, *bool) {
	if d.exact == nil || d.exact.Repo == nil || strings.TrimSpace(contentID) == "" {
		return command.RepresentationPlacementUnknown, nil
	}
	err := d.exact.Repo.Verify(ctx, contentID)
	ok := err == nil
	if ok {
		return command.RepresentationPlacementPresent, &ok
	}
	return command.RepresentationPlacementMissing, &ok
}

func projectRepresentation(record sqlite.Representation, authoritative bool, placement string, verified *bool) command.RepresentationData {
	class := command.RepresentationClassRecorded
	fidelity := command.RepresentationFidelityRecorded
	if record.CodecProfileRef == "identity/sha256-v1" {
		class = command.RepresentationClassExact
		fidelity = command.RepresentationFidelityExact
	}
	return command.RepresentationData{
		ID:              record.ID,
		ContentID:       record.ContentID,
		Class:           class,
		Fidelity:        fidelity,
		CodecProfileRef: record.CodecProfileRef,
		AccessMode:      string(record.AccessMode),
		OwnershipMode:   string(record.OwnershipMode),
		DecodedLength:   record.DecodedLength,
		Authoritative:   authoritative,
		Placement:       placement,
		Verified:        verified,
		RecordDigest:    record.RecordDigest,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
