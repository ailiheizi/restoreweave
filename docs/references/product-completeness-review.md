# Product Completeness Review

> **Review status:** This review was reframed on 2026-08-12 around the corrected self-hosted NAS product. It distinguishes implemented foundations, documented design, inferred product value, and unverified market claims. A large requirements archive is not treated as a complete product.

## 1. Conclusion

RestoreWeave is not currently a complete storage, catalog, backup, or NAS product. It is a substantial design archive plus early Go foundations for filesystem observation, extension capability modeling, namespace access, representation records, and a rebuildable SQLite projection.

The corrected product is:

> A self-hosted, NAS-first content-aware managed data layer whose first profile minimizes exact archive bytes, preserves a recoverable filesystem namespace, provides useful heterogeneous discovery, and supports versioned replaceable implementations.

The product verdict remains a **conditional go**. The architecture contains several valuable recovery and identity mechanisms, but the central product loop is not implemented and direct demand remains unproven.

Completeness now means one operator can:

```text
attach a NAS tree
-> inventory and identify it
-> preview and apply exact storage treatment
-> publish the original namespace
-> search and read managed content
-> verify and restore it
-> rebuild derived indexes
-> upgrade a processor without changing durable meaning
```

Mac/APFS qualification is not a completeness gate. It is one optional capture-profile gate.

## 2. Evidence status

### 2.1 Observed in the repository

- A deterministic filesystem scanner with streaming SHA-256, raw-name preservation, hard-link evidence, mutation detection, explicit incomplete states, and final-component no-follow opens. Ancestor and mount substitution remain unqualified because traversal is not yet rooted in retained directory handles.
- An experimental plugin manifest, capability, evidence, access, and runtime foundation.
- Host-owned read-service types for immutable snapshot trees, namespace lookup, bounded file access, and representation selection.
- SQLite migrations and records for namespace, representations, and operational projections.
- Extensive English requirements and research covering recovery, processing, storage, search, security, applications, games, databases, virtual machines, cold media, and later P2P concepts.
- No command package, controller daemon, complete CLI, MCP server, repository writer, storage-engine adapter, search implementation, WebUI, FUSE gateway, or end-to-end managed-archive flow was found in the current Go tree.

### 2.2 Documented but not implemented end to end

- Durable plans, operation journal, fencing, cancellation, retries, and reconciliation.
- Portable Recovery Record Format publication using payload, prepared closure, and publication-commit evidence.
- A complete capture-driver lifecycle.
- Restic, Kopia, Borg, or another repository integration.
- Incremental managed ingest and deletion reconciliation.
- Processor isolation through an external process protocol.
- Common document and media extraction.
- Metadata, duplicate, full-text, vector, or multimodal search.
- Processor-generation rebuild and index cutover.
- Representation migration and dependency-closure enforcement.
- Clean recovery after loss of the operational database.
- A supported Linux/container reference distribution.

### 2.3 Inferred and unverified

- Representative NAS users will migrate meaningful data into a managed archive.
- The complete system produces net savings after catalog, index, temporary, model, dictionary, and decoder overhead.
- Unified discovery is used often enough to justify another self-hosted service.
- Replaceable processors reduce lock-in more than they increase compatibility and operating cost.
- A mature backup engine provides adequate random-read and mounted-archive performance.
- Users or organizations will pay for the product.

## 3. Corrected product fingerprint

The former product center was a minimum-safe Mac backup plan. The corrected center is a durable heterogeneous data and representation graph with three visible outcomes:

1. **Store less:** exact hashing, duplicate accounting, mature repository compression and deduplication, and later class-specific representations.
2. **Find more:** path, metadata, type, checksum, duplicate, and extracted-text discovery by default; optional semantic providers later.
3. **Restore with proof:** original-path browsing, bounded reads, portable records, independent verification, and exact restore.

The core is not an AI runtime. AI may be an ordinary processor or an external CLI/MCP client.

The core is also not merely a backup wrapper. Search and read access are product features even though their indexes remain rebuildable and non-authoritative.

## 4. Product modes

| Mode | Product value | Storage claim | Current state |
| --- | --- | --- | --- |
| **Observe** | Inventory, duplicates, extraction, and search over an existing tree. | No net source-volume saving. | Scanner foundations exist; default extraction and search do not. |
| **Managed archive** | Verified ingest, efficient exact storage, read-only namespace, search, and restore. | Managed footprint may be lower than raw input; whole-system saving depends on source release. | Target ruthless MVP; not implemented end to end. |
| **Primary writable NAS** | Active file service over managed representations. | Potential primary-storage saving. | Explicitly later; requires concurrency, protocol, application-consistency, and writable-gateway work. |

