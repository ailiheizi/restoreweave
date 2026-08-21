package plugin

import (
	"errors"
	"strings"
	"testing"
)

func TestBuiltinIdentificationManifestPassesStructuralValidation(t *testing.T) {
	manifest := validTestManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	wantID := BuiltinCorePackageID + "/" + BuiltinIdentificationEntryID
	ids := manifest.CanonicalEntryPointIDs()
	if len(ids) != 1 || ids[0] != wantID {
		t.Fatalf("canonical IDs = %v, want [%s]", ids, wantID)
	}
}

func TestManifestRejectsCapabilityInheritanceShortcut(t *testing.T) {
	manifest := validTestManifest()
	manifest.EntryPoints[0].Capabilities = nil
	if err := manifest.Validate(); err == nil {
		t.Fatal("Validate() accepted a detector without its independent capability declaration")
	}
}

func TestManifestRejectsTransformationClassOnDetector(t *testing.T) {
	manifest := validTestManifest()
	manifest.EntryPoints[0].TransformationClass = TransformationIdentityRaw
	err := manifest.Validate()
	if err == nil || !strings.Contains(err.Error(), "non-transformer") {
		t.Fatalf("Validate() error = %v, want non-transformer class rejection", err)
	}
}

func TestMVPPluginCatalogRejectsExternalRuntime(t *testing.T) {
	manifest := validTestManifest()
	manifest.EntryPoints[0].Runtime = RuntimeDescriptor{
		Kind:           RuntimeWASM,
		Lifecycle:      RuntimeOneShot,
		AdapterID:      "restoreweave.wasi.v1",
		Protocol:       "restoreweave.rpc/v1",
		ArtifactDigest: testDigest('f'),
		Entrypoint:     "detect",
	}
	catalog, catalogErr := NewMVPPluginCatalog()
	if catalogErr != nil {
		t.Fatalf("NewMVPPluginCatalog() error = %v", catalogErr)
	}
	err := catalog.Register(manifest)
	if !errors.Is(err, ErrExternalPluginsDisabled) {
		t.Fatalf("Register() error = %v, want ErrExternalPluginsDisabled", err)
	}
}

func TestMVPPluginCatalogCopiesRegisteredManifest(t *testing.T) {
	manifest := validTestManifest()
	catalog := newPinnedTestCatalog(t, manifest)
	if err := catalog.Register(manifest); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	manifest.EntryPoints[0].Capabilities[0].Name = CapabilityPublicAnnouncement
	stored, ok := catalog.Manifest(BuiltinCorePackageID)
	if !ok {
		t.Fatal("Manifest() did not find registered package")
	}
	if stored.EntryPoints[0].Capabilities[0].Name == CapabilityPublicAnnouncement {
		t.Fatal("catalog retained caller-owned manifest memory")
	}

	stored.EntryPoints[0].Support.Formats[0] = "tampered"
	again, _ := catalog.Manifest(BuiltinCorePackageID)
	if again.EntryPoints[0].Support.Formats[0] == "tampered" {
		t.Fatal("catalog returned mutable internal manifest memory")
	}
}

func TestMVPPluginCatalogRejectsForgedBuiltinManifest(t *testing.T) {
	manifest := validTestManifest()
	catalog := newPinnedTestCatalog(t, manifest)
	manifest.EntryPoints[0].Name = "Forged detector"
	err := catalog.Register(manifest)
	if !errors.Is(err, ErrBuiltinNotPinned) {
		t.Fatalf("Register() error = %v, want ErrBuiltinNotPinned", err)
	}
}

func newPinnedTestCatalog(t *testing.T, manifest Manifest) *Catalog {
	t.Helper()
	digest, err := ManifestContentDigest(manifest)
	if err != nil {
		t.Fatalf("ManifestContentDigest() error = %v", err)
	}
	catalog, err := NewMVPPluginCatalog(BuiltinBinding{
		ManifestDigest: digest,
		PackageID:      manifest.Package.ID,
		ArtifactDigest: manifest.Package.ArtifactDigest,
	})
	if err != nil {
		t.Fatalf("NewMVPPluginCatalog() error = %v", err)
	}
	return catalog
}

func validTestManifest() Manifest {
	return Manifest{
		SchemaVersion: ManifestSchemaV1Alpha1,
		Package: PackageManifest{
			ID:                 BuiltinCorePackageID,
			Version:            "0.1.0-test",
			Compatibility:      ">=0.1.0 <0.2.0",
			Publisher:          "restoreweave-test",
			PublisherKeyID:     "test-release-key",
			PublisherSignature: testDigest('1'),
			ArtifactDigest:     testDigest('2'),
			Platforms:          []Platform{CurrentPlatform()},
			LicenseExpression:  "NOASSERTION",
			SBOMDigest:         testDigest('3'),
			TrustState:         PackageInstalledTrusted,
		},
		EntryPoints: []EntryPointManifest{BuiltinIdentificationEntryPointManifest()},
	}
}

func testDigest(character byte) Digest {
	return Digest("sha256:" + strings.Repeat(string(character), 64))
}
