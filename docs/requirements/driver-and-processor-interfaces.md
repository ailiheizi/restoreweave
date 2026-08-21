# Driver and Processor Interface Requirements

## 1. Purpose

RestoreWeave is a self-hosted, NAS-first content-aware managed data layer. Its extension contracts let operators replace or upgrade content algorithms without changing namespace identity, recovery meaning, policy authority, or durable storage records. `RW-MVP-1` exercises those contracts through a read-only managed-archive and search profile.

The product has five current extension seams and one reserved later seam:

- **CaptureDriver** presents a bounded source view.
- **Processor** performs one typed content-processing capability.
- **RepositoryDriver** stores, reads, verifies, and restores durable representations.
- **IndexProvider** builds and updates replaceable discovery projections.
- **QueryProvider** queries a named index generation and returns immutable subject references.
- A later **RetrieverDriver** may reacquire pinned external content under a separately qualified profile.

These seams are stable because their implementations vary materially. The core does not expose every internal function as a plugin. Scheduling, identity, policy, provenance, transactions, representation admission, namespace projection, and authorization remain host-owned.

This document is normative for extension behavior. File classification and routing are defined by [File Identification and Extraction Requirements](file-identification-and-extraction.md). AI and semantic providers are defined by [External AI and Semantic Extension Requirements](external-ai-and-semantic-extensions.md). Namespace access is defined by [Namespace and Content Access Technical Design](../technical/namespace-and-content-access.md).

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

## 2. Product boundary

The core MUST own:

- Stable content, file-version, namespace-entry, collection, representation, protection-reference, description-document, and segment identities.
- The evidence ledger and the selected classification result.
- Typed routing, fallback order, resource policy, and privacy policy.
- Immutable operation plans and human-authorized policy decisions.
- Staging, validation, commit, publication, rollback, and garbage-collection eligibility.
- Original-directory browsing and exact-byte recovery semantics.
- Representation selection and the distinction between authoritative, rebuildable, perceptual, and discovery-only data.
- Provenance, dependency closure, operation history, and audit events.
- Durable user tag, note, and description records, revisions, tombstones, provenance, and portable export/import.
- Query-time authorization of every returned subject.

Extensions MAY:

- Observe explicitly granted content.
- Return classification evidence, parse trees, extracted data, fingerprints, embeddings, candidate annotations, representation candidates, or validation measurements.
- Write bytes only to host-controlled staging.
- Place admitted representations in an authorized repository.
- Build a replaceable index generation from authorized records.
- Query an index and return subject-bound candidates.

An extension MUST NOT:

- Write the core catalog, operation journal, policy store, or namespace records directly.
- Choose its own source scope, broaden a route, delete source data, or publish a snapshot.
- Treat a filename, MIME value, model score, embedding distance, perceptual hash, repository response, or transport checksum as proof of exact identity.
- Receive ambient filesystem, network, secret, repository-administration, or database authority.
- Require an embedded LLM harness, prompt loop, agent memory, A2A runtime, or general workflow engine.

The reference product MUST ship a local semantic processor and vector provider by default, while exact ingest, storage, verification, and restore remain independent of their derived state. Description generation is a typed `Processor` capability, not a second authority or an embedded agent. If the semantic branch is absent or fails, the host MUST expose an explicit degraded status and continue lexical/structured search over path, name, type, metadata, checksum, duplicate, protection state, recovery references, descriptions, tag, note, processing-state, and available extracted-text fields.

## 3. Platform-neutral interface inventory

| Interface | Responsibility | Required behavior |
| --- | --- | --- |
| **CaptureDriver** | Present a bounded and consistently described source view. | Required for each supported source profile. |
| **Processor** | Execute one declared stateless or bounded-state content capability. | Required extension seam; capabilities are optional unless the active product profile, such as the default local embedding profile, explicitly mandates one. |
| **RepositoryDriver** | Place and retrieve admitted representations in local, NAS, object, or engine-managed storage. | At least one qualified implementation is required. |
| **IndexProvider** | Build and update immutable or versioned search generations. | Hybrid lexical/structured plus local semantic generations are required for the reference product; other vector and multimodal implementations are optional. |
| **QueryProvider** | Query exactly one explicitly named `IndexGenerationRef` per invocation and return subject-bound candidates; compatibility is validated before invocation. | The default broker fuses lexical, structured, and local semantic candidates; other semantic and multimodal providers are optional. |

A single executable MAY implement several interfaces. In particular, one plugin may implement both IndexProvider and QueryProvider, but each capability remains separately negotiated and authorized.

`RetrieverDriver` is reserved for later reacquisition profiles. It is not an `RW-MVP-1` dependency and does not add a sixth current extension obligation.

CaptureDriver examples include filesystem walks, Linux snapshot adapters, ZFS and Btrfs snapshots, NAS vendor snapshot APIs, object-store version views, and application-consistent exports. Other operating-system and filesystem integrations qualify independently and do not define the product identity, global data model, default deployment shape, or release gate.

RepositoryDriver examples include a local filesystem repository, a repository engine with built-in deduplication and compression, an object store, and a remote NAS target. Backend-private chunk or pack identifiers MUST NOT become RestoreWeave content or namespace identity.

## 4. The classified-content processing path

The host constructs and executes this logical path:

