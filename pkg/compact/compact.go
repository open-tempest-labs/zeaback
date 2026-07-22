// Package compact implements auto-compaction (garbage collection) of a zeaback
// repository. Chunks reachable from any snapshot are live; packs whose live
// fraction falls below a threshold have their live chunks rewritten into fresh
// packs, and fully-dead packs are dropped. The chunk and pack indexes are then
// consolidated.
package compact

import (
	"context"
	"sort"

	"github.com/open-tempest-labs/zeaback/pkg/cas"
	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/repo"
)

// DefaultThreshold is the live-fraction below which a pack is rewritten.
const DefaultThreshold = 0.5

// Options configures compaction.
type Options struct {
	// Threshold is the live-chunk fraction below which a pack is rewritten.
	Threshold float64
}

// Stats summarizes a compaction run.
type Stats struct {
	PacksBefore     int
	PacksAfter      int
	PacksDeleted    int
	PacksRewritten  int
	ChunksRelocated int
	LiveChunks      int
	DeadChunks      int
	BytesReclaimed  int64
}

// Run compacts the repository.
func Run(ctx context.Context, r *repo.Repository, opts Options) (Stats, error) {
	if opts.Threshold <= 0 {
		opts.Threshold = DefaultThreshold
	}
	var stats Stats
	cat := r.Catalog()

	snaps, err := cat.LoadSnapshots(ctx)
	if err != nil {
		return stats, err
	}
	live := map[string]bool{}
	for _, s := range snaps {
		nodes, err := cat.LoadNodes(ctx, s.ID)
		if err != nil {
			return stats, err
		}
		for _, n := range nodes {
			for _, h := range n.Chunks {
				live[h] = true
			}
		}
	}

	chunkIdx, err := cat.LoadChunkIndex(ctx)
	if err != nil {
		return stats, err
	}
	packIdx, err := cat.LoadPackIndex(ctx)
	if err != nil {
		return stats, err
	}
	stats.PacksBefore = len(packIdx)
	packSize := map[string]int64{}
	for _, p := range packIdx {
		packSize[p.PackID] = p.Size
	}

	byPack := map[string][]catalog.ChunkEntry{}
	for _, e := range chunkIdx {
		byPack[e.PackID] = append(byPack[e.PackID], e)
		if live[e.Hash] {
			stats.LiveChunks++
		} else {
			stats.DeadChunks++
		}
	}

	deadPack := map[string]bool{}
	candPack := map[string]bool{}
	var candidates []string
	for id, entries := range byPack {
		liveN := 0
		for _, e := range entries {
			if live[e.Hash] {
				liveN++
			}
		}
		switch {
		case liveN == 0:
			deadPack[id] = true
		case float64(liveN)/float64(len(entries)) < opts.Threshold:
			candPack[id] = true
			candidates = append(candidates, id)
		}
	}
	sort.Strings(candidates)

	// Relocate live chunks out of low-liveness packs into fresh packs.
	packer := cas.NewPacker(r.Store(), 0)
	relocated := map[string]catalog.ChunkEntry{}
	for _, packID := range candidates {
		for _, e := range byPack[packID] {
			if !live[e.Hash] {
				continue
			}
			id, err := cas.ParseChunkID(e.Hash)
			if err != nil {
				return stats, err
			}
			data, err := cas.ReadBlob(ctx, r.Store(), cas.BlobLoc{
				Hash: id, PackID: e.PackID, Offset: e.Offset, CompLength: e.CompLength, Length: e.Length,
			})
			if err != nil {
				return stats, err
			}
			loc, err := packer.Add(ctx, id, data)
			if err != nil {
				return stats, err
			}
			relocated[e.Hash] = catalog.ChunkEntry{
				Hash: e.Hash, PackID: loc.PackID, Offset: loc.Offset,
				CompLength: loc.CompLength, Length: loc.Length,
			}
			stats.ChunksRelocated++
		}
	}
	if err := packer.Flush(ctx); err != nil {
		return stats, err
	}
	newInfos, _ := packer.Drain()

	if len(deadPack) == 0 && len(candPack) == 0 {
		stats.PacksAfter = stats.PacksBefore
		return stats, nil // nothing to reclaim
	}

	// Build the consolidated chunk index.
	var newChunks []catalog.ChunkEntry
	for _, e := range chunkIdx {
		switch {
		case deadPack[e.PackID]:
			// dropped
		case candPack[e.PackID]:
			if r, ok := relocated[e.Hash]; ok {
				newChunks = append(newChunks, r)
			}
		default:
			newChunks = append(newChunks, e)
		}
	}

	// Build the consolidated pack index and account for reclaimed bytes.
	var newPacks []catalog.PackEntry
	var newBytes int64
	for _, p := range packIdx {
		if deadPack[p.PackID] || candPack[p.PackID] {
			stats.BytesReclaimed += p.Size
			continue
		}
		newPacks = append(newPacks, p)
	}
	for _, pi := range newInfos {
		newPacks = append(newPacks, catalog.PackEntry{PackID: pi.PackID, Size: pi.Size, ChunkCount: int64(pi.ChunkCount)})
		newBytes += pi.Size
	}
	stats.BytesReclaimed -= newBytes
	stats.PacksDeleted = len(deadPack)
	stats.PacksRewritten = len(candPack)
	stats.PacksAfter = len(newPacks)

	// Point the indexes at the new layout, then delete the retired pack files.
	if err := cat.ReplaceChunkAndPackIndex(ctx, newChunks, newPacks); err != nil {
		return stats, err
	}
	for id := range deadPack {
		if err := r.Store().Delete(ctx, cas.PackKey(id)); err != nil {
			return stats, err
		}
	}
	for id := range candPack {
		if err := r.Store().Delete(ctx, cas.PackKey(id)); err != nil {
			return stats, err
		}
	}
	return stats, nil
}
