# Core Protocol and Reference Userland Technical Design

## 1. Product and implementation decision

RestoreWeave is a self-hosted, NAS-first content-aware managed data layer. It coordinates efficient exact storage, heterogeneous processing, intelligent discovery, file-shaped access, and verified recovery without binding durable meaning to one operating system, repository engine, model, or index. `RW-MVP-1` is its first read-only managed-archive profile.

The core product loop is:

~~~text
capture -> inventory
  |-> exact ingest and storage minimization
  |-> classify -> process -> derive -> index
-> publish authenticated content, fact, namespace, and recovery records
-> search, annotate, and save a dynamic view
-> freeze and materialize an export, read exact bytes, verify, or restore
-> incrementally rescan, reprocess, reindex, migrate, or rebuild projections
~~~

The reference distribution MUST ship a useful default path rather than an empty plugin framework:

- Scan ordinary directory trees through a qualified capture profile.
- Identify files from extension evidence and magic-byte evidence.
- Preserve unknown and unsupported readable content exactly.
- Store exact content through a mature repository engine with deduplication, compression, encryption, and packing.
- Reconstruct the original directory tree independently of the repository's physical layout.
- Verify published data through authenticated metadata and content reads.
- Expose stable CLI and local read-only MCP automation surfaces.
- Allow `Processor`, `IndexProvider`, `QueryProvider`, `CaptureDriver`, and `RepositoryDriver` implementations to evolve behind explicit contracts.

Linux snapshots, ZFS snapshots, Btrfs snapshots, SMB shares, NFS mounts, object stores, live directory scans, and other platform integrations are capture or storage profiles. None defines the product or its durable identity model.

RestoreWeave does not embed a general AI harness, agent loop, model router, vector database, or workflow engine. An AI model may be an optional processor, and an external harness may call the same typed commands as any other client.

Related normative boundaries are defined in:

- [Core Kernel and Interface Requirements](../requirements/core-kernel-and-interface.md).
- [Driver and Processor Interfaces](../requirements/driver-and-processor-interfaces.md).
- [CLI and MCP Contract](../requirements/cli-and-mcp-contract.md).
- [Namespace and Content Access](namespace-and-content-access.md).

## 2. Core authority and invariants

The kernel owns the meanings that must survive implementation replacement:

- Source, capture, namespace, file-version, content, representation, placement, snapshot, and operation identities.
- Immutable plans and explicit policy decisions.
- The distinction between observations, processor claims, user decisions, repository receipts, and verification evidence.
- Durable operation state, idempotency, cancellation, fencing, and reconciliation.
- Portable publication and clean-machine recovery records.
- Selection of the representation used for an exact read or restore.
- Incremental snapshot, deletion, retention, and garbage-collection semantics.
- Verification acceptance and truthful health reporting.

The following invariants apply to every adapter and profile:

1. The original namespace is logical data, not an accidental property of repository objects.
2. Physical deduplication, chunking, compression, encryption, packing, or transformation cannot change path or file-version identity.
3. Exact content identity and perceptual similarity are different claims.
4. The empty or default representation selection means the authoritative exact representation.
5. A derived, normalized, regenerated, downloaded, or perceptually similar representation is never substituted at an exact path without an explicit compatible recovery contract.
6. Unknown readable data defaults to exact preservation.
7. A processor reports typed evidence; it does not approve omission, publication, deletion, or verification.
8. A repository receipt proves a named placement effect; it does not define namespace truth.
9. A search index is a rebuildable projection and is never required for exact browse or restore.
10. Logical deletion and physical reclamation are separate operations.
11. Loss of SQLite, an index, a UI, or an AI service cannot make a published exact snapshot undecodable.
12. A successful repository write is not a published recoverable state until the required portable publication and verification gates pass.

## 3. Compatibility boundary

The following are intended to become stable public contracts after qualification:

- Recovery Record Format, abbreviated RRF.
- Typed Core Command command, result, reason, event, plan, artifact, and handle schemas.
- CLI command names, JSON and JSON Lines output, exit codes, and raw-content behavior.
- Read-only local MCP tool and resource mappings.
- `SnapshotTree` and `FileAccess` semantics.
- Content, representation, placement, and snapshot identity rules.
- Portable publication-commit and clean-recovery semantics.

The following extension contracts are normative semantic seams but MAY remain `v0alpha` until independently implemented and tested across versions:

- `CaptureDriver`
- `Processor`
- `RepositoryDriver`
- `IndexProvider`
- `QueryProvider`
- A later `RetrieverDriver`

The following remain private and unstable:

- Go packages and in-process interfaces.
- SQLite schemas and migrations.
- Worker topology, sockets, queues, caches, leases, and temporary handles.
- Repository-private chunk, pack, object, encryption, and compaction formats.
- Index schemas, vector dimensions, tokenizer choices, ranking algorithms, and model-serving APIs.
- Human-readable CLI layout and diagnostic prose.

No integration may require importing an `internal/` package, reading RestoreWeave SQLite tables, or decoding a repository's private pack layout.

## 4. Runtime topology

~~~mermaid
flowchart TB
    Human["Human operator"] --> CLI["CLI"]
    Automation["Script or external AI harness"] --> MCP["Local read-only MCP"]
    UI["Optional WebUI or REST adapter"] -.-> ABI["Typed Core Command ABI"]
    CLI --> ABI
    MCP --> ABI

    ABI --> Kernel["RestoreWeave storage kernel"]
    Kernel --> Planner["Deterministic planner"]
    Kernel --> Journal["Finite operation journal"]
    Kernel --> Records["Authenticated RRF store"]
    Kernel --> Access["SnapshotTree and FileAccess"]

    Journal --> Capture["CaptureDriver"]
    Capture --> Inventory["Namespace inventory"]
    Inventory --> ExactLane["Mandatory exact hash and identity lane"]
    ExactLane --> Repository["RepositoryDriver"]
    Repository --> ExactVerify["Host exact-lane readback and verification"]
    ExactVerify --> Records
    Inventory --> Processor["Optional Processor host"]
    Processor --> Staging["Host-controlled staging"]
    Staging --> Admission["Host validation and admission"]
    Admission -. "admitted representation" .-> Repository
    Admission --> Records
    Access --> Repository

    Records --> IndexProvider["Bundled baseline IndexProvider"]
    Access --> IndexProvider
    IndexProvider --> Generation["Named rebuildable index generation"]
    ABI --> QueryBroker["Core query broker"]
    QueryBroker --> QueryProvider["Bundled baseline QueryProvider"]
    QueryProvider --> Generation
    QueryProvider -. "ranked SubjectRefs" .-> QueryBroker
    QueryBroker --> Access
    Access --> Export["ExportManifest materializer"]

    Capture --> Sources["NAS, local filesystems, shares, snapshots, object views"]
    Repository --> Storage["Local disks, NAS pools, object stores, backup repositories"]
~~~

Arrows represent scoped calls, not transferred authority. Bindings do not implement policy or publication. Processors cannot invoke repository mutation directly. Query providers return candidates bound to authoritative identities; the core resolves and authorizes content access.

A self-hosted deployment SHOULD run the command dispatcher as a long-lived local controller for scheduling, indexing feeds, and concurrent clients. The same core MAY also be composed in-process for offline recovery or single-command maintenance. Both modes use one domain model and one journal protocol.

## 5. Identity model

### 5.1 Stable logical identities

| Identity | Meaning | Must not depend on |
| --- | --- | --- |
| `SourceRef` | Configured logical source and capture policy | Current mount point alone |
| `CaptureRef` | One immutable or explicitly qualified source view | A later live source head |
| `NamespaceRootRef` | Root of one authenticated file-shaped tree | Repository enumeration order |
| `NamespaceEntryRef` | One entry at one path in one namespace generation | Database row IDs |
| `FileVersionRef` | One recorded file state and metadata contract | Storage placement |
| `ContentRef` | Exact logical byte identity, normally algorithm-qualified digest plus length | Filename, extension, or perceptual score |
| `RepresentationRef` | One recoverable encoding of a subject | A mutable codec name alone |
| `PlacementRef` | One durable location and repository receipt for a representation | Search-index document IDs |
| `SnapshotRef` | One portable committed namespace and representation graph | Local publication pointers |
| `SubjectRef` | Stable target for annotations, extracted information, and search results | Display paths alone |

Repacking, repository migration, re-encryption, index rebuild, or cache eviction may change placements without changing content, file-version, namespace-entry, or snapshot identity.

### 5.2 Content and representation separation

`ContentRef` describes exact bytes. A `RepresentationRecord` describes how a subject can be materialized and validated. Representation kind, recovery claim, and lifecycle are separate dimensions. A representation records at least:

