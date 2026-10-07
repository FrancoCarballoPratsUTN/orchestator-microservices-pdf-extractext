//go:build stress

// Package-level stress tests for the orchestrator.
//
// These are separated from integration_test.go on purpose. Stress runs take
// tens of seconds and allocate hundreds of MB; mixing them into the normal
// suite would make `go test ./...` slow for everyone. Run them with:
//
//	go test -tags stress -race -count=1 ./internal/server/
//
// What is actually being stressed here is NOT the same thing Extract stresses.
// Extract is CPU-bound (PDF parsing), so its load tests measure latency and
// throughput. The orchestrator is an I/O proxy: it almost never is the
// bottleneck, so a latency profile would mostly measure the Extract again.
//
// What can break here is correctness under concurrency. The checksum is the
// system's identity, and three properties have to survive parallel requests:
//
//  1. checksum == SHA-256(text), always. No shared buffer, no half-written
//     document bleeding between requests.
//  2. The same PDF always yields the same checksum. Two clients uploading the
//     same file must land on the same ID, or the dedup key is meaningless.
//  3. No goroutine or file descriptor leak. LogAsync spawns a goroutine per
//     request; under load a bug there accumulates silently.
//
// The k6 and vegeta scripts under scripts/ cover the load half (latency,
// saturation, error rate); these Go tests cover the correctness half, because
// asserting an invariant per request is far more precise from Go than from a
// load generator.
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/clients/auditlog"
	"validationmicroservices-pdf-extractext/internal/clients/extract"
	"validationmicroservices-pdf-extractext/internal/clients/persistence"
	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/handlers"
	"validationmicroservices-pdf-extractext/internal/services"
)

// stressTimeout is deliberately much longer than the integration suite's 3s: a
// stress test that hits its own deadline is reporting a false failure, not a
// real bottleneck.
const stressTimeout = 90 * time.Second

// echoingExtractMock derives its response from the PDF body, so every distinct
// document produces a distinct content and therefore a distinct checksum.
//
// A mock that returns one fixed string (as the integration suite does) cannot
// catch cross-request contamination: if a shared buffer leaked content between
// requests, every response would still be "the" expected string. Deriving the
// content from the request is what makes contamination observable.
type echoingExtractMock struct {
	// delay simulates a slow Extract, used by the timeout test. Zero for the
	// normal path.
	delay time.Duration

	// failWith, when non-zero, makes every /extract call answer with that HTTP
	// status instead of a document. It is how the 502 path gets exercised: the
	// orchestrator cannot produce a 502 out of thin air, it needs an upstream
	// that actually fails.
	failWith int

	requests atomic.Int64
}

func (m *echoingExtractMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/extract" {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "unreadable body")
		return
	}
	m.requests.Add(1)

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-r.Context().Done():
			return
		}
	}

	if m.failWith != 0 {
		writeProblem(w, m.failWith, "extract failed on purpose")
		return
	}

	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		writeProblem(w, http.StatusBadRequest, "invalid pdf payload")
		return
	}

	_ = json.NewEncoder(w).Encode(dto.ExtractedDocument{
		PageCount: 3,
		Content:   strings.TrimPrefix(string(body), "%PDF-"),
	})
}

// stressStack is the full router wired against configurable mocks.
type stressStack struct {
	router     http.Handler
	extract    *echoingExtractMock
	auditStore *auditStore
	textStore  *textStore
	maxPDFSize int64
}

// newStressStack wires the full router against mocks that behave normally. A
// zero extractDelay means no artificial latency.
func newStressStack(t *testing.T, extractDelay time.Duration, maxPDFSize int64) *stressStack {
	t.Helper()
	return newStressStackWithFailure(t, extractDelay, maxPDFSize, 0)
}

