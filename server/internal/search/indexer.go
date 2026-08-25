package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// Indexer builds and queries disposable FTS5 generations from durable catalog
// records. Deleting a generation file must not remove annotations or namespace
// rows; Rebuild creates a new generation from those records.
type Indexer struct {
	Store  *sqlite.Store
	Engine *Engine

	// SemanticProvider and SemanticZvec are optional host-owned bindings. A
	// missing or unhealthy binding leaves the exact and lexical lanes usable
	// and keeps the semantic dimension explicitly unavailable.
	SemanticProvider      SemanticEmbeddingProvider
	SemanticZvec          ZvecGenerationDriver
	SemanticLibraryPath   string
	SemanticLibraryDigest string

	// Binding fields are populated by the daemon from the resolved config.
	// An empty binding is retained for low-level fixture harnesses; daemon
	// wiring always supplies ConfigDigest and the relevant profile digest.
	ConfigDigest            string
	LexicalProfileDigest    string
	SemanticProfileDigest   string
	MultimodalProfileDigest string
	AcousticProfileDigest   string
	GraphProfileDigest      string
	SemanticSpace           string
	MultimodalSemanticSpace string
	SemanticManifest        EmbeddingGenerationManifest
	MultimodalManifest      EmbeddingGenerationManifest

	// semanticUnavailable is a host-owned health latch for the current
	// semantic binding. A failed rebuild must not leave an older disposable
	// generation looking healthy or silently serve it with an unhealthy
	// provider. A later successful rebuild clears the latch.
	semanticUnavailable atomic.Bool
	// semanticIndexReady is evidence that a real zvec generation was built or
	// opened and queried successfully. Provider embedding alone is insufficient.
	semanticIndexReady atomic.Bool
	semanticFailure    atomic.Value

	// EnableFixtureDimensions is test/qualification-only wiring for the
	// deterministic acoustic and embedding fixtures. Production readiness must
	// stay UNAVAILABLE until a real provider and vector index are installed.
	EnableFixtureDimensions bool

	mu sync.Mutex
}

func (idx *Indexer) profileDigest(dimension string) string {
	var digest string
	switch dimension {
	case DimensionLexical:
		digest = idx.LexicalProfileDigest
		if digest == "" && idx.ConfigDigest != "" {
			digest = ProfileDigest(dimension, LexicalProfileV1)
		}
	case DimensionGraph:
		digest = idx.GraphProfileDigest
		if digest == "" && idx.ConfigDigest != "" {
			digest = ProfileDigest(dimension, GraphProfileV1)
		}
	case DimensionAcoustic:
		digest = idx.AcousticProfileDigest
		if digest == "" && idx.ConfigDigest != "" && idx.EnableFixtureDimensions {
			digest = ProfileDigest(dimension, AcousticFixtureProfileV1)
		}
	case DimensionSemantic:
		if idx.SemanticManifest != (EmbeddingGenerationManifest{}) {
			digest = idx.SemanticManifest.CanonicalDigest()
		} else {
			digest = idx.SemanticProfileDigest
		}
		if digest == "" && idx.ConfigDigest != "" && idx.EnableFixtureDimensions {
			digest = ProfileDigest(dimension, SemanticFixtureProfileV1)
		}
	case DimensionMultimodal:
		if idx.MultimodalManifest != (EmbeddingGenerationManifest{}) {
			digest = idx.MultimodalManifest.CanonicalDigest()
		} else {
			digest = idx.MultimodalProfileDigest
		}
		if digest == "" && idx.ConfigDigest != "" && idx.EnableFixtureDimensions {
			digest = ProfileDigest(dimension, MultimodalFixtureProfileV1)
		}
	}
	return strings.TrimSpace(digest)
}

func (idx *Indexer) semanticSpace(dimension string) string {
	if dimension == DimensionMultimodal {
		if idx.MultimodalManifest.SemanticSpace != "" {
			return strings.TrimSpace(idx.MultimodalManifest.SemanticSpace)
		}
		return strings.TrimSpace(idx.MultimodalSemanticSpace)
	}
	if idx.SemanticManifest.SemanticSpace != "" {
		return strings.TrimSpace(idx.SemanticManifest.SemanticSpace)
	}
	return strings.TrimSpace(idx.SemanticSpace)
}

