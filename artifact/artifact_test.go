package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/leetcode-solver/problem"
)

func TestStorePublishesOnlyAccepted(t *testing.T) {
	store := Store{Root: t.TempDir()}
	failed := Result{Problem: problem.Problem{Slug: "x"}, Language: "python3", Explanation: "draft"}
	if err := store.Save(failed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "x", "python3.md")); !os.IsNotExist(err) {
		t.Fatal("failed solution was published")
	}
	failed.Accepted = true
	failed.Code = "pass"
	if err := store.Save(failed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "x", "python3.md")); !os.IsNotExist(err) {
		t.Fatal("code that passed execution was published before its article was verified")
	}
	loaded, err := store.Load("x", "python3")
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Accepted || loaded.Verified {
		t.Fatalf("accepted=%t verified=%t", loaded.Accepted, loaded.Verified)
	}
	failed.Verified = true
	if err := store.Save(failed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "x", "python3.md")); err != nil {
		t.Fatalf("a verified solution was not published: %v", err)
	}
}

func TestSaveCombinedRequiresAndPublishesBothLanguages(t *testing.T) {
	store := Store{Root: t.TempDir()}
	p := problem.Problem{FrontendID: "1", Slug: "two-sum", Title: "Two Sum", Difficulty: "Easy", Topics: []problem.Topic{{Name: "Array", Slug: "array"}}}
	python := Result{Problem: p, Language: "python3", Accepted: true, Verified: true, Code: "pass", Explanation: "## Problem Understanding\nP\n\n## Python Solution\n```python\npass\n```\n\n## Worked Examples\nE"}
	golang := Result{Problem: p, Language: "golang", Accepted: true, Verified: true, Code: "func twoSum() {}", Explanation: "## Problem Understanding\nP\n\n## Go Solution\n```go\nfunc twoSum() {}\n```\n\n## Worked Examples\nE"}
	unverified := golang
	unverified.Verified = false
	if _, err := store.SaveCombined(p.Slug, []Result{python, unverified}); err == nil {
		t.Fatal("an unverified implementation was combined into a published article")
	}
	path, err := store.SaveCombined(p.Slug, []Result{python, golang})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"title: \"LeetCode 1 - Two Sum\"", "## Python Solution", "## Go Solution", "func twoSum"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}
