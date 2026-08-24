package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/scanner"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type attachmentPlacementBoundaryRepo struct {
	*repository.Dir
	contentID   string
	beforePlace bool
	failed      bool
}

func (r *attachmentPlacementBoundaryRepo) PlaceExact(ctx context.Context, contentID string, body io.Reader) (repository.Receipt, error) {
	if contentID != r.contentID || r.failed {
		return r.Dir.PlaceExact(ctx, contentID, body)
	}
	r.failed = true
	if r.beforePlace {
		return repository.Receipt{}, errors.New("attachment placement failed before commit")
	}
	if _, err := r.Dir.PlaceExact(ctx, contentID, body); err != nil {
		return repository.Receipt{}, err
	}
	return repository.Receipt{}, errors.New("attachment placement response lost")
}

func addPortableDescriptionForTest(t *testing.T, fixture signedPublicationFixture, result IngestResult, body string) string {
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
		t.Fatal("file subject is unavailable")
	}
	docID, err := sqlite.NewStableID(sqlite.IDPrefixDescription)
	if err != nil {
		t.Fatal(err)
	}
	doc := sqlite.DescriptionDocument{
		ID: docID, WorkspaceID: result.WorkspaceID, SubjectRef: subject,
		Kind: sqlite.DescriptionUser, Language: "en", Body: body, SourceRef: "user:test",
		ConfigDigest: result.ConfigDigest,
	}
	if err := fixture.store.InsertDescriptionDocument(context.Background(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.BodyDigest
}

func TestPortableFactClosureIsCatalogFreeAndIdempotent(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "facts.txt", []byte("durable facts"))
	result := fixture.ingest(t, "sha256:portable-facts-plan")
	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	closures, err := reader.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatalf("read portable fact closure: %v", err)
	}
	if len(closures) != 1 || closures[0].Closure.ClosureSequence != 1 {
		t.Fatalf("portable fact closures = %+v", closures)
	}
	var bundle portableFactBundle
	if err := decodeStrictRecord(closures[0].Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Records) == 0 || bundle.Records[0].RecordKind == "" {
		t.Fatalf("portable fact bundle has no records: %+v", bundle)
	}
	var mappingCount, capturedFactCount int
	for _, record := range bundle.Records {
		switch record.RecordKind {
		case "SUBJECT_MAPPING":
			mappingCount++
			var mapping subjectMappingPayload
			if err := decodeStrictRecord(record.Payload, &mapping); err != nil {
				t.Fatal(err)
			}
			if mapping.ContentID != "" && (len(mapping.Protection.RecoveryReferences) == 0 || len(mapping.SelectedRepresentationRefs) == 0) {
				t.Fatalf("exact subject mapping lacks portable recovery content: %+v", mapping)
			}
		case "METADATA_FACT":
			if strings.Contains(record.RecordID, ":capture:") {
				capturedFactCount++
			}
		}
	}
	if mappingCount == 0 || capturedFactCount != mappingCount*len(requiredPortableFactNames) {
		t.Fatalf("portable mapping/fact coverage = mappings:%d facts:%d", mappingCount, capturedFactCount)
	}
	if err := fixture.service.PublishPortableFactClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatalf("idempotent portable fact closure: %v", err)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	closures, err = reader.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 1 {
		t.Fatalf("catalog-free portable fact read = %d, err=%v", len(closures), err)
	}
}

func TestPortableExtendedFactsNormalizeZeroState(t *testing.T) {
	binding := capture.BindingRecord{
		Schema: capture.SchemaBindingV1, Profile: capture.ProfileLocalTree,
		CaptureMode: scanner.CaptureModeRootedFD, BoundAt: time.Unix(1, 0).UTC(),
	}
	entry := scanner.EntryRecord{
		Kind:      scanner.KindRegularFile,
		HardLink:  scanner.HardLinkFacts{State: scanner.HardLinkSingle, LinkCount: 1},
		Sparse:    scanner.SparseFacts{State: scanner.SparseNotIndicated},
		Boundary:  scanner.BoundaryObservation{Checked: true, Action: scanner.BoundaryInclude},
		Detection: scanner.DetectionObservation{State: scanner.DetectionNotRequested},
	}
	facts, err := buildManifestEntryFacts(entry, binding)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts.Facts {
		if fact.Name != PortableFactXAttrs && fact.Name != PortableFactACLs {
			continue
		}
		if fact.State != PortableFactUnsupported {
			t.Fatalf("zero %s state = %q, want UNSUPPORTED", fact.Name, fact.State)
		}
		if err := validatePortableFactValue(fact); err != nil {
			t.Fatalf("zero %s validation: %v", fact.Name, err)
		}
	}
}

