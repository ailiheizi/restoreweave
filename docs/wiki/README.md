# RestoreWeave Wiki

This is the short, plain-language guide to RestoreWeave. It explains the
current product without repeating every implementation rule. Normative
requirements live in the documents linked from the [documentation map](../README.md);
this Wiki must not become a second source of truth.

## Start here

- [Quick start](quick-start.md) — the normal configure, save, search, and restore loop.
- [Storage and capacity](storage-and-capacity.md) — what a storage scheme is, how exact deduplication works, and how space is reported.
- [Index status and search](index-status-and-search.md) — keyword, BGE, and future media indexes without confusing them with user tags.
- [Recovery and boundaries](recovery-and-boundaries.md) — what remains safe when indexes or optional models are unavailable.
- [Capability reference](capability-reference.md) — configuration, exact identity, notes, search, capacity, recovery, and current limits.

## The product in one sentence

RestoreWeave keeps exact file content safe, helps people find it with names,
notes, tags, and optional semantic indexes, and can restore the original bytes
without making a filesystem mount its core.

## Current state in plain language

- One configured content repository is used by the current development profile.
- Whole-file SHA-256 deduplication is the common identity rule.
- Keyword search is the baseline; the local BGE text profile is the default
  semantic direction but does not block saving a file.
- Image CLIP/SigLIP and music-feature indexes are optional future dimensions,
  not shipped default capabilities.
- “No user tag” and “index not ready” are system filters, not automatically
  written user tags.
- Capacity may be reported as unknown when the selected backend cannot provide
  a trustworthy value.

See [Remaining Work and Closed Decisions](../technical/remaining-work-and-closed-decisions.md)
and [ADR-009](../technical/build-stack-and-architecture-selection.md#adr-009-storage-schemes-and-derived-index-status)
for the recorded direction and its limits.
