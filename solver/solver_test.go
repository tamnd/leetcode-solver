package solver

import (
	"context"
	"fmt"
	"testing"

	"github.com/tamnd/leetcode-solver/artifact"
	"github.com/tamnd/leetcode-solver/judge"
	"github.com/tamnd/leetcode-solver/llm"
	"github.com/tamnd/leetcode-solver/offline"
	"github.com/tamnd/leetcode-solver/problem"
)

type scriptedLLM struct{ calls int }

func (s *scriptedLLM) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	s.calls++
	switch s.calls {
	case 1:
		return llm.Response{Text: "reference"}, nil
	case 2, 3:
		return llm.Response{Text: validResponse(fmt.Sprint(s.calls))}, nil
	case 4:
		return llm.Response{Text: "SELECTED: 2"}, nil
	default:
		return llm.Response{}, fmt.Errorf("unexpected call %d", s.calls)
	}
}

type acceptingJudge struct{ calls int }

func (j *acceptingJudge) Run(_ context.Context, r judge.Request) (judge.Result, error) {
	j.calls++
	return judge.Result{Accepted: r.Submit, Status: "Accepted", SubmissionID: 99}, nil
}

type passingOffline struct{ calls int }

func (o *passingOffline) Verify(_ context.Context, _, _, _ string) (offline.Result, error) {
	o.calls++
	return offline.Result{Passed: true, TestCount: 100, Dataset: "unit", Revision: "1"}, nil
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
func TestSolveSelectsAndAccepts(t *testing.T) {
	model := &scriptedLLM{}
	runner := &acceptingJudge{}
	offlineRunner := &passingOffline{}
	engine := Engine{Client: model, Judge: runner, Offline: offlineRunner, Store: artifact.Store{Root: t.TempDir()}}
	p := problem.Problem{ID: "1", FrontendID: "1", Slug: "x", Title: "X", Snippets: []problem.CodeSnippet{{Language: "Python3", LanguageSlug: "python3", Code: "starter"}}}
	got, err := engine.Solve(context.Background(), p, Options{Model: "m", Language: "python3", Candidates: 2, MaxRepairs: 0, Force: true, Submit: true})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Accepted || got.Code != "3" || runner.calls != 3 || offlineRunner.calls != 2 {
		t.Fatalf("%+v calls=%d", got, runner.calls)
	}
}

func TestSolveReverifiesCachedCandidate(t *testing.T) {
	store := artifact.Store{Root: t.TempDir()}
	p := problem.Problem{ID: "1", FrontendID: "1", Slug: "x", Title: "X"}
	cached := artifact.Result{Problem: p, Language: "python3", Model: "m", Code: "cached", Explanation: "accepted", Accepted: true}
	if err := store.Save(cached); err != nil {
		t.Fatal(err)
	}
	model := &scriptedLLM{}
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

func TestSolveSubmitsCachedCandidateWhenRequested(t *testing.T) {
	store := artifact.Store{Root: t.TempDir()}
	p := problem.Problem{ID: "1", FrontendID: "1", Slug: "x", Title: "X"}
	cached := artifact.Result{Problem: p, Language: "python3", Model: "m", Code: "cached", Explanation: "accepted", Accepted: true}
	if err := store.Save(cached); err != nil {
		t.Fatal(err)
	}
	model := &scriptedLLM{}
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
