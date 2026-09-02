package exact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/scanner"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

type memorySink struct {
	start   scanner.ScanStart
	entries []scanner.EntryRecord
	result  scanner.ScanResult
}

func (sink *memorySink) BeginScan(_ context.Context, start scanner.ScanStart) error {
	sink.start = start
	return nil
}

func (sink *memorySink) PutEntry(_ context.Context, entry scanner.EntryRecord) error {
	sink.entries = append(sink.entries, entry)
	return nil
}

func (sink *memorySink) FinishScan(_ context.Context, result scanner.ScanResult) error {
	sink.result = result
	return nil
}

// Ingest captures a local tree using the effective configured protection
// policy. A zero-value Service keeps the historical STORE_EXACT behavior.
func (s *Service) Ingest(ctx context.Context, root string) (IngestResult, error) {
	return s.IngestWithOptions(ctx, root, IngestOptions{})
}

// IngestWithOptions captures a local tree and binds one explicit protection
// decision to the resulting catalog records and portable manifest.
func (s *Service) IngestWithOptions(ctx context.Context, root string, options IngestOptions) (IngestResult, error) {
	var result IngestResult
	if err := s.require(); err != nil {
		return result, err
	}
	policy, err := s.resolveIngestOptions(options)
	if err != nil {
		return result, err
	}
	captured, err := s.captureForIngest(ctx, root, "")
	if err != nil {
		return result, err
	}
	defer captured.session.Close()
	boundLocators, err := bindIngestLocators(captured.sink.entries, policy)
	if err != nil {
		return result, err
	}
	return s.executeCapturedIngest(ctx, captured, policy, boundLocators)
}

func (s *Service) executeCapturedIngest(ctx context.Context, captured capturedIngest, policy ingestPolicy, boundLocators map[string][]IngestLocator) (IngestResult, error) {
	return s.executeCapturedIngestWithExecutionKey(ctx, captured, policy, boundLocators, "")
}

func (s *Service) executeCapturedIngestWithExecutionKey(ctx context.Context, captured capturedIngest, policy ingestPolicy, boundLocators map[string][]IngestLocator, executionKey string) (IngestResult, error) {
	var result IngestResult
	if err := requireQualifiedWithEntries(captured.sink.start.CaptureMode, captured.scanResult, captured.sink.entries, policy); err != nil {
		return result, err
	}
	protectionDecisions, err := buildProtectionDecisionsWithResolutions(captured.sink.entries, policy, boundLocators)
	if err != nil {
		return result, err
	}
	protectionDigest, err := protectionDecisionDigest(protectionDecisions)
	if err != nil {
		return result, err
	}
	captureDigest, err := captureBasisDigest(captured.binding, captured.sink.entries)
	if err != nil {
		return result, err
	}
	policy.decisionDigest = protectionDigest
	ids, err := s.beginCatalog(ctx, captured.binding, captured.sourceID, captured.scanID)
	if err != nil {
		return result, err
	}
	placed, err := s.placeFiles(ctx, captured.session, captured.sink.entries, policy)
	if err != nil {
		_ = s.finishScan(ctx, ids.workspaceID, ids.scanID, sqlite.ScanFailed, false, captured.scanResult)
		return result, err
	}
	adopted, err := s.adopt(ctx, ids, captured.binding, captured.sink.entries, placed, policy, boundLocators)
	if err != nil {
		_ = s.finishScan(ctx, ids.workspaceID, ids.scanID, sqlite.ScanFailed, false, captured.scanResult)
		return result, err
	}
	publicationScanState := sqlite.ScanComplete
	publicationFullTraversal := true
	if captured.scanResult.State != scanner.ScanComplete {
		// Metadata-only resolution authorizes a namespace publication, but it
		// does not turn an incomplete source scan into a complete claim.
		publicationScanState = sqlite.ScanIncomplete
		publicationFullTraversal = false
	}
	if err := s.finishScan(ctx, ids.workspaceID, ids.scanID, publicationScanState, publicationFullTraversal, captured.scanResult); err != nil {
		return result, err
	}
	manifest := Manifest{Schema: CurrentSnapshotSchema, SnapshotRef: adopted.snapshotRef, CreatedAt: s.now(), Binding: captured.binding, ConfigDigest: s.ConfigDigest, ProtectionDigest: protectionDigest, Entries: adopted.entries}
	written, err := writeManifest(s.Repo.Root(), manifest)
	if err != nil {
		return result, err
	}
	closure, err := s.publishRecoveryClosure(ctx, adopted, written, placed, executionKey, captureDigest, protectionDigest)
	if err != nil {
		return result, err
	}
	result = IngestResult{WorkspaceID: ids.workspaceID, SourceID: ids.sourceID, ScanID: ids.scanID, BindingID: ids.bindingID, RootID: adopted.rootID, SnapshotRef: written.SnapshotRef, ManifestDigest: written.ManifestDigest, ConfigDigest: s.ConfigDigest, ProtectionDigest: protectionDigest, ProtectionMode: policy.mode, ProtectionDecisions: protectionDecisions, Files: placed.files, Bytes: placed.bytes, LocalFiles: placed.localFiles, LocalBytes: placed.localBytes, NewBytes: placed.newBytes, SavingsMeasured: placed.savingsMeasured, NewPhysicalBytes: placed.newPhysicalBytes, CompressionSavedBytes: placed.compressionSavedBytes, LinkOnlyFiles: placed.linkOnlyFiles, LocatorCount: len(policy.locators), PreparedClosureDigest: closure.PreparedDigest, PublicationCommitDigest: closure.CommitDigest, PublicationGeneration: closure.Generation}
	if placed.savingsWarning != "" {
		result.Warnings = append(result.Warnings, placed.savingsWarning)
	}
	if err := s.Store.Update(ctx, func(tx *sqlite.Tx) error {
		metadata, _ := json.Marshal(map[string]any{
			"config_digest":             s.ConfigDigest,
			"prepared_closure_digest":   closure.PreparedDigest,
			"publication_commit_digest": closure.CommitDigest,
			"publication_generation":    closure.Generation,
		})
		return tx.InsertPublication(ctx, &sqlite.Publication{ID: adopted.publicationID, WorkspaceID: ids.workspaceID, PlanDigest: executionKey, SnapshotRef: written.SnapshotRef, ScanGenerationID: ids.scanID, BindingID: ids.bindingID, NamespaceRootID: adopted.rootID, ManifestDigest: written.ManifestDigest, Metadata: metadata})
	}); err != nil {
		if closure.CommitDigest != "" {
			return result, &PublicationOutcomeError{
				PlanDigest: executionKey, SnapshotRef: written.SnapshotRef,
				PublicationID: adopted.publicationID, Role: repository.RecordPublicationCommit,
				Cause: fmt.Errorf("project committed publication %s (snapshot %s) in catalog: %w", adopted.publicationID, written.SnapshotRef, err),
			}
		}
		return result, err
	}
	var reconciliationErrs []error
	// The exact publication is already durable at this point. Optional
	// processing and indexing are post-commit branches, so their failures are
	// warnings rather than ingest failures.
	if s.Processor != nil {
		processErr := s.Processor.ProcessPublication(ctx, result.WorkspaceID, result.SnapshotRef, result.RootID)
		if processErr != nil {
			var source interface{ PublicationWarnings() []string }
			if errors.As(processErr, &source) {
				for _, warning := range source.PublicationWarnings() {
					result.Warnings = append(result.Warnings, "processor: "+warning)
				}
			} else {
				result.Warnings = append(result.Warnings, "processor: "+processErr.Error())
			}
		}
		if err := s.publishProcessorAttemptClosure(context.WithoutCancel(ctx), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
			result.Warnings = append(result.Warnings, "processor closure: "+err.Error())
			if errors.Is(err, ErrUnknownExternalOutcome) || errors.Is(err, ErrNeedsReconciliation) {
				reconciliationErrs = append(reconciliationErrs, err)
			}
		}
		if processErr != nil {
			if err := s.scheduleProcessorRetry(context.WithoutCancel(ctx), result, executionKey, processErr); err != nil {
				result.Warnings = append(result.Warnings, "processor retry: "+err.Error())
			}
		}
	}
	if err := s.publishPortableFactClosure(context.WithoutCancel(ctx), result.WorkspaceID, result.SnapshotRef, result.PublicationCommitDigest); err != nil {
		result.Warnings = append(result.Warnings, "portable fact closure: "+err.Error())
		if errors.Is(err, ErrUnknownExternalOutcome) || errors.Is(err, ErrNeedsReconciliation) {
			reconciliationErrs = append(reconciliationErrs, err)
		}
	}
	if s.Indexer != nil {
		if _, err := s.Indexer.Rebuild(ctx, result.WorkspaceID, result.SnapshotRef, result.RootID); err != nil {
			result.Warnings = append(result.Warnings, "indexer: "+err.Error())
		}
	}
	if len(reconciliationErrs) > 0 {
		return result, errors.Join(reconciliationErrs...)
	}
	return result, nil
}

