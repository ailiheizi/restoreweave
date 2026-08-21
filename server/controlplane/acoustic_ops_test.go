package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/processor"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestAcousticFingerprintDimension(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	host := processor.NewHost(store, repo, processor.Options{
		StagingDir: filepath.Join(repo.Root(), "staging"),
		Processors: append(processor.DefaultProcessors(), processor.AudioFingerprint{}),
	})
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithFixtureDimensions(), WithExact(&exact.Service{
		Store:     store,
		Repo:      repo,
		Processor: host,
	}))

	root := t.TempDir()
	mp3 := testID3v23(map[string]string{
		"TIT2": "Nightfall",
		"TPE1": "Example Artist",
		"TALB": "Demo Album",
	})
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), mp3, 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}
	fingerprint := processor.FixtureFingerprint(mp3)

	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAudioList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"snapshot_ref": ingestData.SnapshotRef,
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
	subject := tracks.Tracks[0].SubjectRef

	found := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionAcoustic,
		"query":        map[string]any{"fingerprint": fingerprint},
	}))
	if found.Status != command.StatusSucceeded {
		t.Fatalf("acoustic search = %q: %+v", found.Status, found.Reasons)
	}
	var searchData command.SearchQueryData
	if err := json.Unmarshal(found.Data, &searchData); err != nil {
		t.Fatalf("decode acoustic search: %v", err)
	}
	if searchData.Dimension != search.DimensionAcoustic || searchData.Provider != search.ProviderAcousticFix {
		t.Fatalf("acoustic provenance = %+v", searchData)
	}
	if searchData.ScoreSemantics != search.ScoreAcousticExact || searchData.GenerationID == "" {
		t.Fatalf("acoustic score/generation = %+v", searchData)
	}
	if len(searchData.Hits) != 1 || searchData.Hits[0].SubjectRef != subject {
		t.Fatalf("acoustic hits = %+v want %s", searchData.Hits, subject)
	}

	lexical := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "Nightfall",
	}))
	if lexical.Status != command.StatusSucceeded {
		t.Fatalf("lexical search = %q: %+v", lexical.Status, lexical.Reasons)
	}

	generation, err := store.LatestIndexGeneration(ctx, ingestData.WorkspaceID, search.DimensionAcoustic)
	if err != nil {
		t.Fatalf("latest acoustic generation: %v", err)
	}
	if err := os.Remove(generation.DBPath); err != nil {
		t.Fatalf("delete acoustic index: %v", err)
	}
	degraded := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionAcoustic,
		"query":        fingerprint,
	}))
	if degraded.Status != command.StatusDegraded || !hasReasonCode(degraded, ReasonCodeUnavailable) {
		t.Fatalf("acoustic after index loss = %q reasons=%+v", degraded.Status, degraded.Reasons)
	}

	verified := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("verify after acoustic index loss = %q: %+v", verified.Status, verified.Reasons)
	}
	dest := filepath.Join(t.TempDir(), "out")
	mustAppliedRestore(t, ctx, dispatcher, ingestData.WorkspaceID, ingestData.SnapshotRef, dest)
	got, err := os.ReadFile(filepath.Join(dest, "song.mp3"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if string(got) != string(mp3) {
		t.Fatal("restored payload changed")
	}
}