func (idx *Indexer) bindingActive() bool {
	return strings.TrimSpace(idx.ConfigDigest) != "" ||
		strings.TrimSpace(idx.LexicalProfileDigest) != "" ||
		strings.TrimSpace(idx.SemanticProfileDigest) != "" ||
		strings.TrimSpace(idx.MultimodalProfileDigest) != "" ||
		strings.TrimSpace(idx.AcousticProfileDigest) != "" ||
		strings.TrimSpace(idx.GraphProfileDigest) != "" ||
		idx.SemanticManifest != (EmbeddingGenerationManifest{}) ||
		idx.MultimodalManifest != (EmbeddingGenerationManifest{})
}

func generationBindingMatches(idx *Indexer, generation sqlite.IndexGeneration, dimension string) bool {
	if !idx.bindingActive() {
		return true
	}
	if strings.TrimSpace(generation.ConfigDigest) == "" ||
		generation.ConfigDigest != strings.TrimSpace(idx.ConfigDigest) {
		return false
	}
	manifest := idx.SemanticManifest
	if dimension == DimensionMultimodal {
		manifest = idx.MultimodalManifest
	}
	if manifest != (EmbeddingGenerationManifest{}) {
		if err := manifest.Validate(); err != nil {
			return false
		}
		if strings.TrimSpace(manifest.ConfigDigest) != strings.TrimSpace(idx.ConfigDigest) {
			return false
		}
	}
	expectedProfile := idx.profileDigest(dimension)
	if expectedProfile == "" || generation.ProviderProfileDigest != expectedProfile {
		return false
	}
	if dimension == DimensionSemantic || dimension == DimensionMultimodal {
		expectedSpace := idx.semanticSpace(dimension)
		return expectedSpace != "" && generation.SemanticSpace == expectedSpace
	}
	return true
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

	// A committed rebuild represents the current library, which may contain
	// several sources. Keep the newest published root for every source instead
	// of silently narrowing the index to the publication that triggered this
	// rebuild. Low-level callers without any publication retain the historical
	// root-scoped behavior used by import and fixture harnesses.
	_, publicationErr := idx.Store.LatestPublication(ctx, workspaceID)
	useLatestSources := publicationErr == nil
	if publicationErr != nil && !errors.Is(publicationErr, sqlite.ErrNotFound) {
		return generation, publicationErr
	}
	var (
		err   error
		nodes []sqlite.NamespaceNode
	)
	var entries []sqlite.NamespaceEntry
	if useLatestSources {
		entries, err = idx.Store.ListLatestNamespaceEntries(ctx, workspaceID)
		if err != nil {
			return generation, err
		}
		nodes = make([]sqlite.NamespaceNode, 0, len(entries))
		for _, entry := range entries {
			nodes = append(nodes, sqlite.NamespaceNode{Entry: entry})
		}
	} else {
		nodes, err = idx.Store.ListNamespaceSubtree(ctx, workspaceID, namespaceRootID, "")
		if err != nil {
			return generation, err
		}
	}
	currentSubjects := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		currentSubjects[node.Entry.ID] = struct{}{}
	}
	annotations, err := idx.Store.ListAnnotations(ctx, workspaceID, "", false)
	if err != nil {
		return generation, err
	}
	artifactSnapshotRef := snapshotRef
	if useLatestSources {
		// Derived facts are durable per subject. Loading all records here lets
		// the current-subject filter below retain facts from older sources while
		// excluding superseded roots.
		artifactSnapshotRef = ""
	}
	artifacts, err := idx.Store.ListAdmittedArtifacts(ctx, workspaceID, artifactSnapshotRef)
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
	attemptSnapshotRef := snapshotRef
	if useLatestSources {
		attemptSnapshotRef = ""
	}
	attempts, err := idx.Store.ListProcessorAttempts(ctx, workspaceID, attemptSnapshotRef)
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
		if _, ok := currentSubjects[record.SubjectRef]; !ok {
			continue
		}
		switch record.Kind {
		case sqlite.AnnotationTag:
			tagsBySubject[record.SubjectRef] = append(tagsBySubject[record.SubjectRef], record.Body)
		case sqlite.AnnotationNote:
			notesBySubject[record.SubjectRef] = append(notesBySubject[record.SubjectRef], record.Body)
			segmentsBySubject[record.SubjectRef] = append(segmentsBySubject[record.SubjectRef], SegmentRef{
				SourceType: "ANNOTATION", SourceID: record.ID,
				SegmentID: record.ID, MatchedText: record.Body,
				Kind: string(sqlite.AnnotationNote), Producer: "USER", Accepted: true, Language: "und",
			})
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
		if _, ok := currentSubjects[description.SubjectRef]; !ok {
			continue
		}
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
				SourceType:            "DESCRIPTION",
				SourceID:              description.ID,
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
		ID:                    generationID,
		WorkspaceID:           workspaceID,
		SnapshotRef:           snapshotRef,
		NamespaceRootID:       namespaceRootID,
		DBPath:                dbPath,
		Dimension:             DimensionLexical,
		ConfigDigest:          idx.ConfigDigest,
		ProviderProfileDigest: idx.profileDigest(DimensionLexical),
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
	if idx.SemanticProvider != nil && idx.SemanticZvec != nil && idx.SemanticManifest != (EmbeddingGenerationManifest{}) {
		if _, semanticErr := idx.rebuildSemantic(ctx, workspaceID, snapshotRef, namespaceRootID, segmentsBySubject); semanticErr != nil {
			// Semantic indexing is disposable derived work. Preserve the lexical
			// generation and let capability/status report the unavailable branch.
			idx.semanticUnavailable.Store(true)
			idx.semanticFailure.Store(semanticErr.Error())
		} else {
			idx.semanticUnavailable.Store(false)
			idx.semanticFailure.Store("")
		}
	}
	_, _ = idx.rebuildGraph(ctx, workspaceID, snapshotRef, namespaceRootID, byID, annotations, artifacts)
	return generation, nil
}

