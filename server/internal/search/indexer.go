package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

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
	// semanticIndexReady is evidence that a real generation passed complete
	// coverage verification for the current profile/binding. Provider embedding
	// or a local query result alone is insufficient.
	semanticIndexReady atomic.Bool
	semanticFailure    atomic.Value

	// semanticVerified records immutable backend coverage evidence per
	// generation. It is process-local and disposable: durable generation
	// metadata remains the recovery/index authority, while this cache avoids
	// re-enumerating a frozen collection for every query.
	semanticVerifiedMu sync.RWMutex
	semanticVerified   map[string]semanticGenerationReceipt
	semanticVerifyMu   sync.Mutex

	// EnableFixtureDimensions is test/qualification-only wiring for the
	// deterministic acoustic and embedding fixtures. Production readiness must
	// stay UNAVAILABLE until a real provider and vector index are installed.
	EnableFixtureDimensions bool

	mu sync.Mutex
}

type semanticGenerationReceipt struct {
	GenerationID   string
	DBPath         string
	ConfigDigest   string
	ProfileDigest  string
	SemanticSpace  string
	LibraryPath    string
	LibraryDigest  string
	IdentityCount  int
	IdentityDigest string
}

func (idx *Indexer) cachedSemanticGeneration(generation sqlite.IndexGeneration) (int, bool) {
	if idx == nil || strings.TrimSpace(generation.ID) == "" {
		return 0, false
	}
	idx.semanticVerifiedMu.RLock()
	receipt, ok := idx.semanticVerified[generation.ID]
	idx.semanticVerifiedMu.RUnlock()
	if !ok || receipt.DBPath != generation.DBPath || receipt.ConfigDigest != generation.ConfigDigest ||
		receipt.ProfileDigest != generation.ProviderProfileDigest || receipt.SemanticSpace != generation.SemanticSpace ||
		receipt.LibraryPath != idx.SemanticLibraryPath || receipt.LibraryDigest != idx.SemanticLibraryDigest {
		return 0, false
	}
	// Re-read the immutable receipt on every fast-path use. This preserves the
	// no-full-enumeration query performance contract while ensuring a changed,
	// truncated, or rebound sidecar cannot keep a cached generation healthy.
	identities, identityDigest, libraryDigest, err := readSemanticGenerationReceipt(generation)
	if err != nil || len(identities) != receipt.IdentityCount || identityDigest != receipt.IdentityDigest || libraryDigest != idx.SemanticLibraryDigest {
		return 0, false
	}
	return receipt.IdentityCount, true
}

func (idx *Indexer) cacheSemanticGeneration(generation sqlite.IndexGeneration, identities []ZvecCoverageIdentity) {
	if idx == nil || strings.TrimSpace(generation.ID) == "" {
		return
	}
	idx.semanticVerifiedMu.Lock()
	if idx.semanticVerified == nil {
		idx.semanticVerified = make(map[string]semanticGenerationReceipt)
	}
	idx.semanticVerified[generation.ID] = semanticGenerationReceipt{
		GenerationID: generation.ID, DBPath: generation.DBPath, ConfigDigest: generation.ConfigDigest,
		ProfileDigest: generation.ProviderProfileDigest, SemanticSpace: generation.SemanticSpace,
		LibraryPath: idx.SemanticLibraryPath, LibraryDigest: idx.SemanticLibraryDigest,
		IdentityCount: len(identities), IdentityDigest: semanticCoverageDigest(identities),
	}
	idx.semanticVerifiedMu.Unlock()
}

