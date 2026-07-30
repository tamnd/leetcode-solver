package prompt

import (
	"strings"
	"testing"

	"github.com/tamnd/leetcode-solver/problem"
)

// The falsification judge decides whether an already-executed solution ships. A
// judge that misreads the problem's rules invents a counterexample and blocks
// code the official suite has already settled, so it is told what execution
// proved. It must still be told to attack the article, which is the part
// execution says nothing about.
func TestFalsifyCarriesTheExecutionEvidence(t *testing.T) {
	p := problem.Problem{Slug: "two-sum", Title: "Two Sum", ContentMarkdown: "add up to target"}
	_, input := Builder{}.Falsify(p, "python3", "code", "article", 109)
	for _, want := range []string{"109 assertions", "authoritative", "official test suite"} {
		if !strings.Contains(input, want) {
			t.Errorf("the evidence must state %q:\n%s", want, input)
		}
	}
	// The gate's real value is article defects, which no test count can excuse.
	if !strings.Contains(input, "complexity claim that does not match the code") {
		t.Error("article attacks must survive the evidence")
	}
	// Every attack vector stays available; the evidence bounds them, not removes them.
	if !strings.Contains(input, "Recompute the complexity from the code") {
		t.Error("the attack list was lost")
	}
}

// An unexecuted solution must not be handed an authority it does not have.
func TestFalsifyClaimsNoEvidenceWithoutAssertions(t *testing.T) {
	p := problem.Problem{Slug: "two-sum", Title: "Two Sum"}
	_, input := Builder{}.Falsify(p, "python3", "code", "article", 0)
	if strings.Contains(input, "authoritative") {
		t.Error("code that never ran was described as authoritative")
	}
	if !strings.Contains(input, "has not been executed") {
		t.Errorf("the absence of evidence must be stated:\n%s", input)
	}
}
