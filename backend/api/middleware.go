package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"meeting-assistant/backend/api/response"
)

// notFound answers unknown routes and wrong methods inside the response envelope.
func notFound(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		probe := &bufferedWriter{header: http.Header{}, status: http.StatusOK}
		h.ServeHTTP(probe, r)
		if allow := probe.header.Get("Allow"); allow != "" {
			w.Header().Set("Allow", allow)
		}
		if probe.status == http.StatusMethodNotAllowed {
			response.Fail(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
			return
		}
		response.Fail(w, http.StatusNotFound, "PAGE_NOT_FOUND")
	})
}

// etag tags JSON GET responses with a hash of the body and answers 304 when it matches.
// The audio stream is left out so it isn't buffered in memory.
func etag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || !strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasSuffix(r.URL.Path, "/recording") {
			next.ServeHTTP(w, r)
			return
		}

		buffered := &bufferedWriter{header: w.Header(), status: http.StatusOK}
		next.ServeHTTP(buffered, r)

		if buffered.status == http.StatusOK {
			sum := sha256.Sum256(buffered.body.Bytes())
			tag := `W/"` + hex.EncodeToString(sum[:8]) + `"`
			w.Header().Set("ETag", tag)
			w.Header().Set("Cache-Control", "no-cache")
			if etagMatches(r.Header.Get("If-None-Match"), tag) {
				w.Header().Del("Content-Type")
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.WriteHeader(buffered.status)
		_, _ = w.Write(buffered.body.Bytes())
	})
}

func etagMatches(ifNoneMatch, tag string) bool {
	weak := strings.TrimPrefix(tag, "W/")
	for _, t := range strings.Split(ifNoneMatch, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == weak {
			return true
		}
	}
	return false
}

// cors lets the listed origins (Swagger UI) call the API from the browser.
func cors(origins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !slices.Contains(origins, origin) {
			next.ServeHTTP(w, r)
			return
		}

		header := w.Header()
		header.Set("Access-Control-Allow-Origin", origin)
		header.Set("Vary", "Origin")
		header.Set("Access-Control-Expose-Headers", "ETag, Location")
		if r.Method == http.MethodOptions {
			header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			header.Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, If-None-Match, Range")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLogger logs one line per request through slog, except health checks.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			return
		}
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration", time.Since(start),
		)
	})
}

type bufferedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedWriter) Header() http.Header         { return b.header }
func (b *bufferedWriter) Write(p []byte) (int, error) { return b.body.Write(p) }
func (b *bufferedWriter) WriteHeader(status int)      { b.status = status }

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