- Representation kind: `EXACT_RAW`, `EXACT_REVERSIBLE`, `APPROXIMATE`, or `DERIVED`.
- Recovery-claim reference using the subject, relation, validator, and policy vocabulary in the recovery-fidelity contract.
- Lifecycle class: `AUTHORITATIVE_DATA`, `RECOVERABLE_REPRESENTATION`, `REBUILDABLE_DERIVATIVE`, or `EPHEMERAL_CACHE`.
- Input subject and content references.
- Encoder or producer identity, version, parameters, and implementation digest where available.
- Required decoder and dependency closure.
- Expected output identity or validator contract.
- Losslessness and fidelity claims.
- Placement receipts and availability state.
- Verification evidence and last successful readback.

The initial profile publishes an authoritative `EXACT_RAW` or independently verified `EXACT_REVERSIBLE` representation for every included regular file. A later media or neural representation may coexist, but it does not replace the exact default unless a separately qualified policy explicitly permits that representation kind and recovery claim. A reacquisition recipe remains a source binding until acquired bytes are validated and placed.

### 5.3 Namespace-to-storage indirection

The logical read chain is:

~~~text
SnapshotRef
-> NamespaceRootRef
-> NamespaceEntryRef
-> FileVersionRef
-> selected RepresentationRef
-> decoder or reconstruction graph
-> one or more PlacementRefs
-> verified byte stream
~~~

This indirection is what allows physically packed, deduplicated, compressed, encrypted, or transformed storage to appear as the original directory tree.

## 6. Typed command dispatcher

The in-process shape may resemble:

~~~go
type Dispatcher interface {
    Execute(context.Context, CommandEnvelope) ResultEnvelope
    Events(context.Context, OperationRef, uint64, uint32) (EventPage, error)
}
~~~

The Go interface is private. Serialized behavior is the contract. Every request follows this sequence:

~~~text
decode
-> negotiate version
-> authenticate adapter principal
-> validate bounded input
-> authorize workspace and resource scope
-> enforce idempotency and expected revisions
-> execute one registered operation
-> validate typed result
-> render through the calling adapter
~~~

The registry is closed over named product operations. It MUST NOT accept arbitrary shell commands, SQL, plugin entry points, prompt execution, URLs, or a generic `run_tool` escape hatch.

Mutating commands bind an idempotency key to the digest of the canonical command body before the first external effect. Reuse with the same body returns the existing operation. Reuse with different input fails with `IDEMPOTENCY_CONFLICT`.

## 7. Deterministic planning and processing

### 7.1 Identification sequence

Identification is evidence aggregation, not a one-shot classifier:

1. Record extension or suffix hints without trusting them as fact.
2. Inspect bounded magic bytes and container signatures.
3. Optionally request `CLASSIFY_LEARNED` evidence for unknown, ambiguous, or policy-selected content.
4. Apply deterministic policy to select a provisional class.
5. Run only matching bounded `PARSE` capabilities when structural evidence can refine that class.
6. Preserve conflicts, confidence, inspected ranges, and producer provenance.
7. Publish the final classification and build the remaining processing and storage plan.

Learned classification is optional. Its absence, failure, or low confidence cannot make readable content disappear. A conflicting classification yields an explicit reason and exact fallback. A profile-specific processor requirement may block only that processing branch, derived representation, or stronger profile claim; it cannot block capture, inventory, host-owned exact hashing, exact placement eligibility, required exact-lane verification, or exact publication of readable bytes.

### 7.2 Processor extension point

A `Processor` is a bounded transformation or analysis unit, not a hosted agent. Processor capabilities may include:

- Learned classification.
- Parsing and structural enumeration.
- Metadata and text extraction or enrichment.
- OCR, transcription, captions, thumbnails, and previews.
- Auxiliary cryptographic checksums and perceptual, acoustic, visual, semantic, or structural comparison features; canonical content identity remains host-owned.
- Compression, transcoding, normalization, or neural encoding.
- Representation validation.
- Provider-neutral index-preparation artifacts, embeddings, or other typed features.

Every invocation binds:

- Processor identity, version, capability, and implementation digest.
- Input `SubjectRef`, `FileVersionRef`, `ContentRef`, or bounded content handle.
- Immutable configuration and parameter digest.
- Resource, time, network, secret, filesystem, and accelerator grants.
- Cancellation token and output-size limits.

