package exact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

// ReachabilityEvidenceSchemaV1 is a signed, non-destructive root-accounting
// envelope. It is deliberately separate from portable recovery records and
// never authorizes a repository mutation.
const ReachabilityEvidenceSchemaV1 = "org.restoreweave.reachability-evidence.v1"

const ReachabilityEvidenceKind = "REACHABILITY_EVIDENCE"

const reachabilityEvidenceReadLimit = int64(16 << 20)

// SignedReachabilityEvidence binds the complete inventory and root inputs to
// the deterministic candidate plan. A reader recomputes the plan before
// accepting the evidence, so a signed but stale or incomplete projection is
// not sufficient. Placement identities are opaque and never paths.
type SignedReachabilityEvidence struct {
	Schema            string                         `json:"schema"`
	SignatureDomain   string                         `json:"signature_domain"`
	RecordKind        string                         `json:"record_kind"`
	PublicationDomain string                         `json:"publication_domain"`
	RepositoryID      string                         `json:"repository_id"`
	TrustAnchorDigest string                         `json:"trust_anchor_digest"`
	Inventory         []repository.PlacementRef      `json:"inventory"`
	Roots             []repository.ReachabilityRoot  `json:"roots"`
	Leases            []repository.ReachabilityLease `json:"leases"`
	Plan              repository.ReachabilityPlan    `json:"plan"`
	WriterIdentity    string                         `json:"writer_identity"`
	KeyID             string                         `json:"key_id"`
	SignedAt          time.Time                      `json:"signed_at"`
	Signature         []byte                         `json:"signature,omitempty"`
}

// BuildReachabilityEvidence computes and signs one complete root-accounting
// observation. It performs no placement or deletion and may be used by a
// qualification harness or a future host-owned collector.
func BuildReachabilityEvidence(identity SigningIdentity, anchor TrustAnchor, publicationDomain, repositoryID string, inventory []repository.PlacementRef, roots []repository.ReachabilityRoot, leases []repository.ReachabilityLease, signedAt time.Time) (SignedReachabilityEvidence, error) {
	if err := anchor.validate(); err != nil {
		return SignedReachabilityEvidence{}, err
	}
	if publicationDomain != anchor.PublicationDomain {
		return SignedReachabilityEvidence{}, fmt.Errorf("%w: publication domain differs from trust anchor", ErrRecoveryTrustAnchor)
	}
	inventoryDigest, err := repository.PlacementInventoryDigest(inventory)
	if err != nil {
		return SignedReachabilityEvidence{}, err
	}
	boundRoots := make([]repository.ReachabilityRoot, len(roots))
	for i, root := range roots {
		boundRoots[i], err = repository.BindReachabilityRoot(repositoryID, inventoryDigest, root)
		if err != nil {
			return SignedReachabilityEvidence{}, err
		}
	}
	boundLeases := make([]repository.ReachabilityLease, len(leases))
	for i, lease := range leases {
		boundLeases[i], err = repository.BindReachabilityLease(repositoryID, inventoryDigest, lease)
		if err != nil {
			return SignedReachabilityEvidence{}, err
		}
	}
	plan, err := repository.PlanReachability(repositoryID, inventory, boundRoots, boundLeases)
	if err != nil {
		return SignedReachabilityEvidence{}, err
	}
	anchorDigest, err := DigestCanonicalJSON(anchor)
	if err != nil {
		return SignedReachabilityEvidence{}, err
	}
	evidence := SignedReachabilityEvidence{
		Schema: ReachabilityEvidenceSchemaV1, SignatureDomain: RecoverySignatureDomainV1,
		RecordKind: ReachabilityEvidenceKind, PublicationDomain: publicationDomain,
		RepositoryID: repositoryID, TrustAnchorDigest: anchorDigest,
		Inventory: clonePlacementRefs(inventory), Roots: cloneReachabilityRoots(boundRoots),
		Leases: cloneReachabilityLeases(boundLeases), Plan: plan,
		WriterIdentity: identity.WriterIdentity, KeyID: identity.KeyID, SignedAt: signedAt.UTC(),
	}
	return SignReachabilityEvidence(identity, evidence)
}

// SignReachabilityEvidence signs the canonical unsigned envelope. The caller
// must have supplied the complete inventory, roots, leases, and derived plan;
// VerifyReachabilityEvidence recomputes and cross-checks all of them.
func SignReachabilityEvidence(identity SigningIdentity, evidence SignedReachabilityEvidence) (SignedReachabilityEvidence, error) {
	evidence.SignedAt = evidence.SignedAt.UTC()
	evidence.Signature = nil
	if err := evidence.validate(); err != nil {
		return SignedReachabilityEvidence{}, err
	}
	if evidence.WriterIdentity != identity.WriterIdentity || evidence.KeyID != identity.KeyID {
		return SignedReachabilityEvidence{}, fmt.Errorf("%w: record signer differs from signing identity", ErrRecoveryRecordInvalid)
	}
	payload, err := evidence.unsignedCanonical()
	if err != nil {
		return SignedReachabilityEvidence{}, err
	}
	evidence.Signature, err = signRecord(identity, evidence.Schema, evidence.SignatureDomain, payload)
	if err != nil {
		return SignedReachabilityEvidence{}, err
	}
	return evidence, nil
}

