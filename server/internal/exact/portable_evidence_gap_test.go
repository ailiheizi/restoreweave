package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type portableEvidenceFixture struct {
	fixture signedPublicationFixture
	result  IngestResult
}

func portableFactRecordSchemaForTest(bundle portableFactBundle) string {
	if bundle.Schema == PortableFactBundleSchemaV2 {
		return PortableFactRecordSchemaV2
	}
	return PortableFactRecordSchemaV1
}

func newPortableEvidenceFixture(t *testing.T) portableEvidenceFixture {
	t.Helper()
	ctx := context.Background()
	fixture := newSignedPublicationFixture(t, "portable-evidence.txt", []byte("portable evidence"))
	result := fixture.ingest(t, "sha256:portable-evidence-plan")
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
		t.Fatal("portable evidence subject is unavailable")
	}
	annotationID, err := sqlite.NewStableID(sqlite.IDPrefixAnnotation)
	if err != nil {
		t.Fatal(err)
	}
	annotation := &sqlite.Annotation{
		ID: annotationID, WorkspaceID: result.WorkspaceID,
		SubjectRef: subject, Kind: sqlite.AnnotationNote, Body: "portable note", Revision: 1,
	}
	if err := fixture.store.CreateAnnotation(ctx, annotation); err != nil {
		t.Fatal(err)
	}
	documentID, err := sqlite.NewStableID(sqlite.IDPrefixDescription)
	if err != nil {
		t.Fatal(err)
	}
	document := &sqlite.DescriptionDocument{
		ID: documentID, WorkspaceID: result.WorkspaceID,
		SubjectRef: subject, Kind: sqlite.DescriptionUser, Language: "en",
		Body: "portable description", SourceRef: "user:portable-evidence",
		ConfigDigest: result.ConfigDigest,
	}
	if err := fixture.store.InsertDescriptionDocument(ctx, document); err != nil {
		t.Fatal(err)
	}
	segmentID, err := sqlite.NewStableID(sqlite.IDPrefixSemanticSegment)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.InsertSemanticSegment(ctx, &sqlite.SemanticSegment{
		ID: segmentID, WorkspaceID: result.WorkspaceID,
		DocumentID: document.ID, SubjectRef: subject, Ordinal: 0,
		Text: document.Body, Language: "en", Section: "body",
		SourceSpan: json.RawMessage(fmt.Sprintf(`{"start_byte":0,"end_byte":%d}`, len([]byte(document.Body)))),
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.PublishPortableFactClosure(ctx, result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatal(err)
	}
	return portableEvidenceFixture{fixture: fixture, result: result}
}

func latestPortableFactEnvelope(t *testing.T, fixture signedPublicationFixture, snapshotRef string) (string, PortableFactClosureEnvelope, portableFactBundle) {
	t.Helper()
	driver := repository.RecordDriver(fixture.repo)
	digests, err := driver.ListRecordDigests(context.Background(), repository.RecordPortableFactClosure)
	if err != nil {
		t.Fatal(err)
	}
	var latestDigest string
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
			latestDigest, latest = digest, envelope
		}
	}
	if latestDigest == "" {
		t.Fatal("portable fact closure is unavailable")
	}
	var bundle portableFactBundle
	if err := decodeStrictRecord(latest.Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	return latestDigest, latest, bundle
}

