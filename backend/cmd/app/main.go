package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"meeting-assistant/backend/api"
	"meeting-assistant/backend/internal/config"
	"meeting-assistant/backend/internal/domain"
	"meeting-assistant/backend/internal/jobs"
	"meeting-assistant/backend/internal/meeting"
	"meeting-assistant/backend/internal/search"
	"meeting-assistant/backend/internal/storage"
	"meeting-assistant/backend/internal/store"
	"meeting-assistant/backend/internal/store/db"
	"meeting-assistant/backend/internal/summary"
	"meeting-assistant/backend/internal/transcription"
)

const (
	shutdownTimeout = 20 * time.Second
	retryAttempts   = 30
	retryDelay      = 2 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("meeting assistant stopped", "error", err)
		os.Exit(1)
	}
}

// run loads the config and runs the command given as the first argument: serve (default), migrate or reindex.
// Returns an error instead of exiting, so every deferred cleanup still runs.
func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "serve":
		return runServe(ctx, cfg)
	case "migrate":
		return runMigrate(ctx, cfg)
	case "reindex":
		return runReindex(ctx, cfg)
	}
	return fmt.Errorf("unknown command %q (use serve, migrate or reindex)", command)
}

// runServe starts the job workers and the HTTP API, and stops both on shutdown.
func runServe(ctx context.Context, cfg config.Config) error {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()

	objects, err := newStorage(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build storage client: %w", err)
	}
	searchClient := newSearchClient(cfg, 10*time.Second)

	workers, err := newWorkers(pool, cfg, objects, searchClient)
	if err != nil {
		return fmt.Errorf("build job workers: %w", err)
	}
	if err := workers.Start(ctx); err != nil {
		return fmt.Errorf("start job workers: %w", err)
	}
	defer stopWorkers(workers)

	meetings := &meeting.Service{
		Pool:           pool,
		Storage:        objects,
		Search:         searchClient,
		Jobs:           workers,
		WorkspaceID:    domain.DefaultWorkspaceID,
		MaxUploadBytes: cfg.MaxUploadBytes,
	}
	handler := api.NewHandler(meetings, searchClient, domain.DefaultWorkspaceID, map[string]api.Check{
		"postgres":   pool.Ping,
		"opensearch": searchClient.Ping,
		"storage":    objects.Ping,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(handler, cfg.CORSOrigins, cfg.OpenAPIPath),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return serve(ctx, server)
}

// serve runs the given server until the context is cancelled, then drains it.
func serve(ctx context.Context, server *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("meeting assistant listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

// newWorkers creates the River client that runs the processing pipeline.
func newWorkers(pool *pgxpool.Pool, cfg config.Config, objects storage.Storage, indexer jobs.Indexer) (*river.Client[pgx.Tx], error) {
	deps := &jobs.Deps{
		Pool:        pool,
		Storage:     objects,
		Transcriber: newTranscriber(cfg),
		Summarizer:  newSummarizer(cfg),
		Indexer:     indexer,
		Logger:      slog.Default(),
	}
	slog.Info("providers", "transcription", cfg.TranscriptionProvider, "summary", cfg.SummaryProvider)

	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:   jobs.Queues(cfg.Workers),
		Workers:  jobs.NewWorkers(deps),
		PollOnly: cfg.RiverPollOnly,
		Logger:   slog.Default(),
	})
}

func stopWorkers(workers *river.Client[pgx.Tx]) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := workers.Stop(ctx); err != nil {
		slog.Warn("stop job workers", "error", err)
	}
}

func newTranscriber(cfg config.Config) transcription.Provider {
	if cfg.TranscriptionProvider == "whisper" {
		return &transcription.Whisper{BaseURL: cfg.WhisperURL, Client: &http.Client{Timeout: cfg.WhisperTimeout}}
	}
	return transcription.Fake{}
}

func newSummarizer(cfg config.Config) summary.Provider {
	if cfg.SummaryProvider == "claude" {
		return summary.NewClaude(cfg.AnthropicAPIKey, cfg.AnthropicModel, cfg.AnthropicEffort)
	}
	return summary.Fake{}
}

func newSearchClient(cfg config.Config, timeout time.Duration) *search.Client {
	return &search.Client{BaseURL: cfg.OpenSearchURL, Alias: cfg.OpenSearchAlias, HTTP: &http.Client{Timeout: timeout}}
}

// runMigrate applies the app and River migrations, then creates the search index and storage bucket.
// Each step retries while its dependency is still starting.
func runMigrate(ctx context.Context, cfg config.Config) error {
	sqlDB, err := sql.Open("pgx", cfg.DatabaseDirectURL)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	pool, err := pgxpool.New(ctx, cfg.DatabaseDirectURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	objects, err := newStorage(ctx, cfg)
	if err != nil {
		return err
	}
	searchClient := newSearchClient(cfg, 30*time.Second)

	goose.SetBaseFS(store.Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	steps := []struct {
		name string
		run  func() error
	}{
		{"app migrations", func() error { return goose.UpContext(ctx, sqlDB, "migrations") }},
		{"river migrations", func() error {
			_, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
			return err
		}},
		{"search index", func() error { return searchClient.EnsureIndex(ctx) }},
		{"storage bucket", func() error { return objects.EnsureBucket(ctx) }},
	}
	for _, step := range steps {
		if err := retry(ctx, step.name, step.run); err != nil {
			return err
		}
	}

	slog.Info("migrations complete")
	return nil
}

// runReindex queues an index job for every ready meeting in one batch, rebuilding search from Postgres.
func runReindex(ctx context.Context, cfg config.Config) error {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	client, err := river.NewClient[pgx.Tx](riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		return err
	}
	ids, err := db.New(pool).ListReadyMeetingIDs(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}

	batch := make([]river.InsertManyParams, len(ids))
	for i, id := range ids {
		batch[i] = river.InsertManyParams{Args: jobs.IndexMeetingArgs{MeetingID: id}}
	}
	if _, err := client.InsertMany(ctx, batch); err != nil {
		return err
	}

	slog.Info("reindex queued", "meetings", len(ids))
	return nil
}

func newStorage(ctx context.Context, cfg config.Config) (*storage.S3, error) {
	return storage.NewS3(ctx, cfg.S3Endpoint, cfg.S3Region, cfg.S3Bucket, cfg.S3AccessKey, cfg.S3SecretKey)
}

func retry(ctx context.Context, step string, fn func() error) error {
	var err error
	for range retryAttempts {
		if err = fn(); err == nil {
			return nil
		}
		slog.Info("waiting for dependency", "step", step, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay):
		}
	}
	return fmt.Errorf("%s: %w", step, err)
}
