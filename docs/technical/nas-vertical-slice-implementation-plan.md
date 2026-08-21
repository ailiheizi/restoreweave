# NAS Vertical Slice Implementation Plan

## 1. Objective

This plan turns RestoreWeave from a broad research design into one implementable self-hosted NAS data-layer slice.

The slice must prove four product outcomes together:

1. Reduce the managed exact footprint through a mature deduplicating and compressing repository engine.
2. Understand heterogeneous files through a bounded, replaceable processing runtime.
3. Expose one authenticated original-path namespace with useful baseline search.
4. Verify and restore exact data without the live catalog, index, AI service, or processor registry.

The reference deployment is a single Linux-based NAS or server. A platform-specific snapshot driver is optional. The exact-storage and recovery slice remains independently testable without embeddings, but a conforming `RW-MVP-1` reference distribution must bundle the local ONNX/BGE embedding profile and in-process zvec generation for its default discovery experience. CLIP, neural codecs, P2P, writable NAS gateways, HA, and multitenancy are not required for this slice. Component choices and qualification gates are tracked in [Open-Source Adoption and Code Borrowing](../references/open-source-adoption-and-code-borrowing.md).

## 2. Current implementation reality

The repository currently contains:

- A deterministic scanner with streaming SHA-256, source-change evidence, final-component no-follow opens, and descriptor-rooted capture (`ROOTED_FD`).
- A local-tree `CaptureDriver`, raw development and local-zstd candidate `RepositoryDriver` profiles, capture-qualified catalog adoption, signed portable snapshot publication, and catalog-free restore.
- SQLite records for namespace, representation, capture bindings, and publications.
- `SnapshotTree`, `FileAccess`, bounded repository reads, storage-range reads, and representation-decoder interfaces.
- A Unix-socket control plane (`restoreweaved` / `rw`) and a legacy `internal/plugin` prototype.

The legacy plugin prototype is not the implementation target. It exposes many historical categories, mixes representation concerns, and lacks the current route, artifact, provenance, resource, and decoder contracts. External execution is disabled, so the prototype may be replaced without preserving it as a public ABI.

## 3. Product boundary to implement

The active extension seams are:

```text
CaptureDriver
Processor
RepositoryDriver
IndexProvider
QueryProvider
```

`RetrieverDriver` remains later.

The core owns:

- Stable source, capture, subject, namespace, file-version, content, representation, placement, snapshot, artifact, and generation identities.
- Exact hashing and duplicate identity.
- Identification and processing routes.
- Staging, sealing, digesting, validation, admission, publication, and verification acceptance.
- Plans, authority, lifecycle, rollback, and garbage-collection eligibility.
- `SnapshotTree`, `FileAccess`, query brokering, and result reauthorization.

Suffix and magic-byte detection are host-owned. CLI, MCP, REST, WebUI, and external export consumers are northbound clients rather than algorithm plugins.

## 4. Canonical data flow

```text
CaptureDriver -> inventory
  |-> host exact hash -> RepositoryDriver -> readback -> portable publication
  |-> suffix -> magic -> IdentificationRouteRef
      -> optional CLASSIFY_LEARNED / classification PARSE
      -> host ClassificationRecord
      -> ProcessingRouteRef
      -> Processor RUN_STAGE
      -> sealed ProcessorArtifactEnvelope
      -> host validation and admission
      -> optional RepositoryDriver placement or authorized index feed

portable records + namespace -> SnapshotTree / FileAccess
admitted records -> IndexProvider generation
adapter query -> host broker -> exact IndexGenerationRef
-> QueryProvider -> broker reauthorization -> adapter result
```

Exact ingest and interactive reads must have resource reservations independent of optional processing.

## 5. Processor narrow waist

### 5.1 Route types

An invocation binds exactly one route reference:

- `IdentificationRouteRef`: may contain only `CLASSIFY_LEARNED` and classification-refining `PARSE` nodes.
- `ProcessingRouteRef`: begins after final or explicitly accepted unresolved classification and may contain `PARSE`, `EXTRACT`, `ENRICH`, `FINGERPRINT`, `TRANSFORM`, `VALIDATE`, and `INDEX_PREPARE` nodes.

Both routes are immutable, digest-addressed, host-built, typed DAGs. A processor cannot add a node or broaden its own scope.

### 5.2 Operations

The initial Processor protocol needs two operations:

| Operation | Responsibility |
| --- | --- |
| `RUN_STAGE` | Execute one declared stage node against bounded immutable inputs. |
| `DECODE_REPRESENTATION` | Materialize a pinned retained representation for read, verification, migration, or restore. |

A transform profile declares encode and decode directions separately. A historical decoder may stop accepting new encoding while remaining available for retained representations.

### 5.3 Handles

