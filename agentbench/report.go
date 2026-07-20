package agentbench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type TokenDetail struct {
	Input           int `json:"input"`
	CachedInput     int `json:"cached_input"`
	CacheWriteInput int `json:"cache_write_input"`
	FreshInput      int `json:"fresh_input"`
	Output          int `json:"output"`
	Reasoning       int `json:"reasoning_output"`
	Total           int `json:"total"`
}
type Measurement struct {
	Provider    string      `json:"provider"`
	Tool        string      `json:"tool"`
	Scenario    string      `json:"scenario"`
	Model       string      `json:"model"`
	Timestamp   string      `json:"timestamp"`
	Passed      bool        `json:"passed"`
	Requests    int         `json:"requests"`
	WallSeconds int         `json:"wall_seconds"`
	MaxRSSKB    int         `json:"max_rss_kb"`
	Tokens      TokenDetail `json:"tokens"`
	ListCostUSD float64     `json:"list_cost_usd"`
}
type Report struct {
	Schema           string        `json:"schema"`
	DatasetRevision  string        `json:"dataset_revision"`
	DatasetSHA256    string        `json:"dataset_sha256"`
	TomoRevision     string        `json:"tomo_revision"`
	TomoLabsRevision string        `json:"tomo_labs_revision"`
	Runs             []Measurement `json:"runs"`
}

type labResult struct {
	Tool, Scenario, Time, Model     string
	Passed                          bool
	WallSeconds, MaxRSSKB, Requests int
	Tokens                          struct{ Prompt, Completion, Total, Cached, CacheWrite, Reasoning int }
}
type usageLine struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens"`
}

func WriteReport(data, jsonPath, markdownPath string) error {
	root := filepath.Join(data, "evals", Suite)
	var runs []Measurement
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "result.json" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r labResult
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		provider := "unknown"
		if r.Model != "" {
			provider = "zen"
		}
		if strings.Contains(r.Model, "deepseek") {
			provider = "zen/deepseek"
		}
		if r.Model == "gpt-5.6-luna" {
			provider = "luna"
		}
		if r.Model == "gpt-5.4-mini" {
			provider = "mini"
		}
		t := TokenDetail{Input: r.Tokens.Prompt, CachedInput: r.Tokens.Cached, CacheWriteInput: r.Tokens.CacheWrite, FreshInput: max(r.Tokens.Prompt-r.Tokens.Cached, 0), Output: r.Tokens.Completion, Reasoning: r.Tokens.Reasoning, Total: r.Tokens.Total}
		usagePath := filepath.Join(filepath.Dir(path), "attempt-1", "trace", "usage.jsonl")
		if t.Reasoning == 0 {
			if f, e := os.Open(usagePath); e == nil {
				s := bufio.NewScanner(f)
				for s.Scan() {
					var u usageLine
					if json.Unmarshal(s.Bytes(), &u) == nil {
						t.Reasoning += u.ReasoningTokens
					}
				}
				_ = f.Close()
			}
		}
		m := Measurement{provider, r.Tool, r.Scenario, r.Model, r.Time, r.Passed, r.Requests, r.WallSeconds, r.MaxRSSKB, t, listCost(r.Model, t)}
		runs = append(runs, m)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(runs, func(i, j int) bool {
		a, b := runs[i], runs[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Scenario != b.Scenario {
			return a.Scenario < b.Scenario
		}
		return a.Tool < b.Tool
	})
	report := Report{"leetcode-agentbench/v1", DatasetRevision, DatasetSHA256, TomoRevision, TomoLabsRevision, runs}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(b, '\n'), 0o644); err != nil {
		return err
	}
	var md strings.Builder
	md.WriteString("# LeetCode agent benchmark\n\nPass@1. Agents ran without network access; hidden LiveCodeBench tests were graded outside their containers. Costs are list-price estimates, not subscription charges.\n\n| Provider | Problem | Tool | Pass | Input (fresh/cache) | Output (reasoning) | Total | List cost | Wall |\n|---|---|---|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range runs {
		fmt.Fprintf(&md, "| %s | %s | %s | %v | %d / %d | %d / %d | %d | $%.6f | %ds |\n", r.Provider, r.Scenario, r.Tool, r.Passed, r.Tokens.FreshInput, r.Tokens.CachedInput, r.Tokens.Output, r.Tokens.Reasoning, r.Tokens.Total, r.ListCostUSD, r.WallSeconds)
	}
	return os.WriteFile(markdownPath, []byte(md.String()), 0o644)
}

func listCost(model string, t TokenDetail) float64 {
	var input, cache, output float64
	switch model {
	case "gpt-5.6-luna":
		input = 1e-6
		cache = 1e-7
		output = 6e-6
	case "gpt-5.4-mini":
		input = 7.5e-7
		cache = 7.5e-8
		output = 4.5e-6
	case "deepseek-v4-flash-free", "deepseek-v4-flash", "deepseek-chat":
		input = 2.8e-7
		cache = 2.8e-8
		output = 4.2e-7
	default:
		return 0
	}
	return float64(t.FreshInput)*input + float64(t.CachedInput)*cache + float64(t.Output)*output
}
