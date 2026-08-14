package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

func artistID(name string) string {
	sum := sha256.Sum256([]byte("artist\x1f" + name))
	return "ar_" + hex.EncodeToString(sum[:8])
}

func albumID(artist, album string) string {
	sum := sha256.Sum256([]byte("album\x1f" + artist + "\x1f" + album))
	return "al_" + hex.EncodeToString(sum[:10])
}

func indexName(label string) string {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		return "#"
	}
	r, _ := utf8.DecodeRuneInString(trimmed)
	if !unicode.IsLetter(r) {
		return "#"
	}
	return strings.ToUpper(string(unicode.ToUpper(r)))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func suffixOf(name string) string {
	lower := strings.ToLower(name)
	if i := strings.LastIndex(lower, "."); i >= 0 {
		return lower[i:]
	}
	return ""
}

func audioContentType(name string) string {
	switch suffixOf(name) {
	case ".mp3":
		return "audio/mpeg"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	case ".m4a", ".mp4":
		return "audio/mp4"
	default:
		return "application/octet-stream"
	}
}

func bookContentType(kind, name string) string {
	if kind == "epub" || suffixOf(name) == ".epub" {
		return "application/epub+zip"
	}
	if suffixOf(name) == ".md" {
		return "text/markdown"
	}
	return "text/plain"
}
