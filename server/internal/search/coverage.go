package search

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

type semanticCoverageProbe interface {
	Coverage(context.Context, ZvecGenerationSpec) ([]string, error)
}

// semanticCoveragePairsProbe is the subject-bound coverage contract. The
// older ID-only probe remains recognized below for replaceable backends, but
// it can never establish complete coverage because it cannot bind rows back
// to their durable subject.
type semanticCoveragePairsProbe interface {
	CoveragePairs(context.Context, ZvecGenerationSpec) ([]ZvecCoverageIdentity, error)
}

// semanticCoveragePairMethodProbe permits backends that adopted the pair
// return shape directly on Coverage while keeping the legacy shape accepted.
type semanticCoveragePairMethodProbe interface {
	Coverage(context.Context, ZvecGenerationSpec) ([]ZvecCoverageIdentity, error)
}

type SemanticCoverageStatement struct {
	Dimension     string
	GenerationID  string
	ConfigDigest  string
	ProfileDigest string
	Available     bool
	Complete      bool
	Expected      int
	Indexed       int
	Missing       []string
	Notes         string
}

// semanticCoverageComparison is the host-owned comparison between the
// durable segment set and the identities reported by one backend generation.
// A segment ID is only an identity when its subject binding also matches. The
// distinction is intentionally retained so callers can report the reason a
// generation was rejected without treating a partial result as ready.
type semanticCoverageComparison struct {
	Expected        int
	Indexed         int
	Missing         []string
	Extra           []ZvecCoverageIdentity
	Duplicate       []ZvecCoverageIdentity
	SubjectMismatch []ZvecCoverageIdentity
}

// semanticGenerationReceiptFile is an immutable, generation-local snapshot of
// the expected semantic input set. The operational catalog is intentionally
// not consulted when reopening an old generation: descriptions and notes may
// have advanced since that projection was built.
type semanticGenerationReceiptFile struct {
	Schema         string                 `json:"schema"`
	GenerationID   string                 `json:"generation_id"`
	WorkspaceID    string                 `json:"workspace_id"`
	SnapshotRef    string                 `json:"snapshot_ref"`
	NamespaceRoot  string                 `json:"namespace_root_id"`
	ConfigDigest   string                 `json:"config_digest"`
	ProfileDigest  string                 `json:"profile_digest"`
	SemanticSpace  string                 `json:"semantic_space"`
	LibraryDigest  string                 `json:"library_digest"`
	IdentityDigest string                 `json:"identity_digest"`
	Identities     []ZvecCoverageIdentity `json:"identities"`
}

const semanticGenerationReceiptSchema = "restoreweave.semantic-generation-receipt.v1"

// Receipts are disposable search projection evidence, not an unbounded
// metadata channel. Keep the limit high enough for a large real generation,
// while refusing attacker-controlled allocations before decoding JSON.
const semanticGenerationReceiptMaxBytes = uint64(16 << 20)

func semanticGenerationReceiptPath(generationPath string) string {
	return filepath.Clean(generationPath) + ".receipt.json"
}

