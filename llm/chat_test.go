package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