func replacePortableFactBundleForTest(t *testing.T, fixture signedPublicationFixture, mutate func(*portableFactBundle)) {
	t.Helper()
	ctx := context.Background()
	driver := repository.RecordDriver(fixture.repo)
	digest, envelope, bundle := latestPortableFactEnvelope(t, fixture, "")
	if err := os.Remove(recordRelocationPath(t, fixture.repo.Root(), repository.RecordPortableFactClosure, digest)); err != nil {
		t.Fatal(err)
	}
	mutate(&bundle)
	bundleBytes, err := CanonicalJSON(bundle)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Bundle = bundleBytes
	envelope.Closure.BundleDigest = DigestBytes(bundleBytes)
	envelope.Closure.BundleLength = int64(len(bundleBytes))
	envelope.Closure.RecordCount = int64(len(bundle.Records))
	envelope.Closure.AttachmentCount = int64(len(bundle.Attachments))
	envelope.Closure.Signature = nil
	signed, err := SignPortableFactClosure(*fixture.service.SigningIdentity, envelope.Closure)
	if err != nil {
		t.Fatal(err)
	}
	envelopeSchema := PortableFactClosureEnvelopeSchemaV1
	if signed.Schema == PortableFactClosureSchemaV2 {
		envelopeSchema = PortableFactClosureEnvelopeSchemaV2
	}
	payload, err := CanonicalJSON(PortableFactClosureEnvelope{
		Schema: envelopeSchema, Closure: signed, Bundle: bundleBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.PlaceRecord(ctx, repository.RecordPortableFactClosure, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
}

func findPortableRecord(t *testing.T, bundle portableFactBundle, kind string) int {
	t.Helper()
	for i, record := range bundle.Records {
		if record.RecordKind == kind {
			return i
		}
	}
	t.Fatalf("portable record kind %q is unavailable", kind)
	return -1
}

func rewritePortableRecordPayload(t *testing.T, record *portableFactRecord, payload any) {
	t.Helper()
	payloadBytes, err := CanonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	record.Payload = payloadBytes
	record.PayloadDigest = DigestBytes(payloadBytes)
	record.PayloadLength = int64(len(payloadBytes))
}

func TestPortableEvidenceIncludesAnnotationAndSemanticSegmentAfterRelocation(t *testing.T) {
	ctx := context.Background()
	evidence := newPortableEvidenceFixture(t)
	_, _, bundle := latestPortableFactEnvelope(t, evidence.fixture, evidence.result.SnapshotRef)
	seen := map[string]bool{}
	for _, record := range bundle.Records {
		seen[record.RecordKind] = true
	}
	for _, kind := range []string{"ANNOTATION_REVISION", "DESCRIPTION_REVISION", "SEMANTIC_SEGMENT"} {
		if !seen[kind] {
			t.Fatalf("portable closure omitted %s", kind)
		}
	}
	if err := evidence.fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	movedRoot := filepath.Join(t.TempDir(), "relocated")
	if err := os.Rename(evidence.fixture.repo.Root(), movedRoot); err != nil {
		t.Fatal(err)
	}
	reader := relocatedReader(t, repository.RepositoryProfileDirectoryCASDev, movedRoot, loadRelocatedAnchor(t, evidence.fixture.service))
	closures, err := reader.ListPortableFactClosures(ctx, evidence.result.SnapshotRef)
	if err != nil || len(closures) != 2 {
		t.Fatalf("relocated durable closure = %d, err=%v", len(closures), err)
	}
	manifest, err := reader.loadManifest(ctx, evidence.result.SnapshotRef)
	if err != nil {
		t.Fatalf("relocated manifest: %v", err)
	}
	var relocated portableFactBundle
	if err := decodeStrictRecord(closures[1].Bundle, &relocated); err != nil {
		t.Fatal(err)
	}
	mappings := make(map[string]subjectMappingPayload)
	for _, record := range relocated.Records {
		if record.RecordKind != "SUBJECT_MAPPING" {
			continue
		}
		var mapping subjectMappingPayload
		if err := decodeStrictRecord(record.Payload, &mapping); err != nil {
			t.Fatal(err)
		}
		mappings[portableRawPathKey(mapping.RawPath)] = mapping
	}
	wantMappings := 0
	for _, entry := range manifest.Entries {
		if !(entry.RelativePath == "." && portableRawPathKey(entry.RawPath) == ".") {
			wantMappings++
		}
	}
	if len(mappings) != wantMappings {
		t.Fatalf("relocated mappings = %d, want %d", len(mappings), wantMappings)
	}
	for _, entry := range manifest.Entries {
		if entry.RelativePath == "." && portableRawPathKey(entry.RawPath) == "." {
			continue
		}
		mapping, ok := mappings[portableRawPathKey(entry.RawPath)]
		if !ok || !bytes.Equal(mapping.RawPath, entry.RawPath) || !bytes.Equal(mapping.RawName, entry.RawName) || mapping.EntryType != entry.EntryType {
			t.Fatalf("relocated mapping for %q does not round-trip manifest entry: %+v", entry.RelativePath, mapping)
		}
		for _, fact := range entry.Facts.Facts {
			id := mapping.NamespaceEntryID + ":capture:" + fact.Name
			var found portableFactRecord
			for _, record := range relocated.Records {
				if record.RecordKind == "METADATA_FACT" && record.RecordID == id {
					found = record
					break
				}
			}
			wantPayload, err := CanonicalJSON(fact)
			if err != nil {
				t.Fatal(err)
			}
			wantProvenance, err := CanonicalJSON(map[string]any{
				"source_profile": fact.SourceProfile, "authority": fact.Authority,
				"capture_time": fact.CapturedAt, "provenance_digest": fact.ProvenanceDigest,
			})
			if err != nil || found.RecordID == "" || !bytes.Equal(found.Payload, wantPayload) || !bytes.Equal(found.Provenance, wantProvenance) {
				t.Fatalf("relocated captured fact %q does not round-trip manifest", id)
			}
		}
	}
	for _, kind := range []string{"ANNOTATION_REVISION", "DESCRIPTION_REVISION", "SEMANTIC_SEGMENT"} {
		if findPortableRecord(t, relocated, kind) < 0 {
			t.Fatalf("relocated portable closure omitted %s", kind)
		}
	}
}

func sortPortableFactRecordsForTest(bundle *portableFactBundle) {
	sort.Slice(bundle.Records, func(i, j int) bool {
		a, b := bundle.Records[i], bundle.Records[j]
		if a.RecordKind != b.RecordKind {
			return a.RecordKind < b.RecordKind
		}
		if a.RecordID != b.RecordID {
			return a.RecordID < b.RecordID
		}
		return a.Revision < b.Revision
	})
}

func TestPortableEvidenceRequiresManifestCompleteState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*portableFactBundle)
	}{
		{
			name: "mapping omitted",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "SUBJECT_MAPPING")
				bundle.Records = append(bundle.Records[:index], bundle.Records[index+1:]...)
			},
		},
		{
			name: "captured fact omitted",
			mutate: func(bundle *portableFactBundle) {
				for index, record := range bundle.Records {
					if record.RecordKind == "METADATA_FACT" && strings.Contains(record.RecordID, ":capture:") {
						bundle.Records = append(bundle.Records[:index], bundle.Records[index+1:]...)
						return
					}
				}
				t.Fatal("captured fact is unavailable")
			},
		},
		{
			name: "captured fact substituted",
			mutate: func(bundle *portableFactBundle) {
				var first, second int = -1, -1
				for index, record := range bundle.Records {
					if record.RecordKind != "METADATA_FACT" || !strings.Contains(record.RecordID, ":capture:") {
						continue
					}
					if first < 0 {
						first = index
					} else {
						second = index
						break
					}
				}
				if first < 0 || second < 0 {
					t.Fatal("two captured facts are unavailable")
				}
				bundle.Records[first].Payload = append([]byte(nil), bundle.Records[second].Payload...)
				bundle.Records[first].PayloadDigest = DigestBytes(bundle.Records[first].Payload)
				bundle.Records[first].PayloadLength = int64(len(bundle.Records[first].Payload))
			},
		},
		{
			name: "mapping outer identity",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "SUBJECT_MAPPING")
				var mapping subjectMappingPayload
				if err := decodeStrictRecord(bundle.Records[index].Payload, &mapping); err != nil {
					t.Fatal(err)
				}
				bundle.Records[index].RecordID = mapping.NamespaceEntryID + ":different-outer-id"
			},
		},
		{
			name: "mapping parent reference",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "SUBJECT_MAPPING")
				var mapping subjectMappingPayload
				if err := decodeStrictRecord(bundle.Records[index].Payload, &mapping); err != nil {
					t.Fatal(err)
				}
				mapping.ParentSubjectRef = "subject:missing-parent"
				rewritePortableRecordPayload(t, &bundle.Records[index], mapping)
			},
		},
		{
			name: "duplicate identical logical key",
			mutate: func(bundle *portableFactBundle) {
				bundle.Records = append(bundle.Records, bundle.Records[0])
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := newPortableEvidenceFixture(t)
			replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
				test.mutate(bundle)
				sortPortableFactRecordsForTest(bundle)
			})
			reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
				t.Fatalf("invalid %s portable bundle was accepted", test.name)
			}
		})
	}
}

