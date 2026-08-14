package fuse

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/access"
	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
)

// Export is the in-process, kernel-free projection of one snapshot view.
// The Linux FUSE adapter and portable tests both use it so CLI/restore/FUSE
// byte equality does not depend on /dev/fuse.
type Export struct {
	Host   readsvc.GatewayHost
	Access readsvc.FileAccess
}

func (e Export) ReadFile(ctx context.Context, relPath string) ([]byte, error) {
	entry, err := e.lookup(ctx, relPath)
	if err != nil {
		return nil, err
	}
	if entry.Kind != readsvc.EntryRegularFile {
		return nil, access.ErrNotFile
	}
	return access.ReadAll(ctx, e.Access, e.Host.View(), entry.ID)
}

func (e Export) ReadLink(ctx context.Context, relPath string) (string, error) {
	entry, err := e.lookup(ctx, relPath)
	if err != nil {
		return "", err
	}
	target, err := e.Host.View().ReadLink(ctx, entry.ID)
	if err != nil {
		return "", err
	}
	return string(target), nil
}

func (e Export) lookup(ctx context.Context, relPath string) (readsvc.NamespaceEntry, error) {
	view := e.Host.View()
	current, err := view.Root(ctx)
	if err != nil {
		return readsvc.NamespaceEntry{}, err
	}
	relPath = strings.Trim(relPath, "/")
	if relPath == "" || relPath == "." {
		return current, nil
	}
	for _, name := range strings.Split(relPath, "/") {
		if name == "" || name == "." {
			continue
		}
		if name == ".." {
			return readsvc.NamespaceEntry{}, errors.New("path escape is not allowed")
		}
		current, err = view.Lookup(ctx, current.ID, readsvc.PathComponent{
			Normalized:           name,
			NormalizationProfile: "posix",
		})
		if err != nil {
			return readsvc.NamespaceEntry{}, fmt.Errorf("%s: %w", relPath, err)
		}
	}
	return current, nil
}

// MutationOpcodes are write-capable FUSE operations that must fail with
// EROFS and have no side effects. The table is host-owned so Darwin tests
// can lock the policy without compiling go-fuse.
func MutationOpcodes() []string {
	return []string{
		"SETATTR",
		"MKNOD",
		"MKDIR",
		"UNLINK",
		"RMDIR",
		"SYMLINK",
		"RENAME",
		"LINK",
		"CREATE",
		"WRITE",
		"FALLOCATE",
		"SETXATTR",
		"REMOVEXATTR",
		"COPY_FILE_RANGE",
		"FSYNC",
	}
}
