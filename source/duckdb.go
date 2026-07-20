//go:build duckdb

package source

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/tamnd/leetcode-solver/problem"
)

// DuckDB reads the credential-free local dataset produced by the go-mizu
// LeetCode crawler used by the original chatgpt-tool solver.
type DuckDB struct {
	db   *sql.DB
	Path string
}

func OpenDuckDB(path string) (*DuckDB, error) {
	if path == "" {
		return nil, errors.New("leetcode database path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("open LeetCode dataset %s: %w (populate it with the go-mizu LeetCode seed and crawl commands)", path, err)
	}
	db, err := sql.Open("duckdb", path+"?access_mode=read_only")
	if err != nil {
		return nil, err
	}
	source := &DuckDB{db: db, Path: path}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open LeetCode DuckDB: %w", err)
	}
	return source, nil
}
func (d *DuckDB) Close() error { return d.db.Close() }

func (d *DuckDB) Catalog(ctx context.Context) ([]CatalogItem, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT question_id, frontend_id, title, title_slug, difficulty, paid_only, COALESCE(topic_tags,'[]') FROM questions ORDER BY CAST(COALESCE(NULLIF(frontend_id,''),'0') AS INTEGER)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []CatalogItem
	for rows.Next() {
		var item CatalogItem
		var topics string
		if err := rows.Scan(&item.ID, &item.FrontendID, &item.Title, &item.Slug, &item.Difficulty, &item.PaidOnly, &topics); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(topics), &item.Topics)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DuckDB) Problem(ctx context.Context, slug string) (problem.Problem, error) {
	var p problem.Problem
	var hints, snippets, topics string
	var fetched sql.NullTime
	err := d.db.QueryRowContext(ctx, `SELECT question_id, frontend_id, title, title_slug, difficulty, paid_only, COALESCE(content_html,''), COALESCE(content_md,''), COALESCE(hints_json,'[]'), COALESCE(example_testcases,''), COALESCE(sample_testcase,''), COALESCE(meta_data,''), COALESCE(code_snippets,'[]'), COALESCE(topic_tags,'[]'), fetched_at FROM questions WHERE title_slug=? OR frontend_id=? LIMIT 1`, slug, slug).Scan(&p.ID, &p.FrontendID, &p.Title, &p.Slug, &p.Difficulty, &p.PaidOnly, &p.ContentHTML, &p.ContentMarkdown, &hints, &p.ExampleTestcases, &p.SampleTestcase, &p.MetaData, &snippets, &topics, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("problem %q not found in local LeetCode dataset", slug)
	}
	if err != nil {
		return p, err
	}
	_ = json.Unmarshal([]byte(hints), &p.Hints)
	_ = json.Unmarshal([]byte(topics), &p.Topics)
	var raw []struct {
		Lang     string `json:"lang"`
		LangSlug string `json:"langSlug"`
		Code     string `json:"code"`
	}
	if err := json.Unmarshal([]byte(snippets), &raw); err != nil {
		return p, fmt.Errorf("decode code snippets for %s: %w", p.Slug, err)
	}
	for _, s := range raw {
		p.Snippets = append(p.Snippets, problem.CodeSnippet{Language: s.Lang, LanguageSlug: s.LangSlug, Code: s.Code})
	}
	if fetched.Valid {
		p.UpdatedAt = fetched.Time
	} else {
		p.UpdatedAt = time.Now().UTC()
	}
	if p.ContentMarkdown == "" && p.ContentHTML == "" {
		return p, fmt.Errorf("problem %s has no statement content in local dataset", p.Slug)
	}
	return p, nil
}
