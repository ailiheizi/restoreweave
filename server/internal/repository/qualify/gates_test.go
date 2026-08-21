package qualify

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

func TestFakeCASPassesDriverGates(t *testing.T) {
	ctx := context.Background()
	payload := []byte("qualification-exact-bytes")
	dir, err := repository.OpenDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatalf("open dir: %v", err)
	}
	if err := DriverGates(ctx, dir, payload); err != nil {
		t.Fatalf("dir driver gates: %v", err)
	}
	if err := DriverGates(ctx, repository.NewMemory(), payload); err != nil {
		t.Fatalf("memory driver gates: %v", err)
	}

	want := contentID(payload)
	if err := CorruptStoredObject(dir.Root()); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	err = dir.Verify(ctx, want)
	if !errors.Is(err, repository.ErrDigestMismatch) {
		t.Fatalf("verify after corruption = %v, want digest mismatch", err)
	}
}

func TestLocalZstdPassesDriverGates(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.OpenZstdDir(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatalf("open zstd: %v", err)
	}
	if err := DriverGates(ctx, repo, []byte("zstd qualification payload")); err != nil {
		t.Fatalf("zstd driver gates: %v", err)
	}
}

func TestResticControlBackupRestoreIndependentHash(t *testing.T) {
	bin, err := exec.LookPath("restic")
	if err != nil {
		t.Skip("restic not on PATH; control probe skipped")
	}
	runBackupEngine(t, resticEngine{bin: bin})
}

func TestKopiaProbeBackupRestoreIndependentHash(t *testing.T) {
	bin := os.Getenv("KOPIA_BIN")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("kopia")
		if err != nil {
			t.Skip("kopia not on PATH; engine probe skipped (not a selection)")
		}
	}
	runBackupEngine(t, kopiaEngine{bin: bin})
}
