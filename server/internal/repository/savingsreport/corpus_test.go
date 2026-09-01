//go:build savingsreport

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
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
