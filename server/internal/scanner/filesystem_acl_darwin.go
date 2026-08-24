//go:build darwin

package scanner

func aclFactsFromXAttrs(xattrs XAttrFacts, _ EntryKind) ACLFacts {
	if xattrs.State != CaptureFactObserved {
		state := xattrs.State
		if state == CaptureFactUnsupported {
			return unsupportedACLFacts("ACL_CAPTURE_DEPENDENCY_UNSUPPORTED")
		}
		if state == CaptureFactInconsistent {
			state = CaptureFactUnobserved
		}
		return ACLFacts{State: state, Format: "darwin.apple-acl-text-v1", Records: []ACLRecord{}, ReasonCode: "ACL_XATTR_CAPTURE_INCOMPLETE"}
	}
	for _, attribute := range xattrs.Attributes {
		if attribute.Name == "com.apple.acl.text" {
			return ACLFacts{
				State:   CaptureFactObserved,
				Format:  "darwin.apple-acl-text-v1",
				Records: []ACLRecord{{Name: attribute.Name, Raw: append([]byte(nil), attribute.Value...)}},
			}
		}
	}
	return ACLFacts{
		// The xattr namespace was successfully enumerated. An absent Apple
		// ACL is therefore an observed empty ACL, not unknown state.
		State:      CaptureFactObserved,
		Format:     "darwin.apple-acl-text-v1",
		Records:    []ACLRecord{},
		ReasonCode: "NO_EXPLICIT_APPLE_ACL_XATTR",
	}
}
