# Historical AI and Runtime Research

> **Current-status note:** This document preserves an earlier AI-centered exploration. The current product is a NAS-first, self-hosted content-aware managed data layer whose first profile is a managed archive and search system. The authoritative boundary is defined by [System Architecture](../requirements/system-architecture.md) and [Core Kernel and Interface Requirements](../requirements/core-kernel-and-interface.md); conflicting platform-, engine-, AI-, or micro-plugin conclusions below are historical.

## 1. Current conclusion

RestoreWeave should be built as a NAS-first managed archive and search system over a small authoritative data core, not as an operating-system-specific utility or embedded AI-agent platform.

The product combines:

- A stable authoritative core for identities, accepted decisions, provenance, verification, transactions, namespace meaning, placement evidence, and clean recovery.
- A strong bundled pipeline for file inventory, suffix and magic-byte identification, exact fingerprinting, deduplication, lossless compression, placement, verification, original-directory access, baseline extraction, and metadata or text search.
- Five active extension seams—`CaptureDriver`, capability-oriented `Processor`, `RepositoryDriver`, `IndexProvider`, and `QueryProvider`—plus a later `RetrieverDriver`. Detection, extraction, fingerprinting, transformation, validation, and index preparation are `Processor` roles rather than separate public interfaces.
- CLI and MCP interfaces for operators, scripts, and external AI harnesses, with optional WebUI and REST adapters over the same core operations.

External AI, CLIP-like encoders, text or media embeddings, learned file detection, captions, OCR, ASR, and semantic ranking are valuable extensions. They should run as replaceable stages or external clients. They do not own content identity, storage truth, namespace truth, destructive authority, or publication.

APFS is one optional `CaptureDriver` profile beside ZFS, Btrfs, LVM, VSS, appliance snapshots, immutable imports, and explicitly qualified live local or mounted paths. Restic, Kopia, content-addressed stores, object storage, and other engines may provide placement implementations. Neither APFS nor Restic defines the product.

The current descriptive category is:

> A self-hosted NAS managed archive and search system over a content-aware managed data layer.

## 2. Historical decision trail

This document originally explored an **AI-operable semantic recovery system**. A now-superseded 2026-08-11 experiment temporarily narrowed that idea to a content-aware recovery kernel with one qualified APFS-to-Restic path; the current [Thin-Core Product Research Audit](thin-kernel-product-research.md) instead defines the NAS-first managed-archive product.

That narrowing produced several durable lessons:

- AI must not own recovery or deletion authority.
- Durable state and acceptance decisions belong to deterministic host code.
- CLI and MCP should expose typed operations rather than a general agent runtime.
- Search indexes and model outputs must not become the only recovery record.
- A complete default product is more valuable than an empty plugin framework.

The Mac/APFS wedge, the single-Restic-target distribution, and the conclusion that semantic search should remain entirely outside the product are superseded. They are retained only as historical evidence of how the authority boundary was derived.

The current direction keeps the small authoritative center while broadening the useful product around it: NAS-first deployment, exact storage reduction, a recoverable filesystem projection, baseline search, and replaceable content-processing stages.

## 3. Evidence legend

- **Observed:** inspected in local source, tests, or a primary repository artifact.
- **Documented:** stated by the project's own documentation but not independently proven end to end.
- **Inferred:** a RestoreWeave design conclusion drawn from observed or documented mechanisms.
- **Historical conclusion:** a decision that informed the architecture but is no longer the current product recommendation.

Attention signals such as stars and discussion counts are not treated as demand, correctness, or production-readiness evidence.

## 4. Primary seed audits

### 4.1 Siftline

Local seed: `/Users/macos/Documents/other_project/siftline`

Observed:

- Siftline separates the research brain from deterministic search sensors.
- The CLI returns a schema-versioned result envelope with query identity, provider, operation, parameters, items, errors, and provenance.
- Shared middleware provides cache lookup, error classification, stable deduplication, and a SQLite research ledger.
- Provider registration is static. There is no runtime plugin discovery, REST service, vector store, WebUI, MCP, A2A, embedded LLM, capability sandbox, or signed audit chain.
- The local and globally installed CLIs reported version `0.2.0` during the audit. The inspected local test set passed with 132 tests and three deliberate deselections.
- The project is MIT licensed.

