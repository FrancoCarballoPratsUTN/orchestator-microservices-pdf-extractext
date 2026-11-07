package health

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/logging"
)

const (
	testTimeout = 2 * time.Second
	asyncWait   = 5 * time.Second
)

func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func okHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func TestCheckReportsOKWhenEveryDependencyIsHealthy(t *testing.T) {
	t.Parallel()

	extract := newServer(t, okHandler)
	persistence := newServer(t, okHandler)

	checker := NewChecker(CheckerOptions{
		Probes: []Probe{
			{Name: "extract", URL: extract.URL},
			{Name: "persistence", URL: persistence.URL},
		},
		Timeout: testTimeout,
		Logger:  logging.Discard(),
	})

	report := checker.Check(context.Background())

	if report.Status != StatusOK {
		t.Errorf("Status = %q, want %q", report.Status, StatusOK)
	}
	for _, name := range []string{"extract", "persistence"} {
		if got := report.Dependencies[name].Status; got != DependencyOK {
			t.Errorf("Dependencies[%q].Status = %q, want %q", name, got, DependencyOK)
		}
	}
}

func TestCheckReportsDegradedWhenAnyDependencyIsDown(t *testing.T) {
	t.Parallel()

	healthy := newServer(t, okHandler)
	broken := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	checker := NewChecker(CheckerOptions{
		Probes: []Probe{
			{Name: "extract", URL: healthy.URL},
			{Name: "persistence", URL: broken.URL},
		},
		Timeout: testTimeout,
		Logger:  logging.Discard(),
	})

	report := checker.Check(context.Background())

	if report.Status != StatusDegraded {
		t.Errorf("Status = %q, want %q", report.Status, StatusDegraded)
	}
	if got := report.Dependencies["extract"].Status; got != DependencyOK {
		t.Errorf("extract.Status = %q, want %q", got, DependencyOK)
	}
	down := report.Dependencies["persistence"]
	if down.Status != DependencyDown {
		t.Errorf("persistence.Status = %q, want %q", down.Status, DependencyDown)
	}
	if down.Detail == "" {
		t.Error("persistence.Detail is empty, want the reason it is down")
	}
}

func TestCheckTreatsRedirectAsDown(t *testing.T) {
	t.Parallel()

	redirecting := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/somewhere-else", http.StatusFound)
	})

	checker := NewChecker(CheckerOptions{
		Probes:  []Probe{{Name: "extract", URL: redirecting.URL}},
		Timeout: testTimeout,
		Logger:  logging.Discard(),
	})

	report := checker.Check(context.Background())

	if got := report.Dependencies["extract"].Status; got != DependencyDown {
		t.Errorf("Status = %q, want %q (only 2xx counts as healthy)", got, DependencyDown)
	}
}

func TestCheckReportsDownWhenProbeTimesOut(t *testing.T) {
	t.Parallel()

	slow := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(asyncWait):
			w.WriteHeader(http.StatusOK)
		}
	})

	checker := NewChecker(CheckerOptions{
		Probes:  []Probe{{Name: "persistence", URL: slow.URL}},
		Timeout: 50 * time.Millisecond,
		Logger:  logging.Discard(),
	})

	start := time.Now()
	report := checker.Check(context.Background())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("Check() took %s, want it to return as soon as the probe times out", elapsed)
	}
	down := report.Dependencies["persistence"]
	if down.Status != DependencyDown {
		t.Errorf("Status = %q, want %q", down.Status, DependencyDown)
	}
	if !strings.Contains(down.Detail, "deadline exceeded") {
		t.Errorf("Detail = %q, want it to mention the deadline", down.Detail)
	}
}

func TestCheckReportsDownWhenUpstreamIsUnreachable(t *testing.T) {
	t.Parallel()

	server := newServer(t, okHandler)
	unreachable := server.URL
	server.Close()

	checker := NewChecker(CheckerOptions{
		Probes:  []Probe{{Name: "audit", URL: unreachable}},
		Timeout: testTimeout,
		Logger:  logging.Discard(),
	})

	report := checker.Check(context.Background())

	down := report.Dependencies["audit"]
	if down.Status != DependencyDown {
		t.Errorf("Status = %q, want %q", down.Status, DependencyDown)
	}
	if down.Detail == "" {
		t.Error("Detail is empty, want the transport error")
	}
}

