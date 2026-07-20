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
	"strings"
	"time"
)

// ChatComplete is used by the isolated agent adapter because both the Zen API
// and the Codex-subscription bridge expose chat completions. It accepts either
// an ordinary JSON response or an SSE stream, regardless of the requested mode.
func (c *Client) ChatComplete(ctx context.Context, model, system, user string) (string, error) {
	payload := map[string]any{"model": model, "stream": true, "stream_options": map[string]bool{"include_usage": true}, "messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	url += "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return "", fmt.Errorf("model endpoint: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
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
