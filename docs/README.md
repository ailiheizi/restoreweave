# RestoreWeave Documentation

All project documentation is written in English.

> **Documentation status — research-stage / pre-alpha.** The requirements and reference architecture are broad enough to begin implementation, but the end-to-end product is not implemented or released. The current Go tree contains a daemon/CLI harness and tested foundations; it does not yet provide a supported installer, qualified repository, local zvec/ONNX semantic bundle, saved-view/export workflow, or release profile.

RestoreWeave is a self-hosted, content-first managed data layer. Its first product profile combines conservative exact-storage minimization, a recoverable original-path projection, local multi-dimensional discovery, durable user annotations, saved views, reproducible exports, and safely replaceable processing, repository, and search implementations. `RW-MVP-1` is a read-only managed archive and discovery system; later NAS and enterprise profiles must qualify their additional writable and distributed semantics separately. RestoreWeave is not defined by a particular operating system, NAS vendor, repository engine, search engine, or AI runtime.

Use the public descriptor **“Self-hosted content-aware storage and discovery for NAS and heterogeneous data”** with the working name RestoreWeave. Recovery is the trust contract behind storage reduction, not the product’s only daily use.

The core owns durable identity, policy, transactions, provenance, verification, and recovery meaning. Capture, file identification, extraction, fingerprinting, transformation, storage placement, indexing, and query ranking are replaceable where their algorithms genuinely vary. The reference distribution must still ship useful defaults; the project is a product with extension points, not an empty framework.

## Recommended reading order

1. [Product Requirements](requirements/product-requirements.md) defines the product, target users, product promise, functional scope, and roadmap.
2. [Content Store, Views, and Export Requirements](requirements/content-store-views-and-exports.md) freezes the content-first storage model, default deduplication, embedding policy, view/export semantics, and replaceable seams.
3. [MVP and Operator Contract](requirements/mvp-and-operator-contract.md) freezes the first exact profile and its acceptance tests.
4. [System Architecture](requirements/system-architecture.md) defines runtime components, data flow, and authority boundaries.
5. [Core Kernel and Interface Requirements](requirements/core-kernel-and-interface.md) defines durable core ownership and compatibility policy.
6. [Driver and Processor Interfaces](requirements/driver-and-processor-interfaces.md) defines replaceable capture, processing, storage, indexing, and related extension behavior.
7. [File Identification and Extraction](requirements/file-identification-and-extraction.md) defines the extension, magic-byte, structural, and optional learned evidence ladder.
8. [Namespace and Content Access](technical/namespace-and-content-access.md) defines how physically transformed or deduplicated data maps back to a file-shaped recovery projection.
9. [Security and Threat Model](requirements/security-and-threat-model.md) defines the active filesystem, parser, repository, and interface security gates.
10. [Release Qualification and Traceability](requirements/release-qualification-and-traceability.md) defines the compatibility tuple and evidence required before a profile ships.
11. [Core MVP Execution Plan](technical/core-mvp-execution-plan.md) is the only dependency-ordered implementation path for configuration, protection, recovery, descriptions, search, storage, and exports.
12. [Remaining Work and Closed Decisions](technical/remaining-work-and-closed-decisions.md) records current status and decisions that must not be reopened.
13. [Implementation Completion Plan](technical/implementation-completion-plan.md) is a host/platform and repository-qualification record; it cannot reorder the core plan.
14. [NAS Vertical Slice Implementation Plan](technical/nas-vertical-slice-implementation-plan.md) preserves earlier slice detail but cannot reorder the core plan.
15. [Experience Completion Plan](technical/experience-completion-plan.md), [Index Dimension Plan](technical/index-dimension-plan.md), [Foreign App Jobs](technical/foreign-app-jobs.md), and [Tool-Core Wedge Plan](technical/tool-core-wedge-plan.md) are optional adapter, fixture, or ecosystem history. They do not add requirements or completion credit.
16. [Whole-Architecture Open-Source Reference](technical/architecture-open-source-reference.md) records how each layer may consume open source without changing durable contracts.
17. [Ecosystem and Vertical Applications](requirements/ecosystem-and-vertical-apps.md) and [Ecosystem App Interface](technical/ecosystem-app-interface.md) define later bounded domain experiences over the same data plane.

## MVP-defining requirements

