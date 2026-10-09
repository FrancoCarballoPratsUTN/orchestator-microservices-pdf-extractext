package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/testsupport"
)

// gate bloquea un stub hasta que el test lo libera, para reproducir trabajo en
// vuelo (una emisión de auditoría o una request) en el momento del apagado.
type gate struct {
	once sync.Once
	c    chan struct{}
}

func newGate() *gate { return &gate{c: make(chan struct{})} }

func (g *gate) open() { g.once.Do(func() { close(g.c) }) }

// wait devuelve false si el request se canceló antes de liberar la compuerta.
func (g *gate) wait(r *http.Request) bool {
	select {
	case <-g.c:
		return true
	case <-r.Context().Done():
		return false
	}
}

type extractStub struct {
	gate  *gate
	calls atomic.Int32
}

func (s *extractStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/extract" {
		http.NotFound(w, r)
		return
	}
	s.calls.Add(1)
	if s.gate != nil && !s.gate.wait(r) {
		return
	}
	_ = json.NewEncoder(w).Encode(dto.ExtractedDocument{
		PageCount: 3,
		Content:   "contenido extraido por el stub de Extract",
	})
}

type auditStub struct {
	gate     *gate
	received atomic.Int32
}

func (s *auditStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/audit/logs" {
		http.NotFound(w, r)
		return
	}
	if s.gate != nil && !s.gate.wait(r) {
		return
	}
	s.received.Add(1)
	w.WriteHeader(http.StatusCreated)
}

// persistStub responde 404 a la búsqueda por checksum: la dedup la interpreta
// como miss y el flujo sigue hacia el Extract.
type persistStub struct{}

func (persistStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(httpclient.Problem{
		Type:   "about:blank",
		Title:  http.StatusText(http.StatusNotFound),
		Status: http.StatusNotFound,
		Detail: "text not stored",
	})
}

type stubStack struct {
	extract *extractStub
	audit   *auditStub
	cfg     config.Config
}

// newStubStack arma el orquestador real contra stubs en proceso. Todas las
// variables de entorno son valores de test; el puerto se reserva dinámicamente.
func newStubStack(t *testing.T, shutdownTimeout string, extractGate, auditGate *gate) *stubStack {
	t.Helper()

	extractStub := &extractStub{gate: extractGate}
	auditStub := &auditStub{gate: auditGate}

	extractServer := httptest.NewServer(extractStub)
	persistServer := httptest.NewServer(persistStub{})
	auditServer := httptest.NewServer(auditStub)
	t.Cleanup(func() {
		// Liberar las compuertas antes de cerrar: httptest.Server.Close espera a
		// que terminen los handlers en vuelo y, si el test falla a mitad, nadie
		// las abriría y Close se colgaría.
		if extractGate != nil {
			extractGate.open()
		}
		if auditGate != nil {
			auditGate.open()
		}
		extractServer.Close()
		persistServer.Close()
		auditServer.Close()
	})

	t.Setenv("PORT", freePort(t))
	t.Setenv("EXTRACT_BASE_URL", extractServer.URL)
	t.Setenv("PERSISTENCE_BASE_URL", persistServer.URL)
	t.Setenv("AUDIT_LOG_BASE_URL", auditServer.URL)
	t.Setenv("AUDIT_LOG_API_TOKEN", "test-audit-token")
	t.Setenv("PERSISTENCE_API_TOKEN", "test-persistence-token")
	t.Setenv("HTTP_TIMEOUT", "5s")
	t.Setenv("SHUTDOWN_TIMEOUT", shutdownTimeout)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() unexpected error: %v", err)
	}
	return &stubStack{extract: extractStub, audit: auditStub, cfg: cfg}
}

func startApp(t *testing.T, cfg config.Config) (<-chan error, chan<- os.Signal) {
	t.Helper()

	stopCh := make(chan os.Signal, 1)
	done := make(chan error, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() {
		done <- runWithSignals(context.Background(), logger, cfg, stopCh)
	}()
	return done, stopCh
}

func freePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return strconv.Itoa(port)
}

