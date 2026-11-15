package transcription

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"meeting-assistant/backend/internal/domain"
)

func TestWhisper(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantErr      bool
		permanent    bool
		wantSegments int
		wantDuration int64
		wantLanguage string
	}{
		{"maps segments", 200, `{"language":"pt","segments":[{"start":0,"end":2.5,"text":" Olá a todos. "},{"start":2.5,"end":5.12,"text":"Bruno vai enviar as notas."}]}`, false, false, 2, 5120, "pt"},
		{"drops blank segments", 200, `{"language":"en","segments":[{"start":0,"end":1,"text":"  "},{"start":1,"end":3,"text":"Hi"}]}`, false, false, 1, 3000, "en"},
		{"silence", 200, `{"language":"en","segments":[]}`, false, false, 0, 0, "en"},
		{"server error is retried", 500, `oops`, true, false, 0, 0, ""},
		{"bad audio is permanent", 422, `{"detail":"invalid"}`, true, true, 0, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("output") != "json" || r.URL.Query().Get("encode") != "true" {
					t.Errorf("unexpected query %q", r.URL.RawQuery)
				}
				f, _, err := r.FormFile("audio_file")
				if err != nil {
					t.Fatalf("no audio_file: %v", err)
				}
				if b, _ := io.ReadAll(f); string(b) != "voice" {
					t.Errorf("audio = %q", b)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			res, err := (&Whisper{BaseURL: srv.URL, Client: srv.Client()}).Transcribe(context.Background(), Request{Audio: strings.NewReader("voice"), ContentType: "audio/webm"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				if domain.IsPermanent(err) != tt.permanent {
					t.Errorf("permanent = %v, want %v", domain.IsPermanent(err), tt.permanent)
				}
				return
			}
			if len(res.Segments) != tt.wantSegments || res.DurationMs != tt.wantDuration || res.Language != tt.wantLanguage {
				t.Errorf("got %d segments, %d ms, %q", len(res.Segments), res.DurationMs, res.Language)
			}
			if tt.wantSegments > 0 && res.Segments[0].Text != strings.TrimSpace(res.Segments[0].Text) {
				t.Errorf("text not trimmed: %q", res.Segments[0].Text)
			}
		})
	}
}
