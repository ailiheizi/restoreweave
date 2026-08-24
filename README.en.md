# RestoreWeave

[中文](README.md) | [English](README.en.md)

**Self-hosted content protection, discovery, and recovery for NAS and heterogeneous data.**

**Store fewer duplicate bytes. Find content more easily. Restore with proof.**

> Current version: `v0.1.0-prealpha.1` core preview. The main core workflow is implemented and tested for the current development profile, but this is not a production release. There is no supported installer, automatic model installation, qualified production repository, or complete multi-platform release guarantee yet.

## What the core is

RestoreWeave is not a cloud-drive filesystem, OpenList fork, media server, or a thin copy of a backup tool. It owns one complete data-management loop:

```text
configure storage locations
-> inspect a directory before protection
-> approve a protection plan
-> retain exact content and recoverable metadata
-> add Notes or Descriptions
-> find content with ordinary filters and semantic search
-> save a view and freeze an export manifest
-> verify, export, or restore the original bytes
```

Three invariants define the core:

1. **Identical bytes need only one stored object.** Exact identity is the full-content SHA-256 plus logical length. A filename, path, similarity score, embedding, or model output can never declare two files identical.
2. **Search data is rebuildable.** Lexical and embedding indexes are projections. Removing them temporarily degrades search; it does not remove files, Notes, Descriptions, or recovery authority.
3. **Recovery does not depend on AI.** Even if the model, vector index, and live SQLite catalog are unavailable, a clean reader can authenticate and restore exact bytes when the repository, authenticated recovery records, and independently retained trust anchor remain intact.

## What works now

The list below is organized around user-visible work. Except where marked as a candidate, these capabilities have implementation and automated test evidence. “Implemented and tested” does not mean “production-qualified.”

### Configuration and status

- Persisted TOML configuration, with read compatibility for older YAML profiles.
- Explicit catalog, repository, vector, model, and publication-signing-material locations.
- Relative paths resolve against the config file, never an accidental daemon working directory.
- The config CLI provides separate `rw config init --path <file>`, `rw config validate --path <file>`, and `rw config show --path <file>` commands; the daemon uses `restoreweaved --config <file>`, with environment overrides also available.
- A configuration digest bound to plans, snapshots, Descriptions, and index/semantic generations.
- Status and diagnostics for the daemon, catalog, repository, indexes, plans, jobs, and providers.

### Inspect, plan, and protect

- Read-only traversal of local or already-mounted directories.
- Capture of names, original paths, types, sizes, timestamps, symlinks, hard links, detection evidence, platform-observable xattr/ACL state, and sparse indication.
- Changed, unreadable, out-of-boundary, or unstable entries become visible blockers instead of fabricated successes.
- A non-mutating protection plan reports file count, logical size, expected new storage, and per-entry outcomes; already-stored or duplicate content appears as reduced `new_bytes`.
- Placement begins only after the plan ID and digest are approved. Reapplying the same plan replays the same logical result.
- `STORE_EXACT` is the default.
- Advanced CLI flows can explicitly retain `LINK_ONLY`, `METADATA_ONLY`, and external locator records. Their unprotected or unavailable state remains visible and is never reported as exact protection.
- Derived processing failures do not block exact preservation of readable files; they produce explicit fallback or degraded outcomes.

### SHA-256 identity and whole-file deduplication

- Streaming SHA-256 over the complete logical file, with logical length included in identity.
- Byte-identical files under different names or paths reuse one content object.
- Each original name, path, and metadata record remains distinct, so deduplication does not destroy directory recovery.
- Protection preview separates logical bytes from bytes that actually need new repository placement.
- Exact whole-file deduplication is the current default. Chunk deduplication is not presented as an existing feature or a core-completion requirement.

### Notes, tags, Descriptions, and extraction

- Multiple editable, revisioned Notes for one file.
- Notes feed both ordinary and semantic search directly, without copying them into a hidden second record.
- Durable tags and annotation import/export remain available for scripts and compatibility flows; the daily WebUI centers semantic information on Notes.
- Versioned Descriptions retain source, language, producer, predecessor revision, and semantic segments.
- User, imported, extracted, and model-attributed Descriptions can be retained. No Description generator is built in, and ingest never generates one automatically.
- Basic built-in extraction covers UTF-8 text, ID3/FLAC/OGG audio tags, and EPUB OPF metadata.
- Processor results carry provenance. Failure, timeout, or unavailability never grants content-identity or recovery authority.

