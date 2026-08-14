// Package scanner inventories a filesystem tree without following symlinks.
//
// A scan only appends observations. It deliberately has no deletion or
// tombstone API: absence becomes meaningful only after a higher layer compares
// two complete generations for the same source identity.
package scanner

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"
)

const (
	TraversalVersion  = "depth-first-raw-name-v1"
	MetadataVersion   = "filesystem-metadata-v1"
	HashVersion       = "sha256-stream-v1"
	PathIDVersion     = "source-path-sha256-v1"
	HardLinkIDVersion = "generation-device-inode-sha256-v1"
)

var (
	ErrInvalidRequest = errors.New("invalid scan request")
	ErrIncomplete     = errors.New("scan is incomplete")
	ErrSink           = errors.New("scan sink failure")
)

// ScanState is the authority-safe terminal state of a scan attempt. Only
// ScanComplete is eligible to become an authoritative generation.
type ScanState string

const (
	ScanComplete   ScanState = "COMPLETE"
	ScanIncomplete ScanState = "INCOMPLETE"
	ScanCancelled  ScanState = "CANCELLED"
	ScanFailed     ScanState = "FAILED"
)

// CaptureMode records which traversal basis a scan used. PATH_STRING is the
// legacy ambient traversal; ROOTED_FD is the descriptor-rooted capture whose
// observations are eligible to become authoritative.
type CaptureMode string

const (
	CaptureModePathString CaptureMode = "PATH_STRING"
	CaptureModeRootedFD   CaptureMode = "ROOTED_FD"
)

// EntryKind is derived from lstat metadata. Symlinks and special files are
// represented explicitly and are never opened as ordinary file content.
type EntryKind string

const (
	KindUnknown     EntryKind = "UNKNOWN"
	KindRegularFile EntryKind = "REGULAR_FILE"
	KindDirectory   EntryKind = "DIRECTORY"
	KindSymlink     EntryKind = "SYMLINK"
	KindNamedPipe   EntryKind = "NAMED_PIPE"
	KindSocket      EntryKind = "SOCKET"
	KindBlockDevice EntryKind = "BLOCK_DEVICE"
	KindCharDevice  EntryKind = "CHAR_DEVICE"
	KindIrregular   EntryKind = "IRREGULAR"
)

type EntryState string

const (
	EntryComplete        EntryState = "COMPLETE"
	EntryBoundarySkipped EntryState = "BOUNDARY_SKIPPED"
	EntryFailed          EntryState = "FAILED"
	EntryUnstable        EntryState = "UNSTABLE"
	EntryCancelled       EntryState = "CANCELLED"
)

type IssueStage string

const (
	StageLstat       IssueStage = "LSTAT"
	StageBoundary    IssueStage = "BOUNDARY"
	StageOpen        IssueStage = "OPEN_NOFOLLOW"
	StageRead        IssueStage = "READ"
	StageReadDir     IssueStage = "READ_DIRECTORY"
	StageReadlink    IssueStage = "READLINK"
	StagePostStat    IssueStage = "POST_STAT"
	StageStability   IssueStage = "STABILITY"
	StageDetection   IssueStage = "DETECTION"
	StageEnumeration IssueStage = "ENUMERATION"
)

// Issue is evidence, not an authorization to infer deletion.
type Issue struct {
	Stage   IssueStage `json:"stage"`
	Code    string     `json:"code"`
	Message string     `json:"message,omitempty"`
}

// MetadataSnapshot contains portable lstat/fstat facts plus a small set of
// best-effort native facts. Each Known field is false when the platform does
// not expose the corresponding values through fs.FileInfo.Sys().
type MetadataSnapshot struct {
	Version         string    `json:"version"`
	Size            int64     `json:"size"`
	Mode            uint32    `json:"mode"`
	ModTime         time.Time `json:"mod_time"`
	IdentityKnown   bool      `json:"identity_known"`
	DeviceID        uint64    `json:"device_id,omitempty"`
	Inode           uint64    `json:"inode,omitempty"`
	LinkCountKnown  bool      `json:"link_count_known"`
	LinkCount       uint64    `json:"link_count,omitempty"`
	OwnershipKnown  bool      `json:"ownership_known"`
	UID             uint64    `json:"uid,omitempty"`
	GID             uint64    `json:"gid,omitempty"`
	DeviceTypeKnown bool      `json:"device_type_known"`
	DeviceType      uint64    `json:"device_type,omitempty"`
	BlocksKnown     bool      `json:"blocks_known"`
	Blocks          uint64    `json:"blocks,omitempty"`
}

