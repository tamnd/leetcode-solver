// Package textguard parses and validates generated artifacts.
package textguard

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var codeBlock = regexp.MustCompile(`(?s)<CODE>\s*(.*?)\s*</CODE>`)
var solutionBlock = regexp.MustCompile(`(?s)<SOLUTION>\s*(.*?)\s*</SOLUTION>`)
var fencedCode = regexp.MustCompile("(?s)```[^\\n]*\\n(.*?)\\n```")
var selected = regexp.MustCompile(`(?m)^SELECTED:\s*([1-5])\s*$`)
var verdict = regexp.MustCompile(`(?mi)^\s*\**VERDICT:\**\s*\**(PASS|FAIL)\**\s*\.?\s*$`)

// providerLeak matches the vocabulary of the generation pipeline itself. It must not
// match ordinary algorithmic prose: "the candidate" is the standard name for the
// running answer in Boyer-Moore majority voting, and "the judge" appears in problem
// statements, so both are matched only where they name this pipeline's own machinery.
var providerLeak = regexp.MustCompile(`(?i)(\bchatgpt\b|\bopenai\b|\banthropic\b|\bclaude\b|\bgemini\b|\bcopilot\b|\bas an ai\b|\blanguage model\b|\bsystem prompt\b|\bthis prompt\b|\bthe prompt (?:above|below|says|asks|instructs)\b|\bcandidate #?[1-5]\b|\bprivate reference\b|\bthe selector\b|\b(?:truth|audit|reference) judge\b)`)
var required = []string{"## Problem Understanding", "## Approaches", "## Approach Comparison", "## Algorithm Walkthrough", "### Why it works", "## Worked Examples", "## Complexity Analysis", "## Test Cases", "## Edge Cases"}

func Parse(text string) (code, solution string, err error) {
	cm := codeBlock.FindStringSubmatch(text)
	sm := solutionBlock.FindStringSubmatch(text)
	if len(cm) != 2 || strings.TrimSpace(cm[1]) == "" {
		return "", "", errors.New("response has no non-empty CODE block")
	}
	if len(sm) != 2 || strings.TrimSpace(sm[1]) == "" {
		return "", "", errors.New("response has no non-empty SOLUTION block")
	}
	code = normalizeCode(cm[1])
	solution = Clean(sm[1])
	if providerLeak.MatchString(solution) {
		return "", "", errors.New("solution leaks generation or review process")
	}
	for _, heading := range required {
		if !strings.Contains(solution, heading) {
			return "", "", errors.New("solution is missing " + heading)
		}
	}
	if !containsExactCode(solution, code) {
		return "", "", errors.New("solution has no fenced implementation exactly matching CODE")
	}
	return code, solution, nil
}

func normalizeCode(text string) string {
	text = strings.TrimSpace(text)
	if match := fencedCode.FindStringSubmatch(text); len(match) == 2 && match[0] == text {
		return strings.TrimSpace(match[1])
	}
	return text
}

func containsExactCode(solution, code string) bool {
	for _, match := range fencedCode.FindAllStringSubmatch(solution, -1) {
		if strings.TrimSpace(match[1]) == code {
			return true
		}
	}
	return false
}
func Clean(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "—", "-")
	text = regexp.MustCompile(`(?m)^\s*---+\s*$`).ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

// Verdict reads a judge's decision. A response with no decision field, or with both
// decisions present, is a failure: an unstated verdict is not a pass.
func Verdict(text string) (bool, error) {
	matches := verdict.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return false, errors.New("judge returned no VERDICT line")
	}
	decision := strings.EqualFold(matches[len(matches)-1][1], "PASS")
	for _, match := range matches {
		if strings.EqualFold(match[1], "PASS") != decision {
			return false, errors.New("judge returned conflicting VERDICT lines")
		}
	}
	return decision, nil
}

func Selected(text string) int {
	m := selected.FindStringSubmatch(text)
	if len(m) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
