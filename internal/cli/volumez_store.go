package cli

import (
	"fmt"

	"github.com/open-tempest-labs/zeaback/internal/config"
	"github.com/open-tempest-labs/zeaback/pkg/store"
	"github.com/open-tempest-labs/zeaback/pkg/store/volumez"
)

// newVolumezStore builds a store backed by a volumez storage backend.
func newVolumezStore(ref config.RepoRef) (store.Store, string, error) {
	if ref.Backend == "" {
		return nil, "", fmt.Errorf("volumez store requires a backend name (e.g. s3, http)")
	}
	s, err := volumez.New(ref.Backend, ref.Config)
	if err != nil {
		return nil, "", err
	}
	// A volumez-backed repository has no local filesystem path, so the DuckDB
	// query engine is unavailable against it.
	return s, "", nil
}
