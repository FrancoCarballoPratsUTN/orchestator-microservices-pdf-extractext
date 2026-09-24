package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

type receivedCreatePayload struct {
	Text     string `json:"text"`
	Checksum string `json:"checksum"`
	Name     string `json:"name"`
	Metadata any    `json:"metadata"`
}

func TestClientCreatePostsPayloadToTextsEndpoint(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/texts" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/texts")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}
		var payload receivedCreatePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("body is not valid JSON: %v", err)
		}
		if payload.Text != "un texto" {
			t.Errorf("text = %q, want %q", payload.Text, "un texto")
		}
		if payload.Checksum != "abc123" {
			t.Errorf("checksum = %q, want %q", payload.Checksum, "abc123")
		}
		if payload.Name != "mi documento" {
			t.Errorf("name = %q, want %q", payload.Name, "mi documento")
		}
		w.WriteHeader(http.StatusCreated)
	})
	client := NewClient(server.URL, 5*time.Second)

	err := client.Create(context.Background(), dto.CreateTextPayload{
		Text:     "un texto",
		Checksum: models.Checksum("abc123"),
		Name:     "mi documento",
	})

	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
}

func TestClientCreateReturnsProblemOn409Duplicate(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(httpclient.Problem{
			Type:   "about:blank",
			Title:  "Conflict",
			Status: http.StatusConflict,
			Detail: "checksum already exists",
		})
	})
	client := NewClient(server.URL, 5*time.Second)

	err := client.Create(context.Background(), dto.CreateTextPayload{Checksum: models.Checksum("abc123")})

	var problem httpclient.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("error = %v, want an httpclient.Problem", err)
	}
	if problem.Status != http.StatusConflict {
		t.Errorf("Problem.Status = %d, want %d", problem.Status, http.StatusConflict)
	}
	if problem.Detail != "checksum already exists" {
		t.Errorf("Problem.Detail = %q, want %q", problem.Detail, "checksum already exists")
	}
}

func TestClientCreateReturnsErrorOnTransportFailure(t *testing.T) {
	t.Parallel()

	client := NewClient("http://127.0.0.1:1", time.Millisecond)

	err := client.Create(context.Background(), dto.CreateTextPayload{Checksum: models.Checksum("abc123")})

	var problem httpclient.Problem
	if errors.As(err, &problem) {
		t.Fatalf("error = %v, want a transport error, not a Problem", err)
	}
	if err == nil {
		t.Fatal("Create() expected an error, got nil")
	}
}

type receivedUpdatePayload struct {
	Name     string `json:"name"`
	Metadata any    `json:"metadata"`
}

func TestClientUpdatePutsNameAndMetadataToTextsEndpoint(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPut)
		}
		if r.URL.Path != "/texts/abc123" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/texts/abc123")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}
		var payload receivedUpdatePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("body is not valid JSON: %v", err)
		}
		if payload.Name != "nuevo nombre" {
			t.Errorf("name = %q, want %q", payload.Name, "nuevo nombre")
		}
		_ = json.NewEncoder(w).Encode(models.Text{
			Checksum: models.Checksum("abc123"),
			Text:     "un texto",
			Name:     "nuevo nombre",
		})
	})
	client := NewClient(server.URL, 5*time.Second)

	text, err := client.Update(context.Background(), models.Checksum("abc123"), dto.UpdateTextPayload{
		Name: "nuevo nombre",
	})

	if err != nil {
		t.Fatalf("Update() unexpected error: %v", err)
	}
	if text.Checksum != models.Checksum("abc123") {
		t.Errorf("text.Checksum = %q, want %q", text.Checksum, "abc123")
	}
	if text.Name != "nuevo nombre" {
		t.Errorf("text.Name = %q, want %q", text.Name, "nuevo nombre")
	}
}

func TestClientUpdateReturnsProblemOn404NotFound(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(httpclient.Problem{
			Type:   "about:blank",
			Title:  "Not Found",
			Status: http.StatusNotFound,
			Detail: "checksum not found",
		})
	})
	client := NewClient(server.URL, 5*time.Second)

	_, err := client.Update(context.Background(), models.Checksum("missing"), dto.UpdateTextPayload{})

	var problem httpclient.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("error = %v, want an httpclient.Problem", err)
	}
	if problem.Status != http.StatusNotFound {
		t.Errorf("Problem.Status = %d, want %d", problem.Status, http.StatusNotFound)
	}
}

func TestClientDeleteDeletesTextsEndpoint(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want %q", r.Method, http.MethodDelete)
		}
		if r.URL.Path != "/texts/abc123" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/texts/abc123")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "OK"})
	})
	client := NewClient(server.URL, 5*time.Second)

	err := client.Delete(context.Background(), models.Checksum("abc123"))

	if err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
}

func TestClientDeleteReturnsProblemOn404NotFound(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(httpclient.Problem{
			Type:   "about:blank",
			Title:  "Not Found",
			Status: http.StatusNotFound,
			Detail: "checksum not found",
		})
	})
	client := NewClient(server.URL, 5*time.Second)

	err := client.Delete(context.Background(), models.Checksum("missing"))

	var problem httpclient.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("error = %v, want an httpclient.Problem", err)
	}
	if problem.Status != http.StatusNotFound {
		t.Errorf("Problem.Status = %d, want %d", problem.Status, http.StatusNotFound)
	}
}

func TestClientDeleteReturnsErrorOnTransportFailure(t *testing.T) {
	t.Parallel()

	client := NewClient("http://127.0.0.1:1", time.Millisecond)

	err := client.Delete(context.Background(), models.Checksum("abc123"))

	if err == nil {
		t.Fatal("Delete() expected an error, got nil")
	}
}

func TestClientFindByChecksumGetsTextsEndpoint(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/texts/abc123" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/texts/abc123")
		}
		_ = json.NewEncoder(w).Encode(models.Text{
			Checksum: models.Checksum("abc123"),
			Text:     "un texto",
			Name:     "mi documento",
			Metadata: map[string]any{"pages": 250},
		})
	})
	client := NewClient(server.URL, 5*time.Second)

	text, err := client.FindByChecksum(context.Background(), models.Checksum("abc123"))

	if err != nil {
		t.Fatalf("FindByChecksum() unexpected error: %v", err)
	}
	if text.Checksum != models.Checksum("abc123") {
		t.Errorf("text.Checksum = %q, want %q", text.Checksum, "abc123")
	}
	if text.Text != "un texto" {
		t.Errorf("text.Text = %q, want %q", text.Text, "un texto")
	}
	if text.Name != "mi documento" {
		t.Errorf("text.Name = %q, want %q", text.Name, "mi documento")
	}
}

func TestClientFindByChecksumReturnsProblemOn404NotFound(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(httpclient.Problem{
			Type:   "about:blank",
			Title:  "Not Found",
			Status: http.StatusNotFound,
			Detail: "checksum not found",
		})
	})
	client := NewClient(server.URL, 5*time.Second)

	_, err := client.FindByChecksum(context.Background(), models.Checksum("missing"))

	var problem httpclient.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("error = %v, want an httpclient.Problem", err)
	}
	if problem.Status != http.StatusNotFound {
		t.Errorf("Problem.Status = %d, want %d", problem.Status, http.StatusNotFound)
	}
}

func TestClientFindByChecksumReturnsErrorOnTransportFailure(t *testing.T) {
	t.Parallel()

	client := NewClient("http://127.0.0.1:1", time.Millisecond)

	_, err := client.FindByChecksum(context.Background(), models.Checksum("abc123"))

	if err == nil {
		t.Fatal("FindByChecksum() expected an error, got nil")
	}
}