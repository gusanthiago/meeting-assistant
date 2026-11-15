package jobs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/search"
	"meeting-assistant/backend/internal/store/db"
	"meeting-assistant/backend/internal/summary"
	"meeting-assistant/backend/internal/transcription"
)

func (d *Deps) transcribe(ctx context.Context, args TranscribeArgs) error {
	m, err := d.load(ctx, args.MeetingID, domain.StatusQueued, domain.StatusTranscribing)
	if err != nil || m == nil {
		return err
	}
	if m.AudioKey == nil {
		return domain.Permanent(errors.New("meeting has no recording"))
	}
	if m.Status == string(domain.StatusQueued) {
		if _, err := db.New(d.Pool).SetMeetingStatus(ctx, db.SetMeetingStatusParams{ID: m.ID, Status: string(domain.StatusTranscribing)}); err != nil {
			return err
		}
	}

	audio, err := d.Storage.Get(ctx, *m.AudioKey, "")
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Permanent(domain.WithMessage("The recording file is missing. Upload it again.", err))
	}
	if err != nil {
		return err
	}
	defer audio.Body.Close()

	res, err := d.Transcriber.Transcribe(ctx, transcription.Request{
		Audio: audio.Body, ContentType: deref(m.AudioContentType), DurationHintMs: deref(m.DurationMs),
	})
	if err != nil {
		return err
	}
	segments, err := json.Marshal(transcription.LabelSpeakers(res.Segments, m.Participants))
	if err != nil {
		return err
	}

	return d.commit(ctx, m.ID, domain.StatusTranscribing, func(q *db.Queries, tx pgx.Tx) error {
		err := q.UpsertTranscript(ctx, db.UpsertTranscriptParams{
			MeetingID: m.ID, WorkspaceID: m.WorkspaceID, Language: cmp.Or(res.Language, "en"), Segments: segments,
		})
		if err == nil && res.DurationMs > 0 {
			err = q.SetMeetingDurationIfMissing(ctx, db.SetMeetingDurationIfMissingParams{ID: m.ID, DurationMs: &res.DurationMs})
		}
		if err != nil {
			return err
		}
		return advance(ctx, q, tx, m.ID, domain.StatusSummarizing, SummarizeArgs{MeetingID: m.ID})
	})
}

func (d *Deps) summarize(ctx context.Context, args SummarizeArgs) error {
	m, err := d.load(ctx, args.MeetingID, domain.StatusSummarizing)
	if err != nil || m == nil {
		return err
	}

	segments, err := d.segments(ctx, *m)
	if err != nil {
		return domain.Permanent(err)
	}
	if len(segments) == 0 {
		// No speech: finish without a summary instead of calling the model.
		return d.commit(ctx, m.ID, domain.StatusSummarizing, func(q *db.Queries, tx pgx.Tx) error {
			return advance(ctx, q, tx, m.ID, domain.StatusReady, IndexMeetingArgs{MeetingID: m.ID})
		})
	}
	res, err := d.Summarizer.Summarize(ctx, summary.Input{
		Title: m.Title, Participants: m.Participants, DurationMs: deref(m.DurationMs), Segments: segments,
	})
	if err != nil {
		return err
	}

	return d.commit(ctx, m.ID, domain.StatusSummarizing, func(q *db.Queries, tx pgx.Tx) error {
		err := q.UpsertSummary(ctx, db.UpsertSummaryParams{
			MeetingID: m.ID, WorkspaceID: m.WorkspaceID, Overview: res.Overview,
			Takeaways: jsonList(res.Takeaways), Decisions: jsonList(res.Decisions), Questions: jsonList(res.Questions),
			Provider: res.Provider, Model: ptr(res.Model), InputTokens: ptr(res.InputTokens), OutputTokens: ptr(res.OutputTokens),
		})
		var done map[string]bool
		if err == nil {
			done, err = doneTexts(ctx, q, m.ID)
		}
		if err == nil {
			err = q.DeleteActionItems(ctx, m.ID)
		}
		for i, a := range res.ActionItems {
			if err != nil {
				break
			}
			err = q.InsertActionItem(ctx, db.InsertActionItemParams{
				MeetingID: m.ID, WorkspaceID: m.WorkspaceID, Position: int32(i), Text: a.Text, Owner: ptr(a.Owner), Due: ptr(a.Due),
				Done: done[itemKey(a.Text)],
			})
		}
		if err != nil {
			return err
		}
		return advance(ctx, q, tx, m.ID, domain.StatusReady, IndexMeetingArgs{MeetingID: m.ID})
	})
}

func (d *Deps) index(ctx context.Context, args IndexMeetingArgs) error {
	m, err := d.load(ctx, args.MeetingID, domain.StatusReady)
	if err != nil || m == nil {
		return err
	}

	q := db.New(d.Pool)
	items, err := q.ListActionItems(ctx, m.ID)
	if err != nil {
		return err
	}
	doc := search.Doc{
		WorkspaceID: m.WorkspaceID, Title: m.Title, Status: m.Status, Participants: m.Participants,
		StartedAt: m.StartedAt, CreatedAt: m.CreatedAt, ActionItems: make([]string, len(items)),
	}
	for i, item := range items {
		doc.ActionItems[i] = item.Text
	}
	if doc.Segments, err = d.segments(ctx, *m); err != nil {
		doc.Segments = []domain.Segment{}
	}
	if s, err := q.GetSummary(ctx, m.ID); err == nil {
		doc.Summary = s.Overview
	}
	if err := d.Indexer.Upsert(ctx, m.ID, doc); err != nil {
		return err
	}
	// The meeting may have been deleted or given a new recording while the document was built.
	now, err := q.GetMeetingByID(ctx, m.ID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !keepsTranscript(domain.Status(now.Status))) {
		return d.Indexer.Delete(ctx, m.ID)
	}
	return err
}

// keepsTranscript is true while the indexed transcript is still the meeting's own; a regenerate keeps it.
func keepsTranscript(s domain.Status) bool {
	return s == domain.StatusReady || s == domain.StatusSummarizing
}

func (d *Deps) segments(ctx context.Context, m db.Meeting) ([]domain.Segment, error) {
	t, err := db.New(d.Pool).GetTranscript(ctx, db.GetTranscriptParams{WorkspaceID: m.WorkspaceID, MeetingID: m.ID})
	if err != nil {
		return nil, err
	}
	var segments []domain.Segment
	return segments, json.Unmarshal(t.Segments, &segments)
}

// doneTexts remembers which action items were ticked, so a new summary keeps the ticks on unchanged items.
func doneTexts(ctx context.Context, q *db.Queries, id uuid.UUID) (map[string]bool, error) {
	items, err := q.ListActionItems(ctx, id)
	done := make(map[string]bool, len(items))
	for _, a := range items {
		if a.Done {
			done[itemKey(a.Text)] = true
		}
	}
	return done, err
}

func itemKey(text string) string { return strings.ToLower(strings.TrimSpace(text)) }

func jsonList(v []string) []byte {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return b
}

func ptr[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
