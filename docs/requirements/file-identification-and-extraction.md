# File Identification and Extraction Requirements

## 1. Purpose

RestoreWeave must understand heterogeneous data well enough to choose useful storage, extraction, validation, and discovery algorithms without making any one detector or media stack part of the trusted core.

The required exact and classification paths are:

~~~text
filesystem observation
  |-> mandatory host exact hash -> exact placement eligibility -> required readback
  |-> suffix evidence -> magic-byte evidence
      -> optional CLASSIFY_LEARNED evidence
      -> host provisional classification -> bounded PARSE refinement
      -> final typed classification -> host-owned ProcessingRoute
      -> selected EXTRACT / ENRICH / FINGERPRINT / TRANSFORM / VALIDATE / INDEX_PREPARE capabilities
~~~

This path applies to ordinary files, directories, links, archives, documents, source trees, applications, games, images, audio, video, models, datasets, databases, disk images, and future namespaced content classes.

The classification branch runs beside the unconditional host-owned exact lane. Classification and `ProcessingRoute` completion never determine exact-ingest eligibility. The classification result is evidence for routing and does not by itself authorize data omission, lossy replacement, source deletion, or a recovery claim.

## 2. Core invariants

1. Filename suffixes, magic bytes, structural parsers, context, and learned models remain separate evidence sources.
2. Later evidence never erases earlier disagreement.
3. Unknown and conflicting content remains storable, browsable, restorable, and searchable by generic metadata.
4. Raw exact preservation is the universal fallback for readable bytes.
5. Processing is selected by a typed host route, not by executing a file or letting a plugin choose arbitrary next steps.
6. A content class is an open namespaced value, not a fixed enum compiled into the core.
7. Original namespace identity and recoverable directory structure do not depend on successful classification or extraction.
8. Derived artifacts retain their source revision, coverage, producer, configuration, and dependency provenance.
9. Deep processing may run asynchronously after durable ingest; slow enrichment MUST NOT make the basic NAS path unusable.
10. Search projections may be rebuilt or replaced without changing stored content identity.

## 3. Distinct type dimensions

RestoreWeave MUST NOT collapse every meaning into one MIME string. A **ClassificationRecord** may contain several independent dimensions:

| Dimension | Examples |
| --- | --- |
| Filesystem object kind | regular file, directory, symlink, hard-link member, sparse file, device, socket |
| Physical format | ZIP, PNG, Matroska, ELF, PDF, SQLite, safetensors |
| Encoding or container | UTF-8, gzip, TAR, MP4 container, protobuf |
| Content class | text, image, audio, video, document, archive, application, game, model, dataset |
| Structural role | executable, library, manifest, save data, source asset, cache, thumbnail, model weights |
| Collection role | album track, application bundle member, game mod, source-tree member, dataset shard |
| Risk property | encrypted, signed, executable, macro-bearing, untrusted archive, possible secret |

Values use versioned namespaces. Examples include an IANA media type, a format registry identifier, or an extension-owned value such as **org.restoreweave.game-save/v1**.

A subject MAY have multiple content classes and roles. For example, a game package may be an archive, application asset collection, and executable distribution. The core does not need game-specific control flow; processors declare which typed combinations they accept.

## 4. Observation and identity

Classification runs against an immutable or explicitly versioned **SubjectRef**. The observation binds:

- Source and capture identity.
- Namespace-entry identity and raw path components.
- Filesystem object facts.
- Logical and allocated size.
- Stable file-version or content identity when available.
- Prefix, suffix, range, or complete-read handles granted to detectors.
- Observation time and change-detection result for live views.

If a live source changes during inspection, evidence from different versions MUST NOT be merged. The host retries against a stable version or records an explicit changing-content state.

The same bytes at two paths may share content identity while retaining distinct namespace entries, metadata, ACLs, tags, and collection roles.

## 5. DetectionEvidenceRecord

Every detection stage emits an immutable **DetectionEvidenceRecord** containing:

- SubjectRef and source revision.
- Evidence kind: filesystem, suffix, declared type, magic, parser, context, or learned.
- Detector capability ID, implementation version, profile digest, rule-set or model digest, and configuration digest.
- Candidate format IDs, content classes, roles, and version ranges.
- Evidence-specific confidence or match strength.
- Exact filesystem facts or byte ranges examined.
- Required ranges that were unavailable.
- Match rule, signature identifier, parser branch, or model-output digest.
- Determinism class and runtime conditions.
- Coverage, errors, truncation, resource limits, and unsupported features.
- Creation time and canonical evidence digest.

Confidence values from different detectors MUST NOT be averaged or compared unless a versioned calibration profile explicitly defines that operation.

Evidence records are append-only. A detector upgrade creates a new evidence generation rather than rewriting an earlier conclusion.

## 6. Stage one: suffix evidence

Suffix detection is cheap and always runs before content reads unless policy excludes even filename inspection.

The suffix detector MUST:

- Preserve the original raw name and case.
- Support multiple-part suffixes such as **.tar.zst**.
- Distinguish a suffix from a complete filename convention such as **Dockerfile**.
- Record directory and collection context separately.
- Treat the suffix as a hint, never proof.
- Report unknown, conflicting, missing, and suspicious double-suffix states.
- Avoid interpreting path text as a command, URL, or executable expression.

Suffix evidence is useful for route narrowing and bounded magic reads. It MUST NOT cause a parser to skip validation or cause an executable to run.

## 7. Stage two: magic-byte evidence

Magic detection uses one or more pinned rule sets or deterministic detectors. It normally has stronger physical-format value than a suffix, but disagreement remains explicit.

The magic detector MUST:

- Declare every examined range, including non-prefix signatures.
- Use bounded reads and hard input limits.
- Pin the rule database, implementation, and configuration.
- Report multiple matches and match strength.
- Distinguish container identification from payload classification.
- Record missing bytes, truncation, encryption, and unsupported variants.
- Never mount, extract, deserialize with executable hooks, or run the subject.

Magic evidence alone may be insufficient for polyglots, appended payloads, self-extracting archives, malformed structures, encrypted containers, and formats with weak or shared signatures. Such content proceeds with explicit ambiguity or conflict.

## 8. Stage three: optional learned classification

A learned classifier is an ordinary **CLASSIFY_LEARNED** Processor. It is optional and is normally invoked when:

- Suffix and magic evidence disagree.
- Both stages return unknown or overly broad candidates.
- Policy requests richer semantic or role classification.
- A specialized collection profile needs a probabilistic hint.

The host invokes learned classification only through an immutable **IdentificationRouteRef** built from the current subject revision plus suffix, magic-byte, and path-context evidence. This restricted route may contain only `CLASSIFY_LEARNED` and classification-refining `PARSE` nodes. It cannot depend on a final `ClassificationRecord` or ordinary `ProcessingRoute`, because those records do not yet exist.

The classifier MUST declare:

- Model and weights digest.
- Preprocessor, tokenizer, feature extractor, and configuration digests.
- Input ranges and maximum bytes.
- Candidate label namespace and version.
- Calibration corpus, threshold, confidence semantics, and known out-of-domain behavior.
- Runtime, device, precision, quantization, and determinism class.

Learned evidence MUST NOT silently override a successful structural parse or erase deterministic evidence. A low-confidence or unavailable classifier yields no learned conclusion and follows the ordinary fallback path.

An LLM may implement learned classification through the same Processor contract, but the core never sends an open-ended prompt that grants policy or workflow authority.

## 9. Classification resolution

The host creates a versioned **ClassificationRecord** after the configured evidence stages. The first record may be provisional. It contains:

- All input evidence digests.
- Candidate physical formats and selected typed views.
- Selected content classes and structural or collection roles.
- State and conflict reasons.
- Resolver implementation, rule-set, and policy revision.
- Confidence semantics and unresolved alternatives.
- Required follow-up parser capabilities.
- Safe generic fallback route.
- Canonical classification digest.

The classification state is one of:

- **IDENTIFIED**
- **AMBIGUOUS**
- **POLYGLOT**
- **CONFLICTING_EVIDENCE**
- **PARTIALLY_PARSED**
- **ENCRYPTED_OR_LOCKED**
- **UNSUPPORTED**
- **MALFORMED**
- **UNKNOWN**

