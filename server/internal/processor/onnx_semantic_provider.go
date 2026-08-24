package processor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

// ONNXSemanticEmbeddingProvider adapts a negotiated, host-attested ONNX
// worker to the narrow semantic search provider contract. It owns no catalog,
// repository, or index authority: text is issued through the worker's
// host-owned handle store and only validated vectors cross this boundary.
//
// A provider can be constructed only from a READY worker. The daemon still
// keeps semantic search unavailable when no real worker has been admitted.
type ONNXSemanticEmbeddingProvider struct {
	worker NegotiatedONNXWorker
}

// NewONNXSemanticEmbeddingProvider returns a provider for one immutable
// admission. The worker's text-handle store is intentionally not replaceable;
// handles must be issued by the same host boundary used during negotiation.
func NewONNXSemanticEmbeddingProvider(worker NegotiatedONNXWorker) (*ONNXSemanticEmbeddingProvider, error) {
	if worker.Admission.Capability.State != ONNXWorkerStateReady || worker.runtime == nil || worker.textHandles == nil {
		return nil, fmt.Errorf("%w: negotiated worker is not READY", ErrONNXWorkerUnavailable)
	}
	if worker.Admission.Manifest.Validate() != nil || worker.Admission.ProfileDigest == "" {
		return nil, fmt.Errorf("%w: worker admission is incomplete", ErrONNXWorkerAdmission)
	}
	return &ONNXSemanticEmbeddingProvider{worker: worker}, nil
}

// Embed executes one bounded semantic request. Document inputs retain their
// durable description mapping; query inputs deliberately carry no subject or
// document identity. Any non-accepted terminal outcome fails the whole call,
// so a partial vector set can never create or query a generation.
func (p *ONNXSemanticEmbeddingProvider) Embed(ctx context.Context, req search.SemanticEmbeddingRequest) ([]search.SemanticVector, error) {
	if p == nil {
		return nil, search.ErrSemanticProviderUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", search.ErrSemanticProviderUnavailable, err)
	}
	if err := req.Manifest.Validate(); err != nil || req.Manifest != p.worker.Admission.Manifest {
		return nil, fmt.Errorf("%w: manifest does not match admitted worker", search.ErrSemanticProviderUnavailable)
	}
	if len(req.Inputs) == 0 {
		return nil, search.ErrSemanticProviderUnavailable
	}
	if req.Purpose != search.SemanticEmbeddingDocument && req.Purpose != search.SemanticEmbeddingQuery {
		return nil, search.ErrSemanticProviderUnavailable
	}
	if req.Purpose == search.SemanticEmbeddingQuery && len(req.Inputs) != 1 {
		return nil, search.ErrSemanticProviderUnavailable
	}

	sessionID, workerGenerationID, fenceToken := workerInvocationBinding(p.worker)
	generationID := strings.TrimSpace(req.GenerationID)
	if generationID == "" {
		return nil, fmt.Errorf("%w: generation binding is required", search.ErrSemanticProviderUnavailable)
	}
	if workerGenerationID != "" && workerGenerationID != generationID {
		return nil, fmt.Errorf("%w: worker generation does not match request", search.ErrSemanticProviderUnavailable)
	}
	binding, err := newONNXProviderBinding(p.worker.Admission, req.Purpose, sessionID, generationID, fenceToken)
	if err != nil {
		return nil, err
	}
	language := "und"
	if req.Purpose == search.SemanticEmbeddingDocument {
		language = strings.TrimSpace(req.Inputs[0].Language)
		if language == "" {
			language = "und"
		}
	}
	segments := make([]EmbedTextSegment, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		if strings.TrimSpace(input.SegmentID) == "" || strings.TrimSpace(input.Text) == "" {
			return nil, fmt.Errorf("%w: semantic input identity/text is missing", search.ErrSemanticProviderUnavailable)
		}
		inputLanguage := language
		if req.Purpose == search.SemanticEmbeddingDocument {
			inputLanguage = strings.TrimSpace(input.Language)
			if inputLanguage == "" {
				inputLanguage = "und"
			}
			if inputLanguage != language {
				return nil, fmt.Errorf("%w: document inputs must share one language", search.ErrSemanticProviderUnavailable)
			}
			if strings.TrimSpace(input.DescriptionDocumentID) == "" || strings.TrimSpace(input.SubjectID) == "" || input.Ordinal < 0 || input.Ordinal > int64(^uint(0)>>1) {
				return nil, fmt.Errorf("%w: document provenance is incomplete", search.ErrSemanticProviderUnavailable)
			}
		}
		textDigest := digestText([]byte(input.Text))
		segment := EmbedTextSegment{
			ID: input.SegmentID, Ordinal: int(input.Ordinal), SubjectRef: input.SubjectID,
			DescriptionDocumentID: input.DescriptionDocumentID, Language: inputLanguage,
			TextDigest: textDigest, Source: EmbedTextSource{Ref: input.SegmentID},
		}
		if req.Purpose == search.SemanticEmbeddingQuery {
			segment.Source = EmbedTextSource{Kind: EmbedTextSourceQuerySegment, Ref: input.SegmentID, Revision: textDigest}
		} else {
			segment.Source = EmbedTextSource{Kind: EmbedTextSourceDescriptionSegment, Ref: input.SegmentID, Revision: input.DescriptionDocumentID}
		}
		textHandle, issueErr := p.worker.textHandles.Issue(ctx, binding, []byte(input.Text))
		if issueErr != nil {
			return nil, fmt.Errorf("%w: issue text handle: %v", search.ErrSemanticProviderUnavailable, issueErr)
		}
		segment.TextHandleID = textHandle.ID
		segment.TextBytes = textHandle.Bytes
		segments = append(segments, segment)
	}
	request := EmbedTextRequest{
		Binding: binding, Segments: segments, Language: language,
		Profile:            embedTextProfileForAdmission(p.worker.Admission),
		AuthorizationScope: "authz:" + p.worker.Admission.ProfileDigest + ":semantic-index",
		EgressScope:        "egress:" + p.worker.Admission.ProfileDigest + ":none",
		MaxInputBytes:      MaxEmbedTextInputBytes, MaxInputTokens: MaxEmbedTextInputTokens,
		MaxResourceBytes: MaxEmbedTextResourceBytes, ResourceScope: EmbedTextResourceScopeScratch,
		MaxOutputBytes: MaxEmbedTextOutputBytes,
	}
	if err := ValidateEmbedTextRequest(request); err != nil {
		return nil, fmt.Errorf("%w: request: %v", search.ErrSemanticProviderUnavailable, err)
	}
	batch, err := p.worker.EmbedText(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("%w: worker: %v", search.ErrSemanticProviderUnavailable, err)
	}
	if err := ValidateEmbedTextResult(request, batch); err != nil {
		return nil, fmt.Errorf("%w: result: %v", search.ErrSemanticProviderUnavailable, err)
	}
	results := make([]search.SemanticVector, 0, len(batch.Results))
	for _, result := range batch.Results {
		if result.Status != EmbedTextAccepted {
			return nil, fmt.Errorf("%w: segment %q returned %s", search.ErrSemanticProviderUnavailable, result.SegmentID, result.Status)
		}
		results = append(results, search.SemanticVector{SubjectID: result.SubjectRef, SegmentID: result.SegmentID, Vector: append([]float32(nil), result.Vector...)})
	}
	return results, nil
}

