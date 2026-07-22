package catalog

import "context"

// LiveIndex is an in-memory set of chunk hashes already present in the
// repository. The backup engine loads it once at the start of a run so the
// hot-path "does this chunk already exist?" check is an O(1) map lookup rather
// than a per-chunk query against the columnar catalog (which is optimized for
// scans, not millions of point lookups).
type LiveIndex struct {
	present map[string]struct{}
}

// LoadLiveIndex builds the live-chunk index from the catalog's chunk manifests.
func (c *Catalog) LoadLiveIndex(ctx context.Context) (*LiveIndex, error) {
	idx, err := c.LoadChunkIndex(ctx)
	if err != nil {
		return nil, err
	}
	li := &LiveIndex{present: make(map[string]struct{}, len(idx))}
	for h := range idx {
		li.present[h] = struct{}{}
	}
	return li, nil
}

// Has reports whether a chunk hash is already stored.
func (li *LiveIndex) Has(hash string) bool {
	_, ok := li.present[hash]
	return ok
}

// Add records a newly written chunk hash so duplicate chunks within the same
// backup run are not written twice.
func (li *LiveIndex) Add(hash string) { li.present[hash] = struct{}{} }

// Len returns the number of known chunk hashes.
func (li *LiveIndex) Len() int { return len(li.present) }
