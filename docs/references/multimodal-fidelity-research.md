# Multimodal Fidelity Research

## 1. Conclusion

Audio, image, and video can share one representation graph, plugin contract, policy language, and verification record format. They should not share one universal codec, embedding space, or “similar enough” threshold.

The robust pattern is:

~~~text
common control plane
+ modality-specific representations
+ component-specific preservation profiles
+ pinned validator profiles
+ explicit user authority
~~~

VAE and foundation codecs fit naturally as plugins, but their ordinary outputs are lossy or generative. They are suitable for proxies, search, and approved perceptual substitutes, not default archival recovery.

## 2. Why one score is insufficient

A file may contain multiple independently meaningful components:

- Image pixels, layers, alpha, color profile, metadata, depth, and motion.
- Audio streams, channel layout, timing, tags, lyrics, chapters, and artwork.
- Video picture, audio tracks, subtitles, chapters, fonts, color and HDR metadata, and edit timing.

A global metric can remain high while one critical component is missing. Every required component therefore needs structural gates and an independent outcome.

## 3. Image fidelity

### Image structural gates

- Width, height, crop, orientation, and aspect ratio.
- Alpha semantics, bit depth, and channel count.
- ICC profile, transfer function, gamut, and HDR metadata.
- Required EXIF, XMP, IPTC, captions, and sidecars.
- Page count and ordering for multi-page images.
- Separate presence of RAW, layers, depth, burst, and motion components.

### Image exact and normalized claims

- Original bytes: length plus SHA-256.
- Normalized pixels: pinned decoder, orientation, colorspace, alpha representation, and complete raster or tile hashes.

### Image perceptual evidence

