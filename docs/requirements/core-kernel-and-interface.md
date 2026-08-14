# Core Kernel and Interface Requirements

## 1. Status and purpose

This document defines the normative product boundary for RestoreWeave as a self-hosted, NAS-first content-aware managed data layer. It specifies which semantics remain authoritative and stable, which algorithms are replaceable, what the reference distribution must deliver, and how stored data remains browsable, verifiable, searchable, and recoverable across implementation changes. `RW-MVP-1` is the first read-only managed-archive and search profile over that broader data layer.

For questions of ownership, interface stability, plugin authority, exact fallback, publication, and the minimum useful product, this document takes precedence over platform-specific or older recovery-only descriptions. Detailed documents may add constraints, but they MUST NOT make an optional platform driver, AI model, semantic provider, WebUI, or external harness a prerequisite for the core exact-storage path.

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

## 2. Product promise

RestoreWeave helps operators store large heterogeneous file collections with fewer redundant bytes, discover their contents more intelligently than path-only storage, and retain a verifiable filesystem view that can be browsed and restored independently of the internal packing layout.

The minimum product promise is:

1. Ingest a supported local, NAS-mounted, snapshot, remote, or imported source through a profile with an explicit consistency claim.
2. Identify content using built-in suffix and magic-byte evidence, with optional learned classification and bounded structural parsing.
3. Select a versioned processing pipeline appropriate to the content class.
4. Preserve every readable unknown or unsupported file exactly.
5. Reduce storage through exact content addressing, deduplication, and lossless compression in the default profile.
6. Retain authenticated provenance, placement, and verification evidence.
7. Present stored data through its original directory structure even when physical bytes are chunked, compressed, packed, remote, or shared by many files.
8. Provide useful path, metadata, type, checksum, duplicate, durable tag/note, processing-state, and extracted-text search by default.
9. Allow embeddings, CLIP-like encoders, media fingerprints, alternate compressors, transforms, validators, repositories, index builders, and query implementations to be added or replaced through the stable extension seams without changing core identity semantics.
10. Browse, read, verify, migrate, and restore a committed exact snapshot without the original operational database, UI, search index, AI service, or plugin registry service.

Storage reduction is not merely a repository implementation detail; it is one of the product's primary outcomes. It remains subordinate to the selected fidelity contract: fewer bytes are not a success when the system cannot prove what the operator can recover.

The reference experience SHOULD make good default decisions automatically and explain consequential choices. A human or an explicitly authorized durable policy remains the final authority for exclusions, deletion, non-exact substitution, and other fidelity-reducing actions.

## 3. Product boundary

### 3.1 Authoritative core

RestoreWeave core owns the following stable semantics:

- Logical identity for sources, source views, namespace entries, file versions, exact content, chunks, representations, snapshots, operations, plans, policies, placements, and index generations.
- Immutable accepted decisions, including classification decisions that drive dispatch and fidelity decisions that select a representation.
- Provenance for observations, classifications, extracted facts, fingerprints, transformations, validations, placements, annotations, and migrations.
- Recovery and fidelity contracts.
- Durable operation transactions, journals, idempotency, cancellation, leases, fencing, checkpoints, and reconciliation.
- Final acceptance of validation and placement evidence.
- Portable publication records and clean-recovery semantics.
- Original namespace reconstruction and authoritative representation selection.
- Capability grants to extensions, clients, and gateways.

The core is authoritative because these meanings must survive algorithm upgrades, plugin replacement, repository migration, index rebuilds, and loss of operational projections.

### 3.2 Replaceable algorithmic userland

The stable, independently versioned extension seams are exactly:

- `CaptureDriver` for snapshot, quiesced, live, remote, or imported source views.
- Capability-oriented `Processor` for learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation.
- `RepositoryDriver` for local pools, NAS repositories, object stores, archival tiers, mature storage engines, and replicas.
- `IndexProvider` for path, metadata, full-text, vector, graph, visual, acoustic, or application-specific index generations.
- `QueryProvider` for typed query validation, execution, ranking, fusion, pagination, and subject-bound results.
- A later `RetrieverDriver` for pinned external reacquisition.

Learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation are conceptual `Processor` capability roles. They MUST NOT be specified as separate stable public wire ABIs. One executable or package MAY implement several seams; in particular, one implementation MAY provide both `IndexProvider` and `QueryProvider`, while negotiating, versioning, authorizing, and binding those capabilities separately.

Presentation gateways, WebUIs, REST adapters, MCP clients, schedulers, notification systems, and enterprise consoles are also replaceable, but they are northbound or presentation adapters rather than storage-algorithm extension seams.

Replaceability does not transfer authority. A component may produce facts, candidates, bytes, scores, or receipts. Only the core may accept an outcome against policy, mutate an immutable plan, select the authoritative representation, publish a snapshot, authorize destructive lifecycle work, or state what is recoverable.

### 3.3 Excluded core responsibilities

The core is not:

- A general agent framework or prompt runtime.
- A model-serving platform.
- A universal codec implementation.
- A vector database.
- A visual workflow editor.
- A repository-specific pack or chunk implementation.
- A general-purpose distributed workflow marketplace.

RestoreWeave exposes CLI and MCP operations that external harnesses can call. It does not embed a privileged autonomous agent loop.

### 3.4 A useful reference userland is mandatory

A thin stable core is valuable only when paired with a strong default product. A distribution that exposes interfaces but cannot ingest, deduplicate, losslessly compress, place, search, browse, verify, and restore supported content is not conforming.

