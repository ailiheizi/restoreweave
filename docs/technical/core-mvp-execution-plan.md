# Core MVP Execution Plan

> **Status:** Normative implementation order, updated 2026-08-24. This plan does not add a new product surface. It translates the product requirements into one dependency-ordered path for the content, protection, discovery, export, and recovery core. FUSE, network filesystem servers, players, readers, and domain facades are outside this plan.

## 1. What We Are Building

The first useful RestoreWeave core is one operator loop:

```text
configure data location, storage profile, and embedding profile
-> inspect an explicit source
-> choose exact, exact-with-external-fallback, link-only, or metadata-only protection
-> create a reviewable plan
-> apply and publish immutable records
-> retain names, metadata, descriptions, and recovery references
-> search lexical + structured + semantic information
-> freeze an ExportManifest
-> materialize and verify selected output
-> restore exact bytes or explicitly reacquire an external reference
```

The core owns the records and evidence. A model, repository, external URL, or client can be replaced without changing the meaning of a `Subject`, `ContentIdentity`, `ProtectionRecord`, `DescriptionDocument`, or `ExportManifest`.

The physical personal-use core is intentionally small: one configured SQLite
catalog holds metadata and rebuildable search tables, while one configured
content repository holds file bytes and admitted representations. Portable
recovery records and signed publication bundles are backup artifacts for that
catalog/repository pair, not a second live metadata database. New stores or
services require a concrete workflow and an executable reason; logical layers
alone do not justify physical duplication.

## 2. Current Baseline

The checkout already contains useful foundations:

- raw and display namespace names, path keys, and a portion of filesystem metadata;
- SHA-256 exact placement in a development raw directory CAS and an opt-in embedded whole-file zstd candidate;
- SQLite operational records for observations, namespace entries, file versions, representations, artifacts, annotations, plans, jobs, and publications;
- portable snapshot JSON, self-digest checks, and catalog-free exact restore tests;
- lexical FTS foundations and fixture dimension/fusion contracts;
- CLI, Unix command protocol, read-only MCP, and a bounded loopback `/api/v1` plus local browser client over the same command ABI.

These foundations are not release completion. In particular, fixture embeddings are not a real semantic provider; neither the raw directory CAS nor the local zstd candidate is a qualified production repository. The checkout now has a signed publication-closure path and a catalog-free `PORTABLE_FACT_CLOSURE` successor chain for the currently admitted portable fact profile. That chain authenticates hard-link and sparse-indication facts, boundary and detection evidence, platform xattr/ACL observations or explicit degraded states, explicit unsupported extent-map/stream/fork/flag states, portable subject mapping, description and annotation revisions, and admitted processor-artifact bodies. A separate signed post-commit child authenticates deterministic terminal processor-attempt provenance without changing the exact commit. Clean-install import, independent trust-anchor handling, no-follow input/repository reads, cross-process fencing, unknown-outcome reconciliation, relocation, corruption, and reader-dependency behavior are implemented and tested for the admitted development profile. Remaining release work includes sparse extent maps, broader per-field provenance, packaging and qualification of the real semantic provider, a qualified production repository, and full release acceptance. A snapshot's embedded digest alone detects accidental modification but is not an independent trust anchor: a writer who can replace the JSON can recompute that digest.

## 3. Phase Order

| Phase | Name | Depends on | User-visible result | Exit gate |
| --- | --- | --- | --- | --- |
| 0 | Contract and config freeze | None | One unambiguous configuration and status vocabulary | Normative docs and traceability matrix agree |
| 1 | Protection and portable facts | 0 | `STORE_EXACT`, `STORE_EXACT_WITH_EXTERNAL_FALLBACK`, `LINK_ONLY`, and `METADATA_ONLY` decisions produce canonical, honest outcomes | Clean manifest preserves names, metadata, and recovery state |
| 2 | Plan/review/apply integrity | 1 | Preview is non-mutating; apply is digest-bound and idempotent | Stale or altered plans fail closed |
| 3 | Recovery closure | 1, 2 | A clean machine can verify and restore without SQLite or indexes | Signed closure/publication and corruption tests pass |
| 4 | Descriptions and complete discovery | 1, 3 | Users can search filename, metadata, text, tags, notes, descriptions, and semantic meaning | Structured coverage and segment provenance are complete |
| 5 | Simple qualified storage savings | 2, 3 | Real physical savings are measured and recoverable | Repository codec/dedup qualification passes |
| 6 | Link groups, export, and operator ergonomics | 2, 3, 4, 5 | Users keep related file links together, select by group or view, and export without internal IDs | LinkGroup/view -> manifest -> materialize -> verify passes |
| 7 | Release hardening | 3, 4, 5, 6 | Native personal-use installation and upgrade path | Full `RW-MVP-1` acceptance passes |

The order is deliberate. Semantic features must not be allowed to hide missing recovery evidence, and neural compression must not be allowed to decide exact identity or postpone a trustworthy restore path.

The dependency graph is:

