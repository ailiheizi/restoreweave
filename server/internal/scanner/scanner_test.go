package scanner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestScanDeterministicNamespaceAndHashes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	insideContent := []byte("inside-data")
	sharedContent := []byte("shared-content")
	mustMkdir(t, filepath.Join(root, "alpha"))
	mustWriteFile(t, filepath.Join(root, "alpha", "inside.bin"), insideContent)
	mustWriteFile(t, filepath.Join(root, "beta.txt"), sharedContent)
	if err := os.Link(
		filepath.Join(root, "beta.txt"),
		filepath.Join(root, "beta-link"),
	); err != nil {
		t.Skipf("hard links are unavailable: %v", err)
	}
	if err := os.Symlink("alpha", filepath.Join(root, "alpha-link")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	detector := detectorFunc(func(_ context.Context, input DetectionInput) (DetectionResult, error) {
		return DetectionResult{
			DetectorID:      "test.magic",
			DetectorVersion: "1",
			FormatID:        "test/binary",
			MediaType:       "application/octet-stream",
			Confidence:      1,
			Evidence: []DetectionEvidence{
				{Method: "prefix", Value: hex.EncodeToString(input.Probe)},
			},
		}, nil
	})

	firstSink := &recordingSink{}
	firstScanner := mustNewScanner(t, Config{
		Sink:       firstSink,
		Detector:   detector,
		ProbeBytes: 4,
		Clock:      fixedClock,
	})
	firstResult, err := firstScanner.Scan(context.Background(), ScanRequest{
		GenerationID: "generation-1",
		SourceID:     "source-1",
		Root:         root,
	})
	if err != nil {
		t.Fatalf("first Scan() error = %v", err)
	}

	expectedOrder := []string{
		"alpha/inside.bin",
		"alpha",
		"alpha-link",
		"beta-link",
		"beta.txt",
		".",
	}
	if got := relativePaths(firstSink.entries); !reflect.DeepEqual(got, expectedOrder) {
		t.Fatalf("first traversal order = %#v, want %#v", got, expectedOrder)
	}
	if firstResult.State != ScanComplete {
		t.Fatalf("first result state = %q, want %q", firstResult.State, ScanComplete)
	}
	if firstResult.Entries != 6 || firstResult.RegularFiles != 3 ||
		firstResult.Directories != 2 || firstResult.Symlinks != 1 ||
		firstResult.SpecialFiles != 0 {
		t.Fatalf("unexpected first result counts: %+v", firstResult)
	}
	wantBytesHashed := int64(len(insideContent) + 2*len(sharedContent))
	if firstResult.BytesHashed != wantBytesHashed {
		t.Fatalf("bytes hashed = %d, want %d", firstResult.BytesHashed, wantBytesHashed)
	}
	if firstResult.BoundaryUnchecked != firstResult.Entries {
		t.Fatalf(
			"unchecked boundaries = %d, want %d",
			firstResult.BoundaryUnchecked,
			firstResult.Entries,
		)
	}

	rootRecord := requireRecord(t, firstSink.entries, ".")
	alphaRecord := requireRecord(t, firstSink.entries, "alpha")
	insideRecord := requireRecord(t, firstSink.entries, "alpha/inside.bin")
	symlinkRecord := requireRecord(t, firstSink.entries, "alpha-link")
	linkRecord := requireRecord(t, firstSink.entries, "beta-link")
	sharedRecord := requireRecord(t, firstSink.entries, "beta.txt")

	if rootRecord.ParentPathID != "" || len(rootRecord.RawRelativePath) != 0 {
		t.Fatalf("root namespace fields are not canonical: %+v", rootRecord)
	}
	if alphaRecord.ParentPathID != rootRecord.PathID {
		t.Fatalf("alpha parent = %q, want %q", alphaRecord.ParentPathID, rootRecord.PathID)
	}
	if insideRecord.ParentPathID != alphaRecord.PathID {
		t.Fatalf("inside parent = %q, want %q", insideRecord.ParentPathID, alphaRecord.PathID)
	}
	if insideRecord.PathID != childPathID(alphaRecord.PathID, []byte("inside.bin")) {
		t.Fatalf("inside path ID is not derived from its parent and raw name")
	}
	if !bytes.Equal(insideRecord.RawRelativePath, []byte("alpha/inside.bin")) {
		t.Fatalf("inside raw path = %q", insideRecord.RawRelativePath)
	}

	assertContentDigest(t, insideRecord, insideContent)
	assertContentDigest(t, linkRecord, sharedContent)
	assertContentDigest(t, sharedRecord, sharedContent)
	if linkRecord.Content.ContentID != sharedRecord.Content.ContentID {
		t.Fatalf("hard-linked names have different content IDs")
	}
	if linkRecord.HardLink.State != HardLinkMultiple ||
		sharedRecord.HardLink.State != HardLinkMultiple {
		t.Fatalf(
			"hard-link states = %q and %q, want %q",
			linkRecord.HardLink.State,
			sharedRecord.HardLink.State,
			HardLinkMultiple,
		)
	}
	if linkRecord.HardLink.GroupID == "" ||
		linkRecord.HardLink.GroupID != sharedRecord.HardLink.GroupID {
		t.Fatalf("hard-link group IDs do not match: %+v %+v", linkRecord.HardLink, sharedRecord.HardLink)
	}
	if linkRecord.HardLink.GroupIDVersion != HardLinkIDVersion {
		t.Fatalf(
			"hard-link group version = %q, want %q",
			linkRecord.HardLink.GroupIDVersion,
			HardLinkIDVersion,
		)
	}

	if symlinkRecord.Kind != KindSymlink || symlinkRecord.Content != nil ||
		symlinkRecord.Symlink == nil {
		t.Fatalf("symlink was not represented independently: %+v", symlinkRecord)
	}
	if !bytes.Equal(symlinkRecord.Symlink.RawTarget, []byte("alpha")) {
		t.Fatalf("symlink raw target = %q, want %q", symlinkRecord.Symlink.RawTarget, "alpha")
	}
	targetDigest := sha256.Sum256([]byte("alpha"))
	if symlinkRecord.Symlink.TargetSHA256 != hex.EncodeToString(targetDigest[:]) {
		t.Fatalf("unexpected symlink target digest %q", symlinkRecord.Symlink.TargetSHA256)
	}
	if recordExists(firstSink.entries, "alpha-link/inside.bin") {
		t.Fatalf("scanner followed a directory symlink")
	}
	if insideRecord.Before == nil || insideRecord.After == nil ||
		*insideRecord.Before != *insideRecord.After {
		t.Fatalf("stable file did not retain matching before/after metadata")
	}
	if insideRecord.Detection.State != DetectionSucceeded ||
		insideRecord.Detection.Result.DetectorID != "test.magic" {
		t.Fatalf("unexpected detector observation: %+v", insideRecord.Detection)
	}
	if got := insideRecord.Detection.Result.Evidence[0].Value; got != hex.EncodeToString(insideContent[:4]) {
		t.Fatalf("detector probe = %q, want first four bytes", got)
	}

	secondSink := &recordingSink{}
	secondScanner := mustNewScanner(t, Config{
		Sink:       secondSink,
		Detector:   detector,
		ProbeBytes: 4,
		Clock:      fixedClock,
	})
	secondResult, err := secondScanner.Scan(context.Background(), ScanRequest{
		GenerationID: "generation-2",
		SourceID:     "source-1",
		Root:         root,
	})
	if err != nil || secondResult.State != ScanComplete {
		t.Fatalf("second Scan() = (%+v, %v), want complete", secondResult, err)
	}
	if got := relativePaths(secondSink.entries); !reflect.DeepEqual(got, expectedOrder) {
		t.Fatalf("second traversal order = %#v, want %#v", got, expectedOrder)
	}
	for _, path := range expectedOrder {
		first := requireRecord(t, firstSink.entries, path)
		second := requireRecord(t, secondSink.entries, path)
		if first.PathID != second.PathID {
			t.Fatalf("path ID for %q changed across generations", path)
		}
	}
	secondShared := requireRecord(t, secondSink.entries, "beta.txt")
	if secondShared.HardLink.GroupID == sharedRecord.HardLink.GroupID {
		t.Fatalf("hard-link group ID was not scoped to its scan generation")
	}
}

