package plugin

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type SubjectRef struct {
	ObservationID string `json:"observation_id"`
	ContentID     Digest `json:"content_id,omitempty"`
}

func (s SubjectRef) validate() error {
	if err := validateOpaqueID(s.ObservationID); err != nil {
		return fmt.Errorf("observation_id: %w", err)
	}
	if s.ContentID != "" {
		if err := s.ContentID.Validate(); err != nil {
			return fmt.Errorf("content_id: %w", err)
		}
	}
	return nil
}

type EntryPointRef struct {
	PackageID     string `json:"package_id"`
	PackageDigest Digest `json:"package_digest"`
	EntryPointID  string `json:"entry_point_id"`
	RulesDigest   Digest `json:"rules_digest,omitempty"`
	RuntimeDigest Digest `json:"runtime_digest"`
}

func (r EntryPointRef) validate() error {
	if err := validateStableID(r.PackageID); err != nil {
		return fmt.Errorf("package_id: %w", err)
	}
	if err := validateStableID(r.EntryPointID); err != nil {
		return fmt.Errorf("entry_point_id: %w", err)
	}
	if err := r.PackageDigest.Validate(); err != nil {
		return fmt.Errorf("package_digest: %w", err)
	}
	if err := r.RuntimeDigest.Validate(); err != nil {
		return fmt.Errorf("runtime_digest: %w", err)
	}
	if r.RulesDigest != "" {
		if err := r.RulesDigest.Validate(); err != nil {
			return fmt.Errorf("rules_digest: %w", err)
		}
	}
	return nil
}

type ByteRange struct {
	Offset uint64 `json:"offset"`
	Length uint64 `json:"length"`
}

func (r ByteRange) End() (uint64, error) {
	if r.Length > math.MaxUint64-r.Offset {
		return 0, errors.New("byte range overflows uint64")
	}
	return r.Offset + r.Length, nil
}

func (r ByteRange) validate(allowEmpty bool) error {
	if !allowEmpty && r.Length == 0 {
		return errors.New("byte range must not be empty")
	}
	_, err := r.End()
	return err
}

type ResourceUse struct {
	CPUTimeNanos uint64 `json:"cpu_time_nanos,omitempty"`
	PeakMemory   uint64 `json:"peak_memory_bytes,omitempty"`
	BytesRead    uint64 `json:"bytes_read,omitempty"`
	BytesWritten uint64 `json:"bytes_written,omitempty"`
	FilesOpened  uint64 `json:"files_opened,omitempty"`
}

type ExecutionMetadata struct {
	Class               ExecutionClass `json:"class"`
	StartedAt           time.Time      `json:"started_at"`
	FinishedAt          time.Time      `json:"finished_at"`
	Seed                *uint64        `json:"seed,omitempty"`
	Sampler             string         `json:"sampler,omitempty"`
	SandboxPolicyDigest Digest         `json:"sandbox_policy_digest"`
	ResourceUse         ResourceUse    `json:"resource_use"`
}

func (m ExecutionMetadata) validate() error {
	if !m.Class.valid() {
		return fmt.Errorf("unknown execution class %q", m.Class)
	}
	if m.StartedAt.IsZero() || m.FinishedAt.IsZero() {
		return errors.New("started_at and finished_at are required")
	}
	if m.FinishedAt.Before(m.StartedAt) {
		return errors.New("finished_at precedes started_at")
	}
	if err := m.SandboxPolicyDigest.Validate(); err != nil {
		return fmt.Errorf("sandbox_policy_digest: %w", err)
	}
	if m.Class == ExecutionSeededStochastic && m.Seed == nil {
		return errors.New("seeded stochastic execution requires a seed")
	}
	if m.Class != ExecutionSeededStochastic && m.Seed != nil {
		return errors.New("only seeded stochastic execution may record a seed")
	}
	return nil
}

type EvidenceKind string

const (
	EvidenceSuffix       EvidenceKind = "SUFFIX"
	EvidenceDeclaredMIME EvidenceKind = "DECLARED_MIME"
	EvidenceMagic        EvidenceKind = "MAGIC"
	EvidenceSignature    EvidenceKind = "SIGNATURE"
	EvidenceParser       EvidenceKind = "PARSER"
	EvidenceContext      EvidenceKind = "CONTEXT"
	EvidenceLearned      EvidenceKind = "LEARNED"
)