## 4. Core data model

### 4.1 Identity classes

The core MUST distinguish at least the following identities:

| Identity | Meaning |
| --- | --- |
| `SourceId` | Configured origin or import endpoint |
| `SourceViewId` | One retained, staged, or validated observation boundary with a consistency claim |
| `NamespaceRootId` | Root of one immutable logical directory tree |
| `NamespaceEntryId` | One path entry in that tree |
| `FileVersionId` | Observed content and declared filesystem metadata for one file version |
| `ContentId` / `ContentRef` | Exact byte identity using the required cryptographic digest profile. `ContentRef` is the catalog vocabulary; `ContentId` remains the kernel identifier field. |
| `ChunkId` | Exact byte identity under a named chunking profile |
| `RepresentationId` / `RepresentationRef` | Raw or derived bytes and their decoder/fidelity contract. `RepresentationRef` is the catalog vocabulary; `RepresentationId` remains the identifier field. |
| `SnapshotId` | One committed namespace with selected representations and policy state |
| `PlacementId` | Durable storage of a representation or portable record at one target |
| `IndexGenerationId` | Rebuildable search projection bound to exact input records, `IndexProvider` capability profile, and model space |
| `PlanId` | Immutable selected operation graph and expected inputs |
| `DecisionId` | Durable authority-bearing selection or override |
| `OperationId` | Durable execution and reconciliation scope |

Database row IDs, repository-private chunk IDs, pack IDs, object names, plugin invocation IDs, cache keys, and vector-store document IDs MUST NOT become canonical logical identity.

### 4.2 Exact and derived representations

Every file version may have one or more representations:

- `EXACT_RAW`: the original byte sequence.
- `EXACT_REVERSIBLE`: a losslessly encoded representation with pinned decoder requirements.
- `DERIVED`: an extract, preview, thumbnail, normalized copy, transcript, embedding input, or other non-authoritative artifact.
- `APPROXIMATE`: a representation accepted under an explicit quality or perceptual contract.

An exact content ID can be assigned only after required byte coverage and cryptographic hashing. Exact raw and exact reversible representations may satisfy the same exact content contract after successful decode verification. Derived and approximate representations never inherit exact identity. A pinned acquisition recipe remains a source binding, not a representation or placement, until acquired bytes pass host-owned exact identity, required validation, admission, and repository placement.

The default representation for `FileAccess` MUST be exact. A non-exact representation requires an explicit selector and an accepted contract. It MUST NOT silently appear at the canonical path of an exact file.

### 4.3 Immutable decisions

Core decisions include:

- Accepted content classification and the evidence policy used.
- Selected `Processor` capabilities and `RepositoryDriver`, `IndexProvider`, or `QueryProvider` profiles and versions.
- Exact versus non-exact fidelity.
- Inclusion, exclusion, retention, and placement policy.
- Decoder and dependency retention.
- Verification thresholds and sampling profiles.
- Index and annotation retention.

An extension may propose a decision. The core persists the accepted decision with its policy revision, evidence references, actor or durable policy authority, time, and digest. Changing a decision creates a successor plan or record; it does not mutate history.

## 5. Core invariants

Every implementation and binding MUST preserve these invariants:

1. **Readable unknown data is exact data.** Unknown, conflicting, encrypted, unsupported, or partially processed readable bytes select exact preservation by default.
2. **No silent omission.** An unreadable file, failed capture, missing dependency, or unsatisfied contract is visible as a typed block or degraded result.
3. **Similarity is not identity.** Embeddings, perceptual hashes, captions, model confidence, and nearest-neighbor results cannot prove byte equality.
4. **No silent substitution.** Approximate, generated, normalized, or reacquired content never replaces an exact canonical representation without explicit authority and a compatible contract.
5. **Plans are immutable mutation inputs.** Application requires the exact plan digest, source-view binding, policy revision, selected extension capability profiles, and target expectations.
6. **Extensions report; the core decides.** An extension cannot approve its own result, publish data, weaken fidelity, or grant itself capabilities.
7. **Logical identity is placement-independent.** Repacking, recompression, tier movement, repository migration, and replica repair do not change content, file-version, namespace, or snapshot identity.
8. **The original namespace remains recoverable.** Repository-private layout cannot become the only path-to-content map.
9. **Durable truth outlives projections.** Loss of SQLite, queues, caches, indexes, UI state, AI services, or plugin registries cannot make a committed exact snapshot undecodable.
10. **Authority is explicit and scoped.** Processors, gateways, MCP clients, and `RepositoryDriver` implementations receive no ambient filesystem, credential, network, signing, policy, or deletion authority.
11. **Every accepted result has provenance.** Inputs, implementation digest, parameters, output identity, coverage, validation, policy, and acceptance are replayably linked.
12. **Failures remain typed.** Unknown external outcomes, partial coverage, unsupported metadata, and failed verification are never flattened into success.
13. **Search does not own storage truth.** Index rows, ranks, embeddings, and search-store identifiers remain projections over stable subjects.
14. **Upgrades do not reinterpret history.** New extension capability versions affect new plans or explicit migrations, not previously accepted records.
15. **The default exact path has no AI dependency.** Disabling learned models and external harnesses cannot disable exact ingest, storage reduction, search over baseline fields, browse, verify, or restore.
16. **Publication is a reconciled commit.** Uploaded bytes or a successful process exit alone do not make a snapshot committed.
17. **Human authority remains available.** Fidelity reduction, last-copy deletion, and irreversible dependency removal require explicit durable authority.

## 6. Public and private interfaces

