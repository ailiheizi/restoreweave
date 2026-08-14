# Product Requirements

## 1. Product decision

RestoreWeave is a self-hosted, NAS-first content-aware managed data layer for large heterogeneous collections. `RW-MVP-1` is its first read-only managed archive and search profile; later profiles may broaden NAS and enterprise operation only after their additional consistency and recovery semantics are qualified.

For the operator, it performs one complete job: attach an ordinary file tree, measure and reduce its managed footprint, understand and search mixed content, keep the original directory structure usable, and restore exact files with independent evidence. The system remains useful with every optional learned model disabled.

Internally, RestoreWeave coordinates filesystem-shaped data, replaceable processing algorithms, storage engines, and discovery clients. It preserves a stable subject, namespace, representation, and recovery model so those implementations can change without stranding data or changing what a stored file means.

> Store less. Find more. Restore with proof.

RestoreWeave is broader than a backup engine and narrower than a complete NAS operating system. Its durable middle layer owns:

- Content, namespace, representation, snapshot, and operation identity.
- File identification and class-based processing orchestration.
- Policy-controlled representation selection and storage placement.
- Portable recovery records and verification evidence.
- File-shaped browse and restore semantics above repository-private layouts.
- Rebuildable metadata, content, and semantic discovery tied to durable subjects.
- Versioned interfaces through which algorithms and clients can be replaced.

The reference distribution must be useful without custom plugins. `RW-MVP-1` pins and qualifies bundled capture, identification, processing, repository, lexical-index, query, and read-only namespace implementations. Extension points exist so selected implementations can evolve without changing recovery meaning; they do not require operators to assemble a product or accept an unstable third-party plugin marketplace.

The primary product and release criteria target self-hosted Linux-based NAS and server deployments without requiring a particular vendor, distribution, or filesystem. Other source platforms attach through independently qualified `CaptureDriver` profiles.

### 1.1 Normative and supporting documents

- [MVP and Operator Contract](mvp-and-operator-contract.md) freezes the first qualified profile.
- [Core Kernel and Interface Requirements](core-kernel-and-interface.md) define authority and compatibility boundaries.
- [System Architecture](system-architecture.md) defines runtime components and data flow.
- [Driver and Processor Interfaces](driver-and-processor-interfaces.md) define replaceable implementation contracts.
- [File Identification and Extraction](file-identification-and-extraction.md) defines classification and extraction evidence.
- [External AI and Semantic Extensions](external-ai-and-semantic-extensions.md) define AI and semantic components outside recovery authority.
- [CLI and MCP Contract](cli-and-mcp-contract.md) defines typed operator and automation surfaces.
- [Namespace and Content Access](../technical/namespace-and-content-access.md) defines the recoverable file-shaped view.
- [Core Protocol and Reference Userland](../technical/core-protocol-and-reference-userland.md) maps the boundary to implementation work.

If a platform-specific document conflicts with this product decision, the platform-specific statement applies only to its named qualification profile.

## 2. Problem

NAS and self-hosted users accumulate source code, documents, photos, music, video, archives, applications, games, virtual-machine images, datasets, models, and opaque binary state. Existing products usually solve only part of the job:

- Backup engines store and restore bytes but expose limited content understanding.
- Media libraries understand one modality but do not preserve a general filesystem recovery contract.
- Search systems build useful indexes but treat source and recovery as somebody else's problem.
- Compression or neural-codec experiments rarely integrate lifecycle, provenance, namespace reconstruction, and fallback.
- NAS management interfaces route shares and disks but do not provide a replaceable content-processing layer.
- AI tools can classify or summarize content but should not become a privileged data-loss authority.

Users therefore keep too many copies, cannot explain what is unique or reproducible, struggle to find information across mixed collections, and become locked into one storage layout or processing stack.

RestoreWeave addresses the combined problem through one content-aware managed-data loop: inventory heterogeneous content, minimize storage, process and index it through replaceable implementations, preserve recoverability, keep the filesystem view usable, and improve discovery without binding those outcomes to one algorithm or backend.

## 3. Target users and jobs

### 3.1 Primary users

- NAS and homelab operators managing large mixed datasets on local disks, mounted shares, or object storage.
- Technical individuals who self-host because they value control, privacy, replaceability, and long-lived data access.
- Small creative, research, engineering, and archival teams that need mixed-media discovery without surrendering recovery control.

### 3.2 Secondary users

- Storage and NAS projects that need a content-aware middleware layer rather than another repository format.
- External automation or AI harnesses that need typed, bounded access through CLI or MCP.
- Processor, index, repository, namespace-gateway, and media-tool authors integrating through versioned contracts.
- Enterprises evaluating a later single-tenant or managed deployment profile.

### 3.3 Primary jobs

RestoreWeave helps an operator:

1. Connect one or more heterogeneous filesystem roots without reorganizing them.
2. Understand file identity, type, duplicates, extraction coverage, storage cost, and recovery risk.
3. Select safe storage strategies with strong automatic defaults and human control over weaker outcomes.
4. Reduce physical storage through exact deduplication, compression, and later class-specific representations.
5. Search paths, metadata, durable tags and notes, extracted content, and later semantic or multimodal signals through one subject model.
6. Browse and read data through its original directory structure regardless of physical layout.
7. Update processors or indexes without invalidating stored content or losing historical meaning.
8. Verify storage through independent readback and restore on a fresh installation.

## 4. Product promise

RestoreWeave promises six outcomes.

