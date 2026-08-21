package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// viewGetInput is the view.get input. ViewID is the stable view identifier;
// Name is the human view name. Exactly one must be supplied; name resolves to
// the current view revision.
type viewGetInput struct {
	ViewID string `json:"view_id,omitempty"`
	Name   string `json:"name,omitempty"`
}

// handleViewSave creates the first revision of a named view or writes a
// successor revision when the name already exists. UpdateSavedView never edits
// a historical revision; view.save is therefore always an insert-or-advance.
func (d *Dispatcher) handleViewSave(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input command.ViewSaveInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.Name) == "" {
		return invalidInputResult(env, started, errString("name is required"))
	}
	if strings.TrimSpace(input.Query) == "" {
		return invalidInputResult(env, started, errString("query is required"))
	}
	record := sqlite.SavedView{
		Name:        strings.TrimSpace(input.Name),
		Query:       strings.TrimSpace(input.Query),
		Fields:      normalizeStringList(input.Fields),
		Scope:       strings.TrimSpace(input.Scope),
		Sort:        strings.TrimSpace(input.Sort),
		OutputNames: strings.TrimSpace(input.OutputNames),
		Required:    normalizeStringList(input.Required),
		WhenMissing: strings.TrimSpace(input.WhenMissing),
		Revision:    1,
	}
	viewID, err := sqlite.NewStableID("view")
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	record.ViewID = viewID
	stored, err := d.store.InsertSavedView(ctx, record)
	if err == nil {
		return succeeded(env, started, projectView(stored))
	}
	if !errors.Is(err, sqlite.ErrConflict) {
		return catalogErrorResult(env, started, err)
	}
	existing, getErr := d.store.GetSavedViewByName(ctx, record.Name)
	if getErr != nil {
		return catalogErrorResult(env, started, getErr)
	}
	record.ViewID = existing.ViewID
	record.CreatedAtNS = existing.CreatedAtNS
	record.Revision = 0
	updated, err := d.store.UpdateSavedView(ctx, record)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, projectView(updated))
}

func (d *Dispatcher) handleViewGet(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input viewGetInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	view, err := d.resolveView(ctx, input.ViewID, input.Name)
	if err != nil {
		if containsNotFound(err) {
			return notFoundResult(env, started, "view not found")
		}
		if isStableIDError(err) {
			return invalidInputResult(env, started, err)
		}
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, projectView(view))
}

func (d *Dispatcher) handleViewList(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	views, err := d.store.ListSavedViews(ctx)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	projected := make([]command.ViewData, 0, len(views))
	for _, view := range views {
		projected = append(projected, projectView(view))
	}
	return succeeded(env, started, projected)
}

// resolveView reads a view by stable ID or human name. Stable IDs win when
// both are supplied; an empty reference is an invalid input.
func (d *Dispatcher) resolveView(ctx context.Context, viewID, name string) (sqlite.SavedView, error) {
	if strings.TrimSpace(viewID) != "" {
		if err := requireStableID("view_id", viewID); err != nil {
			return sqlite.SavedView{}, err
		}
		return d.store.GetSavedViewByID(ctx, viewID)
	}
	if strings.TrimSpace(name) == "" {
		return sqlite.SavedView{}, errString("view_id or name is required")
	}
	return d.store.GetSavedViewByName(ctx, strings.TrimSpace(name))
}

func (d *Dispatcher) handleViewEvaluate(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.search == nil {
		return degradedResult(env, started, command.ViewEvaluateData{
			Hits: []command.SearchHitData{}, Coverage: []string{search.DimensionLexical},
		}, "search index is unavailable; view evaluation cannot return membership")
	}
	var input command.ViewEvaluateInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	view, err := d.resolveView(ctx, input.ViewID, input.Name)
	if err != nil {
		if containsNotFound(err) {
			return notFoundResult(env, started, "view not found")
		}
		if isStableIDError(err) {
			return invalidInputResult(env, started, err)
		}
		return catalogErrorResult(env, started, err)
	}
	evaluated, reason := d.evaluateViewNow(ctx, view, input.Limit)
	if reason != nil {
		if errors.Is(reason, exact.ErrBlocked) || strings.Contains(reason.Error(), "unavailable") {
			return degradedResult(env, started, command.ViewEvaluateData{
				ViewID: view.ViewID, Query: view.Query, Hits: []command.SearchHitData{},
				Coverage: []string{search.DimensionLexical},
			}, reason.Error())
		}
		if isStableIDError(reason) {
			return invalidInputResult(env, started, reason)
		}
		return failed(env, started, newReason(ReasonCodeCatalogError, reason.Error()))
	}
	return succeeded(env, started, command.ViewEvaluateData{
		ViewID: view.ViewID, Query: view.Query, Hits: evaluated,
		Coverage: []string{search.DimensionLexical},
	})
}