Large bytes never travel inline in ordinary control messages. The host issues opaque handles:

- `SourceContentHandle`: immutable source or staged exact bytes with declared access pattern.
- `ArtifactReadHandle`: one admitted immutable processor artifact.
- `CollectionViewHandle`: one inventory generation plus an explicit bounded member set and paginated reads.
- `StagingWriteHandle`: one attempt-fenced output object.
- `EncodedRepresentationHandle`: one selected immutable representation for decode.

No handle exposes an unrestricted path, repository credential, SQL connection, signing key, or ambient network access.

### 5.4 Artifact state machine

```text
ALLOCATED
-> WRITING
-> SEALED
-> HOST_DIGESTED
-> SCHEMA_VALIDATED
-> POLICY_ADMITTED
-> PLACED / ROUTE_AVAILABLE / INDEX_FEED_PUBLISHED
```

Only the host advances states after `SEALED`. Rejected, cancelled, expired, superseded, or stale-fenced outputs never become route inputs, repository placements, or index records.

### 5.5 ProcessorArtifactEnvelope

Every admitted candidate binds:

- Artifact ID and content-addressed `SchemaRef`.
- Subject, source revision, optional segment, and input lineage.
- Representation kind, recovery-claim reference, lifecycle, and output authority.
- Sensitivity, ACL, residency, retention, and purge lineage.
- Producer, capability profile, package, configuration, dependency, model, and runtime digests.
- Coverage, media type, length, host-computed digest, warnings, and resource use.
- Route and attempt identity.

This is the common artifact bus for deterministic tools and AI processors. There is no AI-specific data plane.

### 5.6 Selection

Capability matching uses declarative host-evaluated selectors. Precedence is:

1. Explicit operator pin.
2. Supported qualification state.
3. Selector specificity.
4. Configured priority.
5. Stable capability ID.

An equal-precedence match is a visible route conflict. Schema compatibility uses exact schema digests or a qualified compatibility rule, not names alone.

## 6. Implementation milestones

### Milestone 0: retire the legacy extension direction

- Freeze `internal/plugin`; add no new public behavior to its historical categories.
- Introduce private packages for current contracts, for example `internal/processor`, `internal/extension`, `internal/artifact`, and `internal/route`.
- Keep public serialized protocols experimental until at least one out-of-process implementation and cross-version conformance suite exist.
- Remove package-declared trust, enablement, and qualification state from future manifests; these are host-owned deployment records.

Exit condition: the new internal model represents exactly the approved seams and Processor roles without importing legacy category or transformation enums.

### Milestone 1: exact ingest and repository readback

Status (2026-08-13): retained-root descriptor capture is implemented in `server/internal/scanner`. The remaining M1 items below are now implemented as a fake exact lane, not a release engine: `server/internal/capture` emits a durable `CaptureRootBindingRecord` without serializing runtime descriptors; only `ROOTED_FD` complete scans are adopted into content/file-version/namespace records (`server/internal/exact`); a directory CAS `RepositoryDriver` provides idempotent placement, independent SHA-256 readback, and portable snapshot JSON; restore reconstructs that snapshot after catalog loss. An in-tree Driver qualification harness lives in `server/internal/repository/qualify` (`TestFakeCASPassesDriverGates`); optional Restic/Kopia CLI probes run on Darwin/Unix when those binaries are present and do not select a release engine. Local crash-retry, keep-latest prune, and repository-relocation probes exist for both CLIs (`TestResticCrashRetryStillRestores`, `TestResticGCKeepsLatestOnly`, `TestResticRepoRelocationStillRestores`, `TestKopiaCrashRetryStillRestores`, `TestKopiaGCKeepsLatestOnly`, `TestKopiaRepoRelocationStillRestores`). NAS/S3 performance and Linux kernel ABI re-runs remain. The M1 exit condition is covered by `TestIngestPlacesAndRestoresAfterCatalogLoss`.

- Replace ambient absolute-path traversal with one retained source-root handle and component-relative lookup. On Linux, use `openat2(2)` or an independently qualified handle-based equivalent; bind source and mount identity, retain parent handles until children are safely opened, and derive enumeration, metadata, link targets, type validation, and content reads from bound handles.
- Complete one generic local or mounted-tree `CaptureDriver` profile. It must fail closed on unresolved root, ancestor, bind-mount, remount, nested-mount, snapshot, or special-file substitution and must emit a durable `CaptureRootBindingRecord` without serializing runtime descriptor numbers.
- Convert observations into stable content, file-version, and namespace records only after the capture-root binding and traversal-validation gates pass. Candidate records assembled earlier remain non-authoritative and cannot enter `SnapshotTree`, `FileAccess`, repository placement, or publication.
- Run a Kopia v0.23.1 qualification spike with Restic v0.19.1 as the control and Borg 1.4.5 as a bounded local/SSH comparison. Passing garbage-collection safety, crash reconciliation, bounded-read, independent SHA-256 readback, clean-recovery, corruption, and NAS performance gates selects the first `RepositoryDriver`; research preference alone does not.
- Implement idempotent placement, reconciliation, bounded readback, and placement receipts.
- Publish one portable exact snapshot and reconstruct it without the operational database.

