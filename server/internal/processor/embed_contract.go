package processor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"
)

const (
	MaxEmbedTextSegments    = 4096
	MaxEmbedTextInputBytes  = 16 << 20
	MaxEmbedTextInputTokens = 1 << 20
	// MaxEmbedTextResourceBytes bounds the per-invocation scratch-memory
	// high-water mark reported by the worker. It deliberately excludes resident
	// model, runtime, and shared-library memory, which belong to the separately
	// admitted worker profile and host pool budget. Temporary disk has its own
	// host-enforced quota outside this value.
	MaxEmbedTextResourceBytes = 64 << 20
	MaxEmbedTextOutputBytes   = 64 << 20
	MaxEmbedTextDimension     = 8192
	// Control values are part of a bounded worker message even though they do
	// not count toward the text-input budget.
	MaxEmbedTextControlValueBytes  = 512
	MaxEmbedTextScopeBytes         = 4096
	MaxEmbedTextFailureReasonBytes = 4096
	// EmbedTextL2NormTolerance permits float32 rounding while still requiring
	// an accepted vector to be normalized to unit length.
	EmbedTextL2NormTolerance = 1e-3
	vectorElementBytes       = int64(4) // []float32 is the only admitted wire value.
)

// EmbedTextSegment is an immutable, host-issued reference. Text itself is
// deliberately not part of the contract: implementations receive it through
// a bounded host handle and cannot use ambient paths or index credentials.
type EmbedTextSegment struct {
	ID     string          `json:"segment_id"`
	Source EmbedTextSource `json:"source"`
	// DescriptionDocumentID identifies the immutable DescriptionDocument
	// revision that owns this segment. DescriptionDocument rows are append-only;
	// their numeric Revision is sequence metadata, not a second revision ID.
	DescriptionDocumentID string `json:"description_document_id"`
	Ordinal               int    `json:"ordinal"`
	SubjectRef            string `json:"subject_ref"`
	Language              string `json:"language"`
	TextHandleID          string `json:"text_handle_id"`
	TextDigest            string `json:"text_digest"`
	TextBytes             int64  `json:"text_bytes"`
}

type EmbedTextPurpose string

const (
	EmbedTextPurposeQuery    EmbedTextPurpose = "QUERY"
	EmbedTextPurposeDocument EmbedTextPurpose = "DOCUMENT"
)

type EmbedTextSourceKind string

const (
	EmbedTextSourceQuerySegment       EmbedTextSourceKind = "QUERY_TEXT"
	EmbedTextSourceDescriptionSegment EmbedTextSourceKind = "DESCRIPTION_SEGMENT"
)

// EmbedTextSource identifies the immutable source revision represented by the
// host-issued handles. It never contains source text or a filesystem path.
type EmbedTextSource struct {
	Kind     EmbedTextSourceKind `json:"kind"`
	Ref      string              `json:"ref"`
	Revision string              `json:"revision"`
}

// EmbedTextInvocationBinding is the host-owned execution identity. Query and
// document preprocessing are separate fields so a processor cannot silently
// reinterpret one purpose's preprocessing as the other.
type EmbedTextInvocationBinding struct {
	Purpose                    EmbedTextPurpose `json:"purpose"`
	Operation                  string           `json:"operation"`
	SessionID                  string           `json:"session_id"`
	OperationID                string           `json:"operation_id"`
	RequestID                  string           `json:"request_id"`
	InvocationID               string           `json:"invocation_id"`
	AttemptID                  string           `json:"attempt_id"`
	IdempotencyKey             string           `json:"idempotency_key"`
	LeaseID                    string           `json:"lease_id"`
	FenceToken                 int64            `json:"fence_token"`
	GenerationID               string           `json:"generation_id"`
	WorkerDigest               string           `json:"worker_digest"`
	WorkerProfileDigest        string           `json:"worker_profile_digest"`
	AppliedPreprocessingDigest string           `json:"applied_preprocessing_digest"`
}

// EmbedTextProfile binds every output-affecting fact that the host must see
// before admitting an embedding result. It is a contract value, not an
// embedding provider interface.
type EmbedTextProfile struct {
	SemanticProfileDigest       string `json:"semantic_profile_digest"`
	ConfigDigest                string `json:"config_digest"`
	QueryPreprocessingDigest    string `json:"query_preprocessing_digest"`
	DocumentPreprocessingDigest string `json:"document_preprocessing_digest"`
	ModelDigest                 string `json:"model_digest"`
	TokenizerDigest             string `json:"tokenizer_digest"`
	RuntimeDigest               string `json:"runtime_digest"`
	SemanticSpace               string `json:"semantic_space"`
	ElementType                 string `json:"element_type"`
	Dimension                   int    `json:"dimension"`
	Normalization               string `json:"normalization"`
	Pooling                     string `json:"pooling"`
	DeterminismClass            string `json:"determinism_class"`
}