The first release should complete managed archive mode. Observe mode may provide the adoption wedge. Primary writable mode must not be implied by read-only namespace support.

## 5. Coverage matrix

| Domain | Normative or design home | Current evidence | Completeness gap |
| --- | --- | --- | --- |
| Product boundary | [Product Requirements](../requirements/product-requirements.md), [Thin-Core Product Research](thin-kernel-product-research.md) | Corrected NAS-first direction is documented in active rework. | Authoritative documents must remain consistent and avoid old Mac-first release gates. |
| Inventory and source truth | [File Identification and Extraction](../requirements/file-identification-and-extraction.md) | Scanner implementation and tests exist. | Reference Linux/NAS capture lifecycle, incremental reconciliation, scale qualification, and source-profile matrix are incomplete. |
| Type detection and processing | [Driver and Processor Interfaces](../requirements/driver-and-processor-interfaces.md) | A legacy non-normative `internal/plugin` prototype plus built-in detector tests exist. Its broad category and transformation models predate the current five-seam architecture. | Replace the prototype before the out-of-process Processor milestone; implement the route union, sealed artifact state machine, historical decode operation, sandbox, quotas, common processor pack, conformance suite, and upgrade lifecycle. |
| Content and representation identity | [Core Kernel and Interface](../requirements/core-kernel-and-interface.md), [System Architecture](../requirements/system-architecture.md) | SQLite representation types and records exist. | Durable portable schemas, codec dependency closure, migration, and garbage-collection reachability need end-to-end proof. |
| Exact storage and placement | [System Architecture](../requirements/system-architecture.md), [Core Protocol and Reference Userland](../technical/core-protocol-and-reference-userland.md) | Repository-independent concepts and some opaque engine references exist. | No working repository adapter, placement write, readback, reconciliation, or benchmark exists. |
| Namespace and content access | [Namespace and Content Access](../technical/namespace-and-content-access.md) | `SnapshotTree` and `FileAccess` foundations plus SQLite namespace code and tests exist. | No repository-backed range reader, complete browse/cat/restore path, portable rebuild, or read-only gateway exists. |
| Publication and verification | [Recovery Fidelity](../requirements/recovery-fidelity.md), [Restore Manifest](../requirements/restore-manifest.md) | Detailed protocol design exists. | Payload, prepared closure, publication commit, signed verification, and clean recovery are not implemented end to end. |
| Baseline discovery | [External AI and Semantic Extensions](../requirements/external-ai-and-semantic-extensions.md), [File Identification and Extraction](../requirements/file-identification-and-extraction.md) | Experimental plugin categories mention search behavior. | No default metadata, duplicate, extracted-text, index-generation, query, ACL, or result-binding implementation exists. |
| Semantic and multimodal extensions | [External AI and Semantic Extensions](../requirements/external-ai-and-semantic-extensions.md) | Detailed provenance and safety design exists. | Embedding, CLIP, OCR, ASR, caption, vector, and hybrid-search providers remain later and unqualified. |
| CLI and MCP | [CLI and MCP Contract](../requirements/cli-and-mcp-contract.md) | Detailed typed-operation design exists. | No complete CLI or MCP implementation demonstrates adapter equivalence. |
| Lifecycle and operations | [Operations and Lifecycle](../requirements/operations-and-lifecycle.md) | Broad future requirements exist. | Installation, upgrades, scheduling, monitoring, repair, processor migration, repository migration, and 30-day operation remain unproven. |
| Security | [Security and Threat Model](../requirements/security-and-threat-model.md) | Extensive threat analysis exists. | Processor sandboxing, supply-chain trust, secret brokering, hostile-file tests, and least-privilege deployment need implementation evidence. |
| Application, game, database, and VM profiles | [Application and Game Collections](../requirements/application-and-game-collections.md), [Database and Virtual Machine Capture](../requirements/database-and-virtual-machine-capture.md) | Future profile designs exist. | All remain outside the ruthless MVP and require separate consistency qualification. |
| P2P, magnets, and cold media | [Peer-to-Peer and Magnet](../requirements/p2p-and-magnet.md), [Cold Media and Offline Custody](../requirements/cold-media-and-offline-custody.md) | Detailed future research exists. | No direct core demand; exclude from the MVP. |

## 6. Valuable assets to preserve

