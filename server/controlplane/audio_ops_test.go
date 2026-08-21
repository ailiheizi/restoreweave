package controlplane

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestAudioListAfterIngest(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), testID3v23(map[string]string{
		"TIT2": "Nightfall",
		"TPE1": "Example Artist",
		"TALB": "Demo Album",
		"TRCK": "2/10",
	}), 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "late.mp3"), testID3v23(map[string]string{
		"TIT2": "Harbor",
		"TPE1": "Example Artist",
		"TALB": "Demo Album",
		"TRCK": "3/10",
	}), 0o644); err != nil {
		t.Fatalf("write late mp3: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not audio"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAudioList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("audio.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var data command.AudioListData
	if err := json.Unmarshal(listed.Data, &data); err != nil {
		t.Fatalf("decode audio list: %v", err)
	}
	if len(data.Tracks) != 2 {
		t.Fatalf("tracks = %+v, want 2", data.Tracks)
	}
	if data.Tracks[0].Title != "Nightfall" || data.Tracks[0].Track != 2 || data.Tracks[0].Name != "song.mp3" {
		t.Fatalf("first track = %+v", data.Tracks[0])
	}
	if data.Tracks[1].Title != "Harbor" || data.Tracks[1].Track != 3 {
		t.Fatalf("second track = %+v", data.Tracks[1])
	}
	if len(data.Albums) != 1 || data.Albums[0].Title != "Demo Album" || len(data.Albums[0].SubjectRefs) != 2 {
		t.Fatalf("albums = %+v", data.Albums)
	}

	found := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "Nightfall",
	}))
	if found.Status != command.StatusSucceeded {
		t.Fatalf("search Nightfall = %q: %+v", found.Status, found.Reasons)
	}
	var searchData command.SearchQueryData
	if err := json.Unmarshal(found.Data, &searchData); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(searchData.Hits) != 1 || searchData.Hits[0].SubjectRef != data.Tracks[0].SubjectRef {
		t.Fatalf("search hits = %+v, want %s", searchData.Hits, data.Tracks[0].SubjectRef)
	}
}

func TestGroupAudioAlbumsSortsEmptyLast(t *testing.T) {
	tracks := []command.AudioTrack{
		{SubjectRef: "b", Artist: "Other", Title: "Solo", Album: "", Track: 1},
		{SubjectRef: "a2", Artist: "Example Artist", Title: "Harbor", Album: "Demo Album", Track: 3},
		{SubjectRef: "a1", Artist: "Example Artist", Title: "Nightfall", Album: "Demo Album", Track: 2},
	}
	sortAudioTracks(tracks)
	if tracks[0].Title != "Nightfall" || tracks[1].Title != "Harbor" || tracks[2].Title != "Solo" {
		t.Fatalf("sorted = %+v", tracks)
	}
	albums := groupAudioAlbums(tracks)
	if len(albums) != 2 || albums[0].Title != "Demo Album" || len(albums[0].SubjectRefs) != 2 {
		t.Fatalf("albums = %+v", albums)
	}
	if albums[1].Artist != "Other" || albums[1].Title != "" || albums[1].SubjectRefs[0] != "b" {
		t.Fatalf("untitled album = %+v", albums[1])
	}
}

func TestAudioSubjectRangeRead(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))

	payload := testID3v23(map[string]string{
		"TIT2": "Nightfall",
		"TPE1": "Example Artist",
		"TALB": "Demo Album",
	})
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), payload, 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}
	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAudioList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("audio.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var tracks command.AudioListData
	if err := json.Unmarshal(listed.Data, &tracks); err != nil {
		t.Fatalf("decode audio list: %v", err)
	}
	if len(tracks.Tracks) != 1 {
		t.Fatalf("tracks = %+v", tracks.Tracks)
	}

	opened := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentOpen, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"entry_id":     tracks.Tracks[0].SubjectRef,
	}))
	if opened.Status != command.StatusSucceeded {
		t.Fatalf("content.open = %q: %+v", opened.Status, opened.Reasons)
	}
	var openData command.ContentOpenData
	if err := json.Unmarshal(opened.Data, &openData); err != nil {
		t.Fatalf("decode open: %v", err)
	}
	if openData.LogicalSize != int64(len(payload)) {
		t.Fatalf("logical size = %d, want %d", openData.LogicalSize, len(payload))
	}

	header := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentRead, map[string]any{
		"handle": openData.Handle,
		"offset": 0,
		"length": 3,
	}))
	if header.Status != command.StatusSucceeded {
		t.Fatalf("read header = %q: %+v", header.Status, header.Reasons)
	}
	var headerData command.ContentReadData
	if err := json.Unmarshal(header.Data, &headerData); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if string(headerData.Bytes) != "ID3" || headerData.EOF {
		t.Fatalf("header = %q eof=%v", headerData.Bytes, headerData.EOF)
	}

	midOff := int64(10)
	midLen := int64(8)
	mid := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentRead, map[string]any{
		"handle": openData.Handle,
		"offset": midOff,
		"length": midLen,
	}))
	if mid.Status != command.StatusSucceeded {
		t.Fatalf("read mid = %q: %+v", mid.Status, mid.Reasons)
	}
	var midData command.ContentReadData
	if err := json.Unmarshal(mid.Data, &midData); err != nil {
		t.Fatalf("decode mid: %v", err)
	}
	want := payload[midOff : midOff+midLen]
	if string(midData.Bytes) != string(want) {
		t.Fatalf("mid bytes = %q, want %q", midData.Bytes, want)
	}

	tailOff := int64(len(payload) - 2)
	tail := dispatcher.Handle(ctx, mustEnvelope(t, command.OpContentRead, map[string]any{
		"handle": openData.Handle,
		"offset": tailOff,
		"length": 16,
	}))
	if tail.Status != command.StatusSucceeded {
		t.Fatalf("read tail = %q: %+v", tail.Status, tail.Reasons)
	}
	var tailData command.ContentReadData
	if err := json.Unmarshal(tail.Data, &tailData); err != nil {
		t.Fatalf("decode tail: %v", err)
	}
	if string(tailData.Bytes) != string(payload[tailOff:]) || !tailData.EOF {
		t.Fatalf("tail = %q eof=%v", tailData.Bytes, tailData.EOF)
	}
}

func TestAudioListRequiresWorkspace(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))
	result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpAudioList, map[string]any{}))
	if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeInvalidInput) {
		t.Fatalf("audio.list missing workspace = %q reasons=%+v", result.Status, result.Reasons)
	}
}

func testID3v23(frames map[string]string) []byte {
	var body []byte
	for _, id := range []string{"TIT2", "TPE1", "TALB", "TRCK", "TYER"} {
		value, ok := frames[id]
		if !ok {
			continue
		}
		data := append([]byte{3}, []byte(value)...)
		frame := []byte(id)
		frame = binary.BigEndian.AppendUint32(frame, uint32(len(data)))
		frame = append(frame, 0, 0)
		frame = append(frame, data...)
		body = append(body, frame...)
	}
	n := len(body)
	header := []byte{
		'I', 'D', '3', 3, 0, 0,
		byte((n >> 21) & 0x7f), byte((n >> 14) & 0x7f), byte((n >> 7) & 0x7f), byte(n & 0x7f),
	}
	return append(header, body...)
}
