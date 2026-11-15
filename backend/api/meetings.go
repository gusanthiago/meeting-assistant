package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"meeting-assistant/backend/api/gen"
	"meeting-assistant/backend/api/response"
	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/meeting"
)

const (
	maxTitleLength       = 200
	maxParticipants      = 50
	maxParticipantLength = 100
)

// ListMeetings returns a page of meetings, newest first.
func (h *Handler) ListMeetings(w http.ResponseWriter, r *http.Request, params gen.ListMeetingsParams) {
	items, next, err := h.meetings.List(r.Context(), params.Cursor, clamp(params.Limit, 20, 1, 100))
	h.reply(w, r, http.StatusOK, gen.MeetingList{Items: mapAll(items, toMeeting), NextCursor: next}, err)
}

// CreateMeeting creates a meeting; repeating an Idempotency-Key returns the first one.
func (h *Handler) CreateMeeting(w http.ResponseWriter, r *http.Request, params gen.CreateMeetingParams) {
	var body gen.CreateMeetingRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	in := meeting.CreateInput{Title: body.Title, Participants: deref(body.Participants), StartedAt: body.StartedAt, IdempotencyKey: deref(params.IdempotencyKey)}
	if !withinLimits(in) {
		response.Fail(w, http.StatusUnprocessableEntity, "title must be at most 200 characters, with at most 50 participants of 100 characters")
		return
	}

	m, created, err := h.meetings.Create(r.Context(), in)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		w.Header().Set("Location", meetingURL(m.ID))
	}
	h.reply(w, r, status, toMeeting(m), err)
}

// GetMeeting returns a meeting with its summary and action items.
func (h *Handler) GetMeeting(w http.ResponseWriter, r *http.Request, id gen.MeetingId) {
	d, err := h.meetings.Get(r.Context(), id)
	h.reply(w, r, http.StatusOK, toDetail(d), err)
}

// DeleteMeeting deletes a meeting with its recording, results and search entry.
func (h *Handler) DeleteMeeting(w http.ResponseWriter, r *http.Request, id gen.MeetingId) {
	if err := h.meetings.Delete(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RetryMeeting processes a failed meeting again from its stored recording.
func (h *Handler) RetryMeeting(w http.ResponseWriter, r *http.Request, id gen.MeetingId) {
	m, err := h.meetings.Retry(r.Context(), id)
	if err == nil {
		w.Header().Set("Location", meetingURL(m.ID))
	}
	h.reply(w, r, http.StatusAccepted, toMeeting(m), err)
}

// UploadRecording stores the request body as the meeting's audio and queues processing.
func (h *Handler) UploadRecording(w http.ResponseWriter, r *http.Request, id gen.MeetingId, params gen.UploadRecordingParams) {
	m, err := h.meetings.UploadRecording(r.Context(), id, r.Body, r.Header.Get("Content-Type"), params.DurationMs)
	if err == nil {
		w.Header().Set("Location", meetingURL(m.ID))
	}
	h.reply(w, r, http.StatusAccepted, toMeeting(m), err)
}

// GetRecording streams the meeting's audio, honouring Range requests.
func (h *Handler) GetRecording(w http.ResponseWriter, r *http.Request, id gen.MeetingId, params gen.GetRecordingParams) {
	obj, err := h.meetings.OpenRecording(r.Context(), id, deref(params.Range))
	if errors.Is(err, domain.ErrValidation) {
		response.Fail(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	defer obj.Body.Close()

	header := w.Header()
	header.Set("Content-Type", obj.ContentType)
	header.Set("Accept-Ranges", "bytes")
	header.Set("Content-Length", strconv.FormatInt(obj.ContentLength, 10))
	status := http.StatusOK
	if obj.ContentRange != "" {
		header.Set("Content-Range", obj.ContentRange)
		status = http.StatusPartialContent
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, obj.Body)
}

// GetTranscript returns the transcript once transcription has finished.
func (h *Handler) GetTranscript(w http.ResponseWriter, r *http.Request, id gen.MeetingId) {
	t, err := h.meetings.Transcript(r.Context(), id)
	out := gen.Transcript{MeetingId: t.MeetingID, Language: t.Language, Segments: []gen.Segment{}}
	if err == nil {
		err = json.Unmarshal(t.Segments, &out.Segments)
	}
	h.reply(w, r, http.StatusOK, out, err)
}

// RegenerateSummary queues a new summary from the existing transcript.
func (h *Handler) RegenerateSummary(w http.ResponseWriter, r *http.Request, id gen.MeetingId) {
	m, err := h.meetings.RegenerateSummary(r.Context(), id)
	if err == nil {
		w.Header().Set("Location", meetingURL(m.ID))
	}
	h.reply(w, r, http.StatusAccepted, toMeeting(m), err)
}

// UpdateActionItem marks an action item done or not done.
func (h *Handler) UpdateActionItem(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var body gen.UpdateActionItemRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	item, err := h.meetings.SetActionItemDone(r.Context(), id, body.Done)
	h.reply(w, r, http.StatusOK, toActionItem(item), err)
}

// withinLimits counts characters, not bytes, after trimming, so accented names get the full length.
func withinLimits(in meeting.CreateInput) bool {
	if utf8.RuneCountInString(strings.TrimSpace(in.Title)) > maxTitleLength || len(in.Participants) > maxParticipants {
		return false
	}
	for _, p := range in.Participants {
		if utf8.RuneCountInString(strings.TrimSpace(p)) > maxParticipantLength {
			return false
		}
	}
	return true
}