The resolver uses evidence-specific rules rather than one universal score. A strict parser can establish that a structural view is valid, but it does not prove that all bytes belong only to that view. Trailing, prepended, overlapped, concatenated, and otherwise unclaimed ranges remain explicit.

For ambiguous or polyglot data, the host MAY route several safe parser views in parallel. It MUST preserve the full byte stream regardless of which view is most useful.

The host builds or revises the same restricted **IdentificationRouteRef** as identification progresses. Before a provisional record exists, it may invoke `CLASSIFY_LEARNED` against the suffix, magic-byte, and context evidence. After the host publishes a provisional record, the route may invoke only matching bounded `PARSE` capabilities to refine it. The route cannot extract arbitrary derivatives, transform bytes, place data, index content, or broaden itself. Learned or structural results create host-evaluated evidence and may lead to a superseding ClassificationRecord; they cannot erase earlier evidence or directly schedule another processor.

The general `ProcessingRouteRef` begins from the final or explicitly accepted unresolved ClassificationRecord. A processor invocation binds exactly one route kind: `IdentificationRouteRef` for learned or classification-refining work, or `ProcessingRouteRef` for the post-classification pipeline. A later deep parser may still enumerate virtual members or richer structure, but it does not create a routing cycle: any consequential reclassification creates a new immutable classification generation and successor ProcessingRoute.

## 10. ProcessingRoute

A **ProcessingRoute** is a typed, immutable plan built by the host from a final or explicitly accepted unresolved classification generation:

- ClassificationRecord.
- Operator storage and discovery policy.
- Required recovery relation.
- Available Processor and IndexProvider capability profiles.
- Privacy, license, accelerator, time, cost, and energy limits.
- Repository capabilities.
- Explicit fallback order.

Each route node declares:

- Processor stage and exact capability-profile digest.
- Required input schema and evidence predicates.
- Produced output schemas.
- Maximum coverage and resource budgets.
- Whether failure is optional, degradable, or blocks only the named processing branch or stronger profile claim; no route node may block the mandatory exact lane for readable bytes.
- Fallback capability or raw exact fallback.
- Dependencies on earlier node outputs.

The route MUST reject an untyped edge. For example, an embedding result cannot be passed to an exact-byte validator unless that validator explicitly accepts the embedding schema as non-authoritative evidence.

The host MAY defer expensive route branches. Durable ingest can complete first, followed by background extraction, fingerprinting, embedding, or reindexing.

## 11. Parsing

A **PARSE** Processor validates one declared structural view and emits a versioned parse result. Its CapabilityProfile declares:

- Supported formats, versions, and feature subsets.
- Required sequential, seek, or random access.
- Encryption and credential behavior.
- Strict, lenient, or repair mode.
- Size, depth, member, page, frame, sample, symbol, and temporary-space limits.
- Known blind spots and unsupported extensions.
- Emitted component and virtual-member schemas.

Strict, lenient, and repair results are distinct views. A repaired or lenient view is candidate evidence and MUST NOT be presented as proof that the original structure was valid.

### 11.1 Parse coverage

Every parser reports separate coverage dimensions:

- Bytes examined, claimed, overlapping, and unclaimed.
- Members discovered and members opened.
- Pages, frames, streams, tracks, layers, records, symbols, or tensors enumerated.
- Metadata fields attempted and extracted.
- Content regions attempted and extracted.
- Encrypted, malformed, unsupported, skipped, truncated, and resource-blocked regions.

Complete coverage may be claimed only when the denominator is known for the declared unit. Detection coverage, structural coverage, extraction coverage, and semantic-enrichment coverage remain separate.

## 12. Extraction and enrichment

An **EXTRACT** Processor emits typed, rebuildable artifacts such as:

- Plain or structured text.
- Document metadata.
- OCR text and layout.
- Audio transcript and speaker or time segmentation.
- Image, audio, and video technical metadata.
- Captions, thumbnails, waveforms, keyframes, and previews.
- Symbols, package manifests, dependency declarations, and source metadata.
- Game manifests, mod metadata, save descriptors, and asset inventories.
- Model architecture, tensor metadata, tokenizer descriptors, and license information.
- Archive and dataset member inventories.