func (k EvidenceKind) valid() bool {
	switch k {
	case EvidenceSuffix, EvidenceDeclaredMIME, EvidenceMagic, EvidenceSignature,
		EvidenceParser, EvidenceContext, EvidenceLearned:
		return true
	default:
		return false
	}
}

type IdentificationState string

const (
	IdentificationIdentified          IdentificationState = "IDENTIFIED"
	IdentificationAmbiguous           IdentificationState = "AMBIGUOUS"
	IdentificationPolyglot            IdentificationState = "POLYGLOT"
	IdentificationConflictingEvidence IdentificationState = "CONFLICTING_EVIDENCE"
	IdentificationPartiallyParsed     IdentificationState = "PARTIALLY_PARSED"
	IdentificationEncryptedOrLocked   IdentificationState = "ENCRYPTED_OR_LOCKED"
	IdentificationUnsupported         IdentificationState = "UNSUPPORTED"
	IdentificationMalformed           IdentificationState = "MALFORMED"
	IdentificationUnknown             IdentificationState = "UNKNOWN"
)

func (s IdentificationState) valid() bool {
	switch s {
	case IdentificationIdentified, IdentificationAmbiguous, IdentificationPolyglot,
		IdentificationConflictingEvidence, IdentificationPartiallyParsed,
		IdentificationEncryptedOrLocked, IdentificationUnsupported,
		IdentificationMalformed, IdentificationUnknown:
		return true
	default:
		return false
	}
}

type FormatCandidate struct {
	FormatID     string `json:"format_id"`
	FamilyID     string `json:"family_id,omitempty"`
	VersionRange string `json:"version_range,omitempty"`
	MIME         string `json:"mime,omitempty"`
	Producer     string `json:"producer,omitempty"`
}

func (c FormatCandidate) validate() error {
	if err := validateStableID(c.FormatID); err != nil {
		return fmt.Errorf("format_id: %w", err)
	}
	if c.FamilyID != "" {
		if err := validateStableID(c.FamilyID); err != nil {
			return fmt.Errorf("family_id: %w", err)
		}
	}
	if strings.ContainsAny(c.MIME, "\x00\r\n") {
		return errors.New("mime contains a control character")
	}
	return nil
}

func (c FormatCandidate) comparisonFamily() string {
	if c.FamilyID != "" {
		return c.FamilyID
	}
	return c.FormatID
}

type ConfidenceSemantics string

const (
	ConfidenceDeterministicRule ConfidenceSemantics = "DETERMINISTIC_RULE"
	ConfidenceHeuristicHint     ConfidenceSemantics = "HEURISTIC_HINT"
	ConfidencePartialRule       ConfidenceSemantics = "PARTIAL_RULE"
	ConfidenceCalibrated        ConfidenceSemantics = "CALIBRATED_PROBABILITY"
	ConfidenceUncalibrated      ConfidenceSemantics = "UNCALIBRATED_SCORE"
)

type Confidence struct {
	Semantics         ConfidenceSemantics `json:"semantics"`
	Value             *float64            `json:"value,omitempty"`
	CalibrationDigest Digest              `json:"calibration_digest,omitempty"`
}

func (c Confidence) validate() error {
	switch c.Semantics {
	case ConfidenceDeterministicRule, ConfidenceHeuristicHint, ConfidencePartialRule:
		if c.Value != nil || c.CalibrationDigest != "" {
			return errors.New("rule and hint confidence must not carry a numeric score")
		}
	case ConfidenceCalibrated:
		if c.Value == nil || *c.Value < 0 || *c.Value > 1 {
			return errors.New("calibrated probability must be in [0,1]")
		}
		if err := c.CalibrationDigest.Validate(); err != nil {
			return fmt.Errorf("calibration_digest: %w", err)
		}
	case ConfidenceUncalibrated:
		if c.Value == nil {
			return errors.New("uncalibrated score requires a value")
		}
		if c.CalibrationDigest != "" {
			return errors.New("uncalibrated score must not claim calibration")
		}
	default:
		return fmt.Errorf("unknown confidence semantics %q", c.Semantics)
	}
	return nil
}

