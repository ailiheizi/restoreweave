package identify

import (
	"context"
	"reflect"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/scanner"
)

func TestSuffixRuleTableComplete(t *testing.T) {
	rules := SuffixRules()
	if len(rules) != 41 {
		t.Fatalf("suffix rule table has %d rules, want 41", len(rules))
	}
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		if rule.Suffix == "" {
			t.Errorf("suffix rule with empty suffix")
		}
		if seen[rule.Suffix] {
			t.Errorf("duplicate suffix %q", rule.Suffix)
		}
		seen[rule.Suffix] = true
		if len(rule.Candidates) == 0 {
			t.Errorf("suffix %q has no candidates", rule.Suffix)
		}
		for _, candidate := range rule.Candidates {
			if candidate.FormatID == "" {
				t.Errorf("suffix %q has candidate without format_id", rule.Suffix)
			}
		}
	}
	if !seen[".png"] || !seen[".tar.gz"] || !seen[".zst"] || !seen[".epub"] || !seen[".md"] {
		t.Errorf("expected spot-check suffixes .png, .tar.gz, .zst, .epub, .md to be present")
	}
}

func TestMagicRuleTableComplete(t *testing.T) {
	rules := MagicRules()
	// Count matches the migrated legacy source table verbatim.
	if len(rules) != 24 {
		t.Fatalf("magic rule table has %d rules, want 24", len(rules))
	}
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		if seen[rule.ID] {
			t.Errorf("duplicate magic rule id %q", rule.ID)
		}
		seen[rule.ID] = true
		if rule.Candidate.FormatID == "" {
			t.Errorf("magic rule %q has no candidate format", rule.ID)
		}
		if len(rule.Alternatives) == 0 {
			t.Errorf("magic rule %q has no patterns", rule.ID)
		}
		if rule.digest() == "" {
			t.Errorf("magic rule %q has empty digest", rule.ID)
		}
	}
	if RulesDigest() == "" {
		t.Error("RulesDigest must not be empty")
	}
}

func TestDetectIdentifiedByMagic(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		probe  []byte
		format string
	}{
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, "png"},
		{"pdf", []byte("%PDF-1.7"), "pdf"},
		{"zip", []byte{'P', 'K', 0x03, 0x04, 0x00}, "zip"},
		{"gzip", []byte{0x1f, 0x8b, 0x08, 0x00}, "gzip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := NewDetector(0).Detect(ctx, "unknown-name", tc.probe)
			if err != nil {
				t.Fatalf("Detect returned error: %v", err)
			}
			if result.State != IdentificationIdentified {
				t.Fatalf("state = %s, want IDENTIFIED; evidence: %+v", result.State, result.Evidence)
			}
			if len(result.Evidence) != 1 {
				t.Fatalf("got %d evidence records, want 1", len(result.Evidence))
			}
			evidence := result.Evidence[0]
			if evidence.Candidate.FormatID != tc.format {
				t.Errorf("format = %s, want %s", evidence.Candidate.FormatID, tc.format)
			}
			if !evidence.Complete || evidence.Confidence != ConfidenceDeterministicRule {
				t.Errorf("expected complete deterministic rule evidence, got %+v", evidence)
			}
			if evidence.MatchDigest == "" {
				t.Error("match digest must not be empty")
			}
		})
	}
}

func TestConflictPreservesBothEvidenceLines(t *testing.T) {
	// A PNG-named file that is actually a JPEG: suffix says png, magic says
	// jpeg. Both lines must survive; nothing is silently overwritten.
	jpegProbe := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00}
	result, err := NewDetector(0).Detect(context.Background(), "photo.png", jpegProbe)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if result.State != IdentificationConflictingEvidence {
		t.Fatalf("state = %s, want CONFLICTING_EVIDENCE", result.State)
	}
	if len(result.Evidence) != 2 {
		t.Fatalf("got %d evidence records, want both suffix and magic lines", len(result.Evidence))
	}
	if result.Evidence[0].Kind != EvidenceSuffix || result.Evidence[0].Candidate.FormatID != "png" {
		t.Errorf("first evidence = %+v, want suffix png", result.Evidence[0])
	}
	if result.Evidence[1].Kind != EvidenceMagic || result.Evidence[1].Candidate.FormatID != "jpeg" {
		t.Errorf("second evidence = %+v, want magic jpeg", result.Evidence[1])
	}
}

func TestDetectDeterministic(t *testing.T) {
	ctx := context.Background()
	probe := append([]byte{0xff, 0xd8, 0xff}, make([]byte, 4096)...)
	first, err := NewDetector(0).Detect(ctx, "archive.tar.gz", probe)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	for i := 0; i < 10; i++ {
		again, err := NewDetector(0).Detect(ctx, "archive.tar.gz", probe)
		if err != nil {
			t.Fatalf("Detect returned error: %v", err)
		}
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("non-deterministic result:\nfirst: %+v\nagain: %+v", first, again)
		}
	}
}

