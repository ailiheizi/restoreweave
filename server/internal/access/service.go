// Package access implements host-owned SnapshotTree and FileAccess over the
// operational catalog and exact-lane CAS. Gateways receive only this facade.
package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/decode"
	"github.com/ailiheizi/restoreweave/server/internal/readsvc"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const (
	localPrincipal   = "principal:local-operator"
	localIdentity    = "identity:local-operator"
	localAudience    = "restoreweave:local"
	localPolicyEpoch = "policy:local-v1"
	localPolicyRef   = "policy:local-v1"
	defaultTTL       = 24 * time.Hour
)

var (
	ErrNotFile    = errors.New("entry is not a regular file")
	ErrNotSymlink = errors.New("entry is not a symlink")
	ErrNoExact    = errors.New("entry has no exact representation")
	ErrViewClosed = errors.New("snapshot view is closed")
	ErrWrongView  = errors.New("file access request used a foreign snapshot view")
)

// Service is the catalog-backed SnapshotTree, FileAccess, and GatewayHost.
type Service struct {
	Store   *sqlite.Store
	Repo    repository.Driver
	Decoder readsvc.RepresentationDecoder
	Now     func() time.Time
	TTL     time.Duration
}

func (s *Service) decoder() readsvc.RepresentationDecoder {
	if s != nil && s.Decoder != nil {
		return s.Decoder
	}
	return decode.IdentityDecoder{}
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) ttl() time.Duration {
	if s != nil && s.TTL > 0 {
		return s.TTL
	}
	return defaultTTL
}

func (s *Service) require() error {
	if s == nil || s.Store == nil || s.Repo == nil {
		return errors.New("access service requires a catalog and repository")
	}
	return nil
}

// OpenView implements readsvc.SnapshotTree.
func (s *Service) OpenView(ctx context.Context, request readsvc.SnapshotViewRequest) (readsvc.SnapshotView, error) {
	if err := s.require(); err != nil {
		return nil, err
	}
	if err := request.Access.Validate(); err != nil {
		return nil, err
	}
	if err := request.Snapshot.Validate(); err != nil {
		return nil, err
	}
	publication, err := s.Store.GetPublication(ctx, request.Snapshot.WorkspaceID, request.Snapshot.PublicationID)
	if err != nil {
		return nil, err
	}
	if publication.NamespaceRootID != request.Snapshot.NamespaceRootID {
		return nil, fmt.Errorf("publication namespace root mismatch")
	}
	root, err := s.Store.GetNamespaceRoot(ctx, publication.WorkspaceID, publication.NamespaceRootID)
	if err != nil {
		return nil, err
	}
	if request.Snapshot.ExpectedNamespaceDigest != "" && request.Snapshot.ExpectedNamespaceDigest != publication.ManifestDigest {
		return nil, fmt.Errorf("%w: namespace digest mismatch", readsvc.ErrInvalidPin)
	}
	expiry := s.now().Add(s.ttl())
	decisionID, err := sqlite.NewStableID("dec")
	if err != nil {
		return nil, err
	}
	pin := readsvc.ViewPin{
		Snapshot: readsvc.SnapshotPin{
			WorkspaceID:         publication.WorkspaceID,
			PublicationID:       publication.ID,
			NamespaceRootID:     publication.NamespaceRootID,
			NamespaceGeneration: 1,
			NamespaceDigest:     publication.ManifestDigest,
		},
		Authorization: readsvc.AuthorizationPin{
			DecisionID:  decisionID,
			PrincipalID: localPrincipal,
			PolicyEpoch: localPolicyEpoch,
			ExpiresAt:   expiry,
		},
	}
	return &catalogView{
		service:     s,
		pin:         pin,
		root:        root,
		publication: publication,
	}, nil
}

