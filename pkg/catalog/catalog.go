// Package catalog implements zeaback's columnar metadata plane.
//
// Every backup writes an immutable set of Parquet manifests describing the
// snapshot: its record, its file tree (nodes), the chunks it introduced, and the
// packs those chunks landed in. Parquet is the portable source of truth — it
// travels with the archive and is directly queryable by DuckDB, pandas, or the
// sibling Zea tools without zeaback. A DuckDB query path (see the duckdb_arrow
// build tag) sits over these manifests for ad-hoc time-travel and analytics.
//
// Nested values (tag maps, chunk lists, source-path lists) are encoded as JSON
// strings rather than Arrow nested types: this keeps the writer simple and stays
// fully queryable from DuckDB via its json functions. The reserved content_type
// and embedding columns exist from day one so the Phase 2 AI/semantic-restore
// layer is a query, not a schema migration.
package catalog

import (
	"time"

	"github.com/open-tempest-labs/zeaback/pkg/store"
)

// NodeType classifies a filesystem node.
type NodeType string

const (
	NodeFile    NodeType = "file"
	NodeDir     NodeType = "dir"
	NodeSymlink NodeType = "symlink"
)

// Snapshot is a znapshot record: a point-in-time, filesystem-independent backup.
type Snapshot struct {
	ID          string
	Timestamp   time.Time
	EventLabel  string            // optional named event, e.g. "pre-deploy"
	Tags        map[string]string // arbitrary user metadata
	ParentID    string            // parent snapshot for incremental lineage
	Host        string
	SourcePaths []string          // roots that were backed up
	Annotations map[string]string // reserved for AI-generated metadata
}

// Node is one entry in a snapshot's file tree.
type Node struct {
	Path          string
	Type          NodeType
	Mode          uint32
	UID           uint32
	GID           uint32
	Size          int64
	MTime         time.Time
	SymlinkTarget string
	ContentHash   string   // hash of full file content (dedup convenience)
	Chunks        []string // ordered chunk hashes composing a file's content
	ContentType   string   // reserved for AI content classification
	Embedding     string   // reserved for AI (empty in Phase 1)
}

// ChunkEntry maps a content-addressed chunk to its location in a pack.
type ChunkEntry struct {
	Hash       string
	PackID     string
	Offset     int64
	CompLength int64
	Length     int64
}

// PackEntry summarizes a pack file.
type PackEntry struct {
	PackID     string
	Size       int64
	ChunkCount int64
}

// Catalog reads and writes the Parquet manifests of a repository over a store.
type Catalog struct {
	store store.Store
}

// New returns a catalog backed by s.
func New(s store.Store) *Catalog { return &Catalog{store: s} }

func snapshotKey(id string) string { return "meta/snapshots/" + id + ".parquet" }
func nodesKey(id string) string    { return "nodes/" + id + ".parquet" }
func chunksKey(id string) string   { return "meta/chunks/" + id + ".parquet" }
func packsKey(id string) string    { return "meta/packs/" + id + ".parquet" }

const (
	snapshotsPrefix = "meta/snapshots"
	chunksPrefix    = "meta/chunks"
	packsPrefix     = "meta/packs"
)