func newONNXProviderBinding(admission ONNXWorkerAdmission, purpose search.SemanticEmbeddingPurpose, sessionID, generationID string, fenceToken int64) (EmbedTextInvocationBinding, error) {
	var applied string
	switch purpose {
	case search.SemanticEmbeddingDocument:
		applied = admission.DocumentPreprocessingDigest
	case search.SemanticEmbeddingQuery:
		applied = admission.QueryPreprocessingDigest
	default:
		return EmbedTextInvocationBinding{}, search.ErrSemanticProviderUnavailable
	}
	tokens := make([]string, 6)
	for i, prefix := range []string{"session", "operation", "request", "invocation", "attempt", "idempotency"} {
		token, err := newONNXProviderToken(prefix)
		if err != nil {
			return EmbedTextInvocationBinding{}, fmt.Errorf("%w: %v", search.ErrSemanticProviderUnavailable, err)
		}
		tokens[i] = token
	}
	lease, err := newONNXProviderToken("lease")
	if err != nil {
		return EmbedTextInvocationBinding{}, fmt.Errorf("%w: %v", search.ErrSemanticProviderUnavailable, err)
	}
	if sessionID == "" {
		sessionID = tokens[0]
	}
	if generationID == "" {
		generationID = admission.ProfileDigest
	}
	if fenceToken <= 0 {
		fenceToken = 1
	}
	return EmbedTextInvocationBinding{
		Purpose: EmbedTextPurpose(purpose), Operation: EmbedTextOperation,
		SessionID: sessionID, OperationID: tokens[1], RequestID: tokens[2], InvocationID: tokens[3],
		AttemptID: tokens[4], IdempotencyKey: tokens[5], LeaseID: lease, FenceToken: fenceToken,
		GenerationID: generationID, WorkerDigest: admission.WorkerDigest,
		WorkerProfileDigest: admission.ProfileDigest, AppliedPreprocessingDigest: applied,
	}, nil
}

func workerInvocationBinding(worker NegotiatedONNXWorker) (sessionID, generationID string, fenceToken int64) {
	if identity, ok := worker.runtime.(interface {
		onnxWorkerInvocationBinding() (string, string, string)
	}); ok {
		var fence string
		sessionID, generationID, fence = identity.onnxWorkerInvocationBinding()
		if parsed, err := strconv.ParseInt(fence, 10, 64); err == nil {
			fenceToken = parsed
		}
	}
	return sessionID, generationID, fenceToken
}

func newONNXProviderToken(prefix string) (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + ":" + hex.EncodeToString(raw[:]), nil
}

func digestText(text []byte) string {
	// TextHandleStore computes the same SHA-256 digest. Keeping the helper
	// local avoids exposing handle internals to the search adapter.
	sum := sha256.Sum256(text)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func embedTextProfileForAdmission(admission ONNXWorkerAdmission) EmbedTextProfile {
	manifest := admission.Manifest
	return EmbedTextProfile{
		SemanticProfileDigest: admission.ProfileDigest, ConfigDigest: manifest.ConfigDigest,
		QueryPreprocessingDigest: admission.QueryPreprocessingDigest, DocumentPreprocessingDigest: admission.DocumentPreprocessingDigest,
		ModelDigest: manifest.ModelDigest, TokenizerDigest: manifest.TokenizerDigest, RuntimeDigest: manifest.RuntimeDigest,
		SemanticSpace: manifest.SemanticSpace, ElementType: manifest.ElementType, Dimension: manifest.Dimension,
		Normalization: manifest.Normalization, Pooling: manifest.Pooling, DeterminismClass: EmbedTextDeterminismSemantic,
	}
}

var _ search.SemanticEmbeddingProvider = (*ONNXSemanticEmbeddingProvider)(nil)
