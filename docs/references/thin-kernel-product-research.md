# Thin-Core Product Research Audit

> **Historical decision note (superseded 2026-08-19):** This audit preserves the evidence and reasoning available when it was written. Its bundled-FUSE and embedding-optional recommendations are not current product requirements. [Content Store, Views, and Export Requirements](../requirements/content-store-views-and-exports.md) now requires explicit export materialization with no RestoreWeave mount API, plus a bundled local ONNX/BGE and in-process zvec semantic profile for the default experience.

## 1. Conclusion

RestoreWeave should be delivered as a small authoritative core plus an opinionated self-hosted NAS distribution. It should not be delivered as a Mac backup utility, a generic storage SDK, an embedded AI agent, or a marketplace of empty interfaces.

The product statement is:

> RestoreWeave is a self-hosted, NAS-first content-aware managed data layer that converts heterogeneous file trees into verified, space-efficient representations, preserves a directly browsable and restorable filesystem namespace, and lets operators upgrade processing, repository, and discovery implementations without changing durable data meaning.

The product promise is:

> **Store less. Find more. Restore with proof.**

The research verdict is a **conditional go**:

- The underlying NAS storage, discovery, and recovery pain is credible.
- No reviewed project combines heterogeneous storage minimization, recoverable original paths, intelligent discovery, and replaceable algorithms under one durable identity model.
- Exact managed-archive feasibility is high because mature engines and processors already exist.
- Adjacent categories support medium plausibility for the unified product, but direct demand and willingness to pay are unverified.
- Empty-framework, representation-stranding, and operating-complexity risks are high.

The first product must therefore prove one complete loop with strong defaults. Interfaces alone are not product power.

## 2. Research status and evidence labels

This audit was reframed on 2026-08-12 from sources already inspected through the repository, Siftline, canonical repositories, and primary documentation. No new external request was made for this rewrite.

- **Observed:** inspected in the local RestoreWeave repository.
- **Documented:** stated by a previously reviewed primary project source.
- **Inferred:** a RestoreWeave product or architecture conclusion.
- **Unverified:** current behavior, performance, licensing, demand, or willingness to pay still requires a fresh test.

The former Mac/APFS-first conclusion is superseded. APFS is one optional capture profile. Its entitlement, packaging, and compatibility work may block that profile, but it must not block the Linux/NAS product.

## 3. Corrected product fingerprint

The durable product fingerprint is:

```text
heterogeneous source tree
-> incremental inventory and durable subject identity
   |-> mandatory exact hash -> verified `RepositoryDriver` placement
   |-> suffix -> magic -> optional learned and parser evidence
       -> content-class `Processor` routing -> derivatives and candidates
-> authenticated original-path namespace
-> baseline content and metadata index
-> search, browse, read, migrate, verify, and restore
```

The operator-facing value loop is:

```text
attach -> understand -> store -> find -> open -> verify -> restore -> improve
```

This differs from the previous planner-first backup loop in two important ways:

1. Search and file access provide recurring daily value after ingest.
2. Representation and processor upgrades are a lifecycle, not a one-time backup decision.

Planning remains necessary because storage treatment can change cost and recoverability. It is no longer the only product wedge.

## 4. Product category and modes

“Intelligent data fabric” is a useful architecture description but a weak product category. “Self-hosted NAS managed archive and search system” is more concrete for the first profile; “content-aware managed data layer” describes the broader architecture without claiming writable-primary-NAS behavior now.

RestoreWeave should expose three explicit modes:

### 4.1 Observe mode

- Scan an existing local or mounted tree without changing source storage.
- Build path, metadata, type, checksum, duplicate, and extracted-text discovery.
- Produce storage and processing recommendations.
- Make no net-storage-saving claim while the original tree remains unchanged.

This is the lowest-risk adoption wedge.

### 4.2 Managed archive mode

- Ingest selected collections into verified exact storage.
- Publish a read-only filesystem-shaped namespace over the managed representations.
- Search, stream, browse, and restore without the original source.
- Report raw, unique, stored, index, model, and dependency bytes separately.

This is the correct ruthless MVP because it can prove storage economics, discovery, and recovery together.

### 4.3 Primary writable NAS mode

- Accept active writes through SMB, NFS, WebDAV, FUSE, or another filesystem interface.
- Coordinate locking, concurrency, snapshots, application consistency, quotas, permissions, and failure recovery.

This is a substantially larger product and remains later. A read-only managed archive must not be marketed as an active NAS replacement.

## 5. Primary evidence and architecture lessons

