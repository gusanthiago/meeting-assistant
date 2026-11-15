//go:build integration

package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/storage"
	"meeting-assistant/backend/internal/store/db"
	"meeting-assistant/backend/internal/store/storetest"
)

type spyIndex struct{ deleted []uuid.UUID }

func (s *spyIndex) Delete(_ context.Context, id uuid.UUID) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func newService(t *testing.T) (context.Context, *Service, *spyIndex) {
	t.Helper()
	ctx := context.Background()
	pool := storetest.NewPool(t)
	objects := storage.NewMemory()
	if err := objects.Put(ctx, "audio", strings.NewReader("voice"), 5, "audio/wav"); err != nil {
		t.Fatal(err)
	}
	idx := &spyIndex{}
	return ctx, &Service{Pool: pool, Storage: objects, Search: idx, Jobs: storetest.NewInserter(t, pool), WorkspaceID: domain.DefaultWorkspaceID, MaxUploadBytes: 1 << 20}, idx
}

func saveTranscript(t *testing.T, ctx context.Context, s *Service, m db.Meeting, segs ...domain.Segment) {
	t.Helper()
	if segs == nil {
		segs = []domain.Segment{{Speaker: "Ana", Text: "Hi"}}
	}
	b, _ := json.Marshal(segs)
	if err := db.New(s.Pool).UpsertTranscript(ctx, db.UpsertTranscriptParams{MeetingID: m.ID, WorkspaceID: m.WorkspaceID, Language: "en", Segments: b}); err != nil {
		t.Fatal(err)
	}
}

func TestQueueAgain(t *testing.T) {
	tests := []struct {
		name       string
		action     string
		status     domain.Status
		audio      bool
		transcript string
		wantStatus domain.Status
		wantJobs   []string
		wantErr    error
	}{
		{"retry a failed meeting", "retry", domain.StatusFailed, true, "", domain.StatusQueued, []string{"transcribe"}, nil},
		{"retry needs a recording", "retry", domain.StatusFailed, false, "", "", nil, domain.ErrConflict},
		{"retry only failed meetings", "retry", domain.StatusReady, true, "speech", "", nil, domain.ErrConflict},
		{"regenerate a ready meeting", "regenerate", domain.StatusReady, true, "speech", domain.StatusSummarizing, []string{"summarize"}, nil},
		{"regenerate needs a transcript", "regenerate", domain.StatusReady, true, "", "", nil, domain.ErrConflict},
		{"regenerate waits for processing", "regenerate", domain.StatusTranscribing, true, "speech", "", nil, domain.ErrConflict},
		{"regenerate needs speech", "regenerate", domain.StatusReady, true, "silence", "", nil, domain.ErrConflict},
		{"failed meetings are retried, not regenerated", "regenerate", domain.StatusFailed, true, "speech", "", nil, domain.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, s, _ := newService(t)
			audio := ""
			if tt.audio {
				audio = "audio"
			}
			m := storetest.Meeting(t, s.Pool, tt.status, audio)
			switch tt.transcript {
			case "speech":
				saveTranscript(t, ctx, s, m)
			case "silence":
				saveTranscript(t, ctx, s, m, []domain.Segment{}...)
			}
			if tt.status == domain.StatusFailed {
				msg := "Transcription failed."
				if _, err := db.New(s.Pool).SetMeetingStatus(ctx, db.SetMeetingStatusParams{ID: m.ID, Status: string(tt.status), Error: &msg}); err != nil {
					t.Fatal(err)
				}
			}

			queue := s.RegenerateSummary
			if tt.action == "retry" {
				queue = s.Retry
			}
			got, err := queue(ctx, m.ID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Status != string(tt.wantStatus) || got.Error != nil {
				t.Errorf("status = %s, error = %v", got.Status, got.Error)
			}
			if kinds := storetest.JobKinds(t, s.Pool, m.ID); !slices.Equal(kinds, tt.wantJobs) {
				t.Errorf("jobs = %v, want %v", kinds, tt.wantJobs)
			}
		})
	}
}

func TestUploadRecordingReplacesTheOldOne(t *testing.T) {
	ctx, s, idx := newService(t)
	m := storetest.Meeting(t, s.Pool, domain.StatusReady, "")
	old := storage.MeetingPrefix(m.WorkspaceID.String(), m.ID.String()) + "recording.webm"
	if err := s.Storage.Put(ctx, old, strings.NewReader("old"), 3, "audio/webm"); err != nil {
		t.Fatal(err)
	}
	ct, size := "audio/webm", int64(3)
	if _, err := db.New(s.Pool).SetMeetingRecording(ctx, db.SetMeetingRecordingParams{ID: m.ID, AudioKey: &old, AudioContentType: &ct, AudioSize: &size, Status: string(domain.StatusReady)}); err != nil {
		t.Fatal(err)
	}
	saveTranscript(t, ctx, s, m)

	got, err := s.UploadRecording(ctx, m.ID, strings.NewReader("new voice"), "audio/wav", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != string(domain.StatusQueued) || got.AudioKey == nil || *got.AudioKey == old {
		t.Fatalf("status = %s, key = %v", got.Status, got.AudioKey)
	}
	if _, err := s.Storage.Get(ctx, old, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("replaced recording still stored: %v", err)
	}
	if _, err := s.Storage.Get(ctx, *got.AudioKey, ""); err != nil {
		t.Errorf("new recording missing: %v", err)
	}
	if _, err := s.Transcript(ctx, m.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old transcript kept: %v", err)
	}
	if !slices.Equal(idx.deleted, []uuid.UUID{m.ID}) {
		t.Errorf("search deletes = %v, want the stale entry removed", idx.deleted)
	}
}

func TestDelete(t *testing.T) {
	ctx, s, idx := newService(t)
	m := storetest.Meeting(t, s.Pool, domain.StatusReady, "")
	prefix := storage.MeetingPrefix(m.WorkspaceID.String(), m.ID.String())
	keys := []string{prefix + "recording.webm", storage.RecordingKey(m.WorkspaceID.String(), m.ID.String(), "audio/wav")}
	for _, k := range keys {
		if err := s.Storage.Put(ctx, k, strings.NewReader("voice"), 5, "audio/wav"); err != nil {
			t.Fatal(err)
		}
	}
	saveTranscript(t, ctx, s, m)
	if err := db.New(s.Pool).InsertActionItem(ctx, db.InsertActionItemParams{MeetingID: m.ID, WorkspaceID: m.WorkspaceID, Text: "Send notes"}); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, m.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("meeting still readable: %v", err)
	}
	if items, _ := db.New(s.Pool).ListActionItems(ctx, m.ID); len(items) != 0 {
		t.Errorf("action items left: %d", len(items))
	}
	for _, k := range keys {
		if _, err := s.Storage.Get(ctx, k, ""); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s still stored: %v", k, err)
		}
	}
	if _, err := s.Storage.Get(ctx, "audio", ""); err != nil {
		t.Errorf("another meeting's recording was removed: %v", err)
	}
	if !slices.Equal(idx.deleted, []uuid.UUID{m.ID}) {
		t.Errorf("search deletes = %v", idx.deleted)
	}
	if err := s.Delete(ctx, m.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete err = %v, want not found", err)
	}
}