RestoreWeave guarantees a deliberately small public surface.

| Interface | Stability expectation |
| --- | --- |
| Recovery Record Format identity and publication semantics | Durable public format |
| Typed Core Command ABI | Stable public semantic contract after qualification |
| CLI JSON and JSON Lines output | Stable binding of Core Command ABI schemas |
| Local MCP tools and resources | Stable binding of selected Core Command ABI operations after qualification |
| `SnapshotTree`, `FileAccess`, and query subject references | Stable read interfaces independent of physical layout |
| `CaptureDriver`, `Processor`, `RepositoryDriver`, `IndexProvider`, `QueryProvider`, and later `RetrieverDriver` semantic contracts | Normative extension boundary |
| External process, container, or remote extension wire ABI | Versioned experimental interface until independent qualification |
| Human-readable CLI text and WebUI layout | Evolvable product interface |
| Go packages under `internal/` | Unstable implementation detail |
| SQLite schemas, migrations, indexes, and queries | Unstable operational projection |
| Worker topology, queues, caches, and IPC choices | Unstable implementation detail |
| Storage-engine chunks, packs, object names, and garbage collector | Backend-owned implementation detail |
| Search-store schemas and internal document IDs | Rebuildable implementation detail |

### 6.1 Versioning rules

- Stable interfaces MUST carry an explicit major version.
- A compatible minor revision MAY add optional fields, capabilities, commands, events, or enum values with defined unknown-value behavior.
- A breaking semantic change requires a new major version and an explicit migration or compatibility reader.
- Readers MUST ignore unknown optional fields and preserve them when records are copied or repackaged.
- Unknown required fields, unknown critical extensions, or unsupported major versions MUST fail closed with a typed reason.
- Experimental interfaces MUST include `alpha` or `experimental` in their version.
- A reader or decoder required by retained data MUST remain available until the data is migrated and reverified or intentionally retired under explicit policy authority.

### 6.2 Implementation replacement rules

- Multiple implementation versions MAY be installed at the same time.
- Every accepted plan MUST pin the interface family, capability-profile digest, implementation identity, version, immutable digest, semantic profile, and relevant parameters for each selected extension.
- Installing or activating a new version MUST NOT affect an in-flight operation.
- A new implementation MAY become the default only for newly created plans.
- Reprocessing existing data creates new provenance, derivative records, index generations, or representations.
- A representation migration MUST prove the target contract before the source representation becomes eligible for collection.
- An extension can be replaced independently only when its declared input and output schemas, fidelity meaning, state or generation semantics, and dependency contract remain compatible.

## 7. Recovery Record Format

### 7.1 Purpose

The Recovery Record Format, abbreviated **RRF**, is the authenticated portable description of what RestoreWeave observed, decided, stored, derived, verified, indexed, and can recover. It is the durable control and identity graph, not a replacement for every payload repository format.

RRF MUST be sufficient to reconstruct the committed logical namespace, locate and decode selected representations, verify trust and placement evidence, and execute a supported restore path without the original operational database, WebUI, search index, MCP client, model service, or external harness.

### 7.2 Required record families

RRF MUST represent at least:

- `SourceRecord` and `SourceViewRecord`.
- `NamespaceRoot` and `NamespaceEntry`.
- `FileVersionRecord`, exact `ContentRecord`, and optional `ChunkProfileRecord`.
- `ClassificationEvidence` and accepted `ClassificationDecision`.
- `RecoveryContract`, `FidelityContract`, and immutable `ProtectionPlan`.
- `RepresentationRecord` with exactness, transform provenance, and decoder requirements.
- Capability-profile provenance and role-specific extension invocation records.
- `PlacementReceipt` and backend reader requirements.
- `ValidationRecord` and core `VerificationRecord`.
- `SnapshotRecord` and signed `PublicationCommitRecord`.
- `OperationRecord` and durable terminal events.
- `AnnotationRecord` and accepted corrections.
- Retained derivative metadata and optional `IndexGenerationRecord` descriptions.
- `RestoreResult` and migration results.

Canonical records MUST use deterministic serialization for digest and signature verification. A published RRF root MUST bind every required record and attachment digest, schema version, decoder dependency, repository reader requirement, and signature reference.

RRF MUST NOT contain plaintext credentials. Normal operation uses opaque host-resolved credential references; portable secret recovery material requires an explicit separately encrypted export.

### 7.3 Portable recovery closure

A committed exact snapshot MUST have a portable recovery closure containing or authentically referencing:

- The RRF root and required records.
- Required schemas and canonicalization rules.
- Namespace and path-to-representation bindings.
- Placement receipts and storage-reader requirements.
- Decoder identities and immutable digests for non-raw exact representations.
- Signature verification material and instructions using an independently retained trust anchor.
- Clean-machine browse and restore instructions.

Large payloads remain in selected storage targets. The closure proves how to locate, decode, and authenticate them; it does not duplicate all data unless an export profile explicitly requests that behavior.

## 8. Typed Core Command ABI

### 8.1 Canonical contract

The Core Command ABI is the canonical northbound control contract. CLI machine output, MCP, and any later REST or WebUI adapter MUST bind the same command, result, event, reason, reference, and authority semantics.

A command envelope MUST contain:

- ABI version and typed command name.
- Request identity and idempotency key for durable or externally visible work.
- Adapter-derived actor and client identity.
- Expected revisions, plan digest, and source-view binding where applicable.
- Typed input and bounded resource references.
- Deadline, cancellation, dry-run, and budget fields where meaningful.

