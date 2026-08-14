# RestoreWeave Documentation

All project documentation is written in English.

> **Documentation status — research-stage / pre-alpha.** The requirements and reference architecture are broad enough to begin implementation, but the end-to-end NAS product is not implemented or released. The current Go tree contains foundations and tests; it does not yet provide a supported binary, installer, repository adapter, CLI, MCP server, search provider, or FUSE gateway.

RestoreWeave is a self-hosted, NAS-first content-aware managed data layer. Its first product profile combines conservative exact-storage minimization, a recoverable original-path namespace, useful heterogeneous discovery, durable user annotations, read-only filesystem access, and safely replaceable processing, repository, and search implementations. `RW-MVP-1` is a read-only managed archive and search system; later NAS and enterprise profiles must qualify their additional writable and distributed semantics separately. RestoreWeave is not defined by a particular operating system, NAS vendor, repository engine, search engine, or AI runtime.

Use the public descriptor **“Self-hosted content-aware storage and discovery for NAS and heterogeneous data”** with the working name RestoreWeave. Recovery is the trust contract behind storage reduction, not the product’s only daily use.

The core owns durable identity, policy, transactions, provenance, verification, and recovery meaning. Capture, file identification, extraction, fingerprinting, transformation, storage placement, indexing, and query ranking are replaceable where their algorithms genuinely vary. The reference distribution must still ship useful defaults; the project is a product with extension points, not an empty framework.

## Recommended reading order

1. [Product Requirements](requirements/product-requirements.md) defines the product, target users, product promise, functional scope, and roadmap.
2. [MVP and Operator Contract](requirements/mvp-and-operator-contract.md) freezes the first NAS-oriented exact profile and its acceptance tests.
3. [System Architecture](requirements/system-architecture.md) defines runtime components, data flow, and authority boundaries.
4. [Core Kernel and Interface Requirements](requirements/core-kernel-and-interface.md) defines durable core ownership and compatibility policy.
5. [Driver and Processor Interfaces](requirements/driver-and-processor-interfaces.md) defines replaceable capture, processing, storage, indexing, and related extension behavior.
6. [File Identification and Extraction](requirements/file-identification-and-extraction.md) defines the extension, magic-byte, structural, and optional learned evidence ladder.
7. [Namespace and Content Access](technical/namespace-and-content-access.md) defines how physically transformed or deduplicated data maps back to a file-shaped view.
8. [Security and Threat Model](requirements/security-and-threat-model.md) defines the active filesystem, parser, repository, and interface security gates for the MVP and later profiles.
9. [Release Qualification and Traceability](requirements/release-qualification-and-traceability.md) defines the compatibility tuple and evidence required before a profile ships.
10. [NAS Vertical Slice Implementation Plan](technical/nas-vertical-slice-implementation-plan.md) translates the product and Processor contracts into a concrete coding sequence.
11. [Implementation Completion Plan](technical/implementation-completion-plan.md) records remaining repository and later-profile gates. RestoreWeave does not own a FUSE server.
12. [Experience Completion Plan](technical/experience-completion-plan.md) is the long path for OpenSubsonic/OPDS facades, the Inbox shell, and live-client qualification. D1–D4 are in tree; D5 live clients and a release repository are not. It does not add requirements.
12a. [Remaining Work and Closed Decisions](technical/remaining-work-and-closed-decisions.md) is the short list of what is left and which decisions must not be reopened (no RestoreWeave FUSE, no engine selection by slogan).
13. [Whole-Architecture Open-Source Reference](technical/architecture-open-source-reference.md) records how each layer should consume open source so a local slice does not block a later replacement.
14. [Ecosystem and Vertical Applications](requirements/ecosystem-and-vertical-apps.md) is the single normative requirement for one data plane and bounded domain experiences. [Universal Catalog and Experience Pack Overview](requirements/universal-catalog-and-experience-packs.md) is a non-normative orientation summary.
15. [Ecosystem App Interface](technical/ecosystem-app-interface.md) defines the concrete domain-record, segment, collection, rendition, streaming, manifest, and app-grant contracts used by later clients.

## MVP-defining requirements

- [Product Requirements](requirements/product-requirements.md)
- [MVP and Operator Contract](requirements/mvp-and-operator-contract.md)
- [CLI and MCP Contract](requirements/cli-and-mcp-contract.md)
- [System Architecture](requirements/system-architecture.md)
- [Core Kernel and Interface Requirements](requirements/core-kernel-and-interface.md)
- [Security and Threat Model](requirements/security-and-threat-model.md)
- [Release Qualification and Traceability](requirements/release-qualification-and-traceability.md)

The MVP reference profile is self-hosted and NAS-oriented. It accepts local or mounted filesystem roots under a declared capture-consistency class. A Linux host or container is the reference deployment shape, but no specific distribution, NAS brand, or filesystem is a product-wide requirement. Optional platform capture profiles qualify independently.

The target `RW-MVP-1` release includes deterministic suffix and magic-byte identification, exact hashing and duplicate accounting, one release-qualified repository engine, common metadata and text extraction, durable tag and note CRUD with portable export, a portable snapshot namespace, bundled read-only Linux FUSE access, baseline lexical search, verification, and restore. Unknown or failed content processing falls back to exact preservation. None of these end-to-end capabilities is shipped yet; see [Product Completeness Review](references/product-completeness-review.md).

## Current status at a glance

| Area | State |
| --- | --- |
| Product definition and normative requirements | Documented and internally reviewed |
| Filesystem scanner, namespace records, and read service | Foundations implemented and tested |
| Exact repository, plan/apply executor, publication, and clean restore | Not implemented; repository engine qualification pending |
| Processor host and default extraction pack | In-process host plus UTF-8 text EXTRACT; Linux sandbox and Tika/ffprobe pack remain |
| SQLite FTS5 search generations | Decision frozen for the MVP; provider not implemented |
| Linux read-only FUSE, CLI, and read-only MCP | Target interfaces; not implemented |
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

The baseline discovery product is a bundled lexical index over path, filename, type, metadata, checksum, duplicates, durable tags and notes, and available extracted text. User tags and notes are versioned durable records with export; they are not disposable index documents. Embedding, CLIP, vector retrieval, multimodal ranking, collections, ratings, and relationship graphs are staged extensions. Their derived indexes must remain rebuildable, and their staging must not be interpreted as removing intelligent search from the product thesis.

## What is deliberately outside `RW-MVP-1`

Writable NAS protocols, automatic source deletion or pruning, P2P and magnets, mandatory LLM/embedding/CLIP/OCR/ASR services, neural or perceptual replacement, database/VM/application-consistent capture, HA, multitenancy, enterprise governance, REST/WebUI, remote workers, mutation-capable MCP, and A2A are future profiles rather than hidden MVP obligations.

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

P2P and magnet retrieval remain non-core. Perceptual media substitution, neural compression, VAE or RWKV-style codecs, automatic source deletion, writable NAS behavior, alternate namespace gateways, and enterprise control-plane features require separately qualified profiles. The bundled MVP Linux FUSE adapter is read-only presentation over the core `SnapshotTree` and `FileAccess` contracts, not another storage or plugin interface family.

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
3. Core Kernel and Interface Requirements for authority and compatibility boundaries.
4. Topic-specific normative requirements.
5. Extended profiles and research documents.

Platform examples never create global product requirements. An operating-system or filesystem-specific statement applies only to its explicitly named qualification profile.
