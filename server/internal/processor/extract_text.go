package processor

import (
	"bytes"
	"context"
	"unicode/utf8"
)

// TextExtract is the bundled BYTE_DETERMINISTIC EXTRACT processor. It copies
// valid UTF-8 source bytes into a host staging handle and never opens paths.
type TextExtract struct{}

func (TextExtract) CapabilityID() string { return CapabilityTextExtract }
func (TextExtract) Stage() Stage         { return StageExtract }

func (TextExtract) RunStage(ctx context.Context, inv Invocation) (StageResult, error) {
	if err := ctx.Err(); err != nil {
		return StageResult{Status: StatusCancelled, Reason: err.Error()}, err
	}
	if inv.Source == nil || inv.Staging == nil {
		return StageResult{Status: StatusFailed, Reason: "source and staging handles are required"}, nil
	}
	body, err := inv.Source.ReadAll(ctx)
	if err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(body) {
		return StageResult{Status: StatusInapplicable, Reason: "source is not valid UTF-8"}, nil
	}
	if _, err := inv.Staging.Write(body); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	if err := inv.Staging.Seal(); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	return StageResult{
		Status:           StatusSucceeded,
		DeterminismClass: DeterminismByteExact,
		SchemaRef:        SchemaRefExtractedText(),
		MediaType:        MediaTypeUTF8Text,
		Sealed:           true,
	}, nil
}