1. **Conservative storage savings.** The default profile removes physical duplication and applies mature lossless compression without weakening recoverability. More aggressive strategies are explicit, versioned, and independently validated.
2. **A durable filesystem view.** Original paths and declared metadata remain browsable and restorable even when bytes are deduplicated, packed, transformed, placed in multiple backends, or indexed separately.
3. **Useful discovery.** The baseline provides lexical search across path, metadata, type, checksum, duplicate, durable tags and notes, and extracted text. Semantic and multimodal discovery extend the same subjects and results later.
4. **Replaceable algorithms.** Processing implementations can be upgraded or replaced while durable inputs, outputs, provenance, and dependency closure remain understandable.
5. **Safe automation.** The system is highly automatic, but only a human or an already-published policy can accept omission, lossiness, deletion, or another weaker recovery outcome.
6. **Truthful recovery.** A write is not called recoverable until the required placement, publication, and verification evidence exists.

The differentiated product is the combination of these outcomes. RestoreWeave must not collapse into a thin wrapper around one repository, a media catalog for one file type, or a collection of uncoordinated plugins.

### 4.1 Product modes and savings boundary

RestoreWeave distinguishes three modes:

| Mode | Authoritative bytes | Product value | Storage-savings claim |
| --- | --- | --- | --- |
| **Observe** | Existing source remains authoritative | Inventory, duplicate analysis, metadata extraction, and discovery | No net capacity claim |
| **Managed archive** | RestoreWeave repository is an exact managed copy with a read-only file-shaped view | Verified storage reduction, discovery, browse, and restore | Repository footprint is measured; whole-system savings require retiring another copy |
| **Primary writable NAS** | RestoreWeave would accept authoritative writes through gateways | Unified live storage and discovery | Future profile only |

`RW-MVP-1` qualifies managed archive mode. It does not silently count a source copy that still exists as saved capacity. A source may become eligible for retirement only after a separately reviewed migration proves exact recoverability, required placement, decoder availability, and rollback conditions. Automatic source deletion is disabled by default. Writable SMB, NFS, WebDAV, S3, or FUSE behavior is not implied by the read-only snapshot namespace.

Managed archive mode still provides ordinary file-shaped access: the first distribution includes a bundled read-only Linux FUSE projection over the authenticated snapshot namespace. It supports browse and read workflows but rejects create, write, rename, unlink, and metadata mutation. It is a presentation adapter over `SnapshotTree` and `FileAccess`, not a writable primary filesystem or another extension family.

### 4.2 Product layers

RestoreWeave is evaluated as three nested layers. A lower layer is not a substitute for the layer above it.

| Layer | Required outcome |
| --- | --- |
| Operator product | An operator can attach a heterogeneous tree, understand it, store selected data efficiently, search it, access it through original paths, verify it, and restore it. |
| Reference distribution | The supported build ships a controller, qualified capture, a mandatory exact repository lane, default classification and processing, durable tag/note records, generation-pinned baseline lexical search, a read-only Linux FUSE projection, verification, recovery export, and CLI/JSON/MCP bindings. |
| Replaceable implementation seams | `CaptureDriver`, `Processor`, `RepositoryDriver`, `IndexProvider`, `QueryProvider`, and later `RetrieverDriver` allow selected implementations to evolve without changing durable meaning. |

The product is not conforming when it supplies only the third layer. Optional implementations extend a complete reference distribution; they do not create the basic storage, search, or recovery experience on the operator's behalf.

## 5. Product principles

1. Preserve recovery meaning before optimizing representation size.
2. Unknown readable content defaults to exact preservation.
3. The original namespace is a durable product surface, not a temporary scan artifact.
4. Identification evidence and policy decisions are separate records.
5. Replace algorithms at stable data contracts, not every internal function.
6. Ship strong defaults before asking users to assemble a pipeline.
7. Derived indexes are rebuildable; user data and accepted recovery records are durable.
8. AI is an optional processor or external client, never the recovery authority.
9. Incremental operation, deletion semantics, and upgrades are first-class lifecycle concerns.
10. Verification is based on actual evidence, not process exit codes or model confidence.
11. Platform-specific optimizations are profiles, not global architecture.
12. CLI and machine interfaces express the same typed operations.
13. Basic user tags and notes are durable, exportable product data; richer semantic structures may remain staged.

## 6. Product boundary

### 6.1 Authoritative core

The core owns facts and decisions that require one trusted arbiter:

- Source, capture, subject, content, namespace, representation, snapshot, plan, and operation identities.
- Immutable observations, accepted policies, plan digests, and exact-fallback rules.
- Processor selection records, provenance, dependency references, and output admission.
- Repository placement truth and recovery contracts.
- Durable operation state, idempotency, cancellation, fencing, and reconciliation.
- Portable Recovery Record Format publication and verification acceptance.
- `SnapshotTree`, `FileAccess`, and subject-resolution semantics.
- Versioned user tag and note records, revision checks, tombstones, and portable export truth.
- Generation tracking for derived metadata and indexes.

The core is a finite data and recovery coordinator. It is not a general workflow engine, model runtime, vector database, media server, or autonomous agent.

### 6.2 Replaceable stages

The following algorithms vary enough to justify external or replaceable implementations:

| Stage | Responsibility | Core acceptance rule |
| --- | --- | --- |
| Capture | Expose a scoped source view and consistency evidence | Capture identity and declared consistency are recorded |
| Learned classification | Add optional model- or domain-specific evidence after built-in suffix and magic-byte evidence | Evidence never authorizes omission |
| Parse | Validate structure, enumerate members, and refine typed coverage | Parser failure selects exact fallback rather than omission |
| Extract | Produce metadata, text, thumbnails, segments, or descriptors | Coverage and provenance are explicit |
| Fingerprint | Produce auxiliary cryptographic checksums, perceptual, acoustic, visual, structural, or domain comparison features | The host-owned exact lane alone computes or independently verifies canonical content identity; feature type and collision limits remain explicit |
| Transform | Produce raw, compressed, normalized, transcoded, or learned representations | Outputs remain staged until independently validated |
| Validate | Test byte identity, reconstruction, similarity, or profile-specific quality | A processor cannot accept its own claim without host policy |
| Place | Store admitted representations in a repository or storage tier | Durable receipts and reconciliation are required |
| Index | Build metadata, text, vector, graph, or multimodal indexes | Index generation remains rebuildable and subject-bound |
| Rank | Combine lexical, structural, semantic, or user signals | Results cite subjects and provider provenance |
| Retrieve | Reacquire a pinned external artifact in a later profile | Fetch evidence is not durable placement evidence |

These are conceptual pipeline stages, not a list of public interface families. Learned classification, parsing, extraction, fingerprinting, transformation, validation, and index preparation use the capability-oriented `Processor`; placement uses `RepositoryDriver`; indexing uses `IndexProvider`; ranking and query fusion use `QueryProvider`; later retrieval uses `RetrieverDriver`. Stable stage and artifact semantics matter more than internal process topology.

### 6.3 Reference distribution

The first distribution includes:

- A self-hosted controller usable as a native process or container where supported.
- Local and mounted filesystem capture with explicit consistency reporting.
- Deterministic extension and magic-byte identification.
- Independent SHA-256 content identity and duplicate grouping.
- A mature exact repository integration providing compression, deduplication, encryption, and readback.
- Qualified default metadata and text extraction for common formats.
- Durable, versioned tag and note CRUD with portable export independent of index storage.
- An operational catalog and bundled lexical metadata/content/annotation index.
- A read-only snapshot namespace with browse, bounded read, and restore.
- A bundled read-only Linux FUSE adapter over that namespace for NAS-usable file access.
- Human-readable CLI plus stable JSON/JSONL.
- A local read-only MCP adapter for external automation and AI harnesses.
- Portable recovery records and a clean-install restore path independent of the operational catalog.

The distribution must remain useful when every optional processor is disabled. Its pinned bundled routes must work without processor installation or user-authored workflow graphs. Installation and upgrade of later implementations remain observable and generation-aware.

### 6.4 External intelligence

RestoreWeave does not embed a general LLM harness, prompt loop, agent memory, or model router.

An AI implementation may:

- Supply learned classification, OCR, ASR, captions, embeddings, fingerprints, transformations, ranking, or validation evidence through a bounded processor contract.
- Act as an external client through CLI or MCP.

AI output is evidence, a derived artifact, or a proposal. It cannot weaken exact fallback, approve source deletion, publish a representation, accept verification, or mutate an immutable plan.

## 7. Core concepts

### 7.1 Source and capture

A **Source** is a configured filesystem-shaped root. A **Capture** is the exact view read by one inventory or ingest generation, with a declared consistency class and lifecycle.

The core does not infer snapshot consistency from a platform name. A snapshot-capable driver may declare an atomic view. A mounted or live path driver must report weaker consistency and detect mutation where possible.

### 7.2 Subject and content identity

A **Subject** is a durable reference to a source entry, snapshot entry, file version, directory, collection, representation, or segment. A **ContentIdentity** binds exact bytes to length and an independent cryptographic digest.

Paths identify namespace positions, not byte identity. Duplicate paths may share a content identity while retaining distinct names and metadata.

### 7.3 Classification evidence

Classification is an evidence set, not one mutable MIME string. Evidence records include method, implementation, version, inspected range, output, confidence where applicable, and conflicts.

The default evidence ladder is:

1. Filename suffix and path context.
2. Magic bytes and container signatures.
3. Strict structural parsing where a qualified processor is available.
4. Optional learned or AI classification.

Later evidence may refine a class but cannot erase earlier evidence or silently change a committed representation contract.

### 7.4 Processor

A **Processor** advertises typed capabilities and receives bounded immutable inputs. Capabilities may perform learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, or index preparation.

Each result records implementation and configuration identity, dependency digests, coverage, resource use, warnings, and output schemas. Processor output remains non-authoritative until the core validates and admits it.

### 7.5 Representation kind, recovery claim, and lifecycle

A **Representation** is a stored or derivable form of a subject. Representation kind is separate from its recovery claim and lifecycle:

- `EXACT_RAW`: the original byte sequence under the canonical exact content identity.
- `EXACT_REVERSIBLE`: a losslessly encoded representation with a pinned decoder closure and verified original digest.
- `APPROXIMATE`: a representation admitted only under an explicit normalized, perceptual, semantic, or functional recovery claim.
- `DERIVED`: an artifact for discovery or presentation that is not a source replacement.

Recovery claims use the subject, relation, validator, and policy model in [Recovery Fidelity Requirements](recovery-fidelity.md). Lifecycle uses the canonical `AUTHORITATIVE_DATA`, `RECOVERABLE_REPRESENTATION`, `REBUILDABLE_DERIVATIVE`, or `EPHEMERAL_CACHE` vocabulary. A pinned external acquisition recipe is a source binding, not a representation or durable placement, until bytes are acquired, host-hashed, validated, admitted, and placed.

The MVP publishes only exact source recovery. Repository-private compression and deduplication are physical implementation details beneath that contract. Later representation kinds and claims require explicit policy and qualification.

### 7.6 Namespace

A **Namespace** maps original paths and metadata to content and representation identities for one immutable snapshot. It remains independent of repository-private chunk, pack, and object layouts.

### 7.7 Plan and decision authority