The Mac-first product direction was wrong, but several mechanisms remain valuable across platforms.

### 6.1 Durable identity separation

- A path is not content identity.
- Content identity is not representation identity.
- Representation identity is not repository-private location.
- A search document or vector is not namespace truth.

This separation is central to safe processor and storage replacement.

### 6.2 `SnapshotTree` and `FileAccess`

The narrow read contract is the right foundation for:

- CLI browse and cat.
- Exact restore.
- Range reads and media streaming.
- Read-only FUSE, WebDAV, or other gateways.
- Search results that resolve back to immutable subjects.

### 6.3 Portable publication and verification

The distinction among payload placement, prepared recovery closure, signed publication commit, sampled or full readback, and exact restore prevents repository success from being mistaken for recovery success.

The protocol should be generalized across capture and repository drivers, not discarded.

### 6.4 Exact fallback and human authority

Unknown, conflicting, unsupported, encrypted, or processor-failed readable files remain exactly protected. A classifier, embedding, fingerprint, external source, or AI proposal cannot authorize omission or source release.

### 6.5 Capability isolation

Processors should receive bounded content and staging handles, quotas, deadlines, and explicit network or secret grants. They should not receive ambient filesystem, repository, database, or deletion authority.

## 7. Core, extension, and distribution completeness

### 7.1 Core completeness

The core is complete only when it can preserve and enforce:

- Durable identities and namespace mappings.
- Recovery and fidelity contracts.
- Policy decisions and immutable plans.
- Incremental lifecycle, migration state, and reachability.
- Operation truth and publication.
- Provenance and decoder dependency closure.
- Verification acceptance.
- Namespace, content-read, and search-result authorization.

### 7.2 Extension completeness

The extension story is complete only when:

- One external processor works without importing private Go packages.
- Crash, timeout, cancellation, malformed output, quota, and version mismatch are tested.
- A processor upgrade produces a new generation rather than silently rewriting old results.
- An authoritative transform cannot be removed until every dependent representation is migrated and verified.
- A second implementation demonstrates that the protocol is not shaped around one executable.

### 7.3 Distribution completeness

The product is complete only when a supported distribution ships strong defaults:

- Linux/container installation and upgrade.
- One local or mounted capture profile.
- Suffix and magic detection.
- One exact repository engine.
- Common bounded metadata and text extraction.
- Durable whole-subject tags and plain-text notes with portable export/import.
- Generation-pinned baseline path, metadata, duplicate, annotation, and extracted-text search.
- Browse, bundled read-only Linux FUSE, read, verify, and restore.
- CLI and an initial local read-only MCP subset.
- Portable namespace recovery.

A collection of interfaces without these defaults is an SDK, not the requested product.

## 8. Ruthless MVP completeness gate

The first release is a single-node Linux/NAS managed archive with:

- One source root and one exact repository target.
- Explicit source-consistency reporting.
- Full inventory and repeatable reconciliation.
- Extension and magic-byte evidence.
- Independent exact hashing and duplicate accounting.
- Exact raw fallback and mature repository compression/deduplication.
- Bounded metadata and text extraction for a small published format matrix.
- Durable whole-subject tags and plain-text notes with portable export/import.
- Generation-pinned path, metadata, type, checksum, duplicate, tag, note, and extracted-text search.
- Authenticated original-path browse, bundled read-only Linux FUSE, range read, stream, verify, and restore.
- One out-of-process processor implementation.
- CLI and an initial bounded local read-only MCP subset.
- Portable namespace reconstruction without SQLite or a search index.

It deliberately excludes:

- Mandatory embeddings, CLIP, OCR, ASR, or LLM services.
- Lossy, generative, neural, VAE, or RWKV representations.
- Automatic source deletion or omission.
- P2P, magnets, and public sharing.
- Writable NAS protocols.
- Multitenancy, high availability, enterprise governance, and compliance claims.
- APFS as a product-wide dependency.
- A public plugin marketplace.

The MVP is complete only when all six proofs pass:

1. Every managed file restores exactly through its original path.
2. Physical managed bytes are reported with all index and dependency overhead and are lower than raw logical input on the qualification corpus.
3. Direct-engine savings are separated from RestoreWeave-specific savings.
4. Baseline content discovery materially beats filename-only search on a fixed task set.
5. Loss of the operational database and every rebuildable index does not prevent namespace reconstruction and restore.
6. One extractor or indexer upgrade rebuilds safely without payload rewrite or durable-identity change.

## 9. Product-strength review

