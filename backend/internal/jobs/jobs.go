package jobs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/search"
	"meeting-assistant/backend/internal/storage"
	"meeting-assistant/backend/internal/store/db"
	"meeting-assistant/backend/internal/summary"
	"meeting-assistant/backend/internal/transcription"
)

type Indexer interface {
	Upsert(ctx context.Context, id uuid.UUID, doc search.Doc) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type Deps struct {
	Pool        *pgxpool.Pool
	Storage     storage.Storage
	Transcriber transcription.Provider
	Summarizer  summary.Provider
	Indexer     Indexer
	Logger      *slog.Logger
}

func Queues(workers map[string]int) map[string]river.QueueConfig {
	queues := make(map[string]river.QueueConfig, len(workers))
	for name, n := range workers {
		queues[name] = river.QueueConfig{MaxWorkers: max(n, 1)}
	}
	return queues
}

func NewWorkers(d *Deps) *river.Workers {
	w := river.NewWorkers()
	river.AddWorker(w, &step[TranscribeArgs]{deps: d, timeout: 35 * time.Minute, giveUp: d.failMeeting, run: d.transcribe})
	// A failed summary keeps the meeting ready and searchable; the reason is shown next to the summary.
	river.AddWorker(w, &step[SummarizeArgs]{deps: d, timeout: 10 * time.Minute, giveUp: d.skipSummary, run: d.summarize})
	// A failed index only delays search; the meeting itself stays ready.
	river.AddWorker(w, &step[IndexMeetingArgs]{deps: d, run: d.index})
	return w
}

// step runs one stage of the pipeline and turns its error into a retry, a snooze or a call to giveUp.
type step[T meetingArgs] struct {
	river.WorkerDefaults[T]
	deps    *Deps
	timeout time.Duration
	giveUp  func(context.Context, uuid.UUID, error) error
	run     func(context.Context, T) error
}

func (s *step[T]) Timeout(*river.Job[T]) time.Duration { return s.timeout }

func (s *step[T]) Work(ctx context.Context, job *river.Job[T]) error {
	err := s.run(ctx, job.Args)
	if err == nil {
		return nil
	}

	row := job.JobRow
	last := row.Attempt >= row.MaxAttempts
	var wait *domain.RetryAfterError
	if errors.As(err, &wait) && !last {
		return river.JobSnooze(wait.After)
	}

	permanent := domain.IsPermanent(err)
	s.deps.Logger.Warn("job failed", "kind", row.Kind, "meeting_id", job.Args.meetingID(), "attempt", row.Attempt, "permanent", permanent, "error", err)
	if s.giveUp != nil && (permanent || last) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if gerr := s.giveUp(ctx, job.Args.meetingID(), err); gerr != nil {
			s.deps.Logger.Error("give up on meeting", "kind", row.Kind, "meeting_id", job.Args.meetingID(), "error", gerr)
		}
	}
	if permanent {
		return river.JobCancel(err)
	}
	return err
}

// load returns nil when the meeting is gone or has moved past the expected statuses, making a stale job a no-op.
func (d *Deps) load(ctx context.Context, id uuid.UUID, expected ...domain.Status) (*db.Meeting, error) {
	m, err := db.New(d.Pool).GetMeetingByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, s := range expected {
		if m.Status == string(s) {
			return &m, nil
		}
	}
	d.Logger.Info("skipping stale job", "meeting_id", id, "status", m.Status)
	return nil, nil
}

// commit locks the meeting and runs fn in one transaction, skipping it when the status changed meanwhile.
func (d *Deps) commit(ctx context.Context, id uuid.UUID, expected domain.Status, fn func(q *db.Queries, tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		m, err := q.LockMeetingByID(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // deleted meanwhile
		}
		if err != nil || m.Status != string(expected) {
			return err
		}
		return fn(q, tx)
	})
}

// advance moves the meeting to status and enqueues the next job in the caller's transaction.
func advance(ctx context.Context, q *db.Queries, tx pgx.Tx, id uuid.UUID, status domain.Status, next river.JobArgs) error {
	if _, err := q.SetMeetingStatus(ctx, db.SetMeetingStatusParams{ID: id, Status: string(status)}); err != nil {
		return err
	}
	return enqueue(ctx, tx, next)
}

func enqueue(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
	_, err := river.ClientFromContext[pgx.Tx](ctx).InsertTx(ctx, tx, args, nil)
	return err
}

// failMeeting marks a meeting failed while transcription still owns it; the raw cause is already logged.
func (d *Deps) failMeeting(ctx context.Context, id uuid.UUID, cause error) error {
	msg := domain.Message(cause, "Transcription failed.")
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		m, err := q.LockMeetingByID(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil || (m.Status != string(domain.StatusQueued) && m.Status != string(domain.StatusTranscribing)) {
			return err
		}
		_, err = q.SetMeetingStatus(ctx, db.SetMeetingStatusParams{ID: id, Status: string(domain.StatusFailed), Error: &msg})
		return err
	})
}

// skipSummary leaves the meeting ready with its earlier summary, if any, and still indexes the transcript.
func (d *Deps) skipSummary(ctx context.Context, id uuid.UUID, cause error) error {
	msg := domain.Message(cause, "The summary couldn't be written.")
	return d.commit(ctx, id, domain.StatusSummarizing, func(q *db.Queries, tx pgx.Tx) error {
		if _, err := q.SetMeetingStatus(ctx, db.SetMeetingStatusParams{ID: id, Status: string(domain.StatusReady), SummaryError: &msg}); err != nil {
			return err
		}
		return enqueue(ctx, tx, IndexMeetingArgs{MeetingID: id})
	})
}
