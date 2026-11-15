//go:build integration

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/search"
	"meeting-assistant/backend/internal/storage"
	"meeting-assistant/backend/internal/store/db"
	"meeting-assistant/backend/internal/store/storetest"
	"meeting-assistant/backend/internal/summary"
	"meeting-assistant/backend/internal/transcription"
)

type fakeTranscriber struct {
	res transcription.Result
	err error
}

func (f fakeTranscriber) Transcribe(_ context.Context, req transcription.Request) (transcription.Result, error) {
	_, _ = io.Copy(io.Discard, req.Audio)
	return f.res, f.err
}

type fakeSummarizer struct {
	res   domain.SummaryResult
	err   error
	calls *int
}

func (f fakeSummarizer) Summarize(context.Context, summary.Input) (domain.SummaryResult, error) {
	*f.calls++
	return f.res, f.err
}

type nopIndexer struct{}

func (nopIndexer) Upsert(context.Context, uuid.UUID, search.Doc) error { return nil }
func (nopIndexer) Delete(context.Context, uuid.UUID) error             { return nil }

// racingIndexer runs race while the document is written, like a request that lands mid-index.
type racingIndexer struct {
	race    func(ctx context.Context, q *db.Queries, id uuid.UUID) error
	q       *db.Queries
	deleted []uuid.UUID
}

func (r *racingIndexer) Upsert(ctx context.Context, id uuid.UUID, _ search.Doc) error {
	return r.race(ctx, r.q, id)
}

func (r *racingIndexer) Delete(_ context.Context, id uuid.UUID) error {
	r.deleted = append(r.deleted, id)
	return nil
}

var spoken = []domain.Segment{{Speaker: domain.UnknownSpeaker, Text: "I will send the notes.", EndMs: 2000}}

func newDeps(t *testing.T) (context.Context, *Deps) {
	t.Helper()
	pool := storetest.NewPool(t)
	ctx := rivertest.WorkContext(context.Background(), storetest.NewInserter(t, pool))
	objects := storage.NewMemory()
	if err := objects.Put(ctx, "audio", strings.NewReader("voice"), 5, "audio/wav"); err != nil {
		t.Fatal(err)
	}
	return ctx, &Deps{Pool: pool, Storage: objects, Indexer: nopIndexer{}, Logger: slog.New(slog.DiscardHandler)}
}

func saveTranscript(t *testing.T, ctx context.Context, d *Deps, m db.Meeting, segs []domain.Segment) {
	t.Helper()
	b, _ := json.Marshal(segs)
	if err := db.New(d.Pool).UpsertTranscript(ctx, db.UpsertTranscriptParams{MeetingID: m.ID, WorkspaceID: m.WorkspaceID, Language: "en", Segments: b}); err != nil {
		t.Fatal(err)
	}
}

