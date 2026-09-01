//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package exact

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestIngestFIFORetainsMetadataProtectionAndPortableClosure(t *testing.T) {
	fixture := newSignedPublicationFixture(t, "payload.txt", []byte("exact payload"))
	fifoPath := filepath.Join(fixture.source, "events.pipe")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("create FIFO: %v", err)
	}
	result := fixture.ingest(t, "sha256:fifo-ingest")
	ctx := context.Background()

	entries, err := fixture.store.ListNamespaceContent(ctx, result.WorkspaceID, result.RootID)
	if err != nil {
		t.Fatalf("list namespace content: %v", err)
	}
	var fifo sqlite.NamespaceEntry
	for _, entry := range entries {
		if entry.DisplayName == "events.pipe" {
			fifo = entry
			break
		}
	}
	if fifo.ID == "" || fifo.EntryType != sqlite.EntryFIFO {
		t.Fatalf("FIFO namespace entry = %+v", fifo)
	}
	if fifo.SubjectRef == "" || fifo.SubjectRef == fifo.ID {
		t.Fatalf("FIFO did not receive a stable subject: %+v", fifo)
	}
	protection, err := fixture.store.GetProtectionRecordBySubject(ctx, result.WorkspaceID, fifo.SubjectRef)
	if err != nil {
		t.Fatalf("FIFO protection record: %v", err)
	}
	if protection.Mode != sqlite.ProtectionMetadataOnly || protection.Outcome != sqlite.ProtectionUnavailable ||
		protection.LocalRepresentationID != "" || protection.ExpectedContentID != "" {
		t.Fatalf("FIFO protection overclaims bytes: %+v", protection)
	}
	references, err := fixture.store.ListRecoveryReferencesBySubject(ctx, result.WorkspaceID, fifo.SubjectRef)
	if err != nil {
		t.Fatalf("FIFO recovery references: %v", err)
	}
	if len(references) != 0 {
		t.Fatalf("FIFO unexpectedly has recovery bytes: %+v", references)
	}

	manifest, err := fixture.service.loadManifest(ctx, result.SnapshotRef)
	if err != nil {
		t.Fatalf("load signed manifest: %v", err)
	}
	var manifestFIFO ManifestEntry
	for _, entry := range manifest.Entries {
		if entry.RelativePath == "events.pipe" {
			manifestFIFO = entry
			break
		}
	}
	if manifestFIFO.EntryType != string(sqlite.EntryFIFO) || manifestFIFO.Facts == nil ||
		manifestFIFO.Protection.Mode != string(sqlite.ProtectionMetadataOnly) ||
		manifestFIFO.Protection.Outcome != string(sqlite.ProtectionUnavailable) {
		t.Fatalf("FIFO manifest entry = %+v", manifestFIFO)
	}
	if manifestFIFO.ContentID != "" || len(manifestFIFO.Protection.RecoveryReferences) != 0 {
		t.Fatalf("FIFO manifest contains payload recovery: %+v", manifestFIFO)
	}

	closures, err := fixture.service.ListPortableFactClosures(ctx, result.SnapshotRef)
	if err != nil {
		t.Fatalf("list portable FIFO closure: %v", err)
	}
	if len(closures) != 1 {
		t.Fatalf("portable closure count = %d, want one", len(closures))
	}
	var bundle portableFactBundle
	if err := decodeStrictRecord(closures[0].Bundle, &bundle); err != nil {
		t.Fatalf("decode portable FIFO bundle: %v", err)
	}
	var mapping subjectMappingPayload
	foundMapping := false
	for _, record := range bundle.Records {
		if record.RecordKind != "SUBJECT_MAPPING" {
			continue
		}
		var candidate subjectMappingPayload
		if err := json.Unmarshal(record.Payload, &candidate); err != nil {
			t.Fatalf("decode FIFO mapping: %v", err)
		}
		if candidate.DisplayName == "events.pipe" {
			mapping = candidate
			foundMapping = true
			break
		}
	}
	if !foundMapping || mapping.StableSubjectRef != fifo.SubjectRef || mapping.EntryType != string(sqlite.EntryFIFO) ||
		mapping.Protection.Mode != string(sqlite.ProtectionMetadataOnly) || mapping.Protection.Outcome != string(sqlite.ProtectionUnavailable) ||
		len(mapping.SelectedRepresentationRefs) != 0 {
		t.Fatalf("portable FIFO mapping = %+v", mapping)
	}

	// A rescan gets a new snapshot-local entry but carries the same subject and
	// revises the metadata-only protection projection without ever opening FIFO.
	second := fixture.ingest(t, "sha256:fifo-ingest-2")
	secondEntries, err := fixture.store.ListNamespaceContent(ctx, second.WorkspaceID, second.RootID)
	if err != nil {
		t.Fatalf("list rescanned namespace content: %v", err)
	}
	var secondFIFO sqlite.NamespaceEntry
	for _, entry := range secondEntries {
		if entry.DisplayName == "events.pipe" {
			secondFIFO = entry
			break
		}
	}
	if secondFIFO.ID == fifo.ID || secondFIFO.SubjectRef != fifo.SubjectRef {
		t.Fatalf("FIFO rescan continuity = first:%+v second:%+v", fifo, secondFIFO)
	}
	secondProtection, err := fixture.store.GetProtectionRecordBySubject(ctx, second.WorkspaceID, secondFIFO.SubjectRef)
	if err != nil {
		t.Fatal(err)
	}
	if secondProtection.ID != protection.ID || secondProtection.Revision <= protection.Revision {
		t.Fatalf("FIFO protection rescan = first:%+v second:%+v", protection, secondProtection)
	}

	// Keep the source tree available for the signed fixture cleanup and make the
	// test explicit about never materializing the FIFO as a payload.
	if info, err := os.Lstat(fifoPath); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO source changed unexpectedly: info=%v err=%v", info, err)
	}
}
