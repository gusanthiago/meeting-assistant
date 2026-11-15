package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"

	"meeting-assistant/backend/internal/domain"
)

type Object struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	ContentRange  string
}

type Storage interface {
	Put(ctx context.Context, key string, body io.ReadSeeker, size int64, contentType string) error
	Get(ctx context.Context, key, byteRange string) (*Object, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error
	Ping(ctx context.Context) error
}

type S3 struct {
	client *s3.Client
	bucket string
}

func NewS3(ctx context.Context, endpoint, region, bucket, accessKey, secretKey string) (*S3, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
		// S3-compatible stores reject the SDK's default extra checksums.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	return &S3{client: client, bucket: bucket}, nil
}

func (s *S3) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	return err
}

func (s *S3) Get(ctx context.Context, key, byteRange string) (*Object, error) {
	in := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	if byteRange != "" {
		in.Range = aws.String(byteRange)
	}
	out, err := s.client.GetObject(ctx, in)
	if err != nil {
		var nsk *types.NoSuchKey
		var apiErr smithy.APIError
		if errors.As(err, &nsk) {
			return nil, domain.ErrNotFound
		}
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidRange" {
			return nil, fmt.Errorf("%w: %s", domain.ErrValidation, "range not satisfiable")
		}
		return nil, err
	}
	return &Object{
		Body:          out.Body,
		ContentType:   aws.ToString(out.ContentType),
		ContentLength: aws.ToInt64(out.ContentLength),
		ContentRange:  aws.ToString(out.ContentRange),
	}, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

// DeletePrefix removes every object whose key starts with prefix.
func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			if err := s.Delete(ctx, aws.ToString(obj.Key)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *S3) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

func (s *S3) EnsureBucket(ctx context.Context) error {
	if s.Ping(ctx) == nil {
		return nil
	}
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	var owned *types.BucketAlreadyOwnedByYou
	if err != nil && !errors.As(err, &owned) {
		return err
	}
	return nil
}

// MeetingPrefix holds everything stored for one meeting.
func MeetingPrefix(workspaceID, meetingID string) string {
	return fmt.Sprintf("workspaces/%s/meetings/%s/", workspaceID, meetingID)
}

// RecordingKey names one upload. Each upload gets its own key, so a replaced
// recording is never overwritten while a job may still read it.
func RecordingKey(workspaceID, meetingID, contentType string) string {
	return MeetingPrefix(workspaceID, meetingID) + "recordings/" + uuid.Must(uuid.NewV7()).String() + extension(contentType)
}

func extension(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch ct {
	case "audio/webm":
		return ".webm"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	}
	return ""
}

func IsAudio(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	return strings.HasPrefix(ct, "audio/")
}