func reload(t *testing.T, ctx context.Context, d *Deps, id uuid.UUID) db.Meeting {
	t.Helper()
	m, err := db.New(d.Pool).GetMeetingByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTranscribe(t *testing.T) {
	tests := []struct {
		name         string
		segments     []domain.Segment
		wantSpeakers []string
	}{
		{"speech is saved under the only participant's name", spoken, []string{"Ana"}},
		{"no speech still moves on", []domain.Segment{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, d := newDeps(t)
			d.Transcriber = fakeTranscriber{res: transcription.Result{Language: "en", Segments: slices.Clone(tt.segments)}}
			m := storetest.Meeting(t, d.Pool, domain.StatusQueued, "audio", "Ana")

			if err := d.transcribe(ctx, TranscribeArgs{MeetingID: m.ID}); err != nil {
				t.Fatal(err)
			}
			if got := reload(t, ctx, d, m.ID).Status; got != string(domain.StatusSummarizing) {
				t.Errorf("status = %s, want summarizing", got)
			}
			segs, err := d.segments(ctx, m)
			if err != nil {
				t.Fatal(err)
			}
			speakers := []string{}
			for _, s := range segs {
				speakers = append(speakers, s.Speaker)
			}
			if !slices.Equal(speakers, tt.wantSpeakers) {
				t.Errorf("speakers = %v, want %v", speakers, tt.wantSpeakers)
			}
			if got := storetest.JobKinds(t, d.Pool, m.ID); !slices.Equal(got, []string{"summarize"}) {
				t.Errorf("jobs = %v", got)
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name        string
		segments    []domain.Segment
		wantCalls   int
		wantSummary bool
		wantItems   map[string]bool
	}{
		{"writes the summary and keeps ticks on unchanged items", spoken, 1, true, map[string]bool{"Send notes": true, "Book a room": false}},
		{"no speech skips the model", []domain.Segment{}, 0, false, map[string]bool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, d := newDeps(t)
			calls := 0
			d.Summarizer = fakeSummarizer{calls: &calls, res: domain.SummaryResult{
				Overview: "Notes.", Provider: "fake",
				ActionItems: []domain.ActionItemDraft{{Text: "Send notes"}, {Text: "Book a room"}},
			}}
			m := storetest.Meeting(t, d.Pool, domain.StatusSummarizing, "audio")
			saveTranscript(t, ctx, d, m, tt.segments)
			q := db.New(d.Pool)
			if err := q.InsertActionItem(ctx, db.InsertActionItemParams{MeetingID: m.ID, WorkspaceID: m.WorkspaceID, Text: " send notes", Done: true}); err != nil {
				t.Fatal(err)
			}

			if err := d.summarize(ctx, SummarizeArgs{MeetingID: m.ID}); err != nil {
				t.Fatal(err)
			}
			got := reload(t, ctx, d, m.ID)
			if got.Status != string(domain.StatusReady) || got.SummaryError != nil {
				t.Errorf("status = %s, summary error = %v", got.Status, got.SummaryError)
			}
			if calls != tt.wantCalls {
				t.Errorf("summarizer calls = %d, want %d", calls, tt.wantCalls)
			}
			if _, err := q.GetSummary(ctx, m.ID); (err == nil) != tt.wantSummary {
				t.Errorf("summary saved = %v, want %v", err == nil, tt.wantSummary)
			}
			if tt.wantCalls > 0 {
				items, _ := q.ListActionItems(ctx, m.ID)
				done := map[string]bool{}
				for _, a := range items {
					done[a.Text] = a.Done
				}
				if len(done) != len(tt.wantItems) {
					t.Errorf("items = %v, want %v", done, tt.wantItems)
				}
				for text, want := range tt.wantItems {
					if done[text] != want {
						t.Errorf("%q done = %v, want %v", text, done[text], want)
					}
				}
			}
			if kinds := storetest.JobKinds(t, d.Pool, m.ID); !slices.Equal(kinds, []string{"index_meeting"}) {
				t.Errorf("jobs = %v", kinds)
			}
		})
	}
}

func TestStepGivesUp(t *testing.T) {
	raw := errors.New(`POST https://api.example/v1/messages: 400 {"request_id":"req_1"}`)
	tests := []struct {
		name             string
		kind             string
		err              error
		attempt          int
		wantStatus       domain.Status
		wantError        string
		wantSummaryError string
		wantJobs         []string
	}{
		{"transcription failure fails the meeting with a short message", "transcribe", domain.Permanent(domain.WithMessage("The recording couldn't be transcribed. Try another file.", raw)), 1, domain.StatusFailed, "The recording couldn't be transcribed. Try another file.", "", nil},
		{"transcription error is retried", "transcribe", raw, 1, domain.StatusTranscribing, "", "", nil},
		{"missing recording fails at once", "transcribe-missing", nil, 1, domain.StatusFailed, "The recording file is missing. Upload it again.", "", nil},
		{"last transcription attempt without a message", "transcribe", raw, maxAttempts, domain.StatusFailed, "Transcription failed.", "", nil},
		{"summary failure keeps the meeting ready and searchable", "summarize", domain.Permanent(domain.WithMessage("The Anthropic account has no credits.", raw)), 1, domain.StatusReady, "", "The Anthropic account has no credits.", []string{"index_meeting"}},
		{"summary error is retried", "summarize", raw, 1, domain.StatusSummarizing, "", "", nil},
		{"last summary attempt gives up", "summarize", raw, maxAttempts, domain.StatusReady, "", "The summary couldn't be written.", []string{"index_meeting"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, d := newDeps(t)
			calls := 0
			d.Transcriber = fakeTranscriber{err: tt.err}
			d.Summarizer = fakeSummarizer{calls: &calls, err: tt.err}
			row := &rivertype.JobRow{Kind: tt.kind, Attempt: tt.attempt, MaxAttempts: maxAttempts}

			var m db.Meeting
			if tt.kind == "transcribe" || tt.kind == "transcribe-missing" {
				audio := "audio"
				if tt.kind == "transcribe-missing" {
					audio = "missing"
				}
				m = storetest.Meeting(t, d.Pool, domain.StatusQueued, audio)
				s := &step[TranscribeArgs]{deps: d, giveUp: d.failMeeting, run: d.transcribe}
				_ = s.Work(ctx, &river.Job[TranscribeArgs]{JobRow: row, Args: TranscribeArgs{MeetingID: m.ID}})
			} else {
				m = storetest.Meeting(t, d.Pool, domain.StatusSummarizing, "audio")
				saveTranscript(t, ctx, d, m, spoken)
				if err := db.New(d.Pool).UpsertSummary(ctx, db.UpsertSummaryParams{MeetingID: m.ID, WorkspaceID: m.WorkspaceID, Overview: "Earlier summary.", Takeaways: []byte("[]"), Decisions: []byte("[]"), Questions: []byte("[]"), Provider: "fake"}); err != nil {
					t.Fatal(err)
				}
				s := &step[SummarizeArgs]{deps: d, giveUp: d.skipSummary, run: d.summarize}
				_ = s.Work(ctx, &river.Job[SummarizeArgs]{JobRow: row, Args: SummarizeArgs{MeetingID: m.ID}})
				if _, err := db.New(d.Pool).GetSummary(ctx, m.ID); err != nil {
					t.Errorf("earlier summary was lost: %v", err)
				}
			}

			got := reload(t, ctx, d, m.ID)
			if got.Status != string(tt.wantStatus) {
				t.Errorf("status = %s, want %s", got.Status, tt.wantStatus)
			}
			if deref(got.Error) != tt.wantError {
				t.Errorf("error = %q, want %q", deref(got.Error), tt.wantError)
			}
			if deref(got.SummaryError) != tt.wantSummaryError {
				t.Errorf("summary error = %q, want %q", deref(got.SummaryError), tt.wantSummaryError)
			}
			if kinds := storetest.JobKinds(t, d.Pool, m.ID); !slices.Equal(kinds, tt.wantJobs) {
				t.Errorf("jobs = %v, want %v", kinds, tt.wantJobs)
			}
		})
	}
}

func TestDeletedMeetingIsSkipped(t *testing.T) {
	ctx, d := newDeps(t)
	calls := 0
	d.Summarizer = fakeSummarizer{calls: &calls}
	m := storetest.Meeting(t, d.Pool, domain.StatusSummarizing, "audio")
	if _, err := db.New(d.Pool).DeleteMeeting(ctx, db.DeleteMeetingParams{WorkspaceID: m.WorkspaceID, ID: m.ID}); err != nil {
		t.Fatal(err)
	}
	if err := d.summarize(ctx, SummarizeArgs{MeetingID: m.ID}); err != nil || calls != 0 {
		t.Errorf("err = %v, calls = %d", err, calls)
	}
	if err := d.skipSummary(ctx, m.ID, errors.New("boom")); err != nil {
		t.Errorf("skipSummary on a deleted meeting: %v", err)
	}
}

func TestIndexRechecksTheMeeting(t *testing.T) {
	setStatus := func(status domain.Status) func(context.Context, *db.Queries, uuid.UUID) error {
		return func(ctx context.Context, q *db.Queries, id uuid.UUID) error {
			_, err := q.SetMeetingStatus(ctx, db.SetMeetingStatusParams{ID: id, Status: string(status)})
			return err
		}
	}
	tests := []struct {
		name       string
		race       func(context.Context, *db.Queries, uuid.UUID) error
		wantDelete bool
	}{
		{"deleted meanwhile", func(ctx context.Context, q *db.Queries, id uuid.UUID) error {
			_, err := q.DeleteMeeting(ctx, db.DeleteMeetingParams{WorkspaceID: domain.DefaultWorkspaceID, ID: id})
			return err
		}, true},
		{"new recording uploaded meanwhile", setStatus(domain.StatusQueued), true},
		{"summary regenerated meanwhile", setStatus(domain.StatusSummarizing), false},
		{"nothing happened", setStatus(domain.StatusReady), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, d := newDeps(t)
			idx := &racingIndexer{race: tt.race, q: db.New(d.Pool)}
			d.Indexer = idx
			m := storetest.Meeting(t, d.Pool, domain.StatusReady, "audio")
			saveTranscript(t, ctx, d, m, spoken)

			if err := d.index(ctx, IndexMeetingArgs{MeetingID: m.ID}); err != nil {
				t.Fatal(err)
			}
			if deleted := slices.Equal(idx.deleted, []uuid.UUID{m.ID}); deleted != tt.wantDelete {
				t.Errorf("search deletes = %v, want delete %v", idx.deleted, tt.wantDelete)
			}
		})
	}
}
