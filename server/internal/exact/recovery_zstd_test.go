package exact

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestSignedRecoveryRoundTripWithLocalZstdRepository(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := bytes.Repeat([]byte("signed local zstd recovery\n"), 4096)
	if err := os.WriteFile(filepath.Join(source, "archive.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}

	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := repository.OpenZstdDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	identity, anchor, err := OpenSigningMaterial(t.TempDir(), testPublicationDomain, true)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	writer := &Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := writer.InspectIngest(ctx, source, IngestOptions{})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	result, err := writer.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:zstd-signed-plan")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if result.PreparedClosureDigest == "" || result.PublicationCommitDigest == "" {
		_ = store.Close()
		t.Fatalf("signed publication evidence = %+v", result)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}

	reader := &Service{
		Repo: repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true,
	}
	listed, err := reader.ListSnapshots(ctx)
	if err != nil || len(listed) != 1 || listed[0].SnapshotRef != result.SnapshotRef {
		t.Fatalf("catalog-free zstd discovery = %+v, %v", listed, err)
	}
	if _, err := reader.Verify(ctx, result.SnapshotRef); err != nil {
		t.Fatalf("catalog-free zstd verify: %v", err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	restorePlan, err := reader.InspectRestore(ctx, result.SnapshotRef, destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ApplyRestorePlan(ctx, restorePlan); err != nil {
		t.Fatalf("catalog-free zstd restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "archive.txt"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("restored zstd payload length = %d, err = %v", len(got), err)
	}
}
