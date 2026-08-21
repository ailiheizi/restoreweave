package processor

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// AudioFingerprint is an opt-in BYTE_DETERMINISTIC FINGERPRINT processor.
// It is not Chromaprint, not a Shazam client, and not ContentIdentity.
// Default ingest does not register it.
type AudioFingerprint struct{}

func (AudioFingerprint) CapabilityID() string { return CapabilityAudioFingerprint }
func (AudioFingerprint) Stage() Stage         { return StageFingerprint }

type acousticFingerprint struct {
	Algorithm          string `json:"algorithm"`
	Version            string `json:"version"`
	Fingerprint        string `json:"fingerprint"`
	NotContentIdentity bool   `json:"not_content_identity"`
}

func (AudioFingerprint) RunStage(ctx context.Context, inv Invocation) (StageResult, error) {
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
	if !looksLikeAudio(body) {
		return StageResult{Status: StatusInapplicable, Reason: "no audio container for fixture fingerprint"}, nil
	}
	payload, err := json.Marshal(acousticFingerprint{
		Algorithm:          FixtureFingerprintAlgorithm,
		Version:            "1",
		Fingerprint:        FixtureFingerprint(body),
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
		SchemaRef:        SchemaRefAcousticFingerprint(),
		MediaType:        MediaTypeFingerprintJSON,
		Sealed:           true,
	}, nil
}

// FixtureFingerprint returns a coarse 16-byte sketch. It is not SHA-256
// and must not be stored or compared as ContentIdentity.
func FixtureFingerprint(body []byte) string {
	return sketchPrefixed("fix1", body)
}

func sketchPrefixed(prefix string, body []byte) string {
	const buckets = 16
	var acc [buckets]byte
	n := len(body)
	if n == 0 {
		return prefix + ":" + hex.EncodeToString(acc[:])
	}
	for i := 0; i < n; i++ {
		j := i % buckets
		acc[j] ^= body[i]
		acc[j] += byte(i) + byte(n>>uint(j%8))
		acc[(j+3)%buckets] ^= byte(n + i*31)
	}
	return prefix + ":" + hex.EncodeToString(acc[:])
}

func looksLikeAudio(body []byte) bool {
	if _, ok := parseAudioTags(body); ok {
		return true
	}
	if len(body) >= 4 && string(body[:4]) == "fLaC" {
		return true
	}
	if len(body) >= 4 && string(body[:4]) == "OggS" {
		return true
	}
	if len(body) >= 3 && string(body[:3]) == "ID3" {
		return true
	}
	return false
}

// ParseAcousticFingerprint reads an admitted FINGERPRINT artifact body.
func ParseAcousticFingerprint(body string) (fingerprint, algorithm string, ok bool) {
	record, ok := parseAcousticFingerprint(body)
	if !ok {
		return "", "", false
	}
	return record.Fingerprint, record.Algorithm, true
}

func parseAcousticFingerprint(body string) (acousticFingerprint, bool) {
	var record acousticFingerprint
	if err := json.Unmarshal([]byte(body), &record); err != nil {
		return record, false
	}
	record.Fingerprint = strings.TrimSpace(record.Fingerprint)
	if record.Fingerprint == "" || !record.NotContentIdentity {
		return record, false
	}
	if record.Algorithm == "" {
		record.Algorithm = FixtureFingerprintAlgorithm
	}
	return record, true
}
