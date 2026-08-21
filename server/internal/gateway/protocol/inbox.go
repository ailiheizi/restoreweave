package protocol

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ailiheizi/restoreweave/client/command"
)

//go:embed inbox.html
var inboxFS embed.FS

func (s *Server) serveInbox(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/inbox" || r.URL.Path == "/inbox/" {
		if !s.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		page, err := inboxFS.ReadFile("inbox.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/inbox/api/") {
		http.NotFound(w, r)
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/inbox/api/") {
	case "status":
		s.inboxStatus(w, r)
	case "doctor":
		s.inboxDoctor(w, r)
	case "job":
		s.inboxJob(w, r)
	case "plan":
		s.inboxPlan(w, r)
	case "snapshots":
		s.inboxSnapshots(w, r)
	case "diff":
		s.inboxDiff(w, r)
	case "annotations":
		s.inboxAnnotations(w, r)
	case "resolve":
		s.inboxResolve(w, r)
	case "search":
		s.inboxSearch(w, r)
	case "item":
		s.inboxItem(w, r)
	case "preview":
		s.inboxPreview(w, r)
	case "progress":
		s.inboxProgress(w, r)
	case "verify":
		s.inboxVerify(w, r)
	case "restore":
		s.inboxRestore(w, r)
	case "recovery":
		s.inboxRecovery(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) inboxStatus(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"snapshot_ref": s.opts.SnapshotRef,
	}
	if result, err := s.call(r.Context(), command.OpStatusGet, map[string]any{}); err == nil {
		var status command.StatusData
		if json.Unmarshal(result.Data, &status) == nil {
			payload["controller"] = status.Controller
			payload["catalog_ok"] = status.Catalog.OK
			payload["publications"] = status.Publications
			payload["plans"] = status.Plans
			payload["recent_plans"] = status.RecentPlans
			payload["jobs"] = status.Jobs
			payload["recent_jobs"] = status.RecentJobs
			payload["open_handles"] = status.OpenHandles
			if status.Repository != nil {
				payload["repository_ok"] = status.Repository.OK
				payload["snapshots"] = status.Repository.Snapshots
			}
		}
	}
	if strings.TrimSpace(fmtString(payload["snapshot_ref"])) == "" {
		if result, err := s.call(r.Context(), command.OpSnapshotList, map[string]any{}); err == nil {
			var listed command.SnapshotListData
			if json.Unmarshal(result.Data, &listed) == nil && len(listed.Snapshots) > 0 {
				payload["snapshot_ref"] = listed.Snapshots[0].SnapshotRef
			}
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) inboxDoctor(w http.ResponseWriter, r *http.Request) {
	input := map[string]any{}
	if source := strings.TrimSpace(r.URL.Query().Get("source")); source != "" {
		input["source"] = source
	}
	raw, err := json.Marshal(input)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	env, err := command.NormalizeEnvelope(command.Envelope{Operation: command.OpDoctorCheck, Input: raw})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	result := s.dispatch(r.Context(), env)
	if result.Status != command.StatusSucceeded && result.Status != command.StatusDegraded {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": result.Status})
		return
	}
	var data command.DoctorData
	if json.Unmarshal(result.Data, &data) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "doctor payload"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": data.OK, "checks": data.Checks})
}

func (s *Server) inboxJob(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(r.URL.Query().Get("id"))
	if jobID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return
	}
	input := map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"job_id":       jobID,
	}
	if after := strings.TrimSpace(r.URL.Query().Get("after")); after != "" {
		sequence, err := strconv.ParseInt(after, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "after must be an integer"})
			return
		}
		input["after_sequence"] = sequence
	}
	if limit := strings.TrimSpace(r.URL.Query().Get("limit")); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be an integer"})
			return
		}
		input["limit"] = n
	}
	result, err := s.call(r.Context(), command.OpJobEvents, input)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var data command.JobEventsData
	if json.Unmarshal(result.Data, &data) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "job payload"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"job_id":        data.JobID,
		"job_state":     data.JobState,
		"events":        data.Events,
		"next_sequence": data.NextSequence,
		"terminal":      data.Terminal,
	})
}

