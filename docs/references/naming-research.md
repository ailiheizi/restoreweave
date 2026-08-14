# Naming Research

## 1. Current recommendation

Use **RestoreWeave** as the working product name while validating the NAS-first product with operators.

Recommended descriptor:

> Self-hosted content-aware storage and discovery for NAS and heterogeneous data.

Recommended promise:

> Store fewer redundant bytes, find content intelligently, and restore the original filesystem with proof.

Optional short tagline:

> Store less. Find more. Restore with proof.

The name may remain, but product language should lead with storage reduction, intelligent discovery, replaceable processing, and recoverable namespace access. It should not present RestoreWeave as a Mac or APFS backup tool.

## 2. Current product meaning

RestoreWeave is a cross-platform self-hosted storage layer with a stable authoritative core and replaceable algorithms. It combines:

- Exact content identity, deduplication, and lossless compression.
- Original-directory browsing and restore over packed or distributed storage.
- File identification using suffix and magic-byte or structural evidence.
- Versioned detection, extraction, fingerprinting, transformation, validation, placement, indexing, ranking, and retrieval stages.
- Baseline metadata and text search.
- Optional external AI, CLIP-like encoders, embeddings, perceptual fingerprints, and semantic rankers.
- Portable provenance, verification, placement, and recovery records.

The product is NAS-first but not NAS-only. Platform-specific capture mechanisms such as APFS, ZFS, Btrfs, LVM, VSS, or appliance snapshots are profiles beneath the product identity.

## 3. Name rationale

**Restore** communicates the durable trust promise: the system retains enough verified information to reconstruct the selected filesystem view and chosen fidelity, even when physical storage is compressed, deduplicated, packed, tiered, or distributed.

**Weave** communicates the graph connecting:

- Source observations and namespace entries.
- Exact bytes and shared chunks.
- Raw, compressed, derived, and later approximate representations.
- Parsers, models, decoders, dictionaries, and validation dependencies.
- Metadata, annotations, extracted information, and search generations.
- Storage placements and recovery proofs.

The name does not bind the product to one operating system, filesystem, model, codec, storage engine, UI, protocol, or modality.

## 4. Naming risks and mitigation

The largest risk is that **Restore** can make the product sound like backup software only, while the intended product also reduces active storage and improves discovery. **Weave** is flexible but abstract and needs a clear category descriptor.

Mitigation:

- Always pair the name with the NAS-first storage and discovery descriptor during early validation.
- Lead product pages with stored-byte reduction, intelligent search, original-directory access, and extensibility.
- Describe recovery as the safety contract, not the only workflow.
- Avoid Mac, APFS, or single-repository imagery in primary positioning.
- Demonstrate browsing and search as prominently as ingest and restore.
- Use stable terms such as content, namespace, representation, placement, and index rather than backup-only vocabulary where the broader meaning is intended.

A rename should be evidence-driven. It becomes worth reconsidering if repeated operator tests show that the current name materially hides the storage and discovery value, causes persistent backup-only categorization, or conflicts with trademark and distribution requirements.

## 5. Considered alternatives

| Name | Strength | Reason not selected |
| --- | --- | --- |
| Reprovenance | Strong reproduction-plus-provenance concept | An exact GitHub repository name was found in the historical screen, and the coined term needs explanation |
| RestoreSet | Direct connection to the minimum recoverable information set | Descriptive and less distinctive; still recovery-heavy |
| RestorePlane | Communicates a stable control plane | Infrastructure-heavy and less expressive of storage and discovery |
| RestoreLattice | Good graph and dependency metaphor | Lattice is common in technology branding and still leads with restore |
| ReproLoom | Strong reproducibility and weaving metaphor | Loom creates an obvious existing-product association |
| ProofKeep | Communicates retained evidence and storage | Historical screen found related ProofKeeper names; weaker discovery meaning |
| FidelityMesh | Expresses multiple fidelity and placement paths | Strong unrelated financial-brand association and an abstract storage meaning |

These alternatives were evaluated under earlier recovery-oriented framing. They are preserved as historical naming evidence, not as a complete NAS-storage naming search. No new collision or trademark search was performed for this update.

## 6. Historical collision screen

The following engineering-level checks were performed on 2026-08-10:

- Siftline GitHub repository search for RestoreWeave: no exact repository result.
- Siftline Hacker News search for RestoreWeave and “Restore Weave”: no result.
- GitHub user lookup for `restoreweave`: not found.
- npm package lookup for `restoreweave`: not found.
- PyPI JSON endpoint for `restoreweave`: not found.
- DNS lookup for `restoreweave.com` and `restoreweave.dev`: no returned record during the check.

Crates.io could not be reliably checked because its API rejected the request under its data-access policy.

These results are historical and may now be stale. They are not legal clearance and do not reserve a name, account, package, or domain.

## 7. Product-direction re-evaluations

### 7.1 Historical thin-kernel re-evaluation

On 2026-08-11, the historical direction recorded in [Thin-Core Product Research Audit](thin-kernel-product-research.md) narrowed the product from an AI-operable control plane to an APFS-to-Restic recovery kernel plus an opinionated reference distribution.

The name was retained because it described recovery meaning and representation lineage. That authority-boundary lesson remains useful. The Mac/APFS wedge, single-engine default, and category **content-aware recovery kernel for minimal, verifiable backup** are superseded.

### 7.2 Current NAS-first re-evaluation

The current architecture makes RestoreWeave a self-hosted content-aware storage and discovery layer. The name remains workable because:

- Recovery remains the trust anchor behind storage reduction and representation replacement.
- Weave describes shared chunks, multiple representations, semantic derivatives, placements, indexes, and provenance.
- The name does not encode macOS, APFS, Restic, AI, CLIP, vector search, or one compression method.
- External models and indexers can change without forcing a product rename.
- The stable core still provides a coherent identity beneath replaceable algorithms.

The name is less perfect as a direct description of daily discovery and active storage management. The descriptor and product demonstrations must compensate for that ambiguity while operator research determines whether a stronger storage-first name is necessary.

## 8. AI and extension naming boundary

RestoreWeave should not be branded as an AI runtime. Learned file detection, embeddings, CLIP-like search, OCR, ASR, media understanding, and neural representations are replaceable extensions behind stable subject and stage contracts.

Likewise, broad plugin support does not make RestoreWeave a general capability platform. Its fixed domain remains content storage, namespace access, processing provenance, discovery, verification, placement, and recovery. A general-purpose agent or capability platform should remain a separate product.

## 9. Legal and launch boundary

This is an engineering-level working-name recommendation, not trademark clearance.

Before public commercial use:

- Search relevant national and international trademark databases.
- Check company-name registries in target markets.
- Confirm domain registration rather than relying on DNS results.
- Check source-hosting accounts, package registries, container registries, application stores, and social handles again.
- Review confusingly similar names across NAS, storage, backup, data management, media, search, and AI categories.
- Evaluate pronunciation, translation, and confusion risk in target languages.

The documentation and local project folder may continue to use RestoreWeave as the working name until those checks and operator-positioning tests justify a change.