// SemanticFailure returns the latest real-provider rebuild failure for
// operator diagnostics. It never changes exact preservation or recovery.
func (idx *Indexer) SemanticFailure() string {
	if idx == nil {
		return ""
	}
	if health, ok := idx.SemanticProvider.(interface{ SemanticFailure() string }); ok {
		if failure := strings.TrimSpace(health.SemanticFailure()); failure != "" {
			return failure
		}
	}
	value := idx.semanticFailure.Load()
	if value == nil {
		return ""
	}
	failure, _ := value.(string)
	return failure
}

// WarmSemanticGeneration reopens the latest disposable zvec generation after
// a daemon restart. Readiness is granted only after its persisted bindings
// match the current model/config profile and the native driver opens it.
func (idx *Indexer) WarmSemanticGeneration(ctx context.Context, workspaceID string) error {
	if idx == nil || idx.Store == nil || idx.Engine == nil || idx.SemanticProvider == nil || idx.SemanticZvec == nil {
		return ErrUnavailable
	}
	idx.semanticIndexReady.Store(false)
	generation, err := idx.Store.LatestIndexGeneration(ctx, workspaceID, DimensionSemantic)
	if err != nil {
		return err
	}
	if !generationBindingMatches(idx, generation, DimensionSemantic) {
		return fmt.Errorf("%w: semantic generation binding mismatch", ErrUnavailable)
	}
	spec := ZvecGenerationSpec{
		Path: generation.DBPath, LibraryPath: idx.SemanticLibraryPath,
		LibraryDigest: idx.SemanticLibraryDigest,
		ProfileDigest: idx.SemanticManifest.CanonicalDigest(), Manifest: idx.SemanticManifest,
	}
	opened, err := idx.SemanticZvec.Open(ctx, spec)
	if err != nil {
		return fmt.Errorf("%w: open semantic generation: %v", ErrUnavailable, err)
	}
	if err := opened.Close(); err != nil {
		return fmt.Errorf("%w: close semantic generation: %v", ErrUnavailable, err)
	}
	idx.semanticUnavailable.Store(false)
	idx.semanticFailure.Store("")
	idx.semanticIndexReady.Store(true)
	return nil
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
		ID:                    generationID,
		WorkspaceID:           workspaceID,
		SnapshotRef:           snapshotRef,
		NamespaceRootID:       namespaceRootID,
		DBPath:                dbPath,
		Dimension:             DimensionGraph,
		ConfigDigest:          idx.ConfigDigest,
		ProviderProfileDigest: idx.profileDigest(DimensionGraph),
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
		ID:                    generationID,
		WorkspaceID:           workspaceID,
		SnapshotRef:           snapshotRef,
		NamespaceRootID:       namespaceRootID,
		DBPath:                dbPath,
		Dimension:             dimension,
		ConfigDigest:          idx.ConfigDigest,
		ProviderProfileDigest: idx.profileDigest(dimension),
		SemanticSpace:         idx.semanticSpace(dimension),
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
		ID:                    generationID,
		WorkspaceID:           workspaceID,
		SnapshotRef:           snapshotRef,
		NamespaceRootID:       namespaceRootID,
		DBPath:                dbPath,
		Dimension:             DimensionAcoustic,
		ConfigDigest:          idx.ConfigDigest,
		ProviderProfileDigest: idx.profileDigest(DimensionAcoustic),
	}
	if err := idx.Store.InsertIndexGeneration(ctx, &generation); err != nil {
		_ = idx.Engine.RemoveFile(dbPath)
		return sqlite.IndexGeneration{}, err
	}
	return generation, nil
}

