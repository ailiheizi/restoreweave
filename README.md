# RestoreWeave

**Self-hosted content-aware storage and discovery for NAS and heterogeneous data.**

**Store less. Find more. Restore with proof.**

> **Project status — research-stage / pre-alpha.** This repository contains the product requirements, reference architecture, and tested Go foundations. It does not yet contain a supported binary, installer, container image, or end-to-end managed-archive workflow. The commands and reference distribution described below are the target `RW-MVP-1` contract, not a runnable release.

> **Name status — working product name.** RestoreWeave remains the current product and CLI name while operator positioning, trademark, domain, and package checks are completed. See [Naming Research](docs/references/naming-research.md).

RestoreWeave is a self-hosted content-aware storage and discovery layer for NAS and heterogeneous data. It is designed to keep a familiar folder-shaped view while storing content in verified, deduplicated, compressed, and replaceable representations. Recovery is the trust contract behind storage reduction and processing changes; it is not the only daily workflow.

The longer-term product shape is **one authoritative content plane with many bounded experiences**. A universal Inbox and catalog make arbitrary files safe, searchable, and understandable; focused music, reader, video, photo, document, and application/game clients reuse the same identity, access, annotation, search, and recovery contracts. They do not create separate storage silos or bypass the core by reading repository packs, private database tables, or host paths directly. These experiences are roadmap profiles, not implemented applications in the current repository. See [Ecosystem and Vertical Applications](docs/requirements/ecosystem-and-vertical-apps.md).

In plain terms, RestoreWeave is a data layer for a managed archive: it records what a file is, stores an exact recoverable form, adds optional derived information, and projects the result back as a searchable folder-shaped view. It is not a NAS operating system, not a general-purpose AI harness, and not a writable network share in `RW-MVP-1`.

The reference deployment is a Linux-based NAS or server, either native or containerized. Platform-specific capture capabilities attach through `CaptureDriver`; no operating system or filesystem defines the durable data model. RestoreWeave also does not embed a general AI agent platform: AI may participate through bounded processors or external CLI/MCP clients.

## What exists today

This checkout is a research and reference-architecture repository, not a deployable NAS product. The current Go packages provide tested foundations for filesystem scanning, type identification, SQLite namespace/representation records, bounded read-service interfaces, and an M1 exact lane: descriptor-rooted capture, a fake content-addressed `RepositoryDriver`, capture-qualified catalog adoption, portable snapshot publication, and catalog-free restore. The M2/M3 control-plane harness (`restoreweaved`, `rw`, stdio MCP) can ingest a local tree, list/verify snapshots, restore to an empty directory, browse adopted namespace IDs, tag and note subjects, query a disposable SQLite FTS5 generation, read exact CAS bytes through `content.open`/`read`/`close`, and list admitted ID3/FLAC/OGG tags through `audio.list` and EPUB/TXT/Markdown works through `books.list`. Ingest also runs bounded in-process EXTRACT processors (UTF-8 text, audio tags, and EPUB OPF) that cannot block the exact lane. M4 adds host-owned `SnapshotTree`/`FileAccess` and a private Linux FUSE adapter; this Darwin host reports `unsupported_platform` instead of mounting. There is still no mature repository engine or music/reader/video/game application; Linux sandbox isolation and remaining plan/job operations stay unimplemented.

## What it does in practice

The intended operator loop is:

```text
connect an existing tree read-only
-> observe paths, types, duplicates, metadata, and recovery risks
-> create an immutable plan with exact storage estimates
-> apply a reviewed managed-archive plan
-> browse and search through the original directory shape
-> verify, export recovery evidence, and restore to a clean destination
```

The source tree is not deleted or made writable by this workflow. A later migration profile may release a source copy only after exact recovery, placement, rollback, and human-approval gates pass. The operator receives a searchable, file-shaped managed view and evidence explaining what is exact, what is fallback, what is derived, and what remains blocked.

## One content plane, many experiences

The planned ecosystem builds upward from the exact-storage product instead of placing every parser, player, reader, model, downloader, and launcher in one process:

```text
RestoreWeave Core
  -> Universal Catalog and Inbox
  -> domain processors and typed artifacts
  -> Music / Reader / Video / Photos / Documents / Games
  -> CLI / MCP / FUSE / focused clients and later protocol adapters
```

The core owns exact content identity, snapshots, the original-path namespace, representations, placement and publication truth, bounded content access, durable annotations, authorization, verification, and restore. Domain packs and clients own format-specific metadata, segments, previews, playback or reading behavior, playlists, progress, and presentation. Processing failure may reduce understanding or preview quality, but an unknown or unsupported readable file remains exact, visible, searchable by baseline facts, accessible, and restorable.

The first ecosystem feature should be a universal **Inbox**, not a suite of half-complete players:

```text
drop files or attach a directory
-> protect and identify exact content
-> search immediately by path, type, hash, duplicate, tag, note, or extracted text
-> enrich asynchronously with optional domain processors
-> review organization suggestions
-> open in the best available experience
-> verify or restore through the same content plane
```

One search experience does not require one physical mega-index. Lexical, reader-text, acoustic, visual, vector, or graph projections may evolve independently, but every result resolves to the same authorized subject or segment and remains independent of recovery authority. The proposed delivery order is the exact core, then Inbox/catalog, then one contained music experience, a reader, video, other media and document views, read-only application/game inventory, and only later qualified retrieval or execution profiles.

## Product thesis

Conventional backup tools are good at moving bytes, and media catalogs are good at one content class. NAS users still have to combine backup, deduplication, format detection, metadata extraction, search, media understanding, lifecycle policy, and restore tooling themselves.

RestoreWeave provides one stable layer across those concerns. Its product power comes from completing the whole operator loop, not from one codec, model, repository, or plugin API:

- **Storage minimization:** exact hashing, duplicate detection, repository compression and deduplication by default, with replaceable class-specific transforms later.
- **Recoverable namespace:** original paths, directories, links, metadata, and representation mappings remain visible even when physical storage is content-addressed, packed, remote, or transformed.
- **Intelligent discovery:** lexical search across paths, metadata, type, checksums, duplicates, extracted text, and durable user tags and notes forms the baseline. Embedding, CLIP, multimodal ranking, and external enrichment attach later through versioned interfaces.
- **Replaceable processing:** learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation use one bounded `Processor` contract. Repository, index, and query implementations have their own stateful seams and can be upgraded without changing durable content or namespace meaning.
- **Verifiable recovery:** immutable plans, provenance, content digests, repository receipts, portable recovery records, and actual readback distinguish stored data from recoverable data.

The product is useful with all optional AI components disabled. AI can improve classification, extraction, transformation, or search through ordinary processor interfaces, and an external harness can call RestoreWeave through CLI or MCP. Neither receives recovery authority.

## Product modes

RestoreWeave separates three product modes so storage-savings claims remain honest:

- **Observe mode** inventories and indexes an existing tree without moving authoritative bytes. It improves discovery but claims no net storage reduction.
- **Managed archive mode** ingests selected data into verified deduplicated and compressed storage, then serves a read-only original-path namespace for browse, bounded reads, search, and restore. This is the first product mode and the basis of the MVP.
- **Primary writable NAS mode** would accept writes through RestoreWeave-managed gateways. It requires separate concurrency, durability, conflict, and application-consistency design and is not an MVP claim.

Keeping both an original source and its managed archive consumes both copies. Whole-system capacity is released only after an independently verified migration and an explicitly authorized source-retirement decision. Automatic source deletion remains disabled by default.

The read-only original-path namespace is file-shaped access to a published archive snapshot, not a writable share and not the repository's private chunk or pack layout. The target MVP includes a bundled read-only Linux FUSE adapter so a NAS or server can mount that view for ordinary file access. Future SMB, NFS, WebDAV, S3, or media gateways may project the same namespace, but none changes its identity or recovery meaning.

## Stable core and replaceable pipeline

RestoreWeave keeps a small authoritative core and a practical default userland.

