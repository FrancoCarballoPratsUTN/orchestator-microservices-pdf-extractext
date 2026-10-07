//go:build stress

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/httpclient"
)

// TestStressLimitAndErrorPaths walks every non-200 the API can produce, while
// load runs in the background.
//
// The reason to run this concurrently is that error paths are the ones most
// likely to be shared-state bugs: the 413 branch wraps the body in a
// MaxBytesReader, the 422 branch returns before the audit call, and the 502
// branch runs on a shared client. Any of them could behave differently when the
// process is already busy.
func TestStressLimitAndErrorPaths(t *testing.T) {
	// A small limit keeps the oversized cases cheap: the point is the boundary
	// behaviour, not the size of the buffer.
	const maxPDFSize = int64(64 * 1024)

	stack := newStressStack(t, 0, maxPDFSize)

	// A PDF exactly at the limit must be accepted, and one byte over must be
	// rejected. Testing only the "too big" side would pass even if the limit
	// were off by one in the strict direction.
	exactlyAtLimit := makePDFOfSize(maxPDFSize)
	oneOverLimit := makePDFOfSize(maxPDFSize + 1)

	cases := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        []byte
		want        int
	}{
		{
			name: "pdf at the size limit is accepted",
			// fakePDF content is echoed back by the mock, so the body must
			// stay under the limit once converted.
			method: http.MethodPost, path: "/api/v1/pdfs/extract",
			contentType: "application/pdf", body: exactlyAtLimit, want: http.StatusOK,
		},
		{
			name:   "pdf one byte over the limit is 413",
			method: http.MethodPost, path: "/api/v1/pdfs/extract",
			contentType: "application/pdf", body: oneOverLimit, want: http.StatusRequestEntityTooLarge,
		},
		{
			name:   "empty body is 400",
			method: http.MethodPost, path: "/api/v1/pdfs/extract",
			contentType: "application/pdf", body: []byte{}, want: http.StatusBadRequest,
		},
		{
			name:   "body without the pdf signature is 400",
			method: http.MethodPost, path: "/api/v1/pdfs/extract",
			contentType: "application/pdf", body: []byte("esto no es un pdf en absoluto"), want: http.StatusBadRequest,
		},
		{
			name:   "a body that is only the signature reaches Extract and comes back as 422",
			method: http.MethodPost, path: "/api/v1/pdfs/extract",
			contentType: "application/pdf", body: []byte("%PDF-"), want: http.StatusUnprocessableEntity,
		},
		{
			name:   "wrong content type is 415",
			method: http.MethodPost, path: "/api/v1/pdfs/extract",
			contentType: "application/json", body: fakePDF("ct"), want: http.StatusUnsupportedMediaType,
		},
		{
			name:   "missing content type is 415",
			method: http.MethodPost, path: "/api/v1/pdfs/extract",
			body: fakePDF("no-ct"), want: http.StatusUnsupportedMediaType,
		},
		{
			name:   "unknown route is 404",
			method: http.MethodGet, path: "/api/v1/nope",
			body: nil, want: http.StatusNotFound,
		},
		{
			name:   "method not allowed on a known path is 405",
			method: http.MethodPatch, path: "/api/v1/pdfs/extract",
			body: nil, want: http.StatusMethodNotAllowed,
		},
		{
			name:   "get on a checksum that does not exist is 404",
			method: http.MethodGet, path: "/api/v1/texts/" + strings.Repeat("0", 64),
			body: nil, want: http.StatusNotFound,
		},
		{
			name:   "create without a content type is 415",
			method: http.MethodPost, path: "/api/v1/texts",
			body: []byte(`{"text":"x","checksum":"y"}`), want: http.StatusUnsupportedMediaType,
		},
		{
			name:   "update without a content type is 415",
			method: http.MethodPut, path: "/api/v1/texts/" + strings.Repeat("0", 64),
			body: []byte(`{"name":"x"}`), want: http.StatusUnsupportedMediaType,
		},
		{
			name:   "malformed json payload is 400",
			method: http.MethodPost, path: "/api/v1/texts", contentType: "application/json",
			body: []byte(`{"text":`), want: http.StatusBadRequest,
		},
	}

	// Background load so the error paths are exercised against a busy process.
	stop := make(chan struct{})
	var loadWG sync.WaitGroup
	var loadErrs atomic.Int64
	for w := range 16 {
		loadWG.Add(1)
		go func(worker int) {
			defer loadWG.Done()
			pdf := fakePDF(fmt.Sprintf("load-%d", worker))
			for {
				select {
				case <-stop:
					return
				default:
				}
				req := httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", bytes.NewReader(pdf))
				req.Header.Set("Content-Type", "application/pdf")
				rec := httptest.NewRecorder()
				stack.router.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					loadErrs.Add(1)
				}
			}
		}(w)
	}

	// Repeat each case so an intermittent failure has a chance to show up.
	for round := range 5 {
		for _, tc := range cases {
			var reader io.Reader = http.NoBody
			if tc.body != nil {
				reader = bytes.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, reader)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rec := httptest.NewRecorder()
			stack.router.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("round %d %s: status = %d, want %d (%s)",
					round, tc.name, rec.Code, tc.want, rec.Body.String())
			}

			// Every error must be an RFC 9457 problem document, not a bare
			// status. A 500 with an empty body would be indistinguishable to a
			// client from a crash.
			if tc.want >= 400 {
				var problem httpclient.Problem
				if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
					t.Errorf("round %d %s: body is not a problem document: %v (%s)",
						round, tc.name, err, rec.Body.String())
				} else {
					if problem.Status != tc.want {
						t.Errorf("round %d %s: problem.status = %d, want %d", round, tc.name, problem.Status, tc.want)
					}
					if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
						t.Errorf("round %d %s: Content-Type = %q, want application/problem+json", round, tc.name, got)
					}
				}
			}
		}
	}

	close(stop)
	loadWG.Wait()

	if n := loadErrs.Load(); n != 0 {
		t.Errorf("%d background load requests failed; the stack should stay healthy under error traffic", n)
	}
}