// evaluateViewNow runs a dynamic view query through the lexical search lane.
// Membership is intentionally dynamic: evaluating twice may differ. When the
// exact lane or its index is unavailable it fails closed instead of
// fabricating hits.
func (d *Dispatcher) evaluateViewNow(ctx context.Context, view sqlite.SavedView, limit int) ([]command.SearchHitData, error) {
	if d.search == nil {
		return nil, errString("search index is unavailable")
	}
	dimension, ok := search.LookupDimension(search.DimensionLexical, search.IndexerReadiness(d.search))
	if !ok || dimension.State != command.CapabilityAvailable {
		return nil, errString("lexical search is unavailable")
	}
	filters, err := viewFiltersFromFields(view.Fields)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(view.Query) == "" && !filters.Has() {
		return nil, errString("view query is empty and carries no structured filters")
	}
	workspaceID := d.viewEvaluationWorkspace(ctx, view.Scope)
	generation, hits, err := d.search.Query(ctx, search.QueryRequest{
		WorkspaceID: workspaceID, Dimension: search.DimensionLexical,
		Text: view.Query, Axes: nil, Filters: filters,
	})
	if err != nil {
		return nil, err
	}
	authorized := d.authorizeHits(ctx, workspaceID, generation.WorkspaceID, hits, nil)
	if limit > 0 && limit < len(authorized) {
		authorized = authorized[:limit]
	}
	return authorized, nil
}

// viewEvaluationWorkspace resolves the workspace a dynamic view evaluates
// against. Ingest creates the default workspace, so a view without an explicit
// scope evaluates against it; an unavailable workspace fails closed instead of
// fabricating membership.
func (d *Dispatcher) viewEvaluationWorkspace(ctx context.Context, scope string) string {
	if strings.TrimSpace(scope) != "" {
		return strings.TrimSpace(scope)
	}
	workspace, err := d.store.GetWorkspaceByName(ctx, "default")
	if err != nil {
		return ""
	}
	return workspace.ID
}

// viewFiltersFromFields maps saved structured field constraints onto typed
// search filters. Fields use the same snake_case keys as search.filters;
// an empty field list means no structured constraint.
func viewFiltersFromFields(fields []string) (search.Filters, error) {
	var filters search.Filters
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "entry_type":
			filters.EntryType = value
		case "content_id":
			filters.ContentID = value
		case "duplicate_group":
			filters.DuplicateGroup = value
		case "protection_mode":
			filters.ProtectionMode = value
		case "suffix":
			filters.Suffix = value
		case "language":
			filters.Language = value
		case "size_min":
			parsed, err := parseInt64Field(value)
			if err != nil {
				return search.Filters{}, err
			}
			filters.SizeMin = &parsed
		case "size_max":
			parsed, err := parseInt64Field(value)
			if err != nil {
				return search.Filters{}, err
			}
			filters.SizeMax = &parsed
		case "mtime_after":
			parsed, err := parseInt64Field(value)
			if err != nil {
				return search.Filters{}, err
			}
			filters.MtimeAfter = &parsed
		case "mtime_before":
			parsed, err := parseInt64Field(value)
			if err != nil {
				return search.Filters{}, err
			}
			filters.MtimeBefore = &parsed
		default:
			continue
		}
	}
	return search.NormalizeFilters(filters)
}

func parseInt64Field(value string) (int64, error) {
	var parsed int64
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return 0, fmt.Errorf("field value %q must be an integer", value)
	}
	return parsed, nil
}

type exportPlanInput struct {
	ViewID         string   `json:"view_id,omitempty"`
	Name           string   `json:"name,omitempty"`
	Subjects       []string `json:"subjects,omitempty"`
	OutputName     string   `json:"output_name,omitempty"`
	Representation string   `json:"representation,omitempty"`
	Sidecars       string   `json:"sidecars,omitempty"`
	Target         string   `json:"target,omitempty"`
}

