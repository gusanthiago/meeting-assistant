package transcription

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"meeting-assistant/backend/internal/domain"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		retryAfter string
		wantErr    bool
		permanent  bool
		wait       time.Duration
	}{
		{"ok", http.StatusOK, "", false, false, 0},
		{"rate limited", http.StatusTooManyRequests, "7", true, false, 7 * time.Second},
		{"rate limited without header", http.StatusTooManyRequests, "", true, false, 5 * time.Second},
		{"unavailable", http.StatusServiceUnavailable, "", true, false, 0},
		{"timeout", http.StatusRequestTimeout, "", true, false, 0},
		{"bad request", http.StatusBadRequest, "", true, true, 0},
		{"unauthorized", http.StatusUnauthorized, "", true, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("body"))}
			if tt.retryAfter != "" {
				resp.Header.Set("Retry-After", tt.retryAfter)
			}
			err := classify(resp)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				return
			}
			if got := domain.IsPermanent(err); got != tt.permanent {
				t.Errorf("permanent = %v, want %v", got, tt.permanent)
			}
			var ra *domain.RetryAfterError
			if tt.wait > 0 && (!errors.As(err, &ra) || ra.After != tt.wait) {
				t.Errorf("retry after = %v, want %v", ra, tt.wait)
			}
		})
	}
}

func TestFakeIsDeterministicAndScaled(t *testing.T) {
	tests := []struct {
		name     string
		audio    string
		duration int64
	}{
		{"short", "a", 6000},
		{"thirty minutes", "b", 30 * 60 * 1000},
		{"no hint", "c", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r1, _ := Fake{}.Transcribe(context.Background(), Request{Audio: strings.NewReader(tt.audio), DurationHintMs: tt.duration})
			r2, _ := Fake{}.Transcribe(context.Background(), Request{Audio: strings.NewReader(tt.audio), DurationHintMs: tt.duration})
			if r1.Segments[0].Text != r2.Segments[0].Text {
				t.Errorf("not deterministic")
			}
			last := r1.Segments[len(r1.Segments)-1]
			if last.EndMs != r1.DurationMs {
				t.Errorf("last segment ends at %d, want %d", last.EndMs, r1.DurationMs)
			}
		})
	}
}

func TestLabelSpeakers(t *testing.T) {
	segs := func(speakers ...string) []domain.Segment {
		out := make([]domain.Segment, len(speakers))
		for i, s := range speakers {
			out[i] = domain.Segment{Speaker: s, Text: "hi"}
		}
		return out
	}
	tests := []struct {
		name         string
		in           []domain.Segment
		participants []string
		want         []string
	}{
		{"one participant names unknown lines", segs(domain.UnknownSpeaker, domain.UnknownSpeaker), []string{"Ana"}, []string{"Ana", "Ana"}},
		{"known speakers stay", segs("Speaker 1", domain.UnknownSpeaker), []string{"Ana"}, []string{"Speaker 1", "Ana"}},
		{"several participants stay unknown", segs(domain.UnknownSpeaker), []string{"Ana", "Bruno"}, []string{domain.UnknownSpeaker}},
		{"no participants stay unknown", segs(domain.UnknownSpeaker), nil, []string{domain.UnknownSpeaker}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LabelSpeakers(tt.in, tt.participants)
			for i, s := range got {
				if s.Speaker != tt.want[i] {
					t.Errorf("segment %d speaker = %q, want %q", i, s.Speaker, tt.want[i])
				}
			}
		})
	}
}
