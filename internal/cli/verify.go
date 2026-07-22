package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/pkg/verify"
)

var verifyDeep bool

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check repository integrity",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := openRepo(ctx)
		if err != nil {
			return err
		}
		defer r.Close()

		st, err := verify.Run(ctx, r, verify.Options{ReadBlobs: verifyDeep})
		if err != nil {
			return err
		}
		mode := "index-only"
		if verifyDeep {
			mode = "deep (blobs hash-checked)"
		}
		fmt.Printf("OK (%s): %d snapshots, %d files, %d chunk references verified\n",
			mode, st.Snapshots, st.Files, st.Chunks)
		return nil
	},
}

func init() {
	verifyCmd.Flags().BoolVar(&verifyDeep, "deep", false, "fetch and hash-check every referenced blob")
	rootCmd.AddCommand(verifyCmd)
}
