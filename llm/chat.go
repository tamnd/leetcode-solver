package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// postJSON sends one request to an OpenAI-compatible endpoint and returns the
// body only for a success, so no caller has to remember that an error status
// carries an explanation that must be read before the body is closed.
//
// A shared gateway answers a burst with 429 and a busy one with 502, and a
// multi-call solve that gives up on the first of those throws away every call
// it already paid for. Those statuses are retried up to retries times, honoring
// Retry-After when the endpoint states it; a rejection that will not change on
// a second attempt, such as an unknown model or a bad key, is returned at once.
func postJSON(ctx context.Context, hc *http.Client, baseURL, apiKey, path string, payload any, retries int) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Minute}
	}
	var last error
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := hc.Do(req)
		switch {
		case err != nil:
			last = err
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return resp, nil
		default:
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			_ = resp.Body.Close()
			last = fmt.Errorf("model endpoint: %s: %s", resp.Status, strings.TrimSpace(string(data)))
			if !retryableStatus(resp.StatusCode) {
				return nil, last
			}
			if wait, ok := retryAfter(resp.Header.Get("Retry-After")); ok && attempt < retries {
				if err := sleep(ctx, wait); err != nil {
					return nil, err
				}
				continue
			}
		}
		if attempt >= retries {
			return nil, last
		}
		if err := sleep(ctx, time.Duration(attempt+1)*2*time.Second); err != nil {
			return nil, err
		}
	}
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}

// retryAfter reads the delay-seconds form. An HTTP-date is ignored rather than
// guessed at, and the caller then falls back to its own backoff.
func retryAfter(value string) (time.Duration, bool) {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < 0 {
		return 0, false
	}
	return min(time.Duration(seconds)*time.Second, 2*time.Minute), true
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ChatClient speaks the chat-completions API. Most compatible gateways
// implement only that endpoint and answer a Responses request with an upstream
// failure, so a solver that spoke one shape could not use them at all.
type ChatClient struct {
	BaseURL, APIKey string
	HTTPClient      *http.Client
	// MaxRetries bounds retries of a rate-limited or briefly failing endpoint.
	MaxRetries int
}

var _ Completer = (*ChatClient)(nil)

func (c *ChatClient) Complete(ctx context.Context, request Request) (Response, error) {
	if request.Model == "" {
		return Response{}, errors.New("model is required")
	}
	// Instructions become the system message and the prompt the user message.
	// Concatenating them into one user turn would drop the standing rules that
	// every prompt in this repository relies on being separately addressed.
	var messages []map[string]string
	if strings.TrimSpace(request.Instructions) != "" {
		messages = append(messages, map[string]string{"role": "system", "content": request.Instructions})
	}
	messages = append(messages, map[string]string{"role": "user", "content": request.Input})
	payload := map[string]any{"model": request.Model, "messages": messages}
	if request.Effort != "" {
		payload["reasoning_effort"] = request.Effort
	}
	if len(request.Metadata) > 0 {
		payload["metadata"] = request.Metadata
	}
	resp, err := postJSON(ctx, c.HTTPClient, c.BaseURL, c.APIKey, "/chat/completions", payload, c.MaxRetries)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		// Chat usage names the same quantities differently from the Responses API,
		// and gateways add their own cache fields, so each is read where it is
		// actually published rather than assumed.
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
			CacheWriteTokens int `json:"cache_creation_input_tokens"`
			CacheHitTokens   int `json:"prompt_cache_hit_tokens"`
			PromptDetails    struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Response{}, err
	}
	if len(raw.Choices) == 0 {
		return Response{}, errors.New("model returned no choices")
	}
	text := raw.Choices[0].Message.Content
	reason := raw.Choices[0].FinishReason
	if strings.TrimSpace(text) == "" {
		// A model that spent its entire output budget thinking says so in the finish
		// reason, and naming it separates a raisable limit from an apparent outage.
		if reason != "" && reason != "stop" {
			return Response{}, fmt.Errorf("model returned no output text, finish reason %s", reason)
		}
		return Response{}, errors.New("model returned no output text")
	}
	// A truncated answer cannot satisfy any structural contract, and the parse
	// error it causes names a missing section instead of the output cap that is
	// the real cause. Reporting the cap is the difference between "this model
	// cannot write the article" and "this model was cut off mid-article".
	if reason == "length" {
		return Response{}, fmt.Errorf("model output truncated at %d output tokens", raw.Usage.CompletionTokens)
	}
	usage := Usage{
		InputTokens:       raw.Usage.PromptTokens,
		CachedInputTokens: raw.Usage.PromptDetails.CachedTokens,
		CacheWriteTokens:  raw.Usage.CacheWriteTokens,
		OutputTokens:      raw.Usage.CompletionTokens,
		ReasoningTokens:   raw.Usage.CompletionDetails.ReasoningTokens,
		TotalTokens:       raw.Usage.TotalTokens,
	}
	if usage.CachedInputTokens == 0 {
		usage.CachedInputTokens = raw.Usage.CacheHitTokens
	}
	return Response{ID: raw.ID, Model: raw.Model, Text: text, Usage: usage.Normalized()}, nil
}

// ChatComplete is used by the isolated agent adapter because both the Zen API
// and the Codex-subscription bridge expose chat completions. It accepts either
// an ordinary JSON response or an SSE stream, regardless of the requested mode.
func (c *Client) ChatComplete(ctx context.Context, model, system, user string) (string, error) {
	payload := map[string]any{"model": model, "stream": true, "stream_options": map[string]bool{"include_usage": true}, "messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}}
	resp, err := postJSON(ctx, c.HTTPClient, c.BaseURL, c.APIKey, "/chat/completions", payload, c.MaxRetries)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readChatSSE(resp.Body)
	}
	var v struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	var out strings.Builder
	for _, c := range v.Choices {
		out.WriteString(c.Message.Content)
		out.WriteString(c.Delta.Content)
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", errors.New("model returned no chat text")
	}
	return out.String(), nil
}

func readChatSSE(r io.Reader) (string, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64<<10), 4<<20)
	var out strings.Builder
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var v struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &v) != nil {
			continue
		}
		for _, c := range v.Choices {
			out.WriteString(c.Delta.Content)
			out.WriteString(c.Message.Content)
		}
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", errors.New("model returned no streamed chat text")
	}
	return out.String(), nil
}
