package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pingResponse struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestClientDoDecodes2xxResponse(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			t.Errorf("request path = %q, want %q", r.URL.Path, "/ping")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","path":"/ping"}`))
	})
	client := New(server.URL, 5*time.Second)

	var got pingResponse
	err := client.Do(context.Background(), http.MethodGet, "/ping", "", nil, &got)

	if err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("Status = %q, want %q", got.Status, "ok")
	}
}

func TestClientDoSendsContentType(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/pdf" {
			t.Errorf("Content-Type = %q, want %q", got, "application/pdf")
		}
		w.WriteHeader(http.StatusOK)
	})
	client := New(server.URL, 5*time.Second)

	err := client.Do(context.Background(), http.MethodPost, "/extract", "application/pdf", strings.NewReader("%PDF-"), nil)

	if err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}
}

func TestClientDoReturnsProblemOnNon2xx(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"title":"Bad Gateway","status":502,"detail":"Extract unreachable"}`))
	})
	client := New(server.URL, 5*time.Second)

	err := client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	if err == nil {
		t.Fatal("Do() expected an error for a non-2xx response")
	}
	var problem Problem
	if !errors.As(err, &problem) {
		t.Fatalf("Do() error = %v, want it to hold a Problem", err)
	}
	if problem.Status != http.StatusBadGateway {
		t.Errorf("Problem.Status = %d, want %d", problem.Status, http.StatusBadGateway)
	}
	if problem.Detail != "Extract unreachable" {
		t.Errorf("Problem.Detail = %q, want %q", problem.Detail, "Extract unreachable")
	}
}

func TestClientDoReturnsErrorOnNon2xxWithoutProblemBody(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	client := New(server.URL, 5*time.Second)

	err := client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	if err == nil {
		t.Fatal("Do() expected an error for a non-2xx response with an invalid Problem body")
	}
}

func TestClientDoReturnsErrorOnTransportFailure(t *testing.T) {
	t.Parallel()

	client := New("http://127.0.0.1:1", 100*time.Millisecond)

	err := client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	if err == nil {
		t.Fatal("Do() expected an error when the target is unreachable")
	}
}

func TestClientDoRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	client := New(server.URL, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := client.Do(ctx, http.MethodGet, "/slow", "", nil, nil)

	if err == nil {
		t.Fatal("Do() expected an error when the context is cancelled")
	}
}
