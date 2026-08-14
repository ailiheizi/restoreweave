# Processing, Index, and External Automation Runtime

## 1. Design objective

RestoreWeave runs a replaceable content-processing and discovery pipeline around a small authoritative storage core. It is designed for a self-hosted NAS or server that may contain documents, photos, music, video, archives, games, applications, source trees, datasets, databases, and unfamiliar future formats.

The runtime has three goals:

1. Reduce physical storage while preserving an explicit recovery contract and original file-shaped namespace.
2. Make the collection more discoverable than a conventional filename-only NAS.
3. Allow processors, index implementations, and query algorithms to be upgraded without rewriting recovery truth.

It is not a general agent runtime. RestoreWeave does not own prompt memory, conversations, autonomous loops, model routing, or multi-agent orchestration. An external harness may call typed CLI or MCP operations, and a bounded processor may use a model, but neither becomes kernel authority.

The extension policy is defined by [Extension System Requirements](../requirements/plugin-system.md). Core ownership is defined by [Core Kernel and Interface Requirements](../requirements/core-kernel-and-interface.md).

## 2. Runtime topology

~~~mermaid
flowchart LR
    Source["NAS shares, local trees, snapshots, object views"] --> Capture["CaptureDriver"]
    Capture --> Scan["Scanner and identification evidence"]
    Scan --> Exact["Mandatory exact hash and identity lane"]
    Exact --> Repository["RepositoryDriver"]
    Repository --> ExactVerify["Host readback and exact-lane verification"]
    ExactVerify --> Records["Authoritative records and namespace"]

    Scan --> Planner["Host classification and ProcessingRoute"]
    Planner --> Workers["Sandboxed Processor workers"]
    Workers --> Stage["Host-controlled staging"]
    Stage --> Admit["Validation and representation admission"]
    Admit --> Repository["RepositoryDriver"]
    Admit --> Records

    Records --> Feed["Replayable authorized index feed"]
    Feed --> Index["One named IndexProvider generation per build or update"]
    Query["CLI, MCP, or optional REST/UI query"] --> Broker["Query broker"]
    Broker --> Provider["QueryProvider"]
    Provider --> Index
    Provider --> Broker
    Broker --> Resolve["Authorization and SubjectRef resolution"]
    Resolve --> Query

    Harness["External automation or AI harness"] -. "initial typed read-only operations" .-> Query
~~~

Arrows describe scoped data flow, not transferred authority. Processors cannot publish snapshots or write repositories directly. Index and query providers cannot open arbitrary source paths. Search candidates are resolved against authoritative records before metadata or bytes are returned.

## 3. Deployment shape

A typical self-hosted deployment contains:

- One controller and command dispatcher on the NAS or a nearby server.
- A durable operation journal and authoritative record store.
- One or more capture drivers for local filesystems, mounted shares, snapshot APIs, or object views.
- A default repository driver backed by a mature deduplicating, compressing, encrypting storage engine.
- A local CPU processor pool and optional isolated GPU or remote processor pools.
- Bundled baseline lexical `IndexProvider` and `QueryProvider` implementations.
- The CLI and a local `stdio` MCP adapter.

The controller may be a long-lived daemon for schedules, concurrent jobs, and index maintenance, while recovery tooling must also be able to compose the core locally without the daemon. Platform-specific capture implementations are independently qualified profiles; the runtime model is platform-neutral.

Large installations MAY place processors and index services on separate hosts. Remote workers receive short-lived content handles and explicit capability grants. They do not mount the authoritative database or repository administration namespace.

## 4. Identification and route construction

Identification is accumulated evidence rather than one trusted MIME label:

