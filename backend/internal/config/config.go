package config

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr          string
	DatabaseURL       string
	DatabaseDirectURL string
	RiverPollOnly     bool
	Workers           map[string]int

	OpenSearchURL   string
	OpenSearchAlias string

	S3Endpoint  string
	S3Region    string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string

	TranscriptionProvider string
	WhisperURL            string
	WhisperTimeout        time.Duration

	SummaryProvider string
	AnthropicAPIKey string
	AnthropicModel  string
	AnthropicEffort string

	MaxUploadBytes int64
	CORSOrigins    []string
	OpenAPIPath    string
}

func Load() (Config, error) {
	c := Config{
		HTTPAddr:          ":" + get("API_PORT", "8080"),
		DatabaseURL:       get("DATABASE_URL", ""),
		DatabaseDirectURL: get("DATABASE_DIRECT_URL", ""),
		RiverPollOnly:     getBool("RIVER_POLL_ONLY", true),
		Workers: map[string]int{
			"transcribe": getInt("RIVER_TRANSCRIBE_WORKERS", 2),
			"summarize":  getInt("RIVER_SUMMARIZE_WORKERS", 4),
			"index":      getInt("RIVER_INDEX_WORKERS", 4),
		},
		OpenSearchURL:         get("OPENSEARCH_URL", "http://localhost:9200"),
		OpenSearchAlias:       get("OPENSEARCH_INDEX_ALIAS", "meetings"),
		S3Endpoint:            get("S3_ENDPOINT", "http://localhost:9000"),
		S3Region:              get("S3_REGION", "us-east-1"),
		S3Bucket:              get("S3_BUCKET", "recordings"),
		S3AccessKey:           get("S3_ACCESS_KEY", ""),
		S3SecretKey:           get("S3_SECRET_KEY", ""),
		TranscriptionProvider: get("TRANSCRIPTION_PROVIDER", "whisper"),
		WhisperURL:            get("WHISPER_URL", "http://localhost:9000"),
		WhisperTimeout:        time.Duration(getInt("WHISPER_TIMEOUT_SECONDS", 1800)) * time.Second,
		SummaryProvider:       get("SUMMARY_PROVIDER", "fake"),
		AnthropicAPIKey:       get("ANTHROPIC_API_KEY", ""),
		AnthropicModel:        get("ANTHROPIC_MODEL", "claude-haiku-5-5"),
		AnthropicEffort:       get("ANTHROPIC_EFFORT", "low"),
		MaxUploadBytes:        int64(getInt("MAX_UPLOAD_MB", 200)) << 20,
		CORSOrigins:           split(get("CORS_ALLOWED_ORIGINS", "")),
		OpenAPIPath:           get("OPENAPI_PATH", "/app/openapi.yaml"),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if c.DatabaseDirectURL == "" {
		c.DatabaseDirectURL = c.DatabaseURL
	}
	c.TranscriptionProvider = strings.ToLower(c.TranscriptionProvider)
	c.SummaryProvider = strings.ToLower(c.SummaryProvider)
	c.AnthropicEffort = strings.ToLower(c.AnthropicEffort)
	for _, opt := range []struct {
		key, value string
		allowed    []string
	}{
		{"TRANSCRIPTION_PROVIDER", c.TranscriptionProvider, []string{"whisper", "fake"}},
		{"SUMMARY_PROVIDER", c.SummaryProvider, []string{"claude", "fake"}},
		{"ANTHROPIC_EFFORT", c.AnthropicEffort, []string{"low", "medium", "high", "xhigh", "max"}},
	} {
		if !slices.Contains(opt.allowed, opt.value) {
			return c, fmt.Errorf("%s must be one of %s, got %q", opt.key, strings.Join(opt.allowed, ", "), opt.value)
		}
	}
	if c.SummaryProvider == "claude" && c.AnthropicAPIKey == "" {
		slog.Warn("SUMMARY_PROVIDER is claude but ANTHROPIC_API_KEY is empty; using the rule-based summarizer")
		c.SummaryProvider = "fake"
	}
	return c, nil
}

func get(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if n, err := strconv.Atoi(get(key, "")); err == nil {
		return n
	}
	return def
}

func getBool(key string, def bool) bool {
	if b, err := strconv.ParseBool(get(key, "")); err == nil {
		return b
	}
	return def
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
