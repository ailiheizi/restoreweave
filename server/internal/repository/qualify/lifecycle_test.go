package qualify

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestResticCrashRetryStillRestores(t *testing.T) {
	engine, ok := resticOrSkip(t)
	if !ok {
		return
	}
	runCrashRetry(t, engine)
}

func TestResticGCKeepsLatestOnly(t *testing.T) {
	engine, ok := resticOrSkip(t)
	if !ok {
		return
	}
	runGCKeepLatest(t, engine)
}

func TestResticRepoRelocationStillRestores(t *testing.T) {
	engine, ok := resticOrSkip(t)
	if !ok {
		return
	}
	runRepoRelocation(t, engine)
}

func TestKopiaCrashRetryStillRestores(t *testing.T) {
	engine, ok := kopiaOrSkip(t)
	if !ok {
		return
	}
	runCrashRetry(t, engine)
}

func TestKopiaGCKeepsLatestOnly(t *testing.T) {
	engine, ok := kopiaOrSkip(t)
	if !ok {
		return
	}
	runGCKeepLatest(t, engine)
}

func TestKopiaRepoRelocationStillRestores(t *testing.T) {
	engine, ok := kopiaOrSkip(t)
	if !ok {
		return
	}
	runKopiaRelocation(t, engine)
}

func resticOrSkip(t *testing.T) (resticEngine, bool) {
	t.Helper()
	bin, err := exec.LookPath("restic")
	if err != nil {
		t.Skip("restic not on PATH; control probe skipped")
		return resticEngine{}, false
	}
	return resticEngine{bin: bin}, true
}

func kopiaOrSkip(t *testing.T) (kopiaEngine, bool) {
	t.Helper()
	bin := os.Getenv("KOPIA_BIN")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("kopia")
		if err != nil {
			t.Skip("kopia not on PATH; engine probe skipped (not a selection)")
			return kopiaEngine{}, false
		}
	}
	return kopiaEngine{bin: bin}, true
}

func runCrashRetry(t *testing.T, engine backupEngine) {
	t.Helper()
	work := t.TempDir()
	src := filepath.Join(work, "corpus")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := bytes.Repeat([]byte("crash-retry-block\n"), 32*1024)
	if err := os.WriteFile(filepath.Join(src, "blob.bin"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := engine.init(work); err != nil {
		t.Fatalf("init: %v", err)
	}
	interruptBackup(t, engine, work, src)
	if err := engine.backup(work, src); err != nil {
		t.Fatalf("retry backup: %v", err)
	}
	dest := filepath.Join(work, "restore")
	if err := engine.restore(work, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := findRestoredFile(dest, "blob.bin")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("restored size = %d, want %d", len(body), len(payload))
	}
}

func runGCKeepLatest(t *testing.T, engine backupEngine) {
	t.Helper()
	work := t.TempDir()
	src := filepath.Join(work, "corpus")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	keep := []byte("keep-after-gc\n")
	drop := []byte("drop-after-gc\n")
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), keep, 0o600); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "drop.txt"), drop, 0o600); err != nil {
		t.Fatalf("write drop: %v", err)
	}
	if err := engine.init(work); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := engine.backup(work, src); err != nil {
		t.Fatalf("backup 1: %v", err)
	}
	if err := os.Remove(filepath.Join(src, "drop.txt")); err != nil {
		t.Fatalf("remove drop: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if err := engine.backup(work, src); err != nil {
		t.Fatalf("backup 2: %v", err)
	}
	before, err := engine.snapshotIDs(work)
	if err != nil {
		t.Fatalf("list before GC: %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("snapshots before GC = %d (%v), want 2", len(before), before)
	}
	if err := engine.forgetKeepLatest(work); err != nil {
		t.Fatalf("forget/prune: %v", err)
	}
	after, err := engine.snapshotIDs(work)
	if err != nil {
		t.Fatalf("list after GC: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("snapshots after GC = %d (%v), want 1", len(after), after)
	}
	gone := removedIDs(before, after)
	if len(gone) == 0 {
		t.Fatal("forget/prune did not remove an older snapshot")
	}
	dest := filepath.Join(work, "restore")
	if err := engine.restore(work, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := findRestoredFile(dest, "keep.txt"); err != nil {
		t.Fatalf("keep.txt missing after GC: %v", err)
	}
	if _, err := findRestoredFile(dest, "drop.txt"); err == nil {
		t.Fatal("drop.txt still present after keep-latest prune")
	}
	forgotten := filepath.Join(work, "forgotten")
	if err := engine.restoreNamed(work, forgotten, gone[0]); err == nil {
		t.Fatal("forgotten snapshot still restorable after keep-latest prune")
	}
}

func runRepoRelocation(t *testing.T, engine resticEngine) {
	t.Helper()
	work := t.TempDir()
	src := filepath.Join(work, "corpus")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := []byte("relocated-repo-bytes\n")
	if err := os.WriteFile(filepath.Join(src, "note.txt"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := engine.init(work); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := engine.backup(work, src); err != nil {
		t.Fatalf("backup: %v", err)
	}
	moved := filepath.Join(work, "restic-repo-moved")
	if err := os.Rename(engine.repoDir(work), moved); err != nil {
		t.Fatalf("rename repo: %v", err)
	}
	relocated := resticEngine{bin: engine.bin, repo: moved}
	dest := filepath.Join(work, "restore")
	if err := relocated.restore(work, dest); err != nil {
		t.Fatalf("restore after relocate: %v", err)
	}
	got, err := findRestoredFile(dest, "note.txt")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != string(payload) {
		t.Fatalf("restored = %q", body)
	}
}

func runKopiaRelocation(t *testing.T, engine kopiaEngine) {
	t.Helper()
	work := t.TempDir()
	src := filepath.Join(work, "corpus")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := []byte("relocated-kopia-repo-bytes\n")
	if err := os.WriteFile(filepath.Join(src, "note.txt"), payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := engine.init(work); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := engine.backup(work, src); err != nil {
		t.Fatalf("backup: %v", err)
	}
	moved := filepath.Join(work, "kopia-repo-moved")
	if err := os.Rename(engine.repoDir(work), moved); err != nil {
		t.Fatalf("rename repo: %v", err)
	}
	relocated := kopiaEngine{bin: engine.bin, repo: moved}
	if err := relocated.connect(work); err != nil {
		t.Fatalf("connect relocated repo: %v", err)
	}
	dest := filepath.Join(work, "restore")
	if err := relocated.restore(work, dest); err != nil {
		t.Fatalf("restore after relocate: %v", err)
	}
	got, err := findRestoredFile(dest, "note.txt")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != string(payload) {
		t.Fatalf("restored = %q", body)
	}
}

func interruptBackup(t *testing.T, engine backupEngine, work, src string) {
	t.Helper()
	var cmd *exec.Cmd
	switch e := engine.(type) {
	case resticEngine:
		cmd = exec.Command(e.bin, "backup", "--quiet", src)
		cmd.Dir = work
		cmd.Env = e.env(work)
	case kopiaEngine:
		cmd = exec.Command(e.bin, "snapshot", "create", src,
			"--password", spikePassword)
		cmd.Dir = work
		cmd.Env = e.env(work)
	default:
		_ = engine.backup(work, src)
		return
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start backup: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}