| Authoritative core | Replaceable implementations |
| --- | --- |
| Source, content, namespace, snapshot, and representation identity | `CaptureDriver` implementations for local, mounted, snapshot, and remote sources |
| Immutable plans, policy decisions, and exact-fallback rules | `Processor` capabilities for learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation |
| Operation journal, idempotency, fencing, and reconciliation | `RepositoryDriver` implementations and repository-private compression, chunking, packing, and placement strategies |
| Provenance, dependency closure, and verification acceptance | `IndexProvider` and `QueryProvider` implementations for lexical, structured, vector, graph, and multimodal discovery |
| Portable recovery records and publication truth | Later `RetrieverDriver` implementations for policy-qualified external reacquisition |
| `SnapshotTree` and `FileAccess` semantics | CLI clients, MCP clients, gateways, and future UI adapters |

These seams make selected algorithms replaceable; they do not make the first release an install-your-own-pipeline framework. The planned reference distribution will pin and qualify bundled capture, identification, processing, repository, lexical indexing, and query implementations. Operators should be able to use the complete product before installing any third-party component.

The default data path deliberately forks after inventory. Exact ingest never waits for classification, extraction, embeddings, or another optional processor:

```text
source capture
-> filesystem inventory
   |-> mandatory exact hash -> RepositoryDriver -> readback and publication
   |-> suffix -> magic -> optional learned classification and parsing
       -> content-class routing -> Processor stages -> derived artifacts
       -> IndexProvider -> QueryProvider
-> one authenticated original-path namespace
-> browse, mount, search, read, verify, and restore
```

Suffix and magic-byte evidence are retained separately. A disagreement is a visible classification conflict, not a silent override. Unknown, unsupported, encrypted, or processor-failed files remain stored exactly and recoverably when readable. A later exact transform may replace the selected physical representation only after independent decode-and-hash validation; derived search artifacts never replace source bytes.

## Target `RW-MVP-1` reference distribution

The first useful distribution targets self-hosted NAS and homelab operators without requiring a particular NAS brand, Linux distribution, or filesystem. The following is a target contract; it is not shipped in the current repository:

- One local controller and operational catalog.
- Local and mounted filesystem roots with an explicit capture-consistency claim.
- Deterministic extension and magic-byte identification.
- Independent content hashing, duplicate accounting, exact fallback, and a repository engine selected after the Kopia-led qualification spike for exact compression and deduplication.
- Default metadata extraction for common document and media classes, with bounded failure behavior.
- Durable, versioned tag and note CRUD with portable export, separate from rebuildable index state.
- Baseline lexical discovery across path, metadata, type, checksum, duplicate, tag, note, and extracted text.
- A read-only authenticated snapshot namespace with bounded file access.
- A bundled read-only Linux FUSE projection for NAS-usable file access; writes, rename, unlink, and metadata mutation are rejected.
- CLI JSON/JSONL and a local read-only MCP adapter over the same typed operations.
- Portable recovery records that do not depend on the live catalog, search index, processor registry, or AI service.

Embedding and CLIP providers are staged semantic-search extensions, not MVP dependencies. Their derived indexes must be disposable and rebuildable from durable subjects and processor provenance. The product direction remains semantic and multimodal even when the first release ships a lighter search baseline.

## Not in `RW-MVP-1`

The following are deliberately excluded from the first exact profile:

- Writable primary-NAS behavior through SMB, NFS, WebDAV, S3, or FUSE.
- Automatic source deletion, omission, destructive pruning, or autonomous capacity release.
- P2P, magnets, public sharing, external reacquisition, and swarm placement.
- Mandatory LLMs, embeddings, CLIP, OCR, ASR, neural, VAE, RWKV-style, perceptual, or lossy representations.
- Database-, VM-, application-, game-, or bare-metal-consistent capture.
- High availability, multitenancy, enterprise RBAC, legal hold, compliance certification, and managed SaaS.
- REST, WebUI, remote workers, mutation-capable MCP, A2A, and a public plugin marketplace.

These are named future profiles, not hidden assumptions in the MVP.

## Interface and deployment status