A **Plan** is an immutable, digest-addressed blueprint covering scope, processing, representations, placements, expected savings, verification, risks, and unresolved decisions.

Review creates a successor plan. A human or previously published policy supplies authority for exclusions or weaker representations. A processor, search result, MCP client, or conversational confirmation cannot approve its own recommendation.

### 7.8 Derived index generation

An **IndexGeneration** binds an index provider and configuration to a precise set of subjects and processor artifacts. It can be discarded and rebuilt. Search results cite durable subject references and the generation that produced them.

### 7.9 Publication and verification

A **Publication** is a committed recoverable snapshot backed by repository receipts and a portable recovery closure. A **VerificationRecord** captures independent evidence such as authenticated metadata, sampled readback, full-byte readback, or an exact restore.

## 8. Primary product workflows

### 8.1 Ingest, minimize, and publish

```text
connect source
-> capture and inventory
   |-> mandatory exact hash -> duplicate accounting -> exact RepositoryDriver lane
   |-> suffix -> magic -> optional learned or structural classification
       -> class-based Processor route -> derived artifacts and candidates
-> estimate exact placement, optional processing, indexes, and total overhead
-> review immutable plan
-> reconcile exact placement
-> perform required placement and exact-lane readback verification
-> publish portable namespace and commit
-> build or update the baseline index
-> run later sampled, full-byte, exact-restore, or clean-install drills
```

The exact lane is mandatory and independent. Classification and processing improve understanding, discovery, and later representation choices, but their failure cannot prevent a readable file from entering the exact fallback set.

### 8.2 Discover and access

```text
query path, metadata, type, checksum, duplicate, tag, note, or extracted text
-> resolve result to durable SubjectRef
-> inspect provenance and available representations
-> browse namespace, open bounded content, or mount the read-only Linux FUSE view
```

Later semantic providers add embeddings, CLIP, multimodal ranking, and external enrichment to the first step without changing subject resolution or file access.

### 8.3 Update and reprocess

```text
capture changed source generation
-> reuse unchanged content identities
-> process only changed or invalidated subjects
-> publish a new immutable snapshot and index generation
-> retain old recovery meaning
```

Updating a processor may invalidate derived artifacts and indexes. It must not silently reinterpret existing stored representations.

### 8.4 Recover

```text
discover signed publication
-> authenticate portable recovery closure
-> reconstruct namespace
-> read selected representations
-> restore paths
-> compare exact digests and report metadata fidelity
```

Recovery must not require the original controller database, optional processors, semantic index, model registry, WebUI, or AI harness.

## 9. Functional requirements

### FR-01: Platform-neutral source onboarding

RestoreWeave must accept local or mounted filesystem roots through a `CaptureDriver`. Every capture declares source identity, root mapping, supported metadata, consistency class, read scope, and release semantics.

The reference profile runs on a Linux-based NAS or server using generic local or mounted filesystem capture and does not require a particular filesystem. Native snapshot drivers for ZFS, Btrfs, LVM, vendor APIs, or other platforms are optional improvements. Failure or absence of one optional driver must not block another qualified capture profile.

Every achieved root must use an opaque `CaptureRootBinding` that retains one trusted root anchor and binds the filesystem or volume, mount, root object, resolver policy, symlink and nested-mount policy, special-file policy, and snapshot or validated-live basis. On Linux, authoritative traversal and reads are descriptor-relative and use qualified `openat2` semantics or an independently qualified equivalent; final-component-only no-follow opens and string path containment checks are insufficient. Runtime descriptor numbers and mutable exposure paths are never durable identity.

For live paths, the scanner must detect changes during reads where possible, retry within a bounded policy, and expose unresolved instability. It must never label a live mounted path as atomically snapshotted without evidence. It must pin an entry as a regular file before a potentially blocking content open; FIFOs, sockets, devices, and other special objects are metadata-only in the generic profile.

### FR-02: Complete inventory and exact identity

Every selected namespace entry must be traversed or receive an explicit failure record. Inventory preserves raw names, entry type, size, times, permissions, links, sparse information where supported, platform metadata, source and capture identity, and digest state.

Exact content identity uses an independently recorded cryptographic digest and length. Hashing may be progressive for planning, but any exact duplicate or recovery claim must bind a completed digest before publication.

### FR-03: Layered file identification

The default system must collect suffix evidence before reading magic bytes, then may invoke structural or learned identification. It must retain all evidence and conflicts.

Identification runs beside, not in front of, the mandatory exact-content lane. A classifier is never required to compute exact identity, group byte-identical content, or make readable bytes eligible for exact placement.

Routing uses a normalized content-class decision derived from the evidence set. A conflict, low-confidence learned result, unsupported nested format, parser failure, or unknown type must select a safe generic route and exact fallback.

Identification itself cannot authorize omission, deletion, external retrieval, or a lossy representation.

### FR-04: Class-based processing

Every readable file begins the independent exact lane. Its identification branch then uses a host-owned `IdentificationRouteRef`, limited to optional learned classification and classification-refining parsing, before the host publishes a final or explicitly accepted unresolved ClassificationRecord. Only then does the host build the ordinary `ProcessingRouteRef` for parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation.

The processing route is an extensible analysis and representation branch, not the only ingest path. Exact hashing and exact placement eligibility are host-owned baseline work and are not modeled as a `Processor` capability. Optional processing may finish after exact publication and expose explicit pending, partial, stale, failed, or unavailable discovery coverage.

The processor interface must support:

