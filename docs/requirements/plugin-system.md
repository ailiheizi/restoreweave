# Extension System Requirements

## 1. Purpose

RestoreWeave is a self-hosted, NAS-first content-aware managed data layer. Its extension system exists so content algorithms and storage integrations can improve without changing the meaning of a file, a published namespace, an ingest plan, or verification evidence. The first qualified product profile is a read-only managed archive and search system.

The product is not an empty plugin framework. The reference distribution MUST provide a useful path for ordinary heterogeneous files before any optional extension is installed:

- Filesystem ingestion on a supported server platform.
- Suffix evidence followed by magic-byte evidence.
- Exact preservation for unknown or unsupported readable content.
- Deduplication and deterministic compression through a qualified default repository path.
- Original-directory browse, read, verify, and restore.
- Durable whole-subject tags and plain-text notes with revisioned CRUD and portable export/import.
- Metadata, filename, path, type, checksum, duplicate, processing-state, and available extracted-text search.
- Export-manifest materialization over the same authenticated namespace and content-read contracts.
- CLI and local read-only MCP access to the same typed core operations.

Optional extensions add better classification, extraction, compression, media understanding, indexing, ranking, storage placement, or source capture. They do not become recovery authority.

This document defines the product-level extension policy. Detailed invocation semantics are defined by [Driver and Processor Interface Requirements](driver-and-processor-interfaces.md), and runtime behavior is described in [Processing, Index, and External Automation Runtime](../technical/processing-index-and-agent-runtime.md).

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

## 2. Extension boundary

RestoreWeave exposes extension seams only where implementations are expected to vary materially:

| Extension role | Responsibility | Initial expectation |
| --- | --- | --- |
| `CaptureDriver` | Present a bounded source view, snapshot, share, or export. | One platform-neutral filesystem profile is required; snapshot-specific drivers are optional. |
| `Processor` | Analyze or transform one declared class of content through typed inputs and outputs. | The host ships strong baseline processors; richer modality processors are optional. |
| `RepositoryDriver` | Place, read, verify, and restore admitted representations. | One qualified deduplicating and compressing path is required. |
| `IndexProvider` | Consume a replayable authorized feed and build or update a versioned discovery projection. | Hybrid lexical/structured plus local semantic generations are required. |
| `QueryProvider` | Query exactly one explicitly named `IndexGenerationRef` per invocation and return subject-bound candidates; the host validates compatibility before invocation. | The default broker fuses lexical, structured, and local semantic candidates; other vector and multimodal query is optional. |
| Later `RetrieverDriver` | Reacquire a pinned external artifact under a qualified policy. | Reserved; not required by `RW-MVP-1`. |

One package MAY implement more than one role, but each capability is negotiated, versioned, authorized, and tested separately. One implementation MAY provide both `IndexProvider` and `QueryProvider`, but index-feed and query schemas and generation identities remain separate.

The following remain core-owned and are not replaceable plugins:

- Content, file-version, representation, namespace, snapshot, and placement identity.
- Classification evidence history and the host-selected content class.
- Policy, planning, routing, fallback, and final human authority.
- Provenance, operation journaling, idempotency, transactions, and reconciliation.
- Representation validation, admission, preference, publication, and garbage-collection eligibility.
- Namespace projection, exact-content access, authorization, and audit semantics.
- Index-generation activation and authorization of query results.

RestoreWeave MUST NOT create a separate privileged plugin family for every media type or algorithm. OCR, ASR, captioning, parsing, learned classification, fingerprinting, transcoding, compression, neural encoding, and embedding generation are processor capabilities. Full-text, vector, graph, and multimodal systems are index-provider or query-provider implementations.

## 3. Typed processing routes

The host begins the exact lane independently, then builds a bounded `ProcessingRoute` from file-identification evidence:

~~~text
inventory
  |-> mandatory exact hash -> exact RepositoryDriver placement -> readback
  |-> suffix evidence -> magic-byte evidence -> optional learned evidence
      -> host-selected content class -> versioned Processor route
      -> independent validation -> derivative or representation candidate
      -> index feed
~~~

A route is a typed host-owned plan, not a user-authored arbitrary workflow graph. Each node binds an exact capability version, configuration digest, declared input schema, output schema, resource budget, and fallback. The host rejects schema-incompatible or cyclic routes before execution.

The mandatory exact lane is not a `Processor` route and cannot be delayed by any classifier, parser, extractor, transform, validator, embedding, or index. The local text-embedding profile is required for release-level default discovery, but its invocation remains asynchronous and non-authoritative; failure reports degraded coverage while readable bytes remain eligible for exact publication.

Routes MAY branch. For example, an image can be preserved exactly, have metadata and a thumbnail extracted, and later receive a CLIP-compatible embedding. The exact representation, thumbnail, and embedding have different lifecycle and authority classes even when produced in one ingest job.

