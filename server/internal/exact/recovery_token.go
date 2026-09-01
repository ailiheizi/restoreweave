package exact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// RecoveryTokenSchemaV1 is the deterministic proof envelope over one admitted
// recovery reference. A token is a pointer and proof, never the payload and
// never a substitute for the repository: losing the token does not destroy
// repository data, and losing the independent trust anchor prevents the token
// from being trusted.
const RecoveryTokenSchemaV1 = "org.restoreweave.recovery-token.v1"

// RecoveryTokenSetSchemaV1 is the deterministic subject-scope wrapper for
// recovery tokens. The wrapper is derived on demand from the authenticated
// publication; it is not a new durable repository record.
const RecoveryTokenSetSchemaV1 = "org.restoreweave.recovery-token-set.v1"

const RecoveryUnprotectedRecordSchemaV1 = "org.restoreweave.recovery-unprotected.v1"

var (
	// ErrRecoveryTokenInvalid is returned when a token envelope is malformed,
	// its digest does not cover its fields, or its anchor reference is wrong.
	ErrRecoveryTokenInvalid = errors.New("recovery token is invalid")
	// ErrNoRecoveryPath is returned when a subject has no portable recovery
	// reference (for example a METADATA_ONLY / EXPLICITLY_UNPROTECTED subject).
	// Per the normative contract such subjects have no recovery token; their
	// EXPLICITLY_UNPROTECTED outcome is exported instead.
	ErrNoRecoveryPath = errors.New("subject has no recovery path")
)

// RecoveryToken is a deterministic, derivable proof envelope over an admitted
// recovery reference. The digest covers the canonical JSON of every other
// field; tokens need not be pre-stored because identical inputs always produce
// identical bytes.
type RecoveryToken struct {
	TokenSchema          string `json:"token_schema"`
	SnapshotRef          string `json:"snapshot_ref"`
	PublicationDomain    string `json:"publication_domain,omitempty"`
	SubjectRef           string `json:"subject_ref"`
	RecoveryReferenceID  string `json:"recovery_reference_id"`
	ReferenceKind        string `json:"reference_kind,omitempty"`
	ProtectionClaim      string `json:"protection_claim,omitempty"`
	ExpectedContentID    string `json:"expected_content_id,omitempty"`
	ExpectedLength       int64  `json:"expected_length,omitempty"`
	RecipeDigest         string `json:"recipe_digest,omitempty"`
	LocatorSetDigest     string `json:"locator_set_digest,omitempty"`
	PublicationCommitRef string `json:"publication_commit_ref"`
	TrustAnchorRef       string `json:"trust_anchor_ref"`
	Expiry               string `json:"expiry,omitempty"`
	TokenDigest          string `json:"token_digest"`
}

// RecoveryUnprotectedRecord is the explicit, portable result for a
// METADATA_ONLY subject. It is intentionally not a token and carries no
// implication that bytes can be restored or reacquired.
type RecoveryUnprotectedRecord struct {
	Schema               string `json:"schema"`
	SnapshotRef          string `json:"snapshot_ref"`
	SubjectRef           string `json:"subject_ref"`
	SubjectPath          string `json:"subject_path"`
	ProtectionMode       string `json:"protection_mode"`
	ProtectionOutcome    string `json:"protection_outcome"`
	ReasonCode           string `json:"reason_code,omitempty"`
	ExpectedLogicalBytes *int64 `json:"expected_logical_bytes,omitempty"`
	RecordDigest         string `json:"record_digest"`
}

func (record RecoveryUnprotectedRecord) unsignedCanonical() ([]byte, error) {
	record.RecordDigest = ""
	return canonicalJSON(record)
}

