# Decentralized Music Verification Audit

## 1. Conclusion

At commit [54ca190](https://github.com/ailiheizi/decentralized-music/tree/54ca190442c83f3d9857000e1a31a2d1d73072cb), decentralized-music does not implement a melody fingerprint.

It implements two DAC-code audio-similarity vectors:

- Version 1: a 576-dimensional bag-of-code histogram.
- Version 2: a 909-dimensional extension with coarse time segments and code-change rates.

These representations are useful for search, clustering, and broad acoustic candidate ranking. They may contribute to a DISCOVERY_MATCH claim. They are not sufficient for:

- Original byte identity.
- Normalized PCM equality.
- Audio-quality validation.
- Melody identity.
- Release, edition, master, lyrics, or source authentication.
- Automatic approval of a perceptual substitute.

## 2. Observed implementation

### Version 1

[extract-features.mjs](https://github.com/ailiheizi/decentralized-music/blob/54ca190442c83f3d9857000e1a31a2d1d73072cb/packages/publisher/extract-features.mjs) implements:

- Nine DAC codebooks.
- Code values from 0 to 1023.
- 64 pooled histogram bins per codebook.
- Per-codebook L1 normalization.
- Whole-vector L2 normalization for cosine similarity.
- Total dimension: 9 × 64 = 576.

The result is a bag-of-codes representation. It primarily captures broad timbral and spectral usage while discarding order.

### Version 2

[extract-features-v2.mjs](https://github.com/ailiheizi/decentralized-music/blob/54ca190442c83f3d9857000e1a31a2d1d73072cb/packages/publisher/extract-features-v2.mjs) adds:

- 288 dimensions of global histograms.
- 576 dimensions of four coarse time-segment histograms.
- 45 dimensions of global and segmented code-change rates.
- Total dimension: 909.

The source describes change rate as a rhythm or texture proxy. It does not extract pitch contours, notes, chords, or melody.

[search.mjs](https://github.com/ailiheizi/decentralized-music/blob/54ca190442c83f3d9857000e1a31a2d1d73072cb/packages/web/search.mjs) performs cosine similarity and top-K ranking.

Version 2 is optional for catalog building, while the ordinary publishing path and metadata schema still use the 576-value representation.

## 3. Documented but not independently reproduced

The repository documents:

- 67 percent same-folder nearest-neighbor accuracy on 12 songs.
- 0.98 similarity between one FLAC and Opus encoding of the same track.
- A 25 percent random baseline.

Sources:

- [README milestone](https://github.com/ailiheizi/decentralized-music/blob/54ca190442c83f3d9857000e1a31a2d1d73072cb/README.md)
- [Spike findings](https://github.com/ailiheizi/decentralized-music/blob/54ca190442c83f3d9857000e1a31a2d1d73072cb/docs/07_spike_findings.md)

The repository does not contain the real-music dataset, model weights, or generated catalog required to reproduce those measurements. No committed benchmark establishes a safe threshold for recovery decisions.

## 4. Role in RestoreWeave

Recommended role:

~~~text
SEMANTIC_REPRESENTATION
or
DISCOVERY_MATCH(audio.acoustic_character)
~~~

Prohibited direct roles:

~~~text
ORIGINAL_BIT_EXACT
NORMALIZED_CONTENT_EQUIVALENT
PERCEPTUALLY_EQUIVALENT
SOURCE_EQUIVALENT
~~~

The vector may help find candidates. A separate validator profile must determine whether a candidate is an acceptable substitute.

## 5. Audio evidence hierarchy

| Evidence | Claim supported | RestoreWeave role |
| --- | --- | --- |
| SHA-256 plus length | Exact byte identity | Authoritative for ORIGINAL_BIT_EXACT |
| Complete canonical PCM hashes | Exact decoded audio under a pinned decoder and alignment policy | NORMALIZED_CONTENT_EQUIVALENT |
| Provider ID, edition, MusicBrainz recording ID, ISRC, artist, title, album, and artifact digest | Source and provenance evidence | SOURCE_EQUIVALENT when complete and verified |
| [Chromaprint](https://github.com/acoustid/chromaprint) plus duration and structural gates | Near-identical recording candidate | Supporting evidence for a narrow perceptual profile |
| ViSQOL plus segment results and calibrated listening tests | Perceptual audio-quality evidence | PERCEPTUALLY_EQUIVALENT when policy-approved |
| DAC version 1 or 2 vector | Broad acoustic similarity | Search and review only |
| [audfprint](https://github.com/dpwe/audfprint) | Noisy fragment matching | Optional specialist candidate finder |
| [Panako](https://github.com/JorenSix/Panako) | Matching under pitch, speed, and time transformations | Specialist archival or DJ discovery |

Chromaprint itself states that it targets near-identical audio and is not a general-purpose fingerprint.

## 6. Required manifest data

Illustrative identity records:

~~~json
{
  "byte_identity": {
    "algorithm": "sha256",
    "digest": "...",
    "size": 12345678
  },
  "source_identity": {
    "provider": "...",
    "provider_track_id": "...",
    "edition": "...",
    "artifact_digest": "..."
  },
  "recording_evidence": {
    "algorithm": "chromaprint",
    "algorithm_version": "...",
    "duration_seconds": 241,
    "fingerprint": "..."
  }
}
~~~

If the custom DAC vector is retained:

~~~json
{
  "algorithm": "decentralized-music-dac-v1",
  "extractor_commit": "54ca190442c83f3d9857000e1a31a2d1d73072cb",
  "model_digest": "...",
  "codebooks": 9,
  "bins_per_codebook": 64,
  "analyzed_interval": {
    "start_seconds": 0,
    "duration_seconds": 241
  },
  "vector_encoding": "float16",
  "comparison_profile_digest": "sha256:..."
}
~~~

Without model, extractor, parameters, interval, comparator, threshold, and calibration identities, future results are not reproducible.

## 7. Required benchmark

The benchmark must include:

- Exact duplicate files.
- Lossless and lossy transcodes.
- Metadata-only changes.
- Trimming, silence insertion, time shifts, clipping, and channel changes.
- Original mix versus remaster.
- Mono versus stereo or spatial mix.
- Explicit versus clean version.
- Instrumental, karaoke, and vocal versions.
- Live versus studio.
- Radio edit and alternate take.
- Same melody with different lyrics.
- Covers and AI-generated imitations.
- Pitch and tempo changes.
- Truncated and damaged files.
- Unrelated tracks with similar genre and production.

The DAC vector remains advisory until a declared corpus establishes its candidate-ranking behavior and false-match risk.

## 8. Ownership and reuse note

No repository-level LICENSE, COPYING, or NOTICE file was observed at the audited commit, and repository metadata reported no license.

On 2026-08-11, the RestoreWeave project owner stated that decentralized-music is their own project and authorized its reuse for RestoreWeave. RestoreWeave may therefore refactor the audited implementation into an internal or separately versioned plugin, while retaining the exact source commit, model and weight licenses, dependency notices, and provenance.

Before distributing that code to third parties or accepting outside contributions against it, the owner should add an explicit repository license or an equivalent signed grant that defines redistribution and contribution terms. Authorization for the repository's original code does not replace the separate license terms of the upstream DAC model, weights, or dependencies.
