//go:build !darwin && !freebsd && !linux && !netbsd

package scanner

import "time"

func (fileSystem OSFileSystem) CaptureFilesystemFacts(_ string, kind EntryKind) FilesystemFacts {
	facts := emptyFilesystemFacts(time.Now().UTC(), kind)
	facts.XAttrs = unsupportedXAttrFacts("CAPTURE_PROFILE_DOES_NOT_SUPPORT_XATTRS")
	facts.ACLs = unsupportedACLFacts("CAPTURE_PROFILE_DOES_NOT_SUPPORT_ACLS")
	return facts
}
