package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestLogicalSnapshotNamespacePathTreeAndRangeLookup(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "namespace.sqlite"))
	defer store.Close()

	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Namespace workspace"}
	source := Source{
		ID: testID(t, IDPrefixSource), WorkspaceID: workspace.ID,
		StableKey: "apfs:namespace", Kind: "APFS_ROOT", Locator: "/Media", State: SourceActive,
	}
	scan := ScanGeneration{
		ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID,
		SourceID: source.ID, Generation: 1, CaptureSetID: "capture-namespace",
		CaptureSetDigest: "sha256:capture-namespace", State: ScanRunning,
	}
	logicalSize := int64(16)
	allocatedSize := int64(12)
	observation := Observation{
		ID: testID(t, IDPrefixObservation), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan.ID,
		PathKey: []byte("Music/track.flac"), RawPath: []byte("Music/track.flac"),
		DisplayPath: "Music/track.flac", EntryType: EntryFile,
		ContentID: "sha256:file-content", FileVersionID: "scanner-version-fact",
		StatDigest: "sha256:stat", LogicalSize: &logicalSize,
		AllocatedSize: &allocatedSize, ReadState: "READ_OK",
	}
	exactRepresentation := Representation{
		ID: testID(t, IDPrefixRepresentation), WorkspaceID: workspace.ID,
		ContentID: "sha256:file-content", DecodedLength: logicalSize,
		OwnershipMode: OwnershipEngineManaged, CodecProfileRef: "restic-stream/v1",
		AccessMode: AccessSequentialStream, WholeReadRequiredToVerify: true,
		RecordDigest: "sha256:exact-representation",
	}
	nativeRepresentation := Representation{
		ID: testID(t, IDPrefixRepresentation), WorkspaceID: workspace.ID,
		ContentID: "sha256:native-segment", DecodedLength: 4,
		OwnershipMode: OwnershipRestoreWeavePacks, CodecProfileRef: "raw/v1",
		AccessMode: AccessRandomNative, MinimumReadableUnit: 4,
		RecordDigest: "sha256:native-representation",
	}
	fileVersion := FileVersion{
		ID: testID(t, IDPrefixFileVersion), WorkspaceID: workspace.ID,
		ScanGenerationID: scan.ID, ObservationID: observation.ID,
		AssetID: "asset:track", ContentID: exactRepresentation.ContentID,
		LogicalSize: logicalSize, HashingProfile: "sha256-logical-zero-filled/v1",
		AuthoritativeRepresentationID: exactRepresentation.ID,
		ExtentSetDigest:               "sha256:extent-set", HardlinkGroupID: "hardlink:99",
		SparseEvidence:  json.RawMessage(`{"state":"SPARSE"}`),
		VerificationRef: "verification:exact-1", RecordDigest: "sha256:file-version",
	}
	root := NamespaceRoot{
		ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan.ID,
		SnapshotRef: "apfs-snapshot:2026-08-11T12:00:00Z", Name: "Media",
		RootPathKey: []byte{}, FilesystemSemantics: "APFS_NATIVE_V1",
		AuthorityDigest: "sha256:namespace-root",
	}
	musicDirectory := NamespaceEntry{
		ID: testID(t, IDPrefixNamespaceEntry), WorkspaceID: workspace.ID,
		RootID: root.ID, RawName: []byte("Music"), DisplayName: "Music",
		FullPathKey: []byte("Music"), EntryType: EntryDirectory,
	}
	rawFilename := []byte{0xff, 't', 'r', 'a', 'c', 'k', '.', 'f', 'l', 'a', 'c'}
	filePathKey := append(append([]byte("Music"), 0), rawFilename...)
	file := NamespaceEntry{
		ID: testID(t, IDPrefixNamespaceEntry), WorkspaceID: workspace.ID,
		RootID: root.ID, ParentID: musicDirectory.ID, RawName: rawFilename,
		DisplayName: "\\xfftrack.flac", FullPathKey: filePathKey,
		EntryType: EntryFile, ContentID: "sha256:file-content",
		FileVersionID: fileVersion.ID, HardlinkGroupID: "hardlink:99",
		LogicalSize: &logicalSize, AllocatedSize: &allocatedSize,
		Metadata: json.RawMessage(`{"mode":"0644","xattrs":true}`),
	}
	link := NamespaceEntry{
		ID: testID(t, IDPrefixNamespaceEntry), WorkspaceID: workspace.ID,
		RootID: root.ID, ParentID: musicDirectory.ID, RawName: []byte("current"),
		DisplayName: "current", FullPathKey: []byte("Music\x00current"),
		EntryType: EntrySymlink, SymlinkTargetRaw: rawFilename,
		SymlinkTargetDisplay: "\\xfftrack.flac",
	}

	if err := store.Update(ctx, func(tx *Tx) error {
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
		if err := tx.InsertRepresentation(ctx, &exactRepresentation); err != nil {
			return err
		}
		if err := tx.InsertRepresentation(ctx, &nativeRepresentation); err != nil {
			return err
		}
		if err := tx.InsertFileVersion(ctx, &fileVersion); err != nil {
			return err
		}
		if err := tx.InsertNamespaceRoot(ctx, &root); err != nil {
			return err
		}
		if err := tx.InsertNamespaceEntry(ctx, &musicDirectory); err != nil {
			return err
		}
		if err := tx.InsertNamespaceEntry(ctx, &file); err != nil {
			return err
		}
		return tx.InsertNamespaceEntry(ctx, &link)
	}); err != nil {
		t.Fatalf("insert logical namespace: %v", err)
	}

	found, err := store.LookupNamespaceEntry(ctx, workspace.ID, root.ID, filePathKey)
	if err != nil {
		t.Fatalf("LookupNamespaceEntry: %v", err)
	}
	if found.ID != file.ID || !bytes.Equal(found.RawName, rawFilename) || found.DisplayName == string(found.RawName) {
		t.Fatalf("raw/display path identity was not preserved: %+v", found)
	}
	children, err := store.ListNamespaceChildren(ctx, workspace.ID, root.ID, musicDirectory.ID)
	if err != nil {
		t.Fatalf("ListNamespaceChildren: %v", err)
	}
	if len(children) != 2 || children[0].DisplayName != "current" || children[1].ID != file.ID {
		t.Fatalf("unexpected ordered children: %+v", children)
	}
	subtree, err := store.ListNamespaceSubtree(ctx, workspace.ID, root.ID, musicDirectory.ID)
	if err != nil {
		t.Fatalf("ListNamespaceSubtree: %v", err)
	}
	if len(subtree) != 3 || subtree[0].Entry.ID != musicDirectory.ID || subtree[0].Depth != 0 {
		t.Fatalf("unexpected subtree root: %+v", subtree)
	}
	for _, node := range subtree[1:] {
		if node.Depth != 1 {
			t.Fatalf("child depth = %d, want 1", node.Depth)
		}
	}

	offset := int64(4096)
	length := int64(4)
	encodedLength := int64(4)
	packLocator := PhysicalLocator{
		ID: testID(t, IDPrefixPhysicalLocator), WorkspaceID: workspace.ID,
		RepresentationID: nativeRepresentation.ID, ContentID: nativeRepresentation.ContentID,
		OwnershipMode: OwnershipRestoreWeavePacks, Kind: LocatorPackRange,
		BackendID: "backend:native-a", RepositoryID: "repository:native-a",
		PlacementGeneration: 7, ContainerRef: "restoreweave-pack:abc123",
		ByteOffset: &offset, ByteLength: &length, EncodedLength: &encodedLength,
		EncodedDigest:    "sha256:encoded-segment-1",
		AuthorityRef:     "signed-placement:receipt-7",
		ReaderProfileRef: "capsule-reader:native-pack-v1",
		Locator:          json.RawMessage(`{"record":42,"compression":"none"}`),
	}
	engineReadRef := EngineReadRef{
		ID: testID(t, IDPrefixEngineReadRef), WorkspaceID: workspace.ID,
		RepresentationID: exactRepresentation.ID, RepositoryID: "repository:restic-a",
		EngineSnapshotRef: "restic-snapshot:deadbeef",
		EngineReceiptRef:  "signed-restic-receipt:7", EnginePathKey: filePathKey,
		PlacementCheckpointID:     "checkpoint:7",
		PlacementCheckpointDigest: "sha256:checkpoint-7",
		ReaderProfileRef:          "capsule-reader:restic-v1",
		Metadata:                  json.RawMessage(`{"baselineAccess":"SEQUENTIAL_STREAM"}`),
	}
	first := ContentExtent{
		ID: testID(t, IDPrefixContentExtent), WorkspaceID: workspace.ID,
		FileVersionID: fileVersion.ID, Ordinal: 0, LogicalOffset: 0, LogicalLength: 4,
		Kind: ExtentData, RepresentationID: exactRepresentation.ID, RepresentationOffset: 0,
		ExtentDigest: "sha256:extent-1",
	}
	hole := ContentExtent{
		ID: testID(t, IDPrefixContentExtent), WorkspaceID: workspace.ID,
		FileVersionID: fileVersion.ID, Ordinal: 1, LogicalOffset: 4, LogicalLength: 4,
		Kind: ExtentHole, ExtentDigest: "sha256:zero-extent",
	}
	last := ContentExtent{
		ID: testID(t, IDPrefixContentExtent), WorkspaceID: workspace.ID,
		FileVersionID: fileVersion.ID, Ordinal: 2, LogicalOffset: 8, LogicalLength: 8,
		Kind: ExtentData, RepresentationID: exactRepresentation.ID, RepresentationOffset: 8,
		ExtentDigest: "sha256:extent-2",
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertEngineReadRef(ctx, &engineReadRef); err != nil {
			return err
		}
		if err := tx.InsertPhysicalLocator(ctx, &packLocator); err != nil {
			return err
		}
		if err := tx.InsertContentExtent(ctx, &first); err != nil {
			return err
		}
		if err := tx.InsertContentExtent(ctx, &hole); err != nil {
			return err
		}
		return tx.InsertContentExtent(ctx, &last)
	}); err != nil {
		t.Fatalf("insert extent and locator records: %v", err)
	}

	storedLocator, err := store.GetPhysicalLocator(ctx, workspace.ID, packLocator.ID)
	if err != nil {
		t.Fatalf("GetPhysicalLocator: %v", err)
	}
	if storedLocator.Kind != LocatorPackRange || storedLocator.ByteOffset == nil ||
		*storedLocator.ByteOffset != 4096 || storedLocator.ByteLength == nil ||
		*storedLocator.ByteLength != 4 || storedLocator.ContainerRef == "" {
		t.Fatalf("range-readable native-pack locator was not preserved: %+v", storedLocator)
	}
	engineRefs, err := store.ListEngineReadRefs(ctx, workspace.ID, exactRepresentation.ID)
	if err != nil || len(engineRefs) != 1 || !bytes.Equal(engineRefs[0].EnginePathKey, filePathKey) {
		t.Fatalf("opaque Restic engine read refs = %+v, err=%v", engineRefs, err)
	}
	storedRepresentation, err := store.GetRepresentation(ctx, workspace.ID, exactRepresentation.ID)
	if err != nil || storedRepresentation.AccessMode != AccessSequentialStream {
		t.Fatalf("Restic representation access capability = %+v, err=%v", storedRepresentation, err)
	}
	listed, err := store.ListRepresentationsByContentID(ctx, workspace.ID, exactRepresentation.ContentID)
	if err != nil || len(listed) != 1 || listed[0].ID != exactRepresentation.ID {
		t.Fatalf("ListRepresentationsByContentID = %+v, err=%v", listed, err)
	}
	extents, err := store.ListContentExtents(ctx, workspace.ID, fileVersion.ID)
	if err != nil {
		t.Fatalf("ListContentExtents: %v", err)
	}
	if len(extents) != 3 || extents[0].Ordinal != 0 || extents[1].Kind != ExtentHole ||
		extents[2].LogicalOffset != 8 || extents[2].RepresentationOffset != 8 {
		t.Fatalf("unexpected ordered content extents: %+v", extents)
	}
	if err := store.ValidateFileVersionExtents(ctx, workspace.ID, fileVersion.ID); err != nil {
		t.Fatalf("ValidateFileVersionExtents: %v", err)
	}

	outOfOrder := ContentExtent{
		ID: testID(t, IDPrefixContentExtent), WorkspaceID: workspace.ID,
		FileVersionID: fileVersion.ID, Ordinal: 4, LogicalOffset: 15, LogicalLength: 1,
		Kind: ExtentData, RepresentationID: exactRepresentation.ID,
		RepresentationOffset: 15, ExtentDigest: "sha256:bad-extent",
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertContentExtent(ctx, &outOfOrder)
	}); err == nil {
		t.Fatal("out-of-order content extent unexpectedly succeeded")
	}

	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.FinishScanGeneration(ctx, workspace.ID, scan.ID, ScanComplete, true,
			json.RawMessage(`{"entries":3}`), testEpoch.Add(time.Minute))
	}); err != nil {
		t.Fatalf("finish scan generation: %v", err)
	}
	lateRoot := root
	lateRoot.ID = testID(t, IDPrefixNamespaceRoot)
	lateRoot.RootPathKey = []byte("late")
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertNamespaceRoot(ctx, &lateRoot)
	}); err == nil {
		t.Fatal("namespace root after completed scan unexpectedly succeeded")
	}
}