func TestPortableFactProjectionSortsExtendedMetadata(t *testing.T) {
	binding := capture.BindingRecord{
		Schema: capture.SchemaBindingV1, Profile: capture.ProfileLocalTree,
		CaptureMode: scanner.CaptureModeRootedFD, BoundAt: time.Unix(1, 0).UTC(),
	}
	entry := scanner.EntryRecord{
		Kind:     scanner.KindRegularFile,
		HardLink: scanner.HardLinkFacts{State: scanner.HardLinkSingle, LinkCount: 1},
		Sparse:   scanner.SparseFacts{State: scanner.SparseNotIndicated},
		Filesystem: scanner.FilesystemFacts{
			Version: scanner.FilesystemFactsVersion, CapturedAt: binding.BoundAt,
			XAttrs: scanner.XAttrFacts{State: scanner.CaptureFactObserved, Attributes: []scanner.ExtendedAttribute{
				{Name: "user.z", Value: []byte("z")}, {Name: "user.a", Value: []byte("a")},
			}},
			ACLs: scanner.ACLFacts{State: scanner.CaptureFactObserved, Format: "test-acl-v1", Records: []scanner.ACLRecord{
				{Name: "z", Raw: []byte("z")}, {Name: "a", Raw: []byte("a")},
			}},
		},
		Boundary:  scanner.BoundaryObservation{Checked: true, Action: scanner.BoundaryInclude},
		Detection: scanner.DetectionObservation{State: scanner.DetectionNotRequested},
	}
	facts, err := buildManifestEntryFacts(entry, binding)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts.Facts {
		switch fact.Name {
		case PortableFactXAttrs:
			var value PortableXAttrValue
			if err := decodePortableFactValue(fact.Value, &value); err != nil {
				t.Fatal(err)
			}
			if len(value.Attributes) != 2 || value.Attributes[0].Name != "user.a" || value.Attributes[1].Name != "user.z" {
				t.Fatalf("xattr projection order = %+v", value.Attributes)
			}
		case PortableFactACLs:
			var value PortableACLValue
			if err := decodePortableFactValue(fact.Value, &value); err != nil {
				t.Fatal(err)
			}
			if len(value.Records) != 2 || value.Records[0].Name != "a" || value.Records[1].Name != "z" {
				t.Fatalf("ACL projection order = %+v", value.Records)
			}
		}
	}
}

