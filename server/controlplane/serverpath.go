package controlplane

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Socket environment variables. The server resolves its socket and catalog
// paths itself and never imports the client's path package.
const (
	SocketEnv      = "RESTOREWEAVE_SOCKET"
	CatalogEnv     = "RESTOREWEAVE_CATALOG"
	RepositoryEnv  = "RESTOREWEAVE_REPOSITORY"
	socketRelPath  = "restoreweave/restoreweaved.sock"
	catalogRelDir  = "restoreweave"
	catalogName    = "catalog.sqlite"
	repositoryName = "repository"
)

// DefaultSocketPath mirrors the client-side default resolution: explicit
// environment variable, then XDG_RUNTIME_DIR, then the system temp directory.
func DefaultSocketPath() string {
	if value := os.Getenv(SocketEnv); value != "" {
		return value
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, socketRelPath)
	}
	return filepath.Join(os.TempDir(), socketRelPath)
}

// DefaultCatalogPath resolves the catalog database the same way the client
// side does: explicit environment variable, then XDG_DATA_HOME, then the
// user's ~/.local/share directory.
func DefaultCatalogPath() string {
	if value := os.Getenv(CatalogEnv); value != "" {
		return value
	}
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, catalogRelDir, catalogName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), catalogRelDir, catalogName)
	}
	return filepath.Join(home, ".local", "share", catalogRelDir, catalogName)
}

// DefaultRepositoryPath resolves the exact-lane blob store beside the catalog.
func DefaultRepositoryPath() string {
	if value := os.Getenv(RepositoryEnv); value != "" {
		return value
	}
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, catalogRelDir, repositoryName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), catalogRelDir, repositoryName)
	}
	return filepath.Join(home, ".local", "share", catalogRelDir, repositoryName)
}

// ErrSocketInUse is returned when a stale socket file cannot be reclaimed
// because another daemon is already listening on it.
var ErrSocketInUse = errors.New("socket path is already in use by another restoreweaved")

// prepareSocketPath creates the socket parent directory and reclaims a stale
// socket file: if the path exists but nothing is listening, it is removed.
func prepareSocketPath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("socket path exists and is not a socket: %s", absolute)
	}
	probe, probeErr := socketProbe(absolute)
	if probeErr == nil {
		_ = probe.Close()
		return fmt.Errorf("%w: %s", ErrSocketInUse, absolute)
	}
	if err := os.Remove(absolute); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}
