// Package identify classifies content from suffix and magic-byte evidence.
//
// It is the successor of the legacy server/internal/plugin builtin detector: the same
// original 39 suffix rules plus .epub/.md, and 25 magic rules, the same evidence-preserving conflict
// behavior, and the same deterministic classification, without the plugin
// manifest machinery and its 27-category model. The detector is host-owned:
// it receives a display name and a bounded probe captured by the scanner and
// never opens paths itself.
package identify

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
)

// IdentificationState is the host classification result. The values are the
// states the evidence classifier can actually produce; UNSUPPORTED and the
// other parser-driven states from the file-identification requirements are
// host-level states assigned later, not detector outputs.
type IdentificationState string

const (
	IdentificationIdentified          IdentificationState = "IDENTIFIED"
	IdentificationAmbiguous           IdentificationState = "AMBIGUOUS"
	IdentificationConflictingEvidence IdentificationState = "CONFLICTING_EVIDENCE"
	IdentificationUnknown             IdentificationState = "UNKNOWN"
)

// EvidenceKind is the evidence line a record belongs to. Suffix and magic
// lines are kept separate forever; a later stage never erases an earlier one.
type EvidenceKind string

const (
	EvidenceSuffix EvidenceKind = "SUFFIX"
	EvidenceMagic  EvidenceKind = "MAGIC"
)

// Confidence describes what a match means. Suffixes are heuristic hints;
// magic matches are deterministic rules, complete or partial.
type Confidence string

const (
	ConfidenceHeuristicHint     Confidence = "HEURISTIC_HINT"
	ConfidenceDeterministicRule Confidence = "DETERMINISTIC_RULE"
	ConfidencePartialRule       Confidence = "PARTIAL_RULE"
)

// ByteRange is one examined or missing region of the probe.
type ByteRange struct {
	Offset uint64 `json:"offset"`
	Length uint64 `json:"length"`
}

// DetectionEvidence is one immutable observation. The detector emits multiple
// records rather than overwriting a suffix/magic disagreement.
type DetectionEvidence struct {
	Kind                     EvidenceKind    `json:"kind"`
	Candidate                FormatCandidate `json:"candidate"`
	Confidence               Confidence      `json:"confidence"`
	ExaminedRanges           []ByteRange     `json:"examined_ranges,omitempty"`
	RequiredBytesUnavailable []ByteRange     `json:"required_bytes_unavailable,omitempty"`
	MatchRule                string          `json:"match_rule"`
	MatchDigest              string          `json:"match_digest"`
	Complete                 bool            `json:"complete"`
}

// IdentifyResult carries the classification and every piece of evidence it was
// derived from. Evidence is never deduplicated or overwritten.
type IdentifyResult struct {
	State    IdentificationState `json:"state"`
	Evidence []DetectionEvidence `json:"evidence"`
}

// Detector is the in-process suffix and magic-byte detector. It is
// deterministic: the same display name and probe always produce the same
// result and evidence order.
type Detector struct {
	// MaxProbeBytes caps how much of the probe may be examined. Zero or
	// negative means the whole probe is eligible.
	MaxProbeBytes int
}

// NewDetector returns a detector bounded to maxProbeBytes of probe input.
func NewDetector(maxProbeBytes int) *Detector {
	return &Detector{MaxProbeBytes: maxProbeBytes}
}

// Detect classifies one subject from its display name and probe bytes.
func (d *Detector) Detect(ctx context.Context, displayName string, probe []byte) (IdentifyResult, error) {
	if err := ctx.Err(); err != nil {
		return IdentifyResult{}, err
	}
	if d.MaxProbeBytes > 0 && len(probe) > d.MaxProbeBytes {
		probe = probe[:d.MaxProbeBytes]
	}
	evidence := suffixEvidence(displayName)
	evidence = append(evidence, magicEvidence(probe)...)
	return IdentifyResult{State: classifyEvidence(evidence), Evidence: evidence}, nil
}

// suffixEvidence emits one record per candidate for the longest matching
// suffix, if any. Suffix evidence is a heuristic hint, never proof.
func suffixEvidence(displayName string) []DetectionEvidence {
	suffix, candidates := matchSuffix(displayName)
	if suffix == "" {
		return nil
	}
	evidence := make([]DetectionEvidence, 0, len(candidates))
	for _, candidate := range candidates {
		evidence = append(evidence, DetectionEvidence{
			Kind:        EvidenceSuffix,
			Candidate:   candidate,
			Confidence:  ConfidenceHeuristicHint,
			MatchRule:   "suffix:" + suffix,
			MatchDigest: digestText("suffix:" + suffix + ":" + candidateKey(candidate)),
			Complete:    true,
		})
	}
	return evidence
}

