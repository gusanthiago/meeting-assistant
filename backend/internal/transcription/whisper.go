package transcription

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"meeting-assistant/backend/internal/domain"
)

// Whisper talks to a self-hosted openai-whisper-asr-webservice. It has no speaker separation.
type Whisper struct {
	BaseURL string
	Client  *http.Client
}

func (w *Whisper) Transcribe(ctx context.Context, req Request) (Result, error) {
	pr, pw := io.Pipe()
	defer pr.Close()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("audio_file", "recording")
		if err == nil {
			_, err = io.Copy(part, req.Audio)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()

	u := w.BaseURL + "/asr?encode=true&task=transcribe&output=json&vad_filter=true"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, pr)
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := w.Client.Do(httpReq)
	if err != nil {
		return Result{}, domain.WithMessage("The transcription service couldn't be reached.", fmt.Errorf("whisper request: %w", err))
	}
	defer resp.Body.Close()
	if err := classify(resp); err != nil {
		return Result{}, err
	}

	var raw struct {
		Language string `json:"language"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Result{}, domain.WithMessage("The transcription service sent an unreadable answer.", fmt.Errorf("whisper response: %w", err))
	}
	out := Result{Language: raw.Language, Segments: []domain.Segment{}}
	for _, s := range raw.Segments {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		seg := domain.Segment{Speaker: domain.UnknownSpeaker, StartMs: int64(s.Start * 1000), EndMs: int64(s.End * 1000), Text: text}
		out.Segments = append(out.Segments, seg)
		out.DurationMs = max(out.DurationMs, seg.EndMs)
	}
	return out, nil
}
