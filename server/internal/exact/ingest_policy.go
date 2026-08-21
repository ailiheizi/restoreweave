package exact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/ailiheizi/restoreweave/server/internal/scanner"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// IngestLocator is one operator-supplied external route for a regular file.
// Path is the scanner-relative display path. It may be omitted only when the
// captured root contains exactly one regular file.
type IngestLocator struct {
	Path              string `json:"path,omitempty"`
	Kind              string `json:"kind,omitempty"`
	Locator           string `json:"locator"`
	DisplayLocator    string `json:"display_locator,omitempty"`
	CredentialRef     string `json:"credential_ref,omitempty"`
	RightsEvidenceRef string `json:"rights_evidence_ref,omitempty"`
}

// IngestOptions binds the explicit protection decision to one ingest. An
// empty ProtectionMode selects Service.DefaultProtection and then STORE_EXACT.
type IngestOptions struct {
	ProtectionMode sqlite.ProtectionMode `json:"protection_mode,omitempty"`
	// FileProtection overrides ProtectionMode for individual regular files.
	// Keys are scanner-relative paths using '/' separators. Files not listed
	// here inherit ProtectionMode (or the service default).
	FileProtection   map[string]sqlite.ProtectionMode `json:"file_protection,omitempty"`
	ConfirmLinkOnly  bool                             `json:"confirm_link_only,omitempty"`
	ExternalLocators []IngestLocator                  `json:"external_locators,omitempty"`
	// MetadataOnlyResolutions is an internal, plan-bound operator approval.
	// It is never inferred from a failed read or from the configured default
	// mode; callers must name each blocked path explicitly.
	MetadataOnlyResolutions []string `json:"metadata_only_resolutions,omitempty"`
}

// IngestProtectionDecision is the reviewable, per-file protection projection
// bound into an ingest plan. PlannedOutcome is the strongest outcome apply is
// allowed to publish after the exact placement and verification gates pass.
type IngestProtectionDecision struct {
	RelativePath         string                   `json:"relative_path"`
	RawPath              []byte                   `json:"raw_path"`
	Mode                 sqlite.ProtectionMode    `json:"mode"`
	PlannedOutcome       sqlite.ProtectionOutcome `json:"planned_outcome"`
	ReasonCode           string                   `json:"reason_code"`
	ExpectedContentID    string                   `json:"expected_content_id"`
	ExpectedLogicalBytes int64                    `json:"expected_logical_bytes"`
	LocatorCount         int                      `json:"locator_count"`
	LocatorBindingDigest string                   `json:"locator_binding_digest,omitempty"`
}

const (
	ProtectionReasonExactSelected              = "EXACT_SELECTED"
	ProtectionReasonContentClassUnresolved     = "CONTENT_CLASS_UNRESOLVED_EXACT_FALLBACK"
	ProtectionReasonDetectorUnavailable        = "PROCESSOR_UNAVAILABLE_EXACT_FALLBACK_USED"
	ProtectionReasonExternalLocatorUnvalidated = "EXTERNAL_LOCATOR_UNVALIDATED"
	ProtectionReasonMetadataOnlySelected       = "METADATA_ONLY_SELECTED"
	ProtectionReasonMetadataOnlyBlockedEntry   = "METADATA_ONLY_BLOCKED_ENTRY"
)

type ingestPolicy struct {
	mode                    sqlite.ProtectionMode
	fileModes               map[string]sqlite.ProtectionMode
	locators                []IngestLocator
	metadataOnlyResolutions map[string]struct{}
	// decisionDigest is populated once the capture has bound locators to
	// concrete paths. It is copied into durable policy references by adopt.
	decisionDigest string
}

func (p ingestPolicy) modeFor(relativePath string) sqlite.ProtectionMode {
	if mode, ok := p.fileModes[relativePath]; ok {
		return mode
	}
	return p.mode
}