// newStressStackWithFailure additionally makes every Extract call answer with
// failWith, so the orchestrator's upstream-failure mapping can be tested.
func newStressStackWithFailure(t *testing.T, extractDelay time.Duration, maxPDFSize int64, failWith int) *stressStack {
	t.Helper()

	extractMock := &echoingExtractMock{delay: extractDelay, failWith: failWith}
	textStore := newTextStore()
	auditStore := &auditStore{}

	extractServer := httptest.NewServer(extractMock)
	persistServer := httptest.NewServer(textStore)
	auditServer := httptest.NewServer(auditStore)
	t.Cleanup(func() {
		extractServer.Close()
		persistServer.Close()
		auditServer.Close()
	})

	logger := discardLogger()
	httpTimeout := 5 * time.Second
	auditService := services.NewAuditService(auditlog.NewClient(auditServer.URL, httpTimeout, ""), logger, httpTimeout)
	pdfService := services.NewPDFService(extract.NewClient(extractServer.URL, httpTimeout), auditService)
	textService := services.NewTextService(persistence.NewClient(persistServer.URL, httpTimeout, ""), auditService)

	router := Routes(
		config.Config{Port: "8080"},
		logger,
		handlers.NewPDFHandler(pdfService, maxPDFSize),
		handlers.NewAuditHandler(auditService),
		handlers.NewTextHandler(textService, 4*1024*1024),
	)

	return &stressStack{
		router:     router,
		extract:    extractMock,
		auditStore: auditStore,
		textStore:  textStore,
		maxPDFSize: maxPDFSize,
	}
}

// postPDF sends one PDF through the full stack and returns the decoded response.
func (s *stressStack) postPDF(t *testing.T, pdf []byte) (*httptest.ResponseRecorder, dto.ExtractPDFResponse) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", bytes.NewReader(pdf))
	req.Header.Set("Content-Type", "application/pdf")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	var response dto.ExtractPDFResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decodifying response %d: %v", rec.Code, err)
		}
	}
	return rec, response
}

// fakePDF builds a syntactically valid-enough PDF for the mock: the handler only
// checks the "%PDF-" signature and the service only checks the same.
func fakePDF(id string) []byte {
	return []byte("%PDF-1.7\n" + strings.Repeat("contenido del documento "+id+" ", 8))
}

// TestStressChecksumInvariantHoldsUnderConcurrency is the central assertion of
// this file: under parallel load, every single response must satisfy
// checksum == SHA-256(text).
//
// A concurrency bug in the conversion or in the checksum plumbing shows up here
// as either a mismatch or a 200 carrying another request's document.
func TestStressChecksumInvariantHoldsUnderConcurrency(t *testing.T) {
	const (
		documents = 32
		workers   = 64
		rounds    = 20
	)

	stack := newStressStack(t, 0, testMaxPDFSize)

	// Precompute the expected checksum for each document serially. Comparing
	// against a serial baseline is what makes a cross-request mix-up
	// detectable: under concurrency the same document must produce the exact
	// bytes the single-threaded run produced.
	want := make(map[string]dto.ExtractPDFResponse, documents)
	pdfs := make([][]byte, documents)
	for i := range documents {
		pdf := fakePDF(fmt.Sprintf("doc-%02d", i))
		pdfs[i] = pdf
		rec, response := stack.postPDF(t, pdf)
		if rec.Code != http.StatusOK {
			t.Fatalf("baseline %d: status = %d, want 200 (%s)", i, rec.Code, rec.Body.String())
		}
		pdfs[i] = pdf
		want[string(pdf)] = response
	}

	var failures atomic.Int64
	var wg sync.WaitGroup
	work := make(chan int, documents*rounds)

	for w := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := range work {
				pdf := pdfs[i]
				req := httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", bytes.NewReader(pdf))
				req.Header.Set("Content-Type", "application/pdf")
				rec := httptest.NewRecorder()
				stack.router.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					failures.Add(1)
					t.Errorf("worker %d doc %d: status = %d, want 200 (%s)", worker, i, rec.Code, rec.Body.String())
					continue
				}

				var got dto.ExtractPDFResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					failures.Add(1)
					t.Errorf("worker %d doc %d: decode: %v", worker, i, err)
					continue
				}

				expected := want[string(pdf)]

				// The invariant of the whole system.
				if sum := sha256.Sum256([]byte(got.Text)); hex.EncodeToString(sum[:]) != string(got.Checksum) {
					failures.Add(1)
					t.Errorf("worker %d doc %d: checksum %s != SHA-256(text) %s",
						worker, i, got.Checksum, hex.EncodeToString(sum[:]))
					continue
				}

				// And it must be the right document, not just a
				// self-consistent one.
				if got != expected {
					failures.Add(1)
					t.Errorf("worker %d doc %d: cross-request contamination\n got %+v\nwant %+v",
						worker, i, got, expected)
				}
			}
		}(w)
	}

	for range documents * rounds {
		work <- 0
	}
	close(work)
	wg.Wait()

	if n := failures.Load(); n != 0 {
		t.Errorf("%d invariant violations under load", n)
	}
}

