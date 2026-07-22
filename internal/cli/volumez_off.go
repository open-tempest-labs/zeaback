//go:build !volumez

package cli

import (
	"fmt"

	"github.com/open-tempest-labs/zeaback/internal/config"
	"github.com/open-tempest-labs/zeaback/pkg/store"
)

// newVolumezStore reports that this build has no volumez support. volumez
// integration is opt-in: rebuild with the "volumez" build tag (see the README)
// to enable it.
func newVolumezStore(config.RepoRef) (store.Store, string, error) {
	return nil, "", fmt.Errorf("this build has no volumez support; rebuild with -tags volumez")
}
