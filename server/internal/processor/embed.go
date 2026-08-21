package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TextEmbedding is an opt-in BYTE_DETERMINISTIC ENRICH processor. It is not
// a model runtime and not ContentIdentity. Default ingest does not register it.
type TextEmbedding struct{}

func (TextEmbedding) CapabilityID() string { return CapabilityTextEmbedding }
func (TextEmbedding) Stage() Stage         { return StageEnrich }

// ClipEmbedding is an opt-in joint-space fixture over audio tags. It is not
// CLIP weights and not ContentIdentity.
type ClipEmbedding struct{}

func (ClipEmbedding) CapabilityID() string { return CapabilityClipEmbedding }
func (ClipEmbedding) Stage() Stage         { return StageEnrich }

type featureEmbedding struct {
	Space              string `json:"space"`
	Version            string `json:"version"`
	Token              string `json:"token"`
	NotContentIdentity bool   `json:"not_content_identity"`
}

func (TextEmbedding) RunStage(ctx context.Context, inv Invocation) (StageResult, error) {
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
	return sealEmbedding(inv, FixtureTextSpace, FixtureEmbedding("sem1", string(body)), SchemaRefTextEmbedding())
}

func (ClipEmbedding) RunStage(ctx context.Context, inv Invocation) (StageResult, error) {
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
	track, ok := parseAudioTags(body)
	if !ok {
		return StageResult{Status: StatusInapplicable, Reason: "no audio tags for clip fixture"}, nil
	}
	text := ClipQueryText(track.Title, track.Artist)
	if text == "" {
		return StageResult{Status: StatusInapplicable, Reason: "audio tags have no title or artist"}, nil
	}
	return sealEmbedding(inv, FixtureClipSpace, FixtureEmbedding("clip1", text), SchemaRefClipEmbedding())
}

func sealEmbedding(inv Invocation, space, token, schemaRef string) (StageResult, error) {
	payload, err := json.Marshal(featureEmbedding{
		Space:              space,
		Version:            "1",
		Token:              token,
		NotContentIdentity: true,
	})
	if err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	if _, err := inv.Staging.Write(payload); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	if err := inv.Staging.Seal(); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	return StageResult{
		Status:           StatusSucceeded,
		DeterminismClass: DeterminismByteExact,
		SchemaRef:        schemaRef,
		MediaType:        MediaTypeEmbeddingJSON,
		Sealed:           true,
	}, nil
}

// FixtureEmbedding returns a coarse token over normalized text. It is not
// an embedding model output and not SHA-256.
func FixtureEmbedding(prefix, text string) string {
	return sketchPrefixed(prefix, []byte(normalizeFeatureText(text)))
}

// ClipQueryText is the joint-space query string for title plus artist.
func ClipQueryText(title, artist string) string {
	return strings.TrimSpace(strings.TrimSpace(title) + " " + strings.TrimSpace(artist))
}

func normalizeFeatureText(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
