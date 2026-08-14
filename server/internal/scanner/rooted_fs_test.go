package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func buildFixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "alpha"))
	mustWriteFile(t, filepath.Join(root, "alpha", "inside.bin"), []byte("inside-data"))
	mustWriteFile(t, filepath.Join(root, "beta.txt"), []byte("shared-content"))
	if err := os.Link(
		filepath.Join(root, "beta.txt"),
		filepath.Join(root, "beta-link"),
	); err != nil {
		t.Skipf("hard links are unavailable: %v", err)
	}
	if err := os.Symlink("alpha", filepath.Join(root, "alpha-link")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	return root
}

func TestScanBoundAndUnboundAreEquivalent(t *testing.T) {
	root := buildFixtureTree(t)

	unboundSink := &recordingSink{}
	unboundScanner := mustNewScanner(t, Config{Sink: unboundSink, Clock: fixedClock})
	unboundResult, err := unboundScanner.Scan(context.Background(), ScanRequest{
		GenerationID: "unbound-generation",
		SourceID:     "source-1",
		Root:         root,
	})
	if err != nil || unboundResult.State != ScanComplete {
		t.Fatalf("unbound Scan() = (%+v, %v), want complete", unboundResult, err)
	}

	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()
	boundSink := &recordingSink{}
	boundScanner := mustNewScanner(t, Config{Sink: boundSink, Clock: fixedClock, RootBinding: capture})
	boundResult, err := boundScanner.Scan(context.Background(), ScanRequest{
		GenerationID: "bound-generation",
		SourceID:     "source-1",
		Root:         root,
	})
	if err != nil || boundResult.State != ScanComplete {
		t.Fatalf("bound Scan() = (%+v, %v), want complete", boundResult, err)
	}

	if unboundResult.Entries != boundResult.Entries ||
		unboundResult.RegularFiles != boundResult.RegularFiles ||
		unboundResult.Directories != boundResult.Directories ||
		unboundResult.Symlinks != boundResult.Symlinks ||
		unboundResult.BytesHashed != boundResult.BytesHashed {
		t.Fatalf("result counts differ: unbound %+v vs bound %+v", unboundResult, boundResult)
	}
	if boundResult.CaptureMode != CaptureModeRootedFD || unboundResult.CaptureMode != CaptureModePathString {
		t.Fatalf(
			"capture modes = %q and %q, want ROOTED_FD and PATH_STRING",
			boundResult.CaptureMode,
			unboundResult.CaptureMode,
		)
	}

	unboundPaths := relativePaths(unboundSink.entries)
	boundPaths := relativePaths(boundSink.entries)
	if !reflect.DeepEqual(unboundPaths, boundPaths) {
		t.Fatalf("traversal order differs: unbound %#v vs bound %#v", unboundPaths, boundPaths)
	}
	for _, path := range unboundPaths {
		unbound := requireRecord(t, unboundSink.entries, path)
		bound := requireRecord(t, boundSink.entries, path)
		if unbound.Kind != bound.Kind {
			t.Fatalf("kind for %q differs: %q vs %q", path, unbound.Kind, bound.Kind)
		}
		if unbound.PathID != bound.PathID {
			t.Fatalf("path ID for %q differs", path)
		}
		if (unbound.Content == nil) != (bound.Content == nil) {
			t.Fatalf("content presence for %q differs", path)
		}
		if unbound.Content != nil && unbound.Content.ContentID != bound.Content.ContentID {
			t.Fatalf("content ID for %q differs", path)
		}
		if unbound.Symlink != nil && string(unbound.Symlink.RawTarget) != string(bound.Symlink.RawTarget) {
			t.Fatalf("symlink target for %q differs", path)
		}
		if bound.Boundary.Action != BoundaryInclude || !bound.Boundary.Checked {
			t.Fatalf("bound entry %q was not boundary-included: %+v", path, bound.Boundary)
		}
	}

	if got := boundSink.starts[0].CaptureMode; got != CaptureModeRootedFD {
		t.Fatalf("bound ScanStart.CaptureMode = %q, want ROOTED_FD", got)
	}
	if boundResult.Root != capture.Path() {
		t.Fatalf("bound result root = %q, want binding path %q", boundResult.Root, capture.Path())
	}
	if boundResult.BoundaryUnchecked != 0 {
		t.Fatalf("bound BoundaryUnchecked = %d, want 0", boundResult.BoundaryUnchecked)
	}
}

