package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/pkg/catalog"
)

var forgetCmd = &cobra.Command{
	Use:   "forget SNAPSHOT",
	Short: "Make a zeasnap unreachable (its exclusive chunks are reclaimed by compact)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := openRepo(ctx)
		if err != nil {
			return err
		}
		defer r.Close()

		snaps, err := r.Catalog().LoadSnapshots(ctx)
		if err != nil {
			return err
		}
		snap, err := catalog.Resolve(snaps, catalog.Selector{ID: args[0]})
		if err != nil {
			return err
		}
		if err := r.Catalog().DeleteSnapshot(ctx, snap.ID); err != nil {
			return err
		}
		fmt.Printf("Forgot snapshot %s. Run 'zeaback compact' to reclaim space.\n", snap.ID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(forgetCmd)
}
