package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProblem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"question":{"questionId":"1","questionFrontendId":"1","title":"Two Sum","titleSlug":"two-sum","difficulty":"Easy","isPaidOnly":false,"content":"<p>Find pair.</p>","exampleTestcases":"[2,7]\n9","metaData":"{}","hints":[],"topicTags":[{"name":"Array","slug":"array"}],"codeSnippets":[{"lang":"Python3","langSlug":"python3","code":"class Solution: pass"}]}}}`))
	}))
	defer server.Close()
	client := New(server.URL)
	got, err := client.Problem(context.Background(), "two-sum")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "1" || len(got.Snippets) != 1 || got.Snippets[0].LanguageSlug != "python3" {
		t.Fatalf("%+v", got)
	}
}

func TestCatalogPaginates(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		questions := "[]"
		if calls == 1 {
			questions = `[{"questionId":"1","questionFrontendId":"1","title":"A","titleSlug":"a","difficulty":"Easy","isPaidOnly":false,"topicTags":[]}]`
		}
		_, _ = w.Write([]byte(`{"data":{"problemsetQuestionList":{"total":1,"questions":` + questions + `}}}`))
	}))
	defer server.Close()
	items, err := New(server.URL).Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || calls != 1 {
		t.Fatalf("len=%d calls=%d", len(items), calls)
	}
}
