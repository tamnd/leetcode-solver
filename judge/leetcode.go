package judge

import (
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

type LeetCode struct {
	BaseURL, Session, CSRFToken string
	HTTPClient                  *http.Client
	PollInterval                time.Duration
	MaxPolls                    int
}

func (l *LeetCode) Run(ctx context.Context, request Request) (Result, error) {
	if l.Session == "" || l.CSRFToken == "" {
		return Result{}, errors.New("LEETCODE_SESSION and LEETCODE_CSRF_TOKEN are required for execution")
	}
	path := "/problems/" + request.Slug + "/interpret_solution/"
	payload := map[string]any{"lang": request.Language, "question_id": request.QuestionID, "typed_code": request.Code, "data_input": request.Input}
	if request.Submit {
		path = "/problems/" + request.Slug + "/submit/"
		delete(payload, "data_input")
	}
	var queued struct {
		InterpretID  string `json:"interpret_id"`
		SubmissionID int64  `json:"submission_id"`
	}
	if err := l.do(ctx, http.MethodPost, path, payload, &queued); err != nil {
		return Result{}, err
	}
	id := queued.InterpretID
	if id == "" && queued.SubmissionID > 0 {
		id = strconv.FormatInt(queued.SubmissionID, 10)
	}
	if id == "" {
		return Result{}, errors.New("judge returned no execution identifier")
	}
	interval := l.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	polls := l.MaxPolls
	if polls <= 0 {
		polls = 120
	}
	for i := 0; i < polls; i++ {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(interval):
		}
		var raw struct {
			State          string `json:"state"`
			StatusCode     int    `json:"status_code"`
			StatusMsg      string `json:"status_msg"`
			Runtime        string `json:"status_runtime"`
			Memory         string `json:"status_memory"`
			RunSuccess     bool   `json:"run_success"`
			CorrectAnswer  bool   `json:"correct_answer"`
			StdOutput      string `json:"std_output"`
			ExpectedOutput string `json:"expected_output"`
			CompileError   string `json:"compile_error"`
			RuntimeError   string `json:"runtime_error"`
			LastTestcase   string `json:"last_testcase"`
		}
		if err := l.do(ctx, http.MethodGet, "/submissions/detail/"+id+"/check/", nil, &raw); err != nil {
			return Result{}, err
		}
		if raw.State == "PENDING" || raw.State == "STARTED" {
			continue
		}
		submissionID, _ := strconv.ParseInt(id, 10, 64)
		return Result{Accepted: raw.StatusCode == 10 || raw.CorrectAnswer, StatusCode: raw.StatusCode, Status: raw.StatusMsg, Runtime: raw.Runtime, Memory: raw.Memory, Stdout: raw.StdOutput, ExpectedOutput: raw.ExpectedOutput, CompileError: raw.CompileError, RuntimeError: raw.RuntimeError, LastTestcase: raw.LastTestcase, SubmissionID: submissionID}, nil
	}
	return Result{}, errors.New("judge timed out waiting for result")
}

func (l *LeetCode) do(ctx context.Context, method, path string, payload any, target any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	base := strings.TrimRight(l.BaseURL, "/")
	if base == "" {
		base = "https://leetcode.com"
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", base+"/")
	req.Header.Set("X-CSRFToken", l.CSRFToken)
	req.Header.Set("User-Agent", "leetcode-solver/0.1")
	req.AddCookie(&http.Cookie{Name: "LEETCODE_SESSION", Value: l.Session})
	req.AddCookie(&http.Cookie{Name: "csrftoken", Value: l.CSRFToken})
	client := l.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("leetcode judge: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode judge response: %w", err)
	}
	return nil
}
