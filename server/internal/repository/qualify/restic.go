package qualify

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type resticEngine struct {
	bin  string
	repo string
}

func (e resticEngine) name() string { return "restic" }

func (e resticEngine) repoDir(work string) string {
	if e.repo != "" {
		return e.repo
	}
	return filepath.Join(work, "restic-repo")
}

func (e resticEngine) env(work string) []string {
	return e.envWithPassword(work, spikePassword)
}

func (e resticEngine) envWithPassword(work, password string) []string {
	cache := filepath.Join(work, "restic-cache")
	return append(os.Environ(),
		"RESTIC_REPOSITORY="+e.repoDir(work),
		"RESTIC_PASSWORD="+password,
		"RESTIC_CACHE_DIR="+cache,
	)
}

func (e resticEngine) init(work string) error {
	if err := os.MkdirAll(e.repoDir(work), 0o700); err != nil {
		return err
	}
	return runCmd(work, e.bin, e.env(work), "init", "--quiet")
}

func (e resticEngine) backup(work, src string) error {
	return runCmd(work, e.bin, e.env(work), "backup", "--quiet", src)
}

func (e resticEngine) restore(work, dest string) error {
	return e.restoreNamed(work, dest, "latest")
}

func (e resticEngine) restoreNamed(work, dest, snapshot string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return runCmd(work, e.bin, e.env(work), "restore", snapshot, "--target", dest)
}

func (e resticEngine) snapshotIDs(work string) ([]string, error) {
	out, err := runCmdOutput(work, e.bin, e.env(work), "snapshots", "--json", "--quiet")
	if err != nil {
		return nil, err
	}
	var snaps []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &snaps); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		if snap.ID != "" {
			ids = append(ids, snap.ID)
		}
	}
	return uniqueStrings(ids), nil
}

func (e resticEngine) check(work string) error {
	return runCmd(work, e.bin, e.env(work), "check", "--read-data")
}

func (e resticEngine) forgetKeepLatest(work string) error {
	return runCmd(work, e.bin, e.env(work), "forget", "--keep-last", "1", "--prune", "--quiet")
}
