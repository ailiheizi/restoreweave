package exact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/scanner"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type payloadPlacementResponseLossRepo struct {
	*repository.Dir
	lost bool
}

type payloadPlacementFailureRepo struct{ *repository.Dir }

type invalidSavingsReceiptRepo struct{ *repository.Dir }

type contradictorySavingsReceiptRepo struct{ *repository.Dir }

func TestIngestPersistsDetectionFactsAndExcludesReadState(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	// The .zip suffix intentionally conflicts with the PDF magic bytes. Both
	// evidence lines must remain searchable as durable detector facts.
	if err := os.WriteFile(filepath.Join(source, "report.zip"), []byte("%PDF-1.7\nbody"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "document.pdf"), []byte("%PDF-1.7\nbody"), 0o600); err != nil {
		t.Fatalf("write PDF source: %v", err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	indexer := &search.Indexer{Store: store, Engine: &search.Engine{Dir: filepath.Join(t.TempDir(), "index")}}
	result, err := (&Service{Store: store, Repo: repo, Indexer: indexer}).Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	entries, err := store.ListNamespaceSubtree(ctx, result.WorkspaceID, result.RootID, "")
	if err != nil {
		t.Fatalf("list namespace: %v", err)
	}
	var file sqlite.NamespaceEntry
	for _, node := range entries {
		if node.Entry.DisplayName == "report.zip" {
			file = node.Entry
			break
		}
	}
	if file.ID == "" || file.ObservationID == "" {
		t.Fatalf("ingested file entry = %+v", file)
	}
	evidence, err := store.ListDetectionEvidence(ctx, result.WorkspaceID, file.ObservationID)
	if err != nil || len(evidence) < 2 {
		t.Fatalf("detection evidence = %+v, err=%v", evidence, err)
	}
	kinds := map[string]bool{}
	suffixRaw, magicRaw := false, false
	for _, row := range evidence {
		kinds[row.EvidenceKind] = true
		if row.DetectorID == "" || row.DetectorDigest == "" {
			t.Fatalf("incomplete durable detection row = %+v", row)
		}
		suffixRaw = suffixRaw || strings.Contains(string(row.Evidence), "suffix:.zip")
		magicRaw = magicRaw || strings.Contains(string(row.Evidence), "magic:pdf-header")
	}
	if !kinds["SUFFIX"] || !kinds["MAGIC"] || !suffixRaw || !magicRaw {
		t.Fatalf("suffix/magic evidence kinds = %+v", kinds)
	}
	coverage, err := indexer.Coverage(ctx, result.WorkspaceID)
	if err != nil || !coverage.Fields[search.AxisDetection] {
		t.Fatalf("detection coverage = %+v, err=%v", coverage, err)
	}
	if _, hits, err := indexer.Query(ctx, search.QueryRequest{WorkspaceID: result.WorkspaceID, Text: "application/pdf", Axes: []string{search.AxisDetection}}); err != nil || len(hits) != 1 {
		t.Fatalf("MIME detection query hits=%d err=%v", len(hits), err)
	}
	if _, hits, err := indexer.Query(ctx, search.QueryRequest{WorkspaceID: result.WorkspaceID, Text: "suffix:zip", Axes: []string{search.AxisDetection}}); err != nil || len(hits) != 1 {
		t.Fatalf("suffix detection query hits=%d err=%v", len(hits), err)
	}
	if _, hits, err := indexer.Query(ctx, search.QueryRequest{WorkspaceID: result.WorkspaceID, Text: "COMPLETE", Axes: []string{search.AxisDetection}}); err != nil {
		t.Fatalf("read-state query: %v", err)
	} else if len(hits) != 0 {
		t.Fatalf("read state leaked into detection axis: %+v", hits)
	}
}

func (r *invalidSavingsReceiptRepo) PlaceExact(ctx context.Context, contentID string, body io.Reader) (repository.Receipt, error) {
	receipt, err := r.Dir.PlaceExact(ctx, contentID, body)
	if err == nil {
		receipt.StoredBytes = -1
	}
	return receipt, err
}

func (r *contradictorySavingsReceiptRepo) PlaceExact(ctx context.Context, contentID string, body io.Reader) (repository.Receipt, error) {
	receipt, err := r.Dir.PlaceExact(ctx, contentID, body)
	if err == nil && receipt.Existed {
		receipt.Existed = false
	}
	return receipt, err
}

// dishonestPayloadReadbackRepo accepts the expected object but exposes
// different bytes through the reader used by the exact lane. The publication
// helper must hash that stream instead of trusting Verify alone.
type dishonestPayloadReadbackRepo struct {
	*repository.Dir
	contentID string
	payload   []byte
}

func (r *dishonestPayloadReadbackRepo) Open(ctx context.Context, contentID string) (io.ReadCloser, error) {
	if contentID == r.contentID {
		return io.NopCloser(bytes.NewReader(append([]byte(nil), r.payload...))), nil
	}
	return r.Dir.Open(ctx, contentID)
}

func (r *payloadPlacementFailureRepo) PlaceExact(context.Context, string, io.Reader) (repository.Receipt, error) {
	return repository.Receipt{}, errors.New("payload placement unavailable before commit")
}

func (r *payloadPlacementResponseLossRepo) PlaceExact(ctx context.Context, contentID string, body io.Reader) (repository.Receipt, error) {
	receipt, err := r.Dir.PlaceExact(ctx, contentID, body)
	if err == nil && !r.lost {
		r.lost = true
		return repository.Receipt{}, errors.New("response lost after exact payload placement")
	}
	return receipt, err
}

func TestIngestAdoptsExactPayloadAfterPlacementResponseLoss(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("payload placement response loss")
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	baseRepo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	repo := &payloadPlacementResponseLossRepo{Dir: baseRepo}
	service := &Service{Store: store, Repo: repo}

	result, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest after placement response loss: %v", err)
	}
	if result.LocalFiles != 1 || result.NewBytes != 0 {
		t.Fatalf("placement recovery accounting = %+v", result)
	}
	if result.SavingsMeasured || result.NewPhysicalBytes != 0 || result.CompressionSavedBytes != 0 {
		t.Fatalf("placement recovery fabricated savings = %+v", result)
	}
	digest := sha256.Sum256(payload)
	contentID := "sha256:" + hex.EncodeToString(digest[:])
	if err := repo.Verify(ctx, contentID); err != nil {
		t.Fatalf("recovered payload readback: %v", err)
	}
}

func TestIngestReportsReceiptBoundSavingsForRawAndZstd(t *testing.T) {
	ctx := context.Background()
	payload := bytes.Repeat([]byte("restoreweave savings "), 256)
	for _, tc := range []struct {
		name string
		open func(string) (repository.Driver, error)
	}{
		{name: "raw", open: func(root string) (repository.Driver, error) { return repository.OpenDir(root) }},
		{name: "zstd", open: func(root string) (repository.Driver, error) { return repository.OpenZstdDir(root) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := t.TempDir()
			if err := os.WriteFile(filepath.Join(source, "a.bin"), payload, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "b.bin"), payload, 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			repo, err := tc.open(filepath.Join(t.TempDir(), "repository"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := (&Service{Store: store, Repo: repo}).Ingest(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			if !result.SavingsMeasured || result.LocalBytes != int64(len(payload))*2 || result.NewBytes != int64(len(payload)) || result.NewPhysicalBytes <= 0 {
				t.Fatalf("savings result = %+v", result)
			}
			if result.CompressionSavedBytes < 0 || (tc.name == "zstd" && result.CompressionSavedBytes <= 0) || (tc.name == "raw" && result.CompressionSavedBytes != 0) {
				t.Fatalf("compression savings = %d", result.CompressionSavedBytes)
			}
		})
	}
}

func TestIngestReportsMeasuredZeroSavingsForEmptyAndUnavailableForInvalidReceipt(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "empty.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "empty-repository"))
	if err != nil {
		t.Fatal(err)
	}
	empty, err := (&Service{Store: store, Repo: repo}).Ingest(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !empty.SavingsMeasured || empty.NewPhysicalBytes != 0 || empty.CompressionSavedBytes != 0 {
		t.Fatalf("empty savings result = %+v", empty)
	}

	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), []byte("invalid receipt"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidStore, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "invalid.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = invalidStore.Close() })
	base, err := repository.OpenDir(filepath.Join(t.TempDir(), "invalid-repository"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&Service{Store: invalidStore, Repo: &invalidSavingsReceiptRepo{Dir: base}}).Ingest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if result.SavingsMeasured || result.NewPhysicalBytes != 0 || result.CompressionSavedBytes != 0 || len(result.Warnings) == 0 {
		t.Fatalf("invalid receipt savings result = %+v", result)
	}
}

func TestIngestRescanRetainsStableSubjectForProtectionAndExactIdentity(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("stable subject protection")
	if err := os.WriteFile(filepath.Join(source, "stable.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: store, Repo: repo}

	first, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	firstEntries, err := store.ListNamespaceContent(ctx, first.WorkspaceID, first.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstEntries) != 1 || firstEntries[0].EntryType != sqlite.EntryFile {
		t.Fatalf("first namespace content = %+v", firstEntries)
	}
	firstEntry := firstEntries[0]
	if firstEntry.SubjectRef == "" || firstEntry.SubjectRef == firstEntry.ID {
		t.Fatalf("first entry did not receive a stable subject: %+v", firstEntry)
	}
	firstProtection, err := store.GetProtectionRecordBySubject(ctx, first.WorkspaceID, firstEntry.SubjectRef)
	if err != nil {
		t.Fatalf("first protection: %v", err)
	}
	if firstProtection.SubjectRef != firstEntry.SubjectRef || firstProtection.ExpectedContentID != firstEntry.ContentID {
		t.Fatalf("first protection = %+v, entry = %+v", firstProtection, firstEntry)
	}

	second, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.WorkspaceID != first.WorkspaceID || second.SourceID != first.SourceID {
		t.Fatalf("rescan changed catalog source identity: first=%+v second=%+v", first, second)
	}
	secondEntries, err := store.ListNamespaceContent(ctx, second.WorkspaceID, second.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondEntries) != 1 || secondEntries[0].EntryType != sqlite.EntryFile {
		t.Fatalf("second namespace content = %+v", secondEntries)
	}
	secondEntry := secondEntries[0]
	if secondEntry.ID == firstEntry.ID {
		t.Fatalf("rescan reused snapshot-local namespace entry ID %q", secondEntry.ID)
	}
	if secondEntry.SubjectRef != firstEntry.SubjectRef {
		t.Fatalf("rescan changed stable subject: first=%q second=%q", firstEntry.SubjectRef, secondEntry.SubjectRef)
	}
	if secondEntry.ContentID != firstEntry.ContentID || secondEntry.ContentID == "" {
		t.Fatalf("rescan changed exact identity: first=%q second=%q", firstEntry.ContentID, secondEntry.ContentID)
	}
	if second.NewBytes != 0 {
		t.Fatalf("rescan did not preserve whole-file deduplication: %+v", second)
	}
	secondProtection, err := store.GetProtectionRecordBySubject(ctx, second.WorkspaceID, secondEntry.SubjectRef)
	if err != nil {
		t.Fatalf("second protection: %v", err)
	}
	if secondProtection.SubjectRef != firstEntry.SubjectRef || secondProtection.ID != firstProtection.ID {
		t.Fatalf("protection subject continuity lost: first=%+v second=%+v", firstProtection, secondProtection)
	}
	if secondProtection.Revision <= firstProtection.Revision {
		t.Fatalf("rescan did not revise current protection projection: first=%d second=%d", firstProtection.Revision, secondProtection.Revision)
	}
	references, err := store.ListRecoveryReferencesBySubject(ctx, second.WorkspaceID, secondEntry.SubjectRef)
	if err != nil || len(references) != 2 {
		t.Fatalf("stable recovery references = %+v, err=%v", references, err)
	}
	for _, reference := range references {
		if reference.SubjectRef != secondEntry.SubjectRef || reference.ProtectionRecordID != secondProtection.ID {
			t.Fatalf("recovery reference lost stable protection binding: %+v", reference)
		}
	}
	if _, err := store.GetNamespaceEntry(ctx, first.WorkspaceID, firstEntry.ID); err != nil {
		t.Fatalf("legacy namespace observation no longer readable: %v", err)
	}
}

func TestIngestSameSHAPathsRemainDistinctSubjects(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("same bytes, different provenance")
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(source, name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&Service{Store: store, Repo: repo}).Ingest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListNamespaceContent(ctx, result.WorkspaceID, result.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("namespace content = %+v", entries)
	}
	if entries[0].ContentID == "" || entries[0].ContentID != entries[1].ContentID {
		t.Fatalf("same-SHA content identities = %q / %q", entries[0].ContentID, entries[1].ContentID)
	}
	if entries[0].SubjectRef == "" || entries[0].SubjectRef == entries[1].SubjectRef {
		t.Fatalf("different paths were merged into one subject: %+v", entries)
	}
	if result.NewBytes != int64(len(payload)) {
		t.Fatalf("whole-file deduplication accounting = %+v", result)
	}
	for _, entry := range entries {
		protection, err := store.GetProtectionRecordBySubject(ctx, result.WorkspaceID, entry.SubjectRef)
		if err != nil {
			t.Fatalf("protection for %q: %v", entry.DisplayName, err)
		}
		if protection.SubjectRef != entry.SubjectRef || protection.ExpectedContentID != entry.ContentID {
			t.Fatalf("protection for %q = %+v", entry.DisplayName, protection)
		}
	}
}

func TestIngestRejectsContradictoryPreverifiedReceiptSavings(t *testing.T) {
	ctx := context.Background()
	payload := []byte("receipt disagrees with pre-verification")
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	base, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	contentID := "sha256:" + hex.EncodeToString(digest[:])
	if _, err := base.PlaceExact(ctx, contentID, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	result, err := (&Service{Store: store, Repo: &contradictorySavingsReceiptRepo{Dir: base}}).Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest with contradictory receipt: %v", err)
	}
	if result.SavingsMeasured || result.NewPhysicalBytes != 0 || result.CompressionSavedBytes != 0 {
		t.Fatalf("contradictory receipt fabricated savings = %+v", result)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("contradictory receipt did not explain unavailable savings: %+v", result)
	}
	if err := base.Verify(ctx, contentID); err != nil {
		t.Fatalf("exact payload was not preserved: %v", err)
	}
}

func TestIngestPayloadPlacementWithoutReadbackIsTypedUnknown(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("payload placement never committed")
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	baseRepo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	repo := &payloadPlacementFailureRepo{Dir: baseRepo}
	service := &Service{Store: store, Repo: repo}

	_, err = service.Ingest(ctx, source)
	if !errors.Is(err, ErrUnknownExternalOutcome) || !errors.Is(err, ErrNeedsReconciliation) {
		t.Fatalf("payload placement error = %v, want typed unknown reconciliation outcome", err)
	}
	var outcome *PayloadPlacementOutcomeError
	if !errors.As(err, &outcome) || outcome.ContentID == "" {
		t.Fatalf("payload placement outcome = %#v, err=%v", outcome, err)
	}
	if verifyErr := repo.Verify(ctx, outcome.ContentID); !errors.Is(verifyErr, repository.ErrNotFound) {
		t.Fatalf("unexpected payload readback = %v", verifyErr)
	}
}

func TestIngestRejectsDishonestPayloadReadbackBeforePublication(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("payload bytes that were placed")
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	contentID := "sha256:" + hex.EncodeToString(digest[:])
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	baseRepo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	wrong := append([]byte(nil), payload...)
	wrong[0] ^= 0xff
	repo := &dishonestPayloadReadbackRepo{Dir: baseRepo, contentID: contentID, payload: wrong}
	service := &Service{Store: store, Repo: repo}

	_, err = service.Ingest(ctx, source)
	if !errors.Is(err, ErrUnknownExternalOutcome) || !errors.Is(err, ErrNeedsReconciliation) {
		t.Fatalf("dishonest payload readback error = %v, want typed unknown reconciliation outcome", err)
	}
	var outcome *PayloadPlacementOutcomeError
	if !errors.As(err, &outcome) || outcome.ContentID != contentID {
		t.Fatalf("dishonest payload outcome = %#v, err=%v", outcome, err)
	}
}

func TestIngestPlacesAndRestoresAfterCatalogLoss(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte{0x00, 0x01, 0xde, 0xad, 0xbe, 0xef, 0xff}
	if err := os.Mkdir(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	unknown := filepath.Join(source, "nested", "unknown.bin")
	if err := os.WriteFile(unknown, payload, 0o600); err != nil {
		t.Fatalf("write unknown binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "readme.txt"), []byte("hello restoreweave"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if err := os.Symlink("nested/unknown.bin", filepath.Join(source, "alias.bin")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	repoPath := filepath.Join(t.TempDir(), "repository")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	repo, err := repository.OpenDir(repoPath)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	service := &Service{Store: store, Repo: repo}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if ingested.Files < 2 || ingested.SnapshotRef == "" || ingested.RootID == "" {
		t.Fatalf("incomplete ingest result: %+v", ingested)
	}
	if _, err := service.Verify(ctx, ingested.SnapshotRef); err != nil {
		t.Fatalf("verify: %v", err)
	}

	listed, err := store.ListNamespaceChildren(ctx, ingested.WorkspaceID, ingested.RootID, "")
	if err != nil {
		t.Fatalf("list namespace: %v", err)
	}
	if len(listed) == 0 {
		t.Fatal("namespace is empty after ingest")
	}
	protections, err := store.ListProtectionRecords(ctx, ingested.WorkspaceID)
	if err != nil {
		t.Fatalf("list protection records: %v", err)
	}
	if len(protections) < 3 {
		t.Fatalf("protection records = %d, want entries for files and directories", len(protections))
	}
	var exactProtection sqlite.ProtectionRecord
	var fallbackProtection sqlite.ProtectionRecord
	for _, record := range protections {
		if record.Mode == sqlite.ProtectionStoreExact {
			exactProtection = record
			if record.Outcome == sqlite.ProtectionExactFallback {
				fallbackProtection = record
			}
		}
	}
	if exactProtection.ID == "" || exactProtection.LocalRepresentationID == "" || exactProtection.ExpectedContentID == "" {
		t.Fatalf("exact protection record = %+v", exactProtection)
	}
	if fallbackProtection.ID == "" || !bytes.Contains(fallbackProtection.Metadata, []byte(ProtectionReasonContentClassUnresolved)) {
		t.Fatalf("unknown readable file did not record exact fallback: %+v", fallbackProtection)
	}
	references, err := store.ListRecoveryReferencesBySubject(ctx, ingested.WorkspaceID, exactProtection.SubjectRef)
	if err != nil || len(references) != 1 || references[0].Claim != sqlite.RecoveryClaimRestoreVerified {
		t.Fatalf("recovery references = %+v, err=%v", references, err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close catalog: %v", err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatalf("delete catalog: %v", err)
	}

	fresh, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "empty.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatalf("open empty catalog: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	restorer := &Service{Store: fresh, Repo: repo}
	dest := filepath.Join(t.TempDir(), "restored")
	restored, err := restorer.Restore(ctx, ingested.SnapshotRef, dest)
	if err != nil {
		t.Fatalf("restore after catalog loss: %v", err)
	}
	if restored.Files < 2 {
		t.Fatalf("restored files = %d", restored.Files)
	}

	got, err := os.ReadFile(filepath.Join(dest, "nested", "unknown.bin"))
	if err != nil {
		t.Fatalf("read restored binary: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("restored bytes = %x, want %x", got, payload)
	}
	sum := sha256.Sum256(got)
	want := "sha256:" + hex.EncodeToString(sum[:])
	originalSum := sha256.Sum256(payload)
	originalID := "sha256:" + hex.EncodeToString(originalSum[:])
	if want != originalID {
		t.Fatalf("sha256 %s != %s", want, originalID)
	}
	target, err := os.Readlink(filepath.Join(dest, "alias.bin"))
	if err != nil {
		t.Fatalf("read restored symlink: %v", err)
	}
	if target != "nested/unknown.bin" {
		t.Fatalf("symlink target = %q", target)
	}
}

func TestRequireQualifiedRejectsPathString(t *testing.T) {
	err := requireQualified(scanner.CaptureModePathString, scanner.ScanResult{State: scanner.ScanComplete})
	if !errors.Is(err, ErrNotQualified) {
		t.Fatalf("error = %v, want ErrNotQualified", err)
	}
	for _, state := range []scanner.ScanState{scanner.ScanCancelled, scanner.ScanFailed} {
		err = requireQualifiedWithEntries(scanner.CaptureModeRootedFD, scanner.ScanResult{State: state}, nil, ingestPolicy{})
		if !errors.Is(err, ErrNotQualified) {
			t.Fatalf("scan state %s error = %v, want ErrNotQualified", state, err)
		}
	}
}

func TestLinkOnlyIngestPersistsLocatorsWithoutPlacingPayload(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("downloadable-but-not-locally-retained")
	if err := os.WriteFile(filepath.Join(source, "release.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		Store: store, Repo: repo, AllowLinkOnly: true, LinkOnlyRequiresConfirmation: true,
	}
	ingested, err := service.IngestWithOptions(ctx, source, IngestOptions{
		ProtectionMode:  sqlite.ProtectionLinkOnly,
		ConfirmLinkOnly: true,
		ExternalLocators: []IngestLocator{
			{Locator: "https://downloads.example.test/release.bin"},
			{Locator: "ipfs://bafy-example/release.bin"},
		},
	})
	if err != nil {
		t.Fatalf("link-only ingest: %v", err)
	}
	if ingested.ProtectionMode != sqlite.ProtectionLinkOnly || ingested.Files != 1 ||
		ingested.LocalFiles != 0 || ingested.LocalBytes != 0 || ingested.NewBytes != 0 ||
		ingested.LinkOnlyFiles != 1 || ingested.LocatorCount != 2 {
		t.Fatalf("link-only result = %+v", ingested)
	}

	sum := sha256.Sum256(payload)
	contentID := "sha256:" + hex.EncodeToString(sum[:])
	body, err := repo.Open(ctx, contentID)
	if body != nil {
		_ = body.Close()
	}
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("payload unexpectedly present in CAS: %v", err)
	}

	protections, err := store.ListProtectionRecords(ctx, ingested.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var fileProtection sqlite.ProtectionRecord
	for _, protection := range protections {
		if protection.Mode == sqlite.ProtectionLinkOnly {
			fileProtection = protection
		}
	}
	if fileProtection.ID == "" || fileProtection.Outcome != sqlite.ProtectionLinkOnlyUnprotected ||
		fileProtection.LocalRepresentationID != "" || fileProtection.ExpectedContentID != contentID {
		t.Fatalf("link-only protection = %+v", fileProtection)
	}
	references, err := store.ListRecoveryReferencesBySubject(ctx, ingested.WorkspaceID, fileProtection.SubjectRef)
	if err != nil || len(references) != 1 || references[0].Kind != sqlite.RecoveryExternalLocator ||
		references[0].Claim != sqlite.RecoveryClaimLinkOnlyUnprotected {
		t.Fatalf("link-only references = %+v, err=%v", references, err)
	}
	locators, err := store.ListExternalLocators(ctx, ingested.WorkspaceID, references[0].ExternalBindingID)
	if err != nil || len(locators) != 2 {
		t.Fatalf("external locators = %+v, err=%v", locators, err)
	}
	for _, locator := range locators {
		if locator.ValidationStatus != "UNVALIDATED" || locator.ExpectedContentID != contentID {
			t.Fatalf("locator overclaims validation: %+v", locator)
		}
	}

	manifest, err := readManifest(repo.Root(), ingested.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	var fileEntry ManifestEntry
	for _, entry := range manifest.Entries {
		if entry.EntryType == string(sqlite.EntryFile) {
			fileEntry = entry
		}
	}
	if fileEntry.Protection.Outcome != string(sqlite.ProtectionLinkOnlyUnprotected) ||
		len(fileEntry.Protection.RecoveryReferences) != 1 ||
		len(fileEntry.Protection.RecoveryReferences[0].ExternalLocators) != 2 {
		t.Fatalf("portable link-only entry = %+v", fileEntry)
	}
	if _, err := service.VerifyMode(ctx, ingested.SnapshotRef, VerifyAuthenticatedMetadata, ""); err != nil {
		t.Fatalf("metadata verification: %v", err)
	}
	if _, err := service.Verify(ctx, ingested.SnapshotRef); !errors.Is(err, ErrBlocked) {
		t.Fatalf("full verification error = %v, want ErrBlocked", err)
	}
	destination := filepath.Join(t.TempDir(), "must-not-exist")
	if _, err := service.Restore(ctx, ingested.SnapshotRef, destination); !errors.Is(err, ErrBlocked) {
		t.Fatalf("restore error = %v, want ErrBlocked", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("blocked restore created destination: %v", err)
	}
}

func TestLinkOnlyRequiresPolicyAndConfirmation(t *testing.T) {
	service := &Service{}
	options := IngestOptions{
		ProtectionMode:   sqlite.ProtectionLinkOnly,
		ConfirmLinkOnly:  true,
		ExternalLocators: []IngestLocator{{Locator: "https://example.test/file"}},
	}
	if _, err := service.resolveIngestOptions(options); !errors.Is(err, ErrBlocked) {
		t.Fatalf("disabled LINK_ONLY error = %v", err)
	}
	service.AllowLinkOnly = true
	service.LinkOnlyRequiresConfirmation = true
	options.ConfirmLinkOnly = false
	if _, err := service.resolveIngestOptions(options); !errors.Is(err, ErrBlocked) {
		t.Fatalf("unconfirmed LINK_ONLY error = %v", err)
	}
}

func TestRestoreUsesRawPathBytes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("invalid UTF-8 path creation is only portable in the Linux qualification profile")
	}
	ctx := context.Background()
	source := t.TempDir()
	rawName := string([]byte{'b', 'a', 'd', '-', 0xff, '.', 'b', 'i', 'n'})
	payload := []byte("raw-name")
	if err := os.WriteFile(filepath.Join(source, rawName), payload, 0o600); err != nil {
		t.Fatalf("write raw-name source: %v", err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: store, Repo: repo}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err := service.Restore(ctx, ingested.SnapshotRef, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, rawName))
	if err != nil {
		t.Fatalf("read raw-name restore: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("restored payload = %q", got)
	}
}