- [Product Requirements](requirements/product-requirements.md)
- [Content Store, Views, and Export Requirements](requirements/content-store-views-and-exports.md)
- [MVP and Operator Contract](requirements/mvp-and-operator-contract.md)
- [CLI and MCP Contract](requirements/cli-and-mcp-contract.md)
- [System Architecture](requirements/system-architecture.md)
- [Core Kernel and Interface Requirements](requirements/core-kernel-and-interface.md)
- [Security and Threat Model](requirements/security-and-threat-model.md)
- [Release Qualification and Traceability](requirements/release-qualification-and-traceability.md)

The MVP reference profile is self-hosted and NAS-oriented. It accepts local or mounted filesystem roots under a declared capture-consistency class. A Linux host or container is the reference deployment shape, but no specific distribution, NAS brand, or filesystem is a product-wide requirement. Optional platform capture profiles qualify independently.

The target `RW-MVP-1` release includes persisted operator configuration, deterministic suffix and magic-byte identification, exact hashing and duplicate accounting, explicit exact/link-only protection outcomes, one release-qualified repository engine, complete captured metadata and recovery references, durable descriptions and annotations, default lexical/structured/local-semantic search, saved views, export manifests, verification, and restore. Unknown or failed content processing falls back to exact preservation. RestoreWeave does not ship a filesystem projection or mount service; external tools consume materialized exports or authorized reads. Several foundations are implemented, but this complete end-to-end loop is not shipped; see the canonical status matrix in [Content Store, Views, and Export Requirements](requirements/content-store-views-and-exports.md) and the [Core MVP Execution Plan](technical/core-mvp-execution-plan.md).

## Current status at a glance

| Area | State |
| --- | --- |
| Product definition and normative requirements | Documented and internally reviewed |
| Filesystem scanner, namespace records, and read service | Foundations implemented and tested |
| Exact repository, protection records, plan/apply executor, publication, and clean restore | Raw development CAS plus opt-in embedded `local-zstd-v1` candidate, per-file protection, true digest-bound plan/apply, Ed25519 prepared/commit records, a signed post-commit terminal processor-attempt child, and catalog-free signed restore exist. The zstd candidate proves whole-file deduplication, compression, corruption rejection, relocation, and profile isolation without Compose; production repository qualification, encryption/chunking/GC/repair, portable artifact/subject mapping, complete metadata/link-reference closure, independent-anchor clean-install workflow, retry lineage, and cross-process fencing remain. External reacquisition is a later profile |
| Durable descriptions | Append-only revisions, bounded CLI/API, and source-aligned semantic segments exist in SQLite; portable authenticated recovery and model-provider admission remain |
| Processor host and default extraction pack | In-process host plus UTF-8 text EXTRACT; Linux sandbox and Tika/ffprobe pack remain |
| SQLite lexical + zvec semantic generations | Lexical feed and fixture seams exist; fixtures are opt-in, and default readiness reports `SEMANTIC_INDEX_UNAVAILABLE` until the zvec/ONNX bundle is real |
| CLI and read-only MCP | Target interfaces; foundations exist; not a release qualification |
| Demand, net savings, NAS performance, and long-term operations | Unverified; require corpus benchmarks and operator pilots |

## Normative cross-phase interfaces

- [Driver and Processor Interfaces](requirements/driver-and-processor-interfaces.md)
- [External AI and Semantic Extensions](requirements/external-ai-and-semantic-extensions.md)
- [File Identification and Extraction](requirements/file-identification-and-extraction.md)
- [Recovery Fidelity](requirements/recovery-fidelity.md)
- [Namespace and Content Access](technical/namespace-and-content-access.md)
- [Core Protocol and Reference Userland](technical/core-protocol-and-reference-userland.md)
- [NAS Vertical Slice Implementation Plan](technical/nas-vertical-slice-implementation-plan.md)
- [Build Stack and Architecture Selection](technical/build-stack-and-architecture-selection.md)
- [Restore Manifest Extended Schema](requirements/restore-manifest.md)
- [Operations and Lifecycle](requirements/operations-and-lifecycle.md)