// OpenSession implements readsvc.FileAccess.
func (s *Service) OpenSession(ctx context.Context, request readsvc.OpenFileRequest) (readsvc.ReadSession, error) {
	if err := s.require(); err != nil {
		return nil, err
	}
	view, ok := request.View.(*catalogView)
	if !ok || view == nil {
		return nil, ErrWrongView
	}
	if view.closed {
		return nil, ErrViewClosed
	}
	if err := view.pin.Authorization.Validate(); err != nil {
		return nil, err
	}
	if !s.now().Before(view.pin.Authorization.ExpiresAt) {
		return nil, readsvc.ErrSessionExpired
	}
	entry, err := view.lookupEntry(ctx, request.EntryID)
	if err != nil {
		return nil, err
	}
	if entry.Kind != readsvc.EntryRegularFile {
		return nil, ErrNotFile
	}
	if entry.ContentID == "" || entry.FileVersionID == "" {
		return nil, ErrNoExact
	}
	version, err := s.Store.GetFileVersion(ctx, view.pin.Snapshot.WorkspaceID, entry.FileVersionID)
	if err != nil {
		return nil, err
	}
	representationID := request.RepresentationID
	if representationID == "" {
		representationID = version.AuthoritativeRepresentationID
	}
	if representationID != version.AuthoritativeRepresentationID {
		return nil, ErrNoExact
	}
	size := uint64(0)
	if entry.HasLogicalSize {
		size = entry.LogicalSize
	} else if version.LogicalSize >= 0 {
		size = uint64(version.LogicalSize)
	}
	limits := request.Limits
	if err := fillLimits(&limits, size, view.pin.Authorization.ExpiresAt, s.now()); err != nil {
		return nil, err
	}
	sessionID, err := sqlite.NewStableID("ses")
	if err != nil {
		return nil, err
	}
	info := readsvc.ReadSessionInfo{
		ID: sessionID,
		Pin: readsvc.ReadPin{
			View:                      view.pin,
			EntryID:                   entry.ID,
			FileVersionID:             version.ID,
			RepresentationID:          representationID,
			ContentID:                 version.ContentID,
			PlacementCheckpointID:     "cas:" + version.ContentID,
			PlacementCheckpointDigest: version.ContentID,
		},
		Limits: limits,
	}
	if err := info.Validate(view.pin, size, s.now()); err != nil {
		return nil, err
	}
	return &readSession{
		service: s,
		view:    view,
		info:    info,
		size:    size,
	}, nil
}

// Host implements readsvc.GatewayHost for one already-opened view.
func (s *Service) Host(view readsvc.SnapshotView) (readsvc.GatewayHost, error) {
	catalog, ok := view.(*catalogView)
	if !ok || catalog == nil {
		return nil, ErrWrongView
	}
	return &gatewayHost{service: s, view: catalog}, nil
}

type gatewayHost struct {
	service *Service
	view    *catalogView
}

func (h *gatewayHost) View() readsvc.SnapshotView { return h.view }

func (h *gatewayHost) OpenEntrySession(ctx context.Context, request readsvc.GatewayEntryOpenRequest) (readsvc.ReadSession, error) {
	return h.service.OpenSession(ctx, readsvc.OpenFileRequest{
		Access:           request.Access,
		View:             h.view,
		EntryID:          request.EntryID,
		RepresentationID: request.RepresentationID,
		Limits:           request.Limits,
	})
}

func fillLimits(limits *readsvc.SessionLimits, fileSize uint64, authExpiry, now time.Time) error {
	if limits.MaxBytes == 0 {
		limits.MaxBytes = 1 << 20
	}
	if limits.MaxOpens == 0 {
		limits.MaxOpens = 64
	}
	if limits.MaxConcurrentReads == 0 {
		limits.MaxConcurrentReads = 8
	}
	if limits.ExpiresAt.IsZero() {
		limits.ExpiresAt = authExpiry
	}
	return limits.Validate(fileSize, now)
}

func LocalAccess(requestID string) readsvc.AccessRequest {
	if requestID == "" {
		requestID = "req:local"
	}
	return readsvc.AccessRequest{
		IdentityContextID: localIdentity,
		RequestID:         requestID,
		Audience:          localAudience,
	}
}

func SelectorFromPublication(publication sqlite.Publication) readsvc.SnapshotSelector {
	return readsvc.SnapshotSelector{
		WorkspaceID:             publication.WorkspaceID,
		PublicationID:           publication.ID,
		NamespaceRootID:         publication.NamespaceRootID,
		ExpectedNamespaceDigest: publication.ManifestDigest,
	}
}

var (
	_ readsvc.SnapshotTree = (*Service)(nil)
	_ readsvc.FileAccess   = (*Service)(nil)
)