Exit condition: an unknown binary file survives scan, exact placement, catalog loss, clean restore, and SHA-256 comparison.

### Milestone 2: deterministic identification and Processor host

Status (2026-08-13): host-owned suffix/magic identification already lives in `server/internal/identify`. The Processor host is `server/internal/processor`: host-built `ProcessingRouteRef`, opaque source/staging handles, seal → host digest → schema validate → `POLICY_ADMITTED`, and a cooperative in-process `RUN_STAGE` pool with panic recovery and stage timeouts. Bundled BYTE_DETERMINISTIC EXTRACT processors are UTF-8 text (`extract.text.v1`), ID3/FLAC/OGG tags (`extract.audio.tags.v1`), and EPUB OPF (`extract.book.meta.v1`). Admitted artifacts are catalog rows (schema v5) and feed the next FTS generation. Exact ingest ignores processor failures. `audio.list` lists admitted audio-tag artifacts and albums derived from those tags; `books.list` lists EPUB OPF metadata and TXT/Markdown extracts; `content.read` range-reads those subjects. They are catalogs, not a player or reader. The read-only MCP harness exposes both list operations. `server/internal/processor/sandbox` plans a bubblewrap argv (no network, no extra binds, host-owned staging only); Darwin `Run` returns `unsupported_platform` and default ingest `RUN_STAGE` remains in-process. `server/internal/processor/rpc` sends private protobuf RUN_STAGE frames over a Unix socket and passes source/staging bytes with SCM_RIGHTS; the host independently SHA-256s staging. The M2 isolation exit is covered by `TestProcessorPanicDoesNotBlockExactLane` and `TestProcessorTimeoutDoesNotBlockExactLane`. grpc-go wrapping of the same messages is tested (`TestGRPCRunStagePassesBytesOnFDsNotInMessages`); Linux bubblewrap execution and seccomp remain.

- Add host-owned suffix and magic-byte evidence.
- Implement `IdentificationRouteRef`, `ClassificationRecord`, and `ProcessingRouteRef`.
- Implement input handles, staging handles, sealing, host digesting, schema validation, admission, and artifact lineage.
- Implement the reference control plane with protobuf schemas and gRPC over a Unix-domain socket. Pass large input and output through pre-opened file descriptors rather than protobuf, JSON, REST, or MCP messages. This is private implementation plumbing until cross-version conformance proves a public wire contract.

> Landing note: part of the M2 control-plane slice is implemented as the `client/command` JSON-envelope Unix-socket daemon (`server/controlplane`), the thin client transport (`client/transport`), the `rw` CLI (`client/cmd/rw`), and the stdio MCP server (`client/mcp`). Processor RUN_STAGE protobuf frames plus SCM_RIGHTS live in `server/internal/processor/rpc` (private, experimental). grpc-go wrapping uses the same messages over a Unix handshake that still passes FDs out of band.
- Run one isolated out-of-process deterministic Processor through `RUN_STAGE`, with bubblewrap, namespaces, seccomp, `no_new_privs`, rlimits, cgroup v2, an allowlisted environment, no network, no ambient source mount, and host-owned staging.
- Add resource pools, quotas, cancellation, fencing, retry ceilings, crash-loop quarantine, and dead-letter handling.

The first processor should be deliberately small: bounded text or common metadata extraction. It proves the protocol without making a heavy external runtime the core. The subsequent default pack should use bounded libmagic plus qualified isolated Tika, libarchive, and ffprobe routes; optional Siegfried and ExifTool integrations remain separately qualified.

Exit condition: killing or saturating the processor does not stop exact hashing, placement, browse, readback, verification, or restore.

### Milestone 3: baseline discovery

Status (2026-08-13): durable whole-subject tags and notes live in the operational catalog (`annotations`, schema v4), not in the search index. SQLite FTS5 generations are physically separate files under `repository/indexes/`; the catalog only stores an `IndexGenerationRef` pointer. `search.query` reauthorizes every hit through `GetNamespaceEntry`. `content.open`/`read`/`close` read exact CAS bytes. The M3 exit condition is covered by `TestIndexLossDegradesSearchOnly`.

