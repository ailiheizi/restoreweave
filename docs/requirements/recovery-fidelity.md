# Recovery Fidelity Requirements

> **Applicability:** The NAS-first managed-archive MVP publishes only exact source-byte recovery and preserves readable unknown or processor-failed content through exact fallback in one qualified mature repository. It also ships baseline path, metadata, checksum, duplicate, media-metadata, and extracted-text search whose results are discovery evidence rather than recovery claims. Normalized, perceptual, functional, semantic-substitution, and source-reacquisition outcomes remain future opt-in profiles, retained here so their validators and safety boundaries do not need to be reinvented later.

## 1. Purpose

RestoreWeave supports recovery outcomes that are not always byte-identical. This document defines how exact, source, normalized-content, perceptual, functional, semantic, and discovery-only relations are represented without conflation.

The central rule is:

> A substitute is acceptable only for a declared subject, component, purpose, validator profile, and policy. “Close enough” is never a universal property of a file.

When no qualified alternate claim or required validator is available, readable source content follows the generic exact route. Detection, extraction, search, embedding, or validation failure cannot silently lower fidelity, omit the original, or block exact fallback.

## 2. Claim model

A recovery claim is:

~~~text
subject + relation + protected components + validator profile + policy authority
~~~

Required subjects:

- **ORIGINAL_BYTES:** bytes observed at inventory time.
- **PINNED_SOURCE_ARTIFACT:** a provider artifact identified by immutable version, edition, variant, and digest.
- **NORMALIZED_CONTENT:** decoded or canonicalized content after a pinned deterministic process.
- **PERCEPTUAL_REPRESENTATION:** features and structures intended to approximate human perception.
- **FUNCTION_CONTRACT:** explicitly enumerated behaviors and tests.
- **SEMANTIC_INVARIANTS:** declared meaning-bearing fields, records, relations, and constraints.

Required relations:

- **EXACT**
- **WITHIN_TOLERANCE**
- **PASS**
- **MATCH_CANDIDATE**

## 3. Recovery claims

### 3.1 ORIGINAL_BIT_EXACT

Subject: ORIGINAL_BYTES<br>
Relation: EXACT

Requirements:

- Original length matches.
- SHA-256 matches.
- No numeric tolerance or human override can convert a mismatch into an exact pass.

Filesystem metadata and application capture consistency are reported on separate orthogonal axes. `ORIGINAL_BIT_EXACT` proves the observed file-content bytes only; a complete restore profile may additionally require `FILESYSTEM_NATIVE_EXACT` and an application consistency level.

### 3.2 SOURCE_EQUIVALENT

Subject: PINNED_SOURCE_ARTIFACT<br>
Relation: EXACT

Requirements:

- Provider and immutable object or release ID.
- Edition, locale, platform, architecture, region, and variant.
- Expected source artifact digest or signed catalog digest.
- Resolver and source metadata version.
- Recorded relationship to the original observation.

The same version label with a changed digest is source drift. A source-equivalent object is original-bit-exact only when it also matches the original digest.

### 3.3 NORMALIZED_CONTENT_EQUIVALENT

Subject: NORMALIZED_CONTENT<br>
Relation: EXACT

Examples:

- Identical decoded integer PCM after declared delay and alignment handling.
- Identical oriented pixels in a declared colorspace.
- Identical subtitle text and timing after a declared canonicalization.
- Equivalent logical records after a deterministic application export.

The normalization process, ignored fields, decoder, and component scope must be pinned. Any tolerance-based pass belongs to a perceptual, functional, or semantic contract rather than `NORMALIZED_CONTENT_EQUIVALENT`.

### 3.4 PERCEPTUALLY_EQUIVALENT

Subject: PERCEPTUAL_REPRESENTATION<br>
Relation: WITHIN_TOLERANCE

Requirements:

