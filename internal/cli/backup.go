package cli

import (
	"context"
	"fmt"
	"os"
	"os/user"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/snapshot"
)

var (
	backupEvent string
	backupTags  []string
	backupKind  string
	backupActor string
)

// defaultActor returns the current OS user, for labeling who produced a backup.
func defaultActor() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

var backupCmd = &cobra.Command{
	Use:   "backup PATH [PATH...]",
	Short: "Create an incremental znapshot of the given paths",
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
		actor := backupActor
		if actor == "" {
			actor = defaultActor()
		}

		snap, st, err := snapshot.Create(ctx, r, snapshot.Options{
			SourcePaths: args,
			Kind:        backupKind,
			Actor:       actor,
			EventLabel:  backupEvent,
			Tags:        tags,
			Host:        host,
		})
		if err != nil {
			return err
		}

		fmt.Printf("Created snapshot %s\n", snap.ID)
		fmt.Printf("  kind:     %s  (actor: %s)\n", snap.Kind, dashIfEmpty(snap.Actor))
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
	backupCmd.Flags().StringVar(&backupKind, "kind", catalog.KindBackup, "activity type: backup, agent-session, checkpoint, pre-op, ...")
	backupCmd.Flags().StringVar(&backupActor, "actor", "", "who/what produced this snapshot (default: current user)")
	rootCmd.AddCommand(backupCmd)
}
