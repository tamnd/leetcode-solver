package source

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tamnd/leetcode-solver/problem"
)

type Repository interface {
	Catalog(context.Context) ([]CatalogItem, error)
	Problem(context.Context, string) (problem.Problem, error)
	Close() error
}

func Open(path string) (Repository, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sqlite", ".sqlite3", ".db":
		return OpenSQLite(path)
	case ".duckdb":
		return OpenDuckDB(path)
	default:
		return nil, fmt.Errorf("unsupported problem database %q; use .sqlite or .duckdb", path)
	}
}
