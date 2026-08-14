# Build Stack and Architecture Selection (RW-MVP-1)

## 1. Purpose, scope, and status

This document records the concrete build-stack and package-structure decisions for the `RW-MVP-1` vertical slice: a read-only managed-archive and search profile for a self-hosted NAS data layer. It is the implementation-oriented companion to the [NAS Vertical Slice Implementation Plan](nas-vertical-slice-implementation-plan.md) and the [Open-Source Adoption and Code Borrowing](../references/open-source-adoption-and-code-borrowing.md) audit.

Scope:

- Confirm the kernel/userland split and the five active extension seams plus the reserved `RetrieverDriver` (system-architecture.md:56-63).
- Freeze or explicitly defer every component choice that the plan milestones need (repository engine, FTS5 search, FUSE adapter, Processor control plane, MCP, file identification, CLI framework).
- Propose the Go package layout that implements the seams without exporting a public ABI.
- Define the concurrency, lifecycle, and storage-topology model the controller will use.
- Record the key decisions in ADR style so they can be revisited with their rationale.

Status: **informative implementation guidance**. Normative authority remains with the requirement documents ([Core Kernel and Interface](../requirements/core-kernel-and-interface.md), [Driver and Processor Interfaces](../requirements/driver-and-processor-interfaces.md), [System Architecture](../requirements/system-architecture.md)). A row marked **frozen** here is frozen because a normative document or the adoption audit already froze it; a row marked **qualification spike pending** or **open** requires its stated gate before the release binary may depend on it. `go vet` and `-race` test runs must stay clean on every merge; the current tree already keeps a single-module layout (`go.mod:1-5`).

## 2. Architectural decision overview

### 2.1 Kernel and userland are confirmed

RestoreWeave is built as a small authoritative kernel plus replaceable, versioned userland. The kernel owns identities, accepted decisions, provenance, transactions, verification meaning, namespace meaning, and recovery semantics (system-architecture.md:38-52; core-kernel-and-interface.md:36-48). The userland is exactly the seam set declared in system-architecture.md:56-63 and driver-and-processor-interfaces.md:58-68:

- `CaptureDriver`
- capability-oriented `Processor`
- `RepositoryDriver`
- `IndexProvider`
- `QueryProvider`
- a later reserved `RetrieverDriver`

Conceptual `Processor` roles (`CLASSIFY_LEARNED`, `PARSE`, `EXTRACT`, `ENRICH`, `FINGERPRINT`, `TRANSFORM`, `VALIDATE`, `INDEX_PREPARE`) are capability roles inside one interface, not separate public wire ABIs (system-architecture.md:219-232). Presentation gateways (FUSE, CLI, MCP, later REST/WebUI) are northbound adapters, not storage-algorithm seams (system-architecture.md:69-72).

The kernel is not an empty framework: the reference distribution must ship a complete default pipeline that ingests, deduplicates, compresses, places, verifies, searches, browses, and restores without any optional plugin, model, or WebUI (system-architecture.md:76-78; core-kernel-and-interface.md:81-83).

### 2.2 Authoritative versus replaceable

| Area | Kernel-owned (authoritative, durable) | Replaceable (userland, versioned) |
| --- | --- | --- |
| Identity | `SourceId`, `SourceViewId`, `NamespaceEntryId`, `FileVersionId`, `ContentId`, `ChunkId`, `RepresentationId`, `SnapshotId`, `PlacementId`, `IndexGenerationId`, `OperationId`, `PlanId`, `DecisionId` (system-architecture.md:148-166; core-kernel-and-interface.md:89-106). Database row IDs and repository-private object names are never canonical (core-kernel-and-interface.md:108) | Repository-private chunk, pack, and object naming (backend-owned) |
| Plans and policy | Immutable accepted plans, decisions, and fidelity choices (core-kernel-and-interface.md:123-135) | Planning heuristics; estimation models |
| Publication and recovery | RRF roots, `PREPARED_CLOSURE`, `PUBLICATION_COMMIT`, verification gates, clean-recovery meaning (system-architecture.md:300-302; core-protocol-and-reference-userland.md:366-383) | Repacking, recompression, re-encryption, transport |
| Transactions | Durable journal, idempotency, cancellation, leases, fencing, checkpoints, reconciliation (system-architecture.md:46) | Worker topology, queues, IPC (core-kernel-and-interface.md:175) |
| Namespace and access | `SnapshotTree` and `FileAccess` semantics and the empty-selector rule ("authoritative exact", never "something similar") (system-architecture.md:306-317) | Decoders, repository read adapters, gateway protocol bindings |
| Classification | Evidence ledger and the accepted classification decision; suffix and magic-byte stages are host-owned (system-architecture.md:232-246) | Learned classifiers, parsers, extractors |
| Search semantics | The stable query contract over logical subjects; generation-pinned queries; broker reauthorization (system-architecture.md:321-335) | The search store itself: schema, tokenizer, ranking, engine |