Every result contains typed output, coverage, provenance, warnings, measurements, and claimed contracts. The host validates the result before it can affect a plan. A processor cannot approve its own representation, place data, publish a snapshot, delete a source, or authorize garbage collection.

### 7.3 Planning result

For the same capture, policy, repository capabilities, processor results, and prior snapshot, the planner MUST produce the same canonical plan. The plan includes:

- Complete observed scope or explicit failures.
- File-type evidence and conflicts.
- Exact, derived, duplicate, and unsupported byte counts.
- Selected processing graph and fallback for each class.
- Expected representation and placement effects.
- Estimated storage, transfer, compute, and index cost.
- Required decoders and long-term dependencies.
- Incremental reuse from a prior committed snapshot.
- Risks, blocked decisions, and explicit human-authority requirements.
- Plan digest, policy revision, capture digest, and target capability revision.

Plans are immutable. Review produces a successor plan rather than mutating an accepted plan in place.

## 8. Finite operation journal

The operation journal provides durable, replayable state for bounded product operations. It is not a general workflow engine.

Initial nonterminal states are:

- `QUEUED`
- `RUNNING`
- `WAITING_EXTERNAL`
- `RECONCILING`
- `CANCELLING`

Initial terminal states are:

- `SUCCEEDED`
- `DEGRADED`
- `BLOCKED`
- `FAILED`
- `CANCELLED`
- `UNKNOWN_EXTERNAL_OUTCOME`

Every externally visible effect has:

- Operation and attempt identity.
- Canonical request digest and idempotency binding.
- Expected resource revision.
- Lease and fencing token when mutation is possible.
- Intent event before invocation.
- Receipt or typed failure after invocation.
- A reconciliation method for timeout or process loss.

The core never infers success from a missing response. If a driver cannot prove whether a placement, publication, or deletion committed, the job enters reconciliation and may terminate as `UNKNOWN_EXTERNAL_OUTCOME`.

## 9. Repository and placement protocol

### 9.1 Repository responsibility

A `RepositoryDriver` owns its physical implementation:

- Chunking, packing, compression, and encryption.
- Private object names and internal indexes.
- Atomic-write and consistency mechanisms.
- Repository-native checks and repair.
- Physical compaction and garbage collection.

RestoreWeave owns:

- Logical snapshot, namespace, file-version, content, and representation identities.
- The plan and accepted recovery contract.
- Placement roles and portable receipts.
- Which representation satisfies an exact read or restore.
- Publication, verification acceptance, and lifecycle intent.

### 9.2 Placement receipts

A placement receipt binds at least:

- Repository identity and capability revision.
- Representation identity and digest.
- Placement role and subtype.
- Immutable repository locator or restore-point reference.
- Stored-byte observations when the repository can report them.
- Operation attempt, generation, and fencing token.
- Reconciliation evidence.

Repository-private locators are never exposed as content identity. Multiple placements may satisfy one representation. One repository remains one failure domain even if it stores many packs or replicas internally.

### 9.3 Required placement roles

The portable protocol reserves:

- `PAYLOAD` for admitted recoverable representations.
- `RECOVERY_CLOSURE` with subtype `PREPARED_CLOSURE`.
- `RECOVERY_CLOSURE` with subtype `PUBLICATION_COMMIT`.

Additional repositories and representations reuse these logical roles rather than inventing a second publication model.

## 10. Portable publication commit

Publication separates data placement, recoverability proof, and local visibility.

The canonical sequence is:

1. Persist and verify all required capture, observation, namespace, plan, representation, placement, and dependency records.
2. Reconcile every required `PAYLOAD` placement receipt.
3. Validate namespace-to-representation coverage and the selected recovery contract.
4. Complete the required authenticated-metadata verification gate.
5. Build and sign an RRF root over the exact required recovery closure.
6. Store and reconcile a `PREPARED_CLOSURE` containing the root, required records, reader and decoder requirements, repository configuration without plaintext secrets, signatures, and clean-recovery instructions.
7. Allocate a publication generation and current fencing token.
8. Construct and sign a `PublicationCommitRecord` binding the snapshot identity, RRF root digest, plan and capture digests, required payload receipts, prepared-closure receipt, accepted verification evidence, publication generation, and fence.
9. Store and reconcile that record as `PUBLICATION_COMMIT`.
10. Treat the reconciled valid commit-marker placement as the portable logical commit point.
11. Append the local `SNAPSHOT_PUBLISHED` event and atomically project a local publication pointer.
12. Update rebuildable operational and search projections.

