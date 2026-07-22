// Package cas implements zeaback's content-addressed data plane: content-defined
// chunking, content hashing, and immutable compressed pack files written to a
// store.Store. This is the opaque "payload" half of the two-plane design; the
// queryable metadata catalog lives in package catalog.
package cas

import (
	"io"
)

// ChunkerOptions controls content-defined chunking boundaries.
type ChunkerOptions struct {
	Min int // minimum chunk size in bytes
	Avg int // target average chunk size in bytes (must be a power of two)
	Max int // maximum chunk size in bytes
}

// DefaultChunkerOptions are tuned for general file backup: ~64 KiB average.
var DefaultChunkerOptions = ChunkerOptions{
	Min: 16 * 1024,
	Avg: 64 * 1024,
	Max: 256 * 1024,
}

// FastCDC normalized-chunking masks derived from the default average (2^16).
// maskS has more one-bits (harder boundary) and is used before the average
// point; maskL has fewer (easier boundary) and is used after it.
const (
	maskS uint64 = (1 << 18) - 1
	maskL uint64 = (1 << 14) - 1
)

// gear is a deterministic 256-entry table (splitmix64 from a fixed seed) so
// chunk boundaries are stable across runs and machines.
var gear [256]uint64

func init() {
	x := uint64(0x9E3779B97F4A7C15)
	for i := range gear {
		x += 0x9E3779B97F4A7C15
		z := x
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		z = z ^ (z >> 31)
		gear[i] = z
	}
}

// Chunker splits a reader into content-defined chunks.
type Chunker struct {
	r    io.Reader
	opts ChunkerOptions
	buf  []byte
	err  error
}

// NewChunker returns a chunker over r using the given options.
func NewChunker(r io.Reader, opts ChunkerOptions) *Chunker {
	if opts.Min <= 0 || opts.Avg <= 0 || opts.Max <= 0 {
		opts = DefaultChunkerOptions
	}
	return &Chunker{r: r, opts: opts}
}

// Next returns the next chunk, or io.EOF when the input is exhausted. The
// returned slice is owned by the caller.
func (c *Chunker) Next() ([]byte, error) {
	// Fill the buffer with up to Max bytes (or until EOF) so a cut point can be
	// found within a full window.
	for len(c.buf) < c.opts.Max && c.err == nil {
		tmp := make([]byte, c.opts.Max)
		n, err := c.r.Read(tmp)
		if n > 0 {
			c.buf = append(c.buf, tmp[:n]...)
		}
		if err != nil {
			c.err = err
		}
	}
	if len(c.buf) == 0 {
		if c.err != nil && c.err != io.EOF {
			return nil, c.err
		}
		return nil, io.EOF
	}
	cut := c.cutpoint(c.buf)
	chunk := make([]byte, cut)
	copy(chunk, c.buf[:cut])
	c.buf = append([]byte(nil), c.buf[cut:]...)
	return chunk, nil
}

// cutpoint returns the boundary offset within data using FastCDC.
func (c *Chunker) cutpoint(data []byte) int {
	n := len(data)
	if n <= c.opts.Min {
		return n
	}
	if n > c.opts.Max {
		n = c.opts.Max
	}
	normal := c.opts.Avg
	if normal > n {
		normal = n
	}
	var fp uint64
	i := c.opts.Min
	for ; i < normal; i++ {
		fp = (fp << 1) + gear[data[i]]
		if fp&maskS == 0 {
			return i
		}
	}
	for ; i < n; i++ {
		fp = (fp << 1) + gear[data[i]]
		if fp&maskL == 0 {
			return i
		}
	}
	return n
}
