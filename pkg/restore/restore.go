// Package restore reconstructs files, directories, or whole trees from a znapshot
// to a destination on disk. The target snapshot is chosen by id, by point in
// time, or by named event via a catalog.Selector.
package restore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/open-tempest-labs/zeaback/pkg/cas"
	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/repo"
)

// Options configures a restore.
type Options struct {
	Selector   catalog.Selector
	PathPrefix string // "" restores the whole tree; else a file or subtree
	Dest       string // destination directory
}

// Stats summarizes a restore.
type Stats struct {
	Snapshot catalog.Snapshot
	Files    int
	Dirs     int
	Symlinks int
	Bytes    int64
}

func matchesPrefix(path, prefix string) bool {
	if prefix == "" {
		return true
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// Run restores from the repository per opts.
func Run(ctx context.Context, r *repo.Repository, opts Options) (Stats, error) {
	var stats Stats
	cat := r.Catalog()

	snaps, err := cat.LoadSnapshots(ctx)
	if err != nil {
		return stats, err
	}
	snap, err := catalog.Resolve(snaps, opts.Selector)
	if err != nil {
		return stats, err
	}
	stats.Snapshot = snap

	nodes, err := cat.LoadNodes(ctx, snap.ID)
	if err != nil {
		return stats, err
	}
	idx, err := cat.LoadChunkIndex(ctx)
	if err != nil {
		return stats, err
	}

	prefix := strings.TrimSuffix(filepath.ToSlash(opts.PathPrefix), "/")
	var selected []catalog.Node
	for _, n := range nodes {
		if matchesPrefix(n.Path, prefix) {
			selected = append(selected, n)
		}
	}
	if len(selected) == 0 {
		return stats, fmt.Errorf("restore: no entries match %q in snapshot %s", opts.PathPrefix, snap.ID)
	}
	// Shallowest paths first so parent directories exist before their contents.
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })

	var dirNodes []catalog.Node
	for _, n := range selected {
		dst := filepath.Join(opts.Dest, filepath.FromSlash(n.Path))
		switch n.Type {
		case catalog.NodeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return stats, fmt.Errorf("restore: mkdir %s: %w", dst, err)
			}
			dirNodes = append(dirNodes, n)
			stats.Dirs++

		case catalog.NodeSymlink:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return stats, err
			}
			_ = os.Remove(dst)
			if err := os.Symlink(n.SymlinkTarget, dst); err != nil {
				return stats, fmt.Errorf("restore: symlink %s: %w", dst, err)
			}
			stats.Symlinks++

		case catalog.NodeFile:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return stats, err
			}
			n := n
			written, err := restoreFile(ctx, r, idx, n, dst)
			if err != nil {
				return stats, err
			}
			stats.Bytes += written
			stats.Files++
		}
	}

	// Apply stored directory permissions after contents are written.
	for _, n := range dirNodes {
		dst := filepath.Join(opts.Dest, filepath.FromSlash(n.Path))
		if err := os.Chmod(dst, os.FileMode(n.Mode)); err != nil {
			return stats, fmt.Errorf("restore: chmod %s: %w", dst, err)
		}
	}

	return stats, nil
}

func restoreFile(ctx context.Context, r *repo.Repository, idx map[string]catalog.ChunkEntry, n catalog.Node, dst string) (int64, error) {
	f, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("restore: create %s: %w", dst, err)
	}
	var written int64
	for _, hx := range n.Chunks {
		e, ok := idx[hx]
		if !ok {
			f.Close()
			return written, fmt.Errorf("restore: missing chunk %s for %s", hx, n.Path)
		}
		id, err := cas.ParseChunkID(hx)
		if err != nil {
			f.Close()
			return written, err
		}
		data, err := cas.ReadBlob(ctx, r.Store(), cas.BlobLoc{
			Hash: id, PackID: e.PackID, Offset: e.Offset, CompLength: e.CompLength, Length: e.Length,
		})
		if err != nil {
			f.Close()
			return written, err
		}
		m, err := f.Write(data)
		if err != nil {
			f.Close()
			return written, fmt.Errorf("restore: write %s: %w", dst, err)
		}
		written += int64(m)
	}
	if err := f.Close(); err != nil {
		return written, fmt.Errorf("restore: close %s: %w", dst, err)
	}
	if err := os.Chmod(dst, os.FileMode(n.Mode)); err != nil {
		return written, fmt.Errorf("restore: chmod %s: %w", dst, err)
	}
	_ = os.Chtimes(dst, n.MTime, n.MTime)
	return written, nil
}
