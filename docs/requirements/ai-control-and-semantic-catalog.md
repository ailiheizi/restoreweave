# External AI and Semantic Discovery Requirements

## 1. Product decision

RestoreWeave provides intelligent discovery and typed automation without becoming an AI platform. It contains no required embedded LLM, prompt loop, conversation store, agent memory, model router, autonomous planner, A2A runtime, or multi-agent harness.

AI participates through two ordinary boundaries:

1. A bounded `Processor` may classify, extract, caption, transcribe, embed, compare, or enrich content.
2. An external harness may call the same typed CLI or local MCP operations available to scripts and other clients.

Exact ingest, storage, verification, restore, and lexical/structured degraded discovery remain useful when learned processors are unavailable. A qualified `RW-MVP-1` default discovery installation nevertheless includes the pinned local text-embedding processor and zvec generation; disabling them is an explicit degraded state, not an alternate completed default.

This is the operator-facing catalog, annotation, and AI-authority profile. Provider capability schemas, semantic artifact envelopes, lifecycle identifiers, feature-space identity, index-generation protocol, and query wire contracts are defined by [External AI and Semantic Extension Requirements](external-ai-and-semantic-extensions.md). Processor mechanics are defined by [Extension System Requirements](plugin-system.md) and [Driver and Processor Interface Requirements](driver-and-processor-interfaces.md). Client transport behavior is defined by [CLI and MCP Contract](cli-and-mcp-contract.md).

This document does not define a second processor, artifact, index, or query protocol. The provider and artifact contracts in the external-extension requirements govern those technical details; this profile governs what operators can expect to see, preserve, control, and authorize.

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

## 2. Layered discovery model

RestoreWeave discovery has three layers with different durability:

| Layer | Examples | Authority and lifecycle |
| --- | --- | --- |
| Authoritative subjects and facts | Snapshot, path, file version, exact content identity, size, recorded timestamps, representation state | Core-owned and required for recovery. |
| Subject-bound attachments | Operator tags and notes, processor-extracted text, captions, transcripts, fingerprints, external metadata | Versioned with provenance; authoritative only for the fact that the attachment was recorded. |
| Search projections | Inverted indexes, vector indexes, graph indexes, ranker state, query caches | Rebuildable and replaceable; never required for exact browse or restore. |

Search, catalog views, and AI answers reference stable `SubjectRef` values. They MUST NOT make an index document ID, vector row ID, display path, filename, repository object key, or model output the identity of a file.

## 3. Lexical and structured degradation baseline

The reference self-hosted distribution MUST ship the pinned local semantic profile plus lexical and structured discovery. Exact storage and recovery remain useful without that derivative state. At minimum it supports:

- Namespace path and filename token search.
- Exact and prefix filters for type, suffix, selected format, and content class.
- Size, time, source, snapshot, checksum, duplicate-group, processing-state, verification-state, and representation-state filters.
- Exact content digest lookup and duplicate-group navigation.
- Durable whole-subject operator tags and plain-text notes.
- Available extracted-text search for supported documents and text-like files.
- Typed filters for processor availability, extraction status, verification status, and stale derivatives.
- Result navigation back to the original directory tree and bounded content access.

This baseline is a real product feature, not a placeholder for later semantic search. It SHOULD have deterministic query syntax, explain which fields matched, show its indexed revision, and degrade clearly when extraction or indexing is incomplete.

## 4. Operator annotations and machine suggestions

`RW-MVP-1` supports durable whole-subject tags, plain-text notes, versioned `DescriptionDocument` revisions, and source-aligned `SemanticSegment` records. AI description generation is optional and on-demand; user-authored and imported descriptions do not depend on a model. Later catalog profiles may add ratings, corrections, aliases, collections, relationships, and richer typed segments such as a page, time range, image region, archive member, source symbol, or media track.

Operator-authored semantics are durable user data. They MUST:

- Remain attached to stable subject or collection identities when paths or placements change.
- Preserve author, revision, conflict, visibility, and provenance history.
- Survive index deletion, model removal, and provider replacement.
- Participate in portable export and protection rather than existing only inside a search engine.
- Remain distinguishable from processor suggestions and external-source claims.

Accepting a machine suggestion creates an operator-authored revision while preserving the original suggestion and producer provenance. A processor upgrade cannot overwrite a user's tags, notes, or corrections. Concrete attachment and semantic-artifact envelopes follow the external-extension requirements.

## 5. AI as a processor

When an optional selected capability uses AI inference, it is implemented behind the ordinary `Processor` contract. Typical capabilities include:

- Learned classification for unknown or ambiguous formats.
- OCR and document-layout extraction.
- Speech recognition and speaker or chapter segmentation.
- Image and video captions, object or scene labels, and previews.
- Text, image, audio, or multimodal embeddings.
- Similarity, duplicate-candidate, and quality measurements.
- External metadata matching and enrichment.
- Later neural compression or representation candidates with explicit validation contracts.

An AI processor receives only bounded content and metadata grants selected by the host. It declares model, service, preprocessing, runtime, configuration, determinism, and output compatibility through the ordinary versioned capability and semantic-artifact contracts. Output-affecting changes create a new artifact generation.

AI output is evidence or a staged derivative. A model MUST NOT:

- Choose its own source scope.
- Approve omission, deletion, retention, publication, or garbage collection.
- Mark a representation verified.
- Convert similarity into exact identity.
- Replace readable unknown content with a generated substitute.
- Invoke another processor or external tool outside the host-built route.
- Receive unrestricted filesystem, database, repository, network, or secret access.

A local model is not inherently trusted, and a remote model is not inherently forbidden. Both use the same typed result and provenance rules; remote use additionally requires a bounded egress grant.

## 6. Embedding and CLIP product behavior

The default local text embeddings are part of the first discovery profile. CLIP-compatible features and additional embedding spaces remain optional later capabilities and are not prerequisites for exact ingest or recovery.

Embedding generation is a processor capability. An `IndexProvider` consumes its versioned feature artifacts into a named rebuildable generation, and a `QueryProvider` queries that named generation. Exact feature-space identity and compatibility follow the external-extension requirements.

Feature spaces from different model, preprocessing, dimension, quantization, or distance generations are incompatible unless a qualified bridge says otherwise. Upgrading a model builds a new feature and index generation beside the current one. The system validates coverage and known queries before activation and can roll back without rewriting prior artifacts.

Removing all embedding artifacts and vector indexes MUST leave exact namespace browse, reads, verification, restore, and baseline lexical discovery operational. If the originating model remains available, the system can rebuild the semantic projection from authoritative subjects.

## 7. Query and answer presentation

Discovery queries MAY combine:

- Structured filters.
- Lexical text.
- Similarity to a subject or supplied feature.
- Media-specific fingerprints.
- Later graph or relationship signals.

Every operator-facing result MUST identify the search mode and active provider generation, link to stable subjects, and expose stale, partial, degraded, unavailable, or approximate state. It SHOULD explain why a result matched. Query results are candidates, not facts, and disclosure follows the canonical query-time authorization rules.

If an external AI harness summarizes search results or answers a question, the answer is non-authoritative presentation. It SHOULD cite the subject and segment references it used and distinguish indexed facts, operator annotations, processor claims, and generated interpretation. Opening or restoring a result still uses the authoritative namespace and content interfaces.

## 8. External harness contract

The initial AI-facing interface is the local read-only MCP adapter over the Core Command ABI. The CLI provides the same machine-readable operations for scripts and non-MCP harnesses.

An external harness using the CLI, initial read-only MCP, or a later qualified adapter MAY:

- Inspect sources, snapshots, plans, jobs, processor profiles, and index health.
- Search, browse, stat, and read bounded authorized content.
- Ask the core to calculate a deterministic plan through the CLI or a later mutation-capable adapter; initial MCP may only inspect an existing plan.
- Prepare a proposal or human-readable explanation.
- Monitor a previously authorized operation.

