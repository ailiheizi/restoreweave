package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

// replacePortableFactClosureRecord removes the single existing portable-fact
// child for a snapshot and places a re-signed replacement whose closure is
// passed through mutate. The replacement keeps the original bundle bytes, so
// the only behavior under test is the modified closure fields.
func replacePortableFactClosureRecord(t *testing.T, service *Service, snapshotRef string, mutate func(record *PortableFactClosureRecord)) PortableFactClosureRecord {
	t.Helper()
	ctx := context.Background()
	driver := service.Repo.(repository.RecordDriver)
	digests := closureDigests(t, driver, repository.RecordPortableFactClosure)
	if len(digests) != 1 {
		t.Fatalf("portable fact digests = %v, want exactly one", digests)
	}
	payload, err := readRecord(ctx, driver, repository.RecordPortableFactClosure, digests[0])
	if err != nil {
		t.Fatal(err)
	}
	var envelope PortableFactClosureEnvelope
	if err := decodeStrictRecord(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	recordPath := recordRelocationPath(t, service.Repo.Root(), repository.RecordPortableFactClosure, digests[0])
	if err := os.Remove(recordPath); err != nil {
		t.Fatal(err)
	}
	mutate(&envelope.Closure)
	replacement := envelope.Closure
	replacement.Signature = nil
	signed, err := SignPortableFactClosure(*service.SigningIdentity, replacement)
	if err != nil {
		t.Fatal(err)
	}
	replacementPayload, err := CanonicalJSON(PortableFactClosureEnvelope{Schema: PortableFactClosureEnvelopeSchemaV1, Closure: signed, Bundle: envelope.Bundle})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := driver.PlaceRecord(ctx, repository.RecordPortableFactClosure, bytes.NewReader(replacementPayload))
	if err != nil {
		t.Fatal(err)
	}
	_ = receipt
	return signed
}

func TestPortableFactClosureCriticalReaderDependencyFailsClosed(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "dependency.txt", []byte("dependency closure"))
	result := fixture.ingest(t, "sha256:reader-dependency-plan")
	ctx := context.Background()
	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListPortableFactClosures(ctx, result.SnapshotRef); err != nil {
		t.Fatalf("baseline portable facts read: %v", err)
	}
	replacePortableFactClosureRecord(t, fixture.service, result.SnapshotRef, func(record *PortableFactClosureRecord) {
		record.RequiredReaderDependencies = []string{"readback:entire-repository-image-v1"}
	})
	if _, err := reader.ListPortableFactClosures(ctx, result.SnapshotRef); err == nil {
		t.Fatal("unimplemented critical reader dependency was admitted")
	}
}

func TestPortableFactClosureOptionalExtensionAndSatisfiableDependencyAdmitted(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "optional.txt", []byte("optional dependency"))
	result := fixture.ingest(t, "sha256:optional-dependency-plan")
	ctx := context.Background()
	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	replacement := replacePortableFactClosureRecord(t, fixture.service, result.SnapshotRef, func(record *PortableFactClosureRecord) {
		record.RequiredReaderDependencies = portableFactReaderDependencies(fixture.repo)
		record.OptionalExtensions = json.RawMessage(`{"future":{"opaque":true}}`)
	})
	if err := replacement.Verify(*fixture.service.TrustAnchor); err != nil {
		t.Fatalf("replacement closure did not verify: %v", err)
	}
	closures, err := reader.ListPortableFactClosures(ctx, result.SnapshotRef)
	if err != nil || len(closures) != 1 {
		t.Fatalf("satisfiable dependency with optional extension was not admitted: %d, err=%v", len(closures), err)
	}
	if !sameStrings(closures[0].Closure.RequiredReaderDependencies, portableFactReaderDependencies(fixture.repo)) {
		t.Fatalf("admitted closure lost its reader dependencies: %v", closures[0].Closure.RequiredReaderDependencies)
	}
}

func TestRecoveryReferenceReaderProfileMismatchFailsClosed(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "profile.txt", []byte("profile dependency"))
	result := fixture.ingest(t, "sha256:profile-dependency-plan")
	reference, err := fixture.service.BuildRecoveryReference(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	reference.Repository.Profile = "local-zstd-v1"
	if err := reference.ValidateAgainstRepository(context.Background(), fixture.repo, *fixture.service.TrustAnchor); err == nil {
		t.Fatal("reference requiring an absent repository profile was accepted")
	}
}

func TestRecoveryReferenceCriticalReaderDependencyFailsClosed(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "critical.txt", []byte("critical dependency"))
	result := fixture.ingest(t, "sha256:critical-dependency-plan")
	reference, err := fixture.service.BuildRecoveryReference(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	reference.RequiredReaderDependencies = []string{"restoreweave-reader:semantic-semantics-v1"}
	if err := reference.ValidateAgainstRepository(context.Background(), fixture.repo, *fixture.service.TrustAnchor); err == nil {
		t.Fatal("critical reader dependency that the repository cannot satisfy was accepted")
	}
}

// TestPortableFactReaderDependencySurvivesRelocation proves the dependency
// names embedded in a portable-fact closure remain satisfiable by the same
// repository profile after a physical move (the repository tuple is stable).
func TestPortableFactReaderDependencySurvivesRelocation(t *testing.T) {
	service, movedRoot, _, result := relocateSizedRepository(t, false)
	anchor := loadRelocatedAnchor(t, service)
	reader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, movedRoot, anchor)
	closures, err := reader.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 2 {
		t.Fatalf("relocated portable closures = %d, err=%v", len(closures), err)
	}
	if !sameStrings(closures[0].Closure.RequiredReaderDependencies, portableFactReaderDependencies(reader.Repo)) {
		t.Fatalf("relocated reader dependencies changed: %v", closures[0].Closure.RequiredReaderDependencies)
	}
}
