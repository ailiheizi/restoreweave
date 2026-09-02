package search

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type semanticInstallerTestClient struct {
	assets map[string][]byte
	failAt int
	calls  int
}

func (c *semanticInstallerTestClient) Do(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	c.calls++
	if c.failAt > 0 && c.calls == c.failAt {
		return nil, errors.New("injected download interruption")
	}
	payload, ok := c.assets[req.URL.String()]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing"))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(payload)), Body: io.NopCloser(bytes.NewReader(payload))}, nil
}

func TestSemanticBundleInstallerInstallsAndRepeatsWithoutDownload(t *testing.T) {
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	specs, payloads := semanticInstallerTestSpecs(t, platform)
	client := &semanticInstallerTestClient{assets: payloads}
	root := filepath.Join(t.TempDir(), "models")
	first, err := installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), root, platform, client, specs)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if first.Destination != filepath.Join(root, SemanticBundleBGEProfileID, platform.OS+"-"+platform.Arch) {
		t.Fatalf("destination = %q", first.Destination)
	}
	if first.Admission.Descriptor.ModelID != "BAAI/bge-small-zh-v1.5" || first.Admission.Descriptor.ModelRevision != semanticInstallerModelRevision {
		t.Fatalf("model provenance = %+v", first.Admission.Descriptor)
	}
	for _, name := range []string{"model.onnx", "tokenizer.json", "runtime.bin", "zvec.dylib", "profile.json", "NOTICE", "sbom.json"} {
		if _, err := os.Stat(filepath.Join(first.Destination, name)); err != nil {
			t.Fatalf("installed asset %s: %v", name, err)
		}
	}
	for _, name := range []string{"profile.json", "NOTICE", "sbom.json"} {
		data, err := os.ReadFile(filepath.Join(first.Destination, name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if !strings.Contains(text, "BAAI/bge-small-zh-v1.5@"+semanticInstallerBaseRevision) || !strings.Contains(text, "Xenova/bge-small-zh-v1.5@"+semanticInstallerModelRevision) {
			t.Fatalf("%s lacks model/converter provenance: %s", name, text)
		}
	}
	zvecGo, err := os.ReadFile(filepath.Join(first.Destination, "zvec-go.txt"))
	if err != nil || !strings.Contains(string(zvecGo), semanticInstallerZvecCommit) || !strings.Contains(string(zvecGo), semanticInstallerZvecGoModuleVersion) {
		t.Fatalf("zvec-go provenance = %q, err %v", zvecGo, err)
	}
	zvecGoDigest, zvecGoSize, err := semanticFileDigest(filepath.Join(first.Destination, "zvec-go.txt"))
	if err != nil {
		t.Fatalf("zvec-go digest: %v", err)
	}
	var sbom struct {
		Schema       string `json:"schema"`
		Dependencies map[string]struct {
			Module  string `json:"module"`
			Version string `json:"version"`
			Commit  string `json:"commit"`
			License string `json:"license"`
			Asset   struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
				Size   uint64 `json:"size"`
			} `json:"asset"`
		} `json:"dependencies"`
		Assets map[string]struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Size   uint64 `json:"size"`
		} `json:"assets"`
	}
	sbomJSON, err := os.ReadFile(filepath.Join(first.Destination, "sbom.json"))
	if err != nil {
		t.Fatalf("read SBOM: %v", err)
	}
	if err := json.Unmarshal(sbomJSON, &sbom); err != nil {
		t.Fatalf("decode SBOM: %v", err)
	}
	if sbom.Schema != "restoreweave.semantic-bundle.sbom.v1" {
		t.Fatalf("SBOM schema = %q", sbom.Schema)
	}
	dependency, ok := sbom.Dependencies["zvec_go"]
	if !ok {
		t.Fatalf("SBOM zvec_go dependency is missing: %s", sbomJSON)
	}
	if dependency.Module != semanticInstallerZvecGoModule || dependency.Version != semanticInstallerZvecGoModuleVersion || dependency.Commit != semanticInstallerZvecCommit || dependency.License != "Apache-2.0" {
		t.Fatalf("SBOM zvec_go dependency = %+v", dependency)
	}
	if dependency.Asset.Path != "zvec-go.txt" || dependency.Asset.SHA256 != zvecGoDigest || dependency.Asset.Size != zvecGoSize {
		t.Fatalf("SBOM zvec_go asset = %+v, want path=%q sha256=%q size=%d", dependency.Asset, "zvec-go.txt", zvecGoDigest, zvecGoSize)
	}
	if asset := sbom.Assets["zvec_go"]; asset.Path != "zvec-go.txt" || asset.SHA256 != zvecGoDigest || asset.Size != zvecGoSize {
		t.Fatalf("SBOM zvec_go asset inventory = %+v", asset)
	}
	calls := client.calls
	second, err := installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), root, platform, client, specs)
	if err != nil {
		t.Fatalf("repeat install: %v", err)
	}
	if second.Admission.ProfileDigest != first.Admission.ProfileDigest || client.calls != calls {
		t.Fatalf("repeat was not idempotent: calls %d -> %d, digests %q/%q", calls, client.calls, first.Admission.ProfileDigest, second.Admission.ProfileDigest)
	}
}