func TestScanPreservesRawNameBytes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	rawName := []byte{'r', 'a', 'w', '-', 0xff, '.', 'b', 'i', 'n'}
	path := filepath.Join(root, string(rawName))
	if err := os.WriteFile(path, []byte("raw"), 0o600); err != nil {
		t.Skipf("filesystem does not accept the raw filename fixture: %v", err)
	}

	sink := &recordingSink{}
	scanner := mustNewScanner(t, Config{Sink: sink, Clock: fixedClock})
	result, err := scanner.Scan(context.Background(), ScanRequest{
		GenerationID: "raw-generation",
		SourceID:     "raw-source",
		Root:         root,
	})
	if err != nil || result.State != ScanComplete {
		t.Fatalf("Scan() = (%+v, %v), want complete", result, err)
	}

	var rawRecord *EntryRecord
	for index := range sink.entries {
		if bytes.Equal(sink.entries[index].RawName, rawName) {
			rawRecord = &sink.entries[index]
			break
		}
	}
	if rawRecord == nil {
		t.Fatalf("raw filename was not preserved in entries: %+v", sink.entries)
	}
	if !bytes.Equal(rawRecord.RawRelativePath, rawName) {
		t.Fatalf("raw relative path = %v, want %v", rawRecord.RawRelativePath, rawName)
	}
	if []byte(rawRecord.Name) == nil || !bytes.Equal([]byte(rawRecord.Name), rawName) {
		t.Fatalf("display name did not retain the underlying Go path bytes")
	}
	rootRecord := requireRecord(t, sink.entries, ".")
	if rawRecord.PathID != childPathID(rootRecord.PathID, rawName) {
		t.Fatalf("raw filename was not used for path identity")
	}
}