The commit record does not contain the receipt of its own placement. That avoids self-reference. Its signed content plus the authenticated placement receipt proves where it was stored.

A payload or prepared closure without a valid commit marker is an orphan, not a published snapshot. A local pointer or SQLite row without a valid portable commit is not publication authority. A clean machine discovers commit-marker candidates, verifies the signature against an independently supplied trust anchor, follows the bound prepared closure and payload receipts, and reconstructs the namespace without the original database or index.

For one snapshot identity and publication generation, conflicting signed commit-record digests are a hard integrity conflict. Equivalent physical copies of the same record may reconcile to one logical commit.

## 11. Incremental update, deletion, retention, and garbage collection

### 11.1 Incremental snapshots

Every published snapshot is immutable. An update creates a successor generation that may reuse prior content, representations, and placements by identity.

Incremental planning compares the new qualified capture with a selected base snapshot using, in order of reliability:

- Stable source and filesystem entry identity where available.
- Recorded metadata and change tokens.
- Exact content digests.
- Chunk or representation reuse reported by the repository.

Metadata-only changes create new file-version or namespace records without duplicating unchanged exact content. Renames change namespace bindings and may reuse the same file version or content identity when the capture evidence supports that conclusion. Algorithm upgrades may add a new representation without rewriting historical namespace records.

### 11.2 Deletion semantics

Source disappearance in a successor capture means the entry is absent from the new namespace generation. It does not erase the entry from historical snapshots and does not immediately delete stored bytes.

RestoreWeave distinguishes:

- Source deletion observed in a new capture.
- Snapshot retirement under retention policy.
- Representation retirement after a safer or newer representation is accepted.
- Placement retirement for migration or backend removal.
- Physical repository garbage collection.
- Index or cache deletion.

Each has a separate plan, authority check, durable event, and outcome. A search index may remove a retired subject from its current view while historical RRF records remain governed by retention.

### 11.3 Reachability and garbage collection

Physical reclamation is a gated later mutation. Before requesting repository GC, the core MUST compute an authenticated reachability closure from:

- Every retained published snapshot.
- Active plans, jobs, reads, exports, and restore leases.
- Required recovery closures and verification evidence.
- Retention locks, legal or administrative holds when supported, and policy-pinned representations.
- Migration and repair safety windows.

Only placements outside that closure are GC candidates. The candidate set and policy revision are immutable and digest-addressed. Execution requires explicit authority, a lease and fencing token, repository preflight, and post-GC reconciliation. The driver performs physical collection and returns a receipt; the core records what became unreachable and re-verifies retained snapshots as required.

No automatic processor, index provider, query provider, or AI client may authorize deletion of the last accepted exact representation. Destructive lifecycle operations remain disabled in the initial read-only automation profile.

## 12. Namespace, read, restore, and verification path

`SnapshotTree` and `FileAccess` are the stable file-shaped narrow waist.

`SnapshotTree` provides:

- Authenticated root lookup.
- Component-by-component path resolution.
- Paginated directory listing.
- Recorded metadata, hard-link, symbolic-link, sparse-file, ACL, and extended-attribute information where supported.

`FileAccess` provides:

- Core-authorized representation selection.
- Bounded sequential and range reads.
- Decoder and reconstruction execution through typed handles.
- Integrity checks against the selected representation and expected exact content.
- Read-session budgets, expiry, cancellation, and audit evidence.

When a selected representation is not raw, `FileAccess` invokes the pinned transform profile through `DECODE_REPRESENTATION`. The decoder receives only a bounded encoded-representation source, the exact dependency closure, a requested decoded range, and budgets. It receives no original-source handle, repository-selection authority, or policy authority. The host meters encoded and decoded bytes and independently verifies the decoded length and digest at the coverage level actually read.

An exact restore stages data outside the final destination, materializes the original tree, validates bytes and declared metadata, and publishes the destination only after checks pass. Unsupported metadata produces an explicit degraded result or blocks the restore according to the selected contract. A partial range read proves only the bytes and representation scope actually checked.

## 13. Index and query extension points

Intelligent discovery is product value, but search state is not recovery authority.

### 13.1 IndexProvider

An `IndexProvider` builds or updates one named, versioned, rebuildable index generation from authorized change feeds containing `SubjectRef`, namespace metadata, extracted text, typed annotations, fingerprints, thumbnails, captions, embeddings, or other processor-produced artifacts. It returns the exact index-generation reference, complete input revision, coverage, provider version, and warnings.