// magicEvidence emits one record per matching magic rule, in table order. A
// truncated match is kept with its missing ranges made explicit.
func magicEvidence(probe []byte) []DetectionEvidence {
	var evidence []DetectionEvidence
	for _, rule := range magicRuleTable {
		match, ok := rule.match(probe)
		if !ok {
			continue
		}
		confidence := ConfidenceDeterministicRule
		if !match.complete {
			confidence = ConfidencePartialRule
		}
		evidence = append(evidence, DetectionEvidence{
			Kind:                     EvidenceMagic,
			Candidate:                rule.Candidate,
			Confidence:               confidence,
			ExaminedRanges:           match.examined,
			RequiredBytesUnavailable: match.unavailable,
			MatchRule:                "magic:" + rule.ID,
			MatchDigest:              rule.digest(),
			Complete:                 match.complete,
		})
	}
	return evidence
}

// classifyEvidence resolves the evidence set without merging or discarding
// any line. The rules are ported from the legacy detector: a single exact
// magic family agreeing with all suffix and partial families identifies the
// subject; any incompatible family marks a conflict or ambiguity.
func classifyEvidence(evidence []DetectionEvidence) IdentificationState {
	if len(evidence) == 0 {
		return IdentificationUnknown
	}

	exactMagicFamilies := make(map[string]struct{})
	exactMagicFormats := make(map[string]struct{})
	partialMagicFamilies := make(map[string]struct{})
	suffixFamilies := make(map[string]struct{})
	suffixFormats := make(map[string]struct{})
	for _, item := range evidence {
		switch item.Kind {
		case EvidenceMagic:
			if item.Complete {
				exactMagicFamilies[item.Candidate.comparisonFamily()] = struct{}{}
				exactMagicFormats[item.Candidate.FormatID] = struct{}{}
			} else {
				partialMagicFamilies[item.Candidate.comparisonFamily()] = struct{}{}
			}
		case EvidenceSuffix:
			suffixFamilies[item.Candidate.comparisonFamily()] = struct{}{}
			suffixFormats[item.Candidate.FormatID] = struct{}{}
		}
	}

	if len(exactMagicFamilies) == 0 {
		if len(partialMagicFamilies) > 0 || len(suffixFamilies) > 0 {
			return IdentificationAmbiguous
		}
		return IdentificationUnknown
	}
	if len(exactMagicFamilies) > 1 {
		return IdentificationAmbiguous
	}
	for suffixFamily := range suffixFamilies {
		if _, compatible := exactMagicFamilies[suffixFamily]; !compatible {
			return IdentificationConflictingEvidence
		}
	}
	for partialFamily := range partialMagicFamilies {
		if _, compatible := exactMagicFamilies[partialFamily]; !compatible {
			return IdentificationAmbiguous
		}
	}
	if len(suffixFormats) > 0 {
		if len(suffixFormats) != 1 || len(exactMagicFormats) != 1 {
			return IdentificationAmbiguous
		}
		for suffixFormat := range suffixFormats {
			if _, exact := exactMagicFormats[suffixFormat]; !exact {
				return IdentificationAmbiguous
			}
		}
	}
	return IdentificationIdentified
}

// matchSuffix returns the longest matching suffix and its candidates. Matching
// is case-insensitive against the base name only.
func matchSuffix(displayName string) (string, []FormatCandidate) {
	base := strings.ToLower(filepath.Base(displayName))
	for _, rule := range suffixRuleTable {
		if strings.HasSuffix(base, rule.Suffix) {
			return rule.Suffix, append([]FormatCandidate(nil), rule.Candidates...)
		}
	}
	return "", nil
}

type magicMatch struct {
	complete    bool
	examined    []ByteRange
	unavailable []ByteRange
	matched     int
}

// match runs the rule against the probe. A rule matches when at least one
// alternative matches. Truncated matches count only once they prove at least
// two bytes, and they keep their missing ranges explicit.
func (r MagicRule) match(probe []byte) (magicMatch, bool) {
	best := magicMatch{}
	found := false
	for _, alternative := range r.Alternatives {
		candidate, ok := matchMagicPattern(probe, alternative)
		if !ok {
			continue
		}
		if !found || candidate.complete && !best.complete ||
			candidate.complete == best.complete && candidate.matched > best.matched {
			best = candidate
			found = true
		}
	}
	return best, found
}

func matchMagicPattern(probe []byte, pattern MagicPattern) (magicMatch, bool) {
	const minimumPartialBytes = 2
	match := magicMatch{complete: true}
	for _, part := range pattern {
		available := 0
		if part.Offset < uint64(len(probe)) {
			remaining := uint64(len(probe)) - part.Offset
			if remaining < uint64(len(part.Pattern)) {
				available = int(remaining)
			} else {
				available = len(part.Pattern)
			}
		}
		if available > 0 {
			start := int(part.Offset)
			if !bytes.Equal(probe[start:start+available], part.Pattern[:available]) {
				return magicMatch{}, false
			}
			match.examined = append(match.examined, ByteRange{
				Offset: part.Offset, Length: uint64(available),
			})
			match.matched += available
		}
		if available < len(part.Pattern) {
			match.complete = false
			match.unavailable = append(match.unavailable, ByteRange{
				Offset: part.Offset + uint64(available),
				Length: uint64(len(part.Pattern) - available),
			})
			break
		}
	}
	if match.complete {
		return match, true
	}
	if match.matched < minimumPartialBytes {
		return magicMatch{}, false
	}
	return match, true
}