func TestScanMarksFileChangedDuringReadUnstable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "changing.bin")
	mustWriteFile(t, path, []byte("abcdefgh"))

	var mutationErr error
	fileSystem := &mutateOnReadFileSystem{
		target: path,
		mutate: func() {
			mutationErr = os.WriteFile(path, bytes.Repeat([]byte("z"), 64), 0o600)
		},
	}
	sink := &recordingSink{}
	scanner := mustNewScanner(t, Config{
		FileSystem:      fileSystem,
		Sink:            sink,
		HashBufferBytes: 4,
		Clock:           fixedClock,
	})
	result, err := scanner.Scan(context.Background(), ScanRequest{
		GenerationID: "unstable-generation",
		SourceID:     "unstable-source",
		Root:         root,
	})
	if mutationErr != nil {
		t.Fatalf("test mutation failed: %v", mutationErr)
	}
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Scan() error = %v, want ErrIncomplete", err)
	}
	if result.State != ScanIncomplete || result.UnstableEntries != 1 {
		t.Fatalf("unexpected unstable result: %+v", result)
	}

	record := requireRecord(t, sink.entries, "changing.bin")
	if record.State != EntryUnstable {
		t.Fatalf("changing file state = %q, want %q", record.State, EntryUnstable)
	}
	if record.Content != nil {
		t.Fatalf("unstable content was accepted: %+v", record.Content)
	}
	if record.Before == nil || record.After == nil || record.Before.Size == record.After.Size {
		t.Fatalf("before/after mutation evidence is missing: %+v", record)
	}
	if !hasIssueCode(record, "HANDLE_CHANGED_DURING_READ") &&
		!hasIssueCode(record, "PATH_CHANGED_DURING_READ") &&
		!hasIssueCode(record, "SIZE_READ_MISMATCH") {
		t.Fatalf("mutation did not produce stability evidence: %+v", record.Issues)
	}
}

func TestScanRejectsSymlinkSwapBeforeOpen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "candidate.bin")
	mustWriteFile(t, target, []byte("initial"))
	externalRoot := t.TempDir()
	external := filepath.Join(externalRoot, "external.bin")
	mustWriteFile(t, external, []byte("must-not-be-read"))

	fileSystem := &swapBeforeOpenFileSystem{
		target:     target,
		linkTarget: external,
	}
	sink := &recordingSink{}
	scanner := mustNewScanner(t, Config{FileSystem: fileSystem, Sink: sink, Clock: fixedClock})
	result, err := scanner.Scan(context.Background(), ScanRequest{
		GenerationID: "swap-generation",
		SourceID:     "swap-source",
		Root:         root,
	})
	if fileSystem.swapErr != nil {
		if errors.Is(fileSystem.swapErr, os.ErrPermission) {
			t.Skipf("symlink fixture is unavailable: %v", fileSystem.swapErr)
		}
		t.Fatalf("test path swap failed: %v", fileSystem.swapErr)
	}
	if !errors.Is(err, ErrIncomplete) || result.State != ScanIncomplete {
		t.Fatalf("Scan() = (%+v, %v), want incomplete", result, err)
	}
	record := requireRecord(t, sink.entries, "candidate.bin")
	if record.State != EntryUnstable || record.Content != nil {
		t.Fatalf("swapped path was accepted as content: %+v", record)
	}
	if !hasIssueCode(record, "OPEN_FAILED_AFTER_PATH_CHANGE") &&
		!hasIssueCode(record, "OPENED_OBJECT_CHANGED") {
		t.Fatalf("path swap evidence is missing: %+v", record.Issues)
	}
}

