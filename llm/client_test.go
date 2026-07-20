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