The stable read narrow waist already exists in code: `SnapshotTree` and `FileAccess` in `internal/readsvc/service.go:13-45`, the host-only policy seams in `internal/readsvc/service.go:81-102`, and the bounded adapter ports (`RepositoryReadAdapter`, `StorageRangeReader`, `RepresentationDecoder`, `NamespaceGatewayAdapter`) in `internal/readsvc/ports.go:66-181`. New engine, decoder, and gateway code must plug into these ports, never bypass them.

## 3. Technology selection matrix

Legend for the status column:

- **Frozen** — already decided by a normative document or the adoption audit; no further gate except the stated pin.
- **Qualification spike pending** — a leading choice that still has a decision-changing gate; nothing may ship until the gate passes.
- **Open / suggested** — explicitly not yet decided; the recommendation in this document is a proposal for review.

| Component | Selection | Rationale | Status |
| --- | --- | --- | --- |
| Language and toolchain | Go 1.26.0, single module (`go.mod:1-5`); `go vet` clean; `-race` enabled in the default test invocation; only `modernc.org/sqlite` as a direct dependency today | Go is the language of the existing tree (`internal/scanner`, `internal/readsvc`, `internal/store/sqlite`) and of the leading repository candidate (Kopia) and the added Go-native control (Plakar). One module keeps `internal/` private, which is the compatibility contract (core-kernel-and-interface.md:173). Race-detector tests are mandatory because the controller is concurrent (see section 5) | **Frozen** |
| SQLite and FTS5 | `modernc.org/sqlite v1.55.0` (already the sole direct dependency, `go.mod:5`) as the bundled `IndexProvider` and `QueryProvider`; one physically separate disposable database per immutable `IndexGenerationRef`; schema, row IDs, tokenizer tables, and query syntax are private (adoption doc §7, lines 238-251) | Pure-Go, CGO-free, STRICT-table SQLite is already proven in the operational catalog (`internal/store/sqlite/migrations.go`, 16 `STRICT` tables plus `schema_migrations`). FTS5 gives useful lexical search with zero new runtime. Generation-per-database gives atomic activation, bounded rollback, and cheap deletion (nas-vertical-slice-implementation-plan.md:186-191) | **Frozen** |
| Repository engine | **Kopia v0.23.1** as the leading `RepositoryDriver` candidate via library integration; **Restic v0.19.1** as the control benchmark and possible subprocess driver; **Borg 1.4.5** as a bounded local/SSH auxiliary; **Plakar** (Go, ISC) added as a second Go-native control and in-process library candidate. No engine is adopted until the qualification spike passes (adoption doc lines 62-64, 159-175; nas-vertical-slice-implementation-plan.md:164) | Kopia leads because its Go repository APIs, object reads, storage abstractions, and verification fit `RepositoryDriver` better than a CLI-only engine. Restic is a documented CLI, not a supported library. Borg is the least natural adapter but a useful compression/maintenance calibration. Plakar is the new Go-native contrast: its Kloset chunker and verify-oriented design give an in-process, permissive-license (ISC) control against Kopia. Selection is by observed correctness and operating cost, not stars or nominal ratios | **Qualification spike pending** |
| Read-only FUSE | `github.com/hanwen/go-fuse/v2` v2.11.0, pin commit `423b377`, kept private behind `SnapshotTree`/`FileAccess` (adoption doc line 50; nas-vertical-slice-implementation-plan.md:197-201) | Tagged release, demonstrated Kopia/rclone use, resource and invalidation controls, BSD-style license. Kernel-enforced `ro,nodev,nosuid,noexec`, `allow_other` disabled, every mutation opcode returns `EROFS` (adoption doc lines 253-276) | **Frozen** (pin); adapter behavior still subject to the qualification gates in the adoption audit |
| Processor control plane | Protobuf schemas + gRPC over a Unix-domain socket; large bytes travel through pre-opened file descriptors, never inline in messages; bubblewrap, namespaces, seccomp, `no_new_privs`, rlimits, cgroup v2, allowlisted environment, no network by default (adoption doc §6, lines 211-236; nas-vertical-slice-implementation-plan.md:175-176) | This is reference plumbing, not a public wire ABI: the semantic contract stays typed operations, handles, result envelopes, and artifact state. Out-of-process isolation is required for untrusted parsers (system-architecture.md:354-365) | **Frozen** as private plumbing; the wire contract remains experimental until cross-version conformance |
| MCP | Local, read-only MCP adapter over the same typed command dispatcher, using the official MCP Go SDK after pinning one license-stable revision (adoption doc line 53; core-kernel-and-interface.md:167-168) | MCP is a northbound presentation adapter, never an internal bus, scheduler, or data plane (adoption doc line 149). It binds the bounded read-only subset (status, search, namespace, content) | **Frozen** as the adapter approach; the exact SDK revision is **open until the license-stable pin** |
| File identification | Host-owned suffix rules, then bounded `file`/libmagic magic-byte evidence; magic database digest pinned; compressed-inspection and decompressor forking disabled; optional Siegfried for ambiguity/preservation depth (adoption doc lines 14, 51, 65) | Two-step evidence is a normative requirement: suffix and magic evidence must remain independently visible and auditable (system-architecture.md:232-246; core-kernel-and-interface.md:373-386) | **Frozen** (adopt and bundle); the magic database digest pin is release work |
| CLI framework | **cobra (`spf13/cobra`, Apache-2.0)** — see section 3.1 for the full reasoning and guardrails | The CLI command families are few but must stay perfectly stable in name, flags, exit codes, and JSON/JSONL output (core-kernel-and-interface.md:269-289). A closed cobra tree maps 1:1 onto the Core Command ABI families | **Open / suggested** (recommendation in section 3.1; review before freezing) |

