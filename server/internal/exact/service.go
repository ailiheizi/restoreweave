package exact

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/identify"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/search"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

var (
	ErrNotQualified = errors.New("scan is not capture-qualified for authoritative ingest")
	ErrBlocked      = errors.New("operation is blocked")
	// ErrIngestPlanBlocked means inspection produced a useful but deliberately
	// non-executable plan because one or more source entries could not be
	// captured authoritatively.
	ErrIngestPlanBlocked = errors.New("ingest plan contains blocked source entries")
)

const defaultWorkspaceName = "default"

// PublicationProcessor is the optional post-publication Processor host.
// Failures must not fail exact ingest, verification, or restore.
type PublicationProcessor interface {
	ProcessPublication(ctx context.Context, workspaceID, snapshotRef, rootID string) error
}

// Service is the exact-lane host: capture, hash, place, publish, restore.
type Service struct {
	Store                        *sqlite.Store
	Repo                         repository.Driver
	ConfigDigest                 string
	DefaultProtection            sqlite.ProtectionMode
	AllowLinkOnly                bool
	LinkOnlyRequiresConfirmation bool
	Capture                      *capture.LocalTreeDriver
	Identify                     *identify.Detector
	Indexer                      *search.Indexer
	Processor                    PublicationProcessor
	SigningIdentity              *SigningIdentity
	TrustAnchor                  *TrustAnchor
	PublicationDomain            string
	RequireSignedPublication     bool
	// PublicationFencer is the optional cross-process publication lease
	// provider. When nil and Store is non-nil, a store-backed fencer is
	// constructed lazily for signed publication. A repository-scoped lock can
	// provide the cross-process boundary for catalog-free signed writers;
	// signed writers without either coordination mechanism fail closed.
	PublicationFencer PublicationFencer
	Now               func() time.Time
	publicationMu     sync.Mutex
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

func (s *Service) requireRepository() error {
	if s == nil || s.Repo == nil {
		return errors.New("exact recovery requires a repository")
	}
	return nil
}

// ExportTrustAnchor writes the service's public verification anchor to an
// independently retained path. The signing identity and its private key never
// leave the daemon through this operation.
func (s *Service) ExportTrustAnchor(destination string) (string, error) {
	if s == nil || s.TrustAnchor == nil {
		return "", ErrRecoveryTrustAnchor
	}
	return ExportTrustAnchor(*s.TrustAnchor, destination)
}

// IngestResult is returned after a capture-qualified exact publication.
type IngestResult struct {
	WorkspaceID             string
	SourceID                string
	ScanID                  string
	BindingID               string
	RootID                  string
	SnapshotRef             string
	ManifestDigest          string
	ConfigDigest            string
	ProtectionDigest        string
	ProtectionMode          sqlite.ProtectionMode
	ProtectionDecisions     []IngestProtectionDecision
	Files                   int
	Bytes                   int64
	LocalFiles              int
	LocalBytes              int64
	NewBytes                int64
	LinkOnlyFiles           int
	LocatorCount            int
	Warnings                []string
	PreparedClosureDigest   string
	PublicationCommitDigest string
	PublicationGeneration   uint64
}

// RestoreResult is returned after a catalog-free restore.
type RestoreResult struct {
	SnapshotRef string
	Destination string
	Files       int
	Bytes       int64
}

// VerifyResult is returned after independent snapshot readback.
// AcceptedLevel is the mode that actually ran; sampled work is never
// relabeled as full-bytes or restore-verified.
type VerifyResult struct {
	SnapshotRef     string
	Mode            string
	AcceptedLevel   string
	Entries         int
	Files           int
	Bytes           int64
	AttemptedFiles  int
	AttemptedBytes  int64
	PassedFiles     int
	PassedBytes     int64
	OK              bool
	RestoreVerified bool
	CatalogUsed     bool
}
