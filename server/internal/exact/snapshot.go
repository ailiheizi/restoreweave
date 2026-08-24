package exact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
)

const (
	SnapshotSchemaV1       = "org.restoreweave.snapshot.v1"
	SnapshotSchemaV2       = "org.restoreweave.snapshot.v2"
	CurrentSnapshotSchema  = SnapshotSchemaV2
	PortableFactsSchemaV1  = "org.restoreweave.portable-file-facts.v1"
	portableFactsSetSchema = "org.restoreweave.portable-file-facts-set.v1"
	snapshotDirName        = "snapshots"
)

type PortableFactState string

const (
	PortableFactObserved      PortableFactState = "OBSERVED"
	PortableFactUnobserved    PortableFactState = "UNOBSERVED"
	PortableFactUnsupported   PortableFactState = "UNSUPPORTED"
	PortableFactRedacted      PortableFactState = "REDACTED"
	PortableFactInconsistent  PortableFactState = "INCONSISTENT"
	PortableFactNotApplicable PortableFactState = "NOT_APPLICABLE"
)

const (
	PortableFactSparseExtents    = "allocation.sparse-extents"
	PortableFactSparseIndication = "allocation.sparse-indication"
	PortableFactDetection        = "detection.primary"
	PortableFactACLs             = "extended.acls"
	PortableFactAlternateStreams = "extended.alternate-streams"
	PortableFactFlags            = "extended.flags"
	PortableFactResourceForks    = "extended.resource-forks"
	PortableFactXAttrs           = "extended.xattrs"
	PortableFactBoundary         = "link.boundary"
	PortableFactHardLink         = "link.hard-link"
)

var requiredPortableFactNames = [...]string{
	PortableFactSparseExtents,
	PortableFactSparseIndication,
	PortableFactDetection,
	PortableFactACLs,
	PortableFactAlternateStreams,
	PortableFactFlags,
	PortableFactResourceForks,
	PortableFactXAttrs,
	PortableFactBoundary,
	PortableFactHardLink,
}

// Manifest is the portable snapshot document stored beside CAS blobs.
// Recovery does not require the operational SQLite catalog.
type Manifest struct {
	Schema           string                `json:"schema"`
	SnapshotRef      string                `json:"snapshot_ref"`
	CreatedAt        time.Time             `json:"created_at"`
	Binding          capture.BindingRecord `json:"binding"`
	ConfigDigest     string                `json:"config_digest,omitempty"`
	ProtectionDigest string                `json:"protection_digest,omitempty"`
	ManifestDigest   string                `json:"manifest_digest,omitempty"`
	Entries          []ManifestEntry       `json:"entries"`
}

// ManifestEntry is one reconstructed namespace node.
type ManifestEntry struct {
	RelativePath    string              `json:"relative_path"`
	RawPath         []byte              `json:"raw_path"`
	RawName         []byte              `json:"raw_name,omitempty"`
	EntryType       string              `json:"entry_type"`
	ContentID       string              `json:"content_id,omitempty"`
	LogicalSize     *int64              `json:"logical_size,omitempty"`
	AllocatedSize   *int64              `json:"allocated_size,omitempty"`
	Mode            uint32              `json:"mode,omitempty"`
	MetadataBefore  json.RawMessage     `json:"metadata_before,omitempty"`
	MetadataAfter   json.RawMessage     `json:"metadata_after,omitempty"`
	HardlinkGroupID string              `json:"hardlink_group_id,omitempty"`
	ReadState       string              `json:"read_state,omitempty"`
	Issues          []ManifestIssue     `json:"issues,omitempty"`
	SymlinkTarget   []byte              `json:"symlink_target,omitempty"`
	Facts           *ManifestEntryFacts `json:"facts,omitempty"`
	Protection      ManifestProtection  `json:"protection"`
}

// ManifestEntryFacts is an ordered, portable projection of capture facts. A
// fact is present even when its state is unsupported or not applicable, so a
// clean reader never mistakes a missing capture capability for a zero value.
type ManifestEntryFacts struct {
	Schema string                 `json:"schema"`
	Facts  []ManifestPortableFact `json:"facts"`
}

