package command

type StatusData struct {
	Controller    string         `json:"controller"`
	Catalog       CatalogStatus  `json:"catalog"`
	Identify      IdentifyStatus `json:"identify"`
	Listen        string         `json:"listen,omitempty"`
	Unimplemented []string       `json:"unimplemented"`
}

type CatalogStatus struct {
	Path string `json:"path"`
	OK   bool   `json:"ok"`
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
	WorkspaceID    string `json:"workspace_id"`
	SourceID       string `json:"source_id"`
	ScanID         string `json:"scan_id"`
	RootID         string `json:"root_id"`
	SnapshotRef    string `json:"snapshot_ref"`
	ManifestDigest string `json:"manifest_digest"`
	Files          int    `json:"files"`
	Bytes          int64  `json:"bytes"`
}

type PlanRestoreData struct {
	SnapshotRef string `json:"snapshot_ref"`
	Destination string `json:"destination"`
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
}

type SnapshotVerifyData struct {
	SnapshotRef string `json:"snapshot_ref"`
	Entries     int    `json:"entries"`
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
	OK          bool   `json:"ok"`
}

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
}

type SearchHitData struct {
	SubjectRef string `json:"subject_ref"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	EntryType  string `json:"entry_type"`
	ContentID  string `json:"content_id,omitempty"`
}

type SearchQueryData struct {
	GenerationID string          `json:"index_generation_ref,omitempty"`
	Hits         []SearchHitData `json:"hits"`
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

type GatewayMountData struct {
	MountID     string `json:"mount_id"`
	SnapshotRef string `json:"snapshot_ref"`
	Mountpoint  string `json:"mountpoint"`
	Platform    string `json:"platform"`
}

type GatewayUnmountData struct {
	MountID    string `json:"mount_id,omitempty"`
	Mountpoint string `json:"mountpoint,omitempty"`
	Unmounted  bool   `json:"unmounted"`
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
