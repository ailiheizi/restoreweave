package releasequal

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOfflinePackageScriptSyntaxAndCandidateGates(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "scripts", "package-offline.sh")
	if output, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("bash syntax: %v\n%s", err, output)
	}
	payload, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, required := range []string{"CANDIDATE_ONLY_NOT_SUPPORTED", "semantic-bundle.tar.gz", "checksums.sha256", "SBOM.json", "semantic-bundle.sbom.json", "INCOMPLETE_NOT_RELEASE_SBOM", "LICENSE", "NOTICE", "COPYFILE_DISABLE", "gzip -n", "tags=purego", "github.com/zvec-ai/zvec-go", "zvec_expected_version", "daemon_build"} {
		if !strings.Contains(text, required) {
			t.Fatalf("script missing required candidate gate %q", required)
		}
	}
	if strings.Contains(strings.ToLower(text), "docker") {
		t.Fatal("offline package script must not invoke Docker")
	}
}

func TestOfflinePackageScriptRejectsUnsafeInputs(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "scripts", "package-offline.sh")
	for _, args := range [][]string{{"--version", "v1", "--os", "linux", "--arch", "arm64"}, {"--version", "v1", "--os", "linux", "--arch", "arm64", "--rw", "/missing", "--daemon", "/missing", "--web-dist", "/missing", "--semantic-archive", "/missing", "--output", filepath.Join(t.TempDir(), "x.tar")}} {
		if output, err := exec.Command("bash", append([]string{script}, args...)...).CombinedOutput(); err == nil {
			t.Fatalf("unsafe/missing inputs accepted: %s", output)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("the package script requires POSIX tools")
	}
}

