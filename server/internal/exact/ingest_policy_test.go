package exact

import (
	"strings"
	"testing"
)

func TestNormalizeIngestLocatorRejectsCredentialBearingURIForms(t *testing.T) {
	tests := []struct {
		name    string
		locator string
		want    string
	}{
		{name: "userinfo", locator: "https://user:password@example.test/file", want: "embedded credentials"},
		{name: "access token", locator: "https://example.test/file?access_token=secret", want: "credential_ref"},
		{name: "bearer token", locator: "https://example.test/file?token=secret", want: "credential_ref"},
		{name: "api key", locator: "https://example.test/file?api_key=secret", want: "credential_ref"},
		{name: "signed query", locator: "https://example.test/file?signature=secret", want: "credential_ref"},
		{name: "aws signed query", locator: "https://example.test/file?X-Amz-Signature=secret", want: "credential_ref"},
		{name: "google signed query", locator: "https://example.test/file?X-Goog-Signature=secret", want: "credential_ref"},
		{name: "arbitrary query", locator: "https://example.test/file?part=1", want: "credential_ref"},
		{name: "fragment", locator: "https://example.test/file#download", want: "URI fragment"},
		{name: "control character", locator: "\nhttps://example.test/file", want: "control character"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeIngestLocator(IngestLocator{Locator: test.locator})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalize locator error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestNormalizeIngestLocatorRejectsMaliciousDisplayLocator(t *testing.T) {
	for _, display := range []string{
		"https://user:password@example.test/file",
		"https://example.test/file?token=secret",
		"https://example.test/file#fragment",
		"release\x00mirror",
	} {
		_, err := normalizeIngestLocator(IngestLocator{
			Locator:        "https://example.test/file",
			DisplayLocator: display,
		})
		if err == nil || !strings.Contains(err.Error(), "display_locator") {
			t.Fatalf("display locator %q error = %v, want display_locator validation error", display, err)
		}
	}
}

func TestNormalizeIngestLocatorAcceptsQuerylessHTTPSAndIPFS(t *testing.T) {
	for _, locator := range []string{
		"https://downloads.example.test/release.bin",
		"ipfs://bafy-example/release.bin",
	} {
		got, err := normalizeIngestLocator(IngestLocator{Locator: locator})
		if err != nil {
			t.Fatalf("normalize %q: %v", locator, err)
		}
		if got.Locator != locator || got.DisplayLocator != locator {
			t.Fatalf("normalized %q = %+v", locator, got)
		}
	}
}

func TestProtectionPathsPreserveLeadingAndTrailingSpaces(t *testing.T) {
	for _, value := range []string{" leading.txt", "trailing.txt ", "nested/ both "} {
		got, err := normalizeProtectionPath(value)
		if err != nil {
			t.Fatalf("normalize %q: %v", value, err)
		}
		if got != value {
			t.Fatalf("normalize %q = %q", value, got)
		}
		locator, err := normalizeIngestLocator(IngestLocator{
			Path: value, Locator: "https://example.test/file",
		})
		if err != nil {
			t.Fatalf("normalize locator path %q: %v", value, err)
		}
		if locator.Path != value {
			t.Fatalf("normalize locator path %q = %q", value, locator.Path)
		}
	}
}
