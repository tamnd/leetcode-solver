// Package evaldataset imports public evaluation data using Go-only tooling.
package evaldataset

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tamnd/leetcode-solver/offline"
)

type LeetCodeRow struct {
	TaskID     string `json:"task_id"`
	Prompt     string `json:"prompt"`
	EntryPoint string `json:"entry_point"`
	Test       string `json:"test"`
}
type ImportReport struct {
	Rows, Bundles, Tests int
	MissingTests         []string
	// GoBundles and GoTests count the Go implementations that could be driven from
	// the same pinned cases; GoGaps names every problem that could not be, with the
	// reason, so an unreachable language is a measured debt rather than a surprise.
	GoBundles, GoTests int
	GoGaps             []GoGap
}

// GoGap records a problem whose Go bundle could not be generated.
type GoGap struct {
	Slug   string `json:"slug"`
	Reason string `json:"reason"`
}

// Options carries what the importer cannot discover from the dataset itself. A Go
// bundle needs the problem's Go starter signature, which lives in the problem
// repository rather than in the dataset rows, so the caller supplies a lookup.
type Options struct {
	Root, PythonImage, GoImage, Revision string
	GoSignature                          func(slug string) (string, bool)
}

var safeSlug = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func ImportLeetCodeDataset(ctx context.Context, r io.Reader, root, image, revision string) (ImportReport, error) {
	return Import(ctx, r, Options{Root: root, PythonImage: image, Revision: revision})
}

func Import(ctx context.Context, r io.Reader, options Options) (ImportReport, error) {
	root, image, revision := options.Root, options.PythonImage, options.Revision
	var report ImportReport
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		default:
		}
		var row LeetCodeRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return report, fmt.Errorf("decode row %d: %w", report.Rows+1, err)
		}
		report.Rows++
		if !safeSlug.MatchString(row.TaskID) {
			return report, fmt.Errorf("unsafe task id %q", row.TaskID)
		}
		tests := strings.Count(row.Test, "assert candidate")
		if tests == 0 || row.EntryPoint == "" {
			report.MissingTests = append(report.MissingTests, row.TaskID)
			continue
		}
		dir := filepath.Join(root, row.TaskID, "python3")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return report, err
		}
		if err := os.WriteFile(filepath.Join(dir, "test_solution.py"), []byte(pythonHarness(row)), 0o600); err != nil {
			return report, err
		}
		manifest := offline.Manifest{SchemaVersion: offline.SchemaVersion, ProblemSlug: row.TaskID, Language: "python3", Dataset: "newfacade/LeetCodeDataset", Revision: revision, Image: image, CandidateFile: "solution.py", CandidatePrefix: row.Prompt + "\n", Command: []string{"python3", "-I", "test_solution.py"}, Files: []string{"test_solution.py"}, TestCount: tests, TimeoutSeconds: 60}
		data, _ := json.MarshalIndent(manifest, "", "  ")
		data = append(data, '\n')
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
			return report, err
		}
		report.Bundles++
		report.Tests += tests
		if err := writeGoBundle(row, options, &report); err != nil {
			return report, err
		}
	}
	return report, scanner.Err()
}

// writeGoBundle generates the Go implementation's bundle from the same pinned cases
// the Python bundle uses. Every refusal is recorded as a gap and none is fatal: a
// problem whose Go harness cannot be generated must not stop the import of the rest
// of the dataset, but it must also never pass silently.
func writeGoBundle(row LeetCodeRow, options Options, report *ImportReport) error {
	if options.GoSignature == nil || options.GoImage == "" {
		return nil
	}
	snippet, ok := options.GoSignature(row.TaskID)
	if !ok {
		report.GoGaps = append(report.GoGaps, GoGap{Slug: row.TaskID, Reason: "no Go starter code in the problem repository"})
		return nil
	}
	signature, err := parseGoSignature(snippet)
	if err != nil {
		report.GoGaps = append(report.GoGaps, GoGap{Slug: row.TaskID, Reason: err.Error()})
		return nil
	}
	cases, err := extractCases(row.Test)
	if err != nil {
		report.GoGaps = append(report.GoGaps, GoGap{Slug: row.TaskID, Reason: err.Error()})
		return nil
	}
	harness, err := goHarness(signature, cases)
	if err != nil {
		report.GoGaps = append(report.GoGaps, GoGap{Slug: row.TaskID, Reason: err.Error()})
		return nil
	}
	dir := filepath.Join(options.Root, row.TaskID, "golang")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "solution_test.go"), []byte(harness), 0o600); err != nil {
		return err
	}
	manifest := goManifest(row.TaskID, options.GoImage, "newfacade/LeetCodeDataset", options.Revision, len(cases))
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o600); err != nil {
		return err
	}
	report.GoBundles++
	report.GoTests += len(cases)
	return nil
}

// pythonHarness wraps the dataset's own check function. Two properties matter more
// than brevity here. The candidate is imported inside a SystemExit guard, because
// a top-level exit would otherwise end the process with status zero before a single
// assertion ran. The entry point is then wrapped in a counter, so the harness can
// publish how many assertions it truly executed instead of how many the importer
// counted in the source text.
func pythonHarness(row LeetCodeRow) string {
	return `import sys

_calls = 0


class _CountedCandidate:
    def __init__(self, target):
        self._target = target

    def __call__(self, *args, **kwargs):
        global _calls
        _calls += 1
        return self._target(*args, **kwargs)


def _load_candidate():
    with open("solution.py", "rb") as candidate_file:
        source = candidate_file.read()
    try:
        exec(compile(source, "solution.py", "exec"), globals())
    except SystemExit as request:
        raise AssertionError("candidate exited while being imported: %r" % (request,))


_load_candidate()

` + row.Test + `

check(_CountedCandidate(` + row.EntryPoint + `))
if _calls < 1:
    raise AssertionError("the dataset check ran no assertion against the candidate")
print("` + offline.PassMarker + ` ` + offline.NoncePlaceholder + ` assertions=%d" % _calls)
sys.stdout.flush()
`
}