### Lexical, structured, and semantic search

- Search filename, original path, suffix, type, tags, Notes, Descriptions, and extracted text.
- Filter by entry type, size, mtime, SHA-256, duplicate group, `protection_mode`, language, and suffix.
- Lexical, structured, and semantic results resolve to the same stable subject.
- Results preserve the matched Description segment or Note content and its source, not just an opaque score.
- With an explicitly provisioned and verified platform bundle, real local semantic vector generation and query using `BAAI/bge-small-zh-v1.5`, ONNX Runtime, and in-process zvec.
- A real inference probe runs at daemon startup, and a compatible zvec generation can reopen after restart.
- An unhealthy model, lease, or generation reports semantic search unavailable while lexical/structured search, exact protection, and recovery continue.
- Every semantic generation is strictly bound to its embedding profile and configuration. Incompatible generations fail closed without rewriting old Notes, Descriptions, or subject identity.

### Browse and bounded exact reads

- List the original-path projection, resolve a path to a subject, and inspect file, directory, and symlink facts.
- List exact and derived representations available for a subject.
- Read exact content or byte ranges through handles with bounds, expiry, and size limits.
- Neither search nor private repository layout replaces the original-path recovery projection.

### Snapshots, SavedViews, and exports

- List snapshots and compare added, removed, moved, content, metadata, and type changes between snapshots.
- Save a dynamic `SavedView` and reevaluate the same query later.
- Freeze one view evaluation into an immutable `ExportManifest`.
- Materialize the frozen manifest to an explicit destination and verify every path, length, and SHA-256.
- Reapplying one manifest is idempotent; non-empty destinations and symlink attacks fail closed.
- The view/export path passes local end-to-end tests, but is not release-qualified yet.

### Verification, restore, and disaster reading

- Authenticated-metadata, sampled-content, full-bytes, restore-drill, and clean-recovery verification modes.
- Restore also begins with a read-only plan. Approval writes only to a new empty directory, then validates the final path set, lengths, and SHA-256 values.
- Export authenticated recovery references, recovery tokens, and an independently retained public trust anchor.
- Discover, verify, and restore snapshots in a clean-install reader without the live SQLite catalog, search indexes, or signing private key.
- Reject modified, truncated, missing, incorrectly signed, wrong-anchor, or reader-incompatible recovery records and content.
- Read-only verification after repository relocation, plus tested raw/zstd copy-forward migration, target-tamper rejection, and preservation of the old repository as rollback authority.
- Cross-process publication fencing prevents two daemons from interleaving publication. Authenticated records reconcile unknown outcomes.
- The daemon automatically performs bounded retries of the same signed processor plan with lease, fencing, idempotency, restart-resume, unknown-outcome reconciliation, and retry-limit evidence. Arbitrary user-triggered or rerouted reprocessing remains unavailable.

### Interfaces

- **WebUI:** service and storage status, directory selection, protection preview/confirmation, search, path/SHA/protection details, multiple Note creation and editing, and whole-snapshot restore preview/confirmation.
- **CLI:** initialization, diagnostics, scripts, the complete plan/snapshot/view/export surface, and emergency recovery. It is not intended to remain the required daily interface.
- **MCP:** local read-only stdio access for inspection, search, namespace, representation, annotation, and metadata operations.
- **API:** currently only loopback `GET /api/v1/healthz` and typed `POST /api/v1/command`, sharing the same dispatcher with optional bearer-token validation. It is not a complete public-network REST platform.

## One complete workflow

### WebUI

```text
open Add content
-> enter a directory visible to the server
-> Preview protection
-> review file count, logical size, new storage, and blockers
-> Confirm protection
-> search and add multiple Notes
-> run a descriptive BGE semantic query
-> inspect SHA-256 and protection state
-> Preview restore
-> restore to a new empty directory and verify
```

### CLI

