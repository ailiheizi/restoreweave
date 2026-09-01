//go:build savingsreport

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

// TestSameCorpusRepositoryReadRelocateAndMigrateQualification is bounded
// candidate evidence, not release qualification. It deliberately uses one
// small deterministic manifest for both in-tree profiles and exercises only
// exact repository payloads. Signed recovery-record closure is covered by the
// exact package and is not fabricated by this savings-report harness.
func TestSameCorpusRepositoryReadRelocateAndMigrateQualification(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	corpusRoot := filepath.Join(base, "corpus")
	writeQualificationCorpus(t, corpusRoot)
	manifest, err := BuildCorpusManifest(corpusRoot)
	if err != nil {
		t.Fatal(err)
	}
	var corpusBytes int64
	for _, entry := range manifest.Entries {
		corpusBytes += entry.Bytes
	}
	if corpusBytes >= 1<<20 {
		t.Fatalf("qualification corpus is %d bytes, want less than 1 MiB", corpusBytes)
	}

	type placedProfile struct {
		root     string
		expected map[string]int64
	}
	profiles := []struct {
		name    string
		profile string
	}{
		{name: "raw", profile: repository.RepositoryProfileDirectoryCASDev},
		{name: "zstd", profile: repository.RepositoryProfileLocalZstdV1},
	}
	placed := make(map[string]placedProfile, len(profiles))
	for _, profile := range profiles {
		profile := profile
		t.Run(profile.name+"_place_clean_read_and_relocate", func(t *testing.T) {
			root := filepath.Join(base, profile.name+"-repository")
			driver, err := repository.OpenProfile(profile.profile, root)
			if err != nil {
				t.Fatal(err)
			}
			expected := placeCorpusManifest(t, ctx, driver, corpusRoot, manifest)
			if len(expected) >= len(manifest.Entries) {
				t.Fatalf("corpus did not exercise exact duplicate reuse: %d unique objects for %d paths", len(expected), len(manifest.Entries))
			}
			if err := verifyExactObjects(ctx, driver, expected); err != nil {
				t.Fatalf("writable readback: %v", err)
			}

			physicalBeforeRead, err := BuildCorpusManifest(root)
			if err != nil {
				t.Fatal(err)
			}
			reader, err := repository.OpenProfileReadOnly(profile.profile, root)
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyExactObjects(ctx, reader, expected); err != nil {
				t.Fatalf("clean read-only reader: %v", err)
			}
			if _, err := reader.PlaceExact(ctx, firstContentID(expected), bytes.NewReader(nil)); !errors.Is(err, repository.ErrReadOnly) {
				t.Fatalf("clean reader accepted mutation or returned the wrong error: %v", err)
			}
			physicalAfterRead, err := BuildCorpusManifest(root)
			if err != nil {
				t.Fatal(err)
			}
			if !manifestEqual(physicalBeforeRead, physicalAfterRead) {
				t.Fatal("clean reader changed repository bytes")
			}

			relocated := filepath.Join(base, profile.name+"-repository-relocated")
			if err := os.Rename(root, relocated); err != nil {
				t.Fatalf("relocate complete repository: %v", err)
			}
			if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("old repository path still exists after relocation: %v", err)
			}
			relocatedPhysical, err := BuildCorpusManifest(relocated)
			if err != nil {
				t.Fatal(err)
			}
			if !manifestEqual(physicalBeforeRead, relocatedPhysical) {
				t.Fatal("whole-repository relocation changed stored bytes")
			}
			relocatedReader, err := repository.OpenProfileReadOnly(profile.profile, relocated)
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyExactObjects(ctx, relocatedReader, expected); err != nil {
				t.Fatalf("relocated clean reader: %v", err)
			}
			placed[profile.name] = placedProfile{root: relocated, expected: expected}
		})
	}

	raw := placed["raw"]
	zstd := placed["zstd"]
	if !sameExpectedObjects(raw.expected, zstd.expected) {
		t.Fatal("raw and zstd profiles did not place the same manifest identities and lengths")
	}
	corpusAfter, err := BuildCorpusManifest(corpusRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !manifestEqual(manifest, corpusAfter) {
		t.Fatal("repository qualification changed the shared input corpus")
	}

	t.Run("raw_to_zstd_copy_forward_keeps_rollback_reader", func(t *testing.T) {
		sourceBefore, err := BuildCorpusManifest(raw.root)
		if err != nil {
			t.Fatal(err)
		}
		targetRoot := filepath.Join(base, "migrated-zstd-repository")
		report, err := repository.MigrateProfile(
			ctx,
			repository.RepositoryProfileDirectoryCASDev,
			raw.root,
			repository.RepositoryProfileLocalZstdV1,
			targetRoot,
		)
		if err != nil {
			t.Fatal(err)
		}
		if report.PayloadObjects != len(raw.expected) || report.PortableRecords != 0 {
			t.Fatalf("migration report objects = %d payload/%d records, want %d payload/0 records", report.PayloadObjects, report.PortableRecords, len(raw.expected))
		}
		if report.LogicalBytes != sumExpectedBytes(raw.expected) {
			t.Fatalf("migration logical bytes = %d, want %d", report.LogicalBytes, sumExpectedBytes(raw.expected))
		}

		sourceReader, err := repository.OpenProfileReadOnly(repository.RepositoryProfileDirectoryCASDev, raw.root)
		if err != nil {
			t.Fatal(err)
		}
		targetReader, err := repository.OpenProfileReadOnly(repository.RepositoryProfileLocalZstdV1, targetRoot)
		if err != nil {
			t.Fatal(err)
		}
		if sourceReader.RepositoryIdentity() != targetReader.RepositoryIdentity() {
			t.Fatalf("migration changed repository identity: source=%q target=%q", sourceReader.RepositoryIdentity(), targetReader.RepositoryIdentity())
		}
		if err := verifyExactObjects(ctx, sourceReader, raw.expected); err != nil {
			t.Fatalf("source rollback reader after migration: %v", err)
		}
		if err := verifyExactObjects(ctx, targetReader, raw.expected); err != nil {
			t.Fatalf("target clean reader after migration: %v", err)
		}

		tamperedID := firstNonEmptyContentID(raw.expected)
		tamperRepositoryPayload(t, targetRoot, tamperedID)
		if err := targetReader.Verify(ctx, tamperedID); err == nil {
			t.Fatal("tampered migration target reported healthy")
		}
		if err := verifyExactObjects(ctx, sourceReader, raw.expected); err != nil {
			t.Fatalf("target corruption affected source rollback authority: %v", err)
		}
		sourceAfter, err := BuildCorpusManifest(raw.root)
		if err != nil {
			t.Fatal(err)
		}
		if !manifestEqual(sourceBefore, sourceAfter) {
			t.Fatal("copy-forward migration or target tamper changed source repository bytes")
		}
	})
}

