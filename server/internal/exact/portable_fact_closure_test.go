package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

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
