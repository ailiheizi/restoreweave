package scanner

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"math"
	"os"
	"reflect"
	"time"
)

func metadataSnapshot(info fs.FileInfo) MetadataSnapshot {
	metadata := MetadataSnapshot{
		Version: MetadataVersion,
		Size:    info.Size(),
		Mode:    uint32(info.Mode()),
		ModTime: info.ModTime().UTC(),
	}

	metadata.DeviceID, metadata.IdentityKnown = nativeUint(info, "Dev")
	if inode, ok := nativeUint(info, "Ino"); ok {
		metadata.Inode = inode
	} else {
		metadata.IdentityKnown = false
	}
	metadata.LinkCount, metadata.LinkCountKnown = nativeUint(info, "Nlink")
	metadata.UID, metadata.OwnershipKnown = nativeUint(info, "Uid")
	if gid, ok := nativeUint(info, "Gid"); ok {
		metadata.GID = gid
	} else {
		metadata.OwnershipKnown = false
	}
	metadata.DeviceType, metadata.DeviceTypeKnown = nativeUint(info, "Rdev")
	metadata.Blocks, metadata.BlocksKnown = nativeUint(info, "Blocks")
	return metadata
}

func nativeUint(info fs.FileInfo, field string) (uint64, bool) {
	if info == nil || info.Sys() == nil {
		return 0, false
	}
	value := reflect.ValueOf(info.Sys())
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return 0, false
	}
	value = value.FieldByName(field)
	if !value.IsValid() {
		return 0, false
	}
	switch value.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value.Int() < 0 {
			return 0, false
		}
		return uint64(value.Int()), true
	default:
		return 0, false
	}
}

func sameSnapshot(before, after fs.FileInfo) bool {
	if before == nil || after == nil {
		return false
	}
	if !os.SameFile(before, after) {
		return false
	}

	beforeMetadata := metadataSnapshot(before)
	afterMetadata := metadataSnapshot(after)
	beforeModTime := beforeMetadata.ModTime
	afterModTime := afterMetadata.ModTime
	beforeMetadata.ModTime = time.Time{}
	afterMetadata.ModTime = time.Time{}
	return beforeModTime.Equal(afterModTime) && beforeMetadata == afterMetadata
}

func entryKind(mode fs.FileMode) EntryKind {
	switch {
	case mode.IsRegular():
		return KindRegularFile
	case mode.IsDir():
		return KindDirectory
	case mode&fs.ModeSymlink != 0:
		return KindSymlink
	case mode&fs.ModeNamedPipe != 0:
		return KindNamedPipe
	case mode&fs.ModeSocket != 0:
		return KindSocket
	case mode&fs.ModeDevice != 0 && mode&fs.ModeCharDevice != 0:
		return KindCharDevice
	case mode&fs.ModeDevice != 0:
		return KindBlockDevice
	default:
		return KindIrregular
	}
}

func hardLinkFacts(
	generationID string,
	sourceID string,
	kind EntryKind,
	metadata MetadataSnapshot,
) HardLinkFacts {
	if kind != KindRegularFile {
		return HardLinkFacts{State: HardLinkNotApplicable}
	}
	if !metadata.IdentityKnown || !metadata.LinkCountKnown {
		return HardLinkFacts{State: HardLinkUnknown}
	}
	if metadata.LinkCount <= 1 {
		return HardLinkFacts{State: HardLinkSingle, LinkCount: metadata.LinkCount}
	}

	hash := sha256.New()
	writeHashPart(hash, []byte("restoreweave:hard-link-group:v1"))
	writeHashPart(hash, []byte(generationID))
	writeHashPart(hash, []byte(sourceID))
	var native [16]byte
	binary.BigEndian.PutUint64(native[0:8], metadata.DeviceID)
	binary.BigEndian.PutUint64(native[8:16], metadata.Inode)
	writeHashPart(hash, native[:])
	return HardLinkFacts{
		State:          HardLinkMultiple,
		GroupIDVersion: HardLinkIDVersion,
		GroupID:        "sha256:" + hex.EncodeToString(hash.Sum(nil)),
		LinkCount:      metadata.LinkCount,
	}
}

func sparseFacts(kind EntryKind, metadata MetadataSnapshot) SparseFacts {
	if kind != KindRegularFile {
		return SparseFacts{State: SparseNotApplicable}
	}
	facts := SparseFacts{
		State:        SparseUnknown,
		LogicalBytes: metadata.Size,
	}
	if !metadata.BlocksKnown || metadata.Blocks > math.MaxInt64/512 {
		return facts
	}
	facts.AllocatedBytes = int64(metadata.Blocks * 512)
	facts.Evidence = "stat_blocks_512"
	if metadata.Size > 0 && facts.AllocatedBytes < metadata.Size {
		facts.State = SparseAllocationBelowSize
	} else {
		facts.State = SparseNotIndicated
	}
	return facts
}

func rootPathID(sourceID string) string {
	hash := sha256.New()
	writeHashPart(hash, []byte("restoreweave:root-path:v1"))
	writeHashPart(hash, []byte(sourceID))
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func childPathID(parentID string, rawName []byte) string {
	hash := sha256.New()
	writeHashPart(hash, []byte("restoreweave:child-path:v1"))
	writeHashPart(hash, []byte(parentID))
	writeHashPart(hash, rawName)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func writeHashPart(writer interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(value)
}
