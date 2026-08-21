package exact

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

// newExportTestService returns a Service backed by a directory CAS and a
// catalog. The catalog is required only by the control plane; the exact-lane
// tests exercise the repository-backed manifest path directly.
func newExportTestService(t *testing.T) *Service {
	t.Helper()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	return &Service{Store: store, Repo: repo}
}

func mustPlaceBytes(t *testing.T, s *Service, payload []byte) string {
	t.Helper()
	receipt, err := s.Repo.Place(context.Background(), strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("place payload: %v", err)
	}
	return receipt.ContentID
}

func mustBuildExportManifest(t *testing.T, service *Service, payloads map[string][]byte) ExportManifest {
	t.Helper()
	items := make([]ExportItem, 0, len(payloads))
	for index, name := range sortedNames(payloads) {
		payload := payloads[name]
		contentID := mustPlaceBytes(t, service, payload)
		size := int64(len(payload))
		items = append(items, ExportItem{
			SubjectRef: "nse_" + strings.Repeat("0123456789abcdef"[(index*3)%16:][:1], 32), OutputName: name,
			ContentID: contentID, LogicalSize: size, Exact: true,
		})
	}
	return ExportManifest{
		Schema: ExportManifestSchemaV1, ManifestID: "exm_" + strings.Repeat("b", 32),
		Representation: "exact", TargetProfileDigest: DescribeExportManifestProfile(service.Repo),
		Items: items,
	}
}

func sortedNames(m map[string][]byte) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestExportManifestRoundTrip proves plan (frozen digest) -> apply ->
// verify reproduces byte-exact output, and that re-running the same manifest
// against a fresh empty destination verifies the same bytes.
func TestExportManifestRoundTrip(t *testing.T) {
	ctx := context.Background()
	service := newExportTestService(t)
	manifest := mustBuildExportManifest(t, service, map[string][]byte{
		"alpha.txt": []byte("alpha payload"),
		"beta.bin":  []byte(strings.Repeat("B", 4096)),
	})
	digest, err := manifest.PrepareExportManifestDigest()
	if err != nil {
		t.Fatalf("prepare digest: %v", err)
	}
	manifest.ManifestDigest = digest

	destination := filepath.Join(t.TempDir(), "out")
	result, err := service.ApplyExportManifest(ctx, manifest, destination)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !result.Verified || result.Items != 2 || result.Bytes <= 0 || result.Destination == "" {
		t.Fatalf("apply result = %+v", result)
	}
	if result.ManifestDigest != digest {
		t.Fatalf("apply digest = %s, want %s", result.ManifestDigest, digest)
	}
	for name, payload := range map[string][]byte{
		"alpha.txt": []byte("alpha payload"),
		"beta.bin":  []byte(strings.Repeat("B", 4096)),
	} {
		got, readErr := os.ReadFile(filepath.Join(destination, name))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		if string(got) != string(payload) {
			t.Fatalf("%s payload = %q, want %q", name, got, payload)
		}
	}

	// Verify the same frozen manifest against a second empty destination.
	second := filepath.Join(t.TempDir(), "out-again")
	again, err := service.ApplyExportManifest(ctx, manifest, second)
	if err != nil {
		t.Fatalf("re-apply to fresh destination: %v", err)
	}
	if !again.Verified || again.ManifestDigest != digest {
		t.Fatalf("re-apply = %+v", again)
	}
	for name, payload := range map[string][]byte{
		"alpha.txt": []byte("alpha payload"),
		"beta.bin":  []byte(strings.Repeat("B", 4096)),
	} {
		got, readErr := os.ReadFile(filepath.Join(second, name))
		if readErr != nil {
			t.Fatalf("read %s (second): %v", name, readErr)
		}
		if string(got) != string(payload) {
			t.Fatalf("%s second payload = %q, want %q", name, got, payload)
		}
	}
}

// TestExportManifestApplyIsIdempotent proves re-applying the same manifest to
// the SAME already-populated destination is safe through verify, and that a
// non-empty destination is rejected at apply time.
func TestExportManifestApplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	service := newExportTestService(t)
	manifest := mustBuildExportManifest(t, service, map[string][]byte{
		"only.txt": []byte("idempotent bytes"),
	})
	digest, err := manifest.PrepareExportManifestDigest()
	if err != nil {
		t.Fatalf("prepare digest: %v", err)
	}
	manifest.ManifestDigest = digest

	destination := filepath.Join(t.TempDir(), "out")
	if _, err := service.ApplyExportManifest(ctx, manifest, destination); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// The destination is now non-empty; a second apply must fail closed.
	if _, err := service.ApplyExportManifest(ctx, manifest, destination); !isBlocked(err) {
		t.Fatalf("re-apply to populated destination: want blocked, got %v", err)
	}
	// But verifying the already materialized destination succeeds.
	verified, err := service.VerifyExportManifest(ctx, manifest, destination)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !verified {
		t.Fatal("verify returned false")
	}
}