func semanticCoverageDigest(identities []ZvecCoverageIdentity) string {
	canonical := append([]ZvecCoverageIdentity(nil), identities...)
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].SegmentID != canonical[j].SegmentID {
			return canonical[i].SegmentID < canonical[j].SegmentID
		}
		return canonical[i].SubjectID < canonical[j].SubjectID
	})
	b, _ := json.Marshal(canonical)
	digest := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func writeSemanticGenerationReceipt(generation sqlite.IndexGeneration, libraryDigest string, identities []ZvecCoverageIdentity) error {
	if strings.TrimSpace(generation.DBPath) == "" || strings.TrimSpace(generation.ID) == "" {
		return errors.New("semantic generation receipt requires generation identity and path")
	}
	identities = append([]ZvecCoverageIdentity(nil), identities...)
	receipt := semanticGenerationReceiptFile{
		Schema: semanticGenerationReceiptSchema, GenerationID: generation.ID,
		WorkspaceID: generation.WorkspaceID, SnapshotRef: generation.SnapshotRef,
		NamespaceRoot: generation.NamespaceRootID, ConfigDigest: generation.ConfigDigest,
		ProfileDigest: generation.ProviderProfileDigest, SemanticSpace: generation.SemanticSpace,
		LibraryDigest: libraryDigest, IdentityDigest: semanticCoverageDigest(identities), Identities: identities,
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	// Generation IDs are unique. Refuse replacement so a stale or concurrent
	// writer cannot silently reinterpret an existing generation.
	finalPath := semanticGenerationReceiptPath(generation.DBPath)
	parent := filepath.Dir(finalPath)
	file, err := os.CreateTemp(parent, ".restoreweave-semantic-receipt-*")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	published := false
	linked := false
	defer func() {
		_ = file.Close()
		if !published {
			_ = os.Remove(tmpPath)
			if linked {
				_ = os.Remove(finalPath)
			}
		}
	}()
	if _, err := file.Write(payload); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Link is an atomic no-replace publication: unlike Rename it cannot
	// overwrite an existing receipt, and a crash cannot expose a truncated
	// final file. Sync the containing directory after the link.
	if err := os.Link(tmpPath, finalPath); err != nil {
		return err
	}
	linked = true
	if err := os.Remove(tmpPath); err != nil {
		return err
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	if err := directory.Close(); err != nil {
		return err
	}
	published = true
	return nil
}

// openSemanticGenerationReceipt opens the receipt through the same descriptor
// relative, no-follow helper used for admitted semantic bundle assets. Resolve
// only ancestor aliases so Darwin's /var -> /private/var layout remains
// compatible; the final receipt component is still opened with O_NOFOLLOW.
func openSemanticGenerationReceipt(path string) (*os.File, error) {
	clean := filepath.Clean(path)
	if clean == "" || !filepath.IsAbs(clean) || clean != path || clean == string(filepath.Separator) {
		return nil, errors.New("semantic generation receipt path must be an absolute clean non-root path")
	}
	// Reject non-regular objects before opening. In particular, opening a FIFO
	// read-only can block indefinitely; the descriptor-relative helper below
	// then supplies the no-follow race-safe final open for regular files.
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("semantic generation receipt must be a regular non-symlink file")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return nil, fmt.Errorf("resolve semantic generation receipt parent: %w", err)
	}
	canonical := filepath.Join(parent, filepath.Base(clean))
	relative := strings.TrimPrefix(filepath.ToSlash(canonical), "/")
	if relative == "" {
		return nil, errors.New("semantic generation receipt path is empty")
	}
	return openBundleFileNoFollow(string(filepath.Separator), relative)
}

func readBoundedSemanticGenerationReceipt(path string) ([]byte, error) {
	file, err := openSemanticGenerationReceipt(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat semantic generation receipt: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("semantic generation receipt is not a regular file")
	}
	if info.Size() <= 0 || uint64(info.Size()) > semanticGenerationReceiptMaxBytes {
		return nil, fmt.Errorf("semantic generation receipt size %d is outside bounds", info.Size())
	}
	maxInt := uint64(^uint(0) >> 1)
	if uint64(info.Size()) > maxInt {
		return nil, errors.New("semantic generation receipt is too large for this platform")
	}
	payload := make([]byte, int(info.Size()))
	if _, err := io.ReadFull(file, payload); err != nil {
		return nil, fmt.Errorf("read semantic generation receipt: %w", err)
	}
	// A writer may truncate or append after the initial stat. The descriptor
	// remains anchored to the opened inode, so a second stat makes both races
	// fail closed instead of decoding a partial snapshot.
	after, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("restat semantic generation receipt: %w", err)
	}
	if !after.Mode().IsRegular() || after.Size() != info.Size() {
		return nil, errors.New("semantic generation receipt changed during read")
	}
	return payload, nil
}