func TestMetadataOnlyRegularFileHasNoContentOrFileVersion(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "metadata-only-namespace.sqlite"))
	defer store.Close()

	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Metadata-only workspace"}
	source := Source{
		ID: testID(t, IDPrefixSource), WorkspaceID: workspace.ID,
		StableKey: "local:metadata-only", Kind: "LOCAL_TREE", Locator: "/source", State: SourceActive,
	}
	scan := ScanGeneration{
		ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID, SourceID: source.ID,
		Generation: 1, CaptureSetID: "capture:metadata-only",
		CaptureSetDigest: "sha256:metadata-only", State: ScanRunning,
	}
	root := NamespaceRoot{
		ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan.ID, SnapshotRef: "snapshot:metadata-only",
		Name: "source", RootPathKey: []byte{}, FilesystemSemantics: "posix",
		AuthorityDigest: "sha256:metadata-only-root",
	}
	size := int64(17)
	entry := NamespaceEntry{
		ID: testID(t, IDPrefixNamespaceEntry), WorkspaceID: workspace.ID, RootID: root.ID,
		RawName: []byte("unreadable.bin"), DisplayName: "unreadable.bin",
		FullPathKey: []byte("unreadable.bin"), EntryType: EntryFile,
		LogicalSize: &size, Metadata: json.RawMessage(`{"read_state":"FAILED"}`),
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertWorkspace(ctx, &workspace); err != nil {
			return err
		}
		if err := tx.InsertSource(ctx, &source); err != nil {
			return err
		}
		if err := tx.InsertScanGeneration(ctx, &scan); err != nil {
			return err
		}
		if err := tx.InsertNamespaceRoot(ctx, &root); err != nil {
			return err
		}
		return tx.InsertNamespaceEntry(ctx, &entry)
	}); err != nil {
		t.Fatalf("insert metadata-only namespace entry: %v", err)
	}
	found, err := store.GetNamespaceEntry(ctx, workspace.ID, entry.ID)
	if err != nil {
		t.Fatalf("get metadata-only namespace entry: %v", err)
	}
	if found.EntryType != EntryFile || found.ContentID != "" || found.FileVersionID != "" || found.LogicalSize == nil || *found.LogicalSize != size {
		t.Fatalf("metadata-only namespace entry = %+v", found)
	}

	invalid := entry
	invalid.ID = testID(t, IDPrefixNamespaceEntry)
	invalid.FullPathKey = []byte("false-content.bin")
	invalid.RawName = []byte("false-content.bin")
	invalid.ContentID = "sha256:unproven"
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertNamespaceEntry(ctx, &invalid) }); err == nil {
		t.Fatal("metadata-only entry with an unbacked content identity was accepted")
	}
}

