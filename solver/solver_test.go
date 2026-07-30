package solver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/leetcode-solver/artifact"
	"github.com/tamnd/leetcode-solver/judge"
	"github.com/tamnd/leetcode-solver/llm"
	"github.com/tamnd/leetcode-solver/offline"
	"github.com/tamnd/leetcode-solver/problem"
)

// scriptedLLM answers per phase rather than per call index, so a test states which
// stage it is scripting and stays readable when a stage is added.
type scriptedLLM struct {
	calls  int
	script map[string][]string
	seen   map[string]int
}

func newScriptedLLM(script map[string][]string) *scriptedLLM {
	return &scriptedLLM{script: script, seen: map[string]int{}}
}

func (s *scriptedLLM) Complete(_ context.Context, r llm.Request) (llm.Response, error) {
	s.calls++
	phase := strings.TrimPrefix(r.Metadata["task"], "leetcode-")
	index := s.seen[phase]
	s.seen[phase]++
	queue := s.script[phase]
	if index >= len(queue) {
		return llm.Response{}, fmt.Errorf("unscripted %s call %d", phase, index+1)
	}
	return llm.Response{Text: queue[index]}, nil
}

type acceptingJudge struct{ calls int }

func (j *acceptingJudge) Run(_ context.Context, r judge.Request) (judge.Result, error) {
	j.calls++
	return judge.Result{Accepted: r.Submit, Status: "Accepted", SubmissionID: 99}, nil
}

type passingOffline struct{ calls int }

func (o *passingOffline) Verify(_ context.Context, _, _, _ string) (offline.Result, error) {
	o.calls++
	return offline.Result{Passed: true, TestCount: 100, AssertionsRun: 100, Dataset: "unit", Revision: "1"}, nil
}

// codeGatedOffline passes only the candidate bodies it was told to pass.
type codeGatedOffline struct {
	passing map[string]bool
	seen    []string
}

func (o *codeGatedOffline) Verify(_ context.Context, _, _, code string) (offline.Result, error) {
	o.seen = append(o.seen, code)
	if !o.passing[code] {
		return offline.Result{Passed: false, Output: "assertion failed"}, nil
	}
	return offline.Result{Passed: true, TestCount: 100, AssertionsRun: 100, Dataset: "unit", Revision: "1"}, nil
}

func validResponse(code string) string {
	return `<CODE>` + code + `</CODE><SOLUTION>
## Problem Understanding
x
## Approaches
x
## Approach Comparison
x
## Algorithm Walkthrough
x
### Why it works
x
` + "```python\n" + code + "\n```" + `
## Worked Examples
x
## Complexity Analysis
x
## Test Cases
x
## Edge Cases
x
</SOLUTION>`
}

func pythonProblem() problem.Problem {
	return problem.Problem{ID: "1", FrontendID: "1", Slug: "x", Title: "X", Snippets: []problem.CodeSnippet{{Language: "Python3", LanguageSlug: "python3", Code: "starter"}}}
}

func TestSolveSelectsAndAccepts(t *testing.T) {
	model := newScriptedLLM(map[string][]string{
		"reference":     {"reference"},
		"candidate":     {validResponse("2"), validResponse("3")},
		"select":        {"SELECTED: 2"},
		"criteria":      {"no defects\nVERDICT: PASS"},
		"falsification": {"every attack failed\nVERDICT: PASS"},
	})
	runner := &acceptingJudge{}
	offlineRunner := &passingOffline{}
	root := t.TempDir()
	engine := Engine{Client: model, Judge: runner, Offline: offlineRunner, Store: artifact.Store{Root: root}}
	got, err := engine.Solve(context.Background(), pythonProblem(), Options{Model: "m", Language: "python3", Candidates: 2, Force: true, Submit: true})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Accepted || !got.Verified || got.Code != "3" || runner.calls != 3 || offlineRunner.calls != 2 {
		t.Fatalf("%+v judge_calls=%d offline_calls=%d", got, runner.calls, offlineRunner.calls)
	}
	if len(got.Quality) != 2 || !got.Quality[0].Pass || !got.Quality[1].Pass {
		t.Fatalf("quality=%+v", got.Quality)
	}
	if _, err := os.Stat(filepath.Join(root, "x", "python3.md")); err != nil {
		t.Fatalf("verified solution was not published: %v", err)
	}
}

