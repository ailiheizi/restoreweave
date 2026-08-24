package exact

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

func TestReachabilityEvidenceSignsAndRecomputesNonDestructivePlan(t *testing.T) {
	identity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := NewTrustAnchor(identity, "workspace:phase5")
	if err != nil {
		t.Fatal(err)
	}
	inventory := []repository.PlacementRef{
		{Kind: "blob", Digest: "sha256:a"},
		{Kind: "blob", Digest: "sha256:b"},
		{Kind: "record:PUBLICATION_COMMIT", Digest: "sha256:c"},
		{Kind: "blob", Digest: "sha256:orphan"},
	}
	roots := []repository.ReachabilityRoot{{
		Kind: repository.RootCommittedSnapshot, ID: "snapshot:phase5", Verified: true, Complete: true,
		Placements: []repository.PlacementRef{{Kind: "blob", Digest: "sha256:a"}, {Kind: "record:PUBLICATION_COMMIT", Digest: "sha256:c"}},
	}}
	leases := []repository.ReachabilityLease{{
		ID: "lease:restore", Verified: true, Active: true,
		Placements: []repository.PlacementRef{{Kind: "blob", Digest: "sha256:b"}},
	}}
	evidence, err := BuildReachabilityEvidence(identity, anchor, anchor.PublicationDomain, "repo:phase5", inventory, roots, leases, time.Unix(123, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.Verify(anchor); err != nil {
		t.Fatal(err)
	}
	if evidence.Plan.CandidateCollection != "NON_DESTRUCTIVE_ONLY" || len(evidence.Plan.Candidates) != 1 || evidence.Plan.Candidates[0].Digest != "sha256:orphan" {
		t.Fatalf("unexpected signed plan: %+v", evidence.Plan)
	}
	if len(evidence.Plan.ProtectedByLease) != 1 || evidence.Plan.ProtectedByLease[0].Digest != "sha256:b" {
		t.Fatalf("unexpected lease protection: %+v", evidence.Plan.ProtectedByLease)
	}
	if _, err := evidence.Digest(); err != nil {
		t.Fatal(err)
	}
	payload, err := MarshalReachabilityEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeReachabilityEvidence(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoded.Verify(anchor); err != nil {
		t.Fatal(err)
	}
}

func TestReachabilityEvidenceRejectsTamperingAndWrongAnchor(t *testing.T) {
	identity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := NewTrustAnchor(identity, "workspace:phase5")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildReachabilityEvidence(identity, anchor, anchor.PublicationDomain, "repo:phase5",
		[]repository.PlacementRef{{Kind: "blob", Digest: "sha256:a"}},
		[]repository.ReachabilityRoot{{Kind: repository.RootRetentionPin, ID: "pin:1", Verified: true, Complete: true, Placements: []repository.PlacementRef{{Kind: "blob", Digest: "sha256:a"}}}}, nil, time.Unix(456, 0))
	if err != nil {
		t.Fatal(err)
	}
	mutated := evidence
	mutated.Inventory = append([]repository.PlacementRef(nil), evidence.Inventory...)
	mutated.Inventory[0].Digest = "sha256:tampered"
	if err := mutated.Verify(anchor); !errors.Is(err, ErrRecoverySignature) && !errors.Is(err, ErrRecoveryRecordInvalid) {
		t.Fatalf("tampered inventory error = %v, want signature or record failure", err)
	}
	otherIdentity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	otherAnchor, err := NewTrustAnchor(otherIdentity, anchor.PublicationDomain)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.Verify(otherAnchor); !errors.Is(err, ErrRecoveryTrustAnchor) {
		t.Fatalf("wrong anchor error = %v, want trust-anchor failure", err)
	}
}

func TestReachabilityEvidenceRejectsIncompleteOrUnknownInputs(t *testing.T) {
	identity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := NewTrustAnchor(identity, "workspace:phase5")
	if err != nil {
		t.Fatal(err)
	}
	inventory := []repository.PlacementRef{{Kind: "blob", Digest: "sha256:a"}}
	tests := []struct {
		name   string
		roots  []repository.ReachabilityRoot
		leases []repository.ReachabilityLease
	}{
		{
			name:  "unverified root",
			roots: []repository.ReachabilityRoot{{Kind: repository.RootCommittedSnapshot, ID: "snapshot:1", Complete: true, Placements: inventory}},
		},
		{
			name:  "unknown placement",
			roots: []repository.ReachabilityRoot{{Kind: repository.RootCommittedSnapshot, ID: "snapshot:1", Verified: true, Complete: true, Placements: []repository.PlacementRef{{Kind: "blob", Digest: "sha256:missing"}}}},
		},
		{
			name:   "unverified lease",
			leases: []repository.ReachabilityLease{{ID: "lease:1", Active: true, Placements: inventory}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BuildReachabilityEvidence(identity, anchor, anchor.PublicationDomain, "repo:phase5", inventory, test.roots, test.leases, time.Unix(789, 0)); err == nil {
				t.Fatal("invalid reachability input was accepted")
			}
		})
	}
}

func TestReachabilityEvidenceDoesNotDeleteOrMutateRepository(t *testing.T) {
	repo, err := repository.OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	retained, err := repo.Place(ctx, bytes.NewReader([]byte("retained")))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := repo.Place(ctx, bytes.NewReader([]byte("orphan")))
	if err != nil {
		t.Fatal(err)
	}
	before, err := repo.ListPlacementRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := NewTrustAnchor(identity, "workspace:phase5")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildReachabilityEvidence(identity, anchor, anchor.PublicationDomain, repo.RepositoryIdentity(), before,
		[]repository.ReachabilityRoot{{Kind: repository.RootCommittedSnapshot, ID: "snapshot:1", Verified: true, Complete: true, Placements: []repository.PlacementRef{{Kind: "blob", Digest: retained.ContentID}}}}, nil, time.Unix(999, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.Verify(anchor); err != nil {
		t.Fatal(err)
	}
	after, err := repo.ListPlacementRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || !containsPlacement(after, orphan.ContentID) || !containsPlacement(after, retained.ContentID) {
		t.Fatalf("reachability evidence mutated repository: before=%+v after=%+v", before, after)
	}
}

func TestReachabilityEvidenceDecodeRejectsUnknownOrMultipleValues(t *testing.T) {
	identity, err := NewSigningIdentity()
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := NewTrustAnchor(identity, "workspace:phase5")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildReachabilityEvidence(identity, anchor, anchor.PublicationDomain, "repo:phase5",
		[]repository.PlacementRef{{Kind: "blob", Digest: "sha256:a"}},
		[]repository.ReachabilityRoot{{Kind: repository.RootRetentionPin, ID: "pin:1", Verified: true, Complete: true, Placements: []repository.PlacementRef{{Kind: "blob", Digest: "sha256:a"}}}}, nil, time.Unix(1000, 0))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := MarshalReachabilityEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	unknown := append(append([]byte(nil), payload[:len(payload)-1]...), []byte(`,"unknown":true}`)...)
	if _, err := DecodeReachabilityEvidence(unknown); err == nil {
		t.Fatal("unknown reachability field was accepted")
	}
	multiple := append(append([]byte(nil), payload...), payload...)
	if _, err := DecodeReachabilityEvidence(multiple); err == nil {
		t.Fatal("multiple reachability JSON values were accepted")
	}
}

func containsPlacement(refs []repository.PlacementRef, digest string) bool {
	for _, ref := range refs {
		if ref.Digest == digest {
			return true
		}
	}
	return false
}
