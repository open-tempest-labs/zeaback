//go:build duckdb_arrow

package catalog

import (
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// QuerySupported reports whether this build includes the DuckDB query engine.
const QuerySupported = true

// Query runs an ad-hoc SQL statement against the repository's Parquet manifests
// using DuckDB, writing a tab-aligned result table to w. repoDir must be the
// local filesystem directory of the repository.
//
// Four views are exposed over the manifests: snapshots, nodes, chunks, packs.
// Nested columns (tags, chunks, source_paths, annotations) are JSON strings, so
// use DuckDB's json functions to unpack them.
func Query(repoDir, query string, w io.Writer) error {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return fmt.Errorf("catalog: open duckdb: %w", err)
	}
	defer db.Close()

	views := map[string]string{
		"snapshots": filepath.Join(repoDir, "meta", "snapshots", "*.parquet"),
		"nodes":     filepath.Join(repoDir, "nodes", "*.parquet"),
		"chunks":    filepath.Join(repoDir, "meta", "chunks", "*.parquet"),
		"packs":     filepath.Join(repoDir, "meta", "packs", "*.parquet"),
	}
	for name, glob := range views {
		stmt := fmt.Sprintf(
			"CREATE VIEW %s AS SELECT * FROM read_parquet('%s', union_by_name=true)",
			name, strings.ReplaceAll(glob, "'", "''"),
		)
		if _, err := db.Exec(stmt); err != nil {
			// A missing manifest kind (e.g. no snapshots yet) is not fatal; the
			// view simply won't exist for the query.
			continue
		}
	}

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("catalog: query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("catalog: columns: %w", err)
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(cols, "\t"))

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("catalog: scan: %w", err)
		}
		cells := make([]string, len(cols))
		for i, v := range vals {
			cells[i] = formatCell(v)
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("catalog: rows: %w", err)
	}
	return tw.Flush()
}

func formatCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
