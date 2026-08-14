package exact

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// DiffResult compares two portable snapshots by relative path.
type DiffResult struct {
	FromSnapshotRef string
	ToSnapshotRef   string
	Changes         []command.SnapshotDiffItem
}

// DiffSnapshots compares two repository manifests. It does not read SQLite.
func (s *Service) DiffSnapshots(_ context.Context, fromRef, toRef string) (DiffResult, error) {
	var result DiffResult
	if err := s.require(); err != nil {
		return result, err
	}
	if strings.TrimSpace(fromRef) == "" || strings.TrimSpace(toRef) == "" {
		return result, fmt.Errorf("from_snapshot_ref and to_snapshot_ref are required")
	}
	from, err := readManifest(s.Repo.Root(), fromRef)
	if err != nil {
		return result, err
	}
	to, err := readManifest(s.Repo.Root(), toRef)
	if err != nil {
		return result, err
	}
	return DiffManifests(from, to), nil
}

// DiffManifests classifies path-level changes between two snapshots.
func DiffManifests(from, to Manifest) DiffResult {
	fromByPath := indexManifest(from)
	toByPath := indexManifest(to)
	var changes []command.SnapshotDiffItem
	var removed []ManifestEntry
	var added []ManifestEntry
	for path, entry := range fromByPath {
		if _, ok := toByPath[path]; !ok {
			removed = append(removed, entry)
		}
	}
	for path, entry := range toByPath {
		if _, ok := fromByPath[path]; !ok {
			added = append(added, entry)
		}
	}
	movedFrom, movedTo := uniqueContentMoves(removed, added)
	for _, entry := range removed {
		if _, ok := movedFrom[entryKey(entry)]; ok {
			continue
		}
		changes = append(changes, command.SnapshotDiffItem{
			Kind:      command.DiffRemoved,
			Path:      entry.RelativePath,
			EntryType: entry.EntryType,
			ContentID: entry.ContentID,
		})
	}
	for _, entry := range added {
		if fromPath, ok := movedTo[entryKey(entry)]; ok {
			changes = append(changes, command.SnapshotDiffItem{
				Kind:      command.DiffMoved,
				FromPath:  fromPath,
				ToPath:    entry.RelativePath,
				Path:      entry.RelativePath,
				EntryType: entry.EntryType,
				ContentID: entry.ContentID,
			})
			continue
		}
		changes = append(changes, command.SnapshotDiffItem{
			Kind:      command.DiffAdded,
			Path:      entry.RelativePath,
			EntryType: entry.EntryType,
			ContentID: entry.ContentID,
		})
	}
	for path, before := range fromByPath {
		after, ok := toByPath[path]
		if !ok {
			continue
		}
		if before.EntryType != after.EntryType {
			changes = append(changes, command.SnapshotDiffItem{
				Kind:      command.DiffTypeChanged,
				Path:      path,
				FromType:  before.EntryType,
				ToType:    after.EntryType,
				EntryType: after.EntryType,
			})
			continue
		}
		if before.ContentID != after.ContentID {
			changes = append(changes, command.SnapshotDiffItem{
				Kind:          command.DiffContentChanged,
				Path:          path,
				EntryType:     after.EntryType,
				FromContentID: before.ContentID,
				ToContentID:   after.ContentID,
				ContentID:     after.ContentID,
			})
			continue
		}
		if metadataChanged(before, after) {
			changes = append(changes, command.SnapshotDiffItem{
				Kind:      command.DiffMetadataChanged,
				Path:      path,
				EntryType: after.EntryType,
				ContentID: after.ContentID,
			})
		}
	}
	sort.SliceStable(changes, func(i, j int) bool {
		left := changeSortKey(changes[i])
		right := changeSortKey(changes[j])
		if left == right {
			return changes[i].Kind < changes[j].Kind
		}
		return left < right
	})
	return DiffResult{
		FromSnapshotRef: from.SnapshotRef,
		ToSnapshotRef:   to.SnapshotRef,
		Changes:         changes,
	}
}

func indexManifest(manifest Manifest) map[string]ManifestEntry {
	indexed := make(map[string]ManifestEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		path := strings.Trim(entry.RelativePath, "/")
		if path == "" || path == "." {
			continue
		}
		indexed[path] = entry
	}
	return indexed
}

func uniqueContentMoves(removed, added []ManifestEntry) (map[string]struct{}, map[string]string) {
	fromByContent := map[string][]ManifestEntry{}
	toByContent := map[string][]ManifestEntry{}
	for _, entry := range removed {
		if sqlite.NamespaceEntryType(entry.EntryType) != sqlite.EntryFile || entry.ContentID == "" {
			continue
		}
		fromByContent[entry.ContentID] = append(fromByContent[entry.ContentID], entry)
	}
	for _, entry := range added {
		if sqlite.NamespaceEntryType(entry.EntryType) != sqlite.EntryFile || entry.ContentID == "" {
			continue
		}
		toByContent[entry.ContentID] = append(toByContent[entry.ContentID], entry)
	}
	movedFrom := map[string]struct{}{}
	movedTo := map[string]string{}
	for contentID, fromEntries := range fromByContent {
		toEntries := toByContent[contentID]
		if len(fromEntries) != 1 || len(toEntries) != 1 {
			continue
		}
		movedFrom[entryKey(fromEntries[0])] = struct{}{}
		movedTo[entryKey(toEntries[0])] = fromEntries[0].RelativePath
	}
	return movedFrom, movedTo
}

func entryKey(entry ManifestEntry) string {
	return entry.EntryType + "\x00" + entry.RelativePath + "\x00" + entry.ContentID
}

func metadataChanged(before, after ManifestEntry) bool {
	if before.Mode != after.Mode {
		return true
	}
	return logicalSizeOf(before) != logicalSizeOf(after)
}

func logicalSizeOf(entry ManifestEntry) int64 {
	if entry.LogicalSize == nil {
		return -1
	}
	return *entry.LogicalSize
}

func changeSortKey(item command.SnapshotDiffItem) string {
	if item.Path != "" {
		return item.Path
	}
	if item.ToPath != "" {
		return item.ToPath
	}
	return item.FromPath
}
