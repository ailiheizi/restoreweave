//go:build darwin || freebsd || linux || netbsd

package scanner

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func captureRootedFilesystemFacts(rootFD int, rel string, kind EntryKind, now time.Time) FilesystemFacts {
	facts := emptyFilesystemFacts(now, kind)
	if kind != KindRegularFile && kind != KindDirectory {
		return facts
	}
	flags := unix.O_RDONLY
	if kind == KindDirectory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := resolveBeneath(rootFD, rel, flags)
	if err != nil {
		if isUnsupportedXAttrError(err) {
			facts.XAttrs = unsupportedXAttrFacts("XATTR_CAPABILITY_UNSUPPORTED")
			facts.ACLs = unsupportedACLFacts("ACL_CAPTURE_CAPABILITY_UNSUPPORTED")
		} else if os.IsPermission(err) {
			facts.XAttrs.State = CaptureFactUnobserved
			facts.XAttrs.ReasonCode = "XATTR_OPEN_PERMISSION_DENIED"
			facts.ACLs.State = CaptureFactUnobserved
			facts.ACLs.ReasonCode = "ACL_OPEN_PERMISSION_DENIED"
		} else {
			facts.XAttrs.State = CaptureFactUnobserved
			facts.XAttrs.ReasonCode = "XATTR_OPEN_FAILED"
			facts.ACLs.State = CaptureFactUnobserved
			facts.ACLs.ReasonCode = "ACL_OPEN_FAILED"
		}
		return facts
	}
	defer closeFd(fd)
	facts = captureFilesystemFactsFD(fd, kind, now)
	return facts
}