| Surface | Intended role | Current status |
| --- | --- | --- |
| Go scanner, namespace records, and read service | Filesystem observations, `SnapshotTree`, and bounded `FileAccess` foundations | Partially implemented and tested |
| CLI with human and JSON/JSONL output | Main operator and mutation surface | `rw` implements status, capability list, ingest, snapshot list/verify, restore, namespace list/stat/readlink, search, tag/note/annotation, content open/read/close, audio list, books list, unimplemented-operation forwarding, and `rw mcp`. `rw mount` is refused. |
| Local read-only MCP over stdio | Safe inspection and external automation | Harness MCP server implemented (`status.get`, `capability.list`, `namespace.list`, `search.query`, `annotation.list`, `audio.list`, `books.list`) over the same socket protocol |
| Processor protobuf + gRPC over Unix sockets with FD handles | Private local worker plumbing; large bytes stay out of control messages | Private Unix protobuf frames, grpc-go wrapping of the same messages, and SCM_RIGHTS in `processor/rpc` (not a public ABI). Linux sandbox execution remains. Default host is still in-process `RUN_STAGE`. |
| SQLite FTS5 generation and query providers | Bundled MVP lexical search | Implemented as one disposable FTS5 database per index generation; tags/notes live in the operational catalog, not the index file |
| File-shaped egress | Exact bytes as a directory or a read handle; other tools may mount that | `plan.restore` and `FileAccess` / `content.*`. RestoreWeave does not own a FUSE server. The leftover go-fuse adapter is unused product work. |
| Loopback OpenSubsonic / OPDS / Inbox facade | Compatibility adapters plus a thin read-only Inbox shell; not a player and not recovery authority | Optional on `restoreweaved --facade-listen` (loopback + token + pinned workspace). OpenSubsonic browse/search/stream/star/bookmark, OPDS search/pagination/acquire/progress, and `/inbox` search/preview/verify/restore all call the command ABI. Live Feishin/DSub/KOReader qualification has not been run. |
| Repository engine | Exact placement, readback, and restore | Fake directory CAS is the exact-lane driver. In-tree qualification gates always run against it; Restic/Kopia CLI probes are optional and do not select a release engine. |

The verifiable actions today are the Go test suite, including the M1 exit condition (unknown file → exact place → catalog loss → restore → SHA-256), the M2 isolation exit (processor panic or timeout does not block verify/restore), the M3 exit condition (delete the FTS file → search degrades, tags/notes/namespace/content/verify/restore remain, a new generation rebuilds), FileAccess/restore/FUSE-export byte equality, optional Restic crash-retry, keep-latest prune, and repo-relocation probes when `restic` is on PATH, the control-plane ingest/restore/`audio.list`/`books.list` round trip, exact range reads of an audio subject, and the sandbox argv policy (Darwin `Run` is `unsupported_platform`), Processor Unix protobuf/FD RUN_STAGE (payload absent from control frames, host digest independent), and grpc-go wrapping of the same RUN_STAGE messages (FDs still out of band). They do not imply that `RW-MVP-1` is release-qualified, and they do not select a repository engine.

## Client harness

The M2 control-plane slice ships a runnable harness: a daemon, a thin CLI, and a stdio MCP server, all speaking one JSON envelope protocol over a Unix socket.

```text
restoreweaved --socket /path/to/restoreweaved.sock --catalog /path/to/catalog.sqlite --repository /path/to/repository
rw --socket /path/to/restoreweaved.sock status
rw --socket /path/to/restoreweaved.sock ingest /path/to/tree
rw --socket /path/to/restoreweaved.sock snapshot list
rw --socket /path/to/restoreweaved.sock snapshot verify <snapshot-ref>
rw --socket /path/to/restoreweaved.sock restore <snapshot-ref> /path/to/empty-dest
rw --socket /path/to/restoreweaved.sock namespace list <root-id> --workspace <workspace-id>
rw --socket /path/to/restoreweaved.sock search <query> --workspace <workspace-id>
rw --socket /path/to/restoreweaved.sock tag add <entry-id> <tag> --workspace <workspace-id>
rw --socket /path/to/restoreweaved.sock content open <entry-id> --workspace <workspace-id>
rw --socket /path/to/restoreweaved.sock audio list --workspace <workspace-id>
rw --socket /path/to/restoreweaved.sock books list --workspace <workspace-id>
rw --socket /path/to/restoreweaved.sock restore <snapshot-ref> /path/to/empty-dest
rw --socket /path/to/restoreweaved.sock mcp
```

