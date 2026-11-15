package storage

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"

	"meeting-assistant/backend/internal/domain"
)

type memObject struct {
	data        []byte
	contentType string
}

type Memory struct {
	mu      sync.Mutex
	objects map[string]memObject
}

func NewMemory() *Memory { return &Memory{objects: map[string]memObject{}} }

func (m *Memory) Put(_ context.Context, key string, body io.ReadSeeker, _ int64, contentType string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memObject{data: data, contentType: contentType}
	return nil
}

func (m *Memory) Get(_ context.Context, key, _ string) (*Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &Object{Body: io.NopCloser(bytes.NewReader(o.data)), ContentType: o.contentType, ContentLength: int64(len(o.data))}, nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *Memory) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			delete(m.objects, key)
		}
	}
	return nil
}

func (m *Memory) Ping(context.Context) error { return nil }