~~~text
capture -> inventory
  |-> mandatory host-owned exact identity
  |   -> exact RepositoryDriver placement
  |   -> required readback and exact publication
  |-> suffix evidence -> magic-byte evidence
      -> optional learned classification evidence
      -> host-selected typed content class
      -> host-built ProcessingRoute
      -> ordered or parallel Processor invocations
      -> host-controlled staging and independent validation
      -> derived-artifact or representation admission
      -> optional RepositoryDriver placement
      -> IndexProvider generation
~~~

The first two classification stages are available in the baseline distribution. Learned classification is optional and usually runs only for unknown, ambiguous, or policy-selected content. All evidence remains visible when stages disagree.

The path is not an arbitrary user-authored workflow graph. A **ProcessingRoute** is a bounded, typed, host-owned plan whose nodes are selected processor capabilities and whose edges connect declared schemas. The core validates the route before execution and records its canonical digest.

The route MAY contain parallel branches, for example:

- Preserve exact bytes while also extracting text.
- Produce a compressed exact representation and a thumbnail.
- Generate an audio fingerprint and a transcript independently.
- Create a search embedding without making it a recoverable representation.

A processor result cannot add a new executable step. It MAY emit typed evidence that causes the host to generate a new plan revision under policy.

## 5. Common protocol

### 5.1 Negotiation

Every external session begins with protocol and capability negotiation. The host advertises supported protocol versions, schema identifiers, stream modes, digest algorithms, quotas, cancellation behavior, and interface families. The extension returns a canonical **CapabilityProfile**.

The selected protocol version and capability-profile digest MUST be bound to every invocation and durable result that depends on them. Trial execution MUST NOT substitute for negotiation.

Protocol compatibility follows these rules:

- An unsupported major version fails closed.
- A minor version MAY add optional fields or capabilities with defined unknown-value behavior.
- Unknown optional fields are ignored safely.
- Unknown critical fields, output authority classes, or schema revisions fail closed.
- Experimental protocols use an explicit alpha marker and make no stable compatibility promise.
- Stable promotion requires published schemas, conformance tests, migration notes, and at least three independent implementations, including one outside the main source tree.

Internal Go interfaces, SQLite schemas, worker topology, queues, caches, and process-supervisor details are not public compatibility surfaces.

### 5.2 CapabilityProfile

Every capability profile MUST declare:

- Namespaced capability ID, interface family, protocol range, and schema versions.
- Implementation ID, release version, executable or package digest, and publisher identity when available.
- Processor stage or driver operation.
- Accepted subject kinds, content-class selectors, format selectors, and required evidence predicates.
- Required access pattern: metadata-only, prefix read, bounded ranges, sequential stream, random access, or complete object.
- Input and output schema identifiers.
- Output authority class and lifecycle class.
- Determinism and reproducibility class.
- Streaming, seeking, batching, concurrency, cancellation, and partial-result behavior.
- CPU, accelerator, memory, temporary storage, time, input, output, recursion, and expansion bounds.
- Required libraries, codecs, dictionaries, models, tokenizers, runtimes, and decoder dependencies.
- Network destinations, credentials, disclosed data classes, privacy constraints, licenses, and redistribution constraints.
- Supported platforms and architectures.
- Upgrade compatibility and whether prior outputs can be read, compared, migrated, or rebuilt.

Selectors are declarative data evaluated by the host; a package cannot supply predicate code. Every input and output schema uses a content-addressed `SchemaRef` containing a namespaced ID, version, and schema digest. Compatibility must be declared against exact schema digests or an independently qualified compatibility rule; matching names or semantic-version ranges alone are insufficient.

Content-class selectors are open, namespaced values. A processor may support text, image, audio, video, document, archive, application, game, model, dataset, database, disk-image, source-code, or a vendor-specific class without adding that modality to the core schema.

Wildcard filesystem access, unrestricted network access, arbitrary shell execution, raw control-database access, and universal secret access are forbidden capabilities.

### 5.3 Determinism classes

Every processor declares exactly one execution class:

- **BYTE_DETERMINISTIC**: identical pinned inputs and dependencies produce identical output bytes.
- **SEMANTICALLY_DETERMINISTIC**: output bytes may vary, but a versioned canonical comparator proves the same declared result.
- **SEEDED_STOCHASTIC**: output identity includes a pinned seed and all relevant sampling parameters.
- **OPAQUE_NONDETERMINISTIC**: replay is not assumed; any durably referenced output must itself be retained.

The result MUST record the declared class. A host MUST NOT cache an opaque nondeterministic result by input digest alone. Hardware, precision, runtime, model, preprocessing, or configuration changes that can alter output create a new output generation.

Session caches are performance-only and MUST NOT affect output meaning. Every output-affecting model, dictionary, corpus, remote revision, checkpoint, or mutable state is pinned by immutable digest. Unpinned external state forces `OPAQUE_NONDETERMINISTIC` treatment and retention of every durably referenced output. Optional checkpoints are host-owned, attempt-fenced, input/profile/configuration-bound, non-authoritative, and invalid after an incompatible upgrade. A crashed reusable worker is reset or replaced before it receives another subject.

### 5.4 Invocation context

Every invocation MUST bind:

- Session, operation, request, attempt, idempotency, lease, and fencing identifiers.
- Selected capability ID, protocol version, and CapabilityProfile digest.
- Immutable SubjectRef values, source revisions, and known content digests.
- Exactly one route reference: an `IdentificationRouteRef` for `CLASSIFY_LEARNED` or classification-refining `PARSE`, or a `ProcessingRouteRef` for post-classification stages.
- The applicable evidence-set digest and optional ClassificationRecord digest.
- Opaque bounded input handles and host-controlled output-staging handles.
- Canonical parameters, parameter schema, and configuration digest.
- Requested output schemas and authority classes.
- CPU, accelerator, memory, temporary-space, time, byte, recursion, egress, and cost budgets.
- Explicit network destinations and scoped secret handles when allowed.
- Deadline, cancellation reference, and retry policy.

An invocation MUST NOT contain an unrestricted host path, arbitrary command string, plaintext repository credential, general SQL connection, signing key, or open-ended instruction to choose protection policy.

Multi-file work uses a host-issued **CollectionViewHandle**, never a declared root path. The handle binds one immutable inventory generation, an explicit bounded member set, typed relationships, total entry and byte ceilings, ACL scope, pagination, and host-mediated reads. A processor may propose collection identity, membership, and relationships, but only the host publishes authoritative collection IDs and membership records.

Every Processor invocation names one operation:

- **RUN_STAGE** executes one declared Processor stage against immutable source or admitted-artifact handles.
- **DECODE_REPRESENTATION** materializes a pinned retained representation for `FileAccess`, verification, migration, or restore. It receives only the encoded representation handle, exact pinned dependencies, requested decoded range, and budgets; it never receives the original source handle.

### 5.5 Result envelope

Every result MUST include:

- Invocation and attempt bindings.
- One status: **SUCCEEDED**, **PARTIAL**, **INAPPLICABLE**, **BLOCKED**, **FAILED**, **CANCELLED**, or **UNKNOWN_EXTERNAL_OUTCOME**.
- Typed reason codes and retry classification.
- Exact input revision and inspected coverage.
- Typed outputs with schema, length, digest, and staging references.
- Confidence, thresholds, calibration reference, and uncertainty where meaningful.
- Determinism class, seed, runtime conditions, and repeat metadata.
- Complete producer, dependency, preprocessing, model, configuration, and runtime provenance.
- Actual CPU, accelerator, memory, storage, network, time, and external-service use.
- Warnings, unsupported features, omitted regions, and partial-result boundaries.
- Side-effect and reconciliation information when the interface can mutate an external system.

**SUCCEEDED** means only that the extension completed its own operation. The host still decides whether to accept, retain, index, place, or publish the result.

Large outputs use a canonical **ProcessorArtifactEnvelope** rather than inline control-message payloads. It binds a content-addressed `SchemaRef`, subject and source revision, optional segment, representation kind, recovery-claim reference, lifecycle, sensitivity and ACL labels, purge lineage, producer and configuration provenance, dependency closure, coverage, media type, declared length, host-computed digest, and route-scoped availability. Downstream processors receive immutable artifact handles issued by the host.

### 5.6 Output authority and lifecycle

Each output declares one authority class:

| Authority class | Meaning |
| --- | --- |
| **DIAGNOSTIC** | Operational information with no content authority. |
| **CANDIDATE_EVIDENCE** | Classification, caption, tag suggestion, match, recommendation, or other claim. |
| **MEASURED_EVIDENCE** | A bounded measurement evaluated by a host-owned rule. |
| **STAGED_ARTIFACT** | Bytes or structured data awaiting host validation and admission. |
| **EXTERNAL_RECEIPT** | Evidence of an external capture, storage, or index effect requiring reconciliation. |

Each output also declares one lifecycle class:

- **AUTHORITATIVE_DATA** for irreplaceable operator or source data.
- **RECOVERABLE_REPRESENTATION** for an admitted exact, normalized, perceptual, or functional representation with an explicit recovery contract.
- **REBUILDABLE_DERIVATIVE** for extracted text, thumbnails, embeddings, fingerprints, and similar recomputable artifacts.
- **EPHEMERAL_CACHE** for disposable execution state.

Authority and lifecycle are independent. An embedding may be valid candidate evidence and a rebuildable derivative, but it is never exact-byte proof.

Staged output follows this host-owned state machine:

~~~text
ALLOCATED
-> WRITING
-> SEALED
-> HOST_DIGESTED
-> SCHEMA_VALIDATED
-> POLICY_ADMITTED
-> PLACED / ROUTE_AVAILABLE / INDEX_FEED_PUBLISHED
~~~

A processor may write only while the staging object is `WRITING` and may request only one irreversible seal for an attempt. Rejected, cancelled, expired, superseded, or unfenced outputs never become route inputs, repository placements, index-feed records, or published state. Unadmitted staging objects are collected after their host-owned lease expires.

## 6. Processor stages

Processor capabilities use a small set of stage roles:

| Stage | Purpose | Typical outputs |
| --- | --- | --- |
| **CLASSIFY_LEARNED** | Add optional learned format or content-class evidence. | Detection evidence. |
| **PARSE** | Validate structure and enumerate components. | Parse tree, coverage, virtual members. |
| **EXTRACT** | Derive normalized text, metadata, media tracks, symbols, or previews. | Extraction artifacts. |
| **ENRICH** | Attach external or model-derived descriptions and relations. | Candidate metadata and annotations. |
| **FINGERPRINT** | Produce auxiliary checksums and comparison features. | Structural, perceptual, acoustic, visual, semantic, or additional cryptographic features; never the host-owned canonical content identity. |
| **TRANSFORM** | Produce a storage or access representation and, when it creates a retained recoverable representation, provide the pinned historical decode direction. | Raw, compressed, normalized, transcoded, neural, or other candidates plus a decode contract. |
| **VALIDATE** | Measure a candidate against a declared recovery or quality contract. | Measured evidence and coverage. |
| **INDEX_PREPARE** | Convert artifacts into provider-ready index documents or vectors. | Versioned index records. |

