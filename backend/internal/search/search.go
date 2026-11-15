package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"meeting-assistant/backend/internal/domain"
)

const (
	preTag  = "\u0001"
	postTag = "\u0002"
)

type Doc struct {
	WorkspaceID  uuid.UUID        `json:"workspace_id"`
	Title        string           `json:"title"`
	Status       string           `json:"status"`
	Participants []string         `json:"participants"`
	StartedAt    *time.Time       `json:"started_at,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	Summary      string           `json:"summary"`
	ActionItems  []string         `json:"action_items"`
	Segments     []domain.Segment `json:"segments"`
}

type Part struct {
	Text  string
	Match bool
}

type Snippet struct {
	Field   string
	Speaker string
	StartMs *int64
	Parts   []Part
}

type Hit struct {
	MeetingID uuid.UUID
	Title     string
	StartedAt *time.Time
	Snippets  []Snippet
}

type Client struct {
	BaseURL string
	Alias   string
	HTTP    *http.Client
}

// Index is the current index version; bump it whenever Mapping changes.
func (c *Client) Index() string { return c.Alias + "-v2" }

// EnsureIndex points the alias at the current index version. When the alias still points at an
// older version, its documents are copied with _reindex and the old index is dropped in the same
// alias swap, so searches never see an empty index.
func (c *Client) EnsureIndex(ctx context.Context) error {
	current, err := c.aliasedIndices(ctx)
	if err != nil || slices.Equal(current, []string{c.Index()}) {
		return err
	}
	exists, err := c.exists(ctx, c.Index())
	if err != nil {
		return err
	}
	if !exists {
		body := map[string]any{
			"settings": map[string]any{"number_of_shards": 1, "number_of_replicas": 0},
			"mappings": Mapping(),
		}
		if err := c.expectOK(c.do(ctx, http.MethodPut, "/"+c.Index(), body)); err != nil {
			return err
		}
	}
	actions := []any{map[string]any{"add": map[string]any{"index": c.Index(), "alias": c.Alias, "is_write_index": true}}}
	for _, old := range current {
		if old == c.Index() {
			continue
		}
		if err := c.copyIndex(ctx, old, c.Index()); err != nil {
			return err
		}
		actions = append(actions, map[string]any{"remove_index": map[string]any{"index": old}})
	}
	return c.expectOK(c.do(ctx, http.MethodPost, "/_aliases", map[string]any{"actions": actions}))
}

func (c *Client) aliasedIndices(ctx context.Context) ([]string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/_alias/"+c.Alias, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("opensearch alias: %d %s", resp.StatusCode, b)
	}
	var byIndex map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&byIndex); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(byIndex))
	for name := range byIndex {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

func (c *Client) exists(ctx context.Context, index string) (bool, error) {
	resp, err := c.do(ctx, http.MethodHead, "/"+index, nil)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

func (c *Client) copyIndex(ctx context.Context, from, to string) error {
	resp, err := c.do(ctx, http.MethodPost, "/_reindex?refresh=true", map[string]any{"source": map[string]any{"index": from}, "dest": map[string]any{"index": to}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		Failures []json.RawMessage `json:"failures"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil || len(out.Failures) > 0 {
		return fmt.Errorf("opensearch reindex %s to %s: status %d, %d failures", from, to, resp.StatusCode, len(out.Failures))
	}
	return nil
}

func Mapping() map[string]any {
	text := map[string]any{"type": "text"}
	keyword := map[string]any{"type": "keyword"}
	return map[string]any{
		"dynamic": "strict",
		"properties": map[string]any{
			"workspace_id": keyword,
			"title":        text,
			"status":       keyword,
			"participants": map[string]any{"type": "text", "fields": map[string]any{"keyword": keyword}},
			"started_at":   map[string]any{"type": "date"},
			"created_at":   map[string]any{"type": "date"},
			"summary":      text,
			"action_items": text,
			"segments": map[string]any{
				"type": "nested",
				"properties": map[string]any{
					"speaker":  keyword,
					"start_ms": map[string]any{"type": "long"},
					"end_ms":   map[string]any{"type": "long"},
					"text":     text,
				},
			},
		},
	}
}

func (c *Client) Upsert(ctx context.Context, id uuid.UUID, doc Doc) error {
	return c.expectOK(c.do(ctx, http.MethodPut, "/"+c.Alias+"/_doc/"+id.String(), doc))
}

// Delete removes a meeting's document; a missing document is not an error.
func (c *Client) Delete(ctx context.Context, id uuid.UUID) error {
	resp, err := c.do(ctx, http.MethodDelete, "/"+c.Alias+"/_doc/"+id.String(), nil)
	if err == nil && resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil
	}
	return c.expectOK(resp, err)
}

