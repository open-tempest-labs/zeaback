package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

const rowGroupSize = 1 << 20

func strField(name string) arrow.Field {
	return arrow.Field{Name: name, Type: arrow.BinaryTypes.String}
}
func i64Field(name string) arrow.Field {
	return arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int64}
}
func u32Field(name string) arrow.Field {
	return arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Uint32}
}

var (
	snapshotSchema = arrow.NewSchema([]arrow.Field{
		strField("id"), i64Field("timestamp"), strField("kind"), strField("actor"),
		strField("event_label"), strField("tags"), strField("parent_id"),
		strField("host"), strField("source_paths"), strField("annotations"),
	}, nil)

	nodeSchema = arrow.NewSchema([]arrow.Field{
		strField("snapshot_id"), strField("path"), strField("type"),
		u32Field("mode"), u32Field("uid"), u32Field("gid"),
		i64Field("size"), i64Field("mtime"), strField("symlink_target"),
		strField("content_hash"), strField("chunks"),
		strField("content_type"), strField("embedding"),
	}, nil)

	chunkSchema = arrow.NewSchema([]arrow.Field{
		strField("hash"), strField("pack_id"),
		i64Field("offset"), i64Field("comp_length"), i64Field("length"),
	}, nil)

	packSchema = arrow.NewSchema([]arrow.Field{
		strField("pack_id"), i64Field("size"), i64Field("chunk_count"),
	}, nil)
)

func encJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func decStrSlice(s string) []string {
	if s == "" {
		return nil
	}
	var v []string
	_ = json.Unmarshal([]byte(s), &v)
	return v
}

func decStrMap(s string) map[string]string {
	if s == "" {
		return nil
	}
	var v map[string]string
	_ = json.Unmarshal([]byte(s), &v)
	return v
}

func str(b *array.RecordBuilder, col int, v string) {
	b.Field(col).(*array.StringBuilder).Append(v)
}
func i64(b *array.RecordBuilder, col int, v int64) {
	b.Field(col).(*array.Int64Builder).Append(v)
}
func u32(b *array.RecordBuilder, col int, v uint32) {
	b.Field(col).(*array.Uint32Builder).Append(v)
}

// writeParquet builds a single-record Parquet file from schema via build and
// stores it at key.
func (c *Catalog) writeParquet(ctx context.Context, key string, schema *arrow.Schema, build func(b *array.RecordBuilder)) error {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	build(rb)
	rec := rb.NewRecord()
	defer rec.Release()
	tbl := array.NewTableFromRecords(schema, []arrow.Record{rec})
	defer tbl.Release()

	var buf bytes.Buffer
	wrProps := parquet.NewWriterProperties(parquet.WithCompression(compress.Codecs.Snappy))
	arrProps := pqarrow.NewArrowWriterProperties(pqarrow.WithStoreSchema())
	if err := pqarrow.WriteTable(tbl, &buf, rowGroupSize, wrProps, arrProps); err != nil {
		return fmt.Errorf("catalog: write parquet %s: %w", key, err)
	}
	if err := c.store.Put(ctx, key, bytes.NewReader(buf.Bytes())); err != nil {
		return fmt.Errorf("catalog: store parquet %s: %w", key, err)
	}
	return nil
}

// readTable reads the Parquet object at key into an Arrow table. The caller must
// Release the returned table.
func (c *Catalog) readTable(ctx context.Context, key string) (arrow.Table, error) {
	rc, err := c.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("catalog: read %s: %w", key, err)
	}
	pf, err := file.NewParquetReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("catalog: open parquet %s: %w", key, err)
	}
	fr, err := pqarrow.NewFileReader(pf, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		return nil, fmt.Errorf("catalog: arrow reader %s: %w", key, err)
	}
	tbl, err := fr.ReadTable(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: read table %s: %w", key, err)
	}
	return tbl, nil
}

func colStr(tbl arrow.Table, col int) []string {
	var out []string
	for _, a := range tbl.Column(col).Data().Chunks() {
		sa := a.(*array.String)
		for i := 0; i < sa.Len(); i++ {
			if sa.IsNull(i) {
				out = append(out, "")
			} else {
				out = append(out, sa.Value(i))
			}
		}
	}
	return out
}

func colI64(tbl arrow.Table, col int) []int64 {
	var out []int64
	for _, a := range tbl.Column(col).Data().Chunks() {
		ia := a.(*array.Int64)
		for i := 0; i < ia.Len(); i++ {
			out = append(out, ia.Value(i))
		}
	}
	return out
}

func colU32(tbl arrow.Table, col int) []uint32 {
	var out []uint32
	for _, a := range tbl.Column(col).Data().Chunks() {
		ua := a.(*array.Uint32)
		for i := 0; i < ua.Len(); i++ {
			out = append(out, ua.Value(i))
		}
	}
	return out
}

