// Package config loads runtime settings without persisting credentials.
package config

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/tamnd/leetcode-solver/codex"
)

type Config struct {
	Provider, BaseURL, APIKey, Model, Language, LeetCodeURL, Session, CSRFToken, Output, Database, EvalRoot, CompleteCache, ContainerRuntime string
	Candidates, MaxRepairs, MaxCorrections                                                                                                   int
}

// Load reads the environment. The default model depends on the provider,
// because an API model slug and a Codex-subscription slug are different
// namespaces and a wrong one fails on the first request.
func Load() Config {
	provider := get("LEETCODE_SOLVER_PROVIDER", "api")
	defaultModel := "gpt-5.4"
	if provider == "codex" {
		defaultModel = codex.DefaultModel
	}
	return Config{Provider: provider, BaseURL: get("LEETCODE_SOLVER_BASE_URL", "https://api.openai.com/v1"), APIKey: first(os.Getenv("LEETCODE_SOLVER_API_KEY"), os.Getenv("OPENAI_API_KEY")), Model: get("LEETCODE_SOLVER_MODEL", defaultModel), Language: get("LEETCODE_SOLVER_LANGUAGE", "auto"), LeetCodeURL: get("LEETCODE_SOLVER_LEETCODE_URL", "https://leetcode.com"), Session: os.Getenv("LEETCODE_SESSION"), CSRFToken: os.Getenv("LEETCODE_CSRF_TOKEN"), Output: expand(get("LEETCODE_SOLVER_OUTPUT", "~/data/leetcode-solver")), Database: expand(get("LEETCODE_SOLVER_DATABASE", "~/data/leetcode/leetcode.sqlite")), EvalRoot: expand(get("LEETCODE_SOLVER_EVAL_ROOT", "~/data/leetcode-evals")), CompleteCache: expand(get("LEETCODE_SOLVER_COMPLETE_CACHE", "~/data/leetcode-complete")), ContainerRuntime: get("LEETCODE_SOLVER_CONTAINER_RUNTIME", "auto"), Candidates: getInt("LEETCODE_SOLVER_CANDIDATES", 3), MaxRepairs: getInt("LEETCODE_SOLVER_MAX_REPAIRS", 2), MaxCorrections: getInt("LEETCODE_SOLVER_MAX_CORRECTIONS", 1)}
}
func get(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func getInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}
func expand(path string) string {
	if path == "~" || len(path) > 2 && path[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
