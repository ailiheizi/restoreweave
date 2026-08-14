package exact

import (
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
)

func TestDiffManifestsClassifiesPathChanges(t *testing.T) {
	size8 := int64(8)
	size16 := int64(16)
	from := Manifest{
		SnapshotRef: "snap-from",
		Entries: []ManifestEntry{
			{RelativePath: "keep.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:keep", LogicalSize: &size8},
			{RelativePath: "gone.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:gone", LogicalSize: &size8},
			{RelativePath: "moved.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:moved", LogicalSize: &size8},
			{RelativePath: "changed.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:old", LogicalSize: &size8},
			{RelativePath: "meta.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:meta", LogicalSize: &size8, Mode: 0o644},
			{RelativePath: "was-file", EntryType: "REGULAR_FILE", ContentID: "sha256:type", LogicalSize: &size8},
		},
	}
	to := Manifest{
		SnapshotRef: "snap-to",
		Entries: []ManifestEntry{
			{RelativePath: "keep.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:keep", LogicalSize: &size8},
			{RelativePath: "new.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:new", LogicalSize: &size8},
			{RelativePath: "elsewhere.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:moved", LogicalSize: &size8},
			{RelativePath: "changed.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:now", LogicalSize: &size16},
			{RelativePath: "meta.txt", EntryType: "REGULAR_FILE", ContentID: "sha256:meta", LogicalSize: &size16, Mode: 0o644},
			{RelativePath: "was-file", EntryType: "DIRECTORY"},
		},
	}
	diffed := DiffManifests(from, to)
	byKind := map[string][]command.SnapshotDiffItem{}
	for _, change := range diffed.Changes {
		byKind[change.Kind] = append(byKind[change.Kind], change)
	}
	if len(byKind[command.DiffAdded]) != 1 || byKind[command.DiffAdded][0].Path != "new.txt" {
		t.Fatalf("added = %+v", byKind[command.DiffAdded])
	}
	if len(byKind[command.DiffRemoved]) != 1 || byKind[command.DiffRemoved][0].Path != "gone.txt" {
		t.Fatalf("removed = %+v", byKind[command.DiffRemoved])
	}
	if len(byKind[command.DiffMoved]) != 1 ||
		byKind[command.DiffMoved][0].FromPath != "moved.txt" ||
		byKind[command.DiffMoved][0].ToPath != "elsewhere.txt" {
		t.Fatalf("moved = %+v", byKind[command.DiffMoved])
	}
	if len(byKind[command.DiffContentChanged]) != 1 || byKind[command.DiffContentChanged][0].Path != "changed.txt" {
		t.Fatalf("content = %+v", byKind[command.DiffContentChanged])
	}
	if len(byKind[command.DiffMetadataChanged]) != 1 || byKind[command.DiffMetadataChanged][0].Path != "meta.txt" {
		t.Fatalf("metadata = %+v", byKind[command.DiffMetadataChanged])
	}
	if len(byKind[command.DiffTypeChanged]) != 1 || byKind[command.DiffTypeChanged][0].Path != "was-file" {
		t.Fatalf("type = %+v", byKind[command.DiffTypeChanged])
	}
	if len(diffed.Changes) != 6 {
		t.Fatalf("changes = %d, want 6: %+v", len(diffed.Changes), diffed.Changes)
	}
}
