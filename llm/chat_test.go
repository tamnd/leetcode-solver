package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The chat gateway shape is what the solver actually has to send: standing
// rules as a system message, the prompt as a user message, and effort where a
// chat endpoint expects it. Usage arrives under different names than the
// Responses API uses, and losing it would make a run uncostable.
func TestChatClientSendsMessagesAndReadsChatUsage(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"id":"c1","model":"m","choices":[{"finish_reason":"stop","message":{"content":"answer"}}],
			"usage":{"prompt_tokens":100,"completion_tokens":40,"total_tokens":140,
			"prompt_cache_hit_tokens":60,"cache_creation_input_tokens":10,
			"completion_tokens_details":{"reasoning_tokens":25}}}`))
	}))
	defer server.Close()
	response, err := (&ChatClient{BaseURL: server.URL, APIKey: "secret"}).Complete(context.Background(),
		Request{Model: "m", Instructions: "be exact", Input: "solve it", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "answer" {
		t.Errorf("text = %q", response.Text)
	}
	want := Usage{InputTokens: 100, CachedInputTokens: 60, CacheWriteTokens: 10, OutputTokens: 40, ReasoningTokens: 25, TotalTokens: 140}
	if response.Usage != want {
		t.Errorf("usage = %+v, want %+v", response.Usage, want)
	}
	if got["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v", got["reasoning_effort"])
	}
	messages, ok := got["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %v", got["messages"])
	}
	first, second := messages[0].(map[string]any), messages[1].(map[string]any)
	if first["role"] != "system" || first["content"] != "be exact" {
		t.Errorf("system message = %v", first)
	}
	if second["role"] != "user" || second["content"] != "solve it" {
		t.Errorf("user message = %v", second)
	}
}

// A model that spends its whole budget reasoning returns an empty message. That
// has to name the limit rather than read as an endpoint failure.
func TestChatClientNamesAnEmptyAnswersFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"length","message":{"content":""}}]}`))
	}))
	defer server.Close()
	_, err := (&ChatClient{BaseURL: server.URL}).Complete(context.Background(), Request{Model: "m", Input: "hi"})
	if err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatalf("err = %v, want the finish reason named", err)
	}
}

// A truncated article fails the structural guards with a missing-section error
// that hides the output cap actually responsible.
func TestChatClientReportsTruncationRatherThanAShortAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"length","message":{"content":"## Problem Under"}}],
			"usage":{"prompt_tokens":10,"completion_tokens":32000,"total_tokens":32010}}`))
	}))
	defer server.Close()
	_, err := (&ChatClient{BaseURL: server.URL}).Complete(context.Background(), Request{Model: "m", Input: "hi"})
	if err == nil || !strings.Contains(err.Error(), "truncated") || !strings.Contains(err.Error(), "32000") {
		t.Fatalf("err = %v, want the truncation and the count named", err)
	}
}

// A shared gateway answers a burst with 429. Abandoning the solve on the first
// one throws away every call it already paid for, while retrying a rejection
// that cannot change wastes the quota it still has.
func TestPostJSONRetriesOnlyWhatCanChange(t *testing.T) {
	// Retry-After is zero throughout so the assertions are about the decision to
	// retry, not about the length of the backoff.
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		retries    int
		wantCalls  int
		wantOK     bool
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "0", retries: 1, wantCalls: 2, wantOK: true},
		{name: "gateway busy", status: http.StatusBadGateway, retryAfter: "0", retries: 1, wantCalls: 2, wantOK: true},
		{name: "rate limited with no retries left", status: http.StatusTooManyRequests, retryAfter: "0", wantCalls: 1},
		{name: "unknown model", status: http.StatusBadRequest, retries: 1, wantCalls: 1},
		{name: "bad key", status: http.StatusUnauthorized, retries: 1, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls > 1 {
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
					return
				}
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"error":"nope"}`))
			}))
			defer server.Close()
			client := &ChatClient{BaseURL: server.URL, MaxRetries: test.retries}
			_, err := client.Complete(context.Background(), Request{Model: "m", Input: "hi"})
			if test.wantOK != (err == nil) {
				t.Errorf("err = %v, want ok = %v", err, test.wantOK)
			}
			if calls != test.wantCalls {
				t.Errorf("calls = %d, want %d", calls, test.wantCalls)
			}
		})
	}
}

func TestRetryAfterReadsOnlySeconds(t *testing.T) {
	if d, ok := retryAfter("12"); !ok || d != 12*time.Second {
		t.Errorf("12 -> %v %v", d, ok)
	}
	if d, ok := retryAfter("100000"); !ok || d != 2*time.Minute {
		t.Errorf("a huge delay must be capped, got %v", d)
	}
	// An HTTP-date is not guessed at; the caller falls back to its own backoff.
	if _, ok := retryAfter("Wed, 21 Oct 2026 07:28:00 GMT"); ok {
		t.Error("an HTTP-date was accepted as seconds")
	}
}

func TestChatCompleteJSONAndSSE(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("path %s", r.URL.Path)
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					if _, err := fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\ndata: [DONE]\n\n"); err != nil {
						t.Errorf("write SSE response: %v", err)
					}
				} else {
					w.Header().Set("Content-Type", "application/json")
					if _, err := fmt.Fprint(w, `{"choices":[{"message":{"content":"hello world"}}]}`); err != nil {
						t.Errorf("write JSON response: %v", err)
					}
				}
			}))
			defer srv.Close()
			got, err := (&Client{BaseURL: srv.URL}).ChatComplete(context.Background(), "m", "s", "u")
			if err != nil {
				t.Fatal(err)
			}
			if got != "hello world" {
				t.Fatalf("got %q", got)
			}
		})
	}
}
