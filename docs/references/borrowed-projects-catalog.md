# Borrowed Projects Catalog

> **Catalog status:** This is the companion quick-reference to [Open-Source Adoption and Code Borrowing](open-source-adoption-and-code-borrowing.md), completed on 2026-08-12. It lists every external project RestoreWeave plans to borrow from, with the concrete role, the exact thing to borrow, the integration mode, the applicable license, and the pinned version or commit. Decisions, gates, and rationale live in the adoption document; this file is a lookup table, not a replacement for it.

## How this catalog is used

1. **Repository-engine entries are `qualification pending` until the spike passes.** Kopia, Restic, Borg, and Plakar remain unselected. No repository engine is adopted, and no repository-dependent milestone is treated as complete, until the Kopia-led qualification spike (nine proof items, [open-source-adoption-and-code-borrowing.md:159-175](open-source-adoption-and-code-borrowing.md#4-kopia-qualification-spike)) passes and the control corpus comparison is finished.
2. **Every selectively borrowed fragment carries full provenance.** Any code taken from Perkeep, pathrs, Kopia, Restic, or rclone must record the source repository, exact commit, source path, applicable license, retained notices, local destination, substantive modifications, and the tests that establish independent RestoreWeave behavior ([open-source-adoption-and-code-borrowing.md:126](open-source-adoption-and-code-borrowing.md#35-selective-code-borrowing)). A fragment without this record is not admitted.
3. **Status is a gate, not a label.** `frozen` means the decision is recorded and the work is authorized after the stated qualifications; `qualification pending` means a decision-changing gate is still open; `new` means the entry was added by external research on 2026-08-12 and has no prior decision history.
4. **Licenses are recorded for the stated version only.** The project will open-source and is not license-constrained, but every pin must still be recorded honestly and rechecked at the exact release, binary, and transitive-dependency level before distribution ([open-source-adoption-and-code-borrowing.md:278-296](open-source-adoption-and-code-borrowing.md#9-license-and-supply-chain-policy)).

---

## Adopted, bundled, and in-progress entries

### 1. Kopia — leading `RepositoryDriver` candidate

- **Project:** Kopia (Go)
- **URL:** https://github.com/kopia/kopia
- **License:** Apache-2.0
- **Version or pin:** v0.23.1
- **Role in five seams:** `RepositoryDriver` (leading candidate)
- **What to borrow:** The whole repository engine as a planned direct dependency — object model, storage abstractions, verification mechanisms, and maintenance/repair behavior. Do not inherit Kopia's repository or namespace model as RestoreWeave ABI.
- **How to integrate:** Direct dependency (library) inside a narrow reference adapter, after the qualification spike. Disable unneeded server and external-SSH surfaces; keep Kopia types private.
- **Status:** Qualification pending
- **Notes:** The spike must prove all nine items — root/GC safety, crash reconciliation, bounded reads, independent SHA-256 readback, catalog-independent recovery, corruption behavior, maintenance compatibility, deployment performance, and reader closure ([open-source-adoption-and-code-borrowing.md:163-173](open-source-adoption-and-code-borrowing.md#4-kopia-qualification-spike)). Pin at least v0.23.1; [GHSA-2q4c-3mrw-63c3](https://github.com/kopia/kopia/security/advisories/GHSA-2q4c-3mrw-63c3) affects versions through v0.22.3.

### 2. Restic — control benchmark and possible subprocess driver

- **Project:** Restic (Go)
- **URL:** https://github.com/restic/restic
- **License:** BSD-2-Clause
- **Version or pin:** v0.19.1
- **Role in five seams:** `RepositoryDriver` (control benchmark and possible subprocess driver)
- **What to borrow:** Benchmark data (exact footprint, ingest, range reads, mount behavior, verification, restore, operating burden) and, if selected, the driver itself through a process boundary. Borrow chunk-offset lookup, blob caching, and concurrent offset-read patterns from the FUSE read path; retain [issue #3828](https://github.com/restic/restic/issues/3828) (mounted access 8x-25x slower than restore) as performance counterevidence ([open-source-adoption-and-code-borrowing.md:138](open-source-adoption-and-code-borrowing.md#36-design-and-competitor-reference-only)).
- **How to integrate:** Subprocess (CLI control) or direct dependency only after the spike; upstream explicitly treats Restic as a CLI, not a supported library.
- **Status:** Qualification pending
- **Notes:** Run the same corpus as the Kopia spike as the control ([open-source-adoption-and-code-borrowing.md:175](open-source-adoption-and-code-borrowing.md#4-kopia-qualification-spike)).

### 3. Borg — auxiliary benchmark and design reference

- **Project:** BorgBackup (Python/Cython)
- **URL:** https://github.com/borgbackup/borg
- **License:** BSD-3-Clause
- **Version or pin:** 1.4.5
- **Role in five seams:** `RepositoryDriver` (bounded local/SSH benchmark only)
- **What to borrow:** Calibration data for compression, maintenance, and mounted-read expectations. Its CLI boundary is less natural for RestoreWeave and repair may discard damaged data, so it is not a leading adapter ([open-source-adoption-and-code-borrowing.md:64](open-source-adoption-and-code-borrowing.md#32-qualification-candidates)).
- **How to integrate:** Subprocess benchmark harness, and design reference.
- **Status:** Qualification pending
- **Notes:** Use only where local or SSH comparisons add information ([open-source-adoption-and-code-borrowing.md:175](open-source-adoption-and-code-borrowing.md#4-kopia-qualification-spike)).

### 4. Plakar — new `RepositoryDriver` comparison and design source

- **Project:** Plakar (Go)
- **URL:** https://github.com/PlakarKorp/plakar
- **License:** ISC
- **Version or pin:** None pinned yet (tracking upstream `main` and releases)
- **Role in five seams:** `RepositoryDriver` (added qualification-spike comparison and possible same-language in-process library candidate); chunking/verification design reference
- **What to borrow:** Confirmed by external research on 2026-08-12: content-addressed deduplicating snapshots, the Kloset chunking engine, Web UI/API snapshot browsing, and verifiable-recovery claims. Borrow the verification and chunking designs as an in-language counterpoint to the Kopia/Restic spike, and evaluate Plakar itself as an in-process library candidate for `RepositoryDriver`.
- **How to integrate:** Design reference first; direct dependency (in-process Go library) only if it passes the same qualification gates as Kopia. Do not adopt Plakar's object identity, repository layout, or chunking parameters as RestoreWeave ABI.
- **Status:** New
- **Notes:** New entry added 2026-08-12. Because it is in the same language as RestoreWeave, it offers a lower-boundary alternative to a subprocess-driven engine; its claims (verifiability, Web browsing) must be proven against the same spike corpus, not taken from the README.

### 5. go-fuse — rejected product dependency

- **Project:** go-fuse (Go)
- **URL:** https://github.com/hanwen/go-fuse
- **Role:** Historical research reference only
- **Decision:** Do not adopt. RestoreWeave exposes export manifests and bounded file reads; operators choose external mount or sharing tools.
- **Status:** Rejected 2026-08-17

### 6. modernc.org/sqlite — bundled SQLite driver and FTS5 provider basis

- **Project:** modernc.org/sqlite (Go, pure-Go SQLite driver)
- **URL:** https://gitlab.com/cznic/sqlite
- **License:** MIT
- **Version or pin:** v1.55.0 (already in `go.mod`)
- **Role in five seams:** `IndexProvider` and `QueryProvider` (bundled SQLite FTS5 implementation for `RW-MVP-1`)
- **What to borrow:** The SQLite driver plus FTS5-backed lexical search. One physically separate disposable database per immutable `IndexGenerationRef`; the schema, row IDs, token tables, and query syntax are never durable RestoreWeave ABI.
- **How to integrate:** Direct dependency (already present in the Go module).
- **Status:** Frozen
- **Notes:** FTS5 availability is asserted during startup qualification. SQLite itself is public domain; wrappers, generated builds, and extensions are audited separately ([open-source-adoption-and-code-borrowing.md:49](open-source-adoption-and-code-borrowing.md#31-planned-direct-dependencies-and-bundled-implementation-choices), [open-source-adoption-and-code-borrowing.md:238-251](open-source-adoption-and-code-borrowing.md#7-sqlite-fts5-generation-model)).

### 7. file/libmagic — magic-byte evidence

- **Project:** file (the `file` command and libmagic library)
- **URL:** https://github.com/file/file
- **License:** LGPL-2.1 (libmagic; the `file` command itself is permissive BSD-style)
- **Version or pin:** Pin the compiled magic database digest at the adopted version
- **Role in five seams:** `Processor` evidence lane (default magic stage after host-owned suffix inspection)
- **What to borrow:** Deterministic byte signatures and magic rules as bounded evidence, not as parseability or exact-identity proof.
- **How to integrate:** Bundled native component; a tiny isolated helper is preferred where native-code containment is practical. Disable compressed inspection, decompressor forking, device access, symlink following, and unnecessary deep parsers.
- **Status:** Frozen
- **Notes:** Keep suffix rules host-owned; a suffix/magic disagreement is a visible classification conflict, not a silent override ([open-source-adoption-and-code-borrowing.md:14](open-source-adoption-and-code-borrowing.md#1-conclusion), [open-source-adoption-and-code-borrowing.md:51](open-source-adoption-and-code-borrowing.md#31-planned-direct-dependencies-and-bundled-implementation-choices)).

### 8. protobuf + grpc-go — Processor control plane over Unix sockets

- **Project:** Protocol Buffers and grpc-go
- **URL:** https://github.com/protocolbuffers/protobuf and https://github.com/grpc/grpc-go
- **License:** BSD-3-Clause (protobuf) / Apache-2.0 (grpc-go)
- **Version or pin:** Exact release pins follow the first cross-version conformance spike; not yet frozen
- **Role in five seams:** `Processor` control plane (protobuf schemas, gRPC over Unix-domain sockets)
- **What to borrow:** Control-plane schemas and local RPC as implementation plumbing, not yet a frozen public ABI. Pre-opened file descriptors carry large bytes; no large payloads inside messages.
- **How to integrate:** Direct dependency (library) inside the Processor host.
- **Status:** Frozen (as implementation plumbing)
- **Notes:** Exact release and transitive review remain release work ([open-source-adoption-and-code-borrowing.md:52](open-source-adoption-and-code-borrowing.md#31-planned-direct-dependencies-and-bundled-implementation-choices), [open-source-adoption-and-code-borrowing.md:213-223](open-source-adoption-and-code-borrowing.md#6-processor-runtime-and-isolation)).

### 9. MCP Go SDK — local read-only MCP

- **Project:** MCP Go SDK (modelcontextprotocol)
- **URL:** https://github.com/modelcontextprotocol/go-sdk
- **License:** MIT (after license transition; capture the exact applicable license at the selected revision)
- **Version or pin:** Pin one license-stable revision; not yet selected
- **Role in five seams:** Northbound inspection surface only (read-only MCP adapter); not storage, scheduling, or processor transport
- **What to borrow:** The SDK for the local read-only MCP adapter over the same typed operations as the CLI.
- **How to integrate:** Direct dependency (library).
- **Status:** Qualification pending
- **Notes:** The repository is transitioning between Apache and MIT terms; the selected revision must be complete and reproducible. MCP is never an internal bus ([open-source-adoption-and-code-borrowing.md:53](open-source-adoption-and-code-borrowing.md#31-planned-direct-dependencies-and-bundled-implementation-choices), [open-source-adoption-and-code-borrowing.md:149](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp)).

### 10. Perkeep — selective code borrowing

- **Project:** Perkeep (Go)
- **URL:** https://github.com/perkeep/perkeep
- **License:** Apache-2.0
- **Version or pin:** Commit [`9406ea272705d1f8f63c9b2ed31274962c30e3e1`](https://github.com/perkeep/perkeep/tree/9406ea272705d1f8f63c9b2ed31274962c30e3e1) (as referenced in the adoption document)
- **Role in five seams:** Selective code borrowing across storage and namespace seams
- **What to borrow:** Exactly three reviewed areas: [`pkg/blobserver`](https://github.com/perkeep/perkeep/tree/9406ea272705d1f8f63c9b2ed31274962c30e3e1/pkg/blobserver) for small storage interfaces and adapter conformance-test patterns; [`pkg/schema/filewriter.go`](https://github.com/perkeep/perkeep/blob/9406ea272705d1f8f63c9b2ed31274962c30e3e1/pkg/schema/filewriter.go) for chunk-tree construction ideas; [`pkg/schema/filereader.go`](https://github.com/perkeep/perkeep/blob/9406ea272705d1f8f63c9b2ed31274962c30e3e1/pkg/schema/filereader.go) for range reads and reconstruction patterns.
- **How to integrate:** Selective code copying with full provenance (source repo, commit, path, license, notices, local destination, substantive modifications, independent tests — [open-source-adoption-and-code-borrowing.md:126](open-source-adoption-and-code-borrowing.md#35-selective-code-borrowing)).
- **Status:** Frozen
- **Notes:** RestoreWeave does not inherit Perkeep's SHA-224 defaults, schema language, object identity, publication model, or recovery format. SHA-256 content identity, `RepositoryDriver` receipts, portable publication records, and catalog-independent recovery semantics remain RestoreWeave-owned ([open-source-adoption-and-code-borrowing.md:124](open-source-adoption-and-code-borrowing.md#35-selective-code-borrowing)).

### 11. pathrs / libpathrs — root-confined resolution

- **Project:** libpathrs (openSUSE)
- **URL:** https://github.com/openSUSE/libpathrs
- **License:** MPL-2.0
- **Version or pin:** `main` branch, `contrib/bindings/go/pathrs-lite`; no commit pinned yet
- **Role in five seams:** `CaptureDriver` (Linux root-confined capture; handle-oriented retained-root resolution)
- **What to borrow:** The handle APIs in pathrs-lite for retained-root, component-relative resolution with opportunistic Linux `openat2` (`RESOLVE_BENEATH`, `RESOLVE_NO_MAGICLINKS`, no-follow).
- **How to integrate:** Direct dependency (Go library) only after file-level MPL-2.0 review and behavior qualification; otherwise a narrow private `golang.org/x/sys/unix` `openat2` layer.
- **Status:** Qualification pending
- **Notes:** Review file-level copyleft obligations, confirm root-handle lifetime and fallback behavior, and use only the handle API ([open-source-adoption-and-code-borrowing.md:69](open-source-adoption-and-code-borrowing.md#32-qualification-candidates)). Never use the legacy string-returning `SecureJoin` API as a security boundary ([open-source-adoption-and-code-borrowing.md:153](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp)).

### 12. Homebutler — new restore-verification UX design reference

- **Project:** Homebutler
- **URL:** https://github.com/Higangssh/homebutler
- **License:** Not licensed for code adoption (design reference only)
- **Version or pin:** None
- **Role in five seams:** Design reference for restore-verification automation UX
- **What to borrow:** The concept of making restore verification an automated, operator-friendly workflow, confirmed by external research on 2026-08-12. This is UX and workflow vocabulary only; no code is copied or linked.
- **How to integrate:** Design reference (independent implementation).
- **Status:** New
- **Notes:** New entry added 2026-08-12. RestoreWeave's own verification semantics (immutable plans, `PUBLICATION_COMMIT`, readback evidence, exact restore check — [README.md:158](../../README.md)) remain authoritative; Homebutler informs how those facts are presented to an operator.

---

## Rejected or deferred for the MVP

| Project | Decision | Source |
| --- | --- | --- |
| [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin) | Do not use as the public Processor ABI | [open-source-adoption-and-code-borrowing.md:146](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp) |
| [Temporal](https://github.com/temporalio/temporal), [Apache NiFi](https://github.com/apache/nifi), OCI/containerd | Do not make them the mandatory execution substrate; reimplement only narrow storage-specific journal semantics | [open-source-adoption-and-code-borrowing.md:147](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp), [open-source-adoption-and-code-borrowing.md:142](open-source-adoption-and-code-borrowing.md#36-design-and-competitor-reference-only) |
| [Kubo](https://github.com/ipfs/kubo) | Do not embed for P2P; prefer narrow Boxo modules only after a P2P profile is activated | [open-source-adoption-and-code-borrowing.md:150](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp) |
| [Tantivy](https://github.com/quickwit-oss/tantivy), [LanceDB](https://github.com/lancedb/lancedb), [Qdrant](https://github.com/qdrant/qdrant), [OpenSearch](https://github.com/opensearch-project/OpenSearch), [Meilisearch](https://github.com/meilisearch/meilisearch), [FAISS](https://github.com/facebookresearch/faiss) | Do not add before SQLite FTS5 is measured against the MVP corpus | [open-source-adoption-and-code-borrowing.md:151](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp), [open-source-adoption-and-code-borrowing.md:103-110](open-source-adoption-and-code-borrowing.md#34-search-and-semantic-expansion) |
| [rustic](https://github.com/rustic-rs/rustic) | Track only; do not adopt as a production recovery engine while upstream discourages production use | [open-source-adoption-and-code-borrowing.md:152](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp), [open-source-adoption-and-code-borrowing.md:67](open-source-adoption-and-code-borrowing.md#32-qualification-candidates) |
| [bazil/fuse](https://github.com/bazil/fuse) | No decision-changing advantage; fallback is the actively maintained [jacobsa/fuse](https://github.com/jacobsa/fuse) only | [open-source-adoption-and-code-borrowing.md:155](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp) |
| [Watchman](https://github.com/facebook/watchman) | Do not make it the default watcher service; borrow recrawl/overflow/poison-state concepts only | [open-source-adoption-and-code-borrowing.md:154](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp), [open-source-adoption-and-code-borrowing.md:19](open-source-adoption-and-code-borrowing.md#1-conclusion) |
| [SecureJoin](https://github.com/cyphar/filepath-securejoin) (legacy string-returning API) | Rejected for authoritative capture; vulnerable to time-of-check/time-of-use attacks | [open-source-adoption-and-code-borrowing.md:153](open-source-adoption-and-code-borrowing.md#37-rejected-or-deferred-for-the-mvp), [open-source-adoption-and-code-borrowing.md:69](open-source-adoption-and-code-borrowing.md#32-qualification-candidates) |

Additional design-reference-only entries (no code adoption) with licensing cautions are recorded in [open-source-adoption-and-code-borrowing.md:128-142](open-source-adoption-and-code-borrowing.md#36-design-and-competitor-reference-only): Spacedrive (FSL-1.1-ALv2), Seafile (AGPL/GPL), sist2 (GPL-3.0), Immich (AGPL-3.0), rclone v1.75.0 (MIT; GHSA-45pq-889g-fcgh through v1.74.4), zrepl (MIT), and Watchman (MIT).

## Companion documents

- [Open-Source Adoption and Code Borrowing](open-source-adoption-and-code-borrowing.md) — full decisions, gates, and discipline.
- [Competitor and Component Research](competitor-research.md) — product positioning and mechanism survey behind the adoption choices.
