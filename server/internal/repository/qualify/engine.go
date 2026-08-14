package qualify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const spikePassword = "spike-test"

type backupEngine interface {
	name() string
	init(work string) error
	backup(work, src string) error
	restore(work, dest string) error
	restoreNamed(work, dest, snapshot string) error
	check(work string) error
	repoDir(work string) string
	forgetKeepLatest(work string) error
	snapshotIDs(work string) ([]string, error)
}

func runBackupEngine(t *testing.T, engine backupEngine) {
	t.Helper()
	work := t.TempDir()
	src := filepath.Join(work, "corpus")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatalf("mkdir corpus: %v", err)
	}
	payload := []byte("engine-control-unique-bytes\n")
	if err := os.WriteFile(filepath.Join(src, "note.txt"), payload, 0o600); err != nil {
		t.Fatalf("write corpus: %v", err)
	}
	if err := engine.init(work); err != nil {
		t.Fatalf("%s init: %v", engine.name(), err)
	}
	if err := engine.backup(work, src); err != nil {
		t.Fatalf("%s backup: %v", engine.name(), err)
	}
	if err := engine.check(work); err != nil {
		t.Fatalf("%s check after backup: %v", engine.name(), err)
	}
	dest := filepath.Join(work, "restore")
	if err := engine.restore(work, dest); err != nil {
		t.Fatalf("%s restore: %v", engine.name(), err)
	}
	got, err := findRestoredFile(dest, "note.txt")
	if err != nil {
		t.Fatalf("find restored file: %v", err)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("%s restored bytes = %q", engine.name(), body)
	}
	if digestFile(body) != digestFile(payload) {
		t.Fatalf("%s independent sha256 mismatch", engine.name())
	}
	if err := corruptTree(engine.repoDir(work)); err != nil {
		t.Fatalf("corrupt repo: %v", err)
	}
	if err := engine.check(work); err == nil {
		t.Fatalf("%s check succeeded after corruption", engine.name())
	}
}

func findRestoredFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("%s not found under %s", name, root)
	}
	return found, nil
}

func corruptTree(root string) error {
	var flipped int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, statErr := d.Info()
		if statErr != nil || info.Size() < 32 {
			return statErr
		}
		base := filepath.Base(path)
		if base == "config" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "kopia.") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		data[len(data)/2] ^= 0xff
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		flipped++
		return nil
	})
	if err != nil {
		return err
	}
	if flipped == 0 {
		return fmt.Errorf("no file to corrupt under %s", root)
	}
	return nil
}

func digestFile(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func removedIDs(before, after []string) []string {
	keep := make(map[string]struct{}, len(after))
	for _, id := range after {
		keep[id] = struct{}{}
	}
	var gone []string
	for _, id := range before {
		if _, ok := keep[id]; !ok {
			gone = append(gone, id)
		}
	}
	return gone
}

func runCmd(dir, bin string, env []string, args ...string) error {
	_, err := runCmdOutput(dir, bin, env, args...)
	return err
}

func runCmdOutput(dir, bin string, env []string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %v: %w\n%s", bin, args, err, out)
	}
	return string(out), nil
}
