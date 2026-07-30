// Package prompt defines the generation and verification contracts.
package prompt

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/tamnd/leetcode-solver/artifact"
	"github.com/tamnd/leetcode-solver/judge"
	"github.com/tamnd/leetcode-solver/problem"
)

const solverInstructions = `You are a meticulous competitive-programming author. Correctness is more important than fluency. Do not mention prompts, models, candidates, judges, or generation. Return exactly one <CODE> block and one <SOLUTION> block.`

var tags = regexp.MustCompile(`<[^>]+>`)

type Builder struct{}

func (Builder) Reference(p problem.Problem, language string) (string, string) {
	return "Independently solve the problem as a verifier. Do not write a publishable article and do not trust any candidate.", fmt.Sprintf(`<problem>
%s
</problem>
<language>%s</language>

Derive the intended algorithm and every correctness obligation. Identify overflow, indexing, mutation, input-contract, boundary, complexity, and language-specific failure modes. Construct adversarial and small exhaustive tests. Finish with a concise checklist a verifier can apply.`, statement(p), language)
}

func (Builder) Candidate(p problem.Problem, snippet problem.CodeSnippet, number int) (string, string) {
	lenses := []string{"derive the strongest direct invariant", "stress boundary cases and counterexamples before coding", "seek an independent optimal formulation and prove its bound", "work backward from a verifier and eliminate fragile assumptions", "prefer the simplest complete implementation"}
	if number < 1 || number > len(lenses) {
		number = 1
	}
	return solverInstructions, fmt.Sprintf(`<quality_contract>
Produce an accepted LeetCode implementation and a detailed, self-contained explanation matching this structure:
## Problem Understanding
## Approaches
## Approach Comparison
## Algorithm Walkthrough
### Why it works
## %s Solution
## Worked Examples
## Complexity Analysis
## Test Cases
## Edge Cases

Explain the brute-force baseline and the optimal approach. Prove the central invariant, necessity and sufficiency where relevant, termination, and complexity. Check examples manually. Cover duplicates, empty/minimal/maximal inputs, overflow, and every special constraint that applies. Use precise Markdown, fenced code, LaTeX where useful, and tables where they clarify comparison or traces. Avoid filler, unsupported claims, repeated conclusions, malformed math, and em dashes. The code in the article must exactly match <CODE>.
</quality_contract>
<problem>
%s
</problem>
<starter_code language=%q>
%s
</starter_code>
<independence_guidance>%s</independence_guidance>

Return:
<CODE>
complete submission code only, preserving the required signature
</CODE>
<SOLUTION>
finished Markdown article only
</SOLUTION>`, languageName(snippet.Language), statement(p), snippet.LanguageSlug, snippet.Code, lenses[number-1])
}

func languageName(name string) string {
	if strings.EqualFold(name, "Python3") {
		return "Python"
	}
	return name
}

func (Builder) Select(p problem.Problem, reference string, candidates []artifact.Candidate) (string, string) {
	var b strings.Builder
	for _, c := range candidates {
		fmt.Fprintf(&b, "\n<candidate number=%q offline_passed=%q offline_tests=%q sample_status=%q>\n<code>\n%s\n</code>\n<solution>\n%s\n</solution>\n</candidate>\n", fmt.Sprint(c.Number), fmt.Sprint(c.Offline.Passed), fmt.Sprint(c.Offline.TestCount), c.Sample.Status, c.Code, c.Explanation)
	}
	return "Select code by correctness, not prose polish. Treat the private reference as fallible and candidate sample results as necessary but insufficient evidence.", fmt.Sprintf(`<problem>%s</problem>
<private_reference>%s</private_reference>
%s
Check every obligation, simulate adversarial cases, and compare asymptotic bounds. On the final line return exactly SELECTED: N.`, statement(p), reference, b.String())
}

