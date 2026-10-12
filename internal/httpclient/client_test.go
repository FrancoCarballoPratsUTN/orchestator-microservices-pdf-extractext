package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/reqctx"
)

func decodeRecord(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	line := strings.TrimSpace(string(raw))
	if line == "" {
		t.Fatal("expected a log record, got none")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("log record is not valid JSON: %v (%q)", err, line)
	}
	return record
}

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
	client := New(server.URL, 5*time.Second, "")

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
	client := New(server.URL, 5*time.Second, "")

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
	client := New(server.URL, 5*time.Second, "")

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
	client := New(server.URL, 5*time.Second, "")

	err := client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	if err == nil {
		t.Fatal("Do() expected an error for a non-2xx response with an invalid Problem body")
	}
}

func TestClientDoReturnsStatusErrorOnPlainText503(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("service unavailable"))
	})
	client := New(server.URL, 5*time.Second, "")

	err := client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	if err == nil {
		t.Fatal("Do() expected an error for a non-2xx response with an invalid Problem body")
	}
	var statusErr StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("Do() error = %v, want it to hold a StatusError", err)
	}
	if statusErr.Code != http.StatusServiceUnavailable {
		t.Errorf("StatusError.Code = %d, want %d", statusErr.Code, http.StatusServiceUnavailable)
	}
	if statusErr.Path != "/extract" {
		t.Errorf("StatusError.Path = %q, want %q", statusErr.Path, "/extract")
	}
	if code, ok := StatusCode(err); !ok || code != http.StatusServiceUnavailable {
		t.Errorf("StatusCode(%v) = (%d, %v), want (%d, true)", err, code, ok, http.StatusServiceUnavailable)
	}
}

func TestClientDoReturnsErrorOnTransportFailure(t *testing.T) {
	t.Parallel()

	client := New("http://127.0.0.1:1", 100*time.Millisecond, "")

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
	client := New(server.URL, 5*time.Second, "")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := client.Do(ctx, http.MethodGet, "/slow", "", nil, nil)

	if err == nil {
		t.Fatal("Do() expected an error when the context is cancelled")
	}
}

func TestClientDoSendsBearerTokenWhenTokenConfigured(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer service-secret" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer service-secret")
		}
		w.WriteHeader(http.StatusOK)
	})
	client := New(server.URL, 5*time.Second, "service-secret")

	err := client.Do(context.Background(), http.MethodGet, "/audit/logs", "", nil, nil)

	if err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}
}

func TestClientDoOmitsAuthorizationHeaderWhenTokenEmpty(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want it to be omitted", got)
		}
		w.WriteHeader(http.StatusOK)
	})
	client := New(server.URL, 5*time.Second, "")

	err := client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	if err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}
}

func TestIsUnauthorizedIsTrueFor401Problem(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"missing token"}`))
	})
	client := New(server.URL, 5*time.Second, "")

	err := client.Do(context.Background(), http.MethodGet, "/audit/logs", "", nil, nil)

	if !IsUnauthorized(err) {
		t.Errorf("IsUnauthorized(%v) = false, want true", err)
	}
}

func TestClientDoLogsSuccessAtDebugWithRequestID(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	client := NewWithLogger(server.URL, 5*time.Second, "", logger)

	ctx := reqctx.WithRequestID(context.Background(), "req-http")
	if err := client.Do(ctx, http.MethodGet, "/ping", "", nil, nil); err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}

	record := decodeRecord(t, output.Bytes())
	if got := record["level"]; got != "DEBUG" {
		t.Errorf("level = %v, want DEBUG", got)
	}
	if got := record["status"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want %d", got, http.StatusOK)
	}
	if got := record["path"]; got != "/ping" {
		t.Errorf("path = %v, want %q", got, "/ping")
	}
	if got := record["method"]; got != http.MethodGet {
		t.Errorf("method = %v, want %q", got, http.MethodGet)
	}
	if got := record["request_id"]; got != "req-http" {
		t.Errorf("request_id = %v, want %q", got, "req-http")
	}
	if _, ok := record["upstream"]; !ok {
		t.Errorf("record missing upstream field: %v", record)
	}
	if _, ok := record["duration_ms"]; !ok {
		t.Errorf("record missing duration_ms field: %v", record)
	}
}

func TestClientDoLogsNon2xxAtWarnWithStatus(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	client := NewWithLogger(server.URL, 5*time.Second, "", logger)

	_ = client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	record := decodeRecord(t, output.Bytes())
	if got := record["level"]; got != "WARN" {
		t.Errorf("level = %v, want WARN", got)
	}
	if got := record["status"]; got != float64(http.StatusBadGateway) {
		t.Errorf("status = %v, want %d", got, http.StatusBadGateway)
	}
}

func TestClientDoLogsTransportFailureAtError(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})
	client := NewWithLogger("http://127.0.0.1:1", 100*time.Millisecond, "", logger)

	_ = client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	record := decodeRecord(t, output.Bytes())
	if got := record["level"]; got != "ERROR" {
		t.Errorf("level = %v, want ERROR", got)
	}
	if _, ok := record["error"]; !ok {
		t.Errorf("record missing error field: %v", record)
	}
}

func TestClientDoNeverLogsToken(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	client := NewWithLogger(server.URL, 5*time.Second, "service-secret", logger)

	_ = client.Do(context.Background(), http.MethodGet, "/audit/logs", "", nil, nil)

	if strings.Contains(output.String(), "service-secret") {
		t.Errorf("log output leaked the token: %q", output.String())
	}
}

func TestClientWithoutLoggerDoesNotPanic(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	client := New(server.URL, 5*time.Second, "")

	if err := client.Do(context.Background(), http.MethodGet, "/ping", "", nil, nil); err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}
}

func TestIsUnauthorizedIsFalseForOtherErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
	}{
		{"nil error", nil},
		{"plain error", errors.New("boom")},
		{"non-401 problem", Problem{Title: "Bad Gateway", Status: http.StatusBadGateway}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if IsUnauthorized(tc.err) {
				t.Errorf("IsUnauthorized(%v) = true, want false", tc.err)
			}
		})
	}
}
