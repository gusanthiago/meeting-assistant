package summary

import (
	"context"
	"errors"
	"testing"

	"meeting-assistant/backend/internal/domain"
)

func TestFakeActionItems(t *testing.T) {
	tests := []struct {
		name    string
		speaker string
		text    []string
		want    *domain.ActionItemDraft
	}{
		{"named owner with due", "Ana", []string{"Bruno will send the report by Friday."}, &domain.ActionItemDraft{Text: "Send the report", Owner: "Bruno", Due: "Friday"}},
		{"first person uses speaker", "Ana", []string{"I will update the roadmap."}, &domain.ActionItemDraft{Text: "Update the roadmap", Owner: "Ana"}},
		{"we becomes team", "Ana", []string{"We are going to review the contract by next week."}, &domain.ActionItemDraft{Text: "Review the contract", Owner: "Team", Due: "next week"}},
		{"contraction", "Ana", []string{"I'll send the notes by Friday."}, &domain.ActionItemDraft{Text: "Send the notes", Owner: "Ana", Due: "Friday"}},
		{"curly apostrophe", "Ana", []string{"We’re going to fix the build today."}, &domain.ActionItemDraft{Text: "Fix the build", Owner: "Team", Due: "today"}},
		{"planning with due word", "Ana", []string{"I'm planning to review that tomorrow"}, &domain.ActionItemDraft{Text: "Review that", Owner: "Ana", Due: "tomorrow"}},
		{"after a lead-in, split across segments", "Ana", []string{"Hello, I'm testing action items, so after this meeting I will", "make a list of things that we need to do after that meeting."}, &domain.ActionItemDraft{Text: "Make a list of things that we need to do after that meeting", Owner: "Ana"}},
		{"unknown speaker has no owner", domain.UnknownSpeaker, []string{"I need to call the vendor."}, &domain.ActionItemDraft{Text: "Call the vendor", Owner: ""}},
		{"thing as subject", "Ana", []string{"It will rain tomorrow."}, nil},
		{"commitment after a thing as subject", "Ana", []string{"It will be fine, I will send it."}, &domain.ActionItemDraft{Text: "Send it", Owner: "Ana"}},
		{"plain statement", "Ana", []string{"The build is green."}, nil},
		{"question is not an action", "Ana", []string{"Will Bruno send the report?"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var segs []domain.Segment
			for _, text := range tt.text {
				segs = append(segs, domain.Segment{Speaker: tt.speaker, Text: text, EndMs: 60000})
			}
			res, err := Fake{}.Summarize(context.Background(), Input{Title: "Sync", Segments: segs})
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == nil {
				if len(res.ActionItems) != 0 {
					t.Fatalf("got %+v, want none", res.ActionItems)
				}
				return
			}
			if len(res.ActionItems) != 1 || res.ActionItems[0] != *tt.want {
				t.Fatalf("got %+v, want %+v", res.ActionItems, *tt.want)
			}
		})
	}
}

func TestFakeClassifiesSentences(t *testing.T) {
	tests := []struct {
		text  string
		field string
	}{
		{"We decided to ship on Thursday.", "decision"},
		{"Do we need a migration window?", "question"},
		{"The main risk is the vendor delay.", "takeaway"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			res, _ := Fake{}.Summarize(context.Background(), Input{Title: "Sync", Segments: []domain.Segment{{Speaker: "A", Text: tt.text}}})
			got := map[string]int{"decision": len(res.Decisions), "question": len(res.Questions), "takeaway": len(res.Takeaways)}
			if got[tt.field] != 1 {
				t.Errorf("%q: got %v, want one %s", tt.text, got, tt.field)
			}
		})
	}
}

func TestHasServerFallback(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"claude-opus-5-5", true},
		{"claude-sonnet-5-5", true},
		{"claude-haiku-5-5", false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := hasServerFallback(tt.model); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyStatus(t *testing.T) {
	cause := errors.New("raw provider error")
	tests := []struct {
		name      string
		status    int
		body      string
		permanent bool
		message   string
	}{
		{"rate limited", 429, "", false, "Claude is rate limiting requests."},
		{"overloaded", 529, "", false, "Claude is unavailable right now."},
		{"server error", 500, "", false, "Claude is unavailable right now."},
		{"bad key", 401, "", true, "Claude rejected the API key."},
		{"no credits", 400, "Your credit balance is too low", true, "The Anthropic account has no credits."},
		{"other bad request", 400, "invalid schema", true, "Claude rejected the request."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyStatus(tt.status, tt.body, cause)
			if domain.IsPermanent(err) != tt.permanent {
				t.Errorf("permanent = %v, want %v", domain.IsPermanent(err), tt.permanent)
			}
			if got := domain.Message(err, ""); got != tt.message {
				t.Errorf("message = %q, want %q", got, tt.message)
			}
			if !errors.Is(err, cause) {
				t.Error("cause was lost")
			}
		})
	}
}