Extensions MAY return evidence or staged candidates that cause the host to propose a successor plan. They MUST NOT append executable steps to the active route, silently change policy, or weaken the required recovery relation.

## 4. Processor capability model

A processor declares one or more versioned capabilities from a small stage vocabulary:

- `CLASSIFY_LEARNED`
- `PARSE`
- `EXTRACT`
- `ENRICH`
- `FINGERPRINT`
- `TRANSFORM`
- `VALIDATE`
- `INDEX_PREPARE`

Content classes are open namespaced values. The same processor contract must support text, documents, images, audio, video, archives, source code, databases, datasets, models, games, applications, disk images, and future vendor-specific types without adding modality-specific execution logic to the core.

Every processor invocation MUST bind:

- Processor ID, release version, capability ID, implementation digest, and protocol version.
- Immutable subject and file-version references.
- Bounded content handles rather than ambient host paths.
- Classification and route digests.
- Canonical parameters and dependency, model, dictionary, or codec identities.
- CPU, memory, accelerator, temporary-storage, time, output, recursion, network, and cost limits.
- Cancellation, retry, privacy, egress, and secret grants.

Every result MUST report typed outputs, inspected coverage, provenance, warnings, determinism, resource use, and claimed recovery or quality contracts. Processor success means only that the processor completed its declared work. The host still decides whether to accept, retain, index, place, or publish the result.

A processor MAY propose candidate tags, notes, descriptions, captions, entities, relationships, or corrections as attributed derived records. Those candidates are not the authoritative operator annotation set. Only a separately authorized core command may create or revise durable user tags and notes, preserving actor, revision, visibility, provenance, and tombstone semantics.

A transformation claiming exact recovery MUST have a pinned decoder closure and pass host-controlled round-trip validation. A perceptual, normalized, regenerated, or lossy output MUST remain explicitly distinct from exact content and MUST NOT silently replace exact fallback.

## 5. Index-provider and query-provider model

Discovery state is a rebuildable projection, not part of recovery truth.

An `IndexProvider` consumes a replayable, authorized feed containing stable subject references and selected fields such as:

- Namespace path and filename tokens.
- File type, size, time, ownership, and other recorded metadata.
- Operator tags, notes, and corrections.
- Extracted text and structured metadata.
- Captions, transcripts, fingerprints, thumbnails, embeddings, and other derivatives when installed.

Every index generation MUST bind the provider version, schema digest, source inventory revision, feed high-water mark, analyzers, tokenizers, normalization, authorization-label revision, and feature-space identity where relevant. Query-time ranking, fusion, weighting, and tie-breaking belong to the versioned `QueryProvider` configuration unless they alter data materialized during index construction. A generation is built beside the active generation, validated, and then atomically activated by the host. The prior generation remains available for rollback until retention policy permits retirement.

A query provider returns immutable subject references, optional segment references, provider and generation IDs, score components, explanations, and completeness or staleness information. The core query broker MUST reauthorize every candidate before any presentation adapter receives metadata or may open content. Counts, facets, snippets, nearest neighbors, and error messages MUST NOT leak unauthorized subjects.

The reference distribution MUST provide hybrid lexical, structured, and local semantic discovery by default. CLIP, audio embeddings, learned rerankers, and additional hybrid fusion providers are later capabilities. Their outputs and index generations are rebuildable, replaceable, and removable without affecting namespace browsing, exact reads, verification, or restore.

## 6. Package and capability declaration

Each executable extension package MUST be content-addressed and signed under a configured publisher trust policy before activation. Its manifest MUST declare:

- Package identity, semantic version, package digest, publisher, and signature information.
- Supported protocol ranges and platform or architecture constraints.
- One or more independently versioned capability profiles.
- Accepted content classes, evidence predicates, and input and output schemas.
- Determinism and reproducibility class.
- Streaming, seeking, batching, concurrency, cancellation, and partial-result behavior.
- Runtime, library, codec, model, tokenizer, dictionary, and decoder dependencies.
- Required filesystem, network, secret, repository, accelerator, and temporary-storage capabilities.
- Resource ceilings and expected cost characteristics.
- Privacy, data-egress, license, redistribution, and telemetry declarations.
- Upgrade, output-compatibility, rebuild, migration, and rollback behavior.

Unknown critical manifest fields or capability authorities fail closed. Internal Go interfaces, SQLite rows, worker topology, and repository-private object formats are not public extension contracts.

## 7. Versioning, replacement, and upgrade

"Hot-swappable" means that a new implementation can be installed and activated without changing authoritative identity or requiring an in-place catalog rewrite. It does not mean replacing executable code inside an active invocation.

Installing a new version MUST register a new immutable capability-profile digest. Existing plans, results, representations, and index generations retain their original producer identity. Activation MAY be immediate for new work, canary-scoped, or shadowed against the current version.