func readSemanticGenerationReceipt(generation sqlite.IndexGeneration) ([]ZvecCoverageIdentity, string, string, error) {
	if strings.TrimSpace(generation.DBPath) == "" {
		return nil, "", "", errors.New("semantic generation path is empty")
	}
	payload, err := readBoundedSemanticGenerationReceipt(semanticGenerationReceiptPath(generation.DBPath))
	if err == nil {
		var receipt semanticGenerationReceiptFile
		if err := json.Unmarshal(payload, &receipt); err != nil {
			return nil, "", "", fmt.Errorf("semantic generation receipt: %v", err)
		}
		if receipt.Schema != semanticGenerationReceiptSchema || receipt.GenerationID != generation.ID || receipt.WorkspaceID != generation.WorkspaceID ||
			receipt.SnapshotRef != generation.SnapshotRef || receipt.NamespaceRoot != generation.NamespaceRootID ||
			receipt.ConfigDigest != generation.ConfigDigest || receipt.ProfileDigest != generation.ProviderProfileDigest || receipt.SemanticSpace != generation.SemanticSpace ||
			receipt.IdentityDigest == "" || receipt.IdentityDigest != semanticCoverageDigest(receipt.Identities) {
			return nil, "", "", errors.New("semantic generation receipt binding is invalid")
		}
		if validateZvecLibraryDigest(receipt.LibraryDigest) != nil {
			return nil, "", "", errors.New("semantic generation receipt library digest is invalid")
		}
		if err := validateZvecCoverageIdentities(receipt.Identities); err != nil || len(receipt.Identities) == 0 {
			return nil, "", "", errors.New("semantic generation receipt identities are invalid")
		}
		return append([]ZvecCoverageIdentity(nil), receipt.Identities...), receipt.IdentityDigest, receipt.LibraryDigest, nil
	}
	if !os.IsNotExist(err) {
		return nil, "", "", err
	}
	// Generations written by the existing pure-Go zvec backend already carry
	// an immutable identity list in their side metadata. Accept that format as
	// a restart-compatible fallback; generic drivers get the receipt above.
	metadata, metadataErr := readZvecGenerationMetadata(generation.DBPath)
	if metadataErr != nil || len(metadata.Identities) == 0 {
		if metadataErr != nil {
			return nil, "", "", metadataErr
		}
		return nil, "", "", errors.New("semantic generation expected identity receipt is missing")
	}
	if metadata.Path != generation.DBPath || metadata.LibraryDigest == "" || metadata.ProfileDigest != generation.ProviderProfileDigest ||
		metadata.Manifest.ConfigDigest != generation.ConfigDigest || metadata.Manifest.CanonicalDigest() != generation.ProviderProfileDigest || metadata.Manifest.SemanticSpace != generation.SemanticSpace {
		return nil, "", "", errors.New("semantic generation metadata binding is invalid")
	}
	if err := validateZvecCoverageIdentities(metadata.Identities); err != nil || len(metadata.Identities) == 0 {
		return nil, "", "", errors.New("semantic generation metadata identities are invalid")
	}
	return append([]ZvecCoverageIdentity(nil), metadata.Identities...), semanticCoverageDigest(metadata.Identities), metadata.LibraryDigest, nil
}

func validateSemanticReceiptLibrary(idx *Indexer, libraryDigest string) error {
	if idx == nil || strings.TrimSpace(libraryDigest) == "" || libraryDigest != idx.SemanticLibraryDigest {
		return errors.New("semantic generation library binding is invalid")
	}
	return nil
}

func openCloseSemanticGeneration(ctx context.Context, backend ZvecGenerationDriver, spec ZvecGenerationSpec) error {
	opened, err := backend.Open(ctx, spec)
	if err != nil {
		return fmt.Errorf("open semantic generation: %v", err)
	}
	if opened == nil {
		// The in-memory qualification drivers intentionally have no reader
		// object. The pinned local BGE profile, by contrast, must return a real
		// reader and therefore fails closed on nil.
		if spec.Manifest.SemanticSpace != SemanticBundleBGESemanticSpace {
			return nil
		}
		return errors.New("semantic backend returned a nil generation")
	}
	if err := opened.Close(); err != nil {
		return fmt.Errorf("close semantic generation: %v", err)
	}
	return nil
}

func (c semanticCoverageComparison) Complete() bool {
	return len(c.Missing) == 0 && len(c.Extra) == 0 && len(c.Duplicate) == 0 && len(c.SubjectMismatch) == 0 && c.Indexed == c.Expected
}

