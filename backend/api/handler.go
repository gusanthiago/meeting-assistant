package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"meeting-assistant/backend/api/gen"
	"meeting-assistant/backend/api/response"
	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/meeting"
	"meeting-assistant/backend/internal/search"
	"meeting-assistant/backend/internal/storage"
	"meeting-assistant/backend/internal/store/db"
)

const maxJSONBody = 1 << 20

type MeetingService interface {
	Create(ctx context.Context, in meeting.CreateInput) (db.Meeting, bool, error)
	List(ctx context.Context, cursor *uuid.UUID, limit int) ([]db.Meeting, *uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (meeting.Detail, error)
	Transcript(ctx context.Context, id uuid.UUID) (db.Transcript, error)
	UploadRecording(ctx context.Context, id uuid.UUID, body io.Reader, contentType string, durationMs *int64) (db.Meeting, error)
	OpenRecording(ctx context.Context, id uuid.UUID, byteRange string) (*storage.Object, error)
	RegenerateSummary(ctx context.Context, id uuid.UUID) (db.Meeting, error)
	Retry(ctx context.Context, id uuid.UUID) (db.Meeting, error)
	Delete(ctx context.Context, id uuid.UUID) error
	SetActionItemDone(ctx context.Context, id uuid.UUID, done bool) (db.ActionItem, error)
}

type Searcher interface {
	Search(ctx context.Context, workspaceID uuid.UUID, q string, limit int) ([]search.Hit, error)
}

type Check func(context.Context) error

type Handler struct {
	meetings    MeetingService
	search      Searcher
	workspaceID uuid.UUID
	checks      map[string]Check
}

var _ gen.ServerInterface = (*Handler)(nil)

// NewHandler creates a Handler over the given services.
func NewHandler(meetings MeetingService, searcher Searcher, workspaceID uuid.UUID, checks map[string]Check) *Handler {
	return &Handler{meetings: meetings, search: searcher, workspaceID: workspaceID, checks: checks}
}

// reply writes data inside the envelope with status, or the matching error when err is set.
func (h *Handler) reply(w http.ResponseWriter, r *http.Request, status int, data any, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	response.JSON(w, status, data)
}

// fail writes the given service error with the status code that matches it.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		response.Fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrConflict):
		response.Fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrTooLarge):
		response.Fail(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, domain.ErrValidation) && r.Method == http.MethodPut:
		response.Fail(w, http.StatusUnsupportedMediaType, err.Error())
	case errors.Is(err, domain.ErrValidation):
		response.Fail(w, http.StatusUnprocessableEntity, err.Error())
	default:
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		response.Fail(w, http.StatusInternalServerError, "something went wrong; check the server logs")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		response.Fail(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func meetingURL(id uuid.UUID) string { return "/v1/meetings/" + id.String() }

func clamp(v *int, def, lo, hi int) int {
	if v == nil {
		return def
	}
	return min(max(*v, lo), hi)
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
