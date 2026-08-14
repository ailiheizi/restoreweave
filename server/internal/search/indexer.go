package search

import (
	"context"
	"errors"
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
	tagsBySubject := map[string][]string{}
	notesBySubject := map[string][]string{}
	extractedBySubject := map[string][]string{}
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
	byID := make(map[string]sqlite.NamespaceEntry, len(nodes))
	for _, node := range nodes {
		byID[node.Entry.ID] = node.Entry
	}
	docs := make([]Document, 0, len(nodes))
	for _, node := range nodes {
		entry := node.Entry
		docs = append(docs, Document{
			SubjectID: entry.ID,
			Path:      displayPath(byID, entry),
			Name:      entry.DisplayName,
			Suffix:    suffixOf(entry.DisplayName),
			EntryType: string(entry.EntryType),
			ContentID: entry.ContentID,
			Tags:      strings.Join(tagsBySubject[entry.ID], " "),
			Notes:     strings.Join(notesBySubject[entry.ID], " "),
			Extracted: strings.Join(extractedBySubject[entry.ID], " "),
		})
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
func (idx *Indexer) Query(ctx context.Context, workspaceID, generationID, text string) (sqlite.IndexGeneration, []Hit, error) {
	var generation sqlite.IndexGeneration
	if idx == nil || idx.Store == nil || idx.Engine == nil {
		return generation, nil, errors.New("search indexer requires a catalog and engine")
	}
	var err error
	if strings.TrimSpace(generationID) == "" {
		generation, err = idx.Store.LatestIndexGeneration(ctx, workspaceID)
	} else {
		generation, err = idx.Store.GetIndexGeneration(ctx, generationID)
	}
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return generation, nil, ErrUnavailable
		}
		return generation, nil, err
	}
	hits, err := idx.Engine.Query(ctx, generation.DBPath, text)
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
