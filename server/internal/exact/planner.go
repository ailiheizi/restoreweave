package exact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/identify"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/scanner"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

var (
	// ErrIngestPlanStale means the source observed during apply no longer
	// matches the capture that produced the plan. It is returned before any
	// catalog, repository, or publication mutation is attempted.
	ErrIngestPlanStale             = errors.New("ingest plan capture basis is stale")
	ErrIngestPlanConfigChanged     = errors.New("ingest plan configuration is stale")
	ErrIngestPlanProtectionChanged = errors.New("ingest plan protection decisions are stale")
	ErrInvalidIngestPlan           = errors.New("invalid ingest plan")
)

// IngestEstimate is a read-only estimate derived from one rooted capture.
// NewBytes is conservative: it counts unique content IDs not currently
// present in the configured repository and does not reserve repository space.
type IngestEstimate struct {
	Files         int   `json:"files"`
	Bytes         int64 `json:"bytes"`
	UniqueFiles   int   `json:"unique_files"`
	UniqueBytes   int64 `json:"unique_bytes"`
	LocalFiles    int   `json:"local_files"`
	LocalBytes    int64 `json:"local_bytes"`
	NewBytes      int64 `json:"new_bytes"`
	LinkOnlyFiles int   `json:"link_only_files"`
	LocatorCount  int   `json:"locator_count"`
}

// IngestPlanIssue is a portable explanation for why a source entry is not
// eligible for authoritative ingest. It intentionally preserves the scanner
// issue code instead of collapsing the whole inspection into one error.
type IngestPlanIssue struct {
	RelativePath   string                   `json:"relative_path"`
	RawPath        []byte                   `json:"raw_path,omitempty"`
	Mode           sqlite.ProtectionMode    `json:"mode"`
	PlannedOutcome sqlite.ProtectionOutcome `json:"planned_outcome"`
	State          string                   `json:"state"`
	ReasonCode     string                   `json:"reason_code"`
	Message        string                   `json:"message,omitempty"`
}

// IngestPlan is intentionally portable and in-memory. The control plane may
// persist it, but the exact package itself does not write plans while
// inspecting a source.
type IngestPlan struct {
	Root                    string                           `json:"root"`
	SourceID                string                           `json:"source_id,omitempty"`
	Binding                 capture.BindingRecord            `json:"binding"`
	CaptureBasisDigest      string                           `json:"capture_basis_digest"`
	ConfigDigest            string                           `json:"config_digest,omitempty"`
	ProtectionDigest        string                           `json:"protection_digest"`
	ProtectionMode          sqlite.ProtectionMode            `json:"protection_mode"`
	FileProtection          map[string]sqlite.ProtectionMode `json:"file_protection,omitempty"`
	ProtectionDecisions     []IngestProtectionDecision       `json:"protection_decisions"`
	BlockedEntries          []IngestPlanIssue                `json:"blocked_entries,omitempty"`
	Executable              bool                             `json:"executable"`
	ConfirmLinkOnly         bool                             `json:"confirm_link_only,omitempty"`
	ExternalLocators        []IngestLocator                  `json:"external_locators,omitempty"`
	MetadataOnlyResolutions []string                         `json:"metadata_only_resolutions,omitempty"`
	Estimate                IngestEstimate                   `json:"estimate"`
}

