# External AI and Semantic Extension Requirements

## 1. Purpose

Semantic discovery is part of RestoreWeave's product value. A self-hosted operator should be able to find data by path, metadata, text, meaning, similarity, relationship, or personal annotation while retaining a normal recoverable filesystem view.

Concrete AI models, embedding stacks, CLIP implementations, OCR engines, speech recognizers, vector databases, and ranking algorithms remain replaceable. RestoreWeave standardizes the subject, artifact, indexing, query, provenance, authorization, and lifecycle contracts that let those implementations change safely.

RestoreWeave is not a general AI harness. It does not need an embedded agent loop, prompt framework, model router, conversation store, agent memory, A2A runtime, or autonomous workflow engine.

The discovery extension flow has four bounded participants:

1. A **Processor** optionally examines content and emits typed extracted information, fingerprints, embeddings, or other provider-neutral features.
2. An **IndexProvider** builds or updates one named rebuildable projection generation.
3. A **QueryProvider** performs retrieval, ranking, and fusion against one named generation per invocation.
4. An external client or AI harness calls the same CLI or MCP operations as any other authorized client.

The `RW-MVP-1` reference distribution MUST bundle lexical `IndexProvider` and `QueryProvider` implementations. The core product MUST also work when all learned components are disabled. Disabling learned processing may reduce semantic recall, but it MUST NOT break baseline lexical search, ingest, namespace browsing, exact reads, verification, or restore.

This document complements [Driver and Processor Interface Requirements](driver-and-processor-interfaces.md), [File Identification and Extraction Requirements](file-identification-and-extraction.md), [CLI and MCP Contract](cli-and-mcp-contract.md), and [Namespace and Content Access Technical Design](../technical/namespace-and-content-access.md).

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

## 2. Product boundary

The core MUST own:

- Stable SubjectRef and content identities.
- Namespace and representation truth.
- Artifact envelopes and their lineage when retained by the product.
- Typed processing and query contracts.
- Authorization, policy, human decisions, and audit events.
- Index-generation identity and activation state.
- Query-provider compatibility and query-time authorization.
- Recovery-relation acceptance and exact fallback.
- Lifecycle distinctions between durable user data, recoverable representations, rebuildable derivatives, and caches.

Replaceable components MAY own:

- Learned file classification.
- OCR, ASR, captions, summaries, entity extraction, and external metadata lookup.
- Text, image, audio, video, source-code, graph, and multimodal embeddings.
- CLIP-compatible or other joint embedding spaces.
- Fingerprints and similarity candidate generation.
- Lexical, vector, graph, geospatial, temporal, and hybrid indexing.
- `QueryProvider`-owned expansion, retrieval, ranking, fusion, recommendations, typed results, and explanations; northbound adapters own presentation.
- Exact, normalized, perceptual, or neural transform candidates under the normal representation contract.

External harnesses MAY own:

- LLM reasoning and tool selection.
- Prompts, conversations, memory, model-provider routing, and agent state.
- Workflow scheduling above the typed operation boundary.
- User interaction and approval collection.

No AI component becomes authority merely because it produced a confident score, fluent explanation, route suggestion, search result, or storage recommendation.

## 3. First-class discovery capability

RestoreWeave MUST expose a provider-neutral discovery contract even when its initial implementation is lexical.

The reference distribution MUST support:

- Path and filename search.
- Filesystem and extracted metadata filters.
- Content-class and format filters.
- Durable operator tags and notes.
- Full-text search over safely extracted text when available.
- Result resolution to the original directory entry and recoverable content.

Optional providers MAY add:

- Semantic text search.
- Image-text and image-image similarity.
- Audio similarity and acoustic fingerprint lookup.
- Video scene and caption search.
- Source-code symbol and concept search.
- Application, game, package, dependency, and asset relationships.
- Model, dataset, and tensor metadata search.
- Hybrid lexical, vector, graph, and structured ranking.
- Collections, ratings, relationship graphs, and richer personal catalog views.

