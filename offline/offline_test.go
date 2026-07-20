package offline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

func TestContainerArgsGiveOnlyGoAnExecutableBuildTmpfs(t *testing.T) {
	goArgs := containerArgs("/host/work", Manifest{Language: "golang", Image: "go@sha256:abc"})
	if !slices.Contains(goArgs, "/tmp:rw,nosuid,noexec,size=64m") || !slices.Contains(goArgs, "/go-tmp:rw,nosuid,exec,size=256m") || !slices.Contains(goArgs, "GOTMPDIR=/go-tmp") {
		t.Fatalf("Go container args do not isolate executable build storage: %q", goArgs)
	}
	pythonArgs := containerArgs("/host/work", Manifest{Language: "python3", Image: "python@sha256:abc"})
	if slices.Contains(pythonArgs, "/go-tmp:rw,nosuid,exec,size=256m") || slices.Contains(pythonArgs, "GOTMPDIR=/go-tmp") {
		t.Fatalf("Python unexpectedly received executable temporary storage: %q", pythonArgs)
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

func TestSafeWriteProducesReadOnlyContainerReadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	root := t.TempDir()
	if err := safeWrite(root, "nested/test.py", []byte("pass\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "nested", "test.py"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o444 {
		t.Fatalf("mode=%o, want 444", got)
	}
}