| Primary source retained by the prior audit | Documented mechanism | RestoreWeave implication |
| --- | --- | --- |
| [Linux stable userspace API guidance](https://docs.kernel.org/process/stable-api-nonsense.html) | Linux distinguishes stable user-facing interfaces from changeable internal implementation APIs. | Stabilize records, commands, and read semantics; do not freeze internal Go packages, SQL tables, worker topology, or plugin supervisors. |
| [OCI Runtime Specification](https://github.com/opencontainers/runtime-spec) | Bundles, lifecycle, configuration, and runtime behavior allow several implementations to interoperate. | Versioned specifications can create an ecosystem, but each public contract becomes long-lived maintenance work. |
| [libfuse](https://github.com/libfuse/libfuse) | A narrow request/response boundary lets userspace implement file access. | One namespace and content-read contract can support CLI, restore, read-only mounts, and later gateways. |
| [Model Context Protocol](https://github.com/modelcontextprotocol/modelcontextprotocol) | External model-driven clients can call bounded tools and read resources through a published protocol. | RestoreWeave needs CLI and MCP operations, not an embedded agent runtime. |
| [Restic](https://github.com/restic/restic), [Kopia](https://github.com/kopia/kopia), and [Borg](https://github.com/borgbackup/borg) | Mature encrypted, deduplicated, compressed repository engines already provide snapshots, checks, browse or mount behavior, and restore. | Reuse one engine in the first distribution and benchmark it for interactive archive access. Do not make one engine the product identity. |
| [borgmatic](https://github.com/borgmatic-collective/borgmatic), [resticprofile](https://github.com/creativeprojects/resticprofile), and [Backrest](https://github.com/garethgeorge/backrest) | Opinionated configuration, scheduling, checks, onboarding, UI, browse, and restore can create product value above a mature engine. | A thin core still requires complete first-run, recurring, verification, and recovery experiences. |
| [Immich](https://github.com/immich-app/immich), [PhotoPrism](https://github.com/photoprism/photoprism), and [Paperless-ngx](https://github.com/paperless-ngx/paperless-ngx) | Strong defaults, asynchronous processors, metadata, OCR, thumbnails, tags, and semantic search create recurring catalog value. | Search and enrichment may be rebuildable, but a useful reference implementation is required in the product. |
| [Nextcloud](https://github.com/nextcloud/server), [Nextcloud Full Text Search](https://github.com/nextcloud/fulltextsearch), and [sist2](https://github.com/sist2app/sist2) | General file namespaces can be indexed through parser, provider, and search-backend layers. | Preserve stable subject and authorization bindings while keeping physical index engines replaceable. |
| [Perkeep](https://github.com/perkeep/perkeep), [git-annex](https://git-annex.branchable.com/how_it_works/), and [DataLad](https://github.com/datalad/datalad) | Immutable content can be separated from logical names, locations, claims, and provenance. | Namespace identity, content identity, placement, and derived metadata should remain distinct core records. |
| [file/libmagic](https://github.com/file/file), [Magika](https://github.com/google/magika), and [Apache Tika](https://tika.apache.org/3.2.3/detection.html) | Deterministic signatures, learned byte classification, structural inspection, metadata, and text extraction provide complementary evidence. | Preserve conflicts and route processing through policy; no detector may decide recoverability. |
| [Apache NiFi](https://github.com/apache/nifi) | Replaceable processors, queues, provenance, backpressure, and revision-aware control are implementable. | Borrow processor discipline but avoid becoming a general workflow platform. |

The complete component landscape is recorded in [Competitor and Component Research](competitor-research.md). Mechanism relevance is not direct demand evidence.

## 6. Primary users and jobs

### 6.1 NAS and homelab operator

The operator wants to:

- Reduce physical storage for cold and duplicate collections.
- Search across documents, media, code, models, archives, games, and applications.
- Preserve the familiar directory tree.
- Understand which processor or representation produced an outcome.
- Upgrade components without losing earlier data.
- Restore exact files when the catalog, model service, or operational database is unavailable.

### 6.2 Data-heavy small team

The team wants to:

- Share one heterogeneous catalog without importing every file into a vertical application.
- Route data to local, remote, and later cold placements.
- Preserve user-created source material separately from generated or reacquirable content.
- Audit actual storage, index freshness, decoder availability, and recovery evidence.

### 6.3 Integration author

The author wants a bounded way to add a detector, extractor, codec, validator, indexer, storage engine, or gateway without learning private database schemas or receiving ambient filesystem authority.

## 7. Why users would adopt it

RestoreWeave should layer onto an existing NAS before asking to replace it.

Ordinary NAS systems provide files, permissions, and storage pools. Backup engines provide efficient exact snapshots. Vertical catalogs provide excellent experiences for one content class. Search stacks provide connectors, parsers, indexes, and ranking. Workflow frameworks provide replaceable processors.

The proposed advantage is the combined loop:

- One heterogeneous namespace rather than one library per modality.
- Actual physical-byte accounting rather than only logical file size.
- Search results that resolve to immutable file versions and original paths.
- Processing outputs with versioned provenance and rebuild lineage.
- Exact browse and restore independent of semantic indexes.
- Storage and processor implementations that may change without changing durable meaning.

The product loses if the operator still has to assemble and maintain Restic or Kopia, Tika, ffprobe, a vector database, a catalog, and custom glue. The default distribution must remove that integration burden.

## 8. Authoritative core and replaceable userland

### 8.1 Core-owned invariants

The core owns only semantics that need one trusted arbiter:

- Source, capture, content, file-version, namespace, snapshot, representation, placement, processor-generation, and index-generation identities.
- Fidelity and recovery contracts.
- Policy decisions, human authority, and exact fallback.
- Immutable plans and their input revisions.
- Incremental lifecycle, deletion truth, migration state, and garbage-collection reachability.
- Operation journal, idempotency, fencing, cancellation, and reconciliation.
- Provenance and complete representation dependency closure.
- Verification acceptance and portable publication truth.
- Authorization and result binding for namespace, file access, and search subjects.

These are not plugins.

### 8.2 Replaceable algorithmic userland

Replaceable implementations map onto five active seams plus one later seam:

- `CaptureDriver` implementations for live paths, mounted trees, and filesystem snapshots.
- One capability-oriented `Processor` protocol for learned classification, parsing, extraction, enrichment, fingerprinting, transformation, validation, and index preparation.
- `RepositoryDriver` implementations for exact placement, reads, verification, reconciliation, and restore.
- `IndexProvider` implementations for named rebuildable generations.
- `QueryProvider` implementations for generation-pinned retrieval and ranking.
- Later `RetrieverDriver` implementations for qualified external reacquisition.

CLI, MCP, REST, WebUI, FUSE, and other namespace gateways are northbound clients or presentation adapters over core contracts, not storage-algorithm plugin families. The public seams must be proven by independent implementations, but they are the approved architecture boundary rather than optional future categories.

### 8.3 First-party reference distribution

Replaceable implementations do not make defaults optional. The supported distribution must ship:

- A Linux- and container-friendly controller.
- One local or mounted filesystem capture profile with an explicit consistency claim.
- Suffix and magic-byte detection.
- Independent exact hashing and duplicate accounting.
- One mature exact repository engine.
- Bounded common document and media metadata extraction.
- Durable whole-subject tags and plain-text notes with portable export/import.
- Generation-pinned path, metadata, type, checksum, duplicate, tag, note, and extracted-text search.
- An authenticated read-only namespace with bounded content access.
- A bundled read-only Linux FUSE projection over the same namespace and content access contracts.
- CLI and an initial local read-only MCP subset over one typed operation model.
- Portable records sufficient to recover the namespace without the live catalog or search index.

Optional WebUI, REST, alternate gateway, GPU semantic pack, and platform-specific capture drivers may use the same contracts.

## 9. Explicit challenge to over-pluginization

“Replaceable” does not mean transparent hot swap.

| Component | Safe replacement behavior |
| --- | --- |
| Detector or classifier | Rerun against immutable inputs and create a new evidence generation. |
| Extractor or embedding provider | Rebuild derivatives and indexes; retain generation and provenance until cutover. |
| Query ranker | Replace after evaluation while preserving stable result-to-subject binding. |
| Exact or lossy codec | Keep the old decoder and dependency closure until every representation is migrated and independently verified. |
| Chunker or repository engine | Perform an explicit storage migration with dual readability and reconciled placement state. |
| Validator | Bind evidence to the representation, profile, corpus, and threshold it actually evaluated. |
| Capture driver | Qualify per source type and consistency claim; failure does not silently select a weaker claim. |

A public ABI for every micro-stage would freeze premature abstractions and multiply compatibility, security, packaging, and documentation work. The system should stabilize the smallest external seams exercised by the default distribution.

The default UX should use profiles, checkboxes, and policy presets. A node-graph editor may be useful to experts later, but it is not product simplification.

## 10. Search and AI boundary

AI has two valid roles:

1. An ordinary processor that classifies, extracts, fingerprints, embeds, transforms, or validates bounded content.
2. An external harness that calls typed CLI or MCP operations.

RestoreWeave does not need an embedded prompt loop, agent memory, model marketplace, or A2A runtime.

Search has a different product status from AI:

- Search indexes, embeddings, captions, and summaries are rebuildable derivatives.
- Exact recovery must continue when every index is deleted.
- The first useful distribution must nevertheless ship baseline search, because discovery is part of the product promise.
- The core binds every result to an immutable subject, optional segment, authorization decision, index generation, and provenance record.
- Embeddings and CLIP remain later replaceable providers; metadata, duplicate, and extracted-text search form the baseline.

“Not required for disaster recovery” must not be confused with “not required for product completeness.”

## 11. Health and recovery semantics

The existing portable publication and verification machinery remains valuable, including distinct payload placement, prepared recovery closure, publication commit, readback evidence, and exact restore results.

For the NAS product, health should be multidimensional:

- Storage integrity.
- Namespace availability.
- Representation decodability and dependency availability.
- Index freshness and extraction coverage.
- Capture consistency.
- Placement durability and failure-domain count.
- Recovery-reference and clean-restore readiness.

A clean-machine drill may gate a strong recovery-readiness claim. It should not make a healthy local catalog or readable managed archive globally “failed” merely because the operator has not completed a ceremonial drill. Product status should name the exact weak dimension.

## 12. Ruthless MVP

The first release should be a **single-node Linux/NAS managed archive**.

### 12.1 Included

- One local or mounted source root with an honest capture-consistency claim.
- Full inventory plus repeatable reconciliation; watcher events remain hints.
- Suffix and magic-byte identification.
- SHA-256 plus length for the qualified exact content identity; additional hashes may be derived.
- Exact raw fallback and one mature deduplicated/compressed repository route.
- Common bounded metadata and text extraction for a deliberately small format set.
- Logical, unique, stored, index, model, and dependency byte accounting.
- Durable whole-subject tags and plain-text notes with portable export/import.
- Generation-pinned path, filename, metadata, type, checksum, duplicate-group, tag, note, and extracted-text search.
- Authenticated original-path namespace.
- Browse, bundled read-only Linux FUSE, bounded reads, streaming, verification, and exact restore.
- One out-of-process processor path proving capability isolation and versioned reprocessing.
- CLI and an initial bounded local read-only MCP subset over the same operation semantics.
- Portable recovery records that rebuild the namespace after loss of the operational database and every search index.

### 12.2 Explicitly excluded

- APFS as a global release dependency.
- Embeddings, CLIP, OCR, ASR, or an LLM as mandatory components.
- Perceptual, generative, neural, VAE, or RWKV representations.
- Automatic omission or source deletion.
- External reacquisition, magnets, P2P, or public sharing.
- Writable SMB, NFS, WebDAV, or S3 primary-storage behavior.
- Multitenancy, high availability, enterprise RBAC, legal hold, or compliance claims.
- A public plugin marketplace or arbitrary visual workflow graph.

### 12.3 MVP proof

The MVP passes only when it demonstrates all of the following on a representative heterogeneous corpus:

1. Every managed file restores byte-for-byte through its original path.
2. The managed physical footprint is reported honestly and is lower than raw logical input on the target corpus after index and dependency overhead.
3. The report separates engine-provided savings from any RestoreWeave-specific additional savings.
4. Content and duplicate search materially outperform filename-only search on a fixed task set.
5. Deleting the catalog and search index does not prevent namespace reconstruction and exact restore.
6. Upgrading an extractor or indexer rebuilds a new generation without rewriting payload bytes or changing durable subject identity.
7. Installation, update, normal operation, and recovery remain simpler than the focused-tool stack the product intends to replace.

## 13. Adoption wedge

The lowest-friction adoption sequence is:

```text
connect existing NAS read-only
-> receive inventory, duplicate, type, and search value
-> select one cold collection
-> preview actual managed footprint
-> ingest and verify exact bytes
-> browse and search through RestoreWeave
-> restore onto an empty target
-> only later consider broader migration
```

The product should not ask for immediate source deletion or primary-filesystem authority. Trust is earned through verified reads and restores.

The first-run report should make five facts visible:

- Logical source bytes.
- Unique content bytes.
- Actual managed repository bytes.
- Catalog, index, model, and decoder overhead.
- Which capabilities remain unavailable, stale, or unverified.

## 14. Decision-changing experiments

### 14.1 Storage economics

Run the same representative corpora through raw storage, the selected repository engine directly, and RestoreWeave. Include all metadata, indexes, temporary amplification, model weights, dictionaries, and decoder dependencies.

Failure condition: RestoreWeave claims unique storage savings that are actually supplied entirely by the underlying engine, or its derivative overhead erases the benefit.

### 14.2 Discovery value

Compare filename-only search, baseline path/metadata/full-text search, and optional embeddings on a fixed set of filename-blind tasks.

Failure condition: baseline discovery does not change successful retrieval, or semantic processing costs more than the value it adds.

### 14.3 Namespace and clean recovery

Delete the operational database and every rebuildable index. Starting from portable records and repository access, browse the tree, read random ranges, restore selected directories, and verify exact digests.

Failure condition: namespace meaning or exact access depends on a live search service, plugin registry, or private database row.

### 14.4 Processor and repository migration

Upgrade an extractor and indexer, then migrate a small repository cohort between two storage implementations.

Failure condition: an algorithm change silently rewrites historical meaning, or old data becomes unreadable before migration verification completes.

### 14.5 Operational burden

Run the product for at least 30 days on self-hosted hardware. Measure failed jobs, manual interventions, update friction, index rebuild time, memory, disk amplification, and operator time.

Failure condition: RestoreWeave adds more ongoing labor than the tools it replaces.

### 14.6 Security and hostile inputs

Exercise malformed containers, archives, decompression bombs, symlink races, parser crashes, oversized outputs, hostile metadata, prompt injection, stale plugins, and revoked capabilities.

Failure condition: a processor obtains ambient authority or a content-derived instruction changes policy, storage, deletion, or authorization.

### 14.7 Demand and willingness to pay

Run concierge or paid pilots with NAS operators and data-heavy small teams. Measure repeat search, managed data migrated, restores, retained use, and explicit budget or hardware commitment.

Failure condition: users like the idea but do not migrate data, return to search, perform restores, or accept ongoing operation.

## 15. Strongest counterevidence and reversal condition

The strongest argument against RestoreWeave is that focused tools may already be better:

- Restic, Kopia, or Borg may supply nearly all safe storage reduction.
- Immich, PhotoPrism, Paperless-ngx, Nextcloud, and sist2 may provide better specialized discovery.
- Existing NAS filesystems already provide the active writable namespace.
- A new controller, catalog, processor runtime, and compatibility surface may increase operational risk.
- Users may not trust any representation that is not the exact original.
- A plugin ecosystem may strand data through missing models, codecs, dictionaries, or runtimes.
- Technical-community interest may not translate into payment or migration.

The smallest reversal observation is:

> Representative operators achieve little net managed-storage benefit, rarely use unified discovery, or find RestoreWeave harder to run than their existing focused applications plus an exact repository engine.

If that occurs, RestoreWeave should narrow to a portable manifest, processor contract, namespace bridge, and recovery-verification layer rather than claim a new NAS storage category.

## 16. Platform-specific capture boundary

[Apple Time Machine](https://support.apple.com/guide/mac-help/back-up-files-mh35860/mac), [Apple restore guidance](https://support.apple.com/guide/mac-help/restore-files-mh11422/mac), and [Arq](https://www.arqbackup.com/) already provide mature Mac backup baselines. [Arq's documented format](https://www.arqbackup.com/documentation/arq7/English.lproj/dataFormat.html), [restore workflow](https://www.arqbackup.com/documentation/arq7/English.lproj/restore.html), and [open-source restore utility](https://github.com/arqbackup/arq_restore) further weaken a portability-only Mac differentiation claim.

These references show why an operating-system-specific backup wrapper is not a sufficient product wedge. An APFS driver may still be valuable for one source profile, but its entitlement, privilege, retained-snapshot, cleanup, packaging, signing, and compatibility work is isolated to that `CaptureDriver` qualification. The same rule applies to every platform-specific capture implementation.

## 17. Research coverage and limitations

The retained research used GitHub, Hacker News, canonical repositories, primary documentation, standards, RFCs, and public community discussions. The shared Siftline ledger ultimately recorded **14 provider calls** because concurrent agents used the same query identifier. The count reflects execution history, not ecosystem coverage.

Previously unavailable providers included Exa, Tavily, and an OpenAI-compatible Web provider. No new provider request was made for this rewrite.

This audit did not include:

- Customer interviews or paid pilots.
- Independent product, performance, or recovery benchmarks.
- Proprietary NAS platforms and private communities.
- A complete license audit of every binary, model weight, or transitive dependency.
- Longitudinal measurements of index drift, plugin abandonment, or repository migration.

Project documentation is treated as documented capability, not independent proof. Current feature, performance, and licensing claims marked elsewhere in the research must be reverified before adoption.
