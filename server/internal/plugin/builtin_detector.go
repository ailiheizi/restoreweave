package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	BuiltinCorePackageID         = "org.restoreweave.core"
	BuiltinIdentificationEntryID = "detector.extension-magic.v1"
)

type BuiltinDetectorConfig struct {
	PackageDigest       Digest
	RuntimeDigest       Digest
	SandboxPolicyDigest Digest
	Clock               func() time.Time
}

type BuiltinIdentificationDetector struct {
	entryPoint          EntryPointRef
	sandboxPolicyDigest Digest
	clock               func() time.Time
}

func NewBuiltinIdentificationDetector(config BuiltinDetectorConfig) (*BuiltinIdentificationDetector, error) {
	for name, digest := range map[string]Digest{
		"package_digest":        config.PackageDigest,
		"runtime_digest":        config.RuntimeDigest,
		"sandbox_policy_digest": config.SandboxPolicyDigest,
	} {
		if err := digest.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	return &BuiltinIdentificationDetector{
		entryPoint: EntryPointRef{
			PackageID:     BuiltinCorePackageID,
			PackageDigest: config.PackageDigest,
			EntryPointID:  BuiltinIdentificationEntryID,
			RulesDigest:   BuiltinDetectionRulesDigest(),
			RuntimeDigest: config.RuntimeDigest,
		},
		sandboxPolicyDigest: config.SandboxPolicyDigest,
		clock:               clock,
	}, nil
}

func (d *BuiltinIdentificationDetector) EntryPoint() EntryPointRef {
	return d.entryPoint
}

func (d *BuiltinIdentificationDetector) Detect(ctx context.Context, request DetectionRequest) (DetectionResult, error) {
	if err := request.Validate(); err != nil {
		return DetectionResult{}, fmt.Errorf("invalid detection request: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return DetectionResult{}, err
	}

	startedAt := d.clock().UTC()
	evidence := make([]DetectionEvidence, 0, 4)

	if suffix, candidates := matchSuffix(request.DisplayName); suffix != "" {
		for _, candidate := range candidates {
			evidence = append(evidence, DetectionEvidence{
				Subject:    request.Subject,
				Detector:   d.entryPoint,
				Kind:       EvidenceSuffix,
				Candidate:  candidate,
				Confidence: Confidence{Semantics: ConfidenceHeuristicHint},
				ExaminedFacts: []ExaminedFact{{
					Name:  "path.suffix",
					Value: suffix,
				}},
				MatchRule:   "suffix:" + suffix,
				MatchDigest: digestText("suffix:" + suffix + ":" + candidateKey(candidate)),
				Complete:    true,
			})
		}
	}

	for _, rule := range builtinMagicRules {
		match, ok := rule.match(request.Samples)
		if !ok {
			continue
		}
		confidence := Confidence{Semantics: ConfidenceDeterministicRule}
		if !match.complete {
			confidence.Semantics = ConfidencePartialRule
		}
		evidence = append(evidence, DetectionEvidence{
			Subject:                  request.Subject,
			Detector:                 d.entryPoint,
			Kind:                     EvidenceMagic,
			Candidate:                rule.candidate,
			Confidence:               confidence,
			ExaminedRanges:           match.examined,
			RequiredBytesUnavailable: match.unavailable,
			MatchRule:                "magic:" + rule.id,
			MatchDigest:              rule.digest(),
			Complete:                 match.complete,
		})
	}

	finishedAt := d.clock().UTC()
	if finishedAt.Before(startedAt) {
		finishedAt = startedAt
	}
	bytesRead := uint64(0)
	for _, sample := range request.Samples {
		bytesRead += uint64(len(sample.Data))
	}
	for i := range evidence {
		evidence[i].Execution = ExecutionMetadata{
			Class:               ExecutionByteDeterministic,
			StartedAt:           startedAt,
			FinishedAt:          finishedAt,
			SandboxPolicyDigest: d.sandboxPolicyDigest,
			ResourceUse:         ResourceUse{BytesRead: bytesRead},
		}
	}

	result := DetectionResult{
		State:    classifyBuiltinEvidence(evidence),
		Evidence: evidence,
	}
	if err := result.Validate(); err != nil {
		return DetectionResult{}, fmt.Errorf("builtin detector produced invalid evidence: %w", err)
	}
	return result, nil
}

func classifyBuiltinEvidence(evidence []DetectionEvidence) IdentificationState {
	if len(evidence) == 0 {
		return IdentificationUnknown
	}

	exactMagicFamilies := make(map[string]struct{})
	exactMagicFormats := make(map[string]struct{})
	partialMagicFamilies := make(map[string]struct{})
	suffixFamilies := make(map[string]struct{})
	suffixFormats := make(map[string]struct{})
	for _, item := range evidence {
		switch item.Kind {
		case EvidenceMagic:
			if item.Complete {
				exactMagicFamilies[item.Candidate.comparisonFamily()] = struct{}{}
				exactMagicFormats[item.Candidate.FormatID] = struct{}{}
			} else {
				partialMagicFamilies[item.Candidate.comparisonFamily()] = struct{}{}
			}
		case EvidenceSuffix:
			suffixFamilies[item.Candidate.comparisonFamily()] = struct{}{}
			suffixFormats[item.Candidate.FormatID] = struct{}{}
		}
	}

	if len(exactMagicFamilies) == 0 {
		if len(partialMagicFamilies) > 0 || len(suffixFamilies) > 0 {
			return IdentificationAmbiguous
		}
		return IdentificationUnknown
	}
	if len(exactMagicFamilies) > 1 {
		return IdentificationAmbiguous
	}
	for suffixFamily := range suffixFamilies {
		if _, compatible := exactMagicFamilies[suffixFamily]; !compatible {
			return IdentificationConflictingEvidence
		}
	}
	for partialFamily := range partialMagicFamilies {
		if _, compatible := exactMagicFamilies[partialFamily]; !compatible {
			return IdentificationAmbiguous
		}
	}
	if len(suffixFormats) > 0 {
		if len(suffixFormats) != 1 || len(exactMagicFormats) != 1 {
			return IdentificationAmbiguous
		}
		for suffixFormat := range suffixFormats {
			if _, exact := exactMagicFormats[suffixFormat]; !exact {
				return IdentificationAmbiguous
			}
		}
	}
	return IdentificationIdentified
}

// BuiltinIdentificationEntryPointManifest describes the trusted in-process
// detector. The caller combines it with signed package-level release metadata.
func BuiltinIdentificationEntryPointManifest() EntryPointManifest {
	formats := make([]string, 0, len(extensionRules)+len(builtinMagicRules))
	seen := make(map[string]struct{})
	for _, candidates := range extensionRules {
		for _, candidate := range candidates {
			if _, ok := seen[candidate.FormatID]; !ok {
				seen[candidate.FormatID] = struct{}{}
				formats = append(formats, candidate.FormatID)
			}
		}
	}
	for _, rule := range builtinMagicRules {
		if _, ok := seen[rule.candidate.FormatID]; !ok {
			seen[rule.candidate.FormatID] = struct{}{}
			formats = append(formats, rule.candidate.FormatID)
		}
	}
	sort.Strings(formats)

	return EntryPointManifest{
		ID:                  BuiltinIdentificationEntryID,
		Name:                "Built-in extension and magic detector",
		Category:            CategoryDetector,
		TransformationClass: TransformationNotApplicable,
		ExecutionClass:      ExecutionByteDeterministic,
		Inputs: []PortDeclaration{{
			Name: "request", Type: PortDetectionRequest,
			SchemaID: "restoreweave.detection-request/v1", Required: true,
		}},
		Outputs: []PortDeclaration{{
			Name: "evidence", Type: PortDetectionEvidence,
			SchemaID: "restoreweave.detection-evidence/v1", Required: true,
		}},
		Support: SupportDeclaration{Formats: formats},
		Capabilities: CapabilitySet{
			{Name: CapabilityReadInputMetadata, Required: true},
			{Name: CapabilityReadInputSamples, Required: true},
			{Name: CapabilityEmitDetection, Required: true},
		},
		Runtime: RuntimeDescriptor{
			Kind:       RuntimeBuiltin,
			Lifecycle:  RuntimeOneShot,
			AdapterID:  "restoreweave.builtin.v1",
			Protocol:   "restoreweave.internal/v1",
			Entrypoint: BuiltinIdentificationEntryID,
		},
		Enablement:          EnablementEnabled,
		Qualification:       QualificationQualified,
		CanExecuteNewWork:   true,
		CanDecodeHistorical: false,
		ConfigurationSchema: "restoreweave.detector.extension-magic.config/v1",
		ConformanceSuiteID:  "restoreweave.detector.extension-magic.conformance/v1",
	}
}

type magicPart struct {
	offset  uint64
	pattern []byte
}

type magicPattern []magicPart

type magicRule struct {
	id           string
	candidate    FormatCandidate
	alternatives []magicPattern
}

type magicMatch struct {
	complete    bool
	examined    []ByteRange
	unavailable []ByteRange
	matched     int
}

func (r magicRule) match(samples []ByteSample) (magicMatch, bool) {
	best := magicMatch{}
	found := false
	for _, pattern := range r.alternatives {
		candidate, ok := matchMagicPattern(samples, pattern)
		if !ok {
			continue
		}
		if !found || candidate.complete && !best.complete ||
			candidate.complete == best.complete && candidate.matched > best.matched {
			best = candidate
			found = true
		}
	}
	return best, found
}

func matchMagicPattern(samples []ByteSample, pattern magicPattern) (magicMatch, bool) {
	const minimumPartialBytes = 2
	match := magicMatch{complete: true}
	for _, part := range pattern {
		available := bytesAt(samples, part.offset, len(part.pattern))
		if len(available) > 0 {
			if !bytes.Equal(available, part.pattern[:len(available)]) {
				return magicMatch{}, false
			}
			match.examined = append(match.examined, ByteRange{
				Offset: part.offset, Length: uint64(len(available)),
			})
			match.matched += len(available)
		}
		if len(available) < len(part.pattern) {
			match.complete = false
			missingOffset := part.offset + uint64(len(available))
			match.unavailable = append(match.unavailable, ByteRange{
				Offset: missingOffset,
				Length: uint64(len(part.pattern) - len(available)),
			})
			break
		}
	}
	if match.complete {
		return match, true
	}
	if match.matched < minimumPartialBytes {
		return magicMatch{}, false
	}
	return match, true
}

func bytesAt(samples []ByteSample, offset uint64, length int) []byte {
	if length <= 0 {
		return nil
	}
	result := make([]byte, 0, length)
	for position := offset; len(result) < length; position++ {
		found := false
		for _, sample := range samples {
			if position < sample.Offset {
				continue
			}
			index := position - sample.Offset
			if index >= uint64(len(sample.Data)) {
				continue
			}
			result = append(result, sample.Data[index])
			found = true
			break
		}
		if !found || position == ^uint64(0) {
			break
		}
	}
	return result
}

func (r magicRule) digest() Digest {
	var canonical strings.Builder
	canonical.WriteString(r.id)
	canonical.WriteByte('\n')
	canonical.WriteString(candidateKey(r.candidate))
	for _, alternative := range r.alternatives {
		canonical.WriteString("\nalt")
		for _, part := range alternative {
			fmt.Fprintf(&canonical, "\n%d:%s", part.offset, hex.EncodeToString(part.pattern))
		}
	}
	return digestText(canonical.String())
}

func BuiltinDetectionRulesDigest() Digest {
	var canonical strings.Builder
	suffixes := make([]string, 0, len(extensionRules))
	for suffix := range extensionRules {
		suffixes = append(suffixes, suffix)
	}
	sort.Strings(suffixes)
	for _, suffix := range suffixes {
		canonical.WriteString("suffix:")
		canonical.WriteString(suffix)
		for _, candidate := range extensionRules[suffix] {
			canonical.WriteByte(':')
			canonical.WriteString(candidateKey(candidate))
		}
		canonical.WriteByte('\n')
	}
	for _, rule := range builtinMagicRules {
		canonical.WriteString("magic:")
		canonical.WriteString(string(rule.digest()))
		canonical.WriteByte('\n')
	}
	return digestText(canonical.String())
}

func digestText(value string) Digest {
	digest := sha256.Sum256([]byte(value))
	return Digest("sha256:" + hex.EncodeToString(digest[:]))
}

func candidateKey(candidate FormatCandidate) string {
	return strings.Join([]string{
		candidate.FormatID, candidate.FamilyID, candidate.VersionRange,
		candidate.MIME, candidate.Producer,
	}, "|")
}

func matchSuffix(displayName string) (string, []FormatCandidate) {
	base := strings.ToLower(filepath.Base(displayName))
	for _, suffix := range sortedSuffixes {
		if strings.HasSuffix(base, suffix) {
			return suffix, append([]FormatCandidate(nil), extensionRules[suffix]...)
		}
	}
	return "", nil
}

func candidate(formatID, familyID, mime string) FormatCandidate {
	return FormatCandidate{FormatID: formatID, FamilyID: familyID, MIME: mime}
}

var extensionRules = map[string][]FormatCandidate{
	".7z":      {candidate("7z", "7z", "application/x-7z-compressed")},
	".apk":     {candidate("android-apk", "zip", "application/vnd.android.package-archive")},
	".avi":     {candidate("avi", "riff", "video/x-msvideo")},
	".bz2":     {candidate("bzip2", "bzip2", "application/x-bzip2")},
	".db":      {candidate("sqlite", "sqlite", "application/vnd.sqlite3")},
	".dmg":     {candidate("apple-disk-image", "apple-disk-image", "application/x-apple-diskimage")},
	".docx":    {candidate("ooxml-document", "zip", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")},
	".elf":     {candidate("elf", "elf", "application/x-elf")},
	".flac":    {candidate("flac", "flac", "audio/flac")},
	".gif":     {candidate("gif", "gif", "image/gif")},
	".gz":      {candidate("gzip", "gzip", "application/gzip")},
	".jpeg":    {candidate("jpeg", "jpeg", "image/jpeg")},
	".jpg":     {candidate("jpeg", "jpeg", "image/jpeg")},
	".json":    {candidate("json", "json", "application/json")},
	".m4a":     {candidate("iso-bmff-audio", "iso-bmff", "audio/mp4")},
	".mkv":     {candidate("matroska", "ebml", "video/x-matroska")},
	".mov":     {candidate("quicktime", "iso-bmff", "video/quicktime")},
	".mp3":     {candidate("mp3", "mp3", "audio/mpeg")},
	".mp4":     {candidate("mp4", "iso-bmff", "video/mp4")},
	".ogg":     {candidate("ogg", "ogg", "application/ogg")},
	".pdf":     {candidate("pdf", "pdf", "application/pdf")},
	".png":     {candidate("png", "png", "image/png")},
	".pptx":    {candidate("ooxml-presentation", "zip", "application/vnd.openxmlformats-officedocument.presentationml.presentation")},
	".rar":     {candidate("rar", "rar", "application/vnd.rar")},
	".sqlite":  {candidate("sqlite", "sqlite", "application/vnd.sqlite3")},
	".sqlite3": {candidate("sqlite", "sqlite", "application/vnd.sqlite3")},
	".tar":     {candidate("tar", "tar", "application/x-tar")},
	".tar.gz":  {candidate("tar-gzip", "gzip", "application/gzip")},
	".tgz":     {candidate("tar-gzip", "gzip", "application/gzip")},
	".txt":     {candidate("plain-text", "plain-text", "text/plain")},
	".wasm":    {candidate("webassembly", "webassembly", "application/wasm")},
	".wav":     {candidate("wave", "riff", "audio/wav")},
	".webm":    {candidate("webm", "ebml", "video/webm")},
	".webp":    {candidate("webp", "riff", "image/webp")},
	".xlsx":    {candidate("ooxml-workbook", "zip", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")},
	".xml":     {candidate("xml", "xml", "application/xml")},
	".xz":      {candidate("xz", "xz", "application/x-xz")},
	".zip":     {candidate("zip", "zip", "application/zip")},
	".zst":     {candidate("zstandard", "zstandard", "application/zstd")},
}

var sortedSuffixes = func() []string {
	result := make([]string, 0, len(extensionRules))
	for suffix := range extensionRules {
		result = append(result, suffix)
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i]) == len(result[j]) {
			return result[i] < result[j]
		}
		return len(result[i]) > len(result[j])
	})
	return result
}()

func pattern(offset uint64, value ...byte) magicPattern {
	return magicPattern{{offset: offset, pattern: value}}
}

func multipart(parts ...magicPart) magicPattern { return magicPattern(parts) }

var builtinMagicRules = []magicRule{
	{id: "jpeg-soi", candidate: candidate("jpeg", "jpeg", "image/jpeg"), alternatives: []magicPattern{pattern(0, 0xff, 0xd8, 0xff)}},
	{id: "png-signature", candidate: candidate("png", "png", "image/png"), alternatives: []magicPattern{pattern(0, 0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a)}},
	{id: "gif-header", candidate: candidate("gif", "gif", "image/gif"), alternatives: []magicPattern{pattern(0, 'G', 'I', 'F', '8', '7', 'a'), pattern(0, 'G', 'I', 'F', '8', '9', 'a')}},
	{id: "zip-local-or-directory", candidate: candidate("zip", "zip", "application/zip"), alternatives: []magicPattern{pattern(0, 'P', 'K', 0x03, 0x04), pattern(0, 'P', 'K', 0x05, 0x06), pattern(0, 'P', 'K', 0x07, 0x08)}},
	{id: "pdf-header", candidate: candidate("pdf", "pdf", "application/pdf"), alternatives: []magicPattern{pattern(0, '%', 'P', 'D', 'F', '-')}},
	{id: "gzip-header", candidate: candidate("gzip", "gzip", "application/gzip"), alternatives: []magicPattern{pattern(0, 0x1f, 0x8b, 0x08)}},
	{id: "bzip2-header", candidate: candidate("bzip2", "bzip2", "application/x-bzip2"), alternatives: []magicPattern{pattern(0, 'B', 'Z', 'h')}},
	{id: "xz-header", candidate: candidate("xz", "xz", "application/x-xz"), alternatives: []magicPattern{pattern(0, 0xfd, '7', 'z', 'X', 'Z', 0x00)}},
	{id: "zstandard-frame", candidate: candidate("zstandard", "zstandard", "application/zstd"), alternatives: []magicPattern{pattern(0, 0x28, 0xb5, 0x2f, 0xfd)}},
	{id: "7z-signature", candidate: candidate("7z", "7z", "application/x-7z-compressed"), alternatives: []magicPattern{pattern(0, '7', 'z', 0xbc, 0xaf, 0x27, 0x1c)}},
	{id: "rar-signature", candidate: candidate("rar", "rar", "application/vnd.rar"), alternatives: []magicPattern{pattern(0, 'R', 'a', 'r', '!', 0x1a, 0x07, 0x00), pattern(0, 'R', 'a', 'r', '!', 0x1a, 0x07, 0x01, 0x00)}},
	{id: "flac-stream", candidate: candidate("flac", "flac", "audio/flac"), alternatives: []magicPattern{pattern(0, 'f', 'L', 'a', 'C')}},
	{id: "ogg-page", candidate: candidate("ogg", "ogg", "application/ogg"), alternatives: []magicPattern{pattern(0, 'O', 'g', 'g', 'S')}},
	{id: "id3v2-header", candidate: candidate("id3v2-tagged", "mp3", "application/octet-stream"), alternatives: []magicPattern{pattern(0, 'I', 'D', '3')}},
	{id: "ebml-header", candidate: candidate("ebml", "ebml", "application/octet-stream"), alternatives: []magicPattern{pattern(0, 0x1a, 0x45, 0xdf, 0xa3)}},
	{id: "iso-bmff-ftyp", candidate: candidate("iso-bmff", "iso-bmff", "application/octet-stream"), alternatives: []magicPattern{pattern(4, 'f', 't', 'y', 'p')}},
	{id: "sqlite3-header", candidate: candidate("sqlite", "sqlite", "application/vnd.sqlite3"), alternatives: []magicPattern{pattern(0, 'S', 'Q', 'L', 'i', 't', 'e', ' ', 'f', 'o', 'r', 'm', 'a', 't', ' ', '3', 0x00)}},
	{id: "tar-ustar", candidate: candidate("tar", "tar", "application/x-tar"), alternatives: []magicPattern{pattern(257, 'u', 's', 't', 'a', 'r')}},
	{id: "webassembly-module", candidate: candidate("webassembly", "webassembly", "application/wasm"), alternatives: []magicPattern{pattern(0, 0x00, 'a', 's', 'm')}},
	{id: "elf-header", candidate: candidate("elf", "elf", "application/x-elf"), alternatives: []magicPattern{pattern(0, 0x7f, 'E', 'L', 'F')}},
	{id: "mach-o-header", candidate: candidate("mach-o", "mach-o", "application/x-mach-binary"), alternatives: []magicPattern{pattern(0, 0xfe, 0xed, 0xfa, 0xce), pattern(0, 0xce, 0xfa, 0xed, 0xfe), pattern(0, 0xfe, 0xed, 0xfa, 0xcf), pattern(0, 0xcf, 0xfa, 0xed, 0xfe), pattern(0, 0xca, 0xfe, 0xba, 0xbe)}},
	{id: "riff-wave", candidate: candidate("wave", "riff", "audio/wav"), alternatives: []magicPattern{multipart(magicPart{offset: 0, pattern: []byte("RIFF")}, magicPart{offset: 8, pattern: []byte("WAVE")})}},
	{id: "riff-webp", candidate: candidate("webp", "riff", "image/webp"), alternatives: []magicPattern{multipart(magicPart{offset: 0, pattern: []byte("RIFF")}, magicPart{offset: 8, pattern: []byte("WEBP")})}},
	{id: "riff-avi", candidate: candidate("avi", "riff", "video/x-msvideo"), alternatives: []magicPattern{multipart(magicPart{offset: 0, pattern: []byte("RIFF")}, magicPart{offset: 8, pattern: []byte("AVI ")})}},
}

var _ Detector = (*BuiltinIdentificationDetector)(nil)
