package extract

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/logging"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestClientExtractSendsRawPDFBodyWithPDFContentType(t *testing.T) {
	t.Parallel()

	pdfBytes := []byte("%PDF-1.7\nraw binary payload")
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/extract" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/extract")
		}
		if got := r.Header.Get("Content-Type"); got != "application/pdf" {
			t.Errorf("Content-Type = %q, want %q", got, "application/pdf")
		}
		body := make([]byte, 0, len(pdfBytes))
		buf := make([]byte, 64)
		for {
			n, err := r.Body.Read(buf)
			body = append(body, buf[:n]...)
			if err != nil {
				break
			}
		}
		if string(body) != string(pdfBytes) {
			t.Errorf("body = %q, want %q (no debe ir en base64)", string(body), string(pdfBytes))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":"contenido del pdf","page_count":1}`))
	})
	client := NewClient(server.URL, 5*time.Second)

	_, err := client.Extract(context.Background(), pdfBytes)

	if err != nil {
		t.Fatalf("Extract() unexpected error: %v", err)
	}
}

// TestClientExtractDecodesRealExtractContract es el test de regresión del decode
// silencioso. Reproduce la respuesta tal cual la manda el MS Extract hoy
// ({content, page_count}) y exige Content NO vacío. Con el DTO viejo, que pedía
// json:"text", este test fallaba: encoding/json ignoraba "content" sin error y
// devolvía Content == "", que en el servicio se traducía en un SHA-256("") para
// todos los PDFs. Si el Extract vuelve a cambiar la forma, este test lo delata.
func TestClientExtractDecodesRealExtractContract(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content": "primera pagina\n\nsegunda pagina",
			"page_count": 2
		}`))
	})
	client := NewClient(server.URL, 5*time.Second)

	got, err := client.Extract(context.Background(), []byte("%PDF-"))

	if err != nil {
		t.Fatalf("Extract() unexpected error: %v", err)
	}
	if got.PageCount != 2 {
		t.Errorf("PageCount = %d, want %d", got.PageCount, 2)
	}
	if got.Content != "primera pagina\n\nsegunda pagina" {
		t.Errorf("Content = %q, want %q", got.Content, "primera pagina\n\nsegunda pagina")
	}
}

func TestClientExtractReturnsProblemOnNon2xx(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"title":"Bad Gateway","status":502,"detail":"Extract unreachable"}`))
	})
	client := NewClient(server.URL, 5*time.Second)

	_, err := client.Extract(context.Background(), []byte("%PDF-"))

	if err == nil {
		t.Fatal("Extract() expected an error for a non-2xx response")
	}
	var problem httpclient.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("Extract() error = %v, want it to hold an httpclient.Problem", err)
	}
	if problem.Status != http.StatusBadGateway {
		t.Errorf("Problem.Status = %d, want %d", problem.Status, http.StatusBadGateway)
	}
	if problem.Detail != "Extract unreachable" {
		t.Errorf("Problem.Detail = %q, want %q", problem.Detail, "Extract unreachable")
	}
}

func TestClientExtractReturnsErrorOnNon2xxWithoutProblemBody(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	client := NewClient(server.URL, 5*time.Second)

	_, err := client.Extract(context.Background(), []byte("%PDF-"))

	if err == nil {
		t.Fatal("Extract() expected an error for a non-2xx response with an invalid Problem body")
	}
}

func TestClientExtractReturnsErrorOnTransportFailure(t *testing.T) {
	t.Parallel()

	client := NewClient("http://127.0.0.1:1", 100*time.Millisecond)

	_, err := client.Extract(context.Background(), []byte("%PDF-"))

	if err == nil {
		t.Fatal("Extract() expected an error when the target is unreachable")
	}
}

func TestClientExtractHonorsConfiguredTimeout(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	})
	client := NewClient(server.URL, 20*time.Millisecond)

	_, err := client.Extract(context.Background(), []byte("%PDF-"))

	if err == nil {
		t.Fatal("Extract() expected an error when the MS exceeds the configured timeout")
	}
}

// TestClientExtractRetriesPostUpToConfiguredAttempts confirma que la policy del
// Extract reintenta el POST /extract (método no idempotente) hasta agotar los
// intentos configurados. El caller arma la policy sin AllowNonIdempotent a
// propósito: es el propio cliente Extract quien debe habilitar el retry de su
// POST /extract.
func TestClientExtractRetriesPostUpToConfiguredAttempts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":"ok","page_count":1}`))
	})
	policy := httpclient.RetryPolicy{Attempts: 3, BackoffBase: 0}
	client := NewClientWithPolicy(server.URL, 5*time.Second, policy, nil)

	got, err := client.Extract(context.Background(), []byte("%PDF-"))

	if err != nil {
		t.Fatalf("Extract() unexpected error after retries: %v", err)
	}
	if got.Content != "ok" {
		t.Errorf("Content = %q, want %q", got.Content, "ok")
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("upstream calls = %d, want 3", n)
	}
}

// TestClientExtractDoesNotRetryByDefault fija el comportamiento de los
// constructores históricos: sin policy explícita, un solo intento.
func TestClientExtractDoesNotRetryByDefault(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	client := NewClient(server.URL, 5*time.Second)

	if _, err := client.Extract(context.Background(), []byte("%PDF-")); err == nil {
		t.Fatal("Extract() expected an error when the upstream is unavailable")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("upstream calls = %d, want 1 (no retry without an explicit policy)", n)
	}
}

func TestClientExtractWithLoggerEmitsUpstreamLog(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":"x","page_count":1}`))
	})
	client := NewClientWithLogger(server.URL, 5*time.Second, logger)

	if _, err := client.Extract(context.Background(), []byte("%PDF-")); err != nil {
		t.Fatalf("Extract() unexpected error: %v", err)
	}
	if !strings.Contains(output.String(), "upstream request completed") {
		t.Errorf("logger was not wired into the client, output: %q", output.String())
	}
}