### 3.1 CLI framework recommendation (open item)

The requirement is a **small, stable command family set**, not a large user-extensible surface (core-kernel-and-interface.md:269-289: `plan.*`, `status.get`, `job.*`, `snapshot.*`, `namespace.*`, `representation.*`, `content.*`, `search.query`, `recovery.export`, `capability.list`). Recommendation: **`spf13/cobra`** (with `pflag`), not a hand-rolled `flag`-based dispatcher.

Rationale:

1. **1:1 mapping onto the Core Command ABI.** Cobra's static tree makes every ABI command a first-class, documented object (`plan ingest`, `snapshot list`, `search query`...). The registry stays closed — no plugin registration, no `run_tool` escape hatch (core-protocol-and-reference-userland.md:225; core-kernel-and-interface.md:290).
2. **Stable flag conventions.** pflag's GNU-style parsing (interspersed flags, `--flag value`, repeatable flags, persistent flags per family) gives deterministic, scriptable command lines for the stable JSON/JSONL binding (core-kernel-and-interface.md:167). Hand-rolled `flag` parsing drifts easily across ten families.
3. **Uniform help, validation, and exit-code behavior.** Cobra centralizes usage generation and error wrapping; a single exit-code table and a single renderer (human vs `--json`/`--jsonl`) are enforced in one place, which directly serves "CLI, MCP, and later UI adapters bind the same command and authority semantics" (system-architecture.md:405).
4. **Domain precedent.** Kopia (our leading repository candidate) uses cobra; rclone and the wider backup/mount ecosystem provide battle-tested patterns for exactly this command shape.
5. **License.** Apache-2.0 is an accepted in-process license class (adoption doc §9, lines 278-297).

Guardrails if cobra is accepted:

- The command tree is code, but it is *closed*: adding a command requires a code review that changes the ABI surface; no runtime registration.
- No cobra type, flag string, or usage text enters the Core Command ABI schemas or MCP tools; the dispatcher (`internal/command`) stays framework-free.
- Human-readable text is evolvable (core-kernel-and-interface.md:172); the JSON/JSONL binding is stable and lives in one rendering layer.

Fallback position: if dependency discipline ultimately outweighs ergonomics, stdlib `flag` with a small manual subcommand table is acceptable because the family count is genuinely small — but it costs uniform help, completion, and flag consistency. The recommendation stands as **cobra** unless review finds a concrete cost.

## 4. Package layout proposal

### 4.1 Principles

- Everything under `internal/` is an unstable implementation detail (core-kernel-and-interface.md:173); this layout is a direction, not a public package promise (core-protocol-and-reference-userland.md:528).
- Package boundaries follow the seams: host packages own the seam contracts; adapter packages implement them. No adapter package may bypass the host package for a seam (mirrors the "no integration may import `internal/` or read RestoreWeave SQLite" rule at core-protocol-and-reference-userland.md:98, applied inward).
- The existing packages stay where they are and are reused: `internal/scanner`, `internal/readsvc`, `internal/store/sqlite`. `internal/plugin` is legacy and retires (section 4.3).

### 4.2 Proposed tree