// handleExportPlan freezes either a live view evaluation or an explicit subject
// set into an immutable ExportManifest. The manifest digest binds the frozen
// item set, output names, selected representation, target profile, and config
// digest. Re-evaluating the view later never changes this manifest.
func (d *Dispatcher) handleExportPlan(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	var input exportPlanInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if len(input.Subjects) == 0 && strings.TrimSpace(input.ViewID) == "" && strings.TrimSpace(input.Name) == "" {
		return invalidInputResult(env, started, errString("view_id or subjects is required"))
	}
	if len(input.Subjects) > 0 && (strings.TrimSpace(input.ViewID) != "" || strings.TrimSpace(input.Name) != "") {
		return invalidInputResult(env, started, errString("view_id and subjects cannot both be set"))
	}
	if d.exact == nil || d.exact.Repo == nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, "exact repository is unavailable; export planning cannot freeze subject bytes"))
	}
	subjects, viewID, err := d.resolveExportSubjects(ctx, input)
	if err != nil {
		return exportPlanErrorResult(env, started, err)
	}
	items, err := d.freezeExportItems(ctx, subjects)
	if err != nil {
		return exportPlanErrorResult(env, started, err)
	}
	representation := strings.TrimSpace(input.Representation)
	if representation == "" {
		representation = "exact"
	}
	manifest := exact.ExportManifest{
		Schema: exact.ExportManifestSchemaV1, ViewID: viewID,
		Representation: representation, Sidecars: strings.TrimSpace(input.Sidecars),
		Target:              strings.TrimSpace(input.Target),
		ConfigDigest:        d.effectiveConfigDigest(),
		TargetProfileDigest: exact.DescribeExportManifestProfile(d.exact.Repo),
		Items:               items,
	}
	manifestID, err := sqlite.NewStableID("exm")
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	manifest.ManifestID = manifestID
	digest, err := manifest.PrepareExportManifestDigest()
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	manifest.ManifestDigest = digest
	stored := sqlite.ExportManifest{
		ManifestID: manifest.ManifestID, ManifestDigest: digest, ViewID: manifest.ViewID,
		Representation: manifest.Representation, Target: manifest.Target,
		SubjectCount: len(manifest.Items), Items: exportItemsToStrings(manifest.Items),
	}
	persisted, err := d.store.InsertExportManifest(ctx, stored)
	if err != nil {
		if errors.Is(err, sqlite.ErrConflict) {
			return conflictResult(env, started, "a different manifest already occupies this manifest id")
		}
		return catalogErrorResult(env, started, err)
	}
	return succeeded(env, started, command.ExportManifestData{
		ManifestID: persisted.ManifestID, ManifestDigest: persisted.ManifestDigest,
		ViewID: persisted.ViewID, SubjectCount: persisted.SubjectCount,
		Representation: persisted.Representation, Target: persisted.Target,
		CreatedAt: formatUnixNano(persisted.CreatedAtNS), Items: exportItemsToStrings(manifest.Items),
	})
}

// resolveExportSubjects resolves the frozen membership. An explicit Subjects
// list wins; otherwise the view is evaluated NOW and its current hit set is
// captured as stable subject references.
func (d *Dispatcher) resolveExportSubjects(ctx context.Context, input exportPlanInput) ([]sqlite.NamespaceEntry, string, error) {
	if len(input.Subjects) > 0 {
		entries := make([]sqlite.NamespaceEntry, 0, len(input.Subjects))
		for _, subject := range input.Subjects {
			if err := requireStableID("subject_ref", subject); err != nil {
				return nil, "", err
			}
			entry, err := d.lookupSubjectAcrossWorkspaces(ctx, subject)
			if err != nil {
				if containsNotFound(err) {
					return nil, "", fmt.Errorf("subject %s is not in the catalog", subject)
				}
				return nil, "", err
			}
			entries = append(entries, entry)
		}
		return entries, "", nil
	}
	if strings.TrimSpace(input.ViewID) == "" && strings.TrimSpace(input.Name) == "" {
		return nil, "", errString("view_id or subjects is required")
	}
	view, err := d.resolveView(ctx, input.ViewID, input.Name)
	if err != nil {
		return nil, "", err
	}
	evaluated, err := d.evaluateViewNow(ctx, view, 0)
	if err != nil {
		return nil, "", err
	}
	if len(evaluated) == 0 {
		return nil, "", errString("the view evaluated to an empty membership; nothing to freeze")
	}
	subjects := make([]sqlite.NamespaceEntry, 0, len(evaluated))
	for _, hit := range evaluated {
		entry, lookupErr := d.lookupSubjectAcrossWorkspaces(ctx, hit.SubjectRef)
		if lookupErr != nil {
			if containsNotFound(lookupErr) {
				continue
			}
			return nil, "", lookupErr
		}
		subjects = append(subjects, entry)
	}
	return subjects, view.ViewID, nil
}

