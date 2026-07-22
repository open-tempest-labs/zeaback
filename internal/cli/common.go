package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/open-tempest-labs/zeaback/internal/config"
	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/repo"
	"github.com/open-tempest-labs/zeaback/pkg/store"
	"github.com/open-tempest-labs/zeaback/pkg/store/local"
)

var (
	flagRepo string
	flagPath string
)

func init() {
	rootCmd.PersistentFlags().StringVar(&flagRepo, "repo", "", "named repository from ~/.zeaback/config.json (defaults to the configured default)")
	rootCmd.PersistentFlags().StringVar(&flagPath, "path", "", "local repository path (bypasses --repo and the config file)")
}

// storeFromRef builds a store from a repository reference, returning the store
// and its filesystem directory.
//
// zeaback targets cloud/external storage by writing to a local directory that is
// a mounted gateway (volumez, rclone, s3fs, NFS, ...); there is no gateway-
// specific coupling. A native cloud store may be added in the future.
func storeFromRef(ref config.RepoRef) (store.Store, string, error) {
	switch ref.Store {
	case "", "local":
		s, err := local.New(ref.Location)
		return s, ref.Location, err
	default:
		return nil, "", fmt.Errorf("unsupported store type %q; use a local path (a mounted cloud/FUSE gateway such as volumez works as a local path)", ref.Store)
	}
}

// resolveRef determines the repository reference from flags and config.
func resolveRef() (string, config.RepoRef, error) {
	if flagPath != "" {
		return flagPath, config.RepoRef{Store: "local", Location: flagPath}, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", config.RepoRef{}, err
	}
	return cfg.Resolve(flagRepo)
}

// openRepo opens the selected repository.
func openRepo(ctx context.Context) (*repo.Repository, error) {
	_, ref, err := resolveRef()
	if err != nil {
		return nil, err
	}
	s, dir, err := storeFromRef(ref)
	if err != nil {
		return nil, err
	}
	return repo.Open(ctx, s, dir)
}

// parseTime accepts RFC3339, "2006-01-02 15:04:05", or "2006-01-02" (local).
func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q (use RFC3339 or YYYY-MM-DD[ HH:MM:SS])", s)
}

// buildSelector assembles a snapshot selector from the common resolution flags.
func buildSelector(snapID, at, event, before string) (catalog.Selector, error) {
	sel := catalog.Selector{ID: snapID, Event: event}
	if at != "" {
		t, err := parseTime(at)
		if err != nil {
			return sel, err
		}
		sel.At = &t
	}
	if before != "" {
		t, err := parseTime(before)
		if err != nil {
			return sel, err
		}
		sel.Before = &t
	}
	return sel, nil
}

func parseTags(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --tag %q (want key=value)", p)
		}
		m[k] = v
	}
	return m, nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func shortID(id string) string {
	if len(id) > 20 {
		return id[:20]
	}
	return id
}