// ManifestPortableFact carries one fact group and the provenance needed to
// interpret it independently from the operational catalog. Value is a
// schema-named JSON object whose exact bytes are covered by ProvenanceDigest
// and, transitively, by the signed prepared closure.
type ManifestPortableFact struct {
	Name             string            `json:"name"`
	State            PortableFactState `json:"state"`
	SourceProfile    string            `json:"source_profile"`
	Authority        string            `json:"authority"`
	CapturedAt       time.Time         `json:"capture_time"`
	CaptureTimeBasis string            `json:"capture_time_basis"`
	Value            json.RawMessage   `json:"value"`
	ProvenanceDigest string            `json:"provenance_digest"`
}

type PortableHardLinkValue struct {
	State          string `json:"state"`
	GroupIDVersion string `json:"group_id_version,omitempty"`
	GroupID        string `json:"group_id,omitempty"`
	LinkCount      uint64 `json:"link_count,omitempty"`
}

type PortableSparseIndicationValue struct {
	State          string `json:"state"`
	LogicalBytes   int64  `json:"logical_bytes,omitempty"`
	AllocatedBytes int64  `json:"allocated_bytes,omitempty"`
	Evidence       string `json:"evidence,omitempty"`
}

type PortableBoundaryValue struct {
	Checked bool   `json:"checked"`
	Action  string `json:"action"`
	Reason  string `json:"reason,omitempty"`
}

type PortableDetectionEvidence struct {
	Method string `json:"method"`
	Value  string `json:"value"`
}

type PortableDetectionValue struct {
	State           string                      `json:"state"`
	DetectorID      string                      `json:"detector_id,omitempty"`
	DetectorVersion string                      `json:"detector_version,omitempty"`
	FormatID        string                      `json:"format_id,omitempty"`
	MediaType       string                      `json:"media_type,omitempty"`
	Confidence      float64                     `json:"confidence,omitempty"`
	Evidence        []PortableDetectionEvidence `json:"evidence,omitempty"`
}

type PortableUnsupportedValue struct {
	ReasonCode      string `json:"reason_code"`
	CaptureReported bool   `json:"capture_reported,omitempty"`
}

type PortableXAttr struct {
	Name  string `json:"name"`
	Value []byte `json:"value"`
}

type PortableXAttrValue struct {
	State      string          `json:"state"`
	Attributes []PortableXAttr `json:"attributes"`
	ReasonCode string          `json:"reason_code,omitempty"`
}

type PortableACLRecord struct {
	Name string `json:"name"`
	Raw  []byte `json:"raw"`
}

type PortableACLValue struct {
	State      string              `json:"state"`
	Format     string              `json:"format,omitempty"`
	Records    []PortableACLRecord `json:"records"`
	ReasonCode string              `json:"reason_code,omitempty"`
}

func validPortableFactState(state PortableFactState) bool {
	switch state {
	case PortableFactObserved, PortableFactUnobserved, PortableFactUnsupported,
		PortableFactRedacted, PortableFactInconsistent, PortableFactNotApplicable:
		return true
	default:
		return false
	}
}

func (fact ManifestPortableFact) canonicalForDigest() ([]byte, error) {
	fact.ProvenanceDigest = ""
	fact.CapturedAt = fact.CapturedAt.UTC()
	return json.Marshal(fact)
}