func (idx *Indexer) rebuildSemantic(ctx context.Context, workspaceID, snapshotRef, namespaceRootID string, segmentsBySubject map[string][]SegmentRef) (sqlite.IndexGeneration, error) {
	var generation sqlite.IndexGeneration
	idx.semanticIndexReady.Store(false)
	if idx.SemanticProvider == nil || idx.SemanticZvec == nil {
		return generation, ErrUnavailable
	}
	manifest := idx.SemanticManifest
	if err := manifest.Validate(); err != nil {
		return generation, fmt.Errorf("%w: semantic manifest: %v", ErrUnavailable, err)
	}
	if strings.TrimSpace(idx.SemanticLibraryPath) == "" || strings.TrimSpace(idx.SemanticLibraryDigest) == "" {
		return generation, fmt.Errorf("%w: semantic native library binding is missing", ErrUnavailable)
	}
	if !filepath.IsAbs(idx.Engine.Dir) || filepath.Clean(idx.Engine.Dir) != idx.Engine.Dir {
		return generation, fmt.Errorf("%w: semantic index directory must be absolute and canonical", ErrUnavailable)
	}
	inputs := make([]SemanticTextInput, 0)
	for subjectID, segments := range segmentsBySubject {
		for _, segment := range segments {
			if strings.TrimSpace(segment.SegmentID) == "" || strings.TrimSpace(segment.MatchedText) == "" {
				continue
			}
			inputs = append(inputs, SemanticTextInput{
				SubjectID: subjectID, SegmentID: segment.SegmentID,
				DescriptionDocumentID: semanticSourceRevision(segment), Ordinal: segment.Ordinal,
				Language: segment.Language, Text: segment.MatchedText,
			})
		}
	}
	if len(inputs) == 0 {
		return generation, ErrUnavailable
	}
	generationID, err := sqlite.NewStableID(sqlite.IDPrefixIndexGeneration)
	if err != nil {
		return generation, err
	}
	request := SemanticEmbeddingRequest{Purpose: SemanticEmbeddingDocument, GenerationID: generationID, Manifest: manifest, Inputs: inputs}
	if err := validateSemanticEmbeddingRequest(request); err != nil {
		return generation, err
	}
	results, err := idx.SemanticProvider.Embed(ctx, request)
	if err != nil {
		return generation, fmt.Errorf("%w: provider: %v", ErrUnavailable, err)
	}
	if err := validateSemanticEmbeddingResults(request, results); err != nil {
		return generation, err
	}
	zvecSegments := make([]ZvecSegment, 0, len(results))
	for _, result := range results {
		zvecSegments = append(zvecSegments, ZvecSegment{SubjectID: result.SubjectID, SegmentID: result.SegmentID, Vector: result.Vector})
	}
	zvecPath := filepath.Join(idx.Engine.Dir, generationID+".zvec")
	spec := ZvecGenerationSpec{
		Path: zvecPath, LibraryPath: idx.SemanticLibraryPath, LibraryDigest: idx.SemanticLibraryDigest,
		ProfileDigest: manifest.CanonicalDigest(), Manifest: manifest,
	}
	receipt, err := idx.SemanticZvec.Build(ctx, spec, zvecSegments)
	if err != nil {
		idx.semanticIndexReady.Store(false)
		return generation, fmt.Errorf("%w: build generation: %v", ErrUnavailable, err)
	}
	generation = sqlite.IndexGeneration{
		ID: generationID, WorkspaceID: workspaceID, SnapshotRef: snapshotRef, NamespaceRootID: namespaceRootID,
		DBPath: receipt.Path, Dimension: DimensionSemantic, ConfigDigest: manifest.ConfigDigest,
		ProviderProfileDigest: manifest.CanonicalDigest(), SemanticSpace: manifest.SemanticSpace,
	}
	if err := idx.Store.InsertIndexGeneration(ctx, &generation); err != nil {
		idx.semanticIndexReady.Store(false)
		return sqlite.IndexGeneration{}, err
	}
	idx.semanticIndexReady.Store(true)
	return generation, nil
}