func TestSemanticBundleInstallerFailureLeavesNoPublishedDirectory(t *testing.T) {
	platform, _ := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	specs, payloads := semanticInstallerTestSpecs(t, platform)
	cases := []struct {
		name   string
		mutate func([]semanticBundleInstallDownload, map[string][]byte)
		failAt int
	}{
		{name: "wrong digest", mutate: func(specs []semanticBundleInstallDownload, _ map[string][]byte) {
			specs[0].SHA256 = strings.Repeat("0", 64)
		}},
		{name: "interrupted", failAt: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			localSpecs := append([]semanticBundleInstallDownload(nil), specs...)
			localPayloads := map[string][]byte{}
			for key, value := range payloads {
				localPayloads[key] = value
			}
			if tc.mutate != nil {
				tc.mutate(localSpecs, localPayloads)
			}
			client := &semanticInstallerTestClient{assets: localPayloads, failAt: tc.failAt}
			root := filepath.Join(t.TempDir(), "models")
			_, err := installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), root, platform, client, localSpecs)
			if err == nil {
				t.Fatal("install unexpectedly succeeded")
			}
			destination := filepath.Join(root, SemanticBundleBGEProfileID, platform.OS+"-"+platform.Arch)
			if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("failed install published %q: %v", destination, statErr)
			}
		})
	}
}

func TestSemanticBundleInstallerReplacesMismatchedBundleAndRetainsOldSibling(t *testing.T) {
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	specs, payloads := semanticInstallerTestSpecs(t, platform)
	root := filepath.Join(t.TempDir(), "models")
	first, err := installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), root, platform, &semanticInstallerTestClient{assets: payloads}, specs)
	if err != nil {
		t.Fatalf("initial install: %v", err)
	}
	oldModel, err := os.ReadFile(filepath.Join(first.Destination, "model.onnx"))
	if err != nil {
		t.Fatal(err)
	}
	newSpecs := append([]semanticBundleInstallDownload(nil), specs...)
	newPayloads := make(map[string][]byte, len(payloads))
	for key, value := range payloads {
		newPayloads[key] = value
	}
	for i := range newSpecs {
		if newSpecs[i].Name != "model.onnx" {
			continue
		}
		newPayloads[newSpecs[i].URL] = []byte("new-model")
		newSpecs[i].SHA256 = semanticInstallerDigest(newPayloads[newSpecs[i].URL])
		newSpecs[i].Size = uint64(len(newPayloads[newSpecs[i].URL]))
	}
	second, err := installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), root, platform, &semanticInstallerTestClient{assets: newPayloads}, newSpecs)
	if err != nil {
		t.Fatalf("replacement install: %v", err)
	}
	if second.Admission.ProfileDigest == first.Admission.ProfileDigest {
		t.Fatal("replacement retained the old profile digest")
	}
	current, err := os.ReadFile(filepath.Join(second.Destination, "model.onnx"))
	if err != nil || string(current) != "new-model" {
		t.Fatalf("current model = %q, err %v", current, err)
	}
	entries, err := os.ReadDir(filepath.Dir(first.Destination))
	if err != nil {
		t.Fatal(err)
	}
	prefix := "." + filepath.Base(first.Destination) + semanticInstallerOldPrefix
	var retained int
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		retained++
		backupModel, readErr := os.ReadFile(filepath.Join(filepath.Dir(first.Destination), entry.Name(), "model.onnx"))
		if readErr != nil || !bytes.Equal(backupModel, oldModel) {
			t.Fatalf("retained old bundle %q model = %q, err %v", entry.Name(), backupModel, readErr)
		}
	}
	if retained != 1 {
		t.Fatalf("retained old sibling count = %d, want 1", retained)
	}
	thirdSpecs := append([]semanticBundleInstallDownload(nil), newSpecs...)
	thirdPayloads := make(map[string][]byte, len(newPayloads))
	for key, value := range newPayloads {
		thirdPayloads[key] = value
	}
	for i := range thirdSpecs {
		if thirdSpecs[i].Name != "model.onnx" {
			continue
		}
		thirdPayloads[thirdSpecs[i].URL] = []byte("third-model")
		thirdSpecs[i].SHA256 = semanticInstallerDigest(thirdPayloads[thirdSpecs[i].URL])
		thirdSpecs[i].Size = uint64(len(thirdPayloads[thirdSpecs[i].URL]))
	}
	third, err := installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), root, platform, &semanticInstallerTestClient{assets: thirdPayloads}, thirdSpecs)
	if err != nil {
		t.Fatalf("second replacement install: %v", err)
	}
	if third.Admission.ProfileDigest == second.Admission.ProfileDigest {
		t.Fatal("second replacement retained the previous profile digest")
	}
	// Model the process interruption after isolating the current live tree.
	// Recovery must use only the deterministic active marker, even though two
	// historical siblings now exist beside it.
	active := semanticInstallerActiveBackupPath(third.Destination)
	if err := os.Rename(third.Destination, active); err != nil {
		t.Fatal(err)
	}
	if err := recoverSemanticBundleInstall(third.Destination); err != nil {
		t.Fatalf("recover second replacement: %v", err)
	}
	recoveredModel, err := os.ReadFile(filepath.Join(third.Destination, "model.onnx"))
	if err != nil || string(recoveredModel) != "third-model" {
		t.Fatalf("recovered current model = %q, err %v", recoveredModel, err)
	}
	fourthSpecs := append([]semanticBundleInstallDownload(nil), thirdSpecs...)
	fourthPayloads := make(map[string][]byte, len(thirdPayloads))
	for key, value := range thirdPayloads {
		fourthPayloads[key] = value
	}
	for i := range fourthSpecs {
		if fourthSpecs[i].Name != "model.onnx" {
			continue
		}
		fourthPayloads[fourthSpecs[i].URL] = []byte("fourth-model")
		fourthSpecs[i].SHA256 = semanticInstallerDigest(fourthPayloads[fourthSpecs[i].URL])
		fourthSpecs[i].Size = uint64(len(fourthPayloads[fourthSpecs[i].URL]))
	}
	_, err = installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), root, platform, &semanticInstallerTestClient{assets: fourthPayloads, failAt: 3}, fourthSpecs)
	if err == nil {
		t.Fatal("interrupted replacement unexpectedly succeeded")
	}
	finalModel, readErr := os.ReadFile(filepath.Join(third.Destination, "model.onnx"))
	if readErr != nil || string(finalModel) != "third-model" {
		t.Fatalf("interrupted replacement changed live bundle: %q, err %v", finalModel, readErr)
	}
}