func TestRootedFileSystemRejectsEscapePaths(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sub"))
	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()
	fileSystem := NewRootedFileSystem(capture)

	unsafe := []string{"..", "../x", "sub/../x", "/etc", "/etc/passwd", "a\x00b", "sub/a\x00b"}
	for _, path := range unsafe {
		if _, err := fileSystem.Lstat(path); err == nil {
			t.Fatalf("Lstat(%q) succeeded, want rejection", path)
		}
		if _, err := fileSystem.Readlink(path); err == nil {
			t.Fatalf("Readlink(%q) succeeded, want rejection", path)
		}
		if _, err := fileSystem.OpenRegularNoFollow(path); err == nil {
			t.Fatalf("OpenRegularNoFollow(%q) succeeded, want rejection", path)
		}
		if _, err := fileSystem.OpenDirNoFollow(path); err == nil {
			t.Fatalf("OpenDirNoFollow(%q) succeeded, want rejection", path)
		}
	}

	outside := filepath.Join(t.TempDir(), "outside.bin")
	mustWriteFile(t, outside, []byte("x"))
	if _, err := fileSystem.Lstat(outside); err == nil {
		t.Fatalf("Lstat on a path outside the display namespace succeeded")
	}
}

func TestRootedFileSystemRejectsIntermediateSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sub"))
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "secret.bin"), []byte("outside"))
	if err := os.Symlink(outside, filepath.Join(root, "sub", "escape")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()
	fileSystem := NewRootedFileSystem(capture)

	escaped := filepath.Join(root, "sub", "escape", "secret.bin")
	if _, err := fileSystem.Lstat(escaped); err == nil {
		t.Fatalf("Lstat followed an intermediate symlink")
	}
	if _, err := fileSystem.OpenRegularNoFollow(escaped); err == nil {
		t.Fatalf("OpenRegularNoFollow followed an intermediate symlink")
	}
}

func TestRootedFileSystemFinalSymlinkIsRecordedNotFollowed(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "target.bin"), []byte("inside"))
	if err := os.Symlink("target.bin", filepath.Join(root, "link.bin")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()
	fileSystem := NewRootedFileSystem(capture)

	info, err := fileSystem.Lstat(filepath.Join(root, "link.bin"))
	if err != nil {
		t.Fatalf("Lstat(link) error = %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Lstat(link) mode = %v, want symlink", info.Mode())
	}
	target, err := fileSystem.Readlink(filepath.Join(root, "link.bin"))
	if err != nil || target != "target.bin" {
		t.Fatalf("Readlink(link) = (%q, %v), want (target.bin, nil)", target, err)
	}
	if _, err := fileSystem.OpenRegularNoFollow(filepath.Join(root, "link.bin")); err == nil {
		t.Fatalf("OpenRegularNoFollow opened a symlink")
	}
}