const (
	EmbedTextNormalizationNone = "none"
	EmbedTextNormalizationL2   = "l2"

	EmbedTextPoolingMean = "mean"
	EmbedTextPoolingCLS  = "cls"
	EmbedTextPoolingMax  = "max"

	EmbedTextDeterminismByte      = "BYTE_DETERMINISTIC"
	EmbedTextDeterminismSemantic  = "SEMANTICALLY_DETERMINISTIC"
	EmbedTextDeterminismSeeded    = "SEEDED_STOCHASTIC"
	EmbedTextDeterminismOpaque    = "OPAQUE_NONDETERMINISTIC"
	EmbedTextResourceScopeScratch = "INVOCATION_SCRATCH_MEMORY"
	EmbedTextOperation            = "EMBED_TEXT"
)

// EmbedTextRequest is the bounded host-to-processor EMBED_TEXT contract.
// AuthorizationScope is descriptive binding data; authorization remains
// host-owned and is never delegated to the processor.
type EmbedTextRequest struct {
	Binding            EmbedTextInvocationBinding `json:"binding"`
	Segments           []EmbedTextSegment         `json:"segments"`
	Language           string                     `json:"language"`
	Profile            EmbedTextProfile           `json:"profile"`
	AuthorizationScope string                     `json:"authorization_scope"`
	EgressScope        string                     `json:"egress_scope"`
	MaxInputBytes      int64                      `json:"max_input_bytes"`
	MaxInputTokens     int64                      `json:"max_input_tokens"`
	// MaxResourceBytes and PeakResourceBytes below use ResourceScope. Resident
	// model/runtime memory is accounted for by worker admission and is not
	// silently included here.
	MaxResourceBytes int64  `json:"max_resource_bytes"`
	ResourceScope    string `json:"resource_scope"`
	MaxOutputBytes   int64  `json:"max_output_bytes"`
}

type EmbedTextOutcome string

type EmbedTextCoverage string

const (
	EmbedTextAccepted     EmbedTextOutcome = "ACCEPTED"
	EmbedTextInapplicable EmbedTextOutcome = "INAPPLICABLE"
	EmbedTextFailed       EmbedTextOutcome = "FAILED"

	EmbedTextCoverageFull      EmbedTextCoverage = "FULL"
	EmbedTextCoveragePartial   EmbedTextCoverage = "PARTIAL"
	EmbedTextCoverageTruncated EmbedTextCoverage = "TRUNCATED"
	EmbedTextCoverageNone      EmbedTextCoverage = "NONE"
)

// EmbedTextResult is one terminal outcome for exactly one requested segment.
// A processor cannot use a result to publish a generation or alter durable
// text; the host admits accepted vectors only after ValidateEmbedTextResult.
type EmbedTextResult struct {
	Binding               EmbedTextInvocationBinding `json:"binding"`
	SegmentID             string                     `json:"segment_id"`
	Source                EmbedTextSource            `json:"source"`
	DescriptionDocumentID string                     `json:"description_document_id"`
	Ordinal               int                        `json:"ordinal"`
	SubjectRef            string                     `json:"subject_ref"`
	Language              string                     `json:"language"`
	TextHandleID          string                     `json:"text_handle_id"`
	TextDigest            string                     `json:"text_digest"`
	Status                EmbedTextOutcome           `json:"status"`
	Vector                []float32                  `json:"vector,omitempty"`
	ElementType           string                     `json:"element_type,omitempty"`
	Dimension             int                        `json:"dimension,omitempty"`
	Normalization         string                     `json:"normalization,omitempty"`
	Pooling               string                     `json:"pooling,omitempty"`
	SemanticProfileDigest string                     `json:"semantic_profile_digest,omitempty"`
	ConfigDigest          string                     `json:"config_digest,omitempty"`
	PreprocessingDigest   string                     `json:"preprocessing_digest,omitempty"`
	ModelDigest           string                     `json:"model_digest,omitempty"`
	TokenizerDigest       string                     `json:"tokenizer_digest,omitempty"`
	RuntimeDigest         string                     `json:"runtime_digest,omitempty"`
	SemanticSpace         string                     `json:"semantic_space,omitempty"`
	DeterminismClass      string                     `json:"determinism_class,omitempty"`
	Coverage              EmbedTextCoverage          `json:"coverage"`
	InputTokens           int64                      `json:"input_tokens"`
	EmbeddedTokens        int64                      `json:"embedded_tokens"`
	FailureCode           string                     `json:"failure_code,omitempty"`
	Reason                string                     `json:"reason,omitempty"`
}