func TestSemanticBundleInstallerPublishFailureRestoresOldAndQuarantinesCandidate(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "darwin-arm64")
	candidate := filepath.Join(parent, "candidate")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(candidate, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "marker"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidate, "marker"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := publishSemanticBundleReplacement(candidate, destination, func() error { return errors.New("injected readback failure") })
	if err == nil {
		t.Fatal("publish unexpectedly succeeded")
	}
	marker, readErr := os.ReadFile(filepath.Join(destination, "marker"))
	if readErr != nil || string(marker) != "old" {
		t.Fatalf("old destination was not restored: %q, err %v", marker, readErr)
	}
	if _, statErr := os.Lstat(candidate); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("candidate was not quarantined/removed: %v", statErr)
	}
	foundRejected := false
	entries, readErr := os.ReadDir(parent)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), semanticInstallerRejectedPrefix) {
			foundRejected = true
		}
	}
	if !foundRejected {
		t.Fatal("failed candidate was not retained in a rejection sibling")
	}
}

func TestSemanticBundleInstallerRecoveryUsesOnlyActiveMarker(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "darwin-arm64")
	active := semanticInstallerActiveBackupPath(destination)
	oldHistory := filepath.Join(parent, ".darwin-arm64"+semanticInstallerOldPrefix+"historical")
	root, descriptor := testSemanticBundle(t)
	writeOfflineBundleManifest(t, root, descriptor)
	if err := os.Rename(root, active); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(oldHistory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldHistory, "marker"), []byte("history"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverSemanticBundleInstall(destination); err != nil {
		t.Fatalf("recover active marker: %v", err)
	}
	if _, err := LoadSemanticBundle(destination); err != nil {
		t.Fatalf("recovery did not restore the complete active bundle: %v", err)
	}
	if _, err := os.Lstat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active marker remains after recovery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(oldHistory, "marker")); err != nil {
		t.Fatalf("historical backup was altered: %v", err)
	}
}

func TestSemanticBundleInstallerRecoveryRejectsMalformedActiveWithoutMovingIt(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "darwin-arm64")
	active := semanticInstallerActiveBackupPath(destination)
	if err := os.MkdirAll(active, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, "semantic-bundle.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverSemanticBundleInstall(destination); err == nil {
		t.Fatal("malformed active backup was accepted")
	}
	if _, err := os.Lstat(active); err != nil {
		t.Fatalf("malformed active backup was moved: %v", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination changed after malformed active backup: %v", err)
	}
}

func TestSemanticBundleInstallerPinnedRecoveryRejectsGenericValidLiveTree(t *testing.T) {
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	specs, payloads := semanticInstallerTestSpecs(t, platform)
	root := filepath.Join(t.TempDir(), "models")
	installed, err := installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), root, platform, &semanticInstallerTestClient{assets: payloads}, specs)
	if err != nil {
		t.Fatalf("initial install: %v", err)
	}
	active := semanticInstallerActiveBackupPath(installed.Destination)
	if err := os.Rename(installed.Destination, active); err != nil {
		t.Fatal(err)
	}
	generic := installed.Admission.Descriptor
	generic.ModelRevision = "generic-valid-but-not-pinned"
	sources := make(map[string]string, len(generic.assets()))
	for _, entry := range generic.assets() {
		sources[entry.Name] = filepath.Join(active, filepath.FromSlash(entry.Asset.Path))
	}
	if _, err := PackageSemanticBundle(installed.Destination, generic, sources); err != nil {
		t.Fatalf("generic-valid candidate package: %v", err)
	}
	// Keep this recovery test synthetic: the real pinned validator also checks
	// host binary digests, which intentionally cannot be satisfied by the tiny
	// test archives. The test validator preserves the relevant admission rule
	// (the original revision is admitted; a self-consistent generic revision is
	// rejected) without allowing fake binaries to masquerade as pinned BGE.
	expectedRevision := installed.Admission.Descriptor.ModelRevision
	validator := func(admission SemanticBundleAdmission) error {
		if err := admission.Validate(); err != nil {
			return err
		}
		if admission.Descriptor.ModelRevision != expectedRevision {
			return fmt.Errorf("model revision %q is not the test default %q", admission.Descriptor.ModelRevision, expectedRevision)
		}
		return nil
	}
	if err := recoverSemanticBundleInstallWithValidator(installed.Destination, validator); err != nil {
		t.Fatalf("pinned recovery: %v", err)
	}
	recovered, err := LoadSemanticBundle(installed.Destination)
	if err != nil {
		t.Fatalf("load recovered bundle: %v", err)
	}
	if recovered.Descriptor.ModelRevision != expectedRevision {
		t.Fatalf("recovered model revision = %q, want test default %q", recovered.Descriptor.ModelRevision, expectedRevision)
	}
	if _, err := os.Lstat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active marker remains after pinned recovery: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(installed.Destination))
	if err != nil {
		t.Fatal(err)
	}
	foundRejected := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), semanticInstallerRejectedPrefix) {
			continue
		}
		foundRejected = true
		rejected, loadErr := LoadSemanticBundle(filepath.Join(filepath.Dir(installed.Destination), entry.Name()))
		if loadErr != nil || rejected.Descriptor.ModelRevision != "generic-valid-but-not-pinned" {
			t.Fatalf("rejected generic tree = %+v, err %v", rejected.Descriptor, loadErr)
		}
	}
	if !foundRejected {
		t.Fatal("generic-valid non-pinned tree was not quarantined")
	}
	// The active backup was consumed only after the replacement candidate had
	// been quarantined; verify that the restored tree is the admitted one.
	rejected, err := LoadSemanticBundle(installed.Destination)
	if err != nil {
		t.Fatalf("recovered synthetic bundle became unreadable: %v", err)
	}
	if rejected.Descriptor.ModelRevision != expectedRevision {
		t.Fatalf("recovered bundle revision = %q, want test default %q", rejected.Descriptor.ModelRevision, expectedRevision)
	}
}

