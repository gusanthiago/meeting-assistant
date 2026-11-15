package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/jobs"
	"meeting-assistant/backend/internal/storage"
	"meeting-assistant/backend/internal/store/db"
)

type JobInserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

const cleanupTimeout = 30 * time.Second

type SearchIndex interface {
	Delete(ctx context.Context, id uuid.UUID) error
}

type Service struct {
	Pool           *pgxpool.Pool
	Storage        storage.Storage
	Search         SearchIndex
	Jobs           JobInserter
	WorkspaceID    uuid.UUID
	MaxUploadBytes int64
}

type Detail struct {
	Meeting     db.Meeting
	Summary     *db.Summary
	ActionItems []db.ActionItem
}

type CreateInput struct {
	Title          string
	Participants   []string
	StartedAt      *time.Time
	IdempotencyKey string
}

func (s *Service) Create(ctx context.Context, in CreateInput) (db.Meeting, bool, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return db.Meeting{}, false, fmt.Errorf("%w: title is required", domain.ErrValidation)
	}
	key := nonEmpty(strings.TrimSpace(in.IdempotencyKey))

	q := db.New(s.Pool)
	m, err := q.InsertMeeting(ctx, db.InsertMeetingParams{
		WorkspaceID:    s.WorkspaceID,
		Title:          title,
		Participants:   trimAll(in.Participants),
		Status:         string(domain.StatusRecording),
		IdempotencyKey: key,
		StartedAt:      in.StartedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) && key != nil {
		m, err = q.GetMeetingByIdempotencyKey(ctx, db.GetMeetingByIdempotencyKeyParams{WorkspaceID: s.WorkspaceID, IdempotencyKey: key})
		return m, false, err
	}
	return m, err == nil, err
}

// List returns one page of meetings and the cursor of the next page, if any.
func (s *Service) List(ctx context.Context, cursor *uuid.UUID, limit int) ([]db.Meeting, *uuid.UUID, error) {
	items, err := db.New(s.Pool).ListMeetings(ctx, db.ListMeetingsParams{WorkspaceID: s.WorkspaceID, Cursor: cursor, Lim: int32(limit + 1)})
	if err != nil || len(items) <= limit {
		return items, nil, err
	}
	next := items[limit-1].ID
	return items[:limit], &next, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Detail, error) {
	q := db.New(s.Pool)
	m, err := q.GetMeeting(ctx, db.GetMeetingParams{WorkspaceID: s.WorkspaceID, ID: id})
	if err != nil {
		return Detail{}, notFound(err)
	}

	d := Detail{Meeting: m}
	switch sum, err := q.GetSummary(ctx, id); {
	case err == nil:
		d.Summary = &sum
	case !errors.Is(err, pgx.ErrNoRows):
		return Detail{}, err
	}
	d.ActionItems, err = q.ListActionItems(ctx, id)
	return d, err
}

func (s *Service) Transcript(ctx context.Context, id uuid.UUID) (db.Transcript, error) {
	t, err := db.New(s.Pool).GetTranscript(ctx, db.GetTranscriptParams{WorkspaceID: s.WorkspaceID, MeetingID: id})
	return t, notFound(err)
}

// UploadRecording stores the audio, clears earlier results and queues transcription in one transaction.
// The replaced recording and the old search entry are removed once the new upload is committed.
func (s *Service) UploadRecording(ctx context.Context, id uuid.UUID, body io.Reader, contentType string, durationMs *int64) (db.Meeting, error) {
	if !storage.IsAudio(contentType) {
		return db.Meeting{}, fmt.Errorf("%w: content type must be audio/*", domain.ErrValidation)
	}
	// Check before the upload so a busy meeting fails fast; the transaction checks again.
	if _, err := s.check(ctx, db.New(s.Pool), id, false, domain.CanAcceptUpload); err != nil {
		return db.Meeting{}, err
	}

	file, size, cleanup, err := storage.Spool(body, s.MaxUploadBytes)
	if err != nil {
		return db.Meeting{}, err
	}
	defer cleanup()
	key := storage.RecordingKey(s.WorkspaceID.String(), id.String(), contentType)
	if err := s.Storage.Put(ctx, key, file, size, contentType); err != nil {
		return db.Meeting{}, err
	}

	var out db.Meeting
	var previous *string
	err = s.inTx(ctx, func(q *db.Queries, tx pgx.Tx) error {
		m, err := s.check(ctx, q, id, true, domain.CanAcceptUpload)
		if err != nil {
			return err
		}
		previous = m.AudioKey
		for _, clear := range []func(context.Context, uuid.UUID) error{q.DeleteTranscript, q.DeleteSummary, q.DeleteActionItems} {
			if err := clear(ctx, id); err != nil {
				return err
			}
		}
		out, err = q.SetMeetingRecording(ctx, db.SetMeetingRecordingParams{
			ID: id, AudioKey: &key, AudioContentType: &contentType, AudioSize: &size,
			DurationMs: durationMs, Status: string(domain.StatusQueued),
		})
		if err != nil {
			return err
		}
		_, err = s.Jobs.InsertTx(ctx, tx, jobs.TranscribeArgs{MeetingID: id}, nil)
		return err
	})
	if err != nil {
		s.cleanup(ctx, "delete unused upload", id, func(ctx context.Context) error { return s.Storage.Delete(ctx, key) })
		return out, err
	}
	if previous != nil {
		s.cleanup(ctx, "delete replaced recording", id, func(ctx context.Context) error { return s.Storage.Delete(ctx, *previous) })
	}
	s.cleanup(ctx, "delete stale search entry", id, func(ctx context.Context) error { return s.Search.Delete(ctx, id) })
	return out, nil
}

