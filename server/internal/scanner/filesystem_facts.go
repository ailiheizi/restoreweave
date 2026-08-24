package scanner

import "time"

func emptyFilesystemFacts(now time.Time, kind EntryKind) FilesystemFacts {
	facts := FilesystemFacts{
		Version:    FilesystemFactsVersion,
		CapturedAt: now.UTC(),
		XAttrs: XAttrFacts{
			State:      CaptureFactUnobserved,
			Attributes: []ExtendedAttribute{},
			ReasonCode: "CAPTURE_PROFILE_DID_NOT_EMIT_XATTRS",
		},
		ACLs: ACLFacts{
			State:      CaptureFactUnobserved,
			Records:    []ACLRecord{},
			ReasonCode: "CAPTURE_PROFILE_DID_NOT_EMIT_ACLS",
		},
	}
	if kind != KindRegularFile && kind != KindDirectory {
		facts.XAttrs.State = CaptureFactUnsupported
		facts.XAttrs.ReasonCode = "ENTRY_KIND_NOT_SUPPORTED_BY_XATTR_PROFILE"
		facts.ACLs.State = CaptureFactUnsupported
		facts.ACLs.ReasonCode = "ENTRY_KIND_NOT_SUPPORTED_BY_ACL_PROFILE"
	}
	return facts
}

// FilesystemFactProvider is intentionally optional. Injected filesystem test
// doubles and foreign adapters can omit it; the scanner then retains the
// explicit degradation returned by emptyFilesystemFacts.
func captureFilesystemFacts(fileSystem FileSystem, path string, kind EntryKind, now time.Time) FilesystemFacts {
	provider, ok := fileSystem.(FilesystemFactProvider)
	if !ok {
		return emptyFilesystemFacts(now, kind)
	}
	facts := provider.CaptureFilesystemFacts(path, kind)
	if facts.Version == "" {
		facts.Version = FilesystemFactsVersion
	}
	// A provider is replaceable and may omit optional fields. Normalize those
	// omissions here so a zero value never becomes an unqualified fact in the
	// scanner record or its authenticated portable projection.
	if facts.XAttrs.State == "" {
		facts.XAttrs.State = CaptureFactUnobserved
	}
	if facts.XAttrs.Attributes == nil {
		facts.XAttrs.Attributes = []ExtendedAttribute{}
	}
	if facts.XAttrs.ReasonCode == "" && facts.XAttrs.State != CaptureFactObserved {
		facts.XAttrs.ReasonCode = "CAPTURE_PROVIDER_DID_NOT_EMIT_XATTRS"
	}
	if facts.ACLs.State == "" {
		facts.ACLs.State = CaptureFactUnobserved
	}
	if facts.ACLs.Records == nil {
		facts.ACLs.Records = []ACLRecord{}
	}
	if facts.ACLs.ReasonCode == "" && facts.ACLs.State != CaptureFactObserved {
		facts.ACLs.ReasonCode = "CAPTURE_PROVIDER_DID_NOT_EMIT_ACLS"
	}
	// The scanner clock, rather than a provider's ambient wall clock, is the
	// capture-time authority. This keeps capture-basis digests deterministic
	// for plan review/apply and makes test and replay clocks explicit.
	facts.CapturedAt = now.UTC()
	return facts
}
