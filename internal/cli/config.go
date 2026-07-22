package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show the zeaback configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		p, _ := config.Path()
		fmt.Fprintf(os.Stderr, "# %s\n", p)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cfg)
	},
}

var configSetDefaultCmd = &cobra.Command{
	Use:   "set-default NAME",
	Short: "Set the default repository",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if _, ok := cfg.Repos[args[0]]; !ok {
			return fmt.Errorf("no repository named %q", args[0])
		}
		cfg.DefaultRepo = args[0]
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Default repository is now %q\n", args[0])
		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetDefaultCmd)
	rootCmd.AddCommand(configCmd)
}
