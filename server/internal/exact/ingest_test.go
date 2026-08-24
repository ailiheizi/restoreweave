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
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/scanner"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type payloadPlacementResponseLossRepo struct {
	*repository.Dir
	lost bool
}

type payloadPlacementFailureRepo struct{ *repository.Dir }

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
	digest := sha256.Sum256(payload)
	contentID := "sha256:" + hex.EncodeToString(digest[:])
	if err := repo.Verify(ctx, contentID); err != nil {
		t.Fatalf("recovered payload readback: %v", err)
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
