package readsvc

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestByteRangeValidationAndContainment(t *testing.T) {
	tests := []struct {
		name    string
		range_  ByteRange
		size    uint64
		wantErr error
	}{
		{name: "bounded", range_: ByteRange{Offset: 4, Length: 6}, size: 10},
		{name: "empty at eof", range_: ByteRange{Offset: 10}, size: 10},
		{name: "offset past eof", range_: ByteRange{Offset: 11}, size: 10, wantErr: ErrRangeOutOfBounds},
		{name: "end past eof", range_: ByteRange{Offset: 9, Length: 2}, size: 10, wantErr: ErrRangeOutOfBounds},
		{name: "overflow", range_: ByteRange{Offset: math.MaxUint64, Length: 2}, size: math.MaxUint64, wantErr: ErrRangeOverflow},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.range_.Validate(test.size)
			if test.wantErr == nil && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("Validate() error = %v, want errors.Is(_, %v)", err, test.wantErr)
			}
		})
	}

	allowed := ByteRange{Offset: 10, Length: 20}
	if !allowed.Contains(ByteRange{Offset: 15, Length: 5}) {
		t.Fatal("allowed range should contain interior range")
	}
	if allowed.Contains(ByteRange{Offset: 29, Length: 2}) {
		t.Fatal("allowed range unexpectedly contains an overrun")
	}
}

func TestReadSessionInfoPinsViewAndBoundsLifetime(t *testing.T) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	view := testViewPin(now.Add(time.Hour))
	info := ReadSessionInfo{
		ID: "session-1",
		Pin: ReadPin{
			View:                      view,
			EntryID:                   "entry-1",
			FileVersionID:             "file-version-1",
			RepresentationID:          "representation-1",
			ContentID:                 "sha256:content",
			PlacementCheckpointID:     "checkpoint-1",
			PlacementCheckpointDigest: "sha256:checkpoint",
		},
		Limits: SessionLimits{
			RestrictRange:      true,
			AllowedRange:       ByteRange{Offset: 100, Length: 100},
			MaxBytes:           100,
			MaxOpens:           1,
			MaxConcurrentReads: 1,
			ExpiresAt:          now.Add(30 * time.Minute),
		},
	}

	if err := info.Validate(view, 1_000, now); err != nil {
		t.Fatalf("valid session rejected: %v", err)
	}
	if !info.Limits.Allows(ByteRange{Offset: 125, Length: 25}, 1_000) {
		t.Fatal("session should allow range inside its pin")
	}
	if info.Limits.Allows(ByteRange{Offset: 50, Length: 100}, 1_000) {
		t.Fatal("session unexpectedly allows range outside its pin")
	}

	otherView := view
	otherView.Snapshot.NamespaceGeneration++
	if err := info.Validate(otherView, 1_000, now); !errors.Is(err, ErrPinMismatch) {
		t.Fatalf("mismatched view error = %v, want ErrPinMismatch", err)
	}

	info.Limits.ExpiresAt = view.Authorization.ExpiresAt.Add(time.Second)
	if err := info.Validate(view, 1_000, now); err == nil {
		t.Fatal("session outliving authorization unexpectedly validated")
	}
}

func TestSessionExpiryAndRangeRestriction(t *testing.T) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	limits := SessionLimits{
		RestrictRange:      true,
		AllowedRange:       ByteRange{Offset: 90, Length: 20},
		MaxBytes:           20,
		MaxOpens:           1,
		MaxConcurrentReads: 1,
		ExpiresAt:          now,
	}
	if err := limits.Validate(100, now); !errors.Is(err, ErrRangeOutOfBounds) {
		t.Fatalf("out-of-bounds session range error = %v", err)
	}

	limits.AllowedRange = ByteRange{Offset: 80, Length: 20}
	if err := limits.Validate(100, now); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired session error = %v, want ErrSessionExpired", err)
	}
}

func testViewPin(expiry time.Time) ViewPin {
	return ViewPin{
		Snapshot: SnapshotPin{
			WorkspaceID:         "workspace-1",
			PublicationID:       "publication-1",
			NamespaceRootID:     "namespace-1",
			NamespaceGeneration: 7,
			NamespaceDigest:     "sha256:namespace",
		},
		Authorization: AuthorizationPin{
			DecisionID:  "decision-1",
			PrincipalID: "principal-1",
			PolicyEpoch: "policy-7",
			ExpiresAt:   expiry,
		},
	}
}