Decision-changing lesson:

> An intelligent planner may choose which typed operation to request; deterministic adapters execute it and return normalized evidence.

For RestoreWeave, this pattern applies to external research, metadata enrichment, learned detection, and other optional processing. The core needs typed stage and command contracts, input and output identities, provenance, idempotency, cancellation, budgets, and capability grants. It does not need to own a research brain or model-provider runtime.

Siftline is suitable as an external research client or as an implementation behind a bounded enrichment stage. It is not the RestoreWeave storage runtime or authority layer.

### 4.2 Poiema

Local seed: `/Users/macos/Documents/other_project/poiema`

Observed:

- Poiema is outcome-first: conversations and files become durable tasks, reviewed deliverables, and reusable tasks.
- Desktop and CLI clients use typed local contracts while a daemon owns canonical SQLite and content-addressed state.
- Execution handlers, model providers, source resolvers, transports, and secret providers are replaceable seams.
- Task, operation, effect, approval, event, grant, and revision truth remain host-owned.
- External effects have durable request and reconciliation behavior.
- No implemented MCP, A2A, ACP, or general multi-agent protocol was found; A2A is deferred.
- The local test run passed 1,193 tests with one explicit live-provider skip.
- The project is AGPL-3.0-or-later.

Decision-changing lesson:

> The UI and intelligent behavior are projections over durable state and rules; external components may propose bounded work while deterministic host code owns effects and acceptance.

RestoreWeave may borrow outcome-first interaction, durable operations, reconciliation, reviewed changes, and narrow context-adapter patterns. It should not adopt Poiema's task runtime as a storage dependency. Direct code reuse requires AGPL compatibility or separate rights; clean-room adaptation of architecture is safer for a differently licensed product.

### 4.3 Weft

