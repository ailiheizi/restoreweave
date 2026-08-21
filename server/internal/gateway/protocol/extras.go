package protocol

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"

	"github.com/ailiheizi/restoreweave/client/command"
)

const starredTag = "starred"

func (s *Server) serveSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		query = strings.TrimSpace(r.URL.Query().Get("q"))
	}
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	needle := strings.ToLower(query)
	artists := make([]map[string]any, 0)
	albums := make([]map[string]any, 0)
	songs := make([]map[string]any, 0)
	seenArtist := map[string]bool{}
	seenAlbum := map[string]bool{}
	for _, album := range data.Albums {
		if needle == "" || strings.Contains(strings.ToLower(album.Artist), needle) {
			id := artistID(album.Artist)
			if !seenArtist[id] {
				seenArtist[id] = true
				artists = append(artists, map[string]any{
					"id": id, "name": album.Artist, "albumCount": 1,
				})
			}
		}
		if needle == "" || strings.Contains(strings.ToLower(album.Title), needle) || strings.Contains(strings.ToLower(album.Artist), needle) {
			id := albumID(album.Artist, album.Title)
			if !seenAlbum[id] {
				seenAlbum[id] = true
				albums = append(albums, map[string]any{
					"id": id, "name": firstNonEmpty(album.Title, "Unknown Album"),
					"artist": album.Artist, "artistId": artistID(album.Artist),
					"songCount": len(album.SubjectRefs),
				})
			}
		}
	}
	for _, track := range data.Tracks {
		blob := strings.ToLower(track.Title + " " + track.Artist + " " + track.Album + " " + track.Name)
		if needle == "" || strings.Contains(blob, needle) {
			songs = append(songs, songPayload(track))
		}
	}
	if needle != "" {
		if result, err := s.call(r.Context(), command.OpSearchQuery, map[string]any{
			"workspace_id": s.opts.WorkspaceID,
			"query":        query,
		}); err == nil {
			var hits command.SearchQueryData
			if json.Unmarshal(result.Data, &hits) == nil {
				have := map[string]bool{}
				for _, song := range songs {
					have[song["id"].(string)] = true
				}
				byRef := map[string]command.AudioTrack{}
				for _, track := range data.Tracks {
					byRef[track.SubjectRef] = track
				}
				for _, hit := range hits.Hits {
					if have[hit.SubjectRef] {
						continue
					}
					if track, ok := byRef[hit.SubjectRef]; ok {
						songs = append(songs, songPayload(track))
					}
				}
			}
		}
	}
	key := "searchResult3"
	if strings.Contains(r.URL.Path, "search2") {
		key = "searchResult2"
	}
	writeSubsonicOK(w, r, map[string]any{
		key: map[string]any{"artist": artists, "album": albums, "song": songs},
	})
}

func (s *Server) serveCoverArt(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	sum := 0
	for _, c := range id {
		sum += int(c)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	fill := color.RGBA{R: byte(40 + sum%80), G: byte(60 + sum%100), B: byte(90 + sum%120), A: 255}
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, fill)
		}
	}
	w.Header().Set("Content-Type", "image/png")
	_ = png.Encode(w, img)
}

func (s *Server) serveStar(w http.ResponseWriter, r *http.Request, add bool) {
	id := firstNonEmpty(r.URL.Query().Get("id"), r.URL.Query().Get("albumId"), r.URL.Query().Get("artistId"))
	if id == "" {
		writeSubsonicError(w, r, 10, "id is required")
		return
	}
	catalog, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	targets := starSubjectRefs(catalog, id)
	if len(targets) == 0 {
		writeSubsonicError(w, r, 70, "star target not found")
		return
	}
	for _, subject := range targets {
		if add {
			if _, err := s.call(r.Context(), command.OpAnnotationUpsert, map[string]any{
				"workspace_id": s.opts.WorkspaceID,
				"subject_ref":  subject,
				"kind":         "TAG",
				"body":         starredTag,
			}); err != nil {
				writeSubsonicError(w, r, 0, err.Error())
				return
			}
			continue
		}
		if err := s.unstarSubject(r, subject); err != nil {
			writeSubsonicError(w, r, 0, err.Error())
			return
		}
	}
	writeSubsonicOK(w, r, nil)
}