func waitReady(t *testing.T, port string) {
	t.Helper()

	url := "http://127.0.0.1:" + port + "/healthz"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server on :%s never became ready", port)
}

func waitUntil(t *testing.T, condition func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

// assertStillRunning falla si runWithSignals ya retornó dentro de la ventana.
func assertStillRunning(t *testing.T, done <-chan error, window time.Duration, msg string) {
	t.Helper()

	select {
	case err := <-done:
		t.Fatalf("%s: runWithSignals returned early (err=%v)", msg, err)
	case <-time.After(window):
	}
}

func waitResult(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("runWithSignals did not return within the test deadline")
		return nil
	}
}

func postExtract(port string, body []byte) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+"/api/v1/pdfs/extract", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/pdf")
	return http.DefaultClient.Do(request)
}

func TestRunWithSignalsDrainsPendingAuditEmissions(t *testing.T) {
	auditGate := newGate()
	stack := newStubStack(t, "5s", nil, auditGate)
	done, stopCh := startApp(t, stack.cfg)

	waitReady(t, stack.cfg.Port)
	response, err := postExtract(stack.cfg.Port, testsupport.ScrumGuidePDF(t))
	if err != nil {
		t.Fatalf("POST /api/v1/pdfs/extract: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("extract status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	stopCh <- syscall.SIGTERM
	assertStillRunning(t, done, 200*time.Millisecond, "shutdown returned while an audit emission was in flight")

	auditGate.open()
	if err := waitResult(t, done); err != nil {
		t.Fatalf("runWithSignals() error = %v, want nil", err)
	}
	if got := stack.audit.received.Load(); got != 1 {
		t.Errorf("audit emissions received = %d, want 1", got)
	}
}

func TestRunWithSignalsWaitsForInFlightRequest(t *testing.T) {
	extractGate := newGate()
	stack := newStubStack(t, "5s", extractGate, nil)
	done, stopCh := startApp(t, stack.cfg)

	waitReady(t, stack.cfg.Port)

	pdf := testsupport.ScrumGuidePDF(t)
	type result struct {
		status int
		err    error
	}
	requestDone := make(chan result, 1)
	go func() {
		response, err := postExtract(stack.cfg.Port, pdf)
		if err != nil {
			requestDone <- result{err: err}
			return
		}
		defer response.Body.Close()
		requestDone <- result{status: response.StatusCode}
	}()

	waitUntil(t, func() bool { return stack.extract.calls.Load() == 1 }, "extract request did not reach the stub")

	stopCh <- syscall.SIGTERM
	assertStillRunning(t, done, 200*time.Millisecond, "shutdown returned while a request was in flight")

	extractGate.open()
	if err := waitResult(t, done); err != nil {
		t.Fatalf("runWithSignals() error = %v, want nil", err)
	}
	got := <-requestDone
	if got.err != nil {
		t.Fatalf("in-flight request error = %v", got.err)
	}
	if got.status != http.StatusOK {
		t.Errorf("in-flight request status = %d, want %d", got.status, http.StatusOK)
	}
}

func TestRunWithSignalsReturnsErrorWhenShutdownTimeoutExpires(t *testing.T) {
	auditGate := newGate() // se queda cerrada a propósito
	stack := newStubStack(t, "50ms", nil, auditGate)
	done, stopCh := startApp(t, stack.cfg)

	waitReady(t, stack.cfg.Port)
	response, err := postExtract(stack.cfg.Port, testsupport.ScrumGuidePDF(t))
	if err != nil {
		t.Fatalf("POST /api/v1/pdfs/extract: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("extract status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	stopCh <- syscall.SIGTERM
	start := time.Now()
	err = waitResult(t, done)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("runWithSignals() error = nil, want a shutdown timeout error")
	}
	if elapsed > 5*time.Second {
		t.Errorf("runWithSignals() took %v, want it to return promptly on the shutdown deadline", elapsed)
	}
	if got := stack.audit.received.Load(); got != 0 {
		t.Errorf("audit emissions received = %d, want 0 (deadline expired before draining)", got)
	}
}