type EmbedTextResultBatch struct {
	Binding           EmbedTextInvocationBinding `json:"binding"`
	RequestDigest     string                     `json:"request_digest"`
	PeakResourceBytes int64                      `json:"peak_resource_bytes"`
	ResourceScope     string                     `json:"resource_scope"`
	Results           []EmbedTextResult          `json:"results"`
}

var (
	ErrInvalidEmbedTextRequest        = errors.New("invalid EMBED_TEXT request")
	ErrInvalidEmbedTextResult         = errors.New("invalid EMBED_TEXT result")
	ErrEmbedTextSegmentMissing        = errors.New("EMBED_TEXT result is missing a requested segment")
	ErrEmbedTextSegmentExtra          = errors.New("EMBED_TEXT result contains an extra segment")
	ErrEmbedTextSegmentDuplicate      = errors.New("EMBED_TEXT result contains a duplicate segment")
	ErrEmbedTextSubjectMismatch       = errors.New("EMBED_TEXT result subject does not match segment")
	ErrEmbedTextNonFinite             = errors.New("EMBED_TEXT vector contains NaN or infinity")
	ErrEmbedTextDimensionMismatch     = errors.New("EMBED_TEXT vector dimension does not match profile")
	ErrEmbedTextElementTypeMismatch   = errors.New("EMBED_TEXT element type does not match profile")
	ErrEmbedTextNormalizationMismatch = errors.New("EMBED_TEXT normalization does not match profile")
	ErrEmbedTextPoolingMismatch       = errors.New("EMBED_TEXT pooling does not match profile")
	ErrEmbedTextSemanticSpaceMismatch = errors.New("EMBED_TEXT semantic space does not match profile")
	ErrEmbedTextProfileMismatch       = errors.New("EMBED_TEXT semantic profile does not match request")
	ErrEmbedTextConfigMismatch        = errors.New("EMBED_TEXT config does not match request")
	ErrEmbedTextPreprocessingMismatch = errors.New("EMBED_TEXT preprocessing does not match request")
	ErrEmbedTextProvenanceMismatch    = errors.New("EMBED_TEXT model provenance does not match profile")
	ErrEmbedTextDeterminismMismatch   = errors.New("EMBED_TEXT determinism does not match profile")
	ErrEmbedTextTerminalOutcome       = errors.New("EMBED_TEXT result is not a terminal outcome")
	ErrEmbedTextDigest                = errors.New("EMBED_TEXT digest is not canonical SHA-256")
	ErrEmbedTextScope                 = errors.New("EMBED_TEXT authorization or egress scope is not canonical")
	ErrEmbedTextLanguageMismatch      = errors.New("EMBED_TEXT segment language does not match request")
	ErrEmbedTextHandle                = errors.New("EMBED_TEXT text handle is invalid")
	ErrEmbedTextBudget                = errors.New("EMBED_TEXT budget is invalid or exceeded")
	ErrEmbedTextDimensionLimit        = errors.New("EMBED_TEXT dimension exceeds host limit")
	ErrEmbedTextNorm                  = errors.New("EMBED_TEXT l2 vector norm is outside tolerance")
	ErrEmbedTextFailureCode           = errors.New("EMBED_TEXT failure code is not recognized")
	ErrEmbedTextCoverage              = errors.New("EMBED_TEXT coverage is incompatible with the outcome")
	ErrEmbedTextRequestMismatch       = errors.New("EMBED_TEXT result batch does not match the request")
	ErrEmbedTextSegmentBinding        = errors.New("EMBED_TEXT result does not match the requested segment")
	ErrEmbedTextDocumentBinding       = errors.New("EMBED_TEXT document maps to inconsistent subjects")
	ErrEmbedTextPurpose               = errors.New("EMBED_TEXT purpose is invalid")
	ErrEmbedTextSource                = errors.New("EMBED_TEXT source binding is invalid")
	ErrEmbedTextInvocationBinding     = errors.New("EMBED_TEXT invocation binding is invalid")
	ErrEmbedTextFence                 = errors.New("EMBED_TEXT fence token is invalid")
	ErrEmbedTextGeneration            = errors.New("EMBED_TEXT generation binding is invalid")
	ErrEmbedTextWorkerBinding         = errors.New("EMBED_TEXT worker binding is invalid")
)

