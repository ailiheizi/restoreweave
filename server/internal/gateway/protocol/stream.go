package protocol

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ailiheizi/restoreweave/client/command"
)

func (s *Server) serveStream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeSubsonicError(w, r, 10, "id is required")
		return
	}
	opened, err := s.call(r.Context(), command.OpContentOpen, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"entry_id":     id,
	})
	if err != nil {
		writeSubsonicError(w, r, 70, err.Error())
		return
	}
	var openData command.ContentOpenData
	if err := json.Unmarshal(opened.Data, &openData); err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	defer func() {
		_, _ = s.call(r.Context(), command.OpContentClose, map[string]any{"handle": openData.Handle})
	}()

	start, end := int64(0), openData.LogicalSize-1
	if end < 0 {
		end = -1
	}
	if header := r.Header.Get("Range"); strings.HasPrefix(header, "bytes=") {
		spec := strings.TrimPrefix(header, "bytes=")
		parts := strings.SplitN(spec, "-", 2)
		if parts[0] != "" {
			if parsed, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
				start = parsed
			}
		}
		if len(parts) == 2 && parts[1] != "" {
			if parsed, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				end = parsed
			}
		}
	}
	if start < 0 || (openData.LogicalSize > 0 && start >= openData.LogicalSize) {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end < start {
		end = start - 1
	}
	if openData.LogicalSize > 0 && end >= openData.LogicalSize {
		end = openData.LogicalSize - 1
	}
	length := end - start + 1
	if length < 0 {
		length = 0
	}

	catalog, _ := s.audioCatalog(r)
	name := id
	for _, track := range catalog.Tracks {
		if track.SubjectRef == id {
			name = track.Name
			break
		}
	}
	w.Header().Set("Content-Type", audioContentType(name))
	w.Header().Set("Accept-Ranges", "bytes")
	if r.Header.Get("Range") != "" {
		w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(openData.LogicalSize, 10))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(openData.LogicalSize, 10))
	}

	offset := start
	remaining := length
	for remaining > 0 {
		chunk := remaining
		if chunk > commandMaxRead {
			chunk = commandMaxRead
		}
		read, err := s.call(r.Context(), command.OpContentRead, map[string]any{
			"handle": openData.Handle,
			"offset": offset,
			"length": chunk,
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
		remaining -= int64(len(readData.Bytes))
		if readData.EOF {
			return
		}
	}
	_, _ = io.Copy(io.Discard, r.Body)
}

const commandMaxRead = 1 << 20
