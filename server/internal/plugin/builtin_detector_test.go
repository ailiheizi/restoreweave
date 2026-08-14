package plugin

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBuiltinDetectorRetainsAgreeingSuffixAndMagicEvidence(t *testing.T) {
	detector := newTestDetector(t)
	result, err := detector.Detect(context.Background(), DetectionRequest{
		Subject:     SubjectRef{ObservationID: "obs-photo"},
		DisplayName: "holiday.JPG",
		Samples: []ByteSample{{
			Offset: 0,
			Data:   []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10},
		}},
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.State != IdentificationIdentified {
		t.Fatalf("state = %s, want %s", result.State, IdentificationIdentified)
	}
	if len(result.Evidence) != 2 {
		t.Fatalf("evidence count = %d, want 2", len(result.Evidence))
	}
	if result.Evidence[0].Kind != EvidenceSuffix || result.Evidence[1].Kind != EvidenceMagic {
		t.Fatalf("evidence kinds = %s, %s; want suffix, magic", result.Evidence[0].Kind, result.Evidence[1].Kind)
	}
	for _, evidence := range result.Evidence {
		if evidence.Candidate.FormatID != "jpeg" {
			t.Errorf("candidate = %s, want jpeg", evidence.Candidate.FormatID)
		}
		if err := evidence.Validate(); err != nil {
			t.Errorf("evidence validation failed: %v", err)
		}
	}
}

func TestBuiltinDetectorDoesNotHideSuffixMagicConflict(t *testing.T) {
	detector := newTestDetector(t)
	result, err := detector.Detect(context.Background(), DetectionRequest{
		Subject:     SubjectRef{ObservationID: "obs-conflict"},
		DisplayName: "claimed.zip",
		Samples: []ByteSample{{
			Offset: 0,
			Data:   []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a},
		}},
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.State != IdentificationConflictingEvidence {
		t.Fatalf("state = %s, want %s", result.State, IdentificationConflictingEvidence)
	}
	if len(result.Evidence) != 2 {
		t.Fatalf("evidence count = %d, want both conflicting observations", len(result.Evidence))
	}
	if got := result.Evidence[0].Candidate.FormatID; got != "zip" {
		t.Errorf("suffix candidate = %s, want zip", got)
	}
	if got := result.Evidence[1].Candidate.FormatID; got != "png" {
		t.Errorf("magic candidate = %s, want png", got)
	}
}

func TestBuiltinDetectorReportsPartialSignatureWithoutOverclaiming(t *testing.T) {
	detector := newTestDetector(t)
	result, err := detector.Detect(context.Background(), DetectionRequest{
		Subject:     SubjectRef{ObservationID: "obs-truncated"},
		DisplayName: "unknown.bin",
		Samples: []ByteSample{{
			Offset: 0,
			Data:   []byte{0x89, 'P', 'N', 'G'},
		}},
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.State != IdentificationAmbiguous {
		t.Fatalf("state = %s, want %s", result.State, IdentificationAmbiguous)
	}
	if len(result.Evidence) != 1 {
		t.Fatalf("evidence count = %d, want 1", len(result.Evidence))
	}
	evidence := result.Evidence[0]
	if evidence.Complete {
		t.Fatal("partial signature was marked complete")
	}
	if evidence.Confidence.Semantics != ConfidencePartialRule {
		t.Fatalf("confidence = %s, want %s", evidence.Confidence.Semantics, ConfidencePartialRule)
	}
	want := ByteRange{Offset: 4, Length: 4}
	if len(evidence.RequiredBytesUnavailable) != 1 || evidence.RequiredBytesUnavailable[0] != want {
		t.Fatalf("unavailable = %#v, want %#v", evidence.RequiredBytesUnavailable, want)
	}
}

func TestBuiltinDetectorUsesLongestCompoundSuffix(t *testing.T) {
	detector := newTestDetector(t)
	result, err := detector.Detect(context.Background(), DetectionRequest{
		Subject:     SubjectRef{ObservationID: "obs-tarball"},
		DisplayName: "snapshot.tar.gz",
		Samples:     []ByteSample{{Offset: 0, Data: []byte{0x1f, 0x8b, 0x08}}},
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.State != IdentificationAmbiguous {
		t.Fatalf("state = %s, want %s because tar has not been structurally parsed", result.State, IdentificationAmbiguous)
	}
	if got := result.Evidence[0].Candidate.FormatID; got != "tar-gzip" {
		t.Fatalf("suffix candidate = %s, want tar-gzip", got)
	}
	if got := result.Evidence[0].ExaminedFacts[0].Value; got != ".tar.gz" {
		t.Fatalf("matched suffix = %s, want .tar.gz", got)
	}
}

func TestBuiltinDetectorUnknownDoesNotInventEvidence(t *testing.T) {
	detector := newTestDetector(t)
	result, err := detector.Detect(context.Background(), DetectionRequest{
		Subject:     SubjectRef{ObservationID: "obs-unknown"},
		DisplayName: "README",
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.State != IdentificationUnknown || len(result.Evidence) != 0 {
		t.Fatalf("result = %#v, want UNKNOWN with no positive evidence", result)
	}
}

func TestBuiltinDetectorHonorsCancellation(t *testing.T) {
	detector := newTestDetector(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := detector.Detect(ctx, DetectionRequest{Subject: SubjectRef{ObservationID: "obs-cancel"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Detect() error = %v, want context.Canceled", err)
	}
}

func TestBuiltinRuleDigestIsStableAndValid(t *testing.T) {
	first := BuiltinDetectionRulesDigest()
	second := BuiltinDetectionRulesDigest()
	if first != second {
		t.Fatalf("rules digest changed between calls: %s != %s", first, second)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("rules digest invalid: %v", err)
	}
}

func newTestDetector(t *testing.T) *BuiltinIdentificationDetector {
	t.Helper()
	fixed := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	detector, err := NewBuiltinIdentificationDetector(BuiltinDetectorConfig{
		PackageDigest:       testDigest('a'),
		RuntimeDigest:       testDigest('b'),
		SandboxPolicyDigest: testDigest('c'),
		Clock:               func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("NewBuiltinIdentificationDetector() error = %v", err)
	}
	return detector
}