```
cmd/restoreweave/            CLI, controller, and recovery entry points; composition root only
internal/app/                composition root: wires config, store, journal, seams (from core-protocol-and-reference-userland.md:497)
internal/command/            typed Core Command dispatch and authorization (core-protocol-and-reference-userland.md:200-228)
internal/controller/         plan/apply/job/publication orchestration; the single writer of operations
internal/planner/            deterministic ingest, restore, and lifecycle plans
internal/operation/          journal reducers, leases, fencing tokens, reconciliation
internal/journal/            append-only durable operation log (states per core-protocol-and-reference-userland.md:285-316)
internal/rrf/                portable RRF records, roots, signatures, export
internal/identity/           content, representation, namespace, and snapshot IDs
internal/capture/            CaptureDriver host; the scanner becomes one profile behind it
internal/scanner/            existing deterministic scanner (kept; evidence source until capture hardening)
internal/identify/           host-owned Detector: suffix evidence + magic-byte evidence (ported from internal/plugin/builtin_detector)
internal/process/            Processor host: routes, staging, sealing, sandbox supervisor, result validation
internal/repository/         RepositoryDriver seam: contract types + engine adapters (kopia, later plakar/restic controls)
internal/readsvc/            existing stable read seam — SnapshotTree, FileAccess, adapter ports (unchanged)
internal/publication/        portable publication protocol (PAYLOAD / PREPARED_CLOSURE / PUBLICATION_COMMIT)
internal/namespace/          SnapshotTree implementation over the repository driver
internal/verification/       evidence validation and acceptance
internal/lifecycle/          retention, retirement, reachability, and gated GC plans
internal/indexfeed/          replayable authorized feed and IndexProvider coordination
internal/search/             bundled FTS5 IndexProvider + QueryProvider (one disposable DB per generation)
internal/query/              host query broker, generation pinning, result reauthorization
internal/annotations/        durable tag/note CRUD + portable export/import
internal/gateway/fuse/       bundled read-only Linux FUSE adapter (go-fuse v2.11.0, pin 423b377)
internal/mcp/                local read-only MCP adapter (stdio) over the command dispatcher
internal/store/sqlite/       operational catalog (existing; leases, idempotency, annotations tables grow here)
internal/plugin/             LEGACY — frozen; retired at Milestone 0 (section 4.3)
spec/core/v1/                public command and event schemas (from core-protocol-and-reference-userland.md:522)
spec/rrf/v1/                 RRF schemas and canonicalization vectors
spec/extensions/             qualified external-process protocols
testdata/conformance/        cross-binding and clean-recovery fixtures
```

### 4.3 Relation to existing packages and the reference layout

| Proposed package | Replaces or maps to (core-protocol-and-reference-userland.md:496-526) | Relationship to current tree |
| --- | --- | --- |
| `internal/controller/` | `internal/command` + `internal/planner` + `internal/operation` coordination | New; owns the plan/apply job loop |
| `internal/identify/` | (new host-owned baseline stage) | **Ports** the suffix/magic logic out of `internal/plugin/builtin_detector.go`; suffix+magic are host-owned, so they must not live in a retiring plugin package |
| `internal/repository/` | `internal/repository` (Driver host) | New; the seam types plus the Kopia adapter and the read-adapter wiring into `internal/readsvc/ports.go:66-70` |
| `internal/search/` | `internal/indexfeed` + `internal/query` provider impls | New; FTS5 generation builder + query executor (adoption doc §7, lines 238-251) |
| `internal/gateway/fuse/` | `internal/binding/fuse` | New; thin go-fuse adapter over `SnapshotTree`/`FileAccess` (readsvc/service.go:13-45) |
| `internal/mcp/` | `internal/binding/mcp` | New; stdio read-only adapter |
| `internal/annotations/` | (new) | New; durable whole-subject tag/note records with revision and tombstone provenance (core-kernel-and-interface.md:498-504) |
| `internal/scanner/` | `internal/capture` (eventually) | Kept as-is; remains non-authoritative evidence until retained-root capture is qualified (nas-vertical-slice-implementation-plan.md:18-21, 161-163) |
| `internal/readsvc/` | `internal/readsvc` | Unchanged; the stable read seam already implements the required contracts |
| `internal/store/sqlite/` | `internal/store/sqlite` | Unchanged; grows job-lease/idempotency usage and annotation tables |
| `internal/plugin/` | — | Retired (below) |

### 4.4 Retirement of the legacy `internal/plugin`

The legacy plugin prototype is not the implementation target: it exposes historical categories, mixes representation concerns, and predates the route, artifact, provenance, resource, and decoder contracts (nas-vertical-slice-implementation-plan.md:23-25). Retirement path (Milestone 0, nas-vertical-slice-implementation-plan.md:150-157):

