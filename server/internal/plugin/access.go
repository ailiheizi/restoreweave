package plugin

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// The types in this file are wire descriptors for plugin runtime envelopes.
// They are not the in-process data-plane API. internal/readsvc owns the live
// SnapshotTree, FileAccess, repository, storage, and decoder interfaces.

// SnapshotTreeWireHandle identifies one already-opened, export-bound view.
// It cannot select another snapshot or resolve outside the core-issued export.
type SnapshotTreeWireHandle struct {
	ExportID               string `json:"export_id"`
	ViewHandleID           string `json:"view_handle_id"`
	TreeDigest             Digest `json:"tree_digest"`
	PathPolicyDigest       Digest `json:"path_policy_digest"`
	AuthorizationDigest    Digest `json:"authorization_digest"`
	IntegrityProfileDigest Digest `json:"integrity_profile_digest"`
}

func (h SnapshotTreeWireHandle) Validate() error {
	for name, value := range map[string]string{
		"export_id": h.ExportID, "view_handle_id": h.ViewHandleID,
	} {
		if err := validateOpaqueID(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	for name, digest := range map[string]Digest{
		"tree_digest":              h.TreeDigest,
		"path_policy_digest":       h.PathPolicyDigest,
		"authorization_digest":     h.AuthorizationDigest,
		"integrity_profile_digest": h.IntegrityProfileDigest,
	} {
		if err := digest.Validate(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// FileAccessWireHandle identifies the matching host-owned content facade. A
// gateway can call only this export-bound facade; it receives no repository,
// placement, key, policy-arbiter, or global snapshot-selector capability.
type FileAccessWireHandle struct {
	ExportID        string    `json:"export_id"`
	ServiceHandleID string    `json:"service_handle_id"`
	ContractDigest  Digest    `json:"contract_digest"`
	ExpiresAt       time.Time `json:"expires_at"`
}

func (h FileAccessWireHandle) Validate(now time.Time) error {
	if err := validateOpaqueID(h.ExportID); err != nil {
		return fmt.Errorf("export_id: %w", err)
	}
	if err := validateOpaqueID(h.ServiceHandleID); err != nil {
		return fmt.Errorf("service_handle_id: %w", err)
	}
	if err := h.ContractDigest.Validate(); err != nil {
		return fmt.Errorf("contract_digest: %w", err)
	}
	if h.ExpiresAt.IsZero() || !h.ExpiresAt.After(now) {
		return errors.New("file-access handle is expired")
	}
	return nil
}

type RepositoryReadInvocation struct {
	RequestID       string     `json:"request_id"`
	RepositoryScope string     `json:"repository_scope_id"`
	SnapshotID      string     `json:"snapshot_id"`
	ObjectID        string     `json:"object_id"`
	ExpectedContent Digest     `json:"expected_content_digest"`
	Range           *ByteRange `json:"range,omitempty"`
}

func (r RepositoryReadInvocation) Validate() error {
	for name, value := range map[string]string{
		"request_id": r.RequestID, "repository_scope_id": r.RepositoryScope,
		"snapshot_id": r.SnapshotID, "object_id": r.ObjectID,
	} {
		if err := validateOpaqueID(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := r.ExpectedContent.Validate(); err != nil {
		return fmt.Errorf("expected_content_digest: %w", err)
	}
	if r.Range != nil {
		if err := r.Range.validate(false); err != nil {
			return fmt.Errorf("range: %w", err)
		}
	}
	return nil
}

type StorageRangeInvocation struct {
	RequestID       string    `json:"request_id"`
	PlacementScope  string    `json:"placement_scope_id"`
	ImmutableObject string    `json:"immutable_object_id"`
	ObjectDigest    Digest    `json:"object_digest"`
	Range           ByteRange `json:"range"`
	VersionIdentity string    `json:"version_identity"`
}

func (r StorageRangeInvocation) Validate() error {
	for name, value := range map[string]string{
		"request_id": r.RequestID, "placement_scope_id": r.PlacementScope,
		"immutable_object_id": r.ImmutableObject, "version_identity": r.VersionIdentity,
	} {
		if err := validateOpaqueID(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := r.ObjectDigest.Validate(); err != nil {
		return fmt.Errorf("object_digest: %w", err)
	}
	if err := r.Range.validate(false); err != nil {
		return fmt.Errorf("range: %w", err)
	}
	return nil
}

type SeekBehavior string

const (
	SeekSequentialOnly SeekBehavior = "SEQUENTIAL_ONLY"
	SeekRestartable    SeekBehavior = "RESTARTABLE"
	SeekRandomAccess   SeekBehavior = "RANDOM_ACCESS"
)

type RepresentationWireRef struct {
	RepresentationID string              `json:"representation_id"`
	Class            TransformationClass `json:"class"`
	PayloadDigest    Digest              `json:"payload_digest"`
	DecodedDigest    Digest              `json:"decoded_digest"`
	DecodedLength    uint64              `json:"decoded_length"`
	CodecProfile     string              `json:"codec_profile"`
}

func (r RepresentationWireRef) Validate() error {
	if err := validateOpaqueID(r.RepresentationID); err != nil {
		return fmt.Errorf("representation_id: %w", err)
	}
	if _, ok := transformationClasses[r.Class]; !ok || r.Class == TransformationNotApplicable {
		return fmt.Errorf("invalid representation class %q", r.Class)
	}
	if err := r.PayloadDigest.Validate(); err != nil {
		return fmt.Errorf("payload_digest: %w", err)
	}
	if err := r.DecodedDigest.Validate(); err != nil {
		return fmt.Errorf("decoded_digest: %w", err)
	}
	if err := validateOpaqueID(r.CodecProfile); err != nil {
		return fmt.Errorf("codec_profile: %w", err)
	}
	return nil
}

type RepresentationDecodeInvocation struct {
	RequestID    string       `json:"request_id"`
	Range        *ByteRange   `json:"decoded_range,omitempty"`
	RequiredSeek SeekBehavior `json:"required_seek"`
}

func (r RepresentationDecodeInvocation) Validate(decodedLength uint64) error {
	if err := validateOpaqueID(r.RequestID); err != nil {
		return fmt.Errorf("request_id: %w", err)
	}
	if r.Range != nil {
		if err := r.Range.validate(false); err != nil {
			return fmt.Errorf("decoded_range: %w", err)
		}
		end, _ := r.Range.End()
		if end > decodedLength {
			return errors.New("decoded range exceeds declared decoded length")
		}
	}
	switch r.RequiredSeek {
	case SeekSequentialOnly, SeekRestartable, SeekRandomAccess:
	default:
		return fmt.Errorf("unknown required_seek %q", r.RequiredSeek)
	}
	return nil
}

// StreamHandleDescriptor is serializable metadata for a host-retained live
// handle. The actual reader and cancellation authority never cross this DTO.
type StreamHandleDescriptor struct {
	HandleID       string       `json:"handle_id"`
	Length         uint64       `json:"length"`
	ExpectedDigest Digest       `json:"expected_digest"`
	SeekBehavior   SeekBehavior `json:"seek_behavior"`
}

type ReadEvidenceRecord struct {
	RequestID      string            `json:"request_id"`
	Adapter        EntryPointRef     `json:"adapter"`
	SourceIdentity string            `json:"source_identity"`
	BytesProduced  uint64            `json:"bytes_produced"`
	ObservedDigest Digest            `json:"observed_digest,omitempty"`
	Complete       bool              `json:"complete"`
	Execution      ExecutionMetadata `json:"execution"`
}

type DecodeEvidenceRecord struct {
	RequestID     string            `json:"request_id"`
	Decoder       EntryPointRef     `json:"decoder"`
	InputDigest   Digest            `json:"input_digest"`
	OutputDigest  Digest            `json:"output_digest,omitempty"`
	BytesProduced uint64            `json:"bytes_produced"`
	Complete      bool              `json:"complete"`
	SeekBehavior  SeekBehavior      `json:"seek_behavior"`
	Execution     ExecutionMetadata `json:"execution"`
}

type PackIndexInvocation struct {
	RequestID           string                `json:"request_id"`
	PackIndexScope      string                `json:"pack_index_scope_id"`
	IndexRepresentation RepresentationWireRef `json:"index_representation"`
	AuthenticatedRange  ByteRange             `json:"authenticated_range"`
	LookupKeys          []Digest              `json:"lookup_key_digests"`
	MaxCandidates       uint32                `json:"max_candidates"`
}

type PackSliceCandidate struct {
	LookupKeyDigest Digest    `json:"lookup_key_digest"`
	PackObjectID    string    `json:"pack_object_id"`
	EncodedRange    ByteRange `json:"encoded_range"`
	ProofDigest     Digest    `json:"proof_digest"`
}

type PackIndexEvidenceRecord struct {
	RequestID   string               `json:"request_id"`
	Reader      EntryPointRef        `json:"reader"`
	IndexDigest Digest               `json:"index_digest"`
	Candidates  []PackSliceCandidate `json:"candidates"`
	Coverage    CoverageCounter      `json:"coverage"`
	Execution   ExecutionMetadata    `json:"execution"`
}

type GatewayProtocol string

const (
	GatewayWebDAV GatewayProtocol = "WEBDAV"
	GatewayFUSE   GatewayProtocol = "FUSE"
	GatewaySMB    GatewayProtocol = "SMB"
	GatewayNFS    GatewayProtocol = "NFS"
	GatewayS3     GatewayProtocol = "S3"
)

// NamespaceGatewayInvocation carries two matching, export-bound core handles.
// It never carries a global SnapshotTree selector or direct repository access.
type NamespaceGatewayInvocation struct {
	RequestID     string                 `json:"request_id"`
	ExportID      string                 `json:"export_id"`
	Protocol      GatewayProtocol        `json:"protocol"`
	SnapshotTree  SnapshotTreeWireHandle `json:"snapshot_tree"`
	FileAccess    FileAccessWireHandle   `json:"file_access"`
	ListenerScope string                 `json:"listener_scope_id"`
	ReadOnly      bool                   `json:"read_only"`
}

func (r NamespaceGatewayInvocation) Validate(now time.Time) error {
	if err := validateOpaqueID(r.RequestID); err != nil {
		return fmt.Errorf("request_id: %w", err)
	}
	if err := validateOpaqueID(r.ExportID); err != nil {
		return fmt.Errorf("export_id: %w", err)
	}
	if err := r.SnapshotTree.Validate(); err != nil {
		return fmt.Errorf("snapshot_tree: %w", err)
	}
	if err := r.FileAccess.Validate(now); err != nil {
		return fmt.Errorf("file_access: %w", err)
	}
	if r.ExportID != r.SnapshotTree.ExportID || r.ExportID != r.FileAccess.ExportID {
		return errors.New("gateway handles are not bound to the requested export")
	}
	if err := validateOpaqueID(r.ListenerScope); err != nil {
		return fmt.Errorf("listener_scope_id: %w", err)
	}
	switch r.Protocol {
	case GatewayWebDAV, GatewayFUSE, GatewaySMB, GatewayNFS, GatewayS3:
	default:
		return fmt.Errorf("unknown gateway protocol %q", r.Protocol)
	}
	if !r.ReadOnly {
		return errors.New("initial namespace gateways are read-only")
	}
	return nil
}

type GatewaySessionReceiptRecord struct {
	RequestID          string          `json:"request_id"`
	SessionID          string          `json:"session_id"`
	Protocol           GatewayProtocol `json:"protocol"`
	Endpoint           string          `json:"endpoint"`
	SnapshotTreeDigest Digest          `json:"snapshot_tree_digest"`
	StartedAt          time.Time       `json:"started_at"`
	Lease              SessionLease    `json:"lease"`
	State              SessionState    `json:"state"`
}

type NamespaceMutationBatch struct {
	GenerationID        string   `json:"generation_id"`
	NamespaceRootDigest Digest   `json:"namespace_root_digest"`
	BatchOrdinal        uint64   `json:"batch_ordinal"`
	RecordDigests       []Digest `json:"record_digests"`
	FinalBatch          bool     `json:"final_batch"`
}

type NamespaceIndexReceiptRecord struct {
	GenerationID        string `json:"generation_id"`
	NamespaceRootDigest Digest `json:"namespace_root_digest"`
	MaterializedThrough uint64 `json:"materialized_through"`
	Complete            bool   `json:"complete"`
	ProjectionDigest    Digest `json:"projection_digest"`
}

type NamespaceLookupInvocation struct {
	GenerationID string `json:"generation_id"`
	LookupDigest Digest `json:"lookup_digest"`
	CursorDigest Digest `json:"cursor_digest,omitempty"`
	Limit        uint32 `json:"limit"`
}

type NamespaceCandidatesRecord struct {
	GenerationID     string   `json:"generation_id"`
	CandidateDigests []Digest `json:"candidate_digests"`
	NextCursorDigest Digest   `json:"next_cursor_digest,omitempty"`
}

type SearchIndexMutationBatch struct {
	GenerationID  string   `json:"generation_id"`
	ACLContext    Digest   `json:"acl_context_digest"`
	RecordDigests []Digest `json:"record_digests"`
}

type SearchIndexReceiptRecord struct {
	GenerationID string `json:"generation_id"`
	IndexDigest  Digest `json:"index_digest"`
	RecordCount  uint64 `json:"record_count"`
}

type SearchQueryInvocation struct {
	GenerationID string `json:"generation_id"`
	QueryDigest  Digest `json:"query_digest"`
	ACLContext   Digest `json:"acl_context_digest"`
	Limit        uint32 `json:"limit"`
}

type SearchCandidate struct {
	SubjectID string  `json:"subject_id"`
	Score     float64 `json:"score"`
}

func (c SearchCandidate) Validate() error {
	if err := validateOpaqueID(c.SubjectID); err != nil {
		return fmt.Errorf("subject_id: %w", err)
	}
	if math.IsNaN(c.Score) || math.IsInf(c.Score, 0) {
		return errors.New("score must be finite")
	}
	return nil
}
