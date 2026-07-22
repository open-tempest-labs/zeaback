package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/open-tempest-labs/zeaback/internal/config"
	"github.com/open-tempest-labs/zeaback/pkg/repo"
)

var (
	initName       string
	initStore      string
	initBackend    string
	initBackendCfg string
	initDefault    bool
)

var initCmd = &cobra.Command{
	Use:   "init [--path DIR | --store volumez --backend s3 --config JSON]",
	Short: "Create a new repository and register it in the config",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		ref := config.RepoRef{Store: initStore, Location: flagPath, Backend: initBackend}
		if ref.Store == "" {
			ref.Store = "local"
		}
		if initBackendCfg != "" {
			if err := json.Unmarshal([]byte(initBackendCfg), &ref.Config); err != nil {
				return fmt.Errorf("--config: %w", err)
			}
		}
		if ref.Store == "local" && ref.Location == "" {
			return fmt.Errorf("a local repository needs --path DIR")
		}

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
			if ref.Location != "" {
				name = filepath.Base(ref.Location)
			} else {
				name = ref.Backend
			}
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
	initCmd.Flags().StringVar(&initName, "name", "", "config name for the repository (default: basename of path or backend)")
	initCmd.Flags().StringVar(&initStore, "store", "local", "store type: local or volumez")
	initCmd.Flags().StringVar(&initBackend, "backend", "", "volumez backend name (e.g. s3, http)")
	initCmd.Flags().StringVar(&initBackendCfg, "config", "", "volumez backend config as JSON")
	initCmd.Flags().BoolVar(&initDefault, "default", false, "set as the default repository")
	rootCmd.AddCommand(initCmd)
}