func TestSemanticBundleInstallerRejectsModelsRootSymlinkAncestor(t *testing.T) {
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	specs, payloads := semanticInstallerTestSpecs(t, platform)
	parent := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(parent, "models-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	modelsRoot := filepath.Join(link, "models")
	if _, err := installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), modelsRoot, platform, &semanticInstallerTestClient{assets: payloads}, specs); err == nil {
		t.Fatal("models root beneath a symlink ancestor was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "models")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("installer wrote outside configured root: %v", err)
	}
}

func TestValidateDefaultSemanticBundleAdmissionRejectsSelfConsistentOtherRevision(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	admission, err := AdmitSemanticBundle(root, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDefaultSemanticBundleAdmission(admission); err == nil {
		t.Fatal("self-consistent non-pinned bundle was accepted as the default")
	}
}

func TestSemanticBundleInstallerRejectsArchiveTraversal(t *testing.T) {
	platform, _ := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	specs, payloads := semanticInstallerTestSpecs(t, platform)
	badArchive := semanticInstallerTarGz(t, map[string]semanticInstallerTarEntry{
		"../escape": {kind: tar.TypeReg, data: []byte("bad")},
	})
	for i := range specs {
		if specs[i].Name == "runtime.archive" {
			specs[i].SHA256 = semanticInstallerDigest(badArchive)
			specs[i].Size = uint64(len(badArchive))
			payloads[specs[i].URL] = badArchive
		}
	}
	client := &semanticInstallerTestClient{assets: payloads}
	root := filepath.Join(t.TempDir(), "models")
	_, err := installDefaultSemanticBundleForPlatformWithSpecs(context.Background(), root, platform, client, specs)
	if err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("error = %v, want archive traversal rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, SemanticBundleBGEProfileID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed install left profile parent: %v", statErr)
	}
}

