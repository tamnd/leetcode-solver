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
	// MaxCorrections bounds article rewrites after a quality judge fails. Zero means
	// the judges still run and still gate publication; they simply get one chance.
	MaxCorrections int
	Force, Submit  bool
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
		// Cached work is reused only when the stored record is publishable, so an
		// artifact whose code passed but whose article was rejected is solved again
		// instead of being quietly promoted on a later run.
		if cached, err := e.Store.Load(p.Slug, options.Language); err == nil && cached.Publishable() {
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
	// A candidate that answers off-contract, or not at all, is one weak draw from
	// a population that exists precisely so that a single bad draw does not decide
	// the run. It is recorded and skipped; only an empty population is fatal, and
	// then the reason is the last one seen rather than a generic failure.
	var refused error
	for i := 1; i <= options.Candidates; i++ {
		e.log("generating candidate %d/%d", i, options.Candidates)
		instructions, input = e.Prompts.Candidate(p, snippet, i)
		response, err := e.complete(ctx, "candidate", i, options.Model, instructions, input)
		if err != nil {
			refused = fmt.Errorf("candidate %d: %w", i, err)
			e.log("skipping candidate %d: %v", i, err)
			result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "candidate", Number: i, Error: err.Error(), At: time.Now().UTC()})
			continue
		}
		code, explanation, err := textguard.Parse(response.Text)
		if err != nil {
			refused = fmt.Errorf("candidate %d: %w", i, err)
			e.log("skipping candidate %d: %v", i, err)
			result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "candidate", Number: i, Response: response, Error: err.Error(), At: time.Now().UTC()})
			continue
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
	// Selection runs over the candidates that already passed execution. Asking a model
	// to rank a failing candidate against a passing one wastes the strongest evidence
	// in the run, and picking a failing winner only to silently swap in some other
	// candidate afterwards discards the ranking altogether.
	eligible := make([]artifact.Candidate, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		if candidate.Offline.Passed {
			eligible = append(eligible, candidate)
		}
	}
	pool := eligible
	if len(pool) == 0 {
		if len(result.Candidates) == 0 {
			return result, fmt.Errorf("no candidate met the response contract: %w", refused)
		}
		e.log("no candidate passed its offline suite; repairing the first")
		pool = result.Candidates
	}
	current := pool[0]
	if len(pool) > 1 {
		e.log("selecting the strongest of %d candidates that passed execution", len(pool))
		instructions, input = e.Prompts.Select(p, reference, pool)
		response, err := e.complete(ctx, "select", 0, options.Model, instructions, input)
		if err != nil {
			return result, err
		}
		result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "select", Response: response, At: time.Now().UTC()})
		selected := textguard.Selected(response.Text)
		found := false
		for _, candidate := range pool {
			if candidate.Number == selected {
				current, found = candidate, true
				break
			}
		}
		if !found {
			return result, fmt.Errorf("selector returned %q, which names no candidate that passed execution", response.Text[max(0, len(response.Text)-40):])
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
			// A repair that answers off-contract is a failed repair, not a failed run.
			// Discarding it keeps the best candidate the run actually has and saves the
			// audit that says why nothing better arrived, which is what a malformed
			// rewrite already does one stage later.
			e.log("discarding a malformed repair: %v", err)
			result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "repair", Number: repair + 1, Response: response, Error: err.Error(), At: time.Now().UTC()})
			break
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
	// A judge that cannot be reached must not erase the run it was judging. By this
	// point the candidates are generated and executed, and that evidence is the
	// expensive part; returning before the store ran discarded all of it, so a
	// gateway 500 during judging cost the whole run. The audit is written either
	// way, and the verdict is simply absent, which is what Verified already means.
	verifyErr := error(nil)
	if result.Accepted {
		verifyErr = e.verify(ctx, p, options, reference, &result)
	}
	result.CompletedAt = time.Now().UTC()
	if err := e.Store.Save(result); err != nil {
		return result, err
	}
	if verifyErr != nil {
		return result, fmt.Errorf("%w; the audit artifact was saved with the executed candidates", verifyErr)
	}
	if !result.Accepted {
		return result, errors.New("solution was not accepted; unpublished audit artifact was saved")
	}
	if !result.Verified {
		return result, errors.New("code passed execution but the article failed the quality gate; unpublished audit artifact was saved")
	}
	return result, nil
}

// verify applies the two independent quality judges to the article that ships with
// the executed code. One judge reads the private reference, the other never sees it,
// so a defect the reference itself contains cannot pass both at once. Both must pass.
// The code is fixed by this point: a rewrite that returns different code is executed
// again and rejected unless it also passes, so prose repair can never smuggle in
// unverified code.
func (e *Engine) verify(ctx context.Context, p problem.Problem, options Options, reference string, result *artifact.Result) error {
	for pass := 0; ; pass++ {
		e.log("judging the article, pass %d", pass+1)
		var failures []string
		result.Quality = nil
		checks := []struct {
			name  string
			build func() (string, string)
		}{
			{"criteria", func() (string, string) {
				return e.Prompts.Criteria(p, options.Language, reference, result.Code, result.Explanation)
			}},
			{"falsification", func() (string, string) {
				return e.Prompts.Falsify(p, options.Language, result.Code, result.Explanation, result.Offline.AssertionsRun)
			}},
		}
		for _, check := range checks {
			instructions, input := check.build()
			response, err := e.complete(ctx, check.name, pass, options.Model, instructions, input)
			if err != nil {
				return err
			}
			passed, verdictErr := textguard.Verdict(response.Text)
			report := textguard.Clean(response.Text)
			result.Quality = append(result.Quality, artifact.Verdict{Name: check.name, Pass: passed && verdictErr == nil, Report: report, Response: response, At: time.Now().UTC()})
			result.Attempts = append(result.Attempts, artifact.Attempt{Phase: check.name, Number: pass, Response: response, At: time.Now().UTC()})
			if verdictErr != nil {
				failures = append(failures, fmt.Sprintf("<%s_judge>%v; treat this as a failure and restate the article's claims so a verdict is possible</%s_judge>", check.name, verdictErr, check.name))
				continue
			}
			if !passed {
				failures = append(failures, fmt.Sprintf("<%s_judge>\n%s\n</%s_judge>", check.name, report, check.name))
			}
		}
		if len(failures) == 0 {
			result.Verified = true
			return nil
		}
		if pass >= options.MaxCorrections {
			e.log("article rejected by %d of 2 judges with no corrections left", len(failures))
			return nil
		}
		e.log("rewriting the article after %d of 2 judges rejected it", len(failures))
		instructions, input := e.Prompts.Rewrite(p, options.Language, result.Code, result.Explanation, failures)
		response, err := e.complete(ctx, "rewrite", pass+1, options.Model, instructions, input)
		if err != nil {
			return err
		}
		result.Attempts = append(result.Attempts, artifact.Attempt{Phase: "rewrite", Number: pass + 1, Response: response, At: time.Now().UTC()})
		code, explanation, parseErr := textguard.Parse(response.Text)
		if parseErr != nil {
			e.log("discarding a malformed rewrite: %v", parseErr)
			return nil
		}
		if code != result.Code {
			e.log("the rewrite changed verified code; re-executing before accepting it")
			offlineResult, offlineErr := e.Offline.Verify(ctx, p.Slug, options.Language, code)
			if offlineErr != nil || !offlineResult.Passed {
				e.log("discarding a rewrite whose code no longer passes execution")
				return nil
			}
			result.Code, result.Offline = code, offlineResult
		}
		result.Explanation = explanation
	}
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