func (fact ManifestPortableFact) Digest() (string, error) {
	payload, err := fact.canonicalForDigest()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (facts ManifestEntryFacts) validate() error {
	if facts.Schema != PortableFactsSchemaV1 {
		return fmt.Errorf("unsupported portable facts schema %q", facts.Schema)
	}
	if len(facts.Facts) != len(requiredPortableFactNames) {
		return fmt.Errorf("portable facts contain %d records, want %d", len(facts.Facts), len(requiredPortableFactNames))
	}
	for i, fact := range facts.Facts {
		if fact.Name != requiredPortableFactNames[i] {
			return fmt.Errorf("portable fact %d is %q, want %q", i, fact.Name, requiredPortableFactNames[i])
		}
		if !validPortableFactState(fact.State) {
			return fmt.Errorf("portable fact %q has invalid state %q", fact.Name, fact.State)
		}
		if strings.TrimSpace(fact.SourceProfile) == "" || strings.TrimSpace(fact.Authority) == "" ||
			fact.CapturedAt.IsZero() || strings.TrimSpace(fact.CaptureTimeBasis) == "" {
			return fmt.Errorf("portable fact %q lacks provenance", fact.Name)
		}
		if len(fact.Value) == 0 || !json.Valid(fact.Value) {
			return fmt.Errorf("portable fact %q has invalid value", fact.Name)
		}
		digest, err := fact.Digest()
		if err != nil {
			return err
		}
		if fact.ProvenanceDigest != digest {
			return fmt.Errorf("portable fact %q provenance digest mismatch", fact.Name)
		}
		if err := validatePortableFactValue(fact); err != nil {
			return fmt.Errorf("portable fact %q: %w", fact.Name, err)
		}
	}
	return nil
}

func decodePortableFactValue(payload json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("portable fact contains multiple JSON values")
		}
		return err
	}
	return nil
}

