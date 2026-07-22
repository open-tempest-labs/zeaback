# How zeaback works

A backup system quietly juggles two completely different data problems, and most
of zeaback's design falls out of taking that split seriously.

The first problem is **bytes**: potentially terabytes of opaque file content that
you want to store compactly, never corrupt, and never store twice if you can help
it. The second problem is **knowledge**: which files existed, where, when, how
they relate across time — the stuff you actually *ask questions about* ("what
changed since Tuesday?", "restore `/etc` as of the deploy last month", "how much
would I reclaim if I dropped these old backups?").

Those two problems want opposite tools. Bytes want an opaque, deduplicated,
write-once blob store. Knowledge wants a rich, queryable, columnar catalog.
zeaback keeps them in **two planes** and lets each be good at its own job:

```
        ┌──────────────────────── znapshot ────────────────────────┐
        │                                                          │
   DATA PLANE (opaque)                       METADATA PLANE (queryable)
   content-addressed, deduplicated,          Parquet manifests, queried
   compressed blobs in pack files            by DuckDB — the "knowledge"
        │                                                          │
        └── your files, as bytes ──┘         └── what/where/when, as rows ┘
```

The rest of this document walks through both planes and then shows how a backup,
a restore, and a compaction actually move through them.

---

## Part 1 — The data plane: content-addressed storage

### Files are split, not stored whole

The first thing zeaback does with a file is **chop it into chunks** —
variable-sized pieces averaging ~64 KiB. It does *not* cut at fixed offsets.
Instead it uses **content-defined chunking** (a self-contained FastCDC
implementation in [`pkg/cas/chunker.go`](../pkg/cas/chunker.go)): a rolling hash
slides over the bytes, and a chunk boundary is declared wherever that hash hits a
certain bit pattern.

Why go to this trouble instead of fixed 64 KiB blocks? Because of what happens
when a file changes. Imagine inserting 7 bytes near the front of a 1 MiB file:

```
Fixed-size blocks (bad):
  before:  [block 0][block 1][block 2][block 3] ...
  after:   [block 0'][block 1'][block 2'][block 3'] ...   ← every block shifted,
                                                             nothing dedupes

Content-defined chunks (good):
  before:  [ c0 ][  c1  ][ c2 ][   c3   ][ c4 ] ...
  after:   [ c0'][ c1'][ c2 ][   c3   ][ c4 ] ...          ← only chunks near the
                                                             edit change; the rest
                                                             re-sync to the SAME
                                                             boundaries and hashes
```

Because the boundaries are chosen by *content*, not position, an edit only
disturbs the chunks around it. Everything downstream re-synchronizes and keeps
its old identity. That single property is what makes incremental backups cheap.
(zeaback's test suite asserts this directly: insert bytes mid-file and >50% of
chunks are still shared.)

The boundary function is seeded deterministically (a fixed splitmix64 gear table),
so the same bytes always chunk the same way on every machine and every run.

### A chunk's name is its content

Each chunk is hashed with SHA-256. That hash **is** the chunk's address — its
`ChunkID` ([`pkg/cas/blob.go`](../pkg/cas/blob.go)). This is what "content-addressed"
means: you don't ask "what's stored at location X?", you ask "give me the bytes
whose hash is H."

Two things fall out for free:

- **Deduplication.** Identical bytes hash to the same address, so they're stored
  once — across files, across directories, across snapshots, even across machines
  backing up to the same repository.
- **Integrity.** Whatever you fetch, you can re-hash and confirm it matches the
  address you asked for. Silent bit-rot can't hide.

A file, then, is reduced to an **ordered list of chunk hashes**. The bytes are
shared; only the recipe is per-file.

### Chunks are packed, not stored loose

Here's a question that decides whether the whole thing is usable: if a chunk is
named by its hash, is each chunk its own object/file?

**No — and this matters enormously.** Storing millions of tiny hash-named objects
is a performance and cost disaster (more on the S3 angle below). Instead, chunks
are compressed (DEFLATE) and concatenated into larger **immutable pack files**,
about 16 MiB each ([`pkg/cas/packer.go`](../pkg/cas/packer.go)).

A pack is dead simple on disk:

```
data/<random-pack-id>.pack
┌──────────────────────────────────────────────────────────────────┐
│ header: "ZBK1" + version(1) + flags(1) + reserved(2)  (8 bytes)  │
├──────────────────────────────────────────────────────────────────┤
│ deflate(chunk A)                                                 │
│ deflate(chunk B)                                                 │
│ deflate(chunk C)                                                 │
│ ...                                                              │
└──────────────────────────────────────────────────────────────────┘
```

The catalog remembers, for each chunk, *which pack it's in and where*:

```
chunk hash → { pack_id, offset, comp_length, length }
```

So reading one chunk is: look up its location, do a **ranged read** of exactly
`comp_length` bytes at `offset` in that pack, decompress, and verify the hash.
No need to read the whole pack.

Two deliberate choices here:

- **Pack ids are random, not content hashes.** The content-addressing that earns
  its keep (dedup, integrity) happens at the *chunk* level. A pack just needs a
  unique name, so it gets a random one.
- **The header reserves `flags`.** Per-blob encryption can be added later by
  setting a flag, without changing the layout of any pack already written.

### How this lands in object storage (e.g. S3)

zeaback doesn't speak S3 directly. It writes to a small
[`store.Store`](../pkg/store/store.go) interface — put / get / get-range / list /
delete — and the simplest implementation is a local directory
([`pkg/store/local`](../pkg/store/local)). To target the cloud, you point that
directory at a **mounted gateway** (volumez, rclone, s3fs, NFS, …). The gateway
maps each file to one object.

So a repository in S3 looks like this — **one object per pack and per manifest,
never one per chunk**:

```
repo.json                             → 1 object
data/3f9a1c…​.pack                      → 1 object  (~16 MiB; hundreds of chunks)
data/7b02e4…​.pack                      → 1 object
nodes/20260722T…​.parquet              → 1 object
meta/snapshots/20260722T…​.parquet     → 1 object
meta/chunks/20260722T…​.parquet        → 1 object
meta/packs/20260722T…​.parquet         → 1 object
```

Reading a single chunk becomes an HTTP **Range GET** into a pack object
(`Range: bytes=offset-…`), which S3 supports natively — that's what `GetRange`
maps to end to end.

Why packing is non-negotiable at scale: a 1 GiB backup at 64 KiB chunks is
~16,000 chunks. As individual objects that's ~16,000 PUTs (S3 bills per request),
a huge keyspace to `LIST`, and per-object overhead on every one. Packed, it's
~64 objects. This is the same reason restic and borg pack their blobs.

Because packs are written **once, sequentially, and never modified**, this stays
efficient even on gateways or backends that treat objects as immutable — zeaback
never rewrites an object in place.

---

## Part 2 — The metadata plane: a queryable catalog

The data plane is deliberately dumb: opaque bytes keyed by hash, with no idea what
a "file" or a "snapshot" is. All of that meaning lives in the **catalog**
([`pkg/catalog`](../pkg/catalog)).

### The catalog is a star schema

Every backup writes four kinds of record, which together form a classic star
schema:

| Table | What it holds |
|---|---|
| **snapshots** | one row per backup: id, timestamp, event label, tags, parent, host, source paths |
| **nodes** | one row per file/dir/symlink: path, type, mode, uid/gid, size, mtime, symlink target, content hash, **ordered chunk list**, and reserved `content_type` / `embedding` columns |
| **chunks** | the chunk index: hash → pack, offset, lengths |
| **packs** | one row per pack: id, size, chunk count |

Your files aren't tabular — but *fleets of file metadata across time* absolutely
are, and the questions you ask of a backup are analytical queries over exactly
this schema.

### Manifests are Parquet, and they're the source of truth

Each of those tables is written as a **Parquet** file (via arrow-go). Parquet is
compressed, columnar, self-describing, and readable by basically everything —
DuckDB, pandas, Spark, and the sibling Zea tools — with zero zeaback code. That
means a zeaback archive's metadata is a first-class dataset you can explore with
whatever you already have. The archive is portable *and* introspectable.

Manifests are written **append-only, one set per backup**, keyed by the snapshot
id:

```
nodes/<snap-id>.parquet             the file tree for this snapshot
meta/snapshots/<snap-id>.parquet    the snapshot record (1 row)
meta/chunks/<snap-id>.parquet       chunks this backup introduced
meta/packs/<snap-id>.parquet        packs this backup introduced
```

The *full* chunk index is just the union of all `meta/chunks/*.parquet`. DuckDB
globs them in one shot with `read_parquet('meta/chunks/*.parquet')`; the Go code
reads and merges them. Nested values (tag maps, the per-file chunk list, source
paths) are stored as JSON strings — trivial to write and still queryable with
DuckDB's json functions.

### DuckDB is the lens, not the store

The Parquet manifests are authoritative. DuckDB is simply the query engine you
point at them, exposing four views — `snapshots`, `nodes`, `chunks`, `packs` —
over the manifest globs. Time-travel, diffs, storage analytics, and (later)
semantic/AI retrieval are all just SQL over these:

```sql
-- how much unique, compressed storage does the repo hold?
SELECT count(*) AS chunks, sum(comp_length) AS stored_bytes FROM chunks;

-- biggest files in the latest snapshot
SELECT path, size FROM nodes WHERE type='file' ORDER BY size DESC LIMIT 10;
```

The payload stays out of DuckDB entirely. DuckDB never touches a byte of your file
content — only the knowledge about it.

---

## Part 3 — Putting it together

### A backup, step by step

([`pkg/snapshot`](../pkg/snapshot))

1. **Find the parent.** The most recent snapshot becomes the parent; its node
   tree is loaded and indexed by path.
2. **Load the live set.** All known chunk hashes are loaded into an in-memory set
   (the `LiveIndex`). This is the trick that keeps the hot path fast: "does this
   chunk already exist?" is an O(1) map lookup, not a query against a columnar
   store (which is built for scans, not millions of point lookups).
3. **Walk the sources.** For each file:
   - If its size **and** mtime match the parent's record, carry the node forward
     untouched — the file is not even read.
   - Otherwise, chunk it. For each chunk, if its hash is already live, skip it
     (dedup); if not, add it to the current pack and record a new chunk entry.
4. **Flush packs**, then write the manifests in a specific order: **nodes,
   chunks, packs, and the snapshot record *last*.** A snapshot only becomes
   discoverable once its record exists, so a crash mid-backup leaves only
   unreferenced garbage (which compaction later reclaims) — never a half-visible
   snapshot.

What this looks like in practice — a real two-backup sequence, second run after
changing one small config file and adding one file:

```
Backup 1:  files: 3   new: 5 chunks (293 KiB written)   reused: 0
Backup 2:  files: 4   new: 2 chunks (39 B written)      reused: 4
```

The unchanged 293 KiB file was recognized by size+mtime and carried forward for
free; only the genuinely new bytes — 39 of them — were written.

### A restore, step by step

([`pkg/restore`](../pkg/restore))

1. **Resolve the target snapshot.** You pick it by id, by `--at TIME` (the latest
   snapshot at or before an instant), or by `--event LABEL` (the latest snapshot
   carrying a named event, optionally `--before` a time). These are small
   selections over the snapshots table — time-travel is literally a query.
2. **Filter.** Optionally restrict to a single file or a subtree by path prefix.
3. **Materialize.** Create directories shallowest-first, then for each file walk
   its chunk list, fetch each blob (ranged read → decompress → **verify hash**),
   and stream the bytes out; recreate symlinks; finally apply stored directory
   permissions.

The same machinery does file-level, directory-level, and whole-tree restore —
it's just a wider or narrower path filter.

### Compaction and forget

([`pkg/compact`](../pkg/compact))

Nothing is deleted during normal operation, so dead data only appears when you
**forget** a snapshot (`zeaback forget` removes its record and node manifest,
making its exclusive chunks unreferenced). Compaction then does garbage
collection:

1. Compute the **live set** — every chunk referenced by any surviving snapshot.
   (A reachability + set operation, straight out of the catalog.)
2. For each pack, compute its live fraction. Packs with **zero** live chunks are
   deleted outright; packs below a liveness **threshold** have their few live
   chunks rewritten into fresh packs.
3. **Consolidate** the chunk and pack indexes into single manifests pointing at
   the new layout, and only *then* delete the retired pack objects — so the index
   never references bytes that are already gone.

### Verification

([`pkg/verify`](../pkg/verify)) walks every chunk referenced by every snapshot,
confirms it's present in the index, and (in `--deep` mode) fetches and hash-checks
every blob. Because chunks are content-addressed, this is a complete,
self-checking integrity proof.

---

## Try it yourself

Everything above is observable in about two minutes. Build the binary, then follow
along — the numbers below are from a real run.

```sh
make build          # produces ./zeaback (DuckDB included, for `query`)
```

**Set up a small project** with a config, some notes, and a ~1.8 MB log file:

```sh
mkdir -p project/logs
printf 'release = 1.0\n' > project/app.conf
printf 'meeting notes\n- ship it\n' > project/notes.txt
seq 1 28000 | awk '{printf "2026-07-21T10:%02d:%02d INFO req id=%d status=200 path=/api/items\n",$1%60,($1*7)%60,$1}' > project/logs/app.log
```

**1 · Initialize a repository and take the first backup:**

```console
$ ./zeaback init --path ./repo --name demo
Initialized repository "demo" (id 912ef4e7…)
Set as default repository.

$ ./zeaback backup --event baseline --tag team=platform ./project
Created snapshot 20260722T013759-6edccf7b8d76
  files:    3  dirs: 2  symlinks: 0
  data:     1.7 MiB scanned
  new:      10 chunks (89.2 KiB written)   ← the log compressed 1.8 MB → ~89 KB
  reused:   0 chunks (deduplicated)
```

**2 · Change one config line, add a file, back up again** — watch the incremental:

```console
$ printf 'release = 2.0\n' > project/app.conf
$ printf 'draft\n'         > project/notes-2.txt
$ ./zeaback backup --event post-change ./project
Created snapshot 20260722T013759-be22bd34e397
  parent:   20260722T013759-6edccf7b8d76
  files:    4  dirs: 2  symlinks: 0
  new:      2 chunks (32 B written)         ← only the genuinely new bytes
  reused:   9 chunks (deduplicated)         ← the 1.8 MB log wasn't re-read or re-stored
```

That's the whole thesis in one line: the second backup wrote **32 bytes**.

**3 · Explore.** List snapshots, browse a tree, and ask the catalog questions in SQL:

```console
$ ./zeaback snapshots
ID                            TIME                 EVENT        SOURCES    TAGS
20260722T013759-6edccf7b8d76  2026-07-21 21:37:59  baseline     ./project  team=platform
20260722T013759-be22bd34e397  2026-07-21 21:37:59  post-change  ./project  -

$ ./zeaback ls latest
snapshot 20260722T013759-be22bd34e397  (2026-07-21 21:37:59)
d  -rwxr-xr-x  0 B      project
-  -rw-r--r--  14 B     project/app.conf
-  -rw-r--r--  1.7 MiB  project/logs/app.log
...

$ ./zeaback query "SELECT count(*) AS chunks, sum(length) AS logical, sum(comp_length) AS stored FROM chunks"
chunks  logical  stored
12      1808952  91324                        ← ~1.8 MB of content, ~91 KB on disk
```

**4 · Time-travel.** Restore a single file as it was at a named event, and as it is now:

```console
$ ./zeaback restore --event baseline --path project/app.conf ./at-baseline
$ ./zeaback restore                  --path project/app.conf ./at-latest
$ cat ./at-baseline/project/app.conf   # release = 1.0
$ cat ./at-latest/project/app.conf     # release = 2.0
```

You can restore by `--event LABEL`, by `--at "2026-07-21 10:00:00"` (point in time),
or by snapshot id — at file, directory, or whole-tree granularity.

**5 · The full lifecycle: rotate, forget, compact, verify.** Replace the log
entirely, back up, drop the now-stale snapshots, and reclaim their space:

```console
$ seq 28001 56000 | awk '{...}' > project/logs/app.log     # brand-new log content
$ ./zeaback backup --event rotated ./project
  new: 7 chunks (87.7 KiB written)   reused: 3 chunks

# forget the two pre-rotation snapshots, then garbage-collect
$ ./zeaback snapshots | awk 'NR>1{print $1}' | head -2 | xargs -n1 ./zeaback forget
$ ./zeaback compact
Compaction complete.
  packs:    3 -> 3  (deleted 0, rewritten 1)
  chunks:   10 live, 9 dead, 1 relocated
  reclaimed: 89.1 KiB                 ← the old log's chunks, now unreferenced

$ ./zeaback verify --deep
OK (deep (blobs hash-checked)): 1 snapshots, 4 files, 10 chunk references verified
```

**What just happened, mapped back to the design:** content-defined chunking + the
live-set check made the second backup 32 bytes (dedup); DEFLATE turned 1.8 MB into
~91 KB (packing); `query` ran DuckDB straight over the Parquet catalog (the
metadata plane); `restore --event` was a temporal query resolving to the right
snapshot (time-travel); `forget` + `compact` computed the live set, found the old
log's chunks unreferenced, rewrote the one low-liveness pack, and reclaimed the
space (garbage collection); and `verify --deep` re-hashed every stored blob
against its content address (integrity).

To point any of this at the cloud, `init` the repo on a mounted gateway path
(`--path /mnt/volumez/backups`) — nothing else changes.

## What falls out of the design

- **Deduplication** across files, snapshots, and hosts — a consequence of naming
  bytes by content.
- **Integrity** — every read is verifiable against its own address.
- **Cheap incrementals** — content-defined chunking + a live-set membership check.
- **Portable, introspectable archives** — Parquet manifests any tool can read;
  DuckDB for rich queries.
- **Crash-safety** — snapshot records written last; orphaned data is reclaimable,
  never corrupting.
- **Storage-agnostic** — write-once objects compose with any directory-shaped
  backend, including immutable-object gateways, with no coupling.
- **Room to grow** — reserved pack flags for encryption; reserved catalog columns
  (`content_type`, `embedding`) for AI-assisted, intent-based restore.

## Honest limitations (today)

- Node manifests and consolidated indexes are each written as a single Parquet
  file held in memory — fine for large-but-not-enormous trees; row-group
  streaming is a future optimization.
- `query` reads the repository directory directly with DuckDB, so it needs a local
  path (a mounted gateway counts); a future non-filesystem store would need a
  local sync first.
- uid/gid capture is unix-only, and restoring ownership needs privilege.

---

*For the terser design reference, see [DESIGN.md](../DESIGN.md); for usage, see
the [README](../README.md).*