func TestNamespaceRejectsCrossRootParentAndInvalidSparseOrdering(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "namespace-invariants.sqlite"))
	defer store.Close()

	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Invariant workspace"}
	source := Source{
		ID: testID(t, IDPrefixSource), WorkspaceID: workspace.ID,
		StableKey: "apfs:invariants", Kind: "APFS_ROOT", Locator: "/", State: SourceActive,
	}
	scan := ScanGeneration{
		ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID,
		SourceID: source.ID, Generation: 1, CaptureSetID: "capture-invariants",
		CaptureSetDigest: "sha256:capture-invariants", State: ScanRunning,
	}
	logicalSize := int64(6)
	observation := Observation{
		ID: testID(t, IDPrefixObservation), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan.ID,
		PathKey: []byte("sparse"), RawPath: []byte("sparse"), DisplayPath: "sparse",
		EntryType: EntryFile, ContentID: "sha256:sparse", FileVersionID: "scanner:sparse",
		StatDigest: "sha256:sparse-stat", LogicalSize: &logicalSize, ReadState: "READ_OK",
	}
	representation := Representation{
		ID: testID(t, IDPrefixRepresentation), WorkspaceID: workspace.ID,
		ContentID: observation.ContentID, DecodedLength: logicalSize,
		OwnershipMode: OwnershipEngineManaged, CodecProfileRef: "restic-stream/v1",
		AccessMode: AccessSequentialStream, RecordDigest: "sha256:sparse-representation",
	}
	fileVersion := FileVersion{
		ID: testID(t, IDPrefixFileVersion), WorkspaceID: workspace.ID,
		ScanGenerationID: scan.ID, ObservationID: observation.ID,
		ContentID: observation.ContentID, LogicalSize: logicalSize,
		HashingProfile: "sha256/v1", AuthoritativeRepresentationID: representation.ID,
		ExtentSetDigest: "sha256:sparse-extents", SparseEvidence: json.RawMessage(`{"state":"SPARSE"}`),
		RecordDigest: "sha256:sparse-version",
	}
	rootA := NamespaceRoot{
		ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan.ID, SnapshotRef: "snapshot:a",
		Name: "A", RootPathKey: []byte("a"), FilesystemSemantics: "APFS_NATIVE_V1",
		AuthorityDigest: "sha256:a",
	}
	rootB := rootA
	rootB.ID = testID(t, IDPrefixNamespaceRoot)
	rootB.Name = "B"
	rootB.RootPathKey = []byte("b")
	rootB.SnapshotRef = "snapshot:b"
	rootB.AuthorityDigest = "sha256:b"
	parent := NamespaceEntry{
		ID: testID(t, IDPrefixNamespaceEntry), WorkspaceID: workspace.ID,
		RootID: rootA.ID, RawName: []byte("dir"), DisplayName: "dir",
		FullPathKey: []byte("dir"), EntryType: EntryDirectory,
	}
	if err := store.Update(ctx, func(tx *Tx) error {
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
		if err := tx.InsertNamespaceRoot(ctx, &rootA); err != nil {
			return err
		}
		if err := tx.InsertNamespaceRoot(ctx, &rootB); err != nil {
			return err
		}
		return tx.InsertNamespaceEntry(ctx, &parent)
	}); err != nil {
		t.Fatalf("seed namespace invariants: %v", err)
	}

	crossRootChild := NamespaceEntry{
		ID: testID(t, IDPrefixNamespaceEntry), WorkspaceID: workspace.ID,
		RootID: rootB.ID, ParentID: parent.ID, RawName: []byte("child"),
		DisplayName: "child", FullPathKey: []byte("child"), EntryType: EntryDirectory,
	}
	err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertNamespaceEntry(ctx, &crossRootChild)
	})
	if err == nil {
		t.Fatal("cross-root parent relationship unexpectedly succeeded")
	}

	firstWithGap := ContentExtent{
		ID: testID(t, IDPrefixContentExtent), WorkspaceID: workspace.ID,
		FileVersionID: fileVersion.ID, Ordinal: 0, LogicalOffset: 4, LogicalLength: 2,
		Kind: ExtentHole, ExtentDigest: "sha256:hole",
	}
	err = store.Update(ctx, func(tx *Tx) error {
		return tx.InsertContentExtent(ctx, &firstWithGap)
	})
	if err == nil || !stringsContain(err.Error(), "first content extent") {
		t.Fatalf("invalid first sparse extent error = %v", err)
	}
	if _, err := store.LookupNamespaceEntry(ctx, workspace.ID, rootA.ID, []byte("missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing namespace lookup error = %v, want ErrNotFound", err)
	}
}