The feed is replayable. Rebuilding or replacing an index does not alter any snapshot. An index implementation may be full-text, vector, graph, media-specific, or hybrid.

### 13.2 QueryProvider

A `QueryProvider` accepts a bounded query against exactly one explicitly named `IndexGenerationRef` per invocation. It owns retrieval, scoring, ranking, and any lexical, structured, vector, or media-signal fusion within that generation. It returns candidates containing authoritative `SubjectRef` values, the exact index-generation reference, scores, explanations, provider revision, and optional matched derivative references. The host query broker may fuse separately generation-pinned provider results. There is no separate ranker or embedding-provider contract. It MUST NOT return repository credentials or bypass core authorization.

The core resolves an active-generation selector to one exact `IndexGenerationRef`, validates provider and generation compatibility before invocation, and binds pagination to that generation. It then resolves each candidate against the caller's current snapshot, namespace, and access policy. Authorized results expose stable subject, path, file-version, content, representation, and verification references where applicable. Query results may be stale, approximate, or incomplete and MUST identify their exact indexed revision. Opening bytes always proceeds through `FileAccess`.

One implementation MAY implement both `IndexProvider` and `QueryProvider`, but the build/update contract and query contract remain independently versioned and replaceable.

### 13.3 No embedded harness

RestoreWeave supplies typed processor, index-feed, query, CLI, and MCP boundaries. It does not own prompt memory, autonomous loops, model selection, or multi-agent orchestration. External harnesses compose these interfaces as clients.

## 14. Reference userland

The reference distribution MUST include:

- A self-hosted controller process suitable for a NAS or home server.
- A recovery-capable CLI that can operate against the controller or compose the core locally.
- A local, bounded, read-only MCP adapter over the same command dispatcher.
- Filesystem and share capture drivers appropriate to the host platform.
- One qualified repository driver with exact deduplicated and compressed storage.
- A built-in suffix and magic-byte detector.
- Bundled lexical/structured `IndexProvider` and `QueryProvider` implementations over paths, filenames, types, recorded metadata, checksums, duplicate groups, protection state, durable tags/notes/descriptions, and safely extracted text, backed by a replayable feed.
- A bundled local text-embedding `Processor` and in-process zvec generation for the default semantic dimension; its failure is explicit degradation and never a recovery failure.
- Durable tag and note CRUD plus portable annotation export/import.
- Export-manifest materialization over `SnapshotTree` and `FileAccess`; mounting is external.
- Optional processor-produced CLIP, alternate embedding spaces, fingerprints, and media features that compatible index and query implementations may project later.
- RRF storage, portable export, clean-recovery tooling, namespace browse, content reads, verification, and exact restore.

Optional WebUI and REST services are adapters over the same typed operations. They do not own a second scheduler, policy model, database authority, or publication state machine.

### 14.1 Suggested implementation layout

~~~text
cmd/restoreweave/                 CLI, controller, and recovery entry points

internal/app/                     composition root and configured profiles
internal/command/                 typed dispatch and authorization
internal/planner/                 deterministic ingest, restore, and lifecycle plans
internal/operation/               reducers, leases, fencing, and reconciliation
internal/journal/                 append-only durable operation log
internal/rrf/                     canonical records, roots, signatures, and export
internal/identity/                content, representation, namespace, and snapshot IDs
internal/capture/                 CaptureDriver host
internal/processor/               Processor host and result validation
internal/repository/              RepositoryDriver host
internal/publication/             portable publication protocol
internal/namespace/               SnapshotTree implementation
internal/readsvc/                 FileAccess and bounded reads
internal/verification/            evidence validation and acceptance
internal/lifecycle/               retention, retirement, reachability, and GC plans
internal/indexfeed/               replayable feed and IndexProvider coordination
internal/query/                   QueryProvider broker and result authorization
internal/binding/cli/             human, JSON, JSONL, and byte-stream binding
internal/binding/mcp/             local read-only MCP binding
internal/binding/http/            optional REST and WebUI adapter
internal/store/sqlite/            rebuildable operational projection

spec/core/v1/                     public command and event schemas
spec/rrf/v1/                      RRF schemas and canonicalization vectors
spec/extensions/                 qualified external process protocols
testdata/conformance/             cross-binding and clean-recovery fixtures
~~~

The layout is a direction, not a public Go package promise.

## 15. Security boundary

