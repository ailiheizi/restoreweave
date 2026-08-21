package command

import "encoding/json"

type StatusData struct {
	Controller    string            `json:"controller"`
	ConfigDigest  string            `json:"config_digest,omitempty"`
	Catalog       CatalogStatus     `json:"catalog"`
	Identify      IdentifyStatus    `json:"identify"`
	Repository    *RepositoryStatus `json:"repository,omitempty"`
	Publications  int               `json:"publications,omitempty"`
	Plans         int               `json:"plans,omitempty"`
	RecentPlans   []PlanSummary     `json:"recent_plans,omitempty"`
	Jobs          int               `json:"jobs,omitempty"`
	RecentJobs    []JobSummary      `json:"recent_jobs,omitempty"`
	OpenHandles   int               `json:"open_handles,omitempty"`
	ReapedHandles int               `json:"reaped_handles,omitempty"`
	Listen        string            `json:"listen,omitempty"`
	Unimplemented []string          `json:"unimplemented"`
}

type JobSummary struct {
	JobID       string `json:"job_id"`
	WorkspaceID string `json:"workspace_id"`
	PlanID      string `json:"plan_id,omitempty"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type PlanSummary struct {
	PlanID      string `json:"plan_id"`
	WorkspaceID string `json:"workspace_id"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	PlanDigest  string `json:"plan_digest,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type CatalogStatus struct {
	Path string `json:"path"`
	OK   bool   `json:"ok"`
}

type RepositoryStatus struct {
	Path               string `json:"path,omitempty"`
	RepositoryProfile  string `json:"repository_profile,omitempty"`
	CompressionProfile string `json:"compression_profile,omitempty"`
	OK                 bool   `json:"ok"`
	Snapshots          int    `json:"snapshots"`
}

type IdentifyStatus struct {
	ID          string `json:"id"`
	RulesDigest string `json:"rules_digest"`
}

type CapabilityListData struct {
	Capabilities []Capability `json:"capabilities"`
}

type Capability struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

type SnapshotListData struct {
	Snapshots []SnapshotSummary `json:"snapshots"`
}

type SnapshotSummary struct {
	SnapshotRef    string `json:"snapshot_ref"`
	CreatedAt      string `json:"created_at,omitempty"`
	DisplayPath    string `json:"display_path,omitempty"`
	ManifestDigest string `json:"manifest_digest,omitempty"`
}

type PlanIngestData struct {
	WorkspaceID         string                         `json:"workspace_id"`
	SourceID            string                         `json:"source_id"`
	ScanID              string                         `json:"scan_id"`
	RootID              string                         `json:"root_id"`
	SnapshotRef         string                         `json:"snapshot_ref"`
	ManifestDigest      string                         `json:"manifest_digest"`
	PlanID              string                         `json:"plan_id,omitempty"`
	PlanDigest          string                         `json:"plan_digest,omitempty"`
	JobID               string                         `json:"job_id,omitempty"`
	ProtectionMode      string                         `json:"protection_mode"`
	ProtectionDigest    string                         `json:"protection_digest,omitempty"`
	FileProtection      map[string]string              `json:"file_protection,omitempty"`
	ProtectionDecisions []IngestProtectionDecisionData `json:"protection_decisions,omitempty"`
	BlockedEntries      []IngestPlanIssueData          `json:"blocked_entries,omitempty"`
	Files               int                            `json:"files"`
	Bytes               int64                          `json:"bytes"`
	LocalFiles          int                            `json:"local_files"`
	LocalBytes          int64                          `json:"local_bytes"`
	NewBytes            int64                          `json:"new_bytes"`
	LinkOnlyFiles       int                            `json:"link_only_files"`
	LocatorCount        int                            `json:"locator_count"`
	State               string                         `json:"state,omitempty"`
	Executable          bool                           `json:"executable"`
	ConfigDigest        string                         `json:"config_digest,omitempty"`
	SourceBasisDigest   string                         `json:"source_basis_digest,omitempty"`
}

type IngestProtectionDecisionData struct {
	RelativePath         string `json:"relative_path"`
	Mode                 string `json:"mode"`
	PlannedOutcome       string `json:"planned_outcome"`
	ReasonCode           string `json:"reason_code"`
	ExpectedContentID    string `json:"expected_content_id"`
	ExpectedLogicalBytes int64  `json:"expected_logical_bytes"`
	LocatorCount         int    `json:"locator_count"`
}

type IngestPlanIssueData struct {
	RelativePath   string `json:"relative_path"`
	Mode           string `json:"mode"`
	PlannedOutcome string `json:"planned_outcome"`
	State          string `json:"state"`
	ReasonCode     string `json:"reason_code"`
	Message        string `json:"message,omitempty"`
}

// IngestLocatorInput is the command-ABI projection of one external recovery
// locator. Path is relative to the capture root and may be omitted only for a
// capture containing exactly one regular file.
type IngestLocatorInput struct {
	Path              string `json:"path,omitempty"`
	Kind              string `json:"kind,omitempty"`
	Locator           string `json:"locator"`
	DisplayLocator    string `json:"display_locator,omitempty"`
	CredentialRef     string `json:"credential_ref,omitempty"`
	RightsEvidenceRef string `json:"rights_evidence_ref,omitempty"`
}

type JobEventData struct {
	Sequence   int64           `json:"sequence"`
	EventID    string          `json:"event_id"`
	Action     string          `json:"action"`
	Outcome    string          `json:"outcome"`
	OccurredAt string          `json:"occurred_at,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
}

