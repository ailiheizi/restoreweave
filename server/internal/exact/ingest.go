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

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/identify"
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

// Ingest captures a local tree, places exact bytes, and publishes a portable snapshot.
func (s *Service) Ingest(ctx context.Context, root string) (IngestResult, error) {
	var result IngestResult
	if err := s.require(); err != nil {
		return result, err
	}
	session, err := s.captureDriver().Open(root)
	if err != nil {
		return result, err
	}
	defer session.Close()
	binding := session.Binding()

	sink := &memorySink{}
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
		return result, err
	}

	ids, err := s.beginCatalog(ctx, binding)
	if err != nil {
		return result, err
	}
	scanResult, scanErr := host.Scan(ctx, scanner.ScanRequest{
		GenerationID: ids.scanID,
		SourceID:     ids.sourceID,
		Root:         binding.DisplayPath,
	})
	if qerr := requireQualified(sink.start.CaptureMode, scanResult); qerr != nil {
		state := sqlite.ScanFailed
		if scanResult.State == scanner.ScanIncomplete {
			state = sqlite.ScanIncomplete
		}
		_ = s.finishScan(ctx, ids.workspaceID, ids.scanID, state, false, scanResult)
		if scanErr != nil && scanResult.State != scanner.ScanIncomplete {
			return result, scanErr
		}
		return result, qerr
	}

	placed, err := s.placeFiles(ctx, session, sink.entries)
	if err != nil {
		_ = s.finishScan(ctx, ids.workspaceID, ids.scanID, sqlite.ScanFailed, false, scanResult)
		return result, err
	}

	adopted, err := s.adopt(ctx, ids, binding, sink.entries, placed)
	if err != nil {
		_ = s.finishScan(ctx, ids.workspaceID, ids.scanID, sqlite.ScanFailed, false, scanResult)
		return result, err
	}
	if err := s.finishScan(ctx, ids.workspaceID, ids.scanID, sqlite.ScanComplete, true, scanResult); err != nil {
		return result, err
	}

	manifest := Manifest{
		Schema:      SnapshotSchemaV1,
		SnapshotRef: adopted.snapshotRef,
		CreatedAt:   s.now(),
		Binding:     binding,
		Entries:     adopted.entries,
	}
	written, err := writeManifest(s.Repo.Root(), manifest)
	if err != nil {
		return result, err
	}
	if err := s.Store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.InsertPublication(ctx, &sqlite.Publication{
			ID:               adopted.publicationID,
			WorkspaceID:      ids.workspaceID,
			SnapshotRef:      written.SnapshotRef,
			ScanGenerationID: ids.scanID,
			BindingID:        ids.bindingID,
			NamespaceRootID:  adopted.rootID,
			ManifestDigest:   written.ManifestDigest,
		})
	}); err != nil {
		return result, err
	}

	result = IngestResult{
		WorkspaceID:    ids.workspaceID,
		SourceID:       ids.sourceID,
		ScanID:         ids.scanID,
		BindingID:      ids.bindingID,
		RootID:         adopted.rootID,
		SnapshotRef:    written.SnapshotRef,
		ManifestDigest: written.ManifestDigest,
		Files:          placed.files,
		Bytes:          placed.bytes,
	}
	if s.Processor != nil {
		_ = s.Processor.ProcessPublication(ctx, result.WorkspaceID, result.SnapshotRef, result.RootID)
	}
	if s.Indexer != nil {
		if _, err := s.Indexer.Rebuild(ctx, result.WorkspaceID, result.SnapshotRef, result.RootID); err != nil {
			return result, fmt.Errorf("rebuild search index: %w", err)
		}
	}
	return result, nil
}

type catalogIDs struct {
	workspaceID string
	sourceID    string
	scanID      string
	bindingID   string
}

