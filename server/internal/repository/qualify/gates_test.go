package qualify

import (
	"bytes"
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

func TestLocalZstdEncryptedPassesKeyGates(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x5a}, 32)
	provider := repository.KeyProviderFunc(func(_ context.Context, ref string) ([]byte, error) {
		if ref != "key://qualification" {
			return nil, errors.New("unexpected key reference")
		}
		return append([]byte(nil), key...), nil
	})
	if err := EncryptedGates(ctx, filepath.Join(t.TempDir(), "encrypted"), "key://qualification", provider, []byte("encrypted qualification payload")); err != nil {
		t.Fatal(err)
	}
}

func TestInTreeProfilesPassRepairGates(t *testing.T) {
	ctx := context.Background()
	payload := []byte("repair qualification payload")
	raw, err := repository.OpenDir(filepath.Join(t.TempDir(), "raw"))
	if err != nil {
		t.Fatal(err)
	}
	if err := RepairGates(ctx, raw, payload); err != nil {
		t.Fatalf("raw repair gate: %v", err)
	}
	zstd, err := repository.OpenZstdDir(filepath.Join(t.TempDir(), "zstd"))
	if err != nil {
		t.Fatal(err)
	}
	if err := RepairGates(ctx, zstd, payload); err != nil {
		t.Fatalf("zstd repair gate: %v", err)
	}
}

func TestResticControlBackupRestoreIndependentHash(t *testing.T) {
	bin, err := exec.LookPath("restic")
	if err != nil {
		t.Skip("restic not on PATH; control probe skipped")
	}
	runBackupEngine(t, resticEngine{bin: bin})
}

func TestResticEncryptionAndWrongCredentialFailClosed(t *testing.T) {
	bin, err := exec.LookPath("restic")
	if err != nil {
		t.Skip("restic not on PATH; credential probe skipped")
	}
	work := t.TempDir()
	src := filepath.Join(work, "corpus")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "secret-check.txt"), []byte("encrypted candidate probe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := resticEngine{bin: bin}
	if err := engine.init(work); err != nil {
		t.Fatalf("restic init: %v", err)
	}
	if err := engine.backup(work, src); err != nil {
		t.Fatalf("restic backup: %v", err)
	}
	if err := runCmd(work, engine.bin, engine.envWithPassword(work, "wrong-password"), "check"); err == nil {
		t.Fatal("restic accepted an incorrect repository password")
	}
	var sawPassword bool
	if err := filepath.WalkDir(engine.repoDir(work), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(payload, []byte(spikePassword)) {
			sawPassword = true
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect restic repository: %v", err)
	}
	if sawPassword {
		t.Fatal("restic repository contains the plaintext password")
	}
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
