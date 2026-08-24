//go:build freebsd || netbsd || dragonfly || openbsd

package scanner

func aclFactsFromXAttrs(_ XAttrFacts, _ EntryKind) ACLFacts {
	return unsupportedACLFacts("CAPTURE_PROFILE_DOES_NOT_PARSE_NATIVE_ACLS")
}