func (s *Server) inboxPlan(w http.ResponseWriter, r *http.Request) {
	planID := strings.TrimSpace(r.URL.Query().Get("id"))
	if planID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return
	}
	result, err := s.call(r.Context(), command.OpPlanGet, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"plan_id":      planID,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var data command.PlanGetData
	if json.Unmarshal(result.Data, &data) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "plan payload"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"plan_id":     data.PlanID,
		"kind":        data.Kind,
		"state":       data.State,
		"plan_digest": data.PlanDigest,
		"applied":     data.Applied,
		"executable":  data.Executable,
		"abandoned":   data.Abandoned,
		"created_at":  data.CreatedAt,
		"plan":        data.Plan,
	})
}

func (s *Server) inboxSnapshots(w http.ResponseWriter, r *http.Request) {
	result, err := s.call(r.Context(), command.OpSnapshotList, map[string]any{})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var data command.SnapshotListData
	if json.Unmarshal(result.Data, &data) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "snapshot payload"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"snapshots": data.Snapshots,
	})
}

func (s *Server) inboxDiff(w http.ResponseWriter, r *http.Request) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" || to == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "from and to are required"})
		return
	}
	result, err := s.call(r.Context(), command.OpSnapshotDiff, map[string]any{
		"from_snapshot_ref": from,
		"to_snapshot_ref":   to,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var data command.SnapshotDiffData
	if json.Unmarshal(result.Data, &data) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "diff payload"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"from_snapshot_ref": data.FromSnapshotRef,
		"to_snapshot_ref":   data.ToSnapshotRef,
		"changes":           data.Changes,
	})
}

func (s *Server) inboxAnnotations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.inboxAnnotationExport(w, r)
	case http.MethodPost:
		s.inboxAnnotationImport(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "GET or POST required"})
	}
}

func (s *Server) inboxAnnotationExport(w http.ResponseWriter, r *http.Request) {
	input := map[string]any{"workspace_id": s.opts.WorkspaceID}
	if subject := strings.TrimSpace(r.URL.Query().Get("id")); subject != "" {
		input["subject_ref"] = subject
	}
	result, err := s.call(r.Context(), command.OpAnnotationExport, input)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var data command.AnnotationExportData
	if json.Unmarshal(result.Data, &data) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "annotation payload"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"schema":      data.Schema,
		"annotations": data.Annotations,
	})
}

func (s *Server) inboxAnnotationImport(w http.ResponseWriter, r *http.Request) {
	var bundle command.AnnotationExportData
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&bundle); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "annotation bundle is required"})
		return
	}
	if bundle.Schema == "" {
		bundle.Schema = command.AnnotationBundleSchema
	}
	if bundle.Conflict == "" {
		bundle.Conflict = command.AnnotationConflictFail
	}
	result, err := s.call(r.Context(), command.OpAnnotationImport, bundle)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var data command.AnnotationExportData
	if json.Unmarshal(result.Data, &data) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "annotation payload"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"schema":      data.Schema,
		"imported":    len(data.Annotations),
		"annotations": data.Annotations,
	})
}

func (s *Server) inboxResolve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path is required"})
		return
	}
	data, err := s.resolvePath(r, path)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"path":        data.Path,
		"path_ref":    data.PathRef,
		"entry_id":    data.Entry.ID,
		"name":        data.Entry.DisplayName,
		"entry_type":  data.Entry.EntryType,
		"subject_ref": data.PathRef,
	})
}

func (s *Server) resolvePath(r *http.Request, path string) (command.NamespaceResolveData, error) {
	input := map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"path":         path,
	}
	if snapshot := strings.TrimSpace(s.opts.SnapshotRef); snapshot != "" {
		input["snapshot_ref"] = snapshot
	} else if result, err := s.call(r.Context(), command.OpSnapshotList, map[string]any{}); err == nil {
		var listed command.SnapshotListData
		if json.Unmarshal(result.Data, &listed) == nil && len(listed.Snapshots) > 0 {
			input["snapshot_ref"] = listed.Snapshots[0].SnapshotRef
		}
	}
	result, err := s.call(r.Context(), command.OpNamespaceResolve, input)
	if err != nil {
		return command.NamespaceResolveData{}, err
	}
	var data command.NamespaceResolveData
	if json.Unmarshal(result.Data, &data) != nil {
		return command.NamespaceResolveData{}, fmt.Errorf("resolve payload")
	}
	return data, nil
}