| Dimension | Assessment |
| --- | --- |
| Underlying NAS pain | Medium-high |
| Exact managed-archive feasibility | High |
| Heterogeneous search differentiation | Medium-high if a strong default ships |
| Differentiation from backup engines | Medium-high only when search, namespace, and processor lifecycle are integrated |
| Direct unified-product demand | Unverified; adjacent-category evidence supports medium plausibility only |
| Willingness to pay | Unverified |
| Representation migration difficulty | High |
| Empty-framework risk | High |
| Self-hosted operating-complexity risk | High |
| Writable all-in-one NAS feasibility in the near term | Low |

The product has stronger long-term power than the former Mac backup wedge, but only if it remains operationally simpler than a stack of focused tools.

## 10. Strongest counterevidence

- Restic, Kopia, or Borg may already provide nearly all safe byte reduction.
- Immich, PhotoPrism, Paperless-ngx, Nextcloud, and sist2 may remain better discovery products for their domains.
- Search and model indexes can erase storage savings and consume scarce CPU, RAM, or GPU capacity.
- Backup-oriented repositories may be too slow for interactive archive access.
- Users may prefer the original active filesystem and refuse managed migration.
- A broad processor ecosystem can strand data through unavailable codecs, models, dictionaries, or runtimes.
- Another daemon, database, catalog, queue, and update surface may increase operational work.
- Detailed recovery ceremony can overwhelm the daily storage and search job.
- Technical early adopters may not represent a paying market.

If these dominate in representative pilots, RestoreWeave should narrow to portable manifests, namespace mapping, processor contracts, and recovery verification rather than claim a complete intelligent NAS layer.

## 11. Deferred profiles

### 11.1 Platform-specific capture

Snapshot and application-consistency drivers may be valuable for individual source platforms. Their entitlement, privilege, packaging, and compatibility work gates only the named `CaptureDriver` profile and never defines the RestoreWeave product category.

### 11.2 P2P and magnets

P2P may later supply authorized retrieval or encrypted placement. It does not improve the ruthless MVP's central proof and has no retained direct demand evidence. It remains outside Core and outside the first release.

### 11.3 Perceptual and neural representations

Media fingerprints, VAE representations, RWKV-style compression, and learned codecs remain processor research. They require pinned decoders, migration, claim-specific validation, explicit policy, and exact fallback. They are not prerequisites for useful storage and discovery.

### 11.4 Enterprise and primary NAS behavior

Multitenancy, legal hold, enterprise RBAC, high availability, remote workers, writable protocols, and application-consistent databases or VMs each require separate product and qualification evidence.

## 12. Next decision gates

1. **Reference pipeline:** complete mandatory exact lane, suffix-to-magic classification branch, one processor pack, one repository engine, durable annotations, generation-pinned baseline search, bundled read-only FUSE, namespace access, and exact restore.
2. **Storage benchmark:** compare raw, direct-engine, and RestoreWeave footprints with every index and dependency included.
3. **Discovery benchmark:** prove content search changes successful retrieval beyond filename search.
4. **Portable recovery:** rebuild namespace and restore after deleting SQLite and search indexes.
5. **Processor lifecycle:** upgrade and cut over one extractor or indexer generation without payload rewrite.
6. **Operational pilot:** run for 30 days and measure manual interventions, resource use, rebuild time, update friction, and failed jobs.
7. **Demand test:** obtain paid pilots or explicit data-migration and operating commitments from target NAS users.

Adding more future-profile requirements before these gates pass would increase documentation completeness while reducing product completeness.

## 13. Research basis and limitations

The product decision is supported by:

- [Thin-Core Product Research](thin-kernel-product-research.md)
- [Competitor and Component Research](competitor-research.md)
- [Demand Research](demand-research.md)
- [Multimodal Fidelity Research](multimodal-fidelity-research.md)
- [Neural Compression Research](neural-compression-research.md)

The historical review run recorded **14 provider calls** under its shared query identifier. The later open-source adoption audit used query ID `restoreweave-oss-adoption-20260812` and recorded **68 attempts**, **63 provider calls**, **63 provider successes**, and **5 cache hits**; a focused Processor audit recorded **20 attempts**, **12 provider calls**, **12 successes**, and **8 cache hits**. These counts describe bounded research execution, not ecosystem coverage.

The review does not contain customer interviews, paid-pilot evidence, independent repository benchmarks, a complete license audit, or long-term operational data. Project documentation is treated as documented capability, not independent qualification. Unified-product plausibility is medium, while direct demand and willingness to pay remain unverified.
