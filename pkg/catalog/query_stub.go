//go:build !duckdb_arrow

package catalog

import (
	"fmt"
	"io"
)

// QuerySupported reports whether this build includes the DuckDB query engine.
const QuerySupported = false

// Query is unavailable in builds without the duckdb_arrow tag. Build with
// `make build` (or `go build -tags duckdb_arrow`) to enable it.
func Query(repoDir, query string, w io.Writer) error {
	return fmt.Errorf("catalog: query requires a build with the duckdb_arrow tag")
}
