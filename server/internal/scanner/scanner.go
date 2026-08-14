package scanner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultHashBufferBytes = 256 * 1024
	defaultProbeBytes      = 64 * 1024
	maxEmptyReads          = 100
)

type Config struct {
	FileSystem      FileSystem
	Sink            Sink
	Detector        Detector
	BoundaryChecker BoundaryChecker
	RootBinding     *CaptureRoot
	HashBufferBytes int
	ProbeBytes      int
	Clock           func() time.Time
}

type Scanner struct {
	fileSystem      FileSystem
	sink            Sink
	detector        Detector
	boundaryChecker BoundaryChecker
	rootBinding     *CaptureRoot
	hashBufferBytes int
	probeBytes      int
	clock           func() time.Time
}

func New(config Config) (*Scanner, error) {
	if config.Sink == nil {
		return nil, fmt.Errorf("%w: sink is required", ErrInvalidRequest)
	}
	if config.HashBufferBytes < 0 {
		return nil, fmt.Errorf("%w: hash buffer size cannot be negative", ErrInvalidRequest)
	}
	if config.ProbeBytes < 0 {
		return nil, fmt.Errorf("%w: probe size cannot be negative", ErrInvalidRequest)
	}

	if config.FileSystem == nil {
		config.FileSystem = OSFileSystem{}
	}
	if config.HashBufferBytes == 0 {
		config.HashBufferBytes = defaultHashBufferBytes
	}
	if config.ProbeBytes == 0 {
		config.ProbeBytes = defaultProbeBytes
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}

	return &Scanner{
		fileSystem:      config.FileSystem,
		sink:            config.Sink,
		detector:        config.Detector,
		boundaryChecker: config.BoundaryChecker,
		rootBinding:     config.RootBinding,
		hashBufferBytes: config.HashBufferBytes,
		probeBytes:      config.ProbeBytes,
		clock:           config.Clock,
	}, nil
}

// Scan performs a deterministic depth-first traversal. Directory records are
// emitted after their children so their post-traversal lstat can be included
// without buffering the complete namespace in memory.
func (scanner *Scanner) Scan(ctx context.Context, request ScanRequest) (ScanResult, error) {
	result := ScanResult{
		GenerationID: request.GenerationID,
		SourceID:     request.SourceID,
		Root:         request.Root,
		State:        ScanFailed,
	}
	if err := validateRequest(request); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		result.State = ScanCancelled
		return result, err
	}

	captureMode := CaptureModePathString
	fileSystem := scanner.fileSystem
	boundaryChecker := scanner.boundaryChecker
	if scanner.rootBinding != nil {
		// Rooted capture fails closed before any sink event: a replaced,
		// closed, or otherwise unverifiable binding must never produce even a
		// failed scan record against the wrong basis.
		if err := scanner.rootBinding.VerifyRoot(); err != nil {
			if errors.Is(err, ErrCaptureRootReplaced) {
				return result, err
			}
			return result, fmt.Errorf("%w: capture root unavailable: %v", ErrInvalidRequest, err)
		}
		captureMode = CaptureModeRootedFD
		fileSystem = NewRootedFileSystem(scanner.rootBinding)
		request.Root = scanner.rootBinding.Path()
		result.Root = request.Root
		if boundaryChecker == nil {
			boundaryChecker = DeviceBoundaryChecker{}
		}
	}

	root, err := filepath.Abs(request.Root)
	if err != nil {
		return result, fmt.Errorf("%w: resolve root: %v", ErrInvalidRequest, err)
	}
	root = filepath.Clean(root)
	request.Root = root
	result.Root = root
	result.CaptureMode = captureMode
	result.StartedAt = scanner.now()

	start := ScanStart{
		GenerationID:     request.GenerationID,
		SourceID:         request.SourceID,
		Root:             root,
		StartedAt:        result.StartedAt,
		TraversalVersion: TraversalVersion,
		PathIDVersion:    PathIDVersion,
		CaptureMode:      captureMode,
	}
	if err := scanner.sink.BeginScan(ctx, start); err != nil {
		result.FinishedAt = scanner.now()
		if ctxErr := ctx.Err(); ctxErr != nil {
			result.State = ScanCancelled
			return result, ctxErr
		}
		return result, fmt.Errorf("%w: begin scan: %w", ErrSink, err)
	}

	attempt := scanAttempt{
		scanner:         scanner,
		request:         request,
		result:          &result,
		fileSystem:      fileSystem,
		boundaryChecker: boundaryChecker,
	}
	rootName := filepath.Base(root)
	walkErr := attempt.walk(ctx, pathContext{
		absolutePath: root,
		relativePath: ".",
		rawName:      []byte(rootName),
		pathID:       rootPathID(request.SourceID),
		isRoot:       true,
	})

	switch {
	case walkErr != nil && (errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded)):
		result.State = ScanCancelled
	case walkErr != nil:
		result.State = ScanFailed
	case result.FailedEntries != 0 || result.UnstableEntries != 0:
		result.State = ScanIncomplete
		walkErr = ErrIncomplete
	default:
		result.State = ScanComplete
	}
	result.FinishedAt = scanner.now()

	// Recording a terminal cancellation/incomplete state is itself useful. The
	// caller's cancellation still stops all filesystem and detector work; only
	// this bounded logical finalization is detached from it.
	finishErr := scanner.sink.FinishScan(context.WithoutCancel(ctx), result)
	if finishErr != nil {
		sinkErr := fmt.Errorf("%w: finish scan: %w", ErrSink, finishErr)
		if walkErr != nil {
			return result, errors.Join(walkErr, sinkErr)
		}
		result.State = ScanFailed
		return result, sinkErr
	}
	return result, walkErr
}