// compareSemanticCoverage enforces exact set equality over the pair
// (subject_ref, semantic_segment_id). It is deliberately generation-specific:
// backend rows from another generation are extra rows, not harmless stale
// data, and repeated IDs are never collapsed into a ready count.
func compareSemanticCoverage(expected, actual []ZvecCoverageIdentity) semanticCoverageComparison {
	comparison := semanticCoverageComparison{Expected: len(expected)}
	want := make(map[string]string, len(expected))
	for _, identity := range expected {
		id := strings.TrimSpace(identity.SegmentID)
		subject := strings.TrimSpace(identity.SubjectID)
		if id == "" || subject == "" {
			comparison.Extra = append(comparison.Extra, identity)
			continue
		}
		if previous, exists := want[id]; exists {
			// Duplicate durable identities are not a valid expected set. Keep
			// this visible as a subject mismatch when the duplicate disagrees.
			if previous != subject {
				comparison.SubjectMismatch = append(comparison.SubjectMismatch, identity)
			}
			comparison.Duplicate = append(comparison.Duplicate, identity)
			continue
		}
		want[id] = subject
	}
	seen := make(map[string]struct{}, len(actual))
	for _, identity := range actual {
		id := strings.TrimSpace(identity.SegmentID)
		subject := strings.TrimSpace(identity.SubjectID)
		wantSubject, known := want[id]
		if !known || id == "" || subject == "" {
			comparison.Extra = append(comparison.Extra, identity)
			continue
		}
		if _, exists := seen[id]; exists {
			comparison.Duplicate = append(comparison.Duplicate, identity)
			// Continue checking the binding: a repeated ID with a different
			// subject is both duplicate and a binding violation.
		}
		seen[id] = struct{}{}
		if subject != wantSubject {
			comparison.SubjectMismatch = append(comparison.SubjectMismatch, identity)
			continue
		}
	}
	comparison.Indexed = len(seen)
	for id := range want {
		if _, exists := seen[id]; !exists {
			comparison.Missing = append(comparison.Missing, id)
		}
	}
	sort.Strings(comparison.Missing)
	sort.Slice(comparison.Extra, func(i, j int) bool { return comparison.Extra[i].SegmentID < comparison.Extra[j].SegmentID })
	sort.Slice(comparison.Duplicate, func(i, j int) bool { return comparison.Duplicate[i].SegmentID < comparison.Duplicate[j].SegmentID })
	sort.Slice(comparison.SubjectMismatch, func(i, j int) bool {
		return comparison.SubjectMismatch[i].SegmentID < comparison.SubjectMismatch[j].SegmentID
	})
	return comparison
}

func semanticCoverageError(comparison semanticCoverageComparison) error {
	switch {
	case len(comparison.SubjectMismatch) > 0:
		return errors.New("semantic generation contains subject/segment identity mismatch")
	case len(comparison.Extra) > 0:
		return errors.New("semantic generation contains unknown segment identities")
	case len(comparison.Duplicate) > 0:
		return errors.New("semantic generation contains duplicate segment identities")
	case len(comparison.Missing) > 0:
		return errors.New("semantic generation has incomplete segment coverage")
	default:
		return errors.New("semantic generation coverage is incomplete")
	}
}

// validateSemanticGenerationMapping verifies only the immutable namespace-root
// mapping recorded for a generation. It intentionally does not enumerate the
// current namespace projection or derive a mutable semantic denominator: those
// records may have changed since this disposable generation was published.
func validateSemanticGenerationMapping(ctx context.Context, store *sqlite.Store, generation sqlite.IndexGeneration) error {
	if store == nil {
		return errors.New("catalog is required")
	}
	roots, err := store.ListIndexGenerationRoots(ctx, generation.WorkspaceID, generation.ID)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return errors.New("index generation root mapping is empty")
	}
	seen := make(map[string]struct{}, len(roots))
	containsPrimary := false
	for _, root := range roots {
		if root.WorkspaceID != generation.WorkspaceID || strings.TrimSpace(root.ID) == "" || strings.TrimSpace(root.SnapshotRef) == "" {
			return fmt.Errorf("index generation root %q is outside workspace scope", root.ID)
		}
		if _, duplicate := seen[root.ID]; duplicate {
			return fmt.Errorf("duplicate index generation root %q", root.ID)
		}
		seen[root.ID] = struct{}{}
		if root.ID == generation.NamespaceRootID {
			containsPrimary = true
			if root.SnapshotRef != generation.SnapshotRef {
				return fmt.Errorf("primary index generation root %q snapshot %q does not match generation snapshot %q", root.ID, root.SnapshotRef, generation.SnapshotRef)
			}
		}
	}
	if !containsPrimary {
		return fmt.Errorf("index generation root mapping omits primary root %q", generation.NamespaceRootID)
	}
	return nil
}

