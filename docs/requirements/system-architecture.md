# System Architecture

## 1. Architectural objective

RestoreWeave is a self-hosted, NAS-first content-aware managed data layer. It coordinates storage minimization, heterogeneous content processing, intelligent discovery, authenticated original-path access, and verified recovery through replaceable implementations. `RW-MVP-1` is its first read-only managed-archive and search profile.

The reference deployment is a Linux-based NAS or server. Platform- and filesystem-specific source capture remains isolated behind independently qualified profiles; no capture mechanism defines the product-wide data model or release.

The architecture follows a kernel-and-userland model:

- A small authoritative core owns identities, accepted decisions, provenance, transactions, verification, namespace meaning, and recovery semantics.
- Versioned extensions own algorithms that should improve independently. The stable seams are `CaptureDriver`, capability-oriented `Processor`, `RepositoryDriver`, `IndexProvider`, `QueryProvider`, and the later `RetrieverDriver`. Learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation are conceptual roles within `Processor`, not separate public protocols.
- A complete reference distribution ships strong defaults. RestoreWeave MUST be useful without installing third-party plugins, an AI model, a WebUI, or an external automation harness.

The core MUST retain an exact, verifiable path for readable bytes when an optional processor or provider is absent, fails, or does not understand the content. Storage reduction and semantic enrichment are first-class product values, but neither may silently weaken recoverability.

The normative authority boundary and compatibility policy are defined in [Core Kernel and Interface Requirements](core-kernel-and-interface.md). Command bindings are defined in [CLI and MCP Contract](cli-and-mcp-contract.md), extension invocation details in [Driver and Processor Interfaces](driver-and-processor-interfaces.md), and the current implementation mapping in [Core Protocol and Reference Userland](../technical/core-protocol-and-reference-userland.md).

## 2. Product shape

RestoreWeave presents one authenticated original-path namespace and one durable subject model across heterogeneous sources, representations, storage targets, processing outputs, and discovery indexes. `RW-MVP-1` projects that namespace through a read-only managed-archive profile.

For an operator, the product provides:

- A self-hosted controller suitable for a NAS or home server; workstation and enterprise profiles are secondary extensions.
- Policy-driven ingestion of ordinary directory trees and qualified point-in-time source views.
- Exact content-addressed storage with deduplication and lossless compression by default.
- A recoverable original directory tree independent of pack, chunk, object, or repository layout.
- Metadata, path, type, checksum, duplicate, durable tag/note, processing-state, and extracted-text search in the reference distribution.
- A bundled read-only Linux FUSE view over the authenticated original-path namespace.
- Exact browse, bounded reads, verification, recovery export, and clean restore even when every optional model and search derivative is unavailable.
- CLI and local read-only MCP interfaces for humans and automation clients; optional WebUI and REST adapters may later bind the same operations.

RestoreWeave does not require a visual node editor. A UI may expose presets, checkboxes, policies, and expert overrides, while the controller selects bounded processing routes. Additional placement targets, semantic providers, multimodal processing, and alternate gateways are staged capabilities over the same durable identities.

## 3. Authority boundary

### 3.1 Authoritative core

The authoritative core owns:

- Stable identities for sources, source views, namespace entries, file versions, byte content, chunks, representations, snapshots, indexes, operations, policies, and placements.
- Immutable accepted plans and policy decisions, including any explicit decision to use a non-exact representation.
- The evidence-to-decision boundary for content classification and processor selection.
- Provenance linking every derived fact or representation to its inputs, implementation, version, parameters, and validation result.
- Durable transactions, idempotency, cancellation, leases, fencing, checkpoints, reconciliation, and typed terminal outcomes.
- Verification meaning and the decision that a representation or placement satisfies a declared contract.
- Publication of portable records and commit evidence.
- Original namespace reconstruction, representation selection, content access, and restore semantics.
- Capability grants and resource limits for external extensions and clients.