```text
Phase 0 -> Phase 1 -> Phase 2 -> Phase 3
                                  |-> Phase 4: descriptions + complete discovery --|
                                  |-> Phase 5: qualified simple storage -----------|
                                                                                   v
                                                                        Phase 6 -> Phase 7
```

Phases 4 and 5 may proceed in parallel only after the Phase 3 portable-record shapes are frozen. Phase 6 waits for both: an export cannot make a qualified recovery or net-savings claim against an unqualified repository, and the ordinary selection loop is incomplete without the default discovery experience.

Every phase uses the same completion rule:

1. **Only in scope:** the records, operations, and user outcome named by that phase.
2. **Explicitly out of scope:** adapters and algorithms that do not close its exit gate.
3. **Entry evidence:** every dependency gate is closed or a narrowly documented interface stub is sufficient for parallel work.
4. **Exit evidence:** executable acceptance tests plus visible status; types, fixtures, mocks, and protocol handshakes alone do not close a phase.
5. **User result:** one ordinary workflow that does not require internal IDs or knowledge of repository/index internals.

### Implementation checkpoint (2026-08-29)

This table is checkout status, not a change to phase gates:

| Phase | Current checkout | Still required before the phase gate closes |
| --- | --- | --- |
| 0 | Strict persisted `restoreweave.config.v1`; XDG paths; `rw config init/validate/show --effective`; config digest on ingest plans, snapshots, publications, real lexical generations, and daemon-created description revisions; immutable embedding-generation manifest fields and fail-closed config/profile/semantic-space matching; verified ONNX/BGE + zvec bundle loading, an explicit fixed development-time bundle installer, startup inference probe, generation warm-up, and explicit degraded state | Supported model packaging and release/default qualification remain open; the development installer is not a release package |
| 1 | `ProtectionRecord`, `RecoveryReference`, `SourceBinding`, multiple credential-free `ExternalLocator` rows, raw names/paths and before/after metadata in the manifest; per-file `STORE_EXACT`, `STORE_EXACT_WITH_EXTERNAL_FALLBACK`, `LINK_ONLY`, and `METADATA_ONLY` decisions bound by a protection digest; typed planned outcome/reason/identity; visible exact fallback for unresolved readable content; failed/unstable entries retained with requested mode, `BLOCKED`/`UNAVAILABLE` outcome, path, state, and reason in a non-executable plan; a narrow successor-plan resolution admits only explicitly selected, stable rooted regular-file read failures as `METADATA_ONLY` and preserves `INCOMPLETE` scan authority; immutable per-subject processor attempts persist terminal outcomes and a signed post-commit child binds their deterministic bundle to the exact parent; `snapshot.v2` records typed hard-link, sparse indication, boundary, detection, xattr/ACL observed-or-degraded facts, and explicit unsupported extent/stream/fork/flag facts with per-record provenance digests | Broader platform capture qualification, sparse extent maps, and per-field ownership/mode/time provenance remain open; the admitted recovery profile is closed by the Phase 3 portable closure |
| 2 | **Sub-items complete in this checkout:** non-mutating ingest and restore planning, digest-bound apply, source/config/manifest/destination revalidation, same-plan replay, committed-publication reconciliation, verified completed-restore reconciliation, typed ingest-protection revision with reinspection/recomputation, fail-closed apply for blocked plans, per-subject degraded processor reporting, durable append-only processor terminal outcomes, a signed complete-state successor lineage that retains later outcomes without rewriting earlier attempts, and bounded automatic retry of the same signed processor plan with idempotency, leases/fencing, reconciliation, restart/resume, and retry ceilings. The exact apply Job remains successful after post-publication processor/index failure. **The Phase 2 gate remains open.** | Qualify the formal asynchronous processor model and define a separately signed contract for arbitrary manual, rerouted, or general reprocessing; production repository qualification and release acceptance belong to Phases 5 and 7 |
| 3 | **Implemented and tested for the admitted development profile (2026-08-24):** Ed25519 `PREPARED_CLOSURE`, `PUBLICATION_COMMIT`, `PROCESSOR_ATTEMPT_CLOSURE`, and `PORTABLE_FACT_CLOSURE` records; portable mapping, descriptions/annotations, artifact-body attachments, typed filesystem facts, token-set export, immutable placement and independently hashed readback, generation/parent lineage, catalog-free validation/restore, clean-install import, independent-anchor validation, no-follow input/repository reads, cross-process fencing (including two real daemon processes sharing one catalog/repository), unknown-outcome reconciliation, bounded retry of the same signed processor plan, and raw/zstd relocation/corruption/reader-dependency evidence. The daemon enables signed publication by default | This is not release qualification. Manual reprocessing needs an explicit signed successor lineage; broader metadata/provenance capture, production repository selection (Phase 5), and full release acceptance remain open |
| 4 | Durable description revisions and source-aligned segments have portable authenticated recovery with config/provider/segmentation profile bindings; the complete stated lexical/structured field scope, typed filters, segment-hit provenance, measured coverage, host-owned fusion, generation binding, honest semantic degradation, focused tests for the fixed development installer, and a real supervised Darwin arm64 ONNX/BGE + zvec daemon/CLI E2E (with corresponding opt-in Linux arm64 evidence) are implemented and executed. The browser adapter build/E2E covers semantic-unavailable degradation and does not claim browser install/restart/rebuild/real-query execution | Package the real pinned worker and bundle for supported offline installs, complete semantic coverage/qualification evidence, and make the qualified default broker lexical + structured + semantic |
| 5 | `local-zstd-v1` remains an opt-in, no-Docker-Compose unencrypted candidate with whole-file SHA-256 deduplication, checksummed zstd payloads, transparent decode-and-hash verification, physical-byte placement receipts, profile mismatch protection, relocation/corruption tests, explicit host-owned repair and copy-forward migration that preserves repository identity, independently reopened source/target signed clean-reader verification with target-tamper rejection and source rollback restore, concurrent no-replace placement, catalog-free signed restore, fail-closed logical/duplicate/compression/physical/overhead/net accounting, and non-destructive verified placement inventory/root/active-lease candidate planning. A separate `local-zstd-encrypted-v1` experimental profile now proves host-owned AES-256-GCM key resolution, missing/wrong-key fail-closed behavior, encrypted readback, relocation, and key-rotation copy-forward without changing portable records or the config default | Encryption release admission, chunk deduplication, destructive collection, broader crash qualification, representative corpus measurements, full migration rollback/reader-closure qualification, packaging, and a release repository decision. Phase 5 remains open |
| 6-7 | SavedView and legacy flat-output ExportManifest work for the stated local scope; a real daemon/CLI process test covers description/annotation/search -> view -> manifest -> materialize/verify -> independent-anchor clean restore. Export v1 still uses a snapshot-local entry handle and single-component name. Minimal LinkGroup is documented but has no schema, operation, subject-kind/search authorization, group-aware export successor, browser flow, portable current-state record, or executable evidence | Phase 6 cannot qualify before Phases 4 and 5 close. Implement and qualify the minimal current-link LinkGroup and export successor without reopening richer Collections; packaging, upgrade, backup, performance, ID-free ergonomics, and full release acceptance remain Phase 7 |