The stable product promise is the ability to submit typed discovery requests and receive authorized SubjectRef results. It is not a promise that every provider implements every query mode.

A provider that lacks a requested mode MUST return a typed capability error or execute an explicitly declared degraded query. It MUST NOT silently present filename matching as semantic similarity.

## 4. Processor artifact plane

Conventional and AI processors emit candidate outputs through the canonical versioned **ProcessorArtifactEnvelope** defined by the Processor contract. Semantic artifacts are one use of that common envelope; the data plane does not create an AI-specific payload ABI. The envelope is independent of any vector database or search engine.

It MUST contain:

- Artifact ID and content-addressed `SchemaRef` containing schema ID, version, and digest.
- Host-computed payload digest, media type, and size after the staging object is sealed.
- Target SubjectRef and exact source revision.
- Optional page, time, frame, byte-range, symbol, tensor, member, or image-region selector.
- Representation kind where applicable, recovery-claim reference, lifecycle class, and output authority class.
- Producer capability ID and CapabilityProfile digest.
- Implementation, model, preprocessing, configuration, runtime, and dependency digests.
- Input subject and artifact references.
- Coverage, confidence, language, normalization, and truncation.
- Creation time and generation identity.
- ACL, tenant or workspace scope, sensitivity, residency, encryption, retention, and purge lineage.
- License, attribution, redistribution, and external-source provenance.
- Warnings and known incompatibilities.

Examples include:

- Extracted text and document structure.
- OCR layout and text.
- Audio transcripts and time segments.
- Image and video captions.
- Thumbnails, keyframes, and waveforms.
- Entities, topics, and relationships.
- Fingerprints and embeddings.
- Package, game, application, model, or dataset metadata.
- External identifiers and descriptions.

The artifact envelope does not make its payload authoritative. It becomes visible to downstream routes or index feeds only after host digesting, schema validation, policy admission, and immutable handle publication. Its schema, authority, recovery claim, and lifecycle determine how it may be used.

## 5. Lifecycle classes

### 5.1 Durable user semantics

User-authored tags and notes are MVP **AUTHORITATIVE_DATA** with subtype **DURABLE_USER_SEMANTICS**. Later catalog profiles MAY add ratings, corrections, collections, aliases, descriptions, and relationships; once enabled, those values use the same authoritative lifecycle rather than disposable index state.

Durable user semantics MUST:

- Bind to a `SubjectRef` and optional typed segment; later collections bind through their own versioned identities.
- Record author, time, revision, and conflict history.
- Support portable export and exact protection.
- Remain available when every semantic model and index is removed.
- Never be overwritten by machine-generated enrichment.

A machine suggestion accepted by a user creates a new user-authored revision while retaining the suggestion's provenance.

### 5.2 Rebuildable derivatives

OCR, transcripts, captions, thumbnails, embeddings, fingerprints, and automatically extracted metadata normally use **REBUILDABLE_DERIVATIVE**.

The host MAY retain expensive derivatives to reduce recomputation, but their loss MUST NOT change authoritative content or namespace truth. Status SHOULD report when lost derivatives reduce search coverage and what reprocessing is required.

### 5.3 Recoverable representations

An AI or conventional Processor may emit a **RECOVERABLE_REPRESENTATION** candidate, such as:

- An exactly decodable compressed stream.
- A normalized document representation.
- A perceptually qualified image, audio, or video representation.
- A functionally qualified application artifact.

It enters the normal representation-admission path. Exact candidates require independent decode-and-hash verification and a retained decoder closure. Perceptual or functional candidates require a separately declared recovery relation and validator. An embedding, caption, summary, or generated reconstruction is never automatically a recoverable representation.

### 5.4 Ephemeral state

Temporary prompts, batches, caches, intermediate tensors, query traces, and partial index state use **EPHEMERAL_CACHE** unless policy explicitly promotes them. They MUST NOT become hidden restore dependencies.

## 6. Processor use of AI

AI-backed processors use exactly the same CapabilityProfile, ProcessInvocation, and ProcessResult contracts as deterministic processors.

