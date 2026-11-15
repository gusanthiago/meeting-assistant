package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"meeting-assistant/backend/api/response"
)

func TestETag(t *testing.T) {
	h := etag(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/missing" {
			response.Fail(w, http.StatusNotFound, "not found")
			return
		}
		response.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}))
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/v1/meetings/1", nil))
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag on first response")
	}

	tests := []struct {
		name        string
		method      string
		path        string
		ifNoneMatch string
		wantStatus  int
		wantETag    bool
	}{
		{"same tag", http.MethodGet, "/v1/meetings/1", tag, http.StatusNotModified, true},
		{"strong form of weak tag", http.MethodGet, "/v1/meetings/1", tag[2:], http.StatusNotModified, true},
		{"wildcard", http.MethodGet, "/v1/meetings/1", "*", http.StatusNotModified, true},
		{"other tag", http.MethodGet, "/v1/meetings/1", `W/"nope"`, http.StatusOK, true},
		{"no header", http.MethodGet, "/v1/meetings/1", "", http.StatusOK, true},
		{"errors are not tagged", http.MethodGet, "/v1/missing", tag, http.StatusNotFound, false},
		{"audio is not buffered", http.MethodGet, "/v1/meetings/1/recording", tag, http.StatusOK, false},
		{"head behaves like get", http.MethodHead, "/v1/meetings/1", tag, http.StatusNotModified, true},
		{"writes pass through", http.MethodPost, "/v1/meetings", tag, http.StatusOK, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.ifNoneMatch != "" {
				req.Header.Set("If-None-Match", tt.ifNoneMatch)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d; want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("ETag") != ""; got != tt.wantETag {
				t.Errorf("has ETag = %v; want %v", got, tt.wantETag)
			}
			if rec.Code == http.StatusNotModified && rec.Body.Len() != 0 {
				t.Errorf("304 with a body")
			}
		})
	}
}

func TestCORS(t *testing.T) {
	h := cors([]string{"http://localhost:8081"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	tests := []struct {
		name       string
		method     string
		origin     string
		wantStatus int
		wantAllow  string
	}{
		{"allowed preflight", http.MethodOptions, "http://localhost:8081", http.StatusNoContent, "http://localhost:8081"},
		{"allowed request", http.MethodGet, "http://localhost:8081", http.StatusOK, "http://localhost:8081"},
		{"other origin", http.MethodGet, "http://evil.example", http.StatusOK, ""},
		{"same origin", http.MethodGet, "", http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/v1/meetings", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus || rec.Header().Get("Access-Control-Allow-Origin") != tt.wantAllow {
				t.Errorf("status %d allow %q; want %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"), tt.wantStatus, tt.wantAllow)
			}
		})
	}
}