`capability.list` is fail-closed: without a real semantic provider it reports `SEMANTIC_INDEX_UNAVAILABLE`. Acoustic, semantic, and multimodal fixtures are available only to an explicitly enabled qualification harness and are never default capabilities.

### Immediate work lock

The Phase 3 implementation lock remains the recovery guardrail for this checkpoint. Its admitted recovery profile has executable exit evidence, but that result does not make the repository release-qualified or authorize Phase 4/5 completion claims. A bounded `/api/v1` adapter and browser client may be developed as convenience surfaces only:

1. Keep terminal processor attempts in their authenticated post-commit successor chain. The admitted worker may perform only bounded automatic retry of the same signed plan with retry intent, idempotency, reconciliation, fencing, and retry ceilings bound to that lineage. Do not enable arbitrary manual, rerouted, or general reprocessing without a separately reviewed successor contract.
2. Extend sparse extent maps or per-field provenance only through a new reviewed portable-record shape; do not reinterpret the frozen admitted shape.
3. Full repository repair, repository-profile migration/rollback, and engine selection remain Phase 5.

After that, Phase 4 and Phase 5 may run in parallel. Phase 4 first delivers the real local ONNX/BGE worker, zvec generation, complete field coverage, segment provenance, and fused query. Phase 5 measures `local-zstd-v1` and selects and qualifies one mature repository target from the common evidence. Only then does Phase 6 implement the reviewed minimal LinkGroup shape and complete and qualify groups, views, and exports. The LinkGroup documentation decision alone does not unlock that implementation.

The later phase locks prohibit FUSE or network filesystem behavior; an OpenList fork or dependency; player or domain-reader behavior; additional acoustic/graph/multimodal dimensions; and RWKV/Transformer codec implementation. The retired OpenSubsonic/OPDS/Inbox compatibility code is not part of the daemon. Any future browser/API surface must remain a bounded adapter and cannot count as recovery or storage qualification.

The implemented and tested Phase 3 item uses the normative `PORTABLE_FACT_CLOSURE` v1 shape. It
is a complete-state signed successor chain over the one exact parent, not a new
publication. Implementation order inside that item is: immutable catalog
revision exports and subject mapping; content-addressed body attachments;
signed child placement/discovery/conflict validation; catalog-free rebuild;
then actual platform fact capture and restore-degradation receipts. The
admitted profile has executable closure, relocation, corruption, and clean
reader evidence; merely defining the structs or placing a child would not
have closed the item.

## 4. Phase 0: Contract and Configuration Freeze

### In scope

Freeze these records and enums before adding more adapters:

- `ContentIdentity = (sha256 digest, logical length)`;
- `ProtectionRecord` modes: `STORE_EXACT`, `STORE_EXACT_WITH_EXTERNAL_FALLBACK`, `LINK_ONLY`, `METADATA_ONLY`;
- visible outcomes: `EXACT_PROTECTED`, `EXACT_FALLBACK`, `EXTERNAL_REPLAYABLE`, `LINK_ONLY_UNPROTECTED`, `EXPLICITLY_UNPROTECTED`, `BLOCKED`, `UNAVAILABLE`;
- `RecoveryReference` kinds: exact, reversible, external locator, and user recipe;
- `MetadataBundle`, `DescriptionDocument`, `DescriptionRevision`, and `SemanticSegment` provenance;
- `ConfigProfile` and `config_digest` binding rules;
- local embedding as the personal default, online/hybrid as explicit profiles;
- no built-in mount or network filesystem service.

The persisted configuration is the operator's small set of choices, not an arbitrary plugin manifest. The first schema contains paths, repository profile, default protection mode, link-only policy, compression profile, embedding mode/profile/backend, description policy, and recovery policy. Credentials are references, not plaintext configuration values.

### Required commands

```text
rw config init
rw config validate
rw config show --effective
```

Path precedence is `one-shot CLI flag -> environment override -> persisted config -> platform default`. The resolved, redacted configuration is printed at startup and its digest is bound into plans, representations, descriptions, index generations, and publications.

### Out of scope

- model download orchestration;
- RWKV codec implementation;
- external retrieval execution;
- UI or protocol facade changes.

### Exit tests

1. An unknown config field or schema is rejected.
2. A relative path is resolved once and reported as an absolute effective path.
3. Secrets never appear in `config show --effective` or plan JSON.
4. Changing an embedding or storage profile creates a new plan/generation and cannot reinterpret old records.

## 5. Phase 1: Protection and Portable Facts

### 5.1 Protection record

Every namespace subject receives a `ProtectionRecord` independent of whether a local payload exists. It records the selected mode, outcome, expected content identity/length, local representation references, external binding references, policy decision, and last verification.

`LINK_ONLY` is never inferred from a failed processor or an unavailable repository. It is an explicit plan decision. When source bytes are readable, the plan shows the exact-storage alternative and the loss of local protection before the operator confirms link-only mode.

The current command shape supports a tree default plus explicit per-file overrides:

```text
rw ingest <root> --protection LINK_ONLY --confirm-link-only \
  --locator 'relative/path.ext=https://example.invalid/path.ext' \
  --locator 'relative/path.ext=ipfs://content-address/path.ext'  # creates READY plan
rw plan apply <plan-id> --workspace <workspace-id> --digest <plan-digest>
```

For a mixed tree, repeat `--file-protection 'relative/path=MODE'`. The READY plan lists each regular file's planned outcome, reason, expected SHA-256 identity and length, and locator count; all of those facts contribute to `protection_digest` and the outer plan digest.

The first command is read-only and must be reviewed before the digest-bound `plan.apply`; it does not publish repository data by itself.

An unscoped `--locator URI` is accepted only when the capture contains exactly one regular file. Repeating `--locator` records alternatives for that subject. The current implementation hashes readable source bytes and records the expected digest and length, but it does not fetch the locator; therefore the outcome remains `LINK_ONLY_UNPROTECTED` and validation remains `UNVALIDATED`. Human output must say this explicitly so a successful catalog write cannot be mistaken for successful protection.

### 5.2 External references

Implement structured `SourceBinding` and `ExternalLocator` records rather than placing URLs in generic metadata. One binding may have multiple locators with priority, type, expected length/digest, immutable revision, credential reference, rights evidence, availability, expiry, and validation history.

Portable locator fields are credential-free. Userinfo, URI fragments, and query strings are rejected by the current inert recorder because signed URLs and bearer parameters would otherwise leak into snapshots, recovery exports, and search indexes. Access material belongs behind `credential_ref`. A future `RetrieverDriver` may define a narrower typed set of public locator parameters, but it must still keep secret material out of portable records.

Reacquisition is always explicit:

```text
external locator
-> quarantine acquisition
-> independent digest/length and component validation
-> new local exact representation
-> new placement and recovery reference
```

A link-only subject remains link-only until this process succeeds. A mutable URL without expected identity is discovery evidence, not a recovery claim.

### 5.3 Portable namespace closure

Extend the portable record so each entry retains:

- raw filename bytes and safe display name;
- raw parent/path component relation, not only a UTF-8 joined path;
- entry type, source identity, before/after metadata, and change evidence;
- size, allocated size, timestamps, mode, ownership, link count, hard-link group, sparse facts, xattrs/ACL references, and declared unsupported fields;
- suffix/magic/detection evidence and processing warnings;
- namespaced `MetadataBundle` facts with per-field provenance and authority;
- protection mode/outcome;
- representation and recovery-reference IDs;
- external locator metadata without credentials.

The display path is for humans. Restore resolves the raw component sequence under the destination profile and reports any fidelity loss explicitly.

### Exit tests

1. A regular file may have no local representation only when its protection mode is link-only or metadata-only and the state is visible; a readable source may still have a computed `ContentIdentity`.
2. Multiple URLs for one subject survive catalog rebuild and portable export.
3. A clean reader can reconstruct the original filename and metadata from the portable record.
4. Link-only content never reports `RESTORE_VERIFIED` without a fresh, independently verified acquisition.