1. Record suffix or extension hints.
2. Inspect bounded magic bytes and container signatures.
3. Build an immutable `IdentificationRouteRef` from the subject revision and current evidence.
4. Optionally request `CLASSIFY_LEARNED` evidence for unknown, ambiguous, or policy-selected content through that restricted route.
5. Let deterministic host policy select a provisional content class.
6. Run only matching bounded `PARSE` capabilities through the same or a successor IdentificationRoute when structural evidence can refine that class.
7. Preserve producer, version, inspected ranges, confidence, coverage, and disagreements.
8. Publish the final classification and build the post-classification `ProcessingRouteRef`.

Suffix and magic-byte evidence are part of the default distribution. Learned classification is optional. Missing, failed, or conflicting learned output never causes readable content to disappear.

Both route types are validated typed directed acyclic graphs derived from installed capability profiles and policy, not arbitrary executable code. `IdentificationRouteRef` may contain only learned classification and classification-refining parsing. `ProcessingRouteRef` contains the remaining selected stages. Each route node includes:

- Capability-profile digest and exact configuration.
- Input and output schema IDs.
- Subject and classification predicates.
- Required access pattern and content-handle scope.
- Resource, time, egress, privacy, and cost limits.
- Determinism class, retry behavior, and fallback candidates.
- Expected output lifecycle and validation rules.

The host records a canonical route digest. Every invocation binds exactly one route reference. A processor cannot modify the running route. New evidence may cause the planner to create a successor route revision for later execution.

## 5. Reference processing graph

The reference distribution supplies an opinionated path:

~~~text
capture immutable or qualified source view
-> scan namespace and record file facts
-> mandatory host-owned exact hash and identity
-> exact repository-native deduplication and compression
-> independent readback or policy-selected verification
-> publish recoverable namespace

in parallel after scan:
suffix evidence
-> magic-byte evidence
-> optional learned classification
-> bounded structural parsing
-> safe common extraction and index preparation
-> feed metadata and extracted text to baseline index
~~~

Optional branches can run in parallel:

- OCR or document structure extraction.
- Audio metadata, fingerprinting, or transcription.
- Image or video previews, perceptual fingerprints, captions, or embeddings.
- Archive, game, application, model, or dataset inspection.
- Alternative exact compression or later perceptual and neural representation candidates.
- External metadata enrichment under an explicit network grant.

Optional discovery work does not sit on the critical path for durable exact ingest. A slow or failed embedding-producing processor may leave semantic search pending while the file remains browsable, lexically searchable, and recoverable.

The exact lane is host-owned and mandatory for every readable file in `RW-MVP-1`. It cannot be represented as a special raw processor, and no optional processor may sit between readable source bytes and exact placement eligibility. A transform may later replace the selected exact representation only after independent decode-and-hash validation and policy admission.

## 6. Processor execution protocol

### 6.1 Invocation

The controller creates an invocation envelope containing:

- Operation discriminator, exactly one route reference, node, attempt, idempotency, lease, and fencing IDs.
- Processor capability ID, protocol version, package digest, and configuration digest.
- Immutable `SubjectRef`, file-version, content, and classification references.
- Bounded input handles and host-owned output-staging handles.
- Requested output schemas and lifecycle classes.
- Dependency, codec, model, tokenizer, dictionary, and preprocessing identities.
- CPU, memory, accelerator, temporary-space, input, output, time, recursion, network, and cost budgets.
- Cancellation deadline and explicit secret or egress references when allowed.

`RUN_STAGE` executes one declared processing node. `DECODE_REPRESENTATION` is the historical read operation for a retained transform profile. It receives only a pinned encoded representation handle, the exact dependency closure, requested decoded range, and budgets; it never receives the original source handle.

The envelope never contains an unrestricted source path, arbitrary command string, raw SQL connection, plaintext repository credential, signing key, or permission to select policy.

### 6.2 Worker lifecycle

The processor supervisor performs:

~~~text
negotiate capability
-> allocate sandbox and budgets
-> open bounded inputs
-> invoke
-> ALLOCATED -> WRITING -> SEALED
-> host digest and schema validation
-> policy admission
-> publish immutable artifact handles to the route, repository, or index feed
-> reconcile timeout or process loss
-> release handles and sandbox
~~~

