package config

import (
	"strings"
	"testing"
)

func TestLoadProviders(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantSummary string
		wantErr     string
	}{
		{"defaults", nil, "fake", ""},
		{"claude with a key", map[string]string{"SUMMARY_PROVIDER": "Claude", "ANTHROPIC_API_KEY": "k"}, "claude", ""},
		{"claude without a key falls back", map[string]string{"SUMMARY_PROVIDER": "claude"}, "fake", ""},
		{"unknown summary provider", map[string]string{"SUMMARY_PROVIDER": "gpt"}, "", "SUMMARY_PROVIDER must be one of claude, fake"},
		{"unknown transcription provider", map[string]string{"TRANSCRIPTION_PROVIDER": "deepgram"}, "", "TRANSCRIPTION_PROVIDER must be one of"},
		{"unknown effort", map[string]string{"ANTHROPIC_EFFORT": "fast"}, "", "ANTHROPIC_EFFORT must be one of"},
		{"missing database", map[string]string{"DATABASE_URL": ""}, "", "DATABASE_URL is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := map[string]string{"DATABASE_URL": "postgres://x", "SUMMARY_PROVIDER": "", "TRANSCRIPTION_PROVIDER": "", "ANTHROPIC_API_KEY": "", "ANTHROPIC_EFFORT": ""}
			for k, v := range tt.env {
				base[k] = v
			}
			for k, v := range base {
				t.Setenv(k, v)
			}
			c, err := Load()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.SummaryProvider != tt.wantSummary {
				t.Errorf("summary provider = %q, want %q", c.SummaryProvider, tt.wantSummary)
			}
		})
	}
}