- Capability discovery and version negotiation.
- Typed input and output schemas.
- Immutable subject references and bounded content handles.
- Exactly one immutable route reference per invocation: `IdentificationRouteRef` or `ProcessingRouteRef`.
- A canonical artifact envelope, host-owned staging handles, sealing, host digesting, schema validation, policy admission, and immutable downstream artifact handles.
- `RUN_STAGE` plus historical `DECODE_REPRESENTATION` operations for transform profiles that create retained recoverable representations.
- Determinism and dependency declarations.
- CPU, memory, accelerator, temporary storage, time, network, and disclosure budgets.
- Cancellation, partial coverage, and typed failure.
- Provenance sufficient to distinguish output generations.

Processor failure on readable content must return to the exact generic route. A profile-specific processor requirement MAY block only that processing branch, derived representation, or stronger profile claim. It MUST NOT block capture, inventory, host-owned exact hashing, exact placement eligibility, required exact-lane verification, or exact publication of readable bytes.

### FR-05: Storage minimization and representation selection

The default exact profile must provide:

- Duplicate grouping by completed content identity.
- One logical namespace entry per original path even when contents are shared.
- Mature repository-managed chunking, compression, deduplication, encryption, and integrity checks.
- Separate reporting of logical source bytes, exact duplicate savings, compression estimates, physical stored bytes, index overhead, and transfer bytes.

Class-specific transformations may be proposed through processors. A transformed exact representation must pass host-controlled round-trip digest validation before admission. A perceptual, reacquirable, rebuildable, or generative result cannot satisfy an exact plan.

Unknown content, missing dependencies, processor unavailability, invalid output, or insufficient validation must preserve the readable source exactly.

### FR-06: Recoverable filesystem namespace

Every published snapshot must expose one authenticated file-shaped view independent of storage layout. It supports:

- Root lookup and component-by-component path resolution.
- Paginated directory listing and metadata lookup.
- Bounded and streaming file reads.
- Original-path restore.
- Symlink and hard-link reconstruction where supported.
- Explicit representation selection without treating similarity as identity.
- Explicit reporting of unsupported filesystem metadata.

`RW-MVP-1` must bundle a read-only Linux FUSE projection over `SnapshotTree` and `FileAccess`. One mount binds one authenticated principal, one authorized export root, and one immutable snapshot. It supports lookup, directory enumeration, attributes, symbolic-link reads, bounded or streaming regular-file reads, raw-name-safe mapping, stable inode and hard-link identity within the mount, sparse-file semantics, scoped directory continuation, and stable snapshot pinning. It uses a fixed safe mount profile including read-only, `nodev`, `nosuid`, and `noexec`, does not enable `allow_other` in the MVP, and rejects every write-capable open and mutation operation with `EROFS`. Cache, page-cache, open-handle, mmap, authorization-expiry, unmount, and repository-amplification behavior must pass qualification; if revocation cannot meet its declared bound, the mount is labeled a local-trust surface. It cannot present a live writable-NAS claim. SMB, NFS, WebDAV, S3, media-server, and alternate FUSE projections may be later presentation adapters, but none may create a second namespace truth.

### FR-07: Discovery and semantic direction

The baseline search experience must cover:

- Path and filename.
- File and content class.
- Size, time, source, snapshot, and declared metadata.
- Exact checksum and duplicate group.
- User-authored tags and note text.
- Extracted text and common media metadata where a qualified default extractor succeeded.
- Processing status, warnings, provenance, and available representations.

The reference distribution ships one bundled lexical `IndexProvider` and `QueryProvider` implementation. Search results must resolve to stable subjects and the original namespace. Indexes are derived and rebuildable; loss of an index cannot remove recovery information or user-authored annotations.

The public discovery model must permit later alternate lexical, vector, hybrid, graph, CLIP, acoustic, multimodal, and external-knowledge providers. Those providers may return scores and evidence but cannot change recovery contracts. Embeddings and CLIP are later or external implementations, not requirements for the exact MVP and not excluded from the product direction.

Basic user-authored tags and notes are Phase 1 durable product data, not an external-catalog dependency. The core stores them as versioned records bound to stable `SubjectRef` values with author, time, revision, visibility, provenance, and tombstone state. The CLI provides create, read/list, update, delete, and portable export operations with optimistic revision checks. Annotation changes feed the lexical index without re-ingesting content, survive index deletion, and can be exported and re-imported without losing subject bindings.

Collections, ratings, accepted relationship graphs, recovery-intent services, and machine-suggested annotation workflows are later capabilities. They must remain distinguishable from operator-authored tags and notes.

### FR-08: Incremental lifecycle

RestoreWeave must distinguish source mutation, new snapshot publication, representation replacement, index rebuild, and repository retention.

- Unchanged content identities should reuse admitted representations and processor artifacts when provenance and policy still match.
- Changed content creates a new subject version and does not rewrite historical snapshots.
- A deleted source path becomes a tombstone or absence in a later namespace generation; it does not erase prior published snapshots.
- Processor or model upgrades create new derived generations and may run incrementally.
- Representation migration must verify the new placement before retiring an old placement.
- Source deletion and destructive garbage collection are disabled by default and require a separate reviewed lifecycle policy.

### FR-09: Planning and human authority

Planning performs no repository or restore-destination mutation. A plan must explain:

- Source and capture scope.
- Identification conflicts and unsupported content.
- Selected processing route and dependency closure.
- Proposed representations and placements.
- Expected logical and physical savings.
- Indexing scope and estimated overhead.
- Recovery fidelity and verification procedure.
- Exclusions, weaker outcomes, unresolved decisions, and required authority.

Apply binds the exact plan digest and rejects stale source, capture, policy, processor, repository, or destination facts. Revision creates an immutable successor.

