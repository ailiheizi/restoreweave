package exact

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	SubjectRef           string `json:"subject_ref"`
	RecoveryReferenceID  string `json:"recovery_reference_id"`
	ExpectedContentID    string `json:"expected_content_id,omitempty"`
	ExpectedLength       int64  `json:"expected_length,omitempty"`
	RecipeDigest         string `json:"recipe_digest,omitempty"`
	PublicationCommitRef string `json:"publication_commit_ref"`
	TrustAnchorRef       string `json:"trust_anchor_ref"`
	Expiry               string `json:"expiry,omitempty"`
	TokenDigest          string `json:"token_digest"`
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
	if token.ExpectedLength < 0 {
		return fmt.Errorf("%w: expected length cannot be negative", ErrRecoveryTokenInvalid)
	}
	if token.RecipeDigest != "" && !validExactContentID(token.RecipeDigest) {
		return fmt.Errorf("%w: recipe digest is invalid", ErrRecoveryTokenInvalid)
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

// BuildRecoveryToken derives the deterministic proof envelope for one subject
// of a committed snapshot without consulting SQLite. The subject is resolved
// by a display-path walk over the authenticated manifest; its recovery
// reference is the highest-priority reference embedded in the manifest
// protection closure. A subject with no recovery path (METADATA_ONLY and
// EXPLICITLY_UNPROTECTED) fails closed with ErrNoRecoveryPath.
func (s *Service) BuildRecoveryToken(ctx context.Context, snapshotRef, subjectPath string, anchor TrustAnchor) (RecoveryToken, error) {
	var token RecoveryToken
	if err := s.requireRepository(); err != nil {
		return token, err
	}
	if err := anchor.validate(); err != nil {
		return token, err
	}
	if strings.TrimSpace(snapshotRef) == "" {
		return token, fmt.Errorf("%w: snapshot reference is required", ErrRecoveryTokenInvalid)
	}
	if strings.TrimSpace(subjectPath) == "" {
		return token, fmt.Errorf("%w: subject path is required", ErrRecoveryTokenInvalid)
	}
	publication, err := s.committedPublicationWithAnchor(ctx, anchor, snapshotRef)
	if err != nil {
		return token, err
	}
	entry, err := resolveManifestSubject(publication.Manifest, subjectPath)
	if err != nil {
		return token, err
	}
	references := entry.Protection.RecoveryReferences
	if len(references) == 0 {
		return token, fmt.Errorf("%w: %s has protection outcome %s", ErrNoRecoveryPath, subjectPath, entry.Protection.Outcome)
	}
	reference := references[0]
	if strings.TrimSpace(reference.ReferenceID) == "" {
		return token, fmt.Errorf("%w: subject %s has an anonymous recovery reference", ErrRecoveryTokenInvalid, subjectPath)
	}
	anchorDigest, err := DigestCanonicalJSON(anchor)
	if err != nil {
		return token, err
	}
	recipeDigest := ""
	if len(reference.Recipe) > 0 {
		if !json.Valid(reference.Recipe) {
			return token, fmt.Errorf("%w: subject %s recovery recipe is invalid", ErrRecoveryTokenInvalid, subjectPath)
		}
		recipeDigest = DigestBytes(reference.Recipe)
	}
	expectedLength := int64(0)
	if entry.Protection.ExpectedLogicalLength != nil {
		expectedLength = *entry.Protection.ExpectedLogicalLength
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return token, err
	}
	domain := s.PublicationDomain
	if domain == "" {
		domain = anchor.PublicationDomain
	}
	subjectRef, err := s.stableSubjectRef(ctx, driver, anchor, domain, publication.CommitDigest, entry)
	if err != nil {
		return token, err
	}
	token = RecoveryToken{
		TokenSchema:          RecoveryTokenSchemaV1,
		SnapshotRef:          snapshotRef,
		SubjectRef:           subjectRef,
		RecoveryReferenceID:  reference.ReferenceID,
		ExpectedContentID:    entry.Protection.ExpectedContentID,
		ExpectedLength:       expectedLength,
		RecipeDigest:         recipeDigest,
		PublicationCommitRef: publication.CommitDigest,
		TrustAnchorRef:       anchorDigest,
	}
	digest, err := token.Digest()
	if err != nil {
		return token, err
	}
	token.TokenDigest = digest
	return token, nil
}

// stableSubjectRef resolves the portable stable subject identity for a
// manifest entry. When the signed portable-fact closure contains a matching
// SUBJECT_MAPPING record, its stable subject reference (the namespace entry
// ID) is authoritative. Otherwise a deterministic subject reference derived
// from the commit and raw path keeps the token derivable without a catalog.
func (s *Service) stableSubjectRef(ctx context.Context, driver repository.RecordDriver, anchor TrustAnchor, domain, commitDigest string, entry ManifestEntry) (string, error) {
	closures, err := listPortableFactClosures(ctx, s.Repo, driver, anchor, domain, commitDigest)
	if err == nil {
		for _, envelope := range closures {
			var bundle portableFactBundle
			if err := decodeStrictRecord(envelope.Bundle, &bundle); err != nil {
				continue
			}
			for _, record := range bundle.Records {
				if record.RecordKind != "SUBJECT_MAPPING" || strings.TrimSpace(record.StableSubjectRef) == "" {
					continue
				}
				var mapping subjectMappingPayload
				if err := decodeStrictRecord(record.Payload, &mapping); err != nil {
					continue
				}
				if bytes.Equal(mapping.RawPath, entry.RawPath) ||
					(len(entry.RawPath) == 0 && mapping.DisplayName == entry.RelativePath) {
					return record.StableSubjectRef, nil
				}
			}
		}
	}
	return "subject:" + commitDigest + ":" + hex.EncodeToString(entry.RawPath), nil
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