func TestScanRecordsLstatFailureWithoutInferringAbsence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "unreadable.bin")
	mustWriteFile(t, target, []byte("present"))
	fileSystem := &lstatFailureFileSystem{
		target: target,
		err:    os.ErrPermission,
	}
	sink := &recordingSink{}
	scanner := mustNewScanner(t, Config{FileSystem: fileSystem, Sink: sink, Clock: fixedClock})
	result, err := scanner.Scan(context.Background(), ScanRequest{
		GenerationID: "lstat-generation",
		SourceID:     "lstat-source",
		Root:         root,
	})
	if !errors.Is(err, ErrIncomplete) || result.State != ScanIncomplete {
		t.Fatalf("Scan() = (%+v, %v), want incomplete", result, err)
	}
	if result.FailedEntries != 1 {
		t.Fatalf("failed entries = %d, want 1", result.FailedEntries)
	}
	record := requireRecord(t, sink.entries, "unreadable.bin")
	if record.Kind != KindUnknown || record.State != EntryFailed || record.Content != nil ||
		!hasIssueCode(record, "LSTAT_FAILED") {
		t.Fatalf("unexpected lstat failure record: %+v", record)
	}
}

func TestScanCancellationFinalizesAttempt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "cancel.bin"), []byte("cancel me"))
	ctx, cancel := context.WithCancel(context.Background())
	detector := detectorFunc(func(_ context.Context, _ DetectionInput) (DetectionResult, error) {
		cancel()
		return DetectionResult{}, context.Canceled
	})

	sink := &recordingSink{}
	scanner := mustNewScanner(t, Config{
		Sink:     sink,
		Detector: detector,
		Clock:    fixedClock,
	})
	result, err := scanner.Scan(ctx, ScanRequest{
		GenerationID: "cancel-generation",
		SourceID:     "cancel-source",
		Root:         root,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() error = %v, want context cancellation", err)
	}
	if result.State != ScanCancelled {
		t.Fatalf("result state = %q, want %q", result.State, ScanCancelled)
	}
	if len(sink.finishes) != 1 || sink.finishes[0].State != ScanCancelled {
		t.Fatalf("cancelled scan was not finalized: %+v", sink.finishes)
	}
	if sink.finishContextErr != nil {
		t.Fatalf("FinishScan received a cancelled context: %v", sink.finishContextErr)
	}
	if len(sink.entries) != 0 {
		t.Fatalf("partially processed entry was emitted after cancellation: %+v", sink.entries)
	}
}

func TestScanSinkFailurePreservesCauseAndFinalizesFailed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "entry.bin"), []byte("entry"))
	putErr := errors.New("sink unavailable")
	sink := &recordingSink{putErr: putErr}
	scanner := mustNewScanner(t, Config{Sink: sink, Clock: fixedClock})

	result, err := scanner.Scan(context.Background(), ScanRequest{
		GenerationID: "sink-generation",
		SourceID:     "sink-source",
		Root:         root,
	})
	if !errors.Is(err, ErrSink) || !errors.Is(err, putErr) {
		t.Fatalf("Scan() error = %v, want ErrSink and original cause", err)
	}
	if result.State != ScanFailed {
		t.Fatalf("result state = %q, want %q", result.State, ScanFailed)
	}
	if len(sink.finishes) != 1 || sink.finishes[0].State != ScanFailed {
		t.Fatalf("failed scan was not finalized: %+v", sink.finishes)
	}
	if len(sink.entries) != 0 {
		t.Fatalf("sink retained an entry despite returning failure")
	}
}