func TestPortableEvidenceAllowsAdditionalDurableMetadataFact(t *testing.T) {
	evidence := newPortableEvidenceFixture(t)
	replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
		var subject string
		for _, record := range bundle.Records {
			if record.RecordKind == "SUBJECT_MAPPING" {
				subject = record.StableSubjectRef
				break
			}
		}
		if subject == "" {
			t.Fatal("subject mapping is unavailable")
		}
		payload, err := CanonicalJSON(sqlite.MetadataFact{
			ID: "metadata:durable-extra", WorkspaceID: bundle.WorkspaceID, SubjectRef: subject,
			Namespace: "test", Key: "value", Value: json.RawMessage(`"durable"`),
			ValueType: "string", AuthorityClass: "TEST", SourceRef: "test:durable", Revision: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		bundle.Records = append(bundle.Records, portableFactRecord{
			Schema: portableFactRecordSchemaForTest(*bundle), RecordKind: "METADATA_FACT", RecordID: "metadata:durable-extra",
			WorkspaceID: bundle.WorkspaceID, SnapshotRef: bundle.SnapshotRef, StableSubjectRef: subject,
			Revision: 1, PayloadDigest: DigestBytes(payload), PayloadLength: int64(len(payload)),
			Provenance: json.RawMessage(`{"authority":"test"}`), Payload: payload,
		})
		sortPortableFactRecordsForTest(bundle)
	})
	reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err != nil {
		t.Fatalf("additional durable metadata fact was rejected: %v", err)
	}
}

func appendPortableMetadataFactForTest(t *testing.T, bundle *portableFactBundle) int {
	t.Helper()
	var subject string
	for _, record := range bundle.Records {
		if record.RecordKind == "SUBJECT_MAPPING" {
			subject = record.StableSubjectRef
			break
		}
	}
	if subject == "" {
		t.Fatal("subject mapping is unavailable")
	}
	value := sqlite.MetadataFact{
		ID: "mdf:portable-extra", WorkspaceID: bundle.WorkspaceID, SubjectRef: subject,
		Namespace: "test", Key: "value", Value: json.RawMessage(`"durable"`),
		ValueType: "string", AuthorityClass: "TEST", SourceRef: "test:durable", Revision: 1,
	}
	payload, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Records = append(bundle.Records, portableFactRecord{
		Schema: portableFactRecordSchemaForTest(*bundle), RecordKind: "METADATA_FACT", RecordID: value.ID,
		WorkspaceID: bundle.WorkspaceID, SnapshotRef: bundle.SnapshotRef, StableSubjectRef: subject,
		Revision: 1, PayloadDigest: DigestBytes(payload), PayloadLength: int64(len(payload)),
		Provenance: json.RawMessage(`{"authority":"test"}`), Payload: payload,
	})
	return len(bundle.Records) - 1
}

func TestPortableEvidenceRejectsDurableMetadataIdentityDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*sqlite.MetadataFact)
	}{
		{name: "id", mutate: func(value *sqlite.MetadataFact) { value.ID = "mdf:foreign" }},
		{name: "workspace", mutate: func(value *sqlite.MetadataFact) { value.WorkspaceID = "workspace:foreign" }},
		{name: "subject", mutate: func(value *sqlite.MetadataFact) { value.SubjectRef = "nse:foreign" }},
		{name: "revision", mutate: func(value *sqlite.MetadataFact) { value.Revision = 2 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := newPortableEvidenceFixture(t)
			replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
				index := appendPortableMetadataFactForTest(t, bundle)
				var value sqlite.MetadataFact
				if err := decodeStrictRecord(bundle.Records[index].Payload, &value); err != nil {
					t.Fatal(err)
				}
				test.mutate(&value)
				rewritePortableRecordPayload(t, &bundle.Records[index], value)
				sortPortableFactRecordsForTest(bundle)
			})
			reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
				t.Fatalf("metadata %s drift was accepted", test.name)
			}
		})
	}
}

