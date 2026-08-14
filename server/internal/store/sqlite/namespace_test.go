package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
