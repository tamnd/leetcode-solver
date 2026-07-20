package source

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tamnd/leetcode-solver/problem"
	_ "modernc.org/sqlite"
)

type SQLite struct {
	db   *sql.DB
	Path string
}

func OpenSQLite(path string) (*SQLite, error) {
	if path == "" {
		return nil, errors.New("SQLite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	source := &SQLite{db: db, Path: path}
	if err := source.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return source, nil
}
func (s *SQLite) Close() error { return s.db.Close() }
func (s *SQLite) init() error {
	_, err := s.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS questions(question_id TEXT PRIMARY KEY,frontend_id TEXT,title_slug TEXT UNIQUE,title TEXT,difficulty TEXT,paid_only INTEGER NOT NULL DEFAULT 0,content_html TEXT,content_md TEXT,hints_json TEXT,example_testcases TEXT,sample_testcase TEXT,meta_data TEXT,code_snippets TEXT,topic_tags TEXT,fetched_at TEXT NOT NULL); CREATE INDEX IF NOT EXISTS idx_questions_frontend ON questions(frontend_id);`)
	return err
}
func (s *SQLite) Catalog(ctx context.Context) ([]CatalogItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT question_id,frontend_id,title,title_slug,difficulty,paid_only,COALESCE(topic_tags,'[]') FROM questions ORDER BY CAST(frontend_id AS INTEGER)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []CatalogItem
	for rows.Next() {
		var item CatalogItem
		var paid int
		var topics string
		if err := rows.Scan(&item.ID, &item.FrontendID, &item.Title, &item.Slug, &item.Difficulty, &paid, &topics); err != nil {
			return nil, err
		}
		item.PaidOnly = paid != 0
		_ = json.Unmarshal([]byte(topics), &item.Topics)
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *SQLite) Problem(ctx context.Context, key string) (problem.Problem, error) {
	var p problem.Problem
	var paid int
	var hints, snippets, topics, fetched string
	err := s.db.QueryRowContext(ctx, `SELECT question_id,frontend_id,title,title_slug,difficulty,paid_only,COALESCE(content_html,''),COALESCE(content_md,''),COALESCE(hints_json,'[]'),COALESCE(example_testcases,''),COALESCE(sample_testcase,''),COALESCE(meta_data,''),COALESCE(code_snippets,'[]'),COALESCE(topic_tags,'[]'),fetched_at FROM questions WHERE title_slug=? OR frontend_id=? LIMIT 1`, key, key).Scan(&p.ID, &p.FrontendID, &p.Title, &p.Slug, &p.Difficulty, &paid, &p.ContentHTML, &p.ContentMarkdown, &hints, &p.ExampleTestcases, &p.SampleTestcase, &p.MetaData, &snippets, &topics, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("problem %q not found in SQLite dataset", key)
	}
	if err != nil {
		return p, err
	}
	p.PaidOnly = paid != 0
	_ = json.Unmarshal([]byte(hints), &p.Hints)
	_ = json.Unmarshal([]byte(topics), &p.Topics)
	_ = json.Unmarshal([]byte(snippets), &p.Snippets)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, fetched)
	if p.ContentHTML == "" && p.ContentMarkdown == "" {
		return p, fmt.Errorf("problem %s has no statement content", p.Slug)
	}
	return p, nil
}
func (s *SQLite) Put(ctx context.Context, p problem.Problem) error {
	hints, _ := json.Marshal(p.Hints)
	snippets, _ := json.Marshal(p.Snippets)
	topics, _ := json.Marshal(p.Topics)
	updated := p.UpdatedAt
	if updated.IsZero() {
		updated = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO questions(question_id,frontend_id,title_slug,title,difficulty,paid_only,content_html,content_md,hints_json,example_testcases,sample_testcase,meta_data,code_snippets,topic_tags,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(question_id) DO UPDATE SET frontend_id=excluded.frontend_id,title_slug=excluded.title_slug,title=excluded.title,difficulty=excluded.difficulty,paid_only=excluded.paid_only,content_html=excluded.content_html,content_md=excluded.content_md,hints_json=excluded.hints_json,example_testcases=excluded.example_testcases,sample_testcase=excluded.sample_testcase,meta_data=excluded.meta_data,code_snippets=excluded.code_snippets,topic_tags=excluded.topic_tags,fetched_at=excluded.fetched_at`, p.ID, p.FrontendID, p.Slug, p.Title, p.Difficulty, boolInt(p.PaidOnly), p.ContentHTML, p.ContentMarkdown, string(hints), p.ExampleTestcases, p.SampleTestcase, p.MetaData, string(snippets), string(topics), updated.Format(time.RFC3339Nano))
	return err
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
