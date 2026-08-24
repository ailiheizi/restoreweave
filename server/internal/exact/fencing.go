package exact

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const (
	// DefaultPublicationFenceTTL is the lease length granted to a publication
	// owner. A crashed owner's lease therefore expires and unblocks other
	// processes before a manual takeover decision is needed.
	DefaultPublicationFenceTTL = 5 * time.Minute

	// FallbackFenceToken is the fencing token bound into signed records when
	// no cross-process fencer is configured. It preserves the historical
	// single-process behavior exactly.
	FallbackFenceToken = 1
)

var publicationFenceRenewInterval = DefaultPublicationFenceTTL / 3

// PublicationFencer is the optional cross-process publication lease seam.
// When nil, exact.Service serializes publication with its in-process
// publicationMu and stamps signed records with FallbackFenceToken.
type PublicationFencer interface {
	// Acquire leases publicationDomain for owner using the opaque leaseToken.
	// now and until define the lease window. A successful acquisition returns
	// the strictly increasing fencing token that must be bound into every
	// signed record published under the lease. Repeating an active request with
	// the same owner and leaseToken renews the window without changing its
	// fencing token.
	Acquire(ctx context.Context, publicationDomain, owner, leaseToken string, now, until time.Time) (token int64, err error)
	// Validate confirms that the exact owner, leaseToken, and fencingToken
	// still hold the domain at now. It must be checked immediately before
	// each signed record is placed.
	Validate(ctx context.Context, publicationDomain, owner, leaseToken string, fencingToken int64, now time.Time) error
	// Release expires the lease for the exact owner, leaseToken, and fencing
	// token. Releasing a stale or foreign lease fails closed.
	Release(ctx context.Context, publicationDomain, owner, leaseToken string, fencingToken int64, now time.Time) error
}

type publicationLocker interface {
	AcquirePublicationLock(context.Context, string) (io.Closer, error)
}

// sqlitePublicationFencer adapts sqlite.Store's publication_fences table to
// the PublicationFencer contract. Store.Update runs each operation inside a
// BEGIN IMMEDIATE transaction, so competing processes cannot both observe an
// unclaimed lease.
type sqlitePublicationFencer struct {
	store *sqlite.Store
	now   func() time.Time
}

var _ PublicationFencer = (*sqlitePublicationFencer)(nil)

// NewPublicationFencer returns a PublicationFencer backed by the store's
// publication_fences table. now defaults to time.Now.
func NewPublicationFencer(store *sqlite.Store, now func() time.Time) PublicationFencer {
	return &sqlitePublicationFencer{store: store, now: now}
}

func (f *sqlitePublicationFencer) acquireNow() time.Time {
	if f != nil && f.now != nil {
		return f.now().UTC()
	}
	return time.Now().UTC()
}

func (f *sqlitePublicationFencer) Acquire(ctx context.Context, publicationDomain, owner, leaseToken string, now, until time.Time) (int64, error) {
	if f == nil || f.store == nil {
		return 0, errors.New("publication fence provider is unavailable")
	}
	var fence sqlite.PublicationFence
	if err := f.store.Update(ctx, func(tx *sqlite.Tx) error {
		var err error
		fence, err = tx.AcquirePublicationFence(ctx, publicationDomain, owner, leaseToken, now, until)
		return err
	}); err != nil {
		return 0, fmt.Errorf("acquire publication fence: %w", err)
	}
	return fence.FencingToken, nil
}

func (f *sqlitePublicationFencer) Validate(ctx context.Context, publicationDomain, owner, leaseToken string, fencingToken int64, now time.Time) error {
	if f == nil || f.store == nil {
		return errors.New("publication fence provider is unavailable")
	}
	return f.store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.ValidatePublicationFence(ctx, publicationDomain, owner, leaseToken, fencingToken, now)
	})
}

func (f *sqlitePublicationFencer) Release(ctx context.Context, publicationDomain, owner, leaseToken string, fencingToken int64, now time.Time) error {
	if f == nil || f.store == nil {
		return errors.New("publication fence provider is unavailable")
	}
	return f.store.Update(ctx, func(tx *sqlite.Tx) error {
		return tx.ReleasePublicationFence(ctx, publicationDomain, owner, leaseToken, fencingToken, now)
	})
}

// fencer returns the configured PublicationFencer, or a store-backed fencer
// when the service has a catalog and no explicit fencer was provided. A
// repository-scoped OS lock can provide the cross-process boundary when no
// catalog fencer is available; otherwise signed writers fail closed.
func (s *Service) fencer() PublicationFencer {
	if s == nil {
		return nil
	}
	if s.PublicationFencer != nil {
		return s.PublicationFencer
	}
	if s.Store == nil {
		return nil
	}
	return NewPublicationFencer(s.Store, s.Now)
}

func (s *Service) locker() publicationLocker {
	if s == nil || s.Repo == nil {
		return nil
	}
	locker, _ := s.Repo.(publicationLocker)
	return locker
}

// publicationOwner is a stable per-process owner name derived from the
// process identifier and the configured publication domain.
func (s *Service) publicationOwner() string {
	return fmt.Sprintf("restoreweave:%d", os.Getpid())
}

// publicationFenceDomain scopes the runtime lease to the repository whose
// immutable records it protects. The signed publication domain remains the
// portable logical namespace; the repository identity is coordination-only.
func (s *Service) publicationFenceDomain() string {
	if s == nil {
		return ""
	}
	if driver, ok := s.Repo.(repository.RecordDriver); ok && strings.TrimSpace(driver.RepositoryIdentity()) != "" {
		return s.PublicationDomain + "|repository:" + DigestBytes([]byte(driver.RepositoryIdentity()))
	}
	return s.PublicationDomain
}

