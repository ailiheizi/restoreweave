package fuse

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"
)

const rootInode uint64 = 1

// inodeMap assigns collision-resolved mount-local inodes. Hard-linked
// entries that share a group id receive the same inode; unrelated entries
// never alias.
type inodeMap struct {
	mu      sync.Mutex
	byKey   map[string]uint64
	byInode map[uint64]string
}

func newInodeMap() *inodeMap {
	return &inodeMap{
		byKey:   map[string]uint64{},
		byInode: map[uint64]string{rootInode: ""},
	}
}

func (m *inodeMap) SetRoot(entryID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := inodeKey(entryID, "")
	m.byKey[key] = rootInode
	m.byInode[rootInode] = key
}

func (m *inodeMap) Get(key string) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ino, ok := m.byKey[key]; ok {
		return ino
	}
	ino := hashInode(key)
	if ino < 2 {
		ino = 2
	}
	for {
		if existing, taken := m.byInode[ino]; !taken || existing == key {
			break
		}
		ino++
		if ino < 2 {
			ino = 2
		}
	}
	m.byKey[key] = ino
	m.byInode[ino] = key
	return ino
}

func inodeKey(entryID, hardlinkGroupID string) string {
	if hardlinkGroupID != "" {
		return "hl:" + hardlinkGroupID
	}
	return "id:" + entryID
}

func hashInode(key string) uint64 {
	sum := sha256.Sum256([]byte(key))
	ino := binary.BigEndian.Uint64(sum[:8])
	ino &^= 1 << 63
	if ino < 2 {
		return 2
	}
	return ino
}
