// Package llm implements a small OpenAI-compatible Responses API client.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Request struct {
	Model, Instructions, Input, Effort string
	Metadata                           map[string]string
}

// Usage separates cached, cache-write, and reasoning tokens from the plain
// input and output counts because they are priced differently. A record that
// reports only three numbers cannot be turned into a cost afterwards.
type Usage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int `json:"cache_write_tokens,omitempty"`
	OutputTokens      int `json:"output_tokens"`
	ReasoningTokens   int `json:"reasoning_tokens,omitempty"`
	TotalTokens       int `json:"total_tokens"`
}

// Normalized fills a total an endpoint omitted and clamps detail that would
// otherwise imply more cached or reasoning tokens than there were tokens.
func (u Usage) Normalized() Usage {
	u.InputTokens = max(0, u.InputTokens)
	u.OutputTokens = max(0, u.OutputTokens)
	u.CachedInputTokens = min(max(0, u.CachedInputTokens), u.InputTokens)
	u.CacheWriteTokens = min(max(0, u.CacheWriteTokens), u.InputTokens-u.CachedInputTokens)
	u.ReasoningTokens = min(max(0, u.ReasoningTokens), u.OutputTokens)
	if u.TotalTokens <= 0 {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	return u
}

// UncachedInputTokens is the input billed at the full rate.
func (u Usage) UncachedInputTokens() int {
	u = u.Normalized()
	return u.InputTokens - u.CachedInputTokens - u.CacheWriteTokens
}

type Response struct {
	ID, Model, Text string
	Usage           Usage
}
type Completer interface {
	Complete(context.Context, Request) (Response, error)
}

type Client struct {
	BaseURL, APIKey string
	HTTPClient      *http.Client
}

func (c *Client) Complete(ctx context.Context, request Request) (Response, error) {
	if request.Model == "" {
		return Response{}, errors.New("model is required")
	}
	payload := map[string]any{"model": request.Model, "instructions": request.Instructions, "input": request.Input, "reasoning": map[string]string{"effort": request.Effort}, "metadata": request.Metadata}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	url := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	url += "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return Response{}, fmt.Errorf("model endpoint: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var raw struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		// The detail lives under nested objects on the official wire and at the
		// top level on some compatible proxies, so both are read.
		Usage struct {
			Usage
			InputDetails struct {
				CachedTokens     int `json:"cached_tokens"`
				CacheWriteTokens int `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Response{}, err
	}
	usage := raw.Usage.Usage
	if usage.CachedInputTokens == 0 {
		usage.CachedInputTokens = raw.Usage.InputDetails.CachedTokens
	}
	if usage.CacheWriteTokens == 0 {
		usage.CacheWriteTokens = raw.Usage.InputDetails.CacheWriteTokens
	}
	if usage.ReasoningTokens == 0 {
		usage.ReasoningTokens = raw.Usage.OutputDetails.ReasoningTokens
	}
	text := raw.OutputText
	if text == "" {
		var parts []string
		for _, item := range raw.Output {
			for _, content := range item.Content {
				if content.Type == "output_text" {
					parts = append(parts, content.Text)
				}
			}
		}
		text = strings.Join(parts, "\n")
	}
	if strings.TrimSpace(text) == "" {
		return Response{}, errors.New("model returned no output text")
	}
	return Response{ID: raw.ID, Model: raw.Model, Text: text, Usage: usage.Normalized()}, nil
}