These semantics remain stable even when every algorithmic implementation changes.

### 3.2 Replaceable extensions and adapters

RestoreWeave exposes exactly these stable logical extension seams:

- `CaptureDriver` presents bounded source views and consistency evidence.
- `Processor` advertises typed capabilities for learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation.
- `RepositoryDriver` places, reads, verifies, and restores admitted representations and portable records while owning backend-private packing and transport.
- `IndexProvider` builds and updates named, versioned, rebuildable discovery generations.
- `QueryProvider` queries exactly one explicitly named `IndexGenerationRef` per invocation and returns stable subject references with score provenance. Compatibility is validated before invocation.
- A later `RetrieverDriver` may reacquire content from pinned external sources.

One executable or package MAY implement several seams. In particular, one implementation MAY provide both `IndexProvider` and `QueryProvider`, but the two capabilities remain separately negotiated, versioned, authorized, and bound to named index generations.

Learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation remain useful logical pipeline stages. They are `Processor` capability roles, not separate stable public wire ABIs. Placement is performed through `RepositoryDriver`; index generation through `IndexProvider`; within-generation query planning and ranking through `QueryProvider`; cross-provider fusion through the host-owned query broker.

RestoreWeave also delegates the following presentation and control concerns without making them stable storage-algorithm seams:

- FUSE, SMB, NFS, WebDAV, S3-compatible, media-server, and other presentation gateways.
- WebUI, REST, desktop, automation, and fleet-management adapters.

An extension may report evidence, candidates, derived outputs, costs, or receipts. It cannot change an accepted plan, grant itself authority, publish a snapshot, accept its own validation as final truth, or silently replace exact content.

### 3.3 A thin core is not an empty framework

A release is not conforming merely because it exposes plugin interfaces. The reference distribution MUST include an end-to-end default pipeline that can ingest supported sources, identify common content, preserve unknown content exactly, deduplicate, losslessly compress, place, verify, search, browse, and restore data.

## 4. Runtime topology

~~~mermaid
flowchart TB
    Human["Human operator"] --> CLI["CLI"]
    Automation["External automation client"] --> MCP["Local read-only MCP adapter"]
    UI["Optional WebUI or REST adapter"] --> Commands["Versioned Core Command ABI"]
    CLI --> Commands
    MCP --> Commands

    Commands --> Core["Authoritative RestoreWeave Core"]
    Core <--> Journal["Transaction journal and decision ledger"]
    Core <--> Records["Portable RRF records"]
    Core <--> Namespace["SnapshotTree and FileAccess"]
    Core <--> Registry["Versioned extension registry"]

    Sources["Local, NAS, snapshot, remote, or object sources"] --> Capture["CaptureDriver"]
    Capture --> Inventory["Namespace inventory"]

    Inventory --> ExactHash["Host exact hash and identity"]
    ExactHash --> ExactPlace["RepositoryDriver: mandatory exact lane"]
    ExactPlace --> ExactVerify["Host readback and exact-lane verification"]
    ExactVerify --> Records

    Inventory --> Suffix["Built-in suffix evidence"]
    Suffix --> Magic["Built-in magic-byte evidence"]
    Magic --> Route["Host classification and ProcessingRoute"]
    Magic -. "optional" .-> Learned["Processor: CLASSIFY_LEARNED"]
    Learned --> Route
    Route -. "selected typed capabilities" .-> Processors["PARSE / EXTRACT / ENRICH / FINGERPRINT / TRANSFORM / VALIDATE / INDEX_PREPARE"]
    Processors --> Stage["Host-controlled staging"]
    Stage --> Admission["Host validation and admission"]
    Admission -. "admitted representation" .-> DerivedPlace["RepositoryDriver"]
    Admission --> Records

    Core <--> Capture
    Core <--> Suffix
    Core <--> Magic
    Core <--> Learned
    Core <--> Route
    Core <--> Processors
    Core <--> Stage
    Core <--> Admission
    Core <--> ExactPlace
    Core <--> ExactVerify
    Core <--> DerivedPlace

    ExactPlace <--> Storage["Local disks, NAS pools, repositories, or object storage"]
    DerivedPlace <--> Storage
    Records --> Index["IndexProvider"]
    Namespace --> Index
    Index <--> SearchStore["Replaceable search stores"]
    QueryRequest["Adapter query request"] --> QueryBroker["Host-owned query broker"]
    QueryBroker --> Generation["Resolve one exact IndexGenerationRef"]
    Generation --> Rank["QueryProvider: query and within-generation ranking"]
    SearchStore --> Rank
    Rank --> Reauthorize["Broker resolves and reauthorizes subjects"]
    Reauthorize --> QueryResponse["Authorized adapter response"]

    Namespace --> Fuse["Bundled read-only Linux FUSE"]
    Namespace --> Gateways["Later alternate gateways"]
    Retrieval["Later RetrieverDriver"] --> Core