Invocations are finite and cancellable. A worker heartbeat may support liveness reporting, but a missing heartbeat does not prove whether an external effect occurred. Any processor capability with an external side effect must provide an idempotent reconciliation method or return an unknown-outcome state.

The host can retry only according to the declared retry class. Opaque nondeterministic output is not cached by input digest alone. Seeded output records its seed; model, runtime, precision, and hardware changes that affect output identity create a new generation.

### 6.3 Result validation and admission

A result envelope contains status, typed reason codes, inspected coverage, staging references, provenance, measurements, warnings, resource use, and dependency closure. Each candidate becomes a canonical ProcessorArtifactEnvelope binding its content-addressed schema, subject revision, optional segment, representation kind, recovery claim, lifecycle, sensitivity and ACL labels, purge lineage, producer, configuration and dependencies, coverage, media type, length, and host-computed digest. Schema-invalid, oversized, out-of-scope, stale-fenced, or unproven output is rejected before it enters authoritative records.

Outputs are treated according to lifecycle:

| Lifecycle | Examples | Runtime treatment |
| --- | --- | --- |
| Authoritative input | Original user data and operator annotations | Never created or silently replaced by a processor. |
| Recoverable representation | Raw, compressed exact, normalized, transcoded, or later qualified representation | Requires a declared decoder and validation contract before admission. |
| Rebuildable derivative | Extracted text, thumbnail, transcript, fingerprint, embedding | Retained with lineage; may be regenerated or discarded. |
| Ephemeral cache | Decode buffers and temporary features | Disposable after the invocation. |

Exact transformation candidates require host-controlled round-trip comparison against the expected exact identity. Perceptual or functional candidates require their own versioned comparator and thresholds and remain distinct from exact content.

Rejected, cancelled, expired, superseded, or unadmitted staging objects never become downstream route inputs or index-feed records. Downstream processors receive immutable host-issued artifact handles rather than plugin paths or large payloads inside control messages.

## 7. Scheduling and backpressure

The controller schedules bounded product jobs, not arbitrary workflows. Queues SHOULD be separated by resource and trust class, for example:

- Metadata-only local CPU work.
- Full-content local CPU work.
- Accelerator work.
- Archive expansion or other high-amplification work.
- Explicitly authorized remote processing.
- Index generation and validation.

Admission control considers source-change stability, memory, scratch space, repository pressure, processor concurrency, GPU availability, index lag, network egress, energy policy, and operator budgets. Interactive browse and restore take priority over optional background enrichment.

The exact and interactive-read paths have reserved resource pools for CPU, memory, file descriptors, inodes, scratch space, and repository staging. Optional processors run in separate queues or platform-enforced resource groups with per-capability concurrency and retry ceilings, crash-loop circuit breakers, quarantine, and dead-letter handling. Processor saturation cannot consume the reservations needed for exact hashing, placement, browse, verification, or restore.

Backpressure must be observable. The system reports queued subjects, oldest pending age, blocking capability, estimated resource need, and whether exact ingest or only discovery is affected. It does not silently drop derivative work.

## 8. Derived-output identity and cache reuse

The identity of a derived output includes every value that can change its meaning:

~~~text
input subject and revision
+ selected parser or evidence view
+ processor capability-profile digest
+ canonical configuration
+ model, codec, dictionary, and dependency digests
+ preprocessing and determinism inputs
+ output schema revision
~~~

A cache hit is valid only when this identity and required coverage match. Approximate feature reuse may be offered as an explicitly different capability but cannot masquerade as an exact cache hit.

When a processor is upgraded, the controller marks dependent outputs stale through lineage. Stale output remains readable and attributable; it is not automatically corrupt. Reprocessing creates a new result beside the old one, validates it, switches preference atomically where allowed, and retains rollback data until policy permits cleanup.

## 9. Index feed and generation lifecycle

### 9.1 Replayable feed