func (s *Service) resolveIngestOptions(options IngestOptions) (ingestPolicy, error) {
	mode, err := normalizeProtectionMode(options.ProtectionMode)
	if err != nil {
		return ingestPolicy{}, err
	}
	if mode == "" {
		mode, err = normalizeProtectionMode(s.DefaultProtection)
		if err != nil {
			return ingestPolicy{}, fmt.Errorf("configured protection mode: %w", err)
		}
	}
	if mode == "" {
		mode = sqlite.ProtectionStoreExact
	}
	if mode == sqlite.ProtectionLinkOnly {
		if !s.AllowLinkOnly {
			return ingestPolicy{}, fmt.Errorf("%w: LINK_ONLY is disabled by the effective config", ErrBlocked)
		}
		if s.LinkOnlyRequiresConfirmation && !options.ConfirmLinkOnly {
			return ingestPolicy{}, fmt.Errorf("%w: LINK_ONLY requires explicit confirmation", ErrBlocked)
		}
	}
	fileModes := make(map[string]sqlite.ProtectionMode, len(options.FileProtection))
	for rawPath, rawMode := range options.FileProtection {
		relativePath, err := normalizeProtectionPath(rawPath)
		if err != nil {
			return ingestPolicy{}, fmt.Errorf("file protection %q: %w", rawPath, err)
		}
		fileMode, err := normalizeProtectionMode(rawMode)
		if err != nil {
			return ingestPolicy{}, fmt.Errorf("file protection %q: %w", rawPath, err)
		}
		if fileMode == "" {
			return ingestPolicy{}, fmt.Errorf("%w: file protection %q has an empty mode", ErrBlocked, rawPath)
		}
		if fileMode == sqlite.ProtectionLinkOnly {
			if !s.AllowLinkOnly {
				return ingestPolicy{}, fmt.Errorf("%w: LINK_ONLY is disabled by the effective config", ErrBlocked)
			}
			if s.LinkOnlyRequiresConfirmation && !options.ConfirmLinkOnly {
				return ingestPolicy{}, fmt.Errorf("%w: LINK_ONLY requires explicit confirmation", ErrBlocked)
			}
		}
		fileModes[relativePath] = fileMode
	}

	locators := make([]IngestLocator, len(options.ExternalLocators))
	for i, locator := range options.ExternalLocators {
		normalized, err := normalizeIngestLocator(locator)
		if err != nil {
			return ingestPolicy{}, fmt.Errorf("external locator %d: %w", i+1, err)
		}
		locators[i] = normalized
	}
	resolutions := make(map[string]struct{}, len(options.MetadataOnlyResolutions))
	for _, rawPath := range options.MetadataOnlyResolutions {
		relativePath, err := normalizeProtectionPath(rawPath)
		if err != nil {
			return ingestPolicy{}, fmt.Errorf("metadata-only resolution %q: %w", rawPath, err)
		}
		if _, exists := resolutions[relativePath]; exists {
			return ingestPolicy{}, fmt.Errorf("%w: duplicate metadata-only resolution %q", ErrBlocked, relativePath)
		}
		resolutions[relativePath] = struct{}{}
	}
	return ingestPolicy{mode: mode, fileModes: fileModes, locators: locators, metadataOnlyResolutions: resolutions}, nil
}

func normalizeProtectionPath(value string) (string, error) {
	value = strings.ReplaceAll(value, "\\", "/")
	if value == "" || value == "." {
		return "", fmt.Errorf("%w: path must name a regular file", ErrBlocked)
	}
	cleaned := path.Clean(value)
	if cleaned == "." || path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("path must be a safe path relative to the capture root")
	}
	return cleaned, nil
}

func normalizeProtectionMode(value sqlite.ProtectionMode) (sqlite.ProtectionMode, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(string(value)), "-", "_"))
	switch sqlite.ProtectionMode(normalized) {
	case "":
		return "", nil
	case sqlite.ProtectionStoreExact, sqlite.ProtectionStoreExactWithExternalFallback,
		sqlite.ProtectionLinkOnly, sqlite.ProtectionMetadataOnly:
		return sqlite.ProtectionMode(normalized), nil
	default:
		return "", fmt.Errorf("%w: unknown protection mode %q", ErrBlocked, value)
	}
}

func normalizeIngestLocator(locator IngestLocator) (IngestLocator, error) {
	locator.Path = strings.ReplaceAll(locator.Path, "\\", "/")
	if locator.Path != "" {
		cleaned := path.Clean(locator.Path)
		if cleaned == "." || path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return IngestLocator{}, errors.New("path must be a safe path relative to the capture root")
		}
		locator.Path = cleaned
	}
	if err := validateIngestLocatorControls("locator", locator.Locator); err != nil {
		return IngestLocator{}, err
	}
	locator.Locator = strings.TrimSpace(locator.Locator)
	if locator.Locator == "" {
		return IngestLocator{}, errors.New("locator is required")
	}
	if err := validateIngestLocatorValue("locator", locator.Locator, true); err != nil {
		return IngestLocator{}, err
	}

	if strings.TrimSpace(locator.Kind) == "" {
		parsed, _ := url.Parse(locator.Locator)
		locator.Kind = strings.ToUpper(parsed.Scheme)
	} else {
		locator.Kind = strings.ToUpper(strings.TrimSpace(locator.Kind))
	}
	if strings.TrimSpace(locator.DisplayLocator) == "" {
		locator.DisplayLocator = locator.Locator
	} else {
		if err := validateIngestLocatorControls("display_locator", locator.DisplayLocator); err != nil {
			return IngestLocator{}, err
		}
		locator.DisplayLocator = strings.TrimSpace(locator.DisplayLocator)
		if err := validateIngestLocatorValue("display_locator", locator.DisplayLocator, false); err != nil {
			return IngestLocator{}, err
		}
	}
	if locator.CredentialRef != "" {
		credential, err := url.Parse(locator.CredentialRef)
		if err != nil || credential.Scheme == "" || credential.User != nil {
			return IngestLocator{}, errors.New("credential_ref must be an opaque host reference with a URI scheme")
		}
	}
	for name, value := range map[string]string{
		"credential_ref":      locator.CredentialRef,
		"rights_evidence_ref": locator.RightsEvidenceRef,
	} {
		if strings.IndexByte(value, 0) >= 0 || strings.ContainsFunc(value, unicode.IsControl) {
			return IngestLocator{}, fmt.Errorf("%s contains a control character", name)
		}
	}
	return locator, nil
}

