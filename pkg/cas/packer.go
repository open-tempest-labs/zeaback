package cas

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/open-tempest-labs/zeaback/pkg/store"
)

// DefaultPackTarget is the size at which the packer flushes a pack to the store.
const DefaultPackTarget = 16 * 1024 * 1024 // 16 MiB

// Packer accumulates compressed chunks into immutable pack files and flushes
// them to a store. Pack ids are random; chunks within them are content-addressed.
type Packer struct {
	store  store.Store
	target int64

	packID    string
	buf       bytes.Buffer
	locs      []BlobLoc
	completed []flushResult
}

// NewPacker returns a packer writing to s. If target <= 0, DefaultPackTarget is
// used.
func NewPacker(s store.Store, target int64) *Packer {
	if target <= 0 {
		target = DefaultPackTarget
	}
	return &Packer{store: s, target: target}
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (p *Packer) startPack() {
	p.packID = randomID()
	p.buf.Reset()
	p.buf.Write(packHeaderBytes())
	p.locs = p.locs[:0]
}

// Add compresses chunk (whose hash is id) and appends it to the current pack,
// flushing to the store if the pack reaches the target size. It returns the
// blob's location. The pack id in the returned location is final and stable.
func (p *Packer) Add(ctx context.Context, id ChunkID, chunk []byte) (BlobLoc, error) {
	if p.packID == "" {
		p.startPack()
	}
	comp, err := deflate(chunk)
	if err != nil {
		return BlobLoc{}, err
	}
	loc := BlobLoc{
		Hash:       id,
		PackID:     p.packID,
		Offset:     int64(p.buf.Len()),
		CompLength: int64(len(comp)),
		Length:     int64(len(chunk)),
	}
	p.buf.Write(comp)
	p.locs = append(p.locs, loc)
	if int64(p.buf.Len()) >= p.target {
		if err := p.Flush(ctx); err != nil {
			return BlobLoc{}, err
		}
	}
	return loc, nil
}

// PackInfo summarizes a flushed pack for the catalog's pack index.
type PackInfo struct {
	PackID     string
	Size       int64
	ChunkCount int
}

// flushed collects packs completed since the last drain; the snapshot engine
// records these in the catalog.
type flushResult struct {
	info PackInfo
	locs []BlobLoc
}

// Flush writes the current pack (if any) to the store. It is safe to call when
// no pack is open.
func (p *Packer) Flush(ctx context.Context) error {
	if p.packID == "" || p.buf.Len() <= packHeader {
		p.packID = ""
		return nil
	}
	key := PackKey(p.packID)
	data := make([]byte, p.buf.Len())
	copy(data, p.buf.Bytes())
	if err := p.store.Put(ctx, key, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("cas: flush pack %s: %w", p.packID, err)
	}
	p.completed = append(p.completed, flushResult{
		info: PackInfo{PackID: p.packID, Size: int64(len(data)), ChunkCount: len(p.locs)},
		locs: append([]BlobLoc(nil), p.locs...),
	})
	p.packID = ""
	return nil
}

// Drain returns and clears the packs flushed since the last Drain call. Callers
// use it to record pack and chunk metadata in the catalog after Flush.
func (p *Packer) Drain() ([]PackInfo, [][]BlobLoc) {
	infos := make([]PackInfo, 0, len(p.completed))
	locs := make([][]BlobLoc, 0, len(p.completed))
	for _, r := range p.completed {
		infos = append(infos, r.info)
		locs = append(locs, r.locs)
	}
	p.completed = nil
	return infos, locs
}