func validateRequest(request ScanRequest) error {
	if strings.TrimSpace(request.GenerationID) == "" {
		return fmt.Errorf("%w: generation ID is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(request.SourceID) == "" {
		return fmt.Errorf("%w: source ID is required", ErrInvalidRequest)
	}
	if request.Root == "" {
		return fmt.Errorf("%w: root is required", ErrInvalidRequest)
	}
	if strings.IndexByte(request.Root, 0) >= 0 {
		return fmt.Errorf("%w: root contains NUL", ErrInvalidRequest)
	}
	return nil
}

func (scanner *Scanner) now() time.Time {
	return scanner.clock().UTC()
}

type scanAttempt struct {
	scanner         *Scanner
	request         ScanRequest
	result          *ScanResult
	fileSystem      FileSystem
	boundaryChecker BoundaryChecker
	rootMetadata    MetadataSnapshot
}

type pathContext struct {
	absolutePath    string
	relativePath    string
	rawRelativePath []byte
	rawName         []byte
	pathID          string
	parentPathID    string
	isRoot          bool
}

func (attempt *scanAttempt) walk(ctx context.Context, path pathContext) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	info, err := attempt.fileSystem.Lstat(path.absolutePath)
	if err != nil {
		record := attempt.baseRecord(path)
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageLstat, "LSTAT_FAILED", err))
		return attempt.emit(ctx, record)
	}

	before := metadataSnapshot(info)
	if path.isRoot {
		attempt.rootMetadata = before
	}
	record := attempt.baseRecord(path)
	record.Kind = entryKind(info.Mode())
	record.Before = &before
	record.HardLink = hardLinkFacts(
		attempt.request.GenerationID,
		attempt.request.SourceID,
		record.Kind,
		before,
	)
	record.Sparse = sparseFacts(record.Kind, before)

	decision, boundary, err := attempt.checkBoundary(ctx, path, before)
	record.Boundary = boundary
	if err != nil {
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageBoundary, "BOUNDARY_CHECK_FAILED", err))
		return attempt.emit(ctx, record)
	}
	if decision.Action == BoundarySkip {
		record.State = EntryBoundarySkipped
		return attempt.emit(ctx, record)
	}

	switch record.Kind {
	case KindRegularFile:
		return attempt.scanRegular(ctx, info, record)
	case KindDirectory:
		return attempt.scanDirectory(ctx, info, path, record)
	case KindSymlink:
		return attempt.scanSymlink(ctx, info, record)
	default:
		return attempt.scanSpecial(ctx, info, record)
	}
}

