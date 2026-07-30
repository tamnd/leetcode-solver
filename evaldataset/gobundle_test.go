package evaldataset

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/leetcode-solver/offline"
)

const twoSumSnippet = "func twoSum(nums []int, target int) []int {\n    \n}"

const twoSumCheck = `def check(candidate):
    assert candidate(nums = [2,7,11,15], target = 9) == [0,1]
    assert candidate(nums = [3,2,4], target = 6) == [1,2]
    assert candidate(nums = [3,3], target = 6) == [0,1]
`

func TestGoHarnessDrivesAKeywordDataset(t *testing.T) {
	signature, err := parseGoSignature(twoSumSnippet)
	if err != nil {
		t.Fatal(err)
	}
	cases, err := extractCases(twoSumCheck)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 3 {
		t.Fatalf("cases=%d", len(cases))
	}
	harness, err := goHarness(signature, cases)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "solution_test.go", harness, parser.SkipObjectResolution); err != nil {
		t.Fatalf("generated harness does not parse: %v\n%s", err, harness)
	}
	for _, want := range []string{
		"arg0 []int",
		"arg1 int",
		"want []int",
		"{[]int{2, 7, 11, 15}, 9, []int{0, 1}},",
		"got := twoSum(tc.arg0, tc.arg1)",
		"!reflect.DeepEqual(got, tc.want)",
		offline.PassMarker + " " + offline.NoncePlaceholder + " assertions=%d",
	} {
		if !strings.Contains(harness, want) {
			t.Fatalf("harness is missing %q:\n%s", want, harness)
		}
	}
}

