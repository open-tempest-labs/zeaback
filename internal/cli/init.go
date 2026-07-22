package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/internal/config"
	"github.com/open-tempest-labs/zeaback/pkg/repo"
)

var (
	initName    string
	initDefault bool
)

var initCmd = &cobra.Command{
	Use:   "init --path DIR",
	Short: "Create a new repository and register it in the config",
	Long: `Create a new repository at a local path and register it in the config.

To target cloud or external storage, point --path at a mounted gateway (volumez,
rclone, s3fs, NFS, ...); zeaback writes to it with ordinary filesystem APIs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if flagPath == "" {
			return fmt.Errorf("init needs --path DIR")
		}
		ref := config.RepoRef{Store: "local", Location: flagPath}

		s, dir, err := storeFromRef(ref)
		if err != nil {
			return err
		}
		r, err := repo.Init(ctx, s, dir)
		if err != nil {
			return err
		}

		name := initName
		if name == "" {
			name = filepath.Base(ref.Location)
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		cfg.Repos[name] = ref
		if initDefault || cfg.DefaultRepo == "" {
			cfg.DefaultRepo = name
		}
		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Printf("Initialized repository %q (id %s)\n", name, r.Meta().ID)
		if cfg.DefaultRepo == name {
			fmt.Println("Set as default repository.")
		}
		return r.Close()
	},
}

func init() {
	initCmd.Flags().StringVar(&initName, "name", "", "config name for the repository (default: basename of path)")
	initCmd.Flags().BoolVar(&initDefault, "default", false, "set as the default repository")
	rootCmd.AddCommand(initCmd)
}
