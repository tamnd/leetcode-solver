package offline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunnerExecutesValidBundle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("true path differs")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "two-sum", "python3")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: 1, ProblemSlug: "two-sum", Language: "python3", Dataset: "unit", Revision: "1", Image: "python@test@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CandidateFile: "solution.py", Command: []string{"python3", "test.py"}, Files: []string{"test.py"}, TestCount: 3}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.py"), []byte("pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (Runner{Root: root, DockerBinary: "/usr/bin/true"}).Verify(context.Background(), "two-sum", "python3", "class Solution: pass")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Passed || got.TestCount != 3 {
		t.Fatalf("%+v", got)
	}
}
func TestValidateRejectsZeroTests(t *testing.T) {
	err := validate(Manifest{SchemaVersion: 1, ProblemSlug: "x", Language: "golang", Dataset: "d", Revision: "r", Image: "go@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CandidateFile: "solution.go", Command: []string{"go"}}, "x", "golang")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateRequiresPinnedImage(t *testing.T) {
	err := validate(Manifest{SchemaVersion: 1, ProblemSlug: "x", Language: "golang", Dataset: "d", Revision: "r", Image: "golang:latest", CandidateFile: "solution.go", Command: []string{"go"}, TestCount: 1}, "x", "golang")
	if err == nil {
		t.Fatal("expected unpinned image to be rejected")
	}
}

func TestSafeCopyRejectsTraversal(t *testing.T) {
	if err := safeCopy(t.TempDir(), t.TempDir(), "../secret"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}
