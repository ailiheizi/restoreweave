package processor

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func testDigest(hexDigit string) string { return "sha256:" + strings.Repeat(hexDigit, 64) }

func testTextHandleID(hexDigit string) string { return "th_" + strings.Repeat(hexDigit, 32) }

func testEmbedRequest() EmbedTextRequest {
	return EmbedTextRequest{
		Binding: EmbedTextInvocationBinding{
			Purpose:                    EmbedTextPurposeDocument,
			Operation:                  EmbedTextOperation,
			SessionID:                  "session-1",
			OperationID:                "operation-1",
			RequestID:                  "request-1",
			InvocationID:               "invocation-1",
			AttemptID:                  "attempt-1",
			IdempotencyKey:             "idempotency-1",
			LeaseID:                    "lease-1",
			FenceToken:                 7,
			GenerationID:               "generation-1",
			WorkerDigest:               testDigest("7"),
			WorkerProfileDigest:        testDigest("8"),
			AppliedPreprocessingDigest: testDigest("d"),
		},
		Segments: []EmbedTextSegment{
			{ID: "seg-1", Source: EmbedTextSource{Kind: EmbedTextSourceDescriptionSegment, Ref: "seg-1", Revision: "doc-1"}, DescriptionDocumentID: "doc-1", Ordinal: 0, SubjectRef: "subject-1", Language: "zh", TextHandleID: testTextHandleID("1"), TextDigest: testDigest("1"), TextBytes: 12},
			{ID: "seg-2", Source: EmbedTextSource{Kind: EmbedTextSourceDescriptionSegment, Ref: "seg-2", Revision: "doc-2"}, DescriptionDocumentID: "doc-2", Ordinal: 0, SubjectRef: "subject-2", Language: "zh", TextHandleID: testTextHandleID("2"), TextDigest: testDigest("2"), TextBytes: 12},
		},
		Language: "zh",
		Profile: EmbedTextProfile{
			SemanticProfileDigest:       testDigest("a"),
			ConfigDigest:                testDigest("b"),
			QueryPreprocessingDigest:    testDigest("c"),
			DocumentPreprocessingDigest: testDigest("d"),
			ModelDigest:                 testDigest("d"),
			TokenizerDigest:             testDigest("e"),
			RuntimeDigest:               testDigest("f"),
			SemanticSpace:               "bge-small-zh-v1.5",
			ElementType:                 "float32",
			Dimension:                   3,
			Normalization:               EmbedTextNormalizationL2,
			Pooling:                     EmbedTextPoolingMean,
			DeterminismClass:            EmbedTextDeterminismByte,
		},
		AuthorizationScope: "authz:" + testDigest("a") + ":local-index-build",
		EgressScope:        "egress:" + testDigest("a") + ":none",
		MaxInputBytes:      64,
		MaxInputTokens:     128,
		MaxResourceBytes:   1024,
		ResourceScope:      EmbedTextResourceScopeScratch,
		MaxOutputBytes:     64,
	}
}

func validEmbedBatch(req EmbedTextRequest) EmbedTextResultBatch {
	result := func(segment EmbedTextSegment, vector []float32) EmbedTextResult {
		return EmbedTextResult{
			Binding:   req.Binding,
			SegmentID: segment.ID, Source: segment.Source, DescriptionDocumentID: segment.DescriptionDocumentID, Ordinal: segment.Ordinal,
			SubjectRef: segment.SubjectRef, Language: segment.Language, TextHandleID: segment.TextHandleID, TextDigest: segment.TextDigest,
			Status: EmbedTextAccepted, Vector: vector, ElementType: req.Profile.ElementType, Dimension: req.Profile.Dimension,
			Normalization: req.Profile.Normalization, Pooling: req.Profile.Pooling, SemanticProfileDigest: req.Profile.SemanticProfileDigest,
			ConfigDigest: req.Profile.ConfigDigest, PreprocessingDigest: req.Binding.AppliedPreprocessingDigest, ModelDigest: req.Profile.ModelDigest,
			TokenizerDigest: req.Profile.TokenizerDigest, RuntimeDigest: req.Profile.RuntimeDigest, SemanticSpace: req.Profile.SemanticSpace,
			DeterminismClass: req.Profile.DeterminismClass, Coverage: EmbedTextCoverageFull,
			InputTokens: 3, EmbeddedTokens: 3,
		}
	}
	requestDigest, err := req.CanonicalDigest()
	if err != nil {
		panic(err)
	}
	results := make([]EmbedTextResult, 0, len(req.Segments))
	for i, segment := range req.Segments {
		vector := []float32{1, 0, 0}
		if i%2 == 1 {
			vector = []float32{0, 1, 0}
		}
		results = append(results, result(segment, vector))
	}
	return EmbedTextResultBatch{Binding: req.Binding, RequestDigest: requestDigest, PeakResourceBytes: 512, ResourceScope: EmbedTextResourceScopeScratch, Results: results}
}

