package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PublicationFence is the rebuildable coordinator state for one publication
// domain. The signed publication records remain the recovery authority.
type PublicationFence struct {
	PublicationDomain string
	Owner             string
	LeaseToken        string
	FencingToken      int64
	LeaseUntil        time.Time
	UpdatedAt         time.Time
}

// AcquirePublicationFence claims a domain for owner and leaseToken. Repeating
// an active request with the same owner and token renews it without advancing
// the fencing token. A takeover after expiry must use a new lease token and
// receives a strictly larger fencing token.
func (tx *Tx) AcquirePublicationFence(
	ctx context.Context,
	publicationDomain, owner, leaseToken string,
	now, until time.Time,
) (PublicationFence, error) {
	var fence PublicationFence
	if err := requireText("publication domain", publicationDomain); err != nil {
		return fence, err
	}
	if err := requireText("fence owner", owner); err != nil {
		return fence, err
	}
	if err := requireText("fence lease token", leaseToken); err != nil {
		return fence, err
	}
	now = recordTime(now, tx.now)
	until = until.UTC()
	if !until.After(now) {
		return fence, errors.New("publication fence lease must expire after its acquisition time")
	}

	var currentOwner, currentToken string
	var currentFence, currentUntil, currentUpdated int64
	err := tx.tx.QueryRowContext(ctx, `
SELECT owner, lease_token, fencing_token, lease_until_ns, updated_at_ns
FROM publication_fences WHERE publication_domain = ?`, publicationDomain).Scan(
		&currentOwner, &currentToken, &currentFence, &currentUntil, &currentUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO publication_fences(
    publication_domain, owner, lease_token, fencing_token,
    lease_until_ns, updated_at_ns
) VALUES (?, ?, ?, 1, ?, ?)`, publicationDomain, owner, leaseToken,
			until.UnixNano(), now.UnixNano()); err != nil {
			return fence, fmt.Errorf("insert publication fence: %w", err)
		}
		return PublicationFence{
			PublicationDomain: publicationDomain,
			Owner:             owner,
			LeaseToken:        leaseToken,
			FencingToken:      1,
			LeaseUntil:        until,
			UpdatedAt:         now,
		}, nil
	}
	if err != nil {
		return fence, fmt.Errorf("read publication fence: %w", err)
	}

	if currentOwner == owner && currentToken == leaseToken && currentUntil > now.UnixNano() {
		if _, err := tx.tx.ExecContext(ctx, `
UPDATE publication_fences
SET lease_until_ns = ?, updated_at_ns = ?
WHERE publication_domain = ? AND owner = ? AND lease_token = ?
  AND fencing_token = ? AND lease_until_ns > ?`,
			until.UnixNano(), now.UnixNano(), publicationDomain, owner, leaseToken,
			currentFence, now.UnixNano()); err != nil {
			return fence, fmt.Errorf("renew publication fence: %w", err)
		}
		return PublicationFence{
			PublicationDomain: publicationDomain,
			Owner:             owner,
			LeaseToken:        leaseToken,
			FencingToken:      currentFence,
			LeaseUntil:        until,
			UpdatedAt:         now,
		}, nil
	}

	// An expired row is retained so the next token is monotonic across
	// restarts. Reusing an expired token is rejected: an old worker must obtain
	// a fresh opaque lease token before it can publish again.
	if currentUntil > now.UnixNano() {
		return fence, ErrConflict
	}
	if currentOwner == owner && currentToken == leaseToken {
		return fence, ErrConflict
	}
	result, err := tx.tx.ExecContext(ctx, `
UPDATE publication_fences
SET owner = ?, lease_token = ?, fencing_token = fencing_token + 1,
    lease_until_ns = ?, updated_at_ns = ?
WHERE publication_domain = ? AND lease_until_ns <= ?`,
		owner, leaseToken, until.UnixNano(), now.UnixNano(), publicationDomain, now.UnixNano())
	if err != nil {
		return fence, fmt.Errorf("take over publication fence: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fence, fmt.Errorf("check publication fence takeover: %w", err)
	}
	if changed != 1 {
		return fence, ErrConflict
	}
	return PublicationFence{
		PublicationDomain: publicationDomain,
		Owner:             owner,
		LeaseToken:        leaseToken,
		FencingToken:      currentFence + 1,
		LeaseUntil:        until,
		UpdatedAt:         now,
	}, nil
}

// ValidatePublicationFence verifies that the exact active owner, lease token,
// and fencing token still hold the domain at now.
func (tx *Tx) ValidatePublicationFence(
	ctx context.Context,
	publicationDomain, owner, leaseToken string,
	fencingToken int64, now time.Time,
) error {
	if err := requireText("publication domain", publicationDomain); err != nil {
		return err
	}
	if err := requireText("fence owner", owner); err != nil {
		return err
	}
	if err := requireText("fence lease token", leaseToken); err != nil {
		return err
	}
	if fencingToken < 1 {
		return errors.New("publication fence token must be positive")
	}
	now = recordTime(now, tx.now)
	var marker int
	err := tx.tx.QueryRowContext(ctx, `
SELECT 1 FROM publication_fences
WHERE publication_domain = ? AND owner = ? AND lease_token = ?
  AND fencing_token = ? AND lease_until_ns > ?`,
		publicationDomain, owner, leaseToken, fencingToken, now.UnixNano()).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("validate publication fence: %w", err)
	}
	return nil
}

// ReleasePublicationFence expires the exact active lease. The row is retained
// to preserve the fencing counter and reject stale releases.
func (tx *Tx) ReleasePublicationFence(
	ctx context.Context,
	publicationDomain, owner, leaseToken string,
	fencingToken int64, now time.Time,
) error {
	if err := requireText("publication domain", publicationDomain); err != nil {
		return err
	}
	if err := requireText("fence owner", owner); err != nil {
		return err
	}
	if err := requireText("fence lease token", leaseToken); err != nil {
		return err
	}
	if fencingToken < 1 {
		return errors.New("publication fence token must be positive")
	}
	now = recordTime(now, tx.now)
	result, err := tx.tx.ExecContext(ctx, `
UPDATE publication_fences
SET lease_until_ns = ?, updated_at_ns = ?
WHERE publication_domain = ? AND owner = ? AND lease_token = ?
  AND fencing_token = ? AND lease_until_ns > ?`,
		now.UnixNano(), now.UnixNano(), publicationDomain, owner, leaseToken,
		fencingToken, now.UnixNano())
	if err != nil {
		return fmt.Errorf("release publication fence: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check publication fence release: %w", err)
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}
