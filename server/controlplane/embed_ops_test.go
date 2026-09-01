package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/processor"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestSemanticMultimodalFuse(t *testing.T) {
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

	// Fixture dimensions prove only the broker/component contract. Real BGE
	// readiness and inference remain covered by the provisioned integration
	// tests and are not inferred from this deterministic fixture.
	defaultFused := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "quarterly",
	}))
	if defaultFused.Status != command.StatusSucceeded {
		t.Fatalf("default broker with fixture semantic = %q: %+v", defaultFused.Status, defaultFused.Reasons)
	}
	var defaultData command.SearchQueryData
	if err := json.Unmarshal(defaultFused.Data, &defaultData); err != nil {
		t.Fatalf("decode default broker: %v", err)
	}
	if defaultData.Provider != search.ProviderBrokerFuse || defaultData.Dimension != "" ||
		len(defaultData.Components) != 2 ||
		defaultData.Components[0].Dimension != search.DimensionLexical ||
		defaultData.Components[0].Status != string(command.StatusSucceeded) ||
		defaultData.Components[1].Dimension != search.DimensionSemantic ||
		defaultData.Components[1].Status != string(command.StatusSucceeded) {
		t.Fatalf("default broker components = %+v", defaultData)
	}
	foundDefaultText := false
	for _, hit := range defaultData.Hits {
		if hit.SubjectRef == textRef {
			foundDefaultText = true
			if len(hit.Dimensions) == 0 {
				t.Fatalf("default fused hit missing provenance: %+v", hit)
			}
		}
	}
	if !foundDefaultText {
		t.Fatalf("default broker hits missing text subject: %+v", defaultData.Hits)
	}

	lexicalGeneration, err := store.LatestIndexGeneration(ctx, ingestData.WorkspaceID, search.DimensionLexical)
	if err != nil {
		t.Fatalf("latest lexical generation: %v", err)
	}
	if err := os.Remove(lexicalGeneration.DBPath); err != nil {
		t.Fatalf("remove lexical generation: %v", err)
	}
	lexicalMissing := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "quarterly experiment report",
	}))
	if lexicalMissing.Status != command.StatusDegraded || !hasReasonCode(lexicalMissing, ReasonCodeUnavailable) {
		t.Fatalf("default broker without lexical = %q: %+v", lexicalMissing.Status, lexicalMissing.Reasons)
	}
	var lexicalMissingData command.SearchQueryData
	if err := json.Unmarshal(lexicalMissing.Data, &lexicalMissingData); err != nil {
		t.Fatalf("decode default broker without lexical: %v", err)
	}
	if len(lexicalMissingData.Components) != 2 ||
		lexicalMissingData.Components[0].Status != string(command.StatusDegraded) ||
		lexicalMissingData.Components[1].Status != string(command.StatusSucceeded) ||
		len(lexicalMissingData.Hits) != 1 || lexicalMissingData.Hits[0].SubjectRef != textRef {
		t.Fatalf("default broker without lexical payload = %+v", lexicalMissingData)
	}
	if len(lexicalMissing.Reasons) != 1 || strings.Contains(strings.ToLower(lexicalMissing.Reasons[0].Message), "semantic search is unavailable") {
		t.Fatalf("default broker reported the wrong unavailable component: %+v", lexicalMissing.Reasons)
	}
	if _, err := dispatcher.search.Rebuild(ctx, ingestData.WorkspaceID, ingestData.SnapshotRef, ingestData.RootID); err != nil {
		t.Fatalf("rebuild search after lexical-loss check: %v", err)
	}

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

}
