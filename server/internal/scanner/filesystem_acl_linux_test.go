//go:build linux

package scanner

import "testing"

func TestACLStateFollowsXAttrCaptureState(t *testing.T) {
	for _, test := range []struct {
		name  string
		xattr CaptureFactState
		want  CaptureFactState
	}{
		{name: "unsupported", xattr: CaptureFactUnsupported, want: CaptureFactUnsupported},
		{name: "unobserved", xattr: CaptureFactUnobserved, want: CaptureFactUnobserved},
		{name: "inconsistent", xattr: CaptureFactInconsistent, want: CaptureFactUnobserved},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := aclFactsFromXAttrs(XAttrFacts{State: test.xattr}, KindRegularFile)
			if facts.State != test.want || facts.ReasonCode == "" {
				t.Fatalf("ACL facts for xattr state %q = %+v, want state %q with reason", test.xattr, facts, test.want)
			}
		})
	}
}
