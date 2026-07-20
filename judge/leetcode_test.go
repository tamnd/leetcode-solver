package judge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunAccepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/problems/two-sum/submit/":
			_, _ = w.Write([]byte(`{"submission_id":42}`))
		case "/submissions/detail/42/check/":
			_, _ = w.Write([]byte(`{"state":"SUCCESS","status_code":10,"status_msg":"Accepted","correct_answer":true,"status_runtime":"1 ms"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := LeetCode{BaseURL: server.URL, Session: "s", CSRFToken: "c", PollInterval: time.Millisecond, MaxPolls: 2}
	got, err := client.Run(context.Background(), Request{QuestionID: "1", Slug: "two-sum", Language: "python3", Code: "x", Submit: true})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Accepted || got.SubmissionID != 42 {
		t.Fatalf("%+v", got)
	}
}
