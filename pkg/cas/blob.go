package cas

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/open-tempest-labs/zeaback/pkg/store"
)

// ChunkID is the content hash (SHA-256) of a chunk's uncompressed bytes.
type ChunkID [32]byte

// String returns the lowercase hex encoding of the chunk id.
func (id ChunkID) String() string { return hex.EncodeToString(id[:]) }

// HashChunk returns the ChunkID of data.
func HashChunk(data []byte) ChunkID { return sha256.Sum256(data) }

// ParseChunkID parses a hex-encoded chunk id (as stored in the catalog).
func ParseChunkID(s string) (ChunkID, error) {
	var id ChunkID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("cas: parse chunk id: %w", err)
	}
	if len(b) != len(id) {
		return id, fmt.Errorf("cas: chunk id has %d bytes, want %d", len(b), len(id))
	}
	copy(id[:], b)
	return id, nil
}

// Pack file header. Immutable packs begin with this fixed 8-byte header. The
// version/flags leave room for future per-blob encryption without changing the
// on-disk layout of existing packs.
const (
	packMagic   = "ZBK1"
	packVersion = 1
	packHeader  = 8 // magic(4) + version(1) + flags(1) + reserved(2)
)

// flags bits (reserved for future use, e.g. encryption).
const flagNone byte = 0

func packHeaderBytes() []byte {
	h := make([]byte, packHeader)
	copy(h, packMagic)
	h[4] = packVersion
	h[5] = flagNone
	return h
}

// PackKey returns the store key for a pack id.
func PackKey(packID string) string { return "data/" + packID + ".pack" }

// BlobLoc records where a chunk's compressed bytes live within a pack. It is the
// bridge between the data plane and the catalog's chunk index.
type BlobLoc struct {
	Hash       ChunkID
	PackID     string
	Offset     int64 // absolute byte offset within the pack file
	CompLength int64 // number of compressed bytes
	Length     int64 // uncompressed length
}

func deflate(data []byte) ([]byte, error) {
	var b bytes.Buffer
	w, err := flate.NewWriter(&b, flate.DefaultCompression)
	if err != nil {
		return nil, fmt.Errorf("cas: new deflate writer: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("cas: deflate write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("cas: deflate close: %w", err)
	}
	return b.Bytes(), nil
}

func inflate(data []byte, sizeHint int64) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()
	out := bytes.NewBuffer(make([]byte, 0, sizeHint))
	if _, err := io.Copy(out, r); err != nil {
		return nil, fmt.Errorf("cas: inflate: %w", err)
	}
	return out.Bytes(), nil
}

// ReadBlob fetches and decompresses the chunk at loc from the store, verifying
// its content hash.
func ReadBlob(ctx context.Context, s store.Store, loc BlobLoc) ([]byte, error) {
	rc, err := s.GetRange(ctx, PackKey(loc.PackID), loc.Offset, loc.CompLength)
	if err != nil {
		return nil, fmt.Errorf("cas: read blob %s: %w", loc.Hash, err)
	}
	comp, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("cas: read blob %s: %w", loc.Hash, err)
	}
	data, err := inflate(comp, loc.Length)
	if err != nil {
		return nil, err
	}
	if got := HashChunk(data); got != loc.Hash {
		return nil, fmt.Errorf("cas: blob %s hash mismatch (got %s)", loc.Hash, got)
	}
	return data, nil
}
