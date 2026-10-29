package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"validationmicroservices-pdf-extractext/internal/httpapi"
	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/reqctx"
)

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := reqctx.WithRequestID(r.Context(), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

// maxMethodLength acota el método HTTP que se registra. Los métodos reales
// ("GET", "PROPFIND") no llegan al tope: sólo un cliente que los infla llega.
const maxMethodLength = 16

// withRecovery convierte un panic en un 500 en vez de tumbar el proceso. El
// stack se registra porque un panic es un fallo real que hay que depurar, pero
// acotado para que una línea no crezca sin límite.
//
// El recovery va dentro del access log (ver Routes), así que un panic también
// queda registrado como una petición más: si no, el 500 sería invisible en el
// access log.
func withRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				reqctx.Logger(r.Context(), logger).Error("panic recovered",
					"panic", logging.Truncate(fmt.Sprint(recovered), logging.MaxValueLength),
					"method", logging.Truncate(r.Method, maxMethodLength),
					"path", logging.Truncate(r.URL.Path, logging.MaxValueLength),
					"stack", logging.Truncate(string(debug.Stack()), logging.MaxStackLength),
				)
				// Si la respuesta ya empezó, no se puede reescribir: un
				// segundo documento dejaría al cliente con un cuerpo
				// corrupto. En ese caso sólo queda el log.
				if recorder, ok := w.(*statusRecorder); ok && recorder.status != 0 {
					return
				}
				httpapi.WriteProblem(w, http.StatusInternalServerError, "Internal Server Error", "internal server error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func enforceContentType(mediaTypes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actual, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || !containsString(mediaTypes, actual) {
				httpapi.WriteProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "expected Content-Type "+strings.Join(mediaTypes, ", "))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func withCORS() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-Filename")
			w.Header().Set("Access-Control-Max-Age", "300")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
