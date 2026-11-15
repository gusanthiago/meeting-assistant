package storage

import (
	"fmt"
	"io"
	"os"

	"meeting-assistant/backend/internal/domain"
)

// Spool copies r to a temp file so the S3 client gets a seekable body with a known size.
func Spool(r io.Reader, maxBytes int64) (f *os.File, size int64, cleanup func(), err error) {
	f, err = os.CreateTemp("", "recording-*")
	if err != nil {
		return nil, 0, nil, err
	}
	cleanup = func() { f.Close(); os.Remove(f.Name()) }
	size, err = io.Copy(f, io.LimitReader(r, maxBytes+1))
	if err == nil && size > maxBytes {
		err = fmt.Errorf("%w: recording is larger than %d MB", domain.ErrTooLarge, maxBytes>>20)
	}
	if err == nil && size == 0 {
		err = fmt.Errorf("%w: recording is empty", domain.ErrValidation)
	}
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		cleanup()
		return nil, 0, nil, err
	}
	return f, size, cleanup, nil
}