type JobEventsData struct {
	JobID        string         `json:"job_id"`
	JobState     string         `json:"job_state"`
	Events       []JobEventData `json:"events"`
	NextSequence int64          `json:"next_sequence"`
	Terminal     bool           `json:"terminal"`
}

type JobCancelData struct {
	JobID             string `json:"job_id"`
	JobState          string `json:"job_state"`
	Cancelled         bool   `json:"cancelled"`
	AlreadyTerminal   bool   `json:"already_terminal"`
	CancellationAsked bool   `json:"cancellation_asked"`
}

const PlanReceiptSchema = "org.restoreweave.plan.v1"

type PlanGetData struct {
	PlanID            string          `json:"plan_id"`
	WorkspaceID       string          `json:"workspace_id"`
	Kind              string          `json:"kind"`
	State             string          `json:"state"`
	PlanDigest        string          `json:"plan_digest"`
	Applied           bool            `json:"applied"`
	Executable        bool            `json:"executable"`
	Abandoned         bool            `json:"abandoned,omitempty"`
	BasePlanID        string          `json:"base_plan_id,omitempty"`
	Plan              json.RawMessage `json:"plan,omitempty"`
	CreatedAt         string          `json:"created_at,omitempty"`
	SourceBasisDigest string          `json:"source_basis_digest,omitempty"`
}