func TestSemanticInstallerAcceptsOfficialZvecArchiveLayout(t *testing.T) {
	archive := semanticInstallerTarGz(t, map[string]semanticInstallerTarEntry{
		"./":                                 {kind: tar.TypeDir},
		"./include/":                         {kind: tar.TypeDir},
		"./darwin_arm64/":                    {kind: tar.TypeDir},
		"./include/zvec/":                    {kind: tar.TypeDir},
		"./include/zvec/c_api.h":             {kind: tar.TypeReg, data: []byte("header")},
		"./darwin_arm64/libzvec_c_api.dylib": {kind: tar.TypeReg, data: []byte("zvec")},
	})
	archivePath := filepath.Join(t.TempDir(), "zvec.tar.gz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "unpacked")
	if err := extractSemanticTarGz(archivePath, root); err != nil {
		t.Fatalf("official zvec layout was rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "darwin_arm64", "libzvec_c_api.dylib")); err != nil {
		t.Fatalf("zvec library was not extracted: %v", err)
	}
	if _, err := selectSemanticLibrary(root, "zvec_c_api"); err != nil {
		t.Fatalf("zvec library was not selectable: %v", err)
	}
}

func TestSemanticInstallerAcceptsOfficialORTArchiveAliases(t *testing.T) {
	archive := semanticInstallerTarGzSequence(t, []semanticInstallerTarNamedEntry{
		{name: "./", entry: semanticInstallerTarEntry{kind: tar.TypeDir}},
		{name: "./onnxruntime-osx-arm64-1.29.0/", entry: semanticInstallerTarEntry{kind: tar.TypeDir}},
		{name: "./onnxruntime-osx-arm64-1.29.0/lib/", entry: semanticInstallerTarEntry{kind: tar.TypeDir}},
		{name: "./onnxruntime-osx-arm64-1.29.0/lib/libonnxruntime.1.29.0.dylib", entry: semanticInstallerTarEntry{kind: tar.TypeReg, data: []byte("versioned")}},
		{name: "./onnxruntime-osx-arm64-1.29.0/lib/libonnxruntime.dylib", entry: semanticInstallerTarEntry{kind: tar.TypeReg, data: []byte("canonical")}},
		{name: "./onnxruntime-osx-arm64-1.29.0/lib/libonnxruntime.1.dylib", entry: semanticInstallerTarEntry{kind: tar.TypeSymlink, data: []byte("../../../../outside")}},
	})
	archivePath := filepath.Join(t.TempDir(), "onnxruntime.tgz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "unpacked")
	if err := extractSemanticTarGz(archivePath, root); err != nil {
		t.Fatalf("official ORT layout was rejected: %v", err)
	}
	alias := filepath.Join(root, "onnxruntime-osx-arm64-1.29.0", "lib", "libonnxruntime.1.dylib")
	if _, err := os.Lstat(alias); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive alias was materialized: %v", err)
	}
	selected, err := selectSemanticLibrary(root, "onnxruntime")
	if err != nil {
		t.Fatalf("ORT library was not selectable: %v", err)
	}
	want := filepath.Join(root, "onnxruntime-osx-arm64-1.29.0", "lib", "libonnxruntime.dylib")
	if selected != want {
		t.Fatalf("selected library = %q, want canonical regular library %q", selected, want)
	}
}

func TestSemanticInstallerSkipsArchiveLinksAndRejectsDuplicatePaths(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []semanticInstallerTarNamedEntry
		wantErr string
		alias   string
	}{
		{
			name: "symlink",
			entries: []semanticInstallerTarNamedEntry{
				{name: "./", entry: semanticInstallerTarEntry{kind: tar.TypeDir}},
				{name: "./safe", entry: semanticInstallerTarEntry{kind: tar.TypeReg, data: []byte("safe")}},
				{name: "./alias", entry: semanticInstallerTarEntry{kind: tar.TypeSymlink, data: []byte("../../outside")}},
			},
			alias: "alias",
		},
		{
			name: "hardlink",
			entries: []semanticInstallerTarNamedEntry{
				{name: "./", entry: semanticInstallerTarEntry{kind: tar.TypeDir}},
				{name: "./safe", entry: semanticInstallerTarEntry{kind: tar.TypeReg, data: []byte("safe")}},
				{name: "./alias", entry: semanticInstallerTarEntry{kind: tar.TypeLink, data: []byte("../../outside")}},
			},
			alias: "alias",
		},
		{
			name: "duplicate after prefix normalization",
			entries: []semanticInstallerTarNamedEntry{
				{name: "safe", entry: semanticInstallerTarEntry{kind: tar.TypeReg, data: []byte("one")}},
				{name: "./safe", entry: semanticInstallerTarEntry{kind: tar.TypeReg, data: []byte("two")}},
			},
			wantErr: "duplicate archive path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
			if err := os.WriteFile(archivePath, semanticInstallerTarGzSequence(t, tc.entries), 0o600); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(t.TempDir(), "unpacked")
			err := extractSemanticTarGz(archivePath, root)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("link entry was not safely ignored: %v", err)
			}
			if _, statErr := os.Lstat(filepath.Join(root, tc.alias)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("link entry was materialized: %v", statErr)
			}
		})
	}
}

func TestSemanticBundleInstallerPlatformMatrix(t *testing.T) {
	for _, tc := range []struct{ goos, goarch string }{{"darwin", "arm64"}, {"linux", "arm64"}, {"linux", "amd64"}} {
		p, err := semanticBundleInstallPlatformFor(tc.goos, tc.goarch)
		if err != nil || !strings.Contains(p.ORTURL, "1.29.0") || !strings.Contains(p.ZvecURL, "0.6.0") {
			t.Fatalf("platform %s/%s = %+v, err %v", tc.goos, tc.goarch, p, err)
		}
	}
	if _, err := semanticBundleInstallPlatformFor("windows", "amd64"); !errors.Is(err, ErrInvalidSemanticBundle) {
		t.Fatalf("unsupported platform error = %v", err)
	}
}

func TestSemanticInstallerRedirectPolicyFailsClosed(t *testing.T) {
	initial, err := http.NewRequest(http.MethodGet, "https://downloads.example.invalid/bundle", nil)
	if err != nil {
		t.Fatal(err)
	}
	downgrade, err := http.NewRequest(http.MethodGet, "http://downloads.example.invalid/bundle", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := semanticInstallerCheckRedirect(downgrade, []*http.Request{initial}); !errors.Is(err, ErrInvalidSemanticBundle) {
		t.Fatalf("HTTPS downgrade error = %v", err)
	}
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = initial
	}
	next, err := http.NewRequest(http.MethodGet, "https://cdn.example.invalid/bundle", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := semanticInstallerCheckRedirect(next, via); !errors.Is(err, ErrInvalidSemanticBundle) {
		t.Fatalf("redirect limit error = %v", err)
	}
}

func TestSemanticInstallerDownloadFailsClosedOnCancellationAndOversize(t *testing.T) {
	client := &semanticInstallerTestClient{assets: map[string][]byte{"https://test.invalid/asset": []byte("0123456789")}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := downloadSemanticInstallerAsset(ctx, client, semanticBundleInstallDownload{Name: "asset", URL: "https://test.invalid/asset", SHA256: semanticInstallerDigest([]byte("0123456789")), Max: 64}, filepath.Join(t.TempDir(), "asset"))
	if err == nil {
		t.Fatal("canceled download unexpectedly succeeded")
	}
	path := filepath.Join(t.TempDir(), "asset")
	err = downloadSemanticInstallerAsset(context.Background(), client, semanticBundleInstallDownload{Name: "asset", URL: "https://test.invalid/asset", SHA256: semanticInstallerDigest([]byte("0123456789")), Max: 4}, path)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize error = %v", err)
	}
	if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("oversize download left staging file: %v", statErr)
	}
	badDigestPath := filepath.Join(t.TempDir(), "bad-digest")
	err = downloadSemanticInstallerAsset(context.Background(), client, semanticBundleInstallDownload{Name: "asset", URL: "https://test.invalid/asset", SHA256: strings.Repeat("0", 64), Max: 64}, badDigestPath)
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("digest error = %v", err)
	}
	if _, statErr := os.Lstat(badDigestPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("digest failure left staging file: %v", statErr)
	}
}