func (s *Server) inboxSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	hits := make([]map[string]any, 0)
	seen := map[string]int{}
	add := func(id, name, path, kind string) {
		if id == "" {
			return
		}
		if idx, ok := seen[id]; ok {
			if kind == "audio" || kind == "epub" || kind == "text" {
				hits[idx]["kind"] = kind
				if name != "" {
					hits[idx]["name"] = name
				}
			}
			return
		}
		seen[id] = len(hits)
		hits = append(hits, map[string]any{
			"subject_ref": id, "name": name, "path": path, "kind": kind,
		})
	}
	dimension := strings.TrimSpace(r.URL.Query().Get("dimension"))
	var fuse []string
	if raw := strings.TrimSpace(r.URL.Query().Get("fuse")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if id := strings.TrimSpace(part); id != "" {
				fuse = append(fuse, id)
			}
		}
	}
	passthrough := dimension != "" || len(fuse) > 0
	if query != "" {
		input := map[string]any{
			"workspace_id": s.opts.WorkspaceID,
			"query":        query,
		}
		if dimension != "" {
			input["dimension"] = dimension
		}
		if len(fuse) > 0 {
			input["fuse"] = fuse
		}
		if result, err := s.call(r.Context(), command.OpSearchQuery, input); err == nil {
			var data command.SearchQueryData
			if json.Unmarshal(result.Data, &data) == nil {
				for _, hit := range data.Hits {
					add(hit.SubjectRef, hit.Name, hit.Path, strings.ToLower(hit.EntryType))
				}
			}
		}
	}
	if passthrough {
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "dimension": dimension, "fuse": fuse})
		return
	}
	if audio, err := s.audioCatalog(r); err == nil {
		needle := strings.ToLower(query)
		for _, track := range audio.Tracks {
			blob := strings.ToLower(track.Title + " " + track.Artist + " " + track.Name)
			if needle == "" || strings.Contains(blob, needle) {
				add(track.SubjectRef, firstNonEmpty(track.Title, track.Name), track.Name, "audio")
			}
		}
	}
	if result, err := s.call(r.Context(), command.OpBooksList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"snapshot_ref": s.opts.SnapshotRef,
	}); err == nil {
		var books command.BookListData
		if json.Unmarshal(result.Data, &books) == nil {
			needle := strings.ToLower(query)
			for _, work := range books.Works {
				blob := strings.ToLower(work.Title + " " + work.Author + " " + work.Name)
				if needle == "" || strings.Contains(blob, needle) {
					add(work.SubjectRef, firstNonEmpty(work.Title, work.Name), work.Name, work.Kind)
				}
			}
		}
	}
	if query != "" {
		if resolved, err := s.resolvePath(r, query); err == nil && resolved.PathRef != "" {
			add(resolved.PathRef, resolved.Entry.DisplayName, resolved.Path, strings.ToLower(resolved.Entry.EntryType))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

func (s *Server) inboxItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return
	}
	payload := map[string]any{
		"subject_ref":  id,
		"kind":         "file",
		"snapshot_ref": s.opts.SnapshotRef,
	}
	if strings.TrimSpace(fmtString(payload["snapshot_ref"])) == "" {
		if result, err := s.call(r.Context(), command.OpSnapshotList, map[string]any{}); err == nil {
			var listed command.SnapshotListData
			if json.Unmarshal(result.Data, &listed) == nil && len(listed.Snapshots) > 0 {
				payload["snapshot_ref"] = listed.Snapshots[0].SnapshotRef
			}
		}
	}
	if audio, err := s.audioCatalog(r); err == nil {
		for _, track := range audio.Tracks {
			if track.SubjectRef == id {
				payload["kind"] = "audio"
				payload["title"] = firstNonEmpty(track.Title, track.Name)
				payload["name"] = track.Name
				payload["artist"] = track.Artist
			}
		}
	}
	if result, err := s.call(r.Context(), command.OpBooksList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"snapshot_ref": s.opts.SnapshotRef,
	}); err == nil {
		var books command.BookListData
		if json.Unmarshal(result.Data, &books) == nil {
			for _, work := range books.Works {
				if work.SubjectRef == id {
					payload["kind"] = work.Kind
					payload["title"] = firstNonEmpty(work.Title, work.Name)
					payload["name"] = work.Name
					payload["author"] = work.Author
				}
			}
		}
	}
	if result, err := s.call(r.Context(), command.OpNamespaceStat, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"entry_id":     id,
	}); err == nil {
		var stat command.NamespaceStatData
		if json.Unmarshal(result.Data, &stat) == nil {
			payload["name"] = firstNonEmpty(fmtString(payload["name"]), stat.Entry.DisplayName)
			payload["content_id"] = stat.Entry.ContentID
			payload["entry_type"] = stat.Entry.EntryType
			if path := s.namespaceDisplayPath(r, id); path != "" {
				payload["path"] = path
			}
		}
	}
	if result, err := s.call(r.Context(), command.OpRepresentationList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"subject_ref":  id,
	}); err == nil {
		var reps command.RepresentationListData
		if json.Unmarshal(result.Data, &reps) == nil && len(reps.Representations) > 0 {
			payload["representations"] = reps.Representations
		}
	}
	if result, err := s.call(r.Context(), command.OpAnnotationList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"subject_ref":  id,
	}); err == nil {
		var notes command.AnnotationListData
		if json.Unmarshal(result.Data, &notes) == nil {
			payload["annotations"] = notes.Annotations
		}
	}
	if kind, _ := payload["kind"].(string); kind == "text" || kind == "epub" {
		if text := s.previewText(r, id); text != "" {
			payload["preview_text"] = text
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) inboxPreview(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	s.streamSubject(w, r, id, "")
}

func (s *Server) inboxProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.serveOPDSProgress(w, r)
}