func (attempt *scanAttempt) baseRecord(path pathContext) EntryRecord {
	name := string(path.rawName)
	return EntryRecord{
		GenerationID:    attempt.request.GenerationID,
		SourceID:        attempt.request.SourceID,
		PathID:          path.pathID,
		ParentPathID:    path.parentPathID,
		AbsolutePath:    path.absolutePath,
		RelativePath:    path.relativePath,
		Name:            name,
		RawName:         append([]byte(nil), path.rawName...),
		RawRelativePath: append([]byte(nil), path.rawRelativePath...),
		Kind:            KindUnknown,
		State:           EntryComplete,
		HardLink:        HardLinkFacts{State: HardLinkNotApplicable},
		Sparse:          SparseFacts{State: SparseNotApplicable},
		Boundary:        BoundaryObservation{Action: BoundaryInclude},
		Detection:       DetectionObservation{State: DetectionNotRequested},
	}
}

func (attempt *scanAttempt) checkBoundary(
	ctx context.Context,
	path pathContext,
	metadata MetadataSnapshot,
) (BoundaryDecision, BoundaryObservation, error) {
	if attempt.boundaryChecker == nil {
		decision := BoundaryDecision{Action: BoundaryInclude, Reason: "no_boundary_checker_configured"}
		return decision, BoundaryObservation{
			Checked: false,
			Action:  decision.Action,
			Reason:  decision.Reason,
		}, nil
	}

	decision, err := attempt.boundaryChecker.CheckBoundary(ctx, BoundaryCandidate{
		SourceID:      attempt.request.SourceID,
		Root:          attempt.request.Root,
		AbsolutePath:  path.absolutePath,
		RelativePath:  path.relativePath,
		RootMetadata:  attempt.rootMetadata,
		EntryMetadata: metadata,
	})
	observation := BoundaryObservation{
		Checked: true,
		Action:  decision.Action,
		Reason:  decision.Reason,
	}
	if err != nil {
		return decision, observation, err
	}
	if decision.Action != BoundaryInclude && decision.Action != BoundarySkip {
		return decision, observation, fmt.Errorf("invalid boundary action %q", decision.Action)
	}
	return decision, observation, nil
}

func (attempt *scanAttempt) scanRegular(
	ctx context.Context,
	initial fs.FileInfo,
	record EntryRecord,
) error {
	file, err := attempt.fileSystem.OpenRegularNoFollow(record.AbsolutePath)
	if err != nil {
		return attempt.emitOpenFailure(ctx, initial, record, err)
	}

	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageOpen, "OPEN_FSTAT_FAILED", err))
		return attempt.emit(ctx, record)
	}
	if !sameSnapshot(initial, opened) || !opened.Mode().IsRegular() {
		_ = file.Close()
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, Issue{
			Stage:   StageStability,
			Code:    "OPENED_OBJECT_CHANGED",
			Message: "opened handle does not match the initial lstat",
		})
		return attempt.emit(ctx, record)
	}

	digest, probe, bytesRead, readErr := hashStream(
		ctx,
		file,
		attempt.scanner.hashBufferBytes,
		attempt.scanner.probeBytes,
	)
	afterHandle, statErr := file.Stat()
	closeErr := file.Close()
	afterPath, pathErr := attempt.fileSystem.Lstat(record.AbsolutePath)
	if afterPath != nil {
		after := metadataSnapshot(afterPath)
		record.After = &after
	}

	if readErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageRead, "CONTENT_READ_FAILED", readErr))
	}
	if statErr != nil {
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StagePostStat, "HANDLE_POST_STAT_FAILED", statErr))
	}
	if closeErr != nil {
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageRead, "CONTENT_CLOSE_FAILED", closeErr))
	}
	if pathErr != nil {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, issue(StagePostStat, "PATH_POST_LSTAT_FAILED", pathErr))
	}

	if statErr == nil && (!sameSnapshot(opened, afterHandle) || !sameSnapshot(initial, afterHandle)) {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, Issue{
			Stage:   StageStability,
			Code:    "HANDLE_CHANGED_DURING_READ",
			Message: "file metadata changed while content was read",
		})
	}
	if pathErr == nil && (!sameSnapshot(initial, afterPath) || !sameSnapshot(opened, afterPath)) {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, Issue{
			Stage:   StageStability,
			Code:    "PATH_CHANGED_DURING_READ",
			Message: "path no longer names the opened file version",
		})
	}
	if bytesRead != initial.Size() {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, Issue{
			Stage:   StageStability,
			Code:    "SIZE_READ_MISMATCH",
			Message: fmt.Sprintf("lstat size %d differs from bytes read %d", initial.Size(), bytesRead),
		})
	}

	if record.State != EntryComplete {
		return attempt.emit(ctx, record)
	}
	record.Content = &ContentDigest{
		Algorithm: "sha256",
		Version:   HashVersion,
		Hex:       digest,
		BytesRead: bytesRead,
		ContentID: "sha256:" + digest,
	}

	if attempt.scanner.detector != nil {
		detection, err := attempt.scanner.detector.Detect(ctx, DetectionInput{
			GenerationID:    record.GenerationID,
			SourceID:        record.SourceID,
			PathID:          record.PathID,
			ParentPathID:    record.ParentPathID,
			RelativePath:    record.RelativePath,
			RawName:         append([]byte(nil), record.RawName...),
			RawRelativePath: append([]byte(nil), record.RawRelativePath...),
			Metadata:        *record.After,
			Content:         *record.Content,
			Probe:           append([]byte(nil), probe...),
		})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			record.Detection.State = DetectionFailed
			record.Issues = append(record.Issues, issue(StageDetection, "DETECTOR_FAILED", err))
		} else {
			record.Detection = DetectionObservation{
				State:  DetectionSucceeded,
				Result: cloneDetectionResult(detection),
			}
		}
	}
	return attempt.emit(ctx, record)
}