Typical AI capability roles use the ordinary Processor vocabulary:

- **CLASSIFY_LEARNED**, including learned or domain-specific file-type evidence
- **PARSE**
- **EXTRACT**
- **ENRICH**
- **FINGERPRINT**
- **TRANSFORM**
- **VALIDATE**
- **INDEX_PREPARE**

A model is a processor dependency, not a special core service category.

The CapabilityProfile MUST declare:

- Model family and immutable weights identity.
- Preprocessor, tokenizer, vocabulary, feature extractor, and prompt-template digests when applicable.
- Accepted content classes and input ranges.
- Output schema and compatible feature-space identity.
- Determinism class, device, precision, quantization, and runtime constraints.
- Evaluation corpus, calibration, thresholds, and known failure domains where scores influence routing or validation.
- Local or remote execution and required egress.
- License, redistribution, training-use, privacy, and residency constraints.

Changing an output-affecting model alias, weights, prompt template, preprocessing step, device mode, precision, or runtime creates a new artifact generation.

Remote aliases such as **latest** without a pinned provider revision produce advisory, non-replayable results. Such outputs MUST be retained if a durable record depends on their exact contents.

## 7. Embedding and multimodal feature rules

Embeddings are `Processor` outputs and `IndexProvider` inputs. Each `QueryProvider` invocation receives exactly one explicitly named `IndexGenerationRef` after compatibility is validated. The core stores feature-space identity and lineage but does not implement the embedding mathematics or define a separate embedding-provider ABI.

Every embedding space is identified by:

- Modality or supported modality set.
- Model and exact weights digest.
- Preprocessing and segmentation.
- Dimensions, dtype, quantization, pooling, and normalization.
- Distance or similarity metric.
- Runtime and numerical mode.
- Schema and compatibility generation.

Vectors from different spaces MUST NOT be mixed or compared unless a qualified compatibility bridge exists.

A CLIP-compatible Processor implementation MAY emit features for text-to-image, image-to-image, image-to-text, and video-keyframe retrieval through a joint space. Other Processor implementations MAY emit audio-text, audio-audio, code-text, or domain-specific features through the same artifact contract.

The product MUST NOT require one universal all-modality model. Operators can install several Processor implementations and separately qualified index/query implementations. A `QueryProvider` may fuse their typed score components only when those components are available in the invocation's explicitly named `IndexGenerationRef`; the host validates compatibility before invocation, and any cross-generation fusion remains host-owned.

Embedding similarity is discovery evidence. It does not prove:

- Byte identity.
- Same edition or master.
- Authenticity or ownership.
- Sufficient visual, acoustic, or functional recovery quality.
- Safe omission of the source.

## 8. IndexProvider generations

Indexes are rebuildable projections. Each generation binds:

- Provider and capability-profile digest.
- Input inventory revision.
- Semantic-artifact high-water marks.
- Analyzer, tokenizer, feature-space, feature encoding, and field-mapping configuration.
- ACL projection version.
- Completeness, stale ranges, and failed-artifact inventory.

Index updates MUST use one of two explicit modes:

- Append or update within a provider generation when its schema and feature space are unchanged.
- Build a new generation when an output-affecting index provider, schema, analyzer, model, feature encoding, field mapping, or ACL change occurs.

A `QueryProvider` retrieval, ranking, or fusion upgrade does not require reindexing unless its new profile requires an incompatible index schema or feature space.

A reindex operation:

1. Creates a new generation beside the active one.
2. Streams authorized namespace and artifact records.
3. Records partial failures and coverage.
4. Validates counts, ACL fixtures, query fixtures, and provider health.
5. Atomically activates the new generation.
6. Retains the previous generation for bounded rollback.

Destructive in-place migration of the only active index is prohibited.

Loss of all index generations MUST leave exact namespace browsing, content reads, verification, and restore operational. Search status becomes degraded until a provider is rebuilt.

## 9. Provider-neutral query contract