A result envelope MUST contain:

- ABI version, request identity, and command name.
- `ACCEPTED`, `SUCCEEDED`, `DEGRADED`, `BLOCKED`, `FAILED`, `CANCELLED`, or `UNKNOWN_EXTERNAL_OUTCOME`.
- Stable reason codes, retryability, warnings, and required actions.
- Durable job, resource, provenance, and evidence references.
- Typed operation-specific output.

Large content travels through scoped byte handles or stream sessions, not unbounded JSON, MCP messages, or ordinary control requests.

### 8.2 Minimum command families

The stable ABI MUST provide semantic operations equivalent to:

| Operation family | Required outcome |
| --- | --- |
| `plan.ingest` | Inspect a qualified source, capture or stage required inputs, and create an immutable, explainable ingest plan without repository publication |
| `plan.revise` / `plan.abandon` | Create a successor decision set or release only the named unapplied plan's holds |
| `plan.restore` | Create an immutable restore plan without destination mutation |
| `plan.get` | Read a plan, its decisions, estimates, dependencies, and validity |
| `plan.apply` | Sole public mutation operation for applying an exact plan digest |
| `status.get` | Report jobs, snapshots, source holds, placements, indexes, health, and blocked actions |
| `job.events` / `job.cancel` | Resume bounded durable events or request cancellation |
| `snapshot.list` / `snapshot.diff` / `snapshot.verify` | Discover, compare, and verify committed snapshots |
| `namespace.list` / `namespace.resolve` / `namespace.stat` / `namespace.readlink` | Browse and resolve the stable original-directory projection |
| `representation.list` | Inspect available representations and their health for one authorized subject |
| `content.open` / `content.read` / `content.close` | Read one authorized representation through bounded `FileAccess` sessions |
| `search.query` | Search stable subjects through configured index and query providers |
| `recovery.export` | Export the committed portable closure and recovery reference |
| `capability.list` | Inspect capture, processor, repository, index, and query capability profiles without arbitrary execution |

The ABI MUST NOT expose arbitrary SQL, arbitrary shell execution, unrestricted plugin invocation, prompt execution, or a generic privileged `run_tool` operation.

### 8.3 Plan requirements

An ingest plan MUST explain at least:

- Selected roots and exclusions.
- Source-view consistency and retention mechanism.
- Observed entries and bytes.
- Classification evidence, conflicts, and accepted content class.
- Exact fallback scope.
- Selected extension interfaces, capability profiles, versions, parameters, and permissions.
- Expected deduplication, compression, transfer, and stored-byte estimates where measurable.
- Placement targets and expected durability properties.
- Search extraction and indexing work.
- Decoder, network, privacy, license, accelerator, and cost dependencies.
- Verification scope and acceptance thresholds.
- Decisions requiring human or durable policy authority.

Planning MAY call `Processor` capabilities and inspect provider or driver capabilities in bounded read-only or staging mode. If a plan is applied later, it MUST bind a retained immutable source view, exact staged bytes, or a revalidatable source revision contract. A mutable path string alone is insufficient.

Readable bytes that are not successfully handled by an optional specialized `Processor` capability MUST appear in the exact fallback set. The plan cannot convert processor absence, timeout, crash, invalid output, low confidence, or unsupported type into omission.

## 9. Versioned extension interfaces

### 9.1 Stable seam inventory

RestoreWeave exposes exactly these storage-domain extension seams:

| Interface | Responsibility | Availability |
| --- | --- | --- |
| `CaptureDriver` | Present a bounded source view with explicit consistency and lifecycle evidence | Required for every supported source profile |
| `Processor` | Execute one typed content-processing capability | Required seam; individual specialized processors are optional |
| `RepositoryDriver` | Place, read, verify, reconcile, and restore admitted representations and portable records | At least one qualified implementation is required |
| `IndexProvider` | Build or update named, versioned discovery generations | Baseline metadata and text implementation required |
| `QueryProvider` | Query exactly one explicitly named `IndexGenerationRef` per invocation and return subject-bound candidates; compatibility is validated before invocation | Baseline metadata and text implementation required |
| `RetrieverDriver` | Reacquire bytes from a pinned external source | Reserved for a later qualified profile |

A single executable MAY implement several interfaces. One implementation MAY provide both `IndexProvider` and `QueryProvider`, but its build and query capabilities remain separately negotiated, versioned, authorized, and bound to explicit index generations.

### 9.2 Common capability contract

Every external extension session negotiates a canonical `CapabilityProfile`. A `Processor` uses the logical invocation and result shape below. Role-specific driver and provider operations MUST preserve the same identity, capability-profile, version, digest, budget, cancellation, provenance, typed-outcome, and reconciliation semantics.

~~~text
CapabilityProfile
- interface family, capability role, and API major/minor version
- implementation ID, version, immutable digest, and supplier
- supported input/output schemas and content classes
- fidelity, determinism, reproducibility, and coverage claims
- resource, accelerator, filesystem, network, and secret requirements
- decoder, migration, and compatibility requirements

ProcessInvocation
- invocation, operation, plan, and policy identities
- pinned capability-profile digest, implementation, and semantic profile
- typed subject, input-record, and scoped byte-handle references
- expected output contract and output staging handle
- deadline, cancellation, quotas, and explicit capability grants

ProcessResult
- typed terminal outcome and reason codes
- inspected and processed coverage
- output records and representation references
- implementation, parameter, dependency, and model provenance
- measurements, confidence, warnings, and validation evidence
- no acceptance, publication, deletion, or policy authority
~~~

