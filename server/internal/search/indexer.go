package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// Indexer builds and queries disposable FTS5 generations from durable catalog
// records. Deleting a generation file must not remove annotations or namespace
// rows; Rebuild creates a new generation from those records.
type Indexer struct {
	Store  *sqlite.Store
	Engine *Engine

	// EnableFixtureDimensions is test/qualification-only wiring for the
	// deterministic acoustic and embedding fixtures. Production readiness must
	// stay UNAVAILABLE until a real provider and vector index are installed.
	EnableFixtureDimensions bool

	mu sync.Mutex
}

// Rebuild writes one new FTS5 database for the given namespace root and
// records its path in the operational catalog.
func (idx *Indexer) Rebuild(ctx context.Context, workspaceID, snapshotRef, namespaceRootID string) (sqlite.IndexGeneration, error) {
	var generation sqlite.IndexGeneration
	if idx == nil || idx.Store == nil || idx.Engine == nil {
		return generation, errors.New("search indexer requires a catalog and engine")
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()

	nodes, err := idx.Store.ListNamespaceSubtree(ctx, workspaceID, namespaceRootID, "")
	if err != nil {
		return generation, err
	}
	annotations, err := idx.Store.ListAnnotations(ctx, workspaceID, "", false)
	if err != nil {
		return generation, err
	}
	artifacts, err := idx.Store.ListAdmittedArtifacts(ctx, workspaceID, snapshotRef)
	if err != nil {
		return generation, err
	}
	metadataFacts, err := idx.Store.ListMetadataFacts(ctx, workspaceID, "")
	if err != nil {
		return generation, err
	}
	protections, err := idx.Store.ListProtectionRecords(ctx, workspaceID)
	if err != nil {
		return generation, err
	}
	descriptions, err := idx.Store.ListDescriptionDocuments(ctx, workspaceID, "")
	if err != nil {
		return generation, err
	}
	bindings, err := idx.Store.ListExternalBindings(ctx, workspaceID, "")
	if err != nil {
		return generation, err
	}
	attempts, err := idx.Store.ListProcessorAttempts(ctx, workspaceID, snapshotRef)
	if err != nil {
		return generation, err
	}
	recoveryRefs, err := idx.Store.ListRecoveryReferences(ctx, workspaceID, "")
	if err != nil {
		return generation, err
	}
	tagsBySubject := map[string][]string{}
	notesBySubject := map[string][]string{}
	extractedBySubject := map[string][]string{}
	metadataBySubject := map[string][]string{}
	protectionBySubject := map[string][]string{}
	processingBySubject := map[string][]string{}
	representationsByContent := map[string][]string{}
	descriptionsBySubject := map[string][]string{}
	segmentsBySubject := map[string][]SegmentRef{}
	locatorsBySubject := map[string][]string{}
	recoveryBySubject := map[string][]string{}
	duplicateGroupBySubject := map[string]string{}
	for _, record := range annotations {
		switch record.Kind {
		case sqlite.AnnotationTag:
			tagsBySubject[record.SubjectRef] = append(tagsBySubject[record.SubjectRef], record.Body)
		case sqlite.AnnotationNote:
			notesBySubject[record.SubjectRef] = append(notesBySubject[record.SubjectRef], record.Body)
		}
	}
	for _, artifact := range artifacts {
		if artifact.Stage == "EXTRACT" && artifact.Body != "" {
			extractedBySubject[artifact.SubjectRef] = append(extractedBySubject[artifact.SubjectRef], artifact.Body)
		}
	}
	for _, fact := range metadataFacts {
		metadataBySubject[fact.SubjectRef] = append(metadataBySubject[fact.SubjectRef],
			fact.Namespace, fact.Key, string(fact.Value), fact.ValueType)
	}
	for _, protection := range protections {
		protectionBySubject[protection.SubjectRef] = append(protectionBySubject[protection.SubjectRef],
			string(protection.Mode), string(protection.Outcome), protection.ExpectedContentID)
	}
	for _, attempt := range attempts {
		processingBySubject[attempt.SubjectRef] = append(processingBySubject[attempt.SubjectRef],
			attempt.Stage, attempt.CapabilityID, attempt.Status, attempt.ReasonCode)
	}
	for _, description := range descriptions {
		descriptionsBySubject[description.SubjectRef] = append(descriptionsBySubject[description.SubjectRef],
			description.Title, description.Language, description.Body, string(description.Kind), description.SourceRef)
		if description.ID == "" {
			continue
		}
		segments, listErr := idx.Store.ListSemanticSegments(ctx, workspaceID, description.ID)
		if listErr != nil {
			return generation, listErr
		}
		for _, segment := range segments {
			segmentsBySubject[description.SubjectRef] = append(segmentsBySubject[description.SubjectRef], SegmentRef{
				DescriptionDocumentID: description.ID,
				SegmentID:             segment.ID,
				Ordinal:               segment.Ordinal,
				MatchedText:           segment.Text,
				Kind:                  string(description.Kind),
				Producer:              description.ProducerProfile,
				Accepted:              description.Accepted,
				Language:              description.Language,
			})
		}
	}
	for _, binding := range bindings {
		locatorsBySubject[binding.SubjectRef] = append(locatorsBySubject[binding.SubjectRef],
			binding.ProviderKind, binding.StableIdentity)
		locators, listErr := idx.Store.ListExternalLocators(ctx, workspaceID, binding.ID)
		if listErr != nil {
			return generation, listErr
		}
		for _, locator := range locators {
			locatorsBySubject[binding.SubjectRef] = append(locatorsBySubject[binding.SubjectRef],
				locator.Kind, locator.DisplayLocator, locator.ExpectedContentID,
				locator.Availability, locator.ValidationStatus)
		}
	}
	for _, reference := range recoveryRefs {
		recoveryBySubject[reference.SubjectRef] = append(recoveryBySubject[reference.SubjectRef],
			string(reference.Kind), string(reference.Claim), reference.Status)
	}
	byID := make(map[string]sqlite.NamespaceEntry, len(nodes))
	for _, node := range nodes {
		byID[node.Entry.ID] = node.Entry
	}
	contentCounts := make(map[string]int)
	for _, node := range nodes {
		if node.Entry.ContentID != "" {
			contentCounts[node.Entry.ContentID]++
		}
	}
	for _, node := range nodes {
		if node.Entry.ContentID != "" && contentCounts[node.Entry.ContentID] > 1 {
			duplicateGroupBySubject[node.Entry.ID] = node.Entry.ContentID
		}
	}
	for contentID := range contentCounts {
		representations, listErr := idx.Store.ListRepresentationsByContentID(ctx, workspaceID, contentID)
		if listErr != nil {
			return generation, listErr
		}
		for _, representation := range representations {
			representationsByContent[contentID] = append(representationsByContent[contentID],
				representation.CodecProfileRef, string(representation.AccessMode),
				string(representation.OwnershipMode), representation.RecordDigest)
		}
	}
	docs := make([]Document, 0, len(nodes))
	for _, node := range nodes {
		entry := node.Entry
		metadata := append([]string{string(entry.Metadata)}, metadataBySubject[entry.ID]...)
		duplicate := ""
		if count := contentCounts[entry.ContentID]; entry.ContentID != "" && count > 1 {
			duplicate = fmt.Sprintf("duplicate duplicates same-content count %d %s", count, entry.ContentID)
		}
		segments := segmentsBySubject[entry.ID]
		if len(segments) == 0 {
			segments = nil
		}
		segmentsJSON := ""
		if len(segments) > 0 {
			encoded, marshalErr := json.Marshal(segments)
			if marshalErr != nil {
				return generation, marshalErr
			}
			segmentsJSON = string(encoded)
		}
		detection := ""
		if entry.ObservationID != "" {
			observation, obsErr := idx.Store.GetObservation(ctx, workspaceID, entry.ObservationID)
			if obsErr == nil {
				detection = observation.ReadState
			}
		}
		doc := Document{
			SubjectID:       entry.ID,
			Path:            displayPath(byID, entry),
			Name:            entry.DisplayName,
			Suffix:          suffixOf(entry.DisplayName),
			EntryType:       string(entry.EntryType),
			ContentID:       entry.ContentID,
			Metadata:        strings.Join(metadata, " "),
			Duplicates:      duplicate,
			DuplicateGroup:  duplicateGroupBySubject[entry.ID],
			Protection:      strings.Join(append(append([]string{}, protectionBySubject[entry.ID]...), recoveryBySubject[entry.ID]...), " "),
			Locators:        strings.Join(locatorsBySubject[entry.ID], " "),
			Tags:            strings.Join(tagsBySubject[entry.ID], " "),
			Notes:           strings.Join(notesBySubject[entry.ID], " "),
			Descriptions:    strings.Join(descriptionsBySubject[entry.ID], " "),
			Extracted:       strings.Join(extractedBySubject[entry.ID], " "),
			Detection:       detection,
			Processing:      strings.Join(processingBySubject[entry.ID], " "),
			Representations: strings.Join(representationsByContent[entry.ContentID], " "),
			Language:        subjectLanguage(descriptionsBySubject[entry.ID]),
			Segments:        segmentsJSON,
		}
		if entry.LogicalSize != nil {
			size := *entry.LogicalSize
			doc.LogicalSize = &size
		}
		if observation, ok := observationMtimeMillis(ctx, idx.Store, workspaceID, entry.ObservationID); ok {
			doc.MtimeMillis = &observation
		}
		docs = append(docs, doc)
	}

	generationID, err := sqlite.NewStableID(sqlite.IDPrefixIndexGeneration)
	if err != nil {
		return generation, err
	}
	dbPath, err := idx.Engine.Build(ctx, generationID, docs)
	if err != nil {
		return generation, err
	}
	generation = sqlite.IndexGeneration{
		ID:              generationID,
		WorkspaceID:     workspaceID,
		SnapshotRef:     snapshotRef,
		NamespaceRootID: namespaceRootID,
		DBPath:          dbPath,
		Dimension:       DimensionLexical,
	}
	if err := idx.Store.InsertIndexGeneration(ctx, &generation); err != nil {
		_ = idx.Engine.RemoveFile(dbPath)
		return sqlite.IndexGeneration{}, err
	}
	if idx.EnableFixtureDimensions {
		_, _ = idx.rebuildAcoustic(ctx, workspaceID, snapshotRef, namespaceRootID, byID, artifacts)
		_, _ = idx.rebuildTokens(ctx, workspaceID, snapshotRef, namespaceRootID, byID, artifacts, DimensionSemantic, "ENRICH", "embed.text.fixture.v1")
		_, _ = idx.rebuildTokens(ctx, workspaceID, snapshotRef, namespaceRootID, byID, artifacts, DimensionMultimodal, "ENRICH", "embed.clip.fixture.v1")
	}
	_, _ = idx.rebuildGraph(ctx, workspaceID, snapshotRef, namespaceRootID, byID, annotations, artifacts)
	return generation, nil
}

func (idx *Indexer) rebuildGraph(ctx context.Context, workspaceID, snapshotRef, namespaceRootID string, byID map[string]sqlite.NamespaceEntry, annotations []sqlite.Annotation, artifacts []sqlite.ProcessorArtifact) (sqlite.IndexGeneration, error) {
	var generation sqlite.IndexGeneration
	if idx.Engine == nil {
		return generation, errors.New("search indexer requires an engine")
	}
	edges := make([]GraphEdge, 0)
	for _, entry := range byID {
		path := displayPath(byID, entry)
		if entry.ParentID != "" {
			edges = append(edges, GraphEdge{
				SubjectID: entry.ID,
				Relation:  RelContains,
				Value:     entry.ParentID,
				Path:      path,
				Name:      entry.DisplayName,
				EntryType: string(entry.EntryType),
				ContentID: entry.ContentID,
			})
		}
		if entry.ContentID != "" && entry.EntryType == sqlite.EntryFile {
			edges = append(edges, GraphEdge{
				SubjectID: entry.ID,
				Relation:  RelSameContent,
				Value:     entry.ContentID,
				Path:      path,
				Name:      entry.DisplayName,
				EntryType: string(entry.EntryType),
				ContentID: entry.ContentID,
			})
		}
	}
	for _, record := range annotations {
		if record.Kind != sqlite.AnnotationTag || record.Tombstoned || record.Body == "" {
			continue
		}
		entry, ok := byID[record.SubjectRef]
		if !ok {
			continue
		}
		edges = append(edges, GraphEdge{
			SubjectID: entry.ID,
			Relation:  RelTagged,
			Value:     record.Body,
			Path:      displayPath(byID, entry),
			Name:      entry.DisplayName,
			EntryType: string(entry.EntryType),
			ContentID: entry.ContentID,
		})
	}
	for _, artifact := range artifacts {
		entry, ok := byID[artifact.SubjectRef]
		if !ok {
			continue
		}
		path := displayPath(byID, entry)
		switch artifact.CapabilityID {
		case "extract.audio.tags.v1":
			artist, album := parseAudioLabels(artifact.Body)
			if artist != "" {
				edges = append(edges, GraphEdge{
					SubjectID: entry.ID, Relation: RelArtist, Value: artist,
					Path: path, Name: entry.DisplayName, EntryType: string(entry.EntryType), ContentID: entry.ContentID,
				})
			}
			if album != "" {
				edges = append(edges, GraphEdge{
					SubjectID: entry.ID, Relation: RelAlbum, Value: album,
					Path: path, Name: entry.DisplayName, EntryType: string(entry.EntryType), ContentID: entry.ContentID,
				})
			}
		case "extract.book.meta.v1":
			if author := parseAuthorLabel(artifact.Body); author != "" {
				edges = append(edges, GraphEdge{
					SubjectID: entry.ID, Relation: RelAuthor, Value: author,
					Path: path, Name: entry.DisplayName, EntryType: string(entry.EntryType), ContentID: entry.ContentID,
				})
			}
		}
	}
	if len(edges) == 0 {
		return generation, nil
	}
	generationID, err := sqlite.NewStableID(sqlite.IDPrefixIndexGeneration)
	if err != nil {
		return generation, err
	}
	dbPath, err := idx.Engine.BuildGraph(ctx, generationID, edges)
	if err != nil {
		return generation, err
	}
	generation = sqlite.IndexGeneration{
		ID:              generationID,
		WorkspaceID:     workspaceID,
		SnapshotRef:     snapshotRef,
		NamespaceRootID: namespaceRootID,
		DBPath:          dbPath,
		Dimension:       DimensionGraph,
	}
	if err := idx.Store.InsertIndexGeneration(ctx, &generation); err != nil {
		_ = idx.Engine.RemoveFile(dbPath)
		return sqlite.IndexGeneration{}, err
	}
	return generation, nil
}

func (idx *Indexer) rebuildTokens(ctx context.Context, workspaceID, snapshotRef, namespaceRootID string, byID map[string]sqlite.NamespaceEntry, artifacts []sqlite.ProcessorArtifact, dimension, stage, capabilityID string) (sqlite.IndexGeneration, error) {
	var generation sqlite.IndexGeneration
	if idx.Engine == nil {
		return generation, errors.New("search indexer requires an engine")
	}
	docs := make([]TokenDocument, 0)
	for _, artifact := range artifacts {
		if artifact.Stage != stage || artifact.CapabilityID != capabilityID {
			continue
		}
		token, space, ok := parseFeatureArtifact(artifact.Body)
		if !ok {
			continue
		}
		entry, ok := byID[artifact.SubjectRef]
		if !ok {
			continue
		}
		docs = append(docs, TokenDocument{
			SubjectID: entry.ID,
			Token:     token,
			Space:     space,
			Path:      displayPath(byID, entry),
			Name:      entry.DisplayName,
			EntryType: string(entry.EntryType),
			ContentID: entry.ContentID,
		})
	}
	if len(docs) == 0 {
		return generation, nil
	}
	generationID, err := sqlite.NewStableID(sqlite.IDPrefixIndexGeneration)
	if err != nil {
		return generation, err
	}
	dbPath, err := idx.Engine.BuildTokens(ctx, generationID, docs)
	if err != nil {
		return generation, err
	}
	generation = sqlite.IndexGeneration{
		ID:              generationID,
		WorkspaceID:     workspaceID,
		SnapshotRef:     snapshotRef,
		NamespaceRootID: namespaceRootID,
		DBPath:          dbPath,
		Dimension:       dimension,
	}
	if err := idx.Store.InsertIndexGeneration(ctx, &generation); err != nil {
		_ = idx.Engine.RemoveFile(dbPath)
		return sqlite.IndexGeneration{}, err
	}
	return generation, nil
}

func (idx *Indexer) rebuildAcoustic(ctx context.Context, workspaceID, snapshotRef, namespaceRootID string, byID map[string]sqlite.NamespaceEntry, artifacts []sqlite.ProcessorArtifact) (sqlite.IndexGeneration, error) {
	var generation sqlite.IndexGeneration
	if idx.Engine == nil {
		return generation, errors.New("search indexer requires an engine")
	}
	docs := make([]AcousticDocument, 0)
	for _, artifact := range artifacts {
		if artifact.Stage != "FINGERPRINT" {
			continue
		}
		fingerprint, algorithm, ok := parseFingerprintArtifact(artifact.Body)
		if !ok {
			continue
		}
		entry, ok := byID[artifact.SubjectRef]
		if !ok {
			continue
		}
		docs = append(docs, AcousticDocument{
			SubjectID:   entry.ID,
			Fingerprint: fingerprint,
			Algorithm:   algorithm,
			Path:        displayPath(byID, entry),
			Name:        entry.DisplayName,
			EntryType:   string(entry.EntryType),
			ContentID:   entry.ContentID,
		})
	}
	if len(docs) == 0 {
		return generation, nil
	}
	generationID, err := sqlite.NewStableID(sqlite.IDPrefixIndexGeneration)
	if err != nil {
		return generation, err
	}
	dbPath, err := idx.Engine.BuildAcoustic(ctx, generationID, docs)
	if err != nil {
		return generation, err
	}
	generation = sqlite.IndexGeneration{
		ID:              generationID,
		WorkspaceID:     workspaceID,
		SnapshotRef:     snapshotRef,
		NamespaceRootID: namespaceRootID,
		DBPath:          dbPath,
		Dimension:       DimensionAcoustic,
	}
	if err := idx.Store.InsertIndexGeneration(ctx, &generation); err != nil {
		_ = idx.Engine.RemoveFile(dbPath)
		return sqlite.IndexGeneration{}, err
	}
	return generation, nil
}

// RebuildLatest indexes the newest publication for workspaceID. If the
// workspace has no publication yet, it is a no-op.
func (idx *Indexer) RebuildLatest(ctx context.Context, workspaceID string) (sqlite.IndexGeneration, error) {
	var generation sqlite.IndexGeneration
	if idx == nil || idx.Store == nil {
		return generation, errors.New("search indexer requires a catalog")
	}
	publication, err := idx.Store.LatestPublication(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return generation, nil
		}
		return generation, err
	}
	return idx.Rebuild(ctx, publication.WorkspaceID, publication.SnapshotRef, publication.NamespaceRootID)
}