The core exposes a versioned, authorized change feed to `IndexProvider` implementations. Each event contains:

- Stable subject and optional segment references.
- Snapshot or inventory revision.
- Namespace facts and selected authoritative metadata.
- Versioned operator annotations.
- Processor-derived artifacts with producer and lineage references.
- Deletion, visibility, and authorization-label changes.

The feed can be replayed from a checkpoint or regenerated from authoritative records. Index-provider acknowledgement is a projection checkpoint, not publication or recovery evidence.

### 9.2 Generation build

Each `IndexProvider` build or update targets one explicitly named index generation. That generation records:

- Index-provider package, capability, schema, and configuration digests.
- Input inventory revision and feed high-water mark.
- Analyzer, tokenizer, language, normalization, field-mapping, and feature-space identities.
- ACL projection version.
- Completeness, stale ranges, skipped artifact classes, and build warnings.

Builds occur beside the active generation. Validation covers feed coverage, fixture queries, authorization labels, missing derivatives, provider-specific health, and rollback. Activation is atomic from the query broker's perspective. A failed build leaves the prior generation active.

The bundled baseline provider indexes paths, filenames, suffixes, selected formats, content classes, sizes, recorded times, source and snapshot identity, representation and verification state, durable tags and notes, and available extracted text. It is a required product implementation and remains useful before any embedding model or vector database is installed.

### 9.3 Embedding and CLIP projections

Embeddings are processor-produced feature artifacts. An embedding record binds the source artifact, processor and model digest, preprocessing revision, vector schema, dimension, quantization, and intended similarity metric. A CLIP-compatible image or text feature is one implementation of the same rule.

`IndexProvider` consumes these artifacts into one named generation. Changing the model, preprocessing, dimensionality, quantization, distance function, or provider field mapping creates a new incompatible feature-space or index generation unless an explicit bridge is qualified. Migration normally builds in parallel. Ranking and fusion configuration belongs to `QueryProvider` and does not by itself require a new index generation.

Deleting all embedding artifacts and vector indexes degrades multimodal or semantic search only. The system can rebuild them from authoritative content and recorded processor configuration when the required implementation remains available.

## 10. Query runtime

`QueryProvider` accepts a bounded query against exactly one explicitly named `IndexGenerationRef` per invocation. The host query broker resolves any active-generation selector to that immutable reference and validates provider and generation compatibility before invocation. Every continuation token remains bound to the same exact generation. The provider owns retrieval, scoring, ranking, and any fusion across lexical, structured, vector, or media signals present in that generation. There is no separate ranker or embedding-provider ABI and no generic code, SQL, or prompt-execution query.

Structured filters use a schema-checked database-like expression tree rather than provider-specific query strings. The baseline field registry includes path, filename, suffix, selected format, content class, logical size, recorded time, source, snapshot, representation state, verification state, processing state, tag, and note text. Predicates use declared typed operators such as equality, membership, ordered comparison, bounded range, prefix, containment, and existence; boolean composition is bounded `all`, `any`, and `not`.

Results include:

- Immutable subject and optional segment references, plus authorized path, file-version, content, representation, and verification references where applicable.
- QueryProvider revision, exact index-generation reference, indexed inventory revision, and query-schema revision.
- Component scores and a concise explanation where available.
- Matched field or derivative references.
- Staleness, incompleteness, and approximate-result indicators.

The query broker reauthorizes every result, resolves current namespace information, and removes stale or hidden subjects before returning it. The response and continuation state repeat the exact generation binding so a page cannot silently cross an activation boundary. Content access always proceeds through the same bounded `FileAccess` path used by exact browse and restore. A score, nearest neighbor, caption, or generated answer is never proof of content identity or availability.

A `QueryProvider` MAY combine lexical, structured, vector, and media-specific signals available in the invocation's explicitly named `IndexGenerationRef`. Its retrieval, fusion, tie-breaking, and weighting rules are versioned QueryProvider configuration. Changing those rules changes query behavior and provider revision, not authoritative records or, by itself, index-generation identity.

