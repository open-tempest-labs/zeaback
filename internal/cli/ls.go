package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/pkg/catalog"
)

var (
	lsAt     string
	lsEvent  string
	lsBefore string
)

var lsCmd = &cobra.Command{
	Use:   "ls [SNAPSHOT] [PATH]",
	Short: "Browse the file tree of a znapshot (time-travel exploration)",
	Args:  cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := openRepo(ctx)
		if err != nil {
			return err
		}
		defer r.Close()

		var snapID, pathPrefix string
		if len(args) >= 1 {
			snapID = args[0]
		}
		if len(args) == 2 {
			pathPrefix = args[1]
		}

		sel, err := buildSelector(snapID, lsAt, lsEvent, lsBefore)
		if err != nil {
			return err
		}
		snaps, err := r.Catalog().LoadSnapshots(ctx)
		if err != nil {
			return err
		}
		snap, err := catalog.Resolve(snaps, sel)
		if err != nil {
			return err
		}
		nodes, err := r.Catalog().LoadNodes(ctx, snap.ID)
		if err != nil {
			return err
		}

		prefix := strings.TrimSuffix(pathPrefix, "/")
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Path < nodes[j].Path })

		fmt.Printf("snapshot %s  (%s)\n", snap.ID, snap.Timestamp.Local().Format("2006-01-02 15:04:05"))
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		shown := 0
		for _, n := range nodes {
			if prefix != "" && n.Path != prefix && !strings.HasPrefix(n.Path, prefix+"/") {
				continue
			}
			suffix := ""
			if n.Type == catalog.NodeSymlink {
				suffix = " -> " + n.SymlinkTarget
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s%s\n",
				typeChar(n.Type),
				fs.FileMode(n.Mode).String(),
				humanBytes(n.Size),
				n.Path, suffix,
			)
			shown++
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		if shown == 0 {
			fmt.Printf("(no entries under %q)\n", pathPrefix)
		}
		return nil
	},
}

func typeChar(t catalog.NodeType) string {
	switch t {
	case catalog.NodeDir:
		return "d"
	case catalog.NodeSymlink:
		return "l"
	default:
		return "-"
	}
}

func init() {
	lsCmd.Flags().StringVar(&lsAt, "at", "", "select the snapshot at or before this time")
	lsCmd.Flags().StringVar(&lsEvent, "event", "", "select the latest snapshot with this event label")
	lsCmd.Flags().StringVar(&lsBefore, "before", "", "with --event, constrain to at/before this time")
	rootCmd.AddCommand(lsCmd)
}
