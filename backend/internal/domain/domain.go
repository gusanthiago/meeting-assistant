package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var DefaultWorkspaceID = uuid.MustParse("01900000-0000-7000-8000-000000000001")

type Status string

const (
	StatusRecording    Status = "recording"
	StatusQueued       Status = "queued"
	StatusTranscribing Status = "transcribing"
	StatusSummarizing  Status = "summarizing"
	StatusReady        Status = "ready"
	StatusFailed       Status = "failed"
)

func CanAcceptUpload(s Status) bool {
	return s == StatusRecording || s == StatusReady || s == StatusFailed
}

// CanRegenerateSummary allows only ready meetings: a failed one has no transcript, so it is retried instead.
func CanRegenerateSummary(s Status) bool {
	return s == StatusReady
}

func CanRetry(s Status) bool {
	return s == StatusFailed
}

func IsFinal(s Status) bool {
	return s == StatusReady || s == StatusFailed
}

// UnknownSpeaker labels transcript lines when the provider can't tell who spoke.
const UnknownSpeaker = "Speaker"

type Segment struct {
	Speaker string `json:"speaker"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

type ActionItemDraft struct {
	Text  string `json:"text"`
	Owner string `json:"owner"`
	Due   string `json:"due"`
}

type SummaryResult struct {
	Overview     string            `json:"overview"`
	Takeaways    []string          `json:"takeaways"`
	Decisions    []string          `json:"decisions"`
	Questions    []string          `json:"questions"`
	ActionItems  []ActionItemDraft `json:"action_items"`
	Provider     string            `json:"-"`
	Model        string            `json:"-"`
	InputTokens  int64             `json:"-"`
	OutputTokens int64             `json:"-"`
}

var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrValidation = errors.New("invalid request")
	ErrTooLarge   = errors.New("too large")
)

// PermanentError marks failures that retrying cannot fix (bad input, 4xx from a provider).
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

func Permanent(err error) error { return &PermanentError{Err: err} }

func IsPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}

// UserError pairs a cause with a short message that is safe to show people; the cause goes to the logs.
type UserError struct {
	Msg string
	Err error
}

func (e *UserError) Error() string { return e.Msg + ": " + e.Err.Error() }
func (e *UserError) Unwrap() error { return e.Err }

func WithMessage(msg string, err error) error { return &UserError{Msg: msg, Err: err} }

// Message returns the message people should see for err, or fallback when it carries none.
func Message(err error, fallback string) string {
	var u *UserError
	if errors.As(err, &u) {
		return u.Msg
	}
	return fallback
}

// RetryAfterError asks the job runner to wait before the next attempt (e.g. a 429 with Retry-After).
type RetryAfterError struct {
	Err   error
	After time.Duration
}

func (e *RetryAfterError) Error() string { return fmt.Sprintf("%v (retry after %s)", e.Err, e.After) }
func (e *RetryAfterError) Unwrap() error { return e.Err }
