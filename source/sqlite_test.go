package source

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamnd/leetcode-solver/problem"
)

func TestSQLiteRoundTrip(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "leetcode.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	want := problem.Problem{ID: "1", FrontendID: "1", Slug: "two-sum", Title: "Two Sum", Difficulty: "Easy", ContentMarkdown: "Statement", UpdatedAt: time.Now().UTC(), Snippets: []problem.CodeSnippet{{Language: "Python3", LanguageSlug: "python3", Code: "class Solution: pass"}}, Topics: []problem.Topic{{Name: "Array", Slug: "array"}}}
	if err := db.Put(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := db.Problem(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != want.Slug || got.Snippets[0].LanguageSlug != "python3" {
		t.Fatalf("%+v", got)
	}
	items, err := db.Catalog(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestConvertSQLite(t *testing.T) {
	ctx := context.Background()
	from, err := OpenSQLite(filepath.Join(t.TempDir(), "from.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = from.Close() }()
	want := problem.Problem{ID: "1", FrontendID: "1", Slug: "two-sum", Title: "Two Sum", Difficulty: "Easy", ContentMarkdown: "Statement"}
	if err := from.Put(ctx, want); err != nil {
		t.Fatal(err)
	}
	to, err := OpenSQLite(filepath.Join(t.TempDir(), "to.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = to.Close() }()
	if err := Convert(ctx, from, to, nil); err != nil {
		t.Fatal(err)
	}
	got, err := to.Problem(ctx, want.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Title != want.Title {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSQLiteReferenceDataRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "references.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	want := ReferenceData{Source: "dataset", Revision: "abc", QuestionID: "1", Slug: "two-sum", SolutionsJSON: []byte(`[{"lang":"golang","typed_code":"code"}]`), MetadataJSON: []byte(`{"split":"train"}`)}
	if err := db.PutReferenceData(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := db.ReferenceData(ctx, want.Source, want.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != want.Revision || string(got.SolutionsJSON) != string(want.SolutionsJSON) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
