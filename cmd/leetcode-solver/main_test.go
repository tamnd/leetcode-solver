package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tamnd/leetcode-solver/codex"
	"github.com/tamnd/leetcode-solver/config"
	"github.com/tamnd/leetcode-solver/llm"
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

// The provider decides which credential a run spends. Reading it wrong means
// either a subscription request sent to an API endpoint with no key, or an API
// request charged to the ChatGPT plan.
func TestCompleterFollowsTheProvider(t *testing.T) {
	if got := completer(config.Config{Provider: "codex"}); !isCodex(got) {
		t.Errorf("provider codex produced %T", got)
	}
	if got := completer(config.Config{Provider: "api", BaseURL: "https://example.test/v1"}); isCodex(got) {
		t.Errorf("provider api produced %T", got)
	}
	// A gateway that implements only chat completions answers a Responses request
	// with an upstream error, so the two must not be interchangeable.
	if _, ok := completer(config.Config{Provider: "chat", BaseURL: "https://example.test/v1"}).(*llm.ChatClient); !ok {
		t.Error("provider chat did not produce a chat client")
	}
	if _, ok := completer(config.Config{Provider: "api", BaseURL: "https://example.test/v1"}).(*llm.Client); !ok {
		t.Error("provider api did not produce a Responses client")
	}
}

// An API model slug and a subscription slug are different namespaces. Choosing
// the provider on the command line, after the environment already defaulted the
// model for the API, must not send gpt-5.4 to the Codex backend.
func TestResolveProviderRepairsADefaultedModel(t *testing.T) {
	t.Setenv("LEETCODE_SOLVER_MODEL", "")
	for _, test := range []struct {
		name, requested, want string
		args                  []string
	}{
		{name: "defaulted model is replaced", requested: "gpt-5.4", want: codex.DefaultModel, args: []string{"--provider", "codex"}},
		{name: "an explicit model is kept", requested: "gpt-5.4", want: "gpt-5.5", args: []string{"--provider", "codex", "--model", "gpt-5.5"}},
		{name: "the api path is untouched", requested: "gpt-5.4", want: "gpt-5.4", args: []string{"--provider", "api"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fs, cfg, _ := flags("solve")
			cfg.Model = test.requested
			if err := fs.Parse(test.args); err != nil {
				t.Fatal(err)
			}
			if err := resolveProvider(fs, cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.Model != test.want {
				t.Errorf("model = %q, want %q", cfg.Model, test.want)
			}
		})
	}
	// A typo must not silently fall back to the API path, where the failure would
	// name a missing key rather than the misspelled provider.
	fs, cfg, _ := flags("solve")
	if err := fs.Parse([]string{"--provider", "chatgpt"}); err != nil {
		t.Fatal(err)
	}
	if err := resolveProvider(fs, cfg); err == nil {
		t.Error("an unknown provider was accepted")
	}
}

func isCodex(c llm.Completer) bool {
	_, ok := c.(*codex.Client)
	return ok
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
