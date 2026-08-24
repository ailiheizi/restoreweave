package exact

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// controlledClock is a minimal mutable clock so tests can drive lease expiry
// deterministically. Both the service and the store must share one instance.
type controlledClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *controlledClock) nowFn() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *controlledClock) set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type observingPublicationFencer struct {
	PublicationFencer
	renewed chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

type releaseFailingPublicationFencer struct {
	PublicationFencer
}

func (f *releaseFailingPublicationFencer) Release(context.Context, string, string, string, int64, time.Time) error {
	return errors.New("injected publication lease release failure")
}

func (f *observingPublicationFencer) Acquire(ctx context.Context, domain, owner, leaseToken string, now, until time.Time) (int64, error) {
	token, err := f.PublicationFencer.Acquire(ctx, domain, owner, leaseToken, now, until)
	if err == nil && f.calls.Add(1) > 1 {
		f.once.Do(func() { close(f.renewed) })
	}
	return token, err
}

func TestPublicationFenceRenewsDuringLongPlacement(t *testing.T) {
	previousInterval := publicationFenceRenewInterval
	publicationFenceRenewInterval = 50 * time.Millisecond
	t.Cleanup(func() { publicationFenceRenewInterval = previousInterval })

	fixture := newSignedPublicationFixture(t, "long-fenced-child.txt", []byte("long fenced child"))
	result := fixture.ingest(t, "sha256:long-fenced-child-plan")
	addClosureTestAttempt(t, fixture, result)
	clock := &controlledClock{now: time.Now().UTC().Add(time.Second)}
	base := NewPublicationFencer(fixture.store, clock.nowFn)
	fencer := &observingPublicationFencer{PublicationFencer: base, renewed: make(chan struct{})}
	gate := &gatedChildRepository{Dir: fixture.repo, role: repository.RecordProcessorAttemptClosure, entered: make(chan struct{}), release: make(chan struct{})}
	service := &Service{
		Store: fixture.store, Repo: gate, SigningIdentity: fixture.service.SigningIdentity,
		TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true, PublicationFencer: fencer, Now: clock.nowFn,
	}
	resultErr := make(chan error, 1)
	go func() {
		resultErr <- service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	}()
	select {
	case <-gate.entered:
	case err := <-resultErr:
		t.Fatalf("publisher exited before gated placement: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not reach gated placement")
	}
	clock.set(clock.nowFn().Add(4 * time.Minute))
	select {
	case <-fencer.renewed:
	case <-time.After(5 * time.Second):
		t.Fatal("publication lease was not renewed")
	}
	// The original five-minute lease is expired, but the renewal above has
	// extended it through this placement.
	clock.set(clock.nowFn().Add(2 * time.Minute))
	close(gate.release)
	if err := <-resultErr; err != nil {
		t.Fatalf("long placement lost its renewed lease: %v", err)
	}
}

func TestRootPublicationReleaseFailureNeedsReconciliation(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "release-failure-root.txt", []byte("release failure root"))
	manifest, err := writeManifest(fixture.repo.Root(), Manifest{
		Schema: SnapshotSchemaV1, SnapshotRef: "release-failure-root-snapshot",
		Binding: capture.BindingRecord{Schema: capture.SchemaBindingV1, Profile: capture.ProfileLocalTree, CaptureMode: "ROOTED_FD", BoundAt: time.Unix(1, 0).UTC()},
		Entries: []ManifestEntry{},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := NewPublicationFencer(fixture.store, fixture.service.Now)
	fixture.service.PublicationFencer = &releaseFailingPublicationFencer{PublicationFencer: base}
	_, err = fixture.service.publishRecoveryClosure(context.Background(), adopted{
		snapshotRef: manifest.SnapshotRef, publicationID: "publication:release-failure-root",
	}, manifest, placedSet{}, "sha256:release-failure-root-plan", "sha256:capture", "sha256:policy")
	if !errors.Is(err, ErrNeedsReconciliation) || !errors.Is(err, ErrPublicationLeaseRelease) {
		t.Fatalf("root release failure = %v, want reconciliation classification", err)
	}
	commits, err := fixture.service.committedPublications(context.Background())
	if err != nil || len(commits) != 1 {
		t.Fatalf("root publication count = %d, err=%v; want one commit", len(commits), err)
	}
}

func TestProcessorAndPortableReleaseFailuresNeedReconciliation(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "release-failure-children.txt", []byte("release failure children"))
	result := fixture.ingest(t, "sha256:release-failure-children-plan")
	base := NewPublicationFencer(fixture.store, fixture.service.Now)
	fixture.service.PublicationFencer = &releaseFailingPublicationFencer{PublicationFencer: base}
	addClosureTestAttempt(t, fixture, result)
	err := fixture.service.publishProcessorAttemptClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	if !errors.Is(err, ErrNeedsReconciliation) || !errors.Is(err, ErrPublicationLeaseRelease) {
		t.Fatalf("processor release failure = %v, want reconciliation classification", err)
	}
	processorClosures, err := fixture.service.ListProcessorAttemptClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(processorClosures) != 1 {
		t.Fatalf("processor closure count = %d, err=%v; want one child", len(processorClosures), err)
	}

	evidence := newPortableEvidenceFixture(t)
	insertRaceDescription(t, evidence.fixture, evidence.result, "release failure portable successor")
	base = NewPublicationFencer(evidence.fixture.store, evidence.fixture.service.Now)
	evidence.fixture.service.PublicationFencer = &releaseFailingPublicationFencer{PublicationFencer: base}
	err = evidence.fixture.service.PublishPortableFactClosure(context.Background(), evidence.result.WorkspaceID, evidence.result.SnapshotRef, evidence.result.PublicationCommitDigest)
	if !errors.Is(err, ErrNeedsReconciliation) || !errors.Is(err, ErrPublicationLeaseRelease) {
		t.Fatalf("portable release failure = %v, want reconciliation classification", err)
	}
	portableClosures, err := evidence.fixture.service.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef)
	if err != nil || len(portableClosures) != 3 {
		t.Fatalf("portable closure count = %d, err=%v; want one successor", len(portableClosures), err)
	}
}

