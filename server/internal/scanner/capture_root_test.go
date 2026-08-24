package scanner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenCaptureRootRejectsSymlinkRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	mustMkdir(t, target)
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	capture, err := OpenCaptureRoot(link)
	if err == nil {
		capture.Close()
		t.Fatalf("OpenCaptureRoot on a symlink root succeeded")
	}
}

func TestOpenCaptureRootRejectsNonDirectoryRoot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file.bin")
	mustWriteFile(t, file, []byte("x"))
	capture, err := OpenCaptureRoot(file)
	if err == nil {
		capture.Close()
		t.Fatalf("OpenCaptureRoot on a regular file succeeded")
	}
}

func TestOpenCaptureRootRejectsNULPath(t *testing.T) {
	capture, err := OpenCaptureRoot("bad\x00root")
	if err == nil {
		capture.Close()
		t.Fatalf("OpenCaptureRoot accepted a NUL path")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("OpenCaptureRoot error = %v, want ErrInvalidRequest", err)
	}
}

func TestCaptureRootIdentityAndVerify(t *testing.T) {
	root := t.TempDir()
	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()

	device, inode := capture.Identity()
	if device == 0 || inode == 0 {
		t.Fatalf("Identity() = (%d, %d), want non-zero", device, inode)
	}
	if capture.Path() != filepath.Clean(root) {
		t.Fatalf("Path() = %q, want %q", capture.Path(), filepath.Clean(root))
	}
	if err := capture.VerifyRoot(); err != nil {
		t.Fatalf("VerifyRoot() on an untouched root error = %v", err)
	}

	whiteboxRecycleDescriptor(t, capture)
	if err := capture.VerifyRoot(); !errors.Is(err, ErrCaptureRootReplaced) {
		t.Fatalf("VerifyRoot() after descriptor recycling = %v, want ErrCaptureRootReplaced", err)
	}
}

// whiteboxRecycleDescriptor simulates a stale binding whose descriptor number
// was reused for a different file, which is exactly what the recorded
// identity comparison must detect.
func whiteboxRecycleDescriptor(t *testing.T, capture *CaptureRoot) {
	t.Helper()
	other := t.TempDir()
	otherFile, err := os.Open(other)
	if err != nil {
		t.Fatalf("open other dir: %v", err)
	}
	t.Cleanup(func() { _ = otherFile.Close() })
	capture.fd = int(otherFile.Fd())
}

func TestCaptureRootCloseIsIdempotent(t *testing.T) {
	capture, err := OpenCaptureRoot(t.TempDir())
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := capture.VerifyRoot(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("VerifyRoot() after Close = %v, want os.ErrClosed", err)
	}
}

func TestCaptureRootDetectsPathLevelReplacement(t *testing.T) {
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

	if err := capture.VerifyRoot(); !errors.Is(err, ErrCaptureRootReplaced) {
		t.Fatalf("VerifyRoot() after root replacement = %v, want ErrCaptureRootReplaced", err)
	}
}

func TestSanitizeRelativePathRejections(t *testing.T) {
	valid := []string{".", "a", "a/b", "a/b/c.bin"}
	for _, path := range valid {
		if _, err := sanitizeRelativePath(path); err != nil {
			t.Fatalf("sanitizeRelativePath(%q) error = %v, want nil", path, err)
		}
	}
	invalid := []string{
		"",
		"..",
		"a/..",
		"a/../b",
		"../a",
		"/a",
		"/",
		"a//b",
		"a/./b",
		"a\x00b",
		"a/\x00/b",
	}
	for _, path := range invalid {
		if _, err := sanitizeRelativePath(path); err == nil {
			t.Fatalf("sanitizeRelativePath(%q) succeeded, want rejection", path)
		}
	}
}

func TestResolveBeneathRejectsEscapeAttempts(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sub"))
	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()

	for _, rel := range []string{"..", "../x", "sub/..", "sub/../../x", "/etc", "/etc/passwd", "a\x00b"} {
		fd, err := resolveBeneath(capture.fd, rel, rootedRegularOpenFlags)
		if err == nil {
			_ = closeFd(fd)
			t.Fatalf("resolveBeneath(%q) succeeded, want rejection", rel)
		}
	}
}

