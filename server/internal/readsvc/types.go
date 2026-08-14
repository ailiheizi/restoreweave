package readsvc

import (
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrInvalidRange     = errors.New("invalid byte range")
	ErrRangeOverflow    = errors.New("byte range overflows uint64")
	ErrRangeOutOfBounds = errors.New("byte range is outside the logical stream")
	ErrInvalidPin       = errors.New("invalid immutable pin")
	ErrPinMismatch      = errors.New("immutable pin does not match the open view")
	ErrSessionExpired   = errors.New("read session has expired")
)

// ByteRange is a half-open logical byte interval [Offset, Offset+Length).
// An empty range at the end of a stream is valid.
type ByteRange struct {
	Offset uint64
	Length uint64
}

// End returns the exclusive end offset.
func (r ByteRange) End() (uint64, error) {
	if r.Length > math.MaxUint64-r.Offset {
		return 0, ErrRangeOverflow
	}
	return r.Offset + r.Length, nil
}

// Validate checks the range against a decoded logical stream size.
func (r ByteRange) Validate(size uint64) error {
	end, err := r.End()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRange, err)
	}
	if r.Offset > size || end > size {
		return fmt.Errorf("%w: offset=%d length=%d size=%d", ErrRangeOutOfBounds, r.Offset, r.Length, size)
	}
	return nil
}

// Contains reports whether candidate is completely inside r. Invalid or
// overflowing ranges never contain another range.
func (r ByteRange) Contains(candidate ByteRange) bool {
	end, err := r.End()
	if err != nil {
		return false
	}
	candidateEnd, err := candidate.End()
	if err != nil {
		return false
	}
	return candidate.Offset >= r.Offset && candidateEnd <= end
}

// SnapshotSelector names an exact immutable namespace root. The expected
// digest is an optional If-Match-style guard; "latest" resolution belongs
// outside this type and must be pinned before a view is opened.
type SnapshotSelector struct {
	WorkspaceID             string
	PublicationID           string
	NamespaceRootID         string
	ExpectedNamespaceDigest string
}

func (s SnapshotSelector) Validate() error {
	if err := require("workspace id", s.WorkspaceID); err != nil {
		return err
	}
	if err := require("publication id", s.PublicationID); err != nil {
		return err
	}
	return require("namespace root id", s.NamespaceRootID)
}

// SnapshotPin is the authenticated immutable identity returned by the host
// after opening a selector.
type SnapshotPin struct {
	WorkspaceID         string
	PublicationID       string
	NamespaceRootID     string
	NamespaceGeneration uint64
	NamespaceDigest     string
}

func (p SnapshotPin) Validate() error {
	if err := requirePin("workspace id", p.WorkspaceID); err != nil {
		return err
	}
	if err := requirePin("publication id", p.PublicationID); err != nil {
		return err
	}
	if err := requirePin("namespace root id", p.NamespaceRootID); err != nil {
		return err
	}
	if p.NamespaceGeneration == 0 {
		return fmt.Errorf("%w: namespace generation must be greater than zero", ErrInvalidPin)
	}
	return requirePin("namespace digest", p.NamespaceDigest)
}

// AuthorizationPin records a host decision. It is evidence of an existing
// authorization decision, not a bearer credential and not permission for an
// adapter to make a new decision.
type AuthorizationPin struct {
	DecisionID  string
	PrincipalID string
	PolicyEpoch string
	ExpiresAt   time.Time
}

func (p AuthorizationPin) Validate() error {
	if err := requirePin("authorization decision id", p.DecisionID); err != nil {
		return err
	}
	if err := requirePin("principal id", p.PrincipalID); err != nil {
		return err
	}
	if err := requirePin("policy epoch", p.PolicyEpoch); err != nil {
		return err
	}
	if p.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: authorization expiry is required", ErrInvalidPin)
	}
	return nil
}

// ViewPin fixes both namespace identity and the authorization decision under
// which the namespace may be observed.
type ViewPin struct {
	Snapshot      SnapshotPin
	Authorization AuthorizationPin
}

func (p ViewPin) Validate() error {
	if err := p.Snapshot.Validate(); err != nil {
		return err
	}
	return p.Authorization.Validate()
}

func (p ViewPin) Equal(other ViewPin) bool {
	return p.Snapshot == other.Snapshot &&
		p.Authorization.DecisionID == other.Authorization.DecisionID &&
		p.Authorization.PrincipalID == other.Authorization.PrincipalID &&
		p.Authorization.PolicyEpoch == other.Authorization.PolicyEpoch &&
		p.Authorization.ExpiresAt.Equal(other.Authorization.ExpiresAt)
}

