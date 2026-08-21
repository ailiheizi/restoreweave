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

func TestFixtureFingerprintIsNotContentIdentity(t *testing.T) {
	body := buildID3v23(map[string]string{"TIT2": "Nightfall", "TPE1": "Example Artist"})
	fp := FixtureFingerprint(body)
	sum := sha256.Sum256(body)
	content := "sha256:" + hex.EncodeToString(sum[:])
	if fp == "" || !strings.HasPrefix(fp, "fix1:") {
		t.Fatalf("fixture fingerprint = %q", fp)
	}
	if fp == content || strings.Contains(fp, hex.EncodeToString(sum[:])) {
		t.Fatalf("fixture fingerprint reused SHA-256: %s", fp)
	}
	if FixtureFingerprint(body) != fp {
		t.Fatal("fixture fingerprint is not deterministic")
	}
}

func TestAudioFingerprintAdmittedWhenOptedIn(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	id3 := buildID3v23(map[string]string{"TIT2": "Nightfall", "TPE1": "Example Artist"})
	if err := os.WriteFile(filepath.Join(source, "song.mp3"), id3, 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}
	store, repo := testLane(t)
	host := NewHost(store, repo, Options{
		StagingDir: t.TempDir(),
		Processors: append(DefaultProcessors(), AudioFingerprint{}),
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
	var tags, prints int
	for _, artifact := range artifacts {
		switch artifact.CapabilityID {
		case CapabilityAudioTags:
			tags++
		case CapabilityAudioFingerprint:
			prints++
			var record acousticFingerprint
			if err := json.Unmarshal([]byte(artifact.Body), &record); err != nil {
				t.Fatalf("decode fingerprint: %v", err)
			}
			if !record.NotContentIdentity || record.Fingerprint != FixtureFingerprint(id3) {
				t.Fatalf("fingerprint artifact = %+v", record)
			}
			if artifact.Stage != "FINGERPRINT" {
				t.Fatalf("stage = %s", artifact.Stage)
			}
		}
	}
	if tags != 1 || prints != 1 {
		t.Fatalf("artifacts tags=%d fingerprints=%d total=%d: %+v", tags, prints, len(artifacts), artifacts)
	}
}

func TestFingerprintPanicDoesNotBlockExactOrTags(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	id3 := buildID3v23(map[string]string{"TIT2": "Nightfall"})
	if err := os.WriteFile(filepath.Join(source, "song.mp3"), id3, 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}
	store, repo := testLane(t)
	host := NewHost(store, repo, Options{
		StagingDir: t.TempDir(),
		Processors: []Processor{AudioTags{}, panicFingerprint{}},
	})
	service := &exact.Service{Store: store, Repo: repo, Processor: host}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ingested.Warnings) != 1 || !strings.Contains(ingested.Warnings[0], CapabilityAudioFingerprint) {
		t.Fatalf("fingerprint panic warnings = %+v", ingested.Warnings)
	}
	if _, err := service.Verify(ctx, ingested.SnapshotRef); err != nil {
		t.Fatalf("verify: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "out")
	if _, err := service.Restore(ctx, ingested.SnapshotRef, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "song.mp3"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if string(got) != string(id3) {
		t.Fatal("restored bytes changed")
	}
	artifacts, err := store.ListAdmittedArtifacts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].CapabilityID != CapabilityAudioTags {
		t.Fatalf("artifacts after fingerprint panic = %+v", artifacts)
	}
}

type panicFingerprint struct{}

func (panicFingerprint) CapabilityID() string { return CapabilityAudioFingerprint }
func (panicFingerprint) Stage() Stage         { return StageFingerprint }
func (panicFingerprint) RunStage(context.Context, Invocation) (StageResult, error) {
	panic("fingerprint exploded")
}