func validateIngestLocatorValue(field, value string, requireScheme bool) error {
	if err := validateIngestLocatorControls(field, value); err != nil {
		return err
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid URI: %w", field, err)
	}
	if requireScheme && strings.TrimSpace(parsed.Scheme) == "" {
		return errors.New("locator must have an explicit URI scheme")
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not contain embedded credentials; use credential_ref", field)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return fmt.Errorf("%s must not contain query parameters; use credential_ref for access material", field)
	}
	if strings.ContainsRune(value, '#') {
		return fmt.Errorf("%s must not contain a URI fragment; use credential_ref for access material", field)
	}
	return nil
}

func validateIngestLocatorControls(field, value string) error {
	if strings.IndexByte(value, 0) >= 0 || strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("%s contains a control character", field)
	}
	return nil
}

func bindIngestLocators(entries []scanner.EntryRecord, policy ingestPolicy) (map[string][]IngestLocator, error) {
	bound := make(map[string][]IngestLocator)
	readableRegular := make([]scanner.EntryRecord, 0)
	byPath := make(map[string]scanner.EntryRecord)
	allRegularByPath := make(map[string]scanner.EntryRecord)
	for _, entry := range entries {
		if entry.Kind != scanner.KindRegularFile {
			continue
		}
		if entry.State == scanner.EntryComplete && entry.Content != nil {
			readableRegular = append(readableRegular, entry)
			byPath[entry.RelativePath] = entry
			allRegularByPath[entry.RelativePath] = entry
			continue
		}
		if metadataOnlyResolutionQualified(scanner.CaptureModeRootedFD, entry, policy) {
			allRegularByPath[entry.RelativePath] = entry
		}
	}
	for _, locator := range policy.locators {
		if locator.Path == "" {
			if len(allRegularByPath) != 1 || len(readableRegular) != 1 {
				return nil, fmt.Errorf("%w: an unscoped locator is only valid when the capture has exactly one regular file", ErrBlocked)
			}
			bound[readableRegular[0].PathID] = append(bound[readableRegular[0].PathID], locator)
			continue
		}
		entry, ok := byPath[locator.Path]
		if !ok {
			return nil, fmt.Errorf("%w: locator path %q does not name a captured regular file", ErrBlocked, locator.Path)
		}
		bound[entry.PathID] = append(bound[entry.PathID], locator)
	}
	for _, entry := range readableRegular {
		mode := policy.modeFor(entry.RelativePath)
		locators := bound[entry.PathID]
		needsLocator := mode == sqlite.ProtectionLinkOnly || mode == sqlite.ProtectionStoreExactWithExternalFallback
		if needsLocator && len(locators) == 0 {
			return nil, fmt.Errorf("%w: %s has no external locator for protection mode %s", ErrBlocked, entry.RelativePath, mode)
		}
		if !needsLocator && len(locators) != 0 {
			return nil, fmt.Errorf("%w: %s has external locators but protection mode is %s", ErrBlocked, entry.RelativePath, mode)
		}
	}
	for path := range policy.fileModes {
		if _, found := allRegularByPath[path]; !found {
			return nil, fmt.Errorf("%w: file protection path %q does not name a captured regular file", ErrBlocked, path)
		}
	}
	return bound, nil
}

