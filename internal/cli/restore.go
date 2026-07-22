package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/pkg/restore"
)

var (
	restoreSnap   string
	restoreAt     string
	restoreEvent  string
	restoreBefore string
	restorePath   string
)

var restoreCmd = &cobra.Command{
	Use:   "restore DEST",
	Short: "Restore a file, directory, or whole tree to DEST",
	Long: `Restore reconstructs content from a znapshot into the destination directory.

Select the snapshot with --snapshot ID, --at TIME (point-in-time), or --event
LABEL (optionally with --before TIME). Restrict to a subtree or single file with
--path. With no selector, the latest snapshot is used.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := openRepo(ctx)
		if err != nil {
			return err
		}
		defer r.Close()

		sel, err := buildSelector(restoreSnap, restoreAt, restoreEvent, restoreBefore)
		if err != nil {
			return err
		}
		st, err := restore.Run(ctx, r, restore.Options{
			Selector:   sel,
			PathPrefix: restorePath,
			Dest:       args[0],
		})
		if err != nil {
			return err
		}

		fmt.Printf("Restored from snapshot %s (%s)\n", st.Snapshot.ID,
			st.Snapshot.Timestamp.Local().Format("2006-01-02 15:04:05"))
		fmt.Printf("  files: %d  dirs: %d  symlinks: %d  (%s)\n",
			st.Files, st.Dirs, st.Symlinks, humanBytes(st.Bytes))
		return nil
	},
}

func init() {
	restoreCmd.Flags().StringVarP(&restoreSnap, "snapshot", "s", "", "snapshot id or prefix (or 'latest')")
	restoreCmd.Flags().StringVar(&restoreAt, "at", "", "restore the snapshot at or before this time")
	restoreCmd.Flags().StringVar(&restoreEvent, "event", "", "restore the latest snapshot with this event label")
	restoreCmd.Flags().StringVar(&restoreBefore, "before", "", "with --event, constrain to at/before this time")
	restoreCmd.Flags().StringVar(&restorePath, "path", "", "restore only this file or subtree (stored path)")
	rootCmd.AddCommand(restoreCmd)
}
