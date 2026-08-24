# RestoreWeave

**Self-hosted content-aware storage and discovery for NAS and heterogeneous data.**

**Store less. Find more. Restore with proof.**

> **Status: `v0.1.0-prealpha.1` core preview.** The core workflow runs from
> source and is covered by end-to-end tests. This is not a production release:
> there is no supported installer or container image, the model bundle is
> provisioned separately, and the repository profiles are not release-qualified.

RestoreWeave protects a directory as exact, content-addressed data; identifies
whole-file duplicates by SHA-256; retains names, paths, facts, Notes, and
recovery references; searches with lexical and local semantic indexes; and
restores the original bytes to a clean destination.

The personal profile deliberately stays small:

```text
configured source directories
        |
        v
one SQLite catalog + rebuildable search projections
        |
        v
content repository + authenticated portable recovery records
```

SQLite is the operational catalog. Repository objects and authenticated
portable records preserve recovery meaning when the catalog or search indexes
are unavailable. Search indexes, including embeddings, never establish exact
identity and never authorize deletion.

## Core preview

The current checkout implements and tests:

- directory inspection followed by an explicit protect/apply step;
- whole-file SHA-256 identity and exact duplicate accounting;
- exact storage with `STORE_EXACT` as the default;
- editable, versioned Notes used by lexical and semantic search;
- real local `BAAI/bge-small-zh-v1.5` inference through ONNX Runtime and zvec;
- disposable search indexes that can be rebuilt from durable catalog data;
- saved views and frozen export manifests for the admitted local scope;
- exact verification and restore with catalog-free clean-reader evidence;
- tamper rejection, independent trust anchors, relocation, migration,
  publication fencing, and unknown-outcome reconciliation;
- a bounded loopback `/api/v1` adapter and React browser client over the same
  typed operations as the daemon and CLI.

The ordinary preview workflow is:

```text
configure storage
-> inspect a directory
-> review exact protection and duplicate estimates
-> protect
-> add or edit Notes
-> search by name, facts, text, or semantic meaning
-> verify or restore exact bytes
```

## Non-goals

This preview does not provide FUSE, SMB, NFS, WebDAV, an S3 gateway, OpenList,
OpenSubsonic, OPDS, a media player, automatic external reacquisition, source
deletion, or destructive garbage collection. Reachability analysis remains
`NON_DESTRUCTIVE_ONLY`.

Exact whole-file deduplication is the default. Filenames, metadata, similarity,
embeddings, perceptual hashes, and model output cannot merge content or permit
deletion. Experimental neural/RWKV compression is not implemented or silently
enabled.

## Build and run

Prerequisites:

- Go 1.26 or the version declared by `go.mod`;
- Node.js supported by Vite 7 for the optional browser client;
- a platform-specific verified BGE/ONNX/zvec bundle for semantic search.

Build the daemon and maintenance CLI:

```bash
go build -o bin/restoreweaved ./server/cmd/restoreweaved
go build -o bin/rw ./client/cmd/rw
```

Create a persisted TOML profile:

```bash
bin/rw config init --path ./restoreweave.toml
bin/rw config validate --path ./restoreweave.toml
```

Storage, catalog, vector, model, and recovery paths come from this profile.
Relative paths resolve against the config file directory, never the process's
current directory. Enable the local browser adapter in the profile:

```toml
[api]
enabled = true
listen = "127.0.0.1:4534"
```

Start the daemon:

```bash
bin/restoreweaved \
  --config ./restoreweave.toml \
  --socket /tmp/restoreweaved.sock
```

Then start the browser client:

```bash
cd web
npm ci
npm run dev
```

Open `http://127.0.0.1:5173/`. The Vite development server proxies `/api` to
the loopback daemon at `127.0.0.1:4534` by default.

The CLI remains available for initialization, diagnostics, scripts, and
emergency recovery:

```bash
bin/rw --socket /tmp/restoreweaved.sock status
bin/rw --socket /tmp/restoreweaved.sock ingest /path/to/directory
bin/rw --socket /tmp/restoreweaved.sock snapshot list
bin/rw --socket /tmp/restoreweaved.sock search "recovery notes" --workspace <id>
```

Ingest and restore are two-step operations: the first command creates a
read-only plan; `plan apply` executes only the reviewed plan ID and digest.
The browser presents the same contract without requiring users to handle
internal IDs during the normal flow.

## Semantic model

The default personal profile is `BAAI/bge-small-zh-v1.5`. Model assets are not
committed to this repository. Place a verified platform bundle at:

```text
<paths.models>/bge-small-zh-v1.5/<goos>-<goarch>/
```

For example, the Darwin ARM64 default is:

```text
~/.local/share/restoreweave/models/bge-small-zh-v1.5/darwin-arm64/
```

You may override it with `--semantic-bundle`. Daemon startup performs a real
inference probe. A missing, incompatible, or unhealthy bundle is reported as
semantic search unavailable; exact protection and restore continue to work.

## Data and recovery

The default development repository uses exact whole-file content identity.
`local-zstd-v1` is an opt-in whole-file compression candidate with transparent
exact readback, but neither in-tree repository profile is production-qualified.
There is no destructive GC.

Back up the configured catalog, repository, recovery records, and independent
public trust anchor together. Deleting only a search index loses derived search
speed, not exact content or durable Notes. Losing both operational metadata and
portable recovery records can make repository objects impossible to organize
or restore correctly.

## Verification

Run the same broad checks used for this preview:

```bash
go test ./... -count=1
go test -race ./server/internal/store/sqlite ./server/internal/exact \
  ./server/internal/processor ./server/internal/search \
  ./server/controlplane ./server/cmd/restoreweaved -count=1
go vet ./...

cd web
npm ci
npm run build
```

These tests establish the current development profile, not production release
qualification or support across every operating system.

## Documentation

- [Documentation map](docs/README.md)
- [MVP and operator contract](docs/requirements/mvp-and-operator-contract.md)
- [Content store, views, and exports](docs/requirements/content-store-views-and-exports.md)
- [Core execution plan](docs/technical/core-mvp-execution-plan.md)
- [API and WebUI boundary](docs/requirements/api-and-webui.md)
- [Release qualification](docs/requirements/release-qualification-and-traceability.md)
- [Changelog](CHANGELOG.md)

The requirements documents define the intended product. Capability claims in
this README are limited to executable evidence in the current checkout.
