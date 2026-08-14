package plugin

import (
	"strings"
	"testing"
	"time"
)

func TestParserEvidencePreservesUnclaimedRanges(t *testing.T) {
	evidence := validParserEvidence()
	if err := evidence.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(evidence.UnclaimedRanges) != 1 || evidence.UnclaimedRanges[0] != (ByteRange{Offset: 80, Length: 20}) {
		t.Fatalf("unclaimed ranges = %#v", evidence.UnclaimedRanges)
	}
}

func TestParserEvidenceRejectsFalseKnownDenominator(t *testing.T) {
	evidence := validParserEvidence()
	total := uint64(1)
	evidence.Coverage.SemanticRegions.Total = &total
	evidence.Coverage.SemanticRegions.DenominatorKind = DenominatorUnknown
	err := evidence.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown denominator") {
		t.Fatalf("Validate() error = %v, want unknown-denominator rejection", err)
	}
}

func TestParserEvidenceRejectsCyclicParseTree(t *testing.T) {
	evidence := validParserEvidence()
	evidence.Nodes = []ParseNode{
		{ID: "root", ParentID: "child", Kind: "container"},
		{ID: "child", ParentID: "root", Kind: "member"},
	}
	err := evidence.Validate()
	if err == nil {
		t.Fatal("Validate() accepted cyclic parse tree")
	}
}

func validParserEvidence() ParserEvidence {
	totalBytes := uint64(100)
	oneMember := uint64(1)
	zero := uint64(0)
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	return ParserEvidence{
		Subject: SubjectRef{ObservationID: "obs-archive", ContentID: testDigest('4')},
		Parser: EntryPointRef{
			PackageID:     "org.restoreweave.test-parser",
			PackageDigest: testDigest('5'),
			EntryPointID:  "parser.test.v1",
			RulesDigest:   testDigest('6'),
			RuntimeDigest: testDigest('7'),
		},
		ViewID: "view-strict-v1",
		Format: FormatCandidate{
			FormatID: "test-container", FamilyID: "test-container",
			MIME: "application/x-test",
		},
		State: IdentificationPartiallyParsed,
		Nodes: []ParseNode{{
			ID: "root", Kind: "container",
			ClaimedRanges: []ByteRange{{Offset: 0, Length: 80}},
		}},
		ClaimedRanges:   []ByteRange{{Offset: 0, Length: 80}},
		UnclaimedRanges: []ByteRange{{Offset: 80, Length: 20}},
		Coverage: ParseCoverage{
			DetectionBytes: CoverageCounter{
				Unit: "bytes", Attempted: 8, Completed: 8,
				Total: &totalBytes, DenominatorKind: DenominatorExact,
			},
			StructuralBytes: CoverageCounter{
				Unit: "bytes", Attempted: 100, Completed: 80,
				Total: &totalBytes, DenominatorKind: DenominatorExact,
			},
			Members: CoverageCounter{
				Unit: "members", Attempted: 1, Completed: 1,
				Total: &oneMember, DenominatorKind: DenominatorExact,
			},
			MetadataFields: CoverageCounter{
				Unit: "fields", Attempted: 0, Completed: 0,
				Total: &zero, DenominatorKind: DenominatorExact,
			},
			ContentRegions: CoverageCounter{
				Unit: "regions", DenominatorKind: DenominatorUnknown,
			},
			SemanticRegions: CoverageCounter{
				Unit: "regions", DenominatorKind: DenominatorUnknown,
			},
		},
		Execution: ExecutionMetadata{
			Class:               ExecutionByteDeterministic,
			StartedAt:           now,
			FinishedAt:          now,
			SandboxPolicyDigest: testDigest('8'),
		},
	}
}
