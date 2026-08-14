package exact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
)

const (
	SnapshotSchemaV1 = "org.restoreweave.snapshot.v1"
	snapshotDirName  = "snapshots"
)

// Manifest is the portable snapshot document stored beside CAS blobs.
// Recovery does not require the operational SQLite catalog.
type Manifest struct {
	Schema         string                `json:"schema"`
	SnapshotRef    string                `json:"snapshot_ref"`
	CreatedAt      time.Time             `json:"created_at"`
	Binding        capture.BindingRecord `json:"binding"`
	ManifestDigest string                `json:"manifest_digest,omitempty"`
	Entries        []ManifestEntry       `json:"entries"`
}

// ManifestEntry is one reconstructed namespace node.
type ManifestEntry struct {
	RelativePath  string `json:"relative_path"`
	RawPath       []byte `json:"raw_path"`
	EntryType     string `json:"entry_type"`
	ContentID     string `json:"content_id,omitempty"`
	LogicalSize   *int64 `json:"logical_size,omitempty"`
	Mode          uint32 `json:"mode,omitempty"`
	SymlinkTarget []byte `json:"symlink_target,omitempty"`
}

func (manifest Manifest) canonicalForDigest() ([]byte, error) {
	copy := manifest
	copy.ManifestDigest = ""
	return json.Marshal(copy)
}

func (manifest Manifest) Digest() (string, error) {
	payload, err := manifest.canonicalForDigest()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func snapshotPath(repoRoot, snapshotRef string) string {
	return filepath.Join(repoRoot, snapshotDirName, snapshotRef+".json")
}

func writeManifest(repoRoot string, manifest Manifest) (Manifest, error) {
	if err := os.MkdirAll(filepath.Join(repoRoot, snapshotDirName), 0o700); err != nil {
		return Manifest{}, err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return Manifest{}, err
	}
	manifest.ManifestDigest = digest
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	dest := snapshotPath(repoRoot, manifest.SnapshotRef)
	temp, err := os.CreateTemp(filepath.Join(repoRoot, snapshotDirName), "snap-*.json")
	if err != nil {
		return Manifest{}, err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if _, err := temp.Write(payload); err != nil {
		return Manifest{}, err
	}
	if err := temp.Sync(); err != nil {
		return Manifest{}, err
	}
	if err := temp.Close(); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(tempName, dest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func readManifest(repoRoot, snapshotRef string) (Manifest, error) {
	if strings.TrimSpace(snapshotRef) == "" {
		return Manifest{}, errors.New("snapshot ref is required")
	}
	payload, err := os.ReadFile(snapshotPath(repoRoot, snapshotRef))
	if err != nil {
		return Manifest{}, fmt.Errorf("read snapshot %s: %w", snapshotRef, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode snapshot %s: %w", snapshotRef, err)
	}
	if manifest.Schema != SnapshotSchemaV1 {
		return Manifest{}, fmt.Errorf("unsupported snapshot schema %q", manifest.Schema)
	}
	digest, err := manifest.Digest()
	if err != nil {
		return Manifest{}, err
	}
	if manifest.ManifestDigest != "" && manifest.ManifestDigest != digest {
		return Manifest{}, fmt.Errorf("snapshot %s digest mismatch", snapshotRef)
	}
	return manifest, nil
}

func listManifests(repoRoot string) ([]Manifest, error) {
	entries, err := os.ReadDir(filepath.Join(repoRoot, snapshotDirName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifests []Manifest
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		ref := strings.TrimSuffix(entry.Name(), ".json")
		manifest, err := readManifest(repoRoot, ref)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}
