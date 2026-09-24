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