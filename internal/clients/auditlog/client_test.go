package auditlog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

type emittedEvent struct {
	Action      string          `json:"action"`
	EntityType  string          `json:"entity_type"`
	Checksum    string          `json:"checksum"`
	Details     json.RawMessage `json:"details"`
	PerformedAt string          `json:"performed_at"`
}

func TestClientEmitPostsJSONEventToAuditLogs(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/audit/logs" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/audit/logs")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}
		var event emittedEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("body is not valid JSON: %v", err)
		}
		if event.Action != "pdf.extract" {
			t.Errorf("action = %q, want %q", event.Action, "pdf.extract")
		}
		if event.EntityType != "document" {
			t.Errorf("entity_type = %q, want %q", event.EntityType, "document")
		}
		if event.Checksum != "abc123" {
			t.Errorf("checksum = %q, want %q", event.Checksum, "abc123")
		}
		var details map[string]any
		if err := json.Unmarshal(event.Details, &details); err != nil {
			t.Errorf("details is not a valid object: %v", err)
		}
		if details["page_count"] != float64(3) {
			t.Errorf("details[page_count] = %v, want %v", details["page_count"], 3)
		}
		if event.PerformedAt == "" {
			t.Error("performed_at is empty")
		}
		w.WriteHeader(http.StatusCreated)
	})
	client := NewClient(server.URL, 5*time.Second)

	err := client.Emit(context.Background(), models.AuditEvent{
		Action:      models.OpPDFExtract,
		EntityType:  "document",
		Checksum:    models.Checksum("abc123"),
		Details:     map[string]any{"page_count": 3},
		PerformedAt: time.Now(),
	})

	if err != nil {
		t.Fatalf("Emit() unexpected error: %v", err)
	}
}

func TestClientEmitReturnsProblemOnNon2xx(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"title":"Internal Server Error","status":500,"detail":"mongo down"}`))
	})
	client := NewClient(server.URL, 5*time.Second)

	err := client.Emit(context.Background(), models.AuditEvent{})

	if err == nil {
		t.Fatal("Emit() expected an error for a non-2xx response")
	}
	var problem httpclient.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("Emit() error = %v, want it to hold an httpclient.Problem", err)
	}
	if problem.Status != http.StatusInternalServerError {
		t.Errorf("Problem.Status = %d, want %d", problem.Status, http.StatusInternalServerError)
	}
}

func TestClientListAllRequestsSkipAndLimitAndDecodesLogs(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/audit/logs" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/audit/logs")
		}
		if got := r.URL.Query().Get("skip"); got != "5" {
			t.Errorf("skip = %q, want %q", got, "5")
		}
		if got := r.URL.Query().Get("limit"); got != "20" {
			t.Errorf("limit = %q, want %q", got, "20")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"_id":"log-1","action":"pdf.extract","entity_type":"document","checksum":"abc123","details":{"page_count":3},"performed_at":"2026-09-23T10:00:00Z"}]`))
	})
	client := NewClient(server.URL, 5*time.Second)

	logs, err := client.ListAll(context.Background(), 5, 20)

	if err != nil {
		t.Fatalf("ListAll() unexpected error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want %d", len(logs), 1)
	}
	log := logs[0]
	if log.ID != "log-1" {
		t.Errorf("ID = %q, want %q", log.ID, "log-1")
	}
	if log.Action != "pdf.extract" {
		t.Errorf("Action = %q, want %q", log.Action, "pdf.extract")
	}
	if log.Checksum != models.Checksum("abc123") {
		t.Errorf("Checksum = %q, want %q", log.Checksum, "abc123")
	}
	if log.PerformedAt.IsZero() {
		t.Error("PerformedAt is zero")
	}
}

func TestClientListByChecksumBuildsPathAndDecodesLogs(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audit/logs/checksum/abc123" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/audit/logs/checksum/abc123")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	client := NewClient(server.URL, 5*time.Second)

	logs, err := client.ListByChecksum(context.Background(), models.Checksum("abc123"))

	if err != nil {
		t.Fatalf("ListByChecksum() unexpected error: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("len(logs) = %d, want %d", len(logs), 0)
	}
}

func TestClientListByChecksumReturnsProblemOnNon2xx(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"no audit logs for checksum"}`))
	})
	client := NewClient(server.URL, 5*time.Second)

	_, err := client.ListByChecksum(context.Background(), models.Checksum("missing"))

	if err == nil {
		t.Fatal("ListByChecksum() expected an error for a not-found response")
	}
}