type catalogIDs struct {
	workspaceID    string
	sourceID       string
	scanID         string
	bindingID      string
	scanGeneration int64
}

func (s *Service) beginCatalog(ctx context.Context, binding capture.BindingRecord, sourceIDHint, scanIDHint string) (catalogIDs, error) {
	var ids catalogIDs
	workspace, err := s.Store.GetWorkspaceByName(ctx, defaultWorkspaceName)
	if err != nil && !isNotFound(err) {
		return ids, err
	}
	createWorkspace := isNotFound(err)
	if createWorkspace {
		workspaceID, idErr := sqlite.NewStableID(sqlite.IDPrefixWorkspace)
		if idErr != nil {
			return ids, idErr
		}
		workspace = sqlite.Workspace{ID: workspaceID, Name: defaultWorkspaceName}
	}

	stableKey := "local-tree:" + binding.DisplayPath
	var source sqlite.Source
	createSource := true
	if !createWorkspace {
		source, err = s.Store.GetSourceByStableKey(ctx, workspace.ID, stableKey)
		if err != nil && !isNotFound(err) {
			return ids, err
		}
		createSource = isNotFound(err)
	}
	if createSource {
		sourceID := sourceIDHint
		if sourceID == "" {
			var idErr error
			sourceID, idErr = sqlite.NewStableID(sqlite.IDPrefixSource)
			if idErr != nil {
				return ids, idErr
			}
		}
		source = sqlite.Source{
			ID:                  sourceID,
			WorkspaceID:         workspace.ID,
			StableKey:           stableKey,
			Kind:                "LOCAL_TREE",
			Locator:             binding.DisplayPath,
			IdentityFingerprint: binding.SourceFingerprint(),
			State:               sqlite.SourceActive,
		}
	}

	err = s.Store.Update(ctx, func(tx *sqlite.Tx) error {
		if createWorkspace {
			if err := tx.InsertWorkspace(ctx, &workspace); err != nil {
				return err
			}
		}
		ids.workspaceID = workspace.ID
		if createSource {
			if err := tx.InsertSource(ctx, &source); err != nil {
				return err
			}
		}
		ids.sourceID = source.ID

		generation, err := tx.NextScanGeneration(ctx, workspace.ID, source.ID)
		if err != nil {
			return err
		}
		scanID := scanIDHint
		if scanID == "" {
			var idErr error
			scanID, idErr = sqlite.NewStableID(sqlite.IDPrefixScanGeneration)
			if idErr != nil {
				return idErr
			}
		}
		bindingID, err := sqlite.NewStableID(sqlite.IDPrefixCaptureBinding)
		if err != nil {
			return err
		}
		ids.scanID = scanID
		ids.bindingID = bindingID
		ids.scanGeneration = generation
		recordJSON, err := json.Marshal(binding)
		if err != nil {
			return err
		}
		if err := tx.InsertScanGeneration(ctx, &sqlite.ScanGeneration{
			ID:               scanID,
			WorkspaceID:      workspace.ID,
			SourceID:         source.ID,
			Generation:       generation,
			CaptureSetID:     bindingID,
			CaptureSetDigest: binding.IdentityDigest(),
			State:            sqlite.ScanRunning,
		}); err != nil {
			return err
		}
		return tx.InsertCaptureRootBinding(ctx, &sqlite.CaptureRootBinding{
			ID:               bindingID,
			WorkspaceID:      workspace.ID,
			SourceID:         source.ID,
			ScanGenerationID: scanID,
			CaptureMode:      string(binding.CaptureMode),
			Profile:          binding.Profile,
			DisplayPath:      binding.DisplayPath,
			DeviceID:         binding.DeviceID,
			Inode:            binding.Inode,
			ConsistencyClaim: binding.ConsistencyClaim,
			IdentityDigest:   binding.IdentityDigest(),
			BoundAt:          binding.BoundAt,
			Record:           recordJSON,
		})
	})
	return ids, err
}