## 6. Phase 2: Plan, Review, and Apply Integrity

Planning must be genuinely non-mutating. The plan contains the resolved config digest, source/capture basis, per-entry protection decision, expected repository target, processing profiles, description policy, index scope, recovery policy, and canonical plan digest.

The lifecycle is:

```text
doctor/preflight (read only)
-> plan create (read only, immutable)
-> plan show / plan revise (read only, successor plan)
-> plan apply (digest-bound mutation)
-> reconcile / publish / job events
```

Apply MUST revalidate the source basis, destination/repository identity, profile digests, and link-only decisions. Processor failure can fail its derived branch but cannot block readable exact fallback. A retry uses idempotency and fencing and cannot create a second logical publication.

Restore and export planning are also non-mutating. A destination is checked before any write, and a changed destination or manifest digest fails closed. The current completed-restore crash reconciliation is intentionally narrower than the final recovery contract: after a worker crash it accepts only a complete, non-empty destination whose path set, entry types, file lengths, SHA-256 values, and symlink targets exactly match the manifest. Partial, changed, or extra output fails closed; an empty snapshot has no output evidence with which to distinguish "not started" from "already complete" and is not covered by this reconciliation claim.

### Exit tests

1. Plan creation leaves repository, catalog publication, and destination bytes unchanged.
2. Applying with a wrong digest, stale capture, changed config, or changed destination is rejected.
3. Applying the same plan twice returns the same logical result.
4. A processor panic, timeout, or unavailable model leaves exact protection and recovery usable.
5. Per-entry outcomes explain why a file is exact, fallback, link-only, blocked, or unavailable.

## 7. Phase 3: Recovery Closure

The admitted signed closure slice is implemented. It is deliberately narrower than the final recovery contract. `PREPARED_CLOSURE` is an immutable, content-addressed envelope containing the manifest/protection/metadata evidence currently admitted by the exact lane; `PUBLICATION_COMMIT` is a separately signed commit marker that binds the prepared-closure digest, payload receipt aggregate, generation, and parent lineage. An Ed25519 trust anchor is exported separately and is never inferred from repository companion data.

The final closure still needs a portable `RecoveryRecipe` for every non-raw representation. Such a recipe binds codec/decoder implementation, model/tokenizer/dictionary digests, framing/chunk index, encoded placement, decoded length, and verification profile. The admitted `snapshot.v2` profile includes typed hard-link, sparse-indication, boundary, detection, and explicit unsupported extended-metadata facts; `PORTABLE_FACT_CLOSURE` binds those facts plus subject mapping, description/annotation revisions, and admitted artifact bodies to the authenticated manifest. A separate signed `PROCESSOR_ATTEMPT_CLOSURE` closes the deterministic terminal-attempt bundle after exact commitment. Its v2 complete-state successor lineage retains later terminal outcomes without rewriting the exact publication or earlier attempts. Bounded automatic retry of the same signed plan uses that lineage; formal asynchronous qualification and arbitrary manual, rerouted, or general reprocessing remain later Phase 2 work. Broader per-field provenance also remains later work. Link-only records include their credential-free locators and unprotected outcome; executing external reacquisition belongs to the later `RetrieverDriver` profile and is not a Phase 3 gate.

The publication sequence is:

```text
payload placements
-> metadata and protection closure
-> signed PREPARED_CLOSURE placement and readback
-> signed PUBLICATION_COMMIT placement and readback
-> SQLite projection (rebuildable)
-> optional processing and immutable terminal-attempt rows
-> signed PROCESSOR_ATTEMPT_CLOSURE child placement and readback
```

The operational SQLite catalog and all search stores remain projections. Orphan payloads, prepared-only closures, and invalid signatures are not published. In-process serialization is supplemented by a repository-scoped cross-process fence/lease with renewal and typed unknown-outcome reconciliation; the signed records remain the recovery authority.

The current signed mode supports catalog-free discovery, verification, diff, restore planning, restore, and recovery export from the repository plus an independently supplied trust anchor. The catalog-free recovery-reader daemon validates the v2 reference at startup and imports the existing v1 export bundle without opening SQLite or signing material. The export bundle contains the commit and prepared closure and does not contain a private signing key; retaining it and the trust anchor separately is still an operator responsibility and does not establish a fully qualified independent failure domain.

### Current tests and admitted-profile gate

Implemented tests cover:

1. Delete SQLite and index projections; repository commit discovery authenticates and lists the committed snapshot.
2. Restore exact files and compare digest plus logical length from the signed publication path.
3. Inject missing, truncated, or modified payload/closure objects, or use the wrong trust anchor; recovery fails closed.
4. A commit created before a missing SQLite projection can reconcile that projection without publishing a second snapshot.
5. Recovery export includes commit/prepared closure records and no private key.
6. A failed processor attempt is authenticated by a post-commit child that a reader without SQLite verifies; tampering, a missing committed parent, and a conflicting bundle fail the processor-provenance read closed while exact discovery and restore remain usable.
7. A clean-install daemon test deletes SQLite and signing material, validates an independently retained v2 reference, imports a v1 bundle, and restores exact bytes over the recovery socket without creating a catalog.
8. Portable mapping, description/annotation, semantic-segment, artifact-body, filesystem-fact, token-set, no-follow, cross-process-fence (including a real two-daemon process test), child-reconciliation, relocation, corruption, and reader-dependency tests cover the admitted profile.