func writeQualificationCorpus(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "中文"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "duplicates"), 0o755); err != nil {
		t.Fatal(err)
	}

	duplicate := bytes.Repeat([]byte("whole-file duplicate payload\n"), 2048)
	nearDuplicate := append([]byte(nil), duplicate...)
	nearDuplicate[len(nearDuplicate)/2] ^= 0x01
	pattern := bytes.Repeat([]byte{0x00, 0xff, 0x11, 0x22, 0x11, 0x22, 0xff, 0x00}, 8192)
	random := make([]byte, 64<<10)
	state := uint32(0x5eed1234)
	for i := range random {
		state = 1664525*state + 1013904223
		random[i] = byte(state >> 24)
	}
	var jsonFixture strings.Builder
	jsonFixture.WriteString("[\n")
	for i := 0; i < 256; i++ {
		suffix := ","
		if i == 255 {
			suffix = ""
		}
		fmt.Fprintf(&jsonFixture, "  {\"id\":%d,\"kind\":\"fixture\",\"tag\":\"group-%02d\",\"active\":true}%s\n", i, i%16, suffix)
	}
	jsonFixture.WriteString("]\n")

	files := map[string][]byte{
		"README.txt":                bytes.Repeat([]byte("RestoreWeave exact storage qualification fixture.\n"), 512),
		"中文/说明.txt":                 bytes.Repeat([]byte("这是用于验证无损读回、搬迁与去重的确定性中文内容。\n"), 512),
		"records.json":              []byte(jsonFixture.String()),
		"duplicates/a-original.bin": duplicate,
		"duplicates/b-copy.bin":     append([]byte(nil), duplicate...),
		"duplicates/c-near.bin":     nearDuplicate,
		"zero.bin":                  make([]byte, 64<<10),
		"pattern.bin":               pattern,
		"fixed-lcg-random.bin":      random,
		"empty.bin":                 {},
	}
	for relative, payload := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatalf("write corpus member %q: %v", relative, err)
		}
	}
}

