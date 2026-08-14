package readsvc

import "context"

type SnapshotViewRequest struct {
	Access   AccessRequest
	Snapshot SnapshotSelector
}

// SnapshotTree is the host entry point for opening an authorized, immutable
// namespace view. Implementations authenticate the namespace before returning
// a view; adapters never construct views themselves.
type SnapshotTree interface {
	OpenView(context.Context, SnapshotViewRequest) (SnapshotView, error)
}

// SnapshotView is pinned to exactly one namespace and authorization decision.
// Lookup and ResolvePath are host-implemented and never follow a stored
// symbolic link server-side. ReadLink returns the captured raw target bytes;
// interpreting or following that target is a caller-side operation.
type SnapshotView interface {
	Pin() ViewPin
	Root(context.Context) (NamespaceEntry, error)
	Stat(context.Context, string) (NamespaceEntry, error)
	Lookup(context.Context, string, PathComponent) (NamespaceEntry, error)
	ListChildren(context.Context, string, PageRequest) (EntryPage, error)
	ResolvePath(context.Context, []PathComponent) (NamespaceEntry, error)
	ReadLink(context.Context, string) ([]byte, error)
	Close() error
}

type OpenFileRequest struct {
	Access           AccessRequest
	View             SnapshotView
	EntryID          string
	RepresentationID string
	Limits           SessionLimits
}

// FileAccess authorizes exact-content access and creates a bounded session.
// An empty RepresentationID means the snapshot's authoritative exact
// representation; it never means "choose a similar representation".
type FileAccess interface {
	OpenSession(context.Context, OpenFileRequest) (ReadSession, error)
}

// ReadSession pins the view, file version, representation, placement
// checkpoint, policy epoch, and authorization decision for every handle it
// opens.
type ReadSession interface {
	Info() ReadSessionInfo
	Open(context.Context) (RandomAccessFile, error)
	Close() error
}

// RandomAccessFile exposes the decoded logical file stream. Implementations
// may emulate a range using a sequential repository, but must enforce session
// budgets and report the resulting source-byte amplification.
type RandomAccessFile interface {
	Pin() ReadPin
	Size() uint64
	ETag() string
	ReadAt(context.Context, []byte, uint64) (RangeReadResult, error)
	Close() error
}

type AccessOperation string

const (
	AccessDiscoverNamespace  AccessOperation = "DISCOVER_NAMESPACE"
	AccessListDirectory      AccessOperation = "LIST_DIRECTORY"
	AccessReadMetadata       AccessOperation = "READ_METADATA"
	AccessReadExactContent   AccessOperation = "READ_EXACT_CONTENT"
	AccessReadRepresentation AccessOperation = "READ_REPRESENTATION"
)

// HostAuthorizer, HostPathResolver, and HostVerificationAcceptor are host-only
// policy seams. They are intentionally absent from every plugin-facing port.
// A gateway can ask the high-level host services to perform an operation, but
// cannot implement or bypass these decisions.
type HostAuthorizer interface {
	AuthorizeView(context.Context, AccessRequest, SnapshotSelector) (AuthorizationDecision, error)
	AuthorizeEntry(context.Context, AccessRequest, ViewPin, string, AccessOperation) (AuthorizationDecision, error)
	RevalidateRead(context.Context, ReadPin) error
}

type HostPathResolver interface {
	ResolvePath(context.Context, ViewPin, []PathComponent) (NamespaceEntry, error)
}

type VerificationAcceptanceRequest struct {
	Pin             ReadPin
	Requested       ByteRange
	Returned        ByteRange
	BytesRead       uint64
	SourceBytesRead uint64
	Claims          []VerificationClaim
}

type HostVerificationAcceptor interface {
	AcceptRange(context.Context, VerificationAcceptanceRequest) (AcceptedVerification, error)
}
