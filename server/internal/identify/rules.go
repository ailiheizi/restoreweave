package identify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// FormatCandidate is one physical-format claim. The version and producer
// fields are retained from the legacy rule data even though every built-in
// rule leaves them empty, so the rule tables migrate without value changes.
type FormatCandidate struct {
	FormatID     string `json:"format_id"`
	FamilyID     string `json:"family_id,omitempty"`
	VersionRange string `json:"version_range,omitempty"`
	MIME         string `json:"mime,omitempty"`
	Producer     string `json:"producer,omitempty"`
}

// comparisonFamily returns the family used to decide whether two candidates
// agree, falling back to the format ID when no family is declared.
func (c FormatCandidate) comparisonFamily() string {
	if c.FamilyID != "" {
		return c.FamilyID
	}
	return c.FormatID
}

// SuffixRule maps one filename suffix to its candidate formats. Suffixes are
// ordered longest-first so ".tar.gz" wins over ".gz".
type SuffixRule struct {
	Suffix     string            `json:"suffix"`
	Candidates []FormatCandidate `json:"candidates"`
}

// MagicPart is one byte pattern at a fixed offset within the probe.
type MagicPart struct {
	Offset  uint64 `json:"offset"`
	Pattern []byte `json:"pattern"`
}

// MagicPattern is a sequence of parts that must all match.
type MagicPattern []MagicPart

// MagicRule is one deterministic magic-byte rule with one or more alternative
// patterns, pinned by a canonical rule digest.
type MagicRule struct {
	ID           string          `json:"id"`
	Candidate    FormatCandidate `json:"candidate"`
	Alternatives []MagicPattern  `json:"alternatives"`
}

// SuffixRules returns the built-in suffix table in deterministic match order
// (longest suffix first, then lexicographic).
func SuffixRules() []SuffixRule {
	result := make([]SuffixRule, len(suffixRuleTable))
	copy(result, suffixRuleTable)
	return result
}

// MagicRules returns the built-in magic table in deterministic order.
func MagicRules() []MagicRule {
	result := make([]MagicRule, len(magicRuleTable))
	copy(result, magicRuleTable)
	return result
}

// RulesDigest pins the complete rule table. Callers that durably record a
// detection result SHOULD record this digest alongside it.
func RulesDigest() string {
	var canonical strings.Builder
	for _, rule := range suffixRuleTable {
		canonical.WriteString("suffix:")
		canonical.WriteString(rule.Suffix)
		for _, candidate := range rule.Candidates {
			canonical.WriteByte(':')
			canonical.WriteString(candidateKey(candidate))
		}
		canonical.WriteByte('\n')
	}
	for _, rule := range magicRuleTable {
		canonical.WriteString("magic:")
		canonical.WriteString(string(rule.digest()))
		canonical.WriteByte('\n')
	}
	return digestText(canonical.String())
}

// candidateKey is the canonical serialization used for rule digests.
func candidateKey(candidate FormatCandidate) string {
	return strings.Join([]string{
		candidate.FormatID, candidate.FamilyID, candidate.VersionRange,
		candidate.MIME, candidate.Producer,
	}, "|")
}

// digestText computes the sha256 digest in the same algorithm-qualified form
// used across the codebase.
func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// candidate builds a candidate with the fields the built-in rules declare.
func candidate(formatID, familyID, mime string) FormatCandidate {
	return FormatCandidate{FormatID: formatID, FamilyID: familyID, MIME: mime}
}

func pattern(offset uint64, value ...byte) MagicPattern {
	return MagicPattern{{Offset: offset, Pattern: value}}
}

func multipart(parts ...MagicPart) MagicPattern { return MagicPattern(parts) }