// InspectIngest performs a rooted, read-only scan and computes the basis that
// ApplyIngestPlan must observe again. It does not create catalog records,
// repository objects, manifests, or publications.
func (s *Service) InspectIngest(ctx context.Context, root string, options IngestOptions) (IngestPlan, error) {
	var plan IngestPlan
	if err := s.require(); err != nil {
		return plan, err
	}
	policy, err := s.resolveIngestOptions(options)
	if err != nil {
		return plan, err
	}
	captureResult, err := s.captureForIngest(ctx, root, "")
	if err != nil {
		return plan, err
	}
	defer captureResult.session.Close()
	blockedEntries := blockedIngestEntriesForMode(captureResult.sink.start.CaptureMode, captureResult.sink.entries, policy)
	// A partial scan is still valuable as a review artifact. Do not attempt
	// locator binding against entries which were not captured; the resulting
	// plan is explicitly blocked and apply will reject it before recapture or
	// any repository/catalog mutation.
	var boundLocators map[string][]IngestLocator
	if len(blockedEntries) == 0 {
		boundLocators, err = bindIngestLocators(captureResult.sink.entries, policy)
		if err != nil {
			return plan, err
		}
	} else {
		boundLocators = make(map[string][]IngestLocator)
	}
	protectionDecisions, err := buildProtectionDecisionsWithResolutions(captureResult.sink.entries, policy, boundLocators)
	if err != nil {
		return plan, err
	}
	protectionDigest, err := protectionDecisionDigest(protectionDecisions)
	if err != nil {
		return plan, err
	}
	basis, err := captureBasisDigest(captureResult.binding, captureResult.sink.entries)
	if err != nil {
		return plan, err
	}
	estimate, err := s.estimateIngest(ctx, captureResult.sink.entries, policy)
	if err != nil {
		return plan, err
	}
	locators := append([]IngestLocator(nil), policy.locators...)
	return IngestPlan{
		Root:                    captureResult.binding.DisplayPath,
		SourceID:                captureResult.sourceID,
		Binding:                 captureResult.binding,
		CaptureBasisDigest:      basis,
		ConfigDigest:            s.ConfigDigest,
		ProtectionDigest:        protectionDigest,
		ProtectionMode:          policy.mode,
		FileProtection:          cloneProtectionModes(policy.fileModes),
		ProtectionDecisions:     protectionDecisions,
		BlockedEntries:          blockedEntries,
		Executable:              len(blockedEntries) == 0 && (captureResult.scanResult.State == scanner.ScanComplete || metadataOnlyScanResolved(captureResult.sink.start.CaptureMode, captureResult.sink.entries, policy)),
		ConfirmLinkOnly:         options.ConfirmLinkOnly,
		ExternalLocators:        locators,
		MetadataOnlyResolutions: append([]string(nil), options.MetadataOnlyResolutions...),
		Estimate:                estimate,
	}, nil
}

// ApplyIngestPlan recaptures the source and only enters the mutating ingest
// lane after binding and capture basis validation have both succeeded.
func (s *Service) ApplyIngestPlan(ctx context.Context, plan IngestPlan) (IngestResult, error) {
	return s.ApplyIngestPlanWithExecutionKey(ctx, plan, "")
}

// ApplyIngestPlanWithExecutionKey binds the resulting immutable publication to
// the durable plan execution. The empty-key wrapper preserves the lower-level
// exact API for callers that do not use the control-plane planner.
func (s *Service) ApplyIngestPlanWithExecutionKey(ctx context.Context, plan IngestPlan, executionKey string) (IngestResult, error) {
	var result IngestResult
	if err := s.require(); err != nil {
		return result, err
	}
	if plan.Root == "" || plan.CaptureBasisDigest == "" || plan.ProtectionDigest == "" || plan.Binding.IdentityDigest() == "" {
		return result, ErrInvalidIngestPlan
	}
	if !plan.Executable || len(plan.BlockedEntries) != 0 {
		return result, ErrIngestPlanBlocked
	}
	if plan.ConfigDigest != s.ConfigDigest {
		return result, fmt.Errorf("%w: plan=%q current=%q", ErrIngestPlanConfigChanged, plan.ConfigDigest, s.ConfigDigest)
	}
	policy, err := s.resolveIngestOptions(IngestOptions{
		ProtectionMode:          plan.ProtectionMode,
		FileProtection:          plan.FileProtection,
		ConfirmLinkOnly:         plan.ConfirmLinkOnly,
		ExternalLocators:        plan.ExternalLocators,
		MetadataOnlyResolutions: plan.MetadataOnlyResolutions,
	})
	if err != nil {
		return result, err
	}
	captureResult, err := s.captureForIngest(ctx, plan.Root, plan.SourceID)
	if err != nil {
		return result, err
	}
	defer captureResult.session.Close()
	if err := requireQualifiedWithEntries(captureResult.sink.start.CaptureMode, captureResult.scanResult, captureResult.sink.entries, policy); err != nil {
		return result, err
	}
	if captureResult.binding.IdentityDigest() != plan.Binding.IdentityDigest() {
		return result, fmt.Errorf("%w: capture root identity changed", ErrIngestPlanStale)
	}
	actualBasis, err := captureBasisDigest(captureResult.binding, captureResult.sink.entries)
	if err != nil {
		return result, err
	}
	if actualBasis != plan.CaptureBasisDigest {
		return result, fmt.Errorf("%w: plan=%s current=%s", ErrIngestPlanStale, plan.CaptureBasisDigest, actualBasis)
	}
	boundLocators, err := bindIngestLocators(captureResult.sink.entries, policy)
	if err != nil {
		return result, err
	}
	actualProtectionDecisions, err := buildProtectionDecisionsWithResolutions(captureResult.sink.entries, policy, boundLocators)
	if err != nil {
		return result, err
	}
	actualProtectionDigest, err := protectionDecisionDigest(actualProtectionDecisions)
	if err != nil {
		return result, err
	}
	if actualProtectionDigest != plan.ProtectionDigest {
		return result, fmt.Errorf("%w: plan=%s current=%s", ErrIngestPlanProtectionChanged, plan.ProtectionDigest, actualProtectionDigest)
	}
	return s.executeCapturedIngestWithExecutionKey(ctx, captureResult, policy, boundLocators, executionKey)
}

