package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ReachabilityPlanSchemaV1 identifies the non-destructive root accounting
// record. A plan is evidence for a later, separately qualified collector; it
// never authorizes deletion by itself.
const ReachabilityPlanSchemaV1 = "restoreweave.reachability-plan.v1"

// ReachabilityRootKind names an authority which can keep a placement alive.
// The list is deliberately closed so a caller cannot silently invent a weaker
// root that would make an object eligible for collection.
type ReachabilityRootKind string

const (
	RootCommittedSnapshot   ReachabilityRootKind = "COMMITTED_SNAPSHOT"
	RootRecoveryClosure     ReachabilityRootKind = "RECOVERY_CLOSURE"
	RootExportManifest      ReachabilityRootKind = "EXPORT_MANIFEST"
	RootRetentionPin        ReachabilityRootKind = "RETENTION_PIN"
	RootDecoderMigration    ReachabilityRootKind = "DECODER_MIGRATION"
	RootPendingOperation    ReachabilityRootKind = "PENDING_OPERATION"
	RootPortablePublication ReachabilityRootKind = "PORTABLE_PUBLICATION"
	RootActiveLease         ReachabilityRootKind = "ACTIVE_LEASE"
)

// PlacementRef is a repository-private physical placement identity. Digest is
// used for content-addressed payloads and signed records; other placement
// kinds may use a host-owned opaque ID. It is never a path.
type PlacementRef struct {
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
}

// PlacementInventory is an optional host-owned view of physical repository
// placements. It is intentionally outside Driver: third-party backends may
// expose their own inventory format without changing the exact read ABI.
type PlacementInventory interface {
	ListPlacementRefs(context.Context) ([]PlacementRef, error)
}

