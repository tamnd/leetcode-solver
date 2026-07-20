package completedataset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/leetcode-solver/source"
)

func TestImportFileStoresProblemAndSeparateReferences(t *testing.T) {
	database, err := source.OpenSQLite(filepath.Join(t.TempDir(), "leetcode.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	path := filepath.Join(t.TempDir(), "train.jsonl")
	row := `{"acceptance_rate":0.5,"category":"Algorithms","code_snippets":[{"code":"class Solution: pass","lang":"python3"},{"code":"func twoSum() {}","lang":"golang"}],"content":"<p>Statement</p>","created_at_approx":"2015-01-01T00:00:00","difficulty":"Easy","dislikes":2,"example_test_cases":"[2,7]\n9","frontend_id":"1","id":"1","is_paid_only":false,"likes":10,"solutions":[{"lang":"python3","typed_code":"python"},{"lang":"golang","typed_code":"go"}],"title":"Two Sum","title_slug":"two-sum","topic_tags":["Hash Table"],"total_accepted":100,"total_submissions":200,"url":"https://leetcode.com/problems/two-sum/"}`
	if err := os.WriteFile(path, []byte(row+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := importFile(context.Background(), database, path, "revision", "train", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Rows != 1 || report.UniqueProblems != 1 || report.DuplicateIDs != 0 || report.Solutions != 2 || report.PythonSolutions != 1 || report.GoSolutions != 1 {
		t.Fatalf("%+v", report)
	}
	problem, err := database.Problem(context.Background(), "two-sum")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := problem.Snippet("python3"); !ok {
		t.Fatal("Python starter was not imported")
	}
	if strings.Contains(problem.MetaData, "typed_code") {
		t.Fatal("reference solution leaked into solver-visible metadata")
	}
	reference, err := database.ReferenceData(context.Background(), Source, "two-sum")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reference.SolutionsJSON), `"typed_code":"go"`) {
		t.Fatalf("references=%s", reference.SolutionsJSON)
	}
}

func TestImportFilePreservesRenamedSlugReferences(t *testing.T) {
	database, err := source.OpenSQLite(filepath.Join(t.TempDir(), "leetcode.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	path := filepath.Join(t.TempDir(), "train.jsonl")
	rows := `{"code_snippets":[],"content":"<p>Old title</p>","id":"627","frontend_id":"627","solutions":[],"title":"Swap Salary","title_slug":"swap-salary"}
{"code_snippets":[],"content":"<p>New title</p>","id":"627","frontend_id":"627","solutions":[],"title":"Swap Sex of Employees","title_slug":"swap-sex-of-employees"}
`
	if err := os.WriteFile(path, []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := importFile(context.Background(), database, path, "revision", "train", make(map[string]bool))
	if err != nil {
		t.Fatal(err)
	}
	if report.Rows != 2 || report.UniqueProblems != 1 || report.DuplicateIDs != 1 {
		t.Fatalf("%+v", report)
	}
	for _, slug := range []string{"swap-salary", "swap-sex-of-employees"} {
		if _, err := database.ReferenceData(context.Background(), Source, slug); err != nil {
			t.Fatalf("reference %s: %v", slug, err)
		}
	}
}

func TestEnsureFileDownloadsVerifiesAndSupportsOfflineReuse(t *testing.T) {
	content := []byte("verified dataset\n")
	sum := sha256.Sum256(content)
	file := File{Split: "test", Name: "fixture.jsonl", SHA256: hex.EncodeToString(sum[:])}
	client := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(string(content)))}, nil
	})}
	options := Options{Revision: DefaultRevision, CacheDir: t.TempDir()}
	path, err := ensureFile(context.Background(), client, options, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(path, file.SHA256); err != nil {
		t.Fatal(err)
	}
	options.Offline = true
	options.HTTPClient = &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("offline reuse attempted a network request")
		return nil, nil
	})}
	if _, err := ensureFile(context.Background(), options.HTTPClient, options, file); err != nil {
		t.Fatal(err)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (fn roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
