package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var snapshotsCmd = &cobra.Command{
	Use:     "snapshots",
	Aliases: []string{"list", "ls-snapshots"},
	Short:   "List the zeasnaps in the repository",
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
		if len(snaps) == 0 {
			fmt.Println("No snapshots yet.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tTIME\tEVENT\tSOURCES\tTAGS")
		for _, s := range snaps {
			tags := make([]string, 0, len(s.Tags))
			for k, v := range s.Tags {
				tags = append(tags, k+"="+v)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				s.ID,
				s.Timestamp.Local().Format("2006-01-02 15:04:05"),
				dashIfEmpty(s.EventLabel),
				dashIfEmpty(strings.Join(s.SourcePaths, ",")),
				dashIfEmpty(strings.Join(tags, ",")),
			)
		}
		return tw.Flush()
	},
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func init() {
	rootCmd.AddCommand(snapshotsCmd)
}
