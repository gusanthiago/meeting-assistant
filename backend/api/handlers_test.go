package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"meeting-assistant/backend/api/response"
	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/meeting"
	"meeting-assistant/backend/internal/search"
	"meeting-assistant/backend/internal/storage"
	"meeting-assistant/backend/internal/store/db"
)

var testID = uuid.MustParse("01900000-0000-7000-8000-0000000000aa")

type fakeMeetings struct {
	create func(meeting.CreateInput) (db.Meeting, bool, error)
	get    func(uuid.UUID) (meeting.Detail, error)
	upload func(contentType string) (db.Meeting, error)
}

func (f *fakeMeetings) Create(_ context.Context, in meeting.CreateInput) (db.Meeting, bool, error) {
	return f.create(in)
}
func (f *fakeMeetings) List(context.Context, *uuid.UUID, int) ([]db.Meeting, *uuid.UUID, error) {
	return []db.Meeting{{ID: testID, Title: "Sync", Status: "ready"}}, nil, nil
}
func (f *fakeMeetings) Get(_ context.Context, id uuid.UUID) (meeting.Detail, error) { return f.get(id) }
func (f *fakeMeetings) Transcript(context.Context, uuid.UUID) (db.Transcript, error) {
	return db.Transcript{}, domain.ErrNotFound
}
func (f *fakeMeetings) UploadRecording(_ context.Context, _ uuid.UUID, _ io.Reader, ct string, _ *int64) (db.Meeting, error) {
	return f.upload(ct)
}
func (f *fakeMeetings) OpenRecording(context.Context, uuid.UUID, string) (*storage.Object, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeMeetings) RegenerateSummary(context.Context, uuid.UUID) (db.Meeting, error) {
	return db.Meeting{}, fmt.Errorf("%w: meeting is transcribing", domain.ErrConflict)
}
func (f *fakeMeetings) Retry(_ context.Context, id uuid.UUID) (db.Meeting, error) {
	if id != testID {
		return db.Meeting{}, fmt.Errorf("%w: meeting is ready", domain.ErrConflict)
	}
	return db.Meeting{ID: id, Title: "Sync", Status: "queued"}, nil
}
func (f *fakeMeetings) Delete(_ context.Context, id uuid.UUID) error {
	if id != testID {
		return domain.ErrNotFound
	}
	return nil
}
func (f *fakeMeetings) SetActionItemDone(_ context.Context, id uuid.UUID, done bool) (db.ActionItem, error) {
	return db.ActionItem{ID: id, Text: "Send notes", Done: done}, nil
}

type fakeSearch struct{}

func (fakeSearch) Search(context.Context, uuid.UUID, string, int) ([]search.Hit, error) {
	return []search.Hit{{MeetingID: testID, Title: "Weekly sync"}}, nil
}

// newTestRouter builds the full router over fakes, so these tests cover routing, envelopes and error mapping together.
func newTestRouter(t *testing.T, meetings *fakeMeetings, checks map[string]Check) http.Handler {
	t.Helper()
	h := NewHandler(meetings, fakeSearch{}, domain.DefaultWorkspaceID, checks)
	return NewRouter(h, []string{"http://localhost:8081"}, "testdata/openapi.yaml")
}

func do(t *testing.T, router http.Handler, method, target, contentType, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) response.WebResponse {
	t.Helper()
	var envelope response.WebResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding response: %v (body: %s)", err, rec.Body.String())
	}
	if envelope.Code != rec.Code || envelope.Status != http.StatusText(rec.Code) {
		t.Errorf("envelope code/status = %d %q, HTTP status %d", envelope.Code, envelope.Status, rec.Code)
	}
	return envelope
}