~~~

Control requests use typed references and bounded handles. Large byte streams travel through scoped file descriptors, range readers, staging objects, or backend-native streams rather than ordinary CLI, MCP, or REST messages.

The diagram is a logical topology. Stages may run in-process, in isolated local processes, in containers, on accelerator nodes, or behind remote services. Deployment changes do not transfer authority away from the core.

## 5. Stable identity and record model

Logical identity is independent of physical placement and algorithm choice.

At minimum, the core distinguishes:

- `SourceId`: a configured origin.
- `SourceViewId`: one captured or validated observation boundary.
- `NamespaceEntryId`: one entry in an immutable namespace snapshot.
- `FileVersionId`: one observed file version, including declared filesystem metadata.
- `ContentId` (catalog `ContentRef`): exact byte identity, based on a required cryptographic digest.
- `ChunkId`: exact identity within a named chunking profile.
- `RepresentationId` (catalog `RepresentationRef`): exact raw bytes or a derived encoding with provenance and decoder requirements.
- `SnapshotId`: one published logical namespace and its selected representations.
- `PlacementId`: one durable physical placement of a representation or portable record.
- `IndexGenerationId`: one rebuildable index projection bound to source records, `IndexProvider` capability profile, and model space.
- `OperationId`, `PlanId`, and `DecisionId`: durable control and authority records.

Rechunking, repacking, recompression, moving between storage tiers, rebuilding an index, changing a `QueryProvider`, or upgrading a `Processor` capability MUST NOT change a content or namespace identity. A transformation creates a new representation and provenance edge; it does not rewrite history.

The portable Recovery Record Format, or RRF, carries the authenticated identity graph, namespace, decisions, provenance, placement receipts, verification evidence, annotations, and publication records required for clean recovery. Operational databases and indexes are projections and can be rebuilt.

## 6. Versioned extension system

### 6.1 Public seam inventory

| Public seam | Responsibility | Reference-product requirement |
| --- | --- | --- |
| `CaptureDriver` | Present one bounded source view with explicit consistency and lifecycle evidence | At least one supported source profile is required |
| `Processor` | Execute one typed, capability-oriented content operation | Strong deterministic baseline processors are bundled; specialized and AI processors are optional |
| `RepositoryDriver` | Place, read, verify, reconcile, and restore admitted representations and portable records | At least one qualified deduplicating and compressing implementation is required |
| `IndexProvider` | Build or update named, versioned discovery generations from an authorized replayable feed | A baseline metadata and text implementation is required |
| `QueryProvider` | Query exactly one explicitly named `IndexGenerationRef` per invocation and return stable subject-bound candidates; compatibility is validated before invocation | A baseline metadata and text implementation is required |
| `RetrieverDriver` | Later reacquire bytes from a pinned external source | Reserved for a later qualified profile |

One package MAY implement multiple interfaces. An implementation that provides both `IndexProvider` and `QueryProvider` still negotiates, versions, and authorizes them separately.

