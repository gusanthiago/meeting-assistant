package api

import (
	"encoding/json"

	"meeting-assistant/backend/api/gen"
	"meeting-assistant/backend/internal/meeting"
	"meeting-assistant/backend/internal/search"
	"meeting-assistant/backend/internal/store/db"
)

// meetingDetail matches the contract's MeetingDetail: the embedded Meeting's fields are promoted in the JSON.
type meetingDetail struct {
	gen.Meeting
	Summary     *gen.Summary     `json:"summary,omitempty"`
	ActionItems []gen.ActionItem `json:"action_items"`
}

func toMeeting(m db.Meeting) gen.Meeting {
	participants := m.Participants
	if participants == nil {
		participants = []string{}
	}
	return gen.Meeting{
		Id:           m.ID,
		Title:        m.Title,
		Participants: participants,
		Status:       gen.MeetingStatus(m.Status),
		Error:        m.Error,
		SummaryError: m.SummaryError,
		DurationMs:   m.DurationMs,
		HasRecording: m.AudioKey != nil,
		StartedAt:    m.StartedAt,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func toDetail(d meeting.Detail) meetingDetail {
	detail := meetingDetail{Meeting: toMeeting(d.Meeting), ActionItems: mapAll(d.ActionItems, toActionItem)}
	if s := d.Summary; s != nil {
		detail.Summary = &gen.Summary{
			Overview:     s.Overview,
			Takeaways:    stringList(s.Takeaways),
			Decisions:    stringList(s.Decisions),
			Questions:    stringList(s.Questions),
			Provider:     s.Provider,
			Model:        s.Model,
			InputTokens:  s.InputTokens,
			OutputTokens: s.OutputTokens,
			CreatedAt:    s.CreatedAt,
		}
	}
	return detail
}

func toActionItem(a db.ActionItem) gen.ActionItem {
	return gen.ActionItem{Id: a.ID, Text: a.Text, Owner: a.Owner, Due: a.Due, Done: a.Done}
}

func toHit(h search.Hit) gen.SearchHit {
	return gen.SearchHit{MeetingId: h.MeetingID, Title: h.Title, StartedAt: h.StartedAt, Snippets: mapAll(h.Snippets, toSnippet)}
}

func toSnippet(s search.Snippet) gen.Snippet {
	snippet := gen.Snippet{
		Field:   gen.SnippetField(s.Field),
		StartMs: s.StartMs,
		Parts:   mapAll(s.Parts, func(p search.Part) gen.SnippetPart { return gen.SnippetPart{Text: p.Text, Match: p.Match} }),
	}
	if s.Speaker != "" {
		snippet.Speaker = &s.Speaker
	}
	return snippet
}

// mapAll converts every element with f into a slice of the same length.
func mapAll[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

func stringList(raw []byte) []string {
	out := []string{}
	_ = json.Unmarshal(raw, &out)
	return out
}
