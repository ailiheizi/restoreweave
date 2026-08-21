// Package processor is the host-owned Processor seam: typed routes, opaque
// handles, staging/seal/admit, and a bounded in-process RUN_STAGE pool.
// Large bytes stay in host-owned handles. The legacy plugin category model
// is not imported. Linux bubblewrap isolation is planned by
// processor/sandbox (argv is tested on every OS; execution is Linux-only).
// The protobuf/gRPC FD control plane lives in processor/rpc: length-prefixed
// Unix protobuf frames, grpc-go wrapping of the same messages, and SCM_RIGHTS
// for source/staging bytes. Default ingest remains in-process RUN_STAGE.
package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/identify"
)

const (
	CapabilityTextExtract       = "extract.text.v1"
	CapabilityAudioTags         = "extract.audio.tags.v1"
	CapabilityBookMeta          = "extract.book.meta.v1"
	CapabilityAudioFingerprint  = "fingerprint.audio.fixture.v1"
	CapabilityTextEmbedding     = "embed.text.fixture.v1"
	CapabilityClipEmbedding     = "embed.clip.fixture.v1"
	SchemaExtractedTextV1       = "restoreweave.artifact.extracted-text.v1"
	SchemaAudioTrackV1          = "restoreweave.domain.audio.track/v1"
	SchemaBookWorkV1            = "restoreweave.domain.book.work/v1"
	SchemaAcousticFingerprintV1 = "restoreweave.artifact.acoustic-fingerprint.v1"
	SchemaTextEmbeddingV1       = "restoreweave.artifact.text-embedding.v1"
	SchemaClipEmbeddingV1       = "restoreweave.artifact.clip-embedding.v1"
	MediaTypeUTF8Text           = "text/plain; charset=utf-8"
	MediaTypeAudioJSON          = "application/json"
	MediaTypeBookJSON           = "application/json"
	MediaTypeFingerprintJSON    = "application/json"
	MediaTypeEmbeddingJSON      = "application/json"
	FixtureFingerprintAlgorithm = "fixture-v1"
	FixtureTextSpace            = "fixture-text-v1"
	FixtureClipSpace            = "fixture-clip-v1"

	AuthorityStagedArtifact = "STAGED_ARTIFACT"
	LifecycleRebuildable    = "REBUILDABLE_DERIVATIVE"
	DeterminismByteExact    = "BYTE_DETERMINISTIC"

	defaultMaxSourceBytes = 1 << 20
	defaultStageTimeout   = 2 * time.Second
	defaultMaxConcurrent  = 2
)

type Stage string

const (
	StageClassifyLearned Stage = "CLASSIFY_LEARNED"
	StageParse           Stage = "PARSE"
	StageExtract         Stage = "EXTRACT"
	StageEnrich          Stage = "ENRICH"
	StageFingerprint     Stage = "FINGERPRINT"
	StageTransform       Stage = "TRANSFORM"
	StageValidate        Stage = "VALIDATE"
	StageIndexPrepare    Stage = "INDEX_PREPARE"
)

type RouteKind string

const (
	RouteIdentification RouteKind = "IDENTIFICATION"
	RouteProcessing     RouteKind = "PROCESSING"
)

type ResultStatus string

const (
	StatusSucceeded    ResultStatus = "SUCCEEDED"
	StatusInapplicable ResultStatus = "INAPPLICABLE"
	StatusFailed       ResultStatus = "FAILED"
	StatusCancelled    ResultStatus = "CANCELLED"
)

type RouteNode struct {
	Stage        Stage  `json:"stage"`
	CapabilityID string `json:"capability_id"`
}

type Route struct {
	Kind  RouteKind   `json:"kind"`
	Nodes []RouteNode `json:"nodes"`
}

func (route Route) Digest() string {
	payload, _ := json.Marshal(route)
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type ClassificationRecord struct {
	State    identify.IdentificationState `json:"state"`
	Evidence []identify.DetectionEvidence `json:"evidence"`
}

type Invocation struct {
	AttemptID      string
	FenceToken     int64
	Route          Route
	Node           RouteNode
	Classification ClassificationRecord
	Source         *SourceHandle
	Staging        *StagingHandle
	MaxOutputBytes int64
}

type StageResult struct {
	Status           ResultStatus
	DeterminismClass string
	SchemaRef        string
	MediaType        string
	Sealed           bool
	Reason           string
}

type Processor interface {
	CapabilityID() string
	Stage() Stage
	RunStage(ctx context.Context, inv Invocation) (StageResult, error)
}

// SchemaRefExtractedText pins the bundled extracted-text schema by digest.
func SchemaRefExtractedText() string {
	sum := sha256.Sum256([]byte(SchemaExtractedTextV1))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func SchemaRefAudioTrack() string {
	sum := sha256.Sum256([]byte(SchemaAudioTrackV1))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func SchemaRefBookWork() string {
	sum := sha256.Sum256([]byte(SchemaBookWorkV1))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func SchemaRefAcousticFingerprint() string {
	sum := sha256.Sum256([]byte(SchemaAcousticFingerprintV1))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func SchemaRefTextEmbedding() string {
	sum := sha256.Sum256([]byte(SchemaTextEmbeddingV1))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func SchemaRefClipEmbedding() string {
	sum := sha256.Sum256([]byte(SchemaClipEmbeddingV1))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DefaultProcessors are the in-process EXTRACT pack. Fixture fingerprint
// and embedding processors are opt-in and are not part of default ingest.
func DefaultProcessors() []Processor {
	return []Processor{TextExtract{}, AudioTags{}, BookMeta{}}
}

func ProducerDigest(capabilityID string) string {
	sum := sha256.Sum256([]byte("processor:" + capabilityID))
	return "sha256:" + hex.EncodeToString(sum[:])
}
