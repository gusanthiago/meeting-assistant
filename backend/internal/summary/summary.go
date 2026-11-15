package summary

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"meeting-assistant/backend/internal/domain"
)

type Input struct {
	Title        string
	Participants []string
	DurationMs   int64
	Segments     []domain.Segment
}

type Provider interface {
	Summarize(ctx context.Context, in Input) (domain.SummaryResult, error)
}

type Fake struct{}

var (
	sentenceSplit = regexp.MustCompile(`[^.!?]+[.!?]?`)
	// A commitment anywhere in the sentence: a pronoun or a capitalised name, then a modal ("I'll", "we need to").
	actionRe   = regexp.MustCompile(`\b((?i:i|we|you|he|she|they)|[A-Z][a-z]+(?: [0-9]+)?)\s*(?i:will|['’]ll|['’]m going to|am going to|is going to|are going to|['’]re going to|need to|needs to|have to|has to|['’]m planning to|am planning to|plan to|plans to)\s+(.+)`)
	dueRe      = regexp.MustCompile(`(?i)^(.+?)(?:\s+by\s+(.+)|\s+(tomorrow|today|tonight|this week|next week|next month))$`)
	decisionRe = regexp.MustCompile(`(?i)\b(decided|agreed|let's go with|we'll go with)\b`)
	takeawayRe = regexp.MustCompile(`(?i)\b(important|priority|risk|blocked|blocker|deadline|customer|launch|release)\b`)
)

func (Fake) Summarize(_ context.Context, in Input) (domain.SummaryResult, error) {
	res := domain.SummaryResult{Provider: "fake", Takeaways: []string{}, Decisions: []string{}, Questions: []string{}, ActionItems: []domain.ActionItemDraft{}}
	seen := map[string]bool{}
	for _, turn := range turns(in.Segments) {
		for _, s := range sentences(turn.Text) {
			if seen[s] {
				continue
			}
			seen[s] = true
			action, isAction := actionFrom(s, turn.Speaker)
			switch {
			case strings.HasSuffix(s, "?"):
				res.Questions = appendMax(res.Questions, s, 5)
			case isAction:
				res.ActionItems = append(res.ActionItems, action)
			case decisionRe.MatchString(s):
				res.Decisions = appendMax(res.Decisions, s, 5)
			case takeawayRe.MatchString(s):
				res.Takeaways = appendMax(res.Takeaways, s, 5)
			}
		}
	}
	res.Overview = overview(in, len(res.ActionItems), len(res.Decisions))
	return res, nil
}

func sentences(text string) []string {
	var out []string
	for _, s := range sentenceSplit.FindAllString(text, -1) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// turns joins consecutive segments of one speaker, since speech-to-text often splits a sentence across segments.
func turns(segments []domain.Segment) []domain.Segment {
	var out []domain.Segment
	for _, s := range segments {
		if n := len(out); n > 0 && out[n-1].Speaker == s.Speaker {
			out[n-1].Text += " " + s.Text
			continue
		}
		out = append(out, s)
	}
	return out
}

var notOwners = map[string]bool{"it": true, "this": true, "that": true, "there": true, "these": true, "those": true, "which": true, "what": true, "who": true, "everything": true, "nothing": true, "something": true}

// actionFrom returns the first commitment in s, skipping matches whose subject is a thing ("It will rain, I will…").
func actionFrom(s, speaker string) (domain.ActionItemDraft, bool) {
	for {
		m := actionRe.FindStringSubmatchIndex(s)
		if m == nil {
			return domain.ActionItemDraft{}, false
		}
		subject, task := s[m[2]:m[3]], s[m[4]:m[5]]
		if !notOwners[strings.ToLower(subject)] {
			return draft(subject, task, speaker), true
		}
		s = task
	}
}

func draft(subject, task, speaker string) domain.ActionItemDraft {
	owner := subject
	switch strings.ToLower(owner) {
	case "i":
		owner = speaker
		if owner == domain.UnknownSpeaker {
			owner = ""
		}
	case "we":
		owner = "Team"
	case "you", "he", "she", "they":
		owner = ""
	}
	text, due := strings.TrimRight(strings.TrimSpace(task), ".!"), ""
	if d := dueRe.FindStringSubmatch(text); d != nil {
		text, due = d[1], d[2]+d[3]
	}
	return domain.ActionItemDraft{Text: upperFirst(text), Owner: owner, Due: due}
}

func overview(in Input, actions, decisions int) string {
	speakers := map[string]bool{}
	for _, s := range in.Segments {
		speakers[s.Speaker] = true
	}
	people := len(in.Participants)
	if people == 0 {
		people = len(speakers)
	}
	minutes := in.DurationMs / 60000
	if minutes == 0 && len(in.Segments) > 0 {
		minutes = in.Segments[len(in.Segments)-1].EndMs / 60000
	}
	length := "under a minute"
	if minutes > 0 {
		length = "about " + plural(int(minutes), "minute")
	}
	return fmt.Sprintf("%s: %s talked for %s. %s and %s were captured.",
		in.Title, plural(people, "participant"), length, plural(actions, "action item"), plural(decisions, "decision"))
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func appendMax(list []string, s string, max int) []string {
	if len(list) >= max {
		return list
	}
	return append(list, s)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