func testQueryRequest() EmbedTextRequest {
	req := testEmbedRequest()
	req.Binding.Purpose = EmbedTextPurposeQuery
	req.Binding.AppliedPreprocessingDigest = req.Profile.QueryPreprocessingDigest
	req.Segments = []EmbedTextSegment{{
		ID:           "query-1",
		Source:       EmbedTextSource{Kind: EmbedTextSourceQuerySegment, Ref: "query-1", Revision: testDigest("1")},
		Ordinal:      0,
		Language:     "zh",
		TextHandleID: testTextHandleID("3"),
		TextDigest:   testDigest("1"),
		TextBytes:    12,
	}}
	return req
}

func TestValidateEmbedTextResultAcceptsDeterministicValidBatch(t *testing.T) {
	req := testEmbedRequest()
	if err := ValidateEmbedTextResult(req, validEmbedBatch(req)); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
}

func TestValidateEmbedTextRequestRejectsInvalidBindingsAndBudgets(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*EmbedTextRequest)
		want   error
	}{
		{"bad digest", func(req *EmbedTextRequest) { req.Profile.ConfigDigest = "sha256:ABC" }, ErrEmbedTextDigest},
		{"missing authorization scope", func(req *EmbedTextRequest) { req.AuthorizationScope = "" }, ErrEmbedTextScope},
		{"scope not bound to profile", func(req *EmbedTextRequest) { req.EgressScope = "egress:" + testDigest("9") + ":none" }, ErrEmbedTextScope},
		{"zero token budget", func(req *EmbedTextRequest) { req.MaxInputTokens = 0 }, ErrEmbedTextBudget},
		{"zero output budget", func(req *EmbedTextRequest) { req.MaxOutputBytes = 0 }, ErrEmbedTextBudget},
		{"unknown resource scope", func(req *EmbedTextRequest) { req.ResourceScope = "TOTAL_RSS" }, ErrEmbedTextBudget},
		{"input budget exceeded", func(req *EmbedTextRequest) { req.MaxInputBytes = 20 }, ErrEmbedTextBudget},
		{"invalid request language", func(req *EmbedTextRequest) { req.Language = "zh\nCN" }, ErrInvalidEmbedTextRequest},
		{"oversized request language", func(req *EmbedTextRequest) { req.Language = strings.Repeat("z", MaxEmbedTextControlValueBytes+1) }, ErrInvalidEmbedTextRequest},
		{"invalid segment id", func(req *EmbedTextRequest) { req.Segments[0].ID = "seg/1" }, ErrInvalidEmbedTextRequest},
		{"invalid segment language", func(req *EmbedTextRequest) { req.Segments[0].Language = "zh CN" }, ErrInvalidEmbedTextRequest},
		{"invalid text handle", func(req *EmbedTextRequest) { req.Segments[0].TextHandleID = "handle\n1" }, ErrEmbedTextHandle},
		{"noncanonical text handle prefix", func(req *EmbedTextRequest) { req.Segments[0].TextHandleID = "handle_" + strings.Repeat("1", 32) }, ErrEmbedTextHandle},
		{"uppercase text handle", func(req *EmbedTextRequest) { req.Segments[0].TextHandleID = "th_" + strings.Repeat("A", 32) }, ErrEmbedTextHandle},
		{"oversized text handle", func(req *EmbedTextRequest) {
			req.Segments[0].TextHandleID = strings.Repeat("h", MaxEmbedTextControlValueBytes+1)
		}, ErrEmbedTextHandle},
		{"duplicate segment", func(req *EmbedTextRequest) {
			req.Segments[1].ID = req.Segments[0].ID
			req.Segments[1].Source.Ref = req.Segments[0].Source.Ref
		}, ErrEmbedTextSegmentDuplicate},
		{"duplicate ordinal in document", func(req *EmbedTextRequest) {
			req.Segments[1].DescriptionDocumentID = req.Segments[0].DescriptionDocumentID
			req.Segments[1].SubjectRef = req.Segments[0].SubjectRef
			req.Segments[1].Ordinal = req.Segments[0].Ordinal
		}, ErrEmbedTextSegmentDuplicate},
		{"document subject mismatch", func(req *EmbedTextRequest) {
			req.Segments[1].DescriptionDocumentID = req.Segments[0].DescriptionDocumentID
			req.Segments[1].Ordinal = 2
		}, ErrEmbedTextDocumentBinding},
		{"dimension limit", func(req *EmbedTextRequest) { req.Profile.Dimension = MaxEmbedTextDimension + 1 }, ErrEmbedTextDimensionLimit},
		{"unknown normalization", func(req *EmbedTextRequest) { req.Profile.Normalization = "unit" }, ErrEmbedTextNormalizationMismatch},
		{"unknown pooling", func(req *EmbedTextRequest) { req.Profile.Pooling = "attention" }, ErrEmbedTextPoolingMismatch},
		{"unknown determinism", func(req *EmbedTextRequest) { req.Profile.DeterminismClass = "MAYBE" }, ErrEmbedTextDeterminismMismatch},
		{"missing purpose", func(req *EmbedTextRequest) { req.Binding.Purpose = "" }, ErrEmbedTextPurpose},
		{"purpose source mismatch", func(req *EmbedTextRequest) { req.Binding.Purpose = EmbedTextPurposeQuery }, ErrEmbedTextPurpose},
		{"query and document preprocessing mixed", func(req *EmbedTextRequest) {
			req.Binding.Purpose = EmbedTextPurposeQuery
			req.Binding.AppliedPreprocessingDigest = req.Profile.QueryPreprocessingDigest
		}, ErrEmbedTextPurpose},
		{"document preprocessing does not bind profile", func(req *EmbedTextRequest) {
			req.Binding.AppliedPreprocessingDigest = testDigest("9")
		}, ErrEmbedTextPurpose},
		{"missing source revision", func(req *EmbedTextRequest) { req.Segments[0].Source.Revision = "" }, ErrEmbedTextSource},
		{"missing invocation", func(req *EmbedTextRequest) { req.Binding.InvocationID = "" }, ErrEmbedTextInvocationBinding},
		{"missing attempt", func(req *EmbedTextRequest) { req.Binding.AttemptID = "" }, ErrEmbedTextInvocationBinding},
		{"wrong operation", func(req *EmbedTextRequest) { req.Binding.Operation = "DESCRIBE_SUBJECT" }, ErrEmbedTextInvocationBinding},
		{"missing session", func(req *EmbedTextRequest) { req.Binding.SessionID = "" }, ErrEmbedTextInvocationBinding},
		{"missing operation id", func(req *EmbedTextRequest) { req.Binding.OperationID = "" }, ErrEmbedTextInvocationBinding},
		{"missing request id", func(req *EmbedTextRequest) { req.Binding.RequestID = "" }, ErrEmbedTextInvocationBinding},
		{"missing idempotency key", func(req *EmbedTextRequest) { req.Binding.IdempotencyKey = "" }, ErrEmbedTextInvocationBinding},
		{"missing lease", func(req *EmbedTextRequest) { req.Binding.LeaseID = "" }, ErrEmbedTextInvocationBinding},
		{"zero fence", func(req *EmbedTextRequest) { req.Binding.FenceToken = 0 }, ErrEmbedTextFence},
		{"missing generation", func(req *EmbedTextRequest) { req.Binding.GenerationID = "" }, ErrEmbedTextGeneration},
		{"bad worker digest", func(req *EmbedTextRequest) { req.Binding.WorkerDigest = "worker-1" }, ErrEmbedTextWorkerBinding},
		{"bad worker profile digest", func(req *EmbedTextRequest) { req.Binding.WorkerProfileDigest = "profile-1" }, ErrEmbedTextWorkerBinding},
		{"oversized generation", func(req *EmbedTextRequest) {
			req.Binding.GenerationID = strings.Repeat("g", MaxEmbedTextControlValueBytes+1)
		}, ErrEmbedTextGeneration},
		{"oversized authorization scope", func(req *EmbedTextRequest) { req.AuthorizationScope += strings.Repeat("a", MaxEmbedTextScopeBytes) }, ErrEmbedTextScope},
		{"invalid semantic space", func(req *EmbedTextRequest) { req.Profile.SemanticSpace = "space\nother" }, ErrInvalidEmbedTextRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := testEmbedRequest()
			tc.mutate(&req)
			if err := ValidateEmbedTextRequest(req); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateEmbedTextResultRejectsCardinalityAndSubjectMismatch(t *testing.T) {
	req := testEmbedRequest()
	cases := []struct {
		name   string
		mutate func(*EmbedTextResultBatch)
		want   error
	}{
		{"missing segment", func(batch *EmbedTextResultBatch) { batch.Results = batch.Results[:1] }, ErrEmbedTextSegmentMissing},
		{"extra segment", func(batch *EmbedTextResultBatch) {
			item := batch.Results[0]
			item.SegmentID = "seg-extra"
			batch.Results = append(batch.Results, item)
		}, ErrEmbedTextSegmentExtra},
		{"wrong subject", func(batch *EmbedTextResultBatch) { batch.Results[0].SubjectRef = "subject-other" }, ErrEmbedTextSubjectMismatch},
		{"wrong text handle", func(batch *EmbedTextResultBatch) { batch.Results[0].TextHandleID = "th-other" }, ErrEmbedTextSegmentBinding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			batch := validEmbedBatch(req)
			tc.mutate(&batch)
			if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateEmbedTextResultRejectsUntrustedVectorAndBinding(t *testing.T) {
	req := testEmbedRequest()
	cases := []struct {
		name   string
		mutate func(*EmbedTextResult)
		want   error
	}{
		{"nan", func(result *EmbedTextResult) { result.Vector[1] = float32(math.NaN()) }, ErrEmbedTextNonFinite},
		{"infinity", func(result *EmbedTextResult) { result.Vector[1] = float32(math.Inf(1)) }, ErrEmbedTextNonFinite},
		{"dimension", func(result *EmbedTextResult) { result.Dimension = 2 }, ErrEmbedTextDimensionMismatch},
		{"element type", func(result *EmbedTextResult) { result.ElementType = "float16" }, ErrEmbedTextElementTypeMismatch},
		{"normalization", func(result *EmbedTextResult) { result.Normalization = "none" }, ErrEmbedTextNormalizationMismatch},
		{"semantic space", func(result *EmbedTextResult) { result.SemanticSpace = "other-space" }, ErrEmbedTextSemanticSpaceMismatch},
		{"profile", func(result *EmbedTextResult) { result.SemanticProfileDigest = testDigest("9") }, ErrEmbedTextProfileMismatch},
		{"config", func(result *EmbedTextResult) { result.ConfigDigest = testDigest("9") }, ErrEmbedTextConfigMismatch},
		{"applied preprocessing", func(result *EmbedTextResult) { result.PreprocessingDigest = testDigest("9") }, ErrEmbedTextPreprocessingMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			batch := validEmbedBatch(req)
			tc.mutate(&batch.Results[0])
			if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateEmbedTextResultRequiresTypedTerminalOutcomes(t *testing.T) {
	req := testEmbedRequest()
	batch := validEmbedBatch(req)
	batch.Results[0].Vector = nil
	batch.Results[0].Status = EmbedTextInapplicable
	batch.Results[0].Coverage = EmbedTextCoverageNone
	batch.Results[0].InputTokens = 0
	batch.Results[0].EmbeddedTokens = 0
	batch.Results[0].FailureCode = "NO_ELIGIBLE_TEXT"
	batch.Results[0].Reason = "segment has no eligible text"
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		t.Fatalf("typed inapplicability rejected: %v", err)
	}
	batch.Results[0].FailureCode = "UNKNOWN"
	if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextFailureCode) {
		t.Fatalf("untyped inapplicability error = %v", err)
	}
	batch = validEmbedBatch(req)
	batch.Results[0].Vector = nil
	batch.Results[0].Status = EmbedTextInapplicable
	batch.Results[0].Coverage = EmbedTextCoverageNone
	batch.Results[0].InputTokens = 0
	batch.Results[0].EmbeddedTokens = 0
	batch.Results[0].FailureCode = "PROVIDER_UNAVAILABLE"
	batch.Results[0].Reason = "provider failed"
	if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextFailureCode) {
		t.Fatalf("status-specific failure code error = %v", err)
	}
}

func TestValidateEmbedTextResultRejectsUnboundedFailureReason(t *testing.T) {
	req := testEmbedRequest()
	for _, reason := range []string{
		"provider failed\nwith injected control data",
		strings.Repeat("x", MaxEmbedTextFailureReasonBytes+1),
	} {
		batch := validEmbedBatch(req)
		batch.Results[0].Vector = nil
		batch.Results[0].Status = EmbedTextFailed
		batch.Results[0].Coverage = EmbedTextCoverageNone
		batch.Results[0].InputTokens = 0
		batch.Results[0].EmbeddedTokens = 0
		batch.Results[0].FailureCode = "PROCESSOR_FAILURE"
		batch.Results[0].Reason = reason
		if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextFailureCode) {
			t.Fatalf("reason %q error = %v, want %v", reason, err, ErrEmbedTextFailureCode)
		}
	}
}

func TestValidateEmbedTextResultEnforcesBatchBudgetAndL2Norm(t *testing.T) {
	req := testEmbedRequest()
	req.MaxOutputBytes = 20
	if err := ValidateEmbedTextResult(req, validEmbedBatch(req)); !errors.Is(err, ErrEmbedTextBudget) {
		t.Fatalf("budget error = %v, want %v", err, ErrEmbedTextBudget)
	}
	req = testEmbedRequest()
	req.MaxInputTokens = 5
	if err := ValidateEmbedTextResult(req, validEmbedBatch(req)); !errors.Is(err, ErrEmbedTextBudget) {
		t.Fatalf("token budget error = %v, want %v", err, ErrEmbedTextBudget)
	}
	req = testEmbedRequest()
	batch := validEmbedBatch(req)
	batch.Results[0].InputTokens = 100
	batch.Results[0].EmbeddedTokens = 1
	batch.Results[0].Coverage = EmbedTextCoverageTruncated
	batch.Results[1].InputTokens = 100
	batch.Results[1].EmbeddedTokens = 1
	batch.Results[1].Coverage = EmbedTextCoverageTruncated
	if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextBudget) {
		t.Fatalf("cumulative input token error = %v, want %v", err, ErrEmbedTextBudget)
	}
	req = testEmbedRequest()
	batch = validEmbedBatch(req)
	batch.PeakResourceBytes = req.MaxResourceBytes + 1
	if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextBudget) {
		t.Fatalf("resource budget error = %v, want %v", err, ErrEmbedTextBudget)
	}
	batch = validEmbedBatch(req)
	batch.ResourceScope = "TOTAL_RSS"
	if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextBudget) {
		t.Fatalf("resource scope error = %v, want %v", err, ErrEmbedTextBudget)
	}
	req = testEmbedRequest()
	batch = validEmbedBatch(req)
	batch.Results[0].Vector = []float32{1, 1, 0}
	if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextNorm) {
		t.Fatalf("norm error = %v, want %v", err, ErrEmbedTextNorm)
	}
	// Non-L2 profiles retain their declared semantics; the host does not
	// impose an L2 constraint on a profile that did not declare one.
	req.Profile.Normalization = "none"
	batch = validEmbedBatch(req)
	batch.Results[0].Vector = []float32{2, 0, 0}
	for i := range batch.Results {
		batch.Results[i].Normalization = "none"
	}
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		t.Fatalf("non-L2 vector rejected: %v", err)
	}
}