func (d *Dispatcher) lookupSubjectAcrossWorkspaces(ctx context.Context, subjectRef string) (sqlite.NamespaceEntry, error) {
	workspace, err := d.store.GetWorkspaceByName(ctx, "default")
	if err != nil {
		return sqlite.NamespaceEntry{}, err
	}
	return d.store.GetNamespaceEntry(ctx, workspace.ID, subjectRef)
}

// freezeExportItems projects stable subjects into frozen manifest items. Only
// regular files freeze; directories, symlinks, and non-exact subjects are
// skipped. Output names are single destination-relative components derived
// from the display name, with the stable subject ref as the collision-proof
// fallback. A subject set with no exact-eligible file fails closed.
func (d *Dispatcher) freezeExportItems(ctx context.Context, entries []sqlite.NamespaceEntry) ([]exact.ExportItem, error) {
	items := make([]exact.ExportItem, 0, len(entries))
	for _, entry := range entries {
		if entry.EntryType != sqlite.EntryFile {
			continue
		}
		contentID := strings.TrimSpace(entry.ContentID)
		exactBytes := false
		if contentID != "" {
			if protection, err := d.store.GetProtectionRecordBySubject(ctx, entry.WorkspaceID, entry.ID); err == nil {
				if protection.Outcome == sqlite.ProtectionExactProtected || protection.Outcome == sqlite.ProtectionExactFallback {
					exactBytes = true
				}
			}
		}
		items = append(items, exact.ExportItem{
			SubjectRef: entry.ID, OutputName: outputNameFor(entry.DisplayName, entry.ID),
			ContentID: contentID, LogicalSize: sizeOrZero(entry.LogicalSize), Exact: exactBytes,
		})
	}
	if len(items) == 0 {
		return nil, errString("the frozen subject set contains no regular file")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].OutputName < items[j].OutputName })
	return items, nil
}

func outputNameFor(displayName, subjectRef string) string {
	name := strings.TrimSpace(displayName)
	if name == "" || name == "." || name == ".." {
		return subjectRef
	}
	name = filepathBaseClean(name)
	if name == "" || name == "." || name == ".." {
		return subjectRef
	}
	return name
}

func filepathBaseClean(name string) string {
	// Output names are destination-relative single components; strip any
	// separators defensively and reject traversal components.
	clean := filepath.Base(filepath.FromSlash(name))
	if clean == "." || clean == ".." {
		return ""
	}
	return clean
}

func sizeOrZero(size *int64) int64 {
	if size == nil {
		return 0
	}
	return *size
}

// exportItemsToStrings encodes each frozen item as one compact JSON string so
// the store's Items projection carries the full frozen output-name list. The
// returned summary keeps stable subject references for the client.
func exportItemsToStrings(items []exact.ExportItem) []string {
	if len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			continue
		}
		result = append(result, string(encoded))
	}
	return result
}