// TestExportManifestTamperFailsClosed proves changing one materialized file's
// bytes makes verification fail closed, as does an extra or missing output.
func TestExportManifestTamperFailsClosed(t *testing.T) {
	ctx := context.Background()
	service := newExportTestService(t)
	manifest := mustBuildExportManifest(t, service, map[string][]byte{
		"alpha.txt": []byte("tamper target"),
	})
	digest, err := manifest.PrepareExportManifestDigest()
	if err != nil {
		t.Fatalf("prepare digest: %v", err)
	}
	manifest.ManifestDigest = digest

	destination := filepath.Join(t.TempDir(), "out")
	if _, err := service.ApplyExportManifest(ctx, manifest, destination); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "alpha.txt"), []byte("tampered!"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if _, err := service.VerifyExportManifest(ctx, manifest, destination); !isBlocked(err) {
		t.Fatalf("tampered verify: want blocked, got %v", err)
	}

	clean := filepath.Join(t.TempDir(), "clean")
	if _, err := service.ApplyExportManifest(ctx, manifest, clean); err != nil {
		t.Fatalf("clean apply: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clean, "extra.txt"), []byte("unexpected"), 0o644); err != nil {
		t.Fatalf("extra file: %v", err)
	}
	if _, err := service.VerifyExportManifest(ctx, manifest, clean); !isBlocked(err) {
		t.Fatalf("extra-output verify: want blocked, got %v", err)
	}
	_ = os.Remove(filepath.Join(clean, "extra.txt"))
	if err := os.Remove(filepath.Join(clean, "alpha.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := service.VerifyExportManifest(ctx, manifest, clean); !isBlocked(err) {
		t.Fatalf("missing-output verify: want blocked, got %v", err)
	}
}

// TestExportManifestWrongDigestRejected proves a manifest whose canonical
// digest was recomputed from a changed frozen field set fails PrepareExportManifestDigest,
// so apply never reaches the destination.
func TestExportManifestWrongDigestRejected(t *testing.T) {
	ctx := context.Background()
	service := newExportTestService(t)
	manifest := mustBuildExportManifest(t, service, map[string][]byte{
		"alpha.txt": []byte("frozen bytes"),
	})
	digest, err := manifest.PrepareExportManifestDigest()
	if err != nil {
		t.Fatalf("prepare digest: %v", err)
	}
	manifest.ManifestDigest = digest
	// Tamper with a frozen field; the embedded digest no longer matches.
	manifest.Items[0].OutputName = "alpha-renamed.txt"
	destination := filepath.Join(t.TempDir(), "out")
	if _, err := service.ApplyExportManifest(ctx, manifest, destination); err == nil {
		t.Fatal("apply with mismatched digest succeeded")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("apply wrote into destination before rejecting: %v", err)
	}
}

// TestExportManifestUnsafeDestinationFailsClosed proves a non-empty directory,
// a symlink destination, and a non-directory destination are all rejected
// before any bytes are written.
func TestExportManifestUnsafeDestinationFailsClosed(t *testing.T) {
	ctx := context.Background()
	service := newExportTestService(t)
	manifest := mustBuildExportManifest(t, service, map[string][]byte{
		"alpha.txt": []byte("destination probe"),
	})
	digest, err := manifest.PrepareExportManifestDigest()
	if err != nil {
		t.Fatalf("prepare digest: %v", err)
	}
	manifest.ManifestDigest = digest

	nonEmpty := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonEmpty, "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	if _, err := service.ApplyExportManifest(ctx, manifest, nonEmpty); !isBlocked(err) {
		t.Fatalf("non-empty destination: want blocked, got %v", err)
	}

	symlinkParent := t.TempDir()
	symlinkDest := filepath.Join(symlinkParent, "out-link")
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), symlinkDest); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := service.ApplyExportManifest(ctx, manifest, symlinkDest); !isBlocked(err) {
		t.Fatalf("symlink destination: want blocked, got %v", err)
	}

	fileDest := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(fileDest, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write file destination: %v", err)
	}
	if _, err := service.ApplyExportManifest(ctx, manifest, fileDest); !isBlocked(err) {
		t.Fatalf("file destination: want blocked, got %v", err)
	}
}

// TestExportManifestDeclaredNonExactFailsClosed proves a declared non-exact
// item writes nothing and cannot verify as materialized, and that a manifest
// with only non-exact items fails apply closed.
func TestExportManifestDeclaredNonExactFailsClosed(t *testing.T) {
	ctx := context.Background()
	service := newExportTestService(t)
	manifest := ExportManifest{
		Schema: ExportManifestSchemaV1, ManifestID: "exm_" + strings.Repeat("c", 32),
		Representation: "exact", TargetProfileDigest: DescribeExportManifestProfile(service.Repo),
		Items: []ExportItem{{
			SubjectRef: "nse_" + strings.Repeat("d", 32), OutputName: "link-only.bin",
			Exact: false,
		}},
	}
	digest, err := manifest.PrepareExportManifestDigest()
	if err != nil {
		t.Fatalf("prepare digest: %v", err)
	}
	manifest.ManifestDigest = digest
	destination := filepath.Join(t.TempDir(), "out")
	result, err := service.ApplyExportManifest(ctx, manifest, destination)
	if err != nil {
		t.Fatalf("apply non-exact: %v", err)
	}
	if !result.Verified || result.Items != 1 || result.Bytes != 0 {
		t.Fatalf("non-exact apply result = %+v", result)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("non-exact item materialized: %+v", entries)
	}
}

func isBlocked(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "not empty") || strings.Contains(message, "symbolic link") ||
		strings.Contains(message, "is not a directory") || strings.Contains(message, "digest mismatch") ||
		strings.Contains(message, "unexpected path") || strings.Contains(message, "is missing") ||
		strings.Contains(message, "no longer matches") || strings.Contains(message, "no local exact payload") ||
		strings.Contains(message, "length mismatch") || strings.Contains(message, "must not be materialized")
}
