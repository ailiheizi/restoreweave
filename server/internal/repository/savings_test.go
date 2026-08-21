package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestMeasureSavingsSeparatesMechanisms places identical files (whole-file
// deduplication) and compressible files (compression) into a fresh zstd
// repository and asserts that the report keeps every layer separate and never
// double-counts. This is the executable exit test for the Phase 5 savings
// measurement slice.
func TestMeasureSavingsSeparatesMechanisms(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenZstdDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}

	// Two distinct compressible payloads: repeated bytes compress strongly and
	// keep physical length well below logical length.
	payloadA := bytes.Repeat([]byte("restoreweave savings measurement a "), 2048)
	payloadB := bytes.Repeat([]byte("restoreweave savings measurement b "), 2048)

	// Place A twice (second placement is a duplicate) and B once. The second
	// placement of A must reuse the stored object without adding physical
	// bytes, so its logical bytes are preserved but its physical bytes are not.
	first, err := repo.Place(ctx, bytes.NewReader(payloadA))
	if err != nil {
		t.Fatal(err)
	}
	if first.Existed {
		t.Fatalf("first placement of payload A unexpectedly reused an object: %+v", first)
	}
	second, err := repo.PlaceExact(ctx, first.ContentID, bytes.NewReader(payloadA))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Existed {
		t.Fatalf("second placement of payload A did not reuse the stored object: %+v", second)
	}
	if second.StoredBytes != first.StoredBytes {
		t.Fatalf("duplicate placement changed stored bytes: first=%d second=%d", first.StoredBytes, second.StoredBytes)
	}
	third, err := repo.Place(ctx, bytes.NewReader(payloadB))
	if err != nil {
		t.Fatal(err)
	}
	if third.Existed || third.ContentID == first.ContentID {
		t.Fatalf("distinct payload B should create a new object: %+v", third)
	}

	placements := []Receipt{first, second, third}
	report, err := MeasureSavings(ctx, repo, placements)
	if err != nil {
		t.Fatal(err)
	}

	// Every placement's logical bytes are preserved, including the duplicate.
	wantLogical := first.Bytes + second.Bytes + third.Bytes
	if report.LogicalBytes != wantLogical {
		t.Fatalf("LogicalBytes = %d, want %d", report.LogicalBytes, wantLogical)
	}

	// Exactly one duplicate placement: the second placement of payload A.
	if report.DuplicateFiles != 1 {
		t.Fatalf("DuplicateFiles = %d, want 1", report.DuplicateFiles)
	}
	if report.DuplicateBytes != first.Bytes {
		t.Fatalf("DuplicateBytes = %d, want %d (one whole-file duplicate of A)", report.DuplicateBytes, first.Bytes)
	}

	// Physical bytes are measured from the repository, and the zstd profile
	// must be smaller than the logical bytes on compressible content.
	if report.PhysicalStoredBytes >= report.LogicalBytes {
		t.Fatalf("zstd physical %d is not below logical %d on compressible content", report.PhysicalStoredBytes, report.LogicalBytes)
	}
	if report.CompressionSavedBytes <= 0 {
		t.Fatalf("CompressionSavedBytes = %d, want > 0 on compressible content", report.CompressionSavedBytes)
	}
	// Mechanism separation identity: distinct logical bytes (logical minus
	// duplicate) minus physical bytes equals the compression-only savings.
	if wantCompression := report.LogicalBytes - report.DuplicateBytes - report.PhysicalStoredBytes; report.CompressionSavedBytes != wantCompression {
		t.Fatalf("CompressionSavedBytes = %d, want distinct-logical-minus-physical = %d", report.CompressionSavedBytes, wantCompression)
	}

	// Net physical savings is the derived number: logical source bytes minus
	// the full measured footprint (physical objects plus repository overhead).
	// It must satisfy both identities, and because deduplication also
	// contributes, it is larger than the compression-only savings here.
	if wantNet := report.LogicalBytes - report.PhysicalStoredBytes - report.OverheadBytes; report.NetPhysicalSavingsBytes != wantNet {
		t.Fatalf("NetPhysicalSavingsBytes = %d, want %d", report.NetPhysicalSavingsBytes, wantNet)
	}
	if wantNet := report.CompressionSavedBytes + report.DuplicateBytes - report.OverheadBytes; report.NetPhysicalSavingsBytes != wantNet {
		t.Fatalf("NetPhysicalSavingsBytes = %d, want compression+duplicate-overhead = %d", report.NetPhysicalSavingsBytes, wantNet)
	}
	if report.NetPhysicalSavingsBytes <= report.CompressionSavedBytes {
		t.Fatalf("NetPhysicalSavingsBytes = %d, want > compression-only %d", report.NetPhysicalSavingsBytes, report.CompressionSavedBytes)
	}
	if report.OverheadBytes <= 0 {
		t.Fatalf("OverheadBytes = %d, want measurable repository metadata", report.OverheadBytes)
	}

	// Both layers must be reported separately, never merged.
	if !containsMechanism(report.Mechanisms, SavingsMechanismWholeFileDedup) {
		t.Fatalf("Mechanisms = %v, want whole-file-deduplication listed", report.Mechanisms)
	}
	if !containsMechanism(report.Mechanisms, SavingsMechanismCompression) {
		t.Fatalf("Mechanisms = %v, want compression listed", report.Mechanisms)
	}
}