An implementation MUST reject an invocation whose interface family, major API, selected capability profile, granted authority, or input contract it cannot satisfy. The core MUST reject malformed, over-budget, out-of-scope, or provenance-incomplete results.

### 9.3 Role-specific contracts

#### `CaptureDriver`

A capture driver creates or validates a read-only source view and reports its consistency semantics. Supported claims MAY include retained filesystem snapshot, quiesced application state, immutable import, staged exact copy, or live validated observation. The claim MUST state whether namespace atomicity, file atomicity, metadata stability, and retention are guaranteed.

Snapshot mechanisms for ZFS, Btrfs, LVM, storage appliances, APFS, VSS, and databases are `CaptureDriver` profiles. Lack of any optional platform profile MUST NOT block another qualified Linux/NAS or server release.

A live local, SMB, or NFS tree MAY be supported under an explicit non-atomic profile using before-and-after metadata checks, retries, and staged bytes. It MUST NOT be presented as a point-in-time snapshot.

#### `Processor`

`Processor` is one capability-oriented interface. Learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation are conceptual routing roles within it, not separate public plugin families. Suffix and magic-byte inspection are host-owned baseline stages:

| Processor role | Required behavior |
| --- | --- |
| `CLASSIFY_LEARNED` | Return optional learned or domain-specific file-type evidence with inspected ranges, confidence where meaningful, and provenance; never authorize omission |
| `PARSE` | Return structural validity, typed members, and explicit inspected coverage; failure never blocks the mandatory exact lane |
| `EXTRACT` | Return typed metadata or derivatives with explicit coverage; failure does not prevent exact preservation |
| `ENRICH` | Return attributed external or model-derived descriptions and relationships without converting them into local identity or authority |
| `FINGERPRINT` | Return auxiliary checksums or comparison features with declared identity strength; canonical `ContentId`, duplicate identity, and the required exact digest are computed or independently verified only by the host-owned exact lane |
| `TRANSFORM` | Return a staged raw, exact reversible, derived, or approximate representation candidate with decoder and fidelity requirements |
| `VALIDATE` | Return measurements for a named exact, structural, perceptual, decode, or application-level validation profile; the core accepts or rejects them |
| `INDEX_PREPARE` | Return provider-ready records bound to stable subjects, schemas, coverage, and producer generations; never mutate an index directly |

The role set above is canonical. The core MUST preserve suffix and magic-byte evidence separately before optional `CLASSIFY_LEARNED` processing, then may use matching `PARSE` capabilities to refine a provisional classification. Conflicts and low confidence remain visible and normally select the generic exact-byte route.

Processor invocation has two operation forms. `RUN_STAGE` executes one node bound to exactly one host-owned `IdentificationRouteRef` or `ProcessingRouteRef`. `DECODE_REPRESENTATION` is the retained historical-read operation for a transform profile: it receives only a selected encoded representation handle, pinned decoder dependencies, requested decoded range, and budgets. A decoder may be retired for new encoding while remaining qualified for historical decode. For an exact claim, the host computes decoded length and digest without exposing the original source handle to the decoder.

The core MUST verify an exact reversible `TRANSFORM` result by decoding or by a qualified equivalent proof before accepting it as satisfying the exact contract. Approximate output requires an explicit fidelity contract and compatible `VALIDATE` evidence.

#### `RepositoryDriver`

A `RepositoryDriver` stores, locates, reads back, verifies, restores, and reconciles admitted immutable representations or portable records. It returns durable receipts and describes achieved durability, availability, encryption, failure-domain, and reader requirements.

The storage engine may own private chunking, compression, packing, object naming, transport, repair, and physical garbage collection. RestoreWeave owns logical identities, selected representations, path bindings, policy, and acceptance of the receipt.

#### `IndexProvider`

An `IndexProvider` consumes an authorized replayable feed of stable subject records and derivatives and produces an `IndexGenerationRecord`. It MUST bind source snapshot or content identities, schema, capability-profile and implementation digests, model space, coverage, and storage location. The index is rebuildable unless policy explicitly retains it as a costly derivative.

#### `QueryProvider`

A `QueryProvider` validates and executes a typed query against exactly one explicitly named `IndexGenerationRef` per invocation. The host validates provider and generation compatibility before invocation. The provider returns ordered stable subject references, component scores, continuation state, and provenance. It cannot return a search-store document ID as the only result identity. Query planning, ranking, and filtering within that generation are responsibilities inside this seam. A host-owned query broker may invoke several providers or generations and fuse their separately generation-pinned typed results; cross-provider fusion is not a separate public ranking ABI.

One implementation MAY provide both `IndexProvider` and `QueryProvider`, but the core still records the exact build generation and query capability profile independently.

#### `RetrieverDriver`

A later `RetrieverDriver` may reacquire bytes from a pinned URL, content network, package source, magnet reference, or other external origin. Successful retrieval is acquisition evidence. The host computes or verifies canonical exact identity first, may invoke specialized `Processor.VALIDATE` or `Processor.FINGERPRINT` capabilities as additional evidence, and satisfies a storage contract only after `RepositoryDriver` placement, required readback, and publication.

### 9.4 Dispatch policy

The default dispatch sequence is:

~~~text
mandatory exact identity and RepositoryDriver lane begins independently

