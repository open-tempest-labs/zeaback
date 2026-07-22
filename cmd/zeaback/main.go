// Command zeaback is the CLI entrypoint for the zeaback backup system.
//
// zeaback creates znapshots: point-in-time, filesystem-independent snapshots that
// combine deduplicated content-addressed data blobs with a queryable columnar
// metadata catalog, enabling incremental backups, auto-compaction, and
// file/directory/whole-tree restore to a point in time or a named event.
package main

import (
	"fmt"
	"os"

	"github.com/open-tempest-labs/zeaback/internal/cli"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cli.SetVersion(version)
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "zeaback:", err)
		os.Exit(1)
	}
}