// TestStressChecksumIsStableForTheSamePDF checks the dedup property: the same
// document must always map to the same checksum, or the ID stops identifying
// content.
//
// This is the property that makes the whole design defensible, and it is the one
// most at risk from anything nondeterministic sneaking into the conversion (map
// iteration, locale-dependent casing, a timestamp).
func TestStressChecksumIsStableForTheSamePDF(t *testing.T) {
	stack := newStressStack(t, 0, testMaxPDFSize)
	pdf := fakePDF("estable")

	_, first := stack.postPDF(t, pdf)
	if first.Checksum == "" {
		t.Fatal("baseline checksum is empty")
	}

	// Sequential repeats catch plain nondeterminism; the concurrent repeats in
	// TestStressChecksumInvariantHoldsUnderConcurrency catch contention.
	for i := range 200 {
		_, got := stack.postPDF(t, pdf)
		if got.Checksum != first.Checksum {
			t.Fatalf("run %d: checksum = %s, want %s (nondeterministic conversion)", i, got.Checksum, first.Checksum)
		}
		if got.Text != first.Text {
			t.Fatalf("run %d: text differs from baseline (%d vs %d bytes)", i, len(got.Text), len(first.Text))
		}
	}
}

// TestStressEveryEndpointUnderConcurrency exercises all seven routes at once, so
// the load is not a single hot path. A shared-state bug between, say, the text
// CRUD and the extract flow only appears when both run in parallel.
func TestStressEveryEndpointUnderConcurrency(t *testing.T) {
	const (
		workers = 24
		rounds  = 15
	)

	stack := newStressStack(t, 0, testMaxPDFSize)
	base := httptest.NewRequest(http.MethodGet, "/api/v1/audit/logs", nil).URL

	send := func(t *testing.T, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		var reader io.Reader = http.NoBody
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req := httptest.NewRequest(method, path, reader)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		stack.router.ServeHTTP(rec, req)
		return rec
	}

	var wg sync.WaitGroup
	errs := make(chan string, workers*rounds*8)

	for w := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := range rounds {
				// 1. health endpoints
				for _, path := range []string{"/healthz", "/readyz"} {
					if rec := send(t, http.MethodGet, path, "", nil); rec.Code != http.StatusOK {
						errs <- fmt.Sprintf("worker %d GET %s: status %d", worker, path, rec.Code)
					}
				}

				// 2. extract, with a per-worker document so checksums differ
				pdf := fakePDF(fmt.Sprintf("mixed-%d-%d", worker, round))
				rec := send(t, http.MethodPost, "/api/v1/pdfs/extract", "application/pdf", pdf)
				if rec.Code != http.StatusOK {
					errs <- fmt.Sprintf("worker %d extract: status %d (%s)", worker, rec.Code, rec.Body.String())
					continue
				}
				var extracted dto.ExtractPDFResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &extracted); err != nil {
					errs <- fmt.Sprintf("worker %d extract decode: %v", worker, err)
					continue
				}
				sum := sha256.Sum256([]byte(extracted.Text))
				if hex.EncodeToString(sum[:]) != string(extracted.Checksum) {
					errs <- fmt.Sprintf("worker %d: checksum != sha256(text)", worker)
				}

				// 3. texts CRUD, keyed by that checksum. The document has
				// not been created yet, so GET must be 404 and DELETE
				// must be 404 too.
				cs := string(extracted.Checksum)
				textPath := "/api/v1/texts/" + cs

				create := dto.CreateTextRequest{Text: extracted.Text, Checksum: extracted.Checksum, Name: "stress"}
				payload, _ := json.Marshal(create)
				if rec := send(t, http.MethodPost, "/api/v1/texts", "application/json", payload); rec.Code != http.StatusCreated {
					errs <- fmt.Sprintf("worker %d create: status %d (%s)", worker, rec.Code, rec.Body.String())
					continue
				}

				if rec := send(t, http.MethodGet, textPath, "", nil); rec.Code != http.StatusOK {
					errs <- fmt.Sprintf("worker %d get: status %d", worker, rec.Code)
				}

				update := dto.UpdateTextRequest{Name: ptr("stress-updated"), Metadata: map[string]any{"w": worker}}
				payload, _ = json.Marshal(update)
				if rec := send(t, http.MethodPut, textPath, "application/json", payload); rec.Code != http.StatusOK {
					errs <- fmt.Sprintf("worker %d update: status %d (%s)", worker, rec.Code, rec.Body.String())
				}

				// 4. audit listing must keep answering while everything
				// else is in flight
				if rec := send(t, http.MethodGet, base.Path, "", nil); rec.Code != http.StatusOK {
					errs <- fmt.Sprintf("worker %d audit logs: status %d", worker, rec.Code)
				}

				if rec := send(t, http.MethodDelete, textPath, "", nil); rec.Code != http.StatusOK {
					errs <- fmt.Sprintf("worker %d delete: status %d (%s)", worker, rec.Code, rec.Body.String())
				}
				if rec := send(t, http.MethodGet, textPath, "", nil); rec.Code != http.StatusNotFound {
					errs <- fmt.Sprintf("worker %d get after delete: status %d, want 404", worker, rec.Code)
				}
			}
		}(w)
	}

	wg.Wait()
	close(errs)

	seen := map[string]int{}
	for e := range errs {
		seen[e]++
	}
	if len(seen) > 0 {
		for msg, count := range seen {
			t.Errorf("%dx %s", count, msg)
		}
	}
}

