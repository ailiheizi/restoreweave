package protocol

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ailiheizi/restoreweave/client/command"
)

func (s *Server) serveOPDSNavigation(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeAtom(w, atomFeed{
		XMLName: xml.Name{Local: "feed"},
		Xmlns:   "http://www.w3.org/2005/Atom",
		Title:   "RestoreWeave books",
		ID:      "urn:restoreweave:opds:catalog",
		Links: []atomLink{
			{Rel: "self", Href: "/opds", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
			{Rel: "start", Href: "/opds", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
			{Rel: "subsection", Href: "/opds/works", Type: "application/atom+xml;profile=opds-catalog;kind=acquisition", Title: "Works"},
			{Rel: "search", Href: "/opds/search?q={searchTerms}", Type: "application/atom+xml;profile=opds-catalog;kind=acquisition", Title: "Search"},
		},
	})
}

func (s *Server) serveOPDS(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/opds/")
	switch {
	case path == "works":
		s.serveOPDSWorks(w, r)
	case path == "search":
		s.serveOPDSSearch(w, r)
	case path == "progress":
		s.serveOPDSProgress(w, r)
	case strings.HasPrefix(path, "acquire/"):
		s.serveOPDSAcquire(w, r, strings.TrimPrefix(path, "acquire/"))
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveOPDSSearch(w http.ResponseWriter, r *http.Request) {
	s.serveOPDSWorksFiltered(w, r, strings.TrimSpace(r.URL.Query().Get("q")))
}

func (s *Server) serveOPDSWorks(w http.ResponseWriter, r *http.Request) {
	s.serveOPDSWorksFiltered(w, r, "")
}

func (s *Server) serveOPDSWorksFiltered(w http.ResponseWriter, r *http.Request, query string) {
	result, err := s.call(r.Context(), command.OpBooksList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"snapshot_ref": s.opts.SnapshotRef,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	var data command.BookListData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	needle := strings.ToLower(query)
	entries := make([]atomEntry, 0, len(data.Works))
	for _, work := range data.Works {
		blob := strings.ToLower(work.Title + " " + work.Author + " " + work.Name)
		if needle != "" && !strings.Contains(blob, needle) {
			continue
		}
		entries = append(entries, atomEntry{
			ID:      "urn:restoreweave:subject:" + work.SubjectRef,
			Title:   firstNonEmpty(work.Title, work.Name),
			Author:  work.Author,
			Updated: "1970-01-01T00:00:00Z",
			Links: []atomLink{{
				Rel:  "http://opds-spec.org/acquisition",
				Href: "/opds/acquire/" + work.SubjectRef,
				Type: bookContentType(work.Kind, work.Name),
			}},
		})
	}
	start, count := pageBounds(r, len(entries))
	page := entries[start:min(start+count, len(entries))]
	self := opdsSelf(query, start, count)
	title := "RestoreWeave works"
	if query != "" {
		title = "Search: " + query
	}
	links := []atomLink{
		{Rel: "self", Href: self, Type: "application/atom+xml;profile=opds-catalog;kind=acquisition"},
		{Rel: "start", Href: "/opds", Type: "application/atom+xml;profile=opds-catalog;kind=navigation"},
	}
	if start+count < len(entries) {
		links = append(links, atomLink{
			Rel:  "next",
			Href: opdsSelf(query, start+count, count),
			Type: "application/atom+xml;profile=opds-catalog;kind=acquisition",
		})
	}
	writeAtom(w, atomFeed{
		XMLName: xml.Name{Local: "feed"},
		Xmlns:   "http://www.w3.org/2005/Atom",
		Title:   title,
		ID:      "urn:restoreweave:opds:works",
		Links:   links,
		Entries: page,
	})
}

func pageBounds(r *http.Request, total int) (start, count int) {
	count = 50
	if raw := strings.TrimSpace(r.URL.Query().Get("count")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			count = parsed
		}
	}
	if count > 200 {
		count = 200
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("start")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			start = parsed
		}
	}
	if start > total {
		start = total
	}
	return start, count
}

func opdsSelf(query string, start, count int) string {
	path := "/opds/works"
	if query != "" {
		path = "/opds/search?q=" + query
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "start=" + strconv.Itoa(start) + "&count=" + strconv.Itoa(count)
}

func (s *Server) serveOPDSAcquire(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		http.NotFound(w, r)
		return
	}
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
	result, err := s.call(r.Context(), command.OpBooksList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"snapshot_ref": s.opts.SnapshotRef,
	})
	name, kind := id, ""
	if err == nil {
		var data command.BookListData
		if json.Unmarshal(result.Data, &data) == nil {
			for _, work := range data.Works {
				if work.SubjectRef == id {
					name, kind = work.Name, work.Kind
					break
				}
			}
		}
	}
	w.Header().Set("Content-Type", bookContentType(kind, name))
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
		if err := json.Unmarshal(read.Data, &readData); err != nil {
			return
		}
		if len(readData.Bytes) == 0 {
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

type atomFeed struct {
	XMLName xml.Name
	Xmlns   string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry,omitempty"`
}

type atomEntry struct {
	ID      string     `xml:"id"`
	Title   string     `xml:"title"`
	Author  string     `xml:"author>name,omitempty"`
	Updated string     `xml:"updated"`
	Links   []atomLink `xml:"link"`
}

type atomLink struct {
	Rel   string `xml:"rel,attr"`
	Href  string `xml:"href,attr"`
	Type  string `xml:"type,attr,omitempty"`
	Title string `xml:"title,attr,omitempty"`
}

func (s *Server) serveOPDSProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var input struct {
		SubjectRef string `json:"subject_ref"`
		PositionMS int64  `json:"position_ms"`
		Completed  bool   `json:"completed"`
	}
	if err := json.Unmarshal(body, &input); err != nil || strings.TrimSpace(input.SubjectRef) == "" {
		http.Error(w, "subject_ref is required", http.StatusBadRequest)
		return
	}
	payload, err := json.Marshal(map[string]any{
		"position_ms": input.PositionMS,
		"completed":   input.Completed,
		"source":      "opds",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.call(r.Context(), command.OpAnnotationUpsert, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"subject_ref":  input.SubjectRef,
		"kind":         "PROGRESS",
		"body":         string(payload),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "subject_ref": input.SubjectRef})
}

func writeAtom(w http.ResponseWriter, feed atomFeed) {
	w.Header().Set("Content-Type", "application/atom+xml;profile=opds-catalog;charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(feed)
}