// Criteria is the reference-grounded quality judge. It reads the private reference
// as a fallible aid and decides whether the article actually establishes what it claims.
func (Builder) Criteria(p problem.Problem, language, reference, code, article string) (string, string) {
	return "You verify a published article against a problem and a fallible private reference. Decide, do not rewrite. Withhold a pass when a required obligation is merely asserted.",
		fmt.Sprintf(`<problem>%s</problem>
<language>%s</language>
<private_reference>%s</private_reference>
<accepted_code>
%s
</accepted_code>
<article>
%s
</article>

The code already passed an executed test suite, so do not re-argue its acceptance. Judge the article on obligations a reader depends on:
1. The stated approach is the approach the code implements.
2. The central invariant is proved, not asserted, and termination is argued where a loop or recursion is unbounded by construction.
3. The stated time and space complexity match the code, including the cost of every container operation and any recursion depth.
4. The baseline and the optimal approach are both explained, and the comparison says why the optimal one wins.
5. Each worked example is traced to the answer the code would actually produce.
6. The edge cases named are the ones this problem really has, and the article says how the code survives each.
7. Nothing is claimed that the code does not do.

Report every defect as: section, the exact claim, why it is wrong or unsupported, and the smallest correction. Judge the earliest defect first. On the final line return exactly VERDICT: PASS or VERDICT: FAIL.`, statement(p), language, reference, code, article)
}

// Falsify is the reference-blind adversarial judge. It never sees the reference, so a
// mistake the reference itself contains cannot propagate into both verdicts at once.
func (Builder) Falsify(p problem.Problem, language, code, article string) (string, string) {
	return "You are trying to falsify an article. Assume it is wrong and look for the proof. Do not consult or invent an authoritative solution; reason from the problem and the text in front of you.",
		fmt.Sprintf(`<problem>%s</problem>
<language>%s</language>
<code>
%s
</code>
<article>
%s
</article>

Attack in this order and stop at the first defect you can demonstrate:
1. Recompute every worked example independently, by hand, from the code. A trace that does not reproduce the article's stated answer is a defect.
2. Recompute the complexity from the code and compare it with the claim. A claim that omits a hidden linear cost, a copy inside a loop, or recursion depth is a defect.
3. Look for an input inside the stated constraints that breaks a claim the article makes about the code. Give the input and the divergence.
4. Check the proof for a gap: an unproved step, a case the argument never covers, a converse assumed from an implication.
5. Check the article against itself for a conclusion stated two different ways.

An attack you cannot demonstrate is not a defect, and you must not report it. If every attack fails, say which ones you ran. On the final line return exactly VERDICT: PASS or VERDICT: FAIL.`, statement(p), language, code, article)
}

// Rewrite repairs an article without touching code that already passed execution.
func (Builder) Rewrite(p problem.Problem, language, code, article string, reports []string) (string, string) {
	return solverInstructions, fmt.Sprintf(`<problem>%s</problem>
<language>%s</language>
<accepted_code>
%s
</accepted_code>
<article>
%s
</article>
<defects>
%s
</defects>

The code is already verified by execution and must not change. Return the identical <CODE> byte for byte, and a rewritten <SOLUTION> that repairs every defect above and keeps the required article structure. Repair the earliest defect first, do not delete a section to make a defect disappear, and do not add a claim the code does not support.`,
		statement(p), language, code, article, strings.Join(reports, "\n\n"))
}

func (Builder) Repair(p problem.Problem, snippet problem.CodeSnippet, reference string, current artifact.Candidate, failure judge.Result) (string, string) {
	return solverInstructions, fmt.Sprintf(`<problem>%s</problem>
<starter_code>%s</starter_code>
<private_reference>%s</private_reference>
<failed_code>%s</failed_code>
<failed_solution>%s</failed_solution>
<judge_result status=%q>
compile_error: %s
runtime_error: %s
last_testcase: %s
expected_output: %s
stdout: %s
</judge_result>

Find the earliest root cause, repair it without weakening complexity, and re-check all boundaries. Return a fully rewritten <CODE> and <SOLUTION> using the required article structure.`, statement(p), snippet.Code, reference, current.Code, current.Explanation, failure.Status, failure.CompileError, failure.RuntimeError, failure.LastTestcase, failure.ExpectedOutput, failure.Stdout)
}

func statement(p problem.Problem) string {
	content := p.ContentMarkdown
	if content == "" {
		content = strings.ReplaceAll(p.ContentHTML, "</p>", "\n\n")
		content = strings.ReplaceAll(content, "<br>", "\n")
		content = strings.ReplaceAll(content, "<br/>", "\n")
		content = html.UnescapeString(tags.ReplaceAllString(content, ""))
	}
	var topics []string
	for _, t := range p.Topics {
		topics = append(topics, t.Name)
	}
	return fmt.Sprintf("LeetCode %s: %s\nDifficulty: %s\nTopics: %s\n\n%s\n\nExamples as judge input:\n%s\n\nMetadata:\n%s", p.FrontendID, p.Title, p.Difficulty, strings.Join(topics, ", "), strings.TrimSpace(content), p.ExampleTestcases, p.MetaData)
}