The Phase 3 recovery-closure gate is implemented and tested for the admitted development profile. The following evidence supports that bounded claim:

1. Every retained recovery-relevant fact in the admitted profile, including processor-attempt provenance and xattr/ACL/sparse/detection state, has a portable authenticated closure.
2. The clean-install import/reader and independently retained trust-anchor workflows pass real daemon/CLI process tests on the current Darwin host; an explicitly provisioned Linux arm64 run also passes the supervised ONNX worker plus host zvec lifecycle test. This is implementation evidence, not release qualification.
3. No-follow traversal, cross-process fencing/lease (including real daemon-to-daemon publication), and recovery-record corruption/relocation behavior are implemented and tested for the admitted profile.
4. Every currently admitted reversible representation decodes with its pinned closure and matches the source identity; every link-only reference round-trips with its honest unprotected state, while any future acquisition remains explicit and never silently falls back to a mutable URL.
5. Every published subject with a recovery reference can export a signed, independently retainable token set; metadata-only subjects export only their unprotected record.

## 8. Phase 4: Descriptions and Complete Discovery

### 8.1 Durable description model

Use a versioned `DescriptionDocument` rather than overloading `Annotation.NOTE` or `ProcessorArtifact.Body`. Kinds include user-authored, imported, extracted, AI summary, and AI analysis. Each revision records language, body digest, source artifacts/spans, producer/model profile, confidence/coverage, visibility, acceptance, predecessor, and timestamps. Structured facts such as language, edition, platform, characters, or game metadata belong in a namespaced `MetadataBundle` with per-field provenance; they are not silently flattened into prose.

Long descriptions are split into ordered `SemanticSegment` records. Segment embeddings point back to the document revision and subject, and search results return the matched segment and provenance.

Durable `Annotation.NOTE` revisions are also direct semantic sources. They remain annotations rather than being copied into `DescriptionDocument`; zvec stores their stable annotation ID, and query projection reopens the current durable revision and returns `source_type = ANNOTATION` plus its source ID.

An embedding model is not automatically a description generator. The config therefore has a semantic embedding profile and an optional description provider profile. A local description model or online model is selected explicitly, with egress policy and credential reference. Generated descriptions are retained as labeled evidence and never overwrite user facts.

### 8.2 Search feed

The complete baseline feed must cover:

- raw/display filename and path;
- entry type, suffix and magic evidence;
- full captured metadata and time/size facets;
- exact digest/length and duplicate groups;
- protection/recovery status and external locator metadata;
- tags, notes, user descriptions, imported descriptions, extracted text, and model descriptions;
- processing state, warnings, provenance, language, and available representations.

The query broker fuses lexical, structured, and semantic segment results. It aggregates segments to subjects without discarding the matched text or source. The local ONNX/BGE + zvec profile is the default; online and hybrid profiles are replaceable and require explicit egress authorization. A provisioned Darwin arm64 bundle passes real daemon and local WebUI ingest/dedup/Note/query/restart/restore tests. Startup performs a real inference probe and reopens the latest digest-compatible zvec generation; lease loss immediately marks the provider unavailable and closes active workers. An explicitly provisioned Linux arm64 environment also exercises the supervised worker and zvec lifecycle. Supported-install packaging, broader coverage evidence, and release/default qualification remain open.

### Exit tests

1. Search by filename, metadata, checksum, duplicate group, tag, note, extracted text, and description returns the same authorized subject.
2. A semantic hit identifies its matched durable Note or description segment and source provenance.
3. Deleting every index preserves descriptions, annotations, protection records, and recovery.
4. Switching local/online embedding profiles creates a new generation and does not rewrite old descriptions or vectors.
5. Semantic provider failure leaves lexical/structured search and exact recovery available with an explicit degraded state.

## 9. Phase 5: Simple Qualified Storage Savings

The first storage path is intentionally conservative:

1. SHA-256 plus logical length for identity.
2. Whole-content exact deduplication.
3. Lossless repository compression behind `RepositoryDriver`.
4. Optional content-defined chunking only after independent chunk readback and corruption tests.

The controller reports logical bytes, duplicate savings, compression savings, repository growth, metadata/index/model overhead, and net physical savings separately.

