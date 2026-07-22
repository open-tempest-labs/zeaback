package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/pkg/snapshot"
)

var (
	backupEvent string
	backupTags  []string
)

var backupCmd = &cobra.Command{
	Use:   "backup PATH [PATH...]",
	Short: "Create an incremental zeasnap of the given paths",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := openRepo(ctx)
		if err != nil {
			return err
		}
		defer r.Close()

		tags, err := parseTags(backupTags)
		if err != nil {
			return err
		}
		host, _ := os.Hostname()

		snap, st, err := snapshot.Create(ctx, r, snapshot.Options{
			SourcePaths: args,
			EventLabel:  backupEvent,
			Tags:        tags,
			Host:        host,
		})
		if err != nil {
			return err
		}

		fmt.Printf("Created snapshot %s\n", snap.ID)
		if st.ParentID != "" {
			fmt.Printf("  parent:   %s\n", st.ParentID)
		}
		fmt.Printf("  files:    %d  dirs: %d  symlinks: %d\n", st.Files, st.Dirs, st.Symlinks)
		fmt.Printf("  data:     %s scanned\n", humanBytes(st.TotalBytes))
		fmt.Printf("  new:      %d chunks (%s written)\n", st.NewChunks, humanBytes(st.NewBytes))
		fmt.Printf("  reused:   %d chunks (deduplicated)\n", st.ReusedChunks)
		return nil
	},
}

func init() {
	backupCmd.Flags().StringVar(&backupEvent, "event", "", "label this snapshot with a named event (e.g. pre-deploy)")
	backupCmd.Flags().StringArrayVar(&backupTags, "tag", nil, "attach a key=value tag (repeatable)")
	rootCmd.AddCommand(backupCmd)
}