func (attempt *scanAttempt) emitOpenFailure(
	ctx context.Context,
	initial fs.FileInfo,
	record EntryRecord,
	openErr error,
) error {
	after, afterErr := attempt.fileSystem.Lstat(record.AbsolutePath)
	if afterErr == nil {
		metadata := metadataSnapshot(after)
		record.After = &metadata
	}
	if afterErr != nil || !sameSnapshot(initial, after) {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, issue(StageOpen, "OPEN_FAILED_AFTER_PATH_CHANGE", openErr))
		if afterErr != nil {
			record.Issues = append(record.Issues, issue(StagePostStat, "PATH_POST_LSTAT_FAILED", afterErr))
		}
	} else {
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageOpen, "OPEN_NOFOLLOW_FAILED", openErr))
	}
	return attempt.emit(ctx, record)
}

func (attempt *scanAttempt) scanDirectory(
	ctx context.Context,
	initial fs.FileInfo,
	path pathContext,
	record EntryRecord,
) error {
	directory, err := attempt.fileSystem.OpenDirNoFollow(record.AbsolutePath)
	if err != nil {
		return attempt.emitOpenFailure(ctx, initial, record, err)
	}
	opened, err := directory.Stat()
	if err != nil {
		_ = directory.Close()
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageOpen, "DIRECTORY_FSTAT_FAILED", err))
		return attempt.emit(ctx, record)
	}
	if !sameSnapshot(initial, opened) || !opened.IsDir() {
		_ = directory.Close()
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, Issue{
			Stage:   StageStability,
			Code:    "OPENED_DIRECTORY_CHANGED",
			Message: "opened directory does not match the initial lstat",
		})
		return attempt.emit(ctx, record)
	}

	entries, readErr := directory.ReadDir(-1)
	afterEnumeration, statErr := directory.Stat()
	closeErr := directory.Close()
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if readErr != nil {
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageReadDir, "READ_DIRECTORY_FAILED", readErr))
	}
	if statErr != nil {
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StagePostStat, "DIRECTORY_POST_STAT_FAILED", statErr))
	} else if !sameSnapshot(opened, afterEnumeration) {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, Issue{
			Stage:   StageStability,
			Code:    "DIRECTORY_CHANGED_DURING_ENUMERATION",
			Message: "directory metadata changed while entries were enumerated",
		})
	}
	if closeErr != nil {
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageReadDir, "DIRECTORY_CLOSE_FAILED", closeErr))
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return bytes.Compare([]byte(entries[i].Name()), []byte(entries[j].Name())) < 0
	})
	var previousName []byte
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		rawName := []byte(entry.Name())
		if !validChildName(entry.Name()) {
			markEntryState(&record, EntryFailed)
			record.Issues = append(record.Issues, Issue{
				Stage:   StageEnumeration,
				Code:    "INVALID_CHILD_NAME",
				Message: fmt.Sprintf("directory returned unsafe child name at sorted index %d", index),
			})
			continue
		}
		if previousName != nil && bytes.Equal(previousName, rawName) {
			markEntryState(&record, EntryFailed)
			record.Issues = append(record.Issues, Issue{
				Stage:   StageEnumeration,
				Code:    "DUPLICATE_CHILD_NAME",
				Message: fmt.Sprintf("directory returned duplicate child name at sorted index %d", index),
			})
			continue
		}
		previousName = append(previousName[:0], rawName...)

		childRawPath := joinRawPath(path.rawRelativePath, rawName)
		childRelativePath := string(childRawPath)
		if childRelativePath == "" {
			childRelativePath = entry.Name()
		}
		if err := attempt.walk(ctx, pathContext{
			absolutePath:    filepath.Join(path.absolutePath, entry.Name()),
			relativePath:    childRelativePath,
			rawRelativePath: childRawPath,
			rawName:         append([]byte(nil), rawName...),
			pathID:          childPathID(path.pathID, rawName),
			parentPathID:    path.pathID,
		}); err != nil {
			return err
		}
	}

	afterPath, pathErr := attempt.fileSystem.Lstat(record.AbsolutePath)
	if pathErr != nil {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, issue(StagePostStat, "DIRECTORY_PATH_POST_LSTAT_FAILED", pathErr))
	} else {
		after := metadataSnapshot(afterPath)
		record.After = &after
		if !sameSnapshot(initial, afterPath) {
			markEntryState(&record, EntryUnstable)
			record.Issues = append(record.Issues, Issue{
				Stage:   StageStability,
				Code:    "DIRECTORY_CHANGED_DURING_TRAVERSAL",
				Message: "directory metadata changed while descendants were scanned",
			})
		}
	}
	return attempt.emit(ctx, record)
}