// La URL de una sonda viene de la configuración y puede llevar credenciales:
// un usuario, y un token en el query string. El error de transporte de
// net/http la envuelve en un *url.Error que incluye la URL entera (sólo
// net/http le quita la contraseña): si esa URL llegara al reporte de readiness
// o al log, el token acabaría en la respuesta de /readyz y en stdout.
func TestCheckNeverLeaksTheCredentialsOfTheProbeURL(t *testing.T) {
	t.Parallel()

	server := newServer(t, okHandler)
	unreachable := server.URL
	server.Close()

	probeURL := "http://extract-user@" + strings.TrimPrefix(unreachable, "http://") +
		"/healthz?token=SECRET-TOKEN"

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	checker := NewChecker(CheckerOptions{
		Probes:  []Probe{{Name: "extract", URL: probeURL}},
		Timeout: testTimeout,
		Logger:  logger,
	})

	report := checker.Check(context.Background())

	down := report.Dependencies["extract"]
	if down.Status != DependencyDown {
		t.Fatalf("Status = %q, want %q", down.Status, DependencyDown)
	}
	if strings.Contains(down.Detail, "SECRET-TOKEN") {
		t.Errorf("Detail = %q, want it without the token of the probe URL", down.Detail)
	}
	if strings.Contains(down.Detail, "extract-user") {
		t.Errorf("Detail = %q, want it without the user of the probe URL", down.Detail)
	}
	if strings.Contains(output.String(), "SECRET-TOKEN") {
		t.Errorf("the log leaked the token of the probe URL: %s", output.String())
	}
	if strings.Contains(output.String(), "extract-user") {
		t.Errorf("the log leaked the user of the probe URL: %s", output.String())
	}
}

// TestCheckProbesRunConcurrently bloquea ambas sondas hasta que cada una avisa
// su llegada y luego las libera. Una implementación secuencial nunca observaría
// la segunda llegada (y fallaría por el deadline), así que esto demuestra que
// las sondas corren en paralelo sin depender de umbrales de reloj.
func TestCheckProbesRunConcurrently(t *testing.T) {
	t.Parallel()

	arrived := make(chan struct{}, 2)
	release := make(chan struct{})

	blocking := func() http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			arrived <- struct{}{}
			select {
			case <-release:
				w.WriteHeader(http.StatusOK)
			case <-r.Context().Done():
			}
		}
	}

	first := newServer(t, blocking())
	second := newServer(t, blocking())

	checker := NewChecker(CheckerOptions{
		Probes: []Probe{
			{Name: "extract", URL: first.URL},
			{Name: "persistence", URL: second.URL},
		},
		Timeout: asyncWait,
		Logger:  logging.Discard(),
	})

	done := make(chan Report, 1)
	go func() { done <- checker.Check(context.Background()) }()

	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(asyncWait):
			t.Fatal("probes did not run concurrently: only one reached its upstream")
		}
	}
	close(release)

	report := <-done
	if report.Status != StatusOK {
		t.Errorf("Status = %q, want %q", report.Status, StatusOK)
	}
}

func TestCheckReportsEveryDependency(t *testing.T) {
	t.Parallel()

	server := newServer(t, okHandler)
	checker := NewChecker(CheckerOptions{
		Probes: []Probe{
			{Name: "extract", URL: server.URL},
			{Name: "persistence", URL: server.URL},
			{Name: "audit", URL: server.URL},
		},
		Timeout: testTimeout,
		Logger:  logging.Discard(),
	})

	report := checker.Check(context.Background())

	if len(report.Dependencies) != 3 {
		t.Fatalf("len(Dependencies) = %d, want 3", len(report.Dependencies))
	}
	for _, name := range []string{"extract", "persistence", "audit"} {
		if _, ok := report.Dependencies[name]; !ok {
			t.Errorf("Dependencies is missing %q", name)
		}
	}
}