// Verify authenticates the envelope and then recomputes its root-accounting
// plan. The candidate collection marker is required to remain advisory.
func (evidence SignedReachabilityEvidence) Verify(anchor TrustAnchor) error {
	evidence.SignedAt = evidence.SignedAt.UTC()
	if err := evidence.validate(); err != nil {
		return err
	}
	anchorDigest, err := DigestCanonicalJSON(anchor)
	if err != nil {
		return err
	}
	if anchorDigest != evidence.TrustAnchorDigest {
		return fmt.Errorf("%w: reachability trust-anchor digest differs", ErrRecoveryTrustAnchor)
	}
	payload, err := evidence.unsignedCanonical()
	if err != nil {
		return err
	}
	if err := verifyRecord(anchor, evidence.Schema, evidence.SignatureDomain, payload, evidence.Signature, evidence.WriterIdentity, evidence.KeyID, evidence.PublicationDomain); err != nil {
		return err
	}
	want, err := repository.PlanReachability(evidence.RepositoryID, evidence.Inventory, evidence.Roots, evidence.Leases)
	if err != nil {
		return fmt.Errorf("%w: recompute reachability plan: %v", ErrRecoveryRecordInvalid, err)
	}
	if err := compareReachabilityPlans(evidence.Plan, want); err != nil {
		return err
	}
	return nil
}

func (evidence SignedReachabilityEvidence) Digest() (string, error) {
	if err := evidence.validate(); err != nil {
		return "", err
	}
	payload, err := canonicalJSON(evidence)
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// MarshalReachabilityEvidence emits one canonical bounded JSON envelope. It
// does not verify the trust anchor because callers may serialize before the
// independently retained anchor is supplied to a reader.
func MarshalReachabilityEvidence(evidence SignedReachabilityEvidence) ([]byte, error) {
	if err := evidence.validate(); err != nil {
		return nil, err
	}
	return canonicalJSON(evidence)
}

// DecodeReachabilityEvidence parses exactly one bounded envelope and rejects
// unknown top-level fields. Signature and plan authenticity are checked by
// Verify after the caller supplies the independent trust anchor.
func DecodeReachabilityEvidence(payload []byte) (SignedReachabilityEvidence, error) {
	var evidence SignedReachabilityEvidence
	if len(payload) == 0 || int64(len(payload)) > reachabilityEvidenceReadLimit {
		return evidence, fmt.Errorf("reachability evidence exceeds %d bytes", reachabilityEvidenceReadLimit)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return evidence, fmt.Errorf("decode reachability evidence: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return evidence, errors.New("reachability evidence contains more than one JSON value")
		}
		return evidence, err
	}
	if err := evidence.validate(); err != nil {
		return evidence, err
	}
	return evidence, nil
}

func (evidence SignedReachabilityEvidence) unsignedCanonical() ([]byte, error) {
	evidence.Signature = nil
	evidence.SignedAt = evidence.SignedAt.UTC()
	return canonicalJSON(evidence)
}

func (evidence SignedReachabilityEvidence) validate() error {
	if evidence.Schema != ReachabilityEvidenceSchemaV1 || evidence.RecordKind != ReachabilityEvidenceKind || evidence.SignatureDomain != RecoverySignatureDomainV1 {
		return fmt.Errorf("%w: reachability evidence schema, kind, or signature domain is invalid", ErrRecoveryRecordInvalid)
	}
	for name, value := range map[string]string{
		"publication domain":  evidence.PublicationDomain,
		"repository ID":       evidence.RepositoryID,
		"trust anchor digest": evidence.TrustAnchorDigest,
		"writer identity":     evidence.WriterIdentity,
		"key ID":              evidence.KeyID,
	} {
		if strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
			return fmt.Errorf("%w: reachability evidence %s is required", ErrRecoveryRecordInvalid, name)
		}
	}
	if !validExactContentID(evidence.TrustAnchorDigest) || evidence.SignedAt.IsZero() {
		return fmt.Errorf("%w: reachability trust anchor digest or signed time is invalid", ErrRecoveryRecordInvalid)
	}
	if len(evidence.Inventory) == 0 || (len(evidence.Roots) == 0 && len(evidence.Leases) == 0) {
		return errors.New("reachability evidence requires inventory and at least one root or lease")
	}
	if evidence.Plan.Schema != repository.ReachabilityPlanSchemaV1 || evidence.Plan.RepositoryID != evidence.RepositoryID || evidence.Plan.CandidateCollection != "NON_DESTRUCTIVE_ONLY" {
		return fmt.Errorf("%w: reachability evidence plan is not a non-destructive v1 plan", ErrRecoveryRecordInvalid)
	}
	return nil
}

func compareReachabilityPlans(got, want repository.ReachabilityPlan) error {
	gotBytes, err := canonicalJSON(got)
	if err != nil {
		return err
	}
	wantBytes, err := canonicalJSON(want)
	if err != nil {
		return err
	}
	if string(gotBytes) != string(wantBytes) {
		return fmt.Errorf("%w: reachability plan does not match its signed inventory and roots", ErrRecoveryRecordInvalid)
	}
	return nil
}

func clonePlacementRefs(refs []repository.PlacementRef) []repository.PlacementRef {
	return append([]repository.PlacementRef(nil), refs...)
}

func cloneReachabilityRoots(roots []repository.ReachabilityRoot) []repository.ReachabilityRoot {
	cloned := make([]repository.ReachabilityRoot, len(roots))
	for i, root := range roots {
		cloned[i] = root
		cloned[i].Placements = clonePlacementRefs(root.Placements)
	}
	return cloned
}

func cloneReachabilityLeases(leases []repository.ReachabilityLease) []repository.ReachabilityLease {
	cloned := make([]repository.ReachabilityLease, len(leases))
	for i, lease := range leases {
		cloned[i] = lease
		cloned[i].Placements = clonePlacementRefs(lease.Placements)
	}
	return cloned
}