func (record RecoveryUnprotectedRecord) Digest() (string, error) {
	payload, err := record.unsignedCanonical()
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// RecoveryTokenSet is the deterministic subject-scope export. Tokens are
// ordered by reference priority and ID for human output, while SetDigest is
// computed over token digests sorted independently of that order so the set
// remains stable when the authenticated manifest presents references in a
// different order.
type RecoveryTokenSet struct {
	Schema            string                     `json:"schema"`
	SnapshotRef       string                     `json:"snapshot_ref"`
	PublicationDomain string                     `json:"publication_domain"`
	SubjectPath       string                     `json:"subject_path"`
	SubjectRef        string                     `json:"subject_ref"`
	ProtectionOutcome string                     `json:"protection_outcome"`
	Tokens            []RecoveryToken            `json:"tokens"`
	Unprotected       *RecoveryUnprotectedRecord `json:"unprotected,omitempty"`
	SetDigest         string                     `json:"set_digest"`
}

func (set RecoveryTokenSet) unsignedCanonical() ([]byte, error) {
	set.SetDigest = ""
	set.Tokens = append([]RecoveryToken(nil), set.Tokens...)
	sort.SliceStable(set.Tokens, func(i, j int) bool {
		return set.Tokens[i].TokenDigest < set.Tokens[j].TokenDigest
	})
	return canonicalJSON(set)
}

func (set RecoveryTokenSet) Digest() (string, error) {
	payload, err := set.unsignedCanonical()
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

func (token RecoveryToken) unsignedCanonical() ([]byte, error) {
	copy := token
	copy.TokenDigest = ""
	return canonicalJSON(copy)
}

// Digest returns the SHA-256 over the canonical JSON of every non-digest
// field. This avoids a digest cycle while binding the full envelope.
func (token RecoveryToken) Digest() (string, error) {
	payload, err := token.unsignedCanonical()
	if err != nil {
		return "", err
	}
	return DigestBytes(payload), nil
}

// Validate checks the envelope shape, the anchor reference, expiry, and the
// deterministic digest. It does not read a repository; the caller separately
// authenticates the publication and reference it points at.
func (token RecoveryToken) Validate(anchor TrustAnchor) error {
	if err := anchor.validate(); err != nil {
		return err
	}
	if token.TokenSchema != RecoveryTokenSchemaV1 {
		return fmt.Errorf("%w: schema %q", ErrRecoveryTokenInvalid, token.TokenSchema)
	}
	for name, value := range map[string]string{
		"snapshot ref":           token.SnapshotRef,
		"subject ref":            token.SubjectRef,
		"recovery reference id":  token.RecoveryReferenceID,
		"publication commit ref": token.PublicationCommitRef,
		"trust anchor ref":       token.TrustAnchorRef,
	} {
		if strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
			return fmt.Errorf("%w: %s is required", ErrRecoveryTokenInvalid, name)
		}
	}
	if token.PublicationDomain != "" && token.PublicationDomain != anchor.PublicationDomain {
		return fmt.Errorf("%w: publication domain differs from supplied anchor", ErrRecoveryTrustAnchor)
	}
	if token.ExpectedLength < 0 {
		return fmt.Errorf("%w: expected length cannot be negative", ErrRecoveryTokenInvalid)
	}
	// Legacy v1 envelopes predate the explicit claim fields. Preserve their
	// validation behavior, but reject a partially populated new projection.
	if (strings.TrimSpace(token.ReferenceKind) == "") != (strings.TrimSpace(token.ProtectionClaim) == "") {
		return fmt.Errorf("%w: reference kind and protection claim must be supplied together", ErrRecoveryTokenInvalid)
	}
	if token.ReferenceKind != "" {
		switch sqlite.RecoveryReferenceKind(token.ReferenceKind) {
		case sqlite.RecoveryExactRepresentation, sqlite.RecoveryExactReversible,
			sqlite.RecoveryExternalLocator, sqlite.RecoveryUserRecipe:
		default:
			return fmt.Errorf("%w: unknown recovery reference kind %q", ErrRecoveryTokenInvalid, token.ReferenceKind)
		}
		switch sqlite.RecoveryClaim(token.ProtectionClaim) {
		case sqlite.RecoveryClaimRestoreVerified, sqlite.RecoveryClaimExternalReplayable,
			sqlite.RecoveryClaimLinkOnlyUnprotected, sqlite.RecoveryClaimUnavailable:
		default:
			return fmt.Errorf("%w: unknown protection claim %q", ErrRecoveryTokenInvalid, token.ProtectionClaim)
		}
	}
	if token.ExpectedContentID != "" && !validExactContentID(token.ExpectedContentID) {
		return fmt.Errorf("%w: expected content identity is invalid", ErrRecoveryTokenInvalid)
	}
	if token.RecipeDigest != "" && !validExactContentID(token.RecipeDigest) {
		return fmt.Errorf("%w: recipe digest is invalid", ErrRecoveryTokenInvalid)
	}
	if token.LocatorSetDigest != "" && !validExactContentID(token.LocatorSetDigest) {
		return fmt.Errorf("%w: locator-set digest is invalid", ErrRecoveryTokenInvalid)
	}
	anchorDigest, err := DigestCanonicalJSON(anchor)
	if err != nil {
		return err
	}
	if token.TrustAnchorRef != anchorDigest {
		return fmt.Errorf("%w: trust-anchor reference differs from supplied anchor", ErrRecoveryTrustAnchor)
	}
	if token.Expiry != "" {
		expiry, err := time.Parse(time.RFC3339, token.Expiry)
		if err != nil {
			return fmt.Errorf("%w: expiry %q is not RFC3339: %v", ErrRecoveryTokenInvalid, token.Expiry, err)
		}
		if time.Now().UTC().After(expiry) {
			return fmt.Errorf("%w: token expired at %s", ErrRecoveryTokenInvalid, token.Expiry)
		}
	}
	digest, err := token.Digest()
	if err != nil {
		return err
	}
	if token.TokenDigest != digest {
		return fmt.Errorf("%w: token digest mismatch", ErrRecoveryTokenInvalid)
	}
	return nil
}

// ValidateAgainstRepository authenticates a token against the repository
// records it names. Validate intentionally remains repository-free for callers
// that only need to check an exported envelope; this method is the stronger
// clean-install proof and therefore requires the signed commit, prepared
// closure, and portable subject mapping to be present and readable.
func (token RecoveryToken) ValidateAgainstRepository(ctx context.Context, repo repository.Driver, anchor TrustAnchor) error {
	if repo == nil {
		return fmt.Errorf("%w: repository is required", ErrRecoveryTokenInvalid)
	}
	driver, ok := repo.(repository.RecordDriver)
	if !ok {
		return fmt.Errorf("%w: repository does not support recovery records", ErrRecoveryTokenInvalid)
	}
	if err := token.Validate(anchor); err != nil {
		return err
	}
	// Legacy v1 token envelopes are still accepted by Validate for migration,
	// but they do not contain enough projection fields to prove equality with a
	// selected manifest reference. A repository-backed proof must be explicit.
	if strings.TrimSpace(token.PublicationDomain) == "" ||
		strings.TrimSpace(token.ReferenceKind) == "" ||
		strings.TrimSpace(token.ProtectionClaim) == "" {
		return fmt.Errorf("%w: repository proof requires current token projection fields", ErrRecoveryTokenInvalid)
	}
	profile := repository.DescribeProfile(repo)
	if strings.TrimSpace(profile.Repository) == "" || strings.TrimSpace(profile.Compression) == "" {
		return fmt.Errorf("%w: repository profile is unavailable", ErrRecoveryTokenInvalid)
	}

	commitPayload, err := readRecord(ctx, driver, repository.RecordPublicationCommit, token.PublicationCommitRef)
	if err != nil {
		return fmt.Errorf("%w: read publication commit: %v", ErrRecoveryTokenInvalid, err)
	}
	var commit PublicationCommitRecord
	if err := decodeStrictRecord(commitPayload, &commit); err != nil {
		return fmt.Errorf("%w: decode publication commit: %v", ErrRecoveryTokenInvalid, err)
	}
	commitDigest, err := commit.Digest()
	if err != nil {
		return err
	}
	if commitDigest != token.PublicationCommitRef {
		return fmt.Errorf("%w: publication commit digest differs from token", ErrRecoveryTokenInvalid)
	}
	if err := commit.Verify(anchor); err != nil {
		return fmt.Errorf("%w: verify publication commit: %v", ErrRecoveryTokenInvalid, err)
	}
	if commit.TargetIdentity != driver.RepositoryIdentity() {
		return fmt.Errorf("%w: publication commit targets another repository", ErrRecoveryTokenInvalid)
	}
	if commit.PublicationDomain != anchor.PublicationDomain || commit.PublicationDomain != token.PublicationDomain || commit.SnapshotRef != token.SnapshotRef {
		return fmt.Errorf("%w: token publication binding differs from signed commit", ErrRecoveryTokenInvalid)
	}
	commits, err := listCommitMarkers(ctx, driver, anchor, commit.PublicationDomain)
	if err != nil {
		return fmt.Errorf("%w: authenticated publication lineage is unavailable: %v", ErrRecoveryTokenInvalid, err)
	}
	committed := false
	for _, candidate := range commits {
		if candidate.CommitDigest == token.PublicationCommitRef {
			committed = true
			break
		}
	}
	if !committed {
		return fmt.Errorf("%w: referenced publication commit is not in the authenticated lineage", ErrRecoveryTokenInvalid)
	}

	preparedPayload, err := readRecord(ctx, driver, repository.RecordPreparedClosure, commit.PreparedObjectDigest)
	if err != nil {
		return fmt.Errorf("%w: read prepared closure: %v", ErrRecoveryTokenInvalid, err)
	}
	var prepared PreparedClosureEnvelope
	if err := decodeStrictRecord(preparedPayload, &prepared); err != nil {
		return fmt.Errorf("%w: decode prepared closure: %v", ErrRecoveryTokenInvalid, err)
	}
	if err := validatePreparedEnvelope(driver, anchor, commit, prepared, int64(len(preparedPayload))); err != nil {
		return fmt.Errorf("%w: validate prepared closure: %v", ErrRecoveryTokenInvalid, err)
	}

	entry, err := resolveTokenManifestSubject(ctx, repo, driver, anchor, commit.PublicationDomain, commitDigest, prepared.Manifest, token.SubjectRef)
	if err != nil {
		return err
	}
	var selected *ManifestRecoveryReference
	for i := range entry.Protection.RecoveryReferences {
		reference := &entry.Protection.RecoveryReferences[i]
		if reference.ReferenceID != token.RecoveryReferenceID {
			continue
		}
		if selected != nil {
			return fmt.Errorf("%w: recovery reference %q is ambiguous", ErrRecoveryTokenInvalid, token.RecoveryReferenceID)
		}
		selected = reference
	}
	if selected == nil {
		return fmt.Errorf("%w: recovery reference %q is absent from authenticated manifest", ErrRecoveryTokenInvalid, token.RecoveryReferenceID)
	}
	if err := validateTokenLocalRecoveryReference(ctx, repo, *selected, entry); err != nil {
		return err
	}

	anchorDigest, err := DigestCanonicalJSON(anchor)
	if err != nil {
		return err
	}
	expected, err := buildRecoveryTokenForReference(token.SnapshotRef, commit.PublicationDomain, entry.RelativePath, entry, token.SubjectRef, *selected, commitDigest, anchorDigest)
	if err != nil {
		return err
	}
	if err := compareRepositoryTokenProjection(token, expected); err != nil {
		return err
	}
	return nil
}

// validateTokenLocalRecoveryReference validates only local payload-backed
// recovery references. The admitted Phase 3 profile has a raw exact reader,
// but no reversible codec decoder/encoded-placement reader yet. Treating an
// EXACT_REVERSIBLE reference as the source ContentID blob would make a token
// claim verified bytes without decoding and independently hashing them.
func validateTokenLocalRecoveryReference(ctx context.Context, repo repository.Driver, reference ManifestRecoveryReference, entry ManifestEntry) error {
	switch reference.Kind {
	case string(sqlite.RecoveryExactRepresentation):
		if reference.RepresentationID == "" || entry.Protection.ExpectedContentID == "" || entry.Protection.ExpectedLogicalLength == nil {
			return fmt.Errorf("%w: exact recovery reference %q lacks payload identity or length", ErrRecoveryTokenInvalid, reference.ReferenceID)
		}
		if err := verifyExactObjectReadback(ctx, repo, entry.Protection.ExpectedContentID, *entry.Protection.ExpectedLogicalLength); err != nil {
			return fmt.Errorf("%w: exact recovery reference %q payload: %v", ErrRecoveryTokenInvalid, reference.ReferenceID, err)
		}
	case string(sqlite.RecoveryExactReversible):
		return fmt.Errorf("%w: exact reversible recovery reference %q cannot be validated: decoder closure and encoded placement reader are unavailable", ErrRecoveryTokenInvalid, reference.ReferenceID)
	}
	return nil
}

// VerifyRecoveryToken is the package-level clean-install verifier. Keeping a
// function alongside the value method makes the dependency explicit for
// readers that do not construct a Service or open SQLite.
func VerifyRecoveryToken(ctx context.Context, repo repository.Driver, token RecoveryToken, anchor TrustAnchor) error {
	return token.ValidateAgainstRepository(ctx, repo, anchor)
}

// VerifyRecoveryToken verifies one token using this Service's repository. The
// service's catalog and signing identity are deliberately not consulted.
func (s *Service) VerifyRecoveryToken(ctx context.Context, token RecoveryToken, anchor TrustAnchor) error {
	if s == nil {
		return fmt.Errorf("%w: service is required", ErrRecoveryTokenInvalid)
	}
	return token.ValidateAgainstRepository(ctx, s.Repo, anchor)
}

func compareRepositoryTokenProjection(token, expected RecoveryToken) error {
	if token.TokenSchema != expected.TokenSchema ||
		token.SnapshotRef != expected.SnapshotRef ||
		token.PublicationDomain != expected.PublicationDomain ||
		token.SubjectRef != expected.SubjectRef ||
		token.RecoveryReferenceID != expected.RecoveryReferenceID ||
		token.ReferenceKind != expected.ReferenceKind ||
		token.ProtectionClaim != expected.ProtectionClaim ||
		token.ExpectedContentID != expected.ExpectedContentID ||
		token.ExpectedLength != expected.ExpectedLength ||
		token.RecipeDigest != expected.RecipeDigest ||
		token.LocatorSetDigest != expected.LocatorSetDigest ||
		token.PublicationCommitRef != expected.PublicationCommitRef ||
		token.TrustAnchorRef != expected.TrustAnchorRef ||
		token.Expiry != expected.Expiry {
		return fmt.Errorf("%w: token fields differ from authenticated manifest recovery reference", ErrRecoveryTokenInvalid)
	}
	return nil
}

// resolveTokenManifestSubject finds the one authenticated SUBJECT_MAPPING
// record named by token.SubjectRef and returns its already manifest-bound
// entry. listPortableFactClosures validates the complete closure, its reader
// dependency tuple, repository identity, and every mapping against manifest
// before this lookup is allowed to proceed.
func resolveTokenManifestSubject(ctx context.Context, repo repository.Driver, driver repository.RecordDriver, anchor TrustAnchor, domain, commitDigest string, manifest Manifest, subjectRef string) (ManifestEntry, error) {
	if strings.TrimSpace(subjectRef) == "" {
		return ManifestEntry{}, fmt.Errorf("%w: subject reference is required", ErrRecoveryTokenInvalid)
	}
	closures, err := listPortableFactClosures(ctx, repo, driver, anchor, domain, commitDigest)
	if err != nil {
		return ManifestEntry{}, fmt.Errorf("%w: portable subject mapping closure is unavailable: %v", ErrRecoveryTokenInvalid, err)
	}
	if len(closures) == 0 {
		return ManifestEntry{}, fmt.Errorf("%w: portable subject mapping closure is unavailable", ErrRecoveryTokenInvalid)
	}
	var bundle portableFactBundle
	if err := decodeStrictRecord(closures[len(closures)-1].Bundle, &bundle); err != nil {
		return ManifestEntry{}, fmt.Errorf("%w: portable subject mapping bundle is invalid: %v", ErrRecoveryTokenInvalid, err)
	}
	var found *ManifestEntry
	for _, record := range bundle.Records {
		if record.RecordKind != "SUBJECT_MAPPING" || record.StableSubjectRef != subjectRef {
			continue
		}
		var mapping subjectMappingPayload
		if err := decodeStrictRecord(record.Payload, &mapping); err != nil {
			return ManifestEntry{}, fmt.Errorf("%w: portable subject mapping payload is invalid: %v", ErrRecoveryTokenInvalid, err)
		}
		for i := range manifest.Entries {
			entry := &manifest.Entries[i]
			if !bytes.Equal(mapping.RawPath, entry.RawPath) {
				continue
			}
			if found != nil {
				return ManifestEntry{}, fmt.Errorf("%w: subject mapping %q is ambiguous", ErrRecoveryTokenInvalid, subjectRef)
			}
			copy := *entry
			found = &copy
		}
	}
	if found == nil {
		return ManifestEntry{}, fmt.Errorf("%w: authenticated subject mapping %q is missing", ErrRecoveryTokenInvalid, subjectRef)
	}
	return *found, nil
}

// BuildRecoveryToken derives the deterministic proof envelope for the highest
// priority reference of one subject. It remains as a compatibility helper;
// subject-scope callers should use BuildRecoveryTokenSet so every admitted
// RecoveryReference is exported.
func (s *Service) BuildRecoveryToken(ctx context.Context, snapshotRef, subjectPath string, anchor TrustAnchor) (RecoveryToken, error) {
	set, err := s.BuildRecoveryTokenSet(ctx, snapshotRef, subjectPath, anchor)
	if err != nil {
		return RecoveryToken{}, err
	}
	if len(set.Tokens) == 0 {
		return RecoveryToken{}, fmt.Errorf("%w: %s has protection outcome %s", ErrNoRecoveryPath, subjectPath, set.ProtectionOutcome)
	}
	return set.Tokens[0], nil
}

// BuildRecoveryTokenSet derives one deterministic token per admitted
// RecoveryReference in the authenticated manifest. Metadata-only subjects
// return an explicit unprotected record and an empty token set.
func (s *Service) BuildRecoveryTokenSet(ctx context.Context, snapshotRef, subjectPath string, anchor TrustAnchor) (RecoveryTokenSet, error) {
	var set RecoveryTokenSet
	if err := s.requireRepository(); err != nil {
		return set, err
	}
	if err := anchor.validate(); err != nil {
		return set, err
	}
	if strings.TrimSpace(snapshotRef) == "" {
		return set, fmt.Errorf("%w: snapshot reference is required", ErrRecoveryTokenInvalid)
	}
	if strings.TrimSpace(subjectPath) == "" {
		return set, fmt.Errorf("%w: subject path is required", ErrRecoveryTokenInvalid)
	}
	publication, err := s.committedPublicationWithAnchor(ctx, anchor, snapshotRef)
	if err != nil {
		return set, err
	}
	entry, err := resolveManifestSubject(publication.Manifest, subjectPath)
	if err != nil {
		return set, err
	}
	pathComponents, err := normalizeSubjectPath(subjectPath)
	if err != nil {
		return set, err
	}
	canonicalSubjectPath := strings.Join(pathComponents, "/")
	publicationDomain := publication.Commit.PublicationDomain
	if strings.TrimSpace(publicationDomain) == "" {
		return set, fmt.Errorf("%w: authenticated publication domain is missing", ErrRecoveryTokenInvalid)
	}
	anchorDigest, err := DigestCanonicalJSON(anchor)
	if err != nil {
		return set, err
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return set, err
	}
	domain := s.PublicationDomain
	if domain == "" {
		domain = anchor.PublicationDomain
	}
	subjectRef, err := s.stableSubjectRef(ctx, driver, anchor, domain, publication.CommitDigest, entry)
	if err != nil {
		return set, err
	}
	set = RecoveryTokenSet{
		Schema:            RecoveryTokenSetSchemaV1,
		SnapshotRef:       snapshotRef,
		PublicationDomain: publicationDomain,
		SubjectPath:       canonicalSubjectPath,
		SubjectRef:        subjectRef,
		ProtectionOutcome: entry.Protection.Outcome,
		Tokens:            []RecoveryToken{},
	}
	references := append([]ManifestRecoveryReference(nil), entry.Protection.RecoveryReferences...)
	if entry.Protection.Mode == string(sqlite.ProtectionMetadataOnly) && len(references) > 0 {
		return RecoveryTokenSet{}, fmt.Errorf("%w: metadata-only subject %s has recovery references", ErrRecoveryTokenInvalid, subjectPath)
	}
	sort.SliceStable(references, func(i, j int) bool {
		if references[i].Priority != references[j].Priority {
			return references[i].Priority < references[j].Priority
		}
		return references[i].ReferenceID < references[j].ReferenceID
	})
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if strings.TrimSpace(reference.ReferenceID) == "" {
			return RecoveryTokenSet{}, fmt.Errorf("%w: subject %s has an anonymous recovery reference", ErrRecoveryTokenInvalid, subjectPath)
		}
		if _, duplicate := seen[reference.ReferenceID]; duplicate {
			return RecoveryTokenSet{}, fmt.Errorf("%w: subject %s repeats recovery reference %s", ErrRecoveryTokenInvalid, subjectPath, reference.ReferenceID)
		}
		seen[reference.ReferenceID] = struct{}{}
		if entry.Protection.Mode == string(sqlite.ProtectionLinkOnly) &&
			reference.Claim != string(sqlite.RecoveryClaimLinkOnlyUnprotected) {
			return RecoveryTokenSet{}, fmt.Errorf("%w: link-only subject %s has claim %q", ErrRecoveryTokenInvalid, subjectPath, reference.Claim)
		}
		token, buildErr := buildRecoveryTokenForReference(snapshotRef, publicationDomain, canonicalSubjectPath, entry, subjectRef, reference, publication.CommitDigest, anchorDigest)
		if buildErr != nil {
			return RecoveryTokenSet{}, buildErr
		}
		set.Tokens = append(set.Tokens, token)
	}
	if len(set.Tokens) == 0 {
		if entry.Protection.Mode != string(sqlite.ProtectionMetadataOnly) ||
			entry.Protection.Outcome != string(sqlite.ProtectionExplicitlyUnprotected) {
			return RecoveryTokenSet{}, fmt.Errorf("%w: %s has protection outcome %s", ErrNoRecoveryPath, subjectPath, entry.Protection.Outcome)
		}
		set.Unprotected = &RecoveryUnprotectedRecord{
			Schema:               RecoveryUnprotectedRecordSchemaV1,
			SnapshotRef:          snapshotRef,
			SubjectRef:           subjectRef,
			SubjectPath:          canonicalSubjectPath,
			ProtectionMode:       entry.Protection.Mode,
			ProtectionOutcome:    entry.Protection.Outcome,
			ReasonCode:           entry.Protection.ReasonCode,
			ExpectedLogicalBytes: cloneInt64Pointer(entry.Protection.ExpectedLogicalLength),
		}
		recordDigest, digestErr := set.Unprotected.Digest()
		if digestErr != nil {
			return RecoveryTokenSet{}, digestErr
		}
		set.Unprotected.RecordDigest = recordDigest
	}
	setDigest, err := set.Digest()
	if err != nil {
		return RecoveryTokenSet{}, err
	}
	set.SetDigest = setDigest
	return set, nil
}

