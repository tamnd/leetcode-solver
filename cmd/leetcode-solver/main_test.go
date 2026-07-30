package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tamnd/leetcode-solver/offline"
	"github.com/tamnd/leetcode-solver/problem"
)

func both() problem.Problem {
	return problem.Problem{Slug: "two-sum", Snippets: []problem.CodeSnippet{{LanguageSlug: "python3"}, {LanguageSlug: "golang"}}}
}

// A language whose bundle does not exist cannot produce a provable article. It has
// to be reported and skipped, not allowed to fail the languages that can.
func TestChooseLanguagesReportsAMissingBundleAsAGapNotAFailure(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "two-sum", "python3")
	languages, gaps, err := chooseLanguages(both(), "auto", root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(languages, []string{"python3"}) {
		t.Fatalf("languages=%q", languages)
	}
	if !slices.Equal(gaps, []string{"golang"}) {
		t.Fatalf("gaps=%q, want golang reported", gaps)
	}
}

func TestChooseLanguagesFailsWhenNoLanguageHasABundle(t *testing.T) {
	_, gaps, err := chooseLanguages(both(), "auto", t.TempDir())
	if err == nil {
		t.Fatal("a problem with no bundles at all was accepted")
	}
	if !slices.Equal(gaps, []string{"python3", "golang"}) {
		t.Fatalf("gaps=%q", gaps)
	}
}

// An explicit request is answered with the bundle error, because the caller asked
// for that language and silently substituting another would be a lie.
func TestChooseLanguagesNeverSkipsAnExplicitRequest(t *testing.T) {
	languages, gaps, err := chooseLanguages(both(), "golang", t.TempDir())
	if err != nil || len(gaps) != 0 || !slices.Equal(languages, []string{"golang"}) {
		t.Fatalf("languages=%q gaps=%q err=%v", languages, gaps, err)
	}
}

// Coverage measures the debt, so it must not shrink to whatever was generated.
func TestRequiredLanguagesIgnoresBundleAvailability(t *testing.T) {
	if got := requiredLanguages(both()); !slices.Equal(got, []string{"python3", "golang"}) {
		t.Fatalf("got %q", got)
	}
	sql := problem.Problem{Slug: "big-countries", Snippets: []problem.CodeSnippet{{LanguageSlug: "mysql"}}}
	if got := requiredLanguages(sql); !slices.Equal(got, []string{"mysql"}) {
		t.Fatalf("got %q", got)
	}
	if got := requiredLanguages(problem.Problem{Slug: "x"}); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func writeManifest(t *testing.T, root, slug, language string) {
	t.Helper()
	dir := filepath.Join(root, slug, language)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := offline.Manifest{SchemaVersion: offline.SchemaVersion, ProblemSlug: slug, Language: language, Dataset: "unit", Revision: "1", Image: "python@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CandidateFile: "solution.py", Command: []string{"python3", "test.py"}, Files: []string{"test.py"}, TestCount: 1}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