- Explicit user or organization opt-in.
- Structural hard gates.
- More than one complementary metric where practical.
- Pass, review, and fail bands.
- Target-corpus calibration.
- Segment or region worst-case reporting.
- Human review for identity-sensitive or uncalibrated candidates.

This claim never authenticates a source and never implies byte or normalized-content identity.

### 3.5 FUNCTIONALLY_EQUIVALENT

Subject: FUNCTION_CONTRACT<br>
Relation: PASS

Requirements:

- Pinned test harness and environment.
- Locked inputs and fixtures.
- Required test identifiers.
- Explicit quantitative tolerances.
- Repeat rules for nondeterministic systems.
- Zero permitted failures among required tests.

Undeclared behavior is not protected by this claim.

### 3.6 SEMANTICALLY_EQUIVALENT

Subject: SEMANTIC_INVARIANTS<br>
Relation: PASS or WITHIN_TOLERANCE

Requirements:

- Pinned parser, schema, canonicalizer, and invariant set.
- Declared ignored fields, ordering, units, locale, encoding, and numeric or time tolerances.
- Deterministic validation for required facts and relations where possible.
- Independent approval of any LLM-proposed invariant set.

Embedding similarity, a summary, or an LLM judgment alone cannot issue this claim.

### 3.7 DISCOVERY_MATCH

Subject: any searchable representation<br>
Relation: MATCH_CANDIDATE

`RW-MVP-1` uses this relation for subject-bound baseline catalog results over path, metadata, checksums, duplicate groups, qualified media metadata, and extracted text. Later providers may add CLIP similarity, audio embeddings, perceptual hashes, acoustic fingerprints, vector ranking, and semantic search scores through the same subject and index-generation model.

This claim may rank candidates for review. It is not a recovery success state.

## 4. User-facing presets

### 4.1 Archive Original

Use for:

- User-created, captured, edited, or private content.
- Creative masters and project files.
- Databases, archives, disk images, and compound datasets.
- Signed, legal, medical, financial, forensic, or provenance-sensitive objects.
- Applications, games, saves, mods, configuration, and licensed artifacts.
- Unknown or unsupported data.

Default claim: ORIGINAL_BIT_EXACT. Unknown, unsupported, conflicting, or processor-failed readable content uses `EXACT_FALLBACK` while retaining this exact recovery relation.

### 4.2 Preserve Decoded Content

Use when container bytes may change but declared decoded components must remain exact.

Examples:

- PCM audio with exact tags retained separately.
- Raster pixels with selected metadata retained separately.
- A database exported to a tested canonical logical format.

Default claim: NORMALIZED_CONTENT_EQUIVALENT.

### 4.3 Consumption Copy

Use only for explicitly selected replaceable media where the user accepts bounded perceptual differences.

Default claim: PERCEPTUALLY_EQUIVALENT.

The original must remain protected during a validation and grace period.

### 4.4 Works for This Purpose

Use for rebuildable outputs, generated caches, and selected application artifacts whose required behavior is completely represented by tests.

Default claim: FUNCTIONALLY_EQUIVALENT.

### 4.5 Pinned Source

Use when an immutable provider artifact is independently reacquirable and tested.

Default claim: SOURCE_EQUIVALENT or ORIGINAL_BIT_EXACT when hashes match.

### 4.6 Discovery Only

Use for searchable and preview representations. This profile never permits original omission.

## 5. Component-specific protection

A container-level result is insufficient. A contract must declare required components.

### 5.1 Audio example

| Component | Required claim |
| --- | --- |
| Audio recording | PERCEPTUALLY_EQUIVALENT |
| Channel layout | EXACT |
| Duration and alignment | WITHIN_TOLERANCE |
| Tags | ORIGINAL_BIT_EXACT or deterministic field equality |
| Cover art | ORIGINAL_BIT_EXACT |
| Lyrics and chapters | ORIGINAL_BIT_EXACT |
| Source provenance | SOURCE_EQUIVALENT |