path and suffix evidence
-> magic-byte evidence
-> optional learned or domain-specific `CLASSIFY_LEARNED` Processor evidence
-> provisional classification decision
-> matching `PARSE` Processor evidence when safe
-> final versioned classification decision
-> `EXTRACT`, `ENRICH`, and `FINGERPRINT` Processor selection
-> `TRANSFORM`, `VALIDATE`, and `INDEX_PREPARE` Processor selection
-> `RepositoryDriver` placement
-> `IndexProvider` generation
-> host validation and activation of the named generation
-> `QueryProvider` invocation against that exact generation
~~~

The core records all consequential evidence and the accepted decision. An extension may be replaced without changing the classification or representation schema. A changed result from a new capability version creates new records and, where consequential, a successor plan.

## 10. Reference exact pipeline

The reference distribution MUST include a complete exact pipeline with no model-service dependency.

| Step | Required default capability |
| --- | --- |
| Source | Ordinary readable trees plus at least one qualified snapshot or immutable-import profile on the release platform |
| Inventory | Portable namespace records and declared filesystem metadata |
| Identification | Built-in suffix plus magic-byte evidence, optional learned classification, and bounded structural parsing with conservative conflict handling |
| Exact fingerprinting | Required full-content cryptographic digest and versioned chunk fingerprints |
| Storage reduction | Exact content reuse, chunk-level deduplication where supported, and lossless compression |
| Transformation fallback | Raw or exact reversible representation for every readable unsupported file |
| Extraction | Safe baseline metadata and extracted text for supported common formats |
| Placement | At least one qualified local, NAS, repository, or object-storage target with readback and reconciliation |
| Search | Bundled path, metadata, detected-type, checksum, duplicate, durable tag/note, processing-state, and extracted-text search |
| Verification | Authenticated metadata plus policy-defined sampled or full-byte readback |
| Access | Browse, stat, read, export, restore, and bundled read-only Linux FUSE through the original namespace |
| Recovery | Portable closure and clean-machine reader path |

The reference product SHOULD select sensible defaults automatically from source, content class, available resources, target capabilities, and operator policy. Advanced users MAY pin or override `Processor`, driver, and provider capability profiles.

Embeddings, CLIP-like encoders, OCR beyond the baseline, ASR, media understanding, perceptual transforms, and neural representations are valuable optional `Processor`, `IndexProvider`, or `QueryProvider` capabilities. They are not required for the first exact pipeline, but the stable subject, derivative, index-generation, and query contracts MUST leave room for them.

## 11. Namespace, content, and query interfaces

### 11.1 `SnapshotTree`

`SnapshotTree` exposes one authenticated immutable namespace view. It supports:

- Root lookup and component-by-component path resolution.
- Paginated directory listing.
- Metadata inspection.
- Symbolic-link target inspection.
- Hard-link relationships.
- Declared permission, time, extended-attribute, sparse-file, and platform metadata.
- Stable subject references for annotations and search.

The tree reconstructs original directory form independently of repository-private packs, chunks, or objects.

Directory continuation uses opaque core-issued `PageToken` values. A token binds the exact snapshot and namespace-root digest, parent `PathRef`, stable ordering and filters, principal and authorization revision, interface version, and expiry. It is valid only for that scope, cannot cross a directory, snapshot, principal, authorization change, or query shape, and cannot be interpreted as an index, row ID, path, or FUSE directory offset. Following one token chain over an immutable directory MUST return each authorized child exactly once without duplication or omission; retrying the identical page request MUST be idempotent, while invalid, expired, or out-of-scope tokens fail closed.

### 11.2 `FileAccess`

`FileAccess` authorizes and opens one representation for one immutable namespace entry. The default selector is the authoritative exact representation. Reads use bounded sequential or random-access sessions and validate representation identity, placement state, and integrity policy.

Large bytes use streams, range handles, or file descriptors. Control adapters do not proxy unbounded content inside JSON or MCP messages.

### 11.3 Query interface

The stable query contract operates on logical subjects rather than one index implementation. `RW-MVP-1` uses one exact named generation per provider invocation and supports text plus typed source, snapshot, path, entry-type, content-class, format, size, time, digest, duplicate, tag, note, processing, representation, placement, and verification filters. It also supports bounded projections, stable sorting, facets, explanations, and an explicit stale policy. Later profiles MAY add vector, image, audio, example-document, graph, or hybrid inputs.

Results include:

- Stable subject and snapshot references.
- Matching namespace paths.
- Available representation references.
- Typed scores and `QueryProvider` provenance.
- Matching extracted facts or annotation references where authorized.
- Index generation and model-space identity.
- Indexed-through revision, coverage, and `PENDING`, `CURRENT`, `PARTIAL`, `STALE`, `FAILED`, or `UNAVAILABLE` state.

Continuation binds the exact generation, query digest, authorization scope, sort order, and expiry. It cannot cross a generation or authorization change silently. Loss of an index affects discovery performance, not namespace or content correctness. The controller can schedule a rebuild from portable records, annotations, and stored representations.

### 11.4 Annotations

Whole-subject user tags and plain-text notes are required durable semantic data in `RW-MVP-1`. They bind stable subjects, carry authorship, visibility, revision, and tombstone provenance, support optimistic CRUD and portable export/import, and enter the portable closure according to policy.

Collections, ratings, relationship graphs, typed segment annotations, accepted machine suggestions, and recovery-intent services are later profile capabilities. When enabled, they follow the same durable subject and provenance rules.

Machine-generated captions, transcripts, embeddings, thumbnails, summaries, and classifications are derived records. They MAY be retained when their cost, uniqueness, or reproducibility matters, but search-store rows and rank scores remain non-authoritative.

## 12. Transaction coordinator and publication protocol