// Query runs one lexical query against a named generation, or the latest
// generation when generationID is empty. Missing generation files return
// ErrUnavailable without touching durable catalog rows.
func (idx *Indexer) Query(ctx context.Context, req QueryRequest) (sqlite.IndexGeneration, []Hit, error) {
	var generation sqlite.IndexGeneration
	if idx == nil || idx.Store == nil || idx.Engine == nil {
		return generation, nil, errors.New("search indexer requires a catalog and engine")
	}
	dimension := strings.TrimSpace(req.Dimension)
	if dimension == "" {
		dimension = DimensionLexical
	}
	var err error
	if strings.TrimSpace(req.GenerationID) == "" {
		generation, err = idx.Store.LatestIndexGeneration(ctx, req.WorkspaceID, dimension)
	} else {
		generation, err = idx.Store.GetIndexGeneration(ctx, req.GenerationID)
	}
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return generation, nil, ErrUnavailable
		}
		return generation, nil, err
	}
	if generation.Dimension != "" && generation.Dimension != dimension {
		return generation, nil, ErrUnavailable
	}
	var hits []Hit
	switch dimension {
	case DimensionAcoustic:
		hits, err = idx.Engine.QueryAcoustic(ctx, generation.DBPath, req.Text)
	case DimensionSemantic, DimensionMultimodal:
		hits, err = idx.Engine.QueryTokens(ctx, generation.DBPath, queryToken(dimension, req.Text))
	case DimensionGraph:
		hits, err = idx.Engine.QueryGraph(ctx, generation.DBPath, req.Text)
	default:
		hits, err = idx.Engine.QueryFiltered(ctx, generation.DBPath, req.Text, req.Axes, req.Filters)
	}
	if err != nil {
		return generation, nil, err
	}
	return generation, hits, nil
}