func TestValidateEmbedTextResultBindsTypedFailureProvenance(t *testing.T) {
	req := testEmbedRequest()
	batch := validEmbedBatch(req)
	batch.Results[0].Vector = nil
	batch.Results[0].Status = EmbedTextFailed
	batch.Results[0].Coverage = EmbedTextCoveragePartial
	batch.Results[0].InputTokens = 3
	batch.Results[0].EmbeddedTokens = 0
	batch.Results[0].FailureCode = "PROVIDER_UNAVAILABLE"
	batch.Results[0].Reason = "local model is unavailable"
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		t.Fatalf("typed failure rejected: %v", err)
	}
	batch.Results[0].ModelDigest = testDigest("9")
	if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextProvenanceMismatch) {
		t.Fatalf("failure provenance error = %v, want %v", err, ErrEmbedTextProvenanceMismatch)
	}
}

func TestValidateEmbedTextResultBindsRequestAndTypedCoverage(t *testing.T) {
	req := testEmbedRequest()
	batch := validEmbedBatch(req)
	changed := req
	changed.AuthorizationScope += ":other-principal"
	if err := ValidateEmbedTextResult(changed, batch); !errors.Is(err, ErrEmbedTextRequestMismatch) {
		t.Fatalf("request binding error = %v, want %v", err, ErrEmbedTextRequestMismatch)
	}

	batch = validEmbedBatch(req)
	batch.Results[0].Coverage = "UNKNOWN"
	if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextCoverage) {
		t.Fatalf("coverage error = %v, want %v", err, ErrEmbedTextCoverage)
	}
	batch = validEmbedBatch(req)
	batch.Results[0].Coverage = EmbedTextCoverageTruncated
	batch.Results[0].InputTokens = 4
	if err := ValidateEmbedTextResult(req, batch); err != nil {
		t.Fatalf("declared truncation rejected: %v", err)
	}
	batch.Results[0].InputTokens = batch.Results[0].EmbeddedTokens
	if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, ErrEmbedTextCoverage) {
		t.Fatalf("false truncation error = %v, want %v", err, ErrEmbedTextCoverage)
	}
}