1. **Freeze** — no new public behavior in its historical categories; no new callers.
2. **Port the evidence code** — `builtin_detector.go` (suffix/magic evidence) moves to `internal/identify` because suffix and magic stages are host-owned (system-architecture.md:232); the result is the same detector, owned by the kernel instead of a plugin.
3. **Delete once unreferenced** — after the milestone-0 exit condition ("the new internal model represents exactly the approved seams and Processor roles without importing legacy category or transformation enums") holds and no import of `internal/plugin` remains, delete the package. External execution is already disabled, so no ABI must be preserved (nas-vertical-slice-implementation-plan.md:25).

## 5. Concurrency and lifecycle model

### 5.1 One controller, one domain model

The reference deployment runs a long-lived local controller for scheduling, index feeds, and concurrent clients; the same core may also be composed in-process for offline recovery or single-command maintenance (core-protocol-and-reference-userland.md:145). The controller is the single writer of durable operations; adapters (CLI, MCP, later REST) only submit commands through `internal/command`, which enforces versioning, authorization, idempotency, and the closed registry (core-protocol-and-reference-userland.md:200-228).

### 5.2 Job leases, fencing, and idempotent transactions

The operation journal (core-protocol-and-reference-userland.md:285-316) is the state machine: `QUEUED -> RUNNING -> WAITING_EXTERNAL -> RECONCILING -> CANCELLING`, terminating in `SUCCEEDED`, `DEGRADED`, `BLOCKED`, `FAILED`, `CANCELLED`, or `UNKNOWN_EXTERNAL_OUTCOME`. The SQLite operational catalog already provides the two primitives this model needs:

- `Store.Idempotent` (`internal/store/sqlite/store.go:200`) records the callback's response in the same write transaction as the request, replaying identical requests and rejecting reuse with different input (`ErrIdempotencyConflict`). Every mutating command binds an idempotency key before the first external effect (core-protocol-and-reference-userland.md:227).
- `Tx.AcquireJobLease` (`internal/store/sqlite/records.go:697`) hands out a lease plus a fencing token per job row. Fencing covers staging allocation and seal, artifact admission, derivative-preference changes, and index-feed publication (driver-and-processor-interfaces.md:449): a stale worker whose fence has been replaced cannot seal or admit anything.

Crash semantics: the core never infers success from a missing response. If a driver cannot prove whether a placement, publication, or deletion committed, the job enters reconciliation and may terminate as `UNKNOWN_EXTERNAL_OUTCOME` (core-protocol-and-reference-userland.md:316). Reconciliation must be idempotent — this is exactly what the repository qualification spike tests (adoption doc lines 166).

### 5.3 The exact lane never waits for optional processing

Resource separation is normative (driver-and-processor-interfaces.md:445): optional processing is physically isolated from the exact and read paths, with reserved CPU, memory, file descriptors, and staging capacity for exact hashing, placement, browse, verification, and restore.

Consequences for the concurrency model:

- **Repository uploads do not block processors.** The mandatory exact lane (hash -> `RepositoryDriver` placement -> readback -> portable publication) runs as its own job path (system-architecture.md:250-254). Processor results are staged, sealed, host-digested, validated, and *admitted* independently; a processor that crashes, times out, or saturates its pool changes nothing about the exact lane (nas-vertical-slice-implementation-plan.md:181).
- Processor queues use per-capability concurrency ceilings, retry ceilings, crash-loop circuit breakers, automatic quarantine, and dead-lettered subjects; exhausting a processor pool must not exhaust the exact or interactive-read pool (driver-and-processor-interfaces.md:445).
- Index generation is built asynchronously from the replayable feed and activated atomically (adoption doc §7, lines 244-251); publication does not wait for it (system-architecture.md:261).

### 5.4 Recommended worker model for the MVP

| Worker | Runs in | Lifetime | Owns |
| --- | --- | --- | --- |
| Command dispatcher | controller process | long-lived | dispatch, authorization, idempotency |
| Journal reducers | controller process | long-lived | leases, fencing, reconciliation |
| Exact-lane workers | controller process (bounded pool) | job-scoped | hashing, placement, readback, publication |
| Processor supervisor | controller process | long-lived | spawns/recycles sandboxed children |
| Processor child | sandboxed process | one invocation (recycled after crash) | the single `RUN_STAGE`/`DECODE_REPRESENTATION` operation |
| Index builder | controller process | generation-scoped | replay feed -> disposable FTS5 DB |
| FUSE gateway | controller process or sidecar | mount-scoped | one pinned snapshot view (readsvc/service.go:13-45) |

## 6. Storage and layout

Two distinct storage domains exist, and their roles must not blur (system-architecture.md:168, 302; core-kernel-and-interface.md:149):