func exportItemsFromStrings(values []string) ([]exact.ExportItem, error) {
	items := make([]exact.ExportItem, 0, len(values))
	for _, value := range values {
		var item exact.ExportItem
		if err := json.Unmarshal([]byte(value), &item); err != nil {
			return nil, fmt.Errorf("decode frozen export item: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

type exportApplyVerifyInput struct {
	ManifestID     string `json:"manifest_id"`
	ManifestDigest string `json:"manifest_digest"`
	Destination    string `json:"destination"`
}

// handleExportApply revalidates the frozen manifest digest, checks the
// destination, and materializes exact bytes from the repository. Each item
// receives an exact or declared non-exact receipt via the apply result.
func (d *Dispatcher) handleExportApply(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil || d.exact.Repo == nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, "exact repository is unavailable; export.apply cannot materialize exact bytes"))
	}
	var input exportApplyVerifyInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	manifest, err := d.loadFrozenManifest(ctx, input.ManifestID, input.ManifestDigest)
	if err != nil {
		return exportManifestLookupResult(env, started, err)
	}
	applyResult, err := d.exact.ApplyExportManifest(ctx, manifest, input.Destination)
	if err != nil {
		return exportApplyVerifyErrorResult(env, started, err)
	}
	return succeeded(env, started, command.ExportApplyVerifyData{
		ManifestID: applyResult.ManifestID, ManifestDigest: applyResult.ManifestDigest,
		Destination: applyResult.Destination, Items: applyResult.Items,
		Bytes: applyResult.Bytes, Verified: applyResult.Verified,
	})
}

// handleExportVerify checks a materialized destination against the frozen
// manifest. Partial, changed, or extra output fails closed.
func (d *Dispatcher) handleExportVerify(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil || d.exact.Repo == nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, "exact repository is unavailable; export.verify cannot read the frozen manifest"))
	}
	var input exportApplyVerifyInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	manifest, err := d.loadFrozenManifest(ctx, input.ManifestID, input.ManifestDigest)
	if err != nil {
		return exportManifestLookupResult(env, started, err)
	}
	verified, err := d.exact.VerifyExportManifest(ctx, manifest, input.Destination)
	if err != nil {
		return exportApplyVerifyErrorResult(env, started, err)
	}
	if !verified {
		return failed(env, started, newReason(ReasonCodeConflict, "export verification failed: materialized output does not match the frozen manifest"))
	}
	items, bytes := manifestItemTotals(manifest)
	return succeeded(env, started, command.ExportApplyVerifyData{
		ManifestID: manifest.ManifestID, ManifestDigest: manifest.ManifestDigest,
		Destination: input.Destination, Items: items, Bytes: bytes, Verified: true,
	})
}

// loadFrozenManifest resolves a manifest by stable ID or canonical digest and
// re-derives its frozen item set from the current catalog. The recomputed
// canonical digest must equal the stored frozen digest; any drift (changed
// subject bytes, output name, representation, target profile, or config)
// fails closed rather than materializing a different set.
func (d *Dispatcher) loadFrozenManifest(ctx context.Context, manifestID, manifestDigest string) (exact.ExportManifest, error) {
	var stored sqlite.ExportManifest
	var err error
	if strings.TrimSpace(manifestID) != "" {
		if err := requireStableID("manifest_id", manifestID); err != nil {
			return exact.ExportManifest{}, err
		}
		stored, err = d.store.GetExportManifestByID(ctx, manifestID)
	} else {
		digest := strings.TrimSpace(manifestDigest)
		if !isExactDigest(digest) {
			return exact.ExportManifest{}, errString("manifest_id or manifest_digest is required")
		}
		stored, err = d.store.GetExportManifestByDigest(ctx, digest)
	}
	if err != nil {
		return exact.ExportManifest{}, err
	}
	if strings.TrimSpace(manifestDigest) != "" && stored.ManifestDigest != strings.TrimSpace(manifestDigest) {
		return exact.ExportManifest{}, fmt.Errorf("manifest digest %s does not match stored digest %s", strings.TrimSpace(manifestDigest), stored.ManifestDigest)
	}
	return d.rebuildFrozenManifest(ctx, stored)
}

