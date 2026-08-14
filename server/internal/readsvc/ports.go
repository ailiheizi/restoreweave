package readsvc

import (
	"context"
	"io"
)

type AccessMode string

const (
	AccessRandomNative       AccessMode = "RANDOM_ACCESS_NATIVE"
	AccessRandomCheckpointed AccessMode = "RANDOM_ACCESS_CHECKPOINTED"
	AccessSequentialStream   AccessMode = "SEQUENTIAL_STREAM"
	AccessWholeObjectOnly    AccessMode = "WHOLE_OBJECT_ONLY"
)

type ReadBudget struct {
	MaxSourceBytes    uint64
	MaxOutputBytes    uint64
	MaxTemporaryBytes uint64
}

// RepresentationRef contains immutable logical identity and decoding facts;
// it never contains a physical storage locator.
type RepresentationRef struct {
	ID              string
	ContentID       string
	LogicalSize     uint64
	CodecProfileRef string
	AccessMode      AccessMode
}

type RepositoryReadCapabilities struct {
	AccessMode             AccessMode
	MinimumReadableUnit    uint64
	SeekCheckpointInterval uint64
	MaximumConcurrency     uint32
}

// RepositoryReadRequest is fully selected and bounded by the host. EnginePathKey
// is an opaque path key inside the authenticated engine snapshot; it is not a
// local filesystem path. CapabilityScopeID names a short-lived host grant.
type RepositoryReadRequest struct {
	InvocationID          string
	CapabilityScopeID     string
	RepositoryID          string
	EngineSnapshotRef     string
	EngineReceiptRef      string
	EnginePathKey         []byte
	PlacementCheckpointID string
	Representation        RepresentationRef
	Range                 ByteRange
	Budget                ReadBudget
}

type RepositoryReadReceipt struct {
	AdapterID       string
	BytesWritten    uint64
	SourceBytesRead uint64
	Claims          []VerificationClaim
}

// RepositoryReadAdapter opens exact representation bytes from an
// engine-managed repository. It cannot choose a repository, path, version,
// representation, or verification scope.
type RepositoryReadAdapter interface {
	ID() string
	Capabilities() RepositoryReadCapabilities
	Read(context.Context, RepositoryReadRequest, io.Writer) (RepositoryReadReceipt, error)
}

// StorageObjectRef is a host-issued opaque immutable-object handle. Adapters
// receive neither a caller-selected URL nor ambient credentials.
type StorageObjectRef struct {
	BackendID     string
	PlacementID   string
	ObjectHandle  string
	EncodedSize   uint64
	EncodedDigest string
}

type StorageRangeRequest struct {
	InvocationID      string
	CapabilityScopeID string
	Object            StorageObjectRef
	Range             ByteRange
	Budget            ReadBudget
}

type StorageRangeReceipt struct {
	AdapterID       string
	BytesWritten    uint64
	SourceBytesRead uint64
	Claims          []VerificationClaim
}

// StorageRangeReader performs only bounded reads against one host-selected
// immutable object placement.
type StorageRangeReader interface {
	ID() string
	ReadRange(context.Context, StorageRangeRequest, io.Writer) (StorageRangeReceipt, error)
}

// EncodedRangeSource is a host-enforced capability passed to a decoder. It
// prevents a decoder from opening storage, repositories, files, or networks on
// its own.
type EncodedRangeSource interface {
	Size() uint64
	ReadRange(context.Context, ByteRange, io.Writer) (EncodedRangeReceipt, error)
}

type EncodedRangeReceipt struct {
	BytesWritten    uint64
	SourceBytesRead uint64
	Claims          []VerificationClaim
}

type DecoderCapabilities struct {
	AccessMode             AccessMode
	MinimumReadableUnit    uint64
	SeekCheckpointInterval uint64
	MaximumConcurrency     uint32
}

type DecodeRequest struct {
	InvocationID      string
	CapabilityScopeID string
	Representation    RepresentationRef
	OutputRange       ByteRange
	Budget            ReadBudget
}

type DecodeReceipt struct {
	DecoderID        string
	BytesWritten     uint64
	EncodedBytesRead uint64
	Claims           []VerificationClaim
}

// RepresentationDecoder converts one declared encoded representation into
// its logical stream. The host bounds its source and output and independently
// accepts or rejects all returned evidence.
type RepresentationDecoder interface {
	ID() string
	Capabilities() DecoderCapabilities
	DecodeRange(context.Context, DecodeRequest, EncodedRangeSource, io.Writer) (DecodeReceipt, error)
}

// GatewayExport identifies one read-only export. The absence of mutation
// methods from GatewayHost is deliberate.
type GatewayExport struct {
	ExportID  string
	Snapshot  SnapshotSelector
	PolicyRef string
}

type GatewayEntryOpenRequest struct {
	Access           AccessRequest
	EntryID          string
	RepresentationID string
	Limits           SessionLimits
}

// GatewayHost is bound by the host to GatewayExport's one pre-opened snapshot
// view. The gateway cannot submit another selector or attach a different view
// to an open request. ResolvePath remains on the host-created view, while
// OpenEntrySession performs host authorization and verification for an entry
// in that same view.
type GatewayHost interface {
	View() SnapshotView
	OpenEntrySession(context.Context, GatewayEntryOpenRequest) (ReadSession, error)
}

// NamespaceGatewayAdapter translates an external read-only filesystem,
// WebDAV, SMB, NFS, or S3-compatible protocol into export-bound snapshot-view
// and entry-session calls.
type NamespaceGatewayAdapter interface {
	ID() string
	Protocols() []string
	Serve(context.Context, GatewayExport, GatewayHost) error
}