### 5.2 Video example

| Component | Required claim |
| --- | --- |
| Picture | PERCEPTUALLY_EQUIVALENT |
| Audio tracks | Independent audio profile |
| Subtitles and timing | NORMALIZED_CONTENT_EQUIVALENT |
| Chapters and attachments | EXACT |
| Color and HDR metadata | Explicit exact or bounded profile |
| Scene order and frame timing | Structural hard gate |

### 5.3 Image example

| Component | Required claim |
| --- | --- |
| Visible raster | NORMALIZED_CONTENT_EQUIVALENT or PERCEPTUALLY_EQUIVALENT |
| Geometry, crop, orientation, alpha | Structural hard gates |
| ICC profile and HDR data | Explicit requirement |
| EXIF, XMP, IPTC, captions | Explicit field or byte requirements |
| RAW source, layers, depth, motion component | Separate exact assets |

### 5.4 Application or game example

| Component | Required claim |
| --- | --- |
| Executables and signatures | ORIGINAL_BIT_EXACT or pinned source artifact |
| Saves, mods, configuration | ORIGINAL_BIT_EXACT |
| Dependency and load order | Deterministic semantic invariants |
| Launch and state loading | FUNCTIONALLY_EQUIVALENT |
| Store entitlement | Source and access evidence |

## 6. Validator profiles

Every validator profile must be immutable or content-addressed and include:

- Profile ID and version.
- Supported claim and subject.
- Implementation name, version, artifact digest, and runtime digest.
- Input contract and required coverage.
- Canonicalization and alignment rules.
- Metrics and decision rules.
- Pass, review, fail, and inconclusive behavior.
- Missing-data behavior.
- Calibration dataset and code digests.
- Full-reference, reduced-reference, or no-reference classification.
- Resource and privacy requirements.
- Fail-closed behavior.

Every metric declares:

- Name, version, units, and direction.
- Pass threshold.
- Review band.
- Hard-fail threshold.
- Required or advisory role.
- Segment, region, percentile, and aggregation rules.

Thresholds must be pinned before evaluating production candidates. Future library defaults cannot reinterpret a historical profile.

Every probabilistic detector, candidate generator, or acceptance validator references an immutable `EvaluationCorpusRecord` as defined in [File Identification and Extraction Requirements](file-identification-and-extraction.md). It records corpus provenance, license, consent, sensitivity, calibration and held-out partitions, difficult near-miss and out-of-domain slices, duplicate and training-overlap analysis, threshold-selection procedure, code and model digests, and human-test protocol where applicable.

Candidate generation is evaluated with recall-oriented discovery metrics. Recovery acceptance is evaluated separately with false-accept and false-reject rates plus confidence intervals or a conservative upper confidence bound for false acceptance. A model revision receives shadow evaluation and drift analysis before a historical threshold can be reused.

## 7. Reference-material classes

### Full-reference

The original content is needed during validation. This may be suitable while the original remains local, but cannot independently validate future recovery after the original is discarded.

### Reduced-reference

An authenticated compact descriptor is retained. Its sufficiency, privacy, size, and false-accept behavior must be measured.

### No-reference

The validator estimates quality without proving relation to the original. It cannot by itself justify original omission.

The manifest must include the size and protection requirements of retained reference material when calculating storage savings.

## 8. Media validation guidance

No metric or threshold below is a universal standard. Release profiles require corpus-specific calibration and human testing.

### 8.1 Images

Required structural gates may include geometry, crop, orientation, alpha semantics, bit depth, color profile, HDR metadata, required EXIF/XMP, and page count.

Candidate metric families:

- SSIM or MS-SSIM for structural signal similarity.
- LPIPS for learned perceptual difference.
- CIEDE2000 for color-critical differences.
- OCR and layout checks for text-bearing images.

Report global results and worst required regions. Perceptual hashes and CLIP/SigLIP scores are discovery evidence only.