func (s *Service) finishScan(ctx context.Context, workspaceID, scanID string, state sqlite.ScanState, full bool, result scanner.ScanResult) error {
	summary, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.Store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.FinishScanGeneration(ctx, workspaceID, scanID, state, full, summary, s.now())
	})
}

type placedSet struct {
	files                   int
	bytes                   int64
	localFiles              int
	localBytes              int64
	newBytes                int64
	newPhysicalBytes        int64
	compressionSavedBytes   int64
	savingsMeasured         bool
	savingsWarning          string
	savingsPlacementUnknown bool
	linkOnlyFiles           int
	payloadReceipts         []repository.Receipt
}

func (s *Service) placeFiles(
	ctx context.Context,
	session *capture.Session,
	entries []scanner.EntryRecord,
	policy ingestPolicy,
) (placedSet, error) {
	fsys := scanner.NewRootedFileSystem(session.Root())
	placed := placedSet{savingsMeasured: true}
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if entry.Kind != scanner.KindRegularFile {
			continue
		}
		if metadataOnlyResolutionQualified(scanner.CaptureModeRootedFD, entry, policy) {
			placed.files++
			if entry.Before != nil {
				placed.bytes += entry.Before.Size
			}
			continue
		}
		if entry.State != scanner.EntryComplete || entry.Content == nil {
			continue
		}
		placed.files++
		placed.bytes += entry.Content.BytesRead
		mode := policy.modeFor(entry.RelativePath)
		if mode == sqlite.ProtectionLinkOnly {
			placed.linkOnlyFiles++
		}
		if mode != sqlite.ProtectionStoreExact && mode != sqlite.ProtectionStoreExactWithExternalFallback {
			continue
		}
		placed.localFiles++
		placed.localBytes += entry.Content.BytesRead
		if _, ok := seen[entry.Content.ContentID]; ok {
			continue
		}
		body, err := fsys.OpenRegularNoFollow(entry.AbsolutePath)
		if err != nil {
			return placed, fmt.Errorf("reopen %s: %w", entry.RelativePath, err)
		}
		// A placement response can be lost after the repository has committed.
		// If the object was not independently verified before PlaceExact but the
		// helper later recovers it as Existed, its physical delta is unknown.
		// Keep exact ingest successful, but do not claim a measured increment.
		wasVerified := s.Repo.Verify(ctx, entry.Content.ContentID) == nil
		receipt, err := placeExactWithReadback(ctx, s.Repo, entry.Content.ContentID, entry.Content.BytesRead, body)
		_ = body.Close()
		if err != nil {
			return placed, fmt.Errorf("place %s: %w", entry.RelativePath, err)
		}
		if !validExactContentID(receipt.ContentID) || receipt.ContentID != entry.Content.ContentID || receipt.Bytes < 0 || receipt.Bytes != entry.Content.BytesRead || receipt.StoredBytes < 0 {
			placed.savingsMeasured = false
			placed.savingsWarning = "savings measurement unavailable: placement receipt is invalid"
		}
		// The exact lane already independently read back the expected identity
		// and length. Normalize those fields for publication so a malformed
		// diagnostic receipt cannot block exact preservation; StoredBytes remains
		// untrusted and only affects the optional measurement.
		receipt.ContentID = entry.Content.ContentID
		receipt.Bytes = entry.Content.BytesRead
		seen[entry.Content.ContentID] = struct{}{}
		placed.payloadReceipts = append(placed.payloadReceipts, receipt)
		if wasVerified && !receipt.Existed {
			placed.savingsPlacementUnknown = true
			if placed.savingsWarning == "" {
				placed.savingsWarning = "savings measurement unavailable: placement receipt contradicted pre-placement verification"
			}
		} else if !wasVerified && receipt.Existed {
			placed.savingsPlacementUnknown = true
			if placed.savingsWarning == "" {
				placed.savingsWarning = "savings measurement unavailable: placement response was lost before physical increment was known"
			}
		}
		if !receipt.Existed {
			placed.newBytes += receipt.Bytes
		}
	}
	for _, receipt := range placed.payloadReceipts {
		if !validExactContentID(receipt.ContentID) || receipt.Bytes < 0 || receipt.StoredBytes < 0 {
			placed.savingsMeasured = false
			placed.savingsWarning = "savings measurement unavailable: placement receipt is invalid"
			break
		}
		if !receipt.Existed {
			placed.newPhysicalBytes += receipt.StoredBytes
			if receipt.Bytes > receipt.StoredBytes {
				placed.compressionSavedBytes += receipt.Bytes - receipt.StoredBytes
			}
		}
	}
	if placed.savingsPlacementUnknown {
		placed.savingsMeasured = false
	}
	if !placed.savingsMeasured {
		placed.newPhysicalBytes = 0
		placed.compressionSavedBytes = 0
	}
	return placed, nil
}

type adopted struct {
	rootID        string
	snapshotRef   string
	publicationID string
	entries       []ManifestEntry
}

