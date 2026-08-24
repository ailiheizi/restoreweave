package exact

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// cleanInstallFixture publishes a signed snapshot with a real catalog and
// repository, exports the recovery bundle and the trust anchor, then deletes
// the catalog so the reader is forced onto the portable records alone.
func cleanInstallFixture(t *testing.T) (repoRoot, catalogPath, bundlePath, anchorPath string, anchor TrustAnchor, result IngestResult) {
	t.Helper()
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("clean-install recovery payload")
	if err := os.WriteFile(filepath.Join(source, "archive.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath = filepath.Join(t.TempDir(), "catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repoRoot = filepath.Join(t.TempDir(), "repository")
	repo, err := repository.OpenDir(repoRoot)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	identity, anchor, err := OpenSigningMaterial(t.TempDir(), testPublicationDomain, true)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	service := &Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := service.InspectIngest(ctx, source, IngestOptions{})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	result, err = service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:clean-install-plan")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	bundlePath = filepath.Join(t.TempDir(), "recovery.json")
	if _, err := service.ExportRecovery(ctx, result.SnapshotRef, bundlePath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	anchorPath = filepath.Join(t.TempDir(), "trust-anchor.json")
	if _, err := ExportTrustAnchor(anchor, anchorPath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	return repoRoot, catalogPath, bundlePath, anchorPath, anchor, result
}

func openCleanInstallReader(t *testing.T, repoRoot string, anchor TrustAnchor) *Service {
	t.Helper()
	repo, err := repository.OpenProfileReadOnly(repository.RepositoryProfileDirectoryCASDev, repoRoot)
	if err != nil {
		t.Fatalf("open read-only repository: %v", err)
	}
	return &Service{
		Repo: repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true,
	}
}

func TestCleanInstallImportReadsBundleAndCatalogFreeReaderWorks(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, bundlePath, anchorPath, anchor, result := cleanInstallFixture(t)

	reader := openCleanInstallReader(t, repoRoot, anchor)
	imported, err := reader.ImportRecoveryBundle(ctx, bundlePath, anchorPath, testPublicationDomain)
	if err != nil {
		t.Fatalf("import recovery bundle: %v", err)
	}
	if imported.SnapshotRef != result.SnapshotRef || imported.CommitDigest != result.PublicationCommitDigest ||
		imported.PreparedClosureDigest != result.PreparedClosureDigest || imported.Generation != result.PublicationGeneration ||
		imported.ManifestDigest != result.ManifestDigest {
		t.Fatalf("import result = %+v, want snapshot %s commit %s prepared %s", imported, result.SnapshotRef, result.PublicationCommitDigest, result.PreparedClosureDigest)
	}
	if imported.Files != 1 || imported.Bytes == 0 {
		t.Fatalf("import totals = files %d bytes %d", imported.Files, imported.Bytes)
	}
	if imported.TrustAnchorDigest == "" {
		t.Fatal("import did not bind the trust anchor digest")
	}
	if imported.CatalogCreated {
		t.Fatal("catalog-free import fabricated CatalogCreated")
	}

	listed, err := reader.ListSnapshots(ctx)
	if err != nil || len(listed) != 1 || listed[0].SnapshotRef != result.SnapshotRef {
		t.Fatalf("clean discovery = %+v, err=%v", listed, err)
	}
	if _, err := reader.Verify(ctx, result.SnapshotRef); err != nil {
		t.Fatalf("clean verify: %v", err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	plan, err := reader.InspectRestore(ctx, result.SnapshotRef, destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ApplyRestorePlan(ctx, plan); err != nil {
		t.Fatalf("clean restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "archive.bin"))
	if err != nil || !bytes.Equal(got, []byte("clean-install recovery payload")) {
		t.Fatalf("restored payload = %q, err=%v", got, err)
	}
}

func TestCleanInstallImportArtifactAcceptsV2AndLegacyV1(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, legacyPath, anchorPath, anchor, result := cleanInstallFixture(t)
	reader := openCleanInstallReader(t, repoRoot, anchor)

	v2Path := filepath.Join(t.TempDir(), "recovery-reference.json")
	exported, err := reader.ExportRecoveryReference(ctx, result.SnapshotRef, v2Path)
	if err != nil {
		t.Fatalf("export v2 recovery reference: %v", err)
	}
	if exported.Schema != RecoveryReferenceSchemaV2 {
		t.Fatalf("v2 export schema = %q", exported.Schema)
	}
	imported, err := reader.ImportRecoveryArtifact(ctx, v2Path, anchorPath, testPublicationDomain)
	if err != nil {
		t.Fatalf("import v2 recovery reference: %v", err)
	}
	if imported.Schema != RecoveryReferenceSchemaV2 || imported.SnapshotRef != result.SnapshotRef ||
		imported.FactHealth != RecoveryFactHealthComplete || imported.CatalogCreated {
		t.Fatalf("v2 import result = %+v", imported)
	}

	legacy, err := reader.ImportRecoveryArtifact(ctx, legacyPath, anchorPath, testPublicationDomain)
	if err != nil {
		t.Fatalf("import legacy v1 recovery bundle: %v", err)
	}
	if legacy.Schema != RecoveryExportBundleSchemaV1 || legacy.SnapshotRef != result.SnapshotRef ||
		legacy.FactHealth != RecoveryFactHealthIncomplete {
		t.Fatalf("legacy import result = %+v", legacy)
	}
}

func TestCleanInstallRecoveryArtifactInputsRejectSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test requires Unix link semantics")
	}
	ctx := context.Background()
	repoRoot, _, legacyPath, anchorPath, anchor, result := cleanInstallFixture(t)
	reader := openCleanInstallReader(t, repoRoot, anchor)
	v2Path := filepath.Join(t.TempDir(), "recovery-reference.json")
	if _, err := reader.ExportRecoveryReference(ctx, result.SnapshotRef, v2Path); err != nil {
		t.Fatalf("export v2 recovery reference: %v", err)
	}

	legacyLink := filepath.Join(t.TempDir(), "legacy-link.json")
	if err := os.Symlink(legacyPath, legacyLink); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ImportRecoveryBundle(ctx, legacyLink, anchorPath, testPublicationDomain); err == nil {
		t.Fatal("legacy recovery bundle symlink was followed")
	}
	if _, err := reader.ImportRecoveryArtifact(ctx, legacyLink, anchorPath, testPublicationDomain); err == nil {
		t.Fatal("legacy recovery artifact symlink was followed")
	}

	v2Link := filepath.Join(t.TempDir(), "v2-link.json")
	if err := os.Symlink(v2Path, v2Link); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ImportRecoveryArtifact(ctx, v2Link, anchorPath, testPublicationDomain); err == nil {
		t.Fatal("v2 recovery artifact symlink was followed")
	}

	destination := filepath.Join(t.TempDir(), "should-not-exist.json")
	if _, err := reader.MigrateRecoveryArtifact(ctx, legacyLink, anchorPath, destination, testPublicationDomain); err == nil {
		t.Fatal("migration followed a legacy recovery artifact symlink")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("migration wrote destination after rejecting symlink: stat error=%v", err)
	}
}

func TestCleanInstallMigratesLegacyV1ToV2Successor(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, legacyPath, anchorPath, anchor, result := cleanInstallFixture(t)
	reader := openCleanInstallReader(t, repoRoot, anchor)
	destination := filepath.Join(t.TempDir(), "migrated-reference.json")
	migrated, err := reader.MigrateRecoveryArtifact(ctx, legacyPath, anchorPath, destination, testPublicationDomain)
	if err != nil {
		t.Fatalf("migrate legacy recovery artifact: %v", err)
	}
	if migrated.Schema != RecoveryReferenceSchemaV2 || migrated.SnapshotRef != result.SnapshotRef ||
		migrated.ManifestDigest != result.ManifestDigest || !migrated.IndependentlyStored {
		t.Fatalf("migration result = %+v", migrated)
	}
	payload, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := DecodeRecoveryReference(payload)
	if err != nil {
		t.Fatalf("decode migrated reference: %v", err)
	}
	if reference.PublicationCommitDigest != result.PublicationCommitDigest ||
		reference.PreparedClosure.Manifest.ManifestDigest != result.ManifestDigest ||
		reference.FactHealth != RecoveryFactHealthComplete {
		t.Fatalf("migrated reference changed identity or lost facts: %+v", reference)
	}
	if err := reference.ValidateAgainstRepository(ctx, reader.Repo, anchor); err != nil {
		t.Fatalf("migrated reference does not validate: %v", err)
	}
	// The legacy source remains intact and independently readable.
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy source was removed during migration: %v", err)
	}
}

func TestCleanInstallMigrationRejectsTamperedLegacyArtifact(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, legacyPath, anchorPath, anchor, _ := cleanInstallFixture(t)
	payload, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) < 2 {
		t.Fatal("legacy recovery artifact unexpectedly empty")
	}
	payload[len(payload)/2] ^= 0x01
	tamperedPath := filepath.Join(t.TempDir(), "tampered-legacy.json")
	if err := os.WriteFile(tamperedPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	reader := openCleanInstallReader(t, repoRoot, anchor)
	destination := filepath.Join(t.TempDir(), "should-not-exist.json")
	if _, err := reader.MigrateRecoveryArtifact(ctx, tamperedPath, anchorPath, destination, testPublicationDomain); err == nil {
		t.Fatal("migration accepted a tampered legacy artifact")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("migration wrote destination after rejection: stat error=%v", err)
	}
}

func TestCleanInstallLegacyImportRejectsMissingExactPayload(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, legacyPath, anchorPath, anchor, _ := cleanInstallFixture(t)
	bundle, err := readRecoveryBundle(legacyPath)
	if err != nil || len(bundle.PreparedClosure.PayloadReceipt.Objects) == 0 {
		t.Fatalf("read legacy payload receipt: bundle=%+v err=%v", bundle, err)
	}
	contentID := bundle.PreparedClosure.PayloadReceipt.Objects[0].ContentID
	hexDigest := strings.TrimPrefix(contentID, "sha256:")
	if err := os.Remove(filepath.Join(repoRoot, "blobs", "sha256", hexDigest[:2], hexDigest)); err != nil {
		t.Fatal(err)
	}
	reader := openCleanInstallReader(t, repoRoot, anchor)
	if _, err := reader.ImportRecoveryBundle(ctx, legacyPath, anchorPath, testPublicationDomain); err == nil {
		t.Fatal("legacy recovery import accepted a missing exact payload")
	}
	destination := filepath.Join(t.TempDir(), "should-not-exist.json")
	if _, err := reader.MigrateRecoveryArtifact(ctx, legacyPath, anchorPath, destination, testPublicationDomain); err == nil {
		t.Fatal("legacy migration accepted a missing exact payload")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("migration wrote destination after missing-payload rejection: stat error=%v", err)
	}
}

func TestCleanInstallImportWrongAnchorFailsClosed(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, bundlePath, _, anchor, _ := cleanInstallFixture(t)
	reader := openCleanInstallReader(t, repoRoot, anchor)

	anchorPath := filepath.Join(t.TempDir(), "wrong-anchor.json")
	if _, err := ExportTrustAnchor(mustDifferentAnchor(t), anchorPath); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ImportRecoveryBundle(ctx, bundlePath, anchorPath, testPublicationDomain); err == nil {
		t.Fatal("import accepted a bundle under the wrong trust anchor")
	}
}

func mustDifferentIdentity(t *testing.T) SigningIdentity {
	t.Helper()
	identity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func mustDifferentAnchor(t *testing.T) TrustAnchor {
	t.Helper()
	anchor, err := NewTrustAnchor(mustDifferentIdentity(t), testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	return anchor
}

func TestCleanInstallImportTamperedBundleFailsClosed(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, bundlePath, anchorPath, anchor, _ := cleanInstallFixture(t)

	payload, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatal(err)
	}
	raw["snapshot_ref"] = "snapshot:tampered"
	tampered, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	tamperedPath := filepath.Join(t.TempDir(), "tampered.json")
	if err := os.WriteFile(tamperedPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	reader := openCleanInstallReader(t, repoRoot, anchor)
	if _, err := reader.ImportRecoveryBundle(ctx, tamperedPath, anchorPath, testPublicationDomain); err == nil {
		t.Fatal("tampered bundle was admitted")
	}

	flipped := append([]byte(nil), payload...)
	flipped[len(flipped)/2] ^= 0x01
	flippedPath := filepath.Join(t.TempDir(), "flipped.json")
	if err := os.WriteFile(flippedPath, flipped, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ImportRecoveryBundle(ctx, flippedPath, anchorPath, testPublicationDomain); err == nil {
		t.Fatal("byte-flipped bundle was admitted")
	}
}

func TestCleanInstallImportCorruptAnchorFailsClosed(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, bundlePath, _, anchor, _ := cleanInstallFixture(t)
	reader := openCleanInstallReader(t, repoRoot, anchor)

	corruptPath := filepath.Join(t.TempDir(), "corrupt-anchor.json")
	if err := os.WriteFile(corruptPath, []byte(`{"schema":"org.restoreweave.trust-anchor.v1","public_key":"not-a-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ImportRecoveryBundle(ctx, bundlePath, corruptPath, testPublicationDomain); err == nil {
		t.Fatal("corrupt trust anchor was accepted")
	}
}

func TestCleanInstallImportDomainMismatchFailsClosed(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, bundlePath, anchorPath, anchor, _ := cleanInstallFixture(t)
	reader := openCleanInstallReader(t, repoRoot, anchor)
	if _, err := reader.ImportRecoveryBundle(ctx, bundlePath, anchorPath, "workspace:other"); err == nil {
		t.Fatal("import accepted a mismatched publication domain")
	}
}

func TestCleanInstallImportReconcilesExistingStoreProjection(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "archive.bin"), []byte("reconcile projection"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(t.TempDir(), "repository")
	repo, err := repository.OpenDir(repoRoot)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	identity, anchor, err := OpenSigningMaterial(t.TempDir(), testPublicationDomain, true)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	service := &Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := service.InspectIngest(ctx, source, IngestOptions{})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	result, err := service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:reconcile-plan")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "recovery.json")
	if _, err := service.ExportRecovery(ctx, result.SnapshotRef, bundlePath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	anchorPath := filepath.Join(t.TempDir(), "trust-anchor.json")
	if _, err := ExportTrustAnchor(anchor, anchorPath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Drop the rebuildable publication projection while keeping the staged
	// namespace, mirroring a crash after exact commitment but before the
	// projection row is recorded.
	reopened, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	raw, err := sql.Open("sqlite", catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `DROP TRIGGER publications_no_delete`); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `DELETE FROM publications`); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	repo, err = repository.OpenDir(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	reconciling := &Service{
		Store: reopened, Repo: repo, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	imported, err := reconciling.ImportRecoveryBundle(ctx, bundlePath, anchorPath, testPublicationDomain)
	if err != nil {
		t.Fatalf("import with store: %v", err)
	}
	if !imported.CatalogCreated {
		t.Fatal("import with staged projection did not create the publication row")
	}
	publication, err := reopened.GetPublicationBySnapshotRef(ctx, "", result.SnapshotRef)
	if err != nil {
		t.Fatalf("reconciled publication missing: %v", err)
	}
	if publication.ManifestDigest != result.ManifestDigest || publication.SnapshotRef != result.SnapshotRef {
		t.Fatalf("reconciled publication = %+v", publication)
	}
}

func TestCleanInstallImportStoreWithoutStagedRootStaysCatalogFree(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, bundlePath, anchorPath, anchor, _ := cleanInstallFixture(t)

	empty, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "empty.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	repo, err := repository.OpenDir(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		Store: empty, Repo: repo, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	imported, err := service.ImportRecoveryBundle(ctx, bundlePath, anchorPath, testPublicationDomain)
	if err != nil {
		t.Fatalf("import with empty store: %v", err)
	}
	if imported.CatalogCreated {
		t.Fatal("import fabricated a projection for an unstaged catalog")
	}
}

func TestCleanInstallImportRejectsForeignRepository(t *testing.T) {
	ctx := context.Background()
	_, _, bundlePath, anchorPath, anchor, _ := cleanInstallFixture(t)

	foreign, err := repository.OpenDir(filepath.Join(t.TempDir(), "foreign-repository"))
	if err != nil {
		t.Fatal(err)
	}
	reader := &Service{
		Repo: foreign, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true,
	}
	if _, err := reader.ImportRecoveryBundle(ctx, bundlePath, anchorPath, testPublicationDomain); err == nil {
		t.Fatal("import admitted a bundle into a foreign repository")
	}
}

func TestCleanInstallMetadataOnlySnapshotHasNoImportableToken(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	filePath := filepath.Join(source, "unreadable.bin")
	if err := os.WriteFile(filePath, []byte("unreadable"), 0o600); err != nil {
		t.Fatal(err)
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
	identity, anchor, err := OpenSigningMaterial(t.TempDir(), testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := service.InspectIngest(ctx, source, IngestOptions{
		FileProtection:          map[string]sqlite.ProtectionMode{"unreadable.bin": sqlite.ProtectionMetadataOnly},
		MetadataOnlyResolutions: []string{"unreadable.bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:metadata-only-import-plan")
	if err != nil {
		t.Fatal(err)
	}
	reader := &Service{
		Repo: repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true,
	}
	if _, err := reader.BuildRecoveryToken(ctx, result.SnapshotRef, "unreadable.bin", anchor); !errors.Is(err, ErrNoRecoveryPath) {
		t.Fatalf("metadata-only token error = %v, want ErrNoRecoveryPath", err)
	}
}