func locatorBindingDigest(contentID string, locators []IngestLocator) (string, error) {
	ordered := append([]IngestLocator(nil), locators...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Kind == ordered[j].Kind {
			return ordered[i].Locator < ordered[j].Locator
		}
		return ordered[i].Kind < ordered[j].Kind
	})
	payload, err := json.Marshal(struct {
		ContentID string          `json:"content_id"`
		Locators  []IngestLocator `json:"locators"`
	}{ContentID: contentID, Locators: ordered})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func buildProtectionDecisions(entries []scanner.EntryRecord, policy ingestPolicy, bound map[string][]IngestLocator) ([]IngestProtectionDecision, error) {
	return buildProtectionDecisionsWithResolutions(entries, policy, bound)
}

func buildProtectionDecisionsWithResolutions(entries []scanner.EntryRecord, policy ingestPolicy, bound map[string][]IngestLocator) ([]IngestProtectionDecision, error) {
	ordered := make([]IngestProtectionDecision, 0)
	for _, entry := range entries {
		if entry.Kind != scanner.KindRegularFile {
			continue
		}
		if entry.State != scanner.EntryComplete || entry.Content == nil {
			if !metadataOnlyResolutionQualified(scanner.CaptureModeRootedFD, entry, policy) {
				continue
			}
			size := int64(0)
			if entry.Before != nil {
				size = entry.Before.Size
			}
			ordered = append(ordered, IngestProtectionDecision{
				RelativePath: entry.RelativePath, RawPath: append([]byte(nil), entry.RawRelativePath...),
				Mode: sqlite.ProtectionMetadataOnly, PlannedOutcome: sqlite.ProtectionExplicitlyUnprotected,
				ReasonCode: ProtectionReasonMetadataOnlyBlockedEntry, ExpectedLogicalBytes: size,
			})
			continue
		}
		locators := append([]IngestLocator(nil), bound[entry.PathID]...)
		sort.SliceStable(locators, func(i, j int) bool {
			if locators[i].Kind == locators[j].Kind {
				return locators[i].Locator < locators[j].Locator
			}
			return locators[i].Kind < locators[j].Kind
		})
		locatorDigest := ""
		if len(locators) > 0 {
			var err error
			locatorDigest, err = locatorBindingDigest(entry.Content.ContentID, locators)
			if err != nil {
				return nil, err
			}
		}
		mode := policy.modeFor(entry.RelativePath)
		outcome, reason := protectionOutcome(entry, mode)
		rawPath := append([]byte(nil), entry.RawRelativePath...)
		if len(rawPath) == 0 {
			rawPath = []byte(entry.RelativePath)
		}
		ordered = append(ordered, IngestProtectionDecision{
			RelativePath: entry.RelativePath, RawPath: rawPath, Mode: mode,
			PlannedOutcome: outcome, ReasonCode: reason,
			ExpectedContentID: entry.Content.ContentID, ExpectedLogicalBytes: entry.Content.BytesRead,
			LocatorCount: len(locators), LocatorBindingDigest: locatorDigest,
		})
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if comparison := bytes.Compare(ordered[i].RawPath, ordered[j].RawPath); comparison != 0 {
			return comparison < 0
		}
		return ordered[i].RelativePath < ordered[j].RelativePath
	})
	return ordered, nil
}

func protectionOutcome(entry scanner.EntryRecord, mode sqlite.ProtectionMode) (sqlite.ProtectionOutcome, string) {
	switch mode {
	case sqlite.ProtectionStoreExact, sqlite.ProtectionStoreExactWithExternalFallback:
		if entry.Detection.State == scanner.DetectionFailed || entry.Detection.State == scanner.DetectionNotRequested {
			return sqlite.ProtectionExactFallback, ProtectionReasonDetectorUnavailable
		}
		if entry.Detection.Result.FormatID == "" {
			return sqlite.ProtectionExactFallback, ProtectionReasonContentClassUnresolved
		}
		return sqlite.ProtectionExactProtected, ProtectionReasonExactSelected
	case sqlite.ProtectionLinkOnly:
		return sqlite.ProtectionLinkOnlyUnprotected, ProtectionReasonExternalLocatorUnvalidated
	case sqlite.ProtectionMetadataOnly:
		return sqlite.ProtectionExplicitlyUnprotected, ProtectionReasonMetadataOnlySelected
	default:
		return sqlite.ProtectionBlocked, "PROTECTION_MODE_UNAVAILABLE"
	}
}

// protectionDecisionDigest is the immutable policy projection carried by an
// ingest plan and manifest. It binds identity, requested mode, planned
// outcome, reason, and external locator set for every captured regular file.
func protectionDecisionDigest(decisions []IngestProtectionDecision) (string, error) {
	payload, err := json.Marshal(struct {
		Schema    string                     `json:"schema"`
		Decisions []IngestProtectionDecision `json:"decisions"`
	}{Schema: "org.restoreweave.protection-decisions.v2", Decisions: decisions})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