// semanticCoveragePairs returns only subject-bound backend evidence. The
// legacy ID-only Coverage method remains useful for a degraded report, but it
// cannot establish readiness because it cannot prove subject provenance.
func semanticCoveragePairs(ctx context.Context, backend ZvecGenerationDriver, spec ZvecGenerationSpec) ([]ZvecCoverageIdentity, error) {
	switch probe := backend.(type) {
	case semanticCoveragePairsProbe:
		return probe.CoveragePairs(ctx, spec)
	case semanticCoveragePairMethodProbe:
		return probe.Coverage(ctx, spec)
	default:
		return nil, errors.New("semantic backend does not provide generation identity coverage")
	}
}

func semanticExpectedPairs(expected map[string]string) []ZvecCoverageIdentity {
	pairs := make([]ZvecCoverageIdentity, 0, len(expected))
	for id, subject := range expected {
		pairs = append(pairs, ZvecCoverageIdentity{SegmentID: id, SubjectID: subject})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].SegmentID != pairs[j].SegmentID {
			return pairs[i].SegmentID < pairs[j].SegmentID
		}
		return pairs[i].SubjectID < pairs[j].SubjectID
	})
	return pairs
}

func (idx *Indexer) verifySemanticGenerationCoverage(ctx context.Context, spec ZvecGenerationSpec, expected []ZvecCoverageIdentity) ([]ZvecCoverageIdentity, error) {
	actual, err := semanticCoveragePairs(ctx, idx.SemanticZvec, spec)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	comparison := compareSemanticCoverage(expected, actual)
	if !comparison.Complete() {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, semanticCoverageError(comparison))
	}
	verifier, ok := idx.SemanticZvec.(ZvecGenerationMembershipVerifier)
	if !ok {
		return nil, fmt.Errorf("%w: semantic backend does not provide generation membership verification", ErrUnavailable)
	}
	if err := verifier.VerifyMembership(ctx, spec, actual); err != nil {
		return nil, fmt.Errorf("%w: semantic generation membership: %v", ErrUnavailable, err)
	}
	return append([]ZvecCoverageIdentity(nil), actual...), nil
}

// ensureSemanticGenerationVerified validates a generation once per process
// and profile binding. Restarted processes begin with an empty cache and
// re-open the immutable backend metadata/collection; they do not consult the
// mutable catalog to reconstruct an old generation's denominator.
func (idx *Indexer) ensureSemanticGenerationVerified(ctx context.Context, generation sqlite.IndexGeneration) error {
	if idx == nil || idx.SemanticZvec == nil {
		return fmt.Errorf("%w: semantic backend is unavailable", ErrUnavailable)
	}
	// Serialize cache misses so concurrent first queries cannot each perform a
	// full collection enumeration before one of them publishes the receipt.
	idx.semanticVerifyMu.Lock()
	defer idx.semanticVerifyMu.Unlock()
	if identityCount, ok := idx.cachedSemanticGeneration(generation); ok {
		if identityCount == 0 {
			return fmt.Errorf("%w: semantic generation coverage is empty", ErrUnavailable)
		}
		return nil
	}
	expected, _, libraryDigest, err := readSemanticGenerationReceipt(generation)
	if err != nil {
		return fmt.Errorf("%w: read semantic generation receipt: %v", ErrUnavailable, err)
	}
	if err := validateSemanticReceiptLibrary(idx, libraryDigest); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	spec := ZvecGenerationSpec{
		Path: generation.DBPath, LibraryPath: idx.SemanticLibraryPath,
		LibraryDigest: idx.SemanticLibraryDigest,
		ProfileDigest: idx.SemanticManifest.CanonicalDigest(), Manifest: idx.SemanticManifest,
	}
	if err := openCloseSemanticGeneration(ctx, idx.SemanticZvec, spec); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	actual, err := idx.verifySemanticGenerationCoverage(ctx, spec, expected)
	if err != nil {
		return err
	}
	idx.cacheSemanticGeneration(generation, actual)
	return nil
}