func displayPath(byID map[string]sqlite.NamespaceEntry, entry sqlite.NamespaceEntry) string {
	var parts []string
	current := entry
	for {
		if current.DisplayName != "" {
			parts = append([]string{current.DisplayName}, parts...)
		}
		if current.ParentID == "" {
			break
		}
		parent, ok := byID[current.ParentID]
		if !ok {
			break
		}
		current = parent
	}
	return strings.Join(parts, "/")
}

func suffixOf(name string) string {
	ext := filepath.Ext(name)
	return strings.TrimPrefix(strings.ToLower(ext), ".")
}

// subjectLanguage reports the first non-empty language declared by any
// description revision attached to a subject. Absence stays empty; the feed
// never invents a language.
func subjectLanguage(descriptions []string) string {
	// descriptions entries are title, language, body, kind, sourceRef tuples.
	for i := 1; i < len(descriptions); i += 5 {
		if language := strings.TrimSpace(descriptions[i]); language != "" {
			return language
		}
	}
	return ""
}

// observationMtimeMillis returns the observation observed-at timestamp in
// milliseconds since the Unix epoch. The catalog observation timestamp is the
// closest honest mtime facet available for a namespace entry; when the entry
// has no observation, the facet is absent.
func observationMtimeMillis(ctx context.Context, store *sqlite.Store, workspaceID, observationID string) (int64, bool) {
	if observationID == "" {
		return 0, false
	}
	observation, err := store.GetObservation(ctx, workspaceID, observationID)
	if err != nil {
		return 0, false
	}
	if observation.ObservedAt.IsZero() {
		return 0, false
	}
	return observation.ObservedAt.UnixMilli(), true
}
