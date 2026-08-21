package protocol

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
)

const (
	subsonicVersion = "1.16.1"
	serverType      = "restoreweave"
	serverVersion   = "dev"
)

func (s *Server) serveOpenSubsonic(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeSubsonicError(w, r, 40, "wrong username or password")
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/rest/"), "/")
	name = strings.TrimSuffix(name, ".view")
	switch name {
	case "ping":
		writeSubsonicOK(w, r, nil)
	case "getLicense":
		writeSubsonicOK(w, r, map[string]any{
			"license": map[string]any{"valid": true, "email": "local@restoreweave", "licenseExpires": "2099-01-01T00:00:00Z"},
		})
	case "getMusicFolders":
		writeSubsonicOK(w, r, map[string]any{
			"musicFolders": map[string]any{
				"musicFolder": []map[string]any{{"id": 1, "name": "RestoreWeave"}},
			},
		})
	case "getArtists", "getIndexes":
		s.serveArtists(w, r)
	case "getArtist":
		s.serveArtist(w, r)
	case "getAlbum", "getMusicDirectory":
		s.serveAlbumOrDirectory(w, r)
	case "getAlbumList", "getAlbumList2":
		s.serveAlbumList(w, r, name)
	case "getSong":
		s.serveSong(w, r)
	case "stream", "download":
		s.serveStream(w, r)
	case "scrobble":
		s.serveScrobble(w, r)
	case "search2", "search3":
		s.serveSearch(w, r)
	case "getCoverArt":
		s.serveCoverArt(w, r)
	case "star":
		s.serveStar(w, r, true)
	case "unstar":
		s.serveStar(w, r, false)
	case "getStarred", "getStarred2":
		s.serveStarred(w, r)
	case "getBookmarks":
		s.serveGetBookmarks(w, r)
	case "createBookmark":
		s.serveCreateBookmark(w, r)
	case "deleteBookmark":
		s.serveDeleteBookmark(w, r)
	case "getRandomSongs":
		s.serveRandomSongs(w, r)
	case "getSimilarSongs", "getSimilarSongs2":
		s.serveSimilar(w, r)
	case "getSongsByGenre":
		writeSubsonicOK(w, r, map[string]any{"songsByGenre": map[string]any{"song": []any{}}})
	case "getUser", "getScanStatus", "getOpenSubsonicExtensions", "getPlaylists", "getPlaylist",
		"createPlaylist", "updatePlaylist", "deletePlaylist", "getGenres", "getNowPlaying",
		"getInternetRadioStations", "getPodcasts", "getShares", "getLyrics", "getLyricsBySongId",
		"getAvatar", "startScan", "getAlbumInfo", "getAlbumInfo2", "getArtistInfo", "getArtistInfo2",
		"getPlayQueue", "savePlayQueue", "setRating", "getVideos":
		s.serveHandshake(w, r, name)
	default:
		writeSubsonicError(w, r, 0, "unimplemented OpenSubsonic method")
	}
}

func (s *Server) audioCatalog(r *http.Request) (command.AudioListData, error) {
	result, err := s.call(r.Context(), command.OpAudioList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"snapshot_ref": s.opts.SnapshotRef,
	})
	if err != nil {
		return command.AudioListData{}, err
	}
	var data command.AudioListData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return command.AudioListData{}, err
	}
	return data, nil
}

func (s *Server) serveArtists(w http.ResponseWriter, r *http.Request) {
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	starred, _ := s.starredTimes(r)
	counts := map[string]int{}
	names := map[string]string{}
	for _, album := range data.Albums {
		id := artistID(album.Artist)
		counts[id]++
		names[id] = album.Artist
	}
	indexes := map[string][]map[string]any{}
	order := make([]string, 0)
	seen := map[string]bool{}
	for _, album := range data.Albums {
		id := artistID(album.Artist)
		if seen[id] {
			continue
		}
		seen[id] = true
		key := indexName(album.Artist)
		if _, ok := indexes[key]; !ok {
			order = append(order, key)
		}
		indexes[key] = append(indexes[key], markStarred(map[string]any{
			"id": id, "name": names[id], "albumCount": counts[id],
		}, artistStarred(data, starred, id)))
	}
	index := make([]map[string]any, 0, len(order))
	for _, key := range order {
		index = append(index, map[string]any{"name": key, "artist": indexes[key]})
	}
	if strings.Contains(r.URL.Path, "getIndexes") {
		writeSubsonicOK(w, r, map[string]any{"indexes": map[string]any{"index": index}})
		return
	}
	writeSubsonicOK(w, r, map[string]any{"artists": map[string]any{"index": index}})
}