The default automatic path may apply monotonic exact changes. Any new exclusion, lossiness, source deletion, dependency weakening, placement reduction, or unresolved risk requires durable human or published-policy authority.

### FR-10: Repository placement

Repository implementations remain replaceable. A repository driver must provide durable placement receipts, immutable lookup, bounded readback, verification, restore access, and reconciliation after ambiguous outcomes.

The first distribution may use one mature repository engine. One repository is one placement and must never be described as redundant. Later policy may require multiple failure-independent placements or storage tiers.

Repository-private chunks, packs, objects, encryption layout, and physical garbage collection remain engine concerns. RestoreWeave owns the portable logical snapshot and path-to-representation mapping.

### FR-11: Portable publication and verification

A successful managed-ingest apply must publish an authenticated Recovery Record Format root containing at least:

- Source, capture, namespace, content, representation, and snapshot identities.
- Accepted plan digest and recovery contract.
- Protected, explicitly unprotected, and blocked entries.
- Content digests, dependency closure, repository receipts, and root mappings.
- Filesystem fidelity and capture-consistency claims.
- Component and schema compatibility information.

User tags and notes may change after an immutable snapshot publication, so their revision stream is exported as a separately authenticated portable annotation bundle bound to `SubjectRef` values and snapshot identities. Recovery export may include or reference a pinned annotation-bundle revision without rewriting the original snapshot commit. Losing a lexical index must never lose this bundle or its authoritative records.

The crash-safe publication sequence preserves distinct `PAYLOAD`, `PREPARED_CLOSURE`, and signed `PUBLICATION_COMMIT` facts. Orphan payloads or prepared closures are not published snapshots.

Namespace-record conversion and publication require a validated `CaptureRootBindingRecord` for every achieved root. The gate verifies root, filesystem or volume, mount, snapshot or validated-live basis, resolver policy, boundary coverage, special-file policy, and current lease or hold evidence. A terminal scanner status, unchecked boundary result, absolute path, or final-component-only no-follow open cannot independently authorize publication.

Verification levels remain distinct:

- Authenticated metadata and publication closure.
- Deterministic sampled-content readback.
- Full-byte readback.
- Exact test restore.
- Clean-install recovery drill.

Upload completion, processor success, index availability, and repository process exit are not verification.

### FR-12: CLI and MCP interfaces

The canonical human and scripting interface is the CLI. Every non-content command supports human, JSON, or JSONL output over the same typed command, result, reason, and event model. The first profile includes read-only namespace mount/unmount commands and basic tag/note create, list, update, delete, and export commands; annotation mutation uses explicit subject references and expected revisions.

The first MCP profile uses local stdio and is read-only. It exposes bounded inspection, status, search, namespace, annotation-read, verification, processing, and existing-plan information to external harnesses without embedding a harness in RestoreWeave. It cannot create, revise, approve, abandon, or apply an ingest or restore plan; mutate a repository, restore destination, policy, annotation, or job; or request an arbitrary processor invocation.

MCP cannot execute arbitrary shell text, access ambient credentials, read unrestricted paths, or treat natural-language confirmation as authority. Later mutation grants remain explicit, bounded, digest-pinned, and subject to the same core checks as the CLI.

A REST service or WebUI may later bind the same typed operations. It must not introduce a second policy or recovery state machine.

### FR-13: Isolation and security

Processors receive least-privilege opaque handles, bounded resources, and no ambient filesystem, repository, network, secret, or signing authority. Remote processing requires an explicit egress profile covering destination, disclosure scope, minimization, budget, credential reference, validity, and revocation.

Ordinary recovery records contain no plaintext credentials or private signing keys. Secrets remain host-scoped references or independently managed recovery material.

Untrusted file parsing must be isolated according to the selected deployment profile. Expansion, recursion, decompression-bomb, and parser-resource limits are mandatory.

### FR-14: Disaster independence

Exact browse and restore must work without:

- The original source host.
- The operational catalog or SQLite database.
- Optional capture, processor, search, embedding, CLIP, or ranking implementations.
- A model registry, AI harness, WebUI, MCP client, or REST service.
- The original plugin registry.

A compatible RestoreWeave reader, repository access, portable recovery closure, required credentials, and an independent trust anchor must be sufficient for the declared exact recovery profile.

Tag and note recovery must work without the lexical index by importing a valid portable annotation bundle into a compatible catalog. The index may then be rebuilt from namespace, extracted artifacts, and authoritative annotation records.

### FR-15: Operations and observability

Long-running operations must be durable, idempotent, cancellable, resumable, and observable through ordered events. Ambiguous external outcomes enter reconciliation rather than silent retry.

Status must separate:

- Inventory and processing coverage.
- Storage and transfer savings.
- Placement state and failure-domain claims.
- Index freshness and provider generation.
- Verification level and age.
- Blocked or explicitly weaker outcomes.
- The next operator action.

## 10. Opinionated defaults