RestoreWeave owns a finite operation coordinator because source retention, transformation, placement, verification, restore mutation, migration, and garbage collection must survive crashes and ambiguous external outcomes. It is not a general agent workflow engine.

The coordinator MUST provide:

- Durable operation and attempt identities.
- Append-only state transitions and causal events.
- Idempotency keys, leases, and fencing tokens.
- Bounded checkpoints and resumable event pages.
- Deadlines and cooperative cancellation.
- Reconciliation for timeout, transport loss, crash, or ambiguous external success.
- Typed terminal outcomes.
- Safe source-view and staging holds.
- Dependency reachability before lifecycle deletion.

### 12.1 Portable commit protocol

The portable logical commit uses three roles:

1. `PAYLOAD`: required representation placements.
2. `RECOVERY_CLOSURE/PREPARED_CLOSURE`: signed RRF root and required portable records after payload reconciliation and the publication verification gate.
3. `RECOVERY_CLOSURE/PUBLICATION_COMMIT`: signed marker binding the RRF root, payload and prepared-closure receipts, plan and source-view digests, publication generation, verification evidence, and fence.

The operation sequence is:

~~~text
stage and place required payload representations
-> reconcile payload receipts
-> verify namespace and selected representation coverage
-> create and sign RRF root
-> place and reconcile PREPARED_CLOSURE
-> create and sign PublicationCommitRecord
-> place and reconcile PUBLICATION_COMMIT
-> expose committed snapshot locally
~~~

Storing and reconciling the publication marker is the logical commit point. A local database flag, process exit, upload response, or prepared closure alone is insufficient.

Clean-machine discovery begins from a valid `PUBLICATION_COMMIT`, authenticates the bound prepared closure and payloads, and ignores orphan placements. Local publication pointers can be rebuilt from portable evidence.

### 12.2 Source-view lifecycle

A plan consumer hold and an apply-job hold MUST keep a retained source view or required staged bytes available until the corresponding consumer has a durable terminal receipt. Release is idempotent and fenced. A successor plan acquires its own hold before the predecessor can release one.

A mutable live path may be rescanned, but it cannot be assumed to contain the bytes approved in an earlier plan. Drift produces a successor plan, exact staging, or a typed block according to the source profile.

### 12.3 Migration and garbage collection

Physical garbage collection remains backend-owned, but the core controls eligibility. A representation or decoder is not eligible for removal while a committed snapshot, retained derivative, verification requirement, migration rollback window, or recovery closure depends on it.

Migration must place and validate the target representation, update portable placement records through a new committed generation, and preserve rollback semantics before old placement eligibility changes.

## 13. Verification semantics

Verification claims remain distinct:

- `AUTHENTICATED_METADATA`: signatures, record graph, namespace bindings, and placement receipts validate.
- `SAMPLED_CONTENT`: declared samples were read, decoded where required, and checked against exact or profile-specific validators.
- `FULL_BYTES`: every selected exact byte was read and verified.
- `RESTORE_DRILL`: selected content and filesystem semantics were restored into an independent destination and evaluated.
- `APPROXIMATE_QUALITY`: named perceptual or application-specific measurements satisfy an explicit non-exact contract.

A stronger claim may include evidence from weaker tiers, but a weaker result never becomes a stronger claim. Approximate quality and exact equality are orthogonal.

`VALIDATE` Processor capabilities report measurements; the core applies policy and records the acceptance decision. Evidence records bind subject scope, representation, implementation, parameters, time, coverage, result, and freshness expectations.

## 14. Security and authority

Every client and extension invocation receives a least-privilege capability grant. Capabilities independently govern:

- Source and namespace reads.
- Byte-range or stream access.
- Output staging writes.
- Network destinations.
- Storage-target mutation.
- Credentials and secret references.
- Accelerator use.
- CPU, memory, scratch, output, and wall-time limits.
- Snapshot export and restore destinations.
- Annotation visibility.

Plugins MUST NOT receive signing keys, raw credential values, arbitrary host paths, global repository access, or deletion authority unless the exact bounded operation requires it and policy grants it. Untrusted parsers and model code SHOULD run in isolated processes or containers.

MCP exposes bounded high-level operations. It does not expose arbitrary extension execution, unrestricted file reads, credentials, repository-private objects, or a general shell. External AI may inspect plans, search subjects, propose annotations or decisions, and request authorized commands; it cannot bypass the same policy and transaction rules as a human client.

## 15. Reference deployment profile

The primary reference deployment is a self-hosted service suitable for common Linux-based NAS and server environments. Stable core semantics remain portable to other operating systems and storage appliances.

The minimum distribution MUST provide:

- A controller or daemon, CLI, local MCP adapter, RRF reader, and clean-recovery tooling.
- A release-specific qualification matrix instead of a global platform assumption.
- An ordinary-tree source profile and at least one stronger snapshot or immutable-import profile where the release platform supports it.
- Suffix and magic-byte detection.
- Exact full-file and chunk fingerprinting.
- A mature exact storage path with deduplication and lossless compression.
- At least one qualified placement backend with durable receipts, readback, and reconciliation.
- Baseline metadata and text extraction and search.
- Durable tag and note CRUD plus portable annotation export/import.
- `SnapshotTree` and `FileAccess` browse and restore, plus a bundled read-only Linux FUSE projection.
- Portable commit and recovery-reference export.
- Component digests, decoder inventory, and license inventory.
- A `doctor` command that validates only the selected profiles and target capabilities.

