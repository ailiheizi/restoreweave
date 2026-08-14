package processor

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/identify"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

var ErrUnavailable = errors.New("processor host is not configured")

type Options struct {
	StagingDir     string
	MaxSourceBytes int64
	StageTimeout   time.Duration
	MaxConcurrent  int
	Identify       *identify.Detector
	Processors     []Processor
	Now            func() time.Time
}

// Host runs host-owned identification routing and RUN_STAGE admission.
// Exact ingest must not depend on this host succeeding.
type Host struct {
	Store  *sqlite.Store
	Repo   repository.Driver
	opts   Options
	pool   *pool
	byCap  map[string]Processor
	detect *identify.Detector
}

type Report struct {
	Attempted int
	Admitted  int
	Rejected  int
	Skipped   int
	Failed    int
	Cancelled int
}

func NewHost(store *sqlite.Store, repo repository.Driver, opts Options) *Host {
	if opts.MaxSourceBytes <= 0 {
		opts.MaxSourceBytes = defaultMaxSourceBytes
	}
	if opts.StageTimeout <= 0 {
		opts.StageTimeout = defaultStageTimeout
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = defaultMaxConcurrent
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if strings.TrimSpace(opts.StagingDir) == "" && repo != nil && repo.Root() != "" && repo.Root() != ":memory:" {
		opts.StagingDir = filepath.Join(repo.Root(), "staging")
	}
	processors := opts.Processors
	if processors == nil {
		processors = []Processor{TextExtract{}, AudioTags{}, BookMeta{}}
	}
	byCap := make(map[string]Processor, len(processors))
	for _, item := range processors {
		if item != nil {
			byCap[item.CapabilityID()] = item
		}
	}
	detect := opts.Identify
	if detect == nil {
		detect = identify.NewDetector(0)
	}
	return &Host{
		Store:  store,
		Repo:   repo,
		opts:   opts,
		pool:   newPool(opts.MaxConcurrent, opts.StageTimeout),
		byCap:  byCap,
		detect: detect,
	}
}

// ProcessPublication runs optional processing after exact publication.
// Per-subject failures are recorded and never returned as a host error.
func (h *Host) ProcessPublication(ctx context.Context, workspaceID, snapshotRef, rootID string) error {
	_, err := h.Process(ctx, workspaceID, snapshotRef, rootID)
	return err
}

func (h *Host) Process(ctx context.Context, workspaceID, snapshotRef, rootID string) (Report, error) {
	var report Report
	if h == nil || h.Store == nil || h.Repo == nil {
		return report, ErrUnavailable
	}
	nodes, err := h.Store.ListNamespaceSubtree(ctx, workspaceID, rootID, "")
	if err != nil {
		return report, err
	}
	var mu sync.Mutex
	for _, node := range nodes {
		entry := node.Entry
		if entry.EntryType != sqlite.EntryFile || entry.ContentID == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			mu.Lock()
			report.Cancelled++
			mu.Unlock()
			continue
		}
		err := h.pool.run(ctx, func(runCtx context.Context) error {
			outcome, _ := h.processEntry(runCtx, workspaceID, snapshotRef, entry)
			mu.Lock()
			defer mu.Unlock()
			report.Attempted++
			switch outcome {
			case StatusSucceeded:
				report.Admitted++
			case StatusInapplicable:
				report.Skipped++
			case StatusCancelled:
				report.Cancelled++
			default:
				report.Failed++
			}
			return nil
		})
		if err != nil {
			mu.Lock()
			report.Failed++
			mu.Unlock()
		}
	}
	return report, nil
}