An **ENRICH** Processor may add externally sourced titles, descriptions, identifiers, relationships, or tags. External facts retain source, retrieval time, license, confidence, and subject-binding provenance.

Every extraction or enrichment result records:

- Source SubjectRef, revision, parser view, and selected ranges or components.
- Output schema, media type, encoding, language, segmentation, and normalization.
- Output digest, size, lifecycle class, and sensitivity label.
- Coverage and truncation.
- Producer, implementation, configuration, model, runtime, and dependency digests.
- ACL inheritance and purge lineage.

Machine-derived output never overwrites user-authored tags, notes, corrections, or collection decisions.

## 13. Fingerprints and embeddings

Fingerprints and embeddings are Processor outputs used for discovery, grouping, duplicate candidates, and qualified perceptual validation. The reference distribution enables its pinned local text-embedding profile by default; other spaces remain optional.

A **FingerprintRecord** binds:

- Algorithm, implementation, version, and configuration.
- Input components and analyzed ranges.
- Preprocessing, alignment, resampling, and normalization.
- Fingerprint bytes, digest, and encoding.
- Comparator, distance metric, threshold, and calibration.
- Known invariances, collision risks, and unsupported inputs.

An **EmbeddingRecord** additionally binds:

- Exact model weights and model-card reference.
- Feature extractor, tokenizer, pooling, dimensions, dtype, quantization, and normalization.
- Runtime, device, precision, and numerical mode.
- Vector digest and provider-ready encoding.
- Compatible index-space identity.

Embeddings from different model, preprocessing, dimensions, quantization, or distance generations are not directly comparable unless a qualified bridge exists.

Acoustic fingerprints, perceptual hashes, CLIP vectors, text embeddings, and similar outputs are discovery evidence by default. They do not prove original-byte identity, edition identity, ownership, or sufficient recovery quality.

## 14. Virtual members and nested content

Archives, packages, disk images, documents, application bundles, games, model containers, and datasets may expose virtual members. A host-owned logical member identity is derived, when the format permits stable reconciliation, from:

- Parent content identity.
- Raw member name, ordinal, offset, or structural identity.

A separate parser-view observation identity binds the logical member candidate to the parser capability-profile digest, parser-view schema, inspected ranges, and parse generation. Replacing a parser therefore creates a new observation without automatically changing a reconciled logical member identity. When two parser views cannot be reconciled safely, the host creates distinct member subjects with explicit proposed equivalence or lineage instead of promising cross-parser stability.

A virtual-member record includes raw and safe display names, logical and stored sizes, offsets, integrity fields, claimed byte ranges, overlap relationships, encryption state, and parse coverage.

The host enforces cumulative limits across the entire nested job:

- Recursion depth.
- Containers and members.
- Logical expanded bytes and expansion ratio.
- Temporary storage, files, handles, CPU, memory, and time.
- Pages, frames, samples, tensors, symbols, thumbnails, and derived artifacts.
- Duplicate paths and normalization collisions.

Virtual members cannot escape their parent namespace. Automatic installation, execution, dynamic linking, macro execution, or untrusted mounting is prohibited.

## 15. Application, game, and model safety

Applications and games are collections, not merely executable files. A collection processor MAY relate binaries, libraries, assets, configuration, saves, mods, manifests, dependencies, and external acquisition identifiers while preserving every physical namespace entry.

Collection processing receives a host-issued `CollectionViewHandle` bound to one immutable inventory generation and an explicit bounded member set. It provides typed relationships, ACL scope, total entry and byte ceilings, pagination, and host-mediated content reads. It never grants a processor ambient access to a directory root. A processor may propose identities and membership, but the host validates and publishes authoritative collection records.

Classification and inspection MUST NOT:

- Launch an application or game.
- Run installer, package, mod, or shader scripts.
- Import untrusted Python or model code.
- Use unsafe native deserialization.
- Mount a disk image outside a constrained parser profile.
- Resolve or download dependencies without a separate authorized retrieval operation.

Model processors SHOULD prefer formats with bounded metadata inspection, such as safely parsed tensor headers. A model file that requires code execution for inspection remains exact raw data with limited metadata.

