package extract

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
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
		_, _ = w.Write([]byte(`{"page_count":1,"pages":[],"text":"","duration_ms":0}`))
	})
	client := NewClient(server.URL, 5*time.Second)

	_, err := client.Extract(context.Background(), pdfBytes)

	if err != nil {
		t.Fatalf("Extract() unexpected error: %v", err)
	}
}

func TestClientExtractDecodesExtractedDocument(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"page_count": 2,
			"pages": [
				{"page_number": 1, "text": "primera pagina"},
				{"page_number": 2, "text": "segunda pagina"}
			],
			"text": "primera pagina segunda pagina",
			"duration_ms": 42
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
	if len(got.Pages) != 2 {
		t.Fatalf("Pages length = %d, want %d", len(got.Pages), 2)
	}
	if got.Pages[0] != (dto.ExtractedPage{PageNumber: 1, Text: "primera pagina"}) {
		t.Errorf("Pages[0] = %+v, want page 1", got.Pages[0])
	}
	if got.Pages[1].PageNumber != 2 || got.Pages[1].Text != "segunda pagina" {
		t.Errorf("Pages[1] = %+v, want page 2", got.Pages[1])
	}
	if got.Text != "primera pagina segunda pagina" {
		t.Errorf("Text = %q, want %q", got.Text, "primera pagina segunda pagina")
	}
	if got.DurationMs != 42 {
		t.Errorf("DurationMs = %d, want %d", got.DurationMs, 42)
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