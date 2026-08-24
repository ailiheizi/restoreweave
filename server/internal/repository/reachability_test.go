package repository

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestPlanReachabilityKeepsExplicitRootsAndActiveLeases(t *testing.T) {
	inventory := []PlacementRef{
		{Kind: "blob", Digest: "sha256:a"},
		{Kind: "blob", Digest: "sha256:b"},
		{Kind: "record", Digest: "sha256:c"},
		{Kind: "blob", Digest: "sha256:orphan"},
	}
	inventoryDigest := placementDigest(mustNormalizedInventory(t, inventory))
	root, err := BindReachabilityRoot("repo:test", inventoryDigest, ReachabilityRoot{
		Kind: RootCommittedSnapshot, ID: "snapshot:1", Verified: true, Complete: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: "sha256:a"}, {Kind: "record", Digest: "sha256:c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := BindReachabilityLease("repo:test", inventoryDigest, ReachabilityLease{
		ID: "lease:restore", Verified: true, Active: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: "sha256:b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanReachability("repo:test", inventory, []ReachabilityRoot{root}, []ReachabilityLease{lease})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Schema != ReachabilityPlanSchemaV1 || plan.CandidateCollection != "NON_DESTRUCTIVE_ONLY" {
		t.Fatalf("plan identity = %+v", plan)
	}
	if len(plan.Reachable) != 3 || len(plan.Candidates) != 1 || plan.Candidates[0].Digest != "sha256:orphan" {
		t.Fatalf("reachability plan = %+v", plan)
	}
	if len(plan.ProtectedByLease) != 1 || plan.ProtectedByLease[0].Digest != "sha256:b" {
		t.Fatalf("lease protection = %+v", plan.ProtectedByLease)
	}
}

func TestPlanReachabilityRejectsIncompleteOrUnknownEvidence(t *testing.T) {
	inventory := []PlacementRef{{Kind: "blob", Digest: "sha256:a"}}
	inventoryDigest := placementDigest(mustNormalizedInventory(t, inventory))
	validRoot, err := BindReachabilityRoot("repo:test", inventoryDigest, ReachabilityRoot{
		Kind: RootCommittedSnapshot, ID: "snapshot:1", Verified: true, Complete: true,
		Placements: inventory,
	})
	if err != nil {
		t.Fatal(err)
	}
	validLease, err := BindReachabilityLease("repo:test", inventoryDigest, ReachabilityLease{
		ID: "lease:1", Verified: true, Active: true,
		Placements: inventory,
	})
	if err != nil {
		t.Fatal(err)
	}
	unknownRoot, err := BindReachabilityRoot("repo:test", inventoryDigest, ReachabilityRoot{
		Kind: RootCommittedSnapshot, ID: "snapshot:unknown", Verified: true, Complete: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: "sha256:unknown"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	unknownLease, err := BindReachabilityLease("repo:test", inventoryDigest, ReachabilityLease{
		ID: "lease:unknown", Verified: true, Active: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: "sha256:unknown"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		roots []ReachabilityRoot
		lease []ReachabilityLease
	}{
		{name: "unverified root", roots: []ReachabilityRoot{{RepositoryID: validRoot.RepositoryID, InventoryDigest: validRoot.InventoryDigest, EvidenceDigest: validRoot.EvidenceDigest, Kind: validRoot.Kind, ID: validRoot.ID, Complete: true, Placements: validRoot.Placements}}},
		{name: "incomplete root", roots: []ReachabilityRoot{{RepositoryID: validRoot.RepositoryID, InventoryDigest: validRoot.InventoryDigest, EvidenceDigest: validRoot.EvidenceDigest, Kind: validRoot.Kind, ID: validRoot.ID, Verified: true, Placements: validRoot.Placements}}},
		{name: "unknown root placement", roots: []ReachabilityRoot{unknownRoot}},
		{name: "unverified lease", lease: []ReachabilityLease{{RepositoryID: validLease.RepositoryID, InventoryDigest: validLease.InventoryDigest, EvidenceDigest: validLease.EvidenceDigest, ID: validLease.ID, Active: true, Placements: validLease.Placements}}},
		{name: "unknown lease placement", lease: []ReachabilityLease{unknownLease}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := PlanReachability("repo:test", inventory, test.roots, test.lease); err == nil {
				t.Fatal("incomplete or unknown evidence was accepted")
			}
		})
	}
}

func TestPlanReachabilityIsDeterministicAndRejectsDuplicateInventory(t *testing.T) {
	firstInventory := []PlacementRef{
		{Kind: "blob", Digest: "sha256:b"}, {Kind: "blob", Digest: "sha256:a"},
	}
	firstDigest := placementDigest(mustNormalizedInventory(t, firstInventory))
	firstRoot, err := BindReachabilityRoot("repo:test", firstDigest, ReachabilityRoot{Kind: RootRetentionPin, ID: "pin:1", Verified: true, Complete: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: "sha256:a"}}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := PlanReachability("repo:test", firstInventory, []ReachabilityRoot{firstRoot}, nil)
	if err != nil {
		t.Fatal(err)
	}
	secondInventory := []PlacementRef{
		{Kind: "blob", Digest: "sha256:a"}, {Kind: "blob", Digest: "sha256:b"},
	}
	secondDigest := placementDigest(mustNormalizedInventory(t, secondInventory))
	secondRoot, err := BindReachabilityRoot("repo:test", secondDigest, ReachabilityRoot{Kind: RootRetentionPin, ID: "pin:1", Verified: true, Complete: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: "sha256:a"}}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanReachability("repo:test", secondInventory, []ReachabilityRoot{secondRoot}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.InventoryDigest != second.InventoryDigest || first.RootsDigest != second.RootsDigest {
		t.Fatalf("non-deterministic digests: first=%+v second=%+v", first, second)
	}
	if _, err := PlanReachability("repo:test", []PlacementRef{{Kind: "blob", Digest: "sha256:a"}, {Kind: "blob", Digest: "sha256:a"}}, nil, nil); err == nil {
		t.Fatal("duplicate inventory was accepted")
	}
}

func TestPlanReachabilityRejectsUnboundOrTamperedEvidence(t *testing.T) {
	inventory := []PlacementRef{{Kind: "blob", Digest: "sha256:a"}, {Kind: "blob", Digest: "sha256:b"}}
	inventoryDigest := placementDigest(mustNormalizedInventory(t, inventory))
	root, err := BindReachabilityRoot("repo:test", inventoryDigest, ReachabilityRoot{
		Kind: RootCommittedSnapshot, ID: "snapshot:1", Verified: true, Complete: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: "sha256:a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := BindReachabilityLease("repo:test", inventoryDigest, ReachabilityLease{
		ID: "lease:1", Verified: true, Active: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: "sha256:b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	validPlan, err := PlanReachability("repo:test", inventory, []ReachabilityRoot{root}, []ReachabilityLease{lease})
	if err != nil || len(validPlan.Reachable) != 2 {
		t.Fatalf("bound evidence plan = %+v, err=%v", validPlan, err)
	}

	tests := []struct {
		name string
		plan func() error
	}{
		{"unbound root", func() error {
			copy := root
			copy.RepositoryID = ""
			copy.InventoryDigest = ""
			copy.EvidenceDigest = ""
			_, err := PlanReachability("repo:test", inventory, []ReachabilityRoot{copy}, nil)
			return err
		}},
		{"unbound lease", func() error {
			copy := lease
			copy.RepositoryID = ""
			copy.InventoryDigest = ""
			copy.EvidenceDigest = ""
			_, err := PlanReachability("repo:test", inventory, nil, []ReachabilityLease{copy})
			return err
		}},
		{"root repository mismatch", func() error {
			copy := root
			copy.RepositoryID = "repo:other"
			_, err := PlanReachability("repo:test", inventory, []ReachabilityRoot{copy}, nil)
			return err
		}},
		{"lease repository mismatch", func() error {
			copy := lease
			copy.RepositoryID = "repo:other"
			_, err := PlanReachability("repo:test", inventory, nil, []ReachabilityLease{copy})
			return err
		}},
		{"root inventory mismatch", func() error {
			copy := root
			copy.InventoryDigest = "sha256:" + strings.Repeat("0", 64)
			_, err := PlanReachability("repo:test", inventory, []ReachabilityRoot{copy}, nil)
			return err
		}},
		{"tampered root placement", func() error {
			copy := root
			copy.Placements = []PlacementRef{{Kind: "blob", Digest: "sha256:b"}}
			_, err := PlanReachability("repo:test", inventory, []ReachabilityRoot{copy}, nil)
			return err
		}},
		{"tampered root evidence digest", func() error {
			copy := root
			copy.EvidenceDigest = "sha256:" + strings.Repeat("f", 64)
			_, err := PlanReachability("repo:test", inventory, []ReachabilityRoot{copy}, nil)
			return err
		}},
		{"lease inventory mismatch", func() error {
			copy := lease
			copy.InventoryDigest = "sha256:" + strings.Repeat("0", 64)
			_, err := PlanReachability("repo:test", inventory, nil, []ReachabilityLease{copy})
			return err
		}},
		{"tampered lease placement", func() error {
			copy := lease
			copy.Placements = []PlacementRef{{Kind: "blob", Digest: "sha256:a"}}
			_, err := PlanReachability("repo:test", inventory, nil, []ReachabilityLease{copy})
			return err
		}},
		{"tampered lease evidence digest", func() error {
			copy := lease
			copy.EvidenceDigest = "sha256:" + strings.Repeat("f", 64)
			_, err := PlanReachability("repo:test", inventory, nil, []ReachabilityLease{copy})
			return err
		}},
		{"inventory mutation after binding", func() error {
			mutated := append([]PlacementRef(nil), inventory...)
			mutated[1].Digest = "sha256:changed"
			_, err := PlanReachability("repo:test", mutated, []ReachabilityRoot{root}, []ReachabilityLease{lease})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.plan(); err == nil {
				t.Fatal("tampered or mismatched evidence was accepted")
			}
		})
	}
}

func TestReachabilityEvidenceDigestIsOrderIndependent(t *testing.T) {
	base := ReachabilityRoot{
		RepositoryID: "repo:test", InventoryDigest: "sha256:" + strings.Repeat("a", 64),
		Kind: RootRetentionPin, ID: "pin:1", Verified: true, Complete: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: "sha256:b"}, {Kind: "blob", Digest: "sha256:a"}},
	}
	reordered := base
	reordered.Placements = []PlacementRef{{Kind: "blob", Digest: "sha256:a"}, {Kind: "blob", Digest: "sha256:b"}}
	first, err := ReachabilityRootDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReachabilityRootDigest(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("placement ordering changed evidence digest: %q != %q", first, second)
	}
}

func TestPlanReachabilityRootAndLeaseOrderingIsDeterministic(t *testing.T) {
	inventory := []PlacementRef{{Kind: "blob", Digest: "sha256:a"}, {Kind: "blob", Digest: "sha256:b"}}
	inventoryDigest := placementDigest(mustNormalizedInventory(t, inventory))
	rootA, err := BindReachabilityRoot("repo:test", inventoryDigest, ReachabilityRoot{
		Kind: RootRetentionPin, ID: "pin:a", Verified: true, Complete: true,
		Placements: []PlacementRef{inventory[0]},
	})
	if err != nil {
		t.Fatal(err)
	}
	rootB, err := BindReachabilityRoot("repo:test", inventoryDigest, ReachabilityRoot{
		Kind: RootRetentionPin, ID: "pin:b", Verified: true, Complete: true,
		Placements: []PlacementRef{inventory[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	leaseA, err := BindReachabilityLease("repo:test", inventoryDigest, ReachabilityLease{
		ID: "lease:a", Verified: true, Active: true, Placements: []PlacementRef{inventory[0]},
	})
	if err != nil {
		t.Fatal(err)
	}
	leaseB, err := BindReachabilityLease("repo:test", inventoryDigest, ReachabilityLease{
		ID: "lease:b", Verified: true, Active: true, Placements: []PlacementRef{inventory[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := PlanReachability("repo:test", inventory, []ReachabilityRoot{rootA, rootB}, []ReachabilityLease{leaseA, leaseB})
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanReachability("repo:test", inventory, []ReachabilityRoot{rootB, rootA}, []ReachabilityLease{leaseB, leaseA})
	if err != nil {
		t.Fatal(err)
	}
	if first.RootsDigest != second.RootsDigest {
		t.Fatalf("root input ordering changed plan digest: %q != %q", first.RootsDigest, second.RootsDigest)
	}
}

func TestInTreePlacementInventoryFeedsReachabilityPlan(t *testing.T) {
	repo, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := repo.Place(ctx, bytes.NewReader([]byte("retained")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Place(ctx, bytes.NewReader([]byte("orphan"))); err != nil {
		t.Fatal(err)
	}
	record, err := repo.PlaceRecord(ctx, RecordPublicationCommit, bytes.NewReader([]byte(`{"schema":"test"}`)))
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := repo.ListPlacementRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inventoryDigest := placementDigest(mustNormalizedInventory(t, inventory))
	root, err := BindReachabilityRoot(repo.RepositoryIdentity(), inventoryDigest, ReachabilityRoot{
		Kind: RootCommittedSnapshot, ID: "snapshot:test", Verified: true, Complete: true,
		Placements: []PlacementRef{{Kind: "blob", Digest: first.ContentID}, {Kind: "record:" + string(RecordPublicationCommit), Digest: record.Digest}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanReachability(repo.RepositoryIdentity(), inventory, []ReachabilityRoot{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Inventory) != 3 || len(plan.Reachable) != 2 || len(plan.Candidates) != 1 {
		t.Fatalf("inventory plan = %+v", plan)
	}
	after, err := repo.ListPlacementRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(inventory) {
		t.Fatalf("reachability planning changed repository inventory: before=%d after=%d", len(inventory), len(after))
	}
}

func mustNormalizedInventory(t *testing.T, inventory []PlacementRef) []PlacementRef {
	t.Helper()
	_, normalized, err := normalizePlacementInventory(inventory)
	if err != nil {
		t.Fatal(err)
	}
	return normalized
}