`QueryProvider` is the replaceable retrieval, ranking, and fusion seam. Each invocation queries exactly one explicitly named `IndexGenerationRef` and returns immutable references; compatibility is validated before invocation. One implementation or package MAY implement both interfaces, but their capabilities and version bindings remain distinct. There is no separate ranker seam.

A **DiscoveryQuery** contains:

- Authenticated transport claims forwarded by the adapter, from which the core derives the effective actor and workspace scope.
- One or more query clauses.
- Requested query modes.
- A schema-checked structured-filter expression and namespace scope.
- Optional example SubjectRef or segment.
- Requested result fields, snippets, facets, explanations, and maximum count.
- Latency, compute, privacy, and remote-egress limits.
- Requested `IndexProvider` generation selector; `EXACT` names an immutable generation reference, while `ACTIVE` names an IndexProvider and is resolved to one exact generation before QueryProvider invocation.
- Stable pagination or continuation state.
- Selected QueryProvider profile.

The structured-filter expression is a bounded tree of `all`, `any`, and `not` nodes over typed field predicates. A predicate contains a registered field, a schema-allowed operator, and typed value or values. Baseline operators include `EQ`, `NE`, `IN`, `NOT_IN`, `LT`, `LTE`, `GT`, `GTE`, `BETWEEN`, `PREFIX`, `CONTAINS`, and `EXISTS`. Providers MUST reject unsupported field/operator pairs rather than interpreting raw SQL or provider query syntax.

The MVP field registry includes path, filename, suffix, selected format, content class, logical size, recorded time, source, snapshot, representation state, verification state, processing state, tag, and note text. Later profiles MAY register collection, rating, relationship, and graph fields through versioned query schemas.

Supported clause types MAY include:

- Lexical text.
- Exact path, name, digest, identifier, or tag.
- Structured metadata.
- Time, size, content-class, or representation filter.
- Similar-to-subject.
- Vector query bound to an embedding-space identity.
- Later graph or relationship traversal.
- Hybrid fusion.

A **DiscoveryResult** contains:

- `SubjectRef` and exact indexed revision.
- Optional segment selector.
- Authorized `PathRef`, `FileVersionRef`, `ContentRef`, `RepresentationRef`, and verification-evidence references where applicable.
- Namespace paths available to the actor.
- QueryProvider identity and revision plus the exact IndexProvider generation reference.
- QueryProvider capability-profile digest.
- Typed score components and QueryProvider retrieval, ranking, and fusion provenance.
- Matched fields, snippets, facets, or explanations when requested and authorized.
- Stale, incomplete, or degraded indicators.
- Semantic-artifact and feature-space references used for the match.

Every result is reauthorized at query time. ACL filtering after retrieving an unrestricted top-k list is insufficient because it can harm recall and leak counts, facets, timing, snippets, or neighbor relationships. Every response page and continuation token MUST remain bound to the exact resolved generation and QueryProvider revision; pagination cannot silently cross activation or provider-upgrade boundaries.

A result is a candidate reference. Before reading content or taking a storage action, the core resolves the current subject revision and applies ordinary authorization and planning.

## 10. Hybrid search and ranking

The product SHOULD support hybrid ranking without fixing one algorithm in the core or defining another extension family.

A `QueryProvider` may combine:

- Path and filename match.
- Full-text relevance.
- Structured metadata and tags.
- Vector similarity.
- Acoustic or perceptual fingerprints.
- Later collection and graph relations.
- Recency, frequency, and operator-defined importance.

Each score component MUST retain its producer, index generation, range, and normalization semantics. The `QueryProvider` profile defines retrieval, fusion, tie breaking, missing-component behavior, and deterministic or stochastic class.

Search evaluation SHOULD include content-class slices, multilingual queries, out-of-domain data, missing derivatives, ACL boundaries, and adversarial near matches. Upgrades use shadow queries and drift reports before activation.

Discovery thresholds and recovery-acceptance thresholds MUST remain separate. A model that is excellent at candidate generation may still be unsafe for data omission.

## 11. External metadata and knowledge enrichment

