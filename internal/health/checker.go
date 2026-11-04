// Package health sondea las dependencias del orquestador (los microservicios)
// para responder la readiness agregada de /readyz.
package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"validationmicroservices-pdf-extractext/internal/logging"
)

const (
	// StatusOK indica que todas las dependencias respondieron sanas.
	StatusOK = "ok"
	// StatusDegraded indica que al menos una dependencia está caída.
	StatusDegraded = "degraded"

	DependencyOK   = "ok"
	DependencyDown = "down"
)

// Nombres de las dependencias sondadas. Son estables: aparecen como clave en
// el reporte de readiness, así que un cliente puede consultarlos por nombre.
const (
	ServiceExtract     = "extract"
	ServicePersistence = "persistence"
	ServiceAudit       = "audit"
)

// Probe describe una dependencia a sondear: un nombre estable (el que aparece
// como clave en el reporte de readiness) y su URL de salud directa.
type Probe struct {
	Name string
	URL  string
}

// Dependency es el resultado de sondear una dependencia.
type Dependency struct {
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Detail     string `json:"detail,omitempty"`
}

// Report es la readiness agregada: ok sólo si todas las dependencias están ok.
type Report struct {
	Status       string                `json:"status"`
	Dependencies map[string]Dependency `json:"dependencies"`
}

// Checker sondea sus dependencias en paralelo. Es seguro para uso concurrente.
type Checker struct {
	probes    []Probe
	timeout   time.Duration
	client    *http.Client
	logger    *slog.Logger
	logLimits logging.Limits
}

// CheckerOptions configures a health Checker.
type CheckerOptions struct {
	Probes  []Probe
	Timeout time.Duration
	Logger  *slog.Logger
	Limits  logging.Limits
}

// NewChecker construye un Checker con las opciones provistas. Un logger nil
// desactiva el logging.
func NewChecker(opts CheckerOptions) *Checker {
	return &Checker{
		probes:  opts.Probes,
		timeout: opts.Timeout,
		client: &http.Client{
			// El presupuesto lo impone el contexto por sonda; además no se
			// siguen redirects: sólo un 2xx directo cuenta como sano.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		logger:    opts.Logger,
		logLimits: opts.Limits,
	}
}

// Check sondea todas las dependencias de forma concurrente y devuelve el
// reporte agregado. El total ronda la sonda más lenta, no la suma.
func (c *Checker) Check(ctx context.Context) Report {
	results := make([]Dependency, len(c.probes))

	var wg sync.WaitGroup
	for i, probe := range c.probes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = c.checkOne(ctx, probe)
		}()
	}
	wg.Wait()

	return aggregate(c.probes, results)
}

func (c *Checker) checkOne(ctx context.Context, probe Probe) Dependency {
	probeCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	start := time.Now()
	dep := Dependency{Status: DependencyOK}

	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, probe.URL, nil)
	if err != nil {
		dep.Status = DependencyDown
		dep.Detail = probeDetail(err)
	} else {
		response, err := c.client.Do(request)
		if err != nil {
			dep.Status = DependencyDown
			dep.Detail = probeDetail(err)
		} else {
			defer response.Body.Close()
			if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
				dep.Status = DependencyDown
				dep.Detail = fmt.Sprintf("unexpected status %d", response.StatusCode)
			}
		}
	}

	dep.DurationMS = time.Since(start).Milliseconds()
	c.log(ctx, probe, dep)
	return dep
}

// aggregate arma el reporte preservando el nombre de cada sonda. El status es
// ok sólo si todas las dependencias respondieron sanas.
func aggregate(probes []Probe, results []Dependency) Report {
	report := Report{Status: StatusOK, Dependencies: make(map[string]Dependency, len(probes))}
	for i, probe := range probes {
		report.Dependencies[probe.Name] = results[i]
		if results[i].Status != DependencyOK {
			report.Status = StatusDegraded
		}
	}
	return report
}

// probeDetail normaliza el error de una sonda. Un deadline agotado se reporta
// como "context deadline exceeded", sin el ruido de la URL envolvente.
//
// net/http envuelve sus errores en un *url.Error que incluye la URL
// completa. Esa URL viene de la configuración y puede llevar credenciales
// (http://user:pass@host) o un token en el query string, así que no puede
// devolverse al cliente en el reporte de readiness ni escribirse en el log:
// logging.SafeError conserva sólo el error de verdad, que es el que explica
// el fallo.
func probeDetail(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded.Error()
	}
	return logging.SafeError(err)
}

func (c *Checker) log(ctx context.Context, probe Probe, dep Dependency) {
	if c.logger == nil {
		return
	}
	level := slog.LevelDebug
	if dep.Status != DependencyOK {
		level = slog.LevelWarn
	}
	c.logger.Log(ctx, level, "health probe",
		"probe", probe.Name, "url", logging.SafeURL(probe.URL, c.logLimits.ValueLength()),
		"status", dep.Status, "duration_ms", dep.DurationMS, "detail", dep.Detail)
}