func validatePortableFactValue(fact ManifestPortableFact) error {
	unsupported := func(allowed ...PortableFactState) error {
		var value PortableUnsupportedValue
		if err := decodePortableFactValue(fact.Value, &value); err != nil {
			return err
		}
		if strings.TrimSpace(value.ReasonCode) == "" {
			return errors.New("unsupported fact lacks a reason code")
		}
		for _, state := range allowed {
			if fact.State == state {
				return nil
			}
		}
		return fmt.Errorf("state %q is invalid for this declared capability", fact.State)
	}

	switch fact.Name {
	case PortableFactSparseExtents:
		return unsupported(PortableFactUnsupported, PortableFactNotApplicable, PortableFactInconsistent)
	case PortableFactAlternateStreams, PortableFactFlags, PortableFactResourceForks:
		return unsupported(PortableFactUnsupported)
	case PortableFactXAttrs:
		var value PortableXAttrValue
		if err := decodePortableFactValue(fact.Value, &value); err != nil {
			return err
		}
		if value.State == "" {
			return validateLegacyUnsupportedFact(fact)
		}
		if err := validatePortableExtendedState(fact.State, value.State, value.ReasonCode, fact.Name); err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(value.Attributes))
		previousName := ""
		for _, attribute := range value.Attributes {
			if strings.TrimSpace(attribute.Name) == "" {
				return errors.New("xattr has an empty name")
			}
			if previousName != "" && attribute.Name <= previousName {
				return errors.New("xattrs are not in stable name order")
			}
			if _, exists := seen[attribute.Name]; exists {
				return fmt.Errorf("xattr %q is duplicated", attribute.Name)
			}
			seen[attribute.Name] = struct{}{}
			previousName = attribute.Name
		}
		return nil
	case PortableFactACLs:
		var value PortableACLValue
		if err := decodePortableFactValue(fact.Value, &value); err != nil {
			return err
		}
		if value.State == "" {
			return validateLegacyUnsupportedFact(fact)
		}
		if err := validatePortableExtendedState(fact.State, value.State, value.ReasonCode, fact.Name); err != nil {
			return err
		}
		if value.State == "OBSERVED" && strings.TrimSpace(value.Format) == "" {
			return errors.New("observed ACL lacks a format profile")
		}
		seen := make(map[string]struct{}, len(value.Records))
		previousName := ""
		for _, record := range value.Records {
			if strings.TrimSpace(record.Name) == "" || len(record.Raw) == 0 {
				return errors.New("ACL record is incomplete")
			}
			if previousName != "" && record.Name <= previousName {
				return errors.New("ACL records are not in stable name order")
			}
			if _, exists := seen[record.Name]; exists {
				return fmt.Errorf("ACL record %q is duplicated", record.Name)
			}
			seen[record.Name] = struct{}{}
			previousName = record.Name
		}
		return nil
	case PortableFactSparseIndication:
		var value PortableSparseIndicationValue
		if err := decodePortableFactValue(fact.Value, &value); err != nil {
			return err
		}
		if value.LogicalBytes < 0 || value.AllocatedBytes < 0 {
			return errors.New("sparse byte counts cannot be negative")
		}
		var want PortableFactState
		switch value.State {
		case "NOT_APPLICABLE":
			want = PortableFactNotApplicable
		case "UNKNOWN", "UNRECORDED":
			want = PortableFactUnobserved
		case "NOT_INDICATED", "ALLOCATION_BELOW_LOGICAL_SIZE":
			want = PortableFactObserved
		default:
			return fmt.Errorf("unknown sparse state %q", value.State)
		}
		if fact.State != want {
			return fmt.Errorf("state %q conflicts with sparse state %q", fact.State, value.State)
		}
		return nil
	case PortableFactBoundary:
		var value PortableBoundaryValue
		if err := decodePortableFactValue(fact.Value, &value); err != nil {
			return err
		}
		if value.Action != "INCLUDE" && value.Action != "SKIP" && value.Action != "UNRECORDED" {
			return fmt.Errorf("unknown boundary action %q", value.Action)
		}
		if value.Checked && fact.State != PortableFactObserved {
			return errors.New("checked boundary must be observed")
		}
		if !value.Checked && fact.State != PortableFactUnobserved {
			return errors.New("unchecked boundary must be unobserved")
		}
		return nil
	case PortableFactHardLink:
		var value PortableHardLinkValue
		if err := decodePortableFactValue(fact.Value, &value); err != nil {
			return err
		}
		var want PortableFactState
		switch value.State {
		case "NOT_APPLICABLE":
			want = PortableFactNotApplicable
		case "UNKNOWN", "UNRECORDED":
			want = PortableFactUnobserved
		case "SINGLE_LINK", "MULTIPLE_LINKS":
			want = PortableFactObserved
		default:
			return fmt.Errorf("unknown hard-link state %q", value.State)
		}
		if fact.State != want {
			return fmt.Errorf("state %q conflicts with hard-link state %q", fact.State, value.State)
		}
		if value.State == "MULTIPLE_LINKS" && (value.GroupID == "" || value.GroupIDVersion == "" || value.LinkCount < 2) {
			return errors.New("multiple hard links require a group, version, and link count")
		}
		return nil
	case PortableFactDetection:
		var value PortableDetectionValue
		if err := decodePortableFactValue(fact.Value, &value); err != nil {
			return err
		}
		if value.Confidence < 0 || value.Confidence > 1 {
			return errors.New("detection confidence is outside [0,1]")
		}
		switch value.State {
		case "SUCCEEDED":
			if fact.State != PortableFactObserved || value.DetectorID == "" || value.DetectorVersion == "" {
				return errors.New("successful detection lacks observed detector provenance")
			}
		case "FAILED":
			if fact.State != PortableFactObserved {
				return errors.New("failed detection must remain observed failure evidence")
			}
		case "NOT_REQUESTED", "UNRECORDED":
			if fact.State != PortableFactUnobserved && fact.State != PortableFactNotApplicable {
				return errors.New("not-requested detection must be unobserved or not applicable")
			}
		default:
			return fmt.Errorf("unknown detection state %q", value.State)
		}
		return nil
	default:
		return fmt.Errorf("unknown portable fact name %q", fact.Name)
	}
}

func validateLegacyUnsupportedFact(fact ManifestPortableFact) error {
	var value PortableUnsupportedValue
	if err := decodePortableFactValue(fact.Value, &value); err != nil {
		return err
	}
	if fact.State != PortableFactUnsupported || strings.TrimSpace(value.ReasonCode) == "" {
		return errors.New("legacy unsupported fact is not explicitly degraded")
	}
	return nil
}