- **Exact lane.** `rw ingest` runs descriptor-rooted capture, places SHA-256 blobs in the fake CAS, adopts only `ROOTED_FD` complete scans into namespace records, and writes a portable snapshot JSON beside the blobs. `rw restore` reconstructs that snapshot into an empty directory without reading SQLite.
- **Discovery.** Tags and notes are durable catalog rows. Each search generation is a separate FTS5 file under `repository/indexes/`. Deleting that file degrades `search.query` only; a new generation rebuilds from namespace and annotation records. `content.open`/`read`/`close` return exact CAS bytes for a regular file, capped at 1MiB per read, including range reads of `audio.list` and `books.list` subjects. `audio.list` returns admitted ID3/FLAC/OGG tag artifacts plus albums derived from those tags; it is not a player. `books.list` returns admitted EPUB OPF metadata plus TXT/Markdown extracts; it is not a reader. Extracted titles are also in the lexical index.
- **Socket protocol.** The wire format is the `client/command` envelope vocabulary (`org.restoreweave.command.v1` requests, `org.restoreweave.result.v1` results) serialized as JSON inside 4-byte big-endian length-prefixed frames over a Unix socket. `restoreweaved` resolves its socket from `--socket`, then `RESTOREWEAVE_SOCKET`, then `XDG_RUNTIME_DIR`; the client mirrors that resolution without sharing code.
- **Honest outcomes.** When the daemon is unreachable, `rw` prints `cannot reach restoreweaved at <path>...` and exits non-zero. Operations this build does not implement return an explicit `unimplemented` reason (exit code 4) instead of fabricated success; `rw status` lists them, and `rw --json` prints the raw command Result. A missing search generation returns `DEGRADED` (exit code 5).
- **MCP.** `rw mcp` runs a local read-only MCP server over stdio with `status.get`, `capability.list`, `namespace.list`, `search.query`, `annotation.list`, `audio.list`, and `books.list` tools, each executing a real round trip through the same socket protocol. Annotation mutation is not exposed over MCP.
- **Namespace addressing.** `namespace` commands address catalog stable IDs (workspace/root/entry), not filesystem paths; `rw ingest` returns those IDs.
- **Player/reader sidecars.** Existing OpenSubsonic and OPDS clients can attach to an optional loopback facade (`restoreweaved --facade-listen 127.0.0.1:4534 --facade-token … --facade-workspace …`). The same listener serves `/inbox`. The facade only calls the command ABI; song and work IDs are `SubjectRef`. Play/read progress lands as a `PROGRESS` annotation, not a client-private library. If someone wants a folder, they restore (or read exact bytes) and mount with rclone, sshfs, SMB, or similar. Do not treat “point Navidrome at a mount” as the product proof.

## Terms at a glance

| Term | Plain-language meaning |
| --- | --- |
| `ContentIdentity` | The identity of exact bytes, independent of where they came from or which path names them. |
| `SubjectRef` | The stable reference used by annotations, search results, and access authorization. |
| `SnapshotTree` | The authenticated, immutable directory-shaped view of one published snapshot. |
| `FileAccess` | The bounded read/stream interface used by CLI, restore, and future gateways. |
| `IndexGenerationRef` | One immutable, rebuildable search projection with its own provider and schema provenance. |
| Exact representation | A representation that can be decoded and verified to the recorded bytes. |
| Derived artifact | Search, metadata, embedding, fingerprint, or other rebuildable output that never replaces source authority. |

## Safety model

Every selected entry has one explicit result:

- `EXACT`: a byte-exact recoverable representation was placed and verified.
- `EXACT_FALLBACK`: readable bytes were preserved exactly because classification or processing was unavailable, unsupported, conflicting, or uncertain.
- `EXPLICITLY_UNPROTECTED`: a human or previously published policy deliberately accepted non-recoverability.
- `BLOCKED`: the declared exact-ingest and recoverability contract could not be satisfied.