// rebuildFrozenManifest reconstructs the full frozen manifest from the stored
// projection and the immutable catalog. The subject list, output names, and
// auxiliary fields come from the stored record; content identity and
// exactness are re-read from the catalog, and the canonical digest is
// re-derived and compared against the frozen digest.
func (d *Dispatcher) rebuildFrozenManifest(ctx context.Context, stored sqlite.ExportManifest) (exact.ExportManifest, error) {
	frozenItems, err := exportItemsFromStrings(stored.Items)
	if err != nil {
		return exact.ExportManifest{}, err
	}
	manifest := exact.ExportManifest{
		Schema: exact.ExportManifestSchemaV1, ManifestID: stored.ManifestID,
		ViewID: stored.ViewID, Representation: stored.Representation, Target: stored.Target,
		ConfigDigest:        d.effectiveConfigDigest(),
		TargetProfileDigest: exact.DescribeExportManifestProfile(d.exact.Repo),
	}
	for _, frozen := range frozenItems {
		entry, lookupErr := d.lookupSubjectAcrossWorkspaces(ctx, frozen.SubjectRef)
		if lookupErr != nil {
			if containsNotFound(lookupErr) {
				return exact.ExportManifest{}, fmt.Errorf("frozen subject %s is no longer in the catalog", frozen.SubjectRef)
			}
			return exact.ExportManifest{}, lookupErr
		}
		if entry.EntryType != sqlite.EntryFile {
			return exact.ExportManifest{}, fmt.Errorf("frozen subject %s is no longer a regular file", frozen.SubjectRef)
		}
		contentID := strings.TrimSpace(entry.ContentID)
		exactBytes := false
		if contentID != "" {
			if protection, protectionErr := d.store.GetProtectionRecordBySubject(ctx, entry.WorkspaceID, entry.ID); protectionErr == nil {
				if protection.Outcome == sqlite.ProtectionExactProtected || protection.Outcome == sqlite.ProtectionExactFallback {
					exactBytes = true
				}
			}
		}
		item := frozen
		item.ContentID = contentID
		item.LogicalSize = sizeOrZero(entry.LogicalSize)
		item.Exact = exactBytes
		manifest.Items = append(manifest.Items, item)
	}
	digest, err := manifest.PrepareExportManifestDigest()
	if err != nil {
		return exact.ExportManifest{}, err
	}
	if digest != stored.ManifestDigest {
		return exact.ExportManifest{}, fmt.Errorf("frozen manifest no longer matches the current catalog or configuration; re-plan the export (stored %s, now %s)", stored.ManifestDigest, digest)
	}
	manifest.ManifestDigest = digest
	return manifest, nil
}

func manifestItemTotals(manifest exact.ExportManifest) (int, int64) {
	var bytes int64
	for _, item := range manifest.Items {
		if item.Exact && item.LogicalSize > 0 {
			bytes += item.LogicalSize
		}
	}
	return len(manifest.Items), bytes
}

func projectView(view sqlite.SavedView) command.ViewData {
	return command.ViewData{
		ViewID: view.ViewID, Name: view.Name, Query: view.Query,
		Fields: append([]string(nil), view.Fields...), Scope: view.Scope, Sort: view.Sort,
		OutputNames: view.OutputNames, Required: append([]string(nil), view.Required...),
		WhenMissing: view.WhenMissing, Revision: view.Revision,
		CreatedAt: formatUnixNano(view.CreatedAtNS), UpdatedAt: formatUnixNano(view.UpdatedAtNS),
	}
}

func formatUnixNano(ns int64) string {
	if ns <= 0 {
		return ""
	}
	return time.Unix(0, ns).UTC().Format(time.RFC3339)
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	sort.Strings(result)
	return result
}

func isStableIDError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "prefix_")
}

func exportPlanErrorResult(env command.Envelope, started time.Time, err error) command.Result {
	if isStableIDError(err) || errors.Is(err, errString("view_id and subjects cannot both be set")) ||
		errors.Is(err, errString("view_id or subjects is required")) {
		return invalidInputResult(env, started, err)
	}
	if strings.Contains(err.Error(), "unavailable") || errors.Is(err, exact.ErrBlocked) {
		return failed(env, started, newReason(ReasonCodeUnavailable, err.Error()))
	}
	if strings.Contains(err.Error(), "empty membership") || strings.Contains(err.Error(), "contains no regular file") {
		return invalidInputResult(env, started, err)
	}
	if strings.Contains(err.Error(), "is not in the catalog") {
		return notFoundResult(env, started, err.Error())
	}
	return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
}

func exportManifestLookupResult(env command.Envelope, started time.Time, err error) command.Result {
	if isStableIDError(err) || strings.HasPrefix(err.Error(), "manifest_id or manifest_digest is required") {
		return invalidInputResult(env, started, err)
	}
	if containsNotFound(err) {
		return notFoundResult(env, started, "export manifest not found")
	}
	if strings.Contains(err.Error(), "does not match stored digest") || strings.Contains(err.Error(), "no longer matches") {
		return conflictResult(env, started, err.Error())
	}
	return catalogErrorResult(env, started, err)
}

func exportApplyVerifyErrorResult(env command.Envelope, started time.Time, err error) command.Result {
	if errors.Is(err, exact.ErrBlocked) {
		return failed(env, started, newReason(ReasonCodeUnavailable, err.Error()))
	}
	return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
}

func isExactDigest(value string) bool {
	algorithm, payload, ok := strings.Cut(value, ":")
	if !ok || algorithm != "sha256" || len(payload) != 64 {
		return false
	}
	for i := 0; i < len(payload); i++ {
		char := payload[i]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