func TestResolveBeneathRejectsIntermediateSymlinkEscape(t *testing.T) {
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

	for _, rel := range []string{"sub/escape/secret.bin", "sub/escape"} {
		fd, err := resolveBeneath(capture.fd, rel, rootedRegularOpenFlags)
		if err == nil {
			_ = closeFd(fd)
			t.Fatalf("resolveBeneath(%q) followed an intermediate symlink", rel)
		}
	}
	// The symlink itself must still be observable as a symlink.
	info, err := statRelative(capture.fd, "sub/escape")
	if err != nil {
		t.Fatalf("statRelative on the symlink itself error = %v", err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("statRelative kind = %v, want symlink", info.Mode())
	}
}

func TestResolveBeneathRejectsMagicLink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("magic-link resolution is Linux-specific")
	}
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skipf("/proc is unavailable: %v", err)
	}
	// Keep the retained root itself a real directory. /proc/self is a
	// procfs magic link and must be rejected by OpenCaptureRoot's no-follow
	// root check; the traversal check below targets the magic-link component.
	capture, err := OpenCaptureRoot("/proc")
	if err != nil {
		t.Fatalf("OpenCaptureRoot(/proc) error = %v", err)
	}
	defer capture.Close()

	// /proc/self/fd/N entries are magic links; RESOLVE_NO_MAGICLINKS must
	// refuse to traverse one even though the path stays beneath the root.
	fd, err := resolveBeneath(capture.fd, "self/fd/0", rootedRegularOpenFlags)
	if err == nil {
		_ = closeFd(fd)
		t.Fatalf("resolveBeneath traversed a magic link")
	}
}

func TestResolveBeneathOpensRootItself(t *testing.T) {
	root := t.TempDir()
	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()

	fd, err := resolveBeneath(capture.fd, ".", rootedDirOpenFlags)
	if err != nil {
		t.Fatalf("resolveBeneath(\".\") error = %v", err)
	}
	defer closeFd(fd)
	device, inode, err := statFd(fd)
	if err != nil {
		t.Fatalf("statFd error = %v", err)
	}
	wantDevice, wantInode := capture.Identity()
	if device != wantDevice || inode != wantInode {
		t.Fatalf("root identity = (%d, %d), want (%d, %d)", device, inode, wantDevice, wantInode)
	}
}

func TestResolveBeneathOpensNestedFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "a"))
	mustMkdir(t, filepath.Join(root, "a", "b"))
	mustWriteFile(t, filepath.Join(root, "a", "b", "f.bin"), []byte("data"))
	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()

	fd, err := resolveBeneath(capture.fd, "a/b/f.bin", rootedRegularOpenFlags)
	if err != nil {
		t.Fatalf("resolveBeneath(nested file) error = %v", err)
	}
	defer closeFd(fd)
	file := os.NewFile(uintptr(fd), "f.bin")
	buffer := make([]byte, 4)
	count, err := file.Read(buffer)
	_ = file.Close()
	if count != 4 || err != nil {
		t.Fatalf("read = (%d, %v), want (4, nil)", count, err)
	}

	dirFd, err := resolveBeneath(capture.fd, "a/b", rootedDirOpenFlags)
	if err != nil {
		t.Fatalf("resolveBeneath(nested dir) error = %v", err)
	}
	defer closeFd(dirFd)
	directory := os.NewFile(uintptr(dirFd), "b")
	entries, err := directory.ReadDir(-1)
	_ = directory.Close()
	if err != nil || len(entries) != 1 || entries[0].Name() != "f.bin" {
		t.Fatalf("readdir = (%v, %v), want f.bin", entries, err)
	}
}

func TestResolveBeneathRejectsFinalSymlinkFollow(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "secret.bin"), []byte("outside"))
	if err := os.Symlink(filepath.Join(outside, "secret.bin"), filepath.Join(root, "link.bin")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	capture, err := OpenCaptureRoot(root)
	if err != nil {
		t.Fatalf("OpenCaptureRoot() error = %v", err)
	}
	defer capture.Close()

	fd, err := resolveBeneath(capture.fd, "link.bin", rootedRegularOpenFlags)
	if err == nil {
		_ = closeFd(fd)
		t.Fatalf("resolveBeneath followed a final symlink")
	}
}