### 6.2 Common capability discipline

Every public seam negotiates a canonical `CapabilityProfile`. A `Processor` invocation uses the authority-limited logical envelopes below; role-specific driver and provider operations carry the same implementation identity, version, digest, capability, budget, cancellation, provenance, and reconciliation rules.

~~~text
CapabilityProfile
- interface family, capability role, and API version
- implementation identity, version, and immutable digest
- supported input and output schemas or media classes
- determinism and reproducibility claims
- resource, accelerator, network, filesystem, and secret requirements
- decoder and migration requirements

ProcessInvocation
- operation, plan, and invocation identities
- exact capability-profile digest selected by the plan
- typed input references and scoped byte handles
- policy constraints and expected output contract
- deadline, cancellation, budgets, and capability grants
- output staging handles

ProcessResult
- typed status and reason codes
- inspected and processed coverage
- facts, candidates, staged representations, or measurements
- provenance and dependency records
- measurements, confidence, warnings, and validation evidence
- no authority to accept, publish, delete, or weaken data
~~~

Implementations with different major API versions may coexist. A plan pins an interface family, capability profile, implementation digest, and semantic configuration. Installing a newer extension affects no in-flight operation and does not reinterpret an existing result. Old decoders remain available while a retained representation depends on them, or the data must be migrated and reverified before removal.

### 6.3 Conceptual Processor roles

The table below describes routing roles inside the capability-oriented `Processor` interface. These names are not independent public plugin families or wire ABIs.

| Processor capability role | Responsibility | Required conservative behavior |
| --- | --- | --- |
| `CLASSIFY_LEARNED` | Add optional learned or domain-specific type evidence after built-in suffix and magic-byte evidence | Return unknown or conflicting evidence; never authorize omission |
| `PARSE` | Validate structure, enumerate members, and refine typed coverage | Report partial or failed coverage; never make exact storage depend on parser success |
| `EXTRACT` | Produce metadata, text, frames, waveforms, symbols, archive listings, or other typed derivatives | Preserve coverage and provenance; failure leaves source bytes eligible for exact storage |
| `ENRICH` | Attach external or model-derived descriptions and relationships | Preserve source attribution and conflicts; never convert an external claim into identity or authority |
| `FINGERPRINT` | Produce auxiliary checksums and structural, perceptual, acoustic, visual, or semantic comparison features | Label identity strength; canonical content identity and required exact digests are computed or independently verified only by the host-owned exact lane |
| `TRANSFORM` | Produce raw, chunked, compressed, normalized, transcoded, neural, or packaged representation candidates | Declare exactness, decoder requirements, and reversibility |
| `VALIDATE` | Measure byte equality, structural validity, perceptual similarity, decode success, or policy-specific quality | Report evidence; the core makes the acceptance decision |
| `INDEX_PREPARE` | Convert admitted facts and derivatives into provider-ready records | Preserve SubjectRef, schema, coverage, and producer generation; never write an index directly |

Suffix and magic-byte inspection are host-owned baseline stages, not `Processor` roles. The role set above is the canonical `Processor` stage vocabulary. `RepositoryDriver`, `IndexProvider`, `QueryProvider`, and later `RetrieverDriver` remain distinct because they own stateful external effects or generation/query semantics that do not fit a bounded content-processing result.

### 6.4 Detection and dispatch

The default classifier uses ordered evidence, not filename extensions alone:

1. Collect suffix and path-context evidence.
2. Inspect magic bytes and structural signatures.
3. Optionally call `CLASSIFY_LEARNED` processors for unknown, ambiguous, or policy-selected content.
4. Preserve every evidence item and conflict.
5. Apply a versioned provisional classification policy.
6. Run only safe, matching `PARSE` capabilities when structural evidence can refine that decision.
7. Publish the final versioned classification and build the remaining `ProcessingRoute`.

