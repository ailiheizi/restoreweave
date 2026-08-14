package qualify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type kopiaEngine struct {
	bin  string
	repo string
}

func (e kopiaEngine) name() string { return "kopia" }

func (e kopiaEngine) repoDir(work string) string {
	if e.repo != "" {
		return e.repo
	}
	return filepath.Join(work, "kopia-repo")
}

func (e kopiaEngine) env(work string) []string {
	return append(os.Environ(),
		"KOPIA_PASSWORD="+spikePassword,
		"KOPIA_CONFIG_PATH="+filepath.Join(work, "kopia.config"),
		"KOPIA_LOG_DIR="+filepath.Join(work, "kopia-log"),
		"KOPIA_CACHE_DIRECTORY="+filepath.Join(work, "kopia-cache"),
		"KOPIA_CHECK_FOR_UPDATES=false",
	)
}

func (e kopiaEngine) init(work string) error {
	if err := os.MkdirAll(e.repoDir(work), 0o700); err != nil {
		return err
	}
	return runCmd(work, e.bin, e.env(work),
		"repository", "create", "filesystem",
		"--path", e.repoDir(work),
		"--password", spikePassword,
	)
}

func (e kopiaEngine) backup(work, src string) error {
	return runCmd(work, e.bin, e.env(work),
		"snapshot", "create", src,
		"--password", spikePassword,
	)
}

func (e kopiaEngine) restore(work, dest string) error {
	ids, err := e.snapshotIDs(work)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("kopia snapshot list had no snapshot id")
	}
	return e.restoreNamed(work, dest, ids[len(ids)-1])
}

func (e kopiaEngine) restoreNamed(work, dest, snapshot string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return runCmd(work, e.bin, e.env(work),
		"snapshot", "restore", snapshot, dest,
		"--password", spikePassword,
	)
}

func (e kopiaEngine) snapshotIDs(work string) ([]string, error) {
	listed, err := runCmdOutput(work, e.bin, e.env(work),
		"snapshot", "list", "--json",
		"--password", spikePassword,
	)
	if err != nil {
		return nil, err
	}
	var snaps []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(listed), &snaps); err != nil {
		return nil, fmt.Errorf("kopia snapshot list json: %w\n%s", err, listed)
	}
	ids := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		if snap.ID != "" {
			ids = append(ids, snap.ID)
		}
	}
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("kopia snapshot list had no snapshot id:\n%s", listed)
	}
	return ids, nil
}

func (e kopiaEngine) check(work string) error {
	if err := os.RemoveAll(filepath.Join(work, "kopia-cache")); err != nil {
		return err
	}
	return runCmd(work, e.bin, e.env(work),
		"content", "verify", "--full", "--download-percent=100",
		"--password", spikePassword,
	)
}

func (e kopiaEngine) forgetKeepLatest(work string) error {
	if err := runCmd(work, e.bin, e.env(work),
		"policy", "set", "--global",
		"--keep-latest", "1",
		"--keep-hourly", "0",
		"--keep-daily", "0",
		"--keep-weekly", "0",
		"--keep-monthly", "0",
		"--keep-annual", "0",
		"--password", spikePassword,
	); err != nil {
		return err
	}
	if err := runCmd(work, e.bin, e.env(work),
		"snapshot", "expire", "--all", "--delete",
		"--password", spikePassword,
	); err != nil {
		return err
	}
	return runCmd(work, e.bin, e.env(work),
		"maintenance", "run", "--full", "--safety=none",
		"--password", spikePassword,
	)
}

func (e kopiaEngine) connect(work string) error {
	return runCmd(work, e.bin, e.env(work),
		"repository", "connect", "filesystem",
		"--path", e.repoDir(work),
		"--password", spikePassword,
	)
}