### 8.2 Audio

Required structural gates may include stream inventory, channel layout, sample rate, duration, alignment, encoder delay, clipping, loudness, phase, and required metadata.

Candidate validators:

- Complete PCM hashes for normalized-content exactness.
- ViSQOL in a pinned audio or speech mode for perceptual evaluation.
- MUSHRA listening tests for profile calibration.
- WER or CER for speech-task preservation.
- Chromaprint for near-identical recording discovery.

An acoustic fingerprint does not establish retained quality, edition, master, lyrics, or source authenticity.

### 8.3 Video

Required structural gates may include frame order, timing, geometry, rotation, colorspace, HDR metadata, audio tracks, subtitles, chapters, and attachments.

Candidate validators:

- Canonical frame, PCM, subtitle, and timestamp hashes for normalized-content exactness.
- VMAF with a pinned model and declared viewing conditions.
- SSIM, MS-SSIM, PSNR, and artifact-specific checks such as CAMBI.
- Independent audio and subtitle validation.

Report mean, percentiles, worst scenes, and every structural failure. Visual quality cannot compensate for missing audio or subtitles.

## 9. VAE and generative representations

VAE, VQ-VAE, diffusion latents, foundation-model tokens, and neural codecs may be useful for:

- Search and clustering.
- Preview and streaming proxies.
- Explicitly approved consumption copies.
- Experimental learned compression.
- Disaster-recovery hints when no stronger representation survives.

They must not:

- Be called original restoration.
- Replace personal media, masters, projects, evidence, signed artifacts, or unknown data by default.
- Use semantic similarity to hide factual changes.
- Become the only representation without an explicit policy and successful validation.

A neural base layer may participate in exact recovery only when a lossless residual or entropy-coding construction reconstructs the original and the final cryptographic hash matches.

## 10. Approval model

Two approvals are distinct:

- **Omission approval:** permits the system to stop retaining stronger original material.
- **Restore acceptance:** accepts a particular weaker candidate during recovery.

Each approval binds:

- Approver identity and signature.
- Asset, entity, component, or bounded selector.
- Root subject digest.
- Allowed claim.
- Validator profile digest and thresholds.
- Reason, issue time, and expiry.
- Bulk-approval permission.
- Fallback policy.

Approval becomes stale when the original, source binding, processor or driver implementation, model, validator, threshold, policy, or protected component changes.

## 11. Fallback rules

Required actions include:

- Try another failure-independent source.
- Try an exact retained replica.
- Quarantine source drift or review-band candidates.
- Request human restore acceptance.
- Promote the original for storage while it still exists.
- Block when the validator or required decoder is unavailable.

No fallback may silently lower the requested claim. Non-passing candidates never overwrite the destination.

## 12. Anti-drift requirements

- Every candidate is compared with the authenticated root subject or retained root descriptor.
- A derivative is never accepted as the new reference merely because it previously passed.
- Repeated transcodes must not accumulate undetected quality loss.
- A changed decoder, normalizer, metric model, or threshold invalidates affected claims.
- Metric disagreement or out-of-domain content produces review or failure, not automatic acceptance.

## 13. Release gates

Before allowing automatic perceptual or semantic omission:

1. Build a target-corpus benchmark with approved positives and difficult near-miss negatives.
2. Measure false-accept and false-reject rates.
3. Run human calibration under declared viewing or listening conditions.
4. Retain original material through a grace period.
5. Perform cold restores without origin access.
6. Confirm zero silent promotions of substitutes to exact.
7. Confirm every missing component remains visible.
8. Confirm every accepted substitute has explicit authority and replayable evidence.
9. Use locked calibration and held-out test partitions with corpus rights and privacy records.
10. Report confidence bounds for false acceptance and separate discovery recall from acceptance error.
11. Shadow-test every model, preprocessing, decoder, or metric upgrade before threshold reuse.

A silent false-equivalence result is a release blocker.