func TestPortableEvidenceRejectsAnnotationBodyDigestDrift(t *testing.T) {
	evidence := newPortableEvidenceFixture(t)
	replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
		index := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
		var value sqlite.AnnotationRevision
		if err := decodeStrictRecord(bundle.Records[index].Payload, &value); err != nil {
			t.Fatal(err)
		}
		value.BodyDigest = DigestBytes([]byte("different annotation body"))
		rewritePortableRecordPayload(t, &bundle.Records[index], value)
	})
	reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
		t.Fatal("annotation body digest drift was accepted")
	}
}

func appendPortableMappingForConsistencyTest(t *testing.T, bundle *portableFactBundle, rootID, sourceID string) {
	t.Helper()
	index := findPortableRecord(t, *bundle, "SUBJECT_MAPPING")
	record := bundle.Records[index]
	var mapping subjectMappingPayload
	if err := decodeStrictRecord(record.Payload, &mapping); err != nil {
		t.Fatal(err)
	}
	mapping.NamespaceEntryID = "nse:portable-consistency"
	mapping.NamespaceRootID = rootID
	mapping.SourceID = sourceID
	mapping.ParentSubjectRef = ""
	mapping.RawPath = []byte("portable-consistency")
	mapping.RawName = []byte("portable-consistency")
	payload, err := CanonicalJSON(mapping)
	if err != nil {
		t.Fatal(err)
	}
	record.RecordID = mapping.NamespaceEntryID
	record.StableSubjectRef = mapping.NamespaceEntryID
	record.Payload = payload
	record.PayloadDigest = DigestBytes(payload)
	record.PayloadLength = int64(len(payload))
	bundle.Records = append(bundle.Records, record)
}

func TestPortableEvidenceRejectsInconsistentMappingRootAndSource(t *testing.T) {
	for _, test := range []struct {
		name     string
		rootID   string
		sourceID string
	}{
		{name: "root", rootID: "root:foreign", sourceID: ""},
		{name: "source", rootID: "", sourceID: "source:foreign"},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence := newPortableEvidenceFixture(t)
			replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "SUBJECT_MAPPING")
				var mapping subjectMappingPayload
				if err := decodeStrictRecord(bundle.Records[index].Payload, &mapping); err != nil {
					t.Fatal(err)
				}
				if test.rootID == "" {
					test.rootID = mapping.NamespaceRootID
				}
				if test.sourceID == "" {
					test.sourceID = mapping.SourceID
				}
				appendPortableMappingForConsistencyTest(t, bundle, test.rootID, test.sourceID)
				sortPortableFactRecordsForTest(bundle)
			})
			reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
				t.Fatalf("inconsistent mapping %s was accepted", test.name)
			}
		})
	}
}

func TestPortableEvidenceRejectsSelectedRepresentationDrift(t *testing.T) {
	evidence := newPortableEvidenceFixture(t)
	replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
		index := findPortableRecord(t, *bundle, "SUBJECT_MAPPING")
		var mapping subjectMappingPayload
		if err := decodeStrictRecord(bundle.Records[index].Payload, &mapping); err != nil {
			t.Fatal(err)
		}
		mapping.SelectedRepresentationRefs = append(mapping.SelectedRepresentationRefs, "representation:unexpected")
		rewritePortableRecordPayload(t, &bundle.Records[index], mapping)
	})
	reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
		t.Fatal("selected representation drift was accepted")
	}
}

func appendPortableAnnotationRevisionForTest(t *testing.T, bundle *portableFactBundle, predecessorIndex int, revision int64, predecessorID, subjectRef string) {
	t.Helper()
	previous := bundle.Records[predecessorIndex]
	var value sqlite.AnnotationRevision
	if err := decodeStrictRecord(previous.Payload, &value); err != nil {
		t.Fatal(err)
	}
	value.ID = value.AnnotationID + "@" + strconv.FormatInt(revision, 10) + "-" + strconv.Itoa(len(bundle.Records))
	value.Revision = revision
	value.PredecessorID = predecessorID
	value.SubjectRef = subjectRef
	payload, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Records = append(bundle.Records, portableFactRecord{
		Schema: portableFactRecordSchemaForTest(*bundle), RecordKind: "ANNOTATION_REVISION", RecordID: value.ID,
		WorkspaceID: bundle.WorkspaceID, SnapshotRef: bundle.SnapshotRef, StableSubjectRef: subjectRef,
		Revision: revision, PredecessorRecordID: predecessorID, PayloadDigest: DigestBytes(payload),
		PayloadLength: int64(len(payload)), Provenance: append(json.RawMessage(nil), previous.Provenance...), Payload: payload,
	})
}

