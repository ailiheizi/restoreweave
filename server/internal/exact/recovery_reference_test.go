package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func artifactRecoveryReferenceFixture(t *testing.T) (signedPublicationFixture, IngestResult, RecoveryReference) {
	t.Helper()
	fixture := newSignedPublicationFixture(t, "reference-artifact.txt", []byte("portable artifact reference"))
	result := fixture.ingest(t, "sha256:reference-artifact-plan")
	ctx := context.Background()
	reference, err := fixture.service.BuildRecoveryReference(ctx, result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := fixture.store.ListNamespaceSubtree(ctx, result.WorkspaceID, result.RootID, "")
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
		t.Fatal("artifact fixture subject is unavailable")
	}
	attemptID, err := sqlite.NewStableID(sqlite.IDPrefixAttempt)
	if err != nil {
		t.Fatal(err)
	}
	artifactID, err := sqlite.NewStableID(sqlite.IDPrefixArtifact)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Unix(10, 0).UTC()
	processorDigest := "sha256:artifact-processor"
	if err := fixture.store.InsertProcessorAttempt(ctx, &sqlite.ProcessorAttempt{
		ID: attemptID, WorkspaceID: result.WorkspaceID, SubjectRef: subject, SnapshotRef: result.SnapshotRef,
		RouteDigest: "sha256:artifact-route", Route: json.RawMessage(`{"kind":"PROCESSING","nodes":[]}`),
		Stage: "EXTRACT", CapabilityID: "extract.artifact.v1", Status: "SUCCEEDED",
		ReasonCode: "PROCESSOR_COMPLETE", Reason: "fixture artifact", Provenance: json.RawMessage(`{"source":"fixture"}`),
		FenceToken: 1, ProcessorDigest: processorDigest, CreatedAt: when, FinishedAt: when.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	body := "portable artifact body"
	if err := fixture.store.InsertProcessorArtifact(ctx, &sqlite.ProcessorArtifact{
		ID: artifactID, WorkspaceID: result.WorkspaceID, SubjectRef: subject, SnapshotRef: result.SnapshotRef,
		RouteDigest: "sha256:artifact-route", Stage: "EXTRACT", CapabilityID: "extract.artifact.v1",
		SchemaRef: "text/plain-v1", State: sqlite.ArtifactAdmitted, AuthorityClass: "PROCESSOR",
		LifecycleClass: "DURABLE", MediaType: "text/plain", ByteLength: int64(len(body)), Digest: DigestBytes([]byte(body)),
		Body: body, AttemptID: attemptID, FenceToken: 1, ProducerDigest: processorDigest,
		Envelope: json.RawMessage(`{"fixture":true}`), CreatedAt: when, UpdatedAt: when.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.publishProcessorAttemptClosure(ctx, result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatal(err)
	}
	publishArtifactPortableFactClosureForTest(t, fixture, result)
	artifactFact, err := portableFactClosureReferenceForTest(t, fixture, result.SnapshotRef, 2)
	if err != nil {
		t.Fatal(err)
	}
	reference.PortableFactClosures = append(reference.PortableFactClosures, artifactFact)
	return fixture, result, reference
}

func portableFactClosureReferenceForTest(t *testing.T, fixture signedPublicationFixture, snapshotRef string, sequence uint64) (RecoveryFactClosureReference, error) {
	t.Helper()
	var result RecoveryFactClosureReference
	driver := repository.RecordDriver(fixture.repo)
	digests, err := driver.ListRecordDigests(context.Background(), repository.RecordPortableFactClosure)
	if err != nil {
		return result, err
	}
	for _, digest := range digests {
		payload, err := readRecord(context.Background(), fixture.repo, repository.RecordPortableFactClosure, digest)
		if err != nil {
			return result, err
		}
		var envelope PortableFactClosureEnvelope
		if err := decodeStrictRecord(payload, &envelope); err != nil {
			return result, err
		}
		if envelope.Closure.SnapshotRef != snapshotRef || envelope.Closure.ClosureSequence != sequence {
			continue
		}
		closureDigest, err := envelope.Closure.Digest()
		if err != nil {
			return result, err
		}
		return RecoveryFactClosureReference{RecordDigest: digest, ClosureDigest: closureDigest, Envelope: envelope}, nil
	}
	return result, fmt.Errorf("portable fact closure sequence %d is unavailable", sequence)
}

func publishArtifactPortableFactClosureForTest(t *testing.T, fixture signedPublicationFixture, result IngestResult) {
	t.Helper()
	ctx := context.Background()
	publications, err := fixture.service.committedPublications(ctx)
	if err != nil || len(publications) != 1 {
		t.Fatalf("committed publications = %d, err=%v", len(publications), err)
	}
	parent := publications[0]
	bundle, attachments, err := fixture.service.buildPortableFactBundle(ctx, result.WorkspaceID, parent.Manifest, fixture.repo.RepositoryIdentity())
	if err != nil {
		t.Fatal(err)
	}
	for _, attachment := range attachments {
		if err := fixture.repo.Verify(ctx, attachment.ContentID); err != nil {
			t.Fatal(err)
		}
	}
	bundleBytes, err := CanonicalJSON(bundle)
	if err != nil {
		t.Fatal(err)
	}
	driver := repository.RecordDriver(fixture.repo)
	digests, err := driver.ListRecordDigests(ctx, repository.RecordPortableFactClosure)
	if err != nil {
		t.Fatal(err)
	}
	var predecessorClosure PortableFactClosureRecord
	found := false
	for _, digest := range digests {
		payload, err := readRecord(ctx, fixture.repo, repository.RecordPortableFactClosure, digest)
		if err != nil {
			t.Fatal(err)
		}
		var envelope PortableFactClosureEnvelope
		if err := decodeStrictRecord(payload, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Closure.SnapshotRef == result.SnapshotRef && envelope.Closure.ClosureSequence == 1 {
			predecessorClosure = envelope.Closure
			found = true
			break
		}
	}
	if !found {
		t.Fatal("initial portable closure is unavailable")
	}
	predecessor, err := predecessorClosure.Digest()
	if err != nil {
		t.Fatal(err)
	}
	processorDigest, err := fixture.service.admittedProcessorAttemptDigest(ctx, result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	if err != nil {
		t.Fatalf("admitted processor attempt digest: %v", err)
	}
	closure, err := SignPortableFactClosure(*fixture.service.SigningIdentity, PortableFactClosureRecord{
		Schema: PortableFactClosureSchemaV1, SignatureDomain: RecoverySignatureDomainV1, RecordKind: PortableFactClosureKind,
		WorkspaceID: result.WorkspaceID, PublicationID: parent.Commit.PublicationID, PublicationDomain: testPublicationDomain,
		SnapshotRef: result.SnapshotRef, ManifestDigest: parent.Commit.ManifestDigest, ParentCommitDigest: result.PublicationCommitDigest,
		ParentGeneration: parent.Commit.Generation, ClosureSequence: 2, PredecessorClosureDigest: predecessor,
		BundleSchema: bundle.Schema, BundleDigest: DigestBytes(bundleBytes), BundleLength: int64(len(bundleBytes)),
		RecordCount: int64(len(bundle.Records)), AttachmentCount: int64(len(bundle.Attachments)), ProcessorAttemptDigest: processorDigest,
		TargetIdentity: fixture.repo.RepositoryIdentity(), WriterIdentity: fixture.service.SigningIdentity.WriterIdentity,
		KeyID: fixture.service.SigningIdentity.KeyID, FenceToken: parent.Commit.FenceToken,
		RequiredReaderDependencies: portableFactReaderDependencies(fixture.repo), CanonicalizationProfile: "encoding/json-compact-v1",
		CriticalExtensions: []string{}, OptionalExtensions: json.RawMessage(`{}`), SignedAt: fixture.service.now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := CanonicalJSON(PortableFactClosureEnvelope{Schema: PortableFactClosureEnvelopeSchemaV1, Closure: closure, Bundle: bundleBytes})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.PlaceRecord(ctx, repository.RecordPortableFactClosure, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
}

func assertArtifactReferenceExactRestore(t *testing.T, fixture signedPublicationFixture, result IngestResult) {
	t.Helper()
	destination := t.TempDir()
	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.Restore(context.Background(), result.SnapshotRef, destination); err != nil {
		t.Fatalf("exact restore after processor-child failure: %v", err)
	}
}

func processorAttemptRecordDigest(t *testing.T, fixture signedPublicationFixture, objectDigest string) string {
	t.Helper()
	driver := repository.RecordDriver(fixture.repo)
	digests, err := driver.ListRecordDigests(context.Background(), repository.RecordProcessorAttemptClosure)
	if err != nil {
		t.Fatal(err)
	}
	for _, digest := range digests {
		payload, err := readRecord(context.Background(), fixture.repo, repository.RecordProcessorAttemptClosure, digest)
		if err != nil {
			t.Fatal(err)
		}
		// ProcessorAttemptDigest is the signed envelope object digest (the
		// record key), not the digest of the closure field alone.
		if DigestBytes(payload) == objectDigest || digest == objectDigest {
			return digest
		}
		var envelope ProcessorAttemptClosureEnvelope
		if err := decodeStrictRecord(payload, &envelope); err != nil {
			t.Fatal(err)
		}
		candidate, err := envelope.Closure.Digest()
		if err != nil {
			t.Fatal(err)
		}
		if candidate == objectDigest {
			return digest
		}
	}
	t.Fatalf("processor attempt closure %s is unavailable", objectDigest)
	return ""
}

func TestRecoveryReferenceValidatesProcessorAttemptChildAndArtifactBinding(t *testing.T) {
	fixture, result, reference := artifactRecoveryReferenceFixture(t)
	if err := reference.ValidateAgainstRepository(context.Background(), fixture.repo, *fixture.service.TrustAnchor); err != nil {
		t.Fatalf("intact processor child rejected: %v", err)
	}

	childClosureDigest := reference.PortableFactClosures[len(reference.PortableFactClosures)-1].Envelope.Closure.ProcessorAttemptDigest
	childDigest := processorAttemptRecordDigest(t, fixture, childClosureDigest)
	childPath := recordRelocationPath(t, fixture.repo.Root(), repository.RecordProcessorAttemptClosure, childDigest)
	if err := os.Remove(childPath); err != nil {
		t.Fatal(err)
	}
	if err := reference.ValidateAgainstRepository(context.Background(), fixture.repo, *fixture.service.TrustAnchor); err == nil {
		t.Fatal("missing processor-attempt child was accepted")
	}
	assertArtifactReferenceExactRestore(t, fixture, result)
}

func TestRecoveryReferenceRejectsTamperedOrMismatchedProcessorAttemptChild(t *testing.T) {
	t.Run("tampered", func(t *testing.T) {
		fixture, result, reference := artifactRecoveryReferenceFixture(t)
		childClosureDigest := reference.PortableFactClosures[len(reference.PortableFactClosures)-1].Envelope.Closure.ProcessorAttemptDigest
		childDigest := processorAttemptRecordDigest(t, fixture, childClosureDigest)
		childPath := recordRelocationPath(t, fixture.repo.Root(), repository.RecordProcessorAttemptClosure, childDigest)
		if !flipFileByte(t, childPath) {
			t.Fatal("processor child tamper did not change bytes")
		}
		if err := reference.ValidateAgainstRepository(context.Background(), fixture.repo, *fixture.service.TrustAnchor); err == nil {
			t.Fatal("tampered processor-attempt child was accepted")
		}
		assertArtifactReferenceExactRestore(t, fixture, result)
	})

	t.Run("binding mismatch", func(t *testing.T) {
		fixture, result, reference := artifactRecoveryReferenceFixture(t)
		ctx := context.Background()
		factIndex := len(reference.PortableFactClosures) - 1
		fact := reference.PortableFactClosures[factIndex]
		childClosureDigest := fact.Envelope.Closure.ProcessorAttemptDigest
		childDigest := processorAttemptRecordDigest(t, fixture, childClosureDigest)
		childPayload, err := readRecord(ctx, fixture.repo, repository.RecordProcessorAttemptClosure, childDigest)
		if err != nil {
			t.Fatal(err)
		}
		var child ProcessorAttemptClosureEnvelope
		if err := decodeStrictRecord(childPayload, &child); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(recordRelocationPath(t, fixture.repo.Root(), repository.RecordProcessorAttemptClosure, childDigest)); err != nil {
			t.Fatal(err)
		}
		child.Closure.FenceToken++
		child.Closure.Signature = nil
		signedChild, err := SignProcessorAttemptClosure(*fixture.service.SigningIdentity, child.Closure)
		if err != nil {
			t.Fatal(err)
		}
		childPayload, err = CanonicalJSON(ProcessorAttemptClosureEnvelope{Schema: ProcessorAttemptClosureEnvelopeSchemaV1, Closure: signedChild, Bundle: child.Bundle})
		if err != nil {
			t.Fatal(err)
		}
		_, err = fixture.repo.PlaceRecord(ctx, repository.RecordProcessorAttemptClosure, bytes.NewReader(childPayload))
		if err != nil {
			t.Fatal(err)
		}

		factPath := recordRelocationPath(t, fixture.repo.Root(), repository.RecordPortableFactClosure, fact.RecordDigest)
		if err := os.Remove(factPath); err != nil {
			t.Fatal(err)
		}
		factClosure := fact.Envelope.Closure
		factClosure.ProcessorAttemptDigest, err = signedChild.Digest()
		if err != nil {
			t.Fatal(err)
		}
		factClosure.Signature = nil
		signedFact, err := SignPortableFactClosure(*fixture.service.SigningIdentity, factClosure)
		if err != nil {
			t.Fatal(err)
		}
		factPayload, err := CanonicalJSON(PortableFactClosureEnvelope{Schema: PortableFactClosureEnvelopeSchemaV1, Closure: signedFact, Bundle: fact.Envelope.Bundle})
		if err != nil {
			t.Fatal(err)
		}
		factReceipt, err := fixture.repo.PlaceRecord(ctx, repository.RecordPortableFactClosure, bytes.NewReader(factPayload))
		if err != nil {
			t.Fatal(err)
		}
		reference.PortableFactClosures[factIndex].Envelope = PortableFactClosureEnvelope{Schema: PortableFactClosureEnvelopeSchemaV1, Closure: signedFact, Bundle: fact.Envelope.Bundle}
		reference.PortableFactClosures[factIndex].RecordDigest = factReceipt.Digest
		reference.PortableFactClosures[factIndex].ClosureDigest, err = signedFact.Digest()
		if err != nil {
			t.Fatal(err)
		}
		if err := reference.ValidateAgainstRepository(ctx, fixture.repo, *fixture.service.TrustAnchor); err == nil {
			t.Fatal("processor-attempt fence mismatch was accepted")
		}
		assertArtifactReferenceExactRestore(t, fixture, result)
	})
}

func TestRecoveryReferenceRoundTripAndRepositoryValidation(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "reference.txt", []byte("portable recovery reference"))
	result := fixture.ingest(t, "sha256:reference-plan")

	reference, err := fixture.service.BuildRecoveryReference(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatalf("build recovery reference: %v", err)
	}
	if reference.Schema != RecoveryReferenceSchemaV2 || reference.FactHealth != RecoveryFactHealthComplete {
		t.Fatalf("reference identity/health = %q/%q", reference.Schema, reference.FactHealth)
	}
	payload, err := MarshalRecoveryReference(reference)
	if err != nil {
		t.Fatalf("marshal recovery reference: %v", err)
	}
	decoded, err := DecodeRecoveryReference(payload)
	if err != nil {
		t.Fatalf("decode recovery reference: %v", err)
	}
	if err := decoded.Validate(*fixture.service.TrustAnchor); err != nil {
		t.Fatalf("validate recovery reference: %v", err)
	}
	if err := decoded.ValidateAgainstRepository(context.Background(), fixture.repo, *fixture.service.TrustAnchor); err != nil {
		t.Fatalf("validate recovery reference against repository: %v", err)
	}
}

func TestRecoveryReferenceRejectsTamperWrongAnchorAndUnknownCriticalExtension(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "reference-tamper.txt", []byte("tamper me"))
	result := fixture.ingest(t, "sha256:reference-tamper-plan")
	reference, err := fixture.service.BuildRecoveryReference(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := MarshalRecoveryReference(reference)
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
	if _, err := DecodeRecoveryReference(tampered); err == nil {
		t.Fatal("tampered recovery reference decoded successfully")
	}

	wrongIdentity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrongAnchor, err := NewTrustAnchor(wrongIdentity, testPublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	if err := reference.Validate(wrongAnchor); err == nil {
		t.Fatal("wrong trust anchor accepted")
	}

	reference.CriticalExtensions = []string{"org.example.future-critical"}
	if _, err := MarshalRecoveryReference(reference); err == nil {
		t.Fatal("unknown critical extension accepted")
	}
}

func TestRecoveryReferenceAllowsOptionalExtensionAndExplicitIncompleteFacts(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "reference-optional.txt", []byte("optional extension"))
	result := fixture.ingest(t, "sha256:reference-optional-plan")
	reference, err := fixture.service.BuildRecoveryReference(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	reference.OptionalExtensions = json.RawMessage(`{"org.example.optional":{"version":1}}`)
	payload, err := MarshalRecoveryReference(reference)
	if err != nil {
		t.Fatalf("marshal optional extension: %v", err)
	}
	decoded, err := DecodeRecoveryReference(payload)
	if err != nil {
		t.Fatalf("decode optional extension: %v", err)
	}
	if err := decoded.Validate(*fixture.service.TrustAnchor); err != nil {
		t.Fatalf("validate optional extension: %v", err)
	}

	decoded.PortableFactClosures = nil
	decoded.FactHealth = RecoveryFactHealthIncomplete
	if _, err := MarshalRecoveryReference(decoded); err != nil {
		t.Fatalf("marshal explicit incomplete reference: %v", err)
	}
}
