package qualificationcorpus

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"go/parser"
	"go/token"
)

func TestGeneratePairIsDeterministicAndHeterogeneous(t *testing.T) {
	base := t.TempDir()
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	a, b, err := GeneratePair(first, second)
	if err != nil {
		t.Fatal(err)
	}
	if a.Schema != Schema || a.Seed != DefaultSeed || a.Provenance == "" || a.License != SelfGenerated || a.Digest == "" {
		t.Fatalf("manifest metadata incomplete: %+v", a)
	}
	if a.Digest != b.Digest || len(a.Entries) != len(b.Entries) {
		t.Fatalf("pair manifests differ: a=%s/%d b=%s/%d", a.Digest, len(a.Entries), b.Digest, len(b.Entries))
	}
	for i := range a.Entries {
		if a.Entries[i] != b.Entries[i] {
			t.Fatalf("pair entry %d differs: a=%+v b=%+v", i, a.Entries[i], b.Entries[i])
		}
	}

	seen := make(map[string]bool)
	duplicates, nearDuplicates := 0, 0
	for _, entry := range a.Entries {
		seen[entry.Category] = true
		if entry.DuplicateOf != "" {
			duplicates++
		}
		if entry.NearDuplicateOf != "" {
			nearDuplicates++
		}
	}
	for _, category := range []string{"text", "source", "json", "pdf", "png", "jpeg", "gif", "wav", "zip", "tar.gz", "opaque"} {
		if !seen[category] {
			t.Fatalf("corpus omitted category %q: %v", category, seen)
		}
	}
	if duplicates < 2 || nearDuplicates < 2 {
		t.Fatalf("corpus duplicate coverage = %d exact, %d near; want at least 2 each", duplicates, nearDuplicates)
	}
	assertValidFormats(t, first, a)
	assertValidFormats(t, second, b)

	rescan, err := BuildManifest(first, a.Seed)
	if err != nil {
		t.Fatal(err)
	}
	if !sameManifest(a, rescan) {
		t.Fatalf("rescan changed manifest: before=%+v after=%+v", a, rescan)
	}
	manifestPath := filepath.Join(base, "manifest.json")
	if err := WriteManifest(manifestPath, a); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Manifest
	if err := json.Unmarshal(written, &decoded); err != nil {
		t.Fatal(err)
	}
	if !sameManifest(a, decoded) {
		t.Fatalf("written manifest changed after JSON round trip: %+v", decoded)
	}
}

func TestGenerateRejectsExistingOverlappingAndSymlinkTargets(t *testing.T) {
	base := t.TempDir()
	existing := filepath.Join(base, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(existing); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing destination error = %v", err)
	}

	if _, _, err := GeneratePair(filepath.Join(base, "pair"), filepath.Join(base, "pair", "nested")); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("overlapping destinations error = %v", err)
	}

	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink destination error = %v", err)
	}

	alias := filepath.Join(base, "alias")
	if err := os.Symlink(base, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(filepath.Join(alias, "child")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink parent error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(base, "child")); !os.IsNotExist(err) {
		t.Fatalf("symlink parent check created a target: %v", err)
	}
}

func TestBuildManifestRejectsSymlinkAndEmptyCorpus(t *testing.T) {
	empty := t.TempDir()
	if _, err := BuildManifest(empty, DefaultSeed); err == nil {
		t.Fatal("empty corpus was accepted")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "valid.txt"), []byte("valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "valid.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildManifest(root, DefaultSeed); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink member error = %v", err)
	}
}

func TestGenerateForQualificationRunner(t *testing.T) {
	output := os.Getenv("RESTOREWEAVE_QUALIFICATION_CORPUS_OUT")
	if output == "" {
		t.Skip("set RESTOREWEAVE_QUALIFICATION_CORPUS_OUT to emit a temporary corpus")
	}
	manifest, err := Generate(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(output+".qualification.json", manifest); err != nil {
		t.Fatal(err)
	}
}

func assertValidFormats(t *testing.T, root string, manifest Manifest) {
	t.Helper()
	for _, entry := range manifest.Entries {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Path, err)
		}
		switch entry.Category {
		case "text":
			if !utf8.Valid(body) {
				t.Fatalf("text %s is not UTF-8", entry.Path)
			}
		case "source":
			if _, err := parser.ParseFile(token.NewFileSet(), entry.Path, body, parser.AllErrors); err != nil {
				t.Fatalf("source %s is not valid Go: %v", entry.Path, err)
			}
		case "json":
			var value any
			if err := json.Unmarshal(body, &value); err != nil {
				t.Fatalf("JSON %s is invalid: %v", entry.Path, err)
			}
		case "pdf":
			if !strings.HasPrefix(string(body), "%PDF-") || !strings.HasSuffix(strings.TrimSpace(string(body)), "%%EOF") {
				t.Fatalf("PDF %s has invalid framing", entry.Path)
			}
		case "png":
			if _, err := png.DecodeConfig(strings.NewReader(string(body))); err != nil {
				t.Fatalf("PNG %s is invalid: %v", entry.Path, err)
			}
		case "jpeg":
			if _, err := jpeg.DecodeConfig(strings.NewReader(string(body))); err != nil {
				t.Fatalf("JPEG %s is invalid: %v", entry.Path, err)
			}
		case "gif":
			if _, err := gif.DecodeConfig(strings.NewReader(string(body))); err != nil {
				t.Fatalf("GIF %s is invalid: %v", entry.Path, err)
			}
		case "wav":
			if len(body) < 44 || string(body[:4]) != "RIFF" || string(body[8:12]) != "WAVE" || string(body[12:16]) != "fmt " || binary.LittleEndian.Uint16(body[20:22]) != 1 {
				t.Fatalf("WAV %s has invalid PCM framing", entry.Path)
			}
		case "zip":
			archive, err := zip.NewReader(strings.NewReader(string(body)), int64(len(body)))
			if err != nil || len(archive.File) == 0 {
				t.Fatalf("ZIP %s is invalid: %v", entry.Path, err)
			}
		case "tar.gz":
			compressed, err := gzip.NewReader(strings.NewReader(string(body)))
			if err != nil {
				t.Fatalf("TAR.GZ %s gzip framing is invalid: %v", entry.Path, err)
			}
			archive := tar.NewReader(compressed)
			header, err := archive.Next()
			if err != nil || header.Name != "inside.txt" {
				t.Fatalf("TAR.GZ %s tar member is invalid: %v", entry.Path, err)
			}
			if _, err := io.ReadAll(archive); err != nil {
				t.Fatalf("TAR.GZ %s body is invalid: %v", entry.Path, err)
			}
			_ = compressed.Close()
		case "opaque":
			if len(body) < 1024 {
				t.Fatalf("opaque fixture %s is too small", entry.Path)
			}
		}
	}
}

func sameManifest(a, b Manifest) bool {
	if a.Schema != b.Schema || a.Seed != b.Seed || a.Provenance != b.Provenance || a.License != b.License || a.Digest != b.Digest || len(a.Entries) != len(b.Entries) {
		return false
	}
	for i := range a.Entries {
		if a.Entries[i] != b.Entries[i] {
			return false
		}
	}
	return true
}