func appendPortableDescriptionRevisionForTest(t *testing.T, bundle *portableFactBundle, predecessorIndex int, revision int64, predecessorID, subjectRef, kind string) {
	t.Helper()
	previous := bundle.Records[predecessorIndex]
	var value descriptionPortablePayload
	if err := decodeStrictRecord(previous.Payload, &value); err != nil {
		t.Fatal(err)
	}
	value.ID = value.ID + "-successor-" + strconv.FormatInt(revision, 10) + "-" + strconv.Itoa(len(bundle.Records))
	value.Revision = revision
	value.PredecessorID = predecessorID
	value.SubjectRef = subjectRef
	value.Kind = sqlite.DescriptionKind(kind)
	attachmentID := "attachment:description:successor:" + strconv.FormatInt(revision, 10) + ":" + strconv.Itoa(len(bundle.Records))
	value.BodyAttachmentID = attachmentID
	payload, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Records = append(bundle.Records, portableFactRecord{
		Schema: portableFactRecordSchemaForTest(*bundle), RecordKind: "DESCRIPTION_REVISION", RecordID: value.ID,
		WorkspaceID: bundle.WorkspaceID, SnapshotRef: bundle.SnapshotRef, StableSubjectRef: subjectRef,
		Revision: revision, PredecessorRecordID: predecessorID, PayloadDigest: DigestBytes(payload),
		PayloadLength: int64(len(payload)), Provenance: append(json.RawMessage(nil), previous.Provenance...), Payload: payload,
	})
	var previousPayload descriptionPortablePayload
	if err := decodeStrictRecord(previous.Payload, &previousPayload); err != nil {
		t.Fatal(err)
	}
	var previousAttachment portableFactAttachment
	for _, attachment := range bundle.Attachments {
		if attachment.AttachmentID == previousPayload.BodyAttachmentID {
			previousAttachment = attachment
			break
		}
	}
	if previousAttachment.AttachmentID == "" {
		t.Fatal("description predecessor attachment is unavailable")
	}
	previousAttachment.AttachmentID = attachmentID
	bundle.Attachments = append(bundle.Attachments, previousAttachment)
}

func TestPortableEvidenceRejectsBrokenAnnotationAndDescriptionRevisionChains(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		mutate func(*portableFactBundle)
	}{
		{
			name: "annotation revision one predecessor",
			kind: "ANNOTATION_REVISION",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
				var value sqlite.AnnotationRevision
				if err := decodeStrictRecord(bundle.Records[index].Payload, &value); err != nil {
					t.Fatal(err)
				}
				value.PredecessorID = "annotation:orphan"
				rewritePortableRecordPayload(t, &bundle.Records[index], value)
				bundle.Records[index].PredecessorRecordID = value.PredecessorID
			},
		},
		{
			name: "annotation orphan",
			kind: "ANNOTATION_REVISION",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
				appendPortableAnnotationRevisionForTest(t, bundle, index, 2, "annotation:orphan", bundle.Records[index].StableSubjectRef)
			},
		},
		{
			name: "annotation skipped revision",
			kind: "ANNOTATION_REVISION",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
				appendPortableAnnotationRevisionForTest(t, bundle, index, 3, bundle.Records[index].RecordID, bundle.Records[index].StableSubjectRef)
			},
		},
		{
			name: "annotation cross subject",
			kind: "ANNOTATION_REVISION",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
				appendPortableAnnotationRevisionForTest(t, bundle, index, 2, bundle.Records[index].RecordID, "nse_foreign_subject")
			},
		},
		{
			name: "annotation two successors",
			kind: "ANNOTATION_REVISION",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
				predecessor := bundle.Records[index].RecordID
				appendPortableAnnotationRevisionForTest(t, bundle, index, 2, predecessor, bundle.Records[index].StableSubjectRef)
				appendPortableAnnotationRevisionForTest(t, bundle, index, 2, predecessor, bundle.Records[index].StableSubjectRef)
			},
		},
		{
			name: "description orphan",
			kind: "DESCRIPTION_REVISION",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "DESCRIPTION_REVISION")
				appendPortableDescriptionRevisionForTest(t, bundle, index, 2, "description:orphan", bundle.Records[index].StableSubjectRef, "USER")
			},
		},
		{
			name: "description skipped revision",
			kind: "DESCRIPTION_REVISION",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "DESCRIPTION_REVISION")
				appendPortableDescriptionRevisionForTest(t, bundle, index, 3, bundle.Records[index].RecordID, bundle.Records[index].StableSubjectRef, "USER")
			},
		},
		{
			name: "description cross subject",
			kind: "DESCRIPTION_REVISION",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "DESCRIPTION_REVISION")
				appendPortableDescriptionRevisionForTest(t, bundle, index, 2, bundle.Records[index].RecordID, "nse_foreign_subject", "USER")
			},
		},
		{
			name: "description two successors",
			kind: "DESCRIPTION_REVISION",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "DESCRIPTION_REVISION")
				predecessor := bundle.Records[index].RecordID
				appendPortableDescriptionRevisionForTest(t, bundle, index, 2, predecessor, bundle.Records[index].StableSubjectRef, "USER")
				appendPortableDescriptionRevisionForTest(t, bundle, index, 2, predecessor, bundle.Records[index].StableSubjectRef, "USER")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := newPortableEvidenceFixture(t)
			replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
				test.mutate(bundle)
				sortPortableFactRecordsForTest(bundle)
			})
			reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
				t.Fatalf("invalid %s revision chain was accepted", test.kind)
			}
		})
	}
}

func TestPortableEvidenceAcceptsCompleteRevisionChains(t *testing.T) {
	evidence := newPortableEvidenceFixture(t)
	replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
		annotationIndex := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
		annotationPredecessor := bundle.Records[annotationIndex].RecordID
		appendPortableAnnotationRevisionForTest(t, bundle, annotationIndex, 2, annotationPredecessor, bundle.Records[annotationIndex].StableSubjectRef)
		descriptionIndex := findPortableRecord(t, *bundle, "DESCRIPTION_REVISION")
		descriptionPredecessor := bundle.Records[descriptionIndex].RecordID
		appendPortableDescriptionRevisionForTest(t, bundle, descriptionIndex, 2, descriptionPredecessor, bundle.Records[descriptionIndex].StableSubjectRef, "USER")
		sortPortableFactRecordsForTest(bundle)
	})
	reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err != nil {
		t.Fatalf("valid annotation/description revision chains were rejected: %v", err)
	}
}