func blockedIngestEntries(entries []scanner.EntryRecord, policy ingestPolicy) []IngestPlanIssue {
	return blockedIngestEntriesForMode(scanner.CaptureModePathString, entries, policy)
}

func blockedIngestEntriesForMode(captureMode scanner.CaptureMode, entries []scanner.EntryRecord, policy ingestPolicy) []IngestPlanIssue {
	blocked := make([]IngestPlanIssue, 0)
	for _, entry := range entries {
		if entry.State != scanner.EntryFailed && entry.State != scanner.EntryUnstable {
			continue
		}
		if metadataOnlyResolutionQualified(captureMode, entry, policy) {
			continue
		}
		reasonCode := "SCAN_ENTRY_" + string(entry.State)
		message := "source entry was not captured authoritatively"
		if len(entry.Issues) > 0 {
			reasonCode = entry.Issues[0].Code
			if entry.Issues[0].Message != "" {
				message = entry.Issues[0].Message
			}
		}
		rawPath := append([]byte(nil), entry.RawRelativePath...)
		if len(rawPath) == 0 {
			rawPath = []byte(entry.RelativePath)
		}
		outcome := sqlite.ProtectionUnavailable
		if entry.State == scanner.EntryUnstable {
			outcome = sqlite.ProtectionBlocked
		}
		blocked = append(blocked, IngestPlanIssue{
			RelativePath:   entry.RelativePath,
			RawPath:        rawPath,
			Mode:           policy.modeFor(entry.RelativePath),
			PlannedOutcome: outcome,
			State:          string(entry.State),
			ReasonCode:     reasonCode,
			Message:        message,
		})
	}
	sort.SliceStable(blocked, func(i, j int) bool {
		if comparison := bytes.Compare(blocked[i].RawPath, blocked[j].RawPath); comparison != 0 {
			return comparison < 0
		}
		return blocked[i].RelativePath < blocked[j].RelativePath
	})
	return blocked
}

func metadataOnlyScanResolved(captureMode scanner.CaptureMode, entries []scanner.EntryRecord, policy ingestPolicy) bool {
	if captureMode != scanner.CaptureModeRootedFD {
		return false
	}
	resolved := 0
	for _, entry := range entries {
		if entry.State == scanner.EntryUnstable {
			return false
		}
		if entry.State != scanner.EntryFailed {
			continue
		}
		if !metadataOnlyResolutionQualified(captureMode, entry, policy) {
			return false
		}
		resolved++
	}
	return resolved > 0
}

// metadataOnlyResolutionQualified proves only the namespace and metadata
// observation. It deliberately does not treat a partial digest, a changed
// file, or a path-string capture as sufficient evidence for ingest.
func metadataOnlyResolutionQualified(captureMode scanner.CaptureMode, entry scanner.EntryRecord, policy ingestPolicy) bool {
	if captureMode != scanner.CaptureModeRootedFD || entry.Kind != scanner.KindRegularFile || entry.State != scanner.EntryFailed {
		return false
	}
	if entry.RelativePath == "" || entry.RelativePath == "." || len(entry.RawRelativePath) == 0 {
		return false
	}
	if policy.modeFor(entry.RelativePath) != sqlite.ProtectionMetadataOnly {
		return false
	}
	if _, approved := policy.metadataOnlyResolutions[entry.RelativePath]; !approved {
		return false
	}
	if entry.Before == nil || entry.After == nil || !sameSnapshotValues(*entry.Before, *entry.After) {
		return false
	}
	if !entry.Boundary.Checked || entry.Boundary.Action != scanner.BoundaryInclude {
		return false
	}
	for _, issue := range entry.Issues {
		switch issue.Stage {
		case scanner.StageLstat, scanner.StageBoundary, scanner.StagePostStat, scanner.StageStability:
			return false
		}
	}
	return true
}

