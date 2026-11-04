package logging

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

const testMaxValueLength = 256

func TestSafeURLDropsCredentials(t *testing.T) {
	safe := SafeURL("http://user:secret@extract:8080/api/v1/extract", testMaxValueLength)

	if strings.Contains(safe, "secret") || strings.Contains(safe, "user") {
		t.Fatalf("SafeURL() = %q, want the credentials removed", safe)
	}
	if safe != "http://extract:8080/api/v1/extract" {
		t.Fatalf("SafeURL() = %q, want http://extract:8080/api/v1/extract", safe)
	}
}

func TestSafeURLDropsQueryAndFragment(t *testing.T) {
	safe := SafeURL("http://extract:8080/api?token=supersecret#frag", testMaxValueLength)

	if strings.Contains(safe, "supersecret") {
		t.Fatalf("SafeURL() = %q, want the query string removed", safe)
	}
	if safe != "http://extract:8080/api" {
		t.Fatalf("SafeURL() = %q, want http://extract:8080/api", safe)
	}
}

func TestSafeURLKeepsSchemeHostAndPath(t *testing.T) {
	safe := SafeURL("http://extract:8080/api/v1/pdfs/extract", testMaxValueLength)

	if safe != "http://extract:8080/api/v1/pdfs/extract" {
		t.Fatalf("SafeURL() = %q, want the URL unchanged", safe)
	}
}

func TestSafeURLBoundsALongValue(t *testing.T) {
	raw := "http://" + strings.Repeat("a", testMaxValueLength*2)

	safe := SafeURL(raw, 256)

	if len([]rune(safe)) > testMaxValueLength+1 {
		t.Fatalf("SafeURL() returned %d runes, want at most %d", len([]rune(safe)), testMaxValueLength+1)
	}
}

func TestSafeURLRejectsInvalidAndRelativeURLs(t *testing.T) {
	for _, raw := range []string{
		"http://bad host/path?token=secret",
		"/relative/path?token=secret",
	} {
		if got := SafeURL(raw, testMaxValueLength); got != "<invalid-url>" {
			t.Errorf("SafeURL(%q) = %q, want <invalid-url>", raw, got)
		}
	}
}

func TestTruncateCutsLongValues(t *testing.T) {
	truncated := Truncate(strings.Repeat("x", 500), 10)

	if len([]rune(truncated)) != 11 {
		t.Fatalf("Truncate() returned %d runes, want 10 plus the ellipsis", len([]rune(truncated)))
	}
	if !strings.HasSuffix(truncated, "…") {
		t.Fatalf("Truncate() = %q, want the ellipsis marker", truncated)
	}
}

func TestTruncateKeepsShortValues(t *testing.T) {
	if got := Truncate("short", 10); got != "short" {
		t.Fatalf("Truncate() = %q, want %q", got, "short")
	}
}

func TestTruncateWithoutLimitKeepsTheValue(t *testing.T) {
	long := strings.Repeat("x", 500)

	if got := Truncate(long, 0); got != long {
		t.Fatalf("Truncate() with no limit returned %d runes, want %d", len([]rune(got)), len([]rune(long)))
	}
}

func TestSafeErrorDropsTheURLOfAnHTTPError(t *testing.T) {
	transport := &url.Error{
		Op:  "Get",
		URL: "http://extract-user:supersecret@extract:8080/api?token=SECRET-TOKEN",
		Err: errors.New("dial tcp: connection refused"),
	}

	safe := SafeError(transport)

	if strings.Contains(safe, "SECRET-TOKEN") {
		t.Fatalf("SafeError() = %q, want the query string removed", safe)
	}
	if strings.Contains(safe, "extract-user") {
		t.Fatalf("SafeError() = %q, want the user removed", safe)
	}
	if safe != "dial tcp: connection refused" {
		t.Fatalf("SafeError() = %q, want the underlying error", safe)
	}
}

func TestSafeErrorUnwrapsThroughWrappingErrors(t *testing.T) {
	transport := &url.Error{Op: "Get", URL: "http://extract:8080/api?token=SECRET-TOKEN", Err: errors.New("boom")}
	wrapped := fmt.Errorf("request to /api: %w", transport)

	safe := SafeError(wrapped)

	if strings.Contains(safe, "SECRET-TOKEN") {
		t.Fatalf("SafeError() = %q, want the query string removed", safe)
	}
	if safe != "boom" {
		t.Fatalf("SafeError() = %q, want the underlying error", safe)
	}
}

func TestSafeErrorKeepsAPlainError(t *testing.T) {
	if got := SafeError(errors.New("plain failure")); got != "plain failure" {
		t.Fatalf("SafeError() = %q, want %q", got, "plain failure")
	}
}

func TestSafeErrorOfNilIsEmpty(t *testing.T) {
	if got := SafeError(nil); got != "" {
		t.Fatalf("SafeError(nil) = %q, want the empty string", got)
	}
}