func TestPortableEvidenceRejectsInnerRecordConflictsAndBoundaryCrossing(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*portableFactBundle)
	}{
		{
			name: "conflicting logical key",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
				conflict := bundle.Records[index]
				var annotation sqlite.AnnotationRevision
				if err := decodeStrictRecord(conflict.Payload, &annotation); err != nil {
					t.Fatal(err)
				}
				annotation.Body = "conflicting portable note"
				annotation.BodyDigest = DigestBytes([]byte(annotation.Body))
				rewritePortableRecordPayload(t, &conflict, annotation)
				bundle.Records = append(bundle.Records, conflict)
				sort.Slice(bundle.Records, func(i, j int) bool {
					a, b := bundle.Records[i], bundle.Records[j]
					if a.RecordKind != b.RecordKind {
						return a.RecordKind < b.RecordKind
					}
					if a.RecordID != b.RecordID {
						return a.RecordID < b.RecordID
					}
					return a.Revision < b.Revision
				})
			},
		},
		{
			name: "workspace boundary",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
				var annotation sqlite.AnnotationRevision
				if err := decodeStrictRecord(bundle.Records[index].Payload, &annotation); err != nil {
					t.Fatal(err)
				}
				annotation.WorkspaceID = "workspace:foreign"
				rewritePortableRecordPayload(t, &bundle.Records[index], annotation)
			},
		},
		{
			name: "subject mismatch",
			mutate: func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "ANNOTATION_REVISION")
				var annotation sqlite.AnnotationRevision
				if err := decodeStrictRecord(bundle.Records[index].Payload, &annotation); err != nil {
					t.Fatal(err)
				}
				annotation.SubjectRef = "ns:foreign-subject"
				rewritePortableRecordPayload(t, &bundle.Records[index], annotation)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := newPortableEvidenceFixture(t)
			replacePortableFactBundleForTest(t, evidence.fixture, test.mutate)
			reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
				t.Fatalf("invalid %s portable bundle was accepted", test.name)
			}
		})
	}
}

func TestPortableEvidenceRejectsInnerRecordPayloadTamper(t *testing.T) {
	evidence := newPortableEvidenceFixture(t)
	replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
		index := findPortableRecord(t, *bundle, "SEMANTIC_SEGMENT")
		mutated := bytes.Replace(bundle.Records[index].Payload, []byte("portable description"), []byte("portable descriptioN"), 1)
		if bytes.Equal(mutated, bundle.Records[index].Payload) {
			t.Fatal("inner record tamper did not change bytes")
		}
		bundle.Records[index].Payload = mutated
	})
	reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
		t.Fatal("tampered inner portable record was accepted")
	}
}

func TestPortableEvidenceBindsSemanticSegmentsToDescriptionBody(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*portableFactBundle, int, *sqlite.SemanticSegment)
	}{
		{
			name: "missing description",
			mutate: func(_ *portableFactBundle, _ int, segment *sqlite.SemanticSegment) {
				segment.DocumentID = "dsc_missing"
			},
		},
		{
			name: "nonzero first ordinal",
			mutate: func(bundle *portableFactBundle, index int, segment *sqlite.SemanticSegment) {
				segment.Ordinal = 1
				bundle.Records[index].Revision = 2
			},
		},
		{
			name: "text differs from body",
			mutate: func(_ *portableFactBundle, _ int, segment *sqlite.SemanticSegment) {
				segment.Text = strings.Replace(segment.Text, "description", "descriptioN", 1)
				segment.TextDigest = DigestBytes([]byte(segment.Text))
			},
		},
		{
			name: "wrong subject",
			mutate: func(_ *portableFactBundle, _ int, segment *sqlite.SemanticSegment) {
				segment.SubjectRef = "nse:foreign"
			},
		},
		{
			name: "text digest mismatch",
			mutate: func(_ *portableFactBundle, _ int, segment *sqlite.SemanticSegment) {
				segment.TextDigest = DigestBytes([]byte("different segment text"))
			},
		},
		{
			name: "document revision mismatch",
			mutate: func(_ *portableFactBundle, _ int, segment *sqlite.SemanticSegment) {
				segment.DocumentRevision++
			},
		},
		{
			name: "segmentation profile missing",
			mutate: func(_ *portableFactBundle, _ int, segment *sqlite.SemanticSegment) {
				segment.SegmentationProfileDigest = ""
			},
		},
		{
			name: "language missing",
			mutate: func(_ *portableFactBundle, _ int, segment *sqlite.SemanticSegment) {
				segment.Language = ""
			},
		},
		{
			name: "provenance document mismatch",
			mutate: func(bundle *portableFactBundle, index int, _ *sqlite.SemanticSegment) {
				bundle.Records[index].Provenance = json.RawMessage(`{"description_document_id":"dsc:foreign"}`)
			},
		},
		{
			name: "discontinuous source span",
			mutate: func(_ *portableFactBundle, _ int, segment *sqlite.SemanticSegment) {
				segment.SourceSpan = json.RawMessage(fmt.Sprintf(`{"start_byte":1,"end_byte":%d}`, len([]byte(segment.Text))+1))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := newPortableEvidenceFixture(t)
			replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "SEMANTIC_SEGMENT")
				var segment sqlite.SemanticSegment
				if err := decodeStrictRecord(bundle.Records[index].Payload, &segment); err != nil {
					t.Fatal(err)
				}
				test.mutate(bundle, index, &segment)
				rewritePortableRecordPayload(t, &bundle.Records[index], segment)
			})
			reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
				t.Fatalf("invalid semantic segment with %s was accepted", test.name)
			}
		})
	}
}