An enrichment Processor MAY query external services for titles, package information, media metadata, game databases, model cards, checksums, or public identifiers.

External enrichment requires:

- Explicit egress authorization.
- Pinned request identity where possible.
- Provider, endpoint, retrieval time, response digest, and license provenance.
- Separation of provider claims from locally observed facts.
- Conflict preservation.
- Refresh and expiry semantics.

An external record MAY improve search and organization. It MUST NOT replace local content, prove reacquisition, or authorize deletion unless a separate recovery plan independently establishes those properties.

## 12. External AI harness boundary

Humans, scripts, applications, and AI harnesses use the same typed northbound operation semantics. The CLI exposes the full authorized operation set. The initial local MCP adapter exposes only a bounded read-only subset; mutation-capable MCP is a later separately enabled profile.

The CLI SHOULD expose provider-neutral operations for:

- Inspecting subjects, classifications, routes, artifacts, representations, and provider status.
- Requesting bounded processing or reprocessing.
- Searching and resolving results.
- Creating and revising user tags and notes under ordinary authorization.
- Planning storage, migration, verification, restore, reindex, and cleanup.
- Applying an immutable authorized plan.
- Following operation events.

The initial MCP subset supports bounded inspection, search, browse, existing-plan reads, status, events, and content-range reads. It does not expose processing requests, plan creation or application, annotation mutation, restore, cancellation, export, or lifecycle mutation. Large content moves through bounded read handles or streams, not ordinary MCP messages.

An external AI harness MAY:

- Search and inspect authorized data.
- Compare route or storage alternatives.
- Produce an immutable advisory Proposal.
- Ask the core to generate a deterministic plan.
- Apply a plan through the authorized CLI, or through a later mutation-capable adapter, only with the same credential, policy, and required human decision as any other client.

There is no AI-specific authority path. A prompt, tool description, model identity, or claimed role cannot grant permission.

RestoreWeave MUST NOT require or persist:

- AgentRun or agent-memory resources.
- Prompt or conversation state.
- A model-provider registry for general chat.
- A2A routing.
- An autonomous planner outside the ordinary plan contract.
- Direct model access to the control database, repository credentials, signing keys, or unrestricted filesystem paths.

REST and WebUI adapters MAY be added over the same typed operations. They MUST NOT create a second policy, query, job, or recovery model.

## 13. Proposals and human authority

An external client may submit a non-authoritative **Proposal** containing:

- Producer and implementation provenance.
- Exact referenced subjects and revisions.
- Suggested classification, processing, storage, indexing, retention, or restore changes.
- Evidence and artifact references.
- Expected savings, quality, search, resource, and privacy effects.
- Assumptions, uncertainty, unsupported scope, and risks.
- Canonical digest.

A Proposal cannot execute itself. The core maps an accepted suggestion into an ordinary plan, independently checks current state and policy, and records the final human or policy decision.

Irreversible or fidelity-reducing actions require explicit authority regardless of whether a model recommended them. High automation means strong defaults, deterministic policy, transparent fallbacks, and low-friction approval; it does not mean hidden authority.

## 14. Privacy, security, and prompt injection

Content, filenames, metadata, extracted text, OCR, transcripts, external descriptions, model output, and index results are untrusted data.

They MUST NOT:

- Add processor capabilities.
- Change network destinations.
- Expand source scope.
- Introduce credentials or executable commands.
- Modify authorization or retention.
- Trigger retrieval, deletion, or source replacement.
- Override a route or validation rule.

Network access is denied by default. A remote Processor, IndexProvider, or QueryProvider requires an immutable egress profile that names:

- Destinations and transport requirements.
- Permitted subjects, fields, modalities, and sensitivity classes.
- Required minimization, redaction, and sampling.
- Credential references.
- Request, byte, time, and monetary budgets.
- Residency, logging, retention, training-use, and output-use policy references.
- Expiry and revocation identity.

The host enforces the controls it can observe and records the remainder as provider-policy provenance. It does not claim to erase data after an authorized external service or client has received it.

