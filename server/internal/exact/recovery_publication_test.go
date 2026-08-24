package exact

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/scanner"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestSignedPublicationAuthenticatesPortableFileFacts(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "facts.txt", []byte("portable facts"))
	if err := os.Link(filepath.Join(fixture.source, "facts.txt"), filepath.Join(fixture.source, "facts-alias.txt")); err != nil {
		t.Fatalf("create hard-link fixture: %v", err)
	}
	result := fixture.ingest(t, "sha256:facts-plan")
	ctx := context.Background()

	publications, err := fixture.service.committedPublications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(publications) != 1 {
		t.Fatalf("committed publications = %d, want 1", len(publications))
	}
	publication := publications[0]
	if publication.Prepared.Schema != PreparedEnvelopeSchemaV2 || publication.Manifest.Schema != SnapshotSchemaV2 {
		t.Fatalf("portable schemas = %q / %q", publication.Prepared.Schema, publication.Manifest.Schema)
	}
	if publication.Prepared.VerificationEvidence.Schema != MetadataEvidenceSchemaV2 ||
		publication.Prepared.VerificationEvidence.PortableFactsSchema != PortableFactsSchemaV1 ||
		publication.Prepared.VerificationEvidence.PortableFactsCoverage != portableFactsCoveragePartial ||
		!sameStrings(publication.Prepared.VerificationEvidence.PortableFactsOmissions, portableFactsV1Omissions) {
		t.Fatalf("portable facts evidence = %+v", publication.Prepared.VerificationEvidence)
	}

	var file, alias ManifestEntry
	for _, entry := range publication.Manifest.Entries {
		switch entry.RelativePath {
		case "facts.txt":
			file = entry
		case "facts-alias.txt":
			alias = entry
		}
	}
	if file.Facts == nil || alias.Facts == nil || len(file.Facts.Facts) != len(requiredPortableFactNames) {
		t.Fatalf("file portable facts = %+v", file.Facts)
	}
	factByName := make(map[string]ManifestPortableFact, len(file.Facts.Facts))
	for _, fact := range file.Facts.Facts {
		factByName[fact.Name] = fact
	}
	if factByName[PortableFactDetection].State != PortableFactObserved {
		t.Fatalf("detection fact = %+v", factByName[PortableFactDetection])
	}
	if factByName[PortableFactSparseIndication].State == "" || factByName[PortableFactHardLink].State == "" ||
		factByName[PortableFactBoundary].State == "" {
		t.Fatalf("captured file facts are incomplete: %+v", factByName)
	}
	for _, name := range []string{PortableFactXAttrs, PortableFactACLs, PortableFactSparseExtents} {
		switch name {
		case PortableFactXAttrs, PortableFactACLs:
			if factByName[name].State != PortableFactObserved && factByName[name].State != PortableFactUnobserved && factByName[name].State != PortableFactUnsupported {
				t.Fatalf("fact %s state = %q, want an explicit capture/degradation state", name, factByName[name].State)
			}
		case PortableFactSparseExtents:
			if factByName[name].State != PortableFactUnsupported {
				t.Fatalf("fact %s state = %q, want UNSUPPORTED", name, factByName[name].State)
			}
		}
	}
	var hardLink PortableHardLinkValue
	if err := decodePortableFactValue(factByName[PortableFactHardLink].Value, &hardLink); err != nil {
		t.Fatal(err)
	}
	aliasFacts := make(map[string]ManifestPortableFact, len(alias.Facts.Facts))
	for _, fact := range alias.Facts.Facts {
		aliasFacts[fact.Name] = fact
	}
	var aliasHardLink PortableHardLinkValue
	if err := decodePortableFactValue(aliasFacts[PortableFactHardLink].Value, &aliasHardLink); err != nil {
		t.Fatal(err)
	}
	if hardLink.State != "MULTIPLE_LINKS" || hardLink.GroupID == "" || hardLink.GroupID != aliasHardLink.GroupID {
		t.Fatalf("hard-link facts do not preserve the shared group: %+v / %+v", hardLink, aliasHardLink)
	}
	factsDigest, err := manifestFactsDigest(publication.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if factsDigest != publication.Prepared.VerificationEvidence.PortableFactsDigest ||
		publication.Prepared.VerificationEvidence.PortableFactsEntryCount != int64(len(publication.Manifest.Entries)) {
		t.Fatalf("facts evidence does not bind manifest: %+v", publication.Prepared.VerificationEvidence)
	}

	reader := &Service{
		Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	manifest, err := reader.loadManifest(ctx, result.SnapshotRef)
	if err != nil {
		t.Fatalf("catalog-free facts read: %v", err)
	}
	readDigest, err := manifestFactsDigest(manifest)
	if err != nil || readDigest != factsDigest {
		t.Fatalf("catalog-free facts digest = %q, %v; want %q", readDigest, err, factsDigest)
	}

	clonePayload, err := json.Marshal(publication.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var tampered Manifest
	if err := json.Unmarshal(clonePayload, &tampered); err != nil {
		t.Fatal(err)
	}
	for i := range tampered.Entries {
		if tampered.Entries[i].RelativePath == "facts.txt" {
			tampered.Entries[i].Facts.Facts[0].Value = json.RawMessage(`{"reason_code":"TAMPERED"}`)
			break
		}
	}
	if err := authenticateManifest(tampered); err == nil {
		t.Fatal("tampered portable fact provenance was accepted")
	}
	for i := range tampered.Entries {
		if tampered.Entries[i].RelativePath == "facts.txt" {
			tampered.Entries[i].Facts.Facts[0].Value = json.RawMessage(`{"arbitrary":true}`)
			digest, err := tampered.Entries[i].Facts.Facts[0].Digest()
			if err != nil {
				t.Fatal(err)
			}
			tampered.Entries[i].Facts.Facts[0].ProvenanceDigest = digest
			break
		}
	}
	if err := authenticateManifest(tampered); err == nil {
		t.Fatal("typed-invalid portable fact was accepted after recomputing its digest")
	}
	for i := range tampered.Entries {
		if tampered.Entries[i].RelativePath == "facts.txt" {
			tampered.Entries[i].Facts = nil
			break
		}
	}
	if err := authenticateManifest(tampered); err == nil {
		t.Fatal("missing snapshot.v2 portable facts were accepted")
	}
}

func TestPayloadReceiptRejectsUnknownZeroLengthObject(t *testing.T) {
	zero := int64(0)
	manifestID := "sha256:" + strings.Repeat("a", 64)
	receiptID := "sha256:" + strings.Repeat("b", 64)
	manifest := Manifest{Schema: SnapshotSchemaV1, Entries: []ManifestEntry{{
		RelativePath: "empty", EntryType: string(sqlite.EntryFile), ContentID: manifestID,
		Protection: ManifestProtection{
			LocalRepresentationID: "rep-empty", ExpectedLogicalLength: &zero,
		},
	}}}
	receipt := PayloadReceipt{Objects: []PayloadObjectReceipt{{ContentID: receiptID, LogicalBytes: 0}}}
	if err := compareManifestPayloadReceipt(manifest, receipt); err == nil {
		t.Fatal("unknown zero-length payload object was accepted as manifest content")
	}
}

func TestFailedDetectionRemainsObservedPortableEvidence(t *testing.T) {
	binding := capture.BindingRecord{
		Schema: capture.SchemaBindingV1, Profile: capture.ProfileLocalTree,
		CaptureMode: scanner.CaptureModeRootedFD, BoundAt: time.Unix(1, 0).UTC(),
	}
	entry := scanner.EntryRecord{
		Kind:      scanner.KindRegularFile,
		HardLink:  scanner.HardLinkFacts{State: scanner.HardLinkSingle, LinkCount: 1},
		Sparse:    scanner.SparseFacts{State: scanner.SparseNotIndicated},
		Boundary:  scanner.BoundaryObservation{Checked: true, Action: scanner.BoundaryInclude},
		Detection: scanner.DetectionObservation{State: scanner.DetectionFailed},
	}
	facts, err := buildManifestEntryFacts(entry, binding)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts.Facts {
		if fact.Name == PortableFactDetection {
			if fact.State != PortableFactObserved {
				t.Fatalf("failed detection state = %q, want OBSERVED", fact.State)
			}
			return
		}
	}
	t.Fatal("portable detection fact is missing")
}

func TestSignedPublicationDrivesCatalogFreeDiscoveryAndRestore(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	payload := []byte("signed portable recovery")
	if err := os.WriteFile(filepath.Join(source, "archive.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
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
	plan, err := service.InspectIngest(ctx, source, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:test-plan")
	if err != nil {
		t.Fatalf("signed ingest: %v", err)
	}
	if result.PreparedClosureDigest == "" || result.PublicationCommitDigest == "" || result.PublicationGeneration != 1 {
		t.Fatalf("publication evidence = %+v", result)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
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
	reopened, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	service.Store = reopened
	reconciled, err := service.ReconcileIngestPublication(ctx, result.WorkspaceID, "sha256:test-plan")
	if err != nil {
		t.Fatalf("reconcile portable commit into projection: %v", err)
	}
	if reconciled.PublicationCommitDigest != result.PublicationCommitDigest || reconciled.SnapshotRef != result.SnapshotRef {
		t.Fatalf("reconciled publication = %+v", reconciled)
	}
	if err := reopened.Close(); err != nil {
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
		t.Fatalf("signed discovery = %+v, %v", listed, err)
	}
	if _, err := writeManifest(repo.Root(), Manifest{
		Schema: SnapshotSchemaV1, SnapshotRef: "orphan-snapshot", Entries: []ManifestEntry{},
	}); err != nil {
		t.Fatal(err)
	}
	listed, err = reader.ListSnapshots(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("orphan manifest became visible: %+v, %v", listed, err)
	}
	if _, err := reader.Verify(ctx, result.SnapshotRef); err != nil {
		t.Fatalf("catalog-free verify: %v", err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	restorePlan, err := reader.InspectRestore(ctx, result.SnapshotRef, destination)
	if err != nil || restorePlan.PublicationCommitDigest != result.PublicationCommitDigest {
		t.Fatalf("restore plan commit binding = %+v, %v", restorePlan, err)
	}
	if _, err := reader.ApplyRestorePlan(ctx, restorePlan); err != nil {
		t.Fatalf("catalog-free restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "archive.bin"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("restored payload = %q, %v", got, err)
	}

	otherIdentity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrongAnchor, err := NewTrustAnchor(otherIdentity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	wrongReader := &Service{
		Repo: repo, TrustAnchor: &wrongAnchor, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true,
	}
	if _, err := wrongReader.ListSnapshots(ctx); err == nil {
		t.Fatal("publication authenticated with the wrong trust anchor")
	}
}

func TestSignedPublicationRejectsTamperedPreparedClosure(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("tamper evidence"), 0o600); err != nil {
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
	service := &Service{Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	plan, err := service.InspectIngest(ctx, source, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:tamper-plan")
	if err != nil {
		t.Fatal(err)
	}
	hexDigest := result.PreparedClosureDigest[len("sha256:"):]
	preparedPath := filepath.Join(repo.Root(), "recovery", "prepared", "sha256", hexDigest[:2], hexDigest)
	if err := os.WriteFile(preparedPath, []byte(`{"tampered":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := &Service{Repo: repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListSnapshots(ctx); err == nil {
		t.Fatal("tampered prepared closure was accepted")
	}
}

type signedPublicationFixture struct {
	service     *Service
	store       *sqlite.Store
	catalogPath string
	repo        *repository.Dir
	source      string
}

// errorAfterRecordPlaceRepo models a lost response after the repository has
// durably accepted one immutable recovery record. The caller must reconcile
// the repository rather than assuming placement failed.
type errorAfterRecordPlaceRepo struct {
	*repository.Dir
	failRole repository.RecordRole
	failed   bool
}

func (r *errorAfterRecordPlaceRepo) PlaceRecord(ctx context.Context, role repository.RecordRole, body io.Reader) (repository.RecordReceipt, error) {
	receipt, err := r.Dir.PlaceRecord(ctx, role, body)
	if err != nil {
		return repository.RecordReceipt{}, err
	}
	if role == r.failRole && !r.failed {
		r.failed = true
		return repository.RecordReceipt{}, errors.New("response lost after record placement")
	}
	return receipt, nil
}

func newSignedPublicationFixture(t *testing.T, name string, payload []byte) signedPublicationFixture {
	t.Helper()
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, name), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	identity, anchor, err := OpenSigningMaterial(t.TempDir(), testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	return signedPublicationFixture{
		service: &Service{
			Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
			PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
			ConfigDigest: "sha256:exact-test-config",
		},
		store: store, catalogPath: catalogPath, repo: repo, source: source,
	}
}

func (f signedPublicationFixture) ingest(t *testing.T, key string) IngestResult {
	t.Helper()
	ctx := context.Background()
	plan, err := f.service.InspectIngest(ctx, f.source, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.ApplyIngestPlanWithExecutionKey(ctx, plan, key)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSignedRecoveryExportBundleBindsCommitAndPreparedClosure(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "bundle.txt", []byte("portable bundle"))
	result := fixture.ingest(t, "sha256:bundle-plan")

	destination := filepath.Join(t.TempDir(), "recovery.json")
	exported, err := fixture.service.ExportRecovery(context.Background(), result.SnapshotRef, destination)
	if err != nil {
		t.Fatalf("export recovery: %v", err)
	}
	if exported.Schema != RecoveryExportBundleSchemaV1 {
		t.Fatalf("export schema = %q", exported.Schema)
	}
	payload, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("private_key")) {
		t.Fatal("recovery export contains private key material")
	}
	var bundle recoveryExportBundle
	if err := decodeStrictRecord(payload, &bundle); err != nil {
		t.Fatalf("decode recovery export bundle: %v", err)
	}
	if bundle.Schema != RecoveryExportBundleSchemaV1 || bundle.SnapshotRef != result.SnapshotRef ||
		bundle.PublicationCommitDigest != result.PublicationCommitDigest || bundle.PreparedClosureDigest != result.PreparedClosureDigest {
		t.Fatalf("incomplete recovery export bundle: %+v", bundle)
	}
	commitDigest, err := bundle.PublicationCommit.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if commitDigest != bundle.PublicationCommitDigest {
		t.Fatalf("bundle commit digest = %q, want %q", commitDigest, bundle.PublicationCommitDigest)
	}
	preparedDigest, err := DigestCanonicalJSON(bundle.PreparedClosure)
	if err != nil {
		t.Fatal(err)
	}
	if preparedDigest != bundle.PreparedClosureDigest {
		t.Fatalf("bundle prepared digest = %q, want %q", preparedDigest, bundle.PreparedClosureDigest)
	}
	anchorDigest, err := DigestCanonicalJSON(*fixture.service.TrustAnchor)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.RequiredTrustAnchorKeyID != fixture.service.TrustAnchor.KeyID || bundle.RequiredTrustAnchorDigest != anchorDigest {
		t.Fatalf("bundle anchor metadata does not describe configured anchor: %+v", bundle)
	}

	// These fields tell an operator which independent anchor is required; they
	// are not a trust root. Authentication still succeeds against the anchor
	// retained outside the export bundle when the metadata is changed.
	bundle.RequiredTrustAnchorKeyID = "ed25519:wrong"
	bundle.RequiredTrustAnchorDigest = "sha256:wrong"
	if err := bundle.PublicationCommit.Verify(*fixture.service.TrustAnchor); err != nil {
		t.Fatalf("commit was not independently authenticated: %v", err)
	}
	if err := bundle.PreparedClosure.Prepared.Verify(*fixture.service.TrustAnchor); err != nil {
		t.Fatalf("prepared closure was not independently authenticated: %v", err)
	}
}

func TestSignedPublicationGenerationTwoRequiresParentLineage(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "first.txt", []byte("first publication"))
	first := fixture.ingest(t, "sha256:first-plan")
	if err := os.WriteFile(filepath.Join(fixture.source, "second.txt"), []byte("second publication"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := fixture.ingest(t, "sha256:second-plan")
	if second.PublicationGeneration != 2 {
		t.Fatalf("second publication generation = %d, want 2", second.PublicationGeneration)
	}
	publications, err := fixture.service.committedPublications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(publications) != 2 {
		t.Fatalf("committed publications = %d, want 2", len(publications))
	}
	if publications[1].Commit.ParentCommitDigest != publications[0].CommitDigest {
		t.Fatalf("generation 2 parent = %q, want %q", publications[1].Commit.ParentCommitDigest, publications[0].CommitDigest)
	}
	if publications[1].Prepared.Prepared.ParentCommitDigest != publications[0].CommitDigest {
		t.Fatalf("generation 2 prepared parent = %q, want %q", publications[1].Prepared.Prepared.ParentCommitDigest, publications[0].CommitDigest)
	}
	if first.PublicationCommitDigest != publications[0].CommitDigest || second.PublicationCommitDigest != publications[1].CommitDigest {
		t.Fatalf("result publication digests do not match committed lineage")
	}
}

func TestSignedPublicationRejectsGenerationTwoWithoutGenesis(t *testing.T) {
	identity := testIdentity(t)
	anchor, err := NewTrustAnchor(identity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	commitInput := testCommit(t, identity)
	commitInput.Generation = 2
	commitInput.ParentCommitDigest = "sha256:missing-genesis"
	commitInput.TargetIdentity = repo.RepositoryIdentity()
	commit, err := SignPublicationCommit(identity, commitInput)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := CanonicalJSON(commit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PlaceRecord(context.Background(), repository.RecordPublicationCommit, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	reader := &Service{Repo: repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListSnapshots(context.Background()); err == nil {
		t.Fatal("generation-two publication without a genesis commit was accepted")
	}
}

func TestPreparedOnlyRecoveryRecordIsNotDiscoverable(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "orphan.txt", []byte("committed publication"))
	result := fixture.ingest(t, "sha256:orphan-plan")
	if _, err := fixture.repo.PlaceRecord(context.Background(), repository.RecordPreparedClosure, bytes.NewReader([]byte(`{"orphan":true}`))); err != nil {
		t.Fatal(err)
	}
	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	listed, err := reader.ListSnapshots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].SnapshotRef != result.SnapshotRef {
		t.Fatalf("prepared-only record became discoverable: %+v", listed)
	}
}

func TestSignedPublicationRetryReconcilesAlreadyCommittedExecution(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "retry.txt", []byte("retry publication"))
	ctx := context.Background()
	plan, err := fixture.service.InspectIngest(ctx, fixture.source, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := fixture.service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:retry-plan")
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:retry-plan")
	if !errors.Is(err, ErrPublicationAlreadyCommitted) {
		t.Fatalf("retry error = %v, want ErrPublicationAlreadyCommitted", err)
	}
	recovered, err := fixture.service.ReconcileIngestPublication(ctx, first.WorkspaceID, "sha256:retry-plan")
	if err != nil {
		t.Fatalf("reconcile already committed publication: %v", err)
	}
	if recovered.SnapshotRef != first.SnapshotRef || recovered.ManifestDigest != first.ManifestDigest ||
		recovered.PublicationCommitDigest != first.PublicationCommitDigest || recovered.PublicationGeneration != first.PublicationGeneration {
		t.Fatalf("reconciled retry result = %+v, first = %+v", recovered, first)
	}
}

func TestSignedPublicationPlacedCommitErrorReconcilesToCommittedResult(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "lost-commit.txt", []byte("lost commit response"))
	fixture.service.Repo = &errorAfterRecordPlaceRepo{Dir: fixture.repo, failRole: repository.RecordPublicationCommit}
	plan, err := fixture.service.InspectIngest(context.Background(), fixture.source, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.ApplyIngestPlanWithExecutionKey(context.Background(), plan, "sha256:lost-commit-plan")
	if err != nil {
		t.Fatalf("placed commit was not reconciled: %v", err)
	}
	if result.PublicationCommitDigest == "" || result.PublicationGeneration != 1 {
		t.Fatalf("reconciled result = %+v", result)
	}
	commits, err := fixture.repo.ListRecordDigests(context.Background(), repository.RecordPublicationCommit)
	if err != nil || len(commits) != 1 {
		t.Fatalf("committed records = %v, %v; want one logical commit", commits, err)
	}
}

func TestSignedPublicationPlacedPreparedErrorIsTypedUnknownAndNeedsReconciliation(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "lost-prepared.txt", []byte("lost prepared response"))
	fixture.service.Repo = &errorAfterRecordPlaceRepo{Dir: fixture.repo, failRole: repository.RecordPreparedClosure}
	ctx := context.Background()
	plan, err := fixture.service.InspectIngest(ctx, fixture.source, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:lost-prepared-plan")
	if !errors.Is(err, ErrUnknownExternalOutcome) || !errors.Is(err, ErrNeedsReconciliation) {
		t.Fatalf("placed prepared error = %v, want typed unknown outcome", err)
	}
	var outcome *PublicationOutcomeError
	if !errors.As(err, &outcome) || outcome.Role != repository.RecordPreparedClosure {
		t.Fatalf("typed outcome = %#v, want prepared placement", err)
	}
	workspace, err := fixture.store.GetWorkspaceByName(ctx, defaultWorkspaceName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.ReconcileIngestPublication(ctx, workspace.ID, "sha256:lost-prepared-plan")
	if !errors.Is(err, ErrUnknownExternalOutcome) || !errors.Is(err, ErrNeedsReconciliation) {
		t.Fatalf("prepared-only reconciliation = %v, want typed unknown outcome", err)
	}
}
