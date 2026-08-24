package exact

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type gatedChildRepository struct {
	*repository.Dir
	role    repository.RecordRole
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *gatedChildRepository) PlaceRecord(ctx context.Context, role repository.RecordRole, body io.Reader) (repository.RecordReceipt, error) {
	if role == r.role {
		r.once.Do(func() { close(r.entered) })
		select {
		case <-r.release:
		case <-ctx.Done():
			return repository.RecordReceipt{}, ctx.Err()
		}
	}
	return r.Dir.PlaceRecord(ctx, role, body)
}

func newChildService(fixture signedPublicationFixture, repo repository.Driver) *Service {
	return &Service{
		Store: fixture.store, Repo: repo,
		SigningIdentity: fixture.service.SigningIdentity, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
}

func TestProcessorAttemptClosureAdvancesAfterCrossServiceChangedBundle(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "gated-attempt.txt", []byte("gated attempt closure"))
	result := fixture.ingest(t, "sha256:gated-attempt-plan")
	addClosureTestAttempt(t, fixture, result)
	gate := &gatedChildRepository{Dir: fixture.repo, role: repository.RecordProcessorAttemptClosure, entered: make(chan struct{}), release: make(chan struct{})}
	first := newChildService(fixture, gate)
	second := newChildService(fixture, fixture.repo)
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- first.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	}()
	select {
	case <-gate.entered:
	case err := <-firstErr:
		t.Fatalf("first processor publisher exited before placement: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first processor publisher did not reach gated placement")
	}
	addClosureTestAttempt(t, fixture, result)
	secondErr := make(chan error, 1)
	go func() {
		secondErr <- second.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	}()
	close(gate.release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first processor publisher: %v", err)
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("second processor publisher: %v", err)
	}
	closures, err := first.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 2 || closures[0].Closure.ClosureSequence != 1 || closures[1].Closure.ClosureSequence != 2 || closures[1].Closure.AttemptCount != 2 {
		t.Fatalf("processor closure chain = %d, err=%v", len(closures), err)
	}
}

func TestPortableFactClosureAdvancesSequenceAfterCrossServiceRace(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "gated-facts.txt", []byte("gated portable facts"))
	result := fixture.ingest(t, "sha256:gated-facts-plan")
	insertRaceDescription(t, fixture, result, "portable first successor description")
	parent, err := fixture.service.committedPublicationForSnapshot(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	bundle, _, err := fixture.service.buildPortableFactBundle(context.Background(), result.WorkspaceID, parent.Manifest, fixture.repo.RepositoryIdentity())
	if err != nil {
		t.Fatal(err)
	}
	bundleBytes, err := CanonicalJSON(bundle)
	if err != nil {
		t.Fatal(err)
	}
	closures, err := fixture.service.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(closures) != 1 || DigestBytes(bundleBytes) == closures[0].Closure.BundleDigest {
		t.Fatalf("portable race did not create a changed successor bundle: closures=%+v", closures)
	}
	gate := &gatedChildRepository{Dir: fixture.repo, role: repository.RecordPortableFactClosure, entered: make(chan struct{}), release: make(chan struct{})}
	first := newChildService(fixture, gate)
	second := newChildService(fixture, fixture.repo)
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- first.publishPortableFactClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	}()
	select {
	case <-gate.entered:
	case err := <-firstErr:
		t.Fatalf("first portable publisher exited before placement: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first portable publisher did not reach gated placement")
	}
	insertRaceDescription(t, fixture, result, "portable second successor description")
	secondErr := make(chan error, 1)
	go func() {
		secondErr <- second.publishPortableFactClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	}()
	close(gate.release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first portable publisher: %v", err)
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("second portable publisher: %v", err)
	}
	closures, err = first.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(closures) != 3 || closures[0].Closure.ClosureSequence != 1 || closures[1].Closure.ClosureSequence != 2 || closures[2].Closure.ClosureSequence != 3 {
		t.Fatalf("portable fact closure chain = %+v, want sequence 1, 2, then 3", closures)
	}
}

func TestProcessorAttemptClosureReaderSeesOnlyCompleteChild(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "gated-attempt-reader.txt", []byte("gated attempt reader"))
	result := fixture.ingest(t, "sha256:gated-attempt-reader-plan")
	addClosureTestAttempt(t, fixture, result)
	gate := &gatedChildRepository{Dir: fixture.repo, role: repository.RecordProcessorAttemptClosure, entered: make(chan struct{}), release: make(chan struct{})}
	writer := newChildService(fixture, gate)
	reader := newChildService(fixture, fixture.repo)
	writerErr := make(chan error, 1)
	go func() {
		writerErr <- writer.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	}()
	waitForGatedChildPlacement(t, gate, writerErr)

	before, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatalf("processor reader observed %d children before placement, want 0", len(before))
	}

	close(gate.release)
	if err := <-writerErr; err != nil {
		t.Fatalf("processor writer: %v", err)
	}
	after, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("processor reader observed %d children after placement, want 1", len(after))
	}
}

func TestPortableFactClosureReaderSeesOldThenCompleteSuccessor(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "gated-fact-reader.txt", []byte("gated fact reader"))
	result := fixture.ingest(t, "sha256:gated-fact-reader-plan")
	insertRaceDescription(t, fixture, result, "portable reader successor")
	gate := &gatedChildRepository{Dir: fixture.repo, role: repository.RecordPortableFactClosure, entered: make(chan struct{}), release: make(chan struct{})}
	writer := newChildService(fixture, gate)
	reader := newChildService(fixture, fixture.repo)
	writerErr := make(chan error, 1)
	go func() {
		writerErr <- writer.publishPortableFactClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	}()
	waitForGatedChildPlacement(t, gate, writerErr)

	before, err := reader.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].Closure.ClosureSequence != 1 {
		t.Fatalf("portable reader observed incomplete pre-placement chain: %+v", before)
	}

	close(gate.release)
	if err := <-writerErr; err != nil {
		t.Fatalf("portable writer: %v", err)
	}
	after, err := reader.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 || after[0].Closure.ClosureSequence != 1 || after[1].Closure.ClosureSequence != 2 {
		t.Fatalf("portable reader observed incomplete post-placement chain: %+v", after)
	}
}

func waitForGatedChildPlacement(t *testing.T, gate *gatedChildRepository, writerErr <-chan error) {
	t.Helper()
	select {
	case <-gate.entered:
	case err := <-writerErr:
		t.Fatalf("child writer exited before placement: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("child writer did not reach gated placement")
	}
}

func insertRaceDescription(t *testing.T, fixture signedPublicationFixture, result IngestResult, body string) {
	t.Helper()
	nodes, err := fixture.store.ListNamespaceSubtree(context.Background(), result.WorkspaceID, result.RootID, "")
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
		t.Fatal("portable fact race subject is unavailable")
	}
	docID, err := sqlite.NewStableID(sqlite.IDPrefixDescription)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := fixture.store.InsertDescriptionDocument(context.Background(), &sqlite.DescriptionDocument{
		ID: docID, WorkspaceID: result.WorkspaceID, SubjectRef: subject, Kind: sqlite.DescriptionUser,
		Language: "en", Body: body, BodyDigest: DigestBytes([]byte(body)), SourceRef: "race:test",
		ConfigDigest: result.ConfigDigest,
		Visibility:   "PUBLIC", Accepted: true, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}