func (s *Service) adopt(
	ctx context.Context,
	ids catalogIDs,
	binding capture.BindingRecord,
	entries []scanner.EntryRecord,
	_ placedSet,
	policy ingestPolicy,
	boundLocators map[string][]IngestLocator,
) (adopted, error) {
	var out adopted
	snapshotRef, err := sqlite.NewStableID("snap")
	if err != nil {
		return out, err
	}
	rootID, err := sqlite.NewStableID(sqlite.IDPrefixNamespaceRoot)
	if err != nil {
		return out, err
	}
	publicationID, err := sqlite.NewStableID(sqlite.IDPrefixPublication)
	if err != nil {
		return out, err
	}
	out.snapshotRef = snapshotRef
	out.rootID = rootID
	out.publicationID = publicationID

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].RelativePath < entries[j].RelativePath
	})

	preparedEntries := make([]prepared, 0, len(entries))
	representations := map[string]string{}
	pathToNamespace := map[string]string{}
	var rootPathID string

	for _, entry := range entries {
		entryType, ok := namespaceType(entry.Kind)
		if !ok {
			continue
		}
		observationID, err := sqlite.NewStableID(sqlite.IDPrefixObservation)
		if err != nil {
			return out, err
		}
		item := prepared{
			entry: entry, observationID: observationID, entryType: entryType,
			protectionMode: policy.modeFor(entry.RelativePath), protectionDigest: policy.decisionDigest,
		}
		item.protectionOutcome, item.protectionReason = protectionOutcome(entry, item.protectionMode)
		if entry.RelativePath == "." {
			rootPathID = entry.PathID
			continue
		} else {
			// Special entries are metadata-only, but they still participate in
			// the namespace and protection projections. No content is opened or
			// placed for them; insertPreparedProtection records UNAVAILABLE.
			namespaceID, idErr := sqlite.NewStableID(sqlite.IDPrefixNamespaceEntry)
			if idErr != nil {
				return out, idErr
			}
			item.namespaceID = namespaceID
			pathToNamespace[entry.PathID] = namespaceID
			protectionID, idErr := sqlite.NewStableID(sqlite.IDPrefixProtectionRecord)
			if idErr != nil {
				return out, idErr
			}
			item.protectionID = protectionID
		}
		if entryType == sqlite.EntryFile {
			if entry.Content == nil && !(item.protectionMode == sqlite.ProtectionMetadataOnly && metadataOnlyEntryEvidence(entry)) {
				return out, fmt.Errorf("%w: regular file %q has no content digest", ErrBlocked, entry.RelativePath)
			}
			if entry.Content != nil {
				fileVersionID, idErr := sqlite.NewStableID(sqlite.IDPrefixFileVersion)
				if idErr != nil {
					return out, idErr
				}
				item.fileVersionID = fileVersionID
				representationKey := entry.Content.ContentID + "\x00" + string(item.protectionMode)
				if existing, ok := representations[representationKey]; ok {
					item.representation = existing
				} else {
					repID, idErr := sqlite.NewStableID(sqlite.IDPrefixRepresentation)
					if idErr != nil {
						return out, idErr
					}
					item.representation = repID
					representations[representationKey] = repID
				}
				if item.protectionMode == sqlite.ProtectionStoreExact || item.protectionMode == sqlite.ProtectionStoreExactWithExternalFallback {
					recoveryID, idErr := sqlite.NewStableID(sqlite.IDPrefixRecoveryReference)
					if idErr != nil {
						return out, idErr
					}
					item.recoveryID = recoveryID
				}
			}
			for priority, locator := range boundLocators[entry.PathID] {
				if item.externalBindingID == "" {
					bindingID, idErr := sqlite.NewStableID(sqlite.IDPrefixExternalBinding)
					if idErr != nil {
						return out, idErr
					}
					externalRecoveryID, idErr := sqlite.NewStableID(sqlite.IDPrefixRecoveryReference)
					if idErr != nil {
						return out, idErr
					}
					item.externalBindingID = bindingID
					item.externalRecoveryID = externalRecoveryID
				}
				locatorID, idErr := sqlite.NewStableID(sqlite.IDPrefixExternalLocator)
				if idErr != nil {
					return out, idErr
				}
				item.externalLocators = append(item.externalLocators, preparedLocator{
					id: locatorID, priority: int64(priority), locator: locator,
				})
			}
		}
		if err := validatePreparedProtection(item); err != nil {
			return out, fmt.Errorf("%w: %v", ErrBlocked, err)
		}
		preparedEntries = append(preparedEntries, item)
	}

	err = s.Store.Update(ctx, func(tx *sqlite.Tx) error {
		// Allocate/reuse stable subjects inside the same transaction that
		// publishes namespace observations. Only committed publications are
		// considered by ResolveNamespaceSubjectRef, so an abandoned scan can
		// never become the continuity basis.
		for index := range preparedEntries {
			if preparedEntries[index].namespaceID == "" {
				continue
			}
			item := &preparedEntries[index]
			subjectRef, err := tx.ResolveNamespaceSubjectRef(
				ctx, ids.workspaceID, ids.sourceID, fullPathKey(item.entry), item.entryType)
			if err != nil {
				return err
			}
			if subjectRef == "" {
				subjectRef, err = sqlite.NewStableID(sqlite.IDPrefixSubject)
				if err != nil {
					return err
				}
			}
			item.subjectRef = subjectRef
		}
		insertedRep := map[string]struct{}{}
		for _, item := range preparedEntries {
			if item.observationID != "" {
				observation := observationRecord(ids, item)
				if err := tx.InsertObservation(ctx, &observation); err != nil {
					return err
				}
				if err := insertDetectionEvidence(ctx, tx, ids.workspaceID, item.observationID, item.entry.Detection, s.now()); err != nil {
					return err
				}
			}
			if item.entryType != sqlite.EntryFile || item.representation == "" {
				continue
			}
			if _, ok := insertedRep[item.representation]; ok {
				continue
			}
			if item.entry.Content == nil {
				continue
			}
			size := item.entry.Content.BytesRead
			representation := representationProfile(item.protectionMode)
			if err := tx.InsertRepresentation(ctx, &sqlite.Representation{
				ID:                        item.representation,
				WorkspaceID:               ids.workspaceID,
				ContentID:                 item.entry.Content.ContentID,
				DecodedLength:             size,
				OwnershipMode:             representation.ownership,
				CodecProfileRef:           representation.codec,
				AccessMode:                representation.access,
				RecordDigest:              item.entry.Content.ContentID,
				WholeReadRequiredToVerify: representation.wholeRead,
				Metadata:                  representation.metadata,
			}); err != nil {
				return err
			}
			insertedRep[item.representation] = struct{}{}
		}
		for index := range preparedEntries {
			item := &preparedEntries[index]
			if item.entryType != sqlite.EntryFile {
				continue
			}
			if item.entry.Content == nil {
				continue
			}
			size := item.entry.Content.BytesRead
			extentDigest := item.entry.Content.ContentID
			if err := tx.InsertFileVersion(ctx, &sqlite.FileVersion{
				ID:                            item.fileVersionID,
				WorkspaceID:                   ids.workspaceID,
				ScanGenerationID:              ids.scanID,
				ObservationID:                 item.observationID,
				AssetID:                       "asset:" + item.entry.PathID,
				ContentID:                     item.entry.Content.ContentID,
				LogicalSize:                   size,
				HashingProfile:                scanner.HashVersion,
				AuthoritativeRepresentationID: item.representation,
				ExtentSetDigest:               extentDigest,
				HardlinkGroupID:               item.entry.HardLink.GroupID,
				VerificationRef:               fileVersionVerificationRef(*item),
				RecordDigest:                  item.entry.Content.ContentID,
			}); err != nil {
				return err
			}
			if size > 0 {
				extentID, idErr := sqlite.NewStableID(sqlite.IDPrefixContentExtent)
				if idErr != nil {
					return idErr
				}
				if err := tx.InsertContentExtent(ctx, &sqlite.ContentExtent{
					ID:                   extentID,
					WorkspaceID:          ids.workspaceID,
					FileVersionID:        item.fileVersionID,
					Ordinal:              0,
					LogicalOffset:        0,
					LogicalLength:        size,
					Kind:                 sqlite.ExtentData,
					RepresentationID:     item.representation,
					RepresentationOffset: 0,
					ExtentDigest:         item.entry.Content.ContentID,
				}); err != nil {
					return err
				}
			}
		}
		if err := tx.InsertNamespaceRoot(ctx, &sqlite.NamespaceRoot{
			ID:                  rootID,
			WorkspaceID:         ids.workspaceID,
			SourceID:            ids.sourceID,
			ScanGenerationID:    ids.scanID,
			SnapshotRef:         snapshotRef,
			Name:                path.Base(binding.DisplayPath),
			RootPathKey:         []byte{},
			FilesystemSemantics: "posix",
			AuthorityDigest:     binding.IdentityDigest(),
		}); err != nil {
			return err
		}
		for index := range preparedEntries {
			item := &preparedEntries[index]
			if item.namespaceID == "" {
				continue
			}
			parentID := ""
			if item.entry.ParentPathID != "" && item.entry.ParentPathID != rootPathID {
				parentID = pathToNamespace[item.entry.ParentPathID]
			}
			ns := sqlite.NamespaceEntry{
				ID:              item.namespaceID,
				SubjectRef:      item.subjectRef,
				WorkspaceID:     ids.workspaceID,
				RootID:          rootID,
				ParentID:        parentID,
				ObservationID:   item.observationID,
				RawName:         item.entry.RawName,
				DisplayName:     item.entry.Name,
				FullPathKey:     fullPathKey(item.entry),
				EntryType:       item.entryType,
				ContentID:       contentIDOf(item.entry),
				FileVersionID:   item.fileVersionID,
				HardlinkGroupID: item.entry.HardLink.GroupID,
			}
			if item.entry.Before != nil {
				size := item.entry.Before.Size
				ns.LogicalSize = &size
				if item.entry.Before.BlocksKnown {
					allocated := int64(item.entry.Before.Blocks * 512)
					ns.AllocatedSize = &allocated
				}
				if raw, err := json.Marshal(item.entry.Before); err == nil {
					ns.Metadata = raw
				}
			}
			if item.entryType == sqlite.EntrySymlink && item.entry.Symlink != nil {
				ns.SymlinkTargetRaw = item.entry.Symlink.RawTarget
				ns.SymlinkTargetDisplay = string(item.entry.Symlink.RawTarget)
			}
			if err := tx.InsertNamespaceEntry(ctx, &ns); err != nil {
				return err
			}
			protectionID, err := insertPreparedProtection(ctx, tx, ids.workspaceID, *item)
			if err != nil {
				return err
			}
			item.protectionID = protectionID
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	// Build manifests only after the current protection projection has been
	// revised, then replace the provisional record IDs with the actual stable
	// row IDs returned by the upsert.
	for index := range preparedEntries {
		manifestEntry, manifestErr := toManifestEntry(
			preparedEntries[index].entry, preparedEntries[index].entryType,
			preparedEntries[index], binding)
		if manifestErr != nil {
			return out, manifestErr
		}
		out.entries = append(out.entries, manifestEntry)
	}
	return out, err
}

func metadataOnlyEntryEvidence(entry scanner.EntryRecord) bool {
	return entry.Before != nil && entry.After != nil && sameSnapshotValues(*entry.Before, *entry.After) &&
		entry.Boundary.Checked && entry.Boundary.Action == scanner.BoundaryInclude
}

func requireQualified(mode scanner.CaptureMode, result scanner.ScanResult) error {
	return requireQualifiedWithEntries(mode, result, nil, ingestPolicy{})
}

func requireQualifiedWithEntries(mode scanner.CaptureMode, result scanner.ScanResult, entries []scanner.EntryRecord, policy ingestPolicy) error {
	if err := capture.MustRooted(mode); err != nil {
		return fmt.Errorf("%w: %v", ErrNotQualified, err)
	}
	if result.State != scanner.ScanComplete && result.State != scanner.ScanIncomplete {
		return fmt.Errorf("%w: scan state %s", ErrNotQualified, result.State)
	}
	if result.State == scanner.ScanComplete {
		if result.FailedEntries != 0 || result.UnstableEntries != 0 {
			return fmt.Errorf("%w: complete scan reports failed or unstable entries", ErrNotQualified)
		}
		return nil
	}
	if !metadataOnlyScanResolved(mode, entries, policy) {
		return fmt.Errorf("%w: scan has failed or unstable entries", ErrNotQualified)
	}
	var resolved uint64
	for _, entry := range entries {
		if metadataOnlyResolutionQualified(mode, entry, policy) {
			resolved++
		}
	}
	if result.UnstableEntries != 0 || result.FailedEntries != resolved {
		return fmt.Errorf("%w: scan issue accounting does not match explicit metadata-only resolutions", ErrNotQualified)
	}
	return nil
}

func namespaceType(kind scanner.EntryKind) (sqlite.NamespaceEntryType, bool) {
	switch kind {
	case scanner.KindRegularFile:
		return sqlite.EntryFile, true
	case scanner.KindDirectory:
		return sqlite.EntryDirectory, true
	case scanner.KindSymlink:
		return sqlite.EntrySymlink, true
	case scanner.KindNamedPipe:
		return sqlite.EntryFIFO, true
	case scanner.KindSocket:
		return sqlite.EntrySocket, true
	case scanner.KindBlockDevice, scanner.KindCharDevice:
		return sqlite.EntryDevice, true
	case scanner.KindIrregular, scanner.KindUnknown:
		return sqlite.EntrySpecial, true
	default:
		return "", false
	}
}

type prepared struct {
	entry              scanner.EntryRecord
	observationID      string
	namespaceID        string
	subjectRef         string
	protectionID       string
	recoveryID         string
	externalRecoveryID string
	externalBindingID  string
	externalLocators   []preparedLocator
	fileVersionID      string
	representation     string
	entryType          sqlite.NamespaceEntryType
	protectionMode     sqlite.ProtectionMode
	protectionOutcome  sqlite.ProtectionOutcome
	protectionReason   string
	protectionDigest   string
}

type preparedLocator struct {
	id       string
	priority int64
	locator  IngestLocator
}

func observationRecord(ids catalogIDs, item prepared) sqlite.Observation {
	rawPath := item.entry.RawRelativePath
	if len(rawPath) == 0 {
		rawPath = []byte(".")
	}
	contentID := contentIDOf(item.entry)
	statDigest := "sha256:" + hex.EncodeToString(statPayload(item.entry))
	record := sqlite.Observation{
		ID:               item.observationID,
		WorkspaceID:      ids.workspaceID,
		SourceID:         ids.sourceID,
		ScanGenerationID: ids.scanID,
		PathKey:          []byte(item.entry.PathID),
		RawPath:          rawPath,
		DisplayPath:      item.entry.RelativePath,
		EntryType:        item.entryType,
		ContentID:        contentID,
		FileVersionID:    item.fileVersionID,
		StatDigest:       statDigest,
		ReadState:        string(item.entry.State),
	}
	if item.entry.Before != nil {
		size := item.entry.Before.Size
		record.LogicalSize = &size
	}
	return record
}

// insertDetectionEvidence projects the scanner's immutable detection result
// into the durable catalog. The scanner result intentionally remains the
// source shape; this row stores each Method/Value line while retaining the
// result-level detector and candidate fields needed by lexical discovery.
func insertDetectionEvidence(ctx context.Context, tx *sqlite.Tx, workspaceID, observationID string, observation scanner.DetectionObservation, observedAt time.Time) error {
	result := observation.Result
	if observation.State != scanner.DetectionSucceeded || len(result.Evidence) == 0 {
		return nil
	}
	detectorID := strings.TrimSpace(result.DetectorID)
	detectorVersion := strings.TrimSpace(result.DetectorVersion)
	if detectorID == "" || detectorVersion == "" {
		// A successful result without detector identity is not durable detector
		// evidence. Do not invent an identity merely to populate a required row.
		return nil
	}
	detectorDigest := detectorVersion
	if !strings.HasPrefix(detectorDigest, "sha256:") {
		detectorDigest = DigestBytes([]byte("restoreweave.detector:" + detectorID + "\x00" + detectorVersion))
	}
	// The scanner detector runs in-process under host control; this digest
	// identifies that policy and does not claim a separate sandbox boundary.
	sandboxPolicyHash := DigestBytes([]byte("restoreweave.detection.host.in-process.v1"))
	for index, evidence := range result.Evidence {
		kind := strings.ToUpper(strings.TrimSpace(evidence.Method))
		if kind == "" {
			kind = "RESULT"
		}
		payload := struct {
			Method          string  `json:"method"`
			Value           string  `json:"value"`
			DetectorID      string  `json:"detector_id"`
			DetectorVersion string  `json:"detector_version"`
			FormatID        string  `json:"format_id,omitempty"`
			MediaType       string  `json:"media_type,omitempty"`
			Confidence      float64 `json:"confidence"`
		}{
			Method: evidence.Method, Value: evidence.Value,
			DetectorID: detectorID, DetectorVersion: detectorVersion,
			FormatID: result.FormatID, MediaType: result.MediaType,
			Confidence: result.Confidence,
		}
		evidenceJSON, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal detection evidence %d: %w", index, err)
		}
		evidenceID, err := sqlite.NewStableID(sqlite.IDPrefixDetectionEvidence)
		if err != nil {
			return err
		}
		confidence := result.Confidence
		if err := tx.InsertDetectionEvidence(ctx, &sqlite.DetectionEvidence{
			ID: evidenceID, WorkspaceID: workspaceID, ObservationID: observationID,
			DetectorID: detectorID, DetectorDigest: detectorDigest,
			EvidenceKind: kind, CandidateFormat: result.FormatID,
			CandidateMIME: result.MediaType, Confidence: &confidence,
			ExecutionClass: "BYTE_DETERMINISTIC", Evidence: evidenceJSON,
			EvidenceDigest: DigestBytes(evidenceJSON), SandboxPolicyHash: sandboxPolicyHash,
			StartedAt: observedAt, FinishedAt: observedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func contentIDOf(entry scanner.EntryRecord) string {
	if entry.Content != nil {
		return entry.Content.ContentID
	}
	return ""
}

func fullPathKey(entry scanner.EntryRecord) []byte {
	if len(entry.RawRelativePath) > 0 {
		return append([]byte(nil), entry.RawRelativePath...)
	}
	return []byte(entry.RelativePath)
}

func statPayload(entry scanner.EntryRecord) []byte {
	payload, err := json.Marshal(entry.Before)
	if err != nil {
		sum := sha256.Sum256(nil)
		return sum[:]
	}
	sum := sha256.Sum256(payload)
	return sum[:]
}

func toManifestEntry(entry scanner.EntryRecord, entryType sqlite.NamespaceEntryType, preparedItem prepared, binding capture.BindingRecord) (ManifestEntry, error) {
	facts, err := buildManifestEntryFacts(entry, binding)
	if err != nil {
		return ManifestEntry{}, err
	}
	item := ManifestEntry{
		RelativePath:    entry.RelativePath,
		RawPath:         append([]byte(nil), entry.RawRelativePath...),
		RawName:         append([]byte(nil), entry.RawName...),
		EntryType:       string(entryType),
		ContentID:       contentIDOf(entry),
		HardlinkGroupID: entry.HardLink.GroupID,
		ReadState:       string(entry.State),
		Facts:           facts,
		Protection: ManifestProtection{
			RecordID:   preparedItem.protectionID,
			Mode:       string(sqlite.ProtectionMetadataOnly),
			Outcome:    string(sqlite.ProtectionUnavailable),
			ReasonCode: "NON_FILE_ENTRY",
		},
	}
	if entryType == sqlite.EntryFile && (entry.Content != nil || preparedItem.protectionMode == sqlite.ProtectionMetadataOnly) {
		size := int64(0)
		if entry.Content != nil {
			size = entry.Content.BytesRead
		}
		item.Protection.Mode = string(preparedItem.protectionMode)
		item.Protection.Outcome = string(preparedItem.protectionOutcome)
		item.Protection.ReasonCode = preparedItem.protectionReason
		if entry.Content == nil {
			if entry.Before != nil {
				size = entry.Before.Size
			}
			item.Protection.ExpectedLogicalLength = &size
		} else {
			item.Protection.ExpectedContentID = entry.Content.ContentID
			item.Protection.ExpectedLogicalLength = &size
		}
		if entry.Content == nil {
			// Metadata-only resolution has no representation or recovery route.
			if len(item.RawPath) == 0 {
				item.RawPath = []byte(".")
			}
		} else {
			switch preparedItem.protectionMode {
			case sqlite.ProtectionStoreExact, sqlite.ProtectionStoreExactWithExternalFallback:
				recipe, verification, err := exactRecoveryRecords(entry.Content.ContentID, size, preparedItem.representation)
				if err != nil {
					return ManifestEntry{}, err
				}
				item.Protection.LocalRepresentationID = preparedItem.representation
				item.Protection.RecoveryReferences = append(item.Protection.RecoveryReferences, ManifestRecoveryReference{
					ReferenceID:      preparedItem.recoveryID,
					Kind:             string(sqlite.RecoveryExactRepresentation),
					Claim:            string(sqlite.RecoveryClaimRestoreVerified),
					Priority:         0,
					RepresentationID: preparedItem.representation,
					CodecProfile:     "identity/sha256-v1",
					Status:           "VERIFIED",
					Recipe:           recipe,
					Verification:     verification,
				})
			case sqlite.ProtectionLinkOnly:
			case sqlite.ProtectionMetadataOnly:
			}
			if preparedItem.externalBindingID != "" {
				external, err := manifestExternalReference(preparedItem, int64(len(item.Protection.RecoveryReferences)))
				if err != nil {
					return ManifestEntry{}, err
				}
				item.Protection.RecoveryReferences = append(item.Protection.RecoveryReferences, external)
			}
		}
	}
	if len(item.RawPath) == 0 {
		item.RawPath = []byte(".")
	}
	if entry.Before != nil {
		size := entry.Before.Size
		item.LogicalSize = &size
		item.Mode = entry.Before.Mode
		item.MetadataBefore, _ = json.Marshal(entry.Before)
		if entry.Before.BlocksKnown {
			allocated := int64(entry.Before.Blocks * 512)
			item.AllocatedSize = &allocated
		}
	}
	if entry.After != nil {
		item.MetadataAfter, _ = json.Marshal(entry.After)
	}
	if len(entry.Issues) > 0 {
		item.Issues = make([]ManifestIssue, 0, len(entry.Issues))
		for _, issue := range entry.Issues {
			item.Issues = append(item.Issues, ManifestIssue{Stage: string(issue.Stage), Code: issue.Code, Message: issue.Message})
		}
	}
	if entry.Symlink != nil {
		item.SymlinkTarget = append([]byte(nil), entry.Symlink.RawTarget...)
	}
	return item, nil
}

func buildManifestEntryFacts(entry scanner.EntryRecord, binding capture.BindingRecord) (*ManifestEntryFacts, error) {
	sourceProfile := binding.Schema + "/" + binding.Profile + "/" + string(binding.CaptureMode) + "/" + scanner.MetadataVersion + "/" + scanner.FilesystemFactsVersion
	capturedAt := binding.BoundAt.UTC()
	if !entry.Filesystem.CapturedAt.IsZero() {
		capturedAt = entry.Filesystem.CapturedAt.UTC()
	}
	hardLinkState := PortableFactObserved
	hardLinkValueState := string(entry.HardLink.State)
	switch entry.HardLink.State {
	case "":
		hardLinkState = PortableFactUnobserved
		hardLinkValueState = "UNRECORDED"
	case scanner.HardLinkNotApplicable:
		hardLinkState = PortableFactNotApplicable
	case scanner.HardLinkUnknown:
		hardLinkState = PortableFactUnobserved
	case scanner.HardLinkSingle, scanner.HardLinkMultiple:
	default:
		hardLinkState = PortableFactInconsistent
	}
	sparseState := PortableFactObserved
	sparseValueState := string(entry.Sparse.State)
	switch entry.Sparse.State {
	case "":
		sparseState = PortableFactUnobserved
		sparseValueState = "UNRECORDED"
	case scanner.SparseNotApplicable:
		sparseState = PortableFactNotApplicable
	case scanner.SparseUnknown:
		sparseState = PortableFactUnobserved
	case scanner.SparseNotIndicated, scanner.SparseAllocationBelowSize:
	default:
		sparseState = PortableFactInconsistent
	}
	boundaryState := PortableFactUnobserved
	boundaryAction := string(entry.Boundary.Action)
	if entry.Boundary.Checked {
		boundaryState = PortableFactObserved
	} else if boundaryAction == "" {
		boundaryAction = "UNRECORDED"
	}
	detectionState := PortableFactUnobserved
	detectionValueState := string(entry.Detection.State)
	switch entry.Detection.State {
	case "":
		detectionValueState = "UNRECORDED"
	case scanner.DetectionSucceeded:
		detectionState = PortableFactObserved
	case scanner.DetectionNotRequested:
		if entry.Kind != scanner.KindRegularFile {
			detectionState = PortableFactNotApplicable
		}
	case scanner.DetectionFailed:
		detectionState = PortableFactObserved
	default:
		detectionState = PortableFactInconsistent
	}
	detectionEvidence := make([]PortableDetectionEvidence, 0, len(entry.Detection.Result.Evidence))
	for _, evidence := range entry.Detection.Result.Evidence {
		detectionEvidence = append(detectionEvidence, PortableDetectionEvidence{Method: evidence.Method, Value: evidence.Value})
	}

	type factInput struct {
		name      string
		state     PortableFactState
		authority string
		value     any
	}
	unsupported := func(reason string) PortableUnsupportedValue {
		return PortableUnsupportedValue{ReasonCode: reason}
	}
	xattrState, xattrValue := portableXAttrFact(entry.Filesystem.XAttrs)
	aclState, aclValue := portableACLFact(entry.Filesystem.ACLs)
	xattrAuthority := portableFactAuthority(xattrState)
	aclAuthority := portableFactAuthority(aclState)
	extentState := PortableFactUnsupported
	extentValue := PortableUnsupportedValue{ReasonCode: "CAPTURE_PROFILE_DOES_NOT_EMIT_EXTENT_MAP"}
	if entry.Kind != scanner.KindRegularFile {
		extentState = PortableFactNotApplicable
		extentValue.ReasonCode = "NOT_A_REGULAR_FILE"
	} else if entry.Sparse.ExtentMapCaptured {
		extentState = PortableFactInconsistent
		extentValue = PortableUnsupportedValue{ReasonCode: "EXTENT_MAP_REPORTED_BUT_NOT_PROJECTED", CaptureReported: true}
	}
	inputs := []factInput{
		{name: PortableFactSparseExtents, state: extentState, authority: "CAPTURE_PROFILE_DECLARATION", value: extentValue},
		{name: PortableFactSparseIndication, state: sparseState, authority: "CAPTURE_OBSERVATION", value: PortableSparseIndicationValue{
			State: sparseValueState, LogicalBytes: entry.Sparse.LogicalBytes,
			AllocatedBytes: entry.Sparse.AllocatedBytes, Evidence: entry.Sparse.Evidence,
		}},
		{name: PortableFactDetection, state: detectionState, authority: "DETECTION_OBSERVATION", value: PortableDetectionValue{
			State: detectionValueState, DetectorID: entry.Detection.Result.DetectorID,
			DetectorVersion: entry.Detection.Result.DetectorVersion, FormatID: entry.Detection.Result.FormatID,
			MediaType: entry.Detection.Result.MediaType, Confidence: entry.Detection.Result.Confidence,
			Evidence: detectionEvidence,
		}},
		{name: PortableFactACLs, state: aclState, authority: aclAuthority, value: aclValue},
		{name: PortableFactAlternateStreams, state: PortableFactUnsupported, authority: "CAPTURE_PROFILE_DECLARATION", value: unsupported("CAPTURE_PROFILE_DOES_NOT_OBSERVE_ALTERNATE_STREAMS")},
		{name: PortableFactFlags, state: PortableFactUnsupported, authority: "CAPTURE_PROFILE_DECLARATION", value: unsupported("CAPTURE_PROFILE_DOES_NOT_OBSERVE_FILE_FLAGS")},
		{name: PortableFactResourceForks, state: PortableFactUnsupported, authority: "CAPTURE_PROFILE_DECLARATION", value: unsupported("CAPTURE_PROFILE_DOES_NOT_OBSERVE_RESOURCE_FORKS")},
		{name: PortableFactXAttrs, state: xattrState, authority: xattrAuthority, value: xattrValue},
		{name: PortableFactBoundary, state: boundaryState, authority: "CAPTURE_OBSERVATION", value: PortableBoundaryValue{
			Checked: entry.Boundary.Checked, Action: boundaryAction, Reason: entry.Boundary.Reason,
		}},
		{name: PortableFactHardLink, state: hardLinkState, authority: "CAPTURE_OBSERVATION", value: PortableHardLinkValue{
			State: hardLinkValueState, GroupIDVersion: entry.HardLink.GroupIDVersion,
			GroupID: entry.HardLink.GroupID, LinkCount: entry.HardLink.LinkCount,
		}},
	}
	facts := &ManifestEntryFacts{Schema: PortableFactsSchemaV1, Facts: make([]ManifestPortableFact, 0, len(inputs))}
	for _, input := range inputs {
		fact, err := newManifestPortableFact(input.name, input.state, sourceProfile, input.authority, capturedAt, input.value)
		if err != nil {
			return nil, err
		}
		facts.Facts = append(facts.Facts, fact)
	}
	if err := facts.validate(); err != nil {
		return nil, err
	}
	return facts, nil
}

func portableCaptureFactState(state scanner.CaptureFactState, valueState string) PortableFactState {
	switch state {
	case scanner.CaptureFactObserved:
		return PortableFactObserved
	case scanner.CaptureFactUnobserved:
		return PortableFactUnobserved
	case scanner.CaptureFactUnsupported:
		return PortableFactUnsupported
	case scanner.CaptureFactInconsistent:
		return PortableFactInconsistent
	default:
		if valueState == "UNSUPPORTED" {
			return PortableFactUnsupported
		}
		return PortableFactUnobserved
	}
}

func portableFactAuthority(state PortableFactState) string {
	if state == PortableFactUnsupported {
		return "CAPTURE_PROFILE_DECLARATION"
	}
	return "CAPTURE_OBSERVATION"
}

func portableXAttrFact(facts scanner.XAttrFacts) (PortableFactState, PortableXAttrValue) {
	state := facts.State
	value := PortableXAttrValue{
		State:      string(state),
		Attributes: make([]PortableXAttr, 0, len(facts.Attributes)),
		ReasonCode: facts.ReasonCode,
	}
	for _, attribute := range facts.Attributes {
		value.Attributes = append(value.Attributes, PortableXAttr{Name: attribute.Name, Value: append([]byte(nil), attribute.Value...)})
	}
	sort.SliceStable(value.Attributes, func(i, j int) bool {
		return value.Attributes[i].Name < value.Attributes[j].Name
	})
	if value.State == "" {
		state = scanner.CaptureFactUnsupported
		value.State = "UNSUPPORTED"
		value.ReasonCode = "CAPTURE_PROFILE_DID_NOT_EMIT_XATTRS"
	}
	return portableCaptureFactState(state, value.State), value
}

func portableACLFact(facts scanner.ACLFacts) (PortableFactState, PortableACLValue) {
	state := facts.State
	value := PortableACLValue{
		State:      string(state),
		Format:     facts.Format,
		Records:    make([]PortableACLRecord, 0, len(facts.Records)),
		ReasonCode: facts.ReasonCode,
	}
	for _, record := range facts.Records {
		value.Records = append(value.Records, PortableACLRecord{Name: record.Name, Raw: append([]byte(nil), record.Raw...)})
	}
	sort.SliceStable(value.Records, func(i, j int) bool {
		return value.Records[i].Name < value.Records[j].Name
	})
	if value.State == "" {
		state = scanner.CaptureFactUnsupported
		value.State = "UNSUPPORTED"
		value.ReasonCode = "CAPTURE_PROFILE_DID_NOT_EMIT_ACLS"
	}
	return portableCaptureFactState(state, value.State), value
}

func newManifestPortableFact(name string, state PortableFactState, sourceProfile, authority string, capturedAt time.Time, value any) (ManifestPortableFact, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return ManifestPortableFact{}, err
	}
	fact := ManifestPortableFact{
		Name: name, State: state, SourceProfile: sourceProfile, Authority: authority,
		CapturedAt: capturedAt.UTC(), CaptureTimeBasis: "CAPTURE_SESSION_BOUND_AT", Value: payload,
	}
	fact.ProvenanceDigest, err = fact.Digest()
	return fact, err
}

func exactRecoveryRecords(contentID string, logicalLength int64, representationID string) (json.RawMessage, json.RawMessage, error) {
	recipe, err := json.Marshal(map[string]any{
		"schema":         "org.restoreweave.recovery-recipe.v1",
		"content_id":     contentID,
		"logical_length": logicalLength,
		"representation": representationID,
		"codec_profile":  "identity/sha256-v1",
	})
	if err != nil {
		return nil, nil, err
	}
	verification, err := json.Marshal(map[string]any{
		"verified":     true,
		"verification": "repository-readback",
		"content_id":   contentID,
	})
	if err != nil {
		return nil, nil, err
	}
	return recipe, verification, nil
}

func isNotFound(err error) bool {
	return errors.Is(err, sqlite.ErrNotFound)
}
