package textguard

import "testing"

func TestParse(t *testing.T) {
	input := `<CODE>
func twoSum() {}
</CODE>
<SOLUTION>
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
## Solution
` + "```go\nfunc twoSum() {}\n```" + `
## Worked Examples
x
## Complexity Analysis
x
## Test Cases
x
## Edge Cases
x
</SOLUTION>`
	code, solution, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if code != "func twoSum() {}" {
		t.Fatalf("code=%q", code)
	}
	if solution == "" {
		t.Fatal("empty solution")
	}
}

func TestParseRejectsDifferentDisplayedCode(t *testing.T) {
	input := `<CODE>func twoSum() {}</CODE><SOLUTION>
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
## Go Solution
` + "```go\nfunc twoSum() { panic(\"different\") }\n```" + `
## Worked Examples
x
## Complexity Analysis
x
## Test Cases
x
## Edge Cases
x
</SOLUTION>`
	if _, _, err := Parse(input); err == nil {
		t.Fatal("expected mismatched displayed code to be rejected")
	}
}

func TestParseRejectsLeaks(t *testing.T) {
	_, _, err := Parse(`<CODE>x</CODE><SOLUTION>## Problem Understanding
ChatGPT
## Approaches
x
## Approach Comparison
x
## Algorithm Walkthrough
### Why it works
## Worked Examples
` + "```x```" + `
## Complexity Analysis
x
## Test Cases
x
## Edge Cases
x</SOLUTION>`)
	if err == nil {
		t.Fatal("expected error")
	}
}

// The Boyer-Moore majority vote is explained with the word "candidate" in every
// textbook. A leak guard that rejects that prose rejects a correct article.
func TestParseAcceptsOrdinaryAlgorithmicUseOfCandidate(t *testing.T) {
	body := `## Problem Understanding
The candidate is the value we currently believe is the majority element.
## Approaches
The judge in the problem statement counts every pair.
## Approach Comparison
x
## Algorithm Walkthrough
x
### Why it works
When the count drops to zero the candidate is replaced.
` + "```go\nfunc majority() {}\n```" + `
## Worked Examples
x
## Complexity Analysis
x
## Test Cases
x
## Edge Cases
x`
	if _, _, err := Parse("<CODE>func majority() {}</CODE><SOLUTION>" + body + "</SOLUTION>"); err != nil {
		t.Fatalf("ordinary prose was rejected as a leak: %v", err)
	}
}

func TestParseRejectsPipelineVocabulary(t *testing.T) {
	for _, leak := range []string{"Candidate 2 was stronger.", "The private reference disagreed.", "As an AI I cannot be certain."} {
		body := `## Problem Understanding
` + leak + `
## Approaches
x
## Approach Comparison
x
## Algorithm Walkthrough
x
### Why it works
x
` + "```go\nfunc f() {}\n```" + `
## Worked Examples
x
## Complexity Analysis
x
## Test Cases
x
## Edge Cases
x`
		if _, _, err := Parse("<CODE>func f() {}</CODE><SOLUTION>" + body + "</SOLUTION>"); err == nil {
			t.Fatalf("leak was accepted: %q", leak)
		}
	}
}

func TestVerdict(t *testing.T) {
	for _, tc := range []struct {
		text string
		pass bool
		err  bool
	}{
		{"analysis\nVERDICT: PASS", true, false},
		{"analysis\n**VERDICT: FAIL**", false, false},
		{"verdict: pass\n", true, false},
		{"the article is excellent", false, true},
		{"VERDICT: PASS\nVERDICT: FAIL", false, true},
		{"I would give a VERDICT: PASS here if pressed", false, true},
	} {
		got, err := Verdict(tc.text)
		if (err != nil) != tc.err || got != tc.pass {
			t.Fatalf("Verdict(%q)=%t,%v", tc.text, got, err)
		}
	}
}

func TestSelected(t *testing.T) {
	if got := Selected("analysis\nSELECTED: 3\n"); got != 3 {
		t.Fatalf("got %d", got)
	}
}
