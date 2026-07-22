package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/pkg/compact"
)

var compactThreshold float64

var compactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Reclaim space by rewriting low-liveness packs and dropping dead ones",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := openRepo(ctx)
		if err != nil {
			return err
		}
		defer r.Close()

		st, err := compact.Run(ctx, r, compact.Options{Threshold: compactThreshold})
		if err != nil {
			return err
		}

		fmt.Printf("Compaction complete.\n")
		fmt.Printf("  packs:    %d -> %d  (deleted %d, rewritten %d)\n",
			st.PacksBefore, st.PacksAfter, st.PacksDeleted, st.PacksRewritten)
		fmt.Printf("  chunks:   %d live, %d dead, %d relocated\n",
			st.LiveChunks, st.DeadChunks, st.ChunksRelocated)
		fmt.Printf("  reclaimed: %s\n", humanBytes(st.BytesReclaimed))
		return nil
	},
}

func init() {
	compactCmd.Flags().Float64Var(&compactThreshold, "threshold", compact.DefaultThreshold,
		"rewrite packs whose live-chunk fraction is below this value")
	rootCmd.AddCommand(compactCmd)
}