// ReadPin fixes every identity that must remain stable behind an open file
// handle. Repacking may create a later placement checkpoint, but cannot alter
// a handle carrying an older pin.
type ReadPin struct {
	View                      ViewPin
	EntryID                   string
	FileVersionID             string
	RepresentationID          string
	ContentID                 string
	PlacementCheckpointID     string
	PlacementCheckpointDigest string
}

func (p ReadPin) Validate() error {
	if err := p.View.Validate(); err != nil {
		return err
	}
	if err := requirePin("entry id", p.EntryID); err != nil {
		return err
	}
	if err := requirePin("file version id", p.FileVersionID); err != nil {
		return err
	}
	if err := requirePin("representation id", p.RepresentationID); err != nil {
		return err
	}
	if err := requirePin("content id", p.ContentID); err != nil {
		return err
	}
	if err := requirePin("placement checkpoint id", p.PlacementCheckpointID); err != nil {
		return err
	}
	return requirePin("placement checkpoint digest", p.PlacementCheckpointDigest)
}

// ValidateAgainstView prevents an entry or representation opened through one
// view from being attached to a different snapshot or policy decision.
func (p ReadPin) ValidateAgainstView(view ViewPin) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := view.Validate(); err != nil {
		return err
	}
	if !p.View.Equal(view) {
		return ErrPinMismatch
	}
	return nil
}

type EntryKind string

const (
	EntryDirectory   EntryKind = "DIRECTORY"
	EntryRegularFile EntryKind = "REGULAR_FILE"
	EntrySymlink     EntryKind = "SYMLINK"
	EntryFIFO        EntryKind = "FIFO"
	EntrySocket      EntryKind = "SOCKET"
	EntryDevice      EntryKind = "DEVICE"
	EntrySpecial     EntryKind = "SPECIAL"
)

// PathComponent is one caller-supplied component. It is never concatenated
// into a host filesystem path. Raw and normalized forms are mutually
// exclusive, and the host resolves components without following symlinks.
type PathComponent struct {
	Raw                  []byte
	Normalized           string
	NormalizationProfile string
}

func (c PathComponent) Validate() error {
	hasRaw := len(c.Raw) != 0
	hasNormalized := c.Normalized != ""
	if hasRaw == hasNormalized {
		return errors.New("path component must contain exactly one of raw or normalized name")
	}
	if hasRaw && c.NormalizationProfile != "" {
		return errors.New("raw path component cannot declare a normalization profile")
	}
	if hasNormalized && c.NormalizationProfile == "" {
		return errors.New("normalized path component requires a normalization profile")
	}
	return nil
}

// NamespaceEntry is logical namespace data only. It deliberately contains no
// backend, repository, pack, object, or byte-offset locator.
type NamespaceEntry struct {
	ID               string
	NamespaceRootID  string
	ParentID         string
	RawName          []byte
	DisplayName      string
	Kind             EntryKind
	FileVersionID    string
	ContentID        string
	LogicalSize      uint64
	HasLogicalSize   bool
	MetadataRecordID string
	SymlinkTargetRaw []byte
	HardlinkGroupID  string
	ModTime          time.Time
	UID              uint32
	GID              uint32
	HasOwnership     bool
}

type PageRequest struct {
	Cursor string
	Limit  uint32
}

type EntryPage struct {
	Entries    []NamespaceEntry
	NextCursor string
}

// AccessRequest carries references to host-authenticated request context. A
// gateway must not place a raw username or credential in IdentityContextID.
type AccessRequest struct {
	IdentityContextID string
	RequestID         string
	Audience          string
}

func (r AccessRequest) Validate() error {
	if err := require("identity context id", r.IdentityContextID); err != nil {
		return err
	}
	if err := require("request id", r.RequestID); err != nil {
		return err
	}
	return require("audience", r.Audience)
}

// AuthorizationDecision is produced only by HostAuthorizer.
type AuthorizationDecision struct {
	Pin        AuthorizationPin
	Operations []AccessOperation
}

// SessionLimits are enforced by the host on every open and read. If
// RestrictRange is false, AllowedRange must be zero-valued.
type SessionLimits struct {
	RestrictRange      bool
	AllowedRange       ByteRange
	MaxBytes           uint64
	MaxOpens           uint32
	MaxConcurrentReads uint32
	ExpiresAt          time.Time
}