func (p EmbedTextProfile) validate() error {
	missing := make([]string, 0, 12)
	for name, value := range map[string]string{
		"semantic_profile_digest":       p.SemanticProfileDigest,
		"config_digest":                 p.ConfigDigest,
		"query_preprocessing_digest":    p.QueryPreprocessingDigest,
		"document_preprocessing_digest": p.DocumentPreprocessingDigest,
		"model_digest":                  p.ModelDigest,
		"tokenizer_digest":              p.TokenizerDigest,
		"runtime_digest":                p.RuntimeDigest,
		"semantic_space":                p.SemanticSpace,
		"element_type":                  p.ElementType,
		"normalization":                 p.Normalization,
		"pooling":                       p.Pooling,
		"determinism_class":             p.DeterminismClass,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if p.Dimension <= 0 {
		missing = append(missing, "dimension")
	} else if p.Dimension > MaxEmbedTextDimension {
		return fmt.Errorf("%w: got %d, limit %d", ErrEmbedTextDimensionLimit, p.Dimension, MaxEmbedTextDimension)
	}
	if len(missing) != 0 {
		return fmt.Errorf("%w: missing profile fields %s", ErrInvalidEmbedTextRequest, strings.Join(missing, ", "))
	}
	for name, digest := range map[string]string{
		"semantic_profile_digest":       p.SemanticProfileDigest,
		"config_digest":                 p.ConfigDigest,
		"query_preprocessing_digest":    p.QueryPreprocessingDigest,
		"document_preprocessing_digest": p.DocumentPreprocessingDigest,
		"model_digest":                  p.ModelDigest,
		"tokenizer_digest":              p.TokenizerDigest,
		"runtime_digest":                p.RuntimeDigest,
	} {
		if err := ValidateEmbedTextDigest(digest); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrInvalidEmbedTextRequest, name, err)
		}
	}
	if p.ElementType != "float32" {
		return fmt.Errorf("%w: only float32 is admitted for []float32 results", ErrEmbedTextElementTypeMismatch)
	}
	if !validEmbedTextNormalization(p.Normalization) {
		return fmt.Errorf("%w: unsupported normalization %q", ErrEmbedTextNormalizationMismatch, p.Normalization)
	}
	if !validEmbedTextPooling(p.Pooling) {
		return fmt.Errorf("%w: unsupported pooling %q", ErrEmbedTextPoolingMismatch, p.Pooling)
	}
	if !validEmbedTextDeterminism(p.DeterminismClass) {
		return fmt.Errorf("%w: unsupported determinism class %q", ErrEmbedTextDeterminismMismatch, p.DeterminismClass)
	}
	if !validateEmbedTextAtom(p.SemanticSpace, MaxEmbedTextControlValueBytes) {
		return fmt.Errorf("%w: semantic_space is not canonical", ErrInvalidEmbedTextRequest)
	}
	return nil
}

func validEmbedTextNormalization(value string) bool {
	return value == EmbedTextNormalizationNone || value == EmbedTextNormalizationL2
}

func validEmbedTextPooling(value string) bool {
	switch value {
	case EmbedTextPoolingMean, EmbedTextPoolingCLS, EmbedTextPoolingMax:
		return true
	default:
		return false
	}
}

func validEmbedTextDeterminism(value string) bool {
	switch value {
	case EmbedTextDeterminismByte, EmbedTextDeterminismSemantic, EmbedTextDeterminismSeeded, EmbedTextDeterminismOpaque:
		return true
	default:
		return false
	}
}

// ValidateEmbedTextDigest accepts only the portable digest spelling used by
// host-owned bindings. Uppercase hex and alternate algorithms are rejected.
func ValidateEmbedTextDigest(value string) error {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return ErrEmbedTextDigest
	}
	for _, r := range value[len("sha256:"):] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ErrEmbedTextDigest
		}
	}
	return nil
}

func validateEmbedTextScope(value, kind, profileDigest string) error {
	prefix := kind + ":" + profileDigest + ":"
	if len(value) > MaxEmbedTextScopeBytes || !strings.HasPrefix(value, prefix) || len(value) <= len(prefix) || strings.TrimSpace(value) != value {
		return ErrEmbedTextScope
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return ErrEmbedTextScope
		}
	}
	return nil
}

