// Package testutil provides shared fixtures for harness tests that need a
// real SQLite catalog. The package intentionally does not import the control
// plane so tests can use it without import cycles.
package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// NamespaceSeed holds the stable IDs of one seeded logical namespace.
type NamespaceSeed struct {
	WorkspaceID      string
	SourceID         string
	ScanGenerationID string
	RootID           string
	DirEntryID       string
	FileEntryID      string
	SymlinkEntryID   string
	FileVersionID    string
}

// OpenStore opens a fresh catalog at path ("" means ":memory:").
func OpenStore(t *testing.T, path string) *sqlite.Store {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, path, sqlite.Options{})
	if err != nil {
		t.Fatalf("open sqlite catalog: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// SeedNamespace writes a workspace, source, running scan generation,
// observation, representation, file version, namespace root, directory, file,
// and symlink into the catalog. It mirrors the store's own seeding tests.
func SeedNamespace(t *testing.T, store *sqlite.Store) *NamespaceSeed {
	t.Helper()
	ctx := context.Background()

	logicalSize := int64(16)
	allocatedSize := int64(12)

	workspace := sqlite.Workspace{ID: mustID(t, sqlite.IDPrefixWorkspace), Name: "Harness workspace"}
	source := sqlite.Source{
		ID: mustID(t, sqlite.IDPrefixSource), WorkspaceID: workspace.ID,
		StableKey: "apfs:harness", Kind: "APFS_ROOT", Locator: "/Media", State: sqlite.SourceActive,
	}
	scan := sqlite.ScanGeneration{
		ID: mustID(t, sqlite.IDPrefixScanGeneration), WorkspaceID: workspace.ID,
		SourceID: source.ID, Generation: 1, CaptureSetID: "capture-harness",
		CaptureSetDigest: "sha256:capture-harness", State: sqlite.ScanRunning,
	}
	observation := sqlite.Observation{
		ID: mustID(t, sqlite.IDPrefixObservation), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan.ID,
		PathKey: []byte("Music/track.flac"), RawPath: []byte("Music/track.flac"),
		DisplayPath: "Music/track.flac", EntryType: sqlite.EntryFile,
		ContentID: "sha256:file-content", FileVersionID: "scanner-version-fact",
		StatDigest: "sha256:stat", LogicalSize: &logicalSize,
		AllocatedSize: &allocatedSize, ReadState: "READ_OK",
	}
	representation := sqlite.Representation{
		ID: mustID(t, sqlite.IDPrefixRepresentation), WorkspaceID: workspace.ID,
		ContentID: "sha256:file-content", DecodedLength: logicalSize,
		OwnershipMode: sqlite.OwnershipEngineManaged, CodecProfileRef: "restic-stream/v1",
		AccessMode: sqlite.AccessSequentialStream, WholeReadRequiredToVerify: true,
		RecordDigest: "sha256:exact-representation",
	}
	fileVersion := sqlite.FileVersion{
		ID: mustID(t, sqlite.IDPrefixFileVersion), WorkspaceID: workspace.ID,
		ScanGenerationID: scan.ID, ObservationID: observation.ID,
		AssetID: "asset:track", ContentID: "sha256:file-content",
		LogicalSize: logicalSize, HashingProfile: "sha256-logical-zero-filled/v1",
		AuthoritativeRepresentationID: representation.ID,
		ExtentSetDigest:               "sha256:extent-set", HardlinkGroupID: "hardlink:99",
		SparseEvidence:  json.RawMessage(`{"state":"SPARSE"}`),
		VerificationRef: "verification:exact-1", RecordDigest: "sha256:file-version",
	}
	root := sqlite.NamespaceRoot{
		ID: mustID(t, sqlite.IDPrefixNamespaceRoot), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan.ID,
		SnapshotRef: "apfs-snapshot:2026-08-11T12:00:00Z", Name: "Media",
		RootPathKey: []byte{}, FilesystemSemantics: "APFS_NATIVE_V1",
		AuthorityDigest: "sha256:namespace-root",
	}
	dir := sqlite.NamespaceEntry{
		ID: mustID(t, sqlite.IDPrefixNamespaceEntry), WorkspaceID: workspace.ID,
		RootID: root.ID, RawName: []byte("Music"), DisplayName: "Music",
		FullPathKey: []byte("Music"), EntryType: sqlite.EntryDirectory,
	}
	rawFilename := []byte{0xff, 't', 'r', 'a', 'c', 'k', '.', 'f', 'l', 'a', 'c'}
	filePathKey := append(append([]byte("Music"), 0), rawFilename...)
	file := sqlite.NamespaceEntry{
		ID: mustID(t, sqlite.IDPrefixNamespaceEntry), WorkspaceID: workspace.ID,
		RootID: root.ID, ParentID: dir.ID, RawName: rawFilename,
		DisplayName: "\\xfftrack.flac", FullPathKey: filePathKey,
		EntryType: sqlite.EntryFile, ContentID: "sha256:file-content",
		FileVersionID: fileVersion.ID, HardlinkGroupID: "hardlink:99",
		LogicalSize: &logicalSize, AllocatedSize: &allocatedSize,
		Metadata: json.RawMessage(`{"mode":"0644","xattrs":true}`),
	}
	link := sqlite.NamespaceEntry{
		ID: mustID(t, sqlite.IDPrefixNamespaceEntry), WorkspaceID: workspace.ID,
		RootID: root.ID, ParentID: dir.ID, RawName: []byte("current"),
		DisplayName: "current", FullPathKey: []byte("Music\x00current"),
		EntryType: sqlite.EntrySymlink, SymlinkTargetRaw: rawFilename,
		SymlinkTargetDisplay: "\\xfftrack.flac",
	}

	if err := store.Update(ctx, func(tx *sqlite.Tx) error {
		if err := tx.InsertWorkspace(ctx, &workspace); err != nil {
			return err
		}
		if err := tx.InsertSource(ctx, &source); err != nil {
			return err
		}
		if err := tx.InsertScanGeneration(ctx, &scan); err != nil {
			return err
		}
		if err := tx.InsertObservation(ctx, &observation); err != nil {
			return err
		}
		if err := tx.InsertRepresentation(ctx, &representation); err != nil {
			return err
		}
		if err := tx.InsertFileVersion(ctx, &fileVersion); err != nil {
			return err
		}
		if err := tx.InsertNamespaceRoot(ctx, &root); err != nil {
			return err
		}
		if err := tx.InsertNamespaceEntry(ctx, &dir); err != nil {
			return err
		}
		if err := tx.InsertNamespaceEntry(ctx, &file); err != nil {
			return err
		}
		return tx.InsertNamespaceEntry(ctx, &link)
	}); err != nil {
		t.Fatalf("seed namespace: %v", err)
	}
	return &NamespaceSeed{
		WorkspaceID:      workspace.ID,
		SourceID:         source.ID,
		ScanGenerationID: scan.ID,
		RootID:           root.ID,
		DirEntryID:       dir.ID,
		FileEntryID:      file.ID,
		SymlinkEntryID:   link.ID,
		FileVersionID:    fileVersion.ID,
	}
}

// TempSocketPath returns a short socket path inside the system temp
// directory. Unix socket names are length-limited (104 bytes on macOS), so
// test temp directories cannot be used directly.
func TempSocketPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("rwtest-%d.sock", os.Getpid()))
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func mustID(t *testing.T, prefix string) string {
	t.Helper()
	id, err := sqlite.NewStableID(prefix)
	if err != nil {
		t.Fatalf("generate stable id: %v", err)
	}
	return id
}
