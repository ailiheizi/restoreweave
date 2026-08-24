package exact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

// TestMigrateProfilePreservesSignedRecoveryReaderClosure proves that a
// copy-forward profile migration keeps the source repository identity bound
// into signed portable records and remains readable without SQLite.
func TestMigrateProfilePreservesSignedRecoveryReaderClosure(t *testing.T) {
	ctx := context.Background()
	fixture := newSignedPublicationFixture(t, "migrated-reader.txt", []byte("migration reader exact bytes"))
	result := fixture.ingest(t, "sha256:migrated-reader-plan")
	anchor := loadRelocatedAnchor(t, fixture.service)
	// Verify the source through the same catalog-free reader contract before
	// copy-forward. Migration must preserve an independently readable rollback
	// copy, not merely produce a target that happens to open.
	sourceReader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, fixture.repo.Root(), anchor)
	if listed, err := sourceReader.ListSnapshots(ctx); err != nil || len(listed) != 1 || listed[0].SnapshotRef != result.SnapshotRef {
		t.Fatalf("source reader snapshots = %+v, err=%v", listed, err)
	}
	if _, err := sourceReader.Verify(ctx, result.SnapshotRef); err != nil {
		t.Fatalf("source reader verify: %v", err)
	}
	sourceDestination := filepath.Join(t.TempDir(), "source-restored")
	sourcePlan, err := sourceReader.InspectRestore(ctx, result.SnapshotRef, sourceDestination)
	if err != nil {
		t.Fatalf("source reader restore plan: %v", err)
	}
	if _, err := sourceReader.ApplyRestorePlan(ctx, sourcePlan); err != nil {
		t.Fatalf("source reader restore: %v", err)
	}
	targetRoot := filepath.Join(t.TempDir(), "zstd-target")
	report, err := repository.MigrateProfile(ctx, repository.RepositoryProfileDirectoryCASDev, fixture.repo.Root(), repository.RepositoryProfileLocalZstdV1, targetRoot)
	if err != nil {
		t.Fatalf("migrate signed repository: %v", err)
	}
	if report.SnapshotFiles != 1 {
		t.Fatalf("migration snapshot files = %d, want 1", report.SnapshotFiles)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	reader := relocatedReader(t, repository.RepositoryProfileLocalZstdV1, targetRoot, anchor)
	listed, err := reader.ListSnapshots(ctx)
	if err != nil || len(listed) != 1 || listed[0].SnapshotRef != result.SnapshotRef {
		t.Fatalf("migrated reader snapshots = %+v, err=%v", listed, err)
	}
	if _, err := reader.Verify(ctx, result.SnapshotRef); err != nil {
		t.Fatalf("migrated reader verify: %v", err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	plan, err := reader.InspectRestore(ctx, result.SnapshotRef, destination)
	if err != nil {
		t.Fatalf("migrated reader restore plan: %v", err)
	}
	if _, err := reader.ApplyRestorePlan(ctx, plan); err != nil {
		t.Fatalf("migrated reader restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "migrated-reader.txt")); err != nil {
		t.Fatalf("migrated reader output: %v", err)
	}
}

// TestMigrateProfileKeepsBothReadersIndependentAfterReopen proves that a
// successful copy-forward does not transfer recovery authority away from the
// source. Both clean readers can be reopened after the catalog is closed;
// corruption in the target is rejected without affecting the rollback copy.
func TestMigrateProfileKeepsBothReadersIndependentAfterReopen(t *testing.T) {
	ctx := context.Background()
	payload := []byte("migration readers remain independently recoverable")
	fixture := newSignedPublicationFixture(t, "dual-reader.txt", payload)
	result := fixture.ingest(t, "sha256:dual-reader-plan")
	anchor := loadRelocatedAnchor(t, fixture.service)
	targetRoot := filepath.Join(t.TempDir(), "zstd-target")
	if _, err := repository.MigrateProfile(ctx, repository.RepositoryProfileDirectoryCASDev, fixture.repo.Root(), repository.RepositoryProfileLocalZstdV1, targetRoot); err != nil {
		t.Fatalf("migrate signed repository: %v", err)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}

	sourceReader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, fixture.repo.Root(), anchor)
	targetReader := relocatedReader(t, repository.RepositoryProfileLocalZstdV1, targetRoot, anchor)
	for name, reader := range map[string]*Service{"source": sourceReader, "target": targetReader} {
		if _, err := reader.Verify(ctx, result.SnapshotRef); err != nil {
			t.Fatalf("%s reader verify after reopen: %v", name, err)
		}
		destination := filepath.Join(t.TempDir(), name+"-restored")
		plan, err := reader.InspectRestore(ctx, result.SnapshotRef, destination)
		if err != nil {
			t.Fatalf("%s reader restore plan after reopen: %v", name, err)
		}
		if _, err := reader.ApplyRestorePlan(ctx, plan); err != nil {
			t.Fatalf("%s reader restore after reopen: %v", name, err)
		}
		got, err := os.ReadFile(filepath.Join(destination, "dual-reader.txt"))
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("%s reader restored bytes = %d, err=%v", name, len(got), err)
		}
	}

	digest := sha256.Sum256(payload)
	contentID := "sha256:" + hex.EncodeToString(digest[:])
	parts := strings.SplitN(contentID, ":", 2)
	if len(parts) != 2 || len(parts[1]) < 2 {
		t.Fatalf("content id = %q", contentID)
	}
	targetBlob := filepath.Join(targetRoot, "blobs", parts[0], parts[1][:2], parts[1])
	blob, err := os.ReadFile(targetBlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(blob) == 0 {
		t.Fatal("target blob is empty")
	}
	blob[len(blob)/2] ^= 0xff
	if err := os.WriteFile(targetBlob, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := targetReader.Verify(ctx, result.SnapshotRef); err == nil {
		t.Fatal("tampered target reader verified")
	}
	if _, err := sourceReader.Verify(ctx, result.SnapshotRef); err != nil {
		t.Fatalf("source rollback reader after target tamper: %v", err)
	}
	destination := filepath.Join(t.TempDir(), "source-after-tamper")
	plan, err := sourceReader.InspectRestore(ctx, result.SnapshotRef, destination)
	if err != nil {
		t.Fatalf("source restore plan after target tamper: %v", err)
	}
	if _, err := sourceReader.ApplyRestorePlan(ctx, plan); err != nil {
		t.Fatalf("source restore after target tamper: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "dual-reader.txt")); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("source rollback bytes = %q, err=%v", got, err)
	}
}