// SemanticTextInput predates generic durable text sources and retains the
// DescriptionDocumentID field name. The worker uses it only as an immutable
// provenance revision, so annotations bind their own durable source ID here.
func semanticSourceRevision(segment SegmentRef) string {
	if strings.TrimSpace(segment.DescriptionDocumentID) != "" {
		return segment.DescriptionDocumentID
	}
	return segment.SourceID
}

func (idx *Indexer) querySemantic(ctx context.Context, generation sqlite.IndexGeneration, text string) ([]Hit, error) {
	idx.semanticIndexReady.Store(false)
	if idx.SemanticProvider == nil || idx.SemanticZvec == nil {
		return nil, ErrUnavailable
	}
	manifest := idx.SemanticManifest
	if err := manifest.Validate(); err != nil {
		return nil, ErrUnavailable
	}
	request := SemanticEmbeddingRequest{Purpose: SemanticEmbeddingQuery, GenerationID: generation.ID, Manifest: manifest, Inputs: []SemanticTextInput{{SegmentID: "query", Language: "und", Text: text}}}
	if err := validateSemanticEmbeddingRequest(request); err != nil {
		return nil, err
	}
	results, err := idx.SemanticProvider.Embed(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("%w: provider: %v", ErrUnavailable, err)
	}
	if err := validateSemanticEmbeddingResults(request, results); err != nil {
		return nil, err
	}
	if len(results) != 1 {
		return nil, ErrUnavailable
	}
	spec := ZvecGenerationSpec{
		Path: generation.DBPath, LibraryPath: idx.SemanticLibraryPath, LibraryDigest: idx.SemanticLibraryDigest,
		ProfileDigest: manifest.CanonicalDigest(), Manifest: manifest,
	}
	opened, err := idx.SemanticZvec.Open(ctx, spec)
	if err != nil {
		idx.semanticIndexReady.Store(false)
		return nil, fmt.Errorf("%w: open generation: %v", ErrUnavailable, err)
	}
	defer opened.Close()
	hits, err := opened.Query(ctx, results[0].Vector, 100)
	if err != nil {
		idx.semanticIndexReady.Store(false)
		return nil, err
	}
	result := make([]Hit, 0, len(hits))
	bySubject := make(map[string]int, len(hits))
	for _, hit := range hits {
		if strings.HasPrefix(hit.SegmentID, sqlite.IDPrefixAnnotation+"_") {
			annotationID := hit.SegmentID
			annotation, annotationErr := idx.Store.GetAnnotation(ctx, generation.WorkspaceID, annotationID)
			if annotationErr != nil {
				return nil, fmt.Errorf("%w: semantic annotation provenance: %v", ErrUnavailable, annotationErr)
			}
			if annotation.SubjectRef != hit.SubjectID || annotation.Kind != sqlite.AnnotationNote || annotation.Tombstoned {
				return nil, fmt.Errorf("%w: semantic annotation %q subject or lifecycle binding mismatch", ErrUnavailable, annotation.ID)
			}
			projectedSegment := SegmentRef{
				SourceType: "ANNOTATION", SourceID: annotation.ID, SegmentID: hit.SegmentID,
				MatchedText: annotation.Body, Kind: string(annotation.Kind), Producer: "USER", Accepted: true, Language: "und",
			}
			if at, ok := bySubject[hit.SubjectID]; ok {
				result[at].Segments = appendUniqueSegment(result[at].Segments, projectedSegment)
				continue
			}
			bySubject[hit.SubjectID] = len(result)
			result = append(result, Hit{SubjectID: hit.SubjectID, Segments: []SegmentRef{projectedSegment}})
			continue
		}
		segment, segmentErr := idx.Store.GetSemanticSegment(ctx, generation.WorkspaceID, hit.SegmentID)
		if segmentErr != nil {
			return nil, fmt.Errorf("%w: semantic segment provenance: %v", ErrUnavailable, segmentErr)
		}
		if segment.SubjectRef != hit.SubjectID {
			return nil, fmt.Errorf("%w: semantic segment %q subject binding mismatch", ErrUnavailable, hit.SegmentID)
		}
		document, documentErr := idx.Store.GetDescriptionDocument(ctx, generation.WorkspaceID, segment.DocumentID)
		if documentErr != nil {
			return nil, fmt.Errorf("%w: semantic description provenance: %v", ErrUnavailable, documentErr)
		}
		if document.SubjectRef != hit.SubjectID {
			return nil, fmt.Errorf("%w: semantic description %q subject binding mismatch", ErrUnavailable, document.ID)
		}
		projectedSegment := SegmentRef{DescriptionDocumentID: document.ID, SourceType: "DESCRIPTION", SourceID: document.ID, SegmentID: segment.ID, Ordinal: segment.Ordinal, MatchedText: segment.Text, Kind: string(document.Kind), Producer: document.ProducerProfile, Accepted: document.Accepted, Language: document.Language}
		if at, ok := bySubject[hit.SubjectID]; ok {
			result[at].Segments = appendUniqueSegment(result[at].Segments, projectedSegment)
			continue
		}
		bySubject[hit.SubjectID] = len(result)
		result = append(result, Hit{SubjectID: hit.SubjectID, Segments: []SegmentRef{projectedSegment}})
	}
	idx.semanticIndexReady.Store(true)
	return result, nil
}