func placeCorpusManifest(t *testing.T, ctx context.Context, driver repository.Driver, corpusRoot string, manifest CorpusManifest) map[string]int64 {
	t.Helper()
	expected := make(map[string]int64)
	for _, entry := range manifest.Entries {
		contentID := repository.AlgorithmSHA256 + ":" + entry.SHA256
		body, err := openCorpusFile(corpusRoot, filepath.Join(corpusRoot, filepath.FromSlash(entry.Path)))
		if err != nil {
			t.Fatalf("open corpus member %q: %v", entry.Path, err)
		}
		receipt, placeErr := driver.PlaceExact(ctx, contentID, body)
		closeErr := body.Close()
		if placeErr != nil {
			t.Fatalf("place corpus member %q: %v", entry.Path, placeErr)
		}
		if closeErr != nil {
			t.Fatalf("close corpus member %q: %v", entry.Path, closeErr)
		}
		priorLength, seen := expected[contentID]
		if seen && priorLength != entry.Bytes {
			t.Fatalf("same content id has conflicting lengths: %s", contentID)
		}
		if receipt.ContentID != contentID || receipt.Bytes != entry.Bytes || receipt.Existed != seen {
			t.Fatalf("placement receipt for %q = %+v, want content=%s bytes=%d existed=%t", entry.Path, receipt, contentID, entry.Bytes, seen)
		}
		expected[contentID] = entry.Bytes
	}
	return expected
}

func verifyExactObjects(ctx context.Context, driver repository.Driver, expected map[string]int64) error {
	for _, contentID := range sortedContentIDs(expected) {
		if err := driver.Verify(ctx, contentID); err != nil {
			return fmt.Errorf("verify %s: %w", contentID, err)
		}
		body, err := driver.Open(ctx, contentID)
		if err != nil {
			return fmt.Errorf("open %s: %w", contentID, err)
		}
		hash := sha256.New()
		read, readErr := io.Copy(hash, body)
		closeErr := body.Close()
		if readErr != nil {
			return fmt.Errorf("read %s: %w", contentID, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", contentID, closeErr)
		}
		gotID := fmt.Sprintf("%s:%x", repository.AlgorithmSHA256, hash.Sum(nil))
		if gotID != contentID || read != expected[contentID] {
			return fmt.Errorf("readback %s produced id=%s bytes=%d, want id=%s bytes=%d", contentID, gotID, read, contentID, expected[contentID])
		}
	}
	return nil
}

func sortedContentIDs(expected map[string]int64) []string {
	ids := make([]string, 0, len(expected))
	for contentID := range expected {
		ids = append(ids, contentID)
	}
	sort.Strings(ids)
	return ids
}

func firstContentID(expected map[string]int64) string {
	return sortedContentIDs(expected)[0]
}

func firstNonEmptyContentID(expected map[string]int64) string {
	for _, contentID := range sortedContentIDs(expected) {
		if expected[contentID] > 0 {
			return contentID
		}
	}
	panic("qualification corpus has no non-empty content")
}

func sumExpectedBytes(expected map[string]int64) int64 {
	var total int64
	for _, size := range expected {
		total += size
	}
	return total
}

func sameExpectedObjects(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for contentID, size := range a {
		otherSize, ok := b[contentID]
		if !ok || otherSize != size {
			return false
		}
	}
	return true
}

func tamperRepositoryPayload(t *testing.T, root, contentID string) {
	t.Helper()
	hexDigest := strings.TrimPrefix(contentID, repository.AlgorithmSHA256+":")
	if len(hexDigest) != 64 {
		t.Fatalf("invalid test content id %q", contentID)
	}
	path := filepath.Join(root, "blobs", repository.AlgorithmSHA256, hexDigest[:2], hexDigest)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) == 0 {
		t.Fatalf("stored payload for %s is empty", contentID)
	}
	payload[len(payload)/2] ^= 0xff
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}
