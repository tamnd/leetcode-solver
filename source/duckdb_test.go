//go:build duckdb

package source

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

func TestDuckDBProblemAndCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leetcode.duckdb")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE questions(question_id VARCHAR,frontend_id VARCHAR,title VARCHAR,title_slug VARCHAR,difficulty VARCHAR,paid_only BOOLEAN,content_html VARCHAR,content_md VARCHAR,hints_json VARCHAR,example_testcases VARCHAR,sample_testcase VARCHAR,meta_data VARCHAR,code_snippets VARCHAR,topic_tags VARCHAR,fetched_at TIMESTAMP); INSERT INTO questions VALUES ('1','1','Two Sum','two-sum','Easy',false,'','Statement','[]','[2,7]\n9','[2,7]\n9','{}','[{"lang":"Python3","langSlug":"python3","code":"class Solution: pass"}]','[{"name":"Array","slug":"array"}]',CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	source, err := OpenDuckDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	p, err := source.Problem(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "two-sum" || p.ContentMarkdown != "Statement" || len(p.Snippets) != 1 || p.Snippets[0].LanguageSlug != "python3" {
		t.Fatalf("%+v", p)
	}
	items, err := source.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("%+v", items)
	}
}