func TestRoutes(t *testing.T) {
	meetings := &fakeMeetings{
		create: func(in meeting.CreateInput) (db.Meeting, bool, error) {
			if in.Title == "invalid" {
				return db.Meeting{}, false, fmt.Errorf("%w: title is required", domain.ErrValidation)
			}
			return db.Meeting{ID: testID, Title: in.Title, Status: "recording"}, in.IdempotencyKey != "replay", nil
		},
		get: func(id uuid.UUID) (meeting.Detail, error) {
			if id != testID {
				return meeting.Detail{}, domain.ErrNotFound
			}
			return meeting.Detail{Meeting: db.Meeting{ID: id, Title: "Sync", Status: "ready"}}, nil
		},
		upload: func(ct string) (db.Meeting, error) {
			switch ct {
			case "text/plain":
				return db.Meeting{}, fmt.Errorf("%w: content type must be audio/*", domain.ErrValidation)
			case "audio/huge":
				return db.Meeting{}, domain.ErrTooLarge
			case "audio/busy":
				return db.Meeting{}, fmt.Errorf("%w: meeting is transcribing", domain.ErrConflict)
			case "audio/broken":
				return db.Meeting{}, errors.New("disk on fire")
			}
			return db.Meeting{ID: testID, Status: "queued"}, nil
		},
	}
	router := newTestRouter(t, meetings, map[string]Check{"postgres": func(context.Context) error { return nil }})
	meetingPath := "/v1/meetings/" + testID.String()

	tests := []struct {
		name        string
		method      string
		target      string
		contentType string
		body        string
		headers     map[string]string
		wantStatus  int
		wantError   string
		wantData    string
	}{
		{"health", http.MethodGet, "/healthz", "", "", nil, http.StatusOK, "", `"status":"ok"`},
		{"readiness", http.MethodGet, "/readyz", "", "", nil, http.StatusOK, "", `"postgres":"ok"`},
		{"unknown route", http.MethodGet, "/nope", "", "", nil, http.StatusNotFound, "PAGE_NOT_FOUND", ""},
		{"wrong method", http.MethodDelete, "/healthz", "", "", nil, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", ""},
		{"invalid id", http.MethodGet, "/v1/meetings/not-a-uuid", "", "", nil, http.StatusBadRequest, "meetingId", ""},
		{"list", http.MethodGet, "/v1/meetings?limit=5", "", "", nil, http.StatusOK, "", `"title":"Sync"`},
		{"create", http.MethodPost, "/v1/meetings", "application/json", `{"title":"Weekly sync"}`, nil, http.StatusCreated, "", `"status":"recording"`},
		{"create replay", http.MethodPost, "/v1/meetings", "application/json", `{"title":"Weekly sync"}`, map[string]string{"Idempotency-Key": "replay"}, http.StatusOK, "", `"title":"Weekly sync"`},
		{"create bad json", http.MethodPost, "/v1/meetings", "application/json", `{"title":`, nil, http.StatusBadRequest, "invalid JSON body", ""},
		{"create unknown field", http.MethodPost, "/v1/meetings", "application/json", `{"title":"a","color":"red"}`, nil, http.StatusBadRequest, "unknown field", ""},
		{"create title too long", http.MethodPost, "/v1/meetings", "application/json", `{"title":"` + strings.Repeat("a", 201) + `"}`, nil, http.StatusUnprocessableEntity, "at most 200", ""},
		{"create rejected by service", http.MethodPost, "/v1/meetings", "application/json", `{"title":"invalid"}`, nil, http.StatusUnprocessableEntity, "title is required", ""},
		{"get", http.MethodGet, meetingPath, "", "", nil, http.StatusOK, "", `"action_items":[]`},
		{"get missing", http.MethodGet, "/v1/meetings/" + uuid.NewString(), "", "", nil, http.StatusNotFound, "not found", ""},
		{"transcript missing", http.MethodGet, meetingPath + "/transcript", "", "", nil, http.StatusNotFound, "not found", ""},
		{"upload", http.MethodPut, meetingPath + "/recording", "audio/webm", "voice", nil, http.StatusAccepted, "", `"status":"queued"`},
		{"upload not audio", http.MethodPut, meetingPath + "/recording", "text/plain", "x", nil, http.StatusUnsupportedMediaType, "audio", ""},
		{"upload too large", http.MethodPut, meetingPath + "/recording", "audio/huge", "x", nil, http.StatusRequestEntityTooLarge, "too large", ""},
		{"upload while processing", http.MethodPut, meetingPath + "/recording", "audio/busy", "x", nil, http.StatusConflict, "transcribing", ""},
		{"upload unexpected error", http.MethodPut, meetingPath + "/recording", "audio/broken", "x", nil, http.StatusInternalServerError, "check the server logs", ""},
		{"create title with accents", http.MethodPost, "/v1/meetings", "application/json", `{"title":"` + strings.Repeat("ç", 200) + `"}`, nil, http.StatusCreated, "", `"status":"recording"`},
		{"create participant too long", http.MethodPost, "/v1/meetings", "application/json", `{"title":"a","participants":["` + strings.Repeat("a", 101) + `"]}`, nil, http.StatusUnprocessableEntity, "100 characters", ""},
		{"delete", http.MethodDelete, meetingPath, "", "", nil, http.StatusNoContent, "", ""},
		{"delete missing", http.MethodDelete, "/v1/meetings/" + uuid.NewString(), "", "", nil, http.StatusNotFound, "not found", ""},
		{"retry", http.MethodPost, meetingPath + "/retry", "", "", nil, http.StatusAccepted, "", `"status":"queued"`},
		{"retry not failed", http.MethodPost, "/v1/meetings/" + uuid.NewString() + "/retry", "", "", nil, http.StatusConflict, "ready", ""},
		{"regenerate while processing", http.MethodPost, meetingPath + "/summary/regenerate", "", "", nil, http.StatusConflict, "transcribing", ""},
		{"toggle action item", http.MethodPatch, "/v1/action-items/" + testID.String(), "application/json", `{"done":true}`, nil, http.StatusOK, "", `"done":true`},
		{"search", http.MethodGet, "/v1/search?q=sync", "", "", nil, http.StatusOK, "", `"title":"Weekly sync"`},
		{"search empty", http.MethodGet, "/v1/search?q=", "", "", nil, http.StatusBadRequest, "1 to 200", ""},
		{"search only spaces", http.MethodGet, "/v1/search?q=%20%20", "", "", nil, http.StatusBadRequest, "1 to 200", ""},
		{"search accented query", http.MethodGet, "/v1/search?q=" + strings.Repeat("%C3%A7", 150), "", "", nil, http.StatusOK, "", `"title":"Weekly sync"`},
		{"search too long", http.MethodGet, "/v1/search?q=" + strings.Repeat("a", 201), "", "", nil, http.StatusBadRequest, "1 to 200", ""},
		{"search missing q", http.MethodGet, "/v1/search", "", "", nil, http.StatusBadRequest, "q", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, router, tt.method, tt.target, tt.contentType, tt.body, tt.headers)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d; want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if rec.Code == http.StatusNoContent {
				if rec.Body.Len() != 0 {
					t.Errorf("204 with body %q", rec.Body)
				}
				return
			}
			envelope := decode(t, rec)
			if tt.wantError != "" && !strings.Contains(envelope.Error, tt.wantError) {
				t.Errorf("error = %q; want it to contain %q", envelope.Error, tt.wantError)
			}
			if tt.wantError == "" && envelope.Error != "" {
				t.Errorf("unexpected error %q", envelope.Error)
			}
			if tt.wantData != "" {
				data, _ := json.Marshal(envelope.Data)
				if !strings.Contains(string(data), tt.wantData) {
					t.Errorf("data = %s; want it to contain %s", data, tt.wantData)
				}
			}
		})
	}
}

func TestReadinessReportsFailingDependency(t *testing.T) {
	router := newTestRouter(t, &fakeMeetings{}, map[string]Check{
		"postgres":   func(context.Context) error { return nil },
		"opensearch": func(context.Context) error { return errors.New("connection refused") },
	})
	rec := do(t, router, http.MethodGet, "/readyz", "", "", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503", rec.Code)
	}
	data, _ := json.Marshal(decode(t, rec).Data)
	if !strings.Contains(string(data), `"opensearch":"connection refused"`) {
		t.Errorf("data = %s", data)
	}
}
