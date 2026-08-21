package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/exact"
)

func TestFixtureEmbeddingIsNotContentIdentity(t *testing.T) {
	text := "quarterly experiment report"
	token := FixtureEmbedding("sem1", text)
	sum := sha256.Sum256([]byte(text))
	if !strings.HasPrefix(token, "sem1:") || strings.Contains(token, hex.EncodeToString(sum[:])) {
		t.Fatalf("token = %q", token)
	}
	if FixtureEmbedding("sem1", "Quarterly  Experiment   Report") != token {
		t.Fatal("normalized text should match")
	}
}

func TestTextAndClipEmbeddingsAdmitWhenOptedIn(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	text := []byte("quarterly experiment report")
	id3 := buildID3v23(map[string]string{"TIT2": "Nightfall", "TPE1": "Example Artist"})
	if err := os.WriteFile(filepath.Join(source, "note.txt"), text, 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "song.mp3"), id3, 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}
	store, repo := testLane(t)
	host := NewHost(store, repo, Options{
		StagingDir: t.TempDir(),
		Processors: append(DefaultProcessors(), TextEmbedding{}, ClipEmbedding{}),
	})
	service := &exact.Service{Store: store, Repo: repo, Processor: host}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if _, err := service.Verify(ctx, ingested.SnapshotRef); err != nil {
		t.Fatalf("verify: %v", err)
	}
	artifacts, err := store.ListAdmittedArtifacts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var textEmbeds, clipEmbeds int
	for _, artifact := range artifacts {
		switch artifact.CapabilityID {
		case CapabilityTextEmbedding:
			textEmbeds++
			var record featureEmbedding
			if err := json.Unmarshal([]byte(artifact.Body), &record); err != nil {
				t.Fatalf("decode text embed: %v", err)
			}
			if !record.NotContentIdentity || record.Token != FixtureEmbedding("sem1", string(text)) {
				t.Fatalf("text embed = %+v", record)
			}
		case CapabilityClipEmbedding:
			clipEmbeds++
			var record featureEmbedding
			if err := json.Unmarshal([]byte(artifact.Body), &record); err != nil {
				t.Fatalf("decode clip embed: %v", err)
			}
			want := FixtureEmbedding("clip1", ClipQueryText("Nightfall", "Example Artist"))
			if !record.NotContentIdentity || record.Token != want {
				t.Fatalf("clip embed = %+v want %s", record, want)
			}
		}
	}
	if textEmbeds != 1 || clipEmbeds != 1 {
		t.Fatalf("embeds text=%d clip=%d artifacts=%+v", textEmbeds, clipEmbeds, artifacts)
	}
}
