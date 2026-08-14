# Open-Source Adoption and Code Borrowing

> **Decision status:** This document records an implementation-oriented open-source audit completed on 2026-08-12. It is informative unless an MVP or technical plan explicitly freezes a choice. “Adopt” below means “use in the planned reference implementation after the stated qualification gate”; it does not claim that the dependency is already present in the current Go module.

## 1. Conclusion

RestoreWeave should be assembled from mature components around a small authority-owning core. It should not rebuild repository compression, general file parsers, a search engine, a workflow runtime, or a FUSE protocol stack.

The practical reference stack is:

| Area | Current decision |
| --- | --- |
| Exact repository | Run a **Kopia-led qualification spike** against Kopia v0.23.1. Keep Restic v0.19.1 as the control benchmark and possible subprocess driver. No repository engine is adopted until the spike passes. |
| File identification | Keep suffix rules host-owned. Adopt bounded [file/libmagic](https://github.com/file/file) evidence as the default magic stage; optionally add [Siegfried](https://github.com/richardlehane/siegfried) for ambiguous or preservation-sensitive formats. |
| Document, archive, and media processing | Use qualified, isolated processors around [Apache Tika](https://github.com/apache/tika), [libarchive](https://github.com/libarchive/libarchive), and [ffprobe/FFmpeg](https://github.com/FFmpeg/FFmpeg). Keep [ExifTool](https://github.com/exiftool/exiftool) optional until its distribution terms are reconciled. |
| Baseline search | Freeze SQLite FTS5 as the bundled `RW-MVP-1` `IndexProvider` and `QueryProvider` implementation. Use one disposable database per immutable `IndexGenerationRef`; keep the schema private. |
| Processor runtime | Use protobuf control schemas, gRPC over Unix-domain sockets, pre-opened file descriptors for large bytes, and host-owned Linux sandboxing and resource controls. |
| Safe Linux capture | Qualify the handle APIs in [pathrs-lite](https://github.com/openSUSE/libpathrs/tree/main/contrib/bindings/go/pathrs-lite) for retained-root, component-relative resolution. If MPL-2.0 review or behavior fails, implement a narrow private [`openat2(2)`](https://man7.org/linux/man-pages/man2/openat2.2.html) layer with `golang.org/x/sys/unix`; never use the legacy string-returning `SecureJoin` API as a security boundary. |
| Change hints | Use [fsnotify v1.10.1](https://github.com/fsnotify/fsnotify/releases/tag/v1.10.1) only as an optional local hint provider. Overflow, reset, non-recursive coverage, NFS, SMB, FUSE, or uncertain continuity forces a complete scan. Borrow recrawl and poison-state concepts from [Watchman](https://github.com/facebook/watchman) without making it the default dependency. |
| Snapshot capture | Use the installed, version-qualified official ZFS or Btrfs command interfaces behind optional `CaptureDriver` profiles. ZFS uses atomic snapshots plus holds; Btrfs uses explicit read-only snapshots, identity evidence, privilege separation, and pre-consumer revalidation. |
| Original-path mount | Adopt [hanwen/go-fuse/v2 v2.11.0](https://github.com/hanwen/go-fuse/tree/v2.11.0) as a private Linux adapter dependency after cache, authorization, and read-amplification qualification. |
| CLI and AI access | Keep CLI as the mutation surface and use the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) for the local read-only MCP adapter after pinning one license-stable revision. MCP is not an internal bus. |
| Selective code borrowing | Use [Perkeep](https://github.com/perkeep/perkeep) as the primary Apache-2.0 source for storage-driver conformance-test patterns and chunk-tree/range-read ideas. Preserve RestoreWeave identity and recovery formats. |
| Later semantic search | Qualify Tantivy for larger lexical indexes, LanceDB for local semantic indexing, Qdrant for a service profile, and OpenSearch for enterprise external search only when measured need appears. |
| P2P | Defer it. If later required, prefer narrow [Boxo](https://github.com/ipfs/boxo) modules over embedding all of Kubo. |

The leading counterargument is that a repository engine, sist2-like indexer, and ordinary mount tools may already satisfy most operators. The adoption strategy therefore optimizes for proving one integrated operator loop, not for maximizing the number of extension points.

How each layer should consume those projects — and which local shortcuts would block a later replacement — is recorded in [Whole-Architecture Open-Source Reference](../technical/architecture-open-source-reference.md). That note does not replace the pins and gates below.

## 2. Adoption categories

RestoreWeave uses five distinct categories. They must not be collapsed into one “supported project” list.

| Category | Meaning |
| --- | --- |
| **Planned direct dependency** | A library used inside a narrow reference adapter. Its types remain private and the release pins an exact version. |
| **Isolated processor or sidecar** | A separately sandboxed executable or worker reached through RestoreWeave handles and typed results. It receives no ambient source, repository, credential, signing, or network authority. |
| **Qualification candidate** | A leading implementation that still has a decision-changing correctness, performance, compatibility, or operational gate. |
| **Selective code borrowing** | Specific code or tests may be adapted after license and dependency review, with source provenance retained. RestoreWeave does not inherit the source project’s identities or durable format. |
| **Design or competitor reference only** | Architectural lessons may be implemented independently. No code is copied or linked into the core unless RestoreWeave deliberately selects a compatible project license and accepts the dependency’s obligations. |

An out-of-process boundary is not automatically a licensing or security safe harbor. The integration must still be arm’s-length, least-privileged, version-pinned, and reviewed.

## 3. Candidate matrix

### 3.1 Planned direct dependencies and bundled implementation choices

| Project | RestoreWeave role | Decision and boundary | License evidence |
| --- | --- | --- | --- |
| [SQLite FTS5](https://www.sqlite.org/fts5.html) through the existing `modernc.org/sqlite` dependency | Bundled lexical `IndexProvider` and `QueryProvider` for `RW-MVP-1` | **Freeze for the MVP implementation.** Create one physically separate disposable database per immutable generation. The SQLite schema, row IDs, token tables, and query syntax are never durable RestoreWeave ABI. Assert FTS5 availability during startup qualification. | SQLite is [public domain](https://github.com/sqlite/sqlite/blob/da7dc33fb2075dc9a9376679889f6843c33d6cb9/LICENSE.md); separately audit wrappers, generated builds, and extensions. |
| [hanwen/go-fuse/v2 v2.11.0](https://github.com/hanwen/go-fuse/tree/v2.11.0) | Bundled read-only Linux FUSE projection | **Adopt behind `SnapshotTree` and `FileAccess`.** Pin commit [`423b377`](https://github.com/hanwen/go-fuse/commit/423b377e1452ab7b3522229185a3047f72e3f966). No go-fuse type enters portable records or public contracts. The tagged CI included a rename-exchange failure outside RestoreWeave's read-only surface, so adoption still depends on the project's own read-only suite; later upstream revisions require normal dependency-upgrade qualification. | Permissive [BSD-style license](https://github.com/hanwen/go-fuse/blob/v2.11.0/LICENSE). |
| [file/libmagic](https://github.com/file/file) | Bounded magic-byte evidence after suffix inspection | **Adopt and bundle.** Pin the compiled magic database digest. Disable compressed inspection, decompressor forking, device access, symlink following, and unnecessary deep parsers. A tiny isolated helper is preferred where native-code containment is practical. | Permissive [BSD-style terms](https://github.com/file/file/blob/711ccc264519cdc5073ccb26651c0a9bafc3b47a/COPYING). |
| [Protocol Buffers](https://github.com/protocolbuffers/protobuf) and [grpc-go](https://github.com/grpc/grpc-go) | Processor control-plane schemas and local RPC | **Adopt as implementation plumbing**, not yet as a frozen public ABI. Use Unix-domain sockets and keep large payloads outside messages. Exact release pins follow the first cross-version conformance spike. | Permissive upstream licenses; exact release and transitive review remain release work. |
| [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | Local read-only MCP adapter | **Adopt after pinning one revision whose license transition is complete and reproducible.** MCP exposes northbound inspection operations only; it is not storage, scheduling, or processor transport. | The repository is transitioning between Apache and MIT terms; capture the exact applicable license at the selected revision. |
| [fsnotify v1.10.1](https://github.com/fsnotify/fsnotify/releases/tag/v1.10.1) | Optional local change-hint provider | **Adopt only as acceleration.** The public API is non-recursive, and upstream documents that NFS, SMB, and FUSE generally do not provide useful notifications. Overflow or continuity loss invalidates the checkpoint and schedules a complete scan; events never directly create authoritative namespace mutations or tombstones. | [BSD-3-Clause](https://github.com/fsnotify/fsnotify/blob/v1.10.1/LICENSE). |

No privileged mount helper should be vendored. The Linux deployment uses the host’s `/dev/fuse` and `fusermount3`, with explicit container privileges when needed.

### 3.2 Qualification candidates

| Project | Candidate role | Decision-changing gate | License |
| --- | --- | --- | --- |
| [Kopia v0.23.1](https://github.com/kopia/kopia/releases/tag/v0.23.1) | Leading `RepositoryDriver` implementation | Prove that RestoreWeave objects are rooted safely under Kopia’s snapshot-centric garbage collection; then prove crash reconciliation, bounded reads, complete SHA-256 readback, catalog-independent recovery, corruption behavior, and NAS performance. Pin at least v0.23.1 and disable unneeded server and external-SSH surfaces; [GHSA-2q4c-3mrw-63c3](https://github.com/kopia/kopia/security/advisories/GHSA-2q4c-3mrw-63c3) affects versions through v0.22.3. | [Apache-2.0](https://github.com/kopia/kopia/blob/b361d0f4100898ce3ad479755f104ff2c5a35e01/LICENSE) |
| [Restic v0.19.1](https://github.com/restic/restic/releases/tag/v0.19.1) | Control benchmark and possible subprocess driver | Upstream explicitly treats Restic as a CLI rather than a supported library. Benchmark exact footprint, ingest, range reads, mount behavior, verification, restore, and operating burden through a process boundary. | [BSD-2-Clause](https://github.com/restic/restic/blob/a80be1478a4c537f8396e0db2b05120aa78f11e0/LICENSE) |
| [Borg 1.4.5](https://github.com/borgbackup/borg/releases/tag/1.4.5) | Bounded local/SSH benchmark and design reference | Its CLI boundary is less natural for RestoreWeave, and repair may discard damaged data. Use it to calibrate compression, maintenance, and mounted-read expectations, not as the leading adapter. | [BSD-3-Clause](https://github.com/borgbackup/borg/blob/1.4.5/LICENSE) |
| [Siegfried v1.11.6](https://github.com/richardlehane/siegfried/releases/tag/v1.11.6) with PRONOM v124 | Optional deeper identification Processor | Pin the signature file and disable online update, server mode, directory walking, and uncontrolled archive recursion. Its result remains evidence, not parseability or exact-identity proof. | [Apache-2.0](https://github.com/richardlehane/siegfried/blob/v1.11.6/LICENSE) |

[rustic v0.11.3](https://github.com/rustic-rs/rustic/tree/v0.11.3) is track-only. Its reusable Rust structure is attractive, but upstream still discourages production use and lacks enough stability evidence for authoritative recovery.

Safe path resolution remains a qualification candidate rather than an adopted dependency. [pathrs-lite](https://github.com/openSUSE/libpathrs/tree/main/contrib/bindings/go/pathrs-lite) exposes handle-oriented APIs and opportunistically uses Linux `openat2`, but its applicable code is primarily [MPL-2.0](https://github.com/openSUSE/libpathrs/blob/main/LICENSE). RestoreWeave must review file-level copyleft obligations, confirm root-handle lifetime and fallback behavior, and use only the handle API. Upstream maintenance is active, but sampled scheduled workflows were not uniformly green; RestoreWeave therefore relies on its own Linux race and mount-substitution qualification rather than a generic upstream-CI claim. Upstream explicitly describes the older string-returning [`SecureJoin`](https://github.com/cyphar/filepath-securejoin#legacy-api) pattern as vulnerable to time-of-check/time-of-use attacks; it is rejected for authoritative capture. If pathrs-lite is unsuitable, the reference implementation should keep a narrow private `golang.org/x/sys/unix` `openat2` resolver rather than expose Linux syscall structures in public contracts.

### 3.3 Isolated Processor pack

| Project | Exact role | Integration decision | License and packaging boundary |
| --- | --- | --- | --- |
| [Apache Tika](https://github.com/apache/tika) | Office, PDF, email, ebook, document metadata/text extraction, and embedded-object enumeration | Use as a private `PARSE`/`EXTRACT` sidecar with RMETA-like per-object output. RestoreWeave owns input staging, routing, output admission, and publication. Do not expose Tika Server or gRPC as a public untrusted endpoint. Select and qualify one pinned stable release; do not follow `main` or a prerelease automatically. | Core is [Apache-2.0](https://github.com/apache/tika/blob/787d9ffe4e3b31fa715eea4690ee6b1febfaee0c/LICENSE.txt), but full distributions contain [NOTICE-listed CDDL and CDDL/LGPL dependencies](https://github.com/apache/tika/blob/787d9ffe4e3b31fa715eea4690ee6b1febfaee0c/NOTICE.txt). Retain the exact NOTICE and SBOM. |
| [libarchive](https://github.com/libarchive/libarchive) | Archive structure enumeration and bounded virtual-member streaming | Run in a separate archive `PARSE` worker. Use callback reads and stream selected members to host staging. Never extract an uncontrolled tree. Enforce format, filter, entry-count, nesting, expanded-byte, ratio, sparse-extent, filename, and time budgets. | Predominantly permissive [BSD-style terms with per-file exceptions](https://github.com/libarchive/libarchive/blob/master/COPYING). |
| [ffprobe/FFmpeg](https://github.com/FFmpeg/FFmpeg) | Media container, stream, codec, duration, chapter, timing, and tag metadata | Bundle a controlled external media worker. Whitelist local file or pipe protocols, disable network access, select output fields, and bound probe bytes, analysis duration, process time, memory, and output. | [LGPL-2.1-or-later by default](https://github.com/FFmpeg/FFmpeg/blob/a681001244b697e33fb94557dfd3f924250edb8c/LICENSE.md); `--enable-gpl` changes the binary to GPL and `--enable-nonfree` makes it unredistributable. Record the complete build configuration, linked libraries, codec review, and SBOM. |
| [ExifTool](https://github.com/exiftool/exiftool) | Optional rich camera, image, audio, video, document, application, and maker-note metadata | Begin as a single-shot read-only subprocess. Accept only sandbox staging paths, request bounded JSON, suppress binary blobs, and recycle or quarantine any later persistent worker. Do not copy its parser implementation into the core. | Upstream evidence describes Perl/Artistic-or-GPL terms while the root license contains GPLv3 text. Reconcile the exact selected distribution before bundling. |

The default route is deliberately overlapping but host-controlled:

```text
mandatory exact hash and placement lane

parallel identification and processing lane:
suffix evidence
-> bounded libmagic evidence
-> optional Siegfried evidence for ambiguity or preservation depth
-> exactly one primary structural route
   |-> document: Tika
   |-> archive: libarchive
   |-> media: ffprobe
-> optional ExifTool metadata supplement
-> host schema validation, digesting, artifact admission, and indexing
```

A parser success proves only that the processor produced an accepted result. It cannot establish exact storage identity, authorize omission, publish a snapshot, or accept verification.

Learned identification such as [Magika](https://github.com/google/magika) remains a later advisory Processor for unknown or conflicting inputs. It is not needed for `RW-MVP-1`, and its confidence never overrides exact fallback.

### 3.4 Search and semantic expansion

| Project | Later role | Decision |
| --- | --- | --- |
| [Tantivy](https://github.com/quickwit-oss/tantivy) | Larger embedded lexical provider | First scale-up candidate after SQLite FTS5 reaches a measured ceiling. MIT licensed. |
| [LanceDB](https://github.com/lancedb/lancedb) | Local semantic and multimodal index | Qualify later for a self-contained semantic profile. Do not make it durable truth. |
| [Qdrant](https://github.com/qdrant/qdrant) | Service or enterprise vector provider | Use only as an optional external `IndexProvider`/`QueryProvider` profile with pinned collection generations and image digest. Apache-2.0. |
| [OpenSearch](https://github.com/opensearch-project/OpenSearch) | Enterprise external lexical, structured, and vector search | Defer until multi-node operations, external ACL integration, or scale justify its cost. |
| [Meilisearch](https://github.com/meilisearch/meilisearch) | Optional search-UX sidecar | Not the default because licensing is mixed and it adds a daemon without solving recovery semantics. |
| [FAISS](https://github.com/facebookresearch/faiss) | Approximate-nearest-neighbor algorithm component | Not a complete provider: it does not supply generation lifecycle, durable subject mapping, authorization, or operational service behavior. |

Embedding, CLIP, audio-language, OCR, ASR, caption, and code-understanding models remain external Processor outputs. Model code, weights, tokenizer, preprocessing, and acceptable-use terms are separately pinned artifacts.

### 3.5 Selective code borrowing

[Perkeep](https://github.com/perkeep/perkeep) is the strongest code-borrowing source because its applicable code is Apache-2.0 and several mechanisms align with RestoreWeave without defining the product.

Review these exact areas before implementation:

- [`pkg/blobserver`](https://github.com/perkeep/perkeep/tree/9406ea272705d1f8f63c9b2ed31274962c30e3e1/pkg/blobserver) for small storage interfaces and adapter conformance-test patterns.
- [`pkg/schema/filewriter.go`](https://github.com/perkeep/perkeep/blob/9406ea272705d1f8f63c9b2ed31274962c30e3e1/pkg/schema/filewriter.go) for chunk-tree construction ideas.
- [`pkg/schema/filereader.go`](https://github.com/perkeep/perkeep/blob/9406ea272705d1f8f63c9b2ed31274962c30e3e1/pkg/schema/filereader.go) for range reads and reconstruction patterns.

Do not inherit Perkeep’s SHA-224 defaults, schema language, object identity, publication model, or recovery format. RestoreWeave retains SHA-256 content identity, `RepositoryDriver` receipts, portable publication records, and catalog-independent recovery semantics.

Every borrowed fragment must record the source repository, exact commit, source path, applicable license, retained notices, local destination, substantive modifications, and the tests that establish independent RestoreWeave behavior.

### 3.6 Design and competitor reference only

| Project | Useful lesson | Why code is not adopted into the core |
| --- | --- | --- |
| [Spacedrive](https://github.com/spacedriveapp/spacedrive) | Separate path identity, content identity, user metadata, sidecars, full-text/vector search, and a headless API. | Current code uses [FSL-1.1-ALv2](https://github.com/spacedriveapp/spacedrive/blob/6dfeccf2113039e35f2ce735f945e70dc3e4ea45/LICENSE) with explicit competing-use restrictions. Use independent design analysis only. Its sampled/truncated large-file hash must not become exact recovery identity. |
| [Seafile Server](https://github.com/haiwen/seafile-server) and [Seafile core/client](https://github.com/haiwen/seafile) | Git-like commits, trees, blocks, virtual repositories, versioning, and drive projection. | The server is [AGPL-3.0 plus an OpenSSL exception](https://github.com/haiwen/seafile-server/blob/8c47d5f5810e71d75778eb02577ef9ad69013d76/LICENSE.txt); core/client is [GPLv2 plus an OpenSSL exception](https://github.com/haiwen/seafile/blob/2ebf6ac8b0755c9a88d5ed9295f50e7c6c9a3255/LICENSE.txt). |
| [sist2](https://github.com/sist2app/sist2) | Heterogeneous parser routing, bounded extraction, index-document preparation, SQLite/Elasticsearch choices, and optional embedding hooks. | [GPL-3.0](https://github.com/sist2app/sist2/blob/fc8dce64457c927b1d193dea585cef81deabc618/LICENSE). It may later run as an independently installed external Processor, but code is not copied or linked into the core unless RestoreWeave deliberately accepts GPL obligations. |
| [Immich](https://github.com/immich-app/immich) | Asynchronous media processing, an external ML service, ACL-aware search, and resource isolation. | [AGPL-3.0](https://github.com/immich-app/immich/blob/199723261c6ffa897fec8ccdaea6359e39c37cc3/LICENSE). It is a vertical media product and not recovery authority. |
| [rclone v1.75.0](https://github.com/rclone/rclone/releases/tag/v1.75.0) | Directory caching, read-ahead, bounded cache behavior, mount metrics, and representative VFS workloads. | Its broad remote and mutable VFS model should not own RestoreWeave namespace semantics. Borrow patterns and benchmark cases only, and never reuse older path-validation assumptions: [GHSA-45pq-889g-fcgh](https://github.com/rclone/rclone/security/advisories/GHSA-45pq-889g-fcgh) affects releases through v1.74.4. MIT licensed. |
| [Kopia](https://github.com/kopia/kopia/tree/v0.23.1/fs) FUSE adapter | A thin go-fuse adapter structure and historical large-directory qualification evidence. [Issue #1135](https://github.com/kopia/kopia/issues/1135) documents a `READDIRPLUS`-related quadratic behavior that was fixed; retain it as a regression case, not a claim about current Kopia performance. | Borrow adapter structure and tests only. Kopia is Apache-2.0, but its repository and namespace models do not become RestoreWeave ABI. |
| [Restic FUSE](https://github.com/restic/restic/tree/v0.19.1/internal/fuse) | Chunk-offset lookup, blob caching, and concurrent offset reads. [Issue #3828](https://github.com/restic/restic/issues/3828) reports mounted access around 8x and, in another workload, 25x slower than restore. | Borrow BSD-2-Clause read-path patterns and use the issue as performance counterevidence; do not assume a repository-backed mount is automatically interactive. |
| [rclone mount2](https://github.com/rclone/rclone/tree/v1.75.0/cmd/mount2) and [`vfs`](https://github.com/rclone/rclone/tree/v1.75.0/vfs) | Thin `ReadAt` handle style plus chunk-growth, stream-count, read-ahead-range, cache, transfer-accounting, retry, metrics, mount-option, and `READDIRPLUS` qualification vocabulary. | Borrow MIT-licensed vocabulary, bounded design patterns, and tests rather than the subsystems wholesale; they are coupled to rclone's writable remote VFS and global configuration model. |
| [Watchman](https://github.com/facebook/watchman) | Recrawl, overflow, synchronization, and poison-state concepts for lossy filesystem observation. | Design and test-pattern reference only. Its daemon and operational footprint are too large for the default watcher dependency. MIT licensed. |
| [zrepl](https://github.com/zrepl/zrepl) | ZFS snapshot lifecycle, holds, pruning safety, and platform-test patterns. | Borrow MIT-licensed lifecycle and test ideas only; do not embed the daemon or adopt its replication policy as RestoreWeave authority. |
| [Temporal](https://github.com/temporalio/temporal), [Apache NiFi](https://github.com/apache/nifi), [containerd](https://github.com/containerd/containerd), and [Google ByteStream](https://github.com/googleapis/googleapis/blob/master/google/bytestream/bytestream.proto) | Durable activity concepts, provenance/backpressure, content lifecycle, and resumable byte-transfer patterns. | Do not embed these broad runtimes into the MVP. Reimplement only the narrow storage-specific semantics required by the operation journal and Processor handles. |

### 3.7 Rejected or deferred for the MVP

- Do not use HashiCorp `go-plugin` as the public Processor ABI.
- Do not make Temporal, NiFi, containerd, or OCI the mandatory execution substrate.
- Do not carry large payloads inside protobuf, JSON, REST, or MCP messages.
- Do not use MCP as an internal scheduler, data plane, or repository protocol.
- Do not embed all of [Kubo](https://github.com/ipfs/kubo) for future P2P work. Prefer narrow Boxo modules only after the P2P profile is activated.
- Do not add Tantivy, LanceDB, Qdrant, OpenSearch, Meilisearch, or FAISS before SQLite FTS5 is measured against the MVP corpus.
- Do not adopt rustic as a production recovery engine while upstream still discourages production use.
- Do not adopt the legacy string-returning `SecureJoin` API, final-component-only no-follow wrappers, or silent fallback from `openat2` to ambient absolute-path traversal for authoritative capture.
- Do not make Watchman the default watcher service; use its failure semantics as a design reference.
- Do not use [bazil/fuse](https://github.com/bazil/fuse) for a new MVP adapter. Keep the actively maintained [jacobsa/fuse](https://github.com/jacobsa/fuse) as a fallback candidate only; go-fuse still leads because of its tagged release, demonstrated Kopia/rclone use, resource and invalidation controls, and fit with the current design. Defer [libfuse](https://github.com/libfuse/libfuse), [cgofuse](https://github.com/winfsp/cgofuse), and [WinFsp](https://github.com/winfsp/winfsp) to later platform profiles.
- Do not copy `btrfs-progs` or OpenZFS implementation code into a permissive core. Integrate their installed, version-qualified command interfaces and retain exact command/output compatibility evidence.
- Do not copy FSL, GPL, or AGPL implementation text into the core under the label “reference only.” Any such adoption requires an explicit RestoreWeave licensing and distribution decision.

## 4. Kopia qualification spike

Kopia leads because its Go repository APIs, object reads, storage abstractions, and verification mechanisms fit `RepositoryDriver` better than a CLI-only engine. The selection can still fail on the most important question: whether RestoreWeave’s portable objects remain live under Kopia maintenance.

The spike must prove all of the following against Kopia v0.23.1:

1. **Root and garbage-collection safety:** every payload, prepared closure, publication commit, and retained recovery dependency is reachable through a documented root model. Stock or supported maintenance cannot collect arbitrary RestoreWeave objects.
2. **Crash reconciliation:** interruption before and after every physical write, logical receipt, and publication boundary yields no false published state and can be reconciled idempotently.
3. **Bounded and random reads:** `FileAccess` can serve first-byte, sequential, and random-range workloads without pathological read amplification.
4. **Independent exact verification:** RestoreWeave can read complete bytes through the driver and compute its own SHA-256 rather than accepting repository metadata as sufficient proof.
5. **Catalog-independent recovery:** a clean installation can locate and authenticate the portable publication closure and restore without the operational SQLite catalog or search index.
6. **Corruption behavior:** injected missing, truncated, reordered, or modified storage objects produce explicit failures and never a healthy recovery claim.
7. **Maintenance compatibility:** connect, upgrade, repair, compaction, and garbage collection preserve the published recovery contract or fail closed.
8. **Deployment performance:** measure local disk, mounted NAS storage, and S3-compatible storage on low-power and mid-range hardware.
9. **Reader closure:** record the exact Kopia version, repository compatibility promise, required binaries or libraries, configuration, credentials, and clean-machine recovery procedure.

Run the same corpus through Restic v0.19.1 as the control. Use Borg 1.4.5 only where local or SSH comparisons add information. Select the release engine from observed correctness and complete operating cost, not from repository stars or nominal compression ratios.

## 5. Capture hardening, change hints, and snapshot drivers

### 5.1 Linux root-confined capture

The existing scanner is useful implementation evidence, but it protects only the final opened component with no-follow semantics. It still reconstructs descendants as ambient absolute paths. That is insufficient for authoritative capture because an ancestor, bind mount, remount, or source root can be substituted after inspection.

The Linux reference profile must have this shape:

```text
retained source-root handle
-> component-relative resolution
-> openat2 or independently qualified equivalent
-> handle-based enumeration, stat, readlink, open, and read
-> mount, root, and snapshot identity validation
-> authoritative publication gate
```

Use `RESOLVE_BENEATH`, `RESOLVE_NO_MAGICLINKS`, no-follow behavior, and policy-controlled mount crossing where the selected resolver provides equivalent semantics. Pin parent directories until children are safely opened. Type-check through non-activating handles before a FIFO, device, socket, or other special object can be opened as ordinary content. Absolute paths remain display and audit values only.

The qualification suite must replace every ancestor and selected entry with symlinks, renamed directories, bind mounts, remounted shares, different filesystem objects, FIFOs, and device-like objects. It must also remove snapshot protection before consumer start and test kernels where required resolver facilities are unavailable. Every unsafe or ambiguous case blocks publication; external bytes, probes, and digests must never reach the content sink.

### 5.2 Incremental observation

`fsnotify` can reduce reconciliation work on ordinary supported local filesystems, but it cannot establish completeness. Each provider declares recursion, filesystem support, overflow behavior, journal epoch, reset behavior, and event-loss limitations. NFS, SMB, FUSE, non-recursive watches, overflow, reset, rollback, or uncertain continuity invalidates the checkpoint and requires a complete baseline. Watcher events may enqueue or prioritize work; they never directly mutate the durable namespace or prove a deletion.

Watchman is valuable counterevidence and a pattern source: mature watcher systems need explicit recrawl, synchronization, and poisoned-state behavior. RestoreWeave should borrow those state-machine ideas while keeping the default dependency small.

### 5.3 Optional ZFS and Btrfs profiles

The generic local or mounted-tree profile remains mandatory. Native snapshot drivers are optional stronger capture profiles:

- **ZFS:** use the installed, version-qualified official [`zfs`](https://github.com/openzfs/zfs/tree/zfs-2.4.3) CLI and JSON output. The official documentation defines snapshots as atomic and holds as preventing destruction while held. Acquire the hold before exposing the snapshot, retain it through all bound consumers, record pool, dataset, snapshot, GUID and transaction evidence where available, and borrow lifecycle/test patterns from MIT-licensed zrepl. OpenZFS is CDDL-1.0; do not copy its implementation or manpage text into a permissive core.
- **Btrfs:** use a version-qualified installed [`btrfs-progs`](https://github.com/kdave/btrfs-progs) CLI and machine-readable output. Create snapshots explicitly read-only and record filesystem UUID, subvolume UUID, received/parent UUID where applicable, generation, and read-only state. Do not treat recursive userspace snapshot creation as atomic. Btrfs has no ZFS-style non-deletable hold, so separate snapshot-deletion privilege from readers and revalidate identity and read-only state before every consumer. `btrfs-progs` is GPL-2.0; integrate the command boundary without copying its code into the core. The LGPL-2.1 [`libbtrfsutil`](https://github.com/kdave/btrfs-progs/blob/v7.1/libbtrfsutil/README.md) fd APIs remain a later native-driver candidate if measured subprocess overhead or fd-rooted semantics justify the extra binding surface.

## 6. Processor runtime and isolation

The first reusable Processor implementation should use:

```text
protobuf schemas
+ grpc-go over a Unix-domain socket for control
+ pre-opened input and output file descriptors for large bytes
+ bubblewrap, namespaces, seccomp, no_new_privs, rlimits, and cgroup v2
+ the existing SQLite operation journal for leases, fencing, retries, and reconciliation
```

This is reference plumbing, not the durable public meaning of a Processor. The contract remains typed operations, handles, result envelopes, artifact state, and conformance behavior.

Every external processor receives:

- No network by default.
- No ambient source or repository mount.
- A private temporary directory.
- Read-only staged input or pre-opened input handles.
- Attempt-fenced outputs owned by the host.
- CPU, memory, process, temporary-disk, wall-time, recursion, expansion, and output limits.
- An empty or allowlisted environment.
- Cancellation, process recycling, and crash-loop quarantine.

The host still computes digests, validates schemas, admits artifacts, records provenance, and decides whether anything may enter an index or repository.

## 7. SQLite FTS5 generation model

SQLite FTS5 is frozen only as the bundled MVP implementation. It does not become a portable search format.

For every new `IndexGenerationRef`:

1. Create a new physical database rather than rewriting the active generation in place.
2. Record the provider revision, schema digest, tokenizer and normalization configuration, feed high-water mark, coverage, and authorization-label revision.
3. Build from the replayable authorized feed.
4. Validate record counts, known-query fixtures, subject resolution, stale-state reporting, and authorization behavior.
5. Atomically activate the immutable generation reference.
6. Retain the prior database for bounded rollback, then delete it under ordinary derived-data retention.

Every result contains stable `SubjectRef` values. The host reauthorizes those subjects; an FTS row ID is never an external identity. Deleting every FTS database must leave content, namespace, tags, notes, publication, verification, and restore intact.

## 8. FUSE adapter qualification

The Linux adapter maps one immutable snapshot view through go-fuse. One mount binds exactly one authenticated principal, one export root, and one immutable committed snapshot; `latest` is resolved before the mount becomes visible. The adapter requires `ro,nodev,nosuid,noexec`, disables `allow_other`, and fails rather than silently weakening those settings. Do not copy Kopia's current mount boundary: its adapter does not explicitly request all four protections, and omission of mutation methods is not equivalent to a kernel-enforced read-only mount. Verify both direct-mount flags and every option passed through `fusermount3`.

Use upstream code as bounded pattern sources:

- Kopia for a thin go-fuse adapter structure and the fixed `READDIRPLUS` regression represented by issue #1135.
- Restic for chunk-offset reads and caching, plus its inode, directory, symlink, and xattr adapters as test-pattern sources, with issue #3828 retained as strong performance counterevidence. Restic's xxhash-derived inode values are an implementation example only; RestoreWeave still owns collision resolution and snapshot-scoped stability.
- rclone `mount2` and its VFS read/cache code for `ReadAt`, chunk-growth, stream-count, read-ahead, transfer accounting, retries, metrics, mount-option wiring, and `READDIRPLUS` qualification vocabulary. Do not import the writable VFS or global configuration model wholesale.

The adapter must preserve:

- Lookup, listing, attributes, and `readlink` use a pinned `SnapshotView`.
- Opens and reads use bounded `FileAccess` sessions and random-access reads.
- Inode identity is stable within the mount, collision-resolved, and follows snapshot-scoped namespace and hard-link identity rather than content hashes alone.
- Directory handles own independent bounded cursors. Opaque cookies translate to snapshot-and-directory-scoped `PageToken` values without replaying earlier pages, and `READDIRPLUS` must remain linear and bounded.
- Raw path-component bytes, hard links, symbolic links, and sparse extent semantics survive the adapter boundary.
- Every write-capable open and mutation opcode returns `EROFS` before any namespace, repository, processor, or annotation change.

The main risk is kernel caching. Attribute, entry, negative, and page caches may serve data without another host authorization decision. Qualification must define bounded TTLs, invalidation or direct-I/O behavior where needed, and unmount-on-authorization-expiry policy. If revocation cannot be enforced acceptably, the product must describe the mount as a local-trust surface rather than an independently authorized request boundary.

Required tests include identical CLI/FUSE/restore SHA-256 bytes; cold and warm first-byte latency; large-directory `readdir` and `READDIRPLUS` scaling; sequential and random reads; repository request and byte amplification; process and kernel-cache memory; concurrent file and directory handles; snapshot pins across repack and garbage collection; non-UTF-8 names; pagination and cookie resume; hard links; sparse files; recorded symlinks; rejection of every write-capable open and mutation opcode; authorization expiry through existing handles, page cache, and mmap; and clean or crash-driven unmount.

The fallback [bazil/fuse](https://github.com/bazil/fuse) project has no decision-changing advantage and showed no substantive current-maintenance evidence beyond 2023 in this bounded audit. The actively maintained [jacobsa/fuse](https://github.com/jacobsa/fuse) remains fallback-only despite useful read-only, permission, adaptive `READDIRPLUS`, direct-I/O, and parallel-directory controls. [libfuse](https://github.com/libfuse/libfuse), [cgofuse](https://github.com/winfsp/cgofuse), [WinFsp](https://github.com/winfsp/winfsp), and macOS adapters remain separate future platform profiles; none changes the portable namespace model. If a future libfuse profile uses io_uring, pin at least v3.18.2 and qualify it explicitly because [GHSA-qxv7-xrc2-qmfx](https://github.com/libfuse/libfuse/security/advisories/GHSA-qxv7-xrc2-qmfx) and [GHSA-x669-v3mq-r358](https://github.com/libfuse/libfuse/security/advisories/GHSA-x669-v3mq-r358) affect earlier 3.18.x releases.

## 9. License and supply-chain policy

Every adopted library, executable, container, signature database, model, and model weight must be release inventory, not an untracked download.

The release process must:

1. Pin an exact release or commit and a source, binary, container, signature-database, or weight digest. Never use a mutable `latest` tag.
2. Retain the applicable LICENSE, NOTICE, source offer where required, build flags, patch record, upstream checksums or signatures, and a transitive SBOM.
3. Produce a separate SBOM for every Processor image or executable bundle.
4. Permit MIT, BSD, Apache, and public-domain code in-process only after transitive-license and provenance review.
5. Prefer LGPL components as separately replaceable executables or dynamically linked libraries. Static linking requires explicit compliance artifacts.
6. Keep GPL, AGPL, and FSL code out of the core and default bundle unless the project deliberately selects a compatible RestoreWeave license and accepts the corresponding distribution obligations and business restrictions.
7. Treat a subprocess or container as an engineering boundary, not automatic legal immunity. Use standard arm’s-length protocols and independent lifecycle/configuration.
8. Record FFmpeg configure flags and block `--enable-gpl` or `--enable-nonfree` unless explicitly authorized. Review codec patents separately from copyright licensing.
9. Treat model code, weights, tokenizer, preprocessing, datasets, model card, acceptable-use terms, and weight license as distinct artifacts. A library license does not license its weights.
10. Quarantine and requalify an installed version after unexpected license, dependency, signer, digest, CVE, build-flag, or provenance drift.
11. Run automated dependency, vulnerability, and SBOM scans plus manual review for every custom, copyleft, `NOASSERTION`, model-weight, codec, and generated-data license.
12. Preserve source provenance for every selectively borrowed fragment and independently test the resulting RestoreWeave behavior.

These rules complement the normative [Release Qualification and Traceability](../requirements/release-qualification-and-traceability.md) requirements.

## 10. Immediate implementation order

1. Implement and adversarially qualify the retained-root Linux capture path before converting scanner observations into authoritative namespace records. Decide pathrs-lite versus a private `x/sys/unix` `openat2` layer through behavior and license review.
2. Complete the core route, handle, artifact, operation-journal, `RepositoryDriver`, `IndexProvider`, and `QueryProvider` test doubles and conformance fixtures.
3. Build the Kopia v0.23.1 qualification spike and run Restic v0.19.1 as the control before committing to a repository engine.
4. Implement the Processor host with protobuf control, Unix-domain gRPC, pre-opened file descriptors, sandboxing, resource budgets, and a deliberately small deterministic test processor.
5. Add host-owned suffix evidence and bounded libmagic. Then qualify one document, archive, and media processor without letting optional processing delay exact ingest.
6. Implement the bundled SQLite FTS5 generation and query providers with one disposable database per immutable generation.
7. Implement the go-fuse v2.11.0 Linux adapter and measure the complete security, directory-scale, memory, cache, and repository-amplification profile before calling it NAS-usable.
8. Add optional fsnotify acceleration only after complete rescans remain correct without it; qualify ZFS and Btrfs drivers independently after the generic profile works.
9. Add license inventory, NOTICE capture, exact build provenance, per-processor SBOMs, and drift quarantine before distributing third-party binaries.
10. Only after the exact vertical slice passes should the project qualify OCR, embeddings, CLIP, alternate search stores, class-specific transforms, or P2P modules.

## 11. Research coverage and limits

The main Siftline run used query ID `restoreweave-oss-adoption-20260812`. Its final machine ledger recorded **68 attempts**, **63 provider calls**, **63 provider successes**, and **5 cache hits**. GitHub and Hacker News were available. Exa, Tavily, and the configured OpenAI-compatible Web provider were unavailable because credentials were not configured.

A separate focused Processor run, `restoreweave-processor-baseline-20260812`, recorded **20 attempts**, **12 provider calls**, **12 provider successes**, and **8 cache hits**.

A focused capture, snapshot, watcher, and namespace run, `restoreweave-capture-namespace-20260812`, recorded **20 attempts**, **19 provider calls**, **19 provider successes**, and **1 cache hit**. It verified the retained projects and primary repository artifacts for path resolution, watchers, ZFS/Btrfs lifecycle, and FUSE implementation patterns.

The research used repository search, source trees, release tags, primary README and API files, actual LICENSE and NOTICE files, official specifications, and focused community queries. It did not reproduce all performance claims, conduct legal advice, inspect every transitive dependency, benchmark proprietary NAS products, or qualify a shipped binary. “Not retained” therefore means “not decision-changing within this bounded audit,” not “the project does not exist or can never fit.”