// WriteSnapshotRecord writes the one-row snapshot manifest.
func (c *Catalog) WriteSnapshotRecord(ctx context.Context, s Snapshot) error {
	kind := s.Kind
	if kind == "" {
		kind = KindBackup
	}
	return c.writeParquet(ctx, snapshotKey(s.ID), snapshotSchema, func(b *array.RecordBuilder) {
		str(b, 0, s.ID)
		i64(b, 1, s.Timestamp.UnixNano())
		str(b, 2, kind)
		str(b, 3, s.Actor)
		str(b, 4, s.EventLabel)
		str(b, 5, encJSON(s.Tags))
		str(b, 6, s.ParentID)
		str(b, 7, s.Host)
		str(b, 8, encJSON(s.SourcePaths))
		str(b, 9, encJSON(s.Annotations))
	})
}

// WriteNodes writes the node (file-tree) manifest for a snapshot.
func (c *Catalog) WriteNodes(ctx context.Context, snapID string, nodes []Node) error {
	return c.writeParquet(ctx, nodesKey(snapID), nodeSchema, func(b *array.RecordBuilder) {
		for _, n := range nodes {
			str(b, 0, snapID)
			str(b, 1, n.Path)
			str(b, 2, string(n.Type))
			u32(b, 3, n.Mode)
			u32(b, 4, n.UID)
			u32(b, 5, n.GID)
			i64(b, 6, n.Size)
			i64(b, 7, n.MTime.UnixNano())
			str(b, 8, n.SymlinkTarget)
			str(b, 9, n.ContentHash)
			str(b, 10, encJSON(n.Chunks))
			str(b, 11, n.ContentType)
			str(b, 12, n.Embedding)
		}
	})
}

// WriteChunks writes the chunk manifest introduced by a backup.
func (c *Catalog) WriteChunks(ctx context.Context, key string, chunks []ChunkEntry) error {
	return c.writeParquet(ctx, key, chunkSchema, func(b *array.RecordBuilder) {
		for _, ch := range chunks {
			str(b, 0, ch.Hash)
			str(b, 1, ch.PackID)
			i64(b, 2, ch.Offset)
			i64(b, 3, ch.CompLength)
			i64(b, 4, ch.Length)
		}
	})
}

// WritePacks writes the pack manifest introduced by a backup.
func (c *Catalog) WritePacks(ctx context.Context, key string, packs []PackEntry) error {
	return c.writeParquet(ctx, key, packSchema, func(b *array.RecordBuilder) {
		for _, p := range packs {
			str(b, 0, p.PackID)
			i64(b, 1, p.Size)
			i64(b, 2, p.ChunkCount)
		}
	})
}

// WriteBackupManifests writes all per-backup manifests (chunks and packs keyed
// by the snapshot id).
func (c *Catalog) WriteBackupManifests(ctx context.Context, snapID string, chunks []ChunkEntry, packs []PackEntry) error {
	if err := c.WriteChunks(ctx, chunksKey(snapID), chunks); err != nil {
		return err
	}
	return c.WritePacks(ctx, packsKey(snapID), packs)
}

// colIndex returns the position of a column by name, or -1 if absent. Reading by
// name (rather than position) lets the snapshot schema gain fields over time
// without breaking manifests written by older versions.
func colIndex(tbl arrow.Table, name string) int {
	for i := 0; i < int(tbl.NumCols()); i++ {
		if tbl.Schema().Field(i).Name == name {
			return i
		}
	}
	return -1
}

// firstStr returns the first value of a string column by name, or "" if the
// column is absent or empty.
func firstStr(tbl arrow.Table, name string) string {
	i := colIndex(tbl, name)
	if i < 0 {
		return ""
	}
	if vals := colStr(tbl, i); len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// firstI64 returns the first value of an int64 column by name, or 0 if absent.
func firstI64(tbl arrow.Table, name string) int64 {
	i := colIndex(tbl, name)
	if i < 0 {
		return 0
	}
	if vals := colI64(tbl, i); len(vals) > 0 {
		return vals[0]
	}
	return 0
}

func (c *Catalog) readSnapshot(ctx context.Context, key string) (Snapshot, error) {
	tbl, err := c.readTable(ctx, key)
	if err != nil {
		return Snapshot{}, err
	}
	defer tbl.Release()
	if tbl.NumRows() == 0 {
		return Snapshot{}, fmt.Errorf("catalog: empty snapshot manifest %s", key)
	}
	kind := firstStr(tbl, "kind")
	if kind == "" {
		kind = KindBackup
	}
	return Snapshot{
		ID:          firstStr(tbl, "id"),
		Timestamp:   time.Unix(0, firstI64(tbl, "timestamp")).UTC(),
		Kind:        kind,
		Actor:       firstStr(tbl, "actor"),
		EventLabel:  firstStr(tbl, "event_label"),
		Tags:        decStrMap(firstStr(tbl, "tags")),
		ParentID:    firstStr(tbl, "parent_id"),
		Host:        firstStr(tbl, "host"),
		SourcePaths: decStrSlice(firstStr(tbl, "source_paths")),
		Annotations: decStrMap(firstStr(tbl, "annotations")),
	}, nil
}

// LoadSnapshots reads every snapshot record, sorted oldest-first by timestamp.
func (c *Catalog) LoadSnapshots(ctx context.Context) ([]Snapshot, error) {
	keys, err := c.store.List(ctx, snapshotsPrefix)
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	for _, k := range keys {
		s, err := c.readSnapshot(ctx, k)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, s)
	}
	sortSnapshots(snaps)
	return snaps, nil
}