| Area | Reference default |
| --- | --- |
| Product shape | Self-hosted NAS-first storage and discovery controller; `RW-MVP-1` is its managed-archive profile |
| Reference environment | NAS/server-oriented; no required vendor, Linux distribution, or filesystem |
| Source | One local or mounted filesystem root with a declared consistency class |
| Capture | Generic validated read-only tree; snapshot-capable drivers are optional profiles |
| Platform-specific capture | Optional `CaptureDriver` profiles, never the product identity or global release gate |
| Operational catalog | SQLite or another local embedded projection; not recovery authority |
| Identification | Suffix evidence, then magic-byte evidence; optional structural and learned processors |
| Content identity | SHA-256 plus length; additional hashes may be derived |
| Exact ingest | Host-owned mandatory lane starts independently of optional classification and processors |
| Unknown content | Exact fallback with visible warning when readable |
| Default processing | Safe metadata and text extraction for qualified common formats; generic exact route for everything else |
| Storage | One mature exact repository integration with compression and deduplication |
| Recovery fidelity | Exact bytes and declared filesystem metadata fidelity |
| Search | Bundled lexical path, metadata, type, checksum, duplicate, tag, note, and extracted-text baseline |
| Embeddings and CLIP | External or later processors and index providers |
| Annotations | Durable versioned tag and note CRUD plus portable export |
| Namespace | Read-only authenticated `SnapshotTree` and `FileAccess`, with bundled Linux FUSE projection |
| Human interface | CLI |
| Automation interface | Stable JSON/JSONL and local read-only MCP |
| Embedded AI harness | None |
| Source deletion | Disabled |
| Destructive repository pruning | Disabled by default |
| WebUI and REST | Optional later adapters |
| P2P and magnet | Non-core later retrieval profile |

## 11. MVP scope and staged product direction

### 11.1 First qualified profile

The first profile proves the complete NAS data-layer loop:

- Local or mounted source inventory under an honest consistency claim.
- A mandatory exact hash and repository lane that remains available under every optional processor failure.
- Extension and magic-byte identification.
- Class-based routing through qualified default processors.
- Exact hashing, duplicate grouping, compression, deduplication, and placement.
- Immutable original-path namespace publication.
- Bundled read-only Linux FUSE access to the published namespace.
- Durable tag and note CRUD with portable export.
- Baseline lexical metadata, content, checksum, duplicate, tag, and note search.
- Incremental reruns and processor-generation tracking.
- Verification and clean-install exact restore.
- CLI and read-only MCP access.

### 11.2 Managed adoption and capacity release

The first post-MVP profile adds an explicit reviewed migration and source-retirement workflow. It may release source capacity only after exact recovery, placement sufficiency, clean restore, rollback retention, a grace period, and a fresh operator-bound plan all pass. It never enables autonomous source deletion and never treats a processor, similarity result, or repository upload as retirement authority.

This profile also proves practical replaceability through at least one independently implemented Processor and RepositoryDriver, side-by-side reprocessing or migration, generation comparison, rollback, and decoder-retention enforcement.

### 11.3 Semantic and multimodal expansion

The next discovery stage adds external OCR, ASR, captions, embeddings, CLIP, acoustic fingerprints, vector or hybrid indexes, richer query ranking, external metadata enrichment, collections, ratings, and relationship graphs.

These are product capabilities delivered through replaceable processors and index providers. They remain outside exact recovery dependencies and can be upgraded or rebuilt independently.

### 11.4 Alternate representation expansion

Later qualified profiles may add lossless class-specific codecs, normalization, perceptual media representations, neural or foundation codecs, reacquisition, or rebuild recipes.

Every profile must define validator thresholds, provenance, dependency closure, migration behavior, exact fallback, and explicit user authority. Similarity is never identity.

### 11.5 NAS and enterprise expansion

Later profiles may add SMB, NFS, WebDAV, S3, media-server, alternate FUSE, or writable gateways, multiple repositories, tiering, replication, HA, multitenancy, RBAC, audit integration, remote REST, and enterprise lifecycle policy. These extensions use the same durable namespace and recovery contracts. Read-only Linux FUSE is already part of `RW-MVP-1`; writable gateways remain separate.

## 12. MVP non-goals

The first qualified profile does not require:

- A particular NAS brand, operating-system vendor, Linux distribution, filesystem, or snapshot API.
- An embedded LLM, general AI harness, model router, or agent runtime.
- Embeddings, CLIP, vector search, or multimodal ranking to complete exact storage and recovery.
- Lossy or generative source substitution.
- Automatic source deletion or destructive garbage collection.
- Authoritative external reacquisition, magnet, BitTorrent, or swarm storage.
- A WebUI, public REST listener, A2A protocol, or workflow marketplace.
- A writable primary NAS filesystem or synchronization engine.
- Collections, ratings, relationship graphs, or semantic knowledge management beyond basic tags and notes.
- Multiple repositories, erasure coding, or a redundancy claim.
- Multitenancy, high availability, enterprise identity, compliance, or legal hold.
- Application-consistent database, virtual-machine, container, game, or bare-metal capture.

These exclusions constrain the first release, not the long-term data-layer architecture.

## 13. Delivery roadmap

### Phase 0: Product and contract validation

- Validate the NAS-first workflow on representative local and mounted collections.
- Measure inventory cost, unknown formats, duplicate savings, compression estimates, search usefulness, and operator trust.
- Freeze core identities, exact fallback, processor artifacts, namespace, and publication semantics.
- Prove one generic live capture profile and at least one optional snapshot-capable profile without making either platform universal.

### Phase 1: Exact intelligent data-layer MVP

- Self-hosted controller, CLI, local catalog, and read-only MCP.
- Generic capture, deterministic identification, processor routing, exact repository placement, and portable namespace.
- Default common metadata and text extraction.
- Durable tag and note CRUD, portable annotation export, and lexical annotation indexing.
- Bundled read-only Linux FUSE access over the authenticated namespace.
- Baseline search, incremental operation, verification, recovery export, and clean-install restore.
- Strong defaults and installation diagnostics.

### Phase 2: Managed adoption, capacity release, and replaceability

- Independently implemented processor and repository integrations.
- Processor upgrade, invalidation, and reindex workflows.
- Reviewed migration and source-retirement workflow with exact recovery, placement sufficiency, grace-period, rollback, and post-retirement restore gates.
- Representation and repository migration with decoder-retention enforcement.