type IssueSeverity string

const (
	IssueInfo    IssueSeverity = "INFO"
	IssueWarning IssueSeverity = "WARNING"
	IssueError   IssueSeverity = "ERROR"
)

type EvidenceIssue struct {
	Code     string        `json:"code"`
	Severity IssueSeverity `json:"severity"`
	Message  string        `json:"message,omitempty"`
}

type ExaminedFact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (f ExaminedFact) validate() error {
	if err := validateStableID(f.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	if strings.ContainsAny(f.Value, "\x00\r\n") {
		return errors.New("value contains a control character")
	}
	return nil
}

func (i EvidenceIssue) validate() error {
	if err := validateStableID(strings.ToLower(i.Code)); err != nil {
		return fmt.Errorf("code: %w", err)
	}
	switch i.Severity {
	case IssueInfo, IssueWarning, IssueError:
	default:
		return fmt.Errorf("unknown severity %q", i.Severity)
	}
	if strings.ContainsAny(i.Message, "\x00") {
		return errors.New("message contains NUL")
	}
	return nil
}

// DetectionEvidence is one immutable observation. Detectors emit multiple
// records rather than overwriting an extension/magic/parser disagreement.
type DetectionEvidence struct {
	Subject                  SubjectRef        `json:"subject"`
	Detector                 EntryPointRef     `json:"detector"`
	Kind                     EvidenceKind      `json:"kind"`
	Candidate                FormatCandidate   `json:"candidate"`
	Confidence               Confidence        `json:"confidence"`
	ExaminedRanges           []ByteRange       `json:"examined_ranges,omitempty"`
	ExaminedFacts            []ExaminedFact    `json:"examined_facts,omitempty"`
	RequiredBytesUnavailable []ByteRange       `json:"required_bytes_unavailable,omitempty"`
	MatchRule                string            `json:"match_rule"`
	MatchDigest              Digest            `json:"match_digest"`
	Complete                 bool              `json:"complete"`
	Execution                ExecutionMetadata `json:"execution"`
	Issues                   []EvidenceIssue   `json:"issues,omitempty"`
}

func (e DetectionEvidence) Validate() error {
	if err := e.Subject.validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if err := e.Detector.validate(); err != nil {
		return fmt.Errorf("detector: %w", err)
	}
	if !e.Kind.valid() {
		return fmt.Errorf("unknown evidence kind %q", e.Kind)
	}
	if err := e.Candidate.validate(); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if err := e.Confidence.validate(); err != nil {
		return fmt.Errorf("confidence: %w", err)
	}
	for i, byteRange := range e.ExaminedRanges {
		if err := byteRange.validate(false); err != nil {
			return fmt.Errorf("examined_ranges[%d]: %w", i, err)
		}
	}
	for i, fact := range e.ExaminedFacts {
		if err := fact.validate(); err != nil {
			return fmt.Errorf("examined_facts[%d]: %w", i, err)
		}
	}
	for i, byteRange := range e.RequiredBytesUnavailable {
		if err := byteRange.validate(false); err != nil {
			return fmt.Errorf("required_bytes_unavailable[%d]: %w", i, err)
		}
	}
	if strings.TrimSpace(e.MatchRule) != e.MatchRule || e.MatchRule == "" {
		return errors.New("match_rule must be non-empty and trimmed")
	}
	if err := e.MatchDigest.Validate(); err != nil {
		return fmt.Errorf("match_digest: %w", err)
	}
	if e.Complete && len(e.RequiredBytesUnavailable) != 0 {
		return errors.New("complete evidence cannot have unavailable required bytes")
	}
	if err := e.Execution.validate(); err != nil {
		return fmt.Errorf("execution: %w", err)
	}
	for i, issue := range e.Issues {
		if err := issue.validate(); err != nil {
			return fmt.Errorf("issues[%d]: %w", i, err)
		}
	}
	return nil
}

type DetectionResult struct {
	State    IdentificationState `json:"state"`
	Evidence []DetectionEvidence `json:"evidence"`
}

func (r DetectionResult) Validate() error {
	if !r.State.valid() {
		return fmt.Errorf("unknown identification state %q", r.State)
	}
	for i, evidence := range r.Evidence {
		if err := evidence.Validate(); err != nil {
			return fmt.Errorf("evidence[%d]: %w", i, err)
		}
	}
	if len(r.Evidence) > 1 {
		subject := r.Evidence[0].Subject
		detector := r.Evidence[0].Detector
		for i := 1; i < len(r.Evidence); i++ {
			if r.Evidence[i].Subject != subject {
				return fmt.Errorf("evidence[%d] belongs to a different subject", i)
			}
			if r.Evidence[i].Detector != detector {
				return fmt.Errorf("evidence[%d] belongs to a different detector", i)
			}
		}
	}
	if r.State == IdentificationUnknown && len(r.Evidence) != 0 {
		return errors.New("UNKNOWN result must not contain positive evidence")
	}
	if r.State != IdentificationUnknown && len(r.Evidence) == 0 {
		return errors.New("non-UNKNOWN result requires evidence")
	}
	return nil
}

type DenominatorKind string

const (
	DenominatorExact     DenominatorKind = "EXACT"
	DenominatorEstimated DenominatorKind = "ESTIMATED"
	DenominatorUnknown   DenominatorKind = "UNKNOWN"
)

type CoverageCounter struct {
	Unit            string          `json:"unit"`
	Attempted       uint64          `json:"attempted"`
	Completed       uint64          `json:"completed"`
	Total           *uint64         `json:"total,omitempty"`
	DenominatorKind DenominatorKind `json:"denominator_kind"`
}

func (c CoverageCounter) validate() error {
	if strings.TrimSpace(c.Unit) != c.Unit || c.Unit == "" {
		return errors.New("unit must be non-empty and trimmed")
	}
	if c.Completed > c.Attempted {
		return errors.New("completed exceeds attempted")
	}
	switch c.DenominatorKind {
	case DenominatorExact, DenominatorEstimated:
		if c.Total == nil {
			return errors.New("known denominator requires total")
		}
		if c.Attempted > *c.Total {
			return errors.New("attempted exceeds total")
		}
	case DenominatorUnknown:
		if c.Total != nil {
			return errors.New("unknown denominator must not claim total")
		}
	default:
		return fmt.Errorf("unknown denominator kind %q", c.DenominatorKind)
	}
	return nil
}

type ParseCoverage struct {
	DetectionBytes  CoverageCounter `json:"detection_bytes"`
	StructuralBytes CoverageCounter `json:"structural_bytes"`
	Members         CoverageCounter `json:"members"`
	MetadataFields  CoverageCounter `json:"metadata_fields"`
	ContentRegions  CoverageCounter `json:"content_regions"`
	SemanticRegions CoverageCounter `json:"semantic_regions"`
	BlockedRegions  []BlockedRegion `json:"blocked_regions,omitempty"`
}

type BlockedRegion struct {
	Range  ByteRange `json:"range"`
	Reason string    `json:"reason"`
}

func (c ParseCoverage) validate() error {
	counters := []struct {
		name    string
		counter CoverageCounter
	}{
		{"detection_bytes", c.DetectionBytes}, {"structural_bytes", c.StructuralBytes},
		{"members", c.Members}, {"metadata_fields", c.MetadataFields},
		{"content_regions", c.ContentRegions}, {"semantic_regions", c.SemanticRegions},
	}
	for _, item := range counters {
		if err := item.counter.validate(); err != nil {
			return fmt.Errorf("%s: %w", item.name, err)
		}
	}
	for i, blocked := range c.BlockedRegions {
		if err := blocked.Range.validate(false); err != nil {
			return fmt.Errorf("blocked_regions[%d].range: %w", i, err)
		}
		if strings.TrimSpace(blocked.Reason) != blocked.Reason || blocked.Reason == "" {
			return fmt.Errorf("blocked_regions[%d].reason must be non-empty and trimmed", i)
		}
	}
	return nil
}

type ParseNode struct {
	ID            string            `json:"id"`
	ParentID      string            `json:"parent_id,omitempty"`
	Kind          string            `json:"kind"`
	DisplayName   string            `json:"display_name,omitempty"`
	ClaimedRanges []ByteRange       `json:"claimed_ranges,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

type VirtualMember struct {
	ID               string      `json:"id"`
	ParentID         string      `json:"parent_id"`
	RawPath          []byte      `json:"raw_path"`
	SafeDisplayPath  string      `json:"safe_display_path"`
	Ordinal          uint64      `json:"ordinal"`
	LogicalSize      uint64      `json:"logical_size"`
	AllocatedSize    uint64      `json:"allocated_size,omitempty"`
	ClaimedRanges    []ByteRange `json:"claimed_ranges,omitempty"`
	ContentDigest    Digest      `json:"content_digest,omitempty"`
	Encrypted        bool        `json:"encrypted,omitempty"`
	Sparse           bool        `json:"sparse,omitempty"`
	DuplicateName    bool        `json:"duplicate_name,omitempty"`
	NormalizationKey string      `json:"normalization_key,omitempty"`
}

// ParserEvidence is a parser view, not a global truth claim. Multiple parser
// views may coexist for ambiguous or polyglot inputs.
type ParserEvidence struct {
	Subject         SubjectRef          `json:"subject"`
	Parser          EntryPointRef       `json:"parser"`
	ViewID          string              `json:"view_id"`
	Format          FormatCandidate     `json:"format"`
	State           IdentificationState `json:"state"`
	Nodes           []ParseNode         `json:"nodes"`
	VirtualMembers  []VirtualMember     `json:"virtual_members,omitempty"`
	ClaimedRanges   []ByteRange         `json:"claimed_ranges,omitempty"`
	UnclaimedRanges []ByteRange         `json:"unclaimed_ranges,omitempty"`
	Coverage        ParseCoverage       `json:"coverage"`
	Execution       ExecutionMetadata   `json:"execution"`
	Issues          []EvidenceIssue     `json:"issues,omitempty"`
}

func (p ParserEvidence) Validate() error {
	if err := p.Subject.validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if err := p.Parser.validate(); err != nil {
		return fmt.Errorf("parser: %w", err)
	}
	if err := validateOpaqueID(p.ViewID); err != nil {
		return fmt.Errorf("view_id: %w", err)
	}
	if err := p.Format.validate(); err != nil {
		return fmt.Errorf("format: %w", err)
	}
	if !p.State.valid() || p.State == IdentificationUnknown {
		return fmt.Errorf("parser view has invalid state %q", p.State)
	}
	if err := validateRangeList(p.ClaimedRanges, "claimed_ranges"); err != nil {
		return err
	}
	if err := validateRangeList(p.UnclaimedRanges, "unclaimed_ranges"); err != nil {
		return err
	}
	for i, claimed := range p.ClaimedRanges {
		for j, unclaimed := range p.UnclaimedRanges {
			if byteRangesOverlap(claimed, unclaimed) {
				return fmt.Errorf("claimed_ranges[%d] overlaps unclaimed_ranges[%d]", i, j)
			}
		}
	}
	if err := p.Coverage.validate(); err != nil {
		return fmt.Errorf("coverage: %w", err)
	}
	if err := p.Execution.validate(); err != nil {
		return fmt.Errorf("execution: %w", err)
	}
	if err := validateParseNodes(p.Nodes); err != nil {
		return err
	}
	if err := validateVirtualMembers(p.VirtualMembers, p.Nodes); err != nil {
		return err
	}
	for i, issue := range p.Issues {
		if err := issue.validate(); err != nil {
			return fmt.Errorf("issues[%d]: %w", i, err)
		}
	}
	return nil
}

func validateRangeList(ranges []ByteRange, name string) error {
	for i, byteRange := range ranges {
		if err := byteRange.validate(false); err != nil {
			return fmt.Errorf("%s[%d]: %w", name, i, err)
		}
	}
	return nil
}

func byteRangesOverlap(left, right ByteRange) bool {
	leftEnd, leftErr := left.End()
	rightEnd, rightErr := right.End()
	if leftErr != nil || rightErr != nil {
		return false
	}
	return left.Offset < rightEnd && right.Offset < leftEnd
}

func validateParseNodes(nodes []ParseNode) error {
	if len(nodes) == 0 {
		return errors.New("nodes must not be empty")
	}
	ids := make(map[string]struct{}, len(nodes))
	for i, node := range nodes {
		if err := validateOpaqueID(node.ID); err != nil {
			return fmt.Errorf("nodes[%d].id: %w", i, err)
		}
		if _, duplicate := ids[node.ID]; duplicate {
			return fmt.Errorf("nodes[%d]: duplicate id %q", i, node.ID)
		}
		ids[node.ID] = struct{}{}
		if strings.TrimSpace(node.Kind) != node.Kind || node.Kind == "" {
			return fmt.Errorf("nodes[%d].kind must be non-empty and trimmed", i)
		}
		if err := validateRangeList(node.ClaimedRanges, fmt.Sprintf("nodes[%d].claimed_ranges", i)); err != nil {
			return err
		}
	}
	rootCount := 0
	for i, node := range nodes {
		if node.ParentID == "" {
			rootCount++
			continue
		}
		if _, ok := ids[node.ParentID]; !ok {
			return fmt.Errorf("nodes[%d].parent_id %q does not exist", i, node.ParentID)
		}
		if node.ParentID == node.ID {
			return fmt.Errorf("nodes[%d] cannot parent itself", i)
		}
	}
	if rootCount != 1 {
		return fmt.Errorf("parse tree must have exactly one root, got %d", rootCount)
	}
	return detectNodeCycle(nodes)
}

func detectNodeCycle(nodes []ParseNode) error {
	parents := make(map[string]string, len(nodes))
	for _, node := range nodes {
		parents[node.ID] = node.ParentID
	}
	for _, node := range nodes {
		seen := make(map[string]struct{})
		current := node.ID
		for current != "" {
			if _, duplicate := seen[current]; duplicate {
				return fmt.Errorf("parse tree contains a cycle at %q", current)
			}
			seen[current] = struct{}{}
			current = parents[current]
		}
	}
	return nil
}

func validateVirtualMembers(members []VirtualMember, nodes []ParseNode) error {
	parents := make(map[string]struct{}, len(nodes)+len(members))
	for _, node := range nodes {
		parents[node.ID] = struct{}{}
	}
	ids := make(map[string]struct{}, len(members))
	memberParents := make(map[string]string, len(members))
	for i, member := range members {
		if err := validateOpaqueID(member.ID); err != nil {
			return fmt.Errorf("virtual_members[%d].id: %w", i, err)
		}
		if _, duplicate := ids[member.ID]; duplicate {
			return fmt.Errorf("virtual_members[%d]: duplicate id %q", i, member.ID)
		}
		ids[member.ID] = struct{}{}
		memberParents[member.ID] = member.ParentID
		parents[member.ID] = struct{}{}
		if member.ContentDigest != "" {
			if err := member.ContentDigest.Validate(); err != nil {
				return fmt.Errorf("virtual_members[%d].content_digest: %w", i, err)
			}
		}
		if err := validateRangeList(member.ClaimedRanges, fmt.Sprintf("virtual_members[%d].claimed_ranges", i)); err != nil {
			return err
		}
	}
	for i, member := range members {
		if _, ok := parents[member.ParentID]; !ok || member.ParentID == member.ID {
			return fmt.Errorf("virtual_members[%d].parent_id %q is invalid", i, member.ParentID)
		}
	}
	for _, member := range members {
		seen := make(map[string]struct{})
		current := member.ID
		for current != "" {
			if _, duplicate := seen[current]; duplicate {
				return fmt.Errorf("virtual member tree contains a cycle at %q", current)
			}
			seen[current] = struct{}{}
			parent, isMember := memberParents[current]
			if !isMember {
				break
			}
			current = parent
		}
	}
	return nil
}

// SortedRanges returns a copy ordered by offset then length. It does not merge
// overlaps because overlaps are meaningful evidence for polyglot views.
func SortedRanges(ranges []ByteRange) []ByteRange {
	result := append([]ByteRange(nil), ranges...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Offset == result[j].Offset {
			return result[i].Length < result[j].Length
		}
		return result[i].Offset < result[j].Offset
	})
	return result
}