// newPublicationLeaseToken returns a fresh opaque lease token for one
// acquisition attempt.
func newPublicationLeaseToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate publication lease token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// publicationLease is the outcome of acquiring the cross-process fence. It
// carries the fencing token for signed records, the opaque lease token needed
// for validation and release, and the release callback.
type publicationLease struct {
	token             uint64 // authenticated publication-lineage token
	leaseToken        string
	coordinationToken int64 // optional SQLite/external lease token
	publicationLock   io.Closer
	operationCtx      context.Context
	stopKeepalive     func() error
	keepaliveError    func() error
	release           func() error
}

// acquirePublicationFence acquires the configured fence and repository lock,
// returning the coordination token and a release callback. The authenticated
// publication-lineage token is selected by the root publication from the
// committed chain; child closures inherit their committed parent's token.
func (s *Service) acquirePublicationFence(ctx context.Context) (publicationLease, error) {
	fencer := s.fencer()
	locker := s.locker()
	if s.signedPublicationEnabled() && fencer == nil && locker == nil {
		return publicationLease{}, errors.New("signed publication requires a repository publication lock or cross-process fencer")
	}
	leaseToken, err := newPublicationLeaseToken()
	if err != nil {
		return publicationLease{}, err
	}
	var publicationLock io.Closer
	if locker != nil {
		publicationLock, err = locker.AcquirePublicationLock(ctx, s.publicationFenceDomain())
		if err != nil {
			return publicationLease{}, err
		}
	}
	coordinationToken := int64(0)
	lineageToken := uint64(FallbackFenceToken)
	if fencer != nil {
		now := s.now()
		coordinationToken, err = fencer.Acquire(ctx, s.publicationFenceDomain(), s.publicationOwner(), leaseToken, now, now.Add(DefaultPublicationFenceTTL))
		if err != nil {
			if publicationLock != nil {
				_ = publicationLock.Close()
			}
			return publicationLease{}, err
		}
		lineageToken = uint64(coordinationToken)
	}
	operationCtx, cancel := context.WithCancel(ctx)
	keepaliveDone := make(chan struct{})
	var keepaliveMu sync.Mutex
	var keepaliveErr error
	stopKeepalive := func() error {
		cancel()
		<-keepaliveDone
		keepaliveMu.Lock()
		defer keepaliveMu.Unlock()
		return keepaliveErr
	}
	if fencer == nil {
		close(keepaliveDone)
	} else {
		go func() {
			ticker := time.NewTicker(publicationFenceRenewInterval)
			defer ticker.Stop()
			defer close(keepaliveDone)
			for {
				select {
				case <-operationCtx.Done():
					return
				case <-ticker.C:
					renewErr := s.renewPublicationFence(operationCtx, publicationLease{leaseToken: leaseToken, coordinationToken: coordinationToken})
					if renewErr != nil {
						if operationCtx.Err() != nil {
							return
						}
						keepaliveMu.Lock()
						keepaliveErr = fmt.Errorf("renew publication fence: %w", renewErr)
						keepaliveMu.Unlock()
						cancel()
						return
					}
				}
			}
		}()
	}
	return publicationLease{
		token: lineageToken, coordinationToken: coordinationToken, leaseToken: leaseToken, publicationLock: publicationLock,
		operationCtx: operationCtx, stopKeepalive: stopKeepalive, keepaliveError: func() error {
			keepaliveMu.Lock()
			defer keepaliveMu.Unlock()
			return keepaliveErr
		},
		release: func() error {
			keepaliveErr := stopKeepalive()
			var releaseErr error
			if fencer != nil {
				releaseErr = fencer.Release(context.WithoutCancel(ctx), s.publicationFenceDomain(), s.publicationOwner(), leaseToken, coordinationToken, s.now())
			}
			if publicationLock != nil {
				if lockErr := publicationLock.Close(); releaseErr == nil {
					releaseErr = lockErr
				}
			}
			return errors.Join(keepaliveErr, releaseErr)
		},
	}, nil
}

// renewPublicationFence extends the current lease without changing its
// fencing token. A changed token means another owner took over and is treated
// as a fencing failure rather than silently adopting the new authority.
func (s *Service) renewPublicationFence(ctx context.Context, lease publicationLease) error {
	fencer := s.fencer()
	if fencer == nil {
		return nil
	}
	now := s.now()
	renewed, err := fencer.Acquire(ctx, s.publicationFenceDomain(), s.publicationOwner(), lease.leaseToken, now, now.Add(DefaultPublicationFenceTTL))
	if err != nil {
		return err
	}
	if renewed != lease.coordinationToken {
		return fmt.Errorf("publication fence renewal returned token %d, want %d", renewed, lease.coordinationToken)
	}
	return nil
}

// context returns a cancellation-aware context for all signed placement work.
// A failed renewal cancels this context so repositories that honor context do
// not continue a placement after another owner has taken over the lease.
func (l publicationLease) context() context.Context {
	if l.operationCtx != nil {
		return l.operationCtx
	}
	return context.Background()
}

func (l publicationLease) renewalError() error {
	if l.keepaliveError == nil {
		return nil
	}
	return l.keepaliveError()
}

// validatePublicationFence checks the active lease immediately before a
// signed record is placed. It is a no-op when no fencer is configured.
func (s *Service) validatePublicationFence(ctx context.Context, lease publicationLease) error {
	if err := lease.renewalError(); err != nil {
		return err
	}
	fencer := s.fencer()
	if fencer == nil {
		return nil
	}
	return fencer.Validate(ctx, s.publicationFenceDomain(), s.publicationOwner(), lease.leaseToken, lease.coordinationToken, s.now())
}
