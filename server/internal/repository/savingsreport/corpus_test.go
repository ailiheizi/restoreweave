//go:build savingsreport

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

func TestBuildCorpusManifestIsDeterministicAndUnicodeSafe(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "资料"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "资料", "重复.bin"), bytes.Repeat([]byte("same"), 3), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "copy.bin"), bytes.Repeat([]byte("same"), 3), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := BuildCorpusManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildCorpusManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if !manifestEqual(a, b) || a.Digest == "" || len(a.Entries) != 2 {
		t.Fatalf("manifest is not stable: a=%+v b=%+v", a, b)
	}
	if a.Entries[0].Path != "copy.bin" || a.Entries[1].Path != "资料/重复.bin" {
		t.Fatalf("entries are not sorted slash paths: %+v", a.Entries)
	}
	if a.Entries[0].SHA256 != a.Entries[1].SHA256 || a.Entries[0].Bytes != a.Entries[1].Bytes {
		t.Fatalf("duplicate content was not represented independently: %+v", a.Entries)
	}
}

func TestOperatorCorpusManifestRoundTripAndExactVerification(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "b.bin"), []byte("beta"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := BuildCorpusManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "operator.json")
	if err := writeJSONFile(manifestPath, expected); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadCorpusManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !manifestEqual(expected, loaded) {
		t.Fatalf("manifest changed on read: %#v %#v", expected, loaded)
	}
	if err := VerifyCorpusManifest(root, loaded); err != nil {
		t.Fatalf("valid operator manifest rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyCorpusManifest(root, loaded); err == nil {
		t.Fatal("tampered corpus accepted")
	}
}

func TestReadCorpusManifestRejectsUnknownFieldsAndBadCanonicalDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "item"), []byte("item"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildCorpusManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(t.TempDir(), "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"schema":"restoreweave.corpus-manifest.v1","entries":[],"digest":"x","extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCorpusManifest(unknown); err == nil {
		t.Fatal("manifest with unknown field accepted")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	manifest.Digest = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := writeJSONFile(bad, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCorpusManifest(bad); err == nil {
		t.Fatal("manifest with bad canonical digest accepted")
	}
}

func TestReadCorpusManifestRejectsNonCanonicalPathsAndUppercaseSHA(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "item"), []byte("item"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildCorpusManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Entries[0].Path = "./item"
	digest, err := manifestDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Digest = digest
	pathManifest := filepath.Join(t.TempDir(), "path.json")
	if err := writeJSONFile(pathManifest, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCorpusManifest(pathManifest); err == nil {
		t.Fatal("manifest with non-canonical path accepted")
	}

	manifest, err = BuildCorpusManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Entries[0].SHA256 = strings.ToUpper(manifest.Entries[0].SHA256)
	digest, err = manifestDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Digest = digest
	shaManifest := filepath.Join(t.TempDir(), "sha.json")
	if err := writeJSONFile(shaManifest, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCorpusManifest(shaManifest); err == nil {
		t.Fatal("manifest with uppercase sha accepted")
	}
}

func TestBuildCorpusManifestRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup requires platform support")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildCorpusManifest(root); err == nil {
		t.Fatal("symlink corpus member was accepted")
	}
}

func TestCorpusMutationChangesManifestAndUnsafeWorkIsRejected(t *testing.T) {
	corpus := t.TempDir()
	path := filepath.Join(corpus, "item.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := BuildCorpusManifest(corpus)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := BuildCorpusManifest(corpus)
	if err != nil {
		t.Fatal(err)
	}
	if manifestEqual(before, after) {
		t.Fatal("corpus mutation did not change manifest")
	}
	work := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "leftover"), []byte("do not overwrite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkDir(corpus, work); err == nil {
		t.Fatal("non-empty work directory was accepted")
	}
	if err := validateWorkDir(corpus, filepath.Join(corpus, "nested")); err == nil {
		t.Fatal("nested work directory was accepted")
	}
}

func TestContainmentRejectsSymlinkParentWithoutCreatingAnything(t *testing.T) {
	base := t.TempDir()
	corpus := filepath.Join(base, "corpus")
	if err := os.Mkdir(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corpus, "item"), []byte("item"), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(corpus, alias); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(alias, "work")
	if err := validateWorkDir(corpus, work); err == nil {
		t.Fatal("work below a symlinked corpus parent was accepted")
	}
	if _, err := os.Lstat(filepath.Join(corpus, "work")); !os.IsNotExist(err) {
		t.Fatalf("containment check created or found corpus child: %v", err)
	}
	output := filepath.Join(alias, "manifest.json")
	if err := validateOutputPath(corpus, output); err == nil {
		t.Fatal("output below a symlinked corpus parent was accepted")
	}
	if _, err := os.Lstat(filepath.Join(corpus, "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("output containment check created or found corpus child: %v", err)
	}
}

func TestContainmentRejectsCaseAliasWhenFilesystemHasOne(t *testing.T) {
	base := t.TempDir()
	corpus := filepath.Join(base, "Corpus")
	if err := os.Mkdir(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "corpus")
	corpusInfo, corpusErr := os.Stat(corpus)
	aliasInfo, aliasErr := os.Stat(alias)
	if corpusErr != nil || aliasErr != nil || !os.SameFile(corpusInfo, aliasInfo) {
		t.Skip("filesystem is case-sensitive")
	}
	work := filepath.Join(alias, "work")
	if err := validateWorkDir(corpus, work); err == nil {
		t.Fatal("case-alias work path was accepted")
	}
	if _, err := os.Lstat(filepath.Join(corpus, "work")); !os.IsNotExist(err) {
		t.Fatalf("case-alias containment check created or found corpus child: %v", err)
	}
}

func TestOpenCorpusFileRejectsSymlinkRedirect(t *testing.T) {
	base := t.TempDir()
	corpus := filepath.Join(base, "corpus")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(corpus, "redirect")); err != nil {
		t.Fatal(err)
	}
	if _, err := openCorpusFile(corpus, filepath.Join(corpus, "redirect", "secret")); err == nil {
		t.Fatal("symlink-redirected corpus path was opened")
	}
}

func TestChangedCorpusBytesFailClosedBeforePlacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "item")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildCorpusManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := openCorpusFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	expected := repository.AlgorithmSHA256 + ":" + manifest.Entries[0].SHA256
	if _, err := repo.PlaceExact(context.Background(), expected, body); err == nil {
		t.Fatal("changed corpus bytes were accepted under the old manifest identity")
	}
}

func TestCandidateEvidenceDeclaresCatalogUnmeasured(t *testing.T) {
	evidence := candidateEvidence(CorpusManifest{}, nil, repository.ProfileDescription{}, repository.SavingsReport{}, time.Now())
	for _, name := range evidence.Unmeasured {
		if name == "catalog" {
			return
		}
	}
	t.Fatalf("candidate evidence omitted unmeasured catalog: %+v", evidence.Unmeasured)
}

func TestWriteCorpusManifestSummaryBindsDigestAndLogicalBytes(t *testing.T) {
	manifest := CorpusManifest{
		Schema: corpusManifestSchema,
		Digest: "manifest-digest",
		Entries: []CorpusEntry{
			{Path: "a", Bytes: 7, SHA256: "a"},
			{Path: "b", Bytes: 11, SHA256: "b"},
		},
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	var output bytes.Buffer
	if err := writeCorpusManifestSummary(path, manifest, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "corpus_manifest_digest=manifest-digest\ncorpus_files=2\nlogical_bytes=18\n"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(written, []byte(`"digest": "manifest-digest"`)) {
		t.Fatalf("written manifest omitted digest: %s", written)
	}
}