// makePDFOfSize builds a syntactically valid PDF of exactly n bytes.
func makePDFOfSize(n int64) []byte {
	const prefix = "%PDF-1.7\n"
	const suffix = "\n%%EOF"
	body := n - int64(len(prefix)+len(suffix))
	if body < 0 {
		return []byte(prefix + suffix)
	}
	return []byte(prefix + strings.Repeat("x", int(body)) + suffix)
}

// TestStressUpstreamFailuresBecome502 checks how each upstream failure from
// Extract is translated, since every one of them must not be reported as a 500
// or, worse, as a success.
//
// The translation matters to the client: a 503 from Extract means "it is busy,
// retry" and a 400 means "this PDF is broken", but both arrive at the caller of
// the orchestrator as 502. That flattening is a deliberate design choice (the
// handler only distinguishes invalid-PDF, no-text and everything else), so the
// test pins the behaviour rather than treating it as an oversight.
func TestStressUpstreamFailuresBecome502(t *testing.T) {
	for _, upstream := range []int{
		http.StatusBadRequest,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
		http.StatusNotImplemented,
	} {
		t.Run(http.StatusText(upstream), func(t *testing.T) {
			stack := newStressStackWithFailure(t, 0, testMaxPDFSize, upstream)

			rec, _ := stack.postPDF(t, fakePDF("upstream-fail"))
			if rec.Code != http.StatusBadGateway {
				t.Errorf("upstream %d: status = %d, want 502 (%s)", upstream, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("upstream %d: Content-Type = %q, want application/problem+json", upstream, ct)
			}

			// A failed extraction must not leave an audit event behind.
			time.Sleep(300 * time.Millisecond)
			if actions := stack.auditStore.actions(); len(actions) != 0 {
				t.Errorf("upstream %d: failed extraction was audited: %v", upstream, actions)
			}
		})
	}
}

// TestStressUpstreamTimeoutBecomes502 checks that a slow Extract degrades to a
// clean 502 instead of hanging the request forever.
//
// The orchestrator's HTTP timeout (35s in production, 5s here) has to fire before
// the process gives up. The interesting part is that the client disconnects: the
// handler must still write a problem document rather than leaving the connection
// hanging, because a hung connection is what exhausts a server's file
// descriptors under load.
func TestStressUpstreamTimeoutBecomes502(t *testing.T) {
	// Longer than the stack's 5s HTTP timeout, so the timeout is what fires.
	stack := newStressStack(t, 30*time.Second, testMaxPDFSize)
	pdf := fakePDF("timeout")

	start := time.Now()
	rec, _ := stack.postPDF(t, pdf)
	elapsed := time.Since(start)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (%s)", rec.Code, rec.Body.String())
	}
	if elapsed > 20*time.Second {
		t.Errorf("the request took %v; the client timeout did not fire promptly", elapsed)
	}
	if rec.Body.Len() == 0 {
		t.Error("502 response has an empty body; clients get nothing to report")
	}

	t.Logf("timeout produced 502 after %v", elapsed.Round(time.Millisecond))
}

// TestStressScannedPDFStays422UnderLoad re-runs the scanned-document case under
// concurrency.
//
// The 422 is the one path that must NOT produce an audit event, and audit
// delivery is asynchronous. A check made right after one request can pass even
// when the goroutine was in fact queued, so the assertion here waits for the
// audit store to be given a chance and then requires it to still be empty.
func TestStressScannedPDFStays422UnderLoad(t *testing.T) {
	const requests = 200

	stack := newIntegrationStackWithExtractContent(t, "")
	pdf := fakePDF("scanned")

	var codes sync.Map
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range requests / 16 {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", bytes.NewReader(pdf))
				req.Header.Set("Content-Type", "application/pdf")
				rec := httptest.NewRecorder()
				stack.router.ServeHTTP(rec, req)
				codes.Store(rec.Code, true)
			}
		}()
	}
	wg.Wait()

	if _, ok := codes.Load(http.StatusUnprocessableEntity); !ok {
		t.Error("no request returned 422; the scanned-document path is not being exercised")
	}
	if _, ok := codes.Load(http.StatusOK); ok {
		t.Error("a scanned PDF returned 200: that is the SHA-256(\"\") collision the rule exists to prevent")
	}

	// Give the fire-and-forget audit goroutines every chance to misbehave.
	time.Sleep(500 * time.Millisecond)
	if actions := stack.auditStore.actions(); len(actions) != 0 {
		t.Errorf("rejected documents were audited: %v", actions)
	}
}