A file-type label, model score, duplicate candidate, downloadable source, perceptual match, or processor result cannot authorize omission. Lossy, generative, reacquired, or rebuildable representations require a separately qualified future profile and explicit policy. Exact source preservation remains the safe fallback.

Repository upload completion is also not verification. RestoreWeave preserves the existing crash-safe publication model: payload placement, a prepared portable recovery closure, a signed `PUBLICATION_COMMIT`, independent readback evidence, and an exact restore check are distinct facts.

## Planned operator surface (not implemented yet)

The planned command family is intentionally small:

```text
restoreweave doctor [<source>] [--to <target>]
restoreweave plan <source> --to <target> [--save-profile <name>]
restoreweave plan revise <plan-ref> --digest <digest> [--decisions <json-file>]
restoreweave apply <plan-ref> --digest <digest>
restoreweave profile run <name>
restoreweave status [<job-or-snapshot-ref>]
restoreweave search <query> [--snapshot <snapshot-ref>]
restoreweave browse [<snapshot-ref>[:<path>]]
restoreweave cat <snapshot-ref>:<path> [--to-file <new-file>]
restoreweave mount <snapshot-ref> <mountpoint> [--foreground]
restoreweave tag list <subject-ref>
restoreweave tag add <subject-ref> <tag> [--expected-revision <revision>]
restoreweave tag remove <subject-ref> <tag> [--expected-revision <revision>]
restoreweave note list <subject-ref>
restoreweave note set <subject-ref> --from-file <file> [--expected-revision <revision>]
restoreweave note remove <subject-ref> [--expected-revision <revision>]
restoreweave annotations export [--subject <subject-ref>] --to-file <new-file>
restoreweave verify <snapshot-ref> [--mode authenticated-metadata|sampled-content|full-bytes]
restoreweave recovery export <snapshot-ref> --to-file <new-file>
restoreweave restore <snapshot-ref>:<path> <destination>
restoreweave mcp serve --stdio
```

The baseline `search` capability uses a bundled lexical index over path, metadata, type, checksum, duplicates, durable tags and notes, and extracted text. Semantic and multimodal providers extend the same subject and result model later; they do not create a second namespace or recovery system.

## Documentation

Start with:

- [Product Requirements](docs/requirements/product-requirements.md)
- [MVP and Operator Contract](docs/requirements/mvp-and-operator-contract.md)
- [System Architecture](docs/requirements/system-architecture.md)
- [Driver and Processor Interfaces](docs/requirements/driver-and-processor-interfaces.md)
- [File Identification and Extraction](docs/requirements/file-identification-and-extraction.md)
- [Namespace and Content Access](docs/technical/namespace-and-content-access.md)
- [Ecosystem and Vertical Applications](docs/requirements/ecosystem-and-vertical-apps.md) (normative roadmap profile)
- [Ecosystem App Interface](docs/technical/ecosystem-app-interface.md)
- [NAS Vertical Slice Implementation Plan](docs/technical/nas-vertical-slice-implementation-plan.md)
- [Implementation Completion Plan](docs/technical/implementation-completion-plan.md)
- [Open-Source Adoption and Code Borrowing](docs/references/open-source-adoption-and-code-borrowing.md)
- [NAS Product Power Review](docs/references/nas-product-power-review.md)
- [Documentation Index](docs/README.md)

## Current implementation status and next build order

The repository contains foundations plus M1 exact ingest/restore, an M2 Processor host with text EXTRACT, M3 baseline discovery, and an M4 FileAccess/FUSE slice, not the complete NAS product. There is still no mature repository engine, Docker image, or installable release.