func (s *Server) inboxVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	snapshot, err := s.inboxPinnedSnapshot(r)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if snapshot == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no snapshot"})
		return
	}
	var input struct {
		Mode        string `json:"mode"`
		Destination string `json:"destination"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&input)
	}
	payload := map[string]any{"snapshot_ref": snapshot}
	if mode := strings.TrimSpace(input.Mode); mode != "" {
		payload["mode"] = mode
	}
	if dest := strings.TrimSpace(input.Destination); dest != "" {
		payload["destination"] = dest
	}
	result, err := s.call(r.Context(), command.OpSnapshotVerify, payload)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var data command.SnapshotVerifyData
	_ = json.Unmarshal(result.Data, &data)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": data.OK, "files": data.Files, "bytes": data.Bytes, "snapshot_ref": data.SnapshotRef,
		"mode": data.Mode, "accepted_level": data.AcceptedLevel,
		"restore_verified": data.RestoreVerified, "catalog_used": data.CatalogUsed,
	})
}

func (s *Server) inboxRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var input struct {
		Destination string `json:"destination"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&input); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid restore payload"})
		return
	}
	snapshot, err := s.inboxPinnedSnapshot(r)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	payload := map[string]any{"snapshot_ref": snapshot}
	if dest := strings.TrimSpace(input.Destination); dest != "" {
		payload["destination"] = dest
	}
	result, err := s.call(r.Context(), command.OpPlanRestore, payload)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var planned command.PlanRestoreData
	if err := json.Unmarshal(result.Data, &planned); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "restore plan payload"})
		return
	}

	// A destination turns the Inbox porcelain into an execution request. The
	// restore operation itself is deliberately only a preflight/plan creator.
	data := planned
	if strings.TrimSpace(input.Destination) != "" {
		workspaceID := strings.TrimSpace(planned.WorkspaceID)
		if workspaceID == "" {
			workspaceID = s.opts.WorkspaceID
		}
		applied, err := s.call(r.Context(), command.OpPlanApply, map[string]any{
			"workspace_id": workspaceID,
			"plan_id":      planned.PlanID,
			"plan_digest":  planned.PlanDigest,
		})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		var applyData command.PlanApplyData
		if err := json.Unmarshal(applied.Data, &applyData); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "restore apply payload"})
			return
		}
		data.Wrote = true
		data.Files = applyData.Files
		data.Bytes = applyData.Bytes
		if applyData.Destination != "" {
			data.Destination = applyData.Destination
		}
		if applyData.SnapshotRef != "" {
			data.SnapshotRef = applyData.SnapshotRef
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "wrote": data.Wrote, "files": data.Files, "bytes": data.Bytes,
		"destination": data.Destination, "plan_id": data.PlanID,
	})
}