Suffix and magic evidence MUST remain independently visible. A learned classifier or parser may improve classification but is never required to preserve readable content. Low confidence, conflict, unsupported types, encrypted content, and processor failure select the generic exact-byte profile. A profile-specific processor requirement MAY block only that processing branch, derived representation, or stronger profile claim; it MUST NOT block capture, inventory, host-owned exact hashing, exact placement eligibility, required exact-lane verification, or exact publication of readable bytes.

## 7. Strong default pipeline

The reference exact profile is the baseline product, not a demonstration plugin. Exact storage is a mandatory lane that does not wait for optional classification, extraction, transformation, or indexing work. It performs:

1. Capture or validate a source view with an explicit consistency claim.
2. Inventory the original namespace and filesystem metadata.
3. Start the mandatory exact lane: compute host-owned full-content identity, reuse trusted exact content where allowed, and place a raw or repository-native losslessly compressed representation.
4. In parallel, identify content using suffix evidence, magic-byte evidence, optional learned classification, and bounded structural parsing.
5. Run qualified extraction, enrichment, fingerprint, transform, validation, and index-preparation capabilities selected by the host route.
6. Admit a transformed exact representation only after independent decode-and-hash validation; otherwise retain the mandatory exact representation.
7. Extract safe baseline metadata and searchable text for supported formats.
8. Build and activate a local path, metadata, type, and extracted-text index generation.
9. Read back required samples or full content according to policy.
10. Publish the exact snapshot after portable records and placement evidence commit; optional discovery branches may complete later and report their coverage explicitly.

For readable bytes, the fallback is always an exact representation. Unknown or unsupported files, applications, games, archives, disk images, model files, encrypted blobs, and proprietary formats remain useful because they are stored exactly and remain visible in the namespace even when no specialized processor exists.

Later policies may select perceptually equivalent media, external reacquisition, or other non-exact representations. Such a selection requires an explicit fidelity contract, pinned dependencies, appropriate validation, and durable decision authority. Similarity never becomes byte identity.

## 8. Ingestion, transaction, and publication flow

Planning and application are distinct, but a mutable source path cannot stand in for immutable plan input.

A plan that may be applied later MUST bind one of:

- A retained point-in-time source view supplied by a snapshot-capable `CaptureDriver`.
- Exact bytes already staged in an immutable intake area.
- A source revision contract that the driver can revalidate without ambiguity.

An ordinary exact ingestion flow is:

~~~text
create or validate source view
-> inventory namespace
-> retain the source view or stage required exact bytes
-> build immutable plan with mandatory exact placement and optional processing branches
-> apply host-owned exact hashing and RepositoryDriver placement
-> collect suffix and magic-byte evidence
-> run optional learned classification and bounded structural parsing
-> publish the final classification and run selected processing stages
-> validate and admit any additional representation candidates
-> reconcile every ambiguous external result
-> validate selected representations and namespace coverage
-> create and sign the portable RRF root
-> place and reconcile PREPARED_CLOSURE
-> create and sign PublicationCommitRecord
-> place and reconcile PUBLICATION_COMMIT
-> expose the committed snapshot, namespace, and content handles
-> build or update rebuildable search indexes
-> release source holds and eligible staging data
~~~

Payload placements use role `PAYLOAD`. The prepared portable metadata placement uses `RECOVERY_CLOSURE/PREPARED_CLOSURE`. After payload and prepared-closure reconciliation, the core signs a `PublicationCommitRecord` binding the RRF root, required placement receipts, plan and source-view digests, verification gate, publication generation, and fence. A snapshot becomes logically published only after a `RECOVERY_CLOSURE/PUBLICATION_COMMIT` placement is stored and reconciled.

Clean recovery begins from a valid publication marker, authenticates the bound prepared closure and payload placements, and ignores orphan payloads or uncommitted closures. Local database rows, publication pointers, caches, and indexes are rebuildable projections.

## 9. Namespace and content access

`SnapshotTree` and `FileAccess` are stable read interfaces over packed, compressed, deduplicated, remote, or tiered storage.