func appendUniqueSegment(segments []SegmentRef, candidate SegmentRef) []SegmentRef {
	for _, existing := range segments {
		if existing.SegmentID == candidate.SegmentID {
			return segments
		}
	}
	return append(segments, candidate)
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
	filters, err := NormalizeFilters(req.Filters)
	if err != nil {
		return generation, nil, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}
	req.Filters = filters
	dimension := strings.TrimSpace(req.Dimension)
	if dimension == "" {
		dimension = DimensionLexical
	}
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
	if !generationBindingMatches(idx, generation, dimension) {
		return generation, nil, ErrUnavailable
	}
	if workspaceID := strings.TrimSpace(req.WorkspaceID); workspaceID != "" &&
		strings.TrimSpace(generation.WorkspaceID) != "" && workspaceID != generation.WorkspaceID {
		// A pinned generation is still scoped to the workspace that published
		// it. Do not let an internal caller use a valid index from another
		// workspace merely because it knows the generation ID.
		return generation, nil, ErrUnavailable
	}
	var hits []Hit
	switch dimension {
	case DimensionAcoustic:
		hits, err = idx.Engine.QueryAcoustic(ctx, generation.DBPath, req.Text)
	case DimensionSemantic:
		if idx.semanticUnavailable.Load() && idx.SemanticManifest != (EmbeddingGenerationManifest{}) {
			err = ErrUnavailable
		} else if !idx.semanticUnavailable.Load() && idx.SemanticProvider != nil && idx.SemanticZvec != nil &&
			idx.SemanticManifest != (EmbeddingGenerationManifest{}) &&
			generation.ProviderProfileDigest == idx.SemanticManifest.CanonicalDigest() {
			hits, err = idx.querySemantic(ctx, generation, req.Text)
		} else if idx.SemanticManifest != (EmbeddingGenerationManifest{}) && idx.SemanticManifest.SemanticSpace != SemanticFixtureProfileV1 {
			// A real semantic binding cannot silently fall back to the legacy
			// fixture token projection when its provider or vector store is gone.
			err = ErrUnavailable
		} else {
			hits, err = idx.Engine.QueryTokens(ctx, generation.DBPath, queryToken(dimension, req.Text))
		}
	case DimensionMultimodal:
		if idx.MultimodalManifest != (EmbeddingGenerationManifest{}) &&
			idx.MultimodalManifest.SemanticSpace != MultimodalFixtureProfileV1 {
			// A real multimodal binding cannot silently fall back to a legacy
			// token projection when its provider or vector store is unavailable.
			err = ErrUnavailable
		} else {
			hits, err = idx.Engine.QueryTokens(ctx, generation.DBPath, queryToken(dimension, req.Text))
		}
	case DimensionGraph:
		hits, err = idx.Engine.QueryGraph(ctx, generation.DBPath, req.Text)
	default:
		hits, err = idx.Engine.QueryFiltered(ctx, generation.DBPath, req.Text, req.Axes, req.Filters)
	}
	if err != nil {
		return generation, nil, err
	}
	if dimension != DimensionLexical && filters.Has() {
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		if workspaceID == "" {
			workspaceID = generation.WorkspaceID
		}
		hits, err = idx.filterProviderHits(ctx, workspaceID, generation, hits, filters)
		if err != nil {
			return generation, nil, err
		}
	}
	return generation, hits, nil
}

