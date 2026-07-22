package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/pkg/catalog"
)

var queryCmd = &cobra.Command{
	Use:   "query SQL",
	Short: "Run SQL over the repository's catalog with DuckDB",
	Long: `Query runs an ad-hoc SQL statement against the repository's Parquet manifests
using DuckDB. Views: snapshots, nodes, chunks, packs. Nested columns (tags,
chunks, source_paths, annotations) are JSON strings — use DuckDB json functions.

Requires a build with the duckdb_arrow tag (use 'make build') and a local
repository.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !catalog.QuerySupported {
			return fmt.Errorf("this build has no DuckDB support; rebuild with 'make build' (tags duckdb_arrow)")
		}
		ctx := context.Background()
		r, err := openRepo(ctx)
		if err != nil {
			return err
		}
		defer r.Close()

		dir := r.LocalDir()
		if dir == "" {
			return fmt.Errorf("query requires a local repository (the store has no filesystem path)")
		}
		return catalog.Query(dir, args[0], os.Stdout)
	},
}

func init() {
	rootCmd.AddCommand(queryCmd)
}