// LoadNodes reads the file tree of a snapshot.
func (c *Catalog) LoadNodes(ctx context.Context, snapID string) ([]Node, error) {
	tbl, err := c.readTable(ctx, nodesKey(snapID))
	if err != nil {
		return nil, err
	}
	defer tbl.Release()
	paths := colStr(tbl, 1)
	types := colStr(tbl, 2)
	modes := colU32(tbl, 3)
	uids := colU32(tbl, 4)
	gids := colU32(tbl, 5)
	sizes := colI64(tbl, 6)
	mtimes := colI64(tbl, 7)
	links := colStr(tbl, 8)
	hashes := colStr(tbl, 9)
	chunks := colStr(tbl, 10)
	ctypes := colStr(tbl, 11)
	embs := colStr(tbl, 12)

	nodes := make([]Node, len(paths))
	for i := range paths {
		nodes[i] = Node{
			Path:          paths[i],
			Type:          NodeType(types[i]),
			Mode:          modes[i],
			UID:           uids[i],
			GID:           gids[i],
			Size:          sizes[i],
			MTime:         time.Unix(0, mtimes[i]).UTC(),
			SymlinkTarget: links[i],
			ContentHash:   hashes[i],
			Chunks:        decStrSlice(chunks[i]),
			ContentType:   ctypes[i],
			Embedding:     embs[i],
		}
	}
	return nodes, nil
}

func (c *Catalog) readChunks(ctx context.Context, key string) ([]ChunkEntry, error) {
	tbl, err := c.readTable(ctx, key)
	if err != nil {
		return nil, err
	}
	defer tbl.Release()
	hashes := colStr(tbl, 0)
	packs := colStr(tbl, 1)
	offs := colI64(tbl, 2)
	comps := colI64(tbl, 3)
	lens := colI64(tbl, 4)
	out := make([]ChunkEntry, len(hashes))
	for i := range hashes {
		out[i] = ChunkEntry{Hash: hashes[i], PackID: packs[i], Offset: offs[i], CompLength: comps[i], Length: lens[i]}
	}
	return out, nil
}

// LoadChunkIndex reads the union of all chunk manifests into a hash->entry map.
func (c *Catalog) LoadChunkIndex(ctx context.Context) (map[string]ChunkEntry, error) {
	keys, err := c.store.List(ctx, chunksPrefix)
	if err != nil {
		return nil, err
	}
	idx := make(map[string]ChunkEntry)
	for _, k := range keys {
		entries, err := c.readChunks(ctx, k)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			idx[e.Hash] = e
		}
	}
	return idx, nil
}

// LoadPackIndex reads the union of all pack manifests, deduplicated by pack id.
func (c *Catalog) LoadPackIndex(ctx context.Context) ([]PackEntry, error) {
	keys, err := c.store.List(ctx, packsPrefix)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]PackEntry)
	for _, k := range keys {
		tbl, err := c.readTable(ctx, k)
		if err != nil {
			return nil, err
		}
		ids := colStr(tbl, 0)
		sizes := colI64(tbl, 1)
		counts := colI64(tbl, 2)
		for i := range ids {
			seen[ids[i]] = PackEntry{PackID: ids[i], Size: sizes[i], ChunkCount: counts[i]}
		}
		tbl.Release()
	}
	out := make([]PackEntry, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	return out, nil
}

// DeleteSnapshot removes a snapshot's record and node manifest, making it
// unreachable. The chunks it exclusively referenced become dead and are
// reclaimed by the next compaction; chunks still referenced by other snapshots
// are unaffected.
func (c *Catalog) DeleteSnapshot(ctx context.Context, id string) error {
	if err := c.store.Delete(ctx, snapshotKey(id)); err != nil {
		return err
	}
	return c.store.Delete(ctx, nodesKey(id))
}

// ReplaceChunkAndPackIndex consolidates the chunk and pack indexes: it deletes
// every existing chunk/pack manifest and writes a single consolidated manifest
// for each. Used by compaction after relocating live chunks.
func (c *Catalog) ReplaceChunkAndPackIndex(ctx context.Context, chunks []ChunkEntry, packs []PackEntry) error {
	oldChunks, err := c.store.List(ctx, chunksPrefix)
	if err != nil {
		return err
	}
	oldPacks, err := c.store.List(ctx, packsPrefix)
	if err != nil {
		return err
	}
	if err := c.WriteChunks(ctx, chunksPrefix+"/index.parquet", chunks); err != nil {
		return err
	}
	if err := c.WritePacks(ctx, packsPrefix+"/index.parquet", packs); err != nil {
		return err
	}
	for _, k := range oldChunks {
		if k == chunksPrefix+"/index.parquet" {
			continue
		}
		if err := c.store.Delete(ctx, k); err != nil {
			return err
		}
	}
	for _, k := range oldPacks {
		if k == packsPrefix+"/index.parquet" {
			continue
		}
		if err := c.store.Delete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}