func stringsContain(value, fragment string) bool {
	return bytes.Contains([]byte(value), []byte(fragment))
}

func TestStableSubjectContinuityUsesCommittedSamePathAndKeepsLegacyIDs(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "stable-subject.sqlite"))
	defer store.Close()

	workspace := Workspace{ID: testID(t, IDPrefixWorkspace), Name: "Stable subject workspace"}
	source := Source{ID: testID(t, IDPrefixSource), WorkspaceID: workspace.ID,
		StableKey: "local:stable-subject", Kind: "LOCAL_TREE", Locator: "/source", State: SourceActive}
	scan1 := ScanGeneration{ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID,
		SourceID: source.ID, Generation: 1, CaptureSetID: "capture:stable:1",
		CaptureSetDigest: "sha256:stable-capture-1", State: ScanRunning}
	root1 := NamespaceRoot{ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan1.ID, SnapshotRef: "snapshot:stable:1",
		Name: "source", RootPathKey: []byte{}, FilesystemSemantics: "posix",
		AuthorityDigest: "sha256:stable-root-1"}
	binding1 := CaptureRootBinding{ID: testID(t, IDPrefixCaptureBinding), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan1.ID, CaptureMode: "ROOTED_FD",
		Profile: "test", DisplayPath: "/source", ConsistencyClaim: "TEST",
		IdentityDigest: "sha256:stable-binding-1", Record: json.RawMessage(`{}`)}
	legacy := NamespaceEntry{ID: testID(t, IDPrefixNamespaceEntry), WorkspaceID: workspace.ID,
		RootID: root1.ID, RawName: []byte("legacy.txt"), DisplayName: "legacy.txt",
		FullPathKey: []byte("legacy.txt"), EntryType: EntryFile}
	// A file entry without a file version is metadata-only and is sufficient
	// to exercise the namespace identity mapping without unrelated records.
	if err := store.Update(ctx, func(tx *Tx) error {
		for _, record := range []any{&workspace, &source, &scan1} {
			switch value := record.(type) {
			case *Workspace:
				if err := tx.InsertWorkspace(ctx, value); err != nil {
					return err
				}
			case *Source:
				if err := tx.InsertSource(ctx, value); err != nil {
					return err
				}
			case *ScanGeneration:
				if err := tx.InsertScanGeneration(ctx, value); err != nil {
					return err
				}
			}
		}
		if err := tx.InsertNamespaceRoot(ctx, &root1); err != nil {
			return err
		}
		if err := tx.InsertCaptureRootBinding(ctx, &binding1); err != nil {
			return err
		}
		return tx.InsertNamespaceEntry(ctx, &legacy)
	}); err != nil {
		t.Fatalf("seed first namespace generation: %v", err)
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.FinishScanGeneration(ctx, workspace.ID, scan1.ID, ScanComplete, true,
			json.RawMessage(`{"entries":1}`), testEpoch.Add(time.Minute)); err != nil {
			return err
		}
		return tx.InsertPublication(ctx, &Publication{ID: testID(t, IDPrefixPublication),
			WorkspaceID: workspace.ID, SnapshotRef: "snapshot:stable:1", ScanGenerationID: scan1.ID,
			BindingID: binding1.ID, NamespaceRootID: root1.ID, ManifestDigest: "sha256:stable-manifest-1"})
	}); err != nil {
		t.Fatalf("commit first namespace generation: %v", err)
	}
	legacyRead, err := store.GetNamespaceEntry(ctx, workspace.ID, legacy.ID)
	if err != nil {
		t.Fatalf("read legacy namespace entry: %v", err)
	}
	if legacyRead.SubjectRef != legacy.ID {
		t.Fatalf("legacy subject ref = %q, want entry ID %q", legacyRead.SubjectRef, legacy.ID)
	}

	scan2 := ScanGeneration{ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID,
		SourceID: source.ID, Generation: 2, CaptureSetID: "capture:stable:2",
		CaptureSetDigest: "sha256:stable-capture-2", State: ScanRunning}
	root2 := NamespaceRoot{ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan2.ID, SnapshotRef: "snapshot:stable:2",
		Name: "source", RootPathKey: []byte("generation-2"), FilesystemSemantics: "posix",
		AuthorityDigest: "sha256:stable-root-2"}
	binding2 := CaptureRootBinding{ID: testID(t, IDPrefixCaptureBinding), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan2.ID, CaptureMode: "ROOTED_FD",
		Profile: "test", DisplayPath: "/source", ConsistencyClaim: "TEST",
		IdentityDigest: "sha256:stable-binding-2", Record: json.RawMessage(`{}`)}
	var resolved string
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertScanGeneration(ctx, &scan2); err != nil {
			return err
		}
		if err := tx.InsertNamespaceRoot(ctx, &root2); err != nil {
			return err
		}
		if err := tx.InsertCaptureRootBinding(ctx, &binding2); err != nil {
			return err
		}
		var err error
		resolved, err = tx.ResolveNamespaceSubjectRef(ctx, workspace.ID, source.ID, []byte("legacy.txt"), EntryFile)
		return err
	}); err != nil {
		t.Fatalf("resolve continuity subject: %v", err)
	}
	if resolved != legacy.ID {
		t.Fatalf("resolved continuity subject = %q, want legacy subject %q", resolved, legacy.ID)
	}
	current := NamespaceEntry{ID: testID(t, IDPrefixNamespaceEntry), SubjectRef: resolved,
		WorkspaceID: workspace.ID, RootID: root2.ID, RawName: []byte("legacy.txt"),
		DisplayName: "legacy.txt", FullPathKey: []byte("legacy.txt"), EntryType: EntryFile}
	if err := store.Update(ctx, func(tx *Tx) error {
		return tx.InsertNamespaceEntry(ctx, &current)
	}); err != nil {
		t.Fatalf("insert second namespace observation: %v", err)
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.FinishScanGeneration(ctx, workspace.ID, scan2.ID, ScanComplete, true,
			json.RawMessage(`{"entries":1}`), testEpoch.Add(2*time.Minute)); err != nil {
			return err
		}
		return tx.InsertPublication(ctx, &Publication{ID: testID(t, IDPrefixPublication),
			WorkspaceID: workspace.ID, SnapshotRef: "snapshot:stable:2", ScanGenerationID: scan2.ID,
			BindingID: binding2.ID, NamespaceRootID: root2.ID, ManifestDigest: "sha256:stable-manifest-2"})
	}); err != nil {
		t.Fatalf("commit second namespace generation: %v", err)
	}
	latest, err := store.LookupLatestNamespaceEntryBySubjectRef(ctx, workspace.ID, resolved)
	if err != nil {
		t.Fatalf("lookup latest stable subject: %v", err)
	}
	if latest.ID != current.ID || latest.SubjectRef != resolved || latest.RootID != root2.ID {
		t.Fatalf("latest stable subject entry = %+v, want second observation", latest)
	}

	// A committed snapshot in which the path is absent creates a real
	// continuity gap. The next observation must not search through root2 and
	// resurrect its subject.
	scan3 := ScanGeneration{ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID,
		SourceID: source.ID, Generation: 3, CaptureSetID: "capture:stable:3",
		CaptureSetDigest: "sha256:stable-capture-3", State: ScanRunning}
	root3 := NamespaceRoot{ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan3.ID, SnapshotRef: "snapshot:stable:3",
		Name: "source", RootPathKey: []byte("generation-3"), FilesystemSemantics: "posix",
		AuthorityDigest: "sha256:stable-root-3"}
	binding3 := CaptureRootBinding{ID: testID(t, IDPrefixCaptureBinding), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan3.ID, CaptureMode: "ROOTED_FD",
		Profile: "test", DisplayPath: "/source", ConsistencyClaim: "TEST",
		IdentityDigest: "sha256:stable-binding-3", Record: json.RawMessage(`{}`)}
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertScanGeneration(ctx, &scan3); err != nil {
			return err
		}
		if err := tx.InsertNamespaceRoot(ctx, &root3); err != nil {
			return err
		}
		if err := tx.InsertCaptureRootBinding(ctx, &binding3); err != nil {
			return err
		}
		if err := tx.FinishScanGeneration(ctx, workspace.ID, scan3.ID, ScanComplete, true,
			json.RawMessage(`{"entries":0}`), testEpoch.Add(3*time.Minute)); err != nil {
			return err
		}
		return tx.InsertPublication(ctx, &Publication{ID: testID(t, IDPrefixPublication),
			WorkspaceID: workspace.ID, SnapshotRef: root3.SnapshotRef, ScanGenerationID: scan3.ID,
			BindingID: binding3.ID, NamespaceRootID: root3.ID, ManifestDigest: "sha256:stable-manifest-3"})
	}); err != nil {
		t.Fatalf("commit disappearance generation: %v", err)
	}
	scan4 := ScanGeneration{ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID,
		SourceID: source.ID, Generation: 4, CaptureSetID: "capture:stable:4",
		CaptureSetDigest: "sha256:stable-capture-4", State: ScanRunning}
	root4 := NamespaceRoot{ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan4.ID, SnapshotRef: "snapshot:stable:4",
		Name: "source", RootPathKey: []byte("generation-4"), FilesystemSemantics: "posix",
		AuthorityDigest: "sha256:stable-root-4"}
	binding4 := CaptureRootBinding{ID: testID(t, IDPrefixCaptureBinding), WorkspaceID: workspace.ID,
		SourceID: source.ID, ScanGenerationID: scan4.ID, CaptureMode: "ROOTED_FD",
		Profile: "test", DisplayPath: "/source", ConsistencyClaim: "TEST",
		IdentityDigest: "sha256:stable-binding-4", Record: json.RawMessage(`{}`)}
	var afterGap string
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertScanGeneration(ctx, &scan4); err != nil {
			return err
		}
		if err := tx.InsertNamespaceRoot(ctx, &root4); err != nil {
			return err
		}
		if err := tx.InsertCaptureRootBinding(ctx, &binding4); err != nil {
			return err
		}
		var err error
		afterGap, err = tx.ResolveNamespaceSubjectRef(ctx, workspace.ID, source.ID, []byte("legacy.txt"), EntryFile)
		return err
	}); err != nil {
		t.Fatalf("resolve after disappearance: %v", err)
	}
	if afterGap != "" {
		t.Fatalf("subject survived a committed disappearance: %q", afterGap)
	}
	newSubject := testID(t, IDPrefixSubject)
	reappeared := current
	reappeared.ID = testID(t, IDPrefixNamespaceEntry)
	reappeared.SubjectRef = newSubject
	reappeared.RootID = root4.ID
	if err := store.Update(ctx, func(tx *Tx) error {
		if err := tx.InsertNamespaceEntry(ctx, &reappeared); err != nil {
			return err
		}
		if err := tx.FinishScanGeneration(ctx, workspace.ID, scan4.ID, ScanComplete, true,
			json.RawMessage(`{"entries":1}`), testEpoch.Add(4*time.Minute)); err != nil {
			return err
		}
		return tx.InsertPublication(ctx, &Publication{ID: testID(t, IDPrefixPublication),
			WorkspaceID: workspace.ID, SnapshotRef: root4.SnapshotRef, ScanGenerationID: scan4.ID,
			BindingID: binding4.ID, NamespaceRootID: root4.ID, ManifestDigest: "sha256:stable-manifest-4"})
	}); err != nil {
		t.Fatalf("commit reappearance generation: %v", err)
	}
	if latest, err := store.LookupLatestNamespaceEntryBySubjectRef(ctx, workspace.ID, newSubject); err != nil {
		t.Fatalf("lookup reappeared subject: %v", err)
	} else if latest.ID != reappeared.ID {
		t.Fatalf("reappeared entry = %+v", latest)
	}

	differentPath := current
	differentPath.ID = testID(t, IDPrefixNamespaceEntry)
	differentPath.RawName = []byte("different.txt")
	differentPath.DisplayName = "different.txt"
	differentPath.FullPathKey = []byte("different.txt")
	differentPath.SubjectRef = ""
	if err := store.Update(ctx, func(tx *Tx) error { return tx.InsertNamespaceEntry(ctx, &differentPath) }); err != nil {
		t.Fatalf("insert different path: %v", err)
	}
	if differentPath.SubjectRef == resolved {
		t.Fatal("different path unexpectedly reused stable subject")
	}
	if err := store.Update(ctx, func(tx *Tx) error {
		otherSource := Source{ID: testID(t, IDPrefixSource), WorkspaceID: workspace.ID,
			StableKey: "local:other", Kind: "LOCAL_TREE", Locator: "/other", State: SourceActive}
		otherScan := ScanGeneration{ID: testID(t, IDPrefixScanGeneration), WorkspaceID: workspace.ID,
			SourceID: otherSource.ID, Generation: 1, CaptureSetID: "capture:other",
			CaptureSetDigest: "sha256:other", State: ScanRunning}
		otherRoot := NamespaceRoot{ID: testID(t, IDPrefixNamespaceRoot), WorkspaceID: workspace.ID,
			SourceID: otherSource.ID, ScanGenerationID: otherScan.ID, SnapshotRef: "snapshot:other",
			Name: "other", RootPathKey: []byte{}, FilesystemSemantics: "posix",
			AuthorityDigest: "sha256:other-root"}
		if err := tx.InsertSource(ctx, &otherSource); err != nil {
			return err
		}
		if err := tx.InsertScanGeneration(ctx, &otherScan); err != nil {
			return err
		}
		if err := tx.InsertNamespaceRoot(ctx, &otherRoot); err != nil {
			return err
		}
		foreign := current
		foreign.ID = testID(t, IDPrefixNamespaceEntry)
		foreign.RootID = otherRoot.ID
		foreign.SubjectRef = resolved
		return tx.InsertNamespaceEntry(ctx, &foreign)
	}); err == nil {
		t.Fatal("stable subject was accepted under a different source")
	}
}

