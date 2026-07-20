// Package judge submits candidates to LeetCode's execution service.
package judge

import "context"

type Request struct {
	QuestionID, Slug, Language, Code, Input string
	Submit                                  bool
}
type Result struct {
	Accepted       bool   `json:"accepted"`
	StatusCode     int    `json:"status_code"`
	Status         string `json:"status"`
	Runtime        string `json:"runtime,omitempty"`
	Memory         string `json:"memory,omitempty"`
	Stdout         string `json:"stdout,omitempty"`
	ExpectedOutput string `json:"expected_output,omitempty"`
	CompileError   string `json:"compile_error,omitempty"`
	RuntimeError   string `json:"runtime_error,omitempty"`
	LastTestcase   string `json:"last_testcase,omitempty"`
	SubmissionID   int64  `json:"submission_id,omitempty"`
}

type Judge interface {
	Run(context.Context, Request) (Result, error)
}