func (s *Server) serveArtist(w http.ResponseWriter, r *http.Request) {
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	id := r.URL.Query().Get("id")
	starred, _ := s.starredTimes(r)
	albums := make([]map[string]any, 0)
	name := ""
	for _, album := range data.Albums {
		if artistID(album.Artist) != id {
			continue
		}
		name = album.Artist
		albums = append(albums, markStarred(map[string]any{
			"id":        albumID(album.Artist, album.Title),
			"name":      firstNonEmpty(album.Title, "Unknown Album"),
			"artist":    album.Artist,
			"artistId":  id,
			"songCount": len(album.SubjectRefs),
			"duration":  int(album.DurationMS / 1000),
			"year":      album.Year,
		}, collectionStarred(data, starred, album.SubjectRefs)))
	}
	if name == "" && id != "" {
		writeSubsonicError(w, r, 70, "artist not found")
		return
	}
	writeSubsonicOK(w, r, map[string]any{
		"artist": markStarred(map[string]any{"id": id, "name": name, "albumCount": len(albums), "album": albums}, artistStarred(data, starred, id)),
	})
}

func (s *Server) serveAlbumOrDirectory(w http.ResponseWriter, r *http.Request) {
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	id := r.URL.Query().Get("id")
	if strings.HasPrefix(id, "ar_") {
		s.serveArtist(w, r)
		return
	}
	var found *command.AudioAlbum
	for i := range data.Albums {
		album := &data.Albums[i]
		if albumID(album.Artist, album.Title) == id {
			found = album
			break
		}
	}
	if found == nil {
		writeSubsonicError(w, r, 70, "album not found")
		return
	}
	starred, _ := s.starredTimes(r)
	songs := songsForAlbum(data.Tracks, *found, starred)
	payload := markStarred(map[string]any{
		"id":        id,
		"name":      firstNonEmpty(found.Title, "Unknown Album"),
		"artist":    found.Artist,
		"artistId":  artistID(found.Artist),
		"songCount": len(songs),
		"duration":  int(found.DurationMS / 1000),
		"song":      songs,
		"child":     songs,
	}, collectionStarred(data, starred, found.SubjectRefs))
	if strings.Contains(r.URL.Path, "getMusicDirectory") {
		writeSubsonicOK(w, r, map[string]any{"directory": payload})
		return
	}
	writeSubsonicOK(w, r, map[string]any{"album": payload})
}

func (s *Server) serveAlbumList(w http.ResponseWriter, r *http.Request, name string) {
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	starred, _ := s.starredTimes(r)
	albums := make([]map[string]any, 0, len(data.Albums))
	for _, album := range data.Albums {
		albums = append(albums, markStarred(map[string]any{
			"id":        albumID(album.Artist, album.Title),
			"name":      firstNonEmpty(album.Title, "Unknown Album"),
			"artist":    album.Artist,
			"artistId":  artistID(album.Artist),
			"songCount": len(album.SubjectRefs),
			"duration":  int(album.DurationMS / 1000),
			"year":      album.Year,
		}, collectionStarred(data, starred, album.SubjectRefs)))
	}
	key := "albumList2"
	if name == "getAlbumList" {
		key = "albumList"
	}
	writeSubsonicOK(w, r, map[string]any{key: map[string]any{"album": albums}})
}

func (s *Server) serveSong(w http.ResponseWriter, r *http.Request) {
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	id := r.URL.Query().Get("id")
	starred, _ := s.starredTimes(r)
	for _, track := range data.Tracks {
		if track.SubjectRef == id {
			writeSubsonicOK(w, r, map[string]any{"song": songPayloadWithStar(track, starred[track.SubjectRef])})
			return
		}
	}
	writeSubsonicError(w, r, 70, "song not found")
}

