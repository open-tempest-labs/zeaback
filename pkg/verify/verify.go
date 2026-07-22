// Package verify checks repository integrity: every chunk referenced by every
// snapshot must exist in the chunk index and its stored blob must decompress and
// match its content hash.
package verify

import (
	"context"
	"fmt"

	"github.com/open-tempest-labs/zeaback/pkg/cas"
	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/repo"
)

// Options configures verification.
type Options struct {
	// ReadBlobs, when true, fetches and hash-checks every referenced chunk
	// (thorough but slower). When false, only index presence is checked.
	ReadBlobs bool
}

// Stats summarizes a verification run.
type Stats struct {
	Snapshots int
	Files     int
	Chunks    int
}

// Run verifies the repository, returning an error describing the first problem
// found.
func Run(ctx context.Context, r *repo.Repository, opts Options) (Stats, error) {
	var stats Stats
	cat := r.Catalog()

	snaps, err := cat.LoadSnapshots(ctx)
	if err != nil {
		return stats, err
	}
	idx, err := cat.LoadChunkIndex(ctx)
	if err != nil {
		return stats, err
	}

	// Cache blob checks so a chunk shared across snapshots is read once.
	checked := map[string]bool{}

	for _, s := range snaps {
		stats.Snapshots++
		nodes, err := cat.LoadNodes(ctx, s.ID)
		if err != nil {
			return stats, err
		}
		for _, n := range nodes {
			if n.Type != catalog.NodeFile {
				continue
			}
			stats.Files++
			for _, h := range n.Chunks {
				stats.Chunks++
				e, ok := idx[h]
				if !ok {
					return stats, fmt.Errorf("verify: snapshot %s file %q references missing chunk %s", s.ID, n.Path, h)
				}
				if !opts.ReadBlobs || checked[h] {
					continue
				}
				id, err := cas.ParseChunkID(h)
				if err != nil {
					return stats, err
				}
				if _, err := cas.ReadBlob(ctx, r.Store(), cas.BlobLoc{
					Hash: id, PackID: e.PackID, Offset: e.Offset, CompLength: e.CompLength, Length: e.Length,
				}); err != nil {
					return stats, fmt.Errorf("verify: snapshot %s file %q: %w", s.ID, n.Path, err)
				}
				checked[h] = true
			}
		}
	}
	return stats, nil
}