// TestMeasureSavingsRawProfilePhysicalEqualsLogical asserts that the raw
// development profile reports no compression savings and that duplicate
// savings remain the only mechanism.
func TestMeasureSavingsRawProfilePhysicalEqualsLogical(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	payloadA := bytes.Repeat([]byte("raw profile duplicate "), 512)
	payloadB := bytes.Repeat([]byte("raw profile distinct "), 256)

	first, err := repo.Place(ctx, bytes.NewReader(payloadA))
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.PlaceExact(ctx, first.ContentID, bytes.NewReader(payloadA))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Existed {
		t.Fatalf("raw duplicate placement did not reuse the object: %+v", second)
	}
	third, err := repo.Place(ctx, bytes.NewReader(payloadB))
	if err != nil {
		t.Fatal(err)
	}

	report, err := MeasureSavings(ctx, repo, []Receipt{first, second, third})
	if err != nil {
		t.Fatal(err)
	}

	// Raw profile stores each distinct object exactly once with no compression.
	wantLogical := first.Bytes + second.Bytes + third.Bytes
	wantPhysical := first.Bytes + third.Bytes
	if report.LogicalBytes != wantLogical {
		t.Fatalf("LogicalBytes = %d, want %d", report.LogicalBytes, wantLogical)
	}
	if report.PhysicalStoredBytes != wantPhysical {
		t.Fatalf("PhysicalStoredBytes = %d, want %d", report.PhysicalStoredBytes, wantPhysical)
	}
	if report.DuplicateFiles != 1 || report.DuplicateBytes != first.Bytes {
		t.Fatalf("duplicate accounting = files %d bytes %d, want 1 / %d", report.DuplicateFiles, report.DuplicateBytes, first.Bytes)
	}
	if report.CompressionSavedBytes != 0 {
		t.Fatalf("raw profile CompressionSavedBytes = %d, want 0", report.CompressionSavedBytes)
	}
	if len(report.Mechanisms) != 1 || report.Mechanisms[0] != SavingsMechanismWholeFileDedup {
		t.Fatalf("raw profile Mechanisms = %v, want exactly [whole-file-deduplication]", report.Mechanisms)
	}
}

// TestMeasureSavingsDuplicateDoesNotDoubleLogicalSavings places the same
// payload repeatedly and asserts the duplicate layer does not report more than
// the logical bytes that were actually saved.
func TestMeasureSavingsDuplicateDoesNotDoubleLogicalSavings(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("single content id "), 100)
	var placements []Receipt
	var first Receipt
	for i := 0; i < 5; i++ {
		receipt, err := repo.Place(ctx, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = receipt
		}
		placements = append(placements, receipt)
	}
	report, err := MeasureSavings(ctx, repo, placements)
	if err != nil {
		t.Fatal(err)
	}
	if report.LogicalBytes != 5*first.Bytes {
		t.Fatalf("LogicalBytes = %d, want %d", report.LogicalBytes, 5*first.Bytes)
	}
	if report.DuplicateBytes != 4*first.Bytes {
		t.Fatalf("DuplicateBytes = %d, want %d (four reused placements)", report.DuplicateBytes, 4*first.Bytes)
	}
	if report.DuplicateFiles != 4 {
		t.Fatalf("DuplicateFiles = %d, want 4", report.DuplicateFiles)
	}
	if report.PhysicalStoredBytes != first.Bytes {
		t.Fatalf("PhysicalStoredBytes = %d, want %d (one stored object)", report.PhysicalStoredBytes, first.Bytes)
	}
	if report.CompressionSavedBytes != 0 {
		t.Fatalf("raw profile CompressionSavedBytes = %d, want 0", report.CompressionSavedBytes)
	}
}

// TestMeasureSavingsFailsClosedOnCorruptRepository corrupts a stored object and
// asserts the measurement returns an error instead of a fabricated report.
func TestMeasureSavingsFailsClosedOnCorruptRepository(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("bytes that must be corruptible")
	receipt, err := repo.Place(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	path := blobPath(repo.Root(), receipt.ContentID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("stored object is empty")
	}
	data[0] ^= 0xff
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := MeasureSavings(ctx, repo, []Receipt{receipt})
	if err == nil {
		t.Fatalf("measurement succeeded on a corrupt repository: %+v", report)
	}
}

// TestMeasureSavingsFailsClosedOnAbsentRepository asserts that measuring a
// non-existent repository path returns an explicit error rather than zeros.
func TestMeasureSavingsFailsClosedOnAbsentRepository(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	missing := repo.Root() + "-does-not-exist"
	dir := &Dir{root: missing}
	report, err := MeasureSavings(ctx, dir, nil)
	if err == nil {
		t.Fatalf("measurement succeeded on an absent repository: %+v", report)
	}
}

// TestMeasureSavingsRefusesUntrackedPlacements asserts that a non-empty
// repository with no placement receipts cannot honestly report logical or
// duplicate savings and fails closed instead.
func TestMeasureSavingsRefusesUntrackedPlacements(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("content placed outside the tracked ledger "), 256)
	if _, err := repo.Place(ctx, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := MeasureSavings(ctx, repo, nil); err == nil {
		t.Fatal("measurement succeeded without placement receipts")
	}
}

// TestMeasureSavingsMemoryProfileFailsClosed asserts that drivers outside the
// in-tree directory profiles cannot be measured and fail closed.
func TestMeasureSavingsMemoryProfileFailsClosed(t *testing.T) {
	ctx := context.Background()
	receipt, err := NewMemory().Place(ctx, bytes.NewBufferString("memory only"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := MeasureSavings(ctx, NewMemory(), []Receipt{receipt})
	if err == nil {
		t.Fatalf("measurement succeeded on the memory profile: %+v", report)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected not-found wrapping for memory profile: %v", err)
	}
}

func containsMechanism(list []SavingsMechanism, want SavingsMechanism) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}