func (s *Server) inboxRecovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var input struct {
		Destination string `json:"destination"`
		SnapshotRef string `json:"snapshot_ref"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&input); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid recovery payload"})
		return
	}
	destination := strings.TrimSpace(input.Destination)
	if destination == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "destination is required"})
		return
	}
	snapshot := strings.TrimSpace(input.SnapshotRef)
	if snapshot == "" {
		var err error
		snapshot, err = s.inboxPinnedSnapshot(r)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
	}
	if snapshot == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no snapshot"})
		return
	}
	raw, err := json.Marshal(map[string]any{
		"snapshot_ref": snapshot,
		"destination":  destination,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	env, err := command.NormalizeEnvelope(command.Envelope{Operation: command.OpRecoveryExport, Input: raw})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	result := s.dispatch(r.Context(), env)
	if resultHasCode(result, "conflict") {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "recovery artifact already exists"})
		return
	}
	if result.Status != command.StatusSucceeded {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": result.Status})
		return
	}
	var data command.RecoveryExportData
	if json.Unmarshal(result.Data, &data) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "recovery payload"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"snapshot_ref":         data.SnapshotRef,
		"schema":               data.Schema,
		"manifest_digest":      data.ManifestDigest,
		"artifact_path":        data.ArtifactPath,
		"length":               data.Length,
		"files":                data.Files,
		"bytes":                data.Bytes,
		"independently_stored": data.IndependentlyStored,
	})
}

func (s *Server) inboxPinnedSnapshot(r *http.Request) (string, error) {
	if snapshot := strings.TrimSpace(s.opts.SnapshotRef); snapshot != "" {
		return snapshot, nil
	}
	listed, err := s.call(r.Context(), command.OpSnapshotList, map[string]any{})
	if err != nil {
		return "", err
	}
	var data command.SnapshotListData
	if json.Unmarshal(listed.Data, &data) != nil || len(data.Snapshots) == 0 {
		return "", nil
	}
	return data.Snapshots[0].SnapshotRef, nil
}

func resultHasCode(result command.Result, code string) bool {
	for _, reason := range result.Reasons {
		if reason.Code == code {
			return true
		}
	}
	return false
}

func (s *Server) previewText(r *http.Request, id string) string {
	opened, err := s.call(r.Context(), command.OpContentOpen, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"entry_id":     id,
	})
	if err != nil {
		return ""
	}
	var openData command.ContentOpenData
	if json.Unmarshal(opened.Data, &openData) != nil {
		return ""
	}
	defer func() {
		_, _ = s.call(r.Context(), command.OpContentClose, map[string]any{"handle": openData.Handle})
	}()
	read, err := s.call(r.Context(), command.OpContentRead, map[string]any{
		"handle": openData.Handle,
		"offset": 0,
		"length": 4096,
	})
	if err != nil {
		return ""
	}
	var readData command.ContentReadData
	if json.Unmarshal(read.Data, &readData) != nil {
		return ""
	}
	text := string(readData.Bytes)
	if !strings.Contains(text, "\x00") {
		return text
	}
	return ""
}

func (s *Server) streamSubject(w http.ResponseWriter, r *http.Request, id, contentType string) {
	opened, err := s.call(r.Context(), command.OpContentOpen, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"entry_id":     id,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var openData command.ContentOpenData
	if err := json.Unmarshal(opened.Data, &openData); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		_, _ = s.call(r.Context(), command.OpContentClose, map[string]any{"handle": openData.Handle})
	}()
	if contentType == "" {
		if audio, err := s.audioCatalog(r); err == nil {
			for _, track := range audio.Tracks {
				if track.SubjectRef == id {
					contentType = audioContentType(track.Name)
				}
			}
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	offset := int64(0)
	for {
		read, err := s.call(r.Context(), command.OpContentRead, map[string]any{
			"handle": openData.Handle,
			"offset": offset,
			"length": commandMaxRead,
		})
		if err != nil {
			return
		}
		var readData command.ContentReadData
		if json.Unmarshal(read.Data, &readData) != nil || len(readData.Bytes) == 0 {
			return
		}
		if _, err := w.Write(readData.Bytes); err != nil {
			return
		}
		offset += int64(len(readData.Bytes))
		if readData.EOF {
			return
		}
	}
}

func (s *Server) namespaceDisplayPath(r *http.Request, entryID string) string {
	var parts []string
	id := entryID
	for i := 0; i < 64 && id != ""; i++ {
		result, err := s.call(r.Context(), command.OpNamespaceStat, map[string]any{
			"workspace_id": s.opts.WorkspaceID,
			"entry_id":     id,
		})
		if err != nil {
			break
		}
		var stat command.NamespaceStatData
		if json.Unmarshal(result.Data, &stat) != nil {
			break
		}
		if stat.Entry.DisplayName != "" {
			parts = append([]string{stat.Entry.DisplayName}, parts...)
		}
		id = stat.Entry.ParentID
	}
	return strings.Join(parts, "/")
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func fmtString(value any) string {
	text, _ := value.(string)
	return text
}
