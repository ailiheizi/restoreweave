package access

import (
	"context"

	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
)

// ReadAll reads the entire exact file for one authorized entry.
func ReadAll(ctx context.Context, files readsvc.FileAccess, view readsvc.SnapshotView, entryID string) ([]byte, error) {
	session, err := files.OpenSession(ctx, readsvc.OpenFileRequest{
		Access:  LocalAccess("read-all"),
		View:    view,
		EntryID: entryID,
	})
	if err != nil {
		return nil, err
	}
	defer session.Close()
	handle, err := session.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	size := handle.Size()
	if size == 0 {
		return []byte{}, nil
	}
	buf := make([]byte, size)
	offset := uint64(0)
	for offset < size {
		chunk := buf[offset:]
		result, err := handle.ReadAt(ctx, chunk, offset)
		if err != nil {
			return nil, err
		}
		if result.BytesRead == 0 {
			break
		}
		offset += result.BytesRead
	}
	return buf[:offset], nil
}

// CollectFiles walks the snapshot view without following symlinks and returns
// display-path → entry maps for regular files. This is the gateway-facing
// traversal used by FUSE and tests; it never talks to the repository engine
// private layout.
func CollectFiles(ctx context.Context, view readsvc.SnapshotView) (map[string]readsvc.NamespaceEntry, error) {
	root, err := view.Root(ctx)
	if err != nil {
		return nil, err
	}
	files := map[string]readsvc.NamespaceEntry{}
	if err := walk(ctx, view, root, "", files); err != nil {
		return nil, err
	}
	return files, nil
}

func walk(ctx context.Context, view readsvc.SnapshotView, dir readsvc.NamespaceEntry, prefix string, files map[string]readsvc.NamespaceEntry) error {
	var cursor string
	for {
		page, err := view.ListChildren(ctx, dir.ID, readsvc.PageRequest{Cursor: cursor, Limit: 256})
		if err != nil {
			return err
		}
		for _, entry := range page.Entries {
			name := entry.DisplayName
			if name == "" {
				name = string(entry.RawName)
			}
			path := name
			if prefix != "" {
				path = prefix + "/" + name
			}
			switch entry.Kind {
			case readsvc.EntryDirectory:
				if err := walk(ctx, view, entry, path, files); err != nil {
					return err
				}
			case readsvc.EntryRegularFile:
				files[path] = entry
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		cursor = page.NextCursor
	}
}
