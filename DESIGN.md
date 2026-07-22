# zeaback design

## Motivation

zeaback is an incremental backup system for the Zea ecosystem. The design goal is
a backup whose *data* is opaque and compact, but whose *metadata* is rich, portable,
and queryable — because the useful operations on a backup (incremental diffing,
compaction, point-in-time and event-relative restore, and eventually AI-assisted
semantic restore) are all fundamentally **queries over metadata**, not operations
on file bytes.

## Two planes

### Data plane (opaque, content-addressed)

- Files are split by **content-defined chunking** (a self-contained FastCDC
  implementation in `pkg/cas`, ~64 KiB average). Content-defined boundaries mean
  an edit only reshapes nearby chunks, so incremental backups dedupe well.
- Each chunk is hashed with SHA-256 (its content id), compressed (DEFLATE), and
  appended to an **immutable pack file**. Packs have random ids; chunks within
  them are content-addressed. Only chunks not already present are written.
- Pack files begin with an 8-byte header (`magic + version + flags`). The reserved
  flags leave room for **per-blob encryption** without changing the layout of
  existing packs.
- Because every object is written exactly once and never modified, the store
  abstraction (`pkg/store.Store`) needs only put/get/get-range/list/delete. This
  is why zeaback composes with volumez despite volumez's whole-object rewrites:
  zeaback never rewrites.

### No coupling to any storage gateway

zeaback deliberately has **no code-level dependency on volumez** (or any other
gateway). Cloud and external storage are reached by pointing the `local` store at
a mounted path — volumez, rclone, s3fs, goofys, NFS, SMB, anything that presents
as a directory. Targeting "a directory" is strictly more general than linking one
gateway's API, and FUSE already abstracts the backend, so an in-process adapter
would mostly re-implement that boundary while coupling zeaback to an external Go
API.

The original design carried an opt-in adapter over volumez's `pkg/backend`; it
was removed because it added a messy optional dependency (volumez's module path
is being reconciled with its hosting, so it is not cleanly `go get`-able) for
marginal benefit over the mount path. If mountless, direct-to-cloud writes are
wanted later, the right answer is a **native store** (e.g. S3 via aws-sdk-go)
implemented directly behind `store.Store` — first-class and testable — rather
than borrowing another project's backend registry.

### Metadata plane (columnar, relational)

Every backup writes immutable **Parquet manifests** (`pkg/catalog`, via
arrow-go/v18):

| Manifest | Key | Contents |
|---|---|---|
| snapshot record | `meta/snapshots/<id>.parquet` | one row: id, timestamp, event, tags, parent, host, sources, annotations |
| node tree | `nodes/<id>.parquet` | one row per file/dir/symlink: path, type, mode, uid/gid, size, mtime, content hash, chunk list, content_type, embedding |
| chunk index | `meta/chunks/<id>.parquet` | chunks introduced by this backup: hash → pack, offset, lengths |
| pack index | `meta/packs/<id>.parquet` | packs introduced by this backup |

Manifests are **append-only per backup**; the full chunk/pack index is the union
of the per-backup files (DuckDB globs them with `read_parquet(...)`). Nested values
(tag maps, chunk lists, source-path lists) are stored as JSON strings — simple to
write and still queryable via DuckDB's json functions.

Parquet is the **portable source of truth**: an archive is self-describing and
readable by DuckDB, pandas, or the sibling Zea tools with no zeaback code. The
DuckDB query engine (`pkg/catalog`, behind the `duckdb_arrow` build tag) is a lens
over these files, never an authoritative store.

## Why DuckDB / Arrow when the payload isn't tabular

The payload is opaque and stays out of DuckDB. The *catalog* is a star schema, and
the questions asked of a backup are analytical queries:

- **Time-travel / event-relative restore** — pick the snapshot at/before a time,
  or the latest carrying an event label; a temporal selection (`pkg/catalog`
  `Selector` / `Resolve`).
- **Incremental diff** — carry unchanged files forward (same size + mtime); the
  hot "does this chunk already exist?" check uses an in-memory set/`LiveIndex`
  loaded once from the chunk index, because scan engines are weak at millions of
  point lookups.
- **Compaction** — live-chunk reachability + per-pack liveness is a group-by; low
  liveness packs are rewritten, dead packs dropped (`pkg/compact`).
- **Analytics / AI retrieval** — dedup ratios, growth over time, and (later)
  semantic/intent queries are one-liners over the catalog.

## Backup algorithm (`pkg/snapshot`)

1. Load the latest snapshot as parent; index its nodes by path.
2. Load the `LiveIndex` (all known chunk hashes).
3. Walk each source (paths stored relative to each source's basename). For each
   regular file: if size + mtime match the parent, carry the node forward
   unchanged; otherwise chunk it, writing only chunks absent from the LiveIndex.
4. Flush packs. Write manifests in order: **nodes, chunks, packs, then the snapshot
   record last** — a snapshot only becomes discoverable once fully written, so a
   crashed backup leaves only unreferenced garbage (reclaimable by compaction).

## Restore algorithm (`pkg/restore`)

Resolve the target snapshot, load its nodes, filter by an optional path prefix
(single file or subtree), then materialize: create dirs shallowest-first,
reconstruct files by reading and hash-verifying each chunk from its pack, recreate
symlinks, and finally apply stored directory permissions.

## Compaction (`pkg/compact`)

Live set = every chunk referenced by any surviving snapshot. Packs with zero live
chunks are deleted; packs below a liveness threshold have their live chunks
relocated into fresh packs. The chunk and pack indexes are then **consolidated**
into single manifests, and retired pack files are deleted only after the indexes
no longer reference them. `forget` (delete a snapshot's record + node manifest) is
what turns chunks dead in the first place.

## Repository layout

```
<repo>/
  repo.json                        # id, format version, chunker params
  data/<pack-id>.pack              # immutable compressed chunk packs
  nodes/<snap-id>.parquet          # file tree per snapshot
  meta/snapshots/<snap-id>.parquet # snapshot record
  meta/chunks/<snap-id>.parquet    # chunks introduced by a backup
  meta/packs/<snap-id>.parquet     # packs introduced by a backup
```

## Roadmap

- **Encryption** — per-blob, using the reserved pack header flags.
- **AI / MCP (Phase 2)** — an MCP server exposing `list_snapshots`, `browse`
  (time-travel), `semantic_restore` (intent → temporal/tag/content-type query,
  later augmented with embeddings), and `auto_backup` (take a safety znapshot of
  affected paths before any destructive tool runs). The catalog already carries
  `event_label`, `tags`, `content_type`, and a reserved `embedding` column so these
  are additive queries, not a schema migration.
- **Iceberg synergy** — optionally map znapshots onto Iceberg table snapshots via
  `zeaos`/`zeaberg` for table-format-native time-travel.

## Known Phase-1 limitations

- Snapshot node manifests and consolidated indexes are written as a single Parquet
  file each (loaded fully in memory). Fine for large-but-not-enormous trees; row-
  group streaming is a later optimization.
- `query` reads the repository directory directly with DuckDB, so it works against
  any local path (including a mounted gateway). A future non-filesystem store would
  need a local sync before querying.
- uid/gid capture is unix-only; restoring ownership requires privilege.