func validatePortableExtendedState(factState PortableFactState, valueState, reason, name string) error {
	if strings.TrimSpace(reason) == "" && valueState != "OBSERVED" {
		return fmt.Errorf("%s degraded state lacks a reason code", name)
	}
	switch valueState {
	case "OBSERVED":
		if factState != PortableFactObserved {
			return fmt.Errorf("%s observed value has fact state %q", name, factState)
		}
	case "UNOBSERVED":
		if factState != PortableFactUnobserved {
			return fmt.Errorf("%s unobserved value has fact state %q", name, factState)
		}
	case "UNSUPPORTED":
		if factState != PortableFactUnsupported {
			return fmt.Errorf("%s unsupported value has fact state %q", name, factState)
		}
	case "INCONSISTENT":
		if factState != PortableFactInconsistent {
			return fmt.Errorf("%s inconsistent value has fact state %q", name, factState)
		}
	default:
		return fmt.Errorf("%s has unknown state %q", name, valueState)
	}
	return nil
}

func validateManifestFacts(manifest Manifest) error {
	if manifest.Schema != SnapshotSchemaV2 {
		return nil
	}
	for _, entry := range manifest.Entries {
		if entry.Facts == nil {
			return fmt.Errorf("manifest entry %q lacks portable facts", entry.RelativePath)
		}
		if err := entry.Facts.validate(); err != nil {
			return fmt.Errorf("manifest entry %q: %w", entry.RelativePath, err)
		}
	}
	return nil
}

