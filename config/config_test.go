package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("LEETCODE_SOLVER_MODEL", "unit-model")
	t.Setenv("LEETCODE_SOLVER_CANDIDATES", "4")
	got := Load()
	if got.Model != "unit-model" || got.Candidates != 4 {
		t.Fatalf("%+v", got)
	}
}