1. **SQLite operational catalog** (`internal/store/sqlite`) — workspaces, sources, scan generations, observations, detection evidence, plans, jobs, audit events, idempotency records, namespace roots/entries, representations, file versions, content extents, physical-locator projections, engine read refs (16 `STRICT` tables plus `schema_migrations` in `migrations.go`). It is a **rebuildable projection**: losing it must not make a committed exact snapshot undecodable. It is never the recovery authority.
2. **The repository** — the authoritative, content-addressed payload store plus the portable RRF records placed through the three canonical roles: `PAYLOAD`, `RECOVERY_CLOSURE/PREPARED_CLOSURE`, and `RECOVERY_CLOSURE/PUBLICATION_COMMIT` (system-architecture.md:300; core-protocol-and-reference-userland.md:353-361). A snapshot is logically published only after the publication-marker placement is reconciled (core-protocol-and-reference-userland.md:366-383).

Search state is a third, even more disposable domain:

- **One physically separate disposable FTS5 database per immutable `IndexGenerationRef`** (adoption doc §7, lines 238-251; nas-vertical-slice-implementation-plan.md:186-187). New generations are built beside the active one, validated, atomically activated, and the prior database retained only for bounded rollback. FTS row IDs, tokenizer tables, and query syntax are never durable ABI (adoption doc line 251; core-kernel-and-interface.md:108).
- Durable whole-subject tags and notes live **outside** index state, as versioned records with authorship, visibility, revision, and tombstone provenance, with portable export/import (core-kernel-and-interface.md:498-504). They enter the portable closure per policy and must survive deleting every index database.

Recovery truth: clean recovery begins from a valid publication marker, authenticates the bound prepared closure and payload placements, and ignores orphan payloads, local pointers, and uncommitted closures (system-architecture.md:302). Nothing in the SQLite catalog or the FTS databases may become required reading for recovery (CKI-AC-15, core-kernel-and-interface.md:634).

## 7. Borrowing and adoption map

This table maps every meaningful upstream into the seam it serves and the exact borrow point. "Borrow" follows the adoption-audit categories: planned direct dependency, isolated processor, qualification candidate, selective code borrowing, or design reference (adoption doc §2, lines 31-41). Per the audit, borrows preserve RestoreWeave identity and recovery formats (adoption doc line 124).

| Upstream | Seam / package | Borrow point and role | License / status |
| --- | --- | --- | --- |
| **Kopia v0.23.1** | `internal/repository` (RepositoryDriver) | **Direct-dependency candidate**: Go repository APIs, object reads, storage abstractions, verification. Gate: the §4 qualification spike — root/GC safety, crash reconciliation, bounded/random reads, independent SHA-256 readback, catalog-independent recovery, corruption behavior, maintenance compatibility, NAS performance (adoption doc lines 159-175). Kopia's own FUSE adapter is a thin go-fuse structure reference and the `READDIRPLUS` regression (issue #1135) is a retained test case (adoption doc line 137) | Apache-2.0; **qualification spike pending**; GHSA-2q4c-3mrw-63c3 affects versions through v0.22.3, hence the v0.23.1 floor |
| **Plakar** (`PlakarKorp/plakar`, Kloset + ptar) | `internal/repository` (RepositoryDriver) | **New Go-native control** added alongside Restic/Borg: Kloset chunking/verification design for dedup-profile comparison; verify-oriented recovery semantics as counterevidence; in-process library candidate against Kopia's library integration. ISC license permits in-process use after review, but it ships only if the same spike gates pass (identity-safe GC, crash reconciliation, bounded reads, catalog-independent recovery) | ISC; **qualification spike pending**; exact release pin + full dependency review are spike prerequisites |
| **Perkeep** | `internal/repository` test suite; `FileAccess` read path | **Selective code borrowing** (Apache-2.0): `pkg/blobserver` for small storage interfaces and adapter conformance-test patterns; `pkg/schema/filewriter.go` for chunk-tree construction; `pkg/schema/filereader.go` for range reads and reconstruction (adoption doc lines 114-126). Do not inherit SHA-224 defaults, object identity, or publication model | Apache-2.0; every borrowed fragment records source, commit, license, and independent tests (adoption doc line 126) |
| **go-fuse v2.11.0** (`423b377`) | `internal/gateway/fuse` | **Planned direct dependency**: read-only adapter over `SnapshotTree`/`FileAccess`. Requirements: one mount = one principal + one export root + one immutable snapshot; `ro,nodev,nosuid,noexec`; `allow_other` disabled; `EROFS` for every write-capable open and mutation opcode; cookie-to-`PageToken` translation without replay (adoption doc lines 253-276; nas-vertical-slice-implementation-plan.md:197-201) | BSD-style; **frozen pin**, behavior qualification pending |
| Restic v0.19.1 | `internal/repository` (control) | Control benchmark and possible subprocess driver; chunk-offset lookup, blob caching, concurrent offset reads as read-path patterns; issue #3828 as performance counterevidence (adoption doc lines 63, 138) | BSD-2-Clause; subprocess boundary |
| Borg 1.4.5 | `internal/repository` (auxiliary control) | Bounded local/SSH compression, maintenance, and mounted-read calibration; not the leading adapter (adoption doc line 64) | BSD-3-Clause; subprocess boundary |
| rclone v1.75.0 (`mount2`, `vfs`) | `internal/gateway/fuse` qualification | ReadAt handle style, chunk-growth, read-ahead, transfer accounting, retries, metrics, `READDIRPLUS` vocabulary (adoption doc line 139) | MIT; design reference only |
| Watchman / zrepl | `internal/capture`, `internal/lifecycle` | Recrawl/overflow/poison-state concepts for lossy observation; ZFS snapshot lifecycle and holds as test patterns (adoption doc lines 140-141) | MIT; design reference only |

