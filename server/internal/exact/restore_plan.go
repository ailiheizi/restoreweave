package exact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrRestorePlanStale   = errors.New("restore plan basis is stale")
	ErrInvalidRestorePlan = errors.New("invalid restore plan")
)

// RestorePlan binds a portable manifest and one destination precondition.
// InspectRestore never creates the destination. ApplyRestorePlan rechecks both
// digests before entering the existing catalog-free restore path.
type RestorePlan struct {
	SnapshotRef             string `json:"snapshot_ref"`
	ManifestDigest          string `json:"manifest_digest"`
	PublicationCommitDigest string `json:"publication_commit_digest,omitempty"`
	Destination             string `json:"destination,omitempty"`
	DestinationBasisDigest  string `json:"destination_basis_digest,omitempty"`
	Files                   int    `json:"files"`
	Bytes                   int64  `json:"bytes"`
}

func (s *Service) InspectRestore(ctx context.Context, snapshotRef, destination string) (RestorePlan, error) {
	var plan RestorePlan
	preflight, err := s.PreflightRestore(ctx, snapshotRef)
	if err != nil {
		return plan, err
	}
	manifest, err := s.loadManifest(ctx, snapshotRef)
	if err != nil {
		return plan, err
	}
	plan = RestorePlan{
		SnapshotRef: snapshotRef, ManifestDigest: manifest.ManifestDigest,
		Files: preflight.Files, Bytes: preflight.Bytes,
	}
	if s.signedPublicationEnabled() {
		publication, err := s.committedPublicationForSnapshot(ctx, snapshotRef)
		if err != nil {
			return RestorePlan{}, err
		}
		plan.PublicationCommitDigest = publication.CommitDigest
	}
	if strings.TrimSpace(destination) == "" {
		return plan, nil
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return RestorePlan{}, err
	}
	basis, err := restoreDestinationBasis(absolute)
	if err != nil {
		return RestorePlan{}, err
	}
	plan.Destination = absolute
	plan.DestinationBasisDigest = basis
	return plan, nil
}

func (s *Service) ApplyRestorePlan(ctx context.Context, plan RestorePlan) (RestoreResult, error) {
	if strings.TrimSpace(plan.SnapshotRef) == "" || strings.TrimSpace(plan.ManifestDigest) == "" ||
		strings.TrimSpace(plan.Destination) == "" || strings.TrimSpace(plan.DestinationBasisDigest) == "" {
		return RestoreResult{}, ErrInvalidRestorePlan
	}
	current, err := s.InspectRestore(ctx, plan.SnapshotRef, plan.Destination)
	if err != nil {
		return RestoreResult{}, err
	}
	if current.ManifestDigest != plan.ManifestDigest {
		return RestoreResult{}, fmt.Errorf("%w: manifest digest changed", ErrRestorePlanStale)
	}
	if current.PublicationCommitDigest != plan.PublicationCommitDigest {
		return RestoreResult{}, fmt.Errorf("%w: publication commit changed", ErrRestorePlanStale)
	}
	if current.Destination != plan.Destination || current.DestinationBasisDigest != plan.DestinationBasisDigest {
		return RestoreResult{}, fmt.Errorf("%w: destination precondition changed", ErrRestorePlanStale)
	}
	return s.Restore(ctx, plan.SnapshotRef, plan.Destination)
}

func restoreDestinationBasis(destination string) (string, error) {
	type basis struct {
		Schema    string `json:"schema"`
		Path      string `json:"path"`
		State     string `json:"state"`
		Mode      uint32 `json:"mode,omitempty"`
		ModTimeNS int64  `json:"mod_time_ns,omitempty"`
	}
	value := basis{Schema: "org.restoreweave.restore-destination-basis.v1", Path: destination}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		value.State = "ABSENT"
	} else if err != nil {
		return "", err
	} else {
		if !info.IsDir() {
			return "", fmt.Errorf("%w: restore destination exists and is not a directory", ErrBlocked)
		}
		entries, readErr := os.ReadDir(destination)
		if readErr != nil {
			return "", readErr
		}
		if len(entries) != 0 {
			return "", fmt.Errorf("%w: restore destination is not empty", ErrBlocked)
		}
		value.State = "EMPTY_DIRECTORY"
		value.Mode = uint32(info.Mode())
		value.ModTimeNS = info.ModTime().UnixNano()
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
