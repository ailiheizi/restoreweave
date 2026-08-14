package local

import (
	"os"
	"path/filepath"
)

const SocketEnv = "RESTOREWEAVE_SOCKET"

func DefaultSocketPath() string {
	if value := os.Getenv(SocketEnv); value != "" {
		return value
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "restoreweave", "restoreweaved.sock")
	}
	return filepath.Join(os.TempDir(), "restoreweave", "restoreweaved.sock")
}

func DefaultCatalogPath() string {
	if value := os.Getenv("RESTOREWEAVE_CATALOG"); value != "" {
		return value
	}
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "restoreweave", "catalog.sqlite")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "restoreweave", "catalog.sqlite")
	}
	return filepath.Join(home, ".local", "share", "restoreweave", "catalog.sqlite")
}

func DefaultRepositoryPath() string {
	if value := os.Getenv("RESTOREWEAVE_REPOSITORY"); value != "" {
		return value
	}
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "restoreweave", "repository")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "restoreweave", "repository")
	}
	return filepath.Join(home, ".local", "share", "restoreweave", "repository")
}