Primary reference: [ailiheizi/weft](https://github.com/ailiheizi/weft)

Observed at audited commit `b91b9c7a6cf30bf54abf36b479eaafff4100d8e8`:

- Weft's primary abstraction is a capability registry plus package composition and an AI agent harness.
- Its Rust core loads WASM, service-process, and native providers, binds stable capability IDs, and manages propose, verify, activate, and rollback generations.
- `agent-core`, team routing, task-board, workflow-template, and workflow-orchestrator packages implement the current agent and multi-agent behavior.
- MCP client support exists, but standard A2A is not implemented and remains documented as future work.
- Current defaults are routing and retry policies, not evidence that the system selects the objectively best algorithm.
- The audited macOS build failed because a Unix code path used an undeclared direct `libc` dependency. Packaging and cross-platform qualification were incomplete at the audited revision.
- Package permission checks are useful but do not constitute a production-grade storage sandbox.
- The root license is Apache-2.0; several official packages use MIT and require their applicable notices.

Decision-changing lesson:

RestoreWeave may borrow capability-registry, manifest, generation, verification, activation, rollback, and protocol-adapter patterns. It should not place byte, namespace, placement, verification, retention, or deletion authority inside a general package or agent runtime.

Weft may be:

- An optional external AI control plane.
- A client of RestoreWeave CLI or MCP.
- A source of package lifecycle and capability negotiation patterns.
- A host for non-authoritative enrichment or planning components when capability isolation is sufficient.

Broad algorithm replaceability does not make RestoreWeave equivalent to Weft. RestoreWeave has a fixed storage-domain contract; Weft composes general capabilities.

## 5. Retained product and implementation references

The following references materially changed the product decision. Their mechanisms remain relevant even where the original product conclusion was superseded.

| Reference | Observed or documented mechanism | Current RestoreWeave boundary |
| --- | --- | --- |
| Siftline | Typed deterministic sensors behind a research brain, normalized evidence, cache, and ledger | External enrichment or research client; not storage authority |
| Poiema | Outcome-first durable tasks, reviewed outputs, typed daemon, durable effects | Borrow durable operation and review patterns; keep storage truth independent; AGPL code boundary |
| [Weft](https://github.com/ailiheizi/weft) | Capability registry, packages, generations, rollback, MCP client, and agent runtime | Optional control plane and plugin-lifecycle reference; never storage authority |
| [Kopia](https://github.com/kopia/kopia) | Content-addressed snapshots, deduplication, compression, policies, verification, selective restore, and mounted snapshot views | Strong reference or candidate placement implementation behind RestoreWeave identities and policy |
| [Immich](https://github.com/immich-app/immich) | Media catalog, CLIP-style search, OCR, tags, metadata, and asynchronous processing | Strong semantic-stage and UX reference; model and index state remains replaceable; AGPL boundary |
| [Paperless-ngx](https://github.com/paperless-ngx/paperless-ngx) | OCR, versions, tags, custom fields, classifiers, workflows, REST, and opt-in AI metadata suggestions | Strong annotation, extraction, and suggestion-review reference; GPL boundary |
| [AI File Sorter](https://github.com/hyperfield/ai-file-sorter) | Headless review-only planning, saved JSON plans, independent apply, dry-run, and undo | Borrow propose, review, apply, and rollback UX; AGPL boundary |
| [LlamaFS](https://github.com/iyaja/llama-fs) | API-driven AI file organization, batching, and watching | Useful interaction proof and safety counterexample; direct moves are not transactionally safe storage execution |
| [Magika](https://github.com/google/magika) | Learned content-type classification with confidence behavior | Optional `CLASSIFY_LEARNED` `Processor` capability after suffix and magic evidence; never identity or fidelity authority |
| [Apache Tika](https://tika.apache.org/) | Broad document detection, metadata, and text extraction | Sandboxed `PARSE` and `EXTRACT` `Processor` capabilities; output is searchable evidence, not structural completeness or recovery proof |
| [Model Context Protocol](https://github.com/modelcontextprotocol/modelcontextprotocol) | Standard tool and resource exposure for AI clients | Northbound adapter over Core Command ABI; annotations do not grant mutation authority |
| [A2A](https://github.com/a2aproject/A2A) | Agent Cards, cross-service tasks, streaming, and asynchronous agent interoperability | Optional later adapter; an A2A task must map to a bounded RestoreWeave operation |

Additional processor, codec, vector, workflow, and storage references remain in [Competitor and Component Research](competitor-research.md).

## 6. Current product shape

The current product has three cooperating layers.

### 6.1 Authoritative storage core

The core owns stable identities, immutable plans and decisions, provenance, source-view binding, transaction state, verification meaning, placement acceptance, portable publication, namespace reconstruction, representation selection, and lifecycle safety.

These meanings remain fixed even when the storage engine, compressor, detector, model, index, ranker, or UI changes.

### 6.2 Opinionated reference distribution

The product ships useful defaults rather than interfaces alone:

- A self-hosted controller suitable for Linux-based NAS and server deployment.
- CLI and local MCP access.
- Cross-platform source profiles with accurately declared consistency.
- Suffix plus magic-byte or structural identification.
- Exact hashing, content reuse, deduplication, and lossless compression.
- Placement, readback, reconciliation, and portable commit.
- Original-directory browsing and restore.
- Baseline metadata and text extraction and search.

### 6.3 Replaceable processing and presentation userland

Detection, extraction, fingerprinting, transformation, validation, placement, indexing, ranking, retrieval, filesystem gateways, WebUI, REST, and external automation are replaceable stages or adapters.

This is not arbitrary workflow composition. Each stage participates in a fixed storage-domain graph and returns typed provenance-bound results. The core retains the right to accept or reject those results against policy.

## 7. Human and AI responsibility

The default storage and discovery loop works without AI:

~~~text
operator or external client requests ingest
-> Core captures or validates a source view
-> deterministic defaults identify, fingerprint, deduplicate, compress, and place data
-> Core verifies and publishes the recoverable namespace
-> baseline indexers make paths, metadata, types, annotations, and extracted text searchable
-> operator, application, or external AI browses, queries, reads, or restores stable subjects
~~~

An external AI may:

- Request inspection, ingest planning, search, or content access through authorized CLI or MCP operations.
- Add classification evidence, extracted information, captions, embeddings, or ranking candidates through a stage contract.
- Propose tags, notes, policies, placements, or transformations.
- Explain plans and verification evidence.

It may not:

- Assert exact content identity from similarity.
- Publish a snapshot or accept its own validation result.
- Exclude or replace exact data without durable authority.
- Bypass transaction, placement, capability, or lifecycle controls.
- Treat an embedding index as the only namespace or recovery record.

## 8. Default UX decision

The primary administrative surfaces are CLI and MCP over one typed command contract. A long-running self-hosted controller is appropriate for NAS operation; this does not require an embedded AI harness.

A WebUI may later provide source selection, storage policies, stage presets, health, search, browse, and review. Ordinary forms, checkboxes, presets, and reviewable diffs are better defaults than a blank node canvas. Expert graph inspection may exist as a diagnostic view, but users should not need to wire every ingest pipeline manually.

## 9. Strong defaults versus plugins

### 9.1 Strong defaults

- Suffix evidence followed by magic-byte or structural evidence, with conservative conflict handling.
- Exact cryptographic full-content identity and a pinned chunking profile.
- Exact content reuse, deduplication, and lossless compression through a mature bundled storage path.
- Raw or exact reversible fallback for every readable unknown or unsupported file.
- Safe metadata and text extraction for supported common formats.
- Path, metadata, type, annotation, and extracted-text search.
- Readback validation, durable placement receipts, and portable publication commit.
- A rebuildable operational database and indexes.

### 9.2 Replaceable stages

- Source capture and consistency adapters.
- Detectors, extractors, and fingerprint providers.
- Chunkers, compressors, packagers, transcoders, neural encoders, and other transformers.
- Exact, structural, perceptual, and application-specific validators.
- Placement engines and storage targets.
- Full-text, vector, graph, visual, acoustic, and multimodal indexers.
- Query rankers and result-fusion implementations.
- External retrieval providers.

Versions may coexist. Plans pin implementation and profile digests, upgrades apply to new plans or explicit migrations, and required decoders remain available while retained representations depend on them.

### 9.3 Non-replaceable semantics

- Logical content, file-version, namespace, representation, snapshot, and placement identities.
- Immutable accepted decisions and policy authority.
- Provenance and dependency meaning.
- Exact versus approximate fidelity meaning.
- Verification acceptance.
- Transaction fencing, idempotency, reconciliation, and portable publication commit.
- Original-directory access and clean recovery.

## 10. Embeddings and semantic information

Semantic discovery is a product capability, but no single model or index engine is part of the authoritative core.

The reference distribution should provide useful metadata and extracted-text search. CLIP-like image or video encoders, text embeddings, audio-language embeddings, OCR, ASR, captions, code intelligence, and domain-specific models can be installed later as replaceable `Processor` capabilities, `IndexProvider` implementations, or `QueryProvider` implementations.

Every derived semantic artifact should bind:

- The exact subject and source representation.
- Model, implementation, and preprocessing digests.
- Schema and model-space version.
- Coverage, parameters, provenance, and sensitivity scope.
- The index generation that consumed it.

Model upgrades create new derivative and index generations. They do not change exact content identity. Search indexes are rebuildable by default. User-authored tags, notes, collections, and accepted corrections are durable semantic records and should enter the portable closure according to policy.

## 11. Incremental update and deletion

NAS operation requires incremental generations, recurring profiles, placement health, and controlled deletion. These are storage-domain responsibilities, not reasons to embed a general workflow engine.

The core must preserve distinctions between:

- Suspected unchanged content and freshly hash-verified content.
- A rename candidate and proven exact asset identity.
- Source absence, confirmed tombstone, and policy exclusion.
- Hiding an item from a catalog and removing its protected representation.
- Snapshot retirement and physical garbage collection.
- Moving or recompressing a representation and changing logical content identity.
- Model-generation invalidation and content invalidation.
- Privacy purge and ordinary retention expiry.

A watch service or scheduler may trigger inspection, but it cannot redefine these semantics. No last required representation, decoder, placement, or portable record becomes collection-eligible while a committed snapshot depends on it.

## 12. Strongest counterevidence

The main risks are operational complexity, plugin fragmentation, and unclear willingness to pay for a combined storage and discovery system.

- Mature semantic products still tell users to protect original media and catalog data separately. Semantic usefulness does not prove storage durability.
- Model changes can require expensive reprocessing and alter search behavior. Embeddings are versioned projections, not durable meaning by themselves.
- Untrusted parsers, codecs, and models expand the security and resource-management surface of a self-hosted NAS.
- Too many plugin interfaces can create compatibility, decoder-retention, upgrade, and support burdens.
- Mature storage engines already provide deduplication, compression, encryption, snapshots, and verification. RestoreWeave must add clear cross-engine namespace, policy, provenance, processing, and discovery value rather than reimplementing their private formats.
- Media-specific approximate replacement can save substantial storage but introduces difficult quality, decoder, and long-term recoverability questions.
- Traditional NAS search, photo, and document applications may already solve enough of the discovery problem for some operators.
- No retained public source proves willingness to pay for the complete combination or willingness to entrust automatic lossy substitution and deletion.

The roadmap should narrow if field tests show weak storage savings, excessive correction burden, unacceptable indexing cost, or little value beyond existing NAS applications. The safe fallback is still a useful exact content-addressed store with baseline search and a recoverable namespace; advanced AI and perceptual representations can remain optional.

## 13. Naming conclusion

No immediate rename is required. **RestoreWeave** may remain the working product name while positioning language changes.

The name still describes a graph that weaves source observations, exact bytes, representations, metadata, storage placements, annotations, indexes, and verification evidence into a recoverable whole. However, product copy should lead with storage reduction and intelligent discovery rather than Mac recovery.

Recommended descriptor:

> Self-hosted content-aware storage and discovery for NAS and heterogeneous data.

Recommended promise:

> Store fewer redundant bytes, find content intelligently, and restore the original filesystem with proof.

The name should be reviewed after operator testing and formal trademark work, not merely because the architecture supports plugins or external AI.

## 14. Historical Siftline discovery run

The earlier research run used stable query ID:

~~~text
restoreweave-product-reframe-2026-08-11
~~~

It searched GitHub and Hacker News for semantic filesystems, unstructured processing, backup and NAS recovery discussion, and MCP filesystem servers.

Observed discovery signals included:

- [AIOS-LSFS](https://github.com/agiresearch/AIOS-LSFS) as a semantic-filesystem research implementation.
- [Extractous](https://github.com/yobix-ai/extractous) and document RAG projects as evidence of a reusable extraction ecosystem.
- Multiple MCP filesystem servers as evidence that AI-facing filesystem tools are an established adapter category.
- Hacker News discussions emphasizing selective restore, multiversion backup, offsite independence, and the difference between RAID and backup.

These historical results support mechanism availability. They do not prove current product demand, correctness, performance, or willingness to grant autonomous deletion authority.

Historical operation accounting from the Siftline ledger:

~~~text
issued_invocations: 8
machine_attempts: 8
unledgered_attempts: 0
effective_attempts: 8
provider_calls: 8
budget: 8
~~~

All eight provider calls succeeded with no cache hits or validation failures. GitHub and Hacker News were available. Exa, Tavily, and the OpenAI-compatible Web provider were unavailable because their keys were not configured.

The installed Codex Siftline skill and repository copy had different file digests during that audit. The CLI and ledger behavior were verified directly; future reproducible runs should pin the intended skill and CLI artifacts.

## 15. Coverage boundary

This research preserves local-source inspection, tests, canonical repository material, official documentation, and public GitHub and Hacker News results available on 2026-08-11. No new external search was performed for this NAS-first reframing.

It did not cover:

- Private enterprise or NAS deployments.
- Paid conversion, retention, or willingness-to-pay data.
- Proprietary storage and backup internals.
- Formal trademark clearance.
- Every model-weight, codec, dataset, plugin, or transitive dependency license.
- Benchmarks of end-to-end storage savings, indexing cost, or retrieval quality for the proposed default pipeline.

Licenses in this report apply to the named repository or inspected subtree only. Model weights, bundled codecs, datasets, plugins, and deployment artifacts require separate verification before reuse.
