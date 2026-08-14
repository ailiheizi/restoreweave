package fuse

import (
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
)

// EntryStat is the portable source-stat projection used by the Linux
// adapter. go-fuse types stay out of this struct.
type EntryStat struct {
	ModTime      time.Time
	UID          uint32
	GID          uint32
	HasOwnership bool
}

func statFromEntry(entry readsvc.NamespaceEntry) EntryStat {
	return EntryStat{
		ModTime:      entry.ModTime,
		UID:          entry.UID,
		GID:          entry.GID,
		HasOwnership: entry.HasOwnership,
	}
}