func (s *Server) unstarSubject(r *http.Request, subject string) error {
	listed, err := s.call(r.Context(), command.OpAnnotationList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"subject_ref":  subject,
	})
	if err != nil {
		return err
	}
	var data command.AnnotationListData
	if err := json.Unmarshal(listed.Data, &data); err != nil {
		return err
	}
	for _, annotation := range data.Annotations {
		if annotation.Kind != "TAG" || annotation.Body != starredTag || annotation.Tombstoned {
			continue
		}
		if _, err := s.call(r.Context(), command.OpAnnotationDelete, map[string]any{
			"workspace_id":      s.opts.WorkspaceID,
			"annotation_id":     annotation.ID,
			"expected_revision": annotation.Revision,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) serveStarred(w http.ResponseWriter, r *http.Request) {
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	starred, err := s.starredTimes(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	songs := make([]map[string]any, 0)
	for _, track := range data.Tracks {
		if when := starred[track.SubjectRef]; when != "" {
			songs = append(songs, songPayloadWithStar(track, when))
		}
	}
	albums := make([]map[string]any, 0)
	artists := make([]map[string]any, 0)
	seenArtist := map[string]bool{}
	for _, album := range data.Albums {
		if when := collectionStarred(data, starred, album.SubjectRefs); when != "" {
			albums = append(albums, map[string]any{
				"id": albumID(album.Artist, album.Title), "name": firstNonEmpty(album.Title, "Unknown Album"),
				"artist": album.Artist, "artistId": artistID(album.Artist), "starred": when,
			})
		}
		id := artistID(album.Artist)
		if seenArtist[id] {
			continue
		}
		seenArtist[id] = true
		if when := artistStarred(data, starred, id); when != "" {
			artists = append(artists, map[string]any{
				"id": id, "name": album.Artist, "albumCount": 1, "starred": when,
			})
		}
	}
	key := "starred"
	if strings.Contains(r.URL.Path, "getStarred2") {
		key = "starred2"
	}
	writeSubsonicOK(w, r, map[string]any{key: map[string]any{"song": songs, "album": albums, "artist": artists}})
}

func (s *Server) serveGetBookmarks(w http.ResponseWriter, r *http.Request) {
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	listed, err := s.call(r.Context(), command.OpAnnotationList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
	})
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	var annotations command.AnnotationListData
	if err := json.Unmarshal(listed.Data, &annotations); err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	byRef := map[string]command.AudioTrack{}
	for _, track := range data.Tracks {
		byRef[track.SubjectRef] = track
	}
	bookmarks := make([]map[string]any, 0)
	for _, annotation := range annotations.Annotations {
		if annotation.Kind != "PROGRESS" || annotation.Tombstoned {
			continue
		}
		track, ok := byRef[annotation.SubjectRef]
		if !ok {
			continue
		}
		var body struct {
			PositionMS int64 `json:"position_ms"`
		}
		_ = json.Unmarshal([]byte(annotation.Body), &body)
		bookmarks = append(bookmarks, map[string]any{
			"position": body.PositionMS,
			"comment":  "restoreweave",
			"created":  annotation.CreatedAt,
			"changed":  annotation.UpdatedAt,
			"entry":    songPayload(track),
		})
	}
	writeSubsonicOK(w, r, map[string]any{"bookmarks": map[string]any{"bookmark": bookmarks}})
}

func (s *Server) serveCreateBookmark(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeSubsonicError(w, r, 10, "id is required")
		return
	}
	position, _ := strconv.ParseInt(r.URL.Query().Get("position"), 10, 64)
	body, err := json.Marshal(map[string]any{
		"position_ms": position,
		"completed":   false,
		"source":      "opensubsonic",
		"comment":     r.URL.Query().Get("comment"),
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

func (s *Server) serveDeleteBookmark(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	listed, err := s.call(r.Context(), command.OpAnnotationList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
		"subject_ref":  id,
	})
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	var data command.AnnotationListData
	if err := json.Unmarshal(listed.Data, &data); err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	for _, annotation := range data.Annotations {
		if annotation.Kind == "PROGRESS" && !annotation.Tombstoned {
			if _, err := s.call(r.Context(), command.OpAnnotationDelete, map[string]any{
				"workspace_id":      s.opts.WorkspaceID,
				"annotation_id":     annotation.ID,
				"expected_revision": annotation.Revision,
			}); err != nil {
				writeSubsonicError(w, r, 0, err.Error())
				return
			}
		}
	}
	writeSubsonicOK(w, r, nil)
}

func (s *Server) serveRandomSongs(w http.ResponseWriter, r *http.Request) {
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	size := 10
	if raw := r.URL.Query().Get("size"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			size = parsed
		}
	}
	songs := make([]map[string]any, 0, len(data.Tracks))
	for _, track := range data.Tracks {
		songs = append(songs, songPayload(track))
		if len(songs) >= size {
			break
		}
	}
	writeSubsonicOK(w, r, map[string]any{"randomSongs": map[string]any{"song": songs}})
}

func (s *Server) serveSimilar(w http.ResponseWriter, r *http.Request) {
	data, err := s.audioCatalog(r)
	if err != nil {
		writeSubsonicError(w, r, 0, err.Error())
		return
	}
	id := r.URL.Query().Get("id")
	artist := ""
	for _, track := range data.Tracks {
		if track.SubjectRef == id {
			artist = track.Artist
			break
		}
	}
	if artist == "" {
		for _, album := range data.Albums {
			if artistID(album.Artist) == id || albumID(album.Artist, album.Title) == id {
				artist = album.Artist
				break
			}
		}
	}
	songs := make([]map[string]any, 0)
	for _, track := range data.Tracks {
		if artist != "" && track.Artist == artist {
			songs = append(songs, songPayload(track))
		}
	}
	key := "similarSongs"
	if strings.Contains(r.URL.Path, "getSimilarSongs2") {
		key = "similarSongs2"
	}
	writeSubsonicOK(w, r, map[string]any{key: map[string]any{"song": songs}})
}

func (s *Server) serveHandshake(w http.ResponseWriter, r *http.Request, name string) {
	switch name {
	case "getUser":
		writeSubsonicOK(w, r, map[string]any{
			"user": map[string]any{
				"username":            firstNonEmpty(r.URL.Query().Get("u"), "local"),
				"scrobblingEnabled":   true,
				"adminRole":           false,
				"settingsRole":        false,
				"downloadRole":        true,
				"uploadRole":          false,
				"playlistRole":        false,
				"coverArtRole":        false,
				"commentRole":         false,
				"podcastRole":         false,
				"streamRole":          true,
				"jukeboxRole":         false,
				"shareRole":           false,
				"videoConversionRole": false,
				"folder":              []any{1},
			},
		})
	case "getScanStatus":
		count := 0
		if data, err := s.audioCatalog(r); err == nil {
			count = len(data.Tracks)
		}
		writeSubsonicOK(w, r, map[string]any{
			"scanStatus": map[string]any{"scanning": false, "count": count},
		})
	case "getOpenSubsonicExtensions":
		writeSubsonicOK(w, r, map[string]any{
			"openSubsonicExtensions": []any{},
		})
	case "getPlaylists":
		writeSubsonicOK(w, r, map[string]any{"playlists": map[string]any{"playlist": []any{}}})
	case "getPlaylist":
		writeSubsonicError(w, r, 70, "playlists are not a RestoreWeave collection")
	case "createPlaylist", "updatePlaylist", "deletePlaylist":
		writeSubsonicError(w, r, 0, "playlists are not stored; use RestoreWeave collections later")
	case "getGenres":
		writeSubsonicOK(w, r, map[string]any{"genres": map[string]any{"genre": []any{}}})
	case "getNowPlaying":
		writeSubsonicOK(w, r, map[string]any{"nowPlaying": map[string]any{"entry": []any{}}})
	case "getInternetRadioStations":
		writeSubsonicOK(w, r, map[string]any{"internetRadioStations": map[string]any{"internetRadioStation": []any{}}})
	case "getPodcasts":
		writeSubsonicOK(w, r, map[string]any{"podcasts": map[string]any{"channel": []any{}}})
	case "getShares":
		writeSubsonicOK(w, r, map[string]any{"shares": map[string]any{"share": []any{}}})
	case "getVideos":
		writeSubsonicOK(w, r, map[string]any{"videos": map[string]any{"video": []any{}}})
	case "getLyrics", "getLyricsBySongId":
		writeSubsonicOK(w, r, map[string]any{"lyrics": map[string]any{"artist": "", "title": "", "value": ""}})
	case "getAvatar":
		s.serveCoverArt(w, r)
	case "startScan":
		count := 0
		if data, err := s.audioCatalog(r); err == nil {
			count = len(data.Tracks)
		}
		writeSubsonicOK(w, r, map[string]any{"scanStatus": map[string]any{"scanning": false, "count": count}})
	case "getAlbumInfo", "getAlbumInfo2", "getArtistInfo", "getArtistInfo2":
		writeSubsonicOK(w, r, map[string]any{
			"albumInfo":  map[string]any{"notes": "", "musicBrainzId": ""},
			"artistInfo": map[string]any{"biography": "", "musicBrainzId": "", "similarArtist": []any{}},
		})
	case "getPlayQueue":
		writeSubsonicOK(w, r, map[string]any{"playQueue": map[string]any{"entry": []any{}}})
	case "savePlayQueue", "setRating":
		writeSubsonicOK(w, r, nil)
	default:
		writeSubsonicError(w, r, 0, "unimplemented OpenSubsonic method")
	}
}

func (s *Server) starredTimes(r *http.Request) (map[string]string, error) {
	listed, err := s.call(r.Context(), command.OpAnnotationList, map[string]any{
		"workspace_id": s.opts.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	var annotations command.AnnotationListData
	if err := json.Unmarshal(listed.Data, &annotations); err != nil {
		return nil, err
	}
	starred := map[string]string{}
	for _, annotation := range annotations.Annotations {
		if annotation.Kind != "TAG" || annotation.Body != starredTag || annotation.Tombstoned {
			continue
		}
		when := firstNonEmpty(annotation.UpdatedAt, annotation.CreatedAt)
		if when == "" {
			when = "1970-01-01T00:00:00Z"
		}
		starred[annotation.SubjectRef] = when
	}
	return starred, nil
}

func starSubjectRefs(data command.AudioListData, id string) []string {
	if strings.HasPrefix(id, "ar_") {
		seen := map[string]bool{}
		refs := make([]string, 0)
		for _, track := range data.Tracks {
			if artistID(track.Artist) != id || seen[track.SubjectRef] {
				continue
			}
			seen[track.SubjectRef] = true
			refs = append(refs, track.SubjectRef)
		}
		return refs
	}
	if strings.HasPrefix(id, "al_") {
		for _, album := range data.Albums {
			if albumID(album.Artist, album.Title) == id {
				return append([]string(nil), album.SubjectRefs...)
			}
		}
		return nil
	}
	for _, track := range data.Tracks {
		if track.SubjectRef == id {
			return []string{id}
		}
	}
	return nil
}

func artistStarred(data command.AudioListData, starred map[string]string, artistIDValue string) string {
	refs := make([]string, 0)
	for _, track := range data.Tracks {
		if artistID(track.Artist) == artistIDValue {
			refs = append(refs, track.SubjectRef)
		}
	}
	return collectionStarred(data, starred, refs)
}

func collectionStarred(_ command.AudioListData, starred map[string]string, refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	when := ""
	for _, ref := range refs {
		ts, ok := starred[ref]
		if !ok {
			return ""
		}
		if ts > when {
			when = ts
		}
	}
	return when
}

func markStarred(payload map[string]any, when string) map[string]any {
	if when != "" {
		payload["starred"] = when
	}
	return payload
}

func songPayloadWithStar(track command.AudioTrack, when string) map[string]any {
	return markStarred(songPayload(track), when)
}