The core MAY add stage roles only through a versioned schema update. It MUST NOT create a separate privileged plugin family for each media type or algorithm.

A transform profile declares supported encode and decode directions separately. A transform that claims exact recovery MUST provide a pinned decoder closure, decoded length and digest expectations, streaming and seek or range behavior, and pass independent host-controlled round-trip verification. A decoder retained for historical reads MAY reject new encoding while it remains obligated to decode every pinned dependent representation. A lossy, perceptual, or generative transform MUST use the corresponding explicit recovery relation and can never silently replace exact fallback.

### 6.1 Core capability mappings

The following names are capability IDs within the existing `Processor` protocol, not new privileged interface families:

| Capability | Stage | Required input | Admissible output | Forbidden authority |
| --- | --- | --- | --- | --- |
| `EMBED_TEXT` | `INDEX_PREPARE` | Immutable `SemanticSegment` refs and bounded text handles, language, semantic-profile/config digests, preprocessing and budgets | Segment-bound vectors plus semantic-space, model, tokenizer, runtime, dimension, normalization, coverage and determinism facts | Writing an index generation, fusing results, changing durable text, or claiming identity |
| `DESCRIBE_SUBJECT` | `ENRICH` | Subject/artifact handles, requested description schema/kind, allowed fields, language, egress policy and budgets | Candidate UTF-8 description, source spans/citations, coverage, confidence and producer provenance | Overwriting or accepting user facts, changing annotations, or publishing recovery truth |
| `TRANSFORM_EXACT_CANDIDATE` | `TRANSFORM` | Exact representation handle, content identity, codec profile and budgets | Encoded staged artifact plus complete `RecoveryRecipe` candidate | Retiring exact fallback or self-approving reversibility |

An `EMBED_TEXT` result has exactly one terminal result per requested segment: accepted vector, typed inapplicability, or typed failure. The host rejects missing/extra segment IDs, NaN or infinite elements, incompatible dimensions or element types, undeclared truncation, and a semantic-space digest that does not match the selected profile. The processor writes only host-controlled staging or a bounded response and never receives zvec credentials or a collection path.

A `DESCRIBE_SUBJECT` result remains candidate evidence until the host validates encoding, length, schema, source bindings, egress receipt, authorization, and provenance and creates a new immutable `DescriptionRevision`. A model cannot mark its own revision user-accepted. Local and online implementations obey the same request/result contract.

## 7. Typed routing and fallback

The core builds an **IdentificationRouteRef** from subject revision, suffix, magic-byte, path-context evidence, and policy. It may contain only `CLASSIFY_LEARNED` and classification-refining `PARSE` nodes. After final classification, the core builds a **ProcessingRouteRef** from:

- Classification evidence and selected content classes.
- Operator policy and storage goals.
- Installed capability profiles.
- Required recovery relation.
- Privacy, license, accelerator, cost, latency, and energy constraints.
- Repository and query-provider capabilities.
- Declared fallback chain.

Each route node binds one exact capability profile and one canonical configuration. Route selection MUST be deterministic for the same inventory revision, installed-capability set, and policy revision.

Candidate selection uses this stable precedence: explicit operator pin, supported qualification state, selector specificity, configured priority, then stable capability ID. An equal-precedence ambiguity produces a visible route conflict rather than nondeterministic selection. Unknown extension schemas may be retained opaquely but cannot feed another node until explicit compatibility is declared. The host enforces maximum route nodes, branches, successor generations, and recursion.

The default fallback rules are:

1. Unknown or conflicting content is preserved as raw exact data.
2. An inapplicable or unavailable optional processor advances to the next compatible route candidate.
3. Transform, compression, or validation failure falls back to a qualified exact representation, normally raw or the default deterministic repository path.
4. Extraction, enrichment, embedding, or indexing failure marks the affected discovery generation pending or degraded; it does not block durable ingest.
5. A profile-specific processor requirement MAY block only that processing branch, derived representation, or stronger profile claim. It MUST NOT block capture, inventory, host-owned exact hashing, exact placement eligibility, required exact-lane verification, or exact publication of readable bytes.
6. No fallback may reduce protected scope or change a recovery relation without a new human-authorized plan.

Fallback is recorded as an event with the failed capability, reason, selected alternative, and effect on storage, search, cost, and recovery.

## 8. CaptureDriver contract

A CaptureDriver presents one explicitly scoped source view and declares its consistency:

- **IMMUTABLE_SNAPSHOT**
- **APPLICATION_CONSISTENT_EXPORT**
- **CRASH_CONSISTENT_VIEW**
- **VERSIONED_OBJECT_VIEW**
- **BEST_EFFORT_LIVE_VIEW**

The driver MUST support capability description, capture creation or opening, root enumeration, consistency inspection, lease or hold management when applicable, and idempotent release.