```bash
# Initialize and start
go build -o bin/restoreweaved ./server/cmd/restoreweaved
go build -o bin/rw ./client/cmd/rw
bin/rw config init --path ./restoreweave.toml
bin/restoreweaved --config ./restoreweave.toml --socket /tmp/restoreweaved.sock

# Inspect a directory; the command prints the matching plan-apply arguments
bin/rw --socket /tmp/restoreweaved.sock ingest /path/to/directory
bin/rw --socket /tmp/restoreweaved.sock plan apply <plan-id> \
  --workspace <workspace-id> --digest <plan-digest>

# Search and inspect snapshots
bin/rw --socket /tmp/restoreweaved.sock search "documents needed after a disaster" \
  --workspace <workspace-id>
bin/rw --socket /tmp/restoreweaved.sock snapshot list

# Restore also creates a plan before execution
bin/rw --socket /tmp/restoreweaved.sock restore <snapshot-ref> /path/to/empty-destination
bin/rw --socket /tmp/restoreweaved.sock plan apply <restore-plan-id> \
  --workspace <workspace-id> --digest <restore-plan-digest>
```

## Where data lives

RestoreWeave keeps the number of physical entities small, while giving each one a distinct responsibility:

| Data | Default shape | Rebuildable? | Purpose |
| --- | --- | --- | --- |
| Configuration | `config.toml` | Not inferable from content | Selects every location and profile |
| Catalog | One SQLite file | Partly recoverable from authenticated records | Subjects, paths, facts, Notes, Descriptions, plans, and state |
| Content repository | Configured repository directory | Not rebuildable from indexes | Original exact bytes and authenticated records |
| Lexical index | Separate SQLite FTS generation | Yes | Text and structured search |
| Semantic index | zvec generation | Yes | BGE embedding search |
| Portable recovery records | Authenticated records in the repository plus an explicitly exported recovery reference | Not replaceable by an empty index | Catalog-free verification and restore |
| Publication signing material | `paths.recovery_records` | No | Publication private key and local anchor copy; not a clean-reader artifact |
| Trust anchor | Exported and retained independently | Not inferable from an untrusted repository | Signature verification for recovery records |

Catalog, repository, and index are separate logical layers; they do not require three database services. The personal profile uses local SQLite, a directory repository, and in-process zvec, with no Qdrant, Milvus, or Docker Compose dependency.

## What happens when data is removed or lost

| Missing data | Result |
| --- | --- |
| Lexical/zvec indexes removed | Search degrades; files, Notes, Descriptions, and recovery remain, and indexes can rebuild |
| BGE/ONNX/zvec bundle unavailable | Semantic search is unavailable; lexical search, protection, verification, and restore continue |
| SQLite catalog unavailable | Daily search and editing are unavailable; a clean reader can still verify and restore when the repository, exported recovery reference, and independent trust anchor remain |
| Repository payload missing | Recovery records cannot recreate source bytes from nothing; the affected content is not restorable |
| Catalog and portable recovery records both missing | Context-free blobs are insufficient to safely reconstruct the original namespace and recovery meaning |
| Publication signing material missing | Existing exported recovery references remain clean-reader verifiable; the ordinary daemon cannot continue the original signed publication lineage |
| Independent trust anchor missing | Signed recovery records cannot be authenticated as designed; do not keep the only copy in the same failure domain |
| Experimental encrypted-repository key missing | Encrypted content is unreadable; this profile is neither the generated default nor a production capability |

## Current status

| Capability | Status |
| --- | --- |
| Config, scan, plans, SHA-256 identity, whole-file dedup, exact protection | Implemented and tested for the development profile |
| Notes/Descriptions and lexical/structured search | Implemented and tested for the stated field scope |
| BGE-small-zh + ONNX + zvec | Implemented and tested with an explicitly provisioned real bundle; not packaged yet |
| Signed recovery, clean reader, tamper rejection, fencing, reconciliation | Implemented and tested for the admitted development profile |
| SavedView, ExportManifest, materialize/verify | Implemented and tested for local scope; not release-qualified |
| React WebUI and loopback API | Usable core convenience surface; not a remote administration platform |
| `directory-cas-dev-v1` | Current generated development default; not a release default |
| `local-zstd-v1` | Runnable candidate with tested whole-file compression, dedup, verification, repair, and migration; not qualified |
| `local-zstd-encrypted-v1` | Experimental candidate with tested AES-256-GCM and external KeyProvider behavior; not a config default |
| GC | `NON_DESTRUCTIVE_ONLY` reachability planning; no deletion executor |
| `RW-MVP-1` | Not complete or release-qualified |

## Quick WebUI start

Use Go 1.26 (or the version declared by `go.mod`) and a Node.js version supported by Vite 7:

```bash
git clone https://github.com/ailiheizi/restoreweave.git
cd restoreweave

go build -o bin/restoreweaved ./server/cmd/restoreweaved
go build -o bin/rw ./client/cmd/rw
bin/rw config init --path ./restoreweave.toml
```