## 11. User annotations and external enrichment

The MVP persists durable tags and notes attached to stable subject or typed segment references. The authoritative record stores author, time, revision, visibility, and provenance. Processor suggestions and external metadata remain separate from operator-confirmed annotations. Collections, ratings, corrections, aliases, and relationship graphs are later catalog capabilities and must not be required by the baseline lexical schema.

An enrichment processor may query an external service only under a declared egress grant. It stores the queried identifier, source, retrieval time, response digest or retained artifact, license and expiry information where applicable, and confidence. External metadata can improve discovery but cannot authorize omission or replace source bytes.

Annotation changes produce index-feed events. They do not require re-ingesting the underlying content.

## 12. External AI harness boundary

The CLI exposes the full authorized product operation set. The initial MCP adapter exposes only a bounded read-only subset. Through that initial MCP subset, an external AI harness may:

- Inspect inventory, processor availability, index health, and job state.
- Search and browse authorized subjects.
- Read bounded content ranges.
- Inspect an existing plan or explain a typed result.
- Prepare proposals for operator review.

The harness does not receive an internal scheduler, prompt store, hidden memory, arbitrary plugin call, or database handle. It cannot mint human authority or convert a processor claim into a durable decision. Plan creation, annotation mutation, apply, restore, cancellation, and other writes use an independently authorized CLI workflow in the initial release. Any later MCP mutation profile uses the same immutable plan digest where applicable, scoped grant, expected revision, idempotency, and audit rules as other clients.

AI inference needed inside data processing is implemented as a bounded processor or external service adapter. RestoreWeave owns the interface, provenance, policy, and validation around it, not the general model-serving or agent platform.

## 13. Failure and degraded modes

The runtime separates recovery impact from discovery impact:

- Processor unavailable: use the declared fallback; exact readable data remains exactly ingested and recoverable.
- Optional extraction failed: publish exact data and report missing derivatives.
- Index provider unavailable: retain the replayable feed and report search degradation.
- Query provider unavailable: namespace browse and exact reads remain available.
- Embedding model removed: retain or discard its derivatives by policy; exact recovery is unchanged.
- New generation failed validation: keep the prior active generation.
- Repository placement uncertain: reconcile before publication; do not infer success.
- Decoder dependency at risk: block removal and schedule representation migration.

Status surfaces must say which subjects and capabilities are affected, the last successful generation or checkpoint, and the safe operator action.

## 14. Technical acceptance criteria

1. The reference pipeline ingests and searches common files with all learned processors and vector providers disabled.
2. Suffix, magic-byte, parser, and optional learned evidence remain distinct and attributable.
3. A processor cannot expand its input scope, mutate the route, write the core database, or publish its own result.
4. Optional processing and indexing failures do not block exact ingest or original-directory restore.
5. Processor upgrades create parallel results with explicit lineage and rollback.
6. `IndexProvider` rebuilds from a replayable feed into a new generation and activates it atomically only after validation.
7. Each `IndexProvider` build or update binds one named generation. Each `QueryProvider` invocation receives exactly one explicitly named `IndexGenerationRef` after host-side compatibility validation; pagination cannot cross generations.
8. `QueryProvider` owns retrieval, ranking, and fusion, returns stable subject and authorized access references, and cannot bypass authorization or content-access handles.
9. A bundled lexical metadata, durable tag and note, and extracted-text query remains usable before embeddings are installed.
10. CLIP and embedding generations can be rebuilt or removed without changing recovery records.
11. The runtime operates on the qualified Linux/NAS reference host without any non-reference operating-system assumption.
12. CLI and MCP observe identical semantics for operations present in the initial bounded MCP subset; the CLI also exposes authorized mutations that MCP does not initially expose.
13. No controller component implements prompt memory, an autonomous agent loop, or generic workflow execution.