type semanticInstallerTarEntry struct {
	kind byte
	data []byte
}

type semanticInstallerTarNamedEntry struct {
	name  string
	entry semanticInstallerTarEntry
}

func semanticInstallerTarGz(t *testing.T, entries map[string]semanticInstallerTarEntry) []byte {
	t.Helper()
	ordered := make([]semanticInstallerTarNamedEntry, 0, len(entries))
	for name, entry := range entries {
		ordered = append(ordered, semanticInstallerTarNamedEntry{name: name, entry: entry})
	}
	return semanticInstallerTarGzSequence(t, ordered)
}

func semanticInstallerTarGzSequence(t *testing.T, entries []semanticInstallerTarNamedEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tarWriter := tar.NewWriter(gz)
	for _, named := range entries {
		hdr := &tar.Header{Name: named.name, Mode: 0o600, Typeflag: named.entry.kind, Size: int64(len(named.entry.data))}
		if named.entry.kind == tar.TypeSymlink || named.entry.kind == tar.TypeLink {
			hdr.Linkname = string(named.entry.data)
			hdr.Size = 0
		}
		if err := tarWriter.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if named.entry.kind == tar.TypeReg {
			if _, err := tarWriter.Write(named.entry.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func semanticInstallerTestSpecs(t *testing.T, platform semanticBundleInstallPlatform) ([]semanticBundleInstallDownload, map[string][]byte) {
	t.Helper()
	runtimeArchive := semanticInstallerTarGz(t, map[string]semanticInstallerTarEntry{
		"libonnxruntime.dylib":                  {kind: tar.TypeReg, data: []byte("runtime")},
		"libonnxruntime_providers_shared.dylib": {kind: tar.TypeReg, data: []byte("provider")},
	})
	zvecArchive := semanticInstallerTarGz(t, map[string]semanticInstallerTarEntry{"libzvec_c_api.dylib": {kind: tar.TypeReg, data: []byte("zvec")}})
	values := map[string][]byte{"runtime": runtimeArchive, "zvec": zvecArchive, "model": []byte("model"), "tokenizer": []byte("tokenizer"), "header": []byte("header")}
	specs := []semanticBundleInstallDownload{}
	add := func(name, key string, max uint64) {
		url := "https://test.invalid/" + key
		if key == "runtime" {
			url = platform.ORTURL
		} else if key == "zvec" {
			url = platform.ZvecURL
		}
		data := values[key]
		if key == "model" {
			url = "https://test.invalid/model"
		} else if key == "tokenizer" {
			url = "https://test.invalid/tokenizer"
		} else if key == "header" {
			url = "https://test.invalid/header"
		}
		values[url] = data
		specs = append(specs, semanticBundleInstallDownload{Name: name, URL: url, SHA256: semanticInstallerDigest(data), Size: uint64(len(data)), Max: max})
	}
	add("runtime.archive", "runtime", semanticInstallerMaxDownload)
	add("zvec.archive", "zvec", semanticInstallerMaxDownload)
	add("model.onnx", "model", semanticInstallerMaxDownload)
	add("tokenizer.json", "tokenizer", semanticInstallerMaxDownload)
	add("onnx-c-api.h", "header", semanticInstallerMaxDownload)
	return specs, values
}

func TestSemanticInstallerRejectsTwoCoreLibraryVersions(t *testing.T) {
	archive := semanticInstallerTarGz(t, map[string]semanticInstallerTarEntry{
		"libonnxruntime.1.29.0.dylib": {kind: tar.TypeReg, data: []byte("one")},
		"libonnxruntime.1.29.1.dylib": {kind: tar.TypeReg, data: []byte("two")},
	})
	archivePath := filepath.Join(t.TempDir(), "runtime.tgz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "unpacked")
	if err := extractSemanticTarGz(archivePath, root); err != nil {
		t.Fatal(err)
	}
	if _, err := selectSemanticLibrary(root, "onnxruntime"); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("selection error = %v, want ambiguity rejection", err)
	}
}

func TestSemanticInstallerRedirectPolicy(t *testing.T) {
	from, _ := http.NewRequest(http.MethodGet, "https://downloads.example/asset", nil)
	to, _ := http.NewRequest(http.MethodGet, "http://downloads.example/asset", nil)
	if err := semanticInstallerCheckRedirect(to, []*http.Request{from}); err == nil {
		t.Fatal("HTTPS to HTTP redirect was accepted")
	}
	to.URL.Scheme = "https"
	if err := semanticInstallerCheckRedirect(to, []*http.Request{from}); err != nil {
		t.Fatalf("HTTPS CDN redirect rejected: %v", err)
	}
}

func semanticInstallerDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestInstallSemanticBundleFromDirectoryIsOfflineAndIdempotent(t *testing.T) {
	sourceRoot, descriptor := testSemanticBundle(t)
	writeOfflineBundleManifest(t, sourceRoot, descriptor)
	modelsRoot := filepath.Join(t.TempDir(), "models")
	first, err := installSemanticBundleFromDirectory(context.Background(), modelsRoot, sourceRoot, false)
	if err != nil {
		t.Fatalf("offline install: %v", err)
	}
	destination, err := DefaultSemanticBundleDestination(modelsRoot)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := LoadSemanticBundle(destination)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProfileDigest != installed.ProfileDigest {
		t.Fatal("installed admission does not match destination readback")
	}
	if _, err := os.Stat(filepath.Join(destination, descriptor.Model.Path)); err != nil {
		t.Fatalf("model was not installed: %v", err)
	}
	second, err := installSemanticBundleFromDirectory(context.Background(), modelsRoot, sourceRoot, false)
	if err != nil {
		t.Fatalf("repeat offline install: %v", err)
	}
	if second.ProfileDigest != first.ProfileDigest || second.Descriptor != first.Descriptor {
		t.Fatalf("repeat admission changed: first=%+v second=%+v", first, second)
	}
	if _, err := LoadSemanticBundle(sourceRoot); err != nil {
		t.Fatalf("source was changed by install: %v", err)
	}
}

func TestInstallSemanticBundleFromDirectoryAllowsOnlyEqualCompleteSource(t *testing.T) {
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		sourceRoot func(modelsRoot, destination string) string
		wantEqual  bool
	}{
		{name: "equal", sourceRoot: func(_, destination string) string { return destination }, wantEqual: true},
		{name: "source ancestor", sourceRoot: func(modelsRoot, _ string) string { return modelsRoot }, wantEqual: false},
		{name: "source descendant", sourceRoot: func(_, destination string) string { return filepath.Join(destination, "nested") }, wantEqual: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelsRoot := filepath.Join(t.TempDir(), "models")
			destination := filepath.Join(modelsRoot, SemanticBundleBGEProfileID, platform.OS+"-"+platform.Arch)
			source, descriptor := testSemanticBundle(t)
			writeOfflineBundleManifest(t, source, descriptor)
			if tc.wantEqual {
				if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(source, destination); err != nil {
					t.Fatal(err)
				}
				source = destination
			} else {
				source = tc.sourceRoot(modelsRoot, destination)
				if err := os.MkdirAll(source, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Stat(source)
			if err != nil {
				t.Fatal(err)
			}
			_, installErr := installSemanticBundleFromDirectory(context.Background(), modelsRoot, source, false)
			if tc.wantEqual {
				if installErr != nil {
					t.Fatalf("equal complete source was rejected: %v", installErr)
				}
			} else if installErr == nil {
				t.Fatal("overlapping source and destination were accepted")
			}
			after, statErr := os.Stat(source)
			if statErr != nil || before.ModTime() != after.ModTime() {
				t.Fatalf("source changed after install attempt: before=%v after=%v err=%v", before, after, statErr)
			}
		})
	}
}

func TestSemanticInstallerOverlapResolvesDarwinPathAliases(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin alias check")
	}
	overlaps, samePath := semanticInstallerPathsOverlap("/tmp/restoreweave-source", "/private/tmp/restoreweave-source/nested")
	if !overlaps || samePath {
		t.Fatalf("alias ancestor overlap = (%v, %v), want (true, false)", overlaps, samePath)
	}
	overlaps, samePath = semanticInstallerPathsOverlap("/tmp/restoreweave-source", "/private/tmp/restoreweave-source")
	if !overlaps || !samePath {
		t.Fatalf("alias equal path = (%v, %v), want (true, true)", overlaps, samePath)
	}
}

func TestSemanticInstallerPinnedReceiptRejectsSelfConsistentTamper(t *testing.T) {
	platform, err := semanticBundleInstallPlatformFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	const fillerDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	runtimeDigest, runtimeSize, zvecDigest, zvecSize := semanticInstallerBinaryPin(platform)
	if runtimeDigest == "" || zvecDigest == "" {
		t.Skipf("no extracted binary pin for %s/%s", platform.OS, platform.Arch)
	}
	asset := func(path, digest string, size uint64) SemanticBundleAsset {
		return SemanticBundleAsset{Path: path, SHA256: digest, Size: size}
	}
	d := SemanticBundleDescriptor{
		Schema: SemanticBundleSchemaV1, ProfileID: SemanticBundleBGEProfileID, PlatformOS: platform.OS, PlatformArch: platform.Arch,
		ONNXRuntimeVersion: semanticInstallerORTVersion, ONNXRuntimeBuild: platform.ORTBuild, ONNXRuntimeCAPI: semanticInstallerORTCAPI,
		ONNXGoBindingCommit: semanticInstallerBindingCommit, ONNXGoBindingDigest: semanticInstallerBindingDigest, ONNXGoBindingCAPI: semanticInstallerORTCAPI,
		ModelID: "BAAI/bge-small-zh-v1.5", ModelRevision: semanticInstallerModelRevision, ModelExport: "onnx-single-file;converter=Xenova", ONNXOpset: 11,
		ModelLicenseID: "BAAI/bge-small-zh-v1.5:MIT", TokenizerVersion: "huggingface-tokenizers", TokenizerRevision: semanticInstallerModelRevision,
		ZvecVersion: semanticInstallerZvecVersion, ZvecBuild: platform.ZvecBuild, ZvecGoVersion: semanticInstallerZvecVersion, ZvecGoCommit: semanticInstallerZvecCommit,
		LicenseExpression: SemanticBundleLicenseExpression, PreprocessingDigest: semanticInstallerPreprocessingDigest, QueryPrefix: SemanticBundleBGEQueryPrefix,
		DocumentPrefix: SemanticBundleBGEDocumentPrefix, MaxTokens: SemanticBundleBGEMaxTokens, Pooling: SemanticBundleBGEPooling, Normalization: SemanticBundleBGENormalization,
		ElementType: SemanticBundleBGEElementType, Dimension: SemanticBundleBGEDimension, VectorSchema: SemanticBundleBGEVectorSchema,
		SemanticSpace: SemanticBundleBGESemanticSpace, Distance: SemanticBundleBGEDistance, IndexConfig: "hnsw:m=16", QueryConfig: "ef=64",
		Runtime:     asset("runtime.bin", runtimeDigest, runtimeSize),
		ONNXBinding: asset("onnx-binding.txt", semanticInstallerBindingDigest, 1), ONNXCAPI: asset("onnx-c-api.h", semanticInstallerHeaderDigest, semanticInstallerHeaderSize),
		Model: asset("model.onnx", semanticInstallerModelDigest, semanticInstallerModelSize), Tokenizer: asset("tokenizer.json", semanticInstallerTokenizerDigest, semanticInstallerTokenizerSize),
		Profile: asset("profile.json", fillerDigest, 1), Zvec: asset("zvec.dylib", zvecDigest, zvecSize),
		ZvecGo: asset("zvec-go.txt", semanticInstallerZvecGoReceiptDigest, semanticInstallerZvecGoReceiptSize), License: asset("LICENSE", fillerDigest, 1),
		Notice: asset("NOTICE", fillerDigest, 1), SBOM: asset("sbom.json", fillerDigest, 1),
	}
	digests := make(map[string]string, len(d.assets()))
	for _, entry := range d.assets() {
		digests[entry.Name] = entry.Asset.SHA256
	}
	profileDigest, err := semanticBundleProfileDigest(d, digests)
	if err != nil {
		t.Fatal(err)
	}
	admission := SemanticBundleAdmission{Descriptor: d, ProfileDigest: profileDigest, AssetDigests: digests}
	if err := validateSemanticInstallerPinnedAdmission(admission, platform); err != nil {
		t.Fatalf("valid pinned admission rejected: %v", err)
	}
	// Tamper the receipt and update both descriptor and derived profile digest,
	// proving that descriptor self-consistency is not the trust decision.
	tampered := admission
	tampered.Descriptor.ZvecGo.SHA256 = strings.Repeat("b", 64)
	tampered.AssetDigests = make(map[string]string, len(digests))
	for name, digest := range digests {
		tampered.AssetDigests[name] = digest
	}
	tampered.AssetDigests["zvec_go"] = tampered.Descriptor.ZvecGo.SHA256
	tampered.ProfileDigest, err = semanticBundleProfileDigest(tampered.Descriptor, tampered.AssetDigests)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSemanticInstallerPinnedAdmission(tampered, platform); err == nil || !strings.Contains(err.Error(), "zvec-go receipt") {
		t.Fatalf("self-consistent tampered receipt error = %v", err)
	}
}

func TestInstallSemanticBundleFromDirectoryRejectsBadSourceBeforePublication(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string, descriptor SemanticBundleDescriptor)
	}{
		{name: "missing", mutate: func(t *testing.T, root string, descriptor SemanticBundleDescriptor) {
			if err := os.Remove(filepath.Join(root, descriptor.Model.Path)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "tampered", mutate: func(t *testing.T, root string, descriptor SemanticBundleDescriptor) {
			if err := os.WriteFile(filepath.Join(root, descriptor.Model.Path), []byte("tampered"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "asset symlink", mutate: func(t *testing.T, root string, descriptor SemanticBundleDescriptor) {
			model := filepath.Join(root, descriptor.Model.Path)
			outside := filepath.Join(t.TempDir(), "model.onnx")
			if err := os.WriteFile(outside, []byte("bge-model-fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(model); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, model); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, descriptor := testSemanticBundle(t)
			writeOfflineBundleManifest(t, root, descriptor)
			tc.mutate(t, root, descriptor)
			modelsRoot := filepath.Join(t.TempDir(), "models")
			if _, err := installSemanticBundleFromDirectory(context.Background(), modelsRoot, root, false); err == nil {
				t.Fatal("invalid source was accepted")
			}
			destination := filepath.Join(modelsRoot, SemanticBundleBGEProfileID, runtime.GOOS+"-"+runtime.GOARCH)
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid source published destination: %v", err)
			}
		})
	}
}

func TestInstallSemanticBundleFromDirectoryRejectsSymlinkRootAndCanceledContext(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	writeOfflineBundleManifest(t, root, descriptor)
	link := filepath.Join(t.TempDir(), "bundle")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	modelsRoot := filepath.Join(t.TempDir(), "models")
	if _, err := installSemanticBundleFromDirectory(context.Background(), modelsRoot, link, false); err == nil {
		t.Fatal("symlink source root was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := installSemanticBundleFromDirectory(ctx, modelsRoot, root, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled install error = %v", err)
	}
}

func TestInstallDefaultSemanticBundleFromDirectoryKeepsPublicPin(t *testing.T) {
	root, descriptor := testSemanticBundle(t)
	writeOfflineBundleManifest(t, root, descriptor)
	_, err := InstallDefaultSemanticBundleFromDirectory(context.Background(), filepath.Join(t.TempDir(), "models"), root)
	if err == nil {
		t.Fatal("synthetic bundle was accepted by pinned public entry point")
	}
}

func writeOfflineBundleManifest(t *testing.T, root string, descriptor SemanticBundleDescriptor) {
	t.Helper()
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, SemanticBundleManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
