package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestGraphRelationDimension(t *testing.T) {
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
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("quarterly experiment report"), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}
	mp3 := testID3v23(map[string]string{"TIT2": "Nightfall", "TPE1": "Example Artist", "TALB": "Demo Album"})
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), mp3, 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}

	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
	}))
	var listData command.NamespaceListData
	if err := json.Unmarshal(listed.Data, &listData); err != nil {
		t.Fatalf("decode namespace: %v", err)
	}
	var docsID, songID string
	for _, entry := range listData.Entries {
		switch entry.DisplayName {
		case "docs":
			docsID = entry.ID
		case "song.mp3":
			songID = entry.ID
		}
	}
	children := dispatcher.Handle(ctx, mustEnvelope(t, command.OpNamespaceList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"root_id":      ingestData.RootID,
		"parent_id":    docsID,
	}))
	var childrenData command.NamespaceListData
	if err := json.Unmarshal(children.Data, &childrenData); err != nil {
		t.Fatalf("decode children: %v", err)
	}
	var noteID string
	for _, entry := range childrenData.Entries {
		if entry.DisplayName == "note.txt" {
			noteID = entry.ID
		}
	}
	if docsID == "" || songID == "" || noteID == "" {
		t.Fatalf("missing subjects docs=%s song=%s note=%s", docsID, songID, noteID)
	}

	tagged := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationUpsert, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"subject_ref":  noteID,
		"kind":         "TAG",
		"body":         "reviewed",
	}))
	if tagged.Status != command.StatusSucceeded {
		t.Fatalf("tag = %q: %+v", tagged.Status, tagged.Reasons)
	}

	artist := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionGraph,
		"query":        map[string]any{"relation": "artist", "value": "Example Artist"},
	}))
	if artist.Status != command.StatusSucceeded {
		t.Fatalf("artist graph = %q: %+v", artist.Status, artist.Reasons)
	}
	var artistData command.SearchQueryData
	if err := json.Unmarshal(artist.Data, &artistData); err != nil {
		t.Fatalf("decode artist: %v", err)
	}
	if artistData.Dimension != search.DimensionGraph || artistData.Provider != search.ProviderGraphCatalog || artistData.ScoreSemantics != search.ScoreGraphExact {
		t.Fatalf("graph provenance = %+v", artistData)
	}
	if len(artistData.Hits) != 1 || artistData.Hits[0].SubjectRef != songID {
		t.Fatalf("artist hits = %+v want %s", artistData.Hits, songID)
	}
	audio := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAudioList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if audio.Status != command.StatusSucceeded {
		t.Fatalf("audio.list = %q: %+v", audio.Status, audio.Reasons)
	}
	var audioData command.AudioListData
	if err := json.Unmarshal(audio.Data, &audioData); err != nil {
		t.Fatalf("decode audio: %v", err)
	}
	if len(audioData.Tracks) != 1 || audioData.Tracks[0].SubjectRef != songID {
		t.Fatalf("audio.list subject = %+v want %s", audioData.Tracks, songID)
	}

	tags := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionGraph,
		"query":        "tagged:reviewed",
	}))
	if tags.Status != command.StatusSucceeded {
		t.Fatalf("tagged graph = %q: %+v", tags.Status, tags.Reasons)
	}
	var tagData command.SearchQueryData
	if err := json.Unmarshal(tags.Data, &tagData); err != nil {
		t.Fatalf("decode tagged: %v", err)
	}
	if len(tagData.Hits) != 1 || tagData.Hits[0].SubjectRef != noteID {
		t.Fatalf("tagged hits = %+v want %s", tagData.Hits, noteID)
	}

	contains := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionGraph,
		"query":        "contains:" + docsID,
	}))
	if contains.Status != command.StatusSucceeded {
		t.Fatalf("contains graph = %q: %+v", contains.Status, contains.Reasons)
	}
	var containsData command.SearchQueryData
	if err := json.Unmarshal(contains.Data, &containsData); err != nil {
		t.Fatalf("decode contains: %v", err)
	}
	if len(containsData.Hits) != 1 || containsData.Hits[0].SubjectRef != noteID {
		t.Fatalf("contains hits = %+v want %s", containsData.Hits, noteID)
	}

	unknown := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionGraph,
		"query":        "lyrics:foo",
	}))
	if unknown.Status != command.StatusFailed || !hasReasonCode(unknown, ReasonCodeInvalidInput) {
		t.Fatalf("unknown relation = %q: %+v", unknown.Status, unknown.Reasons)
	}

	generation, err := store.LatestIndexGeneration(ctx, ingestData.WorkspaceID, search.DimensionGraph)
	if err != nil {
		t.Fatalf("latest graph generation: %v", err)
	}
	if err := os.Remove(generation.DBPath); err != nil {
		t.Fatalf("delete graph index: %v", err)
	}
	degraded := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionGraph,
		"query":        "artist:Example Artist",
	}))
	if degraded.Status != command.StatusDegraded || !hasReasonCode(degraded, ReasonCodeUnavailable) {
		t.Fatalf("graph after index loss = %q: %+v", degraded.Status, degraded.Reasons)
	}
	verified := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("verify after graph index loss = %q: %+v", verified.Status, verified.Reasons)
	}
}
