package sqlite

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stable identifiers are opaque. The prefix is for operator diagnostics only;
// callers must never derive authorization, ordering, or ownership from it.
const (
	IDPrefixWorkspace         = "wsp"
	IDPrefixSource            = "src"
	IDPrefixScanGeneration    = "scn"
	IDPrefixObservation       = "obs"
	IDPrefixDetectionEvidence = "det"
	IDPrefixPlan              = "pln"
	IDPrefixJob               = "job"
	IDPrefixAuditEvent        = "aud"
	IDPrefixNamespaceRoot     = "nsr"
	IDPrefixNamespaceEntry    = "nse"
	IDPrefixFileVersion       = "fvr"
	IDPrefixRepresentation    = "rep"
	IDPrefixContentExtent     = "ext"
	IDPrefixPhysicalLocator   = "loc"
	IDPrefixEngineReadRef     = "err"
	IDPrefixCaptureBinding    = "crb"
	IDPrefixPublication       = "pub"
	IDPrefixAnnotation        = "ann"
	IDPrefixIndexGeneration   = "idx"
	IDPrefixArtifact          = "art"
	IDPrefixAttempt           = "att"
	IDPrefixMetadataFact      = "mdf"
	IDPrefixDescription       = "dsc"
	IDPrefixSemanticSegment   = "seg"
	IDPrefixProtectionRecord  = "prt"
	IDPrefixRecoveryReference = "rrf"
	IDPrefixExternalBinding   = "sbd"
	IDPrefixExternalLocator   = "elc"
	// Compatibility aliases for callers that use the requirements vocabulary.
	IDPrefixProtection    = IDPrefixProtectionRecord
	IDPrefixRecovery      = IDPrefixRecoveryReference
	IDPrefixSourceBinding = IDPrefixExternalBinding
	IDPrefixSourceLocator = IDPrefixExternalLocator
)

var (
	ErrNotFound            = errors.New("sqlite store: not found")
	ErrConflict            = errors.New("sqlite store: conflict")
	ErrInvalidTransition   = errors.New("sqlite store: invalid state transition")
	ErrIdempotencyConflict = errors.New("sqlite store: idempotency key reused with a different request")
	ErrMigrationDrift      = errors.New("sqlite store: applied migration checksum differs")
	ErrSchemaTooNew        = errors.New("sqlite store: database schema is newer than this binary")
	ErrAuditChain          = errors.New("sqlite store: audit chain predecessor mismatch")
)

// NewStableID returns a random, non-semantic 128-bit identifier with a short
// diagnostic prefix. IDs remain stable when records move between backends.
func NewStableID(prefix string) (string, error) {
	if err := validateIDPrefix(prefix); err != nil {
		return "", err
	}
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate stable id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(value[:]), nil
}

func validateIDPrefix(prefix string) error {
	if len(prefix) < 2 || len(prefix) > 12 {
		return errors.New("stable id prefix must contain 2 to 12 lowercase ASCII characters")
	}
	for _, char := range prefix {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return errors.New("stable id prefix must contain only lowercase ASCII letters and digits")
		}
	}
	return nil
}