// SemanticCoverage reports only evidence available from the durable segment
// set and the backend's generation coverage probe. A backend that cannot
// enumerate indexed segment IDs is deliberately partial, never complete.
func (idx *Indexer) SemanticCoverage(ctx context.Context, workspaceID string) (SemanticCoverageStatement, error) {
	statement := SemanticCoverageStatement{Dimension: DimensionSemantic, Notes: "semantic coverage requires backend segment identity evidence"}
	if idx == nil || idx.Store == nil || idx.SemanticProvider == nil || idx.SemanticZvec == nil || idx.SemanticManifest == (EmbeddingGenerationManifest{}) {
		statement.Notes = "semantic provider or backend is unavailable"
		return statement, nil
	}
	if idx.semanticUnavailable.Load() {
		statement.Notes = "semantic provider is degraded"
		return statement, nil
	}
	if health, ok := idx.SemanticProvider.(interface{ SemanticReady() bool }); ok && !health.SemanticReady() {
		statement.Notes = "semantic provider is not ready"
		return statement, nil
	}
	if health, ok := idx.SemanticZvec.(ZvecGenerationReadiness); ok && !health.ZvecReady(idx.SemanticLibraryPath, idx.SemanticLibraryDigest, idx.SemanticManifest) {
		statement.Notes = "semantic backend is not ready"
		return statement, nil
	}
	generation, err := idx.Store.LatestIndexGeneration(ctx, workspaceID, DimensionSemantic)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return statement, nil
		}
		return statement, err
	}
	statement.GenerationID, statement.ConfigDigest, statement.ProfileDigest = generation.ID, generation.ConfigDigest, generation.ProviderProfileDigest
	if !generationBindingMatches(idx, generation, DimensionSemantic) {
		statement.Notes = "semantic generation binding mismatch"
		return statement, nil
	}
	// Root/entry provenance is still required for a usable report, but it is
	// deliberately not used to derive the generation's expected denominator.
	if _, err := semanticNamespaceEntries(ctx, idx.Store, generation); err != nil {
		statement.Notes = "semantic namespace provenance is unavailable"
		return statement, nil
	}
	expectedPairs, _, libraryDigest, receiptErr := readSemanticGenerationReceipt(generation)
	if receiptErr != nil {
		statement.Notes = "semantic generation expected identity receipt is unavailable"
		return statement, nil
	}
	if err := validateSemanticReceiptLibrary(idx, libraryDigest); err != nil {
		statement.Notes = "semantic generation library binding is unavailable"
		return statement, nil
	}
	expected := make(map[string]string, len(expectedPairs))
	for _, pair := range expectedPairs {
		if _, duplicate := expected[pair.SegmentID]; duplicate {
			statement.Notes = "semantic generation expected identity receipt is invalid"
			return statement, nil
		}
		expected[pair.SegmentID] = pair.SubjectID
	}
	statement.Expected = len(expected)
	spec := ZvecGenerationSpec{Path: generation.DBPath, LibraryPath: idx.SemanticLibraryPath, LibraryDigest: idx.SemanticLibraryDigest, ProfileDigest: idx.SemanticManifest.CanonicalDigest(), Manifest: idx.SemanticManifest}
	var pairs []ZvecCoverageIdentity
	pairBound := true
	switch probe := idx.SemanticZvec.(type) {
	case semanticCoveragePairsProbe:
		pairs, err = probe.CoveragePairs(ctx, spec)
	case semanticCoveragePairMethodProbe:
		pairs, err = probe.Coverage(ctx, spec)
	case semanticCoverageProbe:
		var ids []string
		ids, err = probe.Coverage(ctx, spec)
		pairBound = false
		for _, id := range ids {
			pairs = append(pairs, ZvecCoverageIdentity{SegmentID: id})
		}
	default:
		statement.Notes = "semantic backend does not provide segment coverage evidence"
		return statement, nil
	}
	if err != nil {
		statement.Notes = "semantic coverage probe unavailable"
		return statement, nil
	}
	comparison := semanticCoverageComparison{}
	if pairBound {
		comparison = compareSemanticCoverage(semanticExpectedPairs(expected), pairs)
	} else {
		// Preserve the legacy report's ID-only semantics while explicitly
		// denying complete coverage in the absence of subject provenance.
		seen := map[string]struct{}{}
		for _, pair := range pairs {
			id := strings.TrimSpace(pair.SegmentID)
			if _, known := expected[id]; !known {
				comparison.Extra = append(comparison.Extra, pair)
				continue
			}
			seen[id] = struct{}{}
		}
		comparison.Expected = len(expected)
		comparison.Indexed = len(seen)
		for id := range expected {
			if _, ok := seen[id]; !ok {
				comparison.Missing = append(comparison.Missing, id)
			}
		}
		sort.Strings(comparison.Missing)
	}
	statement.Indexed = comparison.Indexed
	statement.Available = true
	statement.Missing = append(statement.Missing, comparison.Missing...)
	statement.Complete = pairBound && comparison.Complete()
	if statement.Complete {
		// Coverage is allowed to report complete only after an explicit
		// generation lifecycle check. CoveragePairs may be metadata-backed and
		// must not be the sole evidence that the reader can reopen cleanly.
		if err := openCloseSemanticGeneration(ctx, idx.SemanticZvec, spec); err != nil {
			statement.Complete = false
			statement.Notes = "semantic generation lifecycle is unavailable"
			return statement, nil
		}
	}
	switch {
	case !pairBound:
		statement.Notes = "semantic backend coverage is ID-only; subject/segment identity evidence is unavailable"
	case len(comparison.SubjectMismatch) > 0:
		statement.Notes = "semantic generation contains subject/segment identity mismatch"
	case len(comparison.Extra) > 0:
		statement.Notes = "semantic generation contains unknown segment identities"
	case len(comparison.Duplicate) > 0:
		statement.Notes = "semantic generation contains duplicate segment identities"
	case statement.Expected > 0 && statement.Indexed == 0:
		statement.Notes = "semantic generation is empty"
	case !statement.Complete:
		statement.Notes = "semantic generation has incomplete segment coverage"
	default:
		statement.Notes = "semantic generation covers every durable segment"
	}
	return statement, nil
}