func (attempt *scanAttempt) scanSymlink(
	ctx context.Context,
	initial fs.FileInfo,
	record EntryRecord,
) error {
	target, readErr := attempt.fileSystem.Readlink(record.AbsolutePath)
	after, statErr := attempt.fileSystem.Lstat(record.AbsolutePath)
	if after != nil {
		metadata := metadataSnapshot(after)
		record.After = &metadata
	}
	if readErr != nil {
		markEntryState(&record, EntryFailed)
		record.Issues = append(record.Issues, issue(StageReadlink, "READLINK_FAILED", readErr))
	}
	if statErr != nil {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, issue(StagePostStat, "SYMLINK_POST_LSTAT_FAILED", statErr))
	} else if !sameSnapshot(initial, after) || after.Mode()&fs.ModeSymlink == 0 {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, Issue{
			Stage:   StageStability,
			Code:    "SYMLINK_CHANGED_DURING_SCAN",
			Message: "symlink metadata changed while its target was read",
		})
	}
	if record.State == EntryComplete {
		digest := sha256.Sum256([]byte(target))
		record.Symlink = &SymlinkFacts{
			RawTarget:    []byte(target),
			TargetSHA256: hex.EncodeToString(digest[:]),
		}
	}
	return attempt.emit(ctx, record)
}

func (attempt *scanAttempt) scanSpecial(
	ctx context.Context,
	initial fs.FileInfo,
	record EntryRecord,
) error {
	after, err := attempt.fileSystem.Lstat(record.AbsolutePath)
	if err != nil {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, issue(StagePostStat, "SPECIAL_FILE_POST_LSTAT_FAILED", err))
		return attempt.emit(ctx, record)
	}
	metadata := metadataSnapshot(after)
	record.After = &metadata
	if !sameSnapshot(initial, after) || entryKind(after.Mode()) != record.Kind {
		markEntryState(&record, EntryUnstable)
		record.Issues = append(record.Issues, Issue{
			Stage:   StageStability,
			Code:    "SPECIAL_FILE_CHANGED_DURING_SCAN",
			Message: "special-file metadata changed during observation",
		})
	}
	return attempt.emit(ctx, record)
}