## 8. Decision records (ADR style)

### ADR-001: Repository engine — library integration preferred over subprocess; selection gated by the spike

- **Status:** Proposed; qualification spike pending.
- **Context:** `RepositoryDriver` must place, read, verify, reconcile, and restore admitted representations (driver-and-processor-interfaces.md:325-341). Kopia exposes Go repository APIs; Restic is a documented CLI; Borg is CLI-native; Plakar is a Go library with a verify-oriented design.
- **Decision:** Prefer **in-process library integration (Kopia)** so receipts, bounded reads, and reconciliation are typed Go calls under the host's control. Keep Restic as the subprocess control and Plakar as a second Go-native library control. **No engine enters the release binary until the spike passes** (adoption doc lines 159-175): root/GC safety for RestoreWeave's portable objects is the first and most decision-changing gate.
- **Consequences:** If Kopia's snapshot-centric GC cannot root RestoreWeave objects safely, Plakar becomes the in-process alternative and Restic the subprocess fallback. Library integration means `internal/repository` must pin the exact engine version and hide all engine types behind the seam; no Kopia/Plakar type may reach portable records.

### ADR-002: SQLite FTS5 first; semantic search deferred

- **Status:** Accepted (frozen by adoption audit and milestone 3).
- **Context:** Baseline discovery must cover path, filename, type, metadata, checksum, duplicates, tags/notes, processing state, and extracted text (driver-and-processor-interfaces.md:506). Vector/semantic search is a later profile.
- **Decision:** SQLite FTS5 is the bundled `RW-MVP-1` `IndexProvider`/`QueryProvider`, one disposable physical database per immutable `IndexGenerationRef`, schema kept private (adoption doc lines 16, 49, 238-251). No Tantivy/LanceDB/Qdrant/OpenSearch/Meilisearch/FAISS before the FTS5 ceiling is measured (adoption doc line 151).
- **Consequences:** Deleting every index degrades search only; namespace access, tags/notes, exact reads, verification, and restore stay intact, and a new generation rebuilds from durable records (nas-vertical-slice-implementation-plan.md:191).

### ADR-003: Bundled FUSE is strictly read-only; writes are refused at the kernel boundary

- **Status:** Accepted (frozen).
- **Context:** The reference distribution bundles a read-only Linux FUSE view over the original-path namespace (system-architecture.md:319). Writable gateways are later adapters and never receive repository-private access.
- **Decision:** go-fuse v2.11.0 pinned at `423b377`, one mount bound to one principal, one export root, one immutable snapshot; require `ro,nodev,nosuid,noexec`, disable `allow_other`, fail the mount if the policy cannot be confirmed, and return `EROFS` for every write-capable open and mutation opcode before any effect (adoption doc lines 253-276). Kernel-enforced read-only, not omission of mutation methods (adoption doc line 255).
- **Consequences:** If kernel caching makes authorization revocation unenforceable within the declared bound, the mount is documented as a local-trust surface (adoption doc line 272). The FUSE adapter remains replaceable presentation without touching `SnapshotTree`/`FileAccess` (driver-and-processor-interfaces.md:528).

### ADR-004: Retire the legacy `internal/plugin` prototype

- **Status:** Accepted (frozen; Milestone 0 work).
- **Context:** The prototype exposes historical categories, mixes representation concerns, and lacks the current route, artifact, provenance, resource, and decoder contracts; external execution is disabled (nas-vertical-slice-implementation-plan.md:23-25).
- **Decision:** Freeze the package, add no new behavior, port `builtin_detector` (suffix/magic evidence) into the host-owned `internal/identify` package, and delete the package once unreferenced. The new internal model must represent exactly the approved seams and Processor roles without importing legacy category or transformation enums (nas-vertical-slice-implementation-plan.md:150-157).
- **Consequences:** No legacy manifest or category enum leaks into plans, routes, or RRF. The milestone-0 exit condition is a hard gate before milestone 1.

