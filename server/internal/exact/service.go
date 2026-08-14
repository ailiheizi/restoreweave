package exact

import (
	"context"
	"errors"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/identify"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

var (
	ErrNotQualified = errors.New("scan is not capture-qualified for authoritative ingest")
	ErrBlocked      = errors.New("exact ingest is blocked")
)

const defaultWorkspaceName = "default"

// PublicationProcessor is the optional post-publication Processor host.
// Failures must not fail exact ingest, verification, or restore.
type PublicationProcessor interface {
	ProcessPublication(ctx context.Context, workspaceID, snapshotRef, rootID string) error
}

// Service is the exact-lane host: capture, hash, place, publish, restore.
type Service struct {
	Store     *sqlite.Store
	Repo      repository.Driver
	Capture   *capture.LocalTreeDriver
	Identify  *identify.Detector
	Indexer   *search.Indexer
	Processor PublicationProcessor
	Now       func() time.Time
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) detector() *identify.Detector {
	if s != nil && s.Identify != nil {
		return s.Identify
	}
	return identify.NewDetector(0)
}

func (s *Service) captureDriver() *capture.LocalTreeDriver {
	if s != nil && s.Capture != nil {
		return s.Capture
	}
	return &capture.LocalTreeDriver{Now: s.now}
}

func (s *Service) require() error {
	if s == nil || s.Store == nil || s.Repo == nil {
		return errors.New("exact service requires a catalog and repository")
	}
	return nil
}

// IngestResult is returned after a capture-qualified exact publication.
type IngestResult struct {
	WorkspaceID    string
	SourceID       string
	ScanID         string
	BindingID      string
	RootID         string
	SnapshotRef    string
	ManifestDigest string
	Files          int
	Bytes          int64
}

// RestoreResult is returned after a catalog-free restore.
type RestoreResult struct {
	SnapshotRef string
	Destination string
	Files       int
	Bytes       int64
}

// VerifyResult is returned after independent blob readback.
type VerifyResult struct {
	SnapshotRef string
	Entries     int
	Files       int
	Bytes       int64
}