func sameSnapshotValues(before, after scanner.MetadataSnapshot) bool {
	return before == after
}

func cloneProtectionModes(input map[string]sqlite.ProtectionMode) map[string]sqlite.ProtectionMode {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]sqlite.ProtectionMode, len(input))
	for path, mode := range input {
		output[path] = mode
	}
	return output
}

type capturedIngest struct {
	session    *capture.Session
	binding    capture.BindingRecord
	sourceID   string
	scanID     string
	sink       *memorySink
	scanResult scanner.ScanResult
}

func (s *Service) captureForIngest(ctx context.Context, root, sourceIDHint string) (capturedIngest, error) {
	var out capturedIngest
	session, err := s.captureDriver().Open(root)
	if err != nil {
		return out, err
	}
	sink := &memorySink{}
	generationID, err := sqlite.NewStableID(sqlite.IDPrefixScanGeneration)
	if err != nil {
		_ = session.Close()
		return out, err
	}
	sourceID := sourceIDHint
	if sourceID == "" {
		sourceID, err = s.captureSourceID(ctx, session.Binding())
	}
	if err != nil {
		_ = session.Close()
		return out, err
	}
	host, err := scanner.New(scanner.Config{
		Sink:        sink,
		RootBinding: session.Root(),
		Detector: &identify.ScannerDetector{
			DetectorID:      "identify:builtin",
			DetectorVersion: identify.RulesDigest(),
			Inner:           s.detector(),
		},
	})
	if err != nil {
		_ = session.Close()
		return out, err
	}
	scanResult, scanErr := host.Scan(ctx, scanner.ScanRequest{
		GenerationID: generationID,
		SourceID:     sourceID,
		Root:         session.Binding().DisplayPath,
	})
	if scanErr != nil && scanResult.State != scanner.ScanIncomplete {
		_ = session.Close()
		return out, scanErr
	}
	return capturedIngest{
		session:    session,
		binding:    session.Binding(),
		sourceID:   sourceID,
		scanID:     generationID,
		sink:       sink,
		scanResult: scanResult,
	}, nil
}

// captureSourceID is a read-only lookup used to keep path identity aligned
// with an existing source. A new source receives an ephemeral ID that is
// passed into beginCatalog after the scan has qualified.
func (s *Service) captureSourceID(ctx context.Context, binding capture.BindingRecord) (string, error) {
	workspace, err := s.Store.GetWorkspaceByName(ctx, defaultWorkspaceName)
	if err == nil {
		source, sourceErr := s.Store.GetSourceByStableKey(ctx, workspace.ID, "local-tree:"+binding.DisplayPath)
		if sourceErr == nil {
			return source.ID, nil
		}
		if !isNotFound(sourceErr) {
			return "", sourceErr
		}
	} else if !isNotFound(err) {
		return "", err
	}
	return sqlite.NewStableID(sqlite.IDPrefixSource)
}

