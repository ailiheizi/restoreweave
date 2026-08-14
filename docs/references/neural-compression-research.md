# Neural Compression Research

## 1. Conclusion

Neural models can participate in both exact and approximate compression, but the mechanism determines the guarantee.

- A predictive model followed by arithmetic or entropy coding can be genuinely lossless.
- A VAE, VQ-VAE, neural audio codec, image codec, or video tokenizer normally reconstructs an approximation.
- A latent-variable model may participate in lossless compression only when used in a reversible coding construction such as bits-back coding or when a lossless residual reconstructs the original.

RestoreWeave should expose all of these as later `Processor.TRANSFORM` capabilities, with independent `Processor.VALIDATE` evidence and distinct transformation classes and recovery claims.

## 2. L3TC and the RWKV memory

The most likely project behind the remembered “RWKV compression” work is:

- [L3TC: Leveraging RWKV for Learned Lossless Low-Complexity Text Compression](https://arxiv.org/abs/2412.16642)
- [L3TC repository](https://github.com/alipay/L3TC-leveraging-rwkv-for-learned-lossless-low-complexity-text-compression)

L3TC:

- Uses a small RWKV model to predict next-token probabilities.
- Uses arithmetic coding to encode the symbols reversibly.
- Includes an outlier-aware tokenizer for difficult bytes.
- Evaluates text-oriented data rather than general media archives.

The authors and repository are associated with Alipay or Ant and Shanghai Jiao Tong University, not the Qwen team. The Qwen association is likely a memory merge caused by organizational proximity.

The L3TC repository contains LEGAL.md but no conventional open-source software license. The paper can inform the design, but the implementation must not be assumed reusable.

## 3. Language Modeling Is Compression

[Language Modeling Is Compression](https://arxiv.org/abs/2309.10668) and its [reference implementation](https://github.com/google-deepmind/language_modeling_is_compression) demonstrate that predictive models can be paired with arithmetic coding across text, image, and audio symbol streams.

The key product lesson is:

> The model supplies probabilities; the entropy coder preserves every symbol.

This is different from asking a model to summarize or regenerate the content.

The implementation is useful for designing a generic predictor-plus-coder interface. Repository code is Apache-2.0.

## 4. VAE and VQ-VAE

- [Auto-Encoding Variational Bayes](https://arxiv.org/abs/1312.6114)
- [Neural Discrete Representation Learning](https://arxiv.org/abs/1711.00937)

Ordinary VAE and VQ-VAE decoding produces a likely reconstruction from a latent representation. It does not preserve the original byte stream and may alter fine factual detail.

Appropriate roles:

- Semantic and perceptual representations.
- Preview and streaming proxies.
- Explicit perceptual substitutes.
- Learned base layers combined with a residual.

Inappropriate default role:

- Authoritative recovery of user data.

## 5. Bits-back coding

[Practical Lossless Compression with Latent Variables using Bits Back Coding](https://arxiv.org/abs/1901.04866) demonstrates how latent-variable models can participate in a genuinely lossless coding system.

This is a useful boundary example: the presence of a VAE does not automatically make a format lossy or lossless. The complete coding construction and verified round trip determine the claim.

Bits-back systems remain experimental recovery dependencies and require:

- Exact model and coder versions.
- Deterministic probability behavior.
- Preserved latent and runtime conventions.
- Complete test vectors.
- Full decoded-byte verification.

## 6. Learned image, audio, and video codecs

### CompressAI

[CompressAI](https://github.com/InterDigitalInc/CompressAI) provides learned image and video compression models and tooling.

RestoreWeave role:

- Experimental lossy image and video `Processor.TRANSFORM` capabilities.
- Benchmarking rate, distortion, compute, and decoder dependencies.
- Possible exact base-plus-residual experiments.

Repository license: BSD-3-Clause-Clear.

### EnCodec

[EnCodec](https://arxiv.org/abs/2210.13438) is a neural audio codec.

RestoreWeave role:

- Low-bitrate proxy.
- Semantic or perceptual representation.
- Explicit consumption-copy profile after calibrated validation.

It does not reproduce the original audio bytes.

### Perception-distortion tradeoff

[The perception-distortion tradeoff](https://arxiv.org/abs/1711.06077) explains why a result can look or sound realistic while being less faithful to the original signal.

This prevents the product from treating “more plausible” as “better preserved.”

## 7. Exact versus approximate matrix

| Mechanism | Typical class | Maximum default claim |
| --- | --- | --- |
| Raw copy | BYTE_LOSSLESS_CODEC | ORIGINAL_BIT_EXACT |
| Zstandard | BYTE_LOSSLESS_CODEC | ORIGINAL_BIT_EXACT |
| RWKV or another predictor plus arithmetic coding | BYTE_LOSSLESS_CODEC | ORIGINAL_BIT_EXACT after full verification |
| Bits-back latent coding | BYTE_LOSSLESS_CODEC when the complete construction is reversible | ORIGINAL_BIT_EXACT after full verification |
| VAE or VQ-VAE latent | GENERATIVE_RECONSTRUCTION | Substitute or discovery only |
| CompressAI lossy model | LOSSY_TRANSCODE | PERCEPTUALLY_EQUIVALENT under an approved profile |
| EnCodec | LOSSY_TRANSCODE | PERCEPTUALLY_EQUIVALENT under an approved profile |
| CLIP, CLAP, or text embedding | SEMANTIC_REPRESENTATION | DISCOVERY_MATCH |
| Summary, caption, OCR, or ASR | SEMANTIC_REPRESENTATION | Derived evidence, not source recovery |
| Neural base plus complete lossless residual | BYTE_LOSSLESS_CODEC | ORIGINAL_BIT_EXACT after full verification |

## 8. Required plugin interface

Every experimental codec should implement:

- probe
- estimate
- encode
- decode
- verify
- dependencies
- capabilities

Required metadata:

- Codec ID, version, and parameters.
- Model, tokenizer, dictionary, and coder digests.
- Runtime, precision, and hardware assumptions.
- Original size and digest.
- Chunk boundaries where applicable.
- Encoded result size and digest.
- Decode resource estimates.
- Applicable media or corpus.
- License and redistribution constraints.
- Conventional fallback availability.

## 9. Optimizer requirements

The selector must calculate total recovery cost:

~~~text
encoded payload
+ model and tokenizer artifacts
+ dictionary or base objects
+ metadata and witnesses
+ compute and energy
+ restore latency
+ dependency and license risk
~~~

Model overhead may be amortized across a large homogeneous corpus but dominate a few small files.

The selector must:

- Use empirical samples from the target corpus.
- Reject candidates that fail required fidelity.
- Require a minimum net saving.
- Account for decode cost and offline availability.
- Prefer mature exact codecs for high-value and long-lived data.
- Keep a raw or Zstandard fallback for experimental exact codecs until repeated drills pass.

## 10. MVP recommendation

Include:

- Raw.
- Zstandard.
- Content-defined chunking and deduplication.
- A transformation-class-aware plugin API.
- Benchmark and round-trip harnesses.

Defer:

- L3TC or RWKV learned text compression.
- Bits-back implementations.
- Learned image, audio, and video codecs.
- Automatic neural codec selection.
- VAE latents as authoritative representations.

## 11. Research gates

Before an experimental codec becomes authoritative:

1. Confirm applicable license and redistribution rights.
2. Preserve source, decoder, model, runtime, and test-vector artifacts.
3. Pass complete encode-decode-byte-hash tests.
4. Test missing models, dictionaries, bases, accelerators, and network access.
5. Run cold restores on supported platforms.
6. Measure total savings after all dependencies.
7. Define a migration path to a conventional representation.
8. Demonstrate that plugin removal does not make historical data unreadable.

Neural compression is a valuable plugin family, not the durability foundation of RestoreWeave.
