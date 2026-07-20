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
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
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
		Usage Usage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Response{}, err
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
	return Response{ID: raw.ID, Model: raw.Model, Text: text, Usage: raw.Usage}, nil
}
