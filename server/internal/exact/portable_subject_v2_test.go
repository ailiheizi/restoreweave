package exact

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

func TestPortableFactV2UsesStableSubjectsAndParentMapping(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "root.txt", []byte("root"))
	if err := os.Mkdir(filepath.Join(fixture.source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.source, "nested", "child.txt"), []byte("child"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := fixture.ingest(t, "sha256:portable-subject-v2")
	closures, err := fixture.service.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(closures) != 1 || closures[0].Schema != PortableFactClosureEnvelopeSchemaV2 || closures[0].Closure.Schema != PortableFactClosureSchemaV2 {
		t.Fatalf("portable v2 closure = %+v", closures)
	}
	var bundle portableFactBundle
	if err := decodeStrictRecord(closures[0].Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Schema != PortableFactBundleSchemaV2 {
		t.Fatalf("portable bundle schema = %q", bundle.Schema)
	}
	mappings := make(map[string]subjectMappingPayload)
	for _, record := range bundle.Records {
		if record.RecordKind != "SUBJECT_MAPPING" {
			continue
		}
		if record.Schema != PortableFactRecordSchemaV2 {
			t.Fatalf("portable record schema = %q", record.Schema)
		}
		var mapping subjectMappingPayload
		if err := decodeStrictRecord(record.Payload, &mapping); err != nil {
			t.Fatal(err)
		}
		if mapping.StableSubjectRef == "" || mapping.StableSubjectRef != record.StableSubjectRef || mapping.NamespaceEntryID != record.RecordID || mapping.StableSubjectRef == mapping.NamespaceEntryID {
			t.Fatalf("v2 mapping identity = %+v, record = %+v", mapping, record)
		}
		mappings[string(mapping.RawPath)] = mapping
	}
	child, ok := mappings["nested/child.txt"]
	if !ok {
		t.Fatalf("nested child mapping missing: %v", mappings)
	}
	parent, ok := mappings["nested"]
	if !ok || child.ParentSubjectRef == "" || child.ParentSubjectRef != parent.StableSubjectRef {
		t.Fatalf("v2 parent mapping = child:%q parent:%+v", child.ParentSubjectRef, parent)
	}

	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	reference, err := reader.BuildRecoveryReference(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatalf("build v2 recovery reference: %v", err)
	}
	if err := reference.ValidateAgainstRepository(context.Background(), fixture.repo, *fixture.service.TrustAnchor); err != nil {
		t.Fatalf("validate v2 recovery reference: %v", err)
	}
	token, err := reader.BuildRecoveryToken(context.Background(), result.SnapshotRef, "nested/child.txt", *fixture.service.TrustAnchor)
	if err != nil {
		t.Fatalf("build v2 recovery token: %v", err)
	}
	if token.SubjectRef != child.StableSubjectRef || token.SubjectRef == child.NamespaceEntryID {
		t.Fatalf("v2 token subject = %q, mapping = %+v", token.SubjectRef, child)
	}
	if err := VerifyRecoveryToken(context.Background(), fixture.repo, token, *fixture.service.TrustAnchor); err != nil {
		t.Fatalf("verify v2 recovery token: %v", err)
	}
}

func TestPortableFactV2RejectsStableSubjectTamper(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "tamper.txt", []byte("tamper"))
	result := fixture.ingest(t, "sha256:portable-subject-v2-tamper")
	driver := repository.RecordDriver(fixture.repo)
	digests, err := driver.ListRecordDigests(context.Background(), repository.RecordPortableFactClosure)
	if err != nil || len(digests) != 1 {
		t.Fatalf("portable closure digests = %v, %v", digests, err)
	}
	oldDigest := digests[0]
	payload, err := readRecord(context.Background(), driver, repository.RecordPortableFactClosure, oldDigest)
	if err != nil {
		t.Fatal(err)
	}
	var envelope PortableFactClosureEnvelope
	if err := decodeStrictRecord(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	var bundle portableFactBundle
	if err := decodeStrictRecord(envelope.Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	for index := range bundle.Records {
		if bundle.Records[index].RecordKind != "SUBJECT_MAPPING" {
			continue
		}
		var mapping subjectMappingPayload
		if err := decodeStrictRecord(bundle.Records[index].Payload, &mapping); err != nil {
			t.Fatal(err)
		}
		mapping.StableSubjectRef = mapping.StableSubjectRef + "-tampered"
		rewritePortableRecordPayload(t, &bundle.Records[index], mapping)
		break
	}
	bundleBytes, err := CanonicalJSON(bundle)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Bundle = bundleBytes
	envelope.Closure.BundleDigest = DigestBytes(bundleBytes)
	envelope.Closure.BundleLength = int64(len(bundleBytes))
	envelope.Closure.Signature = nil
	signed, err := SignPortableFactClosure(*fixture.service.SigningIdentity, envelope.Closure)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := CanonicalJSON(PortableFactClosureEnvelope{Schema: PortableFactClosureEnvelopeSchemaV2, Closure: signed, Bundle: bundleBytes})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(recordRelocationPath(t, fixture.repo.Root(), repository.RecordPortableFactClosure, oldDigest)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.PlaceRecord(context.Background(), repository.RecordPortableFactClosure, bytes.NewReader(encoded)); err != nil {
		t.Fatal(err)
	}
	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListPortableFactClosures(context.Background(), result.SnapshotRef); err == nil {
		t.Fatal("tampered v2 stable subject was accepted")
	}
}