func (attempt *scanAttempt) emit(ctx context.Context, record EntryRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := attempt.scanner.sink.PutEntry(ctx, record); err != nil {
		return fmt.Errorf("%w: put entry %q: %w", ErrSink, record.RelativePath, err)
	}

	attempt.result.Entries++
	switch record.Kind {
	case KindRegularFile:
		attempt.result.RegularFiles++
	case KindDirectory:
		attempt.result.Directories++
	case KindSymlink:
		attempt.result.Symlinks++
	case KindNamedPipe, KindSocket, KindBlockDevice, KindCharDevice, KindIrregular:
		attempt.result.SpecialFiles++
	}
	if record.State == EntryBoundarySkipped {
		attempt.result.BoundarySkipped++
	}
	if !record.Boundary.Checked {
		attempt.result.BoundaryUnchecked++
	}
	if record.State == EntryFailed {
		attempt.result.FailedEntries++
	}
	if record.State == EntryUnstable {
		attempt.result.UnstableEntries++
	}
	if record.Detection.State == DetectionFailed {
		attempt.result.DetectionFailures++
	}
	if record.Content != nil {
		if record.Content.BytesRead > math.MaxInt64-attempt.result.BytesHashed {
			attempt.result.BytesHashed = math.MaxInt64
		} else {
			attempt.result.BytesHashed += record.Content.BytesRead
		}
	}
	return nil
}

func hashStream(
	ctx context.Context,
	reader io.Reader,
	bufferBytes int,
	probeBytes int,
) (digest string, probe []byte, bytesRead int64, err error) {
	hash := sha256.New()
	buffer := make([]byte, bufferBytes)
	probeCapacity := probeBytes
	if probeCapacity > bufferBytes {
		probeCapacity = bufferBytes
	}
	probe = make([]byte, 0, probeCapacity)
	emptyReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, bytesRead, err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			emptyReads = 0
			if bytesRead > math.MaxInt64-int64(count) {
				return "", nil, bytesRead, fmt.Errorf("byte count overflow")
			}
			bytesRead += int64(count)
			_, _ = hash.Write(buffer[:count])
			if len(probe) < probeBytes {
				remaining := probeBytes - len(probe)
				if remaining > count {
					remaining = count
				}
				probe = append(probe, buffer[:remaining]...)
			}
		} else if readErr == nil {
			emptyReads++
			if emptyReads >= maxEmptyReads {
				return "", nil, bytesRead, io.ErrNoProgress
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return hex.EncodeToString(hash.Sum(nil)), probe, bytesRead, nil
			}
			return "", nil, bytesRead, readErr
		}
	}
}

func validChildName(name string) bool {
	return name != "" &&
		name != "." &&
		name != ".." &&
		!filepath.IsAbs(name) &&
		!strings.ContainsRune(name, filepath.Separator) &&
		!strings.ContainsRune(name, 0)
}

func joinRawPath(parent, name []byte) []byte {
	if len(parent) == 0 {
		return append([]byte(nil), name...)
	}
	joined := make([]byte, 0, len(parent)+1+len(name))
	joined = append(joined, parent...)
	joined = append(joined, '/')
	joined = append(joined, name...)
	return joined
}

func cloneDetectionResult(result DetectionResult) DetectionResult {
	result.Evidence = append([]DetectionEvidence(nil), result.Evidence...)
	return result
}

func issue(stage IssueStage, code string, err error) Issue {
	return Issue{Stage: stage, Code: code, Message: err.Error()}
}

func markEntryState(record *EntryRecord, state EntryState) {
	if entryStatePriority(state) > entryStatePriority(record.State) {
		record.State = state
	}
}

func entryStatePriority(state EntryState) int {
	switch state {
	case EntryUnstable:
		return 1
	case EntryFailed:
		return 2
	case EntryCancelled:
		return 3
	default:
		return 0
	}
}
