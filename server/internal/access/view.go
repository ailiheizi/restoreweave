package access

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const defaultPageLimit uint32 = 256
const maxPageLimit uint32 = 4096

type catalogView struct {
	service     *Service
	pin         readsvc.ViewPin
	root        sqlite.NamespaceRoot
	publication sqlite.Publication
	closed      bool
}

func (v *catalogView) Pin() readsvc.ViewPin { return v.pin }

func (v *catalogView) Close() error {
	v.closed = true
	return nil
}

func (v *catalogView) ensure() error {
	if v == nil || v.closed {
		return ErrViewClosed
	}
	if !v.service.now().Before(v.pin.Authorization.ExpiresAt) {
		return readsvc.ErrSessionExpired
	}
	return nil
}

func (v *catalogView) Root(ctx context.Context) (readsvc.NamespaceEntry, error) {
	if err := v.ensure(); err != nil {
		return readsvc.NamespaceEntry{}, err
	}
	return v.syntheticRoot(), nil
}

func (v *catalogView) Stat(ctx context.Context, entryID string) (readsvc.NamespaceEntry, error) {
	if err := v.ensure(); err != nil {
		return readsvc.NamespaceEntry{}, err
	}
	return v.lookupEntry(ctx, entryID)
}

func (v *catalogView) Lookup(ctx context.Context, parentID string, component readsvc.PathComponent) (readsvc.NamespaceEntry, error) {
	if err := v.ensure(); err != nil {
		return readsvc.NamespaceEntry{}, err
	}
	if err := component.Validate(); err != nil {
		return readsvc.NamespaceEntry{}, err
	}
	if parentID == "" {
		parentID = v.pin.Snapshot.NamespaceRootID
	}
	entry, err := v.service.Store.LookupNamespaceChild(
		ctx,
		v.pin.Snapshot.WorkspaceID,
		v.pin.Snapshot.NamespaceRootID,
		parentID,
		component.Raw,
		component.Normalized,
	)
	if err != nil {
		return readsvc.NamespaceEntry{}, err
	}
	return projectEntry(entry), nil
}

func (v *catalogView) ListChildren(ctx context.Context, parentID string, page readsvc.PageRequest) (readsvc.EntryPage, error) {
	if err := v.ensure(); err != nil {
		return readsvc.EntryPage{}, err
	}
	if parentID == "" || parentID == v.pin.Snapshot.NamespaceRootID {
		parentID = ""
	}
	children, err := v.service.Store.ListNamespaceChildren(
		ctx,
		v.pin.Snapshot.WorkspaceID,
		v.pin.Snapshot.NamespaceRootID,
		parentID,
	)
	if err != nil {
		return readsvc.EntryPage{}, err
	}
	limit := page.Limit
	if limit == 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	start := 0
	if page.Cursor != "" {
		cursorID, err := decodeCursor(page.Cursor)
		if err != nil {
			return readsvc.EntryPage{}, err
		}
		start = len(children)
		for i, child := range children {
			if child.ID == cursorID {
				start = i + 1
				break
			}
		}
	}
	if start > len(children) {
		start = len(children)
	}
	end := start + int(limit)
	if end > len(children) {
		end = len(children)
	}
	projected := make([]readsvc.NamespaceEntry, 0, end-start)
	for _, child := range children[start:end] {
		projected = append(projected, projectEntry(child))
	}
	result := readsvc.EntryPage{Entries: projected}
	if end < len(children) {
		result.NextCursor = encodeCursor(children[end-1].ID)
	}
	return result, nil
}

func (v *catalogView) ResolvePath(ctx context.Context, components []readsvc.PathComponent) (readsvc.NamespaceEntry, error) {
	if err := v.ensure(); err != nil {
		return readsvc.NamespaceEntry{}, err
	}
	current := v.syntheticRoot()
	for _, component := range components {
		next, err := v.Lookup(ctx, current.ID, component)
		if err != nil {
			return readsvc.NamespaceEntry{}, err
		}
		current = next
	}
	return current, nil
}

func (v *catalogView) ReadLink(ctx context.Context, entryID string) ([]byte, error) {
	entry, err := v.Stat(ctx, entryID)
	if err != nil {
		return nil, err
	}
	if entry.Kind != readsvc.EntrySymlink {
		return nil, ErrNotSymlink
	}
	return append([]byte(nil), entry.SymlinkTargetRaw...), nil
}

func (v *catalogView) lookupEntry(ctx context.Context, entryID string) (readsvc.NamespaceEntry, error) {
	if entryID == "" || entryID == v.pin.Snapshot.NamespaceRootID {
		return v.syntheticRoot(), nil
	}
	entry, err := v.service.Store.GetNamespaceEntry(ctx, v.pin.Snapshot.WorkspaceID, entryID)
	if err != nil {
		return readsvc.NamespaceEntry{}, err
	}
	if entry.RootID != v.pin.Snapshot.NamespaceRootID {
		return readsvc.NamespaceEntry{}, sqlite.ErrNotFound
	}
	return projectEntry(entry), nil
}

func (v *catalogView) syntheticRoot() readsvc.NamespaceEntry {
	return readsvc.NamespaceEntry{
		ID:              v.pin.Snapshot.NamespaceRootID,
		NamespaceRootID: v.pin.Snapshot.NamespaceRootID,
		DisplayName:     v.root.Name,
		RawName:         []byte(v.root.Name),
		Kind:            readsvc.EntryDirectory,
	}
}

func projectEntry(entry sqlite.NamespaceEntry) readsvc.NamespaceEntry {
	projected := readsvc.NamespaceEntry{
		ID:               entry.ID,
		NamespaceRootID:  entry.RootID,
		ParentID:         entry.ParentID,
		RawName:          append([]byte(nil), entry.RawName...),
		DisplayName:      entry.DisplayName,
		Kind:             readsvc.EntryKind(entry.EntryType),
		FileVersionID:    entry.FileVersionID,
		ContentID:        entry.ContentID,
		SymlinkTargetRaw: append([]byte(nil), entry.SymlinkTargetRaw...),
		HardlinkGroupID:  entry.HardlinkGroupID,
	}
	if entry.LogicalSize != nil {
		projected.HasLogicalSize = true
		if *entry.LogicalSize >= 0 {
			projected.LogicalSize = uint64(*entry.LogicalSize)
		}
	}
	var meta struct {
		ModTime        time.Time `json:"mod_time"`
		UID            uint64    `json:"uid"`
		GID            uint64    `json:"gid"`
		OwnershipKnown bool      `json:"ownership_known"`
	}
	if len(entry.Metadata) > 0 && json.Unmarshal(entry.Metadata, &meta) == nil {
		projected.ModTime = meta.ModTime
		if meta.OwnershipKnown {
			projected.HasOwnership = true
			projected.UID = uint32(meta.UID)
			projected.GID = uint32(meta.GID)
		}
	}
	return projected
}

func encodeCursor(entryID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(entryID))
}

func decodeCursor(cursor string) (string, error) {
	payload, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("invalid directory cursor: %w", err)
	}
	if len(payload) == 0 {
		return "", errors.New("invalid directory cursor")
	}
	return string(payload), nil
}