func TestEmbedTextCanonicalDigestBindsInvocationIdentity(t *testing.T) {
	req := testEmbedRequest()
	first, err := req.CanonicalDigest()
	if err != nil {
		t.Fatalf("canonical digest: %v", err)
	}
	second, err := req.CanonicalDigest()
	if err != nil || first != second {
		t.Fatalf("canonical digest is unstable: first=%q second=%q err=%v", first, second, err)
	}
	mutations := []func(*EmbedTextRequest){
		func(req *EmbedTextRequest) {
			req.Segments[0].ID = "seg-1-renamed"
			req.Segments[0].Source.Ref = "seg-1-renamed"
		},
		func(req *EmbedTextRequest) {
			req.Segments[0].DescriptionDocumentID = "doc-1-new"
			req.Segments[0].Source.Revision = "doc-1-new"
		},
		func(req *EmbedTextRequest) { req.Binding.InvocationID = "invocation-2" },
		func(req *EmbedTextRequest) { req.Binding.AttemptID = "attempt-2" },
		func(req *EmbedTextRequest) { req.Binding.SessionID = "session-2" },
		func(req *EmbedTextRequest) { req.Binding.OperationID = "operation-2" },
		func(req *EmbedTextRequest) { req.Binding.RequestID = "request-2" },
		func(req *EmbedTextRequest) { req.Binding.IdempotencyKey = "idempotency-2" },
		func(req *EmbedTextRequest) { req.Binding.LeaseID = "lease-2" },
		func(req *EmbedTextRequest) { req.Binding.FenceToken++ },
		func(req *EmbedTextRequest) { req.Binding.GenerationID = "generation-2" },
		func(req *EmbedTextRequest) { req.Binding.WorkerDigest = testDigest("9") },
		func(req *EmbedTextRequest) { req.Binding.WorkerProfileDigest = testDigest("9") },
		func(req *EmbedTextRequest) {
			req.Binding.AppliedPreprocessingDigest = testDigest("9")
			req.Profile.DocumentPreprocessingDigest = testDigest("9")
		},
		func(req *EmbedTextRequest) { req.Profile.QueryPreprocessingDigest = testDigest("9") },
		func(req *EmbedTextRequest) {
			req.Profile.DocumentPreprocessingDigest = testDigest("9")
			req.Binding.AppliedPreprocessingDigest = testDigest("9")
		},
	}
	for _, mutate := range mutations {
		changed := req
		mutate(&changed)
		digest, err := changed.CanonicalDigest()
		if err != nil {
			t.Fatalf("changed request rejected: %v", err)
		}
		if digest == first {
			t.Fatalf("binding mutation did not change digest: %q", digest)
		}
	}
	query := testQueryRequest()
	queryDigest, err := query.CanonicalDigest()
	if err != nil {
		t.Fatalf("query canonical digest: %v", err)
	}
	query.Binding.Purpose = EmbedTextPurposeDocument
	query.Profile.DocumentPreprocessingDigest = testDigest("9")
	query.Binding.AppliedPreprocessingDigest = query.Profile.DocumentPreprocessingDigest
	query.Segments[0].Source = EmbedTextSource{Kind: EmbedTextSourceDescriptionSegment, Ref: query.Segments[0].ID, Revision: "query-as-document-revision"}
	query.Segments[0].DescriptionDocumentID = "query-as-document-revision"
	query.Segments[0].SubjectRef = "subject-query"
	changedQueryDigest, err := query.CanonicalDigest()
	if err != nil || changedQueryDigest == queryDigest {
		t.Fatalf("purpose/source mutation did not change digest: first=%q changed=%q err=%v", queryDigest, changedQueryDigest, err)
	}
}