func TestBoundarySkipDoesNotTraverseSubtree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "skip"))
	mustWriteFile(t, filepath.Join(root, "skip", "secret.bin"), []byte("secret"))
	mustWriteFile(t, filepath.Join(root, "keep.bin"), []byte("keep"))

	checker := boundaryCheckerFunc(func(
		_ context.Context,
		candidate BoundaryCandidate,
	) (BoundaryDecision, error) {
		if candidate.RelativePath == "skip" {
			return BoundaryDecision{Action: BoundarySkip, Reason: "different-volume"}, nil
		}
		return BoundaryDecision{Action: BoundaryInclude, Reason: "same-volume"}, nil
	})
	sink := &recordingSink{}
	scanner := mustNewScanner(t, Config{
		Sink:            sink,
		BoundaryChecker: checker,
		Clock:           fixedClock,
	})
	result, err := scanner.Scan(context.Background(), ScanRequest{
		GenerationID: "boundary-generation",
		SourceID:     "boundary-source",
		Root:         root,
	})
	if err != nil || result.State != ScanComplete {
		t.Fatalf("Scan() = (%+v, %v), want complete", result, err)
	}
	if result.BoundarySkipped != 1 || result.BoundaryUnchecked != 0 {
		t.Fatalf("unexpected boundary counts: %+v", result)
	}
	if recordExists(sink.entries, "skip/secret.bin") {
		t.Fatalf("scanner traversed a boundary-skipped subtree")
	}
	skipped := requireRecord(t, sink.entries, "skip")
	if skipped.State != EntryBoundarySkipped || skipped.Boundary.Action != BoundarySkip ||
		skipped.Boundary.Reason != "different-volume" {
		t.Fatalf("unexpected boundary-skipped record: %+v", skipped)
	}
}

func TestSparseFactsAreConservativeEvidence(t *testing.T) {
	t.Parallel()

	indicated := sparseFacts(KindRegularFile, MetadataSnapshot{
		Size:        4096,
		BlocksKnown: true,
		Blocks:      1,
	})
	if indicated.State != SparseAllocationBelowSize || indicated.AllocatedBytes != 512 ||
		indicated.Evidence != "stat_blocks_512" || indicated.ExtentMapCaptured {
		t.Fatalf("unexpected sparse evidence: %+v", indicated)
	}

	unknown := sparseFacts(KindRegularFile, MetadataSnapshot{Size: 4096})
	if unknown.State != SparseUnknown || unknown.ExtentMapCaptured {
		t.Fatalf("unknown sparse facts were overstated: %+v", unknown)
	}

	notApplicable := sparseFacts(KindDirectory, MetadataSnapshot{Size: 4096})
	if notApplicable.State != SparseNotApplicable {
		t.Fatalf("directory sparse state = %q, want %q", notApplicable.State, SparseNotApplicable)
	}
}

func TestHashStreamProducesDigestAndBoundedProbe(t *testing.T) {
	t.Parallel()

	content := "streamed-content"
	digest, probe, bytesRead, err := hashStream(
		context.Background(),
		strings.NewReader(content),
		3,
		5,
	)
	if err != nil {
		t.Fatalf("hashStream() error = %v", err)
	}
	wantDigest := sha256.Sum256([]byte(content))
	if digest != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("digest = %q, want %x", digest, wantDigest)
	}
	if !bytes.Equal(probe, []byte(content[:5])) {
		t.Fatalf("probe = %q, want %q", probe, content[:5])
	}
	if bytesRead != int64(len(content)) {
		t.Fatalf("bytes read = %d, want %d", bytesRead, len(content))
	}
}

type recordingSink struct {
	mu               sync.Mutex
	starts           []ScanStart
	entries          []EntryRecord
	finishes         []ScanResult
	beginErr         error
	putErr           error
	finishErr        error
	finishContextErr error
}

func (sink *recordingSink) BeginScan(_ context.Context, start ScanStart) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.beginErr != nil {
		return sink.beginErr
	}
	sink.starts = append(sink.starts, start)
	return nil
}

func (sink *recordingSink) PutEntry(_ context.Context, entry EntryRecord) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.putErr != nil {
		return sink.putErr
	}
	sink.entries = append(sink.entries, cloneEntryRecord(entry))
	return nil
}

func (sink *recordingSink) FinishScan(ctx context.Context, result ScanResult) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.finishContextErr = ctx.Err()
	if sink.finishErr != nil {
		return sink.finishErr
	}
	sink.finishes = append(sink.finishes, result)
	return nil
}

type detectorFunc func(context.Context, DetectionInput) (DetectionResult, error)

func (function detectorFunc) Detect(
	ctx context.Context,
	input DetectionInput,
) (DetectionResult, error) {
	return function(ctx, input)
}

type boundaryCheckerFunc func(context.Context, BoundaryCandidate) (BoundaryDecision, error)