func TestPortableFactReaderRejectsUnknownStatesAndUnstableOrdering(t *testing.T) {
	tests := []struct {
		name      string
		factName  string
		factState PortableFactState
		value     any
	}{
		{name: "unknown sparse state", factName: PortableFactSparseIndication, factState: PortableFactInconsistent, value: PortableSparseIndicationValue{State: "TYPO"}},
		{name: "unknown hard-link state", factName: PortableFactHardLink, factState: PortableFactInconsistent, value: PortableHardLinkValue{State: "TYPO"}},
		{name: "unsorted xattrs", factName: PortableFactXAttrs, factState: PortableFactObserved, value: PortableXAttrValue{State: "OBSERVED", Attributes: []PortableXAttr{{Name: "user.z", Value: []byte("z")}, {Name: "user.a", Value: []byte("a")}}}},
		{name: "unsorted ACL records", factName: PortableFactACLs, factState: PortableFactObserved, value: PortableACLValue{State: "OBSERVED", Format: "test-acl-v1", Records: []PortableACLRecord{{Name: "z", Raw: []byte("z")}, {Name: "a", Raw: []byte("a")}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fact, err := newManifestPortableFact(test.factName, test.factState, "test-profile", "TEST", time.Unix(1, 0).UTC(), test.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := validatePortableFactValue(fact); err == nil {
				t.Fatalf("invalid %s was accepted", test.name)
			}
		})
	}
}

func TestPortableFactClosureSerializesSameParentSequence(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "concurrent-facts.txt", []byte("concurrent portable facts"))
	result := fixture.ingest(t, "sha256:concurrent-facts-plan")
	second := &Service{
		Store: fixture.store, Repo: fixture.repo,
		SigningIdentity: fixture.service.SigningIdentity, TrustAnchor: fixture.service.TrustAnchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, service := range []*Service{fixture.service, second} {
		wg.Add(1)
		go func(service *Service) {
			defer wg.Done()
			errs <- service.publishPortableFactClosure(context.Background(), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
		}(service)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent portable fact closure: %v", err)
		}
	}
	closures, err := fixture.service.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(closures) != 1 || closures[0].Closure.ClosureSequence != 1 {
		t.Fatalf("same-parent portable fact closures = %+v, want one sequence-1 child", closures)
	}
}

func TestPortableFactClosureExternalizesDescriptionAndRejectsMissingAttachment(t *testing.T) {
	ctx := context.Background()
	fixture := newSignedPublicationFixture(t, "described.txt", []byte("source bytes"))
	result := fixture.ingest(t, "sha256:portable-description-plan")
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
		t.Fatal("file subject is unavailable")
	}
	docID, err := sqlite.NewStableID(sqlite.IDPrefixDescription)
	if err != nil {
		t.Fatal(err)
	}
	body := "a durable description that must not be duplicated inline"
	doc := sqlite.DescriptionDocument{
		ID: docID, WorkspaceID: result.WorkspaceID, SubjectRef: subject,
		Kind: sqlite.DescriptionUser, Language: "en", Body: body, SourceRef: "user:test",
		ConfigDigest: result.ConfigDigest,
	}
	if err := fixture.store.InsertDescriptionDocument(ctx, &doc); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.PublishPortableFactClosure(ctx, result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatalf("publish portable fact successor: %v", err)
	}
	reader := &Service{Repo: fixture.repo, TrustAnchor: fixture.service.TrustAnchor, PublicationDomain: testPublicationDomain, RequireSignedPublication: true}
	closures, err := reader.ListPortableFactClosures(ctx, result.SnapshotRef)
	if err != nil || len(closures) != 2 || closures[1].Closure.ClosureSequence != 2 {
		t.Fatalf("portable fact successor = %d, err=%v", len(closures), err)
	}
	var bundle portableFactBundle
	if err := decodeStrictRecord(closures[1].Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	var attachment portableFactAttachment
	found := false
	for _, record := range bundle.Records {
		if record.RecordKind != "DESCRIPTION_REVISION" || record.RecordID != docID {
			continue
		}
		found = true
		if bytes.Contains(record.Payload, []byte(body)) {
			t.Fatal("description body was duplicated inside the portable fact record")
		}
		var payload descriptionPortablePayload
		if err := decodeStrictRecord(record.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.ConfigDigest != result.ConfigDigest || payload.ProducerProfileDigest == "" {
			t.Fatalf("description profile binding = %+v", payload)
		}
		for _, candidate := range bundle.Attachments {
			if candidate.AttachmentID == payload.BodyAttachmentID {
				attachment = candidate
			}
		}
	}
	if !found || attachment.ContentID != doc.BodyDigest {
		t.Fatalf("description attachment binding = %+v", attachment)
	}
	stream, err := fixture.repo.Open(ctx, attachment.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	read, err := io.ReadAll(stream)
	closeErr := stream.Close()
	if err != nil || closeErr != nil || string(read) != body {
		t.Fatalf("description attachment read = %q, %v / %v", read, err, closeErr)
	}
	hexDigest := strings.TrimPrefix(attachment.ContentID, "sha256:")
	attachmentPath := filepath.Join(fixture.repo.Root(), "blobs", "sha256", hexDigest[:2], hexDigest)
	if err := os.Remove(attachmentPath); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListPortableFactClosures(ctx, result.SnapshotRef); err == nil {
		t.Fatal("missing portable fact attachment was accepted")
	}
}

func TestPortableFactAttachmentPlacementResponseLossReconcilesByDigest(t *testing.T) {
	ctx := context.Background()
	fixture := newSignedPublicationFixture(t, "attachment-response.txt", []byte("source bytes"))
	result := fixture.ingest(t, "sha256:attachment-response-plan")
	body := "portable attachment response loss"
	contentID := addPortableDescriptionForTest(t, fixture, result, body)
	boundary := &attachmentPlacementBoundaryRepo{Dir: fixture.repo, contentID: contentID}
	fixture.service.Repo = boundary

	if err := fixture.service.PublishPortableFactClosure(ctx, result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		t.Fatalf("publish after attachment response loss: %v", err)
	}
	if !boundary.failed {
		t.Fatal("attachment placement boundary was not exercised")
	}
	closures, err := fixture.service.ListPortableFactClosures(ctx, result.SnapshotRef)
	if err != nil || len(closures) != 2 {
		t.Fatalf("portable successors = %d, err=%v", len(closures), err)
	}
	stream, err := fixture.repo.Open(ctx, contentID)
	if err != nil {
		t.Fatal(err)
	}
	payload, readErr := io.ReadAll(stream)
	closeErr := stream.Close()
	if readErr != nil || closeErr != nil || string(payload) != body {
		t.Fatalf("attachment readback = %q, %v / %v", payload, readErr, closeErr)
	}
}

func TestPortableFactAttachmentWithoutReadbackIsTypedUnknown(t *testing.T) {
	ctx := context.Background()
	fixture := newSignedPublicationFixture(t, "attachment-unknown.txt", []byte("source bytes"))
	result := fixture.ingest(t, "sha256:attachment-unknown-plan")
	contentID := addPortableDescriptionForTest(t, fixture, result, "portable attachment missing")
	boundary := &attachmentPlacementBoundaryRepo{Dir: fixture.repo, contentID: contentID, beforePlace: true}
	fixture.service.Repo = boundary

	err := fixture.service.PublishPortableFactClosure(ctx, result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest)
	if !errors.Is(err, ErrUnknownExternalOutcome) || !errors.Is(err, ErrNeedsReconciliation) {
		t.Fatalf("attachment placement = %v, want typed unknown", err)
	}
	if verifyErr := fixture.repo.Verify(ctx, contentID); !errors.Is(verifyErr, repository.ErrNotFound) {
		t.Fatalf("unexpected attachment readback = %v", verifyErr)
	}
	closures, listErr := fixture.service.ListPortableFactClosures(ctx, result.SnapshotRef)
	if listErr != nil || len(closures) != 1 {
		t.Fatalf("unknown attachment published a child: closures=%d err=%v", len(closures), listErr)
	}
}

func TestPortableFactClosureExtensionAdmission(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "extensions.txt", []byte("extensions"))
	result := fixture.ingest(t, "sha256:portable-extension-plan")
	closures, err := fixture.service.ListPortableFactClosures(context.Background(), result.SnapshotRef)
	if err != nil || len(closures) != 1 {
		t.Fatalf("portable closures = %d, err=%v", len(closures), err)
	}
	record := closures[0].Closure
	record.Signature = nil
	record.OptionalExtensions = json.RawMessage(`{"future":{"opaque":true}}`)
	signed, err := SignPortableFactClosure(*fixture.service.SigningIdentity, record)
	if err != nil {
		t.Fatalf("signed optional extension was rejected: %v", err)
	}
	if err := signed.Verify(*fixture.service.TrustAnchor); err != nil {
		t.Fatalf("signed optional extension did not verify: %v", err)
	}
	record.CriticalExtensions = []string{"org.restoreweave.future-required.v1"}
	if _, err := SignPortableFactClosure(*fixture.service.SigningIdentity, record); err == nil {
		t.Fatal("unknown critical extension was accepted")
	}
}