func TestValidateEmbedTextRequestAcceptsQueryBinding(t *testing.T) {
	req := testQueryRequest()
	if err := ValidateEmbedTextRequest(req); err != nil {
		t.Fatalf("query binding rejected: %v", err)
	}
	if err := ValidateEmbedTextResult(req, validEmbedBatch(req)); err != nil {
		t.Fatalf("query result rejected: %v", err)
	}
}

func TestValidateEmbedTextRequestRejectsQueryDurableRefsAndMultipleInputs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*EmbedTextRequest)
	}{
		{"multiple inputs", func(req *EmbedTextRequest) { req.Segments = append(req.Segments, req.Segments[0]) }},
		{"subject reference", func(req *EmbedTextRequest) { req.Segments[0].SubjectRef = "subject-1" }},
		{"description document reference", func(req *EmbedTextRequest) { req.Segments[0].DescriptionDocumentID = "doc-1" }},
		{"wrong source kind", func(req *EmbedTextRequest) { req.Segments[0].Source.Kind = EmbedTextSourceDescriptionSegment }},
		{"wrong source revision", func(req *EmbedTextRequest) { req.Segments[0].Source.Revision = "revision-1" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := testQueryRequest()
			tc.mutate(&req)
			if err := ValidateEmbedTextRequest(req); !errors.Is(err, ErrEmbedTextPurpose) && !errors.Is(err, ErrEmbedTextSource) {
				t.Fatalf("error = %v, want purpose/source rejection", err)
			}
		})
	}
}