- [SSIM](https://ece.uwaterloo.ca/~z70wang/publications/ssim.pdf) and MS-SSIM measure structural signal similarity.
- [LPIPS](https://arxiv.org/abs/1801.03924) measures learned perceptual distance.
- CIEDE2000 can support color-critical comparison.
- OCR and layout validators are required when text is meaningful.

Global averages can hide local damage. Profiles should retain tile or region distributions and worst required regions.

### Image discovery-only evidence

- Perceptual hashes.
- CLIP or SigLIP embeddings.
- Captions and object labels.

These help find candidates but cannot establish fidelity or source identity.

## 4. Audio fidelity

### Audio structural gates

- Stream inventory.
- Channel count and layout.
- Sample rate.
- Duration, alignment, and permitted encoder delay.
- Clipping, phase, silence trimming, and loudness policy.
- Required tags, chapters, cue sheets, lyrics, and artwork.

### Audio exact and normalized claims

- Original bytes: length plus SHA-256.
- Normalized audio: pinned decoder and complete integer PCM or chunk hashes under declared alignment rules.

### Audio perceptual evidence

- [ViSQOL](https://github.com/google/visqol) provides reference-based audio and speech quality assessment.
- [ITU-R BS.1534 MUSHRA](https://www.itu.int/rec/R-REC-BS.1534) provides a controlled listening-test method for codec evaluation.
- Segment-level distributions are more useful than one whole-track average.
- Speech profiles may add WER, CER, intelligibility, and speaker constraints.

ViSQOL scores depend on mode, sample rate, alignment, corpus, and implementation. They require a pinned profile and calibration.

### Audio identity and discovery evidence

- Chromaprint supports near-identical recording discovery.
- Provider IDs and ISRC support provenance.
- CLAP embeddings support semantic search.
- Custom DAC vectors support broad acoustic ranking.

None of these independently establish retained quality, exact master, edition, lyrics, or source authenticity.

## 5. Video fidelity

### Video structural gates

- Complete stream inventory.
- Frame count, order, timing, and time base.
- Geometry, crop, sample aspect ratio, and rotation.
- Colorspace, chroma siting, transfer function, and HDR metadata.
- Required audio tracks, subtitles, chapters, attachments, and fonts.
- No dropped, duplicated, frozen, or reordered required scenes.

### Video exact and normalized claims

- Original bytes: length plus SHA-256.
- Normalized content: pinned frame hashes, PCM hashes, subtitle hashes, and timestamp mapping.

### Video perceptual evidence

- [VMAF](https://github.com/Netflix/vmaf) predicts perceived video quality using a pinned model and viewing assumptions.
- SSIM, MS-SSIM, and PSNR provide complementary signal evidence.
- CAMBI or other artifact-specific metrics may detect banding hidden by a high aggregate score.
- Profiles should retain mean, percentiles, scene-level results, and worst required segments.

Visual quality cannot compensate for damaged audio, missing subtitles, altered timing, or lost HDR metadata.

### Video discovery-only evidence

- Keyframe embeddings.
- Scene embeddings.
- Video fingerprints.
- ASR transcripts.

These support search and candidate generation.

## 6. Perception-distortion boundary

[The perception-distortion tradeoff](https://arxiv.org/abs/1711.06077) shows that outputs can look perceptually realistic while differing from the original signal.

Generative systems may:

- Alter small text, faces, objects, or events.
- Smooth away evidence.
- Invent plausible textures.
- Change identity-sensitive detail.
- Produce high semantic similarity despite factual loss.

Therefore improved realism is not proof of lower factual distortion.

## 7. VAE and learned codecs

Relevant families:

- [Auto-Encoding Variational Bayes](https://arxiv.org/abs/1312.6114)
- [VQ-VAE](https://arxiv.org/abs/1711.00937)
- [CompressAI](https://github.com/InterDigitalInc/CompressAI)
- [EnCodec](https://arxiv.org/abs/2210.13438)

Appropriate RestoreWeave uses:

- Preview and streaming representations.
- Search and clustering.
- Explicitly approved perceptual copies.
- Experimental codec comparison.
- Base layers combined with a lossless residual.

Inappropriate uses:

- The sole copy of personal photographs or recordings.
- Creative masters and project sources.
- Document scans, medical or legal evidence, and signed media.
- Any content where subtle factual changes matter.

Required neural representation metadata:

- Encoder and decoder versions and digests.
- Model-weight digest and license.
- Latent or token schema.
- Precision, bitrate, resolution, sample rate, and parameters.
- Runtime and determinism status.
- Encoded bitstream digest.
- Metric implementations and model digests.
- Reference test vectors.
- Periodic decode drills.

## 8. Validator reference requirements

Validators are:

- **Full-reference:** require the original.
- **Reduced-reference:** require an authenticated compact witness.
- **No-reference:** estimate quality without proving relation to the original.

Full-reference metrics cannot validate future recovery after original deletion unless sufficient reference material is retained. No-reference quality metrics cannot justify identity.

The system must calculate the storage and privacy cost of witnesses, model weights, decoders, and calibration artifacts when estimating savings.

## 9. Threshold policy

No universal threshold is recommended in the base requirements.

Every release profile must:

- Name its target content and viewing or listening conditions.
- Define pass, review, and fail bands.
- Include hard structural gates.
- Report segment or region distributions.
- Bind metric implementation and model digests.
- Bind calibration data and code.
- Include difficult near-miss negatives.
- Report false-accept and false-reject behavior.
- Require explicit authority for weaker recovery.

Illustrative thresholds may be used during experiments, but they must never be presented as general truth.

## 10. High-risk media defaults

Default to original-byte protection for:

- Personal or family media.
- RAW media and sidecars.
- Masters, stems, layers, timelines, and project databases.
- Edited, annotated, or redacted derivatives.
- Evidence and signed objects.
- Multi-stream containers where components matter.
- Rare editions or uncertain provenance.
- Unknown and out-of-domain media.

## 11. Red-team corpus

The test corpus should include:

- Visually similar but wrong people, receipts, maps, or text.
- Cropped, mirrored, rotated, color-shifted, and metadata-stripped images.
- Original versus remaster, mono versus stereo, explicit versus clean, and live versus studio audio.
- Missing subtitles, alternate audio, chapters, fonts, HDR metadata, or frames.
- AI-generated imitations and semantic near-matches.
- Localized corruption hidden by a high global score.
- Repeated transcodes to detect cumulative drift.

A system that silently accepts any near-miss as exact or hides missing components fails the release gate.

## 12. Primary-source summary

| Source | Relation to RestoreWeave |
| --- | --- |
| [SSIM](https://ece.uwaterloo.ca/~z70wang/publications/ssim.pdf) | Structural image and video distortion evidence. |
| [LPIPS](https://arxiv.org/abs/1801.03924) | Learned perceptual image-distance evidence. |
| [ViSQOL](https://github.com/google/visqol) | Reference-based speech and audio quality evidence. |
| [VMAF](https://github.com/Netflix/vmaf) | Reference-based perceived video-quality evidence. |
| [MUSHRA](https://www.itu.int/rec/R-REC-BS.1534) | Human audio-codec calibration method. |
| [ITU-R BT.500](https://www.itu.int/rec/R-REC-BT.500) | Subjective television-picture assessment guidance. |
| [Perception-distortion tradeoff](https://arxiv.org/abs/1711.06077) | Explains why perceptual realism is not signal fidelity. |
| [EnCodec](https://arxiv.org/abs/2210.13438) | Neural audio representation and codec reference. |
| [CompressAI](https://github.com/InterDigitalInc/CompressAI) | Learned image and video compression framework. |
