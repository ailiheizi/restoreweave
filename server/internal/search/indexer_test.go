package search

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestIndexerFeedsDurableFactsProtectionAndDescriptions(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	seed := testutil.SeedNamespace(t, store)
	representationID := mustSearchID(t, sqlite.IDPrefixRepresentation)
	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertRepresentation(ctx, &sqlite.Representation{
			ID: representationID, WorkspaceID: seed.WorkspaceID,
			ContentID: "sha256:file-content", DecodedLength: 16,
			OwnershipMode: sqlite.OwnershipEngineManaged, CodecProfileRef: "test-raw/v1",
			AccessMode: sqlite.AccessSequentialStream, RecordDigest: "sha256:test-representation",
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertMetadataFact(ctx, &sqlite.MetadataFact{
		ID:             mustSearchID(t, sqlite.IDPrefixMetadataFact),
		WorkspaceID:    seed.WorkspaceID,
		SubjectRef:     seed.FileEntryID,
		Namespace:      "media.game",
		Key:            "platform",
		Value:          json.RawMessage(`"dreamcast"`),
		ValueType:      "string",
		AuthorityClass: "USER_CONFIRMED",
		SourceRef:      "user:test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertProtectionRecord(ctx, &sqlite.ProtectionRecord{
		ID:                    mustSearchID(t, sqlite.IDPrefixProtectionRecord),
		WorkspaceID:           seed.WorkspaceID,
		SubjectRef:            seed.FileEntryID,
		Mode:                  sqlite.ProtectionStoreExact,
		Outcome:               sqlite.ProtectionExactProtected,
		ExpectedContentID:     "sha256:file-content",
		LocalRepresentationID: representationID,
		LastVerificationRef:   "readback:sha256:file-content",
	}); err != nil {
		t.Fatal(err)
	}
	descriptionID := mustSearchID(t, sqlite.IDPrefixDescription)
	segmentID := mustSearchID(t, sqlite.IDPrefixSemanticSegment)
	if err := store.InsertDescriptionDocument(ctx, &sqlite.DescriptionDocument{
		ID:              descriptionID,
		WorkspaceID:     seed.WorkspaceID,
		SubjectRef:      seed.FileEntryID,
		Kind:            sqlite.DescriptionUser,
		Title:           "Personal review",
		Language:        "en",
		Body:            "A melancholy adventure about rebuilding a flooded city",
		SourceRef:       "user:test",
		ProducerProfile: "human",
		Accepted:        true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSemanticSegment(ctx, &sqlite.SemanticSegment{
		ID:          segmentID,
		WorkspaceID: seed.WorkspaceID,
		DocumentID:  descriptionID,
		SubjectRef:  seed.FileEntryID,
		Ordinal:     0,
		Text:        "A melancholy adventure about rebuilding a flooded city",
		Language:    "en",
		Section:     "body",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertProcessorAttempt(ctx, &sqlite.ProcessorAttempt{
		ID:              mustSearchID(t, sqlite.IDPrefixAttempt),
		WorkspaceID:     seed.WorkspaceID,
		SubjectRef:      seed.FileEntryID,
		SnapshotRef:     "snapshot:test",
		RouteDigest:     "sha256:route",
		Route:           json.RawMessage(`{"kind":"PROCESSING"}`),
		Stage:           "EXTRACT",
		CapabilityID:    "extract.text.v1",
		Status:          "SUCCEEDED",
		ReasonCode:      "PROCESSOR_STAGE_SUCCEEDED",
		Reason:          "",
		Provenance:      json.RawMessage(`{}`),
		FenceToken:      1,
		ProcessorDigest: "sha256:proc",
	}); err != nil {
		t.Fatal(err)
	}

	indexer := &Indexer{Store: store, Engine: &Engine{Dir: t.TempDir()}}
	generation, err := indexer.Rebuild(ctx, seed.WorkspaceID, "snapshot:test", seed.RootID)
	if err != nil {
		t.Fatal(err)
	}
	for query, axis := range map[string]string{
		"dreamcast":  AxisMetadata,
		"PROTECTED":  AxisProtection,
		"melancholy": AxisDescriptions,
		"sha256":     AxisChecksum,
		"EN":         AxisLanguage,
		"SUCCEEDED":  AxisProcessing,
		"EXTRACT":    AxisProcessing,
	} {
		hits, err := indexer.Engine.Query(ctx, generation.DBPath, query, []string{axis})
		if err != nil || len(hits) != 1 || hits[0].SubjectID != seed.FileEntryID {
			t.Fatalf("query %q on %s = %+v, err=%v", query, axis, hits, err)
		}
	}

	hits, err := indexer.Engine.Query(ctx, generation.DBPath, "flooded", nil)
	if err != nil || len(hits) != 1 || len(hits[0].Segments) != 1 {
		t.Fatalf("segment provenance = %+v err=%v", hits, err)
	}
	if ref := hits[0].Segments[0]; ref.DescriptionDocumentID != descriptionID || ref.SegmentID != segmentID ||
		ref.Kind != "USER" || !ref.Accepted {
		t.Fatalf("segment ref = %+v", ref)
	}

	// A protection-mode filter against the STORE_EXACT record must keep the
	// file, and a LINK_ONLY filter must exclude it honestly.
	kept, err := indexer.Engine.QueryFiltered(ctx, generation.DBPath, "", nil, Filters{
		ProtectionMode: "STORE_EXACT",
	})
	if err != nil || len(kept) != 1 || kept[0].SubjectID != seed.FileEntryID {
		t.Fatalf("protection filter kept = %+v err=%v", kept, err)
	}
	excluded, err := indexer.Engine.QueryFiltered(ctx, generation.DBPath, "", nil, Filters{
		ProtectionMode: "LINK_ONLY",
	})
	if err != nil || len(excluded) != 0 {
		t.Fatalf("protection filter excluded = %+v err=%v", excluded, err)
	}

	coverage, err := indexer.Coverage(ctx, seed.WorkspaceID)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if !coverage.Available || !coverage.Fields[AxisProtection] || !coverage.Fields[AxisDescriptions] ||
		!coverage.Fields[AxisProcessing] || !coverage.Fields[AxisLanguage] {
		t.Fatalf("coverage = %+v", coverage)
	}
	if coverage.Fields[AxisMtime] {
		t.Fatalf("mtime falsely present: %+v", coverage.Fields)
	}
	if !coverage.Fields[AxisSize] {
		t.Fatalf("seed carries a logical size so the size facet must be present: %+v", coverage.Fields)
	}
}

func mustSearchID(t *testing.T, prefix string) string {
	t.Helper()
	id, err := sqlite.NewStableID(prefix)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