func validateEmbedTextToken(value string) bool {
	if !validateEmbedTextAtom(value, MaxEmbedTextControlValueBytes) {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func validateEmbedTextAtom(value string, maxBytes int) bool {
	if len(value) == 0 || len(value) > maxBytes || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validateEmbedTextReason(value string) bool {
	if len(value) == 0 || len(value) > MaxEmbedTextFailureReasonBytes || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

func (b EmbedTextInvocationBinding) validate(profile EmbedTextProfile) error {
	switch b.Purpose {
	case EmbedTextPurposeQuery:
		if b.AppliedPreprocessingDigest != profile.QueryPreprocessingDigest {
			return ErrEmbedTextPurpose
		}
	case EmbedTextPurposeDocument:
		if b.AppliedPreprocessingDigest != profile.DocumentPreprocessingDigest {
			return ErrEmbedTextPurpose
		}
	default:
		return ErrEmbedTextPurpose
	}
	if ValidateEmbedTextDigest(b.AppliedPreprocessingDigest) != nil {
		return ErrEmbedTextPurpose
	}
	if b.Operation != EmbedTextOperation {
		return ErrEmbedTextInvocationBinding
	}
	if !validateEmbedTextToken(b.SessionID) || !validateEmbedTextToken(b.OperationID) ||
		!validateEmbedTextToken(b.RequestID) || !validateEmbedTextToken(b.InvocationID) ||
		!validateEmbedTextToken(b.AttemptID) || !validateEmbedTextToken(b.IdempotencyKey) ||
		!validateEmbedTextToken(b.LeaseID) {
		return ErrEmbedTextInvocationBinding
	}
	if b.FenceToken <= 0 {
		return ErrEmbedTextFence
	}
	if !validateEmbedTextToken(b.GenerationID) {
		return ErrEmbedTextGeneration
	}
	if err := ValidateEmbedTextDigest(b.WorkerDigest); err != nil {
		return ErrEmbedTextWorkerBinding
	}
	if err := ValidateEmbedTextDigest(b.WorkerProfileDigest); err != nil {
		return ErrEmbedTextWorkerBinding
	}
	return nil
}

func embedTextBindingsEqual(left, right EmbedTextInvocationBinding) bool {
	return left.Purpose == right.Purpose &&
		left.Operation == right.Operation && left.SessionID == right.SessionID &&
		left.OperationID == right.OperationID && left.RequestID == right.RequestID &&
		left.InvocationID == right.InvocationID && left.AttemptID == right.AttemptID &&
		left.IdempotencyKey == right.IdempotencyKey && left.LeaseID == right.LeaseID &&
		left.FenceToken == right.FenceToken && left.GenerationID == right.GenerationID &&
		left.WorkerDigest == right.WorkerDigest && left.WorkerProfileDigest == right.WorkerProfileDigest &&
		left.AppliedPreprocessingDigest == right.AppliedPreprocessingDigest
}

func ValidateEmbedTextRequest(req EmbedTextRequest) error {
	if err := req.Profile.validate(); err != nil {
		return err
	}
	if err := req.Binding.validate(req.Profile); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEmbedTextRequest, err)
	}
	if len(req.Segments) == 0 {
		return fmt.Errorf("%w: no segments", ErrInvalidEmbedTextRequest)
	}
	if len(req.Segments) > MaxEmbedTextSegments {
		return fmt.Errorf("%w: segment count exceeds %d", ErrEmbedTextBudget, MaxEmbedTextSegments)
	}
	if req.Binding.Purpose == EmbedTextPurposeQuery && len(req.Segments) != 1 {
		return fmt.Errorf("%w: QUERY requires exactly one input segment", ErrEmbedTextPurpose)
	}
	if !validateEmbedTextToken(req.Language) {
		return fmt.Errorf("%w: language is required", ErrInvalidEmbedTextRequest)
	}
	if err := validateEmbedTextScope(req.AuthorizationScope, "authz", req.Profile.SemanticProfileDigest); err != nil {
		return fmt.Errorf("%w: authorization_scope: %w", ErrInvalidEmbedTextRequest, err)
	}
	if err := validateEmbedTextScope(req.EgressScope, "egress", req.Profile.SemanticProfileDigest); err != nil {
		return fmt.Errorf("%w: egress_scope: %w", ErrInvalidEmbedTextRequest, err)
	}
	if req.MaxInputBytes <= 0 || req.MaxInputBytes > MaxEmbedTextInputBytes ||
		req.MaxInputTokens <= 0 || req.MaxInputTokens > MaxEmbedTextInputTokens ||
		req.MaxResourceBytes <= 0 || req.MaxResourceBytes > MaxEmbedTextResourceBytes ||
		req.MaxOutputBytes <= 0 || req.MaxOutputBytes > MaxEmbedTextOutputBytes {
		return fmt.Errorf("%w: positive bounded input, token, resource, and output budgets are required", ErrEmbedTextBudget)
	}
	if req.ResourceScope != EmbedTextResourceScopeScratch {
		return fmt.Errorf("%w: resource scope must be %q", ErrEmbedTextBudget, EmbedTextResourceScopeScratch)
	}
	var totalInputBytes int64
	type documentOrdinal struct {
		documentID string
		ordinal    int
	}
	seenOrdinals := make(map[documentOrdinal]struct{}, len(req.Segments))
	documentSubjects := make(map[string]string, len(req.Segments))
	seen := make(map[string]struct{}, len(req.Segments))
	for _, segment := range req.Segments {
		if !validateEmbedTextToken(segment.ID) || !validateEmbedTextToken(segment.Language) {
			return fmt.Errorf("%w: segment id and language are required", ErrInvalidEmbedTextRequest)
		}
		if segment.Ordinal < 0 || segment.TextBytes <= 0 || segment.TextBytes > MaxEmbedTextInputBytes {
			return fmt.Errorf("%w: invalid segment ordinal or text size", ErrInvalidEmbedTextRequest)
		}
		ordinalKey := documentOrdinal{documentID: segment.DescriptionDocumentID, ordinal: segment.Ordinal}
		if _, ok := seenOrdinals[ordinalKey]; ok {
			return fmt.Errorf("%w: document %q has duplicate ordinal %d", ErrEmbedTextSegmentDuplicate, segment.DescriptionDocumentID, segment.Ordinal)
		}
		seenOrdinals[ordinalKey] = struct{}{}
		if subject, ok := documentSubjects[segment.DescriptionDocumentID]; ok && subject != segment.SubjectRef {
			return fmt.Errorf("%w: document %q", ErrEmbedTextDocumentBinding, segment.DescriptionDocumentID)
		}
		documentSubjects[segment.DescriptionDocumentID] = segment.SubjectRef
		// Report structural document conflicts before checking the source
		// revision binding. This keeps duplicate ordinals and cross-subject
		// mappings diagnosable even when a forged segment also carries a stale
		// source revision.
		if req.Binding.Purpose == EmbedTextPurposeQuery {
			if segment.Source.Kind != EmbedTextSourceQuerySegment || segment.Source.Ref != segment.ID || segment.Source.Revision != segment.TextDigest ||
				segment.DescriptionDocumentID != "" || segment.SubjectRef != "" || segment.Ordinal != 0 {
				return fmt.Errorf("%w: QUERY segment must not carry durable description or subject references", ErrEmbedTextSource)
			}
		} else {
			if segment.Source.Kind != EmbedTextSourceDescriptionSegment || segment.Source.Ref != segment.ID ||
				!validateEmbedTextToken(segment.DescriptionDocumentID) || segment.Source.Revision != segment.DescriptionDocumentID ||
				!validateEmbedTextToken(segment.SubjectRef) {
				return fmt.Errorf("%w: DOCUMENT segment source does not match its durable revision", ErrEmbedTextSource)
			}
		}
		if segment.Language != req.Language {
			return fmt.Errorf("%w: segment %q", ErrEmbedTextLanguageMismatch, segment.ID)
		}
		if !validTextHandleID(segment.TextHandleID) {
			return fmt.Errorf("%w: segment %q", ErrEmbedTextHandle, segment.ID)
		}
		if err := ValidateEmbedTextDigest(segment.TextDigest); err != nil {
			return fmt.Errorf("%w: segment %q: %w", ErrInvalidEmbedTextRequest, segment.ID, err)
		}
		if totalInputBytes > req.MaxInputBytes-segment.TextBytes {
			return fmt.Errorf("%w: input bytes exceed budget", ErrEmbedTextBudget)
		}
		totalInputBytes += segment.TextBytes
		if _, ok := seen[segment.ID]; ok {
			return fmt.Errorf("%w: duplicate segment %q", ErrEmbedTextSegmentDuplicate, segment.ID)
		}
		seen[segment.ID] = struct{}{}
	}
	return nil
}

// CanonicalDigest binds a result batch to the complete host-issued request,
// including authorization, egress, profile, segment handles, and budgets.
func (req EmbedTextRequest) CanonicalDigest() (string, error) {
	if err := ValidateEmbedTextRequest(req); err != nil {
		return "", err
	}
	return canonicalEmbedTextRequestDigest(req)
}

func canonicalEmbedTextRequestDigest(req EmbedTextRequest) (string, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("%w: canonical request: %v", ErrInvalidEmbedTextRequest, err)
	}
	h := sha256.New()
	_, _ = h.Write([]byte("restoreweave.embed-text-request.v1\n"))
	_, _ = h.Write(payload)
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// ValidateEmbedTextResult performs host-side admission checks for a complete
// batch. It is intentionally independent of any index implementation.
func ValidateEmbedTextResult(req EmbedTextRequest, batch EmbedTextResultBatch) error {
	if err := ValidateEmbedTextRequest(req); err != nil {
		return err
	}
	if !embedTextBindingsEqual(batch.Binding, req.Binding) {
		return ErrEmbedTextRequestMismatch
	}
	wantRequestDigest, err := canonicalEmbedTextRequestDigest(req)
	if err != nil {
		return err
	}
	if err := ValidateEmbedTextDigest(batch.RequestDigest); err != nil || batch.RequestDigest != wantRequestDigest {
		return ErrEmbedTextRequestMismatch
	}
	if batch.PeakResourceBytes < 0 || batch.PeakResourceBytes > req.MaxResourceBytes {
		return fmt.Errorf("%w: peak resource bytes exceed budget", ErrEmbedTextBudget)
	}
	if batch.ResourceScope != req.ResourceScope {
		return fmt.Errorf("%w: resource scope does not match request", ErrEmbedTextBudget)
	}
	expected := make(map[string]EmbedTextSegment, len(req.Segments))
	for _, segment := range req.Segments {
		expected[segment.ID] = segment
	}
	seen := make(map[string]struct{}, len(batch.Results))
	var totalVectorBytes int64
	var totalInputTokens int64
	var totalEmbeddedTokens int64
	accepted := false
	for _, result := range batch.Results {
		segment, ok := expected[result.SegmentID]
		if !ok {
			return fmt.Errorf("%w: %q", ErrEmbedTextSegmentExtra, result.SegmentID)
		}
		if _, duplicate := seen[result.SegmentID]; duplicate {
			return fmt.Errorf("%w: %q", ErrEmbedTextSegmentDuplicate, result.SegmentID)
		}
		if !embedTextBindingsEqual(result.Binding, req.Binding) {
			return fmt.Errorf("%w: result binding", ErrEmbedTextRequestMismatch)
		}
		seen[result.SegmentID] = struct{}{}
		if result.SubjectRef != segment.SubjectRef {
			return fmt.Errorf("%w: segment %q", ErrEmbedTextSubjectMismatch, result.SegmentID)
		}
		if result.Status == EmbedTextAccepted {
			accepted = true
			bytes, ok := embedTextVectorBytes(len(result.Vector))
			if !ok || totalVectorBytes > req.MaxOutputBytes-bytes {
				return fmt.Errorf("%w: vector bytes exceed batch budget", ErrEmbedTextBudget)
			}
			totalVectorBytes += bytes
		}
		if result.InputTokens < 0 || result.InputTokens > MaxEmbedTextInputTokens ||
			result.EmbeddedTokens < 0 || result.EmbeddedTokens > result.InputTokens {
			return fmt.Errorf("%w: invalid per-segment token accounting", ErrEmbedTextBudget)
		}
		if totalInputTokens > req.MaxInputTokens-result.InputTokens {
			return fmt.Errorf("%w: input tokens exceed batch budget", ErrEmbedTextBudget)
		}
		totalInputTokens += result.InputTokens
		if totalEmbeddedTokens > req.MaxInputTokens-result.EmbeddedTokens {
			return fmt.Errorf("%w: embedded tokens exceed batch budget", ErrEmbedTextBudget)
		}
		totalEmbeddedTokens += result.EmbeddedTokens
		if err := validateEmbedTextResult(req, segment, result); err != nil {
			return fmt.Errorf("%w: segment %q: %w", ErrInvalidEmbedTextResult, result.SegmentID, err)
		}
	}
	if accepted && batch.PeakResourceBytes == 0 {
		return fmt.Errorf("%w: accepted batch lacks measured resource use", ErrEmbedTextBudget)
	}
	if len(seen) != len(expected) {
		for id := range expected {
			if _, ok := seen[id]; !ok {
				return fmt.Errorf("%w: %q", ErrEmbedTextSegmentMissing, id)
			}
		}
	}
	return nil
}

func embedTextVectorBytes(elements int) (int64, bool) {
	if elements < 0 || int64(elements) > math.MaxInt64/vectorElementBytes {
		return 0, false
	}
	return int64(elements) * vectorElementBytes, true
}

func (batch EmbedTextResultBatch) Validate(req EmbedTextRequest) error {
	return ValidateEmbedTextResult(req, batch)
}

func validateEmbedTextResult(req EmbedTextRequest, segment EmbedTextSegment, result EmbedTextResult) error {
	if !embedTextBindingsEqual(result.Binding, req.Binding) {
		return ErrEmbedTextRequestMismatch
	}
	profile := req.Profile
	for name, digest := range map[string]string{
		"text_digest":             result.TextDigest,
		"semantic_profile_digest": result.SemanticProfileDigest,
		"config_digest":           result.ConfigDigest,
		"preprocessing_digest":    result.PreprocessingDigest,
		"model_digest":            result.ModelDigest,
		"tokenizer_digest":        result.TokenizerDigest,
		"runtime_digest":          result.RuntimeDigest,
	} {
		if err := ValidateEmbedTextDigest(digest); err != nil {
			return fmt.Errorf("%w: result %s: %w", ErrInvalidEmbedTextResult, name, err)
		}
	}
	if result.Source != segment.Source || result.DescriptionDocumentID != segment.DescriptionDocumentID || result.Ordinal != segment.Ordinal ||
		result.Language != segment.Language || result.TextHandleID != segment.TextHandleID || result.TextDigest != segment.TextDigest {
		return ErrEmbedTextSegmentBinding
	}
	if result.SemanticProfileDigest != profile.SemanticProfileDigest {
		return ErrEmbedTextProfileMismatch
	}
	if result.ConfigDigest != profile.ConfigDigest {
		return ErrEmbedTextConfigMismatch
	}
	if result.PreprocessingDigest != req.Binding.AppliedPreprocessingDigest {
		return ErrEmbedTextPreprocessingMismatch
	}
	if result.SemanticSpace != profile.SemanticSpace {
		return ErrEmbedTextSemanticSpaceMismatch
	}
	if result.DeterminismClass != profile.DeterminismClass {
		return ErrEmbedTextDeterminismMismatch
	}
	if result.ModelDigest != profile.ModelDigest || result.TokenizerDigest != profile.TokenizerDigest || result.RuntimeDigest != profile.RuntimeDigest {
		return ErrEmbedTextProvenanceMismatch
	}
	if result.ElementType != profile.ElementType {
		return ErrEmbedTextElementTypeMismatch
	}
	if result.Dimension != profile.Dimension {
		return ErrEmbedTextDimensionMismatch
	}
	if result.Normalization != profile.Normalization {
		return ErrEmbedTextNormalizationMismatch
	}
	if result.Pooling != profile.Pooling {
		return ErrEmbedTextPoolingMismatch
	}
	if result.InputTokens < 0 || result.InputTokens > MaxEmbedTextInputTokens ||
		result.EmbeddedTokens < 0 || result.EmbeddedTokens > result.InputTokens {
		return fmt.Errorf("%w: invalid per-segment token accounting", ErrEmbedTextBudget)
	}
	switch result.Status {
	case EmbedTextAccepted:
		if len(result.Vector) == 0 {
			return fmt.Errorf("%w: accepted result has no vector", ErrInvalidEmbedTextResult)
		}
		if result.FailureCode != "" || result.Reason != "" {
			return fmt.Errorf("%w: accepted result carries failure data", ErrInvalidEmbedTextResult)
		}
		if result.EmbeddedTokens <= 0 {
			return fmt.Errorf("%w: accepted result processed no tokens", ErrEmbedTextBudget)
		}
		switch result.Coverage {
		case EmbedTextCoverageFull:
			if result.InputTokens != result.EmbeddedTokens {
				return ErrEmbedTextCoverage
			}
		case EmbedTextCoverageTruncated:
			if result.InputTokens <= result.EmbeddedTokens {
				return ErrEmbedTextCoverage
			}
		default:
			return ErrEmbedTextCoverage
		}
	case EmbedTextInapplicable, EmbedTextFailed:
		if !knownEmbedTextFailureCode(result.Status, result.FailureCode) || !validateEmbedTextReason(result.Reason) {
			return ErrEmbedTextFailureCode
		}
		if len(result.Vector) != 0 {
			return fmt.Errorf("%w: non-accepted result contains a vector", ErrInvalidEmbedTextResult)
		}
		if result.EmbeddedTokens != 0 {
			return ErrEmbedTextCoverage
		}
		if result.Status == EmbedTextInapplicable && result.Coverage != EmbedTextCoverageNone {
			return ErrEmbedTextCoverage
		}
		if result.Status == EmbedTextFailed && result.Coverage != EmbedTextCoverageNone && result.Coverage != EmbedTextCoveragePartial {
			return ErrEmbedTextCoverage
		}
		return nil
	default:
		return fmt.Errorf("%w: status %q", ErrEmbedTextTerminalOutcome, result.Status)
	}
	if result.Dimension != profile.Dimension || len(result.Vector) != profile.Dimension {
		return fmt.Errorf("%w: got declared=%d vector=%d want=%d", ErrEmbedTextDimensionMismatch, result.Dimension, len(result.Vector), profile.Dimension)
	}
	for _, value := range result.Vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return ErrEmbedTextNonFinite
		}
	}
	if profile.Normalization == "l2" {
		var sum float64
		for _, value := range result.Vector {
			sum += float64(value) * float64(value)
		}
		if math.Abs(math.Sqrt(sum)-1) > EmbedTextL2NormTolerance {
			return ErrEmbedTextNorm
		}
	}
	return nil
}

func knownEmbedTextFailureCode(status EmbedTextOutcome, code string) bool {
	switch status {
	case EmbedTextInapplicable:
		switch code {
		case "NO_ELIGIBLE_TEXT", "UNSUPPORTED_LANGUAGE":
			return true
		}
	case EmbedTextFailed:
		switch code {
		case "INPUT_TOO_LARGE", "PROVIDER_UNAVAILABLE", "RESOURCE_EXHAUSTED", "PROCESSOR_FAILURE":
			return true
		}
	}
	return false
}