Optional platform capture helpers have profile-specific privilege, packaging, and filesystem requirements. Their `doctor` checks and failures apply only to those profiles and MUST NOT block the qualified Linux/NAS reference path.

A selected storage policy may use one target or several failure-independent targets. Status and UI output MUST state achieved durability, redundancy, locality, and verification properties rather than calling every single target a backup or every multiple target set redundant.

## 16. Conformance and acceptance criteria

The first conforming product MUST satisfy all of the following:

1. **CKI-AC-01 — NAS-oriented deployment:** The reference service runs self-hosted on a qualified Linux-based NAS or server with native and container deployment evidence.
2. **CKI-AC-02 — Platform boundary:** Platform-specific capture code is isolated behind `CaptureDriver`, and no optional profile becomes a global acceptance dependency.
3. **CKI-AC-03 — Useful default pipeline:** With third-party plugins and models disabled, the product can ingest, deduplicate, losslessly compress, place, search baseline fields, mutate durable tags/notes through CLI, mount a read-only Linux view, browse, verify, and restore supported data.
4. **CKI-AC-04 — Two-step identification:** The default classification path records suffix evidence and magic-byte evidence independently and handles disagreement conservatively before optional `CLASSIFY_LEARNED` and `PARSE` processing.
5. **CKI-AC-05 — Exact unknown fallback:** An unknown file or an absent, crashed, timed-out, low-confidence, or invalid optional processor preserves readable bytes exactly and emits a typed warning.
6. **CKI-AC-06 — Immutable source binding:** An applied plan binds a retained source view, exact staged bytes, or a successfully revalidated revision contract; a mutable path string alone cannot satisfy it.
7. **CKI-AC-07 — Storage reduction:** Repeated exact content and chunks are reused according to the default policy, and the scoped accounting waterfall distinguishes logical, unique, prior reuse, compression, repository overhead, actual growth, index growth, retained source bytes, and zero released source capacity in `RW-MVP-1` without double counting.
8. **CKI-AC-08 — Stable extension seams:** `CaptureDriver`, `Processor`, `RepositoryDriver`, `IndexProvider`, and `QueryProvider` implementations or test doubles can be exchanged without changing core identity, plan, namespace, or authority semantics; `RetrieverDriver` remains a later seam.
9. **CKI-AC-09 — Version-pinned execution:** Plans pin interface-family and capability-profile digests, and upgrades do not affect in-flight or historical operations.
10. **CKI-AC-10 — Provenance completeness:** Every accepted derived representation and retained derivative links inputs, implementation, parameters, coverage, dependencies, validation, and core acceptance.
11. **CKI-AC-11 — Original path projection:** A user can list, stat, read, mount read-only, and restore files through the recorded directory tree even when storage uses compressed and deduplicated packs or objects.
12. **CKI-AC-12 — Search and annotation stability:** Generation-pinned baseline search returns stable logical subjects and paths; rebuilding or replacing the index alters neither stored data identity nor durable tag/note revisions.
13. **CKI-AC-13 — Similarity boundary:** Perceptual or semantic similarity cannot authorize exact deduplication or exact-path substitution.
14. **CKI-AC-14 — Reconciled publication:** A snapshot remains undiscoverable as committed until payload, prepared closure, and publication marker placements are reconciled.
15. **CKI-AC-15 — Clean recovery:** A committed exact snapshot can be authenticated, browsed, and restored without the original SQLite database, search indexes, UI, MCP client, AI service, or plugin registry service.
16. **CKI-AC-16 — Typed ambiguous outcome:** Transport loss after placement triggers reconciliation and cannot be flattened into success or blind retry.
17. **CKI-AC-17 — Capability isolation:** A `Processor` cannot access undeclared paths, credentials, network destinations, signing keys, or `RepositoryDriver` mutation.
18. **CKI-AC-18 — Decoder lifecycle:** An exact reversible representation remains decodable across upgrades, or is migrated and fully reverified before its required decoder is retired.
19. **CKI-AC-19 — Explicit non-exact policy:** An approximate representation can become selected only through a named fidelity contract, compatible validation, durable authority, and an explicit non-exact selector in access and restore results.
20. **CKI-AC-20 — Safe lifecycle:** No last required representation, portable record, placement, or decoder becomes collection-eligible while a committed snapshot depends on it.
21. **CKI-AC-21 — Adapter equivalence:** CLI, initial read-only MCP, and any later REST or WebUI adapter produce equivalent core commands, authority checks, results, and events for every operation they share.
22. **CKI-AC-22 — Safe Linux mount:** The bundled Linux FUSE profile binds one principal, one export root, and one immutable snapshot; verifies `ro,nodev,nosuid,noexec`; refuses `allow_other` and unrestricted option passthrough; preserves qualified raw-name, hard-link, sparse-file, inode, and directory-cookie semantics; and returns `EROFS` for every write-capable open or mutation opcode. Its cache and revocation limitations, including residual access through open handles, page cache, and `mmap`, are measured and disclosed for the signed compatibility tuple.

The later semantic-extension profile additionally requires an external embedding or CLIP-like `Processor` to attach versioned derivatives, an `IndexProvider` to build a new generation, and a `QueryProvider` invoked against that exact `IndexGenerationRef` only after compatibility is validated. None becomes a recovery dependency. That test is not an `RW-MVP-1` release gate.

These criteria define RestoreWeave as a strong NAS storage and search product with a stable kernel, not an empty plugin framework. Advanced media representations, multimodal search, additional platforms, retrieval, and enterprise coordination extend this base without redefining its durable semantics.
