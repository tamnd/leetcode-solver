package agentbench

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRow(id, difficulty, date string) Row {
	return Row{QuestionTitle: "Pinned", QuestionContent: "Return x.", Platform: "leetcode", QuestionID: id, ContestDate: date, StarterCode: "class Solution:\n    def solve(self, x: int) -> int:\n        pass\n", Difficulty: difficulty, PublicTestCases: "[]", PrivateTestCases: "opaque", Metadata: `{"func_name":"solve"}`}
}

func TestSelectLatest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rows.jsonl")
	rows := []string{
		`{"question_title":"old","question_content":"x","platform":"leetcode","question_id":"1","contest_date":"2025-01-01","starter_code":"x","difficulty":"easy","public_test_cases":"[]","private_test_cases":"x","metadata":"{\"func_name\":\"f\"}"}`,
		`{"question_title":"new","question_content":"x","platform":"leetcode","question_id":"2","contest_date":"2025-02-01","starter_code":"x","difficulty":"easy","public_test_cases":"[]","private_test_cases":"x","metadata":"{\"func_name\":\"f\"}"}`,
		`{"question_title":"m","question_content":"x","platform":"leetcode","question_id":"3","contest_date":"2025-01-01","starter_code":"x","difficulty":"medium","public_test_cases":"[]","private_test_cases":"x","metadata":"{\"func_name\":\"f\"}"}`,
		`{"question_title":"h","question_content":"x","platform":"leetcode","question_id":"4","contest_date":"2025-01-01","starter_code":"x","difficulty":"hard","public_test_cases":"[]","private_test_cases":"x","metadata":"{\"func_name\":\"f\"}"}`}
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SelectLatest(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].QuestionID != "2" || got[1].QuestionID != "3" || got[2].QuestionID != "4" {
		t.Fatalf("unexpected selection: %#v", got)
	}
}

func TestMaterializeDoesNotLeakOracleIntoVisibleTaskOrManifest(t *testing.T) {
	lab := t.TempDir()
	for _, name := range []string{"lcb_grade.py", "lcb_testing_util.py"} {
		p := filepath.Join(lab, "pkg", "lab", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("runner"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	row := testRow("99", "hard", "2025-02-01")
	if err := Materialize(lab, "suite", []Row{row}); err != nil {
		t.Fatal(err)
	}
	task := filepath.Join(lab, "evals", "suite", "tasks", ScenarioName(row))
	for _, name := range []string{"prompt.txt", "desc", "tags", "files/solution.py"} {
		b, err := os.ReadFile(filepath.Join(task, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "opaque") {
			t.Fatalf("hidden test leaked into %s", name)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(lab, "evals", "suite", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "opaque") {
		t.Fatal("hidden test leaked into manifest")
	}
}

func TestListCostSeparatesFreshAndCached(t *testing.T) {
	tok := TokenDetail{FreshInput: 1000, CachedInput: 2000, Output: 500}
	got := listCost("gpt-5.6-luna", tok)
	want := .001 + .0002 + .003
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("got %v want %v", got, want)
	}
}
