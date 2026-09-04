# RestoreWeave

[中文](README.md) · [English](README.en.md)

<p align="center">
  <strong>Self-hosted content-aware storage, discovery, and recovery for NAS and heterogeneous data</strong><br>
  Store fewer duplicate bytes. Find content easily. Restore it with proof.
</p>

<p align="center">
  <a href="#current-status"><img src="https://img.shields.io/badge/status-unreleased_core_preview-8b5cf6?style=flat-square" alt="Status: unreleased core preview"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-4ed8b0?style=flat-square" alt="License: MIT"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/go-1.26-79c2ff?style=flat-square" alt="Go 1.26"></a>
</p>

> Current `main` is an unreleased core preview after `v0.1.0-prealpha.1`. The development profile has exercised “configure → preview → exact save and whole-file dedup → keyword/optional BGE search → multiple tags and Notes → exact restore.” This is not a production release; a supported installer, production repository qualification, LinkGroup, and complete multi-platform guarantees remain open.

<p align="center">
  <img src="docs/assets/screenshots/unreleased/library-en.png" alt="RestoreWeave content-first library with search, tags, Notes, protection, and index status" width="1100">
</p>
<p align="center"><sub>A real running WebUI populated with sanitized demo data built from this repository's documents.</sub></p>

[5-minute start](#5-minute-start) · [Core capabilities](#core-capabilities) · [Wiki](docs/wiki/README.md) · [Full documentation](docs/README.md) · [Changelog](CHANGELOG.md)

## Core capabilities

- **Exact save and deduplication:** identity is the complete byte stream's SHA-256 plus logical length; byte-identical files share one object while names and original paths remain independently restorable.
- **Review before saving:** a read-only plan shows file count, logical size, expected new bytes, duplicate reuse, and blockers before any bytes are written.
- **Easy discovery:** keywords, structured filters, tags, and Notes work together; after an explicitly provisioned and verified local BGE bundle, semantic search is available too.
- **Context stays attached:** original names, paths, file facts, multiple tags, and one Notes surface are retained; type/format facets do not pretend to be user tags.
- **Recoverable output:** export and restore recheck paths, lengths, and SHA-256. If an index or optional model is unavailable, exact content remains readable and restorable.

RestoreWeave is a content, discovery, export, and recovery plane—not a cloud-drive filesystem, mount service, media server, or OpenList fork. Original directories are provenance and recovery projections; daily organization is content-first with tags, Notes, search, and saved views.

## One complete workflow

```text
configure storage
→ inspect a source
→ preview and approve a protection plan
→ save exact content and reuse duplicates
→ search, tag, and maintain Notes
→ export or restore and verify the original bytes
```

## 5-minute start

Use Go 1.26 (or the version declared by `go.mod`) and a Node.js version supported by Vite 7:

```bash
git clone https://github.com/ailiheizi/restoreweave.git
cd restoreweave
go build -tags=purego -o bin/restoreweaved ./server/cmd/restoreweaved
go build -o bin/rw ./client/cmd/rw
bin/rw config init --path ./restoreweave.toml
```

Enable the local WebUI API:

```toml
[api]
enabled = true
listen = "127.0.0.1:4534"
```

Start the daemon and frontend in separate terminals:

```bash
# Terminal 1
bin/restoreweaved --config ./restoreweave.toml --socket /tmp/restoreweaved.sock

# Terminal 2
cd web
npm ci
npm run dev
```

Open `http://127.0.0.1:5173/`. The API is currently a loopback convenience adapter; do not expose it directly to the public network. See the [Wiki quick start](docs/wiki/quick-start.md) for configuration, source inspection, and restore details.

## Real interface

<details>
<summary>Show the storage-plan preview used while adding a source</summary>

<p align="center">
  <img src="docs/assets/screenshots/unreleased/protection-plan-en.png" alt="A real pre-save storage plan showing logical size, new bytes, and duplicate reuse" width="1100">
</p>

The preview writes no file bytes; duplicate reuse and expected new storage are explicit before confirmation.
</details>

## Current status

### Implemented and tested (current development profile)

- TOML configuration, explicit data paths, source inspection, protection plans, and confirmed writes.
- SHA-256 + length identity, exact whole-file deduplication, Notes, multiple user tags, and keyword/structured search.
- Real BGE-small-zh + ONNX + zvec components run with an explicitly provisioned bundle; without one, the service honestly degrades to ordinary search.
- SavedView, ExportManifest, materialize/verify, signed recovery, clean reader, tamper rejection, migration, and cross-process safety evidence for the local development scope.
- React WebUI and the loopback `/api/v1` convenience adapter, with responsive layout and Chinese localization.

### Candidate, planned, or explicitly out of scope

- `local-zstd-v1` is a runnable whole-file compression candidate, not a production repository; Restic/Kopia/Plakar have not been selected as a default engine.
- Supported offline installers, cross-platform distribution, upgrade/backup workflows, and production qualification still require independent evidence.
- LinkGroup is the later minimal file-link grouping primitive; it has no user-visible version history and its full workflow is not implemented yet.
- Image CLIP/SigLIP, music features, RWKV/Transformer compression, and destructive GC are not part of the current default core; reachability remains `NON_DESTRUCTIVE_ONLY`.

“Implemented and tested” means evidence for the stated checkout scope, not production support. See the [content, deduplication, search, and export contract](docs/requirements/content-store-views-and-exports.md) and the [core execution plan](docs/technical/core-mvp-execution-plan.md) for the complete status matrix.

## Go deeper

- [Wiki overview](docs/wiki/README.md): plain-language guidance for daily use and boundaries.
- [Detailed capability reference](docs/wiki/capability-reference.md): configuration, storage, search, Notes, recovery, and status details.
- [Quick start](docs/wiki/quick-start.md) · [Storage and capacity](docs/wiki/storage-and-capacity.md) · [Index status and search](docs/wiki/index-status-and-search.md) · [Recovery and boundaries](docs/wiki/recovery-and-boundaries.md)
- [Documentation map](docs/README.md): requirements, ADRs, technical plans, and qualification records.
- [Content Store, Views, and Export Requirements](docs/requirements/content-store-views-and-exports.md) · [Core MVP Execution Plan](docs/technical/core-mvp-execution-plan.md)
- [Changelog](CHANGELOG.md)

## License

The source is available under the [MIT License](LICENSE). Model weights, tokenizers, ONNX Runtime, zvec, third-party dependencies, and future install bundles retain their own licensing and redistribution terms.
