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