// Coverage reports what the lexical generation actually feeds, honestly.
// Missing fields are reported as missing; a generation that does not exist
// yields an unavailable statement that is never complete. This is the
// per-generation view behind capability.list and search.query.
func (idx *Indexer) Coverage(ctx context.Context, workspaceID string) (CoverageStatement, error) {
	var statement CoverageStatement
	if idx == nil || idx.Store == nil || idx.Engine == nil {
		return LexicalCoverage(false, nil), nil
	}
	generation, err := idx.Store.LatestIndexGeneration(ctx, workspaceID, DimensionLexical)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return LexicalCoverage(false, nil), nil
		}
		return statement, err
	}
	return idx.CoverageGeneration(ctx, workspaceID, generation)
}

// CoverageGeneration measures the exact lexical generation supplied by the
// caller. It intentionally does not consult the catalog's latest pointer, so
// a rebuild response cannot pair one generation ID with another generation's
// coverage when a concurrent rebuild publishes a newer file.
func (idx *Indexer) CoverageGeneration(ctx context.Context, workspaceID string, generation sqlite.IndexGeneration) (CoverageStatement, error) {
	var statement CoverageStatement
	if idx == nil || idx.Engine == nil {
		return LexicalCoverage(false, nil), nil
	}
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(generation.WorkspaceID) == "" ||
		generation.WorkspaceID != workspaceID ||
		strings.TrimSpace(generation.Dimension) != DimensionLexical ||
		strings.TrimSpace(generation.DBPath) == "" {
		return LexicalCoverage(false, nil), nil
	}
	info, err := os.Stat(generation.DBPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LexicalCoverage(false, nil), nil
		}
		return CoverageStatement{}, err
	}
	if !info.Mode().IsRegular() {
		return LexicalCoverage(false, nil), nil
	}
	perField, err := measureFieldCoverage(ctx, generation.DBPath)
	if err != nil {
		return statement, err
	}
	return LexicalCoverage(true, perField), nil
}

