package search

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	if err != nil || !strings.Contains(string(zvecGo), semanticInstallerZvecCommit) {
		t.Fatalf("zvec-go provenance = %q, err %v", zvecGo, err)
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