Every opened capture root is represented to the host by an opaque `CaptureRootBinding`. The binding is a live authority object, not a path string. It MUST retain or derive operations from one trusted root anchor and bind:

- Source identity, host identity, filesystem or volume identity, mount identity, configured-root identity, and the captured root object.
- Capture-set ID, requested-to-achieved root mapping, snapshot, export, or validated-live identity, and applicable lease, hold, or deletion-protection evidence.
- Resolver profile and version, kernel facilities used, symlink policy, nested-mount policy, special-file policy, and validation evidence.
- The exact read scope and permitted descriptor-relative metadata, enumeration, `readlink`, open, and read operations.

Runtime file-descriptor numbers, process-local handles, absolute exposure paths, and `/proc/self/fd` names are never durable identity. Durable records serialize the facts and evidence needed to reacquire and revalidate the same root binding; they never serialize a live descriptor as authority.

On Linux, every authoritative traversal and content operation MUST be component-relative to the retained root anchor and use [`openat2`](https://man7.org/linux/man-pages/man2/openat2.2.html) with the qualified `RESOLVE_*` policy or an independently qualified equivalent that provides the same root-confinement, symlink, magic-link, and mount-boundary guarantees. Applying `O_NOFOLLOW` only to the final component is insufficient because ancestors may be replaced or redirected between operations. The legacy string-returning `SecureJoin` API is prohibited for authoritative capture; upstream documents that API as TOCTOU-unsafe. A handle-returning resolver such as [`OpenatInRoot`](https://github.com/cyphar/filepath-securejoin#openatinroot) may be used only after its exact revision, per-file license obligations, kernel fallback, and handle-use semantics pass qualification.

The binding MUST pin and validate the entry type before any potentially blocking or side-effecting open. FIFOs, sockets, block devices, character devices, and other special objects are metadata-only unless an explicit separately qualified profile grants typed access. A generic scanner MUST NOT perform an ordinary blocking `O_RDONLY` open and then decide whether the object was a regular file.

Its receipt MUST bind source identity, requested roots, achieved roots, source-to-captured mapping, consistency level, creation boundary, driver profile digest, limitations, a canonical capture digest, and the canonical digest of every `CaptureRootBindingRecord` used by the capture.

A driver MUST NOT claim snapshot consistency after silently falling back to a changing live tree. A best-effort live view is valid only when policy permits it and each observed file version is independently stabilized or reported as changing.

A capture MUST NOT be published as authoritative, converted into namespace records, or consumed by hashing, processing, repository placement, or RRF publication until every required root binding validates its root object, filesystem or volume, mount, snapshot or live basis, resolver policy, and lease or hold state. Validation is repeated before each authority-bearing consumer begins and whenever a remount, provider event, lease transition, watcher reset, or other continuity signal makes the binding uncertain.

Capture behavior is platform-specific; namespace and content identity are not. APFS metadata may appear in an APFS profile, while Linux, ZFS, Btrfs, Windows, NAS, object-store, and application-export profiles record their own facts through namespaced schemas.

## 9. RepositoryDriver contract

A RepositoryDriver stores only host-admitted representations and must support:

- Capability and repository-format description.
- Read-only target validation.
- Safe initialization or explicit adoption.
- Placement estimation where available.
- Idempotent placement and reconciliation.
- Opening immutable restore points or content objects.
- Bounded reads and restore.
- Declared integrity verification.
- Capacity, health, and dependency reporting.

The language-neutral operation set is:

| Operation | Required semantics |
| --- | --- |
| `DescribeCapabilities` | Return driver, repository-format, supported operation, consistency, reader, encryption, compression, chunking, and limit profiles |
| `ValidateTarget` | Read-only inspection of location, identity, format, credentials, capacity, compatibility, and existing state |
| `InitializeOrAdopt` | Explicit, idempotent creation or adoption under a plan-bound repository identity; never implicit format reinterpretation |
| `EstimatePlacement` | Return logical bytes, probable new physical bytes, uncertainty, temporary space, and unsupported estimates without claiming a receipt |
| `PlaceRepresentation` | Idempotently place one admitted immutable stream and return a complete receipt |
| `OpenRepresentation` / `ReadRange` | Return the admitted logical byte stream or a declared bounded range without leaking backend-private object identity |
| `VerifyRepresentation` | Perform the requested metadata, sampled, or full logical readback and return typed evidence |
| `ReconcilePlacement` | Resolve retries and `UNKNOWN_EXTERNAL_OUTCOME` without creating a second logical placement |
| `DescribeHealthAndCapacity` | Report availability, key/reader readiness, capacity, corruption/repair state, and last verified boundary |
| `ListReachabilityRoots` | Report backend objects reachable from host-authorized portable roots; never decide retention itself |
| `ExecuteGC` / `Repair` | Optional separately authorized operations with dry-run, fencing, receipts, and rollback/recovery limits; disabled for `RW-MVP-1` until qualified |

Every mutating request binds operation/request IDs, idempotency key, plan/config/profile digests, repository identity, admitted representation identity and length, lease/fence, deadline, and authorization. Every result uses a typed terminal status including `UNKNOWN_EXTERNAL_OUTCOME`, reports retry/reconciliation behavior, and returns a receipt whose portable fields are distinguished from backend telemetry. A timeout or process exit is not proof that placement failed or succeeded.

The driver MAY delegate chunking, deduplication, compression, encryption, packing, erasure coding, and transport to a mature repository engine. Alternatively, it MAY store already transformed representations without interpreting them.

Every placement receipt MUST bind repository identity, driver and format versions, representation identities, plan and operation digests, stored-byte measurements, required reader dependencies, and verification evidence.

Source deletion, last-copy removal, retention pruning, and repository garbage collection require separate host policy and MUST NOT be granted merely because a driver supports deletion.

### 9.1 Current narrow Go surface

The current checkout implements only the subset required for exact placement and signed publication:

~~~text
Driver:
  Place(reader) -> Receipt
  PlaceExact(content_id, reader) -> Receipt
  Open(content_id) -> logical byte stream
  Verify(content_id)
  Root() -> resolved local root

RecordDriver:
  RepositoryIdentity()
  PlaceRecord(role, reader) -> RecordReceipt
  OpenRecord(role, digest)
  VerifyRecord(receipt)
  ListRecordDigests(role)

Receipt:
  ContentID
  Bytes        # decoded logical length
  StoredBytes  # physical object length; diagnostic, not signed identity
  Existed
~~~

`OpenProfileWithCompression` admits only `directory-cas-dev-v1 + identity-v1` and `local-zstd-v1 + zstd-v1`. Both implement payload and portable-record interfaces; the zstd driver transparently decodes payloads before callers hash or read them, while signed prepared/commit JSON remains uncompressed. `DescribeProfile` supplies status-only tuple metadata without making it a new storage requirement for third-party drivers. The repository root has a fail-closed profile marker so the two physical formats cannot be reinterpreted in place.

This is not full conformance with section 9. It does not yet expose placement estimation, capacity, GC/reachability, repair, encryption/key state, backend placement references, dependency closure, or a complete placement receipt. The signed payload aggregate separately binds repository identity, logical content IDs, and logical lengths. `StoredBytes` remains unsigned telemetry so backend recompression or relocation cannot change portable content identity.

## 10. IndexProvider and QueryProvider contracts

IndexProvider is a stateful projection interface, separate from Processor because it owns build generations. It MUST support:

- **DescribeCapabilities**
- **CreateGeneration**
- **ApplyBatch**
- **FinalizeGeneration**
- **ValidateGeneration**
- **ActivateGeneration**
- **InspectGeneration**
- **RetireGeneration**

An index generation binds:

- Provider profile and schema digests.
- Input inventory revision and artifact high-water marks.
- Embedding or feature-space identities when applicable.
- Analyzer, tokenizer, normalization, feature encoding, and field-mapping configuration.
- ACL projection version.
- Completeness and known-stale boundaries.

Activation MUST be atomic from the host query broker's perspective. The previous generation remains available for rollback until retention policy permits retirement.

Every generation operation binds request and idempotency IDs, provider/profile/config and input-high-water digests, exact generation ID, lease/fence, deadline, authorization projection, and resource limits. `ApplyBatch` returns a batch digest, accepted/rejected counts, per-record typed failures, indexed-through revision, and retry token. `FinalizeGeneration` makes the generation immutable; `ValidateGeneration` checks schemas, counts, coverage, authorization labels, known-query fixtures, and provider health; only the host may call `ActivateGeneration`. `UNKNOWN_EXTERNAL_OUTCOME` requires inspection or reconciliation before retry.

QueryProvider queries exactly one explicitly named `IndexGenerationRef` per invocation. Compatibility is validated before invocation. It MUST support capability description, typed query validation, bounded query execution, stable generation-bound continuation, cancellation, and health inspection. Its profile declares accepted index schemas and generations, query modes, filter and projection schemas, score schemas, within-generation ranking behavior, resource limits, and degradation behavior. A host-owned broker performs any cross-provider or cross-generation fusion over separately generation-pinned results.

Query results MUST contain immutable SubjectRef values, optional segment selectors, score components, exact IndexProvider generation, QueryProvider profile, indexed-through revision, coverage state, and provenance. Continuation binds the exact generation, query digest, authorization scope, sort, and expiry. The core query broker MUST derive the effective principal and workspace and reauthorize every subject before any presentation adapter receives metadata or may open content. A query result is not proof that a subject still exists or that a representation is valid.

The minimum query operations are `DescribeCapabilities`, `ValidateQuery`, `Query`, `Continue`, `Cancel`, and `InspectHealth`. A request binds one immutable generation, query/filter/projection schema digests, normalized query digest, principal/workspace, result and resource bounds, deadline, and continuation when present. A response binds that same generation and includes per-candidate field/segment provenance, provider score components, coverage/high-water state, and a stable continuation. Providers never compare incompatible generations or perform final cross-provider fusion; the host broker owns those actions and records their policy.

Loss of an index MUST degrade discovery, not namespace browsing, exact reads, verification, or restoration.

### 10.1 Later RetrieverDriver contract

`RetrieverDriver` remains outside the MVP dependency set, but its reserved narrow operations are `DescribeCapabilities`, `ProbeReference`, `AcquireToQuarantine`, `InspectOutcome`, and `Release`. A request binds one `RecoveryReference`, expected identity/length, immutable revision or version evidence, credential reference, egress/rights policy, quarantine handle, budgets, idempotency, and fencing. It never receives publication, catalog-write, or repository-administration authority. Candidate bytes become usable only after the host independently hashes and admits them as a new exact representation.

## 11. Upgrade, reclassification, reprocessing, and reindexing

### 11.1 Installation and activation

Installing a new extension version registers a new CapabilityProfile digest. It MUST NOT silently replace the identity of an earlier profile or rewrite existing results.

Activation policy MAY route new data to the new profile immediately, use a canary scope, or run shadow comparison. Accepted plans continue to bind their original profiles unless explicitly revised.

The reference distribution owns a host-managed profile registry. It discovers only bundled manifests and explicitly installed packages from configured registry roots; it never scans the current directory, ambient `PATH`, arbitrary shared-library locations, or repository contents for executable providers. A package manifest binds:

- package, publisher/signature, license/SBOM, platform and architecture;
- every supplied interface and `CapabilityProfile` digest;
- executable, native-library, model, tokenizer, dictionary, schema, and migration digests;
- entrypoint and sandbox profile, network/credential declarations, resource bounds, and health probe;
- install layout, data-format compatibility, rollback, retirement, and removal rules.

The host-owned lifecycle is `inspect -> stage -> verify signature/digests/licenses/platform -> negotiate capabilities in isolation -> run conformance/health checks -> register -> optionally activate`. Its semantic operations are list, inspect, install, verify, activate, retire, and remove; they accept package/profile references, never raw shell commands. `RW-MVP-1` does not add a public `profile.*` Core Command family: bundled defaults arrive through the signed native package, additional packages use the host package/installation mechanism, `capability.list` reports what is registered, and the main config selects an immutable profile ID. A later public lifecycle command requires an explicit Core Command ABI revision. First-run onboarding still asks only for data location, repository profile, and embedding profile.

Activation affects only new plans, processor attempts, or index generations. It cannot reinterpret an accepted plan or mutate an active generation in place. Replacing an embedding profile builds and validates a new generation beside the old one before atomic activation. Retiring a rebuildable processor or index provider stops new routing but preserves its provenance until retention permits removal. A repository driver, decoder, model, tokenizer, or dictionary cannot be removed while any retained representation or recovery recipe depends on it; removal first requires a qualified migration or an independently retained reader closure. Online providers use the same profile registry but bind endpoint identity, TLS/auth policy, model revision, egress scope, and credential reference instead of a local executable.

### 11.2 Staleness

Derived output identity includes:

~~~text
subject revision
+ selected evidence or parser-view revision
+ capability profile digest
+ configuration digest
+ dependency and model digests
+ determinism inputs
+ output schema revision
~~~

A change to any output-affecting term marks dependent derivatives as stale. Stale means eligible for rebuilding; it does not mean corrupt. Old results remain addressable until superseded and safely retired.

### 11.3 Reclassification

A detector-rule or learned-classifier upgrade MAY schedule **reclassify** jobs. Reclassification:

- Adds a new evidence and classification generation.
- Preserves prior evidence.
- Recomputes routes only where the selected class or confidence policy changes.
- Never changes original content identity.
- Marks dependent processor and index generations stale through explicit lineage.

### 11.4 Reprocessing

A **reprocess** job creates new processor outputs beside old outputs. For recoverable representations:

1. Build the new candidate from immutable input.
2. Seal the candidate, let the host compute its encoded digest, and validate its schema and policy.
3. Invoke `DECODE_REPRESENTATION` using only the sealed candidate plus the pinned decoder dependencies; the original source handle is unavailable to the decoder.
4. Let the host compute the decoded length and digest and validate them independently against the declared recovery contract.
5. Place it durably.
6. Prove later readability through the same pinned decode contract used by `FileAccess` and restore.
7. Atomically update representation preference.
8. Retain the prior representation until rollback and retention gates pass.

Uninstalling a processor or decoder is blocked while an admitted representation depends on it, unless every dependent representation has been migrated to a supported closure.

### 11.5 Reindexing

A **reindex** job always builds a new IndexProvider generation. It MUST NOT destructively rewrite the active generation in place. Validation covers record counts, authorization labels, known-query fixtures, missing-artifact behavior, and provider-specific health. Only then may the host atomically activate it.

Embedding spaces from different model, preprocessing, dimension, quantization, or metric generations are incompatible unless an explicit bridge is qualified. Migration normally uses parallel generations and host-broker fusion of separately generation-pinned QueryProvider results during a bounded transition.

### 11.6 Rollback

Rollback selects a previously validated route, representation preference, or index generation. It MUST NOT delete newer evidence or outputs as part of the selection change. Cleanup is a later policy-controlled operation.

## 12. Sandboxing, quotas, and egress

Third-party extensions MUST run under platform-appropriate capability enforcement, such as a hardened container, OS sandbox, or restricted WASM runtime.

The default sandbox has:

- No ambient source or repository access.
- No control-database access.
- No network.
- No plaintext secrets.
- No inherited shell environment or arbitrary process spawning.
- Read-only bounded inputs and host-owned staged outputs.

Optional processing MUST also be physically isolated from the exact and read paths. The reference deployment reserves CPU, memory, file descriptors, inodes, scratch space, and repository-staging capacity for exact hashing, placement, browse, verification, and restore. Processor queues use per-capability concurrency and retry ceilings, crash-loop circuit breakers, automatic quarantine, and dead-lettered subjects. Exhausting an optional processor pool MUST NOT exhaust the reserved exact or interactive-read pool.

Every invocation has hard limits for CPU, accelerator, memory, temporary storage, input, output, files, handles, recursion, expansion, network, time, and cost as applicable.

Attempt fencing covers staging allocation and seal, artifact admission, derivative-preference changes, and index-feed publication. A stale worker cannot seal or admit output after its lease or fence has been replaced.

Remote processing requires an immutable egress grant naming the destination, disclosed data classes, allowed subject classes, minimization, credentials, request and byte budgets, expiry, and audit requirements. Redirects and reconnects remain within the declared destination policy.

File bytes, metadata, extracted text, model output, and external descriptions are untrusted data. They MUST NOT be interpreted as instructions that broaden capabilities or modify the route.

## 13. Conformance requirements

Every extension family MUST have malformed-message, version-mismatch, timeout, cancellation, quota, crash, and capability-escape tests.

Processor conformance additionally proves:

- Schema-invalid and oversized output never enters the catalog.
- Determinism and provenance declarations match qualified fixtures.
- Partial coverage remains explicit.
- Inapplicable content triggers the host fallback chain.
- Exact representation candidates pass independent round-trip validation.

CaptureDriver conformance additionally proves:

- Source scope cannot escape the grant.
- Ancestor symlink replacement, parent-directory replacement, root replacement, bind-mount substitution, unmount and remount, snapshot substitution, magic-link traversal, and nested-mount crossing cannot redirect any authoritative operation outside the bound capture root or silently change its capture basis.
- Runtime descriptor reuse or a changed absolute exposure path cannot satisfy durable capture identity.
- Entry type is pinned before content open; FIFOs, sockets, devices, and other special files cannot block a worker or be read as regular content.
- A missing or unchecked boundary, stale root binding, invalid resolver profile, expired snapshot lease or hold, or failed revalidation prevents authoritative publication.
- Reported consistency matches the observed boundary.
- Release is fenced, idempotent, and cannot invalidate another consumer.
- Platform-specific metadata remains namespaced.

RepositoryDriver conformance additionally proves:

- Retry and reconciliation do not create conflicting logical placements.
- Backend-private identities do not leak into namespace identity.
- Restore and verification use the exact admitted representation.

IndexProvider conformance additionally proves:

- Generation activation is atomic.
- Rollback restores the prior query view.
- Stale and incomplete boundaries are visible.

QueryProvider conformance additionally proves:

- It rejects incompatible or unnamed index generations.
- Stable continuation cannot cross generations silently.
- Unauthorized subjects cannot be disclosed through results, counts, facets, snippets, or similarity neighbors.

## 14. Reference-product requirements

The reference self-hosted distribution MUST ship strong defaults instead of an empty framework:

- Platform-neutral filesystem ingestion with optional snapshot-specific CaptureDrivers.
- Suffix and magic-byte classification.
- Raw exact fallback.
- One proven deduplicating and compressing repository path.
- Common text and metadata extraction where deterministic and safe.
- Durable whole-subject tag and note CRUD with portable export/import.
- Hybrid lexical/structured IndexProvider and QueryProvider implementations for path, filename, type, metadata, checksum, duplicates, tags, notes, processing state, extracted text, and the default local semantic space.
- Export-manifest materialization over `SnapshotTree` and `FileAccess`; any filesystem presentation is an external consumer, not another RestoreWeave extension seam.
- CLI access to the complete authorized ingest, inspect, search, browse, read, verify, reprocess, reindex, and restore operation set.
- Initial MCP access to the bounded read-only inspection, status, search, namespace, and content subset. A mutation-capable MCP profile is separately qualified later.

Optional packs MAY add media parsing, OCR, ASR, acoustic fingerprints, CLIP-compatible embeddings, learned classification, neural codecs, game and application analyzers, model inspection, or external enrichment.

## 15. Acceptance criteria

1. A Linux or NAS deployment can complete ingest, storage reduction, browse, search, verification, and restore without any optional platform-specific capture dependency.
2. Every readable file begins the host-owned exact lane independently, while its classification branch follows suffix evidence, magic-byte evidence, optional learned evidence, typed routing, processor execution, and host validation with every transition recorded.
3. Unknown, conflicting, unsupported, or processor-failed content falls back to an exact admitted representation without disappearing from the namespace.
4. Text, image, audio, video, game, application, archive, model, dataset, and vendor-specific processors use the same contract without core modality-specific execution code.
5. A processor cannot write authoritative records, publish its own output, expand its input scope, or approve its own representation.
6. Updating a processor creates a new profile and output generation; it never rewrites provenance in place.
7. Reprocessing and reindexing build beside the active generation, validate before activation, and support rollback.
8. Removing a required decoder is blocked until dependent admitted representations are migrated.
9. Loss of every semantic or vector index leaves filesystem browsing and restoration operational while search reports a degraded state.
10. A remote AI processor cannot disclose content without an explicit bounded egress grant.
11. Every operation shared by CLI and the initial MCP subset produces the same route, policy, authorization, result, and audit semantics.
12. The reference distribution remains useful with all learned and generative processors disabled.
13. Deleting and rebuilding every index loses no durable tag or note revision.
14. External presentation tools can consume export manifests and `FileAccess` without changing `SnapshotTree`, namespace identity, or repository layout. RestoreWeave does not define or ship a mount adapter.
