package controlplane

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/processor"
)

type audioListInput struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
}

func (d *Dispatcher) handleAudioList(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input audioListInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	artifacts, err := d.store.ListAdmittedArtifacts(ctx, input.WorkspaceID, input.SnapshotRef)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	tracks := make([]command.AudioTrack, 0)
	for _, artifact := range artifacts {
		if artifact.CapabilityID != processor.CapabilityAudioTags {
			continue
		}
		var parsed struct {
			Title      string `json:"title"`
			Artist     string `json:"artist"`
			Album      string `json:"album"`
			Track      int    `json:"track"`
			Year       string `json:"year"`
			DurationMS int64  `json:"duration_ms"`
		}
		_ = json.Unmarshal([]byte(artifact.Body), &parsed)
		name := ""
		if entry, err := d.store.GetNamespaceEntry(ctx, input.WorkspaceID, artifact.SubjectRef); err == nil {
			name = entry.DisplayName
		}
		tracks = append(tracks, command.AudioTrack{
			SubjectRef: artifact.SubjectRef,
			Name:       name,
			Title:      parsed.Title,
			Artist:     parsed.Artist,
			Album:      parsed.Album,
			Track:      parsed.Track,
			Year:       parsed.Year,
			DurationMS: parsed.DurationMS,
			ArtifactID: artifact.ID,
		})
	}
	sortAudioTracks(tracks)
	return succeeded(env, started, command.AudioListData{
		WorkspaceID: input.WorkspaceID,
		SnapshotRef: input.SnapshotRef,
		Albums:      groupAudioAlbums(tracks),
		Tracks:      tracks,
	})
}

func sortAudioTracks(tracks []command.AudioTrack) {
	sort.Slice(tracks, func(i, j int) bool {
		a, b := tracks[i], tracks[j]
		if a.Album != b.Album {
			return catalogLabelLess(a.Album, b.Album)
		}
		if a.Artist != b.Artist {
			return catalogLabelLess(a.Artist, b.Artist)
		}
		if a.Track != b.Track {
			return a.Track < b.Track
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.SubjectRef < b.SubjectRef
	})
}

func groupAudioAlbums(tracks []command.AudioTrack) []command.AudioAlbum {
	type key struct{ artist, album string }
	order := make([]key, 0)
	byKey := make(map[key]*command.AudioAlbum)
	for _, track := range tracks {
		k := key{track.Artist, track.Album}
		album, ok := byKey[k]
		if !ok {
			album = &command.AudioAlbum{
				Artist:      track.Artist,
				Title:       track.Album,
				Year:        track.Year,
				SubjectRefs: make([]string, 0, 1),
			}
			byKey[k] = album
			order = append(order, k)
		}
		if album.Year == "" {
			album.Year = track.Year
		}
		album.DurationMS += track.DurationMS
		album.SubjectRefs = append(album.SubjectRefs, track.SubjectRef)
	}
	albums := make([]command.AudioAlbum, 0, len(order))
	for _, k := range order {
		albums = append(albums, *byKey[k])
	}
	sort.Slice(albums, func(i, j int) bool {
		a, b := albums[i], albums[j]
		if a.Artist != b.Artist {
			return catalogLabelLess(a.Artist, b.Artist)
		}
		return catalogLabelLess(a.Title, b.Title)
	})
	return albums
}

func catalogLabelLess(a, b string) bool {
	if a == b {
		return false
	}
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	return a < b
}