func (s *Server) serveScrobble(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeSubsonicError(w, r, 10, "id is required")
		return
	}
	completed := r.URL.Query().Get("submission") != "false"
	playedAt := time.Now().UTC().UnixMilli()
	if raw := r.URL.Query().Get("time"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			playedAt = parsed
		}
	}
	body, err := json.Marshal(map[string]any{
		"position_ms":       0,
		"completed":         completed,
		"source":            "opensubsonic",
		"played_at_unix_ms": playedAt,
	})
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	if _, err := s.call(r.Context(), command.OpAnnotationUpsert, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"subject_ref":  id,
		"kind":         "PROGRESS",
		"body":         string(body),
	}); err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	writeSubsonicOK(w, r, nil)
}

func songsForAlbum(tracks []command.AudioTrack, album command.AudioAlbum, starred map[string]string) []map[string]any {
	wanted := map[string]bool{}
	for _, ref := range album.SubjectRefs {
		wanted[ref] = true
	}
	songs := make([]map[string]any, 0, len(album.SubjectRefs))
	for _, track := range tracks {
		if wanted[track.SubjectRef] {
			songs = append(songs, songPayloadWithStar(track, starred[track.SubjectRef]))
		}
	}
	return songs
}

func songPayload(track command.AudioTrack) map[string]any {
	return map[string]any{
		"id":          track.SubjectRef,
		"parent":      albumID(track.Artist, track.Album),
		"title":       firstNonEmpty(track.Title, track.Name),
		"album":       track.Album,
		"artist":      track.Artist,
		"track":       track.Track,
		"year":        track.Year,
		"duration":    int(track.DurationMS / 1000),
		"size":        0,
		"suffix":      strings.TrimPrefix(suffixOf(track.Name), "."),
		"contentType": audioContentType(track.Name),
		"isDir":       false,
		"type":        "music",
		"path":        track.Name,
	}
}

func writeSubsonicOK(w http.ResponseWriter, r *http.Request, extra map[string]any) {
	writeSubsonic(w, r, "ok", extra, 0, "")
}

func writeSubsonicError(w http.ResponseWriter, r *http.Request, code int, message string) {
	httpStatus := http.StatusBadRequest
	if code == 40 {
		httpStatus = http.StatusUnauthorized
	}
	if code == 70 {
		httpStatus = http.StatusNotFound
	}
	w.WriteHeader(httpStatus)
	writeSubsonic(w, r, "failed", nil, code, message)
}

func writeSubsonic(w http.ResponseWriter, r *http.Request, status string, extra map[string]any, code int, message string) {
	if wantsJSON(r) {
		payload := map[string]any{
			"status":        status,
			"version":       subsonicVersion,
			"type":          serverType,
			"serverVersion": serverVersion,
			"openSubsonic":  true,
		}
		for key, value := range extra {
			payload[key] = value
		}
		if status != "ok" {
			payload["error"] = map[string]any{"code": code, "message": message}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": payload})
		return
	}
	type xmlError struct {
		Code    int    `xml:"code,attr"`
		Message string `xml:"message,attr"`
	}
	type xmlResp struct {
		XMLName       xml.Name  `xml:"http://subsonic.org/restapi subsonic-response"`
		Status        string    `xml:"status,attr"`
		Version       string    `xml:"version,attr"`
		Type          string    `xml:"type,attr"`
		ServerVersion string    `xml:"serverVersion,attr"`
		OpenSubsonic  bool      `xml:"openSubsonic,attr"`
		Error         *xmlError `xml:"error,omitempty"`
	}
	resp := xmlResp{
		Status: status, Version: subsonicVersion, Type: serverType,
		ServerVersion: serverVersion, OpenSubsonic: true,
	}
	if status != "ok" {
		resp.Error = &xmlError{Code: code, Message: message}
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	if extra != nil && status == "ok" {
		_ = writeXMLTree(w, extra, status, message, code)
		return
	}
	_ = xml.NewEncoder(w).Encode(resp)
}

func wantsJSON(r *http.Request) bool {
	return strings.ToLower(r.URL.Query().Get("f")) != "xml"
}