func (function boundaryCheckerFunc) CheckBoundary(
	ctx context.Context,
	candidate BoundaryCandidate,
) (BoundaryDecision, error) {
	return function(ctx, candidate)
}

type mutateOnReadFileSystem struct {
	OSFileSystem
	target string
	mutate func()
}

func (fileSystem *mutateOnReadFileSystem) OpenRegularNoFollow(path string) (ReadStatCloser, error) {
	file, err := fileSystem.OSFileSystem.OpenRegularNoFollow(path)
	if err != nil || path != fileSystem.target {
		return file, err
	}
	return &mutateAfterFirstRead{
		ReadStatCloser: file,
		mutate:         fileSystem.mutate,
	}, nil
}

type mutateAfterFirstRead struct {
	ReadStatCloser
	once   sync.Once
	mutate func()
}

func (file *mutateAfterFirstRead) Read(buffer []byte) (int, error) {
	count, err := file.ReadStatCloser.Read(buffer)
	if count > 0 {
		file.once.Do(file.mutate)
	}
	return count, err
}

type swapBeforeOpenFileSystem struct {
	OSFileSystem
	target     string
	linkTarget string
	once       sync.Once
	swapErr    error
}

func (fileSystem *swapBeforeOpenFileSystem) OpenRegularNoFollow(
	path string,
) (ReadStatCloser, error) {
	if path == fileSystem.target {
		fileSystem.once.Do(func() {
			if err := os.Remove(path); err != nil {
				fileSystem.swapErr = err
				return
			}
			fileSystem.swapErr = os.Symlink(fileSystem.linkTarget, path)
		})
	}
	return fileSystem.OSFileSystem.OpenRegularNoFollow(path)
}

type lstatFailureFileSystem struct {
	OSFileSystem
	target string
	err    error
}

func (fileSystem *lstatFailureFileSystem) Lstat(path string) (os.FileInfo, error) {
	if path == fileSystem.target {
		return nil, fileSystem.err
	}
	return fileSystem.OSFileSystem.Lstat(path)
}

func mustNewScanner(t *testing.T, config Config) *Scanner {
	t.Helper()
	scanner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return scanner
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir(%q) error = %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func fixedClock() time.Time {
	return time.Date(2026, time.August, 11, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
}

func relativePaths(entries []EntryRecord) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.RelativePath)
	}
	return paths
}

func requireRecord(t *testing.T, entries []EntryRecord, path string) EntryRecord {
	t.Helper()
	for _, entry := range entries {
		if entry.RelativePath == path {
			return entry
		}
	}
	t.Fatalf("record %q not found in %v", path, relativePaths(entries))
	return EntryRecord{}
}

func recordExists(entries []EntryRecord, path string) bool {
	for _, entry := range entries {
		if entry.RelativePath == path {
			return true
		}
	}
	return false
}

func assertContentDigest(t *testing.T, record EntryRecord, content []byte) {
	t.Helper()
	if record.Content == nil {
		t.Fatalf("record %q has no content digest", record.RelativePath)
	}
	want := sha256.Sum256(content)
	wantHex := hex.EncodeToString(want[:])
	if record.Content.Algorithm != "sha256" || record.Content.Version != HashVersion ||
		record.Content.Hex != wantHex || record.Content.ContentID != "sha256:"+wantHex ||
		record.Content.BytesRead != int64(len(content)) {
		t.Fatalf("record %q content digest = %+v", record.RelativePath, record.Content)
	}
}

func hasIssueCode(record EntryRecord, code string) bool {
	for _, issue := range record.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func cloneEntryRecord(entry EntryRecord) EntryRecord {
	entry.RawName = append([]byte(nil), entry.RawName...)
	entry.RawRelativePath = append([]byte(nil), entry.RawRelativePath...)
	entry.Issues = append([]Issue(nil), entry.Issues...)
	entry.Detection.Result.Evidence = append(
		[]DetectionEvidence(nil),
		entry.Detection.Result.Evidence...,
	)
	if entry.Before != nil {
		before := *entry.Before
		entry.Before = &before
	}
	if entry.After != nil {
		after := *entry.After
		entry.After = &after
	}
	if entry.Content != nil {
		content := *entry.Content
		entry.Content = &content
	}
	if entry.Symlink != nil {
		symlink := *entry.Symlink
		symlink.RawTarget = append([]byte(nil), symlink.RawTarget...)
		entry.Symlink = &symlink
	}
	return entry
}
