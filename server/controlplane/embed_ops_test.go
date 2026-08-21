package controlplane

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/gateway/protocol"
	"github.com/ailiheizi/restoreweave/server/internal/processor"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/testutil"
	"net/http/httptest"
)

func TestSemanticMultimodalFuseAndInboxIdentify(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	host := processor.NewHost(store, repo, processor.Options{
		StagingDir: filepath.Join(repo.Root(), "staging"),
		Processors: append(processor.DefaultProcessors(),
			processor.AudioFingerprint{}, processor.TextEmbedding{}, processor.ClipEmbedding{}),
	})
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithFixtureDimensions(), WithExact(&exact.Service{
		Store:     store,
		Repo:      repo,
		Processor: host,
	}))

	root := t.TempDir()
	text := []byte("quarterly experiment report")
	mp3 := testID3v23(map[string]string{"TIT2": "Nightfall", "TPE1": "Example Artist"})
	if err := os.WriteFile(filepath.Join(root, "note.txt"), text, 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), mp3, 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}

	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	semantic := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionSemantic,
		"query":        "quarterly experiment report",
	}))
	if semantic.Status != command.StatusSucceeded {
		t.Fatalf("semantic search = %q: %+v", semantic.Status, semantic.Reasons)
	}
	var semanticData command.SearchQueryData
	if err := json.Unmarshal(semantic.Data, &semanticData); err != nil {
		t.Fatalf("decode semantic: %v", err)
	}
	if semanticData.Dimension != search.DimensionSemantic || semanticData.Provider != search.ProviderSemanticFix || len(semanticData.Hits) != 1 {
		t.Fatalf("semantic payload = %+v", semanticData)
	}
	textRef := semanticData.Hits[0].SubjectRef

	clipQuery := processor.ClipQueryText("Nightfall", "Example Artist")
	multimodal := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionMultimodal,
		"query":        clipQuery,
	}))
	if multimodal.Status != command.StatusSucceeded {
		t.Fatalf("multimodal search = %q: %+v", multimodal.Status, multimodal.Reasons)
	}
	var clipData command.SearchQueryData
	if err := json.Unmarshal(multimodal.Data, &clipData); err != nil {
		t.Fatalf("decode clip: %v", err)
	}
	if clipData.Dimension != search.DimensionMultimodal || len(clipData.Hits) != 1 {
		t.Fatalf("clip payload = %+v", clipData)
	}

	fused := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"fuse":         []string{search.DimensionLexical, search.DimensionSemantic},
		"query":        "quarterly",
	}))
	if fused.Status != command.StatusSucceeded {
		t.Fatalf("fuse = %q: %+v", fused.Status, fused.Reasons)
	}
	var fuseData command.SearchQueryData
	if err := json.Unmarshal(fused.Data, &fuseData); err != nil {
		t.Fatalf("decode fuse: %v", err)
	}
	if fuseData.Provider != search.ProviderBrokerFuse || fuseData.ScoreSemantics != search.ScoreComponentUnion {
		t.Fatalf("fuse provenance = %+v", fuseData)
	}
	if fuseData.Dimension != "" {
		t.Fatalf("fuse invented a dimension id: %q", fuseData.Dimension)
	}
	if len(fuseData.Components) != 2 || fuseData.Components[0].Dimension != search.DimensionLexical {
		t.Fatalf("fuse components = %+v", fuseData.Components)
	}
	foundText := false
	for _, hit := range fuseData.Hits {
		if hit.SubjectRef == textRef {
			foundText = true
			if len(hit.Dimensions) == 0 {
				t.Fatalf("fused hit missing component dimensions: %+v", hit)
			}
		}
	}
	if !foundText {
		t.Fatalf("fuse hits missing text subject: %+v", fuseData.Hits)
	}

	both := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionLexical,
		"fuse":         []string{search.DimensionLexical, search.DimensionSemantic},
		"query":        "quarterly",
	}))
	if both.Status != command.StatusFailed || !hasReasonCode(both, ReasonCodeInvalidInput) {
		t.Fatalf("fuse+dimension = %q: %+v", both.Status, both.Reasons)
	}

	generation, err := store.LatestIndexGeneration(ctx, ingestData.WorkspaceID, search.DimensionSemantic)
	if err != nil {
		t.Fatalf("latest semantic generation: %v", err)
	}
	if err := os.Remove(generation.DBPath); err != nil {
		t.Fatalf("delete semantic index: %v", err)
	}
	degraded := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"dimension":    search.DimensionSemantic,
		"query":        "quarterly experiment report",
	}))
	if degraded.Status != command.StatusDegraded || !hasReasonCode(degraded, ReasonCodeUnavailable) {
		t.Fatalf("semantic after index loss = %q: %+v", degraded.Status, degraded.Reasons)
	}

	partial := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"fuse":         []string{search.DimensionLexical, search.DimensionSemantic},
		"query":        "quarterly",
	}))
	if partial.Status != command.StatusSucceeded {
		t.Fatalf("partial fuse = %q: %+v", partial.Status, partial.Reasons)
	}
	var partialData command.SearchQueryData
	if err := json.Unmarshal(partial.Data, &partialData); err != nil {
		t.Fatalf("decode partial fuse: %v", err)
	}
	if len(partialData.Components) != 2 || partialData.Components[1].Status != string(command.StatusDegraded) {
		t.Fatalf("partial components = %+v", partialData.Components)
	}

	verified := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSnapshotVerify, map[string]any{
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if verified.Status != command.StatusSucceeded {
		t.Fatalf("verify after semantic index loss = %q: %+v", verified.Status, verified.Reasons)
	}

	facade, err := protocol.New(dispatcher.Handle, protocol.Options{
		WorkspaceID: ingestData.WorkspaceID,
		SnapshotRef: ingestData.SnapshotRef,
		Token:       "facade-token",
		Listen:      "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("new facade: %v", err)
	}
	server := httptest.NewServer(facade.Handler())
	defer server.Close()

	fingerprint := processor.FixtureFingerprint(mp3)
	inbox := getPlainJSON(t, server.URL+"/inbox/api/search?token=facade-token&dimension="+search.DimensionAcoustic+"&q="+url.QueryEscape(fingerprint))
	hits, _ := inbox["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("inbox acoustic identify = %#v", inbox)
	}
	identify := getJSON(t, server.URL+"/rest/identify.view?f=json&p=facade-token&query="+fingerprint)
	if statusOf(identify) != "failed" {
		t.Fatalf("invented identify.view succeeded: %#v", identify)
	}
	search3 := getJSON(t, server.URL+"/rest/search3.view?f=json&p=facade-token&query=Nightfall")
	if statusOf(search3) != "ok" {
		t.Fatalf("search3 = %#v", search3)
	}
}
