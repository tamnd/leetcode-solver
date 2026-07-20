// Package solver orchestrates independent generation, execution, selection, repair, and publication.
package solver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tamnd/leetcode-solver/artifact"
	"github.com/tamnd/leetcode-solver/judge"
	"github.com/tamnd/leetcode-solver/llm"
	"github.com/tamnd/leetcode-solver/offline"
	"github.com/tamnd/leetcode-solver/problem"
	"github.com/tamnd/leetcode-solver/prompt"
	"github.com/tamnd/leetcode-solver/textguard"
)

type Options struct {
	Model, Language        string
	Candidates, MaxRepairs int
	Force, Submit          bool
}
type Engine struct {
	Client  llm.Completer
	Judge   judge.Judge
	Offline interface {
		Verify(context.Context, string, string, string) (offline.Result, error)
	}
	Prompts  prompt.Builder
	Store    artifact.Store
	Progress func(string)
}

func (e *Engine) Solve(ctx context.Context, p problem.Problem, options Options) (artifact.Result, error) {
	if e.Client == nil || e.Offline == nil {
		return artifact.Result{}, errors.New("solver requires a model client and offline evaluator")
	}
	if options.Submit && e.Judge == nil {
		return artifact.Result{}, errors.New("online submission requested without a judge")
	}
	if options.Model == "" || options.Language == "" {
		return artifact.Result{}, errors.New("model and language are required")
	}
	if options.Candidates <= 0 {
		options.Candidates = 3
	}
	if options.Candidates > 5 {
		return artifact.Result{}, errors.New("candidate count cannot exceed 5")
	}
	if options.MaxRepairs < 0 {
		return artifact.Result{}, errors.New("max repairs cannot be negative")
	}
	if !options.Force {
		if cached, err := e.Store.Load(p.Slug, options.Language); err == nil && cached.Accepted {
			offlineResult, verifyErr := e.Offline.Verify(ctx, p.Slug, options.Language, cached.Code)
			if verifyErr == nil && offlineResult.Passed {
				cached.Offline = offlineResult
				if options.Submit && cached.SubmissionID == 0 {
					run, judgeErr := e.Judge.Run(ctx, judge.Request{QuestionID: p.ID, Slug: p.Slug, Language: options.Language, Code: cached.Code, Submit: true})
					if judgeErr != nil {
						return cached, judgeErr
					}
					cached.Attempts = append(cached.Attempts, artifact.Attempt{Phase: "cached-submit", Judge: &run, At: time.Now().UTC()})
					cached.Accepted = run.Accepted
					cached.SubmissionID = run.SubmissionID
				}
				if cached.Accepted && (!options.Submit || cached.SubmissionID > 0) {
					cached.CompletedAt = time.Now().UTC()
					if err := e.Store.Save(cached); err != nil {
						return cached, err
					}
					return cached, nil
				}
			}
		}
	}
	snippet, ok := p.Snippet(options.Language)
	if !ok {
		return artifact.Result{}, fmt.Errorf("problem has no %q starter code", options.Language)
	}
	result := artifact.Result{Problem: p, Language: options.Language, Model: options.Model, StartedAt: time.Now().UTC()}
	e.log("building candidate-blind reference")
	instructions, input := e.Prompts.Reference(p, options.Language)
	referenceResponse, err := e.complete(ctx, "reference", 0, options.Model, instructions, input)
	if err != nil {
		return result, err
	}
	reference := referenceResponse.Text
	result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "reference", Response: referenceResponse, At: time.Now().UTC()})
	for i := 1; i <= options.Candidates; i++ {
		e.log("generating candidate %d/%d", i, options.Candidates)
		instructions, input = e.Prompts.Candidate(p, snippet, i)
		response, err := e.complete(ctx, "candidate", i, options.Model, instructions, input)
		if err != nil {
			return result, err
		}
		code, explanation, err := textguard.Parse(response.Text)
		if err != nil {
			return result, fmt.Errorf("candidate %d: %w", i, err)
		}
		e.log("running candidate %d against the complete offline suite", i)
		offlineResult, offlineErr := e.Offline.Verify(ctx, p.Slug, options.Language, code)
		if offlineErr != nil {
			offlineResult.Passed = false
			offlineResult.Output = offlineErr.Error()
		}
		candidate := artifact.Candidate{Number: i, Code: code, Explanation: explanation, Response: response, Offline: offlineResult}
		var run judge.Result
		if offlineErr == nil && options.Submit {
			run, err = e.Judge.Run(ctx, judge.Request{QuestionID: p.ID, Slug: p.Slug, Language: options.Language, Code: code, Input: p.ExampleTestcases})
			if err != nil {
				return result, fmt.Errorf("execute candidate %d: %w", i, err)
			}
			candidate.Sample = run
		}
		result.Candidates = append(result.Candidates, candidate)
		result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "candidate", Number: i, Response: response, Judge: &run, At: time.Now().UTC()})
	}
	selected := 1
	if len(result.Candidates) > 1 {
		e.log("selecting strongest candidate")
		instructions, input = e.Prompts.Select(p, reference, result.Candidates)
		response, err := e.complete(ctx, "select", 0, options.Model, instructions, input)
		if err != nil {
			return result, err
		}
		selected = textguard.Selected(response.Text)
		result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "select", Response: response, At: time.Now().UTC()})
		if selected < 1 || selected > len(result.Candidates) {
			return result, errors.New("selector returned no valid candidate")
		}
	}
	current := result.Candidates[selected-1]
	if !current.Offline.Passed {
		for _, candidate := range result.Candidates {
			if candidate.Offline.Passed {
				current = candidate
				break
			}
		}
	}
	for repair := 0; repair <= options.MaxRepairs; repair++ {
		run := judge.Result{Accepted: current.Offline.Passed, Status: "Offline tests passed"}
		if !current.Offline.Passed {
			run.Status = "Offline tests failed"
			run.RuntimeError = current.Offline.Output
		}
		if current.Offline.Passed && options.Submit {
			e.log("verifying candidate against LeetCode hidden tests, pass %d", repair+1)
			run, err = e.Judge.Run(ctx, judge.Request{QuestionID: p.ID, Slug: p.Slug, Language: options.Language, Code: current.Code, Submit: true})
			if err != nil {
				return result, err
			}
		}
		current.Submission = run
		result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "submit", Number: repair, Judge: &run, At: time.Now().UTC()})
		if run.Accepted {
			result.Code = current.Code
			result.Explanation = current.Explanation
			result.Accepted = true
			result.SubmissionID = run.SubmissionID
			result.Offline = current.Offline
			break
		}
		if repair == options.MaxRepairs {
			break
		}
		e.log("repairing after %s", run.Status)
		instructions, input = e.Prompts.Repair(p, snippet, reference, current, run)
		response, err := e.complete(ctx, "repair", repair+1, options.Model, instructions, input)
		if err != nil {
			return result, err
		}
		code, explanation, err := textguard.Parse(response.Text)
		if err != nil {
			return result, fmt.Errorf("repair %d: %w", repair+1, err)
		}
		current = artifact.Candidate{Number: current.Number, Code: code, Explanation: explanation, Response: response}
		offlineResult, offlineErr := e.Offline.Verify(ctx, p.Slug, options.Language, code)
		if offlineErr != nil {
			offlineResult.Passed = false
			offlineResult.Output = offlineErr.Error()
		}
		current.Offline = offlineResult
		result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "repair", Number: repair + 1, Response: response, At: time.Now().UTC()})
	}
	result.CompletedAt = time.Now().UTC()
	if err := e.Store.Save(result); err != nil {
		return result, err
	}
	if !result.Accepted {
		return result, errors.New("solution was not accepted; unpublished audit artifact was saved")
	}
	return result, nil
}

func (e *Engine) complete(ctx context.Context, phase string, number int, model, instructions, input string) (llm.Response, error) {
	r, err := e.Client.Complete(ctx, llm.Request{Model: model, Instructions: instructions, Input: input, Effort: "high", Metadata: map[string]string{"task": "leetcode-" + phase, "candidate": fmt.Sprint(number)}})
	if err != nil {
		return r, fmt.Errorf("%s: %w", phase, err)
	}
	return r, nil
}
func (e *Engine) log(format string, args ...any) {
	if e.Progress != nil {
		e.Progress(fmt.Sprintf(format, args...))
	}
}