func TestSolveWithholdsPublicationWhenAJudgeRejectsTheArticle(t *testing.T) {
	model := newScriptedLLM(map[string][]string{
		"reference":     {"reference"},
		"candidate":     {validResponse("2")},
		"criteria":      {"the stated complexity is wrong\nVERDICT: FAIL"},
		"falsification": {"no defect found\nVERDICT: PASS"},
	})
	root := t.TempDir()
	engine := Engine{Client: model, Offline: &passingOffline{}, Store: artifact.Store{Root: root}}
	got, err := engine.Solve(context.Background(), pythonProblem(), Options{Model: "m", Language: "python3", Candidates: 1, Force: true})
	if err == nil {
		t.Fatal("expected a rejected article to fail the solve")
	}
	if !got.Accepted || got.Verified {
		t.Fatalf("accepted=%t verified=%t", got.Accepted, got.Verified)
	}
	if _, err := os.Stat(filepath.Join(root, "x", "python3.md")); !os.IsNotExist(err) {
		t.Fatal("an article rejected by a quality judge was published")
	}
	if _, err := os.Stat(filepath.Join(root, "x", "python3.json")); err != nil {
		t.Fatalf("the audit record was not saved: %v", err)
	}
}

func TestSolveTreatsAMissingVerdictAsRejection(t *testing.T) {
	model := newScriptedLLM(map[string][]string{
		"reference":     {"reference"},
		"candidate":     {validResponse("2")},
		"criteria":      {"this article looks excellent to me"},
		"falsification": {"VERDICT: PASS"},
	})
	engine := Engine{Client: model, Offline: &passingOffline{}, Store: artifact.Store{Root: t.TempDir()}}
	got, _ := engine.Solve(context.Background(), pythonProblem(), Options{Model: "m", Language: "python3", Candidates: 1, Force: true})
	if got.Verified {
		t.Fatal("a judge that stated no verdict was treated as a pass")
	}
}

