// Package config manages zeaback's user-level configuration, a JSON file that
// records named repository references so the CLI can address a repo by name.
//
// The config lives at $ZEABACK_HOME/config.json (default ~/.zeaback/config.json).
// It is deliberately hand-rolled JSON with no third-party config library, matching
// the conventions of the sibling Zea projects.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RepoRef describes how to reach a repository.
type RepoRef struct {
	// Store selects the store backend: "local" or "volumez".
	Store string `json:"store"`
	// Location is the local directory for the "local" store. For a local store
	// pointed at a mounted volumez FUSE path, this is that path.
	Location string `json:"location,omitempty"`
	// Backend is the volumez backend name (e.g. "s3", "http") for the "volumez"
	// store.
	Backend string `json:"backend,omitempty"`
	// Config is the backend-specific configuration passed to volumez.
	Config map[string]any `json:"config,omitempty"`
}

// Config is the top-level user configuration.
type Config struct {
	DefaultRepo string             `json:"default_repo,omitempty"`
	Repos       map[string]RepoRef `json:"repos,omitempty"`
}

// Dir returns the zeaback config/state directory, honoring $ZEABACK_HOME.
func Dir() (string, error) {
	if h := os.Getenv("ZEABACK_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: locate home dir: %w", err)
	}
	return filepath.Join(home, ".zeaback"), nil
}

// Path returns the path to the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config file, returning an empty config if it does not exist.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Repos: map[string]RepoRef{}}, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", p, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", p, err)
	}
	if c.Repos == nil {
		c.Repos = map[string]RepoRef{}
	}
	return &c, nil
}

// Save writes the config file, creating the config directory if needed.
func (c *Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: create dir %s: %w", dir, err)
	}
	p := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(p, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("config: write %s: %w", p, err)
	}
	return nil
}

// Resolve returns the repo reference for name. If name is empty, it uses
// DefaultRepo. It returns an error if the repo is not found.
func (c *Config) Resolve(name string) (string, RepoRef, error) {
	if name == "" {
		name = c.DefaultRepo
	}
	if name == "" {
		return "", RepoRef{}, fmt.Errorf("config: no repo specified and no default set")
	}
	ref, ok := c.Repos[name]
	if !ok {
		return "", RepoRef{}, fmt.Errorf("config: repo %q not found", name)
	}
	return name, ref, nil
}