The baseline discovery product fuses lexical and structured fields with a local text embedding generation. Path provenance, filename, type, captured metadata, checksum, duplicates, durable tags and notes, versioned descriptions, processing state, available extracted text, and semantic similarity all resolve to stable subjects. Coverage is reported per field and dimension. User annotations and descriptions are durable records with portable closure requirements; they are not disposable index documents. CLIP, acoustic, graph, multimodal ranking, collections, ratings, and relationship graphs are staged extensions. See [Content Store, Views, and Export Requirements](requirements/content-store-views-and-exports.md) for the default ONNX/zvec profile and replacement policy.

## What is deliberately outside `RW-MVP-1`

Writable NAS protocols, automatic source deletion or pruning, P2P and magnets, remote LLM services, CLIP/OCR/ASR, neural or perceptual replacement, distributed vector services, database/VM/application-consistent capture, HA, multitenancy, enterprise governance, REST/WebUI, remote workers, mutation-capable MCP, and A2A are future profiles rather than hidden MVP obligations.

## Extended profiles

These documents preserve later requirements and design work. Documents already listed in the MVP or cross-phase sections above contain active requirements there; the remaining documents below do not add MVP obligations unless an MVP-defining document explicitly activates them.

- [Protection Policy and Planning](requirements/protection-policy-and-planning.md)
- [Application and Game Collections](requirements/application-and-game-collections.md)
- [Database and Virtual Machine Capture](requirements/database-and-virtual-machine-capture.md)
- [External Source and Retrieval](requirements/external-source-and-retrieval.md)
- [Cold Media and Offline Custody](requirements/cold-media-and-offline-custody.md)
- [Peer-to-Peer and Magnet](requirements/p2p-and-magnet.md)
- [Optional REST and WebUI Adapters](requirements/api-and-webui.md)
- [Ecosystem and Vertical Applications](requirements/ecosystem-and-vertical-apps.md)
- [Ecosystem App Interface](technical/ecosystem-app-interface.md)

P2P and magnet retrieval remain non-core. Perceptual media substitution, neural compression, VAE or RWKV-style codecs, automatic source deletion, writable NAS behavior, built-in filesystem/network gateways, and enterprise control-plane features require separately qualified profiles. External tools may present a materialized export or authorized read stream; they are not RestoreWeave storage or plugin interface families.

## Supporting designs

- [External AI and Semantic Discovery](requirements/ai-control-and-semantic-catalog.md)
- [Extension System](requirements/plugin-system.md)
- [Processing, Index, and External Automation Runtime](technical/processing-index-and-agent-runtime.md)
- [Universal Content Catalog Model](technical/universal-content-catalog-model.md)

## Research and references

- [NAS Product Power Review](references/nas-product-power-review.md)
- [Thin-Core Product Research Audit](references/thin-kernel-product-research.md)
- [Competitor and Component Research](references/competitor-research.md)
- [Open-Source Adoption and Code Borrowing](references/open-source-adoption-and-code-borrowing.md)
- [Borrowed Projects Catalog](references/borrowed-projects-catalog.md)
- [Experience Completion Plan](technical/experience-completion-plan.md)
- [Index Dimension Plan](technical/index-dimension-plan.md)
- [Whole-Architecture Open-Source Reference](technical/architecture-open-source-reference.md)
- [Ecosystem Application and Adapter References](references/ecosystem-application-adapters.md)
- [Demand Research](references/demand-research.md)
- [Historical AI and Runtime Research](references/ai-native-product-and-runtime-research.md)
- [Product Completeness Review](references/product-completeness-review.md)
- [Multimodal Fidelity Research](references/multimodal-fidelity-research.md)
- [Neural Compression Research](references/neural-compression-research.md)
- [Decentralized Music Audit](references/decentralized-music-audit.md)
- [Peer-to-Peer and Magnet Research](references/p2p-and-magnet-research.md)
- [Naming Research](references/naming-research.md)

## Benchmark and qualification runs

- [Repository Engine Qualification Spike — Kopia / Restic / Plakar](technical/qualification-spike-results.md)

## Documentation authority

When documents conflict, use this order:

1. The current MVP and Operator Contract for the first qualified release.
2. Product Requirements for product scope and priority.
3. Content Store, Views, and Export Requirements for durable content, deduplication, default discovery, view/export, and GC semantics.
4. Core Kernel and Interface Requirements for authority and compatibility boundaries.
5. Topic-specific normative requirements.
6. Extended profiles and research documents.

Platform examples never create global product requirements. An operating-system or filesystem-specific statement applies only to its explicitly named qualification profile.
