//go:build savingsreport && !windows

package main

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestBuildCorpusManifestRejectsSpecialFile(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildCorpusManifest(root); err == nil {
		t.Fatal("special file was accepted")
	}
}

func TestBuildCorpusManifestRejectsEmptyCorpus(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildCorpusManifest(root); err == nil {
		t.Fatal("empty corpus was accepted")
	}
}