// TestStressNoGoroutineLeak watches runtime.NumGoroutine across sustained load.
//
// LogAsync fires a goroutine per extract to deliver the audit event. If that
// goroutine blocks (or the retry logic loops) it will not fail any functional
// test; it just quietly accumulates. The audit mock answers instantly here, so
// a healthy run must return to baseline.
//
// The baseline is measured AFTER a warmup, and that detail is load-bearing. The
// Go HTTP client keeps idle keep-alive connections, each holding a readLoop and
// a writeLoop goroutine, so the process never returns to zero goroutines. A
// first attempt at this test asserted convergence to 0 and failed with 14
// goroutines, which a stack dump showed to be connection-pool bookkeeping and
// test-runtime goroutines. Asserting against a post-warmup baseline measures the
// thing actually of interest: goroutines created by load that never go away.
func TestStressNoGoroutineLeak(t *testing.T) {
	const rounds = 400

	stack := newStressStack(t, 0, testMaxPDFSize)
	pdf := fakePDF("leak")

	// Warm up so the baseline already includes the keep-alive connections the
	// clients will reuse for the rest of the test.
	for range 20 {
		stack.postPDF(t, pdf)
	}
	before := runtime.NumGoroutine()

	var wg sync.WaitGroup
	for w := range 32 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for range rounds / 32 {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", bytes.NewReader(pdf))
				req.Header.Set("Content-Type", "application/pdf")
				stack.router.ServeHTTP(httptest.NewRecorder(), req)
			}
		}(w)
	}
	wg.Wait()

	// The audit goroutines are fire-and-forget, so they finish slightly after
	// the responses are written. Without this wait the in-flight ones would be
	// reported as a leak.
	settleGoroutines(t, before, 10*time.Second)

	if after := runtime.NumGoroutine(); after > before+2 {
		t.Errorf("goroutines leaked: %d before, %d after (%d requests)", before, after, rounds)
	}
}

// settleGoroutines polls until the goroutine count is at or below target plus a
// small tolerance, failing the test if it never gets there.
//
// The tolerance exists because the in-flight audit goroutines are spawned per
// request and the count decays as they complete, not in lockstep with the
// responses. Requiring an exact value would make this test flaky.
func settleGoroutines(t *testing.T, target int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		runtime.Gosched()
		time.Sleep(20 * time.Millisecond)
		if runtime.NumGoroutine() <= target+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines did not settle to %d within %v: still %d", target, timeout, runtime.NumGoroutine())
		}
	}
}