- Store durable whole-subject tags and notes outside index state.
- Implement an authorized replayable index feed.
- Ship SQLite FTS5 as the bundled `RW-MVP-1` lexical `IndexProvider` and `QueryProvider`. Use one physically separate disposable database per immutable `IndexGenerationRef`; never expose the SQLite schema, row IDs, tokenizer tables, or query syntax as durable ABI.
- Index path, filename, suffix, selected type, metadata, checksum, duplicates, processing state, tags, notes, and extracted text.
- Query exactly one named `IndexGenerationRef` per provider invocation and reauthorize every result in the host broker.

Exit condition: deleting the complete index degrades search only; namespace access, tags and notes, exact reads, verification, and restore remain intact, and a new generation rebuilds from durable records.

### Milestone 4: original-path recovery access

Status (2026-08-17): repository-backed `SnapshotTree` and `FileAccess` are implemented in `server/internal/access`. Exact reads go through host-owned `DECODE_REPRESENTATION` (`server/internal/decode.IdentityDecoder` for `identity/sha256-v1`); the host independently SHA-256s decoded bytes. File-shaped egress is `plan.restore`, export materialization, and `FileAccess`. The mount ABI and go-fuse implementation were removed. Byte equality is covered by `TestFileAccessMatchesRestoreBytes` and restore SHA-256.

- Complete repository-backed `FileAccess` reads.
- Implement `DECODE_REPRESENTATION` through the existing bounded decoder path.
- Implement frozen `ExportManifest` materialization with explicit destination, collision, metadata-degradation, restart, and verification behavior.

Exit condition: CLI, restore, export materialization, and `FileAccess` return the same exact bytes and snapshot-pinned namespace metadata; destination conflicts never cause silent substitution.

### Milestone 5: portable recovery and upgrade proof

- Complete payload, prepared closure, publication commit, trust-anchor, and recovery export behavior.
- Recover on a clean installation without SQLite, indexes, processor registry, AI, or WebUI.
- Upgrade one processor side by side, rebuild only affected artifacts and index generations, compare results, switch atomically, and roll back.
- Retain a historical decoder until every dependent representation is migrated and reverified.

Exit condition: the complete `RW-MVP-1` acceptance suite passes.

### Milestone 6: first real capacity-release profile

- Add an immutable source-retirement plan.
- Require full exact recovery, placement sufficiency, clean restore, grace period, rollback data, and fresh operator authority.
- Release only the exact approved source scope and reconcile every deletion.
- Continue to disable autonomous or processor-authorized deletion.

Exit condition: the product can report non-zero actually released source capacity without weakening the declared exact recovery contract.

## 7. Default implementations versus stable contracts

The first distribution should be opinionated:

- One generic capture profile.
- One release-qualified repository engine.
- Host-owned suffix and magic detection.
- A small deterministic metadata and text processor pack.
- One bundled lexical index and query implementation.
- CLI and local read-only MCP.

Replaceability is proven by side-by-side generations and migration, not by shipping an empty marketplace. WebUI configuration should use profiles, checkboxes, and expert overrides; a node-graph editor is unnecessary.

## 8. Required performance and isolation tests

Measure on representative low-power and mid-range NAS hardware:

- Scan and hash throughput.
- Repository growth versus raw input and direct engine use.
- Processor CPU, RAM, scratch, and amplification.
- Index size, build time, incremental lag, and rebuild time.
- Restore throughput and clean-recovery time.
- Exact-path service behavior while optional processors are saturated, crashing, or quarantined.

Storage reports separate logical bytes, unique exact bytes, repository growth, catalog and index growth, processor dependencies, retained source bytes, potentially reclaimable capacity, and actually released capacity.

## 9. Immediate coding order

Remaining host-split work is recorded in [Implementation Completion Plan](implementation-completion-plan.md). The original sequence below is retained as the historical coding order for this slice.

1. Implement and adversarially qualify retained-root, descriptor-relative capture before treating scanner output as authoritative. Cover ancestor rename and symlink substitution, bind mounts, remounts, root replacement, snapshot loss, unavailable `openat2`, and regular-file-to-FIFO or device races.
2. Add new route, capability-profile, invocation, artifact-envelope, staging-state, and decoder-operation types with canonical serialization and validation tests.
3. Adapt only capture-qualified observations into stable content, file-version, and namespace records.
4. Build a fake in-memory `RepositoryDriver` and fake Processor to prove state machines and fault handling before binding a mature engine.
5. Add the Kopia-led repository qualification harness and retain Restic as the control before selecting the first release engine.
6. Implement the first exact end-to-end CLI path before semantic search work.
7. Add the isolated Processor runtime and deterministic identification/extraction pack.
8. Add SQLite FTS5 generations, then the local zvec semantic generation, using the provider qualification gates.

Do not extend the legacy category manifest, implement embeddings, add a visual pipeline editor, or begin P2P work before this sequence is complete.
