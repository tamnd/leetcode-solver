package agentbench

import (
	"slices"
	"testing"
)

func TestProviderEnvZenModel(t *testing.T) {
	env := providerEnv("zen/north-mini-code-free", "tomo", RunOptions{})
	for _, want := range []string{
		"LAB_MODEL=north-mini-code-free",
		"LAB_UPSTREAM=https://opencode.ai/zen",
		"LAB_PASSTHROUGH=0",
		"LAB_NAME_PREFIX=leetcode-zen",
		"LAB_PROXY_PORT=8902",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("provider env %q does not contain %q", env, want)
		}
	}
}

func TestProviderEnvRejectsEmptyZenModel(t *testing.T) {
	if env := providerEnv("zen/", "tomo", RunOptions{}); env != nil {
		t.Fatalf("empty Zen model env = %q, want nil", env)
	}
}