The current `local-zstd-v1` candidate implements items 1-3 with whole-file zstd and no external service, plus explicit host-owned repair and copy-forward profile migration. The copy-forward path retains the source repository identity, copies the known snapshot manifest placement, rejects unknown or leftover source entries, and has bounded source/target signed clean-reader list/verify/restore evidence; both readers can be reopened after the catalog is closed, target corruption is rejected, and the source remains independently restorable as rollback authority. Failed migrations stage beside the target without publishing a partial repository, and injected payload/record/pre-publish interruption boundaries can be retried safely. A raw fallback remains readable when the experimental zstd source profile is unavailable. Core-owned raw/zstd drivers also expose optional capability/health, read-only target validation, and placement-estimation seams. The reports keep the embedded reader, whole-file chunking, `NONE` encryption, `NOT_REQUIRED` key state, unknown capacity, and non-receipt physical estimates explicit; they do not initialize, mutate, or authorize placement. A separate `local-zstd-encrypted-v1` candidate now exercises an injected host `KeyProvider`, non-secret key references, AES-256-GCM protection around zstd payloads, authenticated decode-and-hash readback, missing/wrong-key health states, clean-reader dependency, relocation, corruption, and key-rotation copy-forward. Portable recovery records remain independently readable and secrets never enter plans or portable records. Neither profile is accepted by the generated configuration as a release default. This is enough to test exactness, repair, bounded migration reader closure, codec-loss fallback, encryption/key-boundary behavior, and gather corpus evidence, not enough to close the phase: chunking, destructive GC, full crash/migration rollback qualification, representative corpus measurements, packaging, and complete release reader closure remain open. The generated config therefore remains on `directory-cas-dev-v1` until a release profile is qualified.

Phase 5 does not begin with a selected repository brand. Every candidate is scored against the same dated qualification record: complete `RepositoryDriver` operations, immutable logical identity, exact readback, crash/corruption behavior, encryption and credential handling, reachability/GC ownership, repair, relocation, migration/rollback, supported-platform packaging, independently installable reader closure, measured net savings, and license/support risk. Exactly one tuple may become the release default only after all mandatory gates pass; if none passes, `RW-MVP-1` remains blocked rather than silently promoting `local-zstd-v1`, Kopia, Restic, or another candidate.

RWKV/Transformer prediction plus arithmetic/range coding is a later
`EXACT_REVERSIBLE` codec profile. It must implement probe, estimate, encode,
decode, verify, dependency closure, and migration fallback. It is not selected
by default and cannot retire raw/lossless fallback until cold decode, model
absence, corruption, upgrade, and net-saving tests pass. Any experimental UI
or README entry must warn that using it without a database/recovery backup or
the required decoder can make data unrecoverable.

### Exit tests

1. Identical bytes across different paths share exact identity while paths and metadata remain separate.
2. Lossless compression and chunking read back to the original digest and length.
3. Repository corruption is detected before a healthy claim.
4. Total dependency overhead is included in net-savings reports.
5. Removing an experimental codec does not make retained data unreadable.

## 10. Phase 6: Link Groups, Export, and Operator Ergonomics

Implement the user-facing organization loop:

```text
rw view create <name> --query <query>
rw view list
rw export create --view <name> --to-manifest <file>
rw export apply <manifest> --to <directory>
rw export verify <manifest> --to <directory>
```

Add one minimal LinkGroup path to the same typed core and adapters:

```text
create a named group from explicit file selections or one explicit directory
show its current file links and member health
add, remove, or rename links in one atomic update
freeze the current resolved contents into an ExportManifest
```

The durable shape is one stable group subject and one complete current mapping
from safe group-relative path to stable file `SubjectRef`. The LinkGroup has no
version number, revision row, predecessor/successor chain, or member history.
Files may occur in multiple groups without another repository placement.
Directory import is only a convenience that builds the flat current mapping
with relative paths; directories are not group members. The mapping is
unordered and its unique relative-path keys are the only membership authority.

The group does not pin `FileVersionId`, `SnapshotId`, content digest, or
representation. When a stable file subject is updated, the live group shows
the latest or last available admitted state that the ordinary subject resolver
can provide. If it cannot be resolved or read, the link stays visible as
`MISSING`/`UNAVAILABLE`; it does not silently disappear.
An empty group remains until explicit deletion. SQLite transactionality may
protect an update internally, but no concurrency token is exposed as a user
version concept.

Group name, tags, notes, and descriptions reuse the existing subject facts and
search feed. Do not add a group database, group index dimension, nested group,
member role, dependency graph, AI membership decision, ownership, or deletion
authority. Do not add group-owned binary attachments or make a generated
`.rwgroup` file the membership authority.

Because membership is durable and must survive catalog loss, Phase 6 starts
with three reviewed additions: group catalog/current-state portable records
and clean-reader support; host subject-kind authorization plus a replayable
group search feed; and an `ExportManifest` successor capable of resolving and
pinning each current link's `FileVersionId`, snapshot, representation, and
multi-component relative output path at export-plan time. The
current namespace-only subject resolver/search feed, legacy snapshot-entry
handle in export v1, single-component output name, and existing portable-fact
schemas retain their original meaning. Phase 6 must include old-reader,
migration, clean-import, corruption, missing-member, and path-collision
evidence before the group operation is reported as implemented.

Saved views remain dynamic. `ExportManifest` freezes membership, representations, output names, metadata/sidecar policy, config/profile digests, and verification requirements. Replaying a manifest is reproducible; reevaluating a view is not.

