//go:build integration

package storage

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"meeting-assistant/backend/internal/domain"
)

func TestS3DeletePrefix(t *testing.T) {
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_ENDPOINT is not set; run make test-integration")
	}
	ctx := context.Background()
	s, err := NewS3(ctx, endpoint, os.Getenv("S3_REGION"), os.Getenv("S3_BUCKET"), os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	ws := "test-" + uuid.NewString()
	keys := []string{MeetingPrefix(ws, "m1") + "recording.webm", RecordingKey(ws, "m1", "audio/wav"), RecordingKey(ws, "m2", "audio/wav")}
	for _, k := range keys {
		if err := s.Put(ctx, k, strings.NewReader("voice"), 5, "audio/wav"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = s.DeletePrefix(context.Background(), "workspaces/"+ws+"/") })

	if err := s.DeletePrefix(ctx, MeetingPrefix(ws, "m1")); err != nil {
		t.Fatal(err)
	}
	for i, k := range keys {
		_, err := s.Get(ctx, k, "")
		if gone := errors.Is(err, domain.ErrNotFound); gone != (i < 2) {
			t.Errorf("%s gone = %v (err %v)", k, gone, err)
		}
	}
}
