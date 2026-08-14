package protocol

import (
	"embed"
	"encoding/json"
	"io"
	"net/http"
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
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) inboxStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"snapshot_ref": s.opts.SnapshotRef,
	})
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
	if query != "" {
		if result, err := s.call(r.Context(), command.OpSearchQuery, map[string]any{
			"workspace_id": s.opts.WorkspaceID,
			"query":        query,
		}); err == nil {
			var data command.SearchQueryData
			if json.Unmarshal(result.Data, &data) == nil {
				for _, hit := range data.Hits {
					add(hit.SubjectRef, hit.Name, hit.Path, strings.ToLower(hit.EntryType))
				}
			}
		}
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
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

func (s *Server) inboxItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return
	}
	payload := map[string]any{"subject_ref": id, "kind": "file"}
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
	snapshot := s.opts.SnapshotRef
	if snapshot == "" {
		listed, err := s.call(r.Context(), command.OpSnapshotList, map[string]any{})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		var data command.SnapshotListData
		if err := json.Unmarshal(listed.Data, &data); err != nil || len(data.Snapshots) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "no snapshot"})
			return
		}
		snapshot = data.Snapshots[0].SnapshotRef
	}
	result, err := s.call(r.Context(), command.OpSnapshotVerify, map[string]any{"snapshot_ref": snapshot})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var data command.SnapshotVerifyData
	_ = json.Unmarshal(result.Data, &data)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": data.OK, "files": data.Files, "bytes": data.Bytes, "snapshot_ref": data.SnapshotRef,
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
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&input); err != nil || strings.TrimSpace(input.Destination) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "destination is required"})
		return
	}
	snapshot := s.opts.SnapshotRef
	if snapshot == "" {
		listed, err := s.call(r.Context(), command.OpSnapshotList, map[string]any{})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		var data command.SnapshotListData
		if json.Unmarshal(listed.Data, &data) == nil && len(data.Snapshots) > 0 {
			snapshot = data.Snapshots[0].SnapshotRef
		}
	}
	result, err := s.call(r.Context(), command.OpPlanRestore, map[string]any{
		"snapshot_ref": snapshot,
		"destination":  input.Destination,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	var data command.PlanRestoreData
	_ = json.Unmarshal(result.Data, &data)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "files": data.Files, "bytes": data.Bytes, "destination": data.Destination,
	})
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

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func fmtString(value any) string {
	text, _ := value.(string)
	return text
}
