package exact

import (
	"context"
	"io"
	"strings"
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

func TestProcessorAttemptClosureRejectsCrossServiceChangedBundle(t *testing.T) {
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
	if err := <-secondErr; err == nil || !strings.Contains(err.Error(), "conflicting processor attempt closure") {
		t.Fatalf("second processor publisher = %v, want changed-bundle conflict", err)
	}
	closures, err := first.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 1 {
		t.Fatalf("processor closure chain = %d, err=%v", len(closures), err)
	}
}

func TestPortableFactClosureAdvancesSequenceAfterCrossServiceRace(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "gated-facts.txt", []byte("gated portable facts"))
	result := fixture.ingest(t, "sha256:gated-facts-plan")
	insertRaceDescription(t, fixture, result, "portable first successor description")
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
	closures, err := first.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(closures) != 3 || closures[0].Closure.ClosureSequence != 1 || closures[1].Closure.ClosureSequence != 2 || closures[2].Closure.ClosureSequence != 3 {
		t.Fatalf("portable fact closure chain = %+v, want sequence 1, 2, then 3", closures)
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
		Visibility: "PUBLIC", Accepted: true, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}
