package exact

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

// errorBeforeRecordPlaceRepo models an outcome that is unknown to the caller
// because the repository rejected the placement before exposing whether a
// durable child was created.
type errorBeforeRecordPlaceRepo struct {
	*repository.Dir
	failRole repository.RecordRole
	failed   bool
}

func (r *errorBeforeRecordPlaceRepo) PlaceRecord(ctx context.Context, role repository.RecordRole, body io.Reader) (repository.RecordReceipt, error) {
	if role == r.failRole && !r.failed {
		r.failed = true
		return repository.RecordReceipt{}, errors.New("child placement outcome unavailable")
	}
	return r.Dir.PlaceRecord(ctx, role, body)
}

func assertUnknownChildOutcome(t *testing.T, err error, role repository.RecordRole) {
	t.Helper()
	if !errors.Is(err, ErrUnknownExternalOutcome) || !errors.Is(err, ErrNeedsReconciliation) {
		t.Fatalf("child placement error = %v, want typed unknown outcome", err)
	}
	var outcome *PublicationOutcomeError
	if !errors.As(err, &outcome) || outcome.Role != role {
		t.Fatalf("typed child outcome = %#v, want role %s", err, role)
	}
}

func TestProcessorAttemptClosurePlacementResponseLossReconciles(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "child-reconcile-attempt.txt", []byte("child reconcile attempt"))
	result := fixture.ingest(t, "sha256:child-reconcile-attempt-plan")
	addClosureTestAttempt(t, fixture, result)
	fixture.service.Repo = &errorAfterRecordPlaceRepo{Dir: fixture.repo, failRole: repository.RecordProcessorAttemptClosure}

	if err := fixture.service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatalf("processor child response loss was not reconciled: %v", err)
	}
	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	closures, err := reader.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 1 {
		t.Fatalf("processor child closures = %d, err=%v", len(closures), err)
	}
}

func TestProcessorAttemptClosurePlacementUnknownIsTyped(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "child-unknown-attempt.txt", []byte("child unknown attempt"))
	result := fixture.ingest(t, "sha256:child-unknown-attempt-plan")
	addClosureTestAttempt(t, fixture, result)
	fixture.service.Repo = &errorBeforeRecordPlaceRepo{Dir: fixture.repo, failRole: repository.RecordProcessorAttemptClosure}

	err := fixture.service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	assertUnknownChildOutcome(t, err, repository.RecordProcessorAttemptClosure)
}

func TestPortableFactClosurePlacementResponseLossReconciles(t *testing.T) {
	evidence := newPortableEvidenceFixture(t)
	insertRaceDescription(t, evidence.fixture, evidence.result, "child reconcile portable successor")
	evidence.fixture.service.Repo = &errorAfterRecordPlaceRepo{Dir: evidence.fixture.repo, failRole: repository.RecordPortableFactClosure}

	if err := evidence.fixture.service.PublishPortableFactClosure(context.Background(), evidence.result.WorkspaceID, evidence.result.SnapshotRef, evidence.result.PublicationCommitDigest); err != nil {
		t.Fatalf("portable child response loss was not reconciled: %v", err)
	}
	reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	closures, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef)
	if err != nil || len(closures) != 3 {
		t.Fatalf("portable child closures = %d, err=%v", len(closures), err)
	}
}

func TestPortableFactClosurePlacementUnknownIsTyped(t *testing.T) {
	evidence := newPortableEvidenceFixture(t)
	insertRaceDescription(t, evidence.fixture, evidence.result, "child unknown portable successor")
	evidence.fixture.service.Repo = &errorBeforeRecordPlaceRepo{Dir: evidence.fixture.repo, failRole: repository.RecordPortableFactClosure}

	err := evidence.fixture.service.PublishPortableFactClosure(context.Background(), evidence.result.WorkspaceID, evidence.result.SnapshotRef, evidence.result.PublicationCommitDigest)
	assertUnknownChildOutcome(t, err, repository.RecordPortableFactClosure)
}