func (c *Client) Ping(ctx context.Context) error {
	return c.expectOK(c.do(ctx, http.MethodGet, "/_cluster/health", nil))
}

func (c *Client) Search(ctx context.Context, workspaceID uuid.UUID, q string, limit int) ([]Hit, error) {
	resp, err := c.do(ctx, http.MethodPost, "/"+c.Alias+"/_search", Query(workspaceID, q, limit))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("opensearch search: %d %s", resp.StatusCode, b)
	}
	var raw searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw.hits(), nil
}

func Query(workspaceID uuid.UUID, q string, limit int) map[string]any {
	hl := func(fragmentSize, n int) map[string]any {
		return map[string]any{"fragment_size": fragmentSize, "number_of_fragments": n}
	}
	return map[string]any{
		"size":    limit,
		"_source": []string{"title", "started_at"},
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []any{map[string]any{"term": map[string]any{"workspace_id": workspaceID.String()}}},
				"should": []any{
					map[string]any{"multi_match": map[string]any{"query": q, "fields": []string{"title^3", "participants^2", "summary^2", "action_items"}}},
					map[string]any{"nested": map[string]any{
						"path":       "segments",
						"score_mode": "max",
						"query":      map[string]any{"match": map[string]any{"segments.text": q}},
						"inner_hits": map[string]any{
							"size": 3,
							"highlight": map[string]any{
								"pre_tags": []string{preTag}, "post_tags": []string{postTag},
								"fields": map[string]any{"segments.text": hl(0, 0)},
							},
						},
					}},
				},
				"minimum_should_match": 1,
			},
		},
		"highlight": map[string]any{
			"pre_tags": []string{preTag}, "post_tags": []string{postTag},
			"fields": map[string]any{
				"title":        hl(0, 0),
				"participants": hl(0, 0),
				"summary":      hl(160, 1),
				"action_items": hl(160, 1),
			},
		},
	}
}

func SplitHighlight(s string) []Part {
	var parts []Part
	for s != "" {
		start := strings.Index(s, preTag)
		if start < 0 {
			parts = append(parts, Part{Text: s})
			break
		}
		if start > 0 {
			parts = append(parts, Part{Text: s[:start]})
		}
		s = s[start+len(preTag):]
		end := strings.Index(s, postTag)
		if end < 0 {
			parts = append(parts, Part{Text: s, Match: true})
			break
		}
		parts = append(parts, Part{Text: s[:end], Match: true})
		s = s[end+len(postTag):]
	}
	return mergeAdjacent(parts)
}

func mergeAdjacent(parts []Part) []Part {
	var out []Part
	for _, p := range parts {
		if n := len(out); n > 0 && out[n-1].Match == p.Match {
			out[n-1].Text += p.Text
			continue
		}
		out = append(out, p)
	}
	return out
}

type searchResponse struct {
	Hits struct {
		Hits []struct {
			ID     string `json:"_id"`
			Source struct {
				Title     string     `json:"title"`
				StartedAt *time.Time `json:"started_at"`
			} `json:"_source"`
			Highlight map[string][]string `json:"highlight"`
			InnerHits map[string]struct {
				Hits struct {
					Hits []struct {
						Source    domain.Segment      `json:"_source"`
						Highlight map[string][]string `json:"highlight"`
					} `json:"hits"`
				} `json:"hits"`
			} `json:"inner_hits"`
		} `json:"hits"`
	} `json:"hits"`
}

func (r searchResponse) hits() []Hit {
	out := make([]Hit, 0, len(r.Hits.Hits))
	for _, h := range r.Hits.Hits {
		id, err := uuid.Parse(h.ID)
		if err != nil {
			continue
		}
		hit := Hit{MeetingID: id, Title: h.Source.Title, StartedAt: h.Source.StartedAt}
		for _, field := range []string{"title", "participants", "summary", "action_items"} {
			if frags := h.Highlight[field]; len(frags) > 0 {
				hit.Snippets = append(hit.Snippets, Snippet{Field: field, Parts: SplitHighlight(frags[0])})
			}
		}
		for _, ih := range h.InnerHits["segments"].Hits.Hits {
			text := ih.Source.Text
			if frags := ih.Highlight["segments.text"]; len(frags) > 0 {
				text = frags[0]
			}
			start := ih.Source.StartMs
			hit.Snippets = append(hit.Snippets, Snippet{Field: "transcript", Speaker: ih.Source.Speaker, StartMs: &start, Parts: SplitHighlight(text)})
		}
		out = append(out, hit)
	}
	return out
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.HTTP.Do(req)
}

func (c *Client) expectOK(resp *http.Response, err error) error {
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("opensearch: %d %s", resp.StatusCode, b)
	}
	return nil
}