func (s *Service) beginCatalog(ctx context.Context, binding capture.BindingRecord) (catalogIDs, error) {
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
		sourceID, idErr := sqlite.NewStableID(sqlite.IDPrefixSource)
		if idErr != nil {
			return ids, idErr
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
		scanID, err := sqlite.NewStableID(sqlite.IDPrefixScanGeneration)
		if err != nil {
			return err
		}
		bindingID, err := sqlite.NewStableID(sqlite.IDPrefixCaptureBinding)
		if err != nil {
			return err
		}
		ids.scanID = scanID
		ids.bindingID = bindingID
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
	files int
	bytes int64
}

func (s *Service) placeFiles(ctx context.Context, session *capture.Session, entries []scanner.EntryRecord) (placedSet, error) {
	fsys := scanner.NewRootedFileSystem(session.Root())
	var placed placedSet
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if entry.Kind != scanner.KindRegularFile || entry.State != scanner.EntryComplete || entry.Content == nil {
			continue
		}
		if _, ok := seen[entry.Content.ContentID]; ok {
			placed.files++
			placed.bytes += entry.Content.BytesRead
			continue
		}
		body, err := fsys.OpenRegularNoFollow(entry.AbsolutePath)
		if err != nil {
			return placed, fmt.Errorf("reopen %s: %w", entry.RelativePath, err)
		}
		receipt, err := s.Repo.PlaceExact(ctx, entry.Content.ContentID, body)
		_ = body.Close()
		if err != nil {
			return placed, fmt.Errorf("place %s: %w", entry.RelativePath, err)
		}
		if err := s.Repo.Verify(ctx, receipt.ContentID); err != nil {
			return placed, fmt.Errorf("readback %s: %w", entry.RelativePath, err)
		}
		seen[entry.Content.ContentID] = struct{}{}
		placed.files++
		placed.bytes += receipt.Bytes
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
		item := prepared{entry: entry, observationID: observationID, entryType: entryType}
		if entry.RelativePath == "." {
			rootPathID = entry.PathID
		} else if entryType != sqlite.EntryFile && entryType != sqlite.EntryDirectory && entryType != sqlite.EntrySymlink {
			preparedEntries = append(preparedEntries, item)
			continue
		} else {
			namespaceID, idErr := sqlite.NewStableID(sqlite.IDPrefixNamespaceEntry)
			if idErr != nil {
				return out, idErr
			}
			item.namespaceID = namespaceID
			pathToNamespace[entry.PathID] = namespaceID
		}
		if entryType == sqlite.EntryFile {
			if entry.Content == nil {
				return out, fmt.Errorf("%w: regular file %q has no content digest", ErrBlocked, entry.RelativePath)
			}
			fileVersionID, idErr := sqlite.NewStableID(sqlite.IDPrefixFileVersion)
			if idErr != nil {
				return out, idErr
			}
			item.fileVersionID = fileVersionID
			if existing, ok := representations[entry.Content.ContentID]; ok {
				item.representation = existing
			} else {
				repID, idErr := sqlite.NewStableID(sqlite.IDPrefixRepresentation)
				if idErr != nil {
					return out, idErr
				}
				item.representation = repID
				representations[entry.Content.ContentID] = repID
			}
		}
		preparedEntries = append(preparedEntries, item)
		out.entries = append(out.entries, toManifestEntry(entry, entryType))
	}

	err = s.Store.Update(ctx, func(tx *sqlite.Tx) error {
		insertedRep := map[string]struct{}{}
		for _, item := range preparedEntries {
			observation := observationRecord(ids, item)
			if err := tx.InsertObservation(ctx, &observation); err != nil {
				return err
			}
			if item.entryType != sqlite.EntryFile || item.representation == "" {
				continue
			}
			if _, ok := insertedRep[item.representation]; ok {
				continue
			}
			size := item.entry.Content.BytesRead
			if err := tx.InsertRepresentation(ctx, &sqlite.Representation{
				ID:                        item.representation,
				WorkspaceID:               ids.workspaceID,
				ContentID:                 item.entry.Content.ContentID,
				DecodedLength:             size,
				OwnershipMode:             sqlite.OwnershipRestoreWeavePacks,
				CodecProfileRef:           "identity/sha256-v1",
				AccessMode:                sqlite.AccessRandomNative,
				RecordDigest:              item.entry.Content.ContentID,
				WholeReadRequiredToVerify: false,
			}); err != nil {
				return err
			}
			insertedRep[item.representation] = struct{}{}
		}
		for _, item := range preparedEntries {
			if item.entryType != sqlite.EntryFile {
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
				VerificationRef:               "readback:" + item.entry.Content.ContentID,
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
		for _, item := range preparedEntries {
			if item.namespaceID == "" {
				continue
			}
			parentID := ""
			if item.entry.ParentPathID != "" && item.entry.ParentPathID != rootPathID {
				parentID = pathToNamespace[item.entry.ParentPathID]
			}
			ns := sqlite.NamespaceEntry{
				ID:              item.namespaceID,
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
		}
		return nil
	})
	return out, err
}

func requireQualified(mode scanner.CaptureMode, result scanner.ScanResult) error {
	if err := capture.MustRooted(mode); err != nil {
		return fmt.Errorf("%w: %v", ErrNotQualified, err)
	}
	if result.State != scanner.ScanComplete {
		return fmt.Errorf("%w: scan state %s", ErrNotQualified, result.State)
	}
	if result.FailedEntries != 0 || result.UnstableEntries != 0 {
		return fmt.Errorf("%w: scan has failed or unstable entries", ErrNotQualified)
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
	entry          scanner.EntryRecord
	observationID  string
	namespaceID    string
	fileVersionID  string
	representation string
	entryType      sqlite.NamespaceEntryType
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

func toManifestEntry(entry scanner.EntryRecord, entryType sqlite.NamespaceEntryType) ManifestEntry {
	item := ManifestEntry{
		RelativePath: entry.RelativePath,
		RawPath:      append([]byte(nil), entry.RawRelativePath...),
		EntryType:    string(entryType),
		ContentID:    contentIDOf(entry),
	}
	if len(item.RawPath) == 0 {
		item.RawPath = []byte(".")
	}
	if entry.Before != nil {
		size := entry.Before.Size
		item.LogicalSize = &size
		item.Mode = entry.Before.Mode
	}
	if entry.Symlink != nil {
		item.SymlinkTarget = append([]byte(nil), entry.Symlink.RawTarget...)
	}
	return item
}

func isNotFound(err error) bool {
	return errors.Is(err, sqlite.ErrNotFound)
}