// measureFieldCoverage runs one aggregate query over the built documents
// table and returns which non-empty fields exist. Numeric facets count as
// present when any row carries a non-null value; every other field counts as
// present when any row has non-whitespace content. The query never invents a
// field the schema does not feed.
func measureFieldCoverage(ctx context.Context, dbPath string) (map[string]bool, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var suffix, entryType, contentID, metadata, duplicates, duplicateGroup int
	var protection, locators, tags, notes, descriptions, extracted int
	var detection, processing, representations, language int
	var path, name, sizePresent, mtimePresent int
	row := db.QueryRowContext(ctx, `
SELECT
  COUNT(NULLIF(TRIM(path), '')),
  COUNT(NULLIF(TRIM(name), '')),
  COUNT(NULLIF(TRIM(suffix), '')),
  COUNT(NULLIF(TRIM(entry_type), '')),
  COUNT(NULLIF(TRIM(content_id), '')),
  COUNT(NULLIF(TRIM(metadata), '')),
  COUNT(NULLIF(TRIM(duplicates), '')),
  COUNT(NULLIF(TRIM(duplicate_group), '')),
  COUNT(NULLIF(TRIM(protection), '')),
  COUNT(NULLIF(TRIM(locators), '')),
  COUNT(NULLIF(TRIM(tags), '')),
  COUNT(NULLIF(TRIM(notes), '')),
  COUNT(NULLIF(TRIM(descriptions), '')),
  COUNT(NULLIF(TRIM(extracted), '')),
  COUNT(NULLIF(TRIM(detection), '')),
  COUNT(NULLIF(TRIM(processing), '')),
  COUNT(NULLIF(TRIM(representations), '')),
  COUNT(NULLIF(TRIM(language), '')),
  COUNT(size_facet),
  COUNT(mtime_facet)
FROM documents`)
	if err := row.Scan(&path, &name, &suffix, &entryType, &contentID, &metadata,
		&duplicates, &duplicateGroup, &protection, &locators, &tags, &notes,
		&descriptions, &extracted, &detection, &processing, &representations,
		&language, &sizePresent, &mtimePresent); err != nil {
		return nil, err
	}
	fields := map[string]bool{
		AxisPath:            path > 0,
		AxisName:            name > 0,
		AxisSuffix:          suffix > 0,
		AxisType:            entryType > 0,
		AxisChecksum:        contentID > 0,
		AxisMetadata:        metadata > 0,
		AxisDuplicates:      duplicates > 0,
		AxisDuplicateGroup:  duplicateGroup > 0,
		AxisProtection:      protection > 0,
		AxisLocators:        locators > 0,
		AxisTags:            tags > 0,
		AxisNotes:           notes > 0,
		AxisDescriptions:    descriptions > 0,
		AxisExtracted:       extracted > 0,
		AxisDetection:       detection > 0,
		AxisProcessing:      processing > 0,
		AxisRepresentations: representations > 0,
		AxisLanguage:        language > 0,
		AxisSize:            sizePresent > 0,
		AxisMtime:           mtimePresent > 0,
	}
	// Subject count is the honest denominator: a real generation always has
	// at least the namespace entries, so path/name presence is measured, not
	// assumed. Re-check against an actual row count.
	var subjectCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&subjectCount); err != nil {
		return nil, err
	}
	if subjectCount == 0 {
		return map[string]bool{}, nil
	}
	return fields, nil
}

// NormalizeCoverageFields validates a coverage field name against the
// documented surface. Empty returns every lexical coverage field.
func NormalizeCoverageFields(fields []string) ([]string, error) {
	all := LexicalCoverageFields()
	if len(fields) == 0 {
		return append([]string(nil), all...), nil
	}
	allowed := make(map[string]struct{}, len(all))
	for _, field := range all {
		allowed[field] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(strings.ToLower(field))
		if field == "" {
			continue
		}
		if _, ok := allowed[field]; !ok {
			return nil, errors.New("coverage field " + field + " is not a lexical feed field")
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out, nil
}
