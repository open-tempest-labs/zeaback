# zeaback

**Incremental, deduplicating backup for the Zea ecosystem — with a queryable, time-travelable metadata catalog.**

zeaback backs up files, directories, and whole trees into **zeasnaps**:
point-in-time, filesystem-independent snapshots. A zeasnap is not tied to any
particular disk — it is a combination of opaque, content-addressed **data blobs**
and a rich **columnar metadata catalog**. That split is the whole idea:

- **Data plane** — files are split by content-defined chunking, deduplicated,
  compressed, and packed into immutable pack files. Only new chunks are ever
  written, so incremental backups are cheap. Packs are portable and can be copied
  or moved to cloud/external storage.
- **Metadata plane** — every backup writes **Parquet manifests** (the snapshot
  record, the file tree, the chunk index, the pack index). Parquet is the portable
  source of truth: it travels with the archive and is directly queryable by
  DuckDB, pandas, or the sibling Zea tools. zeaback ships a built-in DuckDB query
  path over it.

Your files aren't tabular — but the *questions you ask of a backup* are:
"what changed since Tuesday?", "restore `/etc` as of the event before the deploy",
"which packs are mostly dead?". Those are queries, and that is what the catalog is for.

## Quick start

```sh
make build                      # builds ./zeaback with DuckDB (tags duckdb_arrow)

./zeaback init --path ~/backups/main        # create a local repository
./zeaback backup --event nightly ~/projects # create a zeasnap
./zeaback snapshots                         # list zeasnaps
./zeaback ls latest ~/projects              # browse a snapshot tree

# restore a whole tree, a subtree, or a single file
./zeaback restore ./out
./zeaback restore --path projects/src ./out
./zeaback restore --path projects/app.conf ./out

# time-travel: a point in time, or relative to a named event
./zeaback restore --at "2026-07-01 09:00:00" ./out
./zeaback restore --event nightly --before "2026-07-01" ./out

./zeaback forget <snapshot>     # make a snapshot unreachable
./zeaback compact               # reclaim space from forgotten snapshots
./zeaback verify --deep         # check integrity (hash-checks every blob)
./zeaback query "SELECT event_label, count(*) FROM snapshots GROUP BY 1"
```

## Commands

| Command | Purpose |
|---|---|
| `init` | Create a repository at a local path (or a mounted gateway) and register it |
| `backup PATH...` | Create an incremental zeasnap; `--event`, `--tag k=v` |
| `snapshots` | List zeasnaps |
| `ls [SNAP] [PATH]` | Browse a snapshot's tree; `--at`, `--event` |
| `restore DEST` | Restore file/subtree/tree; `--snapshot`, `--at`, `--event`, `--before`, `--path` |
| `forget SNAPSHOT` | Make a snapshot unreachable |
| `compact` | Rewrite low-liveness packs, drop dead ones |
| `verify` | Check integrity; `--deep` hash-checks blobs |
| `query SQL` | Run DuckDB SQL over the catalog (views: `snapshots`, `nodes`, `chunks`, `packs`) |
| `config` | Show config; `config set-default NAME` |

Select a repository with `--repo NAME` (from `~/.zeaback/config.json`) or
`--path DIR` for an unregistered local repo.

## Architecture

```
zeaback
├── cmd/zeaback         # thin CLI entrypoint (version via -ldflags)
├── internal/cli        # cobra commands, one per file
├── internal/config     # ~/.zeaback/config.json (named repos)
└── pkg
    ├── store           # immutable object-store interface
    │   └── local       # filesystem dir (also a mounted cloud/FUSE gateway)
    ├── cas             # content-defined chunking, hashing, compressed packs
    ├── catalog         # Arrow/Parquet manifests + DuckDB query layer
    ├── repo            # repository: wires store + catalog
    ├── snapshot        # backup engine (incremental, dedup)
    ├── restore         # file/dir/tree restore; point-in-time & event resolution
    ├── compact         # auto-compaction / GC
    └── verify          # integrity checking
```

See [DESIGN.md](DESIGN.md) for the full design, including the AI/MCP roadmap.

## Cloud and external storage (volumez, rclone, s3fs, NFS, ...)

zeaback writes only immutable, write-once objects with ordinary filesystem APIs,
so targeting cloud or external storage needs **no special build and no coupling**
to any particular gateway: point the repository at a mounted path.

```sh
# mount your gateway of choice, then:
zeaback init --path /mnt/volumez/backups
zeaback backup ~/projects
```

This works with [volumez](https://github.com/open-tempest-labs/volumez) (a FUSE
mount for cloud backends), rclone mount, s3fs, goofys, NFS, SMB — anything that
presents as a directory. Because zeaback creates each object exactly once and
never rewrites it, it composes cleanly even with gateways that treat objects as
immutable.

A native, mountless cloud store (e.g. S3 via aws-sdk-go) may be added in the
future behind the same `store.Store` interface; it is not required today.

## Building

`zeaback` follows the ecosystem's DuckDB + Arrow stack and requires the
`duckdb_arrow` build tag (and CGO) for the `query` command:

```sh
make build      # go build -tags duckdb_arrow ...
make test       # go test -tags duckdb_arrow ./...
make install
```

A build without the tag (`go build ./...`) works for everything except `query`.

## Roadmap

Encryption (per-blob, the pack format already reserves header flags) and an **MCP
server** for AI-assisted, intent-based and semantic restores — plus auto-backup
before destructive actions — are planned. The catalog already carries the
`event_label`, `tags`, `content_type`, and reserved `embedding` fields those
features build on. See [DESIGN.md](DESIGN.md).

## Ecosystem

| Project | Role |
|---|---|
| [zeaos](https://github.com/open-tempest-labs/zeaos) | Data OS / warehouse over DuckDB + Iceberg |
| [zeashell](https://github.com/open-tempest-labs/zeashell) | DataFrame shell for modern file formats |
| [volumez](https://github.com/open-tempest-labs/volumez) | FUSE mount + pluggable storage backends for cloud APIs |
| **zeaback** | Incremental backup with a queryable, time-travelable catalog |

## License

Apache License 2.0 — see [LICENSE](LICENSE).