Semantic artifacts and indexes MUST participate in access revocation, retention, and privacy-purge lineage. Deleting a vector alone is insufficient when source text, caches, replicas, query logs, or exported provider data remain.

## 15. Failure and fallback

- Learned-classifier failure leaves suffix, magic, and generic unknown handling intact.
- OCR, ASR, caption, enrichment, fingerprint, or embedding failure marks that artifact branch incomplete.
- IndexProvider failure leaves durable ingest and namespace access operational.
- Query-provider failure returns an explicit degraded or unavailable status and MAY fall back to a declared lexical provider.
- Neural-transform failure falls back to a qualified exact representation.
- Model or index upgrades never rewrite prior provenance in place.
- Opaque nondeterministic output needed by a durable result is retained by digest.

The operator MUST be able to see:

- Which content has not been processed.
- Which artifacts or indexes are stale.
- Which provider or model generation produced a result.
- Which search modes are currently available.
- What reprocess or reindex work is needed.
- Whether any failure affects storage recovery or only discovery quality.

## 16. Reference delivery path

The reference product SHOULD deliver semantic value incrementally:

### Baseline

- Path, filename, type, size, time, source, and metadata search.
- Durable operator tags and notes.
- Full-text indexing of safely extracted text.
- Provider-neutral query API through CLI and MCP.
- Visible processing and index coverage.

### Optional processor packs

- Rich document parsing and OCR.
- Audio transcription and acoustic fingerprints.
- Image and video captions, thumbnails, keyframes, and perceptual features.
- Application, game, package, source-code, model, and dataset metadata.
- External metadata enrichment.

### Optional semantic processing and query profiles

- Processor-produced text embeddings.
- Processor-produced CLIP-compatible image and text features.
- Processor-produced audio, video, code, graph, and domain-specific features.
- Compatible IndexProvider projections and QueryProvider-owned hybrid retrieval, ranking, recommendations, and related-content views.
- Later collections, ratings, relationships, and graph catalog views.

### Experimental storage representations

- Learned or neural compression.
- Perceptual media representations.
- Model-assisted reconstruction.

Experimental representations remain behind explicit recovery contracts, independent validators, dual-write or rollback protection, and retained decoder dependencies. They are not prerequisites for semantic search.

## 17. Acceptance criteria

1. A self-hosted installation provides useful metadata, tag, and available full-text search without an LLM, embedding model, or vector database.
2. Installing a CLIP-compatible Processor plus compatible index and query implementations adds multimodal search without changing core subject, namespace, storage, or authorization semantics.
3. Removing all AI processors and semantic providers leaves ingest, exact browse, read, verify, and restore operational.
4. Every semantic result resolves to an immutable SubjectRef and is reauthorized before disclosure.
5. User-authored tags and notes survive index deletion and cannot be overwritten by generated metadata.
6. Changing model weights, preprocessing, dimensions, quantization, metric, or index mapping creates a new artifact or index generation; changing only QueryProvider ranking or fusion creates a new QueryProvider revision.
7. Reindex builds beside the active generation, validates before atomic activation, and supports rollback.
8. Embedding or fingerprint similarity cannot establish exact identity, approve omission, or authorize source deletion.
9. A neural exact-compression candidate is accepted only after independent decode-and-hash validation and retention of its decoder closure.
10. A remote model cannot receive content without an explicit bounded egress profile.
11. Instruction-like content cannot alter tool authority, processor capabilities, query scope, network destinations, or plan policy.
12. The CLI exposes the full authorized operation set, and operations included in the initial bounded read-only MCP subset have identical discovery, authorization, and audit semantics.
13. An external AI harness can inspect, search, propose, and invoke authorized operations without RestoreWeave embedding a general agent runtime.
14. Search status distinguishes stale, partial, degraded, unavailable, and fully current provider generations.
15. Failure of an optional semantic branch affects only declared discovery coverage. A profile-specific processor requirement may block only that branch, derived representation, or stronger profile claim and never the mandatory exact lane for readable bytes.
