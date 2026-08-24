//go:build linux

package scanner

func aclFactsFromXAttrs(xattrs XAttrFacts, kind EntryKind) ACLFacts {
	if xattrs.State != CaptureFactObserved {
		state := xattrs.State
		if state == CaptureFactUnsupported {
			return unsupportedACLFacts("ACL_CAPTURE_DEPENDENCY_UNSUPPORTED")
		}
		if state == CaptureFactInconsistent {
			state = CaptureFactUnobserved
		}
		return ACLFacts{State: state, Format: "linux.posix-acl-xattr-v1", Records: []ACLRecord{}, ReasonCode: "ACL_XATTR_CAPTURE_INCOMPLETE"}
	}
	records := make([]ACLRecord, 0, 2)
	for _, attribute := range xattrs.Attributes {
		if attribute.Name == "system.posix_acl_access" || (kind == KindDirectory && attribute.Name == "system.posix_acl_default") {
			records = append(records, ACLRecord{Name: attribute.Name, Raw: append([]byte(nil), attribute.Value...)})
		}
	}
	if len(records) == 0 {
		return ACLFacts{
			// The xattr namespace was successfully enumerated. An absent
			// POSIX ACL is therefore an observed empty ACL, not unknown state.
			State:      CaptureFactObserved,
			Format:     "linux.posix-acl-xattr-v1",
			Records:    []ACLRecord{},
			ReasonCode: "NO_EXPLICIT_POSIX_ACL_XATTR",
		}
	}
	return ACLFacts{State: CaptureFactObserved, Format: "linux.posix-acl-xattr-v1", Records: records}
}
