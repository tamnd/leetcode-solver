package agentbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafeSlug = regexp.MustCompile(`[^a-z0-9]+`)

func ScenarioName(r Row) string {
	s := unsafeSlug.ReplaceAllString(strings.ToLower(r.Platform+"-"+r.QuestionID), "-")
	return strings.Trim(s, "-")
}

// Materialize writes visible tasks separately from the hidden oracle. The lab
// mounts only tasks/<name> into an agent container; grading runs later on host.
func Materialize(labRoot, suite string, rows []Row) error {
	base := filepath.Join(labRoot, "evals", suite)
	shared := filepath.Join(base, "oracle", "_lcb")
	for _, name := range []string{"lcb_grade.py", "lcb_testing_util.py"} {
		src := filepath.Join(labRoot, "pkg", "lab", name)
		dst := filepath.Join(shared, strings.TrimPrefix(name, "lcb_"))
		if err := copyFile(src, dst, 0o644); err != nil {
			return err
		}
	}
	for _, row := range rows {
		name := ScenarioName(row)
		task := filepath.Join(base, "tasks", name)
		oracle := filepath.Join(base, "oracle", name)
		if err := os.RemoveAll(task); err != nil {
			return err
		}
		if err := os.RemoveAll(oracle); err != nil {
			return err
		}
		stub := row.StarterCode
		if strings.TrimSpace(stub) == "" {
			stub = "class Solution:\n    pass\n"
		}
		if err := writeFile(filepath.Join(task, "files", "solution.py"), []byte(stub), 0o644); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(oracle, "public.json"), []byte(row.PublicTestCases), 0o644); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(oracle, "private.txt"), []byte(row.PrivateTestCases), 0o600); err != nil {
			return err
		}
		meta := row.Metadata
		if strings.TrimSpace(meta) == "" {
			meta = "{}"
		}
		if err := writeFile(filepath.Join(oracle, "meta.json"), []byte(meta), 0o600); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(task, "prompt.txt"), []byte(Prompt(row)), 0o644); err != nil {
			return err
		}
		desc := fmt.Sprintf("livecodebench v6: LeetCode %s (%s, %s)\n", row.QuestionID, strings.ToLower(row.Difficulty), row.ContestDate)
		if err := writeFile(filepath.Join(task, "desc"), []byte(desc), 0o644); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(task, "tags"), []byte("code\npython\nleetcode\n"), 0o644); err != nil {
			return err
		}
		setup := "#!/usr/bin/env bash\nset -eu\ncp -R \"$(dirname \"$0\")/files/.\" \"$1/\"\n"
		if err := writeFile(filepath.Join(task, "setup.sh"), []byte(setup), 0o755); err != nil {
			return err
		}
		check := "#!/usr/bin/env bash\nset -u\nW=\"$1\"\nD=\"$(cd \"$(dirname \"$0\")\" && pwd)\"\nSUITE=\"$(cd \"$D/../..\" && pwd)\"\nNAME=\"$(basename \"$D\")\"\nLCB=\"$SUITE/oracle/_lcb\"\nPYTHONPATH=\"$LCB\" python3 \"$LCB/grade.py\" \"$W/solution.py\" \"$SUITE/oracle/$NAME\"\n"
		if err := writeFile(filepath.Join(task, "check.sh"), []byte(check), 0o755); err != nil {
			return err
		}
	}
	type problemPin struct {
		ID, Title, Difficulty, ContestDate, Scenario string
	}
	pins := make([]problemPin, 0, len(rows))
	for _, row := range rows {
		pins = append(pins, problemPin{row.QuestionID, row.QuestionTitle, strings.ToLower(row.Difficulty), row.ContestDate, ScenarioName(row)})
	}
	manifest, _ := json.MarshalIndent(struct {
		DatasetRevision, DatasetSHA256, TomoRevision, TomoLabsRevision string
		Problems                                                       []problemPin
	}{DatasetRevision, DatasetSHA256, TomoRevision, TomoLabsRevision, pins}, "", "  ")
	return writeFile(filepath.Join(base, "manifest.json"), append(manifest, '\n'), 0o644)
}

func Prompt(r Row) string {
	return fmt.Sprintf("Solve this LeetCode problem by completing the starter code already present in solution.py.\nWrite only the implementation in solution.py. Keep class Solution and method %s exactly as given because the judge imports and calls it directly. Do not read stdin.\n\nThe environment already has Python, pytest, and Go installed. You may inspect files and run your own tests, but do not install anything or use the network. Hidden judge tests are not present in the workspace.\n\n%s\n\nStarter code:\n\n%s\n", r.FunctionName(), strings.TrimSpace(r.QuestionContent), strings.TrimSpace(r.StarterCode))
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}
func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeFile(dst, b, mode)
}