### ADR-005: CLI command families are a frozen, closed set

- **Status:** Accepted (frozen).
- **Context:** The Core Command ABI is a stable semantic contract after qualification; the registry must not accept arbitrary SQL, shell execution, plugin invocation, prompt execution, or a generic `run_tool` (core-kernel-and-interface.md:269-290).
- **Decision:** The CLI exposes exactly the ABI families (`plan.*`, `status.get`, `job.*`, `snapshot.*`, `namespace.*`, `representation.*`, `content.*`, `search.query`, `recovery.export`, `capability.list`) with stable names, exit codes, and JSON/JSONL output. New families require an ABI version change. The command tree is closed; adapters share one dispatcher (core-protocol-and-reference-userland.md:225).
- **Consequences:** CLI, read-only MCP, and any later REST/WebUI adapter produce equivalent commands, authority checks, results, and events (CKI-AC-21, core-kernel-and-interface.md:641). The CLI framework choice (section 3.1) does not affect this contract.

### ADR-006: One physically separate SQLite database per index generation

- **Status:** Accepted (frozen).
- **Context:** Index generations must be buildable beside the active one, atomically activated, and deletable under ordinary derived-data retention without rewriting history (driver-and-processor-interfaces.md:422-426).
- **Decision:** Every `IndexGenerationRef` is backed by its own disposable FTS5 database created from the replayable authorized feed; validation covers record counts, known-query fixtures, subject resolution, stale-state reporting, and authorization before atomic activation; the prior database is retained for bounded rollback (adoption doc §7, lines 238-251).
- **Consequences:** FTS row IDs are never external identity; search-store schema stays a rebuildable implementation detail (core-kernel-and-interface.md:177).

### ADR-007: Processor control plane — protobuf + gRPC over Unix socket with FD passing and host sandboxing

- **Status:** Accepted as private plumbing; wire contract remains experimental.
- **Context:** Untrusted parsers and models must run isolated (system-architecture.md:354-365); large bytes must not travel inline in control messages (system-architecture.md:144).
- **Decision:** Protobuf schemas, grpc-go over a Unix-domain socket, pre-opened input/output file descriptors for large bytes, bubblewrap, namespaces, seccomp, `no_new_privs`, rlimits, cgroup v2, allowlisted environment, no network by default, attempt-fenced host-owned staging (adoption doc §6, lines 211-236; nas-vertical-slice-implementation-plan.md:175-176). The semantic contract stays typed operations, handles, result envelopes, and artifact state; the wire encoding is experimental until cross-version conformance (core-protocol-and-reference-userland.md:81-87).
- **Consequences:** The first processor is deliberately small (bounded text/metadata extraction) and proves the protocol without embedding a heavy runtime in the core (nas-vertical-slice-implementation-plan.md:179). Processor saturation cannot stop exact hashing, placement, browse, readback, verification, or restore (nas-vertical-slice-implementation-plan.md:181).

## 9. Open gates before the release binary

| Gate | Blocks | Owner of the evidence |
| --- | --- | --- |
| Repository qualification spike (Kopia v0.23.1 with Restic/Plakar controls) — adoption doc §4 | the first release `RepositoryDriver` | `internal/repository` spike harness |
| MCP Go SDK revision with completed, reproducible license state | the MCP adapter pin | license/supply-chain review (adoption doc §9) |
| CLI framework review (cobra recommendation) | freezing the CLI binding | this document, section 3.1 |
| libmagic database digest pin | the bundled magic stage | release inventory (adoption doc §9) |
| FUSE behavior qualification (caches, revocation, `EROFS`, read amplification) | calling the adapter NAS-usable | `internal/gateway/fuse` qualification |
| Processor sandbox + first deterministic processor conformance | milestone 2 exit | `internal/process` |
| Capture hardening (retained-root, `openat2`/equivalent) | scanner output becoming authoritative | `internal/capture` / `internal/scanner` |

## 10. References

- [System Architecture](../requirements/system-architecture.md)
- [Core Kernel and Interface Requirements](../requirements/core-kernel-and-interface.md)
- [Driver and Processor Interfaces](../requirements/driver-and-processor-interfaces.md)
- [Core Protocol and Reference Userland](core-protocol-and-reference-userland.md)
- [NAS Vertical Slice Implementation Plan](nas-vertical-slice-implementation-plan.md)
- [Open-Source Adoption and Code Borrowing](../references/open-source-adoption-and-code-borrowing.md)
- [Documentation index](../README.md)
