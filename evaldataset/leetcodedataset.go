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
}

var safeSlug = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func ImportLeetCodeDataset(ctx context.Context, r io.Reader, root, image, revision string) (ImportReport, error) {
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
		harness := "with open(\"solution.py\", \"rb\") as candidate_file:\n    exec(compile(candidate_file.read(), \"solution.py\", \"exec\"), globals())\n\n" + row.Test + "\n\ncheck(" + row.EntryPoint + ")\nprint(\"PASS " + fmt.Sprint(tests) + " tests\")\n"
		if err := os.WriteFile(filepath.Join(dir, "test_solution.py"), []byte(harness), 0o600); err != nil {
			return report, err
		}
		manifest := offline.Manifest{SchemaVersion: 1, ProblemSlug: row.TaskID, Language: "python3", Dataset: "newfacade/LeetCodeDataset", Revision: revision, Image: image, CandidateFile: "solution.py", CandidatePrefix: row.Prompt + "\n", Command: []string{"python3", "-I", "test_solution.py"}, Files: []string{"test_solution.py"}, TestCount: tests, TimeoutSeconds: 60}
		data, _ := json.MarshalIndent(manifest, "", "  ")
		data = append(data, '\n')
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
			return report, err
		}
		report.Bundles++
		report.Tests += tests
	}
	return report, scanner.Err()
}