The initial MCP profile MUST NOT let a harness:

- Execute a shell command, raw SQL, arbitrary URL fetch, or generic plugin method.
- Install or activate an extension.
- Read arbitrary live host paths or plaintext credentials.
- Mint an approval or represent chat text as human authority.
- Delete source data, prune the last representation, or authorize destructive retention changes.

Any later mutation profile must use a bounded capability grant, immutable plan digest, expected revisions, idempotency, expiry, destination restrictions, and durable audit trail. AI clients receive no special operation semantics and no authority from model identity, prompt text, browser presence, or an MCP tool annotation.

## 9. External metadata enrichment

An enrichment processor or external client may consult publisher catalogs, package registries, media databases, model registries, or other services to improve descriptions and retrieval hints. External information remains a source-attributed attachment under the canonical semantic-artifact contract.

The operator SHOULD be able to inspect the external source, match confidence, retrieved revision and time, disclosed identifier class, applicable license or expiry, and why the match applies. Conflicting local observations and external claims remain visible rather than being silently merged.

External metadata MUST NOT authorize source-byte omission, prove exact identity, or make reacquisition safe by itself. Network disclosure follows operator policy and explicit egress grants.

## 10. Privacy, security, and isolation

Content, filenames, paths, extracted text, thumbnails, transcripts, embeddings, and query history can all be sensitive. The product MUST apply authorization and data-residency policy to derivatives and projections, not only original bytes.

Operator-facing requirements include:

- No remote inference or enrichment by default.
- Explicit review of destination, subject class, disclosed fields, and the active bounded egress policy.
- No repository credentials, signing keys, unrestricted paths, or control-database records in model inputs.
- Sandboxing and resource limits for local processors.
- Authorization-aware index feeds and query results.
- Redaction or exclusion policies before derivative publication.
- Auditable model, service, and configuration provenance without storing secrets.
- Clear deletion and rebuild behavior for derivatives after source visibility or privacy policy changes.

Model output and indexed content are untrusted data. They MUST NOT be interpreted as control instructions, approval, or capability grants.

## 11. Availability and degraded behavior

The system reports discovery health independently from recovery health:

- A missing AI processor reports the affected derivative as unavailable and uses the configured exact or non-AI fallback.
- A failed embedding job leaves lexical discovery available.
- A stale index identifies its last complete inventory revision and missing artifact classes.
- A failed replacement generation leaves the previous validated generation active.
- Loss of every discovery index leaves original-directory browse and exact recovery available.
- An unavailable external harness has no effect on schedules, repository reads, or recovery semantics.

The UI and machine results MUST NOT describe a file as lost merely because a semantic projection is absent, nor describe it as protected merely because it appears in search.

## 12. Acceptance criteria

1. A fresh self-hosted installation can ingest, deduplicate, browse, restore, mutate durable tags/notes through CLI, and search metadata, annotations, extracted text, and local semantic embeddings through the bundled profile. Disabling the semantic derivative leaves exact recovery operational but is a degraded installation.
2. Operator annotations, processor suggestions, and external metadata remain separately attributable and versioned.
3. AI processors cannot broaden scope, approve destructive work, publish recovery truth, or mark their own output verified.
4. An embedding record identifies its model, preprocessing, feature schema, source revision, and lineage.
5. A model upgrade builds a parallel feature and index generation and can roll back.
6. Deleting the vector index affects semantic search only and does not affect exact recovery or lexical discovery.
7. Every search candidate is reauthorized before metadata, counts, snippets, or bytes are returned.
8. The initial MCP profile is local, read-only, bounded, and contains no arbitrary execution surface.
9. An external harness can explain or propose work but cannot manufacture human authority.
10. Remote inference and enrichment disclose no content without an explicit auditable egress grant.
11. Search results and generated answers link back to authoritative subject references and content access.
12. No part of the core requires prompt memory, conversation state, A2A, or a general agent runtime.