type ContentDigest struct {
	Algorithm string `json:"algorithm"`
	Version   string `json:"version"`
	Hex       string `json:"hex"`
	BytesRead int64  `json:"bytes_read"`
	ContentID string `json:"content_id"`
}

type SymlinkFacts struct {
	RawTarget    []byte `json:"raw_target"`
	TargetSHA256 string `json:"target_sha256"`
}

type HardLinkState string

const (
	HardLinkNotApplicable HardLinkState = "NOT_APPLICABLE"
	HardLinkUnknown       HardLinkState = "UNKNOWN"
	HardLinkSingle        HardLinkState = "SINGLE_LINK"
	HardLinkMultiple      HardLinkState = "MULTIPLE_LINKS"
)

type HardLinkFacts struct {
	State          HardLinkState `json:"state"`
	GroupIDVersion string        `json:"group_id_version,omitempty"`
	GroupID        string        `json:"group_id,omitempty"`
	LinkCount      uint64        `json:"link_count,omitempty"`
}

// SparseState is intentionally conservative. stat blocks below logical size
// are only a hint because compression, clones, and filesystem accounting can
// produce the same observation. Exact sparse restoration needs an extent-map
// provider in a later layer.
type SparseState string

const (
	SparseNotApplicable       SparseState = "NOT_APPLICABLE"
	SparseUnknown             SparseState = "UNKNOWN"
	SparseNotIndicated        SparseState = "NOT_INDICATED"
	SparseAllocationBelowSize SparseState = "ALLOCATION_BELOW_LOGICAL_SIZE"
)

type SparseFacts struct {
	State             SparseState `json:"state"`
	LogicalBytes      int64       `json:"logical_bytes,omitempty"`
	AllocatedBytes    int64       `json:"allocated_bytes,omitempty"`
	Evidence          string      `json:"evidence,omitempty"`
	ExtentMapCaptured bool        `json:"extent_map_captured"`
}

type BoundaryAction string

const (
	BoundaryInclude BoundaryAction = "INCLUDE"
	BoundarySkip    BoundaryAction = "SKIP"
)

type BoundaryObservation struct {
	Checked bool           `json:"checked"`
	Action  BoundaryAction `json:"action"`
	Reason  string         `json:"reason,omitempty"`
}

type DetectionState string

const (
	DetectionNotRequested DetectionState = "NOT_REQUESTED"
	DetectionSucceeded    DetectionState = "SUCCEEDED"
	DetectionFailed       DetectionState = "FAILED"
)

type DetectionEvidence struct {
	Method string `json:"method"`
	Value  string `json:"value"`
}

type DetectionResult struct {
	DetectorID      string              `json:"detector_id,omitempty"`
	DetectorVersion string              `json:"detector_version,omitempty"`
	FormatID        string              `json:"format_id,omitempty"`
	MediaType       string              `json:"media_type,omitempty"`
	Confidence      float64             `json:"confidence,omitempty"`
	Evidence        []DetectionEvidence `json:"evidence,omitempty"`
}

type DetectionObservation struct {
	State  DetectionState  `json:"state"`
	Result DetectionResult `json:"result,omitempty"`
}

// EntryRecord is one immutable namespace observation. RawName and
// RawRelativePath preserve the operating-system bytes on Unix, including
// invalid UTF-8. Display paths remain convenience fields and are not identity.
type EntryRecord struct {
	GenerationID    string               `json:"generation_id"`
	SourceID        string               `json:"source_id"`
	PathID          string               `json:"path_id"`
	ParentPathID    string               `json:"parent_path_id,omitempty"`
	AbsolutePath    string               `json:"absolute_path"`
	RelativePath    string               `json:"relative_path"`
	Name            string               `json:"name"`
	RawName         []byte               `json:"raw_name"`
	RawRelativePath []byte               `json:"raw_relative_path"`
	Kind            EntryKind            `json:"kind"`
	State           EntryState           `json:"state"`
	Before          *MetadataSnapshot    `json:"before,omitempty"`
	After           *MetadataSnapshot    `json:"after,omitempty"`
	Content         *ContentDigest       `json:"content,omitempty"`
	Symlink         *SymlinkFacts        `json:"symlink,omitempty"`
	HardLink        HardLinkFacts        `json:"hard_link"`
	Sparse          SparseFacts          `json:"sparse"`
	Boundary        BoundaryObservation  `json:"boundary"`
	Detection       DetectionObservation `json:"detection"`
	Issues          []Issue              `json:"issues,omitempty"`
}