func buildRecoveryTokenForReference(snapshotRef, publicationDomain, subjectPath string, entry ManifestEntry, subjectRef string, reference ManifestRecoveryReference, commitDigest, anchorDigest string) (RecoveryToken, error) {
	recipeDigest := ""
	if len(reference.Recipe) > 0 {
		if !json.Valid(reference.Recipe) {
			return RecoveryToken{}, fmt.Errorf("%w: subject %s recovery recipe is invalid", ErrRecoveryTokenInvalid, subjectPath)
		}
		decoder := json.NewDecoder(bytes.NewReader(reference.Recipe))
		decoder.UseNumber()
		var recipeValue any
		if err := decoder.Decode(&recipeValue); err != nil {
			return RecoveryToken{}, fmt.Errorf("%w: subject %s recovery recipe is invalid", ErrRecoveryTokenInvalid, subjectPath)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return RecoveryToken{}, fmt.Errorf("%w: subject %s recovery recipe contains multiple JSON values", ErrRecoveryTokenInvalid, subjectPath)
		}
		canonicalRecipe, err := canonicalJSON(recipeValue)
		if err != nil {
			return RecoveryToken{}, fmt.Errorf("%w: subject %s recovery recipe cannot be canonicalized: %v", ErrRecoveryTokenInvalid, subjectPath, err)
		}
		recipeDigest = DigestBytes(canonicalRecipe)
	}
	expectedContentID := entry.Protection.ExpectedContentID
	expectedLength := int64(0)
	if entry.Protection.ExpectedLogicalLength != nil {
		expectedLength = *entry.Protection.ExpectedLogicalLength
	}
	locatorDigest := ""
	if len(reference.ExternalLocators) > 0 {
		locators := append([]ManifestExternalLocator(nil), reference.ExternalLocators...)
		sort.SliceStable(locators, func(i, j int) bool {
			if locators[i].Priority != locators[j].Priority {
				return locators[i].Priority < locators[j].Priority
			}
			if locators[i].LocatorID != locators[j].LocatorID {
				return locators[i].LocatorID < locators[j].LocatorID
			}
			return locators[i].Locator < locators[j].Locator
		})
		payload, err := canonicalJSON(locators)
		if err != nil {
			return RecoveryToken{}, err
		}
		locatorDigest = DigestBytes(payload)
	}
	token := RecoveryToken{
		TokenSchema:          RecoveryTokenSchemaV1,
		SnapshotRef:          snapshotRef,
		PublicationDomain:    publicationDomain,
		SubjectRef:           subjectRef,
		RecoveryReferenceID:  reference.ReferenceID,
		ReferenceKind:        reference.Kind,
		ProtectionClaim:      reference.Claim,
		ExpectedContentID:    expectedContentID,
		ExpectedLength:       expectedLength,
		RecipeDigest:         recipeDigest,
		LocatorSetDigest:     locatorDigest,
		PublicationCommitRef: commitDigest,
		TrustAnchorRef:       anchorDigest,
	}
	digest, err := token.Digest()
	if err != nil {
		return RecoveryToken{}, err
	}
	token.TokenDigest = digest
	return token, nil
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// stableSubjectRef resolves the portable stable subject identity for a
// manifest entry. A token is only derivable when the latest authenticated
// complete-state portable-fact closure contains exactly one matching
// SUBJECT_MAPPING record. There is deliberately no path-derived fallback:
// token subject identity must be supported by the signed mapping closure.
func (s *Service) stableSubjectRef(ctx context.Context, driver repository.RecordDriver, anchor TrustAnchor, domain, commitDigest string, entry ManifestEntry) (string, error) {
	closures, err := listPortableFactClosures(ctx, s.Repo, driver, anchor, domain, commitDigest)
	if err != nil {
		return "", fmt.Errorf("%w: portable subject mapping closure is unavailable: %v", ErrRecoveryTokenInvalid, err)
	}
	if len(closures) == 0 {
		return "", fmt.Errorf("%w: portable subject mapping closure is unavailable", ErrRecoveryTokenInvalid)
	}
	// Successor closures are complete-state snapshots. The latest authenticated
	// closure is authoritative for subject mapping, while the signed lineage
	// checked by listPortableFactClosures preserves its ancestry.
	latest := closures[len(closures)-1]
	var bundle portableFactBundle
	if err := decodeStrictRecord(latest.Bundle, &bundle); err != nil {
		return "", fmt.Errorf("%w: portable subject mapping bundle is invalid: %v", ErrRecoveryTokenInvalid, err)
	}
	var matched string
	for _, record := range bundle.Records {
		if record.RecordKind != "SUBJECT_MAPPING" || strings.TrimSpace(record.StableSubjectRef) == "" {
			continue
		}
		var mapping subjectMappingPayload
		if err := decodeStrictRecord(record.Payload, &mapping); err != nil {
			return "", fmt.Errorf("%w: portable subject mapping payload is invalid: %v", ErrRecoveryTokenInvalid, err)
		}
		if !bytes.Equal(mapping.RawPath, entry.RawPath) &&
			!(len(entry.RawPath) == 0 && mapping.DisplayName == entry.RelativePath) {
			continue
		}
		if matched != "" {
			return "", fmt.Errorf("%w: portable subject mapping is duplicated for %q", ErrRecoveryTokenInvalid, entry.RelativePath)
		}
		if mapping.NamespaceEntryID == "" {
			return "", fmt.Errorf("%w: portable subject mapping identity is invalid for %q", ErrRecoveryTokenInvalid, entry.RelativePath)
		}
		if bundle.Schema == PortableFactBundleSchemaV1 {
			if mapping.StableSubjectRef != "" || mapping.NamespaceEntryID != record.StableSubjectRef {
				return "", fmt.Errorf("%w: portable v1 subject mapping identity is invalid for %q", ErrRecoveryTokenInvalid, entry.RelativePath)
			}
		} else if bundle.Schema == PortableFactBundleSchemaV2 {
			if strings.TrimSpace(mapping.StableSubjectRef) == "" || mapping.StableSubjectRef != record.StableSubjectRef {
				return "", fmt.Errorf("%w: portable v2 subject mapping identity is invalid for %q", ErrRecoveryTokenInvalid, entry.RelativePath)
			}
		} else {
			return "", fmt.Errorf("%w: portable subject mapping bundle schema is invalid", ErrRecoveryTokenInvalid)
		}
		if !bytes.Equal(mapping.RawPath, entry.RawPath) || !bytes.Equal(mapping.RawName, entry.RawName) ||
			mapping.EntryType != entry.EntryType || mapping.ContentID != entry.ContentID ||
			!sameOptionalInt64(mapping.LogicalLength, entry.LogicalSize) ||
			!bytes.Equal(mapping.MetadataBefore, entry.MetadataBefore) ||
			!bytes.Equal(mapping.MetadataAfter, entry.MetadataAfter) ||
			!sameCanonicalJSON(mapping.Protection, entry.Protection) {
			return "", fmt.Errorf("%w: portable subject mapping differs from manifest for %q", ErrRecoveryTokenInvalid, entry.RelativePath)
		}
		displayName := entry.RelativePath
		if separator := strings.LastIndexByte(displayName, '/'); separator >= 0 {
			displayName = displayName[separator+1:]
		}
		if mapping.DisplayName != displayName {
			return "", fmt.Errorf("%w: portable subject mapping display name differs for %q", ErrRecoveryTokenInvalid, entry.RelativePath)
		}
		matched = record.StableSubjectRef
	}
	if matched == "" {
		return "", fmt.Errorf("%w: portable subject mapping is missing for %q", ErrRecoveryTokenInvalid, entry.RelativePath)
	}
	return matched, nil
}

// committedPublicationWithAnchor authenticates the full committed closure for
// one snapshot against a caller-supplied anchor, mirroring the catalog-free
// reader path but keeping the passed anchor authoritative for the token.
func (s *Service) committedPublicationWithAnchor(ctx context.Context, anchor TrustAnchor, snapshotRef string) (committedPublication, error) {
	if err := s.requireRepository(); err != nil {
		return committedPublication{}, err
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return committedPublication{}, err
	}
	domain := s.PublicationDomain
	if domain == "" {
		domain = anchor.PublicationDomain
	}
	markers, err := listCommitMarkers(ctx, driver, anchor, domain)
	if err != nil {
		return committedPublication{}, err
	}
	var found *committedPublication
	for i := range markers {
		if markers[i].Commit.SnapshotRef != snapshotRef {
			continue
		}
		preparedBytes, err := readRecord(ctx, driver, repository.RecordPreparedClosure, markers[i].Commit.PreparedObjectDigest)
		if err != nil {
			return committedPublication{}, err
		}
		var envelope PreparedClosureEnvelope
		if err := decodeStrictRecord(preparedBytes, &envelope); err != nil {
			return committedPublication{}, err
		}
		if err := validatePreparedEnvelope(driver, anchor, markers[i].Commit, envelope, int64(len(preparedBytes))); err != nil {
			return committedPublication{}, err
		}
		markers[i].Prepared = envelope
		markers[i].Manifest = envelope.Manifest
		if found != nil {
			return committedPublication{}, fmt.Errorf("conflicting committed publications for snapshot %s", snapshotRef)
		}
		copy := markers[i]
		found = &copy
	}
	if found == nil {
		return committedPublication{}, fmt.Errorf("%w: committed snapshot %s", repository.ErrNotFound, snapshotRef)
	}
	return *found, nil
}

// resolveManifestSubject resolves one subject by display-path walk over the
// authenticated manifest. Prefix components must be directories; the walk
// never follows symbolic links. Duplicate display paths fail closed so a
// tampered manifest cannot yield an ambiguous subject.
func resolveManifestSubject(manifest Manifest, displayPath string) (ManifestEntry, error) {
	path, err := normalizeSubjectPath(displayPath)
	if err != nil {
		return ManifestEntry{}, err
	}
	byDisplay := make(map[string]ManifestEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		rel := strings.Trim(entry.RelativePath, "/")
		if rel == "" || rel == "." {
			continue
		}
		if _, duplicate := byDisplay[rel]; duplicate {
			return ManifestEntry{}, fmt.Errorf("%w: manifest has duplicate display path %q", ErrRecoveryTokenInvalid, rel)
		}
		byDisplay[rel] = entry
	}
	var subject ManifestEntry
	for i := range path {
		prefix := strings.Join(path[:i+1], "/")
		entry, ok := byDisplay[prefix]
		if !ok {
			return ManifestEntry{}, fmt.Errorf("%w: subject %q", ErrNoRecoveryPath, displayPath)
		}
		if i < len(path)-1 {
			if sqlite.NamespaceEntryType(entry.EntryType) != sqlite.EntryDirectory {
				return ManifestEntry{}, fmt.Errorf("%w: subject path %q traverses a non-directory", ErrRecoveryTokenInvalid, displayPath)
			}
			continue
		}
		subject = entry
	}
	if subject.RelativePath == "" {
		return ManifestEntry{}, fmt.Errorf("%w: subject %q", ErrNoRecoveryPath, displayPath)
	}
	return subject, nil
}

func normalizeSubjectPath(displayPath string) ([]string, error) {
	if strings.TrimSpace(displayPath) == "" || strings.ContainsRune(displayPath, 0) {
		return nil, fmt.Errorf("%w: subject path is required", ErrRecoveryTokenInvalid)
	}
	trimmed := strings.Trim(displayPath, "/")
	if trimmed == "" || trimmed == "." {
		return nil, fmt.Errorf("%w: subject path is required", ErrRecoveryTokenInvalid)
	}
	var parts []string
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\x00\\") {
			return nil, fmt.Errorf("%w: subject path %q is unsafe", ErrRecoveryTokenInvalid, displayPath)
		}
		parts = append(parts, part)
	}
	return parts, nil
}
