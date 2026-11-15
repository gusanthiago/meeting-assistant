//go:build integration

// Package storetest gives integration tests their own migrated database on the Postgres from DATABASE_DIRECT_URL.
package storetest

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/store"
	"meeting-assistant/backend/internal/store/db"
)

func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	url := os.Getenv("DATABASE_DIRECT_URL")
	if url == "" {
		t.Skip("DATABASE_DIRECT_URL is not set; run make test-integration")
	}
	admin, err := pgx.Connect(ctx, url)
	check(t, err)
	name := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(ctx, "CREATE DATABASE "+name)
	check(t, err)

	cfg, err := pgxpool.ParseConfig(url)
	check(t, err)
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	check(t, err)
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})

	goose.SetBaseFS(store.Migrations)
	goose.SetLogger(goose.NopLogger())
	check(t, goose.SetDialect("postgres"))
	check(t, goose.UpContext(ctx, stdlib.OpenDBFromPool(pool), "migrations"))
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	check(t, err)
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	check(t, err)
	return pool
}

// NewInserter returns a River client that only inserts jobs, so tests can inspect them without running workers.
func NewInserter(t *testing.T, pool *pgxpool.Pool) *river.Client[pgx.Tx] {
	t.Helper()
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	check(t, err)
	return client
}

// Meeting inserts a meeting in the default workspace with the given status and, when audioKey is set, a recording.
func Meeting(t *testing.T, pool *pgxpool.Pool, status domain.Status, audioKey string, participants ...string) db.Meeting {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	m, err := q.InsertMeeting(ctx, db.InsertMeetingParams{WorkspaceID: domain.DefaultWorkspaceID, Title: "Sync", Participants: append([]string{}, participants...), Status: string(status)})
	check(t, err)
	if audioKey != "" {
		ct, size := "audio/wav", int64(5)
		m, err = q.SetMeetingRecording(ctx, db.SetMeetingRecordingParams{ID: m.ID, AudioKey: &audioKey, AudioContentType: &ct, AudioSize: &size, Status: string(status)})
		check(t, err)
	}
	return m
}

// JobKinds lists the kinds of the jobs queued for a meeting, oldest first.
func JobKinds(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), "SELECT kind FROM river_job WHERE args->>'meeting_id' = $1 ORDER BY id", id.String())
	check(t, err)
	kinds, err := pgx.CollectRows(rows, pgx.RowTo[string])
	check(t, err)
	return kinds
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
