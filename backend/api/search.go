package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"meeting-assistant/backend/api/gen"
	"meeting-assistant/backend/api/response"
)

const maxQueryLength = 200

// SearchMeetings searches titles, participants, summaries, action items and transcripts.
func (h *Handler) SearchMeetings(w http.ResponseWriter, r *http.Request, params gen.SearchMeetingsParams) {
	q := strings.TrimSpace(params.Q)
	if q == "" || utf8.RuneCountInString(q) > maxQueryLength {
		response.Fail(w, http.StatusBadRequest, "q must be 1 to 200 characters")
		return
	}

	hits, err := h.search.Search(r.Context(), h.workspaceID, q, clamp(params.Limit, 10, 1, 50))
	h.reply(w, r, http.StatusOK, gen.SearchResults{Items: mapAll(hits, toHit)}, err)
}
