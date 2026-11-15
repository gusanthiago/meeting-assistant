package transcription

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strconv"
	"time"

	"meeting-assistant/backend/internal/domain"
)

type Request struct {
	Audio          io.Reader
	ContentType    string
	DurationHintMs int64
}

type Result struct {
	Language   string           `json:"language"`
	DurationMs int64            `json:"duration_ms"`
	Segments   []domain.Segment `json:"segments"`
}

// LabelSpeakers names unknown speakers when the meeting has exactly one participant.
func LabelSpeakers(segments []domain.Segment, participants []string) []domain.Segment {
	if len(participants) != 1 {
		return segments
	}
	for i := range segments {
		if segments[i].Speaker == domain.UnknownSpeaker {
			segments[i].Speaker = participants[0]
		}
	}
	return segments
}

type Provider interface {
	Transcribe(ctx context.Context, req Request) (Result, error)
}

type Fake struct{}

var fakeLines = []struct{ speaker, text string }{
	{"Speaker 1", "Thanks everyone for joining, let's go through the updates."},
	{"Speaker 2", "The release candidate is ready and QA found two minor issues."},
	{"Speaker 1", "We decided to ship on Thursday if the fixes land today."},
	{"Speaker 2", "I will send the release notes to the team by Wednesday."},
	{"Speaker 3", "Do we need a migration window for the database change?"},
	{"Speaker 1", "Speaker 3 will check the migration plan with infra by tomorrow."},
}

func (Fake) Transcribe(_ context.Context, req Request) (Result, error) {
	h := fnv.New32a()
	if _, err := io.Copy(h, req.Audio); err != nil {
		return Result{}, err
	}
	duration := req.DurationHintMs
	if duration <= 0 {
		duration = int64(len(fakeLines)) * 6000
	}
	offset := int(h.Sum32() % uint32(len(fakeLines)))
	step := duration / int64(len(fakeLines))
	segs := make([]domain.Segment, len(fakeLines))
	for i := range fakeLines {
		l := fakeLines[(i+offset)%len(fakeLines)]
		segs[i] = domain.Segment{Speaker: l.speaker, StartMs: int64(i) * step, EndMs: int64(i+1) * step, Text: l.text}
	}
	return Result{Language: "en", DurationMs: duration, Segments: segs}, nil
}

func classify(resp *http.Response) error {
	if resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	err := fmt.Errorf("speech-to-text returned %d: %s", resp.StatusCode, body)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &domain.RetryAfterError{Err: domain.WithMessage("The transcription service is busy.", err), After: retryAfter(resp.Header.Get("Retry-After"))}
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
		return domain.WithMessage("The transcription service had an error.", err)
	default:
		return domain.Permanent(domain.WithMessage("The recording couldn't be transcribed. Try another file.", err))
	}
}

func retryAfter(v string) time.Duration {
	if s, err := strconv.Atoi(v); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 5 * time.Second
}