func (idx *Indexer) revokeSemanticGeneration(generationID string) {
	if idx == nil || strings.TrimSpace(generationID) == "" {
		return
	}
	idx.semanticVerifiedMu.Lock()
	delete(idx.semanticVerified, generationID)
	idx.semanticVerifiedMu.Unlock()
	// Readiness belongs to the currently selected/latest generation. A failed
	// query of a pinned generation must never be able to resurrect capability
	// from some other (possibly stale) receipt.
	idx.semanticIndexReady.Store(false)
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
	var scopeRoots []sqlite.NamespaceRoot
	if useLatestSources {
		scopeRoots, err = idx.Store.ListLatestNamespaceRoots(ctx, workspaceID)
		if err != nil {
			return generation, err
		}
		entries, err = idx.Store.ListLatestNamespaceEntries(ctx, workspaceID)
		if err != nil {
			return generation, err
		}
		nodes = make([]sqlite.NamespaceNode, 0, len(entries))
		for _, entry := range entries {
			nodes = append(nodes, sqlite.NamespaceNode{Entry: entry})
		}
	} else {
		root, rootErr := idx.Store.GetNamespaceRoot(ctx, workspaceID, namespaceRootID)
		if rootErr != nil {
			return generation, rootErr
		}
		scopeRoots = []sqlite.NamespaceRoot{root}
		nodes, err = idx.Store.ListNamespaceSubtree(ctx, workspaceID, namespaceRootID, "")
		if err != nil {
			return generation, err
		}
	}
	currentSubjects := make(map[string]struct{}, len(nodes))
	byID := make(map[string]sqlite.NamespaceEntry, len(nodes))
	entriesByObservation := make(map[string]sqlite.NamespaceEntry, len(nodes))
	// rootsByID is the immutable namespace scope captured by this generation.
	// Do not resolve provenance through the current latest projection: a pinned
	// generation must remain readable after a later publication replaces one of
	// these roots.
	rootsByID := make(map[string]sqlite.NamespaceRoot, len(scopeRoots))
	rootIDs := make([]string, 0, len(scopeRoots))
	for _, root := range scopeRoots {
		if root.WorkspaceID != workspaceID || strings.TrimSpace(root.ID) == "" || strings.TrimSpace(root.SnapshotRef) == "" {
			return generation, fmt.Errorf("namespace root %q is not valid for workspace %q", root.ID, workspaceID)
		}
		if _, duplicate := rootsByID[root.ID]; duplicate {
			return generation, fmt.Errorf("duplicate namespace root %q in generation scope", root.ID)
		}
		rootsByID[root.ID] = root
		rootIDs = append(rootIDs, root.ID)
	}
	sort.Strings(rootIDs)
	if len(rootIDs) == 0 {
		return generation, errors.New("namespace root scope is empty")
	}
	if _, ok := rootsByID[namespaceRootID]; !ok {
		return generation, fmt.Errorf("primary namespace root %q is outside generation scope", namespaceRootID)
	}
	generationSnapshotRef := snapshotRef
	if primaryRoot := rootsByID[namespaceRootID]; strings.TrimSpace(primaryRoot.SnapshotRef) != "" {
		// Bind the generation to the snapshot actually captured by its primary
		// root. The rebuild trigger label is not authoritative provenance.
		generationSnapshotRef = primaryRoot.SnapshotRef
	}
	for index := range nodes {
		entry := nodes[index].Entry
		if strings.TrimSpace(entry.SubjectRef) == "" {
			entry.SubjectRef = entry.ID
		}
		byID[entry.ID] = entry
		if entry.ObservationID != "" {
			entriesByObservation[entry.ObservationID] = entry
		}
		currentSubjects[entry.SubjectRef] = struct{}{}
		if _, ok := rootsByID[entry.RootID]; !ok {
			return generation, fmt.Errorf("namespace entry %q is outside generation root scope", entry.ID)
		}
		nodes[index].Entry = entry
	}
	canonicalSubject := func(ref string) string {
		if entry, ok := byID[ref]; ok {
			return entry.SubjectRef
		}
		return ref
	}
	annotations, err := idx.Store.ListAnnotations(ctx, workspaceID, "", false)
	if err != nil {
		return generation, err
	}
	// Derived facts are durable per subject and are admitted below only when
	// their own snapshot resolves into this exact root scope. Filtering by the
	// generation trigger snapshot would discard valid cross-source artifacts.
	artifacts, err := idx.Store.ListAdmittedArtifacts(ctx, workspaceID, "")
	if err != nil {
		return generation, err
	}
	for index := range artifacts {
		artifacts[index].SubjectRef = canonicalSubject(artifacts[index].SubjectRef)
	}
	metadataFacts, err := idx.Store.ListMetadataFacts(ctx, workspaceID, "")
	if err != nil {
		return generation, err
	}
	detectionEvidence, err := idx.Store.ListDetectionEvidence(ctx, workspaceID, "")
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
	for index := range annotations {
		annotations[index].SubjectRef = canonicalSubject(annotations[index].SubjectRef)
	}
	tagsBySubject := map[string][]string{}
	notesBySubject := map[string][]string{}
	extractedBySubject := map[string][]string{}
	metadataBySubject := map[string][]string{}
	detectionBySubject := map[string][]string{}
	protectionBySubject := map[string][]string{}
	processingBySubject := map[string][]string{}
	representationsByContent := map[string][]string{}
	descriptionsBySubject := map[string][]string{}
	// segmentsBySubject is the lexical provenance projection and intentionally
	// retains every durable description revision. Semantic segments are a
	// narrower active-leaf projection built in parallel below.
	segmentsBySubject := map[string][]SegmentRef{}
	semanticSegmentsBySubject := map[string][]SegmentRef{}
	locatorsBySubject := map[string][]string{}
	recoveryBySubject := map[string][]string{}
	duplicateGroupBySubject := map[string]string{}
	for _, record := range annotations {
		subjectRef := canonicalSubject(record.SubjectRef)
		if _, ok := currentSubjects[subjectRef]; !ok {
			continue
		}
		switch record.Kind {
		case sqlite.AnnotationTag:
			tagsBySubject[subjectRef] = append(tagsBySubject[subjectRef], record.Body)
		case sqlite.AnnotationNote:
			notesBySubject[subjectRef] = append(notesBySubject[subjectRef], record.Body)
			segment := SegmentRef{
				SourceType: "ANNOTATION", SourceID: record.ID,
				SegmentID: record.ID, MatchedText: record.Body,
				Kind: string(sqlite.AnnotationNote), Producer: "USER", Accepted: true, Language: "und",
			}
			segmentsBySubject[subjectRef] = append(segmentsBySubject[subjectRef], segment)
			semanticSegmentsBySubject[subjectRef] = append(semanticSegmentsBySubject[subjectRef], segment)
		}
	}
	for _, artifact := range artifacts {
		subjectRef := canonicalSubject(artifact.SubjectRef)
		if _, ok := currentSubjects[subjectRef]; !ok {
			continue
		}
		if _, ok := semanticArtifactEntryInRoots(artifact, subjectRef, byID, rootsByID); !ok {
			continue
		}
		if artifact.Stage == "EXTRACT" && strings.TrimSpace(artifact.Body) != "" {
			extractedBySubject[subjectRef] = append(extractedBySubject[subjectRef], artifact.Body)
			if artifact.State == sqlite.ArtifactAdmitted && utf8.ValidString(artifact.Body) {
				segment := SegmentRef{
					SourceType: "ARTIFACT", SourceID: artifact.ID,
					SegmentID: artifact.ID, MatchedText: artifact.Body,
					Kind: artifact.Stage, Producer: artifact.ProducerDigest,
					Accepted: true, Language: "und",
				}
				segmentsBySubject[subjectRef] = append(segmentsBySubject[subjectRef], segment)
				semanticSegmentsBySubject[subjectRef] = append(semanticSegmentsBySubject[subjectRef], segment)
			}
		}
	}
	for _, fact := range metadataFacts {
		subjectRef := canonicalSubject(fact.SubjectRef)
		metadataBySubject[subjectRef] = append(metadataBySubject[subjectRef],
			fact.Namespace, fact.Key, string(fact.Value), fact.ValueType)
	}
	for _, evidence := range detectionEvidence {
		entry, ok := entriesByObservation[evidence.ObservationID]
		if !ok {
			continue
		}
		subjectRef := canonicalSubject(entry.SubjectRef)
		if _, ok := currentSubjects[subjectRef]; !ok {
			continue
		}
		// Include only durable detector fields. In particular, the observation
		// ReadState is not a detection claim and must never feed this axis.
		detectionBySubject[subjectRef] = append(detectionBySubject[subjectRef],
			evidence.DetectorID, evidence.DetectorDigest, evidence.EvidenceKind,
			evidence.CandidateFormat, evidence.CandidateMIME,
			string(evidence.Evidence), evidence.EvidenceDigest)
	}
	for _, protection := range protections {
		subjectRef := canonicalSubject(protection.SubjectRef)
		protectionBySubject[subjectRef] = append(protectionBySubject[subjectRef],
			string(protection.Mode), string(protection.Outcome), protection.ExpectedContentID)
	}
	for _, attempt := range attempts {
		subjectRef := canonicalSubject(attempt.SubjectRef)
		processingBySubject[subjectRef] = append(processingBySubject[subjectRef],
			attempt.Stage, attempt.CapabilityID, attempt.Status, attempt.ReasonCode)
	}
	activeDescriptions := activeDescriptionLeaves(descriptions)
	for _, description := range descriptions {
		subjectRef := canonicalSubject(description.SubjectRef)
		if _, ok := currentSubjects[subjectRef]; !ok {
			continue
		}
		descriptionsBySubject[subjectRef] = append(descriptionsBySubject[subjectRef],
			description.Title, description.Language, description.Body, string(description.Kind), description.SourceRef)
		if description.ID == "" {
			continue
		}
		segments, listErr := idx.Store.ListSemanticSegments(ctx, workspaceID, description.ID)
		if listErr != nil {
			return generation, listErr
		}
		for _, segment := range segments {
			ref := SegmentRef{
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
			}
			segmentsBySubject[subjectRef] = append(segmentsBySubject[subjectRef], ref)
			// Durable segments from rejected/superseded revisions remain lexical
			// provenance. Only accepted leaves enter a new semantic generation.
			if _, ok := activeDescriptions[description.ID]; ok {
				semanticSegmentsBySubject[subjectRef] = append(semanticSegmentsBySubject[subjectRef], ref)
			}
		}
	}
	for _, binding := range bindings {
		subjectRef := canonicalSubject(binding.SubjectRef)
		locatorsBySubject[subjectRef] = append(locatorsBySubject[subjectRef],
			binding.ProviderKind, binding.StableIdentity)
		locators, listErr := idx.Store.ListExternalLocators(ctx, workspaceID, binding.ID)
		if listErr != nil {
			return generation, listErr
		}
		for _, locator := range locators {
			locatorsBySubject[subjectRef] = append(locatorsBySubject[subjectRef],
				locator.Kind, locator.DisplayLocator, locator.ExpectedContentID,
				locator.Availability, locator.ValidationStatus)
		}
	}
	for _, reference := range recoveryRefs {
		subjectRef := canonicalSubject(reference.SubjectRef)
		recoveryBySubject[subjectRef] = append(recoveryBySubject[subjectRef],
			string(reference.Kind), string(reference.Claim), reference.Status)
	}
	// Filenames are durable namespace facts, not ephemeral index labels. Feed
	// the display filename as a segment while retaining the snapshot-local
	// EntryID as both source and segment identity. The stable SubjectRef is
	// carried separately in the semantic vector row.
	for _, entry := range byID {
		if strings.TrimSpace(entry.DisplayName) == "" {
			continue
		}
		segment := SegmentRef{
			SourceType: "FILENAME", SourceID: entry.ID,
			SegmentID: entry.ID, MatchedText: entry.DisplayName,
			Kind: "FILENAME", Producer: "CATALOG", Accepted: true, Language: "und",
		}
		segmentsBySubject[entry.SubjectRef] = append(segmentsBySubject[entry.SubjectRef], segment)
		semanticSegmentsBySubject[entry.SubjectRef] = append(semanticSegmentsBySubject[entry.SubjectRef], segment)
	}
	contentCounts := make(map[string]int)
	for _, node := range nodes {
		if node.Entry.ContentID != "" {
			contentCounts[node.Entry.ContentID]++
		}
	}
	for _, node := range nodes {
		if node.Entry.ContentID != "" && contentCounts[node.Entry.ContentID] > 1 {
			duplicateGroupBySubject[node.Entry.SubjectRef] = node.Entry.ContentID
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
		metadata := append([]string{string(entry.Metadata)}, metadataBySubject[entry.SubjectRef]...)
		duplicate := ""
		if count := contentCounts[entry.ContentID]; entry.ContentID != "" && count > 1 {
			duplicate = fmt.Sprintf("duplicate duplicates same-content count %d %s", count, entry.ContentID)
		}
		segments := segmentsBySubject[entry.SubjectRef]
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
		doc := Document{
			SubjectID:       entry.SubjectRef,
			Path:            displayPath(byID, entry),
			Name:            entry.DisplayName,
			Suffix:          suffixOf(entry.DisplayName),
			EntryType:       string(entry.EntryType),
			ContentID:       entry.ContentID,
			Metadata:        strings.Join(metadata, " "),
			Duplicates:      duplicate,
			DuplicateGroup:  duplicateGroupBySubject[entry.SubjectRef],
			Protection:      strings.Join(append(append([]string{}, protectionBySubject[entry.SubjectRef]...), recoveryBySubject[entry.SubjectRef]...), " "),
			Locators:        strings.Join(locatorsBySubject[entry.SubjectRef], " "),
			Tags:            strings.Join(tagsBySubject[entry.SubjectRef], " "),
			Notes:           strings.Join(notesBySubject[entry.SubjectRef], " "),
			Descriptions:    strings.Join(descriptionsBySubject[entry.SubjectRef], " "),
			Extracted:       strings.Join(extractedBySubject[entry.SubjectRef], " "),
			Detection:       strings.Join(detectionBySubject[entry.SubjectRef], " "),
			Processing:      strings.Join(processingBySubject[entry.SubjectRef], " "),
			Representations: strings.Join(representationsByContent[entry.ContentID], " "),
			Language:        subjectLanguage(descriptionsBySubject[entry.SubjectRef]),
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
		SnapshotRef:           generationSnapshotRef,
		NamespaceRootID:       namespaceRootID,
		DBPath:                dbPath,
		Dimension:             DimensionLexical,
		ConfigDigest:          idx.ConfigDigest,
		ProviderProfileDigest: idx.profileDigest(DimensionLexical),
	}
	if err := idx.Store.InsertIndexGenerationWithRoots(ctx, &generation, rootIDs); err != nil {
		_ = idx.Engine.RemoveFile(dbPath)
		return sqlite.IndexGeneration{}, err
	}
	if idx.EnableFixtureDimensions {
		_, _ = idx.rebuildAcoustic(ctx, workspaceID, snapshotRef, namespaceRootID, byID, artifacts)
		_, _ = idx.rebuildTokens(ctx, workspaceID, snapshotRef, namespaceRootID, byID, artifacts, DimensionSemantic, "ENRICH", "embed.text.fixture.v1")
		_, _ = idx.rebuildTokens(ctx, workspaceID, snapshotRef, namespaceRootID, byID, artifacts, DimensionMultimodal, "ENRICH", "embed.clip.fixture.v1")
	}
	if idx.SemanticProvider != nil && idx.SemanticZvec != nil && idx.SemanticManifest != (EmbeddingGenerationManifest{}) {
		if _, semanticErr := idx.rebuildSemantic(ctx, workspaceID, snapshotRef, namespaceRootID, rootIDs, semanticSegmentsBySubject); semanticErr != nil {
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

// semanticGenerationMatchesLatestFeed binds the default semantic broker to
// the same durable lexical feed that is current after restart. Pinned
// historical generations remain addressable explicitly; only the implicit
// latest semantic lane is gated by this current-feed authority.
func (idx *Indexer) semanticGenerationMatchesLatestFeed(ctx context.Context, generation sqlite.IndexGeneration) error {
	if idx == nil || idx.Store == nil {
		return ErrUnavailable
	}
	lexical, err := idx.Store.LatestIndexGeneration(ctx, generation.WorkspaceID, DimensionLexical)
	if err != nil {
		return fmt.Errorf("latest lexical generation: %w", err)
	}
	if lexical.WorkspaceID != generation.WorkspaceID || lexical.SnapshotRef != generation.SnapshotRef || lexical.NamespaceRootID != generation.NamespaceRootID || !generationBindingMatches(idx, lexical, DimensionLexical) {
		return errors.New("semantic generation does not match latest lexical feed")
	}
	return nil
}

func semanticRequiresCurrentFeed(idx *Indexer) bool {
	return idx != nil && idx.SemanticManifest != (EmbeddingGenerationManifest{}) && idx.SemanticManifest.SemanticSpace == SemanticBundleBGESemanticSpace
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
	if semanticRequiresCurrentFeed(idx) {
		if err := idx.semanticGenerationMatchesLatestFeed(ctx, generation); err != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}
	if err := validateSemanticGenerationMapping(ctx, idx.Store, generation); err != nil {
		return fmt.Errorf("%w: semantic generation root mapping: %v", ErrUnavailable, err)
	}
	if err := idx.ensureSemanticGenerationVerified(ctx, generation); err != nil {
		idx.semanticIndexReady.Store(false)
		return err
	}
	idx.semanticUnavailable.Store(false)
	idx.semanticFailure.Store("")
	idx.semanticIndexReady.Store(true)
	return nil
}

func namespaceEntryByRef(byID map[string]sqlite.NamespaceEntry, ref string) (sqlite.NamespaceEntry, bool) {
	if entry, ok := byID[ref]; ok {
		return entry, true
	}
	for _, entry := range byID {
		if entry.SubjectRef == ref {
			return entry, true
		}
	}
	return sqlite.NamespaceEntry{}, false
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
			parentRef := entry.ParentID
			if parent, ok := byID[entry.ParentID]; ok {
				parentRef = parent.SubjectRef
			}
			edges = append(edges, GraphEdge{
				SubjectID: entry.SubjectRef,
				Relation:  RelContains,
				Value:     parentRef,
				Path:      path,
				Name:      entry.DisplayName,
				EntryType: string(entry.EntryType),
				ContentID: entry.ContentID,
			})
		}
		if entry.ContentID != "" && entry.EntryType == sqlite.EntryFile {
			edges = append(edges, GraphEdge{
				SubjectID: entry.SubjectRef,
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
		entry, ok := namespaceEntryByRef(byID, record.SubjectRef)
		if !ok {
			continue
		}
		edges = append(edges, GraphEdge{
			SubjectID: entry.SubjectRef,
			Relation:  RelTagged,
			Value:     record.Body,
			Path:      displayPath(byID, entry),
			Name:      entry.DisplayName,
			EntryType: string(entry.EntryType),
			ContentID: entry.ContentID,
		})
	}
	for _, artifact := range artifacts {
		entry, ok := namespaceEntryByRef(byID, artifact.SubjectRef)
		if !ok {
			continue
		}
		path := displayPath(byID, entry)
		switch artifact.CapabilityID {
		case "extract.audio.tags.v1":
			artist, album := parseAudioLabels(artifact.Body)
			if artist != "" {
				edges = append(edges, GraphEdge{
					SubjectID: entry.SubjectRef, Relation: RelArtist, Value: artist,
					Path: path, Name: entry.DisplayName, EntryType: string(entry.EntryType), ContentID: entry.ContentID,
				})
			}
			if album != "" {
				edges = append(edges, GraphEdge{
					SubjectID: entry.SubjectRef, Relation: RelAlbum, Value: album,
					Path: path, Name: entry.DisplayName, EntryType: string(entry.EntryType), ContentID: entry.ContentID,
				})
			}
		case "extract.book.meta.v1":
			if author := parseAuthorLabel(artifact.Body); author != "" {
				edges = append(edges, GraphEdge{
					SubjectID: entry.SubjectRef, Relation: RelAuthor, Value: author,
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
		entry, ok := namespaceEntryByRef(byID, artifact.SubjectRef)
		if !ok {
			continue
		}
		docs = append(docs, TokenDocument{
			SubjectID: entry.SubjectRef,
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
		entry, ok := namespaceEntryByRef(byID, artifact.SubjectRef)
		if !ok {
			continue
		}
		docs = append(docs, AcousticDocument{
			SubjectID:   entry.SubjectRef,
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

func (idx *Indexer) rebuildSemantic(ctx context.Context, workspaceID, snapshotRef, namespaceRootID string, rootIDs []string, segmentsBySubject map[string][]SegmentRef) (sqlite.IndexGeneration, error) {
	var generation sqlite.IndexGeneration
	idx.semanticIndexReady.Store(false)
	if idx.SemanticProvider == nil || idx.SemanticZvec == nil {
		return generation, ErrUnavailable
	}
	if _, ok := idx.SemanticZvec.(ZvecGenerationMembershipVerifier); !ok {
		return generation, fmt.Errorf("%w: semantic backend does not provide generation membership verification", ErrUnavailable)
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
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].SubjectID != inputs[j].SubjectID {
			return inputs[i].SubjectID < inputs[j].SubjectID
		}
		return inputs[i].SegmentID < inputs[j].SegmentID
	})
	if len(inputs) == 0 {
		return generation, ErrUnavailable
	}
	generationID, err := sqlite.NewStableID(sqlite.IDPrefixIndexGeneration)
	if err != nil {
		return generation, err
	}
	results, err := embedSemanticDocumentBatches(ctx, idx.SemanticProvider, manifest, generationID, inputs)
	if err != nil {
		return generation, fmt.Errorf("%w: provider: %v", ErrUnavailable, err)
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
	if err := validateSemanticBuildReceipt(receipt, spec, len(zvecSegments)); err != nil {
		idx.semanticIndexReady.Store(false)
		return generation, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	candidates := make([]ZvecCoverageIdentity, 0, len(zvecSegments))
	for _, segment := range zvecSegments {
		candidates = append(candidates, ZvecCoverageIdentity{SubjectID: segment.SubjectID, SegmentID: segment.SegmentID})
	}
	actualCoverage, err := idx.verifySemanticGenerationCoverage(ctx, spec, candidates)
	if err != nil {
		idx.semanticIndexReady.Store(false)
		return generation, err
	}
	if err := openCloseSemanticGeneration(ctx, idx.SemanticZvec, spec); err != nil {
		idx.semanticIndexReady.Store(false)
		return generation, fmt.Errorf("%w: built semantic generation lifecycle: %v", ErrUnavailable, err)
	}
	semanticSnapshotRef := snapshotRef
	if primaryRoot, rootErr := idx.Store.GetNamespaceRoot(ctx, workspaceID, namespaceRootID); rootErr == nil && strings.TrimSpace(primaryRoot.SnapshotRef) != "" {
		// The semantic generation's primary root binding is its immutable
		// snapshot authority. Rebuild callers may pass a publication trigger
		// label that is not the root snapshot itself.
		semanticSnapshotRef = primaryRoot.SnapshotRef
	}
	generation = sqlite.IndexGeneration{
		ID: generationID, WorkspaceID: workspaceID, SnapshotRef: semanticSnapshotRef, NamespaceRootID: namespaceRootID,
		DBPath: receipt.Path, Dimension: DimensionSemantic, ConfigDigest: manifest.ConfigDigest,
		ProviderProfileDigest: manifest.CanonicalDigest(), SemanticSpace: manifest.SemanticSpace,
	}
	if strings.TrimSpace(generation.DBPath) == "" {
		generation.DBPath = spec.Path
	}
	if err := writeSemanticGenerationReceipt(generation, idx.SemanticLibraryDigest, actualCoverage); err != nil {
		idx.semanticIndexReady.Store(false)
		return generation, fmt.Errorf("%w: write generation receipt: %v", ErrUnavailable, err)
	}
	if err := idx.Store.InsertIndexGenerationWithRoots(ctx, &generation, rootIDs); err != nil {
		idx.semanticIndexReady.Store(false)
		_ = os.Remove(semanticGenerationReceiptPath(generation.DBPath))
		return sqlite.IndexGeneration{}, err
	}
	idx.cacheSemanticGeneration(generation, actualCoverage)
	idx.semanticIndexReady.Store(true)
	return generation, nil
}

func validateSemanticBuildReceipt(receipt ZvecGenerationReceipt, spec ZvecGenerationSpec, segmentCount int) error {
	if receipt.Path != spec.Path || receipt.LibraryDigest != spec.LibraryDigest || receipt.ProfileDigest != spec.ProfileDigest ||
		receipt.Dimension != spec.Manifest.Dimension || receipt.SegmentCount != segmentCount {
		return errors.New("semantic build receipt does not match requested generation")
	}
	return nil
}

// embedSemanticDocumentBatches keeps each provider request within the worker
// contract's single-language boundary. A rebuilt generation can legitimately
// contain filename facts (und) and durable descriptions (for example, zh) at
// the same time; combining those inputs would make the real BGE worker reject
// the complete rebuild. Inputs are also bounded to the provider's request
// limit, then results are restored to the deterministic input order.
func embedSemanticDocumentBatches(ctx context.Context, provider SemanticEmbeddingProvider, manifest EmbeddingGenerationManifest, generationID string, inputs []SemanticTextInput) ([]SemanticVector, error) {
	if provider == nil {
		return nil, ErrSemanticProviderUnavailable
	}
	if len(inputs) == 0 {
		return nil, ErrSemanticProviderUnavailable
	}
	const maxBatchSize = 256
	byLanguage := make(map[string][]SemanticTextInput)
	order := make(map[string]int, len(inputs))
	for position, input := range inputs {
		language := strings.TrimSpace(input.Language)
		if language == "" {
			language = "und"
		}
		input.Language = language
		byLanguage[language] = append(byLanguage[language], input)
		order[input.SegmentID] = position
	}
	languages := make([]string, 0, len(byLanguage))
	for language := range byLanguage {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	results := make([]SemanticVector, 0, len(inputs))
	for _, language := range languages {
		group := byLanguage[language]
		for start := 0; start < len(group); start += maxBatchSize {
			end := start + maxBatchSize
			if end > len(group) {
				end = len(group)
			}
			batchInputs := group[start:end]
			request := SemanticEmbeddingRequest{Purpose: SemanticEmbeddingDocument, GenerationID: generationID, Manifest: manifest, Inputs: batchInputs}
			if err := validateSemanticEmbeddingRequest(request); err != nil {
				return nil, err
			}
			batchResults, err := provider.Embed(ctx, request)
			if err != nil {
				return nil, err
			}
			if err := validateSemanticEmbeddingResults(request, batchResults); err != nil {
				return nil, err
			}
			results = append(results, batchResults...)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return order[results[i].SegmentID] < order[results[j].SegmentID]
	})
	return results, nil
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

func (idx *Indexer) querySemantic(ctx context.Context, generation sqlite.IndexGeneration, text string) (result []Hit, err error) {
	// Querying does not revoke other verified generations. A failed query only
	// drops this generation's receipt through the deferred failure path.
	querySucceeded := false
	var opened ZvecGeneration
	defer func() {
		if opened != nil {
			if closeErr := opened.Close(); closeErr != nil {
				result = nil
				err = fmt.Errorf("%w: close generation: %v", ErrUnavailable, closeErr)
				querySucceeded = false
			}
		}
		if !querySucceeded {
			idx.revokeSemanticGeneration(generation.ID)
		}
	}()
	if idx.SemanticProvider == nil || idx.SemanticZvec == nil {
		return nil, ErrUnavailable
	}
	manifest := idx.SemanticManifest
	if err := manifest.Validate(); err != nil {
		return nil, ErrUnavailable
	}
	spec := ZvecGenerationSpec{
		Path: generation.DBPath, LibraryPath: idx.SemanticLibraryPath, LibraryDigest: idx.SemanticLibraryDigest,
		ProfileDigest: manifest.CanonicalDigest(), Manifest: manifest,
	}
	if err := idx.ensureSemanticGenerationVerified(ctx, generation); err != nil {
		idx.semanticIndexReady.Store(false)
		return nil, err
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
	opened, err = idx.SemanticZvec.Open(ctx, spec)
	if err != nil {
		idx.semanticIndexReady.Store(false)
		return nil, fmt.Errorf("%w: open generation: %v", ErrUnavailable, err)
	}
	if opened == nil {
		return nil, fmt.Errorf("%w: semantic backend returned a nil generation", ErrUnavailable)
	}
	membershipVerifier, ok := idx.SemanticZvec.(ZvecGenerationMembershipVerifier)
	if !ok {
		return nil, fmt.Errorf("%w: semantic backend does not provide generation membership verification", ErrUnavailable)
	}
	hits, err := opened.Query(ctx, results[0].Vector, 100)
	if err != nil {
		idx.semanticIndexReady.Store(false)
		return nil, err
	}
	entries, err := semanticNamespaceEntries(ctx, idx.Store, generation)
	if err != nil {
		idx.semanticIndexReady.Store(false)
		return nil, fmt.Errorf("%w: semantic namespace provenance: %v", ErrUnavailable, err)
	}
	candidates := make([]ZvecCoverageIdentity, 0, len(hits))
	for _, hit := range hits {
		if strings.TrimSpace(hit.SubjectID) == "" || strings.TrimSpace(hit.SegmentID) == "" {
			return nil, fmt.Errorf("%w: semantic hit has incomplete identity", ErrUnavailable)
		}
		candidates = append(candidates, ZvecCoverageIdentity{SubjectID: hit.SubjectID, SegmentID: hit.SegmentID})
	}
	if err := membershipVerifier.VerifyMembership(ctx, spec, candidates); err != nil {
		return nil, fmt.Errorf("%w: semantic generation membership: %v", ErrUnavailable, err)
	}
	result = make([]Hit, 0, len(hits))
	bySubject := make(map[string]int, len(hits))
	for _, hit := range hits {
		if strings.HasPrefix(hit.SegmentID, sqlite.IDPrefixAnnotation+"_") {
			annotationID := hit.SegmentID
			annotation, annotationErr := idx.Store.GetAnnotation(ctx, generation.WorkspaceID, annotationID)
			if annotationErr != nil {
				return nil, fmt.Errorf("%w: semantic annotation provenance: %v", ErrUnavailable, annotationErr)
			}
			if semanticCanonicalSubject(entries, annotation.SubjectRef) != hit.SubjectID || annotation.Kind != sqlite.AnnotationNote || annotation.Tombstoned {
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
		if strings.HasPrefix(hit.SegmentID, sqlite.IDPrefixNamespaceEntry+"_") {
			entry, entryOK, entryErr := semanticFilenameEntry(ctx, idx.Store, generation, entries, hit.SegmentID, hit.SubjectID)
			if entryErr != nil {
				return nil, fmt.Errorf("%w: semantic filename provenance: %v", ErrUnavailable, entryErr)
			}
			if !entryOK || strings.TrimSpace(entry.DisplayName) == "" {
				return nil, fmt.Errorf("%w: semantic filename provenance %q subject binding mismatch", ErrUnavailable, hit.SegmentID)
			}
			projectedSegment := SegmentRef{
				SourceType: "FILENAME", SourceID: entry.ID, SegmentID: entry.ID,
				MatchedText: entry.DisplayName, Kind: "FILENAME", Producer: "CATALOG", Accepted: true, Language: "und",
			}
			if at, ok := bySubject[hit.SubjectID]; ok {
				result[at].Segments = appendUniqueSegment(result[at].Segments, projectedSegment)
				continue
			}
			bySubject[hit.SubjectID] = len(result)
			result = append(result, Hit{SubjectID: hit.SubjectID, Segments: []SegmentRef{projectedSegment}})
			continue
		}
		if strings.HasPrefix(hit.SegmentID, sqlite.IDPrefixArtifact+"_") {
			artifact, artifactErr := idx.Store.GetProcessorArtifact(ctx, generation.WorkspaceID, hit.SegmentID)
			if artifactErr != nil {
				return nil, fmt.Errorf("%w: semantic artifact provenance: %v", ErrUnavailable, artifactErr)
			}
			artifactSubject := semanticCanonicalSubjectForStore(ctx, idx.Store, entries, generation.WorkspaceID, artifact.SubjectRef)
			entry, entryOK, entryErr := semanticArtifactEntry(ctx, idx.Store, artifact, generation, entries, artifactSubject, hit.SubjectID)
			if entryErr != nil {
				return nil, fmt.Errorf("%w: semantic artifact provenance: %v", ErrUnavailable, entryErr)
			}
			if artifact.State != sqlite.ArtifactAdmitted || artifact.Stage != "EXTRACT" || strings.TrimSpace(artifact.Body) == "" || !utf8.ValidString(artifact.Body) || artifact.WorkspaceID != generation.WorkspaceID || artifactSubject != hit.SubjectID || !entryOK || semanticEntrySubject(entry) != hit.SubjectID {
				return nil, fmt.Errorf("%w: semantic artifact %q state, snapshot, or subject binding mismatch", ErrUnavailable, artifact.ID)
			}
			projectedSegment := SegmentRef{
				SourceType: "ARTIFACT", SourceID: artifact.ID, SegmentID: artifact.ID,
				MatchedText: artifact.Body, Kind: artifact.Stage, Producer: artifact.ProducerDigest,
				Accepted: true, Language: "und",
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
		if semanticCanonicalSubjectForStore(ctx, idx.Store, entries, generation.WorkspaceID, segment.SubjectRef) != hit.SubjectID {
			return nil, fmt.Errorf("%w: semantic segment %q subject binding mismatch", ErrUnavailable, hit.SegmentID)
		}
		document, documentErr := idx.Store.GetDescriptionDocument(ctx, generation.WorkspaceID, segment.DocumentID)
		if documentErr != nil {
			return nil, fmt.Errorf("%w: semantic description provenance: %v", ErrUnavailable, documentErr)
		}
		if semanticCanonicalSubjectForStore(ctx, idx.Store, entries, generation.WorkspaceID, document.SubjectRef) != hit.SubjectID {
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
	querySucceeded = true
	return result, nil
}

func semanticEntrySubject(entry sqlite.NamespaceEntry) string {
	if strings.TrimSpace(entry.SubjectRef) != "" {
		return entry.SubjectRef
	}
	return entry.ID
}

func semanticCanonicalSubject(entries map[string]sqlite.NamespaceEntry, ref string) string {
	if entry, ok := entries[ref]; ok {
		return semanticEntrySubject(entry)
	}
	return ref
}

// semanticCanonicalSubjectForStore also resolves historical snapshot-local
// entry IDs. The active/latest namespace projection is intentionally only a
// fast path; it cannot be the authority for a pinned generation.
func semanticCanonicalSubjectForStore(ctx context.Context, store *sqlite.Store, entries map[string]sqlite.NamespaceEntry, workspaceID, ref string) string {
	if subject := semanticCanonicalSubject(entries, ref); subject != ref {
		return subject
	}
	if store != nil && strings.TrimSpace(ref) != "" {
		entry, err := store.GetNamespaceEntry(ctx, workspaceID, ref)
		if err == nil && entry.WorkspaceID == workspaceID {
			return semanticEntrySubject(entry)
		}
	}
	return ref
}

func namespaceRootIDs(entries map[string]sqlite.NamespaceEntry) map[string]struct{} {
	roots := make(map[string]struct{})
	for _, entry := range entries {
		if strings.TrimSpace(entry.RootID) != "" {
			roots[entry.RootID] = struct{}{}
		}
	}
	return roots
}

// semanticArtifactEntryInRoots checks a derived artifact against the actual
// snapshot/root represented by the generation feed. It is deliberately based
// on the artifact snapshot, rather than a stable-subject-to-latest-root map;
// one subject may have observations in more than one source or generation.
func semanticArtifactEntryInRoots(artifact sqlite.ProcessorArtifact, subjectRef string, entries map[string]sqlite.NamespaceEntry, roots map[string]sqlite.NamespaceRoot) (sqlite.NamespaceEntry, bool) {
	if strings.TrimSpace(artifact.WorkspaceID) == "" || strings.TrimSpace(artifact.SnapshotRef) == "" {
		return sqlite.NamespaceEntry{}, false
	}
	for _, entry := range entries {
		if entry.WorkspaceID != artifact.WorkspaceID || semanticEntrySubject(entry) != subjectRef {
			continue
		}
		root, ok := roots[entry.RootID]
		if !ok || root.WorkspaceID != artifact.WorkspaceID || root.ID != entry.RootID || root.SnapshotRef != artifact.SnapshotRef {
			continue
		}
		return entry, true
	}
	return sqlite.NamespaceEntry{}, false
}

// semanticFilenameEntry resolves the immutable EntryID embedded in a vector
// row. It does not consult ListLatestNamespaceEntries, so old generations
// retain their original filename and stable subject after publication.
func semanticFilenameEntry(ctx context.Context, store *sqlite.Store, generation sqlite.IndexGeneration, entries map[string]sqlite.NamespaceEntry, entryID, subjectID string) (sqlite.NamespaceEntry, bool, error) {
	if store == nil {
		return sqlite.NamespaceEntry{}, false, errors.New("catalog is required")
	}
	entry, ok := entries[entryID]
	if !ok {
		return sqlite.NamespaceEntry{}, false, nil
	}
	if entry.WorkspaceID != generation.WorkspaceID || semanticEntrySubject(entry) != subjectID || strings.TrimSpace(entry.RootID) == "" || strings.TrimSpace(entry.DisplayName) == "" {
		return entry, false, nil
	}
	root, err := store.GetNamespaceRoot(ctx, generation.WorkspaceID, entry.RootID)
	if err != nil {
		return sqlite.NamespaceEntry{}, false, err
	}
	if root.WorkspaceID != generation.WorkspaceID || root.ID != entry.RootID || strings.TrimSpace(root.SnapshotRef) == "" {
		return entry, false, nil
	}
	return entry, true, nil
}

// semanticArtifactEntry resolves the root named by an artifact's immutable
// SnapshotRef and then validates the subject within that root. In particular,
// artifactSnapshot == generation.SnapshotRef is not a reason to skip this
// check: snapshot labels alone do not establish namespace membership.
func semanticArtifactEntry(ctx context.Context, store *sqlite.Store, artifact sqlite.ProcessorArtifact, generation sqlite.IndexGeneration, entries map[string]sqlite.NamespaceEntry, artifactSubject, hitSubject string) (sqlite.NamespaceEntry, bool, error) {
	if store == nil {
		return sqlite.NamespaceEntry{}, false, errors.New("catalog is required")
	}
	if artifact.WorkspaceID != generation.WorkspaceID || strings.TrimSpace(artifact.SnapshotRef) == "" || artifactSubject != hitSubject {
		return sqlite.NamespaceEntry{}, false, nil
	}
	root, err := store.GetNamespaceRootBySnapshotRef(ctx, artifact.SnapshotRef)
	if err != nil {
		return sqlite.NamespaceEntry{}, false, err
	}
	if root.WorkspaceID != generation.WorkspaceID || strings.TrimSpace(root.ID) == "" {
		return sqlite.NamespaceEntry{}, false, nil
	}
	if root.SnapshotRef != artifact.SnapshotRef {
		return sqlite.NamespaceEntry{}, false, nil
	}
	for _, entry := range entries {
		if entry.WorkspaceID == generation.WorkspaceID && entry.RootID == root.ID && semanticEntrySubject(entry) == artifactSubject && semanticEntrySubject(entry) == hitSubject {
			return entry, true, nil
		}
	}
	return sqlite.NamespaceEntry{}, false, nil
}

// activeDescriptionLeaves returns only accepted revisions that have not been
// superseded by a successor. Description history remains durable; this is
// solely the rebuild-time semantic feed projection.
func activeDescriptionLeaves(descriptions []sqlite.DescriptionDocument) map[string]struct{} {
	superseded := make(map[string]struct{}, len(descriptions))
	for _, description := range descriptions {
		if predecessor := strings.TrimSpace(description.PredecessorID); predecessor != "" {
			superseded[predecessor] = struct{}{}
		}
	}
	active := make(map[string]struct{}, len(descriptions))
	for _, description := range descriptions {
		if description.Accepted {
			if _, replaced := superseded[description.ID]; !replaced {
				active[description.ID] = struct{}{}
			}
		}
	}
	return active
}

// semanticNamespaceEntries returns only entries from the roots atomically
// recorded for this generation. A missing mapping is a damaged/legacy
// generation and fails closed; the current latest projection is never a
// fallback for pinned provenance.
func semanticNamespaceEntries(ctx context.Context, store *sqlite.Store, generation sqlite.IndexGeneration) (map[string]sqlite.NamespaceEntry, error) {
	if store == nil {
		return nil, errors.New("catalog is required")
	}
	roots, err := store.ListIndexGenerationRoots(ctx, generation.WorkspaceID, generation.ID)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, errors.New("index generation root mapping is empty")
	}
	byID := make(map[string]sqlite.NamespaceEntry)
	seenRoots := make(map[string]struct{}, len(roots))
	containsPrimary := false
	for _, root := range roots {
		if root.WorkspaceID != generation.WorkspaceID || strings.TrimSpace(root.ID) == "" || strings.TrimSpace(root.SnapshotRef) == "" {
			return nil, fmt.Errorf("index generation root %q is outside workspace scope", root.ID)
		}
		if _, duplicate := seenRoots[root.ID]; duplicate {
			return nil, fmt.Errorf("duplicate index generation root %q", root.ID)
		}
		seenRoots[root.ID] = struct{}{}
		if root.ID == generation.NamespaceRootID {
			containsPrimary = true
		}
		nodes, listErr := store.ListNamespaceSubtree(ctx, generation.WorkspaceID, root.ID, "")
		if listErr != nil {
			return nil, listErr
		}
		for _, node := range nodes {
			entry := node.Entry
			if entry.WorkspaceID != generation.WorkspaceID || entry.RootID != root.ID {
				return nil, fmt.Errorf("namespace entry %q is outside generation root scope", entry.ID)
			}
			if _, duplicate := byID[entry.ID]; duplicate {
				return nil, fmt.Errorf("duplicate namespace entry %q in generation roots", entry.ID)
			}
			if strings.TrimSpace(entry.SubjectRef) == "" {
				entry.SubjectRef = entry.ID
			}
			byID[entry.ID] = entry
		}
	}
	if !containsPrimary {
		return nil, fmt.Errorf("index generation root mapping omits primary root %q", generation.NamespaceRootID)
	}
	return byID, nil
}

func semanticEntryForSubject(ctx context.Context, store *sqlite.Store, entries map[string]sqlite.NamespaceEntry, subjectRef, artifactSnapshot string, generation sqlite.IndexGeneration) (sqlite.NamespaceEntry, bool) {
	for _, entry := range entries {
		if semanticEntrySubject(entry) != subjectRef {
			continue
		}
		root, err := store.GetNamespaceRoot(ctx, generation.WorkspaceID, entry.RootID)
		if err == nil && artifactSnapshot != "" && root.SnapshotRef == artifactSnapshot {
			return entry, true
		}
	}
	return sqlite.NamespaceEntry{}, false
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
	if dimension == DimensionSemantic && semanticRequiresCurrentFeed(idx) {
		if err := validateSemanticGenerationMapping(ctx, idx.Store, generation); err != nil {
			return generation, nil, fmt.Errorf("%w: semantic generation root mapping: %v", ErrUnavailable, err)
		}
		if strings.TrimSpace(req.GenerationID) == "" {
			if err := idx.semanticGenerationMatchesLatestFeed(ctx, generation); err != nil {
				return generation, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
			}
		}
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