func TestPortableEvidenceRejectsDescriptionProfileBindingLoss(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*descriptionPortablePayload)
	}{
		{name: "producer profile", mutate: func(description *descriptionPortablePayload) {
			description.ProducerProfileDigest = ""
		}},
		{name: "config missing", mutate: func(description *descriptionPortablePayload) {
			description.ConfigDigest = ""
		}},
		{name: "config drift", mutate: func(description *descriptionPortablePayload) {
			description.ConfigDigest = "sha256:foreign-description-config"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence := newPortableEvidenceFixture(t)
			replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
				index := findPortableRecord(t, *bundle, "DESCRIPTION_REVISION")
				var description descriptionPortablePayload
				if err := decodeStrictRecord(bundle.Records[index].Payload, &description); err != nil {
					t.Fatal(err)
				}
				test.mutate(&description)
				rewritePortableRecordPayload(t, &bundle.Records[index], description)
			})
			reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
				t.Fatalf("portable description with %s binding was accepted", test.name)
			}
		})
	}
}

func TestPortableEvidenceRejectsMissingPreparedCommitAndFactRoles(t *testing.T) {
	tests := []struct {
		name string
		role repository.RecordRole
		read func(*Service, string) error
	}{
		{name: "prepared", role: repository.RecordPreparedClosure, read: func(reader *Service, snapshot string) error {
			_, err := reader.ListSnapshots(context.Background())
			return err
		}},
		{name: "commit", role: repository.RecordPublicationCommit, read: func(reader *Service, snapshot string) error {
			manifests, err := reader.ListSnapshots(context.Background())
			if err == nil && len(manifests) == 0 {
				return errors.New("missing commit yielded no committed snapshots")
			}
			return err
		}},
		{name: "portable fact", role: repository.RecordPortableFactClosure, read: func(reader *Service, snapshot string) error {
			_, err := reader.ListPortableFactClosures(context.Background(), snapshot)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSignedPublicationFixture(t, "missing-role.txt", []byte("missing role"))
			result := fixture.ingest(t, "sha256:missing-"+strings.ReplaceAll(test.name, " ", "-")+"-plan")
			var roleDigest string
			if test.role == repository.RecordPreparedClosure {
				publications, err := fixture.service.committedPublications(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				roleDigest = publications[0].Commit.PreparedObjectDigest
			} else {
				digests, err := fixture.repo.ListRecordDigests(context.Background(), test.role)
				if err != nil || len(digests) != 1 {
					t.Fatalf("%s digests = %v, err=%v", test.name, digests, err)
				}
				roleDigest = digests[0]
			}
			if err := os.Remove(recordRelocationPath(t, fixture.repo.Root(), test.role, roleDigest)); err != nil {
				t.Fatal(err)
			}
			reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if err := test.read(reader, result.SnapshotRef); err == nil {
				t.Fatalf("missing %s record was accepted", test.name)
			}
		})
	}
}

func TestPortableEvidenceRejectsConflictingPublicationCommit(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "conflicting-commit.txt", []byte("conflicting commit"))
	result := fixture.ingest(t, "sha256:conflicting-commit-plan")
	driver := repository.RecordDriver(fixture.repo)
	digests, err := driver.ListRecordDigests(context.Background(), repository.RecordPublicationCommit)
	if err != nil || len(digests) != 1 {
		t.Fatalf("commit digests = %v, err=%v", digests, err)
	}
	payload, err := readRecord(context.Background(), driver, repository.RecordPublicationCommit, digests[0])
	if err != nil {
		t.Fatal(err)
	}
	var commit PublicationCommitRecord
	if err := decodeStrictRecord(payload, &commit); err != nil {
		t.Fatal(err)
	}
	commit.PlanDigest = "sha256:" + strings.Repeat("f", 64)
	commit.Signature = nil
	conflict, err := SignPublicationCommit(*fixture.service.SigningIdentity, commit)
	if err != nil {
		t.Fatal(err)
	}
	conflictPayload, err := CanonicalJSON(conflict)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.PlaceRecord(context.Background(), repository.RecordPublicationCommit, bytes.NewReader(conflictPayload)); err != nil {
		t.Fatal(err)
	}
	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListSnapshots(context.Background()); err == nil {
		t.Fatalf("conflicting generation commit was accepted for %s", result.SnapshotRef)
	}
}