- A deterministic filesystem scanner with streaming SHA-256, source-change evidence, and descriptor-rooted traversal (`server/internal/scanner`). Only `ROOTED_FD` scans are eligible to become authoritative.
- A local-tree `CaptureDriver` that emits a durable `CaptureRootBindingRecord` without serializing runtime file descriptors (`server/internal/capture`).
- A fake directory CAS `RepositoryDriver` with idempotent placement, independent SHA-256 readback, and catalog-free restore (`server/internal/repository`, `server/internal/exact`). In-tree Driver gates live in `server/internal/repository/qualify`; optional Restic/Kopia CLI probes do not select a release engine.
- Capture-qualified adoption into SQLite namespace/file-version/representation records and a portable snapshot JSON. The M1 exit condition is tested: unknown binary → exact place → catalog loss → restore → SHA-256.
- Durable tags/notes in the operational catalog and disposable SQLite FTS5 generations (`server/internal/search`). The M3 exit condition is tested: delete the index file → search degrades; namespace, annotations, exact reads, verify, and restore remain; a new generation rebuilds.
- A host-owned Processor seam (`server/internal/processor`) with opaque handles, staging/seal/admit, a bundled UTF-8 text EXTRACT, a bundled ID3/FLAC/OGG tag EXTRACT (`extract.audio.tags.v1`), and a bundled EPUB OPF EXTRACT (`extract.book.meta.v1`). Killing or saturating that processor does not block exact ingest, verify, or restore. `processor/sandbox` plans a bubblewrap argv with no network and no source-tree bind; Darwin `Run` returns `unsupported_platform`. `processor/rpc` sends RUN_STAGE protobuf frames over a Unix socket and also wraps the same messages in grpc-go; both paths pass source/staging bytes on FDs and the host independently SHA-256s staging. Default ingest still uses in-process `RUN_STAGE`. Linux bubblewrap execution remains.
- Exact `content.open`/`read`/`close` sessions over the fake CAS, capped at 1MiB per read. `audio.list` lists admitted audio-tag artifacts and derived albums for one workspace; `books.list` lists admitted EPUB metadata and TXT/Markdown extracts. `content.read` can range-read those subjects' exact bytes. There is no playback, transcoding, playlist, or reader surface. The same list operations are exposed on the read-only MCP server.
- Host-owned `SnapshotTree` and `FileAccess` over the catalog and CAS (`server/internal/access`). Exact reads use `DECODE_REPRESENTATION` through `decode.IdentityDecoder`; the host independently SHA-256s decoded bytes. File-shaped egress is restore plus `FileAccess`. The leftover go-fuse adapter is not a product gate. M4 byte equality is FileAccess vs restore SHA-256.
- A Unix-socket daemon (`restoreweaved`) and thin client (`rw`) that expose ingest, snapshot list/verify, restore, namespace browse, search, annotations, content reads, audio tag listing, and book listing. `rw mount` is an honest `unimplemented` pointing at `plan.restore`.
- A legacy, non-normative `internal/plugin` prototype, frozen; suffix/magic rules live in `internal/identify`, and Processor roles live in `internal/processor`.

The next implementation order is deliberately narrow:

1. Finish remaining Kopia/Restic qualification gates (NAS/S3, and Linux kernel re-run where needed) before replacing the fake CAS. Crash retry, keep-latest prune, and repo-relocation probes run on Darwin/Unix when the CLIs are on PATH; they do not select a release engine.
2. Execute Linux bubblewrap, then expand the default pack (Tika/libarchive/ffprobe) only after that host is qualified. The argv policy, FD control frames, and grpc-go wrapping of the same private messages are already tested.
3. Grow the music and reader experiences beyond tag/OPF listing and exact range reads: a player and a reader UI remain later. Do not introduce ffmpeg for identity.
4. Do not spend further work making RestoreWeave a FUSE server. Operators who want a mount use other tools against a restored tree or exact reads.

See [Implementation Completion Plan](docs/technical/implementation-completion-plan.md) for the Darwin / Linux / later split.

The first post-MVP milestone is a reviewed migration and source-retirement profile that can release capacity only after clean recovery and rollback gates pass. Optional semantic processors and platform capture drivers then expand the system without redefining its core.

## Current verification

The current Go foundations are verified with:

```text
go test -race -count=1 ./...
go vet ./...
go mod verify
```

These checks validate the implemented packages and dependencies only; they do not imply that `RW-MVP-1` is runnable or release-qualified. Licensing, packaging, and redistribution metadata are not finalized yet; third-party adoption policy is documented in [Open-Source Adoption and Code Borrowing](docs/references/open-source-adoption-and-code-borrowing.md).