type PlanReviseData struct {
	PlanID      string `json:"plan_id"`
	PlanDigest  string `json:"plan_digest"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	BasePlanID  string `json:"base_plan_id"`
	BaseDigest  string `json:"base_plan_digest"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	Applied     bool   `json:"applied"`
	Executable  bool   `json:"executable"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
}

type PlanAbandonData struct {
	PlanID           string `json:"plan_id"`
	PlanDigest       string `json:"plan_digest"`
	AbandonedPlanID  string `json:"abandoned_plan_id"`
	AlreadyAbandoned bool   `json:"already_abandoned"`
}

type DoctorData struct {
	OK     bool          `json:"ok"`
	Checks []DoctorCheck `json:"checks"`
}

type DoctorCheck struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Scope   string `json:"scope,omitempty"`
	Message string `json:"message"`
}

type PlanApplyData struct {
	PlanID              string                         `json:"plan_id"`
	PlanDigest          string                         `json:"plan_digest"`
	AlreadyApplied      bool                           `json:"already_applied"`
	SnapshotRef         string                         `json:"snapshot_ref,omitempty"`
	JobID               string                         `json:"job_id,omitempty"`
	State               string                         `json:"state,omitempty"`
	WorkspaceID         string                         `json:"workspace_id,omitempty"`
	SourceID            string                         `json:"source_id,omitempty"`
	ScanID              string                         `json:"scan_id,omitempty"`
	RootID              string                         `json:"root_id,omitempty"`
	ManifestDigest      string                         `json:"manifest_digest,omitempty"`
	ProtectionDigest    string                         `json:"protection_digest,omitempty"`
	ProtectionDecisions []IngestProtectionDecisionData `json:"protection_decisions,omitempty"`
	Destination         string                         `json:"destination,omitempty"`
	Files               int                            `json:"files,omitempty"`
	Bytes               int64                          `json:"bytes,omitempty"`
	Warnings            []string                       `json:"warnings,omitempty"`
}

type PlanRestoreData struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotRef string `json:"snapshot_ref"`
	Destination string `json:"destination,omitempty"`
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
	Wrote       bool   `json:"wrote"`
	PlanID      string `json:"plan_id,omitempty"`
	PlanDigest  string `json:"plan_digest,omitempty"`
	State       string `json:"state,omitempty"`
	Executable  bool   `json:"executable"`
}

const (
	VerifyAuthenticatedMetadata = "authenticated-metadata"
	VerifySampledContent        = "sampled-content"
	VerifyFullBytes             = "full-bytes"
	VerifyRestoreDrill          = "restore-drill"
	VerifyCleanRecovery         = "clean-recovery"
)

type SnapshotVerifyData struct {
	SnapshotRef     string `json:"snapshot_ref"`
	Mode            string `json:"mode,omitempty"`
	AcceptedLevel   string `json:"accepted_level,omitempty"`
	Entries         int    `json:"entries"`
	Files           int    `json:"files"`
	Bytes           int64  `json:"bytes"`
	AttemptedFiles  int    `json:"attempted_files,omitempty"`
	AttemptedBytes  int64  `json:"attempted_bytes,omitempty"`
	PassedFiles     int    `json:"passed_files,omitempty"`
	PassedBytes     int64  `json:"passed_bytes,omitempty"`
	OK              bool   `json:"ok"`
	RestoreVerified bool   `json:"restore_verified,omitempty"`
	CatalogUsed     bool   `json:"catalog_used"`
}

const (
	AnnotationConflictFail         = "fail"
	AnnotationConflictKeepLocal    = "keep-local"
	AnnotationConflictKeepImported = "keep-imported"
)

const (
	DiffAdded           = "added"
	DiffRemoved         = "removed"
	DiffMoved           = "moved"
	DiffContentChanged  = "content_changed"
	DiffMetadataChanged = "metadata_changed"
	DiffTypeChanged     = "type_changed"
)

type SnapshotDiffData struct {
	FromSnapshotRef string             `json:"from_snapshot_ref"`
	ToSnapshotRef   string             `json:"to_snapshot_ref"`
	Changes         []SnapshotDiffItem `json:"changes"`
}

type SnapshotDiffItem struct {
	Kind          string `json:"kind"`
	Path          string `json:"path,omitempty"`
	FromPath      string `json:"from_path,omitempty"`
	ToPath        string `json:"to_path,omitempty"`
	EntryType     string `json:"entry_type,omitempty"`
	FromType      string `json:"from_type,omitempty"`
	ToType        string `json:"to_type,omitempty"`
	ContentID     string `json:"content_id,omitempty"`
	FromContentID string `json:"from_content_id,omitempty"`
	ToContentID   string `json:"to_content_id,omitempty"`
}

type RecoveryExportData struct {
	SnapshotRef         string `json:"snapshot_ref"`
	Schema              string `json:"schema"`
	ManifestDigest      string `json:"manifest_digest"`
	ArtifactPath        string `json:"artifact_path"`
	Length              int64  `json:"length"`
	Files               int    `json:"files"`
	Bytes               int64  `json:"bytes"`
	IndependentlyStored bool   `json:"independently_stored"`
}

// RecoveryAnchorExportData is the public result of recovery.anchor.export.
// It intentionally exposes only verification metadata; the exported file
// itself contains the public key and never contains private signing material.
type RecoveryAnchorExportData struct {
	Schema            string `json:"schema"`
	ArtifactPath      string `json:"artifact_path"`
	PublicationDomain string `json:"publication_domain"`
	WriterIdentity    string `json:"writer_identity"`
	KeyID             string `json:"key_id"`
	Algorithm         string `json:"algorithm"`
	PublicKeyDigest   string `json:"public_key_digest"`
}

const (
	CapabilityAvailable   = "AVAILABLE"
	CapabilityUnavailable = "UNAVAILABLE"
	CapabilityExternal    = "EXTERNAL"
)

// NamespaceListData is the namespace.list payload. Entries are returned in
// deterministic raw-name order exactly as the catalog stores them.
type NamespaceListData struct {
	RootID   string               `json:"root_id"`
	ParentID string               `json:"parent_id,omitempty"`
	Entries  []NamespaceEntryData `json:"entries"`
}

// NamespaceEntryData is the client-visible projection of one namespace entry.
// Raw byte names are never placed here; they appear only in readlink targets.
type NamespaceEntryData struct {
	ID                   string `json:"entry_id"`
	RootID               string `json:"root_id"`
	ParentID             string `json:"parent_id,omitempty"`
	DisplayName          string `json:"display_name"`
	EntryType            string `json:"entry_type"`
	ContentID            string `json:"content_id,omitempty"`
	FileVersionID        string `json:"file_version_id,omitempty"`
	HardlinkGroupID      string `json:"hardlink_group_id,omitempty"`
	LogicalSize          *int64 `json:"logical_size,omitempty"`
	AllocatedSize        *int64 `json:"allocated_size,omitempty"`
	SymlinkTargetDisplay string `json:"symlink_target_display,omitempty"`
}

// NamespaceStatData is the namespace.stat payload.
type NamespaceStatData struct {
	Entry NamespaceEntryData `json:"entry"`
}

// NamespaceResolveData is the namespace.resolve payload. PathRef is the
// catalog entry id. Resolve does not follow symbolic links.
type NamespaceResolveData struct {
	WorkspaceID string             `json:"workspace_id"`
	RootID      string             `json:"root_id"`
	Path        string             `json:"path"`
	PathRef     string             `json:"path_ref"`
	Entry       NamespaceEntryData `json:"entry"`
}

// NamespaceReadlinkData is the namespace.readlink payload. TargetRaw carries
// the captured raw symlink target bytes (JSON base64); callers must not
// interpret it as a filesystem path.
type NamespaceReadlinkData struct {
	EntryID       string `json:"entry_id"`
	TargetDisplay string `json:"target_display"`
	TargetRaw     []byte `json:"target_raw,omitempty"`
}

const AnnotationBundleSchema = "org.restoreweave.annotations.v1"

type AnnotationData struct {
	ID                  string `json:"annotation_id"`
	WorkspaceID         string `json:"workspace_id"`
	SubjectRef          string `json:"subject_ref"`
	Kind                string `json:"kind"`
	Body                string `json:"body"`
	BodyDigest          string `json:"body_digest,omitempty"`
	Revision            int64  `json:"revision"`
	PredecessorRevision int64  `json:"predecessor_revision"`
	Tombstoned          bool   `json:"tombstoned"`
	CreatedAt           string `json:"created_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

type AnnotationListData struct {
	Annotations []AnnotationData `json:"annotations"`
}

type AnnotationUpsertData struct {
	Annotation AnnotationData `json:"annotation"`
}

type AnnotationExportData struct {
	Schema      string           `json:"schema"`
	Annotations []AnnotationData `json:"annotations"`
	Conflict    string           `json:"conflict,omitempty"`
}

// DescriptionDocumentData is the durable source/provenance projection for a
// description revision. Segments are retained here as source material; any
// embeddings or other retrieval generations remain rebuildable derivatives.
type DescriptionDocumentData struct {
	ID              string                `json:"description_document_id"`
	WorkspaceID     string                `json:"workspace_id"`
	SubjectRef      string                `json:"subject_ref"`
	Kind            string                `json:"kind"`
	Title           string                `json:"title,omitempty"`
	Language        string                `json:"language"`
	Body            string                `json:"body"`
	BodyDigest      string                `json:"body_digest"`
	SourceRef       string                `json:"source_ref,omitempty"`
	ProducerProfile string                `json:"producer_profile,omitempty"`
	Confidence      *float64              `json:"confidence,omitempty"`
	Coverage        *float64              `json:"coverage,omitempty"`
	Visibility      string                `json:"visibility"`
	Accepted        bool                  `json:"accepted"`
	Revision        int64                 `json:"revision"`
	PredecessorID   string                `json:"predecessor_id,omitempty"`
	Metadata        json.RawMessage       `json:"metadata,omitempty"`
	CreatedAt       string                `json:"created_at,omitempty"`
	UpdatedAt       string                `json:"updated_at,omitempty"`
	Segments        []SemanticSegmentData `json:"segments"`
}

// SemanticSegmentData is an ordered, provenance-bearing source span of a
// description document. SourceSpan uses byte offsets into Body.
type SemanticSegmentData struct {
	ID          string          `json:"semantic_segment_id"`
	WorkspaceID string          `json:"workspace_id"`
	DocumentID  string          `json:"description_document_id"`
	SubjectRef  string          `json:"subject_ref"`
	Ordinal     int64           `json:"ordinal"`
	Text        string          `json:"text"`
	TextDigest  string          `json:"text_digest"`
	Language    string          `json:"language"`
	Section     string          `json:"section,omitempty"`
	SourceSpan  json.RawMessage `json:"source_span"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   string          `json:"created_at,omitempty"`
}

type DescriptionListData struct {
	Documents []DescriptionSummaryData `json:"documents"`
	Truncated bool                     `json:"truncated,omitempty"`
}

// DescriptionSummaryData is the bounded description.list projection. Full
// body text and segments are available from description.get.
type DescriptionSummaryData struct {
	ID              string   `json:"description_document_id"`
	WorkspaceID     string   `json:"workspace_id"`
	SubjectRef      string   `json:"subject_ref"`
	Kind            string   `json:"kind"`
	Title           string   `json:"title,omitempty"`
	Language        string   `json:"language"`
	BodyDigest      string   `json:"body_digest"`
	SourceRef       string   `json:"source_ref,omitempty"`
	ProducerProfile string   `json:"producer_profile,omitempty"`
	Confidence      *float64 `json:"confidence,omitempty"`
	Coverage        *float64 `json:"coverage,omitempty"`
	Visibility      string   `json:"visibility"`
	Accepted        bool     `json:"accepted"`
	Revision        int64    `json:"revision"`
	PredecessorID   string   `json:"predecessor_id,omitempty"`
	CreatedAt       string   `json:"created_at,omitempty"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
}

type DescriptionGetData struct {
	Document DescriptionDocumentData `json:"document"`
}

type DescriptionCreateData struct {
	Document DescriptionDocumentData `json:"document"`
}

type SearchHitData struct {
	SubjectRef    string   `json:"subject_ref"`
	Path          string   `json:"path"`
	Name          string   `json:"name"`
	EntryType     string   `json:"entry_type"`
	ContentID     string   `json:"content_id,omitempty"`
	ConstructAxes []string `json:"construct_axes,omitempty"`
	Dimensions    []string `json:"dimensions,omitempty"`
}

type SearchComponentData struct {
	Dimension      string `json:"dimension"`
	Provider       string `json:"query_provider_ref,omitempty"`
	GenerationID   string `json:"index_generation_ref,omitempty"`
	ScoreSemantics string `json:"score_semantics,omitempty"`
	Status         string `json:"status"`
	Hits           int    `json:"hits"`
}

type SearchQueryData struct {
	GenerationID    string                `json:"index_generation_ref,omitempty"`
	Dimension       string                `json:"dimension,omitempty"`
	Provider        string                `json:"query_provider_ref,omitempty"`
	ScoreSemantics  string                `json:"score_semantics,omitempty"`
	ConstructAxes   []string              `json:"construct_axes,omitempty"`
	FusedDimensions []string              `json:"fused_dimensions,omitempty"`
	Components      []SearchComponentData `json:"components,omitempty"`
	Hits            []SearchHitData       `json:"hits"`
}

const (
	RepresentationClassExact       = "EXACT"
	RepresentationClassRecorded    = "RECORDED"
	RepresentationFidelityExact    = "exact"
	RepresentationFidelityRecorded = "recorded"
	RepresentationPlacementPresent = "present"
	RepresentationPlacementMissing = "missing"
	RepresentationPlacementUnknown = "unknown"
)

type RepresentationListData struct {
	WorkspaceID     string               `json:"workspace_id"`
	SubjectRef      string               `json:"subject_ref,omitempty"`
	FileVersionID   string               `json:"file_version_id,omitempty"`
	ContentID       string               `json:"content_id,omitempty"`
	Representations []RepresentationData `json:"representations"`
}

type RepresentationData struct {
	ID              string `json:"representation_id"`
	ContentID       string `json:"content_id"`
	Class           string `json:"class"`
	Fidelity        string `json:"fidelity"`
	CodecProfileRef string `json:"codec_profile_ref"`
	AccessMode      string `json:"access_mode"`
	OwnershipMode   string `json:"ownership_mode"`
	DecodedLength   int64  `json:"decoded_length"`
	Authoritative   bool   `json:"authoritative"`
	Placement       string `json:"placement"`
	Verified        *bool  `json:"verified,omitempty"`
	RecordDigest    string `json:"record_digest,omitempty"`
}

type ContentOpenData struct {
	Handle      string `json:"handle"`
	EntryID     string `json:"entry_id"`
	ContentID   string `json:"content_id"`
	LogicalSize int64  `json:"logical_size"`
	MaxRead     int64  `json:"max_read"`
}

type ContentReadData struct {
	Handle string `json:"handle"`
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
	Bytes  []byte `json:"bytes"`
	EOF    bool   `json:"eof"`
}

type ContentCloseData struct {
	Handle string `json:"handle"`
	Closed bool   `json:"closed"`
}

type AudioListData struct {
	WorkspaceID string       `json:"workspace_id"`
	SnapshotRef string       `json:"snapshot_ref,omitempty"`
	Albums      []AudioAlbum `json:"albums"`
	Tracks      []AudioTrack `json:"tracks"`
}

type AudioAlbum struct {
	Artist      string   `json:"artist,omitempty"`
	Title       string   `json:"title,omitempty"`
	Year        string   `json:"year,omitempty"`
	DurationMS  int64    `json:"duration_ms,omitempty"`
	SubjectRefs []string `json:"subject_refs"`
}

type AudioTrack struct {
	SubjectRef string `json:"subject_ref"`
	Name       string `json:"name,omitempty"`
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	Track      int    `json:"track,omitempty"`
	Year       string `json:"year,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	ArtifactID string `json:"artifact_id"`
}

type BookListData struct {
	WorkspaceID string       `json:"workspace_id"`
	SnapshotRef string       `json:"snapshot_ref,omitempty"`
	Authors     []BookAuthor `json:"authors"`
	Works       []BookWork   `json:"works"`
}

type BookAuthor struct {
	Name        string   `json:"name,omitempty"`
	SubjectRefs []string `json:"subject_refs"`
}

type BookWork struct {
	SubjectRef string `json:"subject_ref"`
	Name       string `json:"name,omitempty"`
	Title      string `json:"title,omitempty"`
	Author     string `json:"author,omitempty"`
	Year       string `json:"year,omitempty"`
	Kind       string `json:"kind,omitempty"`
	ArtifactID string `json:"artifact_id"`
}

// RecoveryImportInput is the recovery.import input. It names a portable
// recovery artifact produced by recovery.export plus an independently retained
// trust anchor path. The operation verifies and admits the closure into a
// clean-install reader without depending on a pre-existing SQLite catalog.
type RecoveryImportInput struct {
	ArtifactPath      string `json:"artifact_path"`
	TrustAnchorPath   string `json:"trust_anchor_path"`
	PublicationDomain string `json:"publication_domain,omitempty"`
}

// RecoveryImportData is the recovery.import result.
type RecoveryImportData struct {
	Schema                string `json:"schema"`
	SnapshotRef           string `json:"snapshot_ref"`
	ManifestDigest        string `json:"manifest_digest"`
	CommitDigest          string `json:"commit_digest"`
	PreparedClosureDigest string `json:"prepared_closure_digest"`
	Generation            uint64 `json:"generation"`
	TrustAnchorDigest     string `json:"trust_anchor_digest"`
	FactHealth            string `json:"fact_health"`
	Files                 int    `json:"files"`
	Bytes                 int64  `json:"bytes"`
	CatalogCreated        bool   `json:"catalog_created"`
}

// RecoveryTokenExportInput is the recovery.token.export input.
type RecoveryTokenExportInput struct {
	SnapshotRef     string `json:"snapshot_ref"`
	SubjectPath     string `json:"subject_path,omitempty"`
	TrustAnchorPath string `json:"trust_anchor_path,omitempty"`
}

// RecoveryTokenData is one deterministic proof envelope over an admitted
// recovery reference, its recipe/locator-set digest, expected identity,
// publication, and trust-anchor reference. It is a pointer and proof, never
// the payload.
type RecoveryTokenData struct {
	TokenSchema          string `json:"token_schema"`
	SnapshotRef          string `json:"snapshot_ref"`
	SubjectRef           string `json:"subject_ref"`
	RecoveryReferenceID  string `json:"recovery_reference_id"`
	ExpectedContentID    string `json:"expected_content_id,omitempty"`
	ExpectedLength       int64  `json:"expected_length,omitempty"`
	RecipeDigest         string `json:"recipe_digest,omitempty"`
	PublicationCommitRef string `json:"publication_commit_ref"`
	TrustAnchorRef       string `json:"trust_anchor_ref"`
	Expiry               string `json:"expiry,omitempty"`
	TokenDigest          string `json:"token_digest"`
}

// ViewSaveInput is the view.save input.
type ViewSaveInput struct {
	Name        string   `json:"name"`
	Query       string   `json:"query"`
	Fields      []string `json:"fields,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Sort        string   `json:"sort,omitempty"`
	OutputNames string   `json:"output_names,omitempty"`
	Required    []string `json:"required_capabilities,omitempty"`
	WhenMissing string   `json:"when_missing,omitempty"`
}

// ViewData is a saved dynamic view revision.
type ViewData struct {
	ViewID      string   `json:"view_id"`
	Name        string   `json:"name"`
	Query       string   `json:"query"`
	Fields      []string `json:"fields,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Sort        string   `json:"sort,omitempty"`
	OutputNames string   `json:"output_names,omitempty"`
	Required    []string `json:"required_capabilities,omitempty"`
	WhenMissing string   `json:"when_missing,omitempty"`
	Revision    int64    `json:"revision"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// ViewEvaluateInput is the view.evaluate input. A generation scope may be
// provided to pin an index generation; otherwise the live default is used.
type ViewEvaluateInput struct {
	ViewID string `json:"view_id"`
	Name   string `json:"name,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// ViewEvaluateData is the view.evaluate result.
type ViewEvaluateData struct {
	ViewID   string          `json:"view_id"`
	Query    string          `json:"query"`
	Hits     []SearchHitData `json:"hits"`
	Coverage []string        `json:"coverage,omitempty"`
}

// ExportPlanInput is the export.plan input. It names a saved view or explicit
// subject set, output naming policy, and optional sidecar policy.
type ExportPlanInput struct {
	ViewID         string   `json:"view_id,omitempty"`
	Subjects       []string `json:"subjects,omitempty"`
	OutputName     string   `json:"output_name,omitempty"`
	Representation string   `json:"representation,omitempty"`
	Sidecars       string   `json:"sidecars,omitempty"`
	Target         string   `json:"target,omitempty"`
}

// ExportManifestData is the frozen export manifest summary.
type ExportManifestData struct {
	ManifestID     string   `json:"manifest_id"`
	ManifestDigest string   `json:"manifest_digest"`
	ViewID         string   `json:"view_id,omitempty"`
	SubjectCount   int      `json:"subject_count"`
	Representation string   `json:"representation,omitempty"`
	Target         string   `json:"target,omitempty"`
	CreatedAt      string   `json:"created_at"`
	Items          []string `json:"items,omitempty"`
}

// ExportApplyInput is the export.apply input.
type ExportApplyInput struct {
	ManifestID     string `json:"manifest_id"`
	ManifestDigest string `json:"manifest_digest"`
	Destination    string `json:"destination"`
}

// ExportVerifyInput is the export.verify input.
type ExportVerifyInput struct {
	ManifestID     string `json:"manifest_id"`
	ManifestDigest string `json:"manifest_digest"`
	Destination    string `json:"destination"`
}

// ExportApplyVerifyData is shared by export.apply and export.verify.
type ExportApplyVerifyData struct {
	ManifestID     string `json:"manifest_id"`
	ManifestDigest string `json:"manifest_digest"`
	Destination    string `json:"destination"`
	Items          int    `json:"items"`
	Bytes          int64  `json:"bytes"`
	Verified       bool   `json:"verified"`
}
