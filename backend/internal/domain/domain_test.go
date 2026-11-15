package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestStatusRules(t *testing.T) {
	tests := []struct {
		status     Status
		upload     bool
		regenerate bool
		retry      bool
		final      bool
	}{
		{StatusRecording, true, false, false, false},
		{StatusQueued, false, false, false, false},
		{StatusTranscribing, false, false, false, false},
		{StatusSummarizing, false, false, false, false},
		{StatusReady, true, true, false, true},
		{StatusFailed, true, false, true, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := CanAcceptUpload(tt.status); got != tt.upload {
				t.Errorf("CanAcceptUpload = %v, want %v", got, tt.upload)
			}
			if got := CanRegenerateSummary(tt.status); got != tt.regenerate {
				t.Errorf("CanRegenerateSummary = %v, want %v", got, tt.regenerate)
			}
			if got := CanRetry(tt.status); got != tt.retry {
				t.Errorf("CanRetry = %v, want %v", got, tt.retry)
			}
			if got := IsFinal(tt.status); got != tt.final {
				t.Errorf("IsFinal = %v, want %v", got, tt.final)
			}
		})
	}
}

func TestIsPermanent(t *testing.T) {
	base := errors.New("boom")
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"plain", base, false},
		{"permanent", Permanent(base), true},
		{"wrapped permanent", fmt.Errorf("ctx: %w", Permanent(base)), true},
		{"retry after", &RetryAfterError{Err: base}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPermanent(tt.err); got != tt.want {
				t.Errorf("IsPermanent = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage(t *testing.T) {
	base := errors.New("POST https://api.example: 400 {raw body}")
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"no message uses fallback", base, "fallback"},
		{"with message", WithMessage("No credits.", base), "No credits."},
		{"inside permanent", Permanent(WithMessage("No credits.", base)), "No credits."},
		{"inside retry after", &RetryAfterError{Err: WithMessage("Busy.", base)}, "Busy."},
		{"wrapped again", fmt.Errorf("summarize: %w", WithMessage("Busy.", base)), "Busy."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Message(tt.err, "fallback"); got != tt.want {
				t.Errorf("Message = %q, want %q", got, tt.want)
			}
		})
	}
}
