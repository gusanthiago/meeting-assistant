package search

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSplitHighlight(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Part
	}{
		{"no match", "plain text", []Part{{Text: "plain text"}}},
		{"middle", "send the \u0001report\u0002 today", []Part{{Text: "send the "}, {Text: "report", Match: true}, {Text: " today"}}},
		{"start and end", "\u0001ship\u0002 it \u0001now\u0002", []Part{{Text: "ship", Match: true}, {Text: " it "}, {Text: "now", Match: true}}},
		{"adjacent matches merge", "\u0001a\u0002\u0001b\u0002", []Part{{Text: "ab", Match: true}}},
		{"unclosed tag", "x \u0001y", []Part{{Text: "x "}, {Text: "y", Match: true}}},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SplitHighlight(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestQueryAlwaysFiltersByWorkspace(t *testing.T) {
	ws := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	tests := []struct {
		name  string
		q     string
		limit int
	}{
		{"single word", "roadmap", 10},
		{"phrase", "release notes", 5},
		{"quotes", `"migration" window`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(Query(ws, tt.q, tt.limit))
			if err != nil {
				t.Fatal(err)
			}
			s := string(b)
			if !strings.Contains(s, `"filter":[{"term":{"workspace_id":"`+ws.String()+`"}}]`) {
				t.Errorf("workspace filter missing: %s", s)
			}
			if !strings.Contains(s, `"path":"segments"`) || !strings.Contains(s, `"inner_hits"`) {
				t.Errorf("nested transcript query missing: %s", s)
			}
		})
	}
}

func TestParseHits(t *testing.T) {
	raw := `{"hits":{"hits":[{"_id":"01900000-0000-7000-8000-0000000000aa","_source":{"title":"Weekly sync"},
	  "highlight":{"title":["Weekly \u0001sync\u0002"]},
	  "inner_hits":{"segments":{"hits":{"hits":[{"_source":{"speaker":"Ana","start_ms":42000,"end_ms":45000,"text":"ship the sync fix"},
	    "highlight":{"segments.text":["ship the \u0001sync\u0002 fix"]}}]}}}},
	  {"_id":"not-a-uuid","_source":{"title":"skip"}}]}}`
	var r searchResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	hits := r.hits()
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	tests := []struct {
		field   string
		speaker string
		startMs int64
	}{
		{"title", "", 0},
		{"transcript", "Ana", 42000},
	}
	for i, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			s := hits[0].Snippets[i]
			if s.Field != tt.field || s.Speaker != tt.speaker {
				t.Errorf("snippet = %+v", s)
			}
			if tt.startMs > 0 && (s.StartMs == nil || *s.StartMs != tt.startMs) {
				t.Errorf("start = %v, want %d", s.StartMs, tt.startMs)
			}
		})
	}
}

func TestEnsureIndex(t *testing.T) {
	v1 := `{"meetings-v1":{"aliases":{"meetings":{}}}}`
	tests := []struct {
		name        string
		aliased     string
		v2Exists    bool
		wantCalls   []string
		wantRemoved bool
	}{
		{"fresh install", "", false, []string{"HEAD /meetings-v2", "PUT /meetings-v2", "POST /_aliases"}, false},
		{"upgrade copies v1 and drops it in the swap", v1, false, []string{"HEAD /meetings-v2", "PUT /meetings-v2", "POST /_reindex", "POST /_aliases"}, true},
		{"interrupted upgrade reuses v2", v1, true, []string{"HEAD /meetings-v2", "POST /_reindex", "POST /_aliases"}, true},
		{"already current", `{"meetings-v2":{"aliases":{"meetings":{}}}}`, true, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			var aliases string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := r.Method + " " + r.URL.Path
				switch {
				case call == "GET /_alias/meetings":
					if tt.aliased == "" {
						w.WriteHeader(http.StatusNotFound)
					} else {
						_, _ = io.WriteString(w, tt.aliased)
					}
					return
				case call == "HEAD /meetings-v2" && !tt.v2Exists:
					w.WriteHeader(http.StatusNotFound)
				case call == "POST /_reindex":
					_, _ = io.WriteString(w, `{"failures":[]}`)
				case call == "POST /_aliases":
					b, _ := io.ReadAll(r.Body)
					aliases = string(b)
				}
				calls = append(calls, call)
			}))
			defer srv.Close()

			c := &Client{BaseURL: srv.URL, Alias: "meetings", HTTP: srv.Client()}
			if err := c.EnsureIndex(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(calls, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", calls, tt.wantCalls)
			}
			if removed := strings.Contains(aliases, `"remove_index":{"index":"meetings-v1"}`); removed != tt.wantRemoved {
				t.Errorf("aliases body = %s", aliases)
			}
		})
	}
}
