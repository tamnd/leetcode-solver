package offline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const nonce = "0123456789abcdef0123456789abcdef"

// marker is what a well-behaved harness prints: the pass token, the nonce the
// runner substituted into the harness, and the count it truly executed.
func marker(assertions string) string {
	return PassMarker + " " + NoncePlaceholder + " assertions=" + assertions
}

// fakeRuntime stands in for docker. It prints the staged harness file verbatim,
// which is the closest a stub can come to the real thing: the file it prints is the
// one the runner just wrote the run's nonce into, so a test cannot pass by guessing
// a nonce it never saw.
func fakeRuntime(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.sh")
	script := `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    *:/workspace:ro)
      cat "${arg%:/workspace:ro}/test.py"
      exit 0
      ;;
  esac
done
exit 3
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeBundle writes a bundle whose harness "runs" by printing body.
func writeBundle(t *testing.T, root string, tests int, body string) {
	t.Helper()
	dir := filepath.Join(root, "two-sum", "python3")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: SchemaVersion, ProblemSlug: "two-sum", Language: "python3", Dataset: "unit", Revision: "1", Image: "python@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CandidateFile: "solution.py", Command: []string{"python3", "test.py"}, Files: []string{"test.py"}, TestCount: tests}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.py"), []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerExecutesValidBundle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake runtime is a shell script")
	}
	root := t.TempDir()
	writeBundle(t, root, 3, marker("3"))
	got, err := (Runner{Root: root, DockerBinary: fakeRuntime(t)}).Verify(context.Background(), "two-sum", "python3", "class Solution: pass")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Passed || got.TestCount != 3 || got.AssertionsRun != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestVerifyRejectsAZeroExitWithNoCompletionMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake runtime is a shell script")
	}
	root := t.TempDir()
	writeBundle(t, root, 3, "# "+NoncePlaceholder+"\ncollecting tests")
	got, err := (Runner{Root: root, DockerBinary: fakeRuntime(t)}).Verify(context.Background(), "two-sum", "python3", "class Solution: pass")
	if err == nil {
		t.Fatal("a run that exited zero without running an assertion was accepted")
	}
	if got.Passed {
		t.Fatalf("%+v", got)
	}
}

// The realistic reward hack: print a success line and exit zero. The line is only
// believed when it carries the nonce this run substituted into the harness, which
// the candidate was written before and cannot have known.
func TestVerifyRejectsAMarkerThatDoesNotCarryThisRunsNonce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake runtime is a shell script")
	}
	root := t.TempDir()
	writeBundle(t, root, 3, "# "+NoncePlaceholder+"\n"+PassMarker+" "+nonce+" assertions=3")
	got, err := (Runner{Root: root, DockerBinary: fakeRuntime(t)}).Verify(context.Background(), "two-sum", "python3", "class Solution: pass")
	if err == nil {
		t.Fatal("a forged completion marker was accepted")
	}
	if got.Passed {
		t.Fatalf("%+v", got)
	}
}

// A bundle whose harness never prints the nonce could not authenticate any marker,
// so running it would only produce a confusing failure. Say so instead.
func TestVerifyRejectsABundleThatCannotAuthenticateItsMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake runtime is a shell script")
	}
	root := t.TempDir()
	writeBundle(t, root, 3, PassMarker+" assertions=3")
	_, err := (Runner{Root: root, DockerBinary: fakeRuntime(t)}).Verify(context.Background(), "two-sum", "python3", "class Solution: pass")
	if err == nil || !strings.Contains(err.Error(), NoncePlaceholder) {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifyRejectsFewerAssertionsThanTheBundleDeclares(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake runtime is a shell script")
	}
	root := t.TempDir()
	writeBundle(t, root, 97, marker("2"))
	got, err := (Runner{Root: root, DockerBinary: fakeRuntime(t)}).Verify(context.Background(), "two-sum", "python3", "class Solution: pass")
	if err == nil {
		t.Fatal("a run that skipped 95 of 97 declared assertions was accepted")
	}
	if got.Passed || got.AssertionsRun != 2 {
		t.Fatalf("%+v", got)
	}
}

// A candidate that can end the process can decide its own verdict, so it never
// reaches the container.
func TestScreenCandidateRefusesCodeThatEndsTheProcess(t *testing.T) {
	for language, code := range map[string]string{
		"python3": "import sys\nsys.exit(0)\n",
		"golang":  "func twoSum(nums []int, t int) []int { os.Exit(0); return nil }",
	} {
		if err := screenCandidate(language, code); err == nil {
			t.Fatalf("%s candidate that exits was accepted", language)
		}
	}
	if err := screenCandidate("python3", "print(\""+PassMarker+" x assertions=9\")"); err == nil {
		t.Fatal("a candidate printing the completion marker was accepted")
	}
	if err := screenCandidate("golang", "func twoSum(nums []int, t int) []int { return nil }"); err != nil {
		t.Fatalf("an ordinary solution was refused: %v", err)
	}
}

func TestCompletionReadsTheLastMarker(t *testing.T) {
	count, ok := completion([]byte("noise\n"+PassMarker+" "+nonce+" assertions=4\n"), nonce)
	if !ok || count != 4 {
		t.Fatalf("count=%d ok=%t", count, ok)
	}
	if _, ok := completion([]byte("PASS 3 tests\n"), nonce); ok {
		t.Fatal("the pre-schema-2 marker was accepted")
	}
	if _, ok := completion([]byte(PassMarker+" assertions=3\n"), nonce); ok {
		t.Fatal("the schema-2 marker was accepted without a nonce")
	}
}

// `go test -v` indents whatever a running test writes, so the marker arrives with
// leading whitespace and surrounded by the framework's own PASS lines.
func TestCompletionReadsAMarkerIndentedByGoTest(t *testing.T) {
	output := "=== RUN   TestCandidate\n    " + PassMarker + " " + nonce + " assertions=10\n--- PASS: TestCandidate (0.00s)\nPASS\nok  \tcommand-line-arguments\t0.001s\n"
	count, ok := completion([]byte(output), nonce)
	if !ok || count != 10 {
		t.Fatalf("count=%d ok=%t", count, ok)
	}
	if _, ok := completion([]byte("the harness prints "+PassMarker+" "+nonce+" assertions=10 when it finishes\n"), nonce); ok {
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
	if _, err := safeCopy(t.TempDir(), t.TempDir(), "../secret", nonce); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestSafeCopySubstitutesTheNonceAndReportsWhetherItWasCarried(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "test.py"), []byte("print(\""+NoncePlaceholder+"\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	carried, err := safeCopy(from, to, "test.py", nonce)
	if err != nil || !carried {
		t.Fatalf("carried=%t err=%v", carried, err)
	}
	staged, err := os.ReadFile(filepath.Join(to, "test.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(staged), nonce) || strings.Contains(string(staged), NoncePlaceholder) {
		t.Fatalf("staged harness = %q", staged)
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
