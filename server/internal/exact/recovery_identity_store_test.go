package exact

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSigningMaterialPersistsAndExportsPublicAnchor(t *testing.T) {
	directory := t.TempDir()
	identity, anchor, err := OpenSigningMaterial(directory, testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	loaded, loadedAnchor, err := OpenSigningMaterial(directory, testPublicationDomain, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(identity.PrivateKey, loaded.PrivateKey) || anchor.PublicKeyDigest != loadedAnchor.PublicKeyDigest {
		t.Fatal("signing material changed after reload")
	}
	info, err := os.Stat(filepath.Join(directory, signingIdentityFileName))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("identity permissions = %04o", info.Mode().Perm())
	}
	destination := filepath.Join(t.TempDir(), "anchor.json")
	if _, err := ExportTrustAnchor(anchor, destination); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("private_key")) {
		t.Fatal("exported trust anchor contains private key material")
	}
	fromExport, err := LoadTrustAnchor(destination)
	if err != nil || fromExport.PublicKeyDigest != anchor.PublicKeyDigest {
		t.Fatalf("load exported anchor = %+v, %v", fromExport, err)
	}
	if _, err := ExportTrustAnchor(anchor, destination); !errors.Is(err, ErrBlocked) {
		t.Fatalf("overwrite export error = %v", err)
	}
}

func TestSigningMaterialFailsClosedOnMissingCorruptOrWrongDomain(t *testing.T) {
	directory := t.TempDir()
	if _, _, err := OpenSigningMaterial(directory, testPublicationDomain, false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing identity error = %v", err)
	}
	if _, _, err := OpenSigningMaterial(directory, testPublicationDomain, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenSigningMaterial(directory, "workspace:other", false); err == nil {
		t.Fatal("wrong publication domain unexpectedly loaded")
	}
	identityPath := filepath.Join(directory, signingIdentityFileName)
	if err := os.WriteFile(identityPath, []byte(`{"schema":"broken"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenSigningMaterial(directory, testPublicationDomain, true); err == nil {
		t.Fatal("corrupt identity was silently replaced")
	}
}

func TestSigningMaterialRejectsTrailingJSONValues(t *testing.T) {
	directory := t.TempDir()
	_, anchor, err := OpenSigningMaterial(directory, testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}

	identityPath := filepath.Join(directory, signingIdentityFileName)
	identityPayload, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identityPath, append(identityPayload, []byte(`{"trailing":true}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenSigningMaterial(directory, testPublicationDomain, false); err == nil {
		t.Fatal("signing identity with trailing JSON value was accepted")
	}

	anchorPath := filepath.Join(t.TempDir(), "anchor.json")
	if _, err := ExportTrustAnchor(anchor, anchorPath); err != nil {
		t.Fatal(err)
	}
	anchorPayload, err := os.ReadFile(anchorPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(anchorPath, append(anchorPayload, []byte(`{"trailing":true}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrustAnchor(anchorPath); err == nil {
		t.Fatal("trust anchor with trailing JSON value was accepted")
	}
}

func TestLoadTrustAnchorRejectsOversizedInput(t *testing.T) {
	anchorPath := filepath.Join(t.TempDir(), "oversized-anchor.json")
	if err := os.WriteFile(anchorPath, bytes.Repeat([]byte{'x'}, int(portableRecordReadLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrustAnchor(anchorPath); err == nil {
		t.Fatal("oversized trust anchor was accepted")
	}
}

func TestRecoveryInputsRejectSymlinkPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test requires Unix link semantics")
	}
	directory := t.TempDir()
	_, anchor, err := OpenSigningMaterial(directory, testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	anchorPath := filepath.Join(t.TempDir(), "anchor.json")
	if _, err := ExportTrustAnchor(anchor, anchorPath); err != nil {
		t.Fatal(err)
	}
	anchorLink := filepath.Join(t.TempDir(), "anchor-link.json")
	if err := os.Symlink(anchorPath, anchorLink); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrustAnchor(anchorLink); err == nil {
		t.Fatal("trust anchor symlink was followed")
	}

	fixture := newSignedPublicationFixture(t, "recovery-reference-input.txt", []byte("recovery reference input"))
	result := fixture.ingest(t, "sha256:recovery-reference-input-plan")
	reference, err := fixture.service.BuildRecoveryReference(t.Context(), result.SnapshotRef)
	if err != nil {
		t.Fatal(err)
	}
	referencePath := filepath.Join(t.TempDir(), "reference.json")
	payload, err := MarshalRecoveryReference(reference)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(referencePath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	referenceLink := filepath.Join(t.TempDir(), "reference-link.json")
	if err := os.Symlink(referencePath, referenceLink); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecoveryReference(referenceLink); err == nil {
		t.Fatal("recovery reference symlink was followed")
	}
}