`SnapshotTree` provides:

- Immutable root and entry lookup.
- Component-by-component path resolution.
- Paginated directory listing and metadata inspection.
- Raw symbolic-link targets and hard-link relationships.
- Declared permissions, timestamps, extended attributes, sparse regions, and other qualified filesystem semantics.
- Stable subject references for annotations and search results.

`FileAccess` opens the representation selected for one namespace entry. An empty or default selector means the authoritative exact representation. It never means “return something similar.” Approximate, normalized, preview, or reacquired representations require explicit selection and authorization.

The reference distribution bundles read-only Linux FUSE over these interfaces. SMB, NFS, WebDAV, S3-compatible, media, database-like, alternate FUSE, and writable gateways are later adapters. None receives repository-private object access or authority to reinterpret the namespace.

## 10. Search and semantic extensions

Intelligent discovery is part of the product, while any particular model or index engine remains replaceable.

The reference distribution provides useful search over paths, metadata, detected types, checksums, duplicates, durable tags/notes, processing state, and extracted text. The stable query surface returns logical subject references, representation availability, typed scores, exact generation, coverage, stale state, and provenance. A `QueryProvider` may combine lexical, structured, temporal, vector, graph, visual, or acoustic candidates within one named generation. The host broker may fuse separately generation-pinned provider results without changing stored content identity.

Embeddings, CLIP-like image and video encoders, audio embeddings, captions, OCR, ASR, code intelligence, and domain-specific models are external or optional `Processor`, `IndexProvider`, or `QueryProvider` capabilities. Their outputs bind to:

- The exact subject and source representation.
- The implementation and model digest.
- The model-space and schema version.
- Coverage, parameters, and provenance.
- The index generation that consumed them.

Index generations are rebuildable by default. User-authored annotations, accepted corrections, and information that cannot be reproduced SHOULD be retained as portable records. An index may be retained as a costly derived artifact, but it never becomes the sole authority for namespace or content recovery.

## 11. Deployment and platform profiles

The primary deployment shape is a long-running self-hosted controller on a Linux-based NAS or server, with the CLI and MCP adapter available locally or through a deliberately configured administrative boundary. The same core semantics support workstation and enterprise deployments.

Platform-specific behavior belongs in profiles and drivers:

- Host-native filesystem, volume-manager, hypervisor, and storage-appliance snapshots.
- Quiesced application or database captures.
- Live local, SMB, and NFS trees with explicit non-atomic consistency claims.
- Object and repository imports.

Failure of an optional snapshot driver MAY reduce the available consistency profile, but it MUST NOT be mislabeled or silently substituted. No optional platform driver defines RestoreWeave or blocks another qualified NAS/server release.

Storage backends may include local pools, removable media, another NAS, object storage, mature backup repositories, archival tiers, or several failure-independent targets. Placement policies state achieved durability and redundancy rather than inferring them from target count.

## 12. Security and isolation

Every adapter, extension, and client receives a least-privilege capability grant. Grants independently cover:

- Source reads.
- Output staging writes.
- Network destinations.
- Credentials and secret references.
- Accelerator use.
- CPU, memory, scratch, output, and wall-time budgets.
- Namespace export and content-read scope.
- Mutating placement or restore operations.

Plugins do not receive ambient host paths, repository credentials, signing keys, or deletion authority. External-process and container isolation is preferred for untrusted parsers and models. Secrets remain host-resolved references and do not enter portable records, plans, logs, or MCP output in plaintext.

## 13. Interface stability

