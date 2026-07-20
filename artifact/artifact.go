// Package artifact stores reproducible solve records and publishable explanations.
package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/leetcode-solver/judge"
	"github.com/tamnd/leetcode-solver/llm"
	"github.com/tamnd/leetcode-solver/offline"
	"github.com/tamnd/leetcode-solver/problem"
)

type Candidate struct {
	Number      int            `json:"number"`
	Code        string         `json:"code"`
	Explanation string         `json:"explanation_md"`
	Response    llm.Response   `json:"response"`
	Sample      judge.Result   `json:"sample_result"`
	Submission  judge.Result   `json:"submission_result"`
	Offline     offline.Result `json:"offline_result"`
}
type Attempt struct {
	Phase    string        `json:"phase"`
	Number   int           `json:"number,omitempty"`
	Response llm.Response  `json:"response"`
	Judge    *judge.Result `json:"judge,omitempty"`
	At       time.Time     `json:"at"`
}
type Result struct {
	Problem                problem.Problem `json:"problem"`
	Language, Model        string
	Code                   string         `json:"code"`
	Explanation            string         `json:"explanation_md"`
	Accepted               bool           `json:"accepted"`
	SubmissionID           int64          `json:"submission_id,omitempty"`
	Offline                offline.Result `json:"offline_result"`
	Candidates             []Candidate    `json:"candidates"`
	Attempts               []Attempt      `json:"attempts"`
	StartedAt, CompletedAt time.Time
}

type Store struct{ Root string }

func (s Store) Load(slug, language string) (Result, error) {
	var value Result
	data, err := os.ReadFile(s.path(slug, language, "json"))
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(data, &value)
	return value, err
}
func (s Store) Save(value Result) error {
	if value.Problem.Slug == "" || value.Language == "" {
		return errors.New("artifact requires problem slug and language")
	}
	dir := filepath.Join(s.Root, value.Problem.Slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := atomicWrite(s.path(value.Problem.Slug, value.Language, "json"), data); err != nil {
		return err
	}
	if !value.Accepted {
		return nil
	}
	markdown := strings.TrimSpace(value.Explanation) + "\n"
	return atomicWrite(s.path(value.Problem.Slug, value.Language, "md"), []byte(markdown))
}

// SaveCombined publishes one brain-compatible article only after both exact
// Python and Go implementations passed their complete offline suites.
func (s Store) SaveCombined(slug string, results []Result) (string, error) {
	var python, golang *Result
	for i := range results {
		if !results[i].Accepted {
			return "", errors.New("cannot combine an unaccepted solution")
		}
		switch results[i].Language {
		case "python3":
			python = &results[i]
		case "golang":
			golang = &results[i]
		}
	}
	if python == nil || golang == nil {
		return "", errors.New("combined article requires accepted python3 and golang results")
	}
	goSection := section(golang.Explanation, "## Go Solution", "## Worked Examples")
	if goSection == "" {
		return "", errors.New("explanation has no Go Solution section")
	}
	marker := "## Worked Examples"
	index := strings.Index(python.Explanation, marker)
	if index < 0 {
		return "", errors.New("explanation has no Python Worked Examples section")
	}
	combined := strings.TrimSpace(python.Explanation[:index]) + "\n\n" + goSection + "\n\n" + strings.TrimSpace(python.Explanation[index:]) + "\n"
	p := python.Problem
	completed := python.CompletedAt
	if golang.CompletedAt.After(completed) {
		completed = golang.CompletedAt
	}
	if completed.IsZero() {
		completed = time.Now().UTC()
	}
	var tags []string
	tags = append(tags, "leetcode", strings.ToLower(p.Difficulty))
	for _, topic := range p.Topics {
		tags = append(tags, topic.Slug)
	}
	quoted := make([]string, len(tags))
	for i, tag := range tags {
		quoted[i] = fmt.Sprintf("%q", tag)
	}
	description := fmt.Sprintf("A rigorous Python and Go solution for LeetCode %s, %s, verified by pinned offline test suites.", p.FrontendID, p.Title)
	frontmatter := fmt.Sprintf("---\ntitle: %q\ndescription: %q\ndate: %q\ntags: [%s]\ncategories: [\"algorithms\"]\ndifficulty: %q\nleetcode: %s\nweight: %s\ndraft: false\n---\n\n[LeetCode Problem %s](https://leetcode.com/problems/%s/)\n\n**Difficulty:** %s  \n**Topics:** %s\n\n## Solution\n\n# LeetCode %s, %s\n\n", fmt.Sprintf("LeetCode %s - %s", p.FrontendID, p.Title), description, completed.Format(time.RFC3339), strings.Join(quoted, ", "), p.Difficulty, p.FrontendID, p.FrontendID, p.FrontendID, p.Slug, p.Difficulty, topicNames(p.Topics), p.FrontendID, p.Title)
	combined = frontmatter + combined
	dir := filepath.Join(s.Root, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "solution.md")
	return path, atomicWrite(path, []byte(combined))
}
func topicNames(topics []problem.Topic) string {
	names := make([]string, len(topics))
	for i, topic := range topics {
		names[i] = topic.Name
	}
	return strings.Join(names, ", ")
}
func section(text, start, end string) string {
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	j := strings.Index(text[i+len(start):], end)
	if j < 0 {
		return strings.TrimSpace(text[i:])
	}
	return strings.TrimSpace(text[i : i+len(start)+j])
}
func (s Store) path(slug, language, ext string) string {
	return filepath.Join(s.Root, slug, fmt.Sprintf("%s.%s", language, ext))
}
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
