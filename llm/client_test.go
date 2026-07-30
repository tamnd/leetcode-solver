package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("missing auth")
		}
		_, _ = w.Write([]byte(`{"id":"r1","model":"test","output_text":"answer","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`))
	}))
	defer server.Close()
	got, err := (&Client{BaseURL: server.URL, APIKey: "secret"}).Complete(context.Background(), Request{Model: "test", Input: "hi", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "answer" || got.Usage.TotalTokens != 5 {
		t.Fatalf("%+v", got)
	}
}

// Cached, cache-write, and reasoning tokens are priced differently from plain
// input and output, and endpoints publish them in two different shapes. A run
// that records only the three headline numbers cannot be costed afterwards.
func TestCompleteReadsTokenDetailInBothShapes(t *testing.T) {
	bodies := map[string]string{
		"nested details": `{"output_text":"a","usage":{"input_tokens":100,"output_tokens":40,"total_tokens":140,
			"input_tokens_details":{"cached_tokens":60,"cache_write_tokens":10},
			"output_tokens_details":{"reasoning_tokens":25}}}`,
		"flat proxy fields": `{"output_text":"a","usage":{"input_tokens":100,"cached_input_tokens":60,
			"cache_write_tokens":10,"output_tokens":40,"reasoning_tokens":25,"total_tokens":140}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			got, err := (&Client{BaseURL: server.URL}).Complete(context.Background(), Request{Model: "test", Input: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			want := Usage{InputTokens: 100, CachedInputTokens: 60, CacheWriteTokens: 10, OutputTokens: 40, ReasoningTokens: 25, TotalTokens: 140}
			if got.Usage != want {
				t.Errorf("usage = %+v, want %+v", got.Usage, want)
			}
			if got.Usage.UncachedInputTokens() != 30 {
				t.Errorf("uncached input = %d, want 30", got.Usage.UncachedInputTokens())
			}
		})
	}
}

// A proxy that omits the total, or reports impossible detail, must not turn
// into a negative billable count.
func TestNormalizedRepairsMissingAndImpossibleCounts(t *testing.T) {
	got := Usage{InputTokens: 10, OutputTokens: 5}.Normalized()
	if got.TotalTokens != 15 {
		t.Errorf("total = %d, want the sum 15", got.TotalTokens)
	}
	got = Usage{InputTokens: 10, CachedInputTokens: 99, CacheWriteTokens: 99, OutputTokens: 5, ReasoningTokens: 99}.Normalized()
	if got.CachedInputTokens != 10 || got.CacheWriteTokens != 0 || got.ReasoningTokens != 5 {
		t.Errorf("clamped usage = %+v", got)
	}
	if got.UncachedInputTokens() != 0 {
		t.Errorf("uncached input = %d, want 0 and never negative", got.UncachedInputTokens())
	}
}