func TestStableSubjectMigrationPreservesLegacyRowsAndStartsRefreshFromLatest(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-v21.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at_ns INTEGER NOT NULL
) STRICT`); err != nil {
		t.Fatal(err)
	}
	for _, item := range migrations[:len(migrations)-2] {
		if _, err := db.ExecContext(ctx, item.sql); err != nil {
			t.Fatalf("apply legacy migration %d: %v", item.version, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, name, checksum, applied_at_ns) VALUES (?, ?, ?, ?)`,
			item.version, item.name, migrationChecksum(item), testEpoch.UnixNano()); err != nil {
			t.Fatalf("record legacy migration %d: %v", item.version, err)
		}
	}

	workspaceID := testID(t, IDPrefixWorkspace)
	sourceID := testID(t, IDPrefixSource)
	if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(
workspace_id, name, metadata_json, revision, created_at_ns, updated_at_ns)
VALUES (?, ?, '{}', 1, ?, ?)`, workspaceID, "legacy", testEpoch.UnixNano(), testEpoch.UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sources(
source_id, workspace_id, stable_key, kind, locator, identity_fingerprint,
state, metadata_json, revision, created_at_ns, updated_at_ns)
VALUES (?, ?, ?, 'LOCAL_TREE', ?, ?, 'ACTIVE', '{}', 1, ?, ?)`,
		sourceID, workspaceID, "local:legacy", "/legacy", "sha256:legacy-source", testEpoch.UnixNano(), testEpoch.UnixNano()); err != nil {
		t.Fatal(err)
	}

	type legacyObservation struct {
		scanID, rootID, bindingID, entryID string
	}
	var observations []legacyObservation
	seedPublication := func(generation int64, includeEntry bool) legacyObservation {
		scan := legacyObservation{scanID: testID(t, IDPrefixScanGeneration), rootID: testID(t, IDPrefixNamespaceRoot), bindingID: testID(t, IDPrefixCaptureBinding)}
		if includeEntry {
			scan.entryID = testID(t, IDPrefixNamespaceEntry)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO scan_generations(
scan_generation_id, workspace_id, source_id, generation, parent_scan_generation_id,
capture_set_id, capture_set_digest, state, full_traversal, summary_json,
started_at_ns, finished_at_ns)
VALUES (?, ?, ?, ?, NULL, ?, ?, 'RUNNING', 0, '{}', ?, NULL)`,
			scan.scanID, workspaceID, sourceID, generation, scan.bindingID,
			"sha256:legacy-capture", testEpoch.Add(time.Duration(generation)*time.Second).UnixNano()); err != nil {
			t.Fatalf("seed scan %d: %v", generation, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO capture_root_bindings(
binding_id, workspace_id, source_id, scan_generation_id, capture_mode, profile,
display_path, device_id, inode, consistency_claim, identity_digest,
bound_at_ns, record_json)
VALUES (?, ?, ?, ?, 'ROOTED_FD', 'test', '/legacy', 1, ?, 'TEST', ?, ?, '{}')`,
			scan.bindingID, workspaceID, sourceID, scan.scanID, generation,
			"sha256:legacy-binding", testEpoch.UnixNano()); err != nil {
			t.Fatalf("seed binding %d: %v", generation, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO namespace_roots(
namespace_root_id, workspace_id, source_id, scan_generation_id, snapshot_ref,
name, root_path_key, filesystem_semantics, authority_digest, metadata_json,
created_at_ns)
VALUES (?, ?, ?, ?, ?, 'legacy', ?, 'posix', ?, '{}', ?)`,
			scan.rootID, workspaceID, sourceID, scan.scanID,
			fmt.Sprintf("snapshot:legacy:%d", generation), []byte(fmt.Sprintf("root-%d", generation)),
			fmt.Sprintf("sha256:legacy-root-%d", generation), testEpoch.UnixNano()); err != nil {
			t.Fatalf("seed root %d: %v", generation, err)
		}
		if includeEntry {
			if _, err := db.ExecContext(ctx, `INSERT INTO namespace_entries(
namespace_entry_id, workspace_id, namespace_root_id, parent_entry_id,
observation_id, raw_name, display_name, full_path_key, entry_type,
metadata_json, content_id, file_version_id, symlink_target_raw,
symlink_target_display, hardlink_group_id, logical_size, allocated_size,
created_at_ns)
VALUES (?, ?, ?, NULL, NULL, CAST('item.txt' AS BLOB), 'item.txt', CAST('item.txt' AS BLOB), 'DIRECTORY',
'{}', '', NULL, NULL, '', '', NULL, NULL, ?)`,
				scan.entryID, workspaceID, scan.rootID, testEpoch.UnixNano()); err != nil {
				t.Fatalf("seed entry %d: %v", generation, err)
			}
		}
		finished := testEpoch.Add(time.Duration(generation) * time.Minute).UnixNano()
		if _, err := db.ExecContext(ctx, `UPDATE scan_generations SET state='COMPLETE',
full_traversal=1, summary_json='{}', finished_at_ns=? WHERE scan_generation_id=?`, finished, scan.scanID); err != nil {
			t.Fatalf("finish scan %d: %v", generation, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO publications(
publication_id, workspace_id, snapshot_ref, scan_generation_id, binding_id,
namespace_root_id, manifest_digest, committed_at_ns, metadata_json, plan_digest)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, '{}', '')`, testID(t, IDPrefixPublication), workspaceID,
			fmt.Sprintf("snapshot:legacy:%d", generation), scan.scanID, scan.bindingID,
			scan.rootID, fmt.Sprintf("sha256:legacy-manifest-%d", generation), finished); err != nil {
			t.Fatalf("publish scan %d: %v", generation, err)
		}
		return scan
	}
	observations = append(observations, seedPublication(1, true))
	observations = append(observations, seedPublication(2, true))
	observations = append(observations, seedPublication(3, false))
	observations = append(observations, seedPublication(4, true))
	legacyNoteID := testID(t, IDPrefixAnnotation)
	if _, err := db.ExecContext(ctx, `INSERT INTO annotations(
annotation_id, workspace_id, subject_ref, kind, body, body_digest, revision,
predecessor_revision, tombstoned, created_at_ns, updated_at_ns)
VALUES (?, ?, ?, 'NOTE', 'legacy note', 'sha256:legacy-note', 1, 0, 0, ?, ?)`,
		legacyNoteID, workspaceID, observations[0].entryID, testEpoch.UnixNano(), testEpoch.UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, path, Options{Now: func() time.Time { return testEpoch }})
	if err != nil {
		t.Fatalf("open and migrate v21 catalog: %v", err)
	}
	defer store.Close()
	first, err := store.GetNamespaceEntry(ctx, workspaceID, observations[0].entryID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.GetNamespaceEntry(ctx, workspaceID, observations[1].entryID)
	if err != nil {
		t.Fatal(err)
	}
	gap, err := store.GetNamespaceEntry(ctx, workspaceID, observations[3].entryID)
	if err != nil {
		t.Fatal(err)
	}
	if first.SubjectRef != first.ID || second.SubjectRef != second.ID {
		t.Fatalf("legacy subject refs were rewritten: first=%q second=%q", first.SubjectRef, second.SubjectRef)
	}
	if gap.SubjectRef != gap.ID || gap.SubjectRef == first.SubjectRef {
		t.Fatalf("legacy gap/reappearance changed subject: %+v", gap)
	}
	latest, err := store.LookupLatestNamespaceEntryBySubjectRef(ctx, workspaceID, first.ID)
	if err != nil {
		t.Fatalf("legacy ID alias latest lookup: %v", err)
	}
	if latest.ID != first.ID {
		t.Fatalf("legacy subject lookup resolved to %q, want historical row %q", latest.ID, first.ID)
	}
	var refreshSubject string
	if err := store.Update(ctx, func(tx *Tx) error {
		var err error
		refreshSubject, err = tx.ResolveNamespaceSubjectRef(ctx, workspaceID, sourceID, []byte("item.txt"), EntryDirectory)
		return err
	}); err != nil {
		t.Fatalf("resolve post-upgrade refresh subject: %v", err)
	}
	if refreshSubject != gap.ID {
		t.Fatalf("post-upgrade refresh subject = %q, want latest committed row %q", refreshSubject, gap.ID)
	}
	var noteSubject string
	if err := store.db.QueryRowContext(ctx,
		`SELECT subject_ref FROM annotations WHERE workspace_id=? AND annotation_id=?`, workspaceID, legacyNoteID).Scan(&noteSubject); err != nil {
		t.Fatal(err)
	}
	if noteSubject != first.ID {
		t.Fatalf("legacy annotation subject was rewritten: %q", noteSubject)
	}
}