// filterProviderHits makes the matching lexical generation the host-owned
// authority for typed facets. Only the provider's already-bounded candidates
// are checked, so a workspace-wide result cap cannot omit an otherwise valid
// provider hit.
func (idx *Indexer) filterProviderHits(ctx context.Context, workspaceID string, generation sqlite.IndexGeneration, hits []Hit, filters Filters) ([]Hit, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("%w: workspace is required for filtered provider query", ErrUnavailable)
	}
	lexical, err := idx.Store.LatestIndexGeneration(ctx, workspaceID, DimensionLexical)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return nil, ErrUnavailable
		}
		return nil, err
	}
	if lexical.WorkspaceID != generation.WorkspaceID ||
		lexical.SnapshotRef != generation.SnapshotRef ||
		lexical.NamespaceRootID != generation.NamespaceRootID {
		// Filters are facts from one immutable index generation. Applying a
		// newer or differently rooted lexical projection to a pinned provider
		// result could authorize the wrong subject set, so fail closed.
		return nil, ErrUnavailable
	}
	if !generationBindingMatches(idx, lexical, DimensionLexical) {
		// The lexical projection is the authority for typed facets. A provider
		// generation must never be authorized by a projection from another
		// configuration or provider profile.
		return nil, ErrUnavailable
	}
	filtered, err := idx.Engine.filterCandidates(ctx, lexical.DBPath, hits, filters)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return nil, ErrUnavailable
		}
		return nil, err
	}
	return filtered, nil
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
