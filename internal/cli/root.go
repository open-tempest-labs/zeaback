package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "zeaback",
	Short: "zeaback - incremental backup system for the Zea ecosystem",
	Long: `zeaback is an incremental backup system built on zeasnaps: point-in-time,
filesystem-independent snapshots that combine deduplicated content-addressed data
blobs with a queryable columnar metadata catalog.

It supports incremental backups with dedup, auto-compaction, and file, directory,
or whole-tree restore to a point in time or a named event. Archives are portable
and can be copied or moved to cloud/external storage, including through volumez.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// SetVersion sets the version reported by the root command. It is called from
// main with the value injected at build time.
func SetVersion(v string) {
	rootCmd.Version = v
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
