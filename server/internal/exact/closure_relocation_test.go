package exact

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// relocateSizedRepository builds a fully committed portable closure chain with
// a processor-attempt child and a portable-fact successor (with a description
// attachment so the closure has a non-empty attachment), then moves the whole
// repository directory to a fresh location. The returned service retains the
// signing material and anchor; tests discover records through a read-only
// driver opened at the moved root.
func relocateSizedRepository(t *testing.T, zstd bool) (service *Service, movedRoot, catalogPath string, result IngestResult) {
	t.Helper()
	ctx := context.Background()
	source := t.TempDir()
	payload := bytes.Repeat([]byte("relocated exact recovery bytes\n"), 64)
	if err := os.WriteFile(filepath.Join(source, "archive.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogPath = filepath.Join(t.TempDir(), "catalog.sqlite")
	store, err := sqlite.Open(ctx, catalogPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(t.TempDir(), "repository")
	var repo repository.DriverRecord
	if zstd {
		repo, err = repository.OpenProfileWithCompression(repository.RepositoryProfileLocalZstdV1, repository.CompressionProfileZstdV1, repoRoot)
	} else {
		repo, err = repository.OpenDir(repoRoot)
	}
	if err != nil {
		t.Fatal(err)
	}
	identity, anchor, err := OpenSigningMaterial(t.TempDir(), testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	service = &Service{
		Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	plan, err := service.InspectIngest(ctx, source, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err = service.ApplyIngestPlanWithExecutionKey(ctx, plan, "sha256:relocated-closure-plan")
	if err != nil {
		t.Fatalf("signed ingest: %v", err)
	}
	var dirRepo *repository.Dir
	switch typed := repo.(type) {
	case *repository.Dir:
		dirRepo = typed
	case *repository.ZstdDir:
		dirRepo = typed.Dir
	default:
		t.Fatalf("unsupported repository driver %T", repo)
	}
	addClosureTestAttempt(t, signedPublicationFixture{service: service, store: store, repo: dirRepo, source: source}, result)
	if err := service.publishProcessorAttemptClosure(ctx, result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatalf("publish processor child: %v", err)
	}
	nodes, err := store.ListNamespaceSubtree(ctx, result.WorkspaceID, result.RootID, "")
	if err != nil {
		t.Fatal(err)
	}
	var subject string
	for _, node := range nodes {
		if node.Entry.ContentID != "" {
			subject = node.Entry.ID
			break
		}
	}
	if subject == "" {
		t.Fatal("file subject is unavailable")
	}
	docID, err := sqlite.NewStableID(sqlite.IDPrefixDescription)
	if err != nil {
		t.Fatal(err)
	}
	doc := sqlite.DescriptionDocument{
		ID: docID, WorkspaceID: result.WorkspaceID, SubjectRef: subject,
		Kind: sqlite.DescriptionUser, Language: "en", Body: "relocated description body", SourceRef: "user:relocation",
	}
	if err := store.InsertDescriptionDocument(ctx, &doc); err != nil {
		t.Fatal(err)
	}
	if err := service.PublishPortableFactClosure(ctx, result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatalf("publish portable fact successor: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	movedRoot = filepath.Join(t.TempDir(), "relocated")
	if err := os.Rename(repoRoot, movedRoot); err != nil {
		t.Fatal(err)
	}
	return service, movedRoot, catalogPath, result
}

// loadRelocatedAnchor writes the service anchor to a fresh path and reads it
// back so the reader always authenticates against independently loaded bytes.
func loadRelocatedAnchor(t *testing.T, service *Service) TrustAnchor {
	t.Helper()
	anchorPath := filepath.Join(t.TempDir(), "trust-anchor.json")
	if _, err := ExportTrustAnchor(*service.TrustAnchor, anchorPath); err != nil {
		t.Fatal(err)
	}
	anchor, err := LoadTrustAnchor(anchorPath)
	if err != nil {
		t.Fatal(err)
	}
	return anchor
}

// relocatedReader opens the moved repository with the clean-install reader
// (OpenProfileReadOnly + a separately loaded trust anchor).
func relocatedReader(t *testing.T, profile, movedRoot string, anchor TrustAnchor) *Service {
	t.Helper()
	repo, err := repository.OpenProfileReadOnly(profile, movedRoot)
	if err != nil {
		t.Fatalf("open relocated repository read-only: %v", err)
	}
	return &Service{Repo: repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
}

// recordRelocationPath maps a signed record digest to its on-disk path after
// relocation. Layout: <root>/recovery/<role-dir>/sha256/<p2>/<hex>.
func recordRelocationPath(t *testing.T, root string, role repository.RecordRole, digest string) string {
	t.Helper()
	var roleDir string
	switch role {
	case repository.RecordPreparedClosure:
		roleDir = "prepared"
	case repository.RecordPublicationCommit:
		roleDir = "commits"
	case repository.RecordProcessorAttemptClosure:
		roleDir = "processor-attempts"
	case repository.RecordPortableFactClosure:
		roleDir = "portable-facts"
	default:
		t.Fatalf("unsupported record role %q", role)
	}
	hexDigest, err := parseExactHexDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "recovery", roleDir, "sha256", hexDigest[:2], hexDigest)
}

// parseExactHexDigest strips a sha256: prefix and validates the hex form.
func parseExactHexDigest(contentID string) (string, error) {
	algorithm, payload, ok := bytes.Cut([]byte(contentID), []byte(":"))
	if !ok || string(algorithm) != "sha256" || len(payload) != 64 {
		return "", errors.New("invalid sha256 content id")
	}
	return string(payload), nil
}

// findLatestPortableFactClosure reads every portable-fact child through the
// given driver and returns the one with the highest sequence.
func findLatestPortableFactClosure(t *testing.T, driver repository.RecordDriver) PortableFactClosureEnvelope {
	t.Helper()
	digests := closureDigests(t, driver, repository.RecordPortableFactClosure)
	var latest PortableFactClosureEnvelope
	for _, digest := range digests {
		payload, err := readRecord(context.Background(), driver, repository.RecordPortableFactClosure, digest)
		if err != nil {
			t.Fatal(err)
		}
		var envelope PortableFactClosureEnvelope
		if err := decodeStrictRecord(payload, &envelope); err != nil {
			t.Fatal(err)
		}
		if latest.Closure.ClosureSequence == 0 || envelope.Closure.ClosureSequence > latest.Closure.ClosureSequence {
			latest = envelope
		}
	}
	if latest.Closure.ClosureSequence == 0 {
		t.Fatal("no portable fact closure is available")
	}
	return latest
}

func closureDigests(t *testing.T, driver repository.RecordDriver, role repository.RecordRole) []string {
	t.Helper()
	digests, err := driver.ListRecordDigests(context.Background(), role)
	if err != nil {
		t.Fatal(err)
	}
	return digests
}

func assertRelocatedClosureChain(t *testing.T, reader *Service, result IngestResult) {
	t.Helper()
	ctx := context.Background()
	listed, err := reader.ListSnapshots(ctx)
	if err != nil || len(listed) != 1 || listed[0].SnapshotRef != result.SnapshotRef {
		t.Fatalf("relocated snapshots = %+v, err=%v", listed, err)
	}
	if _, err := reader.Verify(ctx, result.SnapshotRef); err != nil {
		t.Fatalf("relocated verify: %v", err)
	}
	closures, err := reader.ListPortableFactClosures(ctx, result.SnapshotRef)
	if err != nil || len(closures) != 2 {
		t.Fatalf("relocated portable facts = %d, err=%v", len(closures), err)
	}
	if closures[1].Closure.PredecessorClosureDigest == "" {
		t.Fatalf("relocated portable fact successor lost its predecessor: %+v", closures[1].Closure)
	}
	attempts, err := reader.ListProcessorAttemptClosures(ctx, result.SnapshotRef)
	if err != nil || len(attempts) != 1 || attempts[0].Closure.AttemptCount != 1 {
		t.Fatalf("relocated processor attempts = %+v, err=%v", attempts, err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	plan, err := reader.InspectRestore(ctx, result.SnapshotRef, destination)
	if err != nil {
		t.Fatalf("relocated inspect restore: %v", err)
	}
	if plan.PublicationCommitDigest != result.PublicationCommitDigest {
		t.Fatalf("relocated restore plan commit = %q, want %q", plan.PublicationCommitDigest, result.PublicationCommitDigest)
	}
	if _, err := reader.ApplyRestorePlan(ctx, plan); err != nil {
		t.Fatalf("relocated apply restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "archive.bin"))
	if err != nil {
		t.Fatal(err)
	}
	expected := bytes.Repeat([]byte("relocated exact recovery bytes\n"), 64)
	if !bytes.Equal(got, expected) {
		t.Fatalf("relocated restored payload length = %d", len(got))
	}
}

func TestRelocatedRawRepositoryReproducesFullPortableClosureChain(t *testing.T) {
	service, movedRoot, _, result := relocateSizedRepository(t, false)
	anchor := loadRelocatedAnchor(t, service)
	reader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, movedRoot, anchor)
	assertRelocatedClosureChain(t, reader, result)
}

func TestRelocatedRawRepositoryCreatesNoReaderState(t *testing.T) {
	service, movedRoot, _, _ := relocateSizedRepository(t, false)
	before := snapshotRelocationFiles(t, movedRoot)
	anchor := loadRelocatedAnchor(t, service)
	_ = relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, movedRoot, anchor)
	after := snapshotRelocationFiles(t, movedRoot)
	if !sameStringSlices(before, after) {
		t.Fatalf("read-only relocation open created files:\nbefore=%v\nafter=%v", before, after)
	}
}

func TestRelocatedRawRepositoryCorruptCommitFailsClosed(t *testing.T) {
	service, movedRoot, _, _ := relocateSizedRepository(t, false)
	reader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, movedRoot, loadRelocatedAnchor(t, service))
	digests := closureDigests(t, reader.Repo.(repository.RecordDriver), repository.RecordPublicationCommit)
	if len(digests) != 1 {
		t.Fatalf("commit digests = %v", digests)
	}
	path := recordRelocationPath(t, movedRoot, repository.RecordPublicationCommit, digests[0])
	if !flipFileByte(t, path) {
		t.Fatal("tamper fixture did not modify commit record")
	}
	if _, err := reader.ListSnapshots(context.Background()); err == nil {
		t.Fatal("relocated corrupted commit was accepted")
	}
}

func TestRelocatedRawRepositoryCorruptPreparedFailsClosed(t *testing.T) {
	service, movedRoot, _, _ := relocateSizedRepository(t, false)
	reader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, movedRoot, loadRelocatedAnchor(t, service))
	digests := closureDigests(t, reader.Repo.(repository.RecordDriver), repository.RecordPreparedClosure)
	if len(digests) != 1 {
		t.Fatalf("prepared digests = %v", digests)
	}
	path := recordRelocationPath(t, movedRoot, repository.RecordPreparedClosure, digests[0])
	if !flipFileByte(t, path) {
		t.Fatal("tamper fixture did not modify prepared closure")
	}
	if _, err := reader.ListSnapshots(context.Background()); err == nil {
		t.Fatal("relocated corrupted prepared closure was accepted")
	}
}

func TestRelocatedRawRepositoryCorruptPortableFactFailsClosed(t *testing.T) {
	service, movedRoot, _, result := relocateSizedRepository(t, false)
	reader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, movedRoot, loadRelocatedAnchor(t, service))
	digests := closureDigests(t, reader.Repo.(repository.RecordDriver), repository.RecordPortableFactClosure)
	if len(digests) != 2 {
		t.Fatalf("portable fact digests = %v", digests)
	}
	path := recordRelocationPath(t, movedRoot, repository.RecordPortableFactClosure, digests[0])
	if !flipFileByte(t, path) {
		t.Fatal("tamper fixture did not modify portable fact record")
	}
	if _, err := reader.ListPortableFactClosures(context.Background(), result.SnapshotRef); err == nil {
		t.Fatal("relocated corrupted portable fact closure was accepted")
	}
	listed, err := reader.ListSnapshots(context.Background())
	if err != nil || len(listed) != 1 || listed[0].SnapshotRef != result.SnapshotRef {
		t.Fatalf("corrupted portable fact record affected exact discovery: %+v, err=%v", listed, err)
	}
}

func TestRelocatedRawRepositoryMissingAttachmentKeepsExactRestoreUsable(t *testing.T) {
	service, movedRoot, _, result := relocateSizedRepository(t, false)
	reader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, movedRoot, loadRelocatedAnchor(t, service))
	latest := findLatestPortableFactClosure(t, reader.Repo.(repository.RecordDriver))
	var bundle portableFactBundle
	if err := decodeStrictRecord(latest.Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Attachments) == 0 {
		t.Fatal("relocated fixture has no portable fact attachment")
	}
	hexDigest, err := parseExactHexDigest(bundle.Attachments[0].ContentID)
	if err != nil {
		t.Fatal(err)
	}
	blobPath := filepath.Join(movedRoot, "blobs", "sha256", hexDigest[:2], hexDigest)
	if err := os.Remove(blobPath); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := reader.ListPortableFactClosures(ctx, result.SnapshotRef); err == nil {
		t.Fatal("missing portable fact attachment was accepted")
	}
	listed, err := reader.ListSnapshots(ctx)
	if err != nil || len(listed) != 1 || listed[0].SnapshotRef != result.SnapshotRef {
		t.Fatalf("missing attachment affected exact discovery: %+v, err=%v", listed, err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	plan, err := reader.InspectRestore(ctx, result.SnapshotRef, destination)
	if err != nil {
		t.Fatalf("inspect restore with missing attachment: %v", err)
	}
	if _, err := reader.ApplyRestorePlan(ctx, plan); err != nil {
		t.Fatalf("exact restore gated by missing portable fact attachment: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "archive.bin"))
	if err != nil || !bytes.Equal(got, bytes.Repeat([]byte("relocated exact recovery bytes\n"), 64)) {
		t.Fatalf("restored payload = %d bytes, err=%v", len(got), err)
	}
}

func TestRelocatedZstdRepositoryReproducesFullPortableClosureChain(t *testing.T) {
	service, movedRoot, _, result := relocateSizedRepository(t, true)
	anchor := loadRelocatedAnchor(t, service)
	reader := relocatedReader(t, repository.RepositoryProfileLocalZstdV1, movedRoot, anchor)
	assertRelocatedClosureChain(t, reader, result)
}

func TestRelocatedZstdRepositoryCorruptionFailsClosed(t *testing.T) {
	service, movedRoot, _, result := relocateSizedRepository(t, true)
	reader := relocatedReader(t, repository.RepositoryProfileLocalZstdV1, movedRoot, loadRelocatedAnchor(t, service))
	digests := closureDigests(t, reader.Repo.(repository.RecordDriver), repository.RecordPortableFactClosure)
	if len(digests) != 2 {
		t.Fatalf("zstd portable fact digests = %v", digests)
	}
	path := recordRelocationPath(t, movedRoot, repository.RecordPortableFactClosure, digests[0])
	if !flipFileByte(t, path) {
		t.Fatal("tamper fixture did not modify zstd portable fact record")
	}
	if _, err := reader.ListPortableFactClosures(context.Background(), result.SnapshotRef); err == nil {
		t.Fatal("relocated corrupted zstd portable fact closure was accepted")
	}
}

func TestRelocatedZstdRepositoryMissingAttachmentKeepsExactRestoreUsable(t *testing.T) {
	service, movedRoot, _, result := relocateSizedRepository(t, true)
	reader := relocatedReader(t, repository.RepositoryProfileLocalZstdV1, movedRoot, loadRelocatedAnchor(t, service))
	latest := findLatestPortableFactClosure(t, reader.Repo.(repository.RecordDriver))
	var bundle portableFactBundle
	if err := decodeStrictRecord(latest.Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Attachments) == 0 {
		t.Fatal("zstd relocated fixture has no portable fact attachment")
	}
	hexDigest, err := parseExactHexDigest(bundle.Attachments[0].ContentID)
	if err != nil {
		t.Fatal(err)
	}
	blobPath := filepath.Join(movedRoot, "blobs", "sha256", hexDigest[:2], hexDigest)
	if err := os.Remove(blobPath); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := reader.ListPortableFactClosures(ctx, result.SnapshotRef); err == nil {
		t.Fatal("missing zstd portable fact attachment was accepted")
	}
	destination := filepath.Join(t.TempDir(), "restored")
	plan, err := reader.InspectRestore(ctx, result.SnapshotRef, destination)
	if err != nil {
		t.Fatalf("inspect restore with missing zstd attachment: %v", err)
	}
	if _, err := reader.ApplyRestorePlan(ctx, plan); err != nil {
		t.Fatalf("exact restore gated by missing zstd attachment: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "archive.bin"))
	if err != nil || !bytes.Equal(got, bytes.Repeat([]byte("relocated exact recovery bytes\n"), 64)) {
		t.Fatalf("zstd restored payload = %d bytes, err=%v", len(got), err)
	}
}

// TestRelocatedPortableClosureChainValidatesAgainstRepository proves the
// relocated repository still satisfies repository-side reader-dependency and
// attachment readback checks for a v2 recovery reference.
func TestRelocatedPortableClosureChainValidatesAgainstRepository(t *testing.T) {
	service, movedRoot, _, result := relocateSizedRepository(t, false)
	reader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, movedRoot, loadRelocatedAnchor(t, service))
	reference, err := reader.BuildRecoveryReference(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatalf("build recovery reference after relocation: %v", err)
	}
	if reference.FactHealth != RecoveryFactHealthComplete || len(reference.PortableFactClosures) != 2 {
		t.Fatalf("relocated reference health = %q with %d closures", reference.FactHealth, len(reference.PortableFactClosures))
	}
	if err := reference.ValidateAgainstRepository(context.Background(), reader.Repo, *reader.TrustAnchor); err != nil {
		t.Fatalf("validate relocated recovery reference against repository: %v", err)
	}
}

// snapshotRelocationFiles records the relative paths of every regular file
// under a relocated repository so a read-only open can prove it created none.
func snapshotRelocationFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func sameStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// flipFileByte flips one byte in a record file and reports whether the bytes
// actually changed so a stale tamper fixture cannot silently pass.
func flipFileByte(t *testing.T, path string) bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	original, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(original) == 0 {
		t.Fatalf("read record for tamper: read=%v close=%v length=%d", readErr, closeErr, len(original))
	}
	flipped := append([]byte(nil), original...)
	flipped[len(flipped)/2] ^= 0x01
	if err := os.WriteFile(path, flipped, 0o600); err != nil {
		t.Fatal(err)
	}
	recheck, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return !bytes.Equal(recheck, original)
}
