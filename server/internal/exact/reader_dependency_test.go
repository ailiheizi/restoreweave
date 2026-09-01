package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// dishonestRecordDriver models a backend whose VerifyRecord implementation
// incorrectly reports success. The clean-reader must still authenticate the
// bytes it actually read against the requested content address.
type dishonestRecordDriver struct {
	repository.Driver
	repository.RecordDriver
	role   repository.RecordRole
	digest string
}

// dishonestContentDriver models a backend whose content Verify reports
// success while Open returns bytes that do not match the content address.
type dishonestContentDriver struct {
	repository.Driver
	repository.RecordDriver
	contentID string
	payload   []byte
}

func (r *dishonestContentDriver) Open(ctx context.Context, contentID string) (io.ReadCloser, error) {
	if contentID == r.contentID {
		return io.NopCloser(bytes.NewReader(append([]byte(nil), r.payload...))), nil
	}
	return r.Driver.Open(ctx, contentID)
}

func (r *dishonestContentDriver) Verify(context.Context, string) error { return nil }

func (r *dishonestContentDriver) RepositoryProfile() repository.ProfileDescription {
	return repository.DescribeProfile(r.Driver)
}

func (r *dishonestRecordDriver) OpenRecord(ctx context.Context, role repository.RecordRole, digest string) (io.ReadCloser, error) {
	body, err := r.RecordDriver.OpenRecord(ctx, role, digest)
	if err != nil {
		return nil, err
	}
	payload, err := io.ReadAll(body)
	closeErr := body.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if role == r.role && digest == r.digest {
		payload = append(payload, byte('x'))
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

func (r *dishonestRecordDriver) VerifyRecord(context.Context, repository.RecordReceipt) error {
	return nil
}

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
	envelopeSchema := PortableFactClosureEnvelopeSchemaV1
	if signed.Schema == PortableFactClosureSchemaV2 {
		envelopeSchema = PortableFactClosureEnvelopeSchemaV2
	}
	replacementPayload, err := CanonicalJSON(PortableFactClosureEnvelope{Schema: envelopeSchema, Closure: signed, Bundle: envelope.Bundle})
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

func TestRecoveryReferenceRejectsReadBytesWithWrongDigest(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "wrong-record-digest.txt", []byte("wrong record digest"))
	result := fixture.ingest(t, "sha256:wrong-record-digest-plan")
	reference, err := fixture.service.BuildRecoveryReference(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(reference.PortableFactClosures) != 1 {
		t.Fatalf("portable fact closures = %d, want one", len(reference.PortableFactClosures))
	}
	factDigest := reference.PortableFactClosures[0].RecordDigest
	driver := &dishonestRecordDriver{
		Driver:       fixture.repo,
		RecordDriver: fixture.repo,
		role:         repository.RecordPortableFactClosure,
		digest:       factDigest,
	}
	if err := reference.ValidateAgainstRepository(context.Background(), driver, *fixture.service.TrustAnchor); err == nil {
		t.Fatal("recovery reference accepted bytes whose digest differed from the requested record digest")
	}
}

func TestPortableReadersRejectDishonestContentReadback(t *testing.T) {
	fixture, result, reference := artifactRecoveryReferenceFixture(t)
	latest := reference.PortableFactClosures[len(reference.PortableFactClosures)-1]
	bundle, err := validatePortableFactBundle(latest.Envelope.Bundle, latest.Envelope.Closure.WorkspaceID, latest.Envelope.Closure.SnapshotRef)
	if err != nil || len(bundle.Attachments) == 0 {
		t.Fatalf("portable artifact attachments = %d, err=%v", len(bundle.Attachments), err)
	}
	attachment := bundle.Attachments[0]
	body, err := fixture.repo.Open(context.Background(), attachment.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(body)
	closeErr := body.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read fixture attachment: %v/%v", err, closeErr)
	}
	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{name: "same length wrong bytes", payload: func() []byte {
			wrong := append([]byte(nil), actual...)
			wrong[0] ^= 0xff
			return wrong
		}()},
		{name: "wrong length", payload: append(append([]byte(nil), actual...), byte('x'))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			driver := &dishonestContentDriver{
				Driver: fixture.repo, RecordDriver: fixture.repo,
				contentID: attachment.ContentID, payload: tc.payload,
			}
			reader := &Service{Repo: driver, TrustAnchor: fixture.service.TrustAnchor,
				PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if _, err := reader.ListPortableFactClosures(context.Background(), result.SnapshotRef); err == nil {
				t.Fatal("portable fact reader accepted dishonest content readback")
			}
			if err := reference.ValidateAgainstRepository(context.Background(), driver, *fixture.service.TrustAnchor); err == nil {
				t.Fatal("recovery reference accepted dishonest content readback")
			}
		})
	}
}

func TestFullByteVerificationRejectsDishonestRepositoryVerify(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "dishonest-full-verify.txt", []byte("dishonest full verify"))
	result := fixture.ingest(t, "sha256:dishonest-full-verify-plan")
	publication, err := fixture.service.committedPublicationForSnapshot(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	var file ManifestEntry
	for _, entry := range publication.Manifest.Entries {
		if entry.EntryType == string(sqlite.EntryFile) {
			file = entry
			break
		}
	}
	if file.ContentID == "" || file.LogicalSize == nil || *file.LogicalSize == 0 {
		t.Fatalf("fixture file identity is incomplete: %+v", file)
	}
	wrong := bytes.Repeat([]byte{'x'}, int(*file.LogicalSize))
	driver := &dishonestContentDriver{
		Driver: fixture.repo, RecordDriver: fixture.repo,
		contentID: file.ContentID, payload: wrong,
	}
	reader := &Service{Repo: driver, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.VerifyMode(context.Background(), result.SnapshotRef, VerifyAuthenticatedMetadata, ""); err != nil {
		t.Fatalf("authenticated metadata verification unexpectedly read payload: %v", err)
	}
	if _, err := reader.VerifyMode(context.Background(), result.SnapshotRef, VerifyFullBytes, ""); err == nil {
		t.Fatal("full-byte verification trusted dishonest repository Verify without hashing Open bytes")
	}
}

func TestPortableFactClosureRejectsUnknownBundleSchemaAtSigning(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "bundle-schema.txt", []byte("bundle schema"))
	fixture.ingest(t, "sha256:bundle-schema-plan")
	digests, err := fixture.repo.ListRecordDigests(context.Background(), repository.RecordPortableFactClosure)
	if err != nil || len(digests) != 1 {
		t.Fatalf("portable fact digests = %v, err=%v", digests, err)
	}
	payload, err := readRecord(context.Background(), fixture.repo, repository.RecordPortableFactClosure, digests[0])
	if err != nil {
		t.Fatal(err)
	}
	var envelope PortableFactClosureEnvelope
	if err := decodeStrictRecord(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Closure.BundleSchema = "org.restoreweave.portable-fact-bundle.v3"
	envelope.Closure.Signature = nil
	if _, err := SignPortableFactClosure(*fixture.service.SigningIdentity, envelope.Closure); err == nil {
		t.Fatal("unknown portable fact bundle schema was signed")
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
