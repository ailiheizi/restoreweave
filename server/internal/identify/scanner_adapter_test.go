package identify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/scanner"
)

// memorySink records the scanner output so a test can inspect the entries a
// real scan would have produced. It mirrors the recordingSink used by the
// scanner package tests without touching those files.
type memorySink struct {
	entries []scanner.EntryRecord
	result  scanner.ScanResult
	start   scanner.ScanStart
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

// testScannerDetector builds the adapter the production wiring would use.
func testScannerDetector() *ScannerDetector {
	return &ScannerDetector{
		DetectorID:      "identify.builtin",
		DetectorVersion: RulesDigest(),
		Inner:           NewDetector(0),
	}
}

// scanTree runs a full scanner pass over root with the given detector and
// returns the recorded sink plus the terminal scan result.
func scanTree(t *testing.T, root string, detector scanner.Detector) (*memorySink, scanner.ScanResult) {
	t.Helper()
	sink := &memorySink{}
	instance, err := scanner.New(scanner.Config{
		FileSystem: scanner.OSFileSystem{},
		Sink:       sink,
		Detector:   detector,
	})
	if err != nil {
		t.Fatalf("scanner.New returned error: %v", err)
	}
	result, err := instance.Scan(context.Background(), scanner.ScanRequest{
		GenerationID: "gen-e2e",
		SourceID:     "src-e2e",
		Root:         root,
	})
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	return sink, result
}

func writeTestFile(t *testing.T, root, name string, content []byte) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %q: %v", name, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
}

func findEntry(t *testing.T, sink *memorySink, name string) scanner.EntryRecord {
	t.Helper()
	for _, entry := range sink.entries {
		if entry.Name == name {
			return entry
		}
	}
	t.Fatalf("no entry named %q in sink; entries: %+v", name, sink.entries)
	return scanner.EntryRecord{}
}

func requireDetection(t *testing.T, entry scanner.EntryRecord) scanner.DetectionObservation {
	t.Helper()
	if entry.Detection.State != scanner.DetectionSucceeded {
		t.Fatalf("entry %q detection state = %s, want SUCCEEDED; issues: %+v",
			entry.Name, entry.Detection.State, entry.Issues)
	}
	return entry.Detection
}

func TestScannerDetectorKnownFormatsFlowIntoEntryDetection(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", []byte("known text content without any magic signature"))
	writeTestFile(t, root, "photo.jpg", append([]byte{0xff, 0xd8, 0xff, 0xe0}, make([]byte, 512)...))
	writeTestFile(t, root, filepath.Join("nested", "pic.png"),
		append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 256)...))

	sink, result := scanTree(t, root, testScannerDetector())
	if result.State != scanner.ScanComplete {
		t.Fatalf("scan state = %s, want COMPLETE; result: %+v", result.State, result)
	}

	// photo.jpg: suffix and magic both say jpeg, so the adapter fills the
	// format fields and records both evidence lines.
	photo := requireDetection(t, findEntry(t, sink, "photo.jpg"))
	if photo.Result.FormatID != "jpeg" || photo.Result.MediaType != "image/jpeg" {
		t.Errorf("photo.jpg result = %+v, want jpeg/image/jpeg", photo.Result)
	}
	if photo.Result.Confidence != 1 {
		t.Errorf("photo.jpg confidence = %v, want 1", photo.Result.Confidence)
	}
	if len(photo.Result.Evidence) != 2 ||
		photo.Result.Evidence[0].Method != "suffix" || photo.Result.Evidence[0].Value != "suffix:.jpg" ||
		photo.Result.Evidence[1].Method != "magic" || photo.Result.Evidence[1].Value != "magic:jpeg-soi" {
		t.Errorf("photo.jpg evidence = %+v, want suffix:.jpg then magic:jpeg-soi", photo.Result.Evidence)
	}

	// nested/pic.png: same shape as photo.jpg, from inside a subdirectory.
	pic := requireDetection(t, findEntry(t, sink, "pic.png"))
	if pic.Result.FormatID != "png" || pic.Result.MediaType != "image/png" {
		t.Errorf("pic.png result = %+v, want png/image/png", pic.Result)
	}

	// a.txt: suffix evidence flows through, but a suffix alone is only a
	// heuristic hint (AMBIGUOUS inside identify), so no format is claimed.
	text := requireDetection(t, findEntry(t, sink, "a.txt"))
	if text.Result.FormatID != "" || text.Result.MediaType != "" || text.Result.Confidence != 0 {
		t.Errorf("a.txt result = %+v, want empty format fields for suffix-only evidence", text.Result)
	}
	if len(text.Result.Evidence) != 1 ||
		text.Result.Evidence[0].Method != "suffix" || text.Result.Evidence[0].Value != "suffix:.txt" {
		t.Errorf("a.txt evidence = %+v, want one suffix:.txt line", text.Result.Evidence)
	}

	// Directories never run detection: the record keeps NOT_REQUESTED.
	nested := findEntry(t, sink, "nested")
	if nested.Kind != scanner.KindDirectory || nested.Detection.State != scanner.DetectionNotRequested {
		t.Errorf("nested dir = %+v, want directory with NOT_REQUESTED detection", nested)
	}
}

func TestScannerDetectorUnknownFormatHasNoFalsePositive(t *testing.T) {
	root := t.TempDir()
	// Random bytes with no matching suffix and no magic signature: the
	// detector must claim nothing, while the scan itself still succeeds.
	writeTestFile(t, root, "data.bin", []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x11, 0x22, 0x33})

	sink, result := scanTree(t, root, testScannerDetector())
	if result.State != scanner.ScanComplete {
		t.Fatalf("scan state = %s, want COMPLETE; result: %+v", result.State, result)
	}

	bin := requireDetection(t, findEntry(t, sink, "data.bin"))
	if bin.Result.FormatID != "" || bin.Result.MediaType != "" || bin.Result.Confidence != 0 {
		t.Errorf("data.bin result = %+v, want no claimed format", bin.Result)
	}
	if len(bin.Result.Evidence) != 0 {
		t.Errorf("data.bin evidence = %+v, want none", bin.Result.Evidence)
	}
	if len(bin.Result.DetectorID) == 0 || len(bin.Result.DetectorVersion) == 0 {
		t.Errorf("data.bin result = %+v, want detector id and version recorded", bin.Result)
	}
}

func TestScannerDetectorEmptyDirectoryScanCompletes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sink, result := scanTree(t, root, testScannerDetector())
	if result.State != scanner.ScanComplete {
		t.Fatalf("scan state = %s, want COMPLETE; result: %+v", result.State, result)
	}
	if result.Entries != 2 || result.RegularFiles != 0 {
		t.Errorf("scan counts = %+v, want 2 entries and no regular files", result)
	}
	if len(sink.entries) != 2 {
		t.Fatalf("sink has %d entries, want root and empty dir", len(sink.entries))
	}
	for _, entry := range sink.entries {
		if entry.Kind != scanner.KindDirectory {
			t.Errorf("entry %q kind = %s, want DIRECTORY", entry.Name, entry.Kind)
		}
		if entry.Detection.State != scanner.DetectionNotRequested {
			t.Errorf("entry %q detection state = %s, want NOT_REQUESTED", entry.Name, entry.Detection.State)
		}
	}
}
