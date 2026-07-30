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
	manifest := Manifest{SchemaVersion: SchemaVersion, ProblemSlug: "two-sum", Language: "python3", Dataset: "unit", Revision: "1", Image: "python@test@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CandidateFile: "solution.py", Command: []string{"python3", "test.py"}, Files: []string{"test.py"}, TestCount: 3}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.py"), []byte("pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (Runner{Root: root, DockerBinary: fakeRuntime(t, PassMarker+" assertions=3")}).Verify(context.Background(), "two-sum", "python3", "class Solution: pass")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Passed || got.TestCount != 3 || got.AssertionsRun != 3 {
		t.Fatalf("%+v", got)
	}
}

// fakeRuntime stands in for docker: it exits zero after printing what the test wants
// the harness inside the container to have printed.
func fakeRuntime(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat <<'MARKER'\n"+output+"\nMARKER\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyRejectsAZeroExitWithNoCompletionMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake runtime is a shell script")
	}
	root := t.TempDir()
	writeBundle(t, root, 3)
	got, err := (Runner{Root: root, DockerBinary: fakeRuntime(t, "collecting tests")}).Verify(context.Background(), "two-sum", "python3", "import sys; sys.exit(0)")
	if err == nil {
		t.Fatal("a candidate that exited zero without running an assertion was accepted")
	}
	if got.Passed {
		t.Fatalf("%+v", got)
	}
}

func TestVerifyRejectsFewerAssertionsThanTheBundleDeclares(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake runtime is a shell script")
	}
	root := t.TempDir()
	writeBundle(t, root, 97)
	got, err := (Runner{Root: root, DockerBinary: fakeRuntime(t, PassMarker+" assertions=2")}).Verify(context.Background(), "two-sum", "python3", "class Solution: pass")
	if err == nil {
		t.Fatal("a run that skipped 95 of 97 declared assertions was accepted")
	}
	if got.Passed || got.AssertionsRun != 2 {
		t.Fatalf("%+v", got)
	}
}

func writeBundle(t *testing.T, root string, tests int) {
	t.Helper()
	dir := filepath.Join(root, "two-sum", "python3")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: SchemaVersion, ProblemSlug: "two-sum", Language: "python3", Dataset: "unit", Revision: "1", Image: "python@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CandidateFile: "solution.py", Command: []string{"python3", "test.py"}, Files: []string{"test.py"}, TestCount: tests}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.py"), []byte("pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionReadsTheLastMarker(t *testing.T) {
	count, ok := completion([]byte("noise\n" + PassMarker + " assertions=4\n"))
	if !ok || count != 4 {
		t.Fatalf("count=%d ok=%t", count, ok)
	}
	if _, ok := completion([]byte("PASS 3 tests\n")); ok {
		t.Fatal("the pre-schema-2 marker was accepted")
	}
}

// `go test -v` indents whatever a running test writes, so the marker arrives with
// leading whitespace and surrounded by the framework's own PASS lines.
func TestCompletionReadsAMarkerIndentedByGoTest(t *testing.T) {
	output := "=== RUN   TestTwoSum\n    " + PassMarker + " assertions=10\n--- PASS: TestTwoSum (0.00s)\nPASS\nok  \tcommand-line-arguments\t0.001s\n"
	count, ok := completion([]byte(output))
	if !ok || count != 10 {
		t.Fatalf("count=%d ok=%t", count, ok)
	}
	if _, ok := completion([]byte("the harness prints " + PassMarker + " assertions=10 when it finishes\n")); ok {
		t.Fatal("a marker embedded in prose was accepted")
	}
}

// cmd/go prints only "ok <pkg>" for a passing package unless -v is given, which
// would hide the marker and fail every correct Go candidate.
func TestValidateRequiresVerboseGoTest(t *testing.T) {
	manifest := Manifest{SchemaVersion: SchemaVersion, ProblemSlug: "x", Language: "golang", Dataset: "d", Revision: "r", Image: "golang@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CandidateFile: "solution.go", Command: []string{"go", "test", "-count=1", "solution.go"}, TestCount: 1}
	if err := validate(manifest, "x", "golang"); err == nil {
		t.Fatal("a non-verbose go test command was accepted")
	}
	manifest.Command = []string{"go", "test", "-v", "-count=1", "solution.go"}
	if err := validate(manifest, "x", "golang"); err != nil {
		t.Fatal(err)
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
	err := validate(Manifest{SchemaVersion: SchemaVersion, ProblemSlug: "x", Language: "golang", Dataset: "d", Revision: "r", Image: "go@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CandidateFile: "solution.go", Command: []string{"go"}}, "x", "golang")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateRequiresPinnedImage(t *testing.T) {
	err := validate(Manifest{SchemaVersion: SchemaVersion, ProblemSlug: "x", Language: "golang", Dataset: "d", Revision: "r", Image: "golang:latest", CandidateFile: "solution.go", Command: []string{"go"}, TestCount: 1}, "x", "golang")
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