| Surface | Stability intent |
| --- | --- |
| RRF identity, namespace, decision, provenance, placement, verification, and publication semantics | Durable public contract |
| Typed Core Command ABI and machine-readable results/events | Stable public semantic contract after qualification |
| `SnapshotTree`, `FileAccess`, and stable query subject references | Stable public read contract |
| `CaptureDriver`, `Processor`, `RepositoryDriver`, `IndexProvider`, `QueryProvider`, and later `RetrieverDriver` semantic contracts | Normative extension boundary |
| External plugin process or remote wire encoding | Versioned experimental contract until independently qualified |
| CLI human-readable text, WebUI, and presentation layout | Evolvable product surface |
| SQLite schemas, queues, caches, private Go packages, and worker topology | Unstable implementation detail |
| Repository-private chunks, packs, and object names | Backend-owned implementation detail |
| Search-store document IDs and vector database schemas | Rebuildable index detail |

Conceptual Processor-stage boundaries do not require separate public protocols or an out-of-process plugin for every role. Bundled implementations may remain private while their inputs, outputs, provenance, and authority limits conform to the public seam contracts.

## 14. Delivery sequence

| Phase | Product outcome | Deliberately not required |
| --- | --- | --- |
| Phase 1: useful self-hosted storage | NAS-oriented service, CLI and read-only MCP, ordinary-tree capture plus optional snapshot profiles, exact content addressing, deduplication, lossless compression, portable commits, durable tag/note CRUD, original namespace access, bundled read-only Linux FUSE, baseline lexical search, verification, and clean restore | Mandatory AI, approximate replacement, alternate gateways, visual workflow editor, P2P, or enterprise control plane |
| Phase 2: managed adoption and replaceability | Independently implemented `Processor` and `RepositoryDriver` profiles; reprocessing, migration, decoder-retention, rollback, and a reviewed source-retirement profile after clean-recovery gates | Automatic source deletion, one-way migration, or claiming released capacity before retirement actually completes |
| Phase 3: richer intelligence and advanced representations | Qualified `IndexProvider` and `QueryProvider` alternatives; optional OCR, ASR, embeddings, CLIP-like encoders, richer annotations, alternate search stores, media-aware transforms, perceptual validation, tiering, retrieval, and multiple backends | Separate public ABI families for every conceptual stage, making one model authoritative, or silent lossy substitution |
| Phase 4: scale and enterprise operation | Distributed workers, multi-user authorization, immutable storage profiles, independent monitoring, fleet policy, and qualified high-availability deployment | Dependence of existing snapshots on an enterprise control service |

## 15. Architectural invariants

1. RestoreWeave is a self-hosted, NAS-first content-aware managed data layer; `RW-MVP-1` is its first read-only managed-archive and search profile, and no platform-specific capture profile defines the product.
2. The authoritative core owns identities, decisions, provenance, verification, transactions, namespace meaning, and recovery truth.
3. The stable replaceable seams are `CaptureDriver`, capability-oriented `Processor`, `RepositoryDriver`, `IndexProvider`, `QueryProvider`, and later `RetrieverDriver`; conceptual Processor roles do not become separate public ABIs.
4. The reference distribution ships a strong default pipeline rather than an empty plugin host.
5. Readable unknown or unsupported bytes are preserved exactly.
6. Suffix, magic-byte, structural, and learned detection evidence remain distinguishable and auditable.
7. Similarity, embeddings, perceptual hashes, captions, and model confidence never prove byte identity.
8. Deduplication, compression, repacking, migration, and index rebuilds do not change logical content or namespace identity.
9. Every derived representation records provenance, decoder dependencies, exactness, and validation evidence.
10. A plugin reports evidence or performs bounded work; only the core accepts a result and publishes durable truth.
11. The original directory tree remains browsable and restorable independently of physical storage layout.
12. CLI, MCP, and later UI or REST adapters bind the same command and authority semantics.
13. Search is useful by default, but every search index and model space can be rebuilt or replaced without losing stored data.
14. A snapshot is published only after payload, portable closure, verification gate, and publication marker reconciliation.
15. Loss of the operational database, search indexes, UI, AI services, or optional plugins cannot prevent clean recovery of a committed exact snapshot.
16. Every platform-specific capture implementation is an optional driver profile and never a product-wide prerequisite.