type ScanRequest struct {
	GenerationID string `json:"generation_id"`
	SourceID     string `json:"source_id"`
	Root         string `json:"root"`
}

type ScanStart struct {
	GenerationID     string      `json:"generation_id"`
	SourceID         string      `json:"source_id"`
	Root             string      `json:"root"`
	StartedAt        time.Time   `json:"started_at"`
	TraversalVersion string      `json:"traversal_version"`
	PathIDVersion    string      `json:"path_id_version"`
	CaptureMode      CaptureMode `json:"capture_mode"`
}

type ScanResult struct {
	GenerationID      string      `json:"generation_id"`
	SourceID          string      `json:"source_id"`
	Root              string      `json:"root"`
	State             ScanState   `json:"state"`
	StartedAt         time.Time   `json:"started_at"`
	FinishedAt        time.Time   `json:"finished_at"`
	CaptureMode       CaptureMode `json:"capture_mode"`
	Entries           uint64      `json:"entries"`
	RegularFiles      uint64      `json:"regular_files"`
	Directories       uint64      `json:"directories"`
	Symlinks          uint64      `json:"symlinks"`
	SpecialFiles      uint64      `json:"special_files"`
	BoundarySkipped   uint64      `json:"boundary_skipped"`
	BoundaryUnchecked uint64      `json:"boundary_unchecked"`
	FailedEntries     uint64      `json:"failed_entries"`
	UnstableEntries   uint64      `json:"unstable_entries"`
	DetectionFailures uint64      `json:"detection_failures"`
	BytesHashed       int64       `json:"bytes_hashed"`
}

// ReadStatCloser is a no-follow regular-file handle supplied by FileSystem.
type ReadStatCloser interface {
	io.Reader
	io.Closer
	Stat() (fs.FileInfo, error)
}

// ReadDirStatCloser is a no-follow directory handle supplied by FileSystem.
type ReadDirStatCloser interface {
	io.Closer
	Stat() (fs.FileInfo, error)
	ReadDir(n int) ([]fs.DirEntry, error)
}

// FileSystem separates traversal from the concrete host filesystem. The two
// open methods must reject a symlink at the final path component.
type FileSystem interface {
	Lstat(path string) (fs.FileInfo, error)
	Readlink(path string) (string, error)
	OpenRegularNoFollow(path string) (ReadStatCloser, error)
	OpenDirNoFollow(path string) (ReadDirStatCloser, error)
}

// BoundaryChecker is the placeholder for mount, volume, capture-set, and
// source-identity enforcement. A checker error fails closed for that subtree.
type BoundaryChecker interface {
	CheckBoundary(ctx context.Context, candidate BoundaryCandidate) (BoundaryDecision, error)
}

type BoundaryCandidate struct {
	SourceID      string
	Root          string
	AbsolutePath  string
	RelativePath  string
	RootMetadata  MetadataSnapshot
	EntryMetadata MetadataSnapshot
}

type BoundaryDecision struct {
	Action BoundaryAction
	Reason string
}

// Detector receives only stable metadata, the accepted content digest, and a
// bounded prefix captured during hashing. It never receives an ambient path it
// can reopen, keeping filesystem authority in the scanner host.
type Detector interface {
	Detect(ctx context.Context, input DetectionInput) (DetectionResult, error)
}

type DetectionInput struct {
	GenerationID    string
	SourceID        string
	PathID          string
	ParentPathID    string
	RelativePath    string
	RawName         []byte
	RawRelativePath []byte
	Metadata        MetadataSnapshot
	Content         ContentDigest
	Probe           []byte
}

// Sink is append-only from the scanner's perspective. FinishScan records the
// terminal attempt state; it does not turn an incomplete scan into deletion
// evidence.
type Sink interface {
	BeginScan(ctx context.Context, start ScanStart) error
	PutEntry(ctx context.Context, entry EntryRecord) error
	FinishScan(ctx context.Context, result ScanResult) error
}
