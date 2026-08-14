package scanner

import (
	"context"
)

// DeviceBoundaryChecker is the default boundary policy for rooted capture. It
// compares the entry device against the capture-root device and skips entries
// on a different device (a bind mount, nested mount, or other volume edge)
// with reason "device_boundary". When either identity is unknown the entry is
// conservatively included with reason "device_unknown": a skipped boundary is
// authoritative only when both sides were actually observed. Callers may
// install their own BoundaryChecker to override this policy.
type DeviceBoundaryChecker struct{}

// CheckBoundary implements BoundaryChecker.
func (DeviceBoundaryChecker) CheckBoundary(_ context.Context, candidate BoundaryCandidate) (BoundaryDecision, error) {
	if !candidate.RootMetadata.IdentityKnown || !candidate.EntryMetadata.IdentityKnown {
		return BoundaryDecision{Action: BoundaryInclude, Reason: "device_unknown"}, nil
	}
	if candidate.RootMetadata.DeviceID != candidate.EntryMetadata.DeviceID {
		return BoundaryDecision{Action: BoundarySkip, Reason: "device_boundary"}, nil
	}
	return BoundaryDecision{Action: BoundaryInclude, Reason: "device_same"}, nil
}