// The dataset states the cases; the harness must replay all of them or none. A body
// it only partly understands would produce a bundle that proves less than the
// Python bundle for the same problem while claiming the same authority.
func TestExtractCasesRefusesAnythingItCannotReplayExactly(t *testing.T) {
	for name, body := range map[string]string{
		"wrapped call":     "def check(candidate):\n    assert candidate(nums = [1]) == [1]\n    assert sorted(candidate(nums = [2,1])) == [1,2]\n",
		"tolerance":        "def check(candidate):\n    assert abs(candidate(x = 2) - 1.41) < 1e-6\n",
		"identity":         "def check(candidate):\n    assert candidate(x = 2) is True\n",
		"constructed want": "def check(candidate):\n    assert candidate(x = 2) == Solution().helper(2)\n",
		"no assertion":     "def check(candidate):\n    pass\n",
	} {
		if _, err := extractCases(body); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestExtractCasesReadsPositionalAndNestedLiterals(t *testing.T) {
	cases, err := extractCases("def check(candidate):\n    assert candidate([[1, 2], [3]], \"a,b\", True, -1.5) == [[1], []]\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || len(cases[0].Args) != 4 {
		t.Fatalf("%+v", cases)
	}
	if cases[0].Args[1].Value != "a,b" || cases[0].Args[2].Value != true || cases[0].Args[3].Value != -1.5 {
		t.Fatalf("%+v", cases[0].Args)
	}
	if cases[0].Args[0].Name != "" {
		t.Fatalf("positional argument was read as the keyword %q", cases[0].Args[0].Name)
	}
}

// A comment containing a comma or a bracket must not be parsed as part of the call.
func TestExtractCasesIgnoresComments(t *testing.T) {
	cases, err := extractCases("def check(candidate):\n    assert candidate(nums = [1, 2]) == 3  # sums [1, 2]\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].Want != int64(3) {
		t.Fatalf("%+v", cases)
	}
}

func TestParseGoSignatureRefusesHarnessesItCannotDrive(t *testing.T) {
	for name, snippet := range map[string]string{
		"design problem":  "type MyQueue struct {\n}\n\nfunc Constructor() MyQueue {\n}",
		"method":          "func (this *MyQueue) Push(x int) {\n}",
		"no return value": "func rotate(nums []int, k int) {\n}",
		"two results":     "func split(s string) (string, string) {\n}",
		"not Go":          "class Solution:\n    pass",
	} {
		if _, err := parseGoSignature(snippet); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestRenderGoHonoursLeetCodesStringToSliceConversions(t *testing.T) {
	for _, tc := range []struct {
		goType string
		value  pyValue
		want   string
	}{
		{"[]byte", "abc", `[]byte("abc")`},
		{"[][]byte", []pyValue{"ab", "cd"}, `[][]byte{[]byte("ab"), []byte("cd")}`},
		{"byte", "a", `'a'`},
		{"[][]int", []pyValue{[]pyValue{int64(1)}, []pyValue{}}, "[][]int{[]int{1}, []int{}}"},
		{"float64", int64(3), "3"},
		{"[]string", []pyValue{"a", "b"}, `[]string{"a", "b"}`},
	} {
		got, err := renderGo(tc.goType, tc.value)
		if err != nil || got != tc.want {
			t.Fatalf("renderGo(%q, %v)=%q,%v want %q", tc.goType, tc.value, got, err, tc.want)
		}
	}
	for _, tc := range []struct {
		goType string
		value  pyValue
	}{
		{"int", "7"},
		{"int", nil},
		{"[]int", "abc"},
		{"*TreeNode", []pyValue{int64(1)}},
		{"map[string]int", []pyValue{}},
		{"byte", "ab"},
	} {
		if got, err := renderGo(tc.goType, tc.value); err == nil {
			t.Fatalf("renderGo(%q, %v) produced %q instead of refusing", tc.goType, tc.value, got)
		}
	}
}

// The two languages must agree about the problem. A keyword the Go signature does
// not declare means they do not, and generating a bundle anyway would produce a
// harness that compiles against the wrong parameter order.
func TestOrderArgsRefusesAnUnknownKeyword(t *testing.T) {
	signature, err := parseGoSignature(twoSumSnippet)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := orderArgs(signature, []pyArg{{Name: "target", Value: int64(9)}, {Name: "nums", Value: []pyValue{int64(1)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ordered[1].(int64); !ok {
		t.Fatalf("keyword arguments were not reordered to the Go parameter order: %+v", ordered)
	}
	if _, err := orderArgs(signature, []pyArg{{Name: "numbers", Value: []pyValue{}}, {Name: "target", Value: int64(9)}}); err == nil {
		t.Fatal("an argument that is not a parameter of the Go function was accepted")
	}
	if _, err := orderArgs(signature, []pyArg{{Name: "nums", Value: []pyValue{}}}); err == nil {
		t.Fatal("a case that omits a parameter was accepted")
	}
}

func TestImportGeneratesGoBundlesAndRecordsEveryGap(t *testing.T) {
	root := t.TempDir()
	rows := []LeetCodeRow{
		{TaskID: "two-sum", Prompt: "from typing import List", EntryPoint: "twoSum", Test: twoSumCheck},
		{TaskID: "add-two-numbers", Prompt: "x", EntryPoint: "addTwoNumbers", Test: "def check(candidate):\n    assert candidate(l1 = [2,4,3]) == [7,0,8]\n"},
		{TaskID: "no-starter", Prompt: "x", EntryPoint: "f", Test: "def check(candidate):\n    assert candidate(x = 1) == 1\n"},
	}
	var lines strings.Builder
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		lines.Write(data)
		lines.WriteByte('\n')
	}
	snippets := map[string]string{
		"two-sum": twoSumSnippet,
		// A linked-list signature is real LeetCode and cannot be driven by literals.
		"add-two-numbers": "func addTwoNumbers(l1 *ListNode, l2 *ListNode) *ListNode {\n}",
	}
	report, err := Import(context.Background(), strings.NewReader(lines.String()), Options{
		Root:        root,
		PythonImage: "python@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		GoImage:     "golang@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Revision:    "hf:test",
		GoSignature: func(slug string) (string, bool) { code, ok := snippets[slug]; return code, ok },
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Bundles != 3 || report.GoBundles != 1 || report.GoTests != 3 {
		t.Fatalf("%+v", report)
	}
	if len(report.GoGaps) != 2 {
		t.Fatalf("gaps=%+v", report.GoGaps)
	}
	for _, gap := range report.GoGaps {
		if gap.Slug == "" || gap.Reason == "" {
			t.Fatalf("a gap was recorded without a reason: %+v", gap)
		}
	}
	// The generated manifest has to satisfy the runner's own validation, including
	// the -v that keeps cmd/go from swallowing the completion marker.
	manifest, err := offline.ReadManifest(root, "two-sum", "golang")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.TestCount != 3 || manifest.CandidatePrefix != "package main\n\n" {
		t.Fatalf("%+v", manifest)
	}
	if _, err := os.Stat(filepath.Join(root, "add-two-numbers", "golang")); !os.IsNotExist(err) {
		t.Fatal("a bundle was written for a problem reported as a gap")
	}
}