// cleanup runs a best-effort removal after a commit. It is detached from the request, so a client
// that disconnects can't leave the old recording behind; a failure is logged, not returned.
func (s *Service) cleanup(ctx context.Context, what string, id uuid.UUID, fn func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if err := fn(ctx); err != nil {
		slog.Warn(what, "meeting_id", id, "error", err)
	}
}

func (s *Service) OpenRecording(ctx context.Context, id uuid.UUID, byteRange string) (*storage.Object, error) {
	m, err := db.New(s.Pool).GetMeeting(ctx, db.GetMeetingParams{WorkspaceID: s.WorkspaceID, ID: id})
	if err != nil {
		return nil, notFound(err)
	}
	if m.AudioKey == nil {
		return nil, domain.ErrNotFound
	}
	return s.Storage.Get(ctx, *m.AudioKey, byteRange)
}

func (s *Service) RegenerateSummary(ctx context.Context, id uuid.UUID) (db.Meeting, error) {
	var out db.Meeting
	err := s.inTx(ctx, func(q *db.Queries, tx pgx.Tx) error {
		if _, err := s.check(ctx, q, id, true, domain.CanRegenerateSummary); err != nil {
			return err
		}
		t, err := q.GetTranscript(ctx, db.GetTranscriptParams{WorkspaceID: s.WorkspaceID, MeetingID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: meeting has no transcript yet", domain.ErrConflict)
		}
		if err != nil {
			return err
		}
		var segments []json.RawMessage
		if err := json.Unmarshal(t.Segments, &segments); err != nil || len(segments) == 0 {
			return fmt.Errorf("%w: meeting has no speech to summarize", domain.ErrConflict)
		}
		if out, err = q.SetMeetingStatus(ctx, db.SetMeetingStatusParams{ID: id, Status: string(domain.StatusSummarizing)}); err != nil {
			return err
		}
		_, err = s.Jobs.InsertTx(ctx, tx, jobs.SummarizeArgs{MeetingID: id}, nil)
		return err
	})
	return out, err
}

// Retry queues a failed meeting again from its stored recording.
func (s *Service) Retry(ctx context.Context, id uuid.UUID) (db.Meeting, error) {
	var out db.Meeting
	err := s.inTx(ctx, func(q *db.Queries, tx pgx.Tx) error {
		m, err := s.check(ctx, q, id, true, domain.CanRetry)
		if err != nil {
			return err
		}
		if m.AudioKey == nil {
			return fmt.Errorf("%w: meeting has no recording", domain.ErrConflict)
		}
		if out, err = q.SetMeetingStatus(ctx, db.SetMeetingStatusParams{ID: id, Status: string(domain.StatusQueued)}); err != nil {
			return err
		}
		_, err = s.Jobs.InsertTx(ctx, tx, jobs.TranscribeArgs{MeetingID: id}, nil)
		return err
	})
	return out, err
}

// Delete removes the meeting and its results, then every stored recording and its search entry.
// A job still running for it finds the meeting gone and stops.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := db.New(s.Pool).DeleteMeeting(ctx, db.DeleteMeetingParams{WorkspaceID: s.WorkspaceID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	s.cleanup(ctx, "delete recordings", id, func(ctx context.Context) error {
		return s.Storage.DeletePrefix(ctx, storage.MeetingPrefix(s.WorkspaceID.String(), id.String()))
	})
	s.cleanup(ctx, "delete search entry", id, func(ctx context.Context) error { return s.Search.Delete(ctx, id) })
	return nil
}

func (s *Service) SetActionItemDone(ctx context.Context, id uuid.UUID, done bool) (db.ActionItem, error) {
	a, err := db.New(s.Pool).SetActionItemDone(ctx, db.SetActionItemDoneParams{WorkspaceID: s.WorkspaceID, ID: id, Done: done})
	return a, notFound(err)
}

func (s *Service) inTx(ctx context.Context, fn func(q *db.Queries, tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error { return fn(db.New(tx), tx) })
}

// check loads the meeting, locking its row when lock is set, and returns ErrConflict when allowed rejects its status.
func (s *Service) check(ctx context.Context, q *db.Queries, id uuid.UUID, lock bool, allowed func(domain.Status) bool) (db.Meeting, error) {
	var m db.Meeting
	var err error
	if lock {
		m, err = q.LockMeeting(ctx, db.LockMeetingParams{WorkspaceID: s.WorkspaceID, ID: id})
	} else {
		m, err = q.GetMeeting(ctx, db.GetMeetingParams{WorkspaceID: s.WorkspaceID, ID: id})
	}
	if err != nil {
		return m, notFound(err)
	}
	if !allowed(domain.Status(m.Status)) {
		return m, fmt.Errorf("%w: meeting is %s", domain.ErrConflict, m.Status)
	}
	return m, nil
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func trimAll(list []string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