func (s *Service) estimateIngest(ctx context.Context, entries []scanner.EntryRecord, policy ingestPolicy) (IngestEstimate, error) {
	var estimate IngestEstimate
	seen := make(map[string]struct{})
	exactSeen := make(map[string]struct{})
	for _, entry := range entries {
		if entry.Kind != scanner.KindRegularFile {
			continue
		}
		if metadataOnlyResolutionQualified(scanner.CaptureModeRootedFD, entry, policy) {
			estimate.Files++
			if entry.Before != nil {
				estimate.Bytes += entry.Before.Size
			}
			continue
		}
		if entry.State != scanner.EntryComplete || entry.Content == nil {
			continue
		}
		estimate.Files++
		estimate.Bytes += entry.Content.BytesRead
		if _, ok := seen[entry.Content.ContentID]; !ok {
			seen[entry.Content.ContentID] = struct{}{}
			estimate.UniqueFiles++
			estimate.UniqueBytes += entry.Content.BytesRead
		}
		mode := policy.modeFor(entry.RelativePath)
		if mode == sqlite.ProtectionStoreExact || mode == sqlite.ProtectionStoreExactWithExternalFallback {
			if _, ok := exactSeen[entry.Content.ContentID]; !ok {
				exactSeen[entry.Content.ContentID] = struct{}{}
				exists, err := repositoryObjectExists(ctx, s.Repo, entry.Content.ContentID)
				if err != nil {
					return estimate, err
				}
				if !exists {
					estimate.NewBytes += entry.Content.BytesRead
				}
			}
		}
		if policy.modeFor(entry.RelativePath) == sqlite.ProtectionLinkOnly {
			estimate.LinkOnlyFiles++
		}
	}
	// Local counts follow the per-file decisions, while NewBytes is counted
	// once per content identity for exact decisions only.
	for _, entry := range entries {
		if entry.Kind != scanner.KindRegularFile || entry.State != scanner.EntryComplete || entry.Content == nil {
			continue
		}
		mode := policy.modeFor(entry.RelativePath)
		if mode == sqlite.ProtectionStoreExact || mode == sqlite.ProtectionStoreExactWithExternalFallback {
			estimate.LocalFiles++
			estimate.LocalBytes += entry.Content.BytesRead
		}
	}
	estimate.LocatorCount = len(policy.locators)
	return estimate, nil
}

func repositoryObjectExists(ctx context.Context, repo repository.Driver, contentID string) (bool, error) {
	body, err := repo.Open(ctx, contentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if body == nil {
		return true, nil
	}
	return true, body.Close()
}

// captureBasisDigest excludes volatile scan IDs and absolute paths. Hard-link
// group IDs also contain the generation ID, so only their stable facts are
// retained in the basis projection.
func captureBasisDigest(binding capture.BindingRecord, entries []scanner.EntryRecord) (string, error) {
	type basisEntry struct {
		RawPath      []byte                       `json:"raw_path"`
		RawName      []byte                       `json:"raw_name"`
		RelativePath string                       `json:"relative_path"`
		Kind         scanner.EntryKind            `json:"kind"`
		State        scanner.EntryState           `json:"state"`
		Before       *scanner.MetadataSnapshot    `json:"before,omitempty"`
		After        *scanner.MetadataSnapshot    `json:"after,omitempty"`
		Content      *scanner.ContentDigest       `json:"content,omitempty"`
		Symlink      *scanner.SymlinkFacts        `json:"symlink,omitempty"`
		HardLink     scanner.HardLinkFacts        `json:"hard_link"`
		Sparse       scanner.SparseFacts          `json:"sparse"`
		Boundary     scanner.BoundaryObservation  `json:"boundary"`
		Detection    scanner.DetectionObservation `json:"detection"`
		Issues       []scanner.Issue              `json:"issues,omitempty"`
	}
	ordered := make([]basisEntry, 0, len(entries))
	for _, entry := range entries {
		hardLink := entry.HardLink
		hardLink.GroupID = ""
		ordered = append(ordered, basisEntry{
			RawPath: append([]byte(nil), entry.RawRelativePath...), RawName: append([]byte(nil), entry.RawName...),
			RelativePath: entry.RelativePath,
			Kind:         entry.Kind, State: entry.State, Before: entry.Before, After: entry.After,
			Content: entry.Content, Symlink: entry.Symlink, HardLink: hardLink,
			Sparse: entry.Sparse, Boundary: entry.Boundary, Detection: entry.Detection,
			Issues: append([]scanner.Issue(nil), entry.Issues...),
		})
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if c := bytes.Compare(ordered[i].RawPath, ordered[j].RawPath); c != 0 {
			return c < 0
		}
		return ordered[i].RelativePath < ordered[j].RelativePath
	})
	payload, err := json.Marshal(struct {
		Schema           string       `json:"schema"`
		BindingDigest    string       `json:"binding_digest"`
		TraversalVersion string       `json:"traversal_version"`
		MetadataVersion  string       `json:"metadata_version"`
		HashVersion      string       `json:"hash_version"`
		Entries          []basisEntry `json:"entries"`
	}{
		Schema: "org.restoreweave.exact.capture-basis.v2", BindingDigest: binding.IdentityDigest(),
		TraversalVersion: scanner.TraversalVersion, MetadataVersion: scanner.MetadataVersion,
		HashVersion: scanner.HashVersion, Entries: ordered,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