Enable the local API in the generated config:

```toml
[api]
enabled = true
listen = "127.0.0.1:4534"
```

Start the daemon in terminal 1:

```bash
bin/restoreweaved --config ./restoreweave.toml --socket /tmp/restoreweaved.sock
```

Start the frontend in terminal 2:

```bash
cd web
npm ci
npm run dev
```

Open `http://127.0.0.1:5173/`. The current API is designed only as a loopback convenience adapter. Do not expose it directly to the public network. Remote deployment still requires TLS, authentication, authorization, audit, and separate qualification.

## BGE model

The personal profile selects `BAAI/bge-small-zh-v1.5`, but the model, ONNX Runtime, and native zvec bundle are not downloaded automatically from this repository. Provision a digest-verified platform bundle yourself:

```text
<paths.models>/bge-small-zh-v1.5/<goos>-<goarch>/
```

Example default location for Darwin ARM64:

```text
~/.local/share/restoreweave/models/bge-small-zh-v1.5/darwin-arm64/
```

You can also pass `--semantic-bundle`. Without the bundle, the daemon honestly reports semantic search unavailable; it never substitutes fixture vectors for a real model.

The model, ONNX Runtime, zvec, and Go binding each retain their own license, NOTICE/SBOM, and redistribution obligations. A future install bundle must preserve and qualify them separately; they are not covered automatically by this repository's still-unselected project license.

## What comes next

Near-term work is limited to turning the existing core into a releasable product:

1. **Formal background work:** complete the general async processor worker, retry intent, idempotency, restart reconciliation, fencing, and retry ceilings.
2. **Offline packaging:** package the daemon, WebUI, ONNX Runtime, BGE model/tokenizer, and zvec without first-query downloads.
3. **Production repository qualification:** use representative corpora to select one lossless repository profile and fully test encryption, corruption, repair, relocation, migration, rollback, clean reading, and real net savings.
4. **Complete daily experience:** expose writable configuration, diagnostics, SavedViews, ExportManifests, backup/upgrade/recovery guidance in the WebUI, and remove internal IDs from ordinary workflows.
5. **Release qualification:** measure search coverage, semantic latency, storage growth, recovery time, upgrade/rollback, and clean-install behavior on Linux and NAS-like corpora.

Longer-term options enter the core queue only when concrete demand exists: reviewed source migration and capacity release, more extractors/OCR/ASR/CLIP, alternate embedding or repository profiles, multiple repositories and tiering, and enterprise remote management, RBAC, and HA. RWKV/Transformer compression can only be a future explicit research profile that is reversible, migratable, and has a safe fallback.

## Explicit non-goals

- Built-in FUSE, SMB, NFS, WebDAV, or S3 gateways.
- An OpenList fork or OpenList as a core dependency.
- Built-in players, readers, or media servers.
- Automatic external reacquisition, automatic source deletion, or destructive GC.
- Embeddings, similarity, filenames, or model output as exact identity or deletion authority.
- Qdrant, Milvus, or Docker Compose as personal-profile dependencies.
- Neural codecs before simple lossless storage and complete recovery are qualified.

## Verification

```bash
go test ./... -count=1
go test -race ./server/internal/store/sqlite ./server/internal/exact \
  ./server/internal/processor ./server/internal/search \
  ./server/controlplane ./server/cmd/restoreweaved -count=1
go vet ./...
go mod verify

cd web
npm ci
npm run build
```

The repository currently contains more than 400 Go test entry points, including real daemon/CLI, semantic-bundle, index-rebuild, recovery, tamper, migration, and cross-process scenarios. The tests prove their declared scope; they do not imply support for every platform or production environment.

## Documentation and license

- [中文 README](README.md)
- [Documentation map](docs/README.md)
- [MVP and operator contract](docs/requirements/mvp-and-operator-contract.md)
- [Content, dedup, discovery, view/export, and GC contract](docs/requirements/content-store-views-and-exports.md)
- [Core execution order](docs/technical/core-mvp-execution-plan.md)
- [API and WebUI boundary](docs/requirements/api-and-webui.md)
- [Release qualification](docs/requirements/release-qualification-and-traceability.md)
- [Changelog](CHANGELOG.md)

This repository is publicly readable, but no open-source license has been selected or committed yet. Public access alone does not grant permission to copy, redistribute, or sublicense the code; licensing remains a separate owner decision.
