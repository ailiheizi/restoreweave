//go:build darwin || freebsd || linux || netbsd

package scanner

import (
	"time"

	"golang.org/x/sys/unix"
)

func (fileSystem OSFileSystem) CaptureFilesystemFacts(path string, kind EntryKind) FilesystemFacts {
	now := time.Now().UTC()
	if kind == KindSymlink {
		return captureFilesystemFactsSymlink(path, now)
	}
	if kind != KindRegularFile && kind != KindDirectory {
		return emptyFilesystemFacts(now, kind)
	}
	file, err := openPathNoFollow(path, kind == KindDirectory)
	if err != nil {
		return unavailableFilesystemFacts(now, kind, err)
	}
	defer file.Close()
	return captureFilesystemFactsFD(int(file.Fd()), kind, now)
}

func captureFilesystemFactsSymlink(path string, now time.Time) FilesystemFacts {
	facts := emptyFilesystemFacts(now, KindSymlink)
	attrs := captureXAttrs(
		func(buffer []byte) (int, error) { return unix.Llistxattr(path, buffer) },
		func(name string, buffer []byte) (int, error) { return unix.Lgetxattr(path, name, buffer) },
		parseNULXAttrNames,
	)
	if attrs.State == CaptureFactObserved || attrs.State == CaptureFactInconsistent || attrs.State == CaptureFactUnobserved {
		facts.XAttrs = attrs
		facts.ACLs = aclFactsFromXAttrs(attrs, KindSymlink)
	}
	return facts
}

func captureFilesystemFactsFD(fd int, kind EntryKind, now time.Time) FilesystemFacts {
	attrs := captureXAttrs(
		func(buffer []byte) (int, error) { return unix.Flistxattr(fd, buffer) },
		func(name string, buffer []byte) (int, error) { return unix.Fgetxattr(fd, name, buffer) },
		parseNULXAttrNames,
	)
	facts := emptyFilesystemFacts(now, kind)
	facts.XAttrs = attrs
	facts.ACLs = aclFactsFromXAttrs(attrs, kind)
	return facts
}

func unavailableFilesystemFacts(now time.Time, kind EntryKind, err error) FilesystemFacts {
	facts := emptyFilesystemFacts(now, kind)
	if isUnsupportedXAttrError(err) {
		facts.XAttrs = unsupportedXAttrFacts("XATTR_CAPABILITY_UNSUPPORTED")
		facts.ACLs = unsupportedACLFacts("ACL_CAPTURE_CAPABILITY_UNSUPPORTED")
		return facts
	}
	facts.XAttrs = XAttrFacts{State: CaptureFactUnobserved, Attributes: []ExtendedAttribute{}, ReasonCode: "XATTR_OPEN_FAILED"}
	facts.ACLs = ACLFacts{State: CaptureFactUnobserved, Records: []ACLRecord{}, ReasonCode: "ACL_OPEN_FAILED"}
	return facts
}