### Phase 3: Semantic discovery and advanced storage profiles

- External embedding, CLIP, OCR, ASR, caption, vector, and hybrid search providers.
- Subject-bound collections, ratings, relationship graphs, machine suggestions, and external enrichment.
- Optional SMB, NFS, WebDAV, S3, media-server, or alternate FUSE gateways and companion UI.
- Lossless class-specific transforms and tier placement.
- Multiple failure-independent repositories.
- Perceptual media, learned compression, VAE or RWKV-style experiments, reacquisition, and rebuild profiles.
- Explicit migration, validator, dependency, and fallback qualification.

### Phase 4: Broader NAS and enterprise operation

- Remote control adapters, managed multi-node operation, policy administration, auditing, and enterprise deployment profiles.
- Writable gateways only after conflict, transaction, and recovery semantics are separately designed.

## 14. Success metrics

### 14.1 Product value

- At least 80 percent of target operators can explain what is stored, deduplicated, unsupported, blocked, and recoverable from one report.
- At least 50 percent of representative collections reveal a duplicate, recovery risk, stale copy, or discovery improvement the operator did not already know.
- Median exact physical storage reduction is measured separately for duplicate elimination, repository compression, backend reuse, and total repository overhead.
- Any later non-exact policy reduction is reported as a separate weaker-fidelity metric and is never added to exact savings.
- At least 70 percent of evaluators successfully find a known item using metadata or content terms without knowing its path.
- At least 80 percent can create, revise, export, re-import, and find a tag or note without installing a semantic provider.
- At least 50 percent run a second incremental ingest, reprocessing, search, or verification operation within 30 days.

### 14.2 Recovery quality

- 100 percent namespace accounting for the selected scope.
- Zero silent omissions.
- 100 percent exact fallback for readable unknown or processor-failed content.
- 100 percent byte equality for exact restores in the qualified corpus.
- 100 percent detection of injected corruption covered by the declared verification mode.
- No recoverable or verified state without required repository and publication evidence.
- Zero authoritative namespace publication from an unchecked, stale, substituted, or path-string-only capture root.

### 14.3 Replaceability

- At least one default processor can be replaced by an independent implementation without changing subject, namespace, or recovery semantics.
- Processor upgrades invalidate and rebuild only affected derived artifacts.
- Disabling every optional AI or semantic component leaves exact inventory, storage, browse, verification, and restore functional.
- Loss and rebuild of the baseline index does not change published snapshot meaning.
- Loss and rebuild of the baseline index does not lose tag or note records, and portable annotation import restores their exact revisions and subject bindings.

### 14.4 Operations

- Interrupted jobs resume or reconcile without contradictory logical publications.
- Incremental runs avoid reprocessing unchanged subjects whose provenance remains valid.
- One-repository deployments are never described as redundant.
- Search and status expose index freshness and processing coverage rather than silently returning incomplete results.
- A fresh installation can recover through portable records without the original catalog or plugin registry.

## 15. Product risks

| Risk | Response |
| --- | --- |
| The product is mistaken for a platform-specific backup tool | Keep NAS/self-hosted workflows, generic capture semantics, and Linux reference deployment at the center; qualify platform-specific capture separately |
| The product becomes a thin repository wrapper | Make processing, namespace, discovery, representation provenance, and portable recovery first-class product surfaces |
| The product becomes an empty plugin framework | Ship a complete default pipeline and delay ABI stability until independent implementations validate the seams |
| Plugin count creates operational chaos | Use capability discovery, constrained stage semantics, curated defaults, provenance, health checks, and generation-aware upgrades |
| Classification creates false confidence | Preserve independent evidence, surface conflicts, and keep exact fallback |
| Semantic indexes are expensive or stale | Make them optional, budgeted, generation-bound, observable, and rebuildable |
| Lossy savings are mistaken for exact recovery | Use explicit recovery classes, validators, policy gates, and representation labels |
| Repository or index layouts become lock-in | Preserve portable namespace and recovery records above backend-private layouts |
| NAS live paths change during capture | Declare consistency honestly, detect mutation, retry narrowly, and block affected claims |
| A path appears confined while an ancestor, mount, or snapshot is replaced | Retain a root anchor, resolve every component relative to it, bind durable root and mount evidence, and block namespace publication until revalidation passes |
| Watcher delivery is incomplete or unsupported on the source filesystem | Treat watcher events as hints, expose coverage, invalidate uncertain checkpoints, and require a complete baseline before deletion evidence |
| AI receives excessive authority | Restrict AI to bounded processors or external clients and keep decisions in the core policy boundary |
| Source deletion destroys the safety story | Keep deletion disabled by default and require separately reviewed migration and retention policies |
| Search scope overwhelms the MVP | Ship metadata and extracted-text discovery first while preserving the semantic provider contract |
| User annotations become disposable index state | Store tag and note revisions durably, export them portably, and rebuild lexical projections from authoritative records |

## 16. Research basis

The product direction combines lessons from mature backup repositories, content-addressed storage, self-hosted catalogs, file identification systems, document and media extraction pipelines, and semantic search systems. No one reference project supplies the complete product.

Supporting evidence is recorded in:

- [Competitor and Component Research](../references/competitor-research.md)
- [Demand Research](../references/demand-research.md)
- [Thin-Core Product Research Audit](../references/thin-kernel-product-research.md)
- [Multimodal Fidelity Research](../references/multimodal-fidelity-research.md)
- [Neural Compression Research](../references/neural-compression-research.md)
- [Product Completeness Review](../references/product-completeness-review.md)