## 16. Filesystem-object requirements

### 16.1 Symlinks

- Preserve raw link-target bytes and platform decoding.
- Do not follow links during ordinary inventory unless a bounded traversal policy explicitly permits it.
- Treat a followed target as a separate observation.

### 16.2 Hard links

- Record filesystem identity, link count, and all observed names.
- Reuse stable content reads where safe while preserving namespace topology.
- Never infer hard-link identity from equal content alone.

### 16.3 Sparse files

- Record logical size, allocated size, and extent information when available.
- Define whether a digest covers the logical zero-filled stream, extent map, or both.
- Avoid accidental full allocation during processing and restore.

### 16.4 Special objects

Devices, sockets, FIFOs, reparse points, and other special objects use platform-specific metadata schemas and policy. They are never read as ordinary byte streams accidentally.

## 17. Representation routing

Classification may select compression, transformation, or validation candidates, but recovery policy selects the required relation:

- Exact bytes.
- Exact filesystem semantics.
- Normalized content.
- Perceptual equivalence.
- Functional equivalence.
- Reacquisition.
- Discovery only.

An exact transform must pass independent decode-and-hash validation. A perceptual or functional transform must retain its validator profile, thresholds, source reference, and rollback path. A discovery artifact cannot become the only recoverable representation.

If no qualified processor satisfies the selected relation, the route uses raw exact fallback. A classification or processor failure MUST NOT make a readable file disappear or become unrestorable.

## 18. Reclassification and incremental work

Classification, parsing, extraction, and semantic artifacts are generation-based.

When a suffix map, magic rule set, parser, extractor, model, or resolver changes:

1. The host records a new capability or rule-profile digest.
2. Existing evidence and artifacts remain immutable.
3. A reclassification job targets only content whose result may change.
4. Changed ClassificationRecords mark dependent routes and derivatives stale.
5. Reprocessing creates new artifacts beside old artifacts.
6. Reindexing builds a new provider generation and activates it only after validation.

Content digests and dependency lineage SHOULD allow unchanged files to reuse compatible deterministic results. Path-specific roles, ACLs, and user annotations remain distinct even when content-level extraction is shared.

## 19. Reference profile

The initial self-hosted reference profile MUST provide:

- Platform-neutral filesystem facts.
- Suffix evidence.
- Pinned magic-byte evidence.
- Explicit unknown, ambiguous, conflicting, encrypted, malformed, and unsupported states.
- Raw exact fallback for every readable subject.
- Safe extraction of common text and metadata through qualified processors.
- Generic path, name, type, metadata, tag, and available extracted-text indexing.
- Background queues for deeper optional processing.

The reference profile SHOULD provide curated processor packs for common archives, documents, images, audio, video, source code, applications, games, and model containers. The pinned local text embedding pack is part of the reference discovery default. Learned classification, OCR, ASR, fingerprints, other embedding spaces, CLIP, external enrichment, neural codecs, and perceptual substitution remain optional capabilities that can be installed without changing the core.

## 20. Acceptance criteria

1. Suffix evidence is collected before magic-byte evidence, and optional learned evidence runs only through an explicit route.
2. A renamed executable with an image suffix retains both the suffix and magic conflict and is never executed.
3. A file with no recognized suffix or magic still receives namespace identity, raw exact storage, generic metadata indexing, and a visible unknown classification.
4. A polyglot or appended-payload fixture preserves all bytes and every valid or unresolved structural view.
5. Text, image, audio, video, game, application, archive, model, dataset, and vendor-specific processors route through the same typed interface.
6. Nested-container limits apply cumulatively and terminate safely with explicit partial coverage.
7. A virtual member cannot escape its parent or collide silently after name normalization.
8. Complete coverage is reported only with a known denominator.
9. Learned classification, captions, fingerprints, and embeddings cannot authorize exact success, omission, or source deletion.
10. A parser or extractor upgrade creates a new generation and marks only dependent artifacts and index records stale.
11. A device, precision, model, or preprocessing change never silently reuses an incompatible embedding or threshold.
12. Optional processing failure degrades enrichment or search but never prevents safe raw ingest of readable data.
