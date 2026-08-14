package identify

import (
	"context"

	"github.com/ailiheizi/restoreweave/server/internal/scanner"
)

// ScannerDetector adapts the in-process Detector to the scanner host contract
// declared in server/internal/scanner/types.go.
//
// The scanner Detector interface does not take the (content digest, probe)
// pair directly: it passes the full DetectionInput, of which this adapter uses
// input.Content (the accepted content digest) and input.Probe (the bounded
// prefix captured during hashing), and derives the display name from
// input.RelativePath. Filesystem authority stays with the scanner host; the
// adapter never receives a path it can reopen.
type ScannerDetector struct {
	// DetectorID and DetectorVersion are recorded by the scanner host on
	// every detection observation.
	DetectorID      string
	DetectorVersion string

	// Inner is the identification detector. A nil Inner makes Detect a
	// no-op success, mirroring a detector that is not installed.
	Inner *Detector
}

var _ scanner.Detector = (*ScannerDetector)(nil)

// Detect implements scanner.Detector.
func (d *ScannerDetector) Detect(ctx context.Context, input scanner.DetectionInput) (scanner.DetectionResult, error) {
	if d.Inner == nil {
		return scanner.DetectionResult{
			DetectorID:      d.DetectorID,
			DetectorVersion: d.DetectorVersion,
		}, nil
	}
	if err := ctx.Err(); err != nil {
		return scanner.DetectionResult{}, err
	}
	result, err := d.Inner.Detect(ctx, input.RelativePath, input.Probe)
	if err != nil {
		return scanner.DetectionResult{}, err
	}
	return mapToScannerResult(result, d.DetectorID, d.DetectorVersion), nil
}

func mapToScannerResult(result IdentifyResult, detectorID, detectorVersion string) scanner.DetectionResult {
	out := scanner.DetectionResult{
		DetectorID:      detectorID,
		DetectorVersion: detectorVersion,
	}
	if result.State == IdentificationIdentified {
		for _, evidence := range result.Evidence {
			if evidence.Kind == EvidenceMagic && evidence.Complete {
				out.FormatID = evidence.Candidate.FormatID
				out.MediaType = evidence.Candidate.MIME
				break
			}
		}
		if out.FormatID != "" {
			out.Confidence = 1
		}
	}
	for _, evidence := range result.Evidence {
		method := "magic"
		if evidence.Kind == EvidenceSuffix {
			method = "suffix"
		}
		out.Evidence = append(out.Evidence, scanner.DetectionEvidence{
			Method: method,
			Value:  evidence.MatchRule,
		})
	}
	return out
}
