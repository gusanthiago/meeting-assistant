package summary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"meeting-assistant/backend/internal/domain"
)

const systemPrompt = `You summarize meeting transcripts for the people who attended.
Write in the transcript's language. Be concrete and short.
- overview: 2-3 sentences on what the meeting was about and its outcome.
- takeaways: the most important points, at most 5.
- decisions: what was decided, at most 5.
- questions: questions left open, at most 5.
- action_items: tasks someone committed to; owner is the person's name as said in the meeting, due as said ("Friday"), empty strings when unknown.
A line labelled "Speaker" has an unknown speaker; it is not a name. Use a participant's name only when the transcript makes it clear.
Only use what the transcript says.`

var outputSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []string{"overview", "takeaways", "decisions", "questions", "action_items"},
	"properties": map[string]any{
		"overview":  map[string]any{"type": "string"},
		"takeaways": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"decisions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"questions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"action_items": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"text", "owner", "due"},
				"properties": map[string]any{
					"text":  map[string]any{"type": "string"},
					"owner": map[string]any{"type": "string"},
					"due":   map[string]any{"type": "string"},
				},
			},
		},
	},
}

type Claude struct {
	client anthropic.Client
	model  string
	effort string
}

func NewClaude(apiKey, model, effort string) *Claude {
	// River retries failed jobs with backoff, so the SDK doesn't retry on its own.
	return &Claude{client: anthropic.NewClient(option.WithAPIKey(apiKey), option.WithMaxRetries(0)), model: model, effort: effort}
}

// Haiku has no server-side fallback and rejects the fallbacks field.
func hasServerFallback(model string) bool {
	return !strings.HasPrefix(model, "claude-haiku")
}

func (c *Claude) Summarize(ctx context.Context, in Input) (domain.SummaryResult, error) {
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(transcriptPrompt(in)))},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffort(c.effort),
			Format: anthropic.BetaJSONOutputFormatParam{Schema: outputSchema},
		},
	}
	if hasServerFallback(c.model) {
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	resp, err := c.client.Beta.Messages.New(ctx, params)
	if err != nil {
		return domain.SummaryResult{}, classify(err)
	}
	switch resp.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return domain.SummaryResult{}, domain.Permanent(domain.WithMessage("Claude declined to summarize this transcript.", errors.New("stop reason: refusal")))
	case anthropic.BetaStopReasonMaxTokens:
		return domain.SummaryResult{}, domain.Permanent(domain.WithMessage("Claude's summary was cut off.", errors.New("stop reason: max_tokens")))
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if b, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	var out domain.SummaryResult
	if err := json.Unmarshal([]byte(text.String()), &out); err != nil {
		return domain.SummaryResult{}, domain.WithMessage("Claude's reply couldn't be read.", fmt.Errorf("parse claude output: %w", err))
	}
	out.Provider = "claude"
	out.Model = string(resp.Model)
	out.InputTokens = resp.Usage.InputTokens
	out.OutputTokens = resp.Usage.OutputTokens
	return out, nil
}

func transcriptPrompt(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Meeting: %s\nParticipants: %s\n\nTranscript:\n", in.Title, strings.Join(in.Participants, ", "))
	for _, s := range in.Segments {
		fmt.Fprintf(&b, "[%02d:%02d] %s: %s\n", s.StartMs/60000, (s.StartMs/1000)%60, s.Speaker, s.Text)
	}
	return b.String()
}

func classify(err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return domain.WithMessage("Claude couldn't be reached.", err)
	}
	return classifyStatus(apiErr.StatusCode, apiErr.Error(), err)
}

func classifyStatus(status int, body string, err error) error {
	switch {
	case status == 429:
		return domain.WithMessage("Claude is rate limiting requests.", err)
	case status >= 500 || status == 408 || status == 409:
		return domain.WithMessage("Claude is unavailable right now.", err)
	case status == 401 || status == 403:
		return domain.Permanent(domain.WithMessage("Claude rejected the API key.", err))
	case strings.Contains(body, "credit balance"):
		return domain.Permanent(domain.WithMessage("The Anthropic account has no credits.", err))
	default:
		return domain.Permanent(domain.WithMessage("Claude rejected the request.", err))
	}
}
