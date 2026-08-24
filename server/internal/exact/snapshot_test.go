package exact

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/scanner"
)

func TestManifestDigestStableAfterJSONRoundTripWithInvalidDisplayPath(t *testing.T) {
	invalidDisplayPath := string([]byte{'d', 'i', 's', 'p', 'l', 'a', 'y', '-', 0xff, '.', 'b', 'i', 'n'})
	manifest := Manifest{
		Schema:       SnapshotSchemaV1,
		SnapshotRef:  "snapshot:invalid-display-path",
		CreatedAt:    time.Date(2026, 8, 22, 0, 0, 0, 0, time.FixedZone("capture", 8*60*60)),
		ConfigDigest: "sha256:" + strings.Repeat("1", 64),
		Binding: capture.BindingRecord{
			Schema:           capture.SchemaBindingV1,
			Profile:          capture.ProfileLocalTree,
			CaptureMode:      scanner.CaptureModeRootedFD,
			DisplayPath:      "/source",
			DeviceID:         1,
			Inode:            2,
			ConsistencyClaim: capture.ClaimLiveValidated,
			BoundAt:          time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC),
		},
		Entries: []ManifestEntry{{
			RelativePath:   invalidDisplayPath,
			RawPath:        []byte{'d', 'i', 's', 'p', 'l', 'a', 'y', '-', 0xff, '.', 'b', 'i', 'n'},
			RawName:        []byte{'d', 'i', 's', 'p', 'l', 'a', 'y', '-', 0xff, '.', 'b', 'i', 'n'},
			EntryType:      "FILE",
			MetadataBefore: json.RawMessage(`{"size":11,"display":"bad"}`),
			MetadataAfter:  json.RawMessage(`{"size":11,"display":"bad"}`),
			Protection: ManifestProtection{
				RecordID: "protection:invalid-display-path",
				Mode:     "STORE_EXACT",
				Outcome:  "EXACT_PROTECTED",
			},
		}},
	}

	digestBefore, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := t.TempDir()
	written, err := writeManifest(repoRoot, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if written.ManifestDigest != digestBefore {
		t.Fatalf("written digest = %q, want %q", written.ManifestDigest, digestBefore)
	}
	loaded, err := readManifest(repoRoot, manifest.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	digestAfter, err := loaded.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if digestAfter != digestBefore || loaded.ManifestDigest != digestBefore {
		t.Fatalf("round-trip digest = %q/%q, want %q", digestAfter, loaded.ManifestDigest, digestBefore)
	}
}

func TestManifestDigestNormalizationPreservesValidUTF8Digest(t *testing.T) {
	manifest := Manifest{
		Schema:      SnapshotSchemaV1,
		SnapshotRef: "snapshot:valid-display-path",
		CreatedAt:   time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC),
		Binding: capture.BindingRecord{
			Schema:           capture.SchemaBindingV1,
			Profile:          capture.ProfileLocalTree,
			CaptureMode:      scanner.CaptureModeRootedFD,
			DisplayPath:      "/source",
			DeviceID:         1,
			Inode:            2,
			ConsistencyClaim: capture.ClaimLiveValidated,
			BoundAt:          time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC),
		},
		Entries: []ManifestEntry{{
			RelativePath: "display.bin",
			RawPath:      []byte("display.bin"),
			RawName:      []byte("display.bin"),
			EntryType:    "FILE",
			Protection: ManifestProtection{
				RecordID: "protection:valid-display-path",
				Mode:     "STORE_EXACT",
				Outcome:  "EXACT_PROTECTED",
			},
		}},
	}
	legacyPayload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	want := DigestBytes(legacyPayload)
	got, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("valid UTF-8 digest changed: got %q want legacy %q", got, want)
	}
}