// suffixRuleTable holds 41 suffix rules: 39 migrated unchanged from the legacy
// server/internal/plugin builtin detector, plus .epub and .md for the book catalog.
var suffixRuleTable = func() []SuffixRule {
	table := []SuffixRule{
		{".7z", []FormatCandidate{candidate("7z", "7z", "application/x-7z-compressed")}},
		{".apk", []FormatCandidate{candidate("android-apk", "zip", "application/vnd.android.package-archive")}},
		{".avi", []FormatCandidate{candidate("avi", "riff", "video/x-msvideo")}},
		{".bz2", []FormatCandidate{candidate("bzip2", "bzip2", "application/x-bzip2")}},
		{".db", []FormatCandidate{candidate("sqlite", "sqlite", "application/vnd.sqlite3")}},
		{".dmg", []FormatCandidate{candidate("apple-disk-image", "apple-disk-image", "application/x-apple-diskimage")}},
		{".docx", []FormatCandidate{candidate("ooxml-document", "zip", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")}},
		{".elf", []FormatCandidate{candidate("elf", "elf", "application/x-elf")}},
		{".epub", []FormatCandidate{candidate("epub", "zip", "application/epub+zip")}},
		{".flac", []FormatCandidate{candidate("flac", "flac", "audio/flac")}},
		{".gif", []FormatCandidate{candidate("gif", "gif", "image/gif")}},
		{".gz", []FormatCandidate{candidate("gzip", "gzip", "application/gzip")}},
		{".jpeg", []FormatCandidate{candidate("jpeg", "jpeg", "image/jpeg")}},
		{".jpg", []FormatCandidate{candidate("jpeg", "jpeg", "image/jpeg")}},
		{".json", []FormatCandidate{candidate("json", "json", "application/json")}},
		{".m4a", []FormatCandidate{candidate("iso-bmff-audio", "iso-bmff", "audio/mp4")}},
		{".md", []FormatCandidate{candidate("markdown", "plain-text", "text/plain")}},
		{".mkv", []FormatCandidate{candidate("matroska", "ebml", "video/x-matroska")}},
		{".mov", []FormatCandidate{candidate("quicktime", "iso-bmff", "video/quicktime")}},
		{".mp3", []FormatCandidate{candidate("mp3", "mp3", "audio/mpeg")}},
		{".mp4", []FormatCandidate{candidate("mp4", "iso-bmff", "video/mp4")}},
		{".ogg", []FormatCandidate{candidate("ogg", "ogg", "application/ogg")}},
		{".pdf", []FormatCandidate{candidate("pdf", "pdf", "application/pdf")}},
		{".png", []FormatCandidate{candidate("png", "png", "image/png")}},
		{".pptx", []FormatCandidate{candidate("ooxml-presentation", "zip", "application/vnd.openxmlformats-officedocument.presentationml.presentation")}},
		{".rar", []FormatCandidate{candidate("rar", "rar", "application/vnd.rar")}},
		{".sqlite", []FormatCandidate{candidate("sqlite", "sqlite", "application/vnd.sqlite3")}},
		{".sqlite3", []FormatCandidate{candidate("sqlite", "sqlite", "application/vnd.sqlite3")}},
		{".tar", []FormatCandidate{candidate("tar", "tar", "application/x-tar")}},
		{".tar.gz", []FormatCandidate{candidate("tar-gzip", "gzip", "application/gzip")}},
		{".tgz", []FormatCandidate{candidate("tar-gzip", "gzip", "application/gzip")}},
		{".txt", []FormatCandidate{candidate("plain-text", "plain-text", "text/plain")}},
		{".wasm", []FormatCandidate{candidate("webassembly", "webassembly", "application/wasm")}},
		{".wav", []FormatCandidate{candidate("wave", "riff", "audio/wav")}},
		{".webm", []FormatCandidate{candidate("webm", "ebml", "video/webm")}},
		{".webp", []FormatCandidate{candidate("webp", "riff", "image/webp")}},
		{".xlsx", []FormatCandidate{candidate("ooxml-workbook", "zip", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")}},
		{".xml", []FormatCandidate{candidate("xml", "xml", "application/xml")}},
		{".xz", []FormatCandidate{candidate("xz", "xz", "application/x-xz")}},
		{".zip", []FormatCandidate{candidate("zip", "zip", "application/zip")}},
		{".zst", []FormatCandidate{candidate("zstandard", "zstandard", "application/zstd")}},
	}
	sort.Slice(table, func(i, j int) bool {
		if len(table[i].Suffix) == len(table[j].Suffix) {
			return table[i].Suffix < table[j].Suffix
		}
		return len(table[i].Suffix) > len(table[j].Suffix)
	})
	return table
}()

// magicRuleTable holds the 25 magic rules migrated unchanged from the legacy
// server/internal/plugin builtin detector.
var magicRuleTable = []MagicRule{
	{ID: "jpeg-soi", Candidate: candidate("jpeg", "jpeg", "image/jpeg"), Alternatives: []MagicPattern{pattern(0, 0xff, 0xd8, 0xff)}},
	{ID: "png-signature", Candidate: candidate("png", "png", "image/png"), Alternatives: []MagicPattern{pattern(0, 0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a)}},
	{ID: "gif-header", Candidate: candidate("gif", "gif", "image/gif"), Alternatives: []MagicPattern{pattern(0, 'G', 'I', 'F', '8', '7', 'a'), pattern(0, 'G', 'I', 'F', '8', '9', 'a')}},
	{ID: "zip-local-or-directory", Candidate: candidate("zip", "zip", "application/zip"), Alternatives: []MagicPattern{pattern(0, 'P', 'K', 0x03, 0x04), pattern(0, 'P', 'K', 0x05, 0x06), pattern(0, 'P', 'K', 0x07, 0x08)}},
	{ID: "pdf-header", Candidate: candidate("pdf", "pdf", "application/pdf"), Alternatives: []MagicPattern{pattern(0, '%', 'P', 'D', 'F', '-')}},
	{ID: "gzip-header", Candidate: candidate("gzip", "gzip", "application/gzip"), Alternatives: []MagicPattern{pattern(0, 0x1f, 0x8b, 0x08)}},
	{ID: "bzip2-header", Candidate: candidate("bzip2", "bzip2", "application/x-bzip2"), Alternatives: []MagicPattern{pattern(0, 'B', 'Z', 'h')}},
	{ID: "xz-header", Candidate: candidate("xz", "xz", "application/x-xz"), Alternatives: []MagicPattern{pattern(0, 0xfd, '7', 'z', 'X', 'Z', 0x00)}},
	{ID: "zstandard-frame", Candidate: candidate("zstandard", "zstandard", "application/zstd"), Alternatives: []MagicPattern{pattern(0, 0x28, 0xb5, 0x2f, 0xfd)}},
	{ID: "7z-signature", Candidate: candidate("7z", "7z", "application/x-7z-compressed"), Alternatives: []MagicPattern{pattern(0, '7', 'z', 0xbc, 0xaf, 0x27, 0x1c)}},
	{ID: "rar-signature", Candidate: candidate("rar", "rar", "application/vnd.rar"), Alternatives: []MagicPattern{pattern(0, 'R', 'a', 'r', '!', 0x1a, 0x07, 0x00), pattern(0, 'R', 'a', 'r', '!', 0x1a, 0x07, 0x01, 0x00)}},
	{ID: "flac-stream", Candidate: candidate("flac", "flac", "audio/flac"), Alternatives: []MagicPattern{pattern(0, 'f', 'L', 'a', 'C')}},
	{ID: "ogg-page", Candidate: candidate("ogg", "ogg", "application/ogg"), Alternatives: []MagicPattern{pattern(0, 'O', 'g', 'g', 'S')}},
	{ID: "id3v2-header", Candidate: candidate("id3v2-tagged", "mp3", "application/octet-stream"), Alternatives: []MagicPattern{pattern(0, 'I', 'D', '3')}},
	{ID: "ebml-header", Candidate: candidate("ebml", "ebml", "application/octet-stream"), Alternatives: []MagicPattern{pattern(0, 0x1a, 0x45, 0xdf, 0xa3)}},
	{ID: "iso-bmff-ftyp", Candidate: candidate("iso-bmff", "iso-bmff", "application/octet-stream"), Alternatives: []MagicPattern{pattern(4, 'f', 't', 'y', 'p')}},
	{ID: "sqlite3-header", Candidate: candidate("sqlite", "sqlite", "application/vnd.sqlite3"), Alternatives: []MagicPattern{pattern(0, 'S', 'Q', 'L', 'i', 't', 'e', ' ', 'f', 'o', 'r', 'm', 'a', 't', ' ', '3', 0x00)}},
	{ID: "tar-ustar", Candidate: candidate("tar", "tar", "application/x-tar"), Alternatives: []MagicPattern{pattern(257, 'u', 's', 't', 'a', 'r')}},
	{ID: "webassembly-module", Candidate: candidate("webassembly", "webassembly", "application/wasm"), Alternatives: []MagicPattern{pattern(0, 0x00, 'a', 's', 'm')}},
	{ID: "elf-header", Candidate: candidate("elf", "elf", "application/x-elf"), Alternatives: []MagicPattern{pattern(0, 0x7f, 'E', 'L', 'F')}},
	{ID: "mach-o-header", Candidate: candidate("mach-o", "mach-o", "application/x-mach-binary"), Alternatives: []MagicPattern{pattern(0, 0xfe, 0xed, 0xfa, 0xce), pattern(0, 0xce, 0xfa, 0xed, 0xfe), pattern(0, 0xfe, 0xed, 0xfa, 0xcf), pattern(0, 0xcf, 0xfa, 0xed, 0xfe), pattern(0, 0xca, 0xfe, 0xba, 0xbe)}},
	{ID: "riff-wave", Candidate: candidate("wave", "riff", "audio/wav"), Alternatives: []MagicPattern{multipart(MagicPart{Offset: 0, Pattern: []byte("RIFF")}, MagicPart{Offset: 8, Pattern: []byte("WAVE")})}},
	{ID: "riff-webp", Candidate: candidate("webp", "riff", "image/webp"), Alternatives: []MagicPattern{multipart(MagicPart{Offset: 0, Pattern: []byte("RIFF")}, MagicPart{Offset: 8, Pattern: []byte("WEBP")})}},
	{ID: "riff-avi", Candidate: candidate("avi", "riff", "video/x-msvideo"), Alternatives: []MagicPattern{multipart(MagicPart{Offset: 0, Pattern: []byte("RIFF")}, MagicPart{Offset: 8, Pattern: []byte("AVI ")})}},
}

func (r MagicRule) digest() string {
	var canonical strings.Builder
	canonical.WriteString(r.ID)
	canonical.WriteByte('\n')
	canonical.WriteString(candidateKey(r.Candidate))
	for _, alternative := range r.Alternatives {
		canonical.WriteString("\nalt")
		for _, part := range alternative {
			fmt.Fprintf(&canonical, "\n%d:%s", part.Offset, hex.EncodeToString(part.Pattern))
		}
	}
	return digestText(canonical.String())
}
