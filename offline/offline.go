// Package offline executes versioned evaluation bundles in locked-down containers.
package offline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the only bundle schema this runner executes. Version 2 added
// the mandatory completion marker described by PassMarker.
const SchemaVersion = 2

// PassMarker is the token a bundle harness prints on its own line after the last
// assertion has run. A zero exit status alone is not evidence of correctness: a
// candidate that raises SystemExit while it is being imported, or a harness whose
// assertions were never reached, both exit zero. The runner therefore requires
// the marker and the executed assertion count it carries.
const PassMarker = "LEETCODE_SOLVER_PASS"

// Leading whitespace is tolerated because `go test -v` indents whatever a test
// writes while a subtest is running; the marker still has to own its line, so a
// candidate cannot smuggle it into the tail of another sentence.
var passLine = regexp.MustCompile(`(?m)^[ \t]*` + PassMarker + ` assertions=([0-9]+)[ \t]*\r?$`)

type Manifest struct {
	SchemaVersion   int      `json:"schema_version"`
	ProblemSlug     string   `json:"problem_slug"`
	Language        string   `json:"language"`
	Dataset         string   `json:"dataset"`
	Revision        string   `json:"revision"`
	Image           string   `json:"image"`
	CandidateFile   string   `json:"candidate_file"`
	CandidatePrefix string   `json:"candidate_prefix,omitempty"`
	CandidateSuffix string   `json:"candidate_suffix,omitempty"`
	Command         []string `json:"command"`
	Files           []string `json:"files"`
	TestCount       int      `json:"test_count"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
}
type Result struct {
	Passed bool `json:"passed"`
	// TestCount is the count the bundle declares. AssertionsRun is the count the
	// harness reported actually executing, which is the honest number to publish.
	TestCount     int           `json:"test_count"`
	AssertionsRun int           `json:"assertions_run"`
	Dataset       string        `json:"dataset"`
	Revision      string        `json:"revision"`
	Image         string        `json:"image"`
	Command       []string      `json:"command"`
	Output        string        `json:"output"`
	Elapsed       time.Duration `json:"elapsed"`
}
type Runner struct {
	Root, DockerBinary string
	MaxOutputBytes     int
}

var pinnedImage = regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)

func ReadManifest(root, slug, language string) (Manifest, error) {
	path := filepath.Join(root, slug, language, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	if err := validate(manifest, slug, language); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func (r Runner) Verify(ctx context.Context, slug, language, code string) (Result, error) {
	manifestPath := filepath.Join(r.Root, slug, language, "manifest.json")
	manifest, err := ReadManifest(r.Root, slug, language)
	if err != nil {
		return Result{}, fmt.Errorf("offline eval bundle invalid for %s/%s: %w", slug, language, err)
	}
	work, err := os.MkdirTemp("", "leetcode-eval-*")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	if err := os.Chmod(work, 0o755); err != nil {
		return Result{}, fmt.Errorf("make eval workspace container-readable: %w", err)
	}
	bundleDir := filepath.Dir(manifestPath)
	for _, name := range manifest.Files {
		if err := safeCopy(bundleDir, work, name); err != nil {
			return Result{}, err
		}
	}
	if err := safeWrite(work, manifest.CandidateFile, []byte(manifest.CandidatePrefix+code+manifest.CandidateSuffix)); err != nil {
		return Result{}, err
	}
	docker := r.DockerBinary
	if docker == "" || docker == "auto" {
		docker = "docker"
		if _, err := exec.LookPath(docker); err != nil {
			docker = "podman"
		}
	}
	if _, err := exec.LookPath(docker); err != nil {
		return Result{}, fmt.Errorf("offline evaluation requires docker or podman: %w", err)
	}
	timeout := manifest.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	args := containerArgs(work, manifest)
	args = append(args, manifest.Command...)
	started := time.Now()
	cmd := exec.CommandContext(runCtx, docker, args...)
	output, runErr := cmd.CombinedOutput()
	limit := r.MaxOutputBytes
	if limit <= 0 {
		limit = 64 * 1024
	}
	if len(output) > limit {
		output = output[len(output)-limit:]
	}
	assertions, marked := completion(output)
	result := Result{Passed: runErr == nil && marked, TestCount: manifest.TestCount, AssertionsRun: assertions, Dataset: manifest.Dataset, Revision: manifest.Revision, Image: manifest.Image, Command: manifest.Command, Output: string(output), Elapsed: time.Since(started).Round(time.Millisecond)}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.Passed = false
		return result, fmt.Errorf("offline eval timed out after %ds", timeout)
	}
	if runErr != nil {
		return result, fmt.Errorf("offline eval failed: %w\n%s", runErr, strings.TrimSpace(result.Output))
	}
	if !marked {
		result.Passed = false
		return result, fmt.Errorf("offline eval exited zero without printing %q, so no assertion is known to have run:\n%s", PassMarker, strings.TrimSpace(result.Output))
	}
	if assertions < manifest.TestCount {
		result.Passed = false
		return result, fmt.Errorf("offline eval ran %d of the %d assertions the bundle declares, so this bundle cannot establish correctness", assertions, manifest.TestCount)
	}
	return result, nil
}

// completion reports the executed assertion count a harness published. The runner
// keeps the trailing bytes of a truncated log precisely so this final line survives.
func completion(output []byte) (int, bool) {
	match := passLine.FindAllSubmatch(output, -1)
	if len(match) == 0 {
		return 0, false
	}
	count, err := strconv.Atoi(string(match[len(match)-1][1]))
	if err != nil {
		return 0, false
	}
	return count, true
}

func containerArgs(work string, manifest Manifest) []string {
	args := []string{"run", "--rm", "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=512m", "--cpus=1", "--tmpfs", "/tmp:rw,nosuid,noexec,size=64m", "-e", "PYTHONDONTWRITEBYTECODE=1"}
	if manifest.Language == "golang" {
		// The Go toolchain must execute its compiled test binary. Keep the ordinary
		// temporary directory noexec and provide a bounded build-only tmpfs.
		args = append(args, "--tmpfs", "/go-tmp:rw,nosuid,exec,size=256m", "-e", "GOCACHE=/go-tmp/cache", "-e", "GOTMPDIR=/go-tmp")
	}
	return append(args, "-v", work+":/workspace:ro", "-w", "/workspace", manifest.Image)
}

func validate(m Manifest, slug, language string) error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported eval schema %d, want %d; regenerate the bundle with eval-sync", m.SchemaVersion, SchemaVersion)
	}
	if m.ProblemSlug != slug || m.Language != language {
		return fmt.Errorf("eval identity mismatch: got %s/%s, want %s/%s", m.ProblemSlug, m.Language, slug, language)
	}
	if m.Dataset == "" || m.Revision == "" {
		return errors.New("eval manifest requires dataset and revision")
	}
	if m.Image == "" || m.CandidateFile == "" || len(m.Command) == 0 {
		return errors.New("eval manifest requires image, candidate_file, and command")
	}
	if !pinnedImage.MatchString(m.Image) {
		return errors.New("eval manifest image must be pinned by sha256 digest")
	}
	for _, name := range m.Files {
		if filepath.Clean(name) == filepath.Clean(m.CandidateFile) {
			return errors.New("eval harness files must not replace the candidate file")
		}
	}
	if m.TestCount < 1 {
		return errors.New("eval manifest must declare at least one test")
	}
	if m.Command[0] == "go" && slices.Contains(m.Command, "test") && !slices.Contains(m.Command, "-v") {
		return errors.New("a `go test` command must pass -v: cmd/go discards a passing package's stdout, which would hide the completion marker and fail every correct candidate")
	}
	return nil
}
func safeCopy(fromRoot, toRoot, name string) error {
	clean, err := cleanRelativePath(name)
	if err != nil {
		return err
	}
	source := filepath.Join(fromRoot, clean)
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("eval file %q must be a regular file", name)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return safeWrite(toRoot, clean, data)
}
func safeWrite(root, name string, data []byte) error {
	clean, err := cleanRelativePath(name)
	if err != nil {
		return err
	}
	path := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o444)
}

func cleanRelativePath(name string) (string, error) {
	clean := filepath.Clean(name)
	if name == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe eval path %q", name)
	}
	return clean, nil
}
