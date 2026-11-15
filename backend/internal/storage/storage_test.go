package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"meeting-assistant/backend/internal/domain"
)

func TestRecordingKey(t *testing.T) {
	tests := []struct {
		contentType string
		ext         string
	}{
		{"audio/webm;codecs=opus", ".webm"},
		{"audio/wav", ".wav"},
		{"audio/mpeg", ".mp3"},
		{"audio/flac", ""},
	}
	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			key := RecordingKey("ws", "m1", tt.contentType)
			if !strings.HasPrefix(key, MeetingPrefix("ws", "m1")+"recordings/") || !strings.HasSuffix(key, tt.ext) {
				t.Errorf("key = %q", key)
			}
			if again := RecordingKey("ws", "m1", tt.contentType); again == key {
				t.Error("two uploads got the same key")
			}
		})
	}
}

func TestMemoryDeletePrefix(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	keys := []string{MeetingPrefix("ws", "m1") + "recording.webm", RecordingKey("ws", "m1", "audio/wav"), RecordingKey("ws", "m2", "audio/wav")}
	for _, k := range keys {
		if err := m.Put(ctx, k, strings.NewReader("x"), 1, "audio/wav"); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.DeletePrefix(ctx, MeetingPrefix("ws", "m1")); err != nil {
		t.Fatal(err)
	}
	for i, k := range keys {
		_, err := m.Get(ctx, k, "")
		if gone := errors.Is(err, domain.ErrNotFound); gone != (i < 2) {
			t.Errorf("%s gone = %v", k, gone)
		}
	}
}