func TestValidateEmbedTextRequestRejectsDocumentSourceMismatch(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*EmbedTextRequest)
	}{
		{"missing description revision", func(req *EmbedTextRequest) { req.Segments[0].DescriptionDocumentID = "" }},
		{"source ref mismatch", func(req *EmbedTextRequest) { req.Segments[0].Source.Ref = "other-segment" }},
		{"source revision mismatch", func(req *EmbedTextRequest) { req.Segments[0].Source.Revision = "other-revision" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := testEmbedRequest()
			tc.mutate(&req)
			if err := ValidateEmbedTextRequest(req); !errors.Is(err, ErrEmbedTextSource) {
				t.Fatalf("error = %v, want %v", err, ErrEmbedTextSource)
			}
		})
	}
}

func TestValidateEmbedTextResultRejectsInvocationReplayOrBindingMismatch(t *testing.T) {
	req := testEmbedRequest()
	cases := []struct {
		name   string
		mutate func(*EmbedTextResultBatch)
		want   error
	}{
		{"batch generation", func(batch *EmbedTextResultBatch) { batch.Binding.GenerationID = "generation-other" }, ErrEmbedTextRequestMismatch},
		{"result attempt", func(batch *EmbedTextResultBatch) { batch.Results[0].Binding.AttemptID = "attempt-other" }, ErrEmbedTextRequestMismatch},
		{"result fence", func(batch *EmbedTextResultBatch) { batch.Results[0].Binding.FenceToken = 8 }, ErrEmbedTextRequestMismatch},
		{"result purpose preprocessing", func(batch *EmbedTextResultBatch) {
			batch.Results[0].Binding.Purpose = EmbedTextPurposeQuery
		}, ErrEmbedTextRequestMismatch},
		{"result source", func(batch *EmbedTextResultBatch) {
			batch.Results[0].Source.Ref = "other-segment"
		}, ErrEmbedTextSegmentBinding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			batch := validEmbedBatch(req)
			tc.mutate(&batch)
			if err := ValidateEmbedTextResult(req, batch); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
