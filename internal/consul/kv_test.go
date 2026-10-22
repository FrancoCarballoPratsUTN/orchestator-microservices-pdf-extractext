package consul

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKVLoadPrefixReadsValuesAndSendsToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/kv/pdf-extractext/dev/orchestrator/" {
			t.Fatalf("path = %q, want prefix query", r.URL.Path)
		}
		if r.URL.Query().Get("recurse") != "true" {
			t.Fatal("recurse query parameter is required")
		}
		if got := r.Header.Get("X-Consul-Token"); got != "service-token" {
			t.Fatalf("X-Consul-Token = %q, want service-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"Key":"pdf-extractext/dev/orchestrator/HTTP_TIMEOUT","Value":"MzVz"},
			{"Key":"pdf-extractext/dev/orchestrator/PORT","Value":"ODA4MA=="}
		]`))
	}))
	defer server.Close()

	values, err := NewKVClient(server.URL, "service-token", server.Client()).
		LoadPrefix("pdf-extractext/dev/orchestrator/")
	if err != nil {
		t.Fatalf("LoadPrefix() unexpected error: %v", err)
	}

	if values["HTTP_TIMEOUT"] != "35s" {
		t.Errorf("HTTP_TIMEOUT = %q, want 35s", values["HTTP_TIMEOUT"])
	}
	if values["PORT"] != "8080" {
		t.Errorf("PORT = %q, want 8080", values["PORT"])
	}
}

func TestKVLoadPrefixRejectsNonSuccessResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	_, err := NewKVClient(server.URL, "", server.Client()).
		LoadPrefix("pdf-extractext/dev/orchestrator/")
	if err == nil {
		t.Fatal("LoadPrefix() expected an error for non-success response")
	}
}