// ListPlacementRefs enumerates the in-tree payload and portable-record
// placements without using paths as reachability evidence. It is read-only;
// temporary files and profile metadata are not inventory placements.
func (repo *Dir) ListPlacementRefs(ctx context.Context) ([]PlacementRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var refs []PlacementRef
	blobs := filepath.Join(repo.root, blobDirName, AlgorithmSHA256)
	err := filepath.WalkDir(blobs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && path == blobs {
				return fs.SkipDir
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("repository inventory found non-regular blob %q", path)
		}
		name := entry.Name()
		if len(name) != 64 {
			return fmt.Errorf("repository inventory found invalid blob name %q", name)
		}
		contentID := AlgorithmSHA256 + ":" + name
		if _, err := parseContentID(contentID); err != nil {
			return err
		}
		if filepath.Base(filepath.Dir(path)) != name[:hexPrefixLen] {
			return fmt.Errorf("repository inventory found blob %q in wrong prefix", name)
		}
		refs = append(refs, PlacementRef{Kind: "blob", Digest: contentID})
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, role := range []RecordRole{RecordPreparedClosure, RecordPublicationCommit, RecordProcessorAttemptClosure, RecordPortableFactClosure} {
		digests, err := repo.ListRecordDigests(ctx, role)
		if err != nil {
			return nil, err
		}
		for _, digest := range digests {
			refs = append(refs, PlacementRef{Kind: "record:" + string(role), Digest: digest})
		}
	}
	_, normalized, err := normalizePlacementInventory(refs)
	return normalized, err
}

func (repo *Memory) ListPlacementRefs(ctx context.Context) ([]PlacementRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo.mu.Lock()
	refs := make([]PlacementRef, 0, len(repo.blobs))
	for digest := range repo.blobs {
		refs = append(refs, PlacementRef{Kind: "blob", Digest: digest})
	}
	for role, records := range repo.records {
		for digest := range records {
			refs = append(refs, PlacementRef{Kind: "record:" + string(role), Digest: digest})
		}
	}
	repo.mu.Unlock()
	_, normalized, err := normalizePlacementInventory(refs)
	return normalized, err
}

func (repo *readOnlyDriver) ListPlacementRefs(ctx context.Context) ([]PlacementRef, error) {
	inventory, ok := repo.DriverRecord.(PlacementInventory)
	if !ok {
		return nil, errors.New("repository driver does not expose placement inventory")
	}
	return inventory.ListPlacementRefs(ctx)
}

// ReachabilityRoot is an authenticated, complete root observation. Incomplete
// or unverified roots are rejected rather than treated as empty, because an
// empty root set could make healthy data look collectable.
type ReachabilityRoot struct {
	RepositoryID    string               `json:"repository_id"`
	InventoryDigest string               `json:"inventory_digest"`
	EvidenceDigest  string               `json:"evidence_digest"`
	Kind            ReachabilityRootKind `json:"kind"`
	ID              string               `json:"id"`
	Verified        bool                 `json:"verified"`
	Complete        bool                 `json:"complete"`
	Placements      []PlacementRef       `json:"placements"`
}

// ReachabilityLease keeps placements required by an in-flight operation alive.
// Active leases are roots even when their operation has no committed snapshot
// yet. A stale or unverified lease must block planning.
type ReachabilityLease struct {
	RepositoryID    string         `json:"repository_id"`
	InventoryDigest string         `json:"inventory_digest"`
	EvidenceDigest  string         `json:"evidence_digest"`
	ID              string         `json:"id"`
	Verified        bool           `json:"verified"`
	Active          bool           `json:"active"`
	Placements      []PlacementRef `json:"placements"`
}

// ReachabilityPlan is a deterministic report. Candidates are advisory only:
// no repository driver consumes this type to delete data.
type ReachabilityPlan struct {
	Schema              string         `json:"schema"`
	RepositoryID        string         `json:"repository_id"`
	InventoryDigest     string         `json:"inventory_digest"`
	RootsDigest         string         `json:"roots_digest"`
	Inventory           []PlacementRef `json:"inventory"`
	Reachable           []PlacementRef `json:"reachable"`
	Candidates          []PlacementRef `json:"candidates"`
	ProtectedByLease    []PlacementRef `json:"protected_by_lease,omitempty"`
	CandidateCollection string         `json:"candidate_collection"`
}

// PlanReachability computes an immutable, fail-closed candidate plan. It
// requires a complete verified inventory and complete verified roots. Unknown
// root references return an error; they must not be silently ignored.
func PlanReachability(repositoryID string, inventory []PlacementRef, roots []ReachabilityRoot, leases []ReachabilityLease) (ReachabilityPlan, error) {
	if strings.TrimSpace(repositoryID) == "" {
		return ReachabilityPlan{}, errors.New("reachability repository identity is required")
	}
	if len(roots) == 0 && len(leases) == 0 {
		return ReachabilityPlan{}, errors.New("reachability requires at least one verified root or lease")
	}
	inventoryByKey, normalizedInventory, err := normalizePlacementInventory(inventory)
	if err != nil {
		return ReachabilityPlan{}, err
	}
	inventoryDigest := placementDigest(normalizedInventory)
	rootRefs := make(map[string]struct{})
	leaseRefs := make(map[string]struct{})
	for _, root := range roots {
		if err := validateReachabilityRoot(root); err != nil {
			return ReachabilityPlan{}, err
		}
		if err := validateReachabilityRootBinding(repositoryID, inventoryDigest, root); err != nil {
			return ReachabilityPlan{}, err
		}
		for _, placement := range root.Placements {
			key := placementKey(placement)
			if _, ok := inventoryByKey[key]; !ok {
				return ReachabilityPlan{}, fmt.Errorf("reachability root %s/%s references unknown placement %s", root.Kind, root.ID, key)
			}
			rootRefs[key] = struct{}{}
		}
	}
	for _, lease := range leases {
		if err := validateReachabilityLease(lease); err != nil {
			return ReachabilityPlan{}, err
		}
		if err := validateReachabilityLeaseBinding(repositoryID, inventoryDigest, lease); err != nil {
			return ReachabilityPlan{}, err
		}
		if !lease.Active {
			continue
		}
		for _, placement := range lease.Placements {
			key := placementKey(placement)
			if _, ok := inventoryByKey[key]; !ok {
				return ReachabilityPlan{}, fmt.Errorf("active reachability lease %s references unknown placement %s", lease.ID, key)
			}
			rootRefs[key] = struct{}{}
			leaseRefs[key] = struct{}{}
		}
	}

	reachable := make([]PlacementRef, 0, len(rootRefs))
	protectedByLease := make([]PlacementRef, 0, len(leaseRefs))
	candidates := make([]PlacementRef, 0, len(normalizedInventory))
	for _, placement := range normalizedInventory {
		key := placementKey(placement)
		if _, ok := rootRefs[key]; ok {
			reachable = append(reachable, placement)
			if _, leased := leaseRefs[key]; leased {
				protectedByLease = append(protectedByLease, placement)
			}
			continue
		}
		candidates = append(candidates, placement)
	}
	rootDigest, err := reachabilityDigest(roots, leases)
	if err != nil {
		return ReachabilityPlan{}, err
	}
	return ReachabilityPlan{
		Schema:              ReachabilityPlanSchemaV1,
		RepositoryID:        repositoryID,
		InventoryDigest:     inventoryDigest,
		RootsDigest:         rootDigest,
		Inventory:           normalizedInventory,
		Reachable:           reachable,
		Candidates:          candidates,
		ProtectedByLease:    protectedByLease,
		CandidateCollection: "NON_DESTRUCTIVE_ONLY",
	}, nil
}

func normalizePlacementInventory(inventory []PlacementRef) (map[string]PlacementRef, []PlacementRef, error) {
	if len(inventory) == 0 {
		return nil, nil, errors.New("reachability inventory is empty")
	}
	byKey := make(map[string]PlacementRef, len(inventory))
	for _, placement := range inventory {
		if err := validatePlacementRef(placement); err != nil {
			return nil, nil, err
		}
		key := placementKey(placement)
		if _, exists := byKey[key]; exists {
			return nil, nil, fmt.Errorf("reachability inventory contains duplicate placement %s", key)
		}
		byKey[key] = placement
	}
	normalized := make([]PlacementRef, 0, len(byKey))
	for _, placement := range byKey {
		normalized = append(normalized, placement)
	}
	sort.Slice(normalized, func(i, j int) bool { return placementKey(normalized[i]) < placementKey(normalized[j]) })
	return byKey, normalized, nil
}

func validatePlacementRef(placement PlacementRef) error {
	if strings.TrimSpace(placement.Kind) == "" || strings.TrimSpace(placement.Kind) != placement.Kind {
		return errors.New("reachability placement kind is required and must be trimmed")
	}
	if strings.TrimSpace(placement.Digest) == "" || strings.TrimSpace(placement.Digest) != placement.Digest || strings.ContainsAny(placement.Digest, `/\\`) {
		return fmt.Errorf("reachability placement %s has invalid identity", placement.Kind)
	}
	return nil
}

func validateReachabilityRoot(root ReachabilityRoot) error {
	if !validReachabilityRootKind(root.Kind) {
		return fmt.Errorf("reachability root %q is unsupported", root.Kind)
	}
	if strings.TrimSpace(root.ID) == "" || strings.TrimSpace(root.ID) != root.ID {
		return errors.New("reachability root ID is required and must be trimmed")
	}
	if !root.Verified || !root.Complete {
		return fmt.Errorf("reachability root %s/%s is not verified and complete", root.Kind, root.ID)
	}
	if len(root.Placements) == 0 {
		return fmt.Errorf("reachability root %s/%s has no placements", root.Kind, root.ID)
	}
	for _, placement := range root.Placements {
		if err := validatePlacementRef(placement); err != nil {
			return err
		}
	}
	return nil
}

// ReachabilityRootDigest returns the canonical digest for a root's semantic
// evidence. EvidenceDigest is deliberately excluded so callers can bind it
// without a recursive hash. Placement order is not significant.
func ReachabilityRootDigest(root ReachabilityRoot) (string, error) {
	if err := validateReachabilityRoot(root); err != nil {
		return "", err
	}
	if err := validateReachabilityBindingFields(root.RepositoryID, root.InventoryDigest); err != nil {
		return "", err
	}
	placements, err := canonicalReachabilityPlacements(root.Placements)
	if err != nil {
		return "", err
	}
	canonical := struct {
		RepositoryID    string               `json:"repository_id"`
		InventoryDigest string               `json:"inventory_digest"`
		Kind            ReachabilityRootKind `json:"kind"`
		ID              string               `json:"id"`
		Verified        bool                 `json:"verified"`
		Complete        bool                 `json:"complete"`
		Placements      []PlacementRef       `json:"placements"`
	}{
		RepositoryID:    root.RepositoryID,
		InventoryDigest: root.InventoryDigest,
		Kind:            root.Kind,
		ID:              root.ID,
		Verified:        root.Verified,
		Complete:        root.Complete,
		Placements:      placements,
	}
	return digestCanonicalReachability(canonical)
}

// ReachabilityLeaseDigest returns the canonical digest for a lease's semantic
// evidence. EvidenceDigest is excluded for the same reason as root evidence.
func ReachabilityLeaseDigest(lease ReachabilityLease) (string, error) {
	if err := validateReachabilityLease(lease); err != nil {
		return "", err
	}
	if err := validateReachabilityBindingFields(lease.RepositoryID, lease.InventoryDigest); err != nil {
		return "", err
	}
	placements, err := canonicalReachabilityPlacements(lease.Placements)
	if err != nil {
		return "", err
	}
	canonical := struct {
		RepositoryID    string         `json:"repository_id"`
		InventoryDigest string         `json:"inventory_digest"`
		ID              string         `json:"id"`
		Verified        bool           `json:"verified"`
		Active          bool           `json:"active"`
		Placements      []PlacementRef `json:"placements"`
	}{
		RepositoryID:    lease.RepositoryID,
		InventoryDigest: lease.InventoryDigest,
		ID:              lease.ID,
		Verified:        lease.Verified,
		Active:          lease.Active,
		Placements:      placements,
	}
	return digestCanonicalReachability(canonical)
}

// BindReachabilityRoot attaches the plan identity and canonical evidence
// digest. It does not authorize collection.
func BindReachabilityRoot(repositoryID, inventoryDigest string, root ReachabilityRoot) (ReachabilityRoot, error) {
	root.RepositoryID = repositoryID
	root.InventoryDigest = inventoryDigest
	root.EvidenceDigest = ""
	digest, err := ReachabilityRootDigest(root)
	if err != nil {
		return ReachabilityRoot{}, err
	}
	root.EvidenceDigest = digest
	return root, nil
}

func validateReachabilityLease(lease ReachabilityLease) error {
	if strings.TrimSpace(lease.ID) == "" || strings.TrimSpace(lease.ID) != lease.ID {
		return errors.New("reachability lease ID is required and must be trimmed")
	}
	if !lease.Verified {
		return fmt.Errorf("reachability lease %s is not verified", lease.ID)
	}
	if lease.Active && len(lease.Placements) == 0 {
		return fmt.Errorf("active reachability lease %s has no placements", lease.ID)
	}
	for _, placement := range lease.Placements {
		if err := validatePlacementRef(placement); err != nil {
			return err
		}
	}
	return nil
}

// BindReachabilityLease attaches the plan identity and canonical evidence
// digest. It does not authorize collection.
func BindReachabilityLease(repositoryID, inventoryDigest string, lease ReachabilityLease) (ReachabilityLease, error) {
	lease.RepositoryID = repositoryID
	lease.InventoryDigest = inventoryDigest
	lease.EvidenceDigest = ""
	digest, err := ReachabilityLeaseDigest(lease)
	if err != nil {
		return ReachabilityLease{}, err
	}
	lease.EvidenceDigest = digest
	return lease, nil
}

func validateReachabilityRootBinding(repositoryID, inventoryDigest string, root ReachabilityRoot) error {
	if root.RepositoryID != repositoryID {
		return fmt.Errorf("reachability root %s/%s has mismatched repository identity", root.Kind, root.ID)
	}
	if root.InventoryDigest != inventoryDigest {
		return fmt.Errorf("reachability root %s/%s has mismatched inventory digest", root.Kind, root.ID)
	}
	if !validReachabilityDigest(root.EvidenceDigest) {
		return fmt.Errorf("reachability root %s/%s has invalid evidence digest", root.Kind, root.ID)
	}
	want, err := ReachabilityRootDigest(root)
	if err != nil {
		return err
	}
	if root.EvidenceDigest != want {
		return fmt.Errorf("reachability root %s/%s evidence digest mismatch", root.Kind, root.ID)
	}
	return nil
}

func validateReachabilityBindingFields(repositoryID, inventoryDigest string) error {
	if strings.TrimSpace(repositoryID) == "" || strings.TrimSpace(repositoryID) != repositoryID {
		return errors.New("reachability evidence repository identity is required and must be trimmed")
	}
	if !validReachabilityDigest(inventoryDigest) {
		return errors.New("reachability evidence inventory digest is invalid")
	}
	return nil
}

func validateReachabilityLeaseBinding(repositoryID, inventoryDigest string, lease ReachabilityLease) error {
	if lease.RepositoryID != repositoryID {
		return fmt.Errorf("reachability lease %s has mismatched repository identity", lease.ID)
	}
	if lease.InventoryDigest != inventoryDigest {
		return fmt.Errorf("reachability lease %s has mismatched inventory digest", lease.ID)
	}
	if !validReachabilityDigest(lease.EvidenceDigest) {
		return fmt.Errorf("reachability lease %s has invalid evidence digest", lease.ID)
	}
	want, err := ReachabilityLeaseDigest(lease)
	if err != nil {
		return err
	}
	if lease.EvidenceDigest != want {
		return fmt.Errorf("reachability lease %s evidence digest mismatch", lease.ID)
	}
	return nil
}

func validReachabilityRootKind(kind ReachabilityRootKind) bool {
	switch kind {
	case RootCommittedSnapshot, RootRecoveryClosure, RootExportManifest,
		RootRetentionPin, RootDecoderMigration, RootPendingOperation,
		RootPortablePublication, RootActiveLease:
		return true
	default:
		return false
	}
}

func placementKey(placement PlacementRef) string { return placement.Kind + "\x00" + placement.Digest }

func placementDigest(inventory []PlacementRef) string {
	payload, _ := json.Marshal(inventory)
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PlacementInventoryDigest returns the deterministic identity of a complete
// placement inventory. It is exported for catalog-free evidence builders that
// must bind roots and leases before calling PlanReachability.
func PlacementInventoryDigest(inventory []PlacementRef) (string, error) {
	_, normalized, err := normalizePlacementInventory(inventory)
	if err != nil {
		return "", err
	}
	return placementDigest(normalized), nil
}

func canonicalReachabilityPlacements(placements []PlacementRef) ([]PlacementRef, error) {
	canonical := append([]PlacementRef(nil), placements...)
	for _, placement := range canonical {
		if err := validatePlacementRef(placement); err != nil {
			return nil, err
		}
	}
	sort.Slice(canonical, func(i, j int) bool { return placementKey(canonical[i]) < placementKey(canonical[j]) })
	for i := 1; i < len(canonical); i++ {
		if placementKey(canonical[i-1]) == placementKey(canonical[i]) {
			return nil, fmt.Errorf("reachability evidence contains duplicate placement %s", placementKey(canonical[i]))
		}
	}
	return canonical, nil
}

func digestCanonicalReachability(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validReachabilityDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func reachabilityDigest(roots []ReachabilityRoot, leases []ReachabilityLease) (string, error) {
	canonical := struct {
		Roots  []ReachabilityRoot  `json:"roots"`
		Leases []ReachabilityLease `json:"leases"`
	}{Roots: cloneReachabilityRoots(roots), Leases: cloneReachabilityLeases(leases)}
	for i := range canonical.Roots {
		sort.Slice(canonical.Roots[i].Placements, func(a, b int) bool {
			return placementKey(canonical.Roots[i].Placements[a]) < placementKey(canonical.Roots[i].Placements[b])
		})
	}
	for i := range canonical.Leases {
		sort.Slice(canonical.Leases[i].Placements, func(a, b int) bool {
			return placementKey(canonical.Leases[i].Placements[a]) < placementKey(canonical.Leases[i].Placements[b])
		})
	}
	sort.Slice(canonical.Roots, func(i, j int) bool {
		return reachabilityRootSortKey(canonical.Roots[i]) < reachabilityRootSortKey(canonical.Roots[j])
	})
	sort.Slice(canonical.Leases, func(i, j int) bool {
		return reachabilityLeaseSortKey(canonical.Leases[i]) < reachabilityLeaseSortKey(canonical.Leases[j])
	})
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func reachabilityRootSortKey(root ReachabilityRoot) string {
	payload, _ := json.Marshal(root)
	return string(payload)
}

func reachabilityLeaseSortKey(lease ReachabilityLease) string {
	payload, _ := json.Marshal(lease)
	return string(payload)
}

func cloneReachabilityRoots(roots []ReachabilityRoot) []ReachabilityRoot {
	cloned := make([]ReachabilityRoot, len(roots))
	for i, root := range roots {
		cloned[i] = root
		cloned[i].Placements = append([]PlacementRef(nil), root.Placements...)
	}
	return cloned
}

func cloneReachabilityLeases(leases []ReachabilityLease) []ReachabilityLease {
	cloned := make([]ReachabilityLease, len(leases))
	for i, lease := range leases {
		cloned[i] = lease
		cloned[i].Placements = append([]PlacementRef(nil), lease.Placements...)
	}
	return cloned
}