Normal commands SHOULD accept a LinkGroup, view, subject, path, or search
expression and SHOULD print stable human references. Internal
workspace/root/entry IDs remain available in JSON diagnostics but are not
required for the ordinary loop.

### Exit tests

1. One explicit directory builds one current file-only LinkGroup mapping with complete accounting and preserved safe relative paths; a later rescan or edit atomically replaces that current mapping without creating group history.
2. The same stable file may occur in two groups while exact placement and deduplication remain unchanged. Removing either link or deleting either group does not delete the file or change protection/GC eligibility.
3. Updating a linked file subject is reflected by the live group. Freezing the group resolves and pins the exact file versions into an `ExportManifest`; later file or group changes cannot change that manifest.
4. Missing members remain visible as unavailable, an empty group remains until explicit deletion, and neither state is silently collapsed.
5. Catalog and index removal followed by authenticated import reconstructs the same current group mapping and resolves every available member recovery reference; corruption, missing members, path traversal, and duplicate output paths fail closed.
6. View membership may change while an existing manifest does not.
7. Export destination collisions, unsafe reuse, symlink attacks, and metadata degradation are explicit.
8. Every materialized item has an exact or declared non-exact receipt.
9. Repeating verification returns the same byte evidence. Applying to a
   populated destination either reconciles an exact prior receipt through an
   explicitly admitted successor operation or fails closed and directs the
   operator to verify; it never overwrites merely because the manifest is the
   same.

## 11. Phase 7: Release Hardening

Only after phases 0-6 pass:

- select and qualify exactly one production repository profile and supported backend tuple from the Phase 5 evidence; no engine name is normative before that dated decision, and `local-zstd-v1` remains only a single-machine measurement candidate until it passes every applicable gate;
- package the local model, tokenizer, ONNX runtime, and zvec native library;
- implement native install, config migration, backup/restore, and upgrade checks;
- run the full `RW-MVP-1` acceptance corpus on local and NAS-like sources;
- publish measured search coverage, semantic latency, storage savings, recovery time, and resource limits.

### Exit tests

1. A clean supported host installs without Docker Compose or first-query downloads, validates the generated config, and reports the exact repository, embedding, vector, reader, and trust profiles.
2. The full ordinary loop succeeds: configure -> ingest plan -> review/apply -> fused search -> description/tag -> LinkGroup or saved view -> frozen manifest -> materialize/verify -> clean restore.
3. Removing SQLite and every index still permits authenticated clean discovery of committed recovery records and exact restore with an independently supplied trust anchor.
4. Removing the semantic generation produces explicit degradation; rebuilding it restores the same profile-bound coverage without changing subjects or durable descriptions.
5. Upgrade, interrupted upgrade, rollback, repository relocation, corruption, low-space, process crash, and reboot tests preserve or fail closed on every durable contract.
6. Published corpus results include logical bytes, whole-file duplicate savings, repository compression/chunk savings, physical growth, temporary space, catalog/index/model overhead, and net savings without double counting.
7. No release acceptance step depends on a facade, mount, OpenList, media client, internal database ID, or an online model.

## 12. Explicitly Deferred

These are not blockers for the core plan and must not be pulled forward:

- FUSE or any RestoreWeave mount service;
- SMB/WebDAV/NFS/S3 gateways;
- embedded players/readers or domain-specific UI;
- automatic source deletion, destructive GC, or writable synchronization;
- Qdrant/Milvus personal-use defaults;
- OpenList as a core dependency, storage engine, catalog authority, or fork base;
- OCR/ASR/CLIP/Chromaprint packs beyond the qualified baseline;
- P2P/reacquisition automation without a separate `RetrieverDriver` profile;
- RWKV/Transformer neural compression before the simple lossless profile is qualified;
- enterprise HA, multitenancy, or distributed control plane.
- richer Collections beyond the minimal file-only LinkGroup, including nested
  groups, member roles, dependency graphs, ratings, and automatic AI grouping.

## 13. Traceability Rule

Every implementation change must link to one phase, one normative requirement, one exit test, and one visible status. A fixture, facade handshake, or unit test may prove an interface shape, but it cannot mark a production provider, recovery contract, or user loop complete.

The following review rules are mandatory:

1. Work outside the currently unlocked phase is rejected unless it is a narrowly documented prerequisite for that phase; an adapter, extra index dimension, protocol method, or experimental codec is never such a prerequisite by assertion alone.
2. A status may advance only when the canonical matrix in [Content Store, Views, and Export Requirements](../requirements/content-store-views-and-exports.md) and the corresponding executable exit evidence change together.
3. Changing a frozen decision requires the product requirement, durable schema/config migration, compatibility analysis, and acceptance tests in the same reviewed change. Editing an informative plan cannot change scope.
4. Words such as “complete”, “default”, “available”, and “supported” must use the frozen status vocabulary. Fixture-contract and adapter-harness evidence remain explicitly prefixed and receive no release credit.
5. If a proposed feature does not improve the ordinary configure -> protect -> describe/search -> view/export -> verify/restore loop or close one of its safety gates, it stays outside the core queue.
