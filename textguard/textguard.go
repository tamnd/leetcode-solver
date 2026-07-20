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
var providerLeak = regexp.MustCompile(`(?i)\b(chatgpt|openai|claude|gemini|language model|the prompt|the candidate|the judge)\b`)
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
func Selected(text string) int {
	m := selected.FindStringSubmatch(text)
	if len(m) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