func manifestFactsDigest(manifest Manifest) (string, error) {
	if err := validateManifestFacts(manifest); err != nil {
		return "", err
	}
	record := struct {
		Schema           string                `json:"schema"`
		SnapshotSchema   string                `json:"snapshot_schema"`
		SnapshotRef      string                `json:"snapshot_ref"`
		CreatedAt        time.Time             `json:"created_at"`
		Binding          capture.BindingRecord `json:"binding"`
		ConfigDigest     string                `json:"config_digest,omitempty"`
		ProtectionDigest string                `json:"protection_digest,omitempty"`
		Entries          []ManifestEntry       `json:"entries"`
	}{
		Schema: portableFactsSetSchema, SnapshotSchema: manifest.Schema,
		SnapshotRef: manifest.SnapshotRef, CreatedAt: manifest.CreatedAt.UTC(), Binding: manifest.Binding,
		ConfigDigest: manifest.ConfigDigest, ProtectionDigest: manifest.ProtectionDigest,
		Entries: manifest.Entries,
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ManifestIssue preserves a scanner warning without making the portable
// format depend on the scanner package's Go representation.
type ManifestIssue struct {
	Stage   string `json:"stage"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// ManifestProtection is the portable, catalog-free protection closure for an
// entry. RecoveryReferences are descriptive routes; large encoded payloads
// remain in the repository and are addressed by RepresentationID.
type ManifestProtection struct {
	RecordID              string                      `json:"record_id"`
	Mode                  string                      `json:"mode"`
	Outcome               string                      `json:"outcome"`
	ReasonCode            string                      `json:"reason_code,omitempty"`
	ExpectedContentID     string                      `json:"expected_content_id,omitempty"`
	ExpectedLogicalLength *int64                      `json:"expected_logical_length,omitempty"`
	LocalRepresentationID string                      `json:"local_representation_id,omitempty"`
	RecoveryReferences    []ManifestRecoveryReference `json:"recovery_references,omitempty"`
}

type ManifestRecoveryReference struct {
	ReferenceID       string                    `json:"reference_id"`
	Kind              string                    `json:"kind"`
	Claim             string                    `json:"claim"`
	Priority          int64                     `json:"priority"`
	RepresentationID  string                    `json:"representation_id,omitempty"`
	ExternalBindingID string                    `json:"external_binding_id,omitempty"`
	ExternalLocators  []ManifestExternalLocator `json:"external_locators,omitempty"`
	CodecProfile      string                    `json:"codec_profile,omitempty"`
	Status            string                    `json:"status,omitempty"`
	Recipe            json.RawMessage           `json:"recipe,omitempty"`
	Verification      json.RawMessage           `json:"verification,omitempty"`
}

// ManifestExternalLocator is the credential-free portable projection of one
// external locator. Validation remains explicit and must never be inferred
// from the presence of a URL.
type ManifestExternalLocator struct {
	LocatorID             string `json:"locator_id"`
	Priority              int64  `json:"priority"`
	Kind                  string `json:"kind"`
	Locator               string `json:"locator"`
	DisplayLocator        string `json:"display_locator,omitempty"`
	ExpectedContentID     string `json:"expected_content_id,omitempty"`
	ExpectedLogicalLength int64  `json:"expected_logical_length"`
	Availability          string `json:"availability"`
	ValidationStatus      string `json:"validation_status"`
}

func (manifest Manifest) canonicalForDigest() ([]byte, error) {
	copy := manifest
	copy.ManifestDigest = ""
	encoded, err := json.Marshal(copy)
	if err != nil {
		return nil, err
	}
	// Normalize JSON string encoding before hashing so invalid UTF-8 display
	// paths have the same digest before and after persistence.
	var normalized Manifest
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, err
	}
	normalized.ManifestDigest = ""
	return json.Marshal(normalized)
}

func (manifest Manifest) Digest() (string, error) {
	payload, err := manifest.canonicalForDigest()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func snapshotPath(repoRoot, snapshotRef string) string {
	return filepath.Join(repoRoot, snapshotDirName, snapshotRef+".json")
}

func writeManifest(repoRoot string, manifest Manifest) (Manifest, error) {
	if manifest.Schema != SnapshotSchemaV1 && manifest.Schema != SnapshotSchemaV2 {
		return Manifest{}, fmt.Errorf("unsupported snapshot schema %q", manifest.Schema)
	}
	if err := validateManifestFacts(manifest); err != nil {
		return Manifest{}, fmt.Errorf("validate snapshot facts: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, snapshotDirName), 0o700); err != nil {
		return Manifest{}, err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return Manifest{}, err
	}
	manifest.ManifestDigest = digest
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	dest := snapshotPath(repoRoot, manifest.SnapshotRef)
	temp, err := os.CreateTemp(filepath.Join(repoRoot, snapshotDirName), "snap-*.json")
	if err != nil {
		return Manifest{}, err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if _, err := temp.Write(payload); err != nil {
		return Manifest{}, err
	}
	if err := temp.Sync(); err != nil {
		return Manifest{}, err
	}
	if err := temp.Close(); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(tempName, dest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func readManifest(repoRoot, snapshotRef string) (Manifest, error) {
	if strings.TrimSpace(snapshotRef) == "" {
		return Manifest{}, errors.New("snapshot ref is required")
	}
	payload, err := os.ReadFile(snapshotPath(repoRoot, snapshotRef))
	if err != nil {
		return Manifest{}, fmt.Errorf("read snapshot %s: %w", snapshotRef, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode snapshot %s: %w", snapshotRef, err)
	}
	if manifest.Schema != SnapshotSchemaV1 && manifest.Schema != SnapshotSchemaV2 {
		return Manifest{}, fmt.Errorf("unsupported snapshot schema %q", manifest.Schema)
	}
	if err := validateManifestFacts(manifest); err != nil {
		return Manifest{}, fmt.Errorf("validate snapshot %s facts: %w", snapshotRef, err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		return Manifest{}, err
	}
	if manifest.ManifestDigest != "" && manifest.ManifestDigest != digest {
		return Manifest{}, fmt.Errorf("snapshot %s digest mismatch", snapshotRef)
	}
	return manifest, nil
}

func listManifests(repoRoot string) ([]Manifest, error) {
	entries, err := os.ReadDir(filepath.Join(repoRoot, snapshotDirName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifests []Manifest
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		ref := strings.TrimSuffix(entry.Name(), ".json")
		manifest, err := readManifest(repoRoot, ref)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}