func TestOfflinePackageScriptBuildsDeterministicVerifiedArtifact(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "scripts", "package-offline.sh")
	input := t.TempDir()
	targetOS, targetArch := runtime.GOOS, runtime.GOARCH
	rw := filepath.Join(input, "rw")
	daemon := buildDaemonBinary(t, root, input, "restoreweaved", "purego")
	if err := os.WriteFile(rw, []byte("rw-fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(input, "web")
	if err := os.Mkdir(web, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<main>fixture</main>"), 0o644); err != nil {
		t.Fatal(err)
	}
	semantic := filepath.Join(input, "semantic.tar.gz")
	if err := writeSemanticArchiveFixture(semantic); err != nil {
		t.Fatal(err)
	}
	noTagsDaemon := buildDaemonBinary(t, root, input, "restoreweaved-no-tags")
	baseArgs := []string{"--version", "v-test", "--os", targetOS, "--arch", targetArch, "--rw", rw, "--daemon", noTagsDaemon, "--web-dist", web, "--semantic-archive", semantic, "--output", filepath.Join(input, "no-tags.tar.gz")}
	if output, err := exec.Command("bash", append([]string{script}, baseArgs...)...).CombinedOutput(); err == nil {
		t.Fatalf("daemon without purego accepted: %s", output)
	}
	baseArgs[9] = daemon
	mismatchOS := "linux"
	if targetOS == mismatchOS {
		mismatchOS = "darwin"
	}
	baseArgs[3] = mismatchOS
	if output, err := exec.Command("bash", append([]string{script}, baseArgs...)...).CombinedOutput(); err == nil {
		t.Fatalf("daemon platform mismatch accepted: %s", output)
	}
	fakeGoDir := filepath.Join(input, "fake-go")
	if err := os.Mkdir(fakeGoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeGo := filepath.Join(fakeGoDir, "go")
	fakeBuildInfo := fmt.Sprintf("#!/bin/sh\ncat <<'EOF'\n/tmp/fake: go1.26.5\n\tpath\tgithub.com/ailiheizi/restoreweave/server/cmd/restoreweaved\n\tbuild\t-tags=purego\n\tbuild\tGOOS=%s\n\tbuild\tGOARCH=%s\n\tdep\tgithub.com/zvec-ai/zvec-go\tv0.6.0\th1:4wINeawyVOYz/Rj4mDJQlSAUYLkQ76QELU1dd2IEU3k=\nEOF\n", targetOS, targetArch)
	if err := os.WriteFile(fakeGo, []byte(fakeBuildInfo), 0o755); err != nil {
		t.Fatal(err)
	}
	baseArgs[3] = targetOS
	baseArgs[9] = daemon
	wrongPinCmd := exec.Command("bash", append([]string{script}, baseArgs...)...)
	wrongPinCmd.Env = append(os.Environ(), "PATH="+fakeGoDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := wrongPinCmd.CombinedOutput(); err == nil {
		t.Fatalf("daemon with wrong zvec pin accepted: %s", output)
	}
	linkedRW := filepath.Join(input, "rw-link")
	if err := os.Symlink(rw, linkedRW); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("bash", script, "--version", "v-test", "--os", targetOS, "--arch", targetArch, "--rw", linkedRW, "--daemon", daemon, "--web-dist", web, "--semantic-archive", semantic, "--output", filepath.Join(input, "symlink.tar.gz")).CombinedOutput(); err == nil {
		t.Fatalf("symlink binary accepted: %s", output)
	}
	newlineAsset := filepath.Join(web, "bad\nname.js")
	if err := os.WriteFile(newlineAsset, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("bash", script, "--version", "v-test", "--os", targetOS, "--arch", targetArch, "--rw", rw, "--daemon", daemon, "--web-dist", web, "--semantic-archive", semantic, "--output", filepath.Join(input, "newline.tar.gz")).CombinedOutput(); err == nil {
		t.Fatalf("newline web asset accepted: %s", output)
	}
	if err := os.Remove(newlineAsset); err != nil {
		t.Fatal(err)
	}
	build := func(name string) string {
		out := filepath.Join(input, name+".tar.gz")
		args := []string{script, "--version", "v-test", "--os", targetOS, "--arch", targetArch, "--rw", rw, "--daemon", daemon, "--web-dist", web, "--semantic-archive", semantic, "--output", out}
		if output, err := exec.Command("bash", args...).CombinedOutput(); err != nil {
			t.Fatalf("package %s: %v\n%s", name, err, output)
		}
		return out
	}
	first, second := build("first"), build("second")
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("same inputs did not produce the same artifact bytes")
	}

	extract := filepath.Join(input, "extract")
	if err := os.Mkdir(extract, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("tar", "-xzf", first, "-C", extract).CombinedOutput(); err != nil {
		t.Fatalf("extract: %v\n%s", err, output)
	}
	packageName := "restoreweave-v-test-" + targetOS + "-" + targetArch
	packageRoot := filepath.Join(extract, packageName)
	checkOuterTarHeaders(t, first, packageRoot, packageName)
	manifestBytes, err := os.ReadFile(filepath.Join(packageRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Schema                string `json:"schema"`
		Status                string `json:"status"`
		Version               string `json:"version"`
		SemanticArchiveSHA256 string `json:"semantic_archive_sha256"`
		Platform              struct {
			OS   string `json:"os"`
			Arch string `json:"arch"`
		} `json:"platform"`
		Layout      map[string]string `json:"layout"`
		DaemonBuild struct {
			MainPath  string `json:"main_path"`
			GoVersion string `json:"go_version"`
			GOOS      string `json:"goos"`
			GOARCH    string `json:"goarch"`
			BuildTags string `json:"build_tags"`
			ZvecGo    struct {
				Module  string `json:"module"`
				Version string `json:"version"`
				Sum     string `json:"sum"`
				Commit  string `json:"commit"`
			} `json:"zvec_go"`
		} `json:"daemon_build"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("manifest JSON: %v", err)
	}
	if manifest.Schema != "restoreweave.candidate-offline-artifact.v1" || manifest.Status != "CANDIDATE_ONLY_NOT_SUPPORTED" || manifest.Version != "v-test" || manifest.Platform.OS != targetOS || manifest.Platform.Arch != targetArch {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.DaemonBuild.MainPath != "github.com/ailiheizi/restoreweave/server/cmd/restoreweaved" || manifest.DaemonBuild.GOOS != targetOS || manifest.DaemonBuild.GOARCH != targetArch || manifest.DaemonBuild.GoVersion == "" || manifest.DaemonBuild.BuildTags != "purego" || manifest.DaemonBuild.ZvecGo.Module != "github.com/zvec-ai/zvec-go" || manifest.DaemonBuild.ZvecGo.Version != "v0.6.1-0.20260721023313-9199195b29da" || manifest.DaemonBuild.ZvecGo.Sum != "h1:4wINeawyVOYz/Rj4mDJQlSAUYLkQ76QELU1dd2IEU3k=" || manifest.DaemonBuild.ZvecGo.Commit != "9199195b29da" {
		t.Fatalf("daemon build provenance = %+v", manifest.DaemonBuild)
	}
	archiveData := mustReadBytes(t, filepath.Join(packageRoot, "semantic/semantic-bundle.tar.gz"))
	archiveSum := sha256.Sum256(archiveData)
	if manifest.SemanticArchiveSHA256 != "sha256:"+hex.EncodeToString(archiveSum[:]) {
		t.Fatalf("semantic archive digest = %q", manifest.SemanticArchiveSHA256)
	}
	volume := filepath.VolumeName(input)
	if strings.Contains(string(manifestBytes), input) || (volume != "" && strings.Contains(string(manifestBytes), volume)) {
		t.Fatal("manifest leaked a local path")
	}
	checksumLines, err := os.ReadFile(filepath.Join(packageRoot, "checksums.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(checksumLines)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || len(parts[0]) != 64 || filepath.IsAbs(parts[1]) || strings.Contains(parts[1], "..") {
			t.Fatalf("unsafe checksum line %q", line)
		}
		data, err := os.ReadFile(filepath.Join(packageRoot, filepath.FromSlash(parts[1])))
		if err != nil {
			t.Fatalf("checksum target %s: %v", parts[1], err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != parts[0] {
			t.Fatalf("checksum mismatch %s", parts[1])
		}
		seen[parts[1]] = true
	}
	if seen["checksums.sha256"] || len(seen) < 9 {
		t.Fatalf("checksum coverage = %d, includes self=%t", len(seen), seen["checksums.sha256"])
	}
	fileCount := 0
	if err := filepath.Walk(packageRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() {
			rel, err := filepath.Rel(packageRoot, path)
			if err != nil {
				return err
			}
			if rel != "checksums.sha256" {
				fileCount++
				if !seen[filepath.ToSlash(rel)] {
					t.Fatalf("packaged file lacks checksum: %s", rel)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if fileCount != len(seen) {
		t.Fatalf("checksum coverage count = %d, packaged files = %d", len(seen), fileCount)
	}
	if got, _ := os.ReadFile(filepath.Join(packageRoot, "LICENSE")); string(got) != mustReadFile(t, filepath.Join(root, "LICENSE")) {
		t.Fatal("project LICENSE was not preserved verbatim")
	}
	for _, path := range []string{"NOTICE", "SBOM.json", "licenses/semantic-bundle.LICENSE", "licenses/semantic-bundle.NOTICE", "licenses/semantic-bundle.sbom.json", "bin/rw", "bin/restoreweaved", "web/dist/index.html", "semantic/semantic-bundle.tar.gz"} {
		if _, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(path))); err != nil {
			t.Fatalf("missing packaged path %s: %v", path, err)
		}
	}
	var rootSBOM, semanticSBOM struct {
		Schema string `json:"schema"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(mustReadBytes(t, filepath.Join(packageRoot, "SBOM.json")), &rootSBOM); err != nil || rootSBOM.Schema != "restoreweave.candidate-artifact-sbom.v1" || rootSBOM.Status != "INCOMPLETE_NOT_RELEASE_SBOM" {
		t.Fatalf("root SBOM = %+v, err=%v", rootSBOM, err)
	}
	if err := json.Unmarshal(mustReadBytes(t, filepath.Join(packageRoot, "licenses/semantic-bundle.sbom.json")), &semanticSBOM); err != nil || semanticSBOM.Schema != "fixture.sbom.v1" {
		t.Fatalf("semantic SBOM = %+v, err=%v", semanticSBOM, err)
	}
	if !strings.Contains(string(mustReadBytes(t, filepath.Join(packageRoot, "NOTICE"))), "Third-party semantic notice") {
		t.Fatal("NOTICE evidence was not preserved")
	}
	if !strings.Contains(string(mustReadBytes(t, filepath.Join(packageRoot, "licenses/semantic-bundle.LICENSE"))), "Third-party") {
		t.Fatal("third-party license evidence was not retained")
	}
	if strings.Contains(strings.ToLower(string(mustReadBytes(t, script))), "docker") {
		t.Fatal("package script mentions Docker")
	}
}

func buildDaemonBinary(t *testing.T, root, dir, name string, tags ...string) string {
	t.Helper()
	output := filepath.Join(dir, name)
	args := []string{"build"}
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}
	args = append(args, "-o", output, "./server/cmd/restoreweaved")
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build daemon: %v\n%s", err, out)
	}
	return output
}

func checkOuterTarHeaders(t *testing.T, archivePath, packageRoot, packageName string) {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	headers := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.ToSlash(h.Name)
		name = strings.TrimSuffix(name, "/")
		if filepath.IsAbs(name) || name != filepath.ToSlash(filepath.Clean(name)) || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") || (name != packageName && !strings.HasPrefix(name, packageName+"/")) {
			t.Fatalf("unsafe outer tar header %q", h.Name)
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			t.Fatalf("outer tar header %q has unsupported type %d", h.Name, h.Typeflag)
		}
		if headers[name] {
			t.Fatalf("duplicate outer tar header %q", name)
		}
		if h.Typeflag == tar.TypeReg {
			headers[name] = true
		}
	}
	fileCount := 0
	if err := filepath.Walk(packageRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(packageRoot), path)
		if err != nil {
			return err
		}
		fileCount++
		if !headers[filepath.ToSlash(rel)] {
			t.Fatalf("outer tar omitted extracted file %q", rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if fileCount != len(headers) {
		t.Fatalf("outer tar regular header count=%d, extracted files=%d", len(headers), fileCount)
	}
}

func writeSemanticArchiveFixture(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	gz.Header.ModTime = time.Unix(0, 0)
	tw := tar.NewWriter(gz)
	entries := map[string][]byte{"LICENSE": []byte("Third-party semantic license evidence\n"), "NOTICE": []byte("Third-party semantic notice\n"), "sbom.json": []byte(`{"schema":"fixture.sbom.v1","license":"Apache-2.0"}`)}
	for _, name := range []string{"LICENSE", "NOTICE", "sbom.json"} {
		data := entries[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: time.Unix(0, 0)}); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	return string(mustReadBytes(t, path))
}
func mustReadBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