func TestScanRootReplacementFailsClosed(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "capture")
	mustMkdir(t, root)
	mustWriteFile(t, filepath.Join(root, "a.bin"), []byte("original"))

	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()

	if err := os.Rename(root, filepath.Join(parent, "capture-moved")); err != nil {
		t.Fatalf("rename root away: %v", err)
	}
	mustMkdir(t, root)
	mustWriteFile(t, filepath.Join(root, "a.bin"), []byte("attacker"))

	sink := &recordingSink{}
	scanner := mustNewScanner(t, Config{Sink: sink, Clock: fixedClock, RootBinding: capture})
	result, err := scanner.Scan(context.Background(), ScanRequest{
		GenerationID: "replacement-generation",
		SourceID:     "replacement-source",
		Root:         root,
	})
	if !errors.Is(err, ErrCaptureRootReplaced) {
		t.Fatalf("Scan() error = %v, want ErrCaptureRootReplaced", err)
	}
	if result.State != ScanFailed {
		t.Fatalf("result state = %q, want FAILED", result.State)
	}
	if len(sink.entries) != 0 || len(sink.starts) != 0 || len(sink.finishes) != 0 {
		t.Fatalf("replaced-root scan emitted events: %+v", sink)
	}
}

func TestScanRootedModeHonorsCallerBoundaryChecker(t *testing.T) {
	root := buildFixtureTree(t)
	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()

	checker := boundaryCheckerFunc(func(_ context.Context, candidate BoundaryCandidate) (BoundaryDecision, error) {
		if candidate.RelativePath == "alpha" {
			return BoundaryDecision{Action: BoundarySkip, Reason: "custom-skip"}, nil
		}
		return BoundaryDecision{Action: BoundaryInclude, Reason: "custom-include"}, nil
	})
	sink := &recordingSink{}
	scanner := mustNewScanner(t, Config{
		Sink:            sink,
		Clock:           fixedClock,
		RootBinding:     capture,
		BoundaryChecker: checker,
	})
	result, err := scanner.Scan(context.Background(), ScanRequest{
		GenerationID: "override-generation",
		SourceID:     "override-source",
		Root:         root,
	})
	if err != nil || result.State != ScanComplete {
		t.Fatalf("Scan() = (%+v, %v), want complete", result, err)
	}
	if recordExists(sink.entries, "alpha/inside.bin") {
		t.Fatalf("custom boundary skip was not honored")
	}
	skipped := requireRecord(t, sink.entries, "alpha")
	if skipped.Boundary.Reason != "custom-skip" {
		t.Fatalf("boundary reason = %q, want custom-skip", skipped.Boundary.Reason)
	}
}

func TestDeviceBoundaryCheckerUnit(t *testing.T) {
	checker := DeviceBoundaryChecker{}
	known := MetadataSnapshot{IdentityKnown: true, DeviceID: 7, Inode: 1}

	same, err := checker.CheckBoundary(context.Background(), BoundaryCandidate{
		RootMetadata:  known,
		EntryMetadata: MetadataSnapshot{IdentityKnown: true, DeviceID: 7, Inode: 2},
	})
	if err != nil || same.Action != BoundaryInclude || same.Reason != "device_same" {
		t.Fatalf("same-device decision = (%+v, %v)", same, err)
	}

	boundary, err := checker.CheckBoundary(context.Background(), BoundaryCandidate{
		RootMetadata:  known,
		EntryMetadata: MetadataSnapshot{IdentityKnown: true, DeviceID: 8, Inode: 3},
	})
	if err != nil || boundary.Action != BoundarySkip || boundary.Reason != "device_boundary" {
		t.Fatalf("different-device decision = (%+v, %v)", boundary, err)
	}

	unknownEntry, err := checker.CheckBoundary(context.Background(), BoundaryCandidate{
		RootMetadata:  known,
		EntryMetadata: MetadataSnapshot{IdentityKnown: false},
	})
	if err != nil || unknownEntry.Action != BoundaryInclude || unknownEntry.Reason != "device_unknown" {
		t.Fatalf("unknown-entry decision = (%+v, %v)", unknownEntry, err)
	}

	unknownRoot, err := checker.CheckBoundary(context.Background(), BoundaryCandidate{
		RootMetadata:  MetadataSnapshot{IdentityKnown: false},
		EntryMetadata: known,
	})
	if err != nil || unknownRoot.Action != BoundaryInclude || unknownRoot.Reason != "device_unknown" {
		t.Fatalf("unknown-root decision = (%+v, %v)", unknownRoot, err)
	}
}
