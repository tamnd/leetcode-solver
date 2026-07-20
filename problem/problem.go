// Package problem contains the provider-neutral representation of a coding problem.
package problem

import "time"

type Problem struct {
	ID               string        `json:"id"`
	FrontendID       string        `json:"frontend_id"`
	Title            string        `json:"title"`
	Slug             string        `json:"slug"`
	Difficulty       string        `json:"difficulty"`
	PaidOnly         bool          `json:"paid_only"`
	ContentHTML      string        `json:"content_html"`
	ContentMarkdown  string        `json:"content_markdown"`
	TranslatedTitle  string        `json:"translated_title,omitempty"`
	ExampleTestcases string        `json:"example_testcases"`
	SampleTestcase   string        `json:"sample_testcase"`
	MetaData         string        `json:"metadata_json"`
	Hints            []string      `json:"hints,omitempty"`
	Topics           []Topic       `json:"topics,omitempty"`
	Snippets         []CodeSnippet `json:"snippets,omitempty"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

type Topic struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type CodeSnippet struct {
	Language     string `json:"language"`
	LanguageSlug string `json:"language_slug"`
	Code         string `json:"code"`
}

func (p Problem) Snippet(language string) (CodeSnippet, bool) {
	for _, snippet := range p.Snippets {
		if snippet.LanguageSlug == language || snippet.Language == language {
			return snippet, true
		}
	}
	return CodeSnippet{}, false
}
