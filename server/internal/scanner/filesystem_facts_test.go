//go:build darwin || freebsd || linux || netbsd

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestScanCapturesXAttrsWithExplicitACLAndExtentStates(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "facts.bin")
	mustWriteFile(t, path, []byte("filesystem facts"))
	const name = "user.restoreweave.test"
	value := []byte{0, 1, 2, 255}
	if err := unix.Lsetxattr(path, name, value, 0); err != nil {
		t.Skipf("filesystem does not permit test xattrs: %v", err)
	}

	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()
	sink := &recordingSink{}
	instance := mustNewScanner(t, Config{Sink: sink, RootBinding: capture, Clock: fixedClock})
	result, err := instance.Scan(context.Background(), ScanRequest{
		GenerationID: "filesystem-facts-generation",
		SourceID:     "filesystem-facts-source",
		Root:         root,
	})
	if err != nil || result.State != ScanComplete {
		t.Fatalf("Scan() = (%+v, %v), want complete", result, err)
	}
	record := requireRecord(t, sink.entries, "facts.bin")
	if record.Filesystem.Version != FilesystemFactsVersion || record.Filesystem.CapturedAt.IsZero() {
		t.Fatalf("filesystem fact envelope = %+v", record.Filesystem)
	}
	if record.Filesystem.XAttrs.State != CaptureFactObserved {
		t.Fatalf("xattr state = %q, want OBSERVED", record.Filesystem.XAttrs.State)
	}
	if len(record.Filesystem.XAttrs.Attributes) == 0 {
		t.Fatalf("xattr capture returned no attributes: %+v", record.Filesystem.XAttrs)
	}
	found := false
	for _, attribute := range record.Filesystem.XAttrs.Attributes {
		if attribute.Name == name {
			found = true
			if string(attribute.Value) != string(value) {
				t.Fatalf("xattr %q = %v, want %v", name, attribute.Value, value)
			}
		}
	}
	if !found {
		t.Fatalf("captured xattrs do not contain %q: %+v", name, record.Filesystem.XAttrs)
	}
	if record.Sparse.ExtentMapCaptured {
		t.Fatal("scanner claimed an extent map without retaining extent records")
	}
	if record.Filesystem.ACLs.State == "" || record.Filesystem.ACLs.ReasonCode == "" && record.Filesystem.ACLs.State != CaptureFactObserved {
		t.Fatalf("ACL capture lacks an explicit state/reason: %+v", record.Filesystem.ACLs)
	}
}

type zeroFilesystemFactsProvider struct{ OSFileSystem }

func (zeroFilesystemFactsProvider) CaptureFilesystemFacts(string, EntryKind) FilesystemFacts {
	return FilesystemFacts{}
}

func TestScanNormalizesProviderZeroFilesystemFacts(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "zero.bin"), []byte("zero facts"))

	sink := &recordingSink{}
	instance := mustNewScanner(t, Config{
		FileSystem: zeroFilesystemFactsProvider{}, Sink: sink, Clock: fixedClock,
	})
	result, err := instance.Scan(context.Background(), ScanRequest{
		GenerationID: "zero-facts-generation", SourceID: "zero-facts-source", Root: root,
	})
	if err != nil || result.State != ScanComplete {
		t.Fatalf("Scan() = (%+v, %v), want complete", result, err)
	}
	record := requireRecord(t, sink.entries, "zero.bin")
	if record.Filesystem.XAttrs.State == "" || record.Filesystem.ACLs.State == "" {
		t.Fatalf("zero provider left filesystem fact state empty: %+v", record.Filesystem)
	}
	if record.Filesystem.XAttrs.ReasonCode == "" || record.Filesystem.ACLs.ReasonCode == "" {
		t.Fatalf("zero provider left degradation reason empty: %+v", record.Filesystem)
	}
}

func TestXAttrUnsupportedErrorIsExplicit(t *testing.T) {
	facts := captureXAttrs(
		func([]byte) (int, error) { return 0, unix.ENOTSUP },
		func(string, []byte) (int, error) { return 0, unix.ENOTSUP },
		parseNULXAttrNames,
	)
	if facts.State != CaptureFactUnsupported || facts.ReasonCode == "" {
		t.Fatalf("unsupported xattr result = %+v", facts)
	}
}

func TestScanSymlinkFactsDoNotInheritTargetXAttrs(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.bin")
	link := filepath.Join(root, "link.bin")
	mustWriteFile(t, target, []byte("target"))
	if err := unix.Lsetxattr(target, "user.restoreweave.target", []byte("target-only"), 0); err != nil {
		t.Skipf("filesystem does not permit test xattrs: %v", err)
	}
	if err := os.Symlink("target.bin", link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	sink := &recordingSink{}
	instance := mustNewScanner(t, Config{Sink: sink, Clock: fixedClock})
	result, err := instance.Scan(context.Background(), ScanRequest{
		GenerationID: "symlink-facts-generation", SourceID: "symlink-facts-source", Root: root,
	})
	if err != nil || result.State != ScanComplete {
		t.Fatalf("Scan() = (%+v, %v), want complete", result, err)
	}
	record := requireRecord(t, sink.entries, "link.bin")
	if record.Filesystem.XAttrs.State == "" || record.Filesystem.ACLs.State == "" {
		t.Fatalf("symlink filesystem facts lack explicit states: %+v", record.Filesystem)
	}
	for _, attribute := range record.Filesystem.XAttrs.Attributes {
		if attribute.Name == "user.restoreweave.target" {
			t.Fatalf("symlink inherited target xattr: %+v", record.Filesystem.XAttrs)
		}
	}
}

func TestRootedFilesystemFactsFailClosedOnRootReplacement(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "capture")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "entry.bin")
	mustWriteFile(t, path, []byte("original"))
	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	if err := os.Rename(root, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "entry.bin"), []byte("replacement"))

	facts := NewRootedFileSystem(capture).CaptureFilesystemFacts(path, KindRegularFile)
	if facts.XAttrs.State != CaptureFactUnobserved || facts.ACLs.State != CaptureFactUnobserved {
		t.Fatalf("replaced-root facts = %+v, want explicit UNOBSERVED", facts)
	}
	if facts.XAttrs.ReasonCode != "CAPTURE_ROOT_UNAVAILABLE" || facts.ACLs.ReasonCode != "CAPTURE_ROOT_UNAVAILABLE" {
		t.Fatalf("replaced-root reasons = xattr:%q acl:%q", facts.XAttrs.ReasonCode, facts.ACLs.ReasonCode)
	}
}