func (h *Host) processEntry(ctx context.Context, workspaceID, snapshotRef string, entry sqlite.NamespaceEntry) (ResultStatus, error) {
	source, err := openSource(ctx, h.Repo, entry.ContentID, h.opts.MaxSourceBytes)
	if err != nil {
		return StatusFailed, err
	}
	defer source.Close()

	probe, err := source.ReadAll(ctx)
	if err != nil {
		return StatusFailed, err
	}
	identified, err := h.detect.Detect(ctx, entry.DisplayName, probe)
	if err != nil {
		return StatusFailed, err
	}
	classification := ClassificationRecord{State: identified.State, Evidence: identified.Evidence}
	route := buildProcessingRoute(classification)
	if len(route.Nodes) == 0 {
		return StatusInapplicable, nil
	}
	node := route.Nodes[0]
	proc, ok := h.byCap[node.CapabilityID]
	if !ok {
		return StatusInapplicable, nil
	}

	attemptID, err := sqlite.NewStableID(sqlite.IDPrefixAttempt)
	if err != nil {
		return StatusFailed, err
	}
	stagingDir := h.opts.StagingDir
	if stagingDir == "" {
		stagingDir = filepath.Join(h.Repo.Root(), "staging")
	}
	staging, err := openStaging(stagingDir, attemptID, h.opts.MaxSourceBytes)
	if err != nil {
		return StatusFailed, err
	}
	defer staging.Close()

	result, runErr := proc.RunStage(ctx, Invocation{
		AttemptID:      attemptID,
		FenceToken:     1,
		Route:          route,
		Node:           node,
		Classification: classification,
		Source:         source,
		Staging:        staging,
		MaxOutputBytes: h.opts.MaxSourceBytes,
	})
	if runErr != nil && result.Status == "" {
		result.Status = StatusFailed
		result.Reason = runErr.Error()
	}
	if result.Status != StatusSucceeded {
		return result.Status, nil
	}
	if !result.Sealed {
		return StatusFailed, nil
	}
	body, digest, err := staging.sealedBytes()
	if err != nil {
		return StatusFailed, nil
	}
	if !admittedSchema(result.SchemaRef, result.MediaType) {
		return StatusFailed, nil
	}
	envelope, err := json.Marshal(map[string]any{
		"schema_ref":        result.SchemaRef,
		"determinism_class": result.DeterminismClass,
		"classification":    classification,
		"route":             route,
		"capability_id":     node.CapabilityID,
		"attempt_id":        attemptID,
		"fence_token":       1,
		"source_content_id": entry.ContentID,
	})
	if err != nil {
		return StatusFailed, err
	}
	artifactID, err := sqlite.NewStableID(sqlite.IDPrefixArtifact)
	if err != nil {
		return StatusFailed, err
	}
	record := &sqlite.ProcessorArtifact{
		ID:             artifactID,
		WorkspaceID:    workspaceID,
		SubjectRef:     entry.ID,
		SnapshotRef:    snapshotRef,
		RouteDigest:    route.Digest(),
		Stage:          string(node.Stage),
		CapabilityID:   node.CapabilityID,
		SchemaRef:      result.SchemaRef,
		State:          sqlite.ArtifactAdmitted,
		AuthorityClass: AuthorityStagedArtifact,
		LifecycleClass: LifecycleRebuildable,
		MediaType:      result.MediaType,
		ByteLength:     int64(len(body)),
		Digest:         digest,
		Body:           string(body),
		AttemptID:      attemptID,
		FenceToken:     1,
		ProducerDigest: ProducerDigest(node.CapabilityID),
		Envelope:       envelope,
		CreatedAt:      h.opts.Now().UTC(),
	}
	if err := h.Store.InsertProcessorArtifact(ctx, record); err != nil {
		if errors.Is(err, sqlite.ErrConflict) {
			return StatusSucceeded, nil
		}
		return StatusFailed, err
	}
	return StatusSucceeded, nil
}

func buildProcessingRoute(classification ClassificationRecord) Route {
	if classification.State == identify.IdentificationConflictingEvidence ||
		classification.State == identify.IdentificationUnknown {
		return Route{Kind: RouteProcessing}
	}
	if hasAudioEvidence(classification) {
		return Route{
			Kind: RouteProcessing,
			Nodes: []RouteNode{{
				Stage:        StageExtract,
				CapabilityID: CapabilityAudioTags,
			}},
		}
	}
	if hasBookEvidence(classification) {
		return Route{
			Kind: RouteProcessing,
			Nodes: []RouteNode{{
				Stage:        StageExtract,
				CapabilityID: CapabilityBookMeta,
			}},
		}
	}
	if hasPlainTextEvidence(classification) {
		return Route{
			Kind: RouteProcessing,
			Nodes: []RouteNode{{
				Stage:        StageExtract,
				CapabilityID: CapabilityTextExtract,
			}},
		}
	}
	return Route{Kind: RouteProcessing}
}

func hasAudioEvidence(classification ClassificationRecord) bool {
	for _, item := range classification.Evidence {
		mime := strings.ToLower(item.Candidate.MIME)
		if strings.HasPrefix(mime, "audio/") || mime == "application/ogg" {
			return true
		}
		switch item.Candidate.FormatID {
		case "flac", "mp3", "wave", "id3v2-tagged", "iso-bmff-audio", "ogg":
			return true
		}
	}
	return false
}

func hasBookEvidence(classification ClassificationRecord) bool {
	for _, item := range classification.Evidence {
		mime := strings.ToLower(item.Candidate.MIME)
		if mime == "application/epub+zip" {
			return true
		}
		if item.Candidate.FormatID == "epub" {
			return true
		}
	}
	return false
}

func admittedSchema(schemaRef, mediaType string) bool {
	switch {
	case schemaRef == SchemaRefExtractedText() && mediaType == MediaTypeUTF8Text:
		return true
	case schemaRef == SchemaRefAudioTrack() && mediaType == MediaTypeAudioJSON:
		return true
	case schemaRef == SchemaRefBookWork() && mediaType == MediaTypeBookJSON:
		return true
	default:
		return false
	}
}

func hasPlainTextEvidence(classification ClassificationRecord) bool {
	for _, item := range classification.Evidence {
		if item.Kind == identify.EvidenceSuffix && item.Candidate.MIME == "text/plain" {
			return true
		}
	}
	return false
}