func TestPublicationFenceSerializesConcurrentProcesses(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	clock := &controlledClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	first, err := sqlite.Open(ctx, path, sqlite.Options{Now: clock.nowFn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := sqlite.Open(ctx, path, sqlite.Options{Now: clock.nowFn, BusyTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	firstService := &Service{
		Store: first, PublicationDomain: "workspace:default",
		PublicationFencer: NewPublicationFencer(first, clock.nowFn), Now: clock.nowFn,
	}
	secondService := &Service{
		Store: second, PublicationDomain: "workspace:default",
		PublicationFencer: NewPublicationFencer(second, clock.nowFn), Now: clock.nowFn,
	}

	leaseA, err := firstService.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatalf("first process acquire: %v", err)
	}
	if leaseA.token != 1 {
		t.Fatalf("first fencing token = %d, want 1", leaseA.token)
	}

	// A second, independent "process" cannot hold the fence while the first
	// lease is active.
	if _, err := secondService.acquirePublicationFence(ctx); !errors.Is(err, sqlite.ErrConflict) {
		t.Fatalf("competing acquire = %v, want ErrConflict", err)
	}
	if err := secondService.validatePublicationFence(ctx, publicationLease{token: 1, coordinationToken: 1, leaseToken: "foreign"}); !errors.Is(err, sqlite.ErrConflict) {
		t.Fatalf("foreign validation = %v, want ErrConflict", err)
	}
	if err := leaseA.release(); err != nil {
		t.Fatalf("release first lease: %v", err)
	}
}

func TestPublicationFenceTokenAppearsInSignedRecords(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "fence.txt", []byte("fenced publication"))
	fixture.ingest(t, "sha256:fence-plan")
	publications, err := fixture.service.committedPublications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(publications) != 1 {
		t.Fatalf("committed publications = %d, want 1", len(publications))
	}
	publication := publications[0]
	if publication.Commit.FenceToken != 1 || publication.Prepared.Prepared.FenceToken != 1 {
		t.Fatalf("fence tokens = commit %d prepared %d, want 1/1",
			publication.Commit.FenceToken, publication.Prepared.Prepared.FenceToken)
	}
	if publication.Commit.FenceToken != publication.Prepared.Prepared.FenceToken {
		t.Fatal("prepared closure and commit carry different fence tokens")
	}

	// The in-memory fence row must have been released after publication.
	if err := fixture.service.fencer().Validate(context.Background(), fixture.service.publicationFenceDomain(), fixture.service.publicationOwner(), "stale", 1, fixture.service.now()); !errors.Is(err, sqlite.ErrConflict) {
		t.Fatalf("post-publication fence validation = %v, want ErrConflict", err)
	}
}

func TestPublicationFenceExpiryAllowsMonotonicTakeover(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	clock := &controlledClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	first, err := sqlite.Open(ctx, path, sqlite.Options{Now: clock.nowFn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := sqlite.Open(ctx, path, sqlite.Options{Now: clock.nowFn, BusyTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	firstService := &Service{
		Store: first, PublicationDomain: "workspace:default",
		PublicationFencer: NewPublicationFencer(first, clock.nowFn), Now: clock.nowFn,
	}
	secondService := &Service{
		Store: second, PublicationDomain: "workspace:default",
		PublicationFencer: NewPublicationFencer(second, clock.nowFn), Now: clock.nowFn,
	}

	leaseA, err := firstService.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if leaseA.token != 1 {
		t.Fatalf("initial token = %d, want 1", leaseA.token)
	}

	// Advance past the 5-minute lease so process B can take over.
	clock.set(clock.nowFn().Add(DefaultPublicationFenceTTL + time.Second))
	leaseB, err := secondService.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatalf("takeover after expiry: %v", err)
	}
	if leaseB.token != 2 {
		t.Fatalf("takeover token = %d, want 2", leaseB.token)
	}

	// The old lease's token is rejected after the takeover.
	if err := firstService.validatePublicationFence(ctx, leaseA); !errors.Is(err, sqlite.ErrConflict) {
		t.Fatalf("stale lease validation = %v, want ErrConflict", err)
	}
	if err := leaseA.release(); !errors.Is(err, sqlite.ErrConflict) {
		t.Fatalf("stale lease release = %v, want ErrConflict", err)
	}
	// The new owner validates and releases cleanly.
	if err := secondService.validatePublicationFence(ctx, leaseB); err != nil {
		t.Fatalf("current lease validation: %v", err)
	}
	if err := leaseB.release(); err != nil {
		t.Fatalf("current lease release: %v", err)
	}
}

func TestPublicationFenceReleaseSurvivesParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	store, err := sqlite.Open(ctx, path, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := &Service{
		Store: store, PublicationDomain: "workspace:cancel-release",
		PublicationFencer: NewPublicationFencer(store, nil),
	}
	lease, err := service.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := lease.release(); err != nil {
		t.Fatalf("release after parent cancellation: %v", err)
	}
	if err := service.fencer().Validate(context.Background(), service.publicationFenceDomain(), service.publicationOwner(), lease.leaseToken, lease.coordinationToken, service.now()); !errors.Is(err, sqlite.ErrConflict) {
		t.Fatalf("released lease validation = %v, want ErrConflict", err)
	}
}

func TestSignedPublicationFenceDoesNotReuseAbandonedLeaseCounter(t *testing.T) {
	ctx := context.Background()
	clock := &controlledClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{Now: clock.nowFn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	identity := testIdentity(t)
	anchor, err := NewTrustAnchor(identity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	fencer := NewPublicationFencer(store, clock.nowFn)
	holder := &Service{Store: store, Repo: repo, PublicationDomain: testPublicationDomain, PublicationFencer: fencer, Now: clock.nowFn}
	first, err := holder.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.release(); err != nil {
		t.Fatal(err)
	}
	clock.set(clock.nowFn().Add(DefaultPublicationFenceTTL + time.Second))
	second, err := holder.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.coordinationToken != 2 {
		t.Fatalf("abandoned lease counter = %d, want 2", second.coordinationToken)
	}
	if err := second.release(); err != nil {
		t.Fatal(err)
	}

	manifest, err := writeManifest(repo.Root(), Manifest{
		Schema: SnapshotSchemaV1, SnapshotRef: "abandoned-lease-snapshot",
		Binding: capture.BindingRecord{Schema: capture.SchemaBindingV1, Profile: capture.ProfileLocalTree, CaptureMode: "ROOTED_FD", BoundAt: time.Unix(1, 0).UTC()},
		Entries: []ManifestEntry{},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: store, Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
		PublicationFencer: fencer, Now: clock.nowFn}
	publication, err := service.publishRecoveryClosure(ctx, adopted{
		snapshotRef: manifest.SnapshotRef, publicationID: "publication:abandoned-lease",
	}, manifest, placedSet{}, "sha256:abandoned-lease-plan", "sha256:abandoned-lease-capture", "sha256:abandoned-lease-policy")
	if err != nil {
		t.Fatalf("publish after abandoned lease: %v", err)
	}
	commits, err := listCommitMarkers(ctx, repo, anchor, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].CommitDigest != publication.CommitDigest || commits[0].Commit.FenceToken != 3 {
		t.Fatalf("publication fence after abandoned lease = %+v, want token 3", commits)
	}
}

func TestPublicationFenceFallbackWithoutStorePublishesTokenOne(t *testing.T) {
	ctx := context.Background()
	identity := testIdentity(t)
	anchor, err := NewTrustAnchor(identity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	manifest, err := writeManifest(repo.Root(), Manifest{
		Schema: SnapshotSchemaV1, SnapshotRef: "fence-fallback",
		Binding: capture.BindingRecord{Schema: capture.SchemaBindingV1, Profile: capture.ProfileLocalTree, CaptureMode: "ROOTED_FD", BoundAt: time.Unix(1, 0).UTC()},
		Entries: []ManifestEntry{},
	})
	if err != nil {
		t.Fatal(err)
	}
	adopted := adopted{snapshotRef: manifest.SnapshotRef, publicationID: "pub_fence_fallback"}
	result, err := service.publishRecoveryClosure(ctx, adopted, manifest, placedSet{}, "sha256:fence-fallback-plan", "sha256:capture", "sha256:policy")
	if err != nil {
		t.Fatalf("catalog-free publication: %v", err)
	}
	if result.PreparedDigest == "" || result.CommitDigest == "" || result.Generation != 1 {
		t.Fatalf("publication evidence = %+v", result)
	}
	driver := repo
	markers, err := listCommitMarkers(ctx, driver, anchor, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 1 || markers[0].Commit.FenceToken != FallbackFenceToken {
		t.Fatalf("commit markers = %+v", markers)
	}
	preparedBytes, err := readRecord(ctx, driver, repository.RecordPreparedClosure, markers[0].Commit.PreparedObjectDigest)
	if err != nil {
		t.Fatal(err)
	}
	var envelope PreparedClosureEnvelope
	if err := decodeStrictRecord(preparedBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Prepared.FenceToken != FallbackFenceToken {
		t.Fatalf("prepared closure fence token = %d, want %d", envelope.Prepared.FenceToken, FallbackFenceToken)
	}
}

func TestPublicationFenceAcquireFailureFailsClosed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	clock := &controlledClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	store, err := sqlite.Open(ctx, path, sqlite.Options{Now: clock.nowFn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	holder := &Service{
		Store: store, PublicationDomain: "workspace:default",
		PublicationFencer: NewPublicationFencer(store, clock.nowFn), Now: clock.nowFn,
	}
	if _, err := holder.acquirePublicationFence(ctx); err != nil {
		t.Fatal(err)
	}
	// A competing service cannot acquire while the holder's lease is active;
	// publication must fail closed rather than proceed without a fence.
	competing := &Service{
		Store: store, PublicationDomain: "workspace:default",
		PublicationFencer: NewPublicationFencer(store, clock.nowFn), Now: clock.nowFn,
	}
	if _, err := competing.acquirePublicationFence(ctx); !errors.Is(err, sqlite.ErrConflict) {
		t.Fatalf("competing acquire = %v, want ErrConflict", err)
	}
}

func TestPublicationFencerAutoWiresFromStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	clock := &controlledClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	store, err := sqlite.Open(ctx, path, sqlite.Options{Now: clock.nowFn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	service := &Service{Store: store, PublicationDomain: "workspace:default", Now: clock.nowFn}
	if service.fencer() == nil {
		t.Fatal("store-backed service did not auto-wire a fencer")
	}
	lease, err := service.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if lease.token != 1 {
		t.Fatalf("auto-wired fence token = %d, want 1", lease.token)
	}
	if err := lease.release(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationFenceIsRepositoryScoped(t *testing.T) {
	ctx := context.Background()
	clock := &controlledClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{Now: clock.nowFn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repoA, err := repository.OpenDir(filepath.Join(t.TempDir(), "repo-a"))
	if err != nil {
		t.Fatal(err)
	}
	repoB, err := repository.OpenDir(filepath.Join(t.TempDir(), "repo-b"))
	if err != nil {
		t.Fatal(err)
	}
	first := &Service{Store: store, Repo: repoA, PublicationDomain: "workspace:default", Now: clock.nowFn}
	second := &Service{Store: store, Repo: repoB, PublicationDomain: "workspace:default", Now: clock.nowFn}
	leaseA, err := first.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	leaseB, err := second.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatalf("different repository was incorrectly fenced out: %v", err)
	}
	if leaseA.token != 1 || leaseB.token != 1 {
		t.Fatalf("repository-scoped tokens = %d/%d, want 1/1", leaseA.token, leaseB.token)
	}
	if err := leaseA.release(); err != nil {
		t.Fatal(err)
	}
	if err := leaseB.release(); err != nil {
		t.Fatal(err)
	}
}

func TestSignedPublicationWithoutRepositoryLockOrFencerFailsClosed(t *testing.T) {
	identity := testIdentity(t)
	anchor, err := NewTrustAnchor(identity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		Repo: repository.NewMemory(), SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	if _, err := service.acquirePublicationFence(context.Background()); err == nil {
		t.Fatal("signed publication acquired a fallback authority without lock or fencer")
	}
}

func TestPublicationFenceBlocksDifferentCatalogsForOneRepository(t *testing.T) {
	ctx := context.Background()
	clock := &controlledClock{now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "shared-repository"))
	if err != nil {
		t.Fatal(err)
	}
	firstStore, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog-a.sqlite"), sqlite.Options{Now: clock.nowFn})
	if err != nil {
		t.Fatal(err)
	}
	defer firstStore.Close()
	secondStore, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog-b.sqlite"), sqlite.Options{Now: clock.nowFn})
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	first := &Service{Store: firstStore, Repo: repo, PublicationDomain: testPublicationDomain, Now: clock.nowFn}
	second := &Service{Store: secondStore, Repo: repo, PublicationDomain: testPublicationDomain, Now: clock.nowFn}
	lease, err := first.acquirePublicationFence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	blockedCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := second.acquirePublicationFence(blockedCtx); err == nil {
		t.Fatal("different catalog acquired shared repository lock")
	}
}
