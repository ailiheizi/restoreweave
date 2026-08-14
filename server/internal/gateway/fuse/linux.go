//go:build linux

package fuse

import (
	"context"
	"errors"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/ailiheizi/restoreweave/server/internal/access"
	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// Supported reports whether this build can attach a kernel FUSE mount.
func Supported() bool { return true }

// Serve mounts export at opts.Mountpoint until ctx is cancelled. The mount
// is kernel-enforced read-only: ro,nodev,nosuid,noexec, no allow_other, and
// every mutation opcode returns EROFS.
func Serve(ctx context.Context, export Export, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	rootEntry, err := export.Host.View().Root(ctx)
	if err != nil {
		return err
	}
	inodes := newInodeMap()
	inodes.SetRoot(rootEntry.ID)
	root := &node{export: export, entry: rootEntry, inodes: inodes, rootID: rootEntry.ID}
	timeout := time.Second
	server, err := fs.Mount(opts.Mountpoint, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			AllowOther:    false,
			Options:       opts.effectiveFlags(),
			FsName:        fsName,
			Name:          subtype,
			DisableXAttrs: true,
		},
		EntryTimeout: &timeout,
		AttrTimeout:  &timeout,
		RootStableAttr: &fs.StableAttr{
			Ino:  rootInode,
			Mode: fuse.S_IFDIR,
		},
	})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = server.Unmount()
	}()
	server.Wait()
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

type node struct {
	fs.Inode
	export Export
	entry  readsvc.NamespaceEntry
	inodes *inodeMap
	rootID string
}

type fileHandle struct {
	session readsvc.ReadSession
	file    readsvc.RandomAccessFile
}

func (n *node) Getattr(ctx context.Context, _ fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	n.fillAttr(&out.Attr)
	return 0
}

func (n *node) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	entry, err := n.lookupChild(ctx, name)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return nil, syscall.ENOENT
		}
		return nil, syscall.EIO
	}
	child := &node{export: n.export, entry: entry, inodes: n.inodes, rootID: n.rootID}
	child.fillAttr(&out.Attr)
	return n.NewInode(ctx, child, fs.StableAttr{
		Mode: child.mode(),
		Ino:  child.inode(),
	}), 0
}

func (n *node) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	var entries []fuse.DirEntry
	var cursor string
	for {
		page, err := n.export.Host.View().ListChildren(ctx, n.entry.ID, readsvc.PageRequest{Cursor: cursor, Limit: 256})
		if err != nil {
			return nil, syscall.EIO
		}
		for _, entry := range page.Entries {
			name := entry.DisplayName
			if name == "" {
				name = string(entry.RawName)
			}
			child := &node{export: n.export, entry: entry, inodes: n.inodes, rootID: n.rootID}
			entries = append(entries, fuse.DirEntry{
				Mode: child.mode(),
				Name: name,
				Ino:  child.inode(),
			})
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return fs.NewListDirStream(entries), 0
}

func (n *node) Opendir(ctx context.Context) syscall.Errno {
	if n.entry.Kind != readsvc.EntryDirectory {
		return syscall.ENOTDIR
	}
	return 0
}

func (n *node) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
	target, err := n.export.Host.View().ReadLink(ctx, n.entry.ID)
	if err != nil {
		return nil, syscall.EINVAL
	}
	return target, 0
}

func (n *node) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if flags&(uint32(syscall.O_ACCMODE)) != uint32(syscall.O_RDONLY) ||
		flags&(syscall.O_TRUNC|syscall.O_APPEND|syscall.O_CREAT) != 0 {
		return nil, 0, syscall.EROFS
	}
	if n.entry.Kind != readsvc.EntryRegularFile {
		return nil, 0, syscall.EISDIR
	}
	session, err := n.export.Host.OpenEntrySession(ctx, readsvc.GatewayEntryOpenRequest{
		Access:  n.accessRequest(),
		EntryID: n.entry.ID,
	})
	if err != nil {
		return nil, 0, syscall.EIO
	}
	file, err := session.Open(ctx)
	if err != nil {
		_ = session.Close()
		return nil, 0, syscall.EIO
	}
	return &fileHandle{session: session, file: file}, 0, 0
}

func (h *fileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if off < 0 {
		return nil, syscall.EINVAL
	}
	result, err := h.file.ReadAt(ctx, dest, uint64(off))
	if err != nil {
		return nil, syscall.EIO
	}
	return fuse.ReadResultData(dest[:result.BytesRead]), 0
}

func (h *fileHandle) Release(ctx context.Context) syscall.Errno {
	_ = h.file.Close()
	_ = h.session.Close()
	return 0
}