func (l SessionLimits) Validate(fileSize uint64, now time.Time) error {
	if l.RestrictRange {
		if err := l.AllowedRange.Validate(fileSize); err != nil {
			return fmt.Errorf("allowed range: %w", err)
		}
	} else if l.AllowedRange != (ByteRange{}) {
		return errors.New("allowed range must be zero when range restriction is disabled")
	}
	if l.MaxBytes == 0 {
		return errors.New("session max bytes must be greater than zero")
	}
	if l.MaxOpens == 0 {
		return errors.New("session max opens must be greater than zero")
	}
	if l.MaxConcurrentReads == 0 {
		return errors.New("session max concurrent reads must be greater than zero")
	}
	if l.ExpiresAt.IsZero() {
		return errors.New("session expiry is required")
	}
	if !now.IsZero() && !now.Before(l.ExpiresAt) {
		return ErrSessionExpired
	}
	return nil
}

func (l SessionLimits) Allows(r ByteRange, fileSize uint64) bool {
	if r.Validate(fileSize) != nil {
		return false
	}
	if r.Length > l.MaxBytes {
		return false
	}
	return !l.RestrictRange || l.AllowedRange.Contains(r)
}

type ReadSessionInfo struct {
	ID     string
	Pin    ReadPin
	Limits SessionLimits
}

func (s ReadSessionInfo) Validate(view ViewPin, fileSize uint64, now time.Time) error {
	if err := require("read session id", s.ID); err != nil {
		return err
	}
	if err := s.Pin.ValidateAgainstView(view); err != nil {
		return err
	}
	if err := s.Limits.Validate(fileSize, now); err != nil {
		return err
	}
	if s.Limits.ExpiresAt.After(s.Pin.View.Authorization.ExpiresAt) {
		return errors.New("read session cannot outlive its authorization decision")
	}
	return nil
}

type VerificationScope string

const (
	VerificationRangeFrameVerified   VerificationScope = "RANGE_FRAME_VERIFIED"
	VerificationEngineAuthenticated  VerificationScope = "ENGINE_AUTHENTICATED_STREAM"
	VerificationWholeContentVerified VerificationScope = "WHOLE_CONTENT_VERIFIED"
	VerificationCachedWholeContent   VerificationScope = "CACHED_WHOLE_CONTENT_VERIFIED"
)

func (s VerificationScope) valid() bool {
	switch s {
	case VerificationRangeFrameVerified,
		VerificationEngineAuthenticated,
		VerificationWholeContentVerified,
		VerificationCachedWholeContent:
		return true
	default:
		return false
	}
}

// AcceptedVerification is a host acceptance result. Adapter-facing receipts
// use VerificationClaim instead and cannot manufacture this decision.
type AcceptedVerification struct {
	Scope        VerificationScope
	AcceptanceID string
	PolicyRef    string
	AcceptedAt   time.Time
	EvidenceRefs []string
}

func (v AcceptedVerification) Validate() error {
	if !v.Scope.valid() {
		return fmt.Errorf("unknown verification scope %q", v.Scope)
	}
	if err := require("verification acceptance id", v.AcceptanceID); err != nil {
		return err
	}
	if err := require("verification policy ref", v.PolicyRef); err != nil {
		return err
	}
	if v.AcceptedAt.IsZero() {
		return errors.New("verification acceptance time is required")
	}
	if len(v.EvidenceRefs) == 0 {
		return errors.New("accepted verification requires evidence")
	}
	return nil
}

type EvidenceKind string

const (
	EvidenceEncodedObjectDigest      EvidenceKind = "ENCODED_OBJECT_DIGEST_MATCHED"
	EvidenceAuthenticatedFrame       EvidenceKind = "AUTHENTICATED_FRAME"
	EvidenceEngineAuthenticated      EvidenceKind = "ENGINE_STREAM_AUTHENTICATED"
	EvidenceWholeContentDigest       EvidenceKind = "WHOLE_CONTENT_DIGEST_MATCHED"
	EvidenceCachedWholeContentRecord EvidenceKind = "CACHED_WHOLE_CONTENT_RECORD"
)

// VerificationClaim is an adapter assertion that the host still has to
// authenticate, bind to the requested bytes, and accept under policy.
type VerificationClaim struct {
	Kind           EvidenceKind
	IssuerID       string
	EvidenceRef    string
	Coverage       ByteRange
	HasCoverage    bool
	ObservedDigest string
}

type RangeReadResult struct {
	Requested       ByteRange
	Returned        ByteRange
	BytesRead       uint64
	SourceBytesRead uint64
	Verification    AcceptedVerification
}

func require(label, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	return nil
}

func requirePin(label, value string) error {
	if value == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidPin, label)
	}
	return nil
}