func TestSolveRewritesTheArticleAndRejudgesIt(t *testing.T) {
	model := newScriptedLLM(map[string][]string{
		"reference":     {"reference"},
		"candidate":     {validResponse("2")},
		"criteria":      {"example 2 is traced wrongly\nVERDICT: FAIL", "VERDICT: PASS"},
		"falsification": {"VERDICT: PASS", "VERDICT: PASS"},
		"rewrite":       {validResponse("2")},
	})
	engine := Engine{Client: model, Offline: &passingOffline{}, Store: artifact.Store{Root: t.TempDir()}}
	got, err := engine.Solve(context.Background(), pythonProblem(), Options{Model: "m", Language: "python3", Candidates: 1, MaxCorrections: 1, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Verified || model.seen["rewrite"] != 1 || model.seen["criteria"] != 2 {
		t.Fatalf("verified=%t rewrites=%d criteria=%d", got.Verified, model.seen["rewrite"], model.seen["criteria"])
	}
}

func TestSolveDiscardsARewriteWhoseCodeNoLongerPasses(t *testing.T) {
	model := newScriptedLLM(map[string][]string{
		"reference":     {"reference"},
		"candidate":     {validResponse("good")},
		"criteria":      {"VERDICT: FAIL"},
		"falsification": {"VERDICT: PASS"},
		"rewrite":       {validResponse("regressed")},
	})
	offlineRunner := &codeGatedOffline{passing: map[string]bool{"good": true}}
	engine := Engine{Client: model, Offline: offlineRunner, Store: artifact.Store{Root: t.TempDir()}}
	got, _ := engine.Solve(context.Background(), pythonProblem(), Options{Model: "m", Language: "python3", Candidates: 1, MaxCorrections: 1, Force: true})
	if got.Verified {
		t.Fatal("a rewrite that broke execution was accepted")
	}
	if got.Code != "good" {
		t.Fatalf("unverified code replaced the executed code: %q", got.Code)
	}
}

func TestSolveOffersOnlyExecutedCandidatesToTheSelector(t *testing.T) {
	model := newScriptedLLM(map[string][]string{
		"reference":     {"reference"},
		"candidate":     {validResponse("broken"), validResponse("works")},
		"criteria":      {"VERDICT: PASS"},
		"falsification": {"VERDICT: PASS"},
	})
	offlineRunner := &codeGatedOffline{passing: map[string]bool{"works": true}}
	engine := Engine{Client: model, Offline: offlineRunner, Store: artifact.Store{Root: t.TempDir()}}
	got, err := engine.Solve(context.Background(), pythonProblem(), Options{Model: "m", Language: "python3", Candidates: 2, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	// One candidate survived execution, so no selection call is scripted or needed.
	if got.Code != "works" || model.seen["select"] != 0 {
		t.Fatalf("code=%q select_calls=%d", got.Code, model.seen["select"])
	}
}

func TestSolveReverifiesCachedCandidate(t *testing.T) {
	store := artifact.Store{Root: t.TempDir()}
	p := pythonProblem()
	cached := artifact.Result{Problem: p, Language: "python3", Model: "m", Code: "cached", Explanation: "accepted", Accepted: true, Verified: true}
	if err := store.Save(cached); err != nil {
		t.Fatal(err)
	}
	model := newScriptedLLM(nil)
	offlineRunner := &passingOffline{}
	engine := Engine{Client: model, Offline: offlineRunner, Store: store}
	got, err := engine.Solve(context.Background(), p, Options{Model: "m", Language: "python3"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "cached" || offlineRunner.calls != 1 || model.calls != 0 {
		t.Fatalf("got=%+v offline_calls=%d model_calls=%d", got, offlineRunner.calls, model.calls)
	}
}

func TestSolveResolvesACachedRecordThatWasNeverVerified(t *testing.T) {
	store := artifact.Store{Root: t.TempDir()}
	p := pythonProblem()
	if err := store.Save(artifact.Result{Problem: p, Language: "python3", Model: "m", Code: "cached", Explanation: "draft", Accepted: true}); err != nil {
		t.Fatal(err)
	}
	model := newScriptedLLM(map[string][]string{
		"reference":     {"reference"},
		"candidate":     {validResponse("fresh")},
		"criteria":      {"VERDICT: PASS"},
		"falsification": {"VERDICT: PASS"},
	})
	engine := Engine{Client: model, Offline: &passingOffline{}, Store: store}
	got, err := engine.Solve(context.Background(), p, Options{Model: "m", Language: "python3", Candidates: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "fresh" || !got.Verified {
		t.Fatalf("an accepted but unverified record short-circuited the solve: %+v", got)
	}
}

func TestSolveSubmitsCachedCandidateWhenRequested(t *testing.T) {
	store := artifact.Store{Root: t.TempDir()}
	p := pythonProblem()
	cached := artifact.Result{Problem: p, Language: "python3", Model: "m", Code: "cached", Explanation: "accepted", Accepted: true, Verified: true}
	if err := store.Save(cached); err != nil {
		t.Fatal(err)
	}
	model := newScriptedLLM(nil)
	offlineRunner := &passingOffline{}
	runner := &acceptingJudge{}
	engine := Engine{Client: model, Judge: runner, Offline: offlineRunner, Store: store}
	got, err := engine.Solve(context.Background(), p, Options{Model: "m", Language: "python3", Submit: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.SubmissionID != 99 || offlineRunner.calls != 1 || runner.calls != 1 || model.calls != 0 {
		t.Fatalf("got=%+v offline_calls=%d judge_calls=%d model_calls=%d", got, offlineRunner.calls, runner.calls, model.calls)
	}
}
