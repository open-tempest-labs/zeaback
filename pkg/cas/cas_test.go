package cas_test

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"testing"

	"github.com/open-tempest-labs/zeaback/pkg/cas"
	"github.com/open-tempest-labs/zeaback/pkg/store/local"
)

func genData(seed int64, n int) []byte {
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	// Semi-compressible, semi-random content so chunking has real boundaries.
	for i := range b {
		if r.Intn(4) == 0 {
			b[i] = byte(r.Intn(256))
		} else {
			b[i] = byte(i % 251)
		}
	}
	return b
}

func chunkAll(t *testing.T, data []byte) ([][]byte, []cas.ChunkID) {
	t.Helper()
	ch := cas.NewChunker(bytes.NewReader(data), cas.DefaultChunkerOptions)
	var chunks [][]byte
	var ids []cas.ChunkID
	total := 0
	for {
		c, err := ch.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("chunk: %v", err)
		}
		chunks = append(chunks, c)
		ids = append(ids, cas.HashChunk(c))
		total += len(c)
	}
	if total != len(data) {
		t.Fatalf("reassembled length %d != %d", total, len(data))
	}
	// Reassembly must reproduce the original bytes exactly.
	var buf bytes.Buffer
	for _, c := range chunks {
		buf.Write(c)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatalf("reassembled bytes differ from original")
	}
	return chunks, ids
}

func TestChunkerDeterministic(t *testing.T) {
	data := genData(1, 1<<20)
	_, ids1 := chunkAll(t, data)
	_, ids2 := chunkAll(t, data)
	if len(ids1) != len(ids2) {
		t.Fatalf("chunk count differs: %d vs %d", len(ids1), len(ids2))
	}
	for i := range ids1 {
		if ids1[i] != ids2[i] {
			t.Fatalf("chunk %d differs between runs", i)
		}
	}
	if len(ids1) < 4 {
		t.Fatalf("expected multiple chunks for 1 MiB, got %d", len(ids1))
	}
}

func TestChunkerDedupResync(t *testing.T) {
	data := genData(2, 1<<20)
	_, idsA := chunkAll(t, data)

	// Insert 7 bytes near the middle; content-defined chunking should resync so
	// most chunks after the edit are unchanged.
	edit := append([]byte{}, data[:500_000]...)
	edit = append(edit, []byte("ZEABACK")...)
	edit = append(edit, data[500_000:]...)
	_, idsB := chunkAll(t, edit)

	setB := map[cas.ChunkID]bool{}
	for _, id := range idsB {
		setB[id] = true
	}
	shared := 0
	for _, id := range idsA {
		if setB[id] {
			shared++
		}
	}
	ratio := float64(shared) / float64(len(idsA))
	if ratio < 0.5 {
		t.Fatalf("dedup resync poor: only %.0f%% of chunks shared after insert", ratio*100)
	}
}

func TestPackerRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := local.New(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer s.Close()

	data := genData(3, 300*1024)
	chunks, ids := chunkAll(t, data)

	// Small target forces multiple packs.
	p := cas.NewPacker(s, 64*1024)
	var locs []cas.BlobLoc
	for i, c := range chunks {
		loc, err := p.Add(ctx, ids[i], c)
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		locs = append(locs, loc)
	}
	if err := p.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	infos, packLocs := p.Drain()
	if len(infos) == 0 {
		t.Fatalf("expected at least one pack")
	}
	countedChunks := 0
	for _, pl := range packLocs {
		countedChunks += len(pl)
	}
	if countedChunks != len(chunks) {
		t.Fatalf("pack chunk count %d != %d", countedChunks, len(chunks))
	}

	for i, loc := range locs {
		got, err := cas.ReadBlob(ctx, s, loc)
		if err != nil {
			t.Fatalf("readblob %d: %v", i, err)
		}
		if !bytes.Equal(got, chunks[i]) {
			t.Fatalf("blob %d content mismatch", i)
		}
	}
}
