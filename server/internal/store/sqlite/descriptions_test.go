package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestMetadataDescriptionsAndSegmentsRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	defer store.Close()
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "descriptions"}
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertWorkspace(ctx, &workspace) }); err != nil {
		t.Fatal(err)
	}
	subject := testID(t, IDPrefixNamespaceEntry)
	fact := &MetadataFact{
		ID:             testID(t, IDPrefixMetadataFact),
		WorkspaceID:    workspace.ID,
		SubjectRef:     subject,
		Namespace:      "media.game",
		Key:            "platform",
		Value:          json.RawMessage(`"pc"`),
		ValueType:      "string",
		AuthorityClass: "USER_CONFIRMED",
		SourceRef:      "user:alice",
	}
	if err := store.InsertMetadataFact(ctx, fact); err != nil {
		t.Fatalf("insert metadata fact: %v", err)
	}
	facts, err := store.ListMetadataFacts(ctx, workspace.ID, subject)
	if err != nil || len(facts) != 1 || string(facts[0].Value) != `"pc"` {
		t.Fatalf("metadata facts = %+v, err=%v", facts, err)
	}

	doc := &DescriptionDocument{
		ID:                    testID(t, IDPrefixDescription),
		WorkspaceID:           workspace.ID,
		SubjectRef:            subject,
		Kind:                  DescriptionAISummary,
		Title:                 "剧情摘要",
		Language:              "zh",
		Body:                  "主角在废墟中寻找失落的城市。",
		SourceRef:             "model:local-summary-v1",
		ProducerProfile:       "local-summary-v1",
		ConfigDigest:          "sha256:config-description-v1",
		ProducerProfileDigest: "sha256:producer-description-v1",
		Accepted:              false,
	}
	if err := store.InsertDescriptionDocument(ctx, doc); err != nil {
		t.Fatalf("insert description: %v", err)
	}
	segment := &SemanticSegment{
		ID:                        testID(t, IDPrefixSemanticSegment),
		WorkspaceID:               workspace.ID,
		DocumentID:                doc.ID,
		SubjectRef:                subject,
		DocumentRevision:          doc.Revision,
		Ordinal:                   0,
		Text:                      doc.Body,
		Language:                  "zh",
		Section:                   "summary",
		SegmentationProfileDigest: DescriptionSegmentationProfileDigestV1,
	}
	if err := store.InsertSemanticSegment(ctx, segment); err != nil {
		t.Fatalf("insert segment: %v", err)
	}
	docs, err := store.ListDescriptionDocuments(ctx, workspace.ID, subject)
	if err != nil || len(docs) != 1 || docs[0].BodyDigest == "" || docs[0].ConfigDigest != doc.ConfigDigest || docs[0].ProducerProfileDigest != doc.ProducerProfileDigest {
		t.Fatalf("documents = %+v, err=%v", docs, err)
	}
	summaries, err := store.ListDescriptionSummaries(ctx, workspace.ID, subject, 1)
	if err != nil || len(summaries) != 1 || summaries[0].Body != "" || summaries[0].BodyDigest != doc.BodyDigest {
		t.Fatalf("description summaries = %+v, err=%v", summaries, err)
	}
	segments, err := store.ListSemanticSegments(ctx, workspace.ID, doc.ID)
	if err != nil || len(segments) != 1 || segments[0].TextDigest == "" || segments[0].DocumentRevision != doc.Revision || segments[0].SegmentationProfileDigest != DescriptionSegmentationProfileDigestV1 {
		t.Fatalf("segments = %+v, err=%v", segments, err)
	}
}

func TestDescriptionRejectsInvalidConfidence(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ":memory:")
	defer store.Close()
	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "invalid-description"}
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertWorkspace(ctx, &workspace) }); err != nil {
		t.Fatal(err)
	}
	confidence := 2.0
	err := store.InsertDescriptionDocument(ctx, &DescriptionDocument{
		ID:          testID(t, IDPrefixDescription),
		WorkspaceID: workspace.ID,
		SubjectRef:  testID(t, IDPrefixNamespaceEntry),
		Kind:        DescriptionUser,
		Body:        "body",
		Confidence:  &confidence,
	})
	if err == nil {
		t.Fatal("invalid confidence unexpectedly accepted")
	}
}