func validateStableID(value string) error {
	prefix, encoded, ok := strings.Cut(value, "_")
	if !ok {
		return errors.New("stable id must use prefix_128-bit-hex form")
	}
	if err := validateIDPrefix(prefix); err != nil {
		return err
	}
	if len(encoded) != 32 || encoded != strings.ToLower(encoded) {
		return errors.New("stable id payload must be 32 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return fmt.Errorf("invalid stable id payload: %w", err)
	}
	return nil
}

type Workspace struct {
	ID        string
	Name      string
	Metadata  json.RawMessage
	Revision  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SourceState string

const (
	SourceActive         SourceState = "ACTIVE"
	SourceDecommissioned SourceState = "DECOMMISSIONED"
	SourceLost           SourceState = "LOST"
	SourceQuarantined    SourceState = "QUARANTINED"
)

type Source struct {
	ID                  string
	WorkspaceID         string
	StableKey           string
	Kind                string
	Locator             string
	IdentityFingerprint string
	State               SourceState
	Metadata            json.RawMessage
	Revision            int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ScanState string

const (
	ScanRunning    ScanState = "RUNNING"
	ScanComplete   ScanState = "COMPLETE"
	ScanIncomplete ScanState = "INCOMPLETE"
	ScanFailed     ScanState = "FAILED"
	ScanCancelled  ScanState = "CANCELLED"
)

type ScanGeneration struct {
	ID               string
	WorkspaceID      string
	SourceID         string
	Generation       int64
	ParentID         string
	CaptureSetID     string
	CaptureSetDigest string
	State            ScanState
	FullTraversal    bool
	Summary          json.RawMessage
	StartedAt        time.Time
	FinishedAt       *time.Time
}

type Observation struct {
	ID               string
	WorkspaceID      string
	SourceID         string
	ScanGenerationID string
	PathKey          []byte
	RawPath          []byte
	DisplayPath      string
	EntryType        NamespaceEntryType
	ContentID        string
	FileVersionID    string
	StatDigest       string
	LogicalSize      *int64
	AllocatedSize    *int64
	ReadState        string
	Metadata         json.RawMessage
	ObservedAt       time.Time
}

type DetectionEvidence struct {
	ID                string
	WorkspaceID       string
	ObservationID     string
	DetectorID        string
	DetectorDigest    string
	EvidenceKind      string
	CandidateFormat   string
	CandidateMIME     string
	Confidence        *float64
	ExecutionClass    string
	Evidence          json.RawMessage
	EvidenceDigest    string
	SandboxPolicyHash string
	StartedAt         time.Time
	FinishedAt        time.Time
}

type PlanState string

const (
	PlanDraft      PlanState = "DRAFT"
	PlanReady      PlanState = "READY"
	PlanCommitted  PlanState = "COMMITTED"
	PlanSuperseded PlanState = "SUPERSEDED"
	PlanRejected   PlanState = "REJECTED"
)

type Plan struct {
	ID               string
	WorkspaceID      string
	ScanGenerationID string
	Kind             string
	State            PlanState
	PolicyRevision   string
	Plan             json.RawMessage
	PlanDigest       string
	CreatedAt        time.Time
}

type JobState string

const (
	JobQueued          JobState = "QUEUED"
	JobRunning         JobState = "RUNNING"
	JobWaitingApproval JobState = "WAITING_APPROVAL"
	JobSucceeded       JobState = "SUCCEEDED"
	JobFailed          JobState = "FAILED"
	JobCancelled       JobState = "CANCELLED"
	JobNeedsReconcile  JobState = "NEEDS_RECONCILIATION"
)

type Job struct {
	ID                string
	WorkspaceID       string
	PlanID            string
	Kind              string
	State             JobState
	Input             json.RawMessage
	Checkpoint        json.RawMessage
	Result            json.RawMessage
	ErrorCode         string
	Attempt           int64
	MaxAttempts       int64
	LeaseOwner        string
	LeaseToken        string
	FencingToken      int64
	LeaseUntil        *time.Time
	CancellationAsked bool
	Revision          int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type JobUpdate struct {
	WorkspaceID       string
	JobID             string
	ExpectedRevision  int64
	State             JobState
	Checkpoint        json.RawMessage
	Result            json.RawMessage
	ErrorCode         string
	Attempt           int64
	CancellationAsked bool
	UpdatedAt         time.Time
}

type AuditEvent struct {
	Sequence            int64
	ID                  string
	WorkspaceID         string
	Actor               string
	Action              string
	TargetType          string
	TargetID            string
	RequestID           string
	Attempt             int64
	Source              string
	PolicyRef           string
	ApprovalRef         string
	FencingToken        int64
	Outcome             string
	Details             json.RawMessage
	PreviousEventDigest string
	EventDigest         string
	OccurredAt          time.Time
}

type IdempotencyRequest struct {
	WorkspaceID string
	Scope       string
	Key         string
	RequestHash string
}

type IdempotencyResult struct {
	ResourceType string
	ResourceID   string
	Response     json.RawMessage
}

// NamespaceRoot identifies one immutable logical tree inside a scan/capture
// generation. SnapshotRef is engine-agnostic; it may name a captured volume,
// an engine snapshot, or a future signed publication generation.
type NamespaceRoot struct {
	ID                  string
	WorkspaceID         string
	SourceID            string
	ScanGenerationID    string
	SnapshotRef         string
	Name                string
	RootPathKey         []byte
	FilesystemSemantics string
	AuthorityDigest     string
	Metadata            json.RawMessage
	CreatedAt           time.Time
}

type NamespaceEntryType string

const (
	EntryFile      NamespaceEntryType = "REGULAR_FILE"
	EntryDirectory NamespaceEntryType = "DIRECTORY"
	EntrySymlink   NamespaceEntryType = "SYMLINK"
	EntryFIFO      NamespaceEntryType = "FIFO"
	EntrySocket    NamespaceEntryType = "SOCKET"
	EntryDevice    NamespaceEntryType = "DEVICE"
	EntrySpecial   NamespaceEntryType = "SPECIAL"
)

// NamespaceEntry preserves raw names independently from safe display names.
// FullPathKey is an opaque, filesystem-profile-defined lookup key; it is not a
// slash-joined UTF-8 path and may safely contain arbitrary bytes.
type NamespaceEntry struct {
	ID                   string
	WorkspaceID          string
	RootID               string
	ParentID             string
	ObservationID        string
	RawName              []byte
	DisplayName          string
	FullPathKey          []byte
	EntryType            NamespaceEntryType
	Metadata             json.RawMessage
	ContentID            string
	FileVersionID        string
	SymlinkTargetRaw     []byte
	SymlinkTargetDisplay string
	HardlinkGroupID      string
	LogicalSize          *int64
	AllocatedSize        *int64
	CreatedAt            time.Time
}

type ExtentKind string

const (
	ExtentData ExtentKind = "DATA"
	ExtentHole ExtentKind = "HOLE"
)

type AccessMode string

const (
	AccessRandomNative       AccessMode = "RANDOM_ACCESS_NATIVE"
	AccessRandomCheckpointed AccessMode = "RANDOM_ACCESS_CHECKPOINTED"
	AccessSequentialStream   AccessMode = "SEQUENTIAL_STREAM"
	AccessWholeObjectOnly    AccessMode = "WHOLE_OBJECT_ONLY"
)

type Representation struct {
	ID                        string
	WorkspaceID               string
	ContentID                 string
	DecodedLength             int64
	OwnershipMode             OwnershipMode
	CodecProfileRef           string
	AccessMode                AccessMode
	MinimumReadableUnit       int64
	SeekCheckpointInterval    int64
	WholeReadRequiredToVerify bool
	RecordDigest              string
	Metadata                  json.RawMessage
	CreatedAt                 time.Time
}

type FileVersion struct {
	ID                            string
	WorkspaceID                   string
	ScanGenerationID              string
	ObservationID                 string
	AssetID                       string
	ContentID                     string
	LogicalSize                   int64
	HashingProfile                string
	AuthoritativeRepresentationID string
	ExtentSetDigest               string
	HardlinkGroupID               string
	SparseEvidence                json.RawMessage
	VerificationRef               string
	RecordDigest                  string
	CreatedAt                     time.Time
}

// ContentExtent maps an ordered file-version byte range to an immutable
// representation byte range. Explicit HOLE extents preserve sparse layout.
// It deliberately contains no backend, repository, pack, or engine locator.
type ContentExtent struct {
	ID                   string
	WorkspaceID          string
	FileVersionID        string
	Ordinal              int64
	LogicalOffset        int64
	LogicalLength        int64
	Kind                 ExtentKind
	RepresentationID     string
	RepresentationOffset int64
	ExtentDigest         string
	Metadata             json.RawMessage
	CreatedAt            time.Time
}

type OwnershipMode string

const (
	OwnershipRestoreWeavePacks OwnershipMode = "RESTOREWEAVE_PACKS"
	OwnershipEngineManaged     OwnershipMode = "ENGINE_MANAGED_OBJECTS"
	OwnershipInline            OwnershipMode = "INLINE"
)

type LocatorKind string

const (
	LocatorPackRange LocatorKind = "PACK_RANGE"
	LocatorObject    LocatorKind = "OBJECT"
	LocatorInline    LocatorKind = "INLINE"
)

// PhysicalLocator is a rebuildable native-pack/object projection. It contains
// enough opaque placement and byte-range data for a future adapter to issue a
// bounded read. Restic never uses this type because its private packs remain
// opaque. This SQLite row is neither logical identity nor recovery authority.
type PhysicalLocator struct {
	ID                  string
	WorkspaceID         string
	RepresentationID    string
	ContentID           string
	OwnershipMode       OwnershipMode
	Kind                LocatorKind
	BackendID           string
	RepositoryID        string
	PlacementGeneration int64
	ContainerRef        string
	ByteOffset          *int64
	ByteLength          *int64
	EncodedLength       *int64
	EncodedDigest       string
	AuthorityRef        string
	ReaderProfileRef    string
	Locator             json.RawMessage
	CreatedAt           time.Time
}

// EngineReadRef is the rebuildable mapping from a logical representation to
// an authenticated engine snapshot path. It contains no engine-private pack,
// chunk, or blob coordinates. AccessMode on Representation declares whether a
// requested range can be native or requires bounded sequential amplification.
type EngineReadRef struct {
	ID                        string
	WorkspaceID               string
	RepresentationID          string
	RepositoryID              string
	EngineSnapshotRef         string
	EngineReceiptRef          string
	EnginePathKey             []byte
	PlacementCheckpointID     string
	PlacementCheckpointDigest string
	ReaderProfileRef          string
	Metadata                  json.RawMessage
	CreatedAt                 time.Time
}

type NamespaceNode struct {
	Entry NamespaceEntry
	Depth int64
}

// CaptureRootBinding is the catalog projection of a durable capture-root
// identity. Device and inode are stored as unsigned integers; runtime
// descriptor numbers are never persisted.
type CaptureRootBinding struct {
	ID               string
	WorkspaceID      string
	SourceID         string
	ScanGenerationID string
	CaptureMode      string
	Profile          string
	DisplayPath      string
	DeviceID         uint64
	Inode            uint64
	ConsistencyClaim string
	IdentityDigest   string
	BoundAt          time.Time
	Record           json.RawMessage
}

// Publication is the catalog projection of one committed portable snapshot.
// The snapshot JSON in the repository remains recovery authority.
type Publication struct {
	ID               string
	WorkspaceID      string
	PlanDigest       string
	SnapshotRef      string
	ScanGenerationID string
	BindingID        string
	NamespaceRootID  string
	ManifestDigest   string
	CommittedAt      time.Time
	Metadata         json.RawMessage
}

type AnnotationKind string

const (
	AnnotationTag      AnnotationKind = "TAG"
	AnnotationNote     AnnotationKind = "NOTE"
	AnnotationProgress AnnotationKind = "PROGRESS"
)

// Annotation is a durable whole-subject tag, note, or progress record. It is
// not index state: deleting an FTS generation must not remove these rows.
type Annotation struct {
	ID                  string
	WorkspaceID         string
	SubjectRef          string
	Kind                AnnotationKind
	Body                string
	BodyDigest          string
	Revision            int64
	PredecessorRevision int64
	Tombstoned          bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// AnnotationRevision is an immutable portable revision. Annotation remains the
// current operational projection; this record preserves every admitted body
// and tombstone transition for recovery export.
type AnnotationRevision struct {
	ID              string
	AnnotationID    string
	WorkspaceID     string
	SubjectRef      string
	Kind            AnnotationKind
	Body            string
	BodyDigest      string
	Revision        int64
	PredecessorID   string
	Tombstoned      bool
	HistoryComplete bool
	CreatedAt       time.Time
}

// IndexGeneration is a catalog pointer to one disposable index database file.
// Dimension names the retrieval space; the file is never recovery authority.
type IndexGeneration struct {
	ID              string
	WorkspaceID     string
	SnapshotRef     string
	NamespaceRootID string
	DBPath          string
	Dimension       string
	// ConfigDigest and ProviderProfileDigest bind this disposable projection
	// to the effective host configuration and the immutable provider/profile
	// that produced it. Empty values mean a legacy, unbound row.
	ConfigDigest          string
	ProviderProfileDigest string
	SemanticSpace         string
	CreatedAt             time.Time
}

type ProcessorArtifactState string

const (
	ArtifactAdmitted ProcessorArtifactState = "POLICY_ADMITTED"
	ArtifactRejected ProcessorArtifactState = "REJECTED"
)

// ProcessorArtifact is a host-admitted or host-rejected processor output.
// Intermediate staging states never enter this table.
type ProcessorArtifact struct {
	ID             string
	WorkspaceID    string
	SubjectRef     string
	SnapshotRef    string
	RouteDigest    string
	Stage          string
	CapabilityID   string
	SchemaRef      string
	State          ProcessorArtifactState
	AuthorityClass string
	LifecycleClass string
	MediaType      string
	ByteLength     int64
	Digest         string
	Body           string
	AttemptID      string
	FenceToken     int64
	ProducerDigest string
	Envelope       json.RawMessage
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ProcessorAttempt is the append-only outcome of one host-routed processor
// capability invocation. It remains durable even when no artifact is
// admitted, so replay and audit can distinguish unavailable, inapplicable,
// failed, and cancelled processing from an absent record.
type ProcessorAttempt struct {
	ID              string
	WorkspaceID     string
	SubjectRef      string
	SnapshotRef     string
	RouteDigest     string
	Route           json.RawMessage
	Stage           string
	CapabilityID    string
	Status          string
	ReasonCode      string
	Reason          string
	Provenance      json.RawMessage
	FenceToken      int64
	ProcessorDigest string
	CreatedAt       time.Time
	FinishedAt      time.Time
}

// ProtectionMode is the explicit policy selected for a subject. In
// particular, LINK_ONLY and METADATA_ONLY are deliberate outcomes and must
// never be inferred from a missing representation row.
type ProtectionMode string

const (
	ProtectionStoreExact                         ProtectionMode = "STORE_EXACT"
	ProtectionStoreExactWithExternalFallback     ProtectionMode = "STORE_EXACT_WITH_EXTERNAL_FALLBACK"
	ProtectionLinkOnly                           ProtectionMode = "LINK_ONLY"
	ProtectionMetadataOnly                       ProtectionMode = "METADATA_ONLY"
	ProtectionModeStoreExact                                    = ProtectionStoreExact
	ProtectionModeStoreExactWithExternalFallback                = ProtectionStoreExactWithExternalFallback
	ProtectionModeLinkOnly                                      = ProtectionLinkOnly
	ProtectionModeMetadataOnly                                  = ProtectionMetadataOnly
)

// ProtectionOutcome is the user-visible recovery health claim. It is kept
// separate from ProtectionMode because a selected policy and its current
// validation state answer different questions.
type ProtectionOutcome string

const (
	ProtectionExactProtected ProtectionOutcome = "EXACT_PROTECTED"
	// ProtectionExact is the compact product name for the existing
	// EXACT_PROTECTED value. Keep the persisted value stable for v8 records.
	ProtectionExact                        ProtectionOutcome = ProtectionExactProtected
	ProtectionExactFallback                ProtectionOutcome = "EXACT_FALLBACK"
	ProtectionExternalReplayable           ProtectionOutcome = "EXTERNAL_REPLAYABLE"
	ProtectionLinkOnlyUnprotected          ProtectionOutcome = "LINK_ONLY_UNPROTECTED"
	ProtectionExplicitlyUnprotected        ProtectionOutcome = "EXPLICITLY_UNPROTECTED"
	ProtectionBlocked                      ProtectionOutcome = "BLOCKED"
	ProtectionUnavailable                  ProtectionOutcome = "UNAVAILABLE"
	ProtectionOutcomeExactProtected                          = ProtectionExactProtected
	ProtectionOutcomeExactFallback                           = ProtectionExactFallback
	ProtectionOutcomeExternalReplayable                      = ProtectionExternalReplayable
	ProtectionOutcomeLinkOnlyUnprotected                     = ProtectionLinkOnlyUnprotected
	ProtectionOutcomeExplicitlyUnprotected                   = ProtectionExplicitlyUnprotected
	ProtectionOutcomeBlocked                                 = ProtectionBlocked
	ProtectionOutcomeUnavailable                             = ProtectionUnavailable
)

// ProtectionRecord is the durable policy and health fact for one subject.
// SubjectRef is intentionally opaque: the catalog can attach protection to a
// namespace entry, an imported subject, or a future subject implementation
// without making the storage model depend on one presentation.
type ProtectionRecord struct {
	ID                    string
	WorkspaceID           string
	SubjectRef            string
	Mode                  ProtectionMode
	Outcome               ProtectionOutcome
	ExpectedContentID     string
	ExpectedLogicalLength *int64
	LocalRepresentationID string
	PolicyDecisionRef     string
	LastVerificationRef   string
	LastVerifiedAt        *time.Time
	Revision              int64
	Metadata              json.RawMessage
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type RecoveryReferenceKind string

const (
	RecoveryExactRepresentation          RecoveryReferenceKind = "EXACT_REPRESENTATION"
	RecoveryExactReversible              RecoveryReferenceKind = "EXACT_REVERSIBLE"
	RecoveryExternalLocator              RecoveryReferenceKind = "EXTERNAL_LOCATOR"
	RecoveryUserRecipe                   RecoveryReferenceKind = "USER_RECIPE"
	RecoveryReferenceExactRepresentation                       = RecoveryExactRepresentation
	RecoveryReferenceExactReversible                           = RecoveryExactReversible
	RecoveryReferenceExternalLocator                           = RecoveryExternalLocator
	RecoveryReferenceUserRecipe                                = RecoveryUserRecipe
)

type RecoveryClaim string

const (
	RecoveryClaimRestoreVerified     RecoveryClaim = "RESTORE_VERIFIED"
	RecoveryClaimExternalReplayable  RecoveryClaim = "EXTERNAL_REPLAYABLE"
	RecoveryClaimLinkOnlyUnprotected RecoveryClaim = "LINK_ONLY_UNPROTECTED"
	RecoveryClaimUnavailable         RecoveryClaim = "UNAVAILABLE"
)

// RecoveryReference is one ordered, verifiable route to recover or reacquire
// a subject. Exactly one route may be selected by kind: local representation,
// external binding, or a user-supplied recipe. Recipe and verification JSON
// are canonical portable records, not opaque backend state.
type RecoveryReference struct {
	ID                    string
	WorkspaceID           string
	ProtectionRecordID    string
	SubjectRef            string
	Kind                  RecoveryReferenceKind
	Priority              int64
	Claim                 RecoveryClaim
	ExpectedContentID     string
	ExpectedLogicalLength *int64
	RepresentationID      string
	ExternalBindingID     string
	CodecProfileRef       string
	Recipe                json.RawMessage
	Verification          json.RawMessage
	Status                string
	LastValidatedAt       *time.Time
	ExpiresAt             *time.Time
	PolicyRef             string
	RightsEvidenceRef     string
	CredentialRef         string
	OperatorDecisionRef   string
	RecordDigest          string
	Metadata              json.RawMessage
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// SourceBinding is the immutable provider identity used to reacquire content.
// ExternalBinding is the storage-facing name; the alias keeps the terminology
// used by the requirements available to callers.
type ExternalBinding struct {
	ID                string
	WorkspaceID       string
	SubjectRef        string
	ProviderKind      string
	StableIdentity    string
	Revision          int64
	BindingDigest     string
	CredentialRef     string
	RightsEvidenceRef string
	Metadata          json.RawMessage
	CreatedAt         time.Time
}

type SourceBinding = ExternalBinding

// ExternalLocator is one typed, ordered locator inside an ExternalBinding.
// Locator is retained as supplied (credentials must be represented by
// CredentialRef, never embedded in this field).
type ExternalLocator struct {
	ID                    string
	WorkspaceID           string
	BindingID             string
	Revision              int64
	Priority              int64
	Kind                  string
	Locator               string
	DisplayLocator        string
	ExpectedContentID     string
	ExpectedLogicalLength *int64
	CredentialRef         string
	RightsEvidenceRef     string
	Availability          string
	ValidationStatus      string
	ExpiresAt             *time.Time
	LastValidatedAt       *time.Time
	ValidationRef         string
	Metadata              json.RawMessage
	CreatedAt             time.Time
}

// MetadataFact is one namespaced, provenance-bearing fact about a subject.
// Keeping facts as rows rather than flattening them into an index document
// lets structured filters survive index rebuilds and provider replacement.
type MetadataFact struct {
	ID             string
	WorkspaceID    string
	SubjectRef     string
	Namespace      string
	Key            string
	Value          json.RawMessage
	ValueType      string
	AuthorityClass string
	SourceRef      string
	Confidence     *float64
	Revision       int64
	CreatedAt      time.Time
}

// DescriptionKind identifies who or what authored a durable long-form
// description. Generated text never overwrites user-authored revisions.
type DescriptionKind string

const (
	DescriptionUser       DescriptionKind = "USER"
	DescriptionImported   DescriptionKind = "IMPORTED"
	DescriptionExtracted  DescriptionKind = "EXTRACTED"
	DescriptionAISummary  DescriptionKind = "AI_SUMMARY"
	DescriptionAIAnalysis DescriptionKind = "AI_ANALYSIS"
)

// DescriptionDocument is the durable source text used by lexical and
// semantic discovery. Body remains authoritative; vectors are rebuildable.
type DescriptionDocument struct {
	ID              string
	WorkspaceID     string
	SubjectRef      string
	Kind            DescriptionKind
	Title           string
	Language        string
	Body            string
	BodyDigest      string
	SourceRef       string
	ProducerProfile string
	// ConfigDigest binds this revision to the resolved host configuration.
	// ProducerProfileDigest identifies the immutable producer/profile input;
	// both are distinct from the durable body digest.
	ConfigDigest          string
	ProducerProfileDigest string
	Confidence            *float64
	Coverage              *float64
	Visibility            string
	Accepted              bool
	Revision              int64
	PredecessorID         string
	Metadata              json.RawMessage
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// SemanticSegment is an ordered chunk of a document. Segment text and its
// provenance are durable; an embedding generation may be deleted/rebuilt.
type SemanticSegment struct {
	ID                        string
	WorkspaceID               string
	DocumentID                string
	SubjectRef                string
	DocumentRevision          int64
	Ordinal                   int64
	Text                      string
	TextDigest                string
	Language                  string
	Section                   string
	SourceSpan                json.RawMessage
	Metadata                  json.RawMessage
	SegmentationProfileDigest string
	CreatedAt                 time.Time
}
