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
func TestSelected(t *testing.T) {
	if got := Selected("analysis\nSELECTED: 3\n"); got != 3 {
		t.Fatalf("got %d", got)
	}
}
