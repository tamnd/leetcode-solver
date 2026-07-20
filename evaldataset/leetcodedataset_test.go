package evaldataset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/leetcode-solver/offline"
)

func TestImportLeetCodeDataset(t *testing.T) {
	row := `{"task_id":"two-sum","prompt":"from typing import *","entry_point":"Solution().twoSum","test":"def check(candidate):\n    assert candidate([2,7],9) == [0,1]\n    assert candidate([3,3],6) == [0,1]"}`
	root := t.TempDir()
	report, err := ImportLeetCodeDataset(context.Background(), strings.NewReader(row), root, "python@test", "git:abc")
	if err != nil {
		t.Fatal(err)
	}
	if report.Rows != 1 || report.Bundles != 1 || report.Tests != 2 {
		t.Fatalf("%+v", report)
	}
	data, err := os.ReadFile(filepath.Join(root, "two-sum", "python3", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest offline.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.TestCount != 2 || manifest.CandidatePrefix == "" {
		t.Fatalf("%+v", manifest)
	}
	harness, err := os.ReadFile(filepath.Join(root, "two-sum", "python3", "test_solution.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(harness), `open("solution.py", "rb")`) {
		t.Fatalf("harness does not load candidate: %s", harness)
	}
}
