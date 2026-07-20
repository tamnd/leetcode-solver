//go:build !duckdb

package source

import "fmt"

func OpenDuckDB(path string) (Repository, error) {
	return nil, fmt.Errorf("DuckDB support is not in this CGO-free default build; rebuild with -tags duckdb or convert %s to SQLite", path)
}
