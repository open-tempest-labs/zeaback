// Package snapshot implements the zeaback backup engine: it walks source paths,
// deduplicates content against the repository via content-defined chunking, and
// records an immutable znapshot (node tree + new chunk/pack manifests + snapshot
// record) in the catalog.
package snapshot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/open-tempest-labs/zeaback/pkg/cas"
	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/repo"
)

// Options configures a backup run.
type Options struct {
	SourcePaths []string
	EventLabel  string
	Tags        map[string]string
	Host        string
}

// Stats summarizes a completed backup.
type Stats struct {
	SnapshotID   string
	ParentID     string
	Files        int
	Dirs         int
	Symlinks     int
	TotalBytes   int64 // logical bytes seen
	NewChunks    int
	NewBytes     int64 // compressed bytes newly written
	ReusedChunks int
}

func newSnapshotID(ts time.Time) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return ts.UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

// contentHash is a stable id for a file derived from its ordered chunk hashes.
func contentHash(hashes []string) string {
	h := sha256.New()
	for _, x := range hashes {
		h.Write([]byte(x))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Create runs a backup, writing a new znapshot to the repository. The parent is
// the most recent existing snapshot; unchanged files (same size and mtime) are
// carried forward without re-reading, and unchanged chunks are deduplicated.
func Create(ctx context.Context, r *repo.Repository, opts Options) (catalog.Snapshot, Stats, error) {
	cat := r.Catalog()
	var stats Stats

	snaps, err := cat.LoadSnapshots(ctx)
	if err != nil {
		return catalog.Snapshot{}, stats, err
	}
	var parent *catalog.Snapshot
	parentNodes := map[string]catalog.Node{}
	if len(snaps) > 0 {
		p := snaps[len(snaps)-1]
		parent = &p
		pn, err := cat.LoadNodes(ctx, p.ID)
		if err != nil {
			return catalog.Snapshot{}, stats, err
		}
		for _, n := range pn {
			parentNodes[n.Path] = n
		}
	}

	li, err := cat.LoadLiveIndex(ctx)
	if err != nil {
		return catalog.Snapshot{}, stats, err
	}

	packer := cas.NewPacker(r.Store(), 0)
	var nodes []catalog.Node
	var newChunks []catalog.ChunkEntry

	addFile := func(storedPath string, info fs.FileInfo, p string) error {
		// Incremental fast path: reuse an unchanged file from the parent.
		if pn, ok := parentNodes[storedPath]; ok && pn.Type == catalog.NodeFile &&
			pn.Size == info.Size() && pn.MTime.UnixNano() == info.ModTime().UnixNano() {
			nodes = append(nodes, pn)
			stats.Files++
			stats.TotalBytes += pn.Size
			stats.ReusedChunks += len(pn.Chunks)
			return nil
		}

		f, err := os.Open(p)
		if err != nil {
			return fmt.Errorf("snapshot: open %s: %w", p, err)
		}
		defer f.Close()

		ch := cas.NewChunker(f, r.Meta().Chunker)
		var hashes []string
		for {
			chunk, err := ch.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("snapshot: chunk %s: %w", p, err)
			}
			id := cas.HashChunk(chunk)
			hx := id.String()
			hashes = append(hashes, hx)
			stats.TotalBytes += int64(len(chunk))
			if li.Has(hx) {
				stats.ReusedChunks++
				continue
			}
			loc, err := packer.Add(ctx, id, chunk)
			if err != nil {
				return err
			}
			li.Add(hx)
			newChunks = append(newChunks, catalog.ChunkEntry{
				Hash: hx, PackID: loc.PackID, Offset: loc.Offset,
				CompLength: loc.CompLength, Length: loc.Length,
			})
			stats.NewChunks++
			stats.NewBytes += loc.CompLength
		}

		uid, gid := fileOwner(info)
		nodes = append(nodes, catalog.Node{
			Path:        storedPath,
			Type:        catalog.NodeFile,
			Mode:        uint32(info.Mode().Perm()),
			UID:         uid,
			GID:         gid,
			Size:        info.Size(),
			MTime:       info.ModTime(),
			ContentHash: contentHash(hashes),
			Chunks:      hashes,
		})
		stats.Files++
		return nil
	}

	for _, src := range opts.SourcePaths {
		absSrc, err := filepath.Abs(src)
		if err != nil {
			return catalog.Snapshot{}, stats, fmt.Errorf("snapshot: resolve %s: %w", src, err)
		}
		base := filepath.Base(absSrc)
		rootFI, err := os.Lstat(absSrc)
		if err != nil {
			return catalog.Snapshot{}, stats, fmt.Errorf("snapshot: stat %s: %w", src, err)
		}

		if !rootFI.IsDir() {
			// Single file (or symlink) source.
			if err := addOne(ctx, base, absSrc, rootFI, &nodes, &stats, addFile); err != nil {
				return catalog.Snapshot{}, stats, err
			}
			continue
		}

		err = filepath.WalkDir(absSrc, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(absSrc, p)
			if err != nil {
				return err
			}
			storedPath := filepath.ToSlash(filepath.Join(base, rel))
			info, err := d.Info()
			if err != nil {
				return err
			}
			return addOne(ctx, storedPath, p, info, &nodes, &stats, addFile)
		})
		if err != nil {
			return catalog.Snapshot{}, stats, fmt.Errorf("snapshot: walk %s: %w", src, err)
		}
	}

	if err := packer.Flush(ctx); err != nil {
		return catalog.Snapshot{}, stats, err
	}
	packInfos, _ := packer.Drain()
	newPacks := make([]catalog.PackEntry, 0, len(packInfos))
	for _, pi := range packInfos {
		newPacks = append(newPacks, catalog.PackEntry{PackID: pi.PackID, Size: pi.Size, ChunkCount: int64(pi.ChunkCount)})
	}

	ts := time.Now().UTC()
	snap := catalog.Snapshot{
		ID:          newSnapshotID(ts),
		Timestamp:   ts,
		EventLabel:  opts.EventLabel,
		Tags:        opts.Tags,
		Host:        opts.Host,
		SourcePaths: opts.SourcePaths,
		Annotations: map[string]string{},
	}
	if parent != nil {
		snap.ParentID = parent.ID
	}

	// Write data-referencing manifests first, then the snapshot record last: a
	// snapshot only becomes discoverable once it is fully written.
	if err := cat.WriteNodes(ctx, snap.ID, nodes); err != nil {
		return catalog.Snapshot{}, stats, err
	}
	if err := cat.WriteBackupManifests(ctx, snap.ID, newChunks, newPacks); err != nil {
		return catalog.Snapshot{}, stats, err
	}
	if err := cat.WriteSnapshotRecord(ctx, snap); err != nil {
		return catalog.Snapshot{}, stats, err
	}

	stats.SnapshotID = snap.ID
	stats.ParentID = snap.ParentID
	return snap, stats, nil
}

// addOne records a directory, symlink, or file node.
func addOne(ctx context.Context, storedPath, p string, info fs.FileInfo,
	nodes *[]catalog.Node, stats *Stats,
	addFile func(string, fs.FileInfo, string) error) error {

	uid, gid := fileOwner(info)
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(p)
		if err != nil {
			return fmt.Errorf("snapshot: readlink %s: %w", p, err)
		}
		*nodes = append(*nodes, catalog.Node{
			Path: storedPath, Type: catalog.NodeSymlink, Mode: uint32(info.Mode().Perm()),
			UID: uid, GID: gid, MTime: info.ModTime(), SymlinkTarget: target,
		})
		stats.Symlinks++
		return nil

	case info.IsDir():
		*nodes = append(*nodes, catalog.Node{
			Path: storedPath, Type: catalog.NodeDir, Mode: uint32(info.Mode().Perm()),
			UID: uid, GID: gid, MTime: info.ModTime(),
		})
		stats.Dirs++
		return nil

	case info.Mode().IsRegular():
		return addFile(storedPath, info, p)

	default:
		// Skip sockets, devices, pipes.
		return nil
	}
}