Output staleness is derived from at least:

~~~text
subject revision
+ capability-profile digest
+ configuration digest
+ model, dependency, and preprocessing digests
+ output-schema revision
~~~

Changing an output-affecting term creates a new generation. Reprocessing and reindexing occur beside active results, validate before activation, and support rollback. Prior evidence is not rewritten in place.

An extension or decoder MUST NOT be removed while an admitted recoverable representation depends on it unless all dependent representations have been migrated to a qualified readable closure. Rebuildable derivatives and indexes MAY be discarded after their lineage and rebuild requirements are recorded.

## 8. Defaults and fallback

The reference profile MUST define an opinionated route for common content rather than asking an operator to assemble a graph:

1. Inventory the namespace and start host-owned exact hashing and exact repository placement eligibility.
2. Record suffix evidence.
3. Inspect bounded magic bytes and container signatures.
4. Invoke learned classification only when installed and selected by policy.
5. Let the host select the versioned content class and bounded processing route.
6. Run safe matching parsers and extractors for supported types.
7. Publish metadata, durable tags and notes, and available extracted text to the baseline index.
8. Add optional modality derivatives without blocking durable ingest or exact publication.

Fallback rules are mandatory:

- Unknown, conflicting, or unsupported readable content uses exact raw or repository-native storage.
- A failed optional extraction, fingerprint, embedding, or index operation marks discovery as degraded and does not block exact ingest.
- A failed transform or validator falls back to a qualified exact representation.
- A profile-specific processor requirement MAY block only that processing branch, derived representation, or stronger profile claim. It MUST NOT block capture, inventory, host-owned exact hashing, exact placement eligibility, required exact-lane verification, or exact publication of readable bytes.
- No fallback may omit data or reduce fidelity without a new human-authorized plan.

## 9. Isolation and authority

Third-party extensions MUST run with least privilege under a platform-appropriate sandbox or isolated worker boundary. The default grant has no network, plaintext credentials, control-database access, repository administration, unrestricted host paths, arbitrary shell execution, or ability to spawn unbounded children.

Remote inference or enrichment requires a bounded egress grant naming the destination, disclosed data classes, subject scope, byte and cost budgets, expiry, and audit policy. File contents and model outputs are untrusted data and MUST NOT be interpreted as instructions that broaden the grant.

An extension MUST NOT:

- Approve omission, deletion, publication, retention change, or garbage collection.
- Write authoritative catalog or namespace records directly.
- Choose a broader source scope or access unrelated subjects.
- Mark its own candidate as verified.
- Treat an embedding distance, perceptual hash, remote result, or repository receipt as exact identity.
- Embed a prompt loop, agent memory, or general workflow runtime into the core process.

## 10. External automation

RestoreWeave exposes the full authorized typed operation set through CLI and a bounded local read-only MCP subset for scripts and external AI harnesses. These adapters operate above the extension system. They MUST NOT expose arbitrary plugin invocation, package installation, shell execution, or raw database access.

Through the initial MCP profile, an external harness may inspect inventory, status, capabilities, processing coverage, annotations, snapshots, namespace entries, bounded content, search results, verification evidence, and existing plans. It may produce an external proposal, but it cannot create or revise a plan, mutate annotations, apply work, restore a destination, cancel a job, initialize or prune a repository, install an extension, or request another mutation. A later mutation-capable adapter, if qualified, receives no additional authority because it is an AI client and remains bound to the same plan digest, principal, policy, revision, idempotency, and explicit authorization used by other clients.

## 11. Acceptance criteria

1. A qualified NAS/server deployment can ingest, reduce storage, export, search with the default local semantic profile, verify, and restore without any non-reference platform capture driver, distributed vector service, or WebUI.
2. Exact storage, verification, and restore remain useful when the local semantic extension is disabled; that degraded state is not reported as the complete default experience.
3. Extension upgrades create new immutable profiles and output generations instead of rewriting provenance.
4. Text, image, audio, video, game, application, archive, model, and future content processors use one typed contract.
5. Unknown content and failed optional processors fall back to exact preservation.
6. Reprocessing and reindexing build beside active generations, validate before activation, and can roll back.
7. Loss of all semantic and vector indexes leaves namespace browse, exact reads, verification, and restore operational.
8. Query results are reauthorized and cannot disclose unauthorized counts, snippets, or neighbors.
9. A plugin cannot write recovery truth, expand its grant, invoke an arbitrary next step, or approve its own output.
10. Removing a required decoder is blocked until dependent representations are migrated.
11. Durable tag and note records survive deletion and rebuild of every search index, while processor-proposed annotations remain visibly non-authoritative until accepted through a core mutation.
12. The initial MCP profile cannot create plans or perform any mutation, even when the caller is an AI harness.