- Adapters authenticate transport credentials and forward immutable claims. The core derives the effective actor and workspace; request bodies cannot self-assert roles or capabilities.
- Drivers and processors receive short-lived, least-privilege handles rather than ambient filesystem or repository access.
- Network, secrets, accelerators, host paths, and staging writes require separate explicit grants.
- Large bytes use bounded streams or file descriptors, not ordinary JSON or MCP messages.
- Repository credentials and signing keys never appear in plans, processor results, indexes, or query results.
- Query and index feeds are filtered by workspace and subject authorization.
- Path resolution is component-based and cannot escape the authenticated snapshot root.
- Restores stage into an authorized empty or explicitly prepared destination.
- Publication and destructive lifecycle effects require leases, fencing, and durable idempotency.

## 16. Verification model

Verification levels remain distinct:

- `AUTHENTICATED_METADATA`: RRF closure, signatures, placement receipts, and namespace coverage.
- `SAMPLED_CONTENT`: a declared quantitative sample read through the complete repository and decoder path.
- `FULL_BYTES`: every selected exact byte read and compared with its expected content identity.
- `RESTORE_DRILL`: selected paths materialized and checked in a clean destination.
- `CLEAN_RECOVERY`: recovery bootstrapped from the portable commit and independent trust material without the original operational database.

An index query, processor success, repository-native check, or local cache hit cannot be reported as a stronger level than it proves. Verification records are append-only evidence; they do not rewrite the immutable publication commit.

## 17. Delivery sequence

### Phase 0: protocol foundations

- Freeze identity and RRF canonicalization rules.
- Implement finite operation journal, receipts, reconciliation, and portable publication.
- Implement `SnapshotTree` and `FileAccess` over an exact repository profile.
- Validate clean recovery without SQLite or an index.

### Initial self-hosted product

- Qualify at least one common NAS or local-filesystem capture path on supported hosts.
- Ship exact, deduplicated, compressed, encrypted storage through one qualified repository driver.
- Ship suffix and magic-byte identification with exact fallback.
- Ship incremental immutable snapshots, browse, range read, full restore, and verification.
- Ship export-manifest materialization pinned to immutable subjects and representations.
- Ship the full authorized CLI operation set and equivalent semantics for the bounded operations exposed by local read-only MCP.
- Ship bundled lexical `IndexProvider` and `QueryProvider` implementations, durable tag/note CRUD and portable export/import, `search.query`, and a replayable metadata and extracted-text feed.

### Later profiles

- Additional filesystem and application-consistent capture drivers.
- Rich parsers, media fingerprints, OCR, speech, and processor-produced CLIP or embedding features with compatible index and query implementations.
- Collections, ratings, relationship graphs, recommendations, and other richer catalog semantics.
- Alternative compression and neural representation processors.
- Multiple repositories, placement migration, retention execution, and gated physical GC.
- External SMB, NFS, WebDAV, S3-compatible, media-server, and other consumers of materialized exports or authorized reads.
- Retrieval-backed representations and explicitly qualified non-exact recovery contracts.

## 18. Definition of done

The core protocol and reference userland are conforming when:

1. A heterogeneous source can be captured through the qualified Linux/NAS path without any optional platform-specific global assumption.
2. A deterministic plan records suffix, magic-byte, and optional processor evidence separately.
3. Exact data is physically deduplicated or packed yet browses through its original directory paths.
4. The same immutable file version can be read through CLI and MCP with equivalent authorization and bytes.
5. A clean machine reconstructs a committed snapshot from a valid portable commit and independent trust anchor without SQLite, a search index, or AI service.
6. Incremental successor snapshots reuse unchanged content while preserving historical path views.
7. Observed source deletion does not destroy retained historical data.
8. A simulated GC cannot select any placement reachable from retained snapshots, active leases, or recovery closures.
9. Processor, index, query, repository, and capture failures remain typed and do not silently weaken exact recovery.
10. REST or WebUI adapters can be removed without changing core operation meaning or recovery behavior.
11. A fresh reference installation can search paths, metadata, durable tags and notes, and available extracted text through one exact generation-bound query without installing an LLM, embedding model, or vector database.
12. Export-manifest materialization returns the same exact bytes and namespace metadata as `SnapshotTree` and `FileAccess`, remains manifest-pinned, and reports conflicts without substitution.
13. Tag and note export/import preserves subject bindings and revisions after loss of SQLite and every search index.