func TestDetectEmptyInput(t *testing.T) {
	result, err := NewDetector(0).Detect(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if result.State != IdentificationUnknown {
		t.Fatalf("state = %s, want UNKNOWN", result.State)
	}
	if len(result.Evidence) != 0 {
		t.Fatalf("got %d evidence records, want none", len(result.Evidence))
	}

	// A known suffix with no probe bytes is ambiguous, never silently
	// identified.
	result, err = NewDetector(0).Detect(context.Background(), "notes.txt", nil)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if result.State != IdentificationAmbiguous {
		t.Fatalf("state = %s, want AMBIGUOUS", result.State)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].Kind != EvidenceSuffix {
		t.Fatalf("expected exactly one suffix evidence record, got %+v", result.Evidence)
	}
}

func TestDetectLargeProbe(t *testing.T) {
	probe := make([]byte, 1<<20)
	copy(probe, []byte{'P', 'K', 0x03, 0x04})
	result, err := NewDetector(0).Detect(context.Background(), "big.bin", probe)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if result.State != IdentificationIdentified || result.Evidence[0].Candidate.FormatID != "zip" {
		t.Fatalf("large probe not identified as zip: %+v", result)
	}

	// A bounded detector truncates before matching: three of four required
	// bytes become an explicit partial match.
	bounded := NewDetector(3)
	result, err = bounded.Detect(context.Background(), "big.bin", probe)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if result.State != IdentificationAmbiguous {
		t.Fatalf("truncated state = %s, want AMBIGUOUS", result.State)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].Complete {
		t.Fatalf("expected one partial magic evidence record, got %+v", result.Evidence)
	}
	if len(result.Evidence[0].RequiredBytesUnavailable) == 0 {
		t.Error("partial evidence must record missing bytes")
	}
}

func TestDetectCaseInsensitiveSuffix(t *testing.T) {
	for _, name := range []string{"Photo.PNG", "PHOTO.PNG", "photo.png"} {
		result, err := NewDetector(0).Detect(context.Background(), name, nil)
		if err != nil {
			t.Fatalf("Detect returned error: %v", err)
		}
		if len(result.Evidence) != 1 || result.Evidence[0].Candidate.FormatID != "png" {
			t.Errorf("name %q: expected png suffix evidence, got %+v", name, result.Evidence)
		}
	}

	result, err := NewDetector(0).Detect(context.Background(), "backup.TAR.GZ", nil)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].Candidate.FormatID != "tar-gzip" {
		t.Errorf("expected tar-gzip suffix evidence, got %+v", result.Evidence)
	}
}

func TestDetectPartialMagicBelowMinimum(t *testing.T) {
	// One byte of the PNG signature proves nothing; the rule must not fire.
	result, err := NewDetector(0).Detect(context.Background(), "unknown", []byte{0x89})
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if result.State != IdentificationUnknown || len(result.Evidence) != 0 {
		t.Fatalf("expected UNKNOWN with no evidence, got %+v", result)
	}

	// Three bytes of the signature are a kept partial match with explicit
	// missing ranges.
	result, err = NewDetector(0).Detect(context.Background(), "unknown", []byte{0x89, 'P', 'N'})
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if result.State != IdentificationAmbiguous {
		t.Fatalf("state = %s, want AMBIGUOUS", result.State)
	}
	evidence := result.Evidence[0]
	if evidence.Complete || evidence.Confidence != ConfidencePartialRule {
		t.Errorf("expected partial rule evidence, got %+v", evidence)
	}
	if len(evidence.ExaminedRanges) != 1 || evidence.ExaminedRanges[0].Length != 3 {
		t.Errorf("unexpected examined ranges: %+v", evidence.ExaminedRanges)
	}
}

func TestScannerAdapterSatisfiesContract(t *testing.T) {
	var _ scanner.Detector = (*ScannerDetector)(nil)

	adapter := &ScannerDetector{
		DetectorID:      "identify.builtin",
		DetectorVersion: RulesDigest(),
		Inner:           NewDetector(0),
	}
	ctx := context.Background()

	identified, err := adapter.Detect(ctx, scanner.DetectionInput{
		RelativePath: "photo.jpg",
		Probe:        []byte{0xff, 0xd8, 0xff, 0xe0},
	})
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if identified.FormatID != "jpeg" || identified.MediaType != "image/jpeg" {
		t.Errorf("identified result = %+v, want jpeg/image/jpeg", identified)
	}
	if identified.Confidence != 1 {
		t.Errorf("confidence = %v, want 1", identified.Confidence)
	}
	if len(identified.Evidence) != 2 {
		t.Fatalf("got %d evidence records, want suffix + magic", len(identified.Evidence))
	}
	if identified.Evidence[0].Method != "suffix" || identified.Evidence[1].Method != "magic" {
		t.Errorf("unexpected evidence methods: %+v", identified.Evidence)
	}

	unknown, err := adapter.Detect(ctx, scanner.DetectionInput{
		RelativePath: "no-clue.bin",
		Probe:        []byte{0xde, 0xad, 0xbe, 0xef},
	})
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if unknown.FormatID != "" || len(unknown.Evidence) != 0 {
		t.Errorf("unknown result = %+v, want no format and no evidence", unknown)
	}

	noop, err := (&ScannerDetector{DetectorID: "identify.builtin"}).Detect(ctx, scanner.DetectionInput{})
	if err != nil {
		t.Fatalf("no-op Detect returned error: %v", err)
	}
	if noop.DetectorID != "identify.builtin" || len(noop.Evidence) != 0 {
		t.Errorf("no-op result = %+v, want id only", noop)
	}
}
