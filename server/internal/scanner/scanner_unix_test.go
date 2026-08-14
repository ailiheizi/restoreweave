//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package scanner

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
)

func TestScanRecordsNamedPipeWithoutReadingIt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "events.pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo(%q) error = %v", path, err)
	}

	sink := &recordingSink{}
	scanner := mustNewScanner(t, Config{Sink: sink, Clock: fixedClock})
	result, err := scanner.Scan(context.Background(), ScanRequest{
		GenerationID: "special-generation",
		SourceID:     "special-source",
		Root:         root,
	})
	if err != nil || result.State != ScanComplete {
		t.Fatalf("Scan() = (%+v, %v), want complete", result, err)
	}
	record := requireRecord(t, sink.entries, "events.pipe")
	if record.Kind != KindNamedPipe || record.State != EntryComplete {
		t.Fatalf("named pipe record = %+v", record)
	}
	if record.Content != nil || record.Symlink != nil {
		t.Fatalf("named pipe was treated as ordinary content: %+v", record)
	}
	if result.SpecialFiles != 1 {
		t.Fatalf("special-file count = %d, want 1", result.SpecialFiles)
	}
}