func (n *node) Setattr(context.Context, fs.FileHandle, *fuse.SetAttrIn, *fuse.AttrOut) syscall.Errno {
	return syscall.EROFS
}
func (n *node) Setxattr(context.Context, string, []byte, uint32) syscall.Errno { return syscall.EROFS }
func (n *node) Removexattr(context.Context, string) syscall.Errno              { return syscall.EROFS }
func (n *node) Mkdir(context.Context, string, uint32, *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return nil, syscall.EROFS
}
func (n *node) Mknod(context.Context, string, uint32, uint32, *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return nil, syscall.EROFS
}
func (n *node) Link(context.Context, fs.InodeEmbedder, string, *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return nil, syscall.EROFS
}
func (n *node) Symlink(context.Context, string, string, *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return nil, syscall.EROFS
}
func (n *node) Create(context.Context, string, uint32, uint32, *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	return nil, nil, 0, syscall.EROFS
}
func (n *node) Unlink(context.Context, string) syscall.Errno { return syscall.EROFS }
func (n *node) Rmdir(context.Context, string) syscall.Errno  { return syscall.EROFS }
func (n *node) Rename(context.Context, string, fs.InodeEmbedder, string, uint32) syscall.Errno {
	return syscall.EROFS
}
func (n *node) Write(context.Context, fs.FileHandle, []byte, int64) (uint32, syscall.Errno) {
	return 0, syscall.EROFS
}
func (n *node) Fsync(context.Context, fs.FileHandle, uint32) syscall.Errno { return syscall.EROFS }
func (n *node) Allocate(context.Context, fs.FileHandle, uint64, uint64, uint32) syscall.Errno {
	return syscall.EROFS
}
func (n *node) CopyFileRange(context.Context, fs.FileHandle, uint64, *fs.Inode, fs.FileHandle, uint64, uint64, uint64) (uint32, syscall.Errno) {
	return 0, syscall.EROFS
}

func (n *node) lookupChild(ctx context.Context, name string) (readsvc.NamespaceEntry, error) {
	view := n.export.Host.View()
	entry, err := view.Lookup(ctx, n.entry.ID, readsvc.PathComponent{Raw: []byte(name)})
	if err == nil {
		return entry, nil
	}
	if !errors.Is(err, sqlite.ErrNotFound) {
		return readsvc.NamespaceEntry{}, err
	}
	return view.Lookup(ctx, n.entry.ID, readsvc.PathComponent{
		Normalized:           name,
		NormalizationProfile: "posix",
	})
}

func applyEntryStat(entry readsvc.NamespaceEntry, out *fuse.Attr) {
	stat := statFromEntry(entry)
	if !stat.ModTime.IsZero() {
		sec := uint64(stat.ModTime.Unix())
		nsec := uint32(stat.ModTime.Nanosecond())
		out.Mtime = sec
		out.Mtimensec = nsec
		out.Atime = sec
		out.Atimensec = nsec
		out.Ctime = sec
		out.Ctimensec = nsec
	}
	if stat.HasOwnership {
		out.Uid = stat.UID
		out.Gid = stat.GID
	}
}

func (n *node) fillAttr(out *fuse.Attr) {
	out.Ino = n.inode()
	out.Mode = n.mode()
	applyEntryStat(n.entry, out)
	switch n.entry.Kind {
	case readsvc.EntryDirectory:
		out.Nlink = 2
	case readsvc.EntrySymlink:
		out.Nlink = 1
		out.Size = uint64(len(n.entry.SymlinkTargetRaw))
	default:
		out.Nlink = 1
		if n.entry.HasLogicalSize {
			out.Size = n.entry.LogicalSize
		}
	}
}

func (n *node) mode() uint32 {
	switch n.entry.Kind {
	case readsvc.EntryDirectory:
		return fuse.S_IFDIR | 0555
	case readsvc.EntrySymlink:
		return fuse.S_IFLNK | 0777
	default:
		return fuse.S_IFREG | 0444
	}
}

func (n *node) inode() uint64 {
	if n.entry.ID == n.rootID {
		return rootInode
	}
	return n.inodes.Get(inodeKey(n.entry.ID, n.entry.HardlinkGroupID))
}

func (n *node) accessRequest() readsvc.AccessRequest {
	return access.LocalAccess("fuse")
}

var (
	_ fs.InodeEmbedder      = (*node)(nil)
	_ fs.NodeGetattrer      = (*node)(nil)
	_ fs.NodeLookuper       = (*node)(nil)
	_ fs.NodeReaddirer      = (*node)(nil)
	_ fs.NodeOpendirer      = (*node)(nil)
	_ fs.NodeReadlinker     = (*node)(nil)
	_ fs.NodeOpener         = (*node)(nil)
	_ fs.NodeSetattrer      = (*node)(nil)
	_ fs.NodeSetxattrer     = (*node)(nil)
	_ fs.NodeRemovexattrer  = (*node)(nil)
	_ fs.NodeMkdirer        = (*node)(nil)
	_ fs.NodeMknoder        = (*node)(nil)
	_ fs.NodeLinker         = (*node)(nil)
	_ fs.NodeSymlinker      = (*node)(nil)
	_ fs.NodeCreater        = (*node)(nil)
	_ fs.NodeUnlinker       = (*node)(nil)
	_ fs.NodeRmdirer        = (*node)(nil)
	_ fs.NodeRenamer        = (*node)(nil)
	_ fs.NodeWriter         = (*node)(nil)
	_ fs.NodeFsyncer        = (*node)(nil)
	_ fs.NodeAllocater      = (*node)(nil)
	_ fs.NodeCopyFileRanger = (*node)(nil)
	_ fs.FileReader         = (*fileHandle)(nil)
	_ fs.FileReleaser       = (*fileHandle)(nil)
)