func TestPortableEvidenceRejectsConflictingFactSuccessor(t *testing.T) {
	evidence := newPortableEvidenceFixture(t)
	ctx := context.Background()
	digest, envelope, bundle := latestPortableFactEnvelope(t, evidence.fixture, evidence.result.SnapshotRef)
	if _, err := os.Stat(recordRelocationPath(t, evidence.fixture.repo.Root(), repository.RecordPortableFactClosure, digest)); err != nil {
		t.Fatal(err)
	}
	index := findPortableRecord(t, bundle, "SEMANTIC_SEGMENT")
	bundle.Records[index].Provenance = json.RawMessage(`{"conflict":true}`)
	bundleBytes, err := CanonicalJSON(bundle)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Bundle = bundleBytes
	envelope.Closure.BundleDigest = DigestBytes(bundleBytes)
	envelope.Closure.BundleLength = int64(len(bundleBytes))
	envelope.Closure.Signature = nil
	signed, err := SignPortableFactClosure(*evidence.fixture.service.SigningIdentity, envelope.Closure)
	if err != nil {
		t.Fatal(err)
	}
	envelopeSchema := PortableFactClosureEnvelopeSchemaV1
	if signed.Schema == PortableFactClosureSchemaV2 {
		envelopeSchema = PortableFactClosureEnvelopeSchemaV2
	}
	payload, err := CanonicalJSON(PortableFactClosureEnvelope{Schema: envelopeSchema, Closure: signed, Bundle: bundleBytes})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evidence.fixture.repo.PlaceRecord(ctx, repository.RecordPortableFactClosure, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListPortableFactClosures(ctx, evidence.result.SnapshotRef); err == nil {
		t.Fatal("conflicting same-sequence portable fact successor was accepted")
	}
}

func TestPortableEvidenceRejectsDescriptionWithoutBodyAttachment(t *testing.T) {
	evidence := newPortableEvidenceFixture(t)
	replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
		index := findPortableRecord(t, *bundle, "DESCRIPTION_REVISION")
		var description descriptionPortablePayload
		if err := decodeStrictRecord(bundle.Records[index].Payload, &description); err != nil {
			t.Fatal(err)
		}
		attachmentID := description.BodyAttachmentID
		description.BodyAttachmentID = ""
		rewritePortableRecordPayload(t, &bundle.Records[index], description)
		attachments := bundle.Attachments[:0]
		for _, attachment := range bundle.Attachments {
			if attachment.AttachmentID != attachmentID {
				attachments = append(attachments, attachment)
			}
		}
		bundle.Attachments = attachments
	})
	reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
		t.Fatal("description without a body attachment was accepted")
	}
}

func TestPortableEvidenceRejectsArtifactBodyLossAndTamper(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		name := "missing"
		if tamper {
			name = "tampered"
		}
		t.Run(name, func(t *testing.T) {
			fixture, result, reference := artifactRecoveryReferenceFixture(t)
			latest := reference.PortableFactClosures[len(reference.PortableFactClosures)-1].Envelope
			var bundle portableFactBundle
			if err := decodeStrictRecord(latest.Bundle, &bundle); err != nil {
				t.Fatal(err)
			}
			var attachment portableFactAttachment
			for _, candidate := range bundle.Attachments {
				if candidate.Purpose == "PROCESSOR_ARTIFACT_BODY" {
					attachment = candidate
					break
				}
			}
			if attachment.ContentID == "" {
				t.Fatal("processor artifact body attachment is unavailable")
			}
			hexDigest := strings.TrimPrefix(attachment.ContentID, "sha256:")
			bodyPath := filepath.Join(fixture.repo.Root(), "blobs", "sha256", hexDigest[:2], hexDigest)
			if tamper {
				if !flipFileByte(t, bodyPath) {
					t.Fatal("artifact body tamper did not change bytes")
				}
			} else if err := os.Remove(bodyPath); err != nil {
				t.Fatal(err)
			}
			if err := reference.ValidateAgainstRepository(context.Background(), fixture.repo, *fixture.service.TrustAnchor); err == nil {
				t.Fatalf("%s artifact body was accepted", name)
			}
			assertArtifactReferenceExactRestore(t, fixture, result)
		})
	}
}

func TestPortableEvidenceRejectsAttachmentDescriptorIdentityDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*portableFactAttachment)
	}{
		{name: "wrong length", mutate: func(attachment *portableFactAttachment) {
			attachment.LogicalLength++
		}},
		{name: "wrong content digest", mutate: func(attachment *portableFactAttachment) {
			attachment.ContentID = "sha256:" + strings.Repeat("0", 64)
		}},
		{name: "wrong repository", mutate: func(attachment *portableFactAttachment) {
			attachment.RepositoryID = "repository:foreign"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := newPortableEvidenceFixture(t)
			replacePortableFactBundleForTest(t, evidence.fixture, func(bundle *portableFactBundle) {
				for index := range bundle.Attachments {
					if bundle.Attachments[index].Purpose == "DESCRIPTION_BODY" {
						test.mutate(&bundle.Attachments[index])
						return
					}
				}
				t.Fatal("description attachment is unavailable")
			})
			reader := &Service{Repo: evidence.fixture.repo, TrustAnchor: evidence.fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
			if _, err := reader.ListPortableFactClosures(context.Background(), evidence.result.SnapshotRef); err == nil {
				t.Fatal("portable attachment descriptor identity drift was accepted")
			}
		})
	}
}

func TestPortableEvidenceCleanInstallRejectsUnavailableReaderDependency(t *testing.T) {
	ctx := context.Background()
	repoRoot, _, _, anchorPath, anchor, result := cleanInstallFixture(t)
	reader := openCleanInstallReader(t, repoRoot, anchor)
	referencePath := filepath.Join(t.TempDir(), "reference.json")
	if _, err := reader.ExportRecoveryReference(ctx, result.SnapshotRef, referencePath); err != nil {
		t.Fatal(err)
	}
	reference, err := LoadRecoveryReference(referencePath)
	if err != nil {
		t.Fatal(err)
	}
	reference.RequiredReaderDependencies = []string{"restoreweave-reader:unavailable-v1"}
	mutatedPath := filepath.Join(t.TempDir(), "mutated-reference.json")
	payload, err := MarshalRecoveryReference(reference)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mutatedPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ImportRecoveryArtifact(ctx, mutatedPath, anchorPath, testPublicationDomain); err == nil {
		t.Fatal("clean-install reader accepted an unavailable required dependency")
	}
}